package engine

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// generatorCases are generator programs: each body runs in its own function;
// want is the JSON of its result or "throws " and the error.
var generatorCases = []struct{ name, body, want string }{
	{"basic", `function* g() { yield 1; yield 2; return 3; } const it = g(); return [it.next(), it.next(), it.next(), it.next()];`,
		`[{"value":1,"done":false},{"value":2,"done":false},{"value":3,"done":true},{"done":true}]`},
	{"lazy", `const log = []; function* g() { log.push("start"); yield 1; log.push("end"); } const it = g(); log.push("created"); it.next(); log.push("mid"); it.next(); return log;`,
		`["created","start","mid","end"]`},
	{"sent values", `function* g(a) { const b = yield a; const c = yield a + b; return [a, b, c]; } const it = g(1); return [it.next("x").value, it.next(2).value, it.next(3).value];`, `[1,3,[1,2,3]]`},
	{"params evaluated at call", `const log = []; function* g(a = log.push("param")) { log.push("body"); } g(); return log;`, `["param"]`},
	{"param error at call", `function* g(a = (() => { throw new Error("p"); })()) {} try { g(); } catch (e) { return e.message; }`, `"p"`},
	{"loops and locals", `function* range(n) { for (let i = 0; i < n; i++) yield i * i; } return [...range(5)];`, `[0,1,4,9,16]`},
	{"closures over locals", `function* g() { let i = 0; const fs = []; while (i < 3) { fs.push(() => i); yield fs; i++; } } let last; for (const fs of g()) last = fs; return last.map(f => f());`, `[3,3,3]`},
	{"block env across yield", `function* g() { for (let i = 0; i < 3; i++) { const f = () => i; yield f; } } return [...g()].map(f => f());`, `[0,1,2]`},
	{"this and arguments", `function* g() { yield this.v; yield arguments.length; } const o = { v: 7, g }; return [...o.g(1, 2)];`, `[7,2]`},
	{"method and class", `const o = { *m() { yield "o"; } }; class A { *m() { yield "a"; } static *s() { yield "s"; } *[Symbol.iterator]() { yield 1; yield 2; } }
		return [[...o.m()], [...new A().m()], [...A.s()], [...new A()]];`, `[["o"],["a"],["s"],[1,2]]`},
	{"super in generator method", `class A { m() { return "A"; } } class B extends A { *m() { yield super.m(); } } return [...new B().m()];`, `["A"]`},
	{"computed method names", `const s = Symbol("t"), a = Symbol(); class A { *[s]() {} static *[a]() {} } const o = { *[s]() {} }; return [A.prototype[s].name, A[a].name, o[s].name];`, `["[t]","","[t]"]`},
	{"expression and name", `const g = function* () { yield 1; }; const h = function* named() { yield named === h; }; return [g.name, [...g()], [...h()]];`, `["g",[1],[true]]`},
	{"return method", `const log = []; function* g() { try { yield 1; yield 2; } finally { log.push("fin"); } } const it = g(); it.next();
		const r = it.return(9); return [r, it.next(), log];`, `[{"value":9,"done":true},{"done":true},["fin"]]`},
	{"return overridden in finally", `function* g() { try { yield 1; } finally { return 5; } } const it = g(); it.next(); return it.return(9);`, `{"value":5,"done":true}`},
	{"yield in finally", `function* g() { try { yield 1; } finally { yield "f"; } } const it = g(); it.next(); return [it.return(9), it.next(), it.next()];`,
		`[{"value":"f","done":false},{"value":9,"done":true},{"done":true}]`},
	{"throw method caught", `function* g() { while (true) { try { yield 1; } catch (e) { yield "caught " + e; } } } const it = g(); it.next(); return [it.throw("x"), it.next()];`,
		`[{"value":"caught x","done":false},{"value":1,"done":false}]`},
	{"throw method uncaught", `function* g() { yield 1; } const it = g(); it.next(); try { it.throw(new Error("boom")); } catch (e) { return [e.message, it.next()]; }`,
		`["boom",{"done":true}]`},
	{"throw before start", `const log = []; function* g() { log.push("body"); } const it = g(); try { it.throw("t"); } catch (e) { return [e, log, it.next()]; }`, `["t",[],{"done":true}]`},
	{"return before start", `function* g() { yield 1; } const it = g(); return [it.return(4), it.next()];`, `[{"value":4,"done":true},{"done":true}]`},
	{"already running", `let it; function* g() { it.next(); } it = g(); try { it.next(); } catch (e) { return [e.name, e.message, it.next()]; }`,
		`["TypeError","Generator is already running",{"done":true}]`},
	{"throw in body completes", `function* g() { yield 1; throw new Error("e"); } const it = g(); it.next(); try { it.next(); } catch (e) { return [e.message, it.next()]; }`, `["e",{"done":true}]`},
	{"yield star", `function* inner() { const x = yield 1; yield x; return "r"; } function* outer() { const v = yield* inner(); yield v; }
		const it = outer(); return [it.next().value, it.next("sent").value, it.next().value, it.next()];`, `[1,"sent","r",{"done":true}]`},
	{"yield star iterables", `function* g() { yield* [1, 2]; yield* "ab"; yield* new Set([3]); } return [...g()];`, `[1,2,"a","b",3]`},
	{"yield star return", `const log = []; function* inner() { try { yield 1; } finally { log.push("inner fin"); } } function* outer() { try { yield* inner(); } finally { log.push("outer fin"); } }
		const it = outer(); it.next(); return [it.return(7), log];`, `[{"value":7,"done":true},["inner fin","outer fin"]]`},
	{"yield star throw", `function* inner() { try { yield 1; } catch (e) { yield "inner caught " + e; } } function* outer() { yield* inner(); }
		const it = outer(); it.next(); return it.throw("x");`, `{"value":"inner caught x","done":false}`},
	{"yield star throw without method", `const log = []; const iter = { [Symbol.iterator]() { return this; }, next() { return { value: 1, done: false }; }, return() { log.push("closed"); return {}; } };
		function* g() { yield* iter; } const it = g(); it.next(); try { it.throw("x"); } catch (e) { return [e.name, log]; }`, `["TypeError",["closed"]]`},
	{"yield star result as is", `const res = { value: 1, done: false, extra: true }; const iter = { [Symbol.iterator]() { return this; }, next() { return res; } };
		function* g() { yield* iter; } return g().next() === res;`, `true`},
	{"yield star not iterable", `function* g() { yield* 5; } try { g().next(); } catch (e) { return e.name; }`, `"TypeError"`},
	{"for-of break closes", `const log = []; function* g() { try { yield 1; yield 2; } finally { log.push("fin"); } } for (const x of g()) { log.push(x); break; } return log;`, `[1,"fin"]`},
	{"destructuring closes", `const log = []; function* g() { try { yield 1; yield 2; yield 3; } finally { log.push("fin"); } } const [a, b] = g(); return [a, b, log];`, `[1,2,["fin"]]`},
	{"return closes destructuring", `const log = []; const iter = { [Symbol.iterator]() { return this; }, next() { log.push("next"); return { done: false }; }, return() { log.push("closed"); return {}; } };
		function* g() { let x, y; try { [x, y = yield] = iter; } finally { log.push("fin"); } } const it = g(); it.next(); return [it.return(7), log];`,
		`[{"value":7,"done":true},["next","next","closed","fin"]]`},
	{"return closes destructuring throws", `const iter = { [Symbol.iterator]() { return this; }, next() { return { done: false }; }, return() { throw new Error("r"); } };
		function* g() { [{}[yield]] = iter; } const it = g(); it.next(); try { it.return(7); } catch (e) { return [e.message, it.next()]; }`, `["r",{"done":true}]`},
	{"return closes nested for-of and destructuring", `const log = []; const mk = n => ({ [Symbol.iterator]() { return this; }, next() { return { done: false }; }, return() { log.push(n); return {}; } });
		function* g() { for (const a of mk("loop")) { const [b = yield] = mk("pat"); } } const it = g(); it.next(); it.return(); return log;`, `["pat","loop"]`},
	{"spread and Array.from", `function* g() { yield 1; yield 2; } return [[...g()], Array.from(g()), Array.from(g(), x => x * 10), Math.max(...g())];`, `[[1,2],[1,2],[10,20],2]`},
	{"map and set from generator", `function* g() { yield [1, "a"]; yield [2, "b"]; } return [[...new Map(g()).values()], new Set(g()).size];`, `[["a","b"],2]`},
	{"yield precedence", `function* g() { const a = yield 1, b = yield; yield [a, b]; } const it = g(); it.next(); it.next("A"); return it.next("B").value;`, `["A","B"]`},
	{"yield in expressions", `function* g() { return (yield 1) + (yield 2) * 2; } const it = g(); it.next(); it.next(3); return it.next(4);`, `{"value":11,"done":true}`},
	{"yield in call args", `function f(...a) { return a; } function* g() { return f(1, yield "a", 3, ...(yield "b")); } const it = g(); it.next(); it.next(2); return it.next([4, 5]).value;`, `[1,2,3,4,5]`},
	{"yield in array and object", `function* g() { return [yield, { k: yield }]; } const it = g(); it.next(); it.next(1); return it.next(2).value;`, `[1,{"k":2}]`},
	{"prototype objects", `function* g() {} const GFP = Object.getPrototypeOf(g), GP = Object.getPrototypeOf(g.prototype);
		return [typeof g.prototype, Object.getPrototypeOf(g()) === g.prototype, GP === GFP.prototype, GFP.constructor.name, GFP[Symbol.toStringTag], GP[Symbol.toStringTag],
			String(g()), Object.getPrototypeOf(GP) === Object.getPrototypeOf([][Symbol.iterator]()).__proto__, g.hasOwnProperty("caller"), GFP.constructor.length];`,
		`["object",true,true,"GeneratorFunction","GeneratorFunction","Generator","[object Generator]",true,false,1]`},
	{"prototype descriptor", `function* g() {} const d = Object.getOwnPropertyDescriptor(g, "prototype"); return [d.writable, d.enumerable, d.configurable, Object.getOwnPropertyNames(g), Object.getOwnPropertyNames(g.prototype)];`,
		`[true,false,false,["length","name","prototype"],[]]`},
	{"own prototype replaced", `function* g() { yield 1; } g.prototype = null; const it = g(); return [Object.getPrototypeOf(it) === Object.getPrototypeOf(function* () {}).prototype, it.next().value];`, `[true,1]`},
	{"not a constructor", `function* g() {} try { new g(); } catch (e) { return e.name; }`, `"TypeError"`},
	{"method not a constructor", `const o = { *m() {} }; try { new o.m(); } catch (e) { return e.name; }`, `"TypeError"`},
	{"incompatible receiver", `function* g() {} const next = g().next; try { next.call({}); } catch (e) { return e.message; }`, `"Generator.prototype.next called on incompatible receiver [object Object]"`},
	{"GeneratorFunction constructor", `const GF = Object.getPrototypeOf(function* () {}).constructor; try { GF("yield 1"); } catch (e) { return e.message; }`, `"new Function is not supported yet (see TODO.md)"`},
	{"generator methods length", `function* g() {} const P = Object.getPrototypeOf(g()).__proto__; return [P.next.length, P.return.length, P.throw.length, Object.keys(P)];`, `[1,1,1,[]]`},
	{"infinite with take", `function* nat() { let n = 0; while (true) yield n++; } function* take(it, k) { for (const x of it) { if (k-- <= 0) return; yield x; } } return [...take(nat(), 4)];`, `[0,1,2,3]`},
	{"recursive delegation", `function* walk(t) { if (!t) return; yield* walk(t.l); yield t.v; yield* walk(t.r); } const t = { v: 2, l: { v: 1 }, r: { v: 3, r: { v: 4 } } }; return [...walk(t)];`, `[1,2,3,4]`},
	{"iterator is itself", `function* g() {} const it = g(); return it[Symbol.iterator]() === it;`, `true`},
}

