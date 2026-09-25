package engine

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAsyncFunctions runs async function programs in a mutable and a
// shared realm; the log is read after the jobs drain.
func TestAsyncFunctions(t *testing.T) {
	runAsyncCases(t, []protoCase{
		{"returns a promise", `async function f() { return 1; } const p = f(); log.push(p instanceof Promise, Object.getPrototypeOf(p) === Promise.prototype); p.then(v => log.push(v))`,
			"true|true|1"},
		{"body runs synchronously", `async function f() { log.push("body"); } f(); log.push("after")`, "body|after"},
		{"await suspends", `async function f() { log.push("a"); const v = await 1; log.push("b" + v); } f(); log.push("sync")`, "a|sync|b1"},
		{"await order", `async function f(n) { log.push(n + "1"); await null; log.push(n + "2"); await null; log.push(n + "3"); }
			f("x"); f("y"); Promise.resolve().then(() => log.push("p"))`, "x1|y1|x2|y2|p|x3|y3"},
		{"await a promise takes no extra ticks", `async function f() { await Promise.resolve(); log.push("f"); } f(); Promise.resolve().then(() => log.push("p1")).then(() => log.push("p2"))`,
			"f|p1|p2"},
		{"await a thenable", `async function f() { log.push(await { then(r) { r("t"); } }); } f()`, "t"},
		{"rejection throws at the await", `async function f() { try { await Promise.reject(new Error("r")); } catch (e) { log.push("caught " + e.message); } return "done"; } f().then(v => log.push(v))`,
			"caught r|done"},
		{"throw rejects", `async function f() { await 0; throw new TypeError("t"); } f().catch(e => log.push(e.name, e.message))`, "TypeError|t"},
		{"sync throw rejects", `async function f() { throw 1; } const p = f(); log.push("after"); p.catch(e => log.push("caught", e))`, "after|caught|1"},
		{"param error rejects", `async function f(a = (() => { throw "p"; })()) { log.push("body"); } f().catch(e => log.push(e))`, "p"},
		{"return a promise adopts it", `async function f() { return Promise.resolve(5); } f().then(v => log.push(v))`, "5"},
		{"return in finally", `async function f() { try { await 1; return "try"; } finally { log.push("fin"); } } f().then(v => log.push(v))`, "fin|try"},
		{"finally overrides", `async function f() { try { throw 1; } finally { return "fin"; } } f().then(v => log.push(v))`, "fin"},
		{"loops and closures", `async function f() { const fs = []; for (let i = 0; i < 3; i++) { await i; fs.push(() => i); } return fs.map(g => g()); } f().then(v => log.push(v.join()))`,
			"0,1,2"},
		{"this and arguments", `const o = { v: 3, async m() { await 0; return this.v + arguments.length; } }; o.m(1, 2).then(v => log.push(v))`, "5"},
		{"arrow", `const o = { v: 7, f() { return async () => { await 0; return this.v; }; } }; o.f()().then(v => log.push(v))`, "7"},
		{"arrow expression body", `const f = async x => x * 2; f(4).then(v => log.push(v)); const g = async () => { throw "e"; }; g().catch(e => log.push(e))`, "8|e"},
		{"arrow rejection", `const f = async () => await Promise.reject("x"); f().catch(e => log.push("r " + e))`, "r x"},
		{"class methods", `class A { async m() { return "a"; } static async s() { return "s"; } async #p() { return "p"; } q() { return this.#p(); } }
			Promise.all([new A().m(), A.s(), new A().q()]).then(v => log.push(v.join()))`, "a,s,p"},
		{"super in async method", `class A { m() { return "A"; } } class B extends A { async m() { await 0; return super.m() + "B"; } } new B().m().then(v => log.push(v))`, "AB"},
		{"nested awaits", `async function g(x) { await 0; return x + 1; } async function f() { return await g(await g(1)); } f().then(v => log.push(v))`, "3"},
		{"await in expressions", `async function f() { const o = { a: await 1, [await "k"]: 2 }; return [await 3 + await 4, o.a, o.k, ...[await 5]].join(); } f().then(v => log.push(v))`,
			"7,1,2,5"},
		{"await in switch and conditional", `async function f(x) { switch (await x) { case 1: return (await 0) ? "t" : "f"; default: return "d"; } } Promise.all([f(1), f(2)]).then(v => log.push(v.join()))`,
			"f,d"},
		{"await in try catch finally", `async function f() { let s = ""; try { s += await "a"; throw await "b"; } catch (e) { s += e + await "c"; } finally { s += await "d"; } return s; } f().then(v => log.push(v))`,
			"abcd"},
		{"for-of with await", `async function f() { let s = 0; for (const x of [1, 2, 3]) s += await x; return s; } f().then(v => log.push(v))`, "6"},
		{"generator inside async", `async function f() { function* g() { yield 1; yield 2; } let s = 0; for (const x of g()) s += await x; return s; } f().then(v => log.push(v))`, "3"},
		{"shape", `async function f(a, b) {} const AF = Object.getPrototypeOf(f); log.push(f.length, f.name, "prototype" in f, AF === Function.prototype, AF[Symbol.toStringTag],
			AF.constructor.name, Object.getPrototypeOf(AF.constructor) === Function, AF.constructor.prototype === AF, Object.getOwnPropertyNames(f).join())`,
			"2|f|false|false|AsyncFunction|AsyncFunction|true|true|length,name"},
		{"not a constructor", `async function f() {} try { new f(); } catch (e) { log.push(e.name); } const a = async () => {}; try { new a(); } catch (e) { log.push(e.name); }`,
			"TypeError|TypeError"},
		{"expression name", `const f = async function () {}; const g = async function h() { return typeof h; }; log.push(f.name, g.name); g().then(v => log.push(v))`, "f|h|function"},
		{"await resolves thenable getter error", `async function f() { try { await { get then() { throw "g"; } }; } catch (e) { log.push("caught " + e); } } f()`, "caught g"},
		{"many awaits", `async function f() { let s = 0; for (let i = 0; i < 1000; i++) s += await i; return s; } f().then(v => log.push(v))`, "499500"},
		{"recursion", `async function f(n) { return n === 0 ? 0 : 1 + await f(n - 1); } f(100).then(v => log.push(v))`, "100"},
	})
}

