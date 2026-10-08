package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestLoopConstants runs loops whose operands are literals, which the
// compiler loads once before the outermost loop of a nest into registers
// it keeps for the whole nest (compiler/hoist.go): the loads must not show
// when a loop never runs, when the nest is left by a label, a finally or
// an exception, or when the frame suspends at a yield or an await.
func TestLoopConstants(t *testing.T) {
	runScriptCases(t, []scriptCase{
		{"for never runs", []string{`var s = 5; for (let i = 0; i < 0; i++) s = (s + i * 7) % 1000003; s`}, "5"},
		{"while never runs", []string{`var s = 5, i = 9; while (i < 3) { s = s * 1000003 + 0.5; i++; } s`}, "5"},
		{"do-while runs once", []string{`var s = 5; do { s = s * 7 + 1000003; } while (s < 0); s`}, "1000038"},
		{"for-of over nothing", []string{`var s = "x"; for (const v of []) s = s + "abc" + v * 300; s`}, "x"},
		{"for-in over nothing", []string{`var s = 1; for (const k in {}) s = s * 1000 + k.length; s`}, "1"},
		{"int loop", []string{`var s = 0; for (let i = 0; i < 1000; i++) s = (s + (i ^ (i >>> 3)) * 7) % 1000003; s`}, "513963"},
		{"shared across nested loops", []string{`var s = 0;
			for (let i = 0; i < 20; i++) {
				s = (s * 31 + i * 1000) % 1000003;
				for (let j = 0; j < 20; j++) {
					s = (s * 31 + j * 1000) % 1000003;
					let k = 0;
					while (k < 3) { s = (s + k * 1000 + 1000003) % 1000003; k++; }
				}
			}
			s`}, "223617"},
		{"inner loop constants of a branch", []string{`var out = [];
			for (let i = 0; i < 4; i++) {
				if (i % 2 === 0) { for (let j = 0; j < 2; j++) out.push(i * 100 + j * 1000 - 250); }
				else out.push("odd" + i);
			}
			out.join()`}, "-250,750,odd1,-50,950,odd3"},
		{"labelled break and continue", []string{`var s = 0;
			outer: for (let i = 0; i < 10; i++) {
				for (let j = 0; j < 10; j++) {
					if (j === 7) continue outer;
					if (i * 1000 + j === 5003) break outer;
					s = (s + i * 1000 + j * 77) % 65537;
				}
			}
			s`}, "27779"},
		{"labelled block in a loop", []string{`var s = 0;
			for (let i = 0; i < 5; i++) { b: { if (i === 3) break b; s += i * 300; } s -= 7; }
			s`}, "2065"},
		{"loop in try with finally", []string{`var log = [];
			function f() {
				try {
					for (let i = 0; i < 10; i++) {
						if (i * 300 === 1200) return "ret" + i * 300;
						log.push(i * 300 + 7);
					}
				} finally { log.push("fin" + 1000003); }
			}
			log.push(f()); log.join()`}, "7,307,607,907,fin1000003,ret1200"},
		{"try with finally in a loop", []string{`var log = [];
			for (let i = 0; i < 5; i++) {
				try {
					if (i === 1) continue;
					if (i === 3) break;
					log.push(i * 1000 + 1);
				} finally { log.push(i * 1000 + 2); }
			}
			log.join()`}, "1,2,1002,2001,2002,3002"},
		{"exception caught in the loop", []string{`var s = 0;
			for (let i = 0; i < 6; i++) {
				try { if (i % 3 === 0) throw new Error("bad" + i * 1000); s += i * 1000; }
				catch (e) { s += e.message.length * 100000; }
			}
			s`}, "1112000"},
		{"exception leaves the loop", []string{`var s = 0;
			try { for (let i = 0; i < 10; i++) { s += i * 1000; if (i === 4) null.x; } } catch (e) { s += 0.5; }
			for (let i = 0; i < 3; i++) s += i * 1000;
			s`}, "13000.5"},
		{"generator suspends in the loop", []string{`function* g(n) {
				for (let i = 0; i < n; i++) {
					const v = yield i * 1000 + 7;
					if (v === "stop") return "stopped" + i * 1000;
				}
				return "done";
			}
			var it = g(5), out = [];
			out.push(it.next().value, it.next().value, it.next("x").value, it.next("stop").value, it.next().done);
			out.join()`}, "7,1007,2007,stopped2000,true"},
		{"generator returns through finally", []string{`var log = [];
			function* g() {
				try { for (let i = 0; ; i++) yield i * 1000 + 3; }
				finally { for (let j = 0; j < 2; j++) log.push(j * 1000 + 5); }
			}
			var it = g(); it.next(); it.next(); it.return(); log.join()`}, "5,1005"},
		{"generator in a loop of a loop", []string{`function* g() {
				for (let i = 0; i < 2; i++) for (let j = 0; j < 2; j++) yield (i * 1000 + j * 77) % 1000003;
			}
			[...g()].join()`}, "0,77,1000,1077"},
		{"async function awaits in the loop", []string{`var out = [];
			async function f() {
				let s = 0;
				for (let i = 0; i < 4; i++) { s = (s * 1000 + await (i * 7)) % 1000003; out.push(s); }
				return s;
			}
			f().then(v => out.push("r" + v));`, `out.join()`}, "0,7,7014,14000,r14000"},
		{"for await in an async function", []string{`var out = [];
			async function* src() { yield 1; yield 2; }
			(async () => { for await (const v of src()) out.push(v * 1000 + 3); })();`, `out.join()`}, "1003,2003"},
		{"function nested in the loop", []string{`var fs = [];
			for (let i = 0; i < 3; i++) fs.push(x => { let t = 0; for (let j = 0; j < x; j++) t += j * 1000 + i; return t; });
			fs.map((f, i) => f(i + 1)).join()`}, "0,1002,3006"},
		{"negative zero and zero", []string{`var out = [];
			for (let i = 1; i < 3; i++) out.push(1 / (i * -0), 1 / (i * 0), i * -1.5, i - -300);
			out.join()`}, "-Infinity,Infinity,-1.5,301,-Infinity,Infinity,-3,302"},
		{"strings", []string{`var out = [];
			for (const w of ["a", "image_url", "text"]) out.push(w === "text" ? "T" : w === "image_url" ? "I" : w + "?", "k" in {k: 1});
			out.join()`}, "a?,true,I,true,T,true"},
		{"bigints", []string{`var f = 1n; for (let i = 1n; i <= 30n; i++) f = f * i % 1000000007n; f.toString()`}, "109361473"},
		{"computed keys", []string{`var a = [1, 1, 0, 0]; for (let i = 0; i < 10; i++) { a[2] = a[0] + a[1]; a[0] = a[1]; a[1] = a[2]; } a.join()`}, "89,144,144,0"},
		{"compound assignments", []string{`var s = 3, o = {p: 1}; for (let i = 0; i < 6; i++) { s *= 1000; s %= 65537; o.p += 250; } s + ":" + o.p`}, "1101:1501"},
		{"switch in the loop", []string{`var out = [];
			for (let i = 0; i < 5; i++) switch (i * 1000) { case 1000: out.push("a"); break; case 3000: out.push("b"); continue; default: out.push(i); }
			out.join()`}, "0,a,2,b,4"},
		{"with in the loop", []string{`var out = [], o = {x: 1};
			for (let i = 0; i < 3; i++) with (o) { x = x * 1000 + i; out.push(x % 7); }
			out.join() + ":" + o.x`}, "6,2,0:1000001002"},
		// Where a write of a hoisted register would land if one ever did.
		{"with on a hoisted literal", []string{`var out = []; for (let i = 0; i < 3; i++) { with (5) { out.push(toFixed(1)) } out.push(i * 5, i === 5) } out.join()`}, "5.0,0,false,5.0,5,false,5.0,10,false"},
		{"default equal to a hoisted literal", []string{`var s = 0; for (let i = 0; i < 3; i++) { let [a = 1000] = []; s = s + a * 1000 } s`}, "3000000"},
		{"case equal to a hoisted literal", []string{`var out = []; for (let i = 0; i < 4; i++) { switch (i * 1000) { case 1000: out.push("a"); break; case 2000: out.push("b"); break; default: out.push(i) } } out.join()`}, "0,a,b,3"},
		{"hoisted key updated and deleted", []string{`var a = [1, 2, 3], out = []; for (let i = 0; i < 2; i++) { a[1] += 1000; delete a[1]; a[1] = i * 1000; out.push(a[1]) } out.join()`}, "0,1000"},
		{"string and number keys", []string{`var o = {"5": "five"}, out = []; for (let i = 0; i < 2; i++) { out.push(o["5"], o[5], "5" + i) } out.join()`}, "five,five,50,five,five,51"},
		{"negative literals", []string{`var out = []; for (let i = 0; i < 2; i++) out.push(i * -3, i * 3, -3 === -3, i - -3); out.join()`}, "0,0,true,3,-3,3,true,4"},
		{"catch parameter rewritten", []string{`var s = 0; for (let i = 0; i < 3; i++) { try { throw i } catch (e) { e = e * 1000; s = s + e + 1000 } } s`}, "6000"},
		{"loop in finally in loop", []string{`var out = []; for (let i = 0; i < 2; i++) { try { out.push(i * 1000) } finally { for (let j = 0; j < 2; j++) out.push(j * 1000 + 1) } } out.join()`}, "0,1,1001,1000,1,1001"},
		{"loop condition throws", []string{`var n = 0, o = {get v() { if (++n === 3) throw new RangeError("x" + n * 1000); return 2; }};
			var r; try { for (let i = 0; i * 1000 < 100000; i += o.v) ; } catch (e) { r = e.message; } r`}, "x3000"},
	})
}

// TestLoopConstantsStrict runs some of TestLoopConstants's loops in strict
// code and in a module-like function body, where the registers differ.
func TestLoopConstantsStrict(t *testing.T) {
	got := runScripts(t, `"use strict";
		function f(n) {
			const out = [];
			for (let i = 0; i < n; i++) {
				let s = 0;
				for (let j = 0; j <= i; j++) s = (s * 1000 + j * 7 + 1000003) % 1000003;
				out.push(s);
			}
			return out.join();
		}
		f(0) + "|" + f(4)`)
	assert.Equal(t, "|0,7,7014,14000", got)
}
