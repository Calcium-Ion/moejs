package engine

import "testing"

// TestConditionalReturn covers `return c ? a : b`, which returns from each
// branch where a return is one instruction (compiler/stmt.go returnExpr)
// and joins the branches where it routes through a finally, closes an
// iterator or jumps to an epilogue.
func TestConditionalReturn(t *testing.T) {
	runScriptCases(t, []scriptCase{
		{"both branches", []string{`function f(n) { return n < 2 ? n : n * 10; } f(1) + ":" + f(5)`}, "1:50"},
		{"fib", []string{`function fib(n) { return n < 2 ? n : fib(n - 1) + fib(n - 2); } fib(20)`}, "6765"},
		{"nested", []string{`function f(n) { return n < 0 ? "neg" : n === 0 ? "zero" : n < 10 ? "small" : "big"; } [f(-1), f(0), f(5), f(50)].join()`}, "neg,zero,small,big"},
		{"one branch runs", []string{`var log = []; function a() { log.push("a"); return 1; } function b() { log.push("b"); return 2; }
			function f(c) { return c ? a() : b(); } f(1) + f(0) + ":" + log.join()`}, "3:a,b"},
		{"logical test", []string{`function f(x, y) { return x && !y ? "a" : x || y ? "b" : "c"; } [f(1, 0), f(1, 1), f(0, 0)].join()`}, "a,b,c"},
		{"folded test", []string{`function f(x) { return 1 < 2 ? x : x * 2; } f(3)`}, "3"},
		{"folded whole", []string{`function f() { return 1 < 2 ? "a" : "b"; } f()`}, "a"},
		{"arrow expression body", []string{`const f = n => n % 2 ? "odd" : "even"; f(3) + f(4)`}, "oddeven"},
		{"in try with finally", []string{`var log = []; function f(c) { try { return c ? "a" : "b"; } finally { log.push(c); } } f(1) + f(0) + ":" + log.join()`}, "ab:1,0"},
		{"finally overrides", []string{`function f(c) { try { return c ? "a" : "b"; } finally { return "f"; } } f(1)`}, "f"},
		{"in for-of", []string{`var closed = 0; const it = { [Symbol.iterator]() { let i = 0; return { next: () => ({ value: i++, done: false }), return() { closed++; return {}; } }; } };
			function f(c) { for (const v of it) return c ? v + 10 : v + 20; } f(1) + f(0) + ":" + closed`}, "30:2"},
		{"in a catch", []string{`function f(c) { try { throw 1; } catch (e) { return c ? e : -e; } } f(1) + ":" + f(0)`}, "1:-1"},
		{"generator", []string{`function* g(c) { yield 1; return c ? "a" : "b"; } var it = g(0); it.next(); JSON.stringify(it.next())`}, `{"value":"b","done":true}`},
		{"async function", []string{`var out; async function f(c) { return c ? await "a" : "b"; } f(1).then(v => out = v);`, `out`}, "a"},
		{"async arrow", []string{`var out; const f = async c => c ? "a" : "b"; f(0).then(v => out = v);`, `out`}, "b"},
		{"async generator", []string{`var out; async function* g(c) { return c ? Promise.resolve("a") : "b"; } g(1).next().then(r => out = r.value);`, `out`}, "a"},
		{"derived constructor", []string{`class A { constructor() { this.x = 1; } }
			class B extends A { constructor(c) { super(); return c ? {y: 2} : undefined; } }
			new B(1).y + ":" + new B(0).x`}, "2:1"},
		{"derived constructor returns a primitive", []string{`class A {} class B extends A { constructor(c) { super(); return c ? 1 : undefined; } }
			var r; try { new B(1); } catch (e) { r = e.name; } r`}, "TypeError"},
		{"sloppy this", []string{`function f(c) { return c ? typeof this : this; } f.call(1, 1) + ":" + (f.call(undefined, 0) === globalThis)`}, "object:true"},
		{"in finally", []string{`function f(c) { try { return 1; } finally { return c ? 2 : 3; } } f(1) + ":" + f(0)`}, "2:3"},
		{"in catch with a finally", []string{`var log = []; function f(c) { try { throw 1; } catch (e) { return c ? e : -e; } finally { log.push("f"); } } f(1) + ":" + f(0) + ":" + log.join()`}, "1:-1:f,f"},
		{"derived constructor before super", []string{`class A {} class B extends A { constructor(c) { return c ? undefined : {}; } }
			var r = []; try { new B(1); } catch (e) { r.push(e.name); } r.push(typeof new B(0)); r.join()`}, "ReferenceError,object"},
		{"yield in the test", []string{`function* g() { return (yield 1) ? "a" : "b"; } var it = g(); it.next(); var it2 = g(); it2.next(); it.next(true).value + ":" + it2.next(false).value`}, "a:b"},
		{"await in the test", []string{`var out = []; async function f(c) { return (await c) ? "a" : "b"; } f(1).then(v => out.push(v)); f(0).then(v => out.push(v));`, `out.join()`}, "a,b"},
		{"branch throws in try", []string{`function f(c) { try { return c ? g() : 2; } catch (e) { return "caught"; } function g() { throw 0; } } f(1) + ":" + f(0)`}, "caught:2"},
		{"in with", []string{`function f(c) { var o = {x: 1}; with (o) { return c ? x : -x; } } f(1) + ":" + f(0)`}, "1:-1"},
		{"in a labelled nested loop", []string{`function f(c) { outer: for (let i = 0; i < 3; i++) { for (let j = 0; j < 3; j++) { if (i === 1) return c ? i : j; } } return -1; } f(1) + ":" + f(0)`}, "1:0"},
		{"in a switch in for-of", []string{`var closed = 0; const it = { [Symbol.iterator]() { return { next: () => ({ value: 1, done: false }), return() { closed++; return {}; } }; } };
			function f(c) { for (const v of it) { switch (v) { case 1: return c ? "a" : "b"; } } } f(1) + f(0) + closed`}, "ab2"},
		{"in for-in", []string{`function f(c) { for (const k in {a: 1}) return c ? k : k + k; } f(1) + f(0)`}, "aaa"},
		{"value read before the return", []string{`function f(s) { return s > 0 ? (s = s * 2, s + 1) : s; } f(3) + ":" + f(-1)`}, "7:-1"},
	})
}