// TestAsyncThrowTrackerMovesStack checks that an async function whose body
// throws still returns its promise when the rejection tracker runs code
// that moves the register stack.
func TestAsyncThrowTrackerMovesStack(t *testing.T) {
	f := evalModule(t, `export function deep(n) { return n ? deep(n - 1) + 1 : 0; }
async function thrower() { throw 42; }
export function probe() { const p = thrower(); p.catch(() => {}); return String(p instanceof Promise) + " " + typeof p; }`)
	f.r.SetPromiseRejectionTracker(func(p *Object, op PromiseRejectionOperation) {
		if op == PromiseRejectionReject {
			f.call("deep", 300)
		}
	})
	assert.Equal(t, "true object", f.call("probe"))
}

// TestAsyncGenerators runs async generator programs in a mutable and a
// shared realm: the request queue, return and throw before, at and after
// the body, and the intrinsics.
func TestAsyncGenerators(t *testing.T) {
	runAsyncCases(t, []protoCase{
		{"basic", `async function* g() { yield 1; yield 2; return 3; } const it = g(); it.next().then(r => log.push(r.value, r.done)); it.next().then(r => log.push(r.value, r.done)); it.next().then(r => log.push(r.value, r.done)); it.next().then(r => log.push(r.value, r.done))`,
			"1|false|2|false|3|true||true"},
		{"body starts at first next", `async function* g() { log.push("start"); yield 1; } const it = g(); log.push("created"); it.next(); log.push("after next")`,
			"created|start|after next"},
		{"yield awaits operand", `async function* g() { yield Promise.resolve("p"); } g().next().then(r => log.push(r.value))`,
			"p"},
		{"yield rejected operand throws", `async function* g() { try { yield Promise.reject("r"); } catch (e) { log.push("caught " + e); yield "after"; } } const it = g(); it.next().then(r => log.push(r.value))`,
			"caught r|after"},
		{"await inside", `async function* g() { const a = await 1; const b = await Promise.resolve(2); yield a + b; } g().next().then(r => log.push(r.value))`,
			"3"},
		{"queued requests", `async function* g() { log.push("s"); yield 1; log.push("m"); yield 2; log.push("e"); } const it = g(); const ps = [it.next(), it.next(), it.next(), it.next()]; log.push("sync"); Promise.all(ps).then(rs => log.push(rs.map(r => r.value + ":" + r.done).join()))`,
			"s|sync|m|e|1:false,2:false,undefined:true,undefined:true"},
		{"next values", `async function* g() { const a = yield 1; log.push("a=" + a); const b = yield 2; log.push("b=" + b); } const it = g(); it.next("x"); it.next("y"); it.next("z")`,
			"a=y|b=z"},
		{"return before start", `async function* g() { log.push("never"); } const it = g(); it.return(5).then(r => log.push(r.value, r.done)); it.next().then(r => log.push(r.value, r.done))`,
			"5|true||true"},
		{"return awaits value", `async function* g() {} const it = g(); it.return(Promise.resolve(7)).then(r => log.push(r.value, r.done))`,
			"7|true"},
		{"return rejected value", `async function* g() {} const it = g(); it.return(Promise.reject("no")).catch(e => log.push("rej " + e))`,
			"rej no"},
		{"return at yield runs finally", `async function* g() { try { yield 1; } finally { log.push("fin"); } } const it = g(); it.next().then(() => it.return(9)).then(r => log.push(r.value, r.done))`,
			"fin|9|true"},
		{"return at yield awaits", `async function* g() { try { yield 1; } finally { log.push("fin"); } } const it = g(); it.next(); it.return(Promise.resolve("v")).then(r => log.push(r.value, r.done))`,
			"fin|v|true"},
		{"return at yield rejected throws in body", `async function* g() { try { yield 1; } catch (e) { log.push("caught " + e); yield 2; } } const it = g(); it.next(); it.return(Promise.reject("x")).then(r => log.push(r.value, r.done))`,
			"caught x|2|false"},
		{"finally yields after return", `async function* g() { try { yield 1; } finally { yield "f"; log.push("after f"); } } const it = g(); it.next(); it.return(3).then(r => log.push(r.value, r.done)); it.next().then(r => log.push(r.value, r.done))`,
			"after f|f|false|3|true"},
		{"throw before start", `async function* g() { log.push("never"); } const it = g(); it.throw("t").catch(e => log.push("rej " + e)); it.next().then(r => log.push(r.value, r.done))`,
			"rej t||true"},
		{"throw at yield", `async function* g() { try { yield 1; } catch (e) { log.push("caught " + e); yield 2; } } const it = g(); it.next(); it.throw("t").then(r => log.push(r.value, r.done))`,
			"caught t|2|false"},
		{"throw uncaught rejects", `async function* g() { yield 1; } const it = g(); it.next(); it.throw("t").catch(e => log.push("rej " + e)); it.next().then(r => log.push(r.value, r.done))`,
			"rej t||true"},
		{"body throws", `async function* g() { yield 1; throw new Error("boom"); } const it = g(); it.next(); it.next().catch(e => log.push(e.message)); it.next().then(r => log.push(r.value, r.done))`,
			"boom||true"},
		{"return completes", `async function* g() { return Promise.resolve("r"); } g().next().then(r => log.push(r.value, r.done))`,
			"r|true"},
		{"return rejected in body", `async function* g() { try { return Promise.reject("x"); } catch (e) { log.push("caught " + e); return "y"; } } g().next().then(r => log.push(r.value, r.done))`,
			"caught x|y|true"},
		{"draining queue", `async function* g() { yield 1; } const it = g(); const ps = [it.next(), it.next(), it.throw("t"), it.return("r"), it.next()]; Promise.allSettled(ps).then(rs => log.push(rs.map(r => r.status + ":" + (r.value ? r.value.value + "/" + r.value.done : r.reason)).join()))`,
			"fulfilled:1/false,fulfilled:undefined/true,rejected:t,fulfilled:r/true,fulfilled:undefined/true"},
		{"interleaving", `async function* g() { yield 1; yield 2; } const it = g(); it.next().then(r => log.push("n1 " + r.value)); Promise.resolve().then(() => log.push("p1")).then(() => log.push("p2")).then(() => log.push("p3")); it.next().then(r => log.push("n2 " + r.value))`,
			"p1|n1 1|p2|n2 2|p3"},
		{"shape", `async function* g(a) {} const AGF = Object.getPrototypeOf(g); const AGP = AGF.prototype; const AIP = Object.getPrototypeOf(AGP);
			log.push(g.length, g.name, Object.getOwnPropertyNames(g).join(), AGF[Symbol.toStringTag], AGP[Symbol.toStringTag], AGF.constructor.name, AGF.constructor.length,
			Object.getPrototypeOf(AGF.constructor) === Function, AGP.constructor === AGF, Object.getPrototypeOf(g.prototype) === AGP, Object.getPrototypeOf(g()) === g.prototype,
			AIP[Symbol.asyncIterator].call(5), AIP[Symbol.asyncIterator].name, Object.getPrototypeOf(AIP) === Object.prototype, AGP.next.length, typeof AGP.return, typeof AGP.throw,
			Object.prototype.toString.call(g()), Object.getOwnPropertyDescriptor(g, "prototype").writable, Object.getOwnPropertyDescriptor(AGF, "prototype").writable)`,
			"1|g|length,name,prototype|AsyncGeneratorFunction|AsyncGenerator|AsyncGeneratorFunction|1|true|true|true|true|5|[Symbol.asyncIterator]|true|1|function|function|[object AsyncGenerator]|true|false"},
		{"not a constructor", `async function* g() {} try { new g(); } catch (e) { log.push(e.name); } const o = { async *m() {} }; try { new o.m(); } catch (e) { log.push(e.name); }`,
			"TypeError|TypeError"},
		{"incompatible receiver", `async function* g() {} const n = g().next; n.call({}).catch(e => log.push(e.constructor.name)); g.prototype.return.call(1).catch(e => log.push(e.constructor.name))`,
			"TypeError|TypeError"},
		{"prototype fallback", `async function* g() { yield 1; } g.prototype = null; const it = g(); log.push(Object.getPrototypeOf(it) === Object.getPrototypeOf(async function*(){}).prototype)`,
			"true"},
		{"methods and classes", `class A { async *m() { yield this.v; } static async *s() { yield "s"; } constructor() { this.v = "m"; } } new A().m().next().then(r => log.push(r.value)); A.s().next().then(r => log.push(r.value)); const o = { async *[Symbol.asyncIterator]() { yield "o"; } }; o[Symbol.asyncIterator]().next().then(r => log.push(r.value))`,
			"m|s|o"},
		{"arguments and this", `async function* g() { yield arguments.length; yield this.x; } const it = g.call({ x: "t" }, 1, 2); it.next().then(r => log.push(r.value)); it.next().then(r => log.push(r.value))`,
			"2|t"},
		{"generator request from inside body", `let it; async function* g() { const p = it.next("inner"); log.push("queued"); const x = yield 1; log.push("x=" + x); } it = g(); it.next().then(r => log.push("r1 " + r.value))`,
			"queued|x=inner|r1 1"},
		{"return in async gen awaits", `async function* g() { return Promise.resolve("a"); } const it = g(); it.next().then(r => log.push(r.value, r.done))`,
			"a|true"},
		{"deep yield loop", `async function* g() { for (let i = 0; i < 500; i++) yield i; } (async () => { let s = 0; for await (const v of g()) s += v; log.push(s); })()`,
			"124750"},
		{"many queued", `async function* g() { for (let i = 0; i < 100; i++) yield i; } const it = g(); const ps = []; for (let i = 0; i < 101; i++) ps.push(it.next()); Promise.all(ps).then(rs => log.push(rs.filter(r => !r.done).length, rs[100].done))`,
			"100|true"},
		{"async gen method name", `const o = { async *gen() {} }; log.push(o.gen.name, typeof o.gen.prototype)`,
			"gen|object"},
		{"await in async gen default param", `async function* g(a = 1) { yield a; } g().next().then(r => log.push(r.value))`,
			"1"},
		{"param error rejects first next", `async function* g(a = (() => { throw "p"; })()) { yield 1; } try { g(); } catch (e) { log.push("sync " + e); }`,
			"sync p"},
	})
}

