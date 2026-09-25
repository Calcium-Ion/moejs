package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestArrayFromAsync runs Array.fromAsync programs in a mutable and a shared
// realm; the log is read after the jobs drain.
func TestArrayFromAsync(t *testing.T) {
	const show = `const show = p => p.then(a => log.push(Array.isArray(a) + " " + a.join()), e => log.push("rejected " + (e && e.name || e)));
`
	runAsyncCases(t, []protoCase{
		{"returns a promise", `const p = Array.fromAsync([]); log.push(p instanceof Promise, Array.fromAsync.length, Array.fromAsync.name); p.then(a => log.push(a.length))`,
			"true|1|fromAsync|0"},
		{"sync iterable awaits the values", show + `show(Array.fromAsync([1, Promise.resolve(2), { then(r) { r(3); } }]))`, "true 1,2,3"},
		{"async iterable", show + `async function* g() { yield 1; await null; yield Promise.resolve(2); } show(Array.fromAsync(g()))`, "true 1,2"},
		{"async iterable values are not awaited", `const p = Promise.resolve(1); const it = { [Symbol.asyncIterator]() { let i = 0; return { next() { return i++ ? { done: true } : { value: p, done: false }; } }; } };
			Array.fromAsync(it).then(a => log.push(a[0] === p))`, "true"},
		{"array-like", show + `show(Array.fromAsync({ length: 3, 0: "a", 1: Promise.resolve("b") }))`, "true a,b,"},
		{"mapper", show + `show(Array.fromAsync([1, 2], (x, i) => x * 10 + i)); show(Array.fromAsync({ length: 2, 0: 3, 1: 4 }, async (x, i) => { await null; return x + i; }))`,
			"true 3,5|true 10,21"},
		{"thisArg", `const o = {}; Array.fromAsync([1], function () { return this === o; }, o).then(a => log.push(a[0]))`, "true"},
		{"strings", show + `show(Array.fromAsync("ab"))`, "true a,b"},
		{"mapfn not callable rejects", show + `show(Array.fromAsync([], 1)); log.push("sync")`, "sync|rejected TypeError"},
		{"nullish items reject", show + `show(Array.fromAsync(null)); show(Array.fromAsync())`, "rejected TypeError|rejected TypeError"},
		{"element rejection rejects", show + `show(Array.fromAsync({ length: 1, 0: Promise.reject("e") }))`, "rejected e"},
		{"this constructor", `function C(...a) { this.args = a; } Array.fromAsync.call(C, [1, 2]).then(a => log.push(a instanceof C, a.args.length, a.length, a[1]));
			Array.fromAsync.call(C, { length: 2, 0: "x" }).then(a => log.push(a.args.join(), a.length))`, "2|2|true|0|2|2"},
		{"non-constructor this", `Array.fromAsync.call({}, [1]).then(a => log.push(Array.isArray(a)))`, "true"},
		{"too long", show + `show(Array.fromAsync({ length: 2 ** 32 }))`, "rejected RangeError"},
		{"order of operations", `const items = new Proxy({ length: 2, 0: "a", 1: "b" }, { get(t, k, r) { log.push("get " + String(k)); return Reflect.get(t, k, r); } });
			Array.fromAsync(items).then(a => log.push(a.join())); log.push("sync")`,
			"get Symbol(Symbol.asyncIterator)|get Symbol(Symbol.iterator)|get length|get 0|sync|get 1|a,b"},
		{"mapper throw closes the async iterator", `const it = { [Symbol.asyncIterator]() { return { next() { return Promise.resolve({ value: 1, done: false }); },
			return() { log.push("return"); return { then(r) { log.push("awaited"); r(); } }; } }; } };
			Array.fromAsync(it, () => { throw "m"; }).catch(e => log.push("rejected " + e))`, "return|awaited|rejected m"},
		{"mapper rejection closes the sync iterator", `const it = { [Symbol.iterator]() { return { next() { return { value: 1, done: false }; }, return() { log.push("return"); return {}; } }; } };
			Array.fromAsync(it, async () => { throw "m"; }).catch(e => log.push("rejected " + e))`, "return|rejected m"},
		// V8 leaves this promise pending.
		{"return errors lose to the thrown error", `const it = { [Symbol.asyncIterator]() { return { next() { return { value: 1, done: false }; }, return() { throw "r"; } }; } };
			Array.fromAsync(it, () => { throw "m"; }).catch(e => log.push("rejected " + e))`, "rejected m"},
		{"unsettable element closes", `function C() { Object.defineProperty(this, 0, { value: 0 }); }
			const it = { [Symbol.asyncIterator]() { return { next() { return { value: 1, done: false }; }, return() { log.push("return"); } }; } };
			Array.fromAsync.call(C, it).catch(e => log.push("rejected " + e.name))`, "return|rejected TypeError"},
		// The next result is awaited as for await does it; V8 closes the
		// iterator here.
		{"next rejection does not close", `const it = { [Symbol.asyncIterator]() { return { next() { return Promise.reject("n"); }, return() { log.push("return"); } }; } };
			Array.fromAsync(it).catch(e => log.push("rejected " + e))`, "rejected n"},
		{"result not an object", `const it = { [Symbol.asyncIterator]() { return { next() { return 1; } }; } }; Array.fromAsync(it).catch(e => log.push(e.name))`, "TypeError"},
		{"proxy traps reject", `const thrower = name => new Proxy({}, { get(t, k) { if (k === name) throw "trap " + String(k); } });
			Array.fromAsync(thrower(Symbol.asyncIterator)).catch(e => log.push(e));
			Array.fromAsync(thrower("length")).catch(e => log.push(e));
			Array.fromAsync({ [Symbol.asyncIterator]: () => thrower("next") }).catch(e => log.push(e))`,
			"trap Symbol(Symbol.asyncIterator)|trap length|trap next"},
		{"resolves to a thenable result", `function C() { this.then = r => r("adopted"); } Array.fromAsync.call(C, []).then(v => log.push(v))`, "adopted"},
	})
}

// TestArrayFromAsyncInterrupt checks that an interrupt in a mapper run from
// a job stops the call and leaves the realm reusable.
func TestArrayFromAsyncInterrupt(t *testing.T) {
	f := evalModule(t, `export let n = 0;
export function spin() { Array.fromAsync([1, 2], x => { if (x === 2) for (;;) {} n++; }); }
export async function one() { return (await Array.fromAsync([1])).length; }`)
	time.AfterFunc(20*time.Millisecond, func() { f.r.Interrupt("stop") })
	_, err := f.callErr("spin")
	var ie *InterruptedError
	require.ErrorAs(t, err, &ie)
	assert.Equal(t, int64(1), f.export("n"))
	assert.Zero(t, queued(f.r))
	f.r.ClearInterrupt()
	_, err = f.callErr("one")
	require.NoError(t, err)
}