// TestGenerators runs generatorCases in a mutable realm, which builds the
// generator intrinsics on first use, and in one sharing the template's.
func TestGenerators(t *testing.T) {
	var src strings.Builder
	src.WriteString("export const out = {};\n")
	for _, c := range generatorCases {
		src.WriteString("try { out[" + strconv.Quote(c.name) + "] = JSON.stringify((() => {\n" + c.body + "\n})()); } catch (e) { out[" + strconv.Quote(c.name) + "] = \"throws \" + String(e); }\n")
	}
	for _, shared := range []bool{false, true} {
		f := evalModuleWith(t, src.String(), RealmOptions{SharedIntrinsics: shared})
		out := f.export("out").(map[string]any)
		for _, c := range generatorCases {
			assert.Equal(t, c.want, out[c.name], "%s (shared %v)", c.name, shared)
		}
	}
}

// TestGeneratorIntrinsicsPerRealm checks that the shared template freezes
// the generator intrinsics and that a mutable realm builds its own.
func TestGeneratorIntrinsicsPerRealm(t *testing.T) {
	const src = `const GFP = Object.getPrototypeOf(function* () {});
export const frozen = [Object.isFrozen(GFP), Object.isFrozen(GFP.prototype), Object.isFrozen(GFP.constructor)].join();
export function patch() { GFP.prototype.tag = "patched"; return (function* () {})().tag; }`
	s1 := evalModuleWith(t, src, RealmOptions{SharedIntrinsics: true})
	s2 := evalModuleWith(t, src, RealmOptions{SharedIntrinsics: true})
	assert.Equal(t, "true,true,true", s1.export("frozen"))
	assert.Same(t, s1.r.generatorIntrinsics(), s2.r.generatorIntrinsics())
	_, err := s1.callErr("patch")
	assert.ErrorContains(t, err, "TypeError")

	m1, m2 := evalModule(t, src), evalModule(t, src)
	assert.Equal(t, "false,false,false", m1.export("frozen"))
	assert.NotSame(t, m1.r.generatorIntrinsics(), m2.r.generatorIntrinsics())
	got, err := m1.callErr("patch")
	require.NoError(t, err)
	assert.Equal(t, "patched", got)
	assert.Nil(t, NewRealm().async, "a mutable realm builds them on first use")
}

// TestGeneratorLimits checks that deep delegation hits the call depth limit
// and that a generator's loop and the loop resuming it honour an interrupt.
func TestGeneratorLimits(t *testing.T) {
	f := evalModule(t, `function* deep(n) { if (n) yield* deep(n - 1); else yield n; }
export function recurse() { return [...deep(1e6)].length; }
export function spin() { function* g() { for (;;) {} } g().next(); }
export function drain() { function* g() { for (;;) yield 1; } for (const x of g()) {} }`)
	_, err := f.callErr("recurse")
	assert.ErrorContains(t, err, "Maximum call stack size exceeded")
	for _, name := range []string{"spin", "drain"} {
		go func() { time.Sleep(20 * time.Millisecond); f.r.Interrupt("stop") }()
		_, err := f.callErr(name)
		var ie *InterruptedError
		require.ErrorAs(t, err, &ie, name)
		f.r.ClearInterrupt()
	}
}