// TestAsyncIteration runs for await and async yield* over async and sync
// iterables (the latter through async-from-sync iterators).
func TestAsyncIteration(t *testing.T) {
	runAsyncCases(t, []protoCase{
		{"for await over async gen", `async function* g() { yield 1; yield 2; yield 3; } (async () => { let s = 0; for await (const x of g()) s += x; log.push(s); })()`,
			"6"},
		{"for await over sync iterable", `(async () => { const out = []; for await (const x of [1, Promise.resolve(2), 3]) out.push(x); log.push(out.join()); })()`,
			"1,2,3"},
		{"for await break closes", `async function* g() { try { yield 1; yield 2; } finally { log.push("closed"); } } (async () => { for await (const x of g()) { log.push(x); break; } log.push("after"); })()`,
			"1|closed|after"},
		{"for await throw closes", `async function* g() { try { yield 1; } finally { log.push("closed"); } } (async () => { try { for await (const x of g()) throw "t"; } catch (e) { log.push("caught " + e); } })()`,
			"closed|caught t"},
		{"for await return closes", `async function* g() { try { yield 1; } finally { log.push("closed"); } } (async () => { for await (const x of g()) return x; })().then(v => log.push("ret " + v))`,
			"closed|ret 1"},
		{"for await sync rejected value closes", `const it = { i: 0, [Symbol.iterator]() { return this; }, next() { return { value: Promise.reject("r"), done: false }; }, return() { log.push("return"); return {}; } };
			(async () => { try { for await (const x of it) log.push("never"); } catch (e) { log.push("caught " + e); } })()`,
			"return|caught r"},
		{"for await next rejects does not close", `const it = { [Symbol.asyncIterator]() { return this; }, next() { return Promise.reject("n"); }, return() { log.push("return"); return {}; } };
			(async () => { try { for await (const x of it) log.push("never"); } catch (e) { log.push("caught " + e); } })()`,
			"caught n"},
		{"for await non-object result", `const it = { [Symbol.asyncIterator]() { return this; }, next() { return 5; } }; (async () => { try { for await (const x of it); } catch (e) { log.push(e.constructor.name); } })()`,
			"TypeError"},
		{"for await not iterable", `(async () => { try { for await (const x of 5); } catch (e) { log.push(e.constructor.name); } try { for await (const x of null); } catch (e) { log.push(e.constructor.name); } })()`,
			"TypeError|TypeError"},
		{"for await return method non-object", `const it = { [Symbol.asyncIterator]() { return this; }, next() { return { value: 1, done: false }; }, return() { return 1; } };
			(async () => { try { for await (const x of it) break; } catch (e) { log.push(e.constructor.name); } })()`,
			"TypeError"},
		{"for await return rejects on break", `const it = { [Symbol.asyncIterator]() { return this; }, next() { return { value: 1, done: false }; }, return() { return Promise.reject("rr"); } };
			(async () => { try { for await (const x of it) break; } catch (e) { log.push("caught " + e); } })()`,
			"caught rr"},
		{"for await throw ignores return error", `const it = { [Symbol.asyncIterator]() { return this; }, next() { return { value: 1, done: false }; }, return() { throw "re"; } };
			(async () => { try { for await (const x of it) throw "body"; } catch (e) { log.push("caught " + e); } })()`,
			"caught body"},
		{"for await destructuring", `async function* g() { yield [1, 2]; yield [3, 4]; } (async () => { const out = []; for await (const [a, b] of g()) out.push(a * b); log.push(out.join()); })()`,
			"2,12"},
		{"for await continue and labels", `async function* g() { yield 1; yield 2; yield 3; } (async () => { const out = []; outer: for await (const x of g()) { for await (const y of g()) { if (y === 2) continue outer; out.push(x + "" + y); } } log.push(out.join()); })()`,
			"11,21,31"},
		{"for await order", `async function* g() { yield 1; yield 2; } (async () => { for await (const x of g()) log.push("x" + x); })(); Promise.resolve().then(() => log.push("p1")).then(() => log.push("p2")).then(() => log.push("p3")).then(() => log.push("p4"))`,
			"p1|x1|p2|p3|x2|p4"},
		{"for await in async gen", `async function* g() { for await (const x of [1, 2]) yield x * 10; } (async () => { const out = []; for await (const v of g()) out.push(v); log.push(out.join()); })()`,
			"10,20"},
		{"for await lhs expression", `(async () => { const o = {}; for await (o.x of [1, 2]); log.push(o.x); let y; for await (y of [3]); log.push(y); })()`,
			"2|3"},
		{"yield* async", `async function* inner() { const x = yield 1; log.push("inner got " + x); return "r"; } async function* outer() { const v = yield* inner(); log.push("v=" + v); yield 2; } const it = outer(); it.next().then(r => log.push(r.value)); it.next("a").then(r => log.push(r.value))`,
			"inner got a|1|v=r|2"},
		{"yield* sync iterable", `async function* g() { yield* [1, Promise.resolve(2)]; } (async () => { const out = []; for await (const v of g()) out.push(v); log.push(out.join()); })()`,
			"1,2"},
		{"yield* return forwards", `async function* inner() { try { yield 1; } finally { log.push("inner fin"); } } async function* outer() { try { yield* inner(); } finally { log.push("outer fin"); } } const it = outer(); it.next(); it.return("R").then(r => log.push(r.value, r.done))`,
			"inner fin|outer fin|R|true"},
		{"yield* throw forwards", `async function* inner() { try { yield 1; } catch (e) { log.push("inner " + e); yield 2; } } async function* outer() { yield* inner(); } const it = outer(); it.next(); it.throw("T").then(r => log.push(r.value, r.done))`,
			"inner T|2|false"},
		{"yield* throw without throw method", `const inner = { [Symbol.asyncIterator]() { return this; }, next() { return { value: 1, done: false }; }, return() { log.push("return called"); return {}; } };
			async function* outer() { try { yield* inner; } catch (e) { log.push(e.constructor.name); } } const it = outer(); it.next(); it.throw("x").then(r => log.push(r.value, r.done))`,
			"return called|TypeError||true"},
		{"yield* return without return method", `const inner = { [Symbol.asyncIterator]() { return this; }, next() { return { value: 1, done: false }; } };
			async function* outer() { try { yield* inner; } finally { log.push("fin"); } } const it = outer(); it.next(); it.return(Promise.resolve("rv")).then(r => log.push(r.value, r.done))`,
			"fin|rv|true"},
		{"yield* inner return value not awaited", `const inner = value => ({ [Symbol.asyncIterator]() { return this; }, next() { return { value: 1, done: false }; }, return() { return { value, done: true }; } });
			async function* g() { yield* inner(Promise.resolve(8)); } const gi = g(); gi.next().then(() => gi.return()).then(r => log.push(r.value instanceof Promise, r.done))`,
			"true|true"},
		{"yield* inner return rejected value not thrown", `const inner = value => ({ [Symbol.asyncIterator]() { return this; }, next() { return { value: 1, done: false }; }, return() { return { value, done: true }; } });
			async function* g() { try { yield* inner(Promise.reject("boom")); } catch (e) { log.push("caught " + e); yield "resumed"; } }
			const gi = g(); gi.next().then(() => gi.return()).then(r => { r.value.catch(e => log.push("value rejects " + e)); log.push(r.value instanceof Promise, r.done); }, e => log.push("rejected " + e))`,
			"true|true|value rejects boom"},
		{"yield* inner return thenable value not read", `let reads = 0; const inner = { [Symbol.asyncIterator]() { return this; }, next() { return { value: 1, done: false }; }, return() { return { value: { get then() { reads++; } }, done: true }; } };
			async function* g() { yield* inner; } const gi = g(); gi.next().then(() => gi.return()).then(r => log.push(reads, r.done))`,
			"0|true"},
		// node's ticks: the return through yield* awaits once less when the
		// inner iterator has a return method.
		{"yield* return ticks", `const syncIt = { [Symbol.iterator]() { return { next() { return { value: 1, done: false }; }, return(v) { return { value: v, done: true }; } }; } };
			async function* ag() { yield 1; }
			const noReturn = { [Symbol.asyncIterator]() { return { next() { return { value: 1, done: false }; } }; } };
			const ticks = async (name, it) => {
				async function* g() { try { yield* it; } finally { log.push(name + " finally " + tick); } }
				let tick = 0, running = true;
				const gi = g(); await gi.next();
				const count = () => { if (running) { tick++; Promise.resolve().then(count); } };
				Promise.resolve().then(count);
				const r = await gi.return(42);
				log.push(name + " ret " + tick + " " + r.value);
				running = false;
			};
			(async () => { await ticks("sync", syncIt); await ticks("async", ag()); await ticks("none", noReturn); })()`,
			"sync finally 3|sync ret 4 42|async finally 3|async ret 4 42|none finally 2|none ret 3 42"},
		{"yield* does not await inner values", `const inner = { [Symbol.asyncIterator]() { return this; }, next() { return { value: Promise.resolve(1), done: false }; } };
			async function* outer() { yield* inner; } outer().next().then(r => log.push(r.value instanceof Promise))`,
			"true"},
		{"yield* inner result getters", `const inner = { [Symbol.asyncIterator]() { return this; }, next() { return { get done() { log.push("done"); return false; }, get value() { log.push("value"); return 1; } }; } };
			async function* outer() { yield* inner; } outer().next().then(r => log.push(r.value))`,
			"done|value|1"},
		{"yield* non-object result", `const inner = { [Symbol.asyncIterator]() { return this; }, next() { return 1; } }; async function* outer() { yield* inner; } outer().next().catch(e => log.push(e.constructor.name))`,
			"TypeError"},
		{"yield* sync iterator rejected value closes", `const it = { [Symbol.iterator]() { return this; }, next() { return { value: Promise.reject("r"), done: false }; }, return() { log.push("return"); return {}; } };
			async function* g() { yield* it; } g().next().catch(e => log.push("rej " + e))`,
			"return|rej r"},
		{"sync wrapper throw without throw method", `const it = { [Symbol.iterator]() { return this; }, next() { return { value: 1, done: false }; }, return() { log.push("return"); return {}; } };
			async function* g() { yield* it; } const gi = g(); gi.next(); gi.throw("x").catch(e => log.push(e.constructor.name))`,
			"return|TypeError"},
		{"sync wrapper return without return method", `const it = { [Symbol.iterator]() { return this; }, next() { return { value: 1, done: false }; } };
			async function* g() { yield* it; } const gi = g(); gi.next(); gi.return("rv").then(r => log.push(r.value, r.done))`,
			"rv|true"},
		{"sync wrapper done value not closed", `const it = { [Symbol.iterator]() { return this; }, next() { return { value: Promise.reject("d"), done: true }; }, return() { log.push("return"); return {}; } };
			(async () => { try { for await (const x of it); } catch (e) { log.push("caught " + e); } })()`,
			"caught d"},
	})
}

// loopCloser is the prelude of TestForAwaitCloseAtLoopLevel: it(name, ret)
// is an async iterable that logs its steps and whose return method returns
// ret(name), rej a return method that rejects, and report logs how the
// promise p settles.
const loopCloser = `const it = (name, ret) => ({ [Symbol.asyncIterator]() { let n = 0; return {
	next() { log.push(name + " next " + (++n)); return Promise.resolve({ value: n, done: false }); },
	return() { log.push(name + " return"); return ret(name); } }; } });
const rej = name => Promise.reject(new Error(name + " rej"));
const report = p => p.then(v => log.push("= " + v), e => log.push("threw " + (e instanceof TypeError ? "TypeError" : e.message)));
`

// TestForAwaitCloseAtLoopLevel exits for await loops from inside try
// statements of their bodies: the iterator closes where the loop completes,
// so a rejecting or non-object return() replaces the exit's completion past
// the body's catch clauses and closes the loops around it with the throw
// completion. The logs are node's.
func TestForAwaitCloseAtLoopLevel(t *testing.T) {
	runAsyncCases(t, []protoCase{
		{"return in try", loopCloser + `report((async () => { for await (const x of it("a", rej)) { try { return x; } catch (e) { log.push("inner " + e.message); } } })())`,
			"a next 1|a return|threw a rej"},
		{"generator return at yield in try", loopCloser + `async function* g() { for await (const x of it("g", rej)) { try { yield x; } catch (e) { log.push("inner " + e.message); } } }
			(async () => { const i = g(); await i.next(); await report(i.return("R").then(JSON.stringify)); log.push("next " + JSON.stringify(await i.next())); })()`,
			`g next 1|g return|threw g rej|next {"done":true}`},
		{"break outer with non-object result", loopCloser + `report((async () => { outer: for (const y of [1]) { for await (const x of it("b", () => Promise.resolve(5))) { try { break outer; } catch (e) { log.push("inner " + e.message); } } } return "end"; })())`,
			"b next 1|b return|threw TypeError"},
		{"continue outer through finally", loopCloser + `report((async () => { outer: for (const y of [1, 2]) { for await (const x of it("c" + y, rej)) { try { try { continue outer; } finally { log.push("fin"); } } catch (e) { log.push("inner " + e.message); } } } return "end"; })())`,
			"c1 next 1|fin|c1 return|threw c1 rej"},
		{"try around the loop catches", loopCloser + `report((async () => { let r = 0; for (const o of [1, 2]) { try { for await (const x of it("d" + o, rej)) { try { break; } catch (e) { log.push("inner " + e.message); } } } catch (e) { log.push("caught " + e.message); r++; } } return r; })())`,
			"d1 next 1|d1 return|caught d1 rej|d2 next 1|d2 return|caught d2 rej|= 2"},
		{"outer close rejects", loopCloser + `report((async () => { for await (const x of it("e1", rej)) { for await (const y of it("e2", () => Promise.resolve({}))) { try { return "ret"; } catch (e) { log.push("inner " + e.message); } } } })())`,
			"e1 next 1|e2 next 1|e2 return|e1 return|threw e1 rej"},
		{"outer body catches inner close", loopCloser + `report((async () => { for await (const x of it("h1", () => Promise.resolve({}))) { try { for await (const y of it("h2", rej)) { try { break; } catch (e) { log.push("inner2 " + e.message); } } } catch (e) { log.push("inner1 " + e.message); } if (x === 2) break; } return "end"; })())`,
			"h1 next 1|h2 next 1|h2 return|inner1 h2 rej|h1 next 2|h2 next 1|h2 return|inner1 h2 rej|h1 return|= end"},
	})
}

// TestAsyncProxyTraps checks that what a proxy trap throws in an await or
// an async iteration step rejects or throws where the step stands.
func TestAsyncProxyTraps(t *testing.T) {
	const thrower = `const thrower = name => new Proxy({}, { get(t, k) { if (k === name) throw "trap " + String(k); } });
`
	runAsyncCases(t, []protoCase{
		{"await reads then through the trap", thrower + `(async () => { try { await thrower("then"); } catch (e) { log.push(e); } })()`, "trap then"},
		{"await a proxy thenable", `(async () => { log.push(await new Proxy({}, { get(t, k) { return k === "then" ? r => r("via trap") : undefined; } })); })()`, "via trap"},
		{"for await asyncIterator trap", thrower + `(async () => { try { for await (const x of thrower(Symbol.asyncIterator)); } catch (e) { log.push(e); } })()`,
			"trap Symbol(Symbol.asyncIterator)"},
		{"async-from-sync done trap", `const it = { [Symbol.iterator]() { return { next() { return new Proxy({}, { get(t, k) { if (k === "done") throw "done trap"; } }); } }; } };
			(async () => { try { for await (const x of it); } catch (e) { log.push(e); } })()`, "done trap"},
		{"for await value trap", `const it = { [Symbol.asyncIterator]() { return { next() { return Promise.resolve(new Proxy({ done: false }, { get(t, k) { if (k === "value") throw "value trap"; return t[k]; } })); } }; } };
			(async () => { try { for await (const x of it); } catch (e) { log.push(e); } })()`, "value trap"},
		{"async yield* next trap", thrower + `const it = { [Symbol.asyncIterator]() { return thrower("next"); } }; async function* g() { yield* it; }
			g().next().catch(e => log.push(e))`, "trap next"},
		{"for await through a proxy iterator", `const target = { i: 0, next() { return Promise.resolve(this.i < 2 ? { value: this.i++, done: false } : { done: true }); } };
			const p = new Proxy(target, { get(t, k, r) { log.push("get " + String(k)); return Reflect.get(t, k, r); } });
			(async () => { for await (const x of { [Symbol.asyncIterator]: () => p }) log.push("x" + x); })()`,
			"get next|get i|get i|x0|get i|get i|x1|get i"},
		{"break return trap", `const it = { [Symbol.asyncIterator]() { return new Proxy({ next() { return Promise.resolve({ value: 1, done: false }); } }, { get(t, k) { if (k === "return") throw "return trap"; return t[k]; } }); } };
			(async () => { try { for await (const x of it) break; } catch (e) { log.push(e); } })()`, "return trap"},
		{"proxy of an async generator", `async function* g() {} new Proxy(g(), {}).next().catch(e => log.push(e.constructor.name))`, "TypeError"},
	})
}

// TestAsyncIntrinsicsPerRealm checks that the shared realms freeze the
// async function and async generator intrinsics and that a mutable realm
// builds its own.
func TestAsyncIntrinsicsPerRealm(t *testing.T) {
	const src = `const AFP = Object.getPrototypeOf(async function () {});
const AGFP = Object.getPrototypeOf(async function* () {});
const AGP = AGFP.prototype, AIP = Object.getPrototypeOf(AGP);
export const frozen = [AFP, AFP.constructor, AGFP, AGFP.constructor, AGP, AIP].map(Object.isFrozen).join();`
	s := evalModuleWith(t, src, RealmOptions{SharedIntrinsics: true})
	assert.Equal(t, "true,true,true,true,true,true", s.export("frozen"))
	m := evalModule(t, src)
	assert.Equal(t, "false,false,false,false,false,false", m.export("frozen"))
	r := NewRealm()
	r.generatorIntrinsics()
	assert.Nil(t, r.async.AsyncFunctionPrototype, "generators alone do not build the async intrinsics")
}

// TestSharedAsyncIntrinsicsOnFirstUse checks that the shared template
// leaves the async intrinsics out and that the shared realms build them
// once per process: realms created before and after the build, and realms
// racing to build them, get the same frozen objects.
func TestSharedAsyncIntrinsicsOnFirstUse(t *testing.T) {
	early := NewRealmWith(RealmOptions{SharedIntrinsics: true})
	assert.Nil(t, sharedTpl.async.AsyncFunctionPrototype, "the template has no async intrinsics")
	got := make([]*asyncIntrinsics, 8)
	var wg sync.WaitGroup
	for i := range got {
		wg.Go(func() { got[i] = NewRealmWith(RealmOptions{SharedIntrinsics: true}).asyncIntr() })
	}
	wg.Wait()
	late := NewRealmWith(RealmOptions{SharedIntrinsics: true})
	ai := got[0]
	require.NotNil(t, ai.AsyncFunctionPrototype)
	for _, o := range []*Object{ai.AsyncFunction, ai.AsyncFunctionPrototype, ai.AsyncGeneratorFunction, ai.AsyncGeneratorFunctionPrototype,
		ai.AsyncGeneratorPrototype, ai.AsyncIteratorPrototype, ai.AsyncFromSyncIteratorPrototype, ai.asyncFromSyncNext} {
		assert.NotZero(t, o.flags&flagShared)
	}
	for _, other := range append(got[1:], early.asyncIntr(), late.asyncIntr()) {
		assert.True(t, *ai == *other, "every shared realm gets the same async intrinsics")
	}
	assert.Nil(t, sharedTpl.async.AsyncFunctionPrototype, "the build leaves the template's copy alone")
}

// TestAsyncInterrupt checks that an interrupt stops an async function
// resumed from a job, drops the queue and leaves the realm reusable.
func TestAsyncInterrupt(t *testing.T) {
	f := evalModule(t, `export let n = 0;
export async function spin() { await 0; n++; for (;;) {} }
export async function one() { await 0; return 1; }`)
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

// TestAsyncGeneratorInterrupt checks that an interrupt stops an async
// generator resumed from a job and completes it: its pending requests stay
// pending, and a later next finds it done.
func TestAsyncGeneratorInterrupt(t *testing.T) {
	f := evalModule(t, `export let n = 0, log = [];
async function* g() { yield 1; await 0; n++; for (;;) {} }
const it = g();
export function spin() { it.next(); it.next().then(r => log.push("settled")); it.next().then(r => log.push("queued")); }
export function after() { it.next().then(r => log.push(r.value, r.done)); }
export function done() { return log.join("|"); }`)
	time.AfterFunc(20*time.Millisecond, func() { f.r.Interrupt("stop") })
	_, err := f.callErr("spin")
	var ie *InterruptedError
	require.ErrorAs(t, err, &ie)
	assert.Equal(t, int64(1), f.export("n"))
	assert.Zero(t, queued(f.r))
	f.r.ClearInterrupt()
	_, err = f.callErr("after")
	require.NoError(t, err)
	assert.Equal(t, "|true", f.call("done"))
}
