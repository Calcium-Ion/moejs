package engine

import "testing"

// refKeyPrelude logs every conversion of the key object k (which names "p"
// the first time and "q" after) and every access of o.p and o.q.
const refKeyPrelude = `var log = [], n = 0;
var k = { toString() { log.push("key"); return n++ ? "q" : "p"; } };
var o = {
  get p() { log.push("get p"); return 1; }, set p(v) { log.push("set p " + v); },
  get q() { log.push("get q"); return 10; }, set q(v) { log.push("set q " + v); },
};
function v(x) { log.push("value"); return x; }
`

// TestComputedMemberReferenceOrder checks that a compound assignment or
// update of base[key] converts the key once, after the base's ToObject and
// before reading the property, and writes the property it read.
func TestComputedMemberReferenceOrder(t *testing.T) {
	var cases []scriptCase
	for _, mode := range []string{"", "'use strict';\n"} {
		name := func(s string) string {
			if mode != "" {
				return s + "/strict"
			}
			return s
		}
		for _, c := range []struct{ name, src, want string }{
			{"compound", `o[k] += v(2);`, "key,get p,value,set p 3"},
			{"exponent", `o[k] **= v(2);`, "key,get p,value,set p 1"},
			{"shift", `o[k] <<= v(2);`, "key,get p,value,set p 4"},
			{"or", `o[k] |= v(2);`, "key,get p,value,set p 3"},
			{"logical and", `o[k] &&= v(2);`, "key,get p,value,set p 2"},
			{"logical or", `o[k] ||= v(2);`, "key,get p"},
			{"nullish", `o[k] ??= v(2);`, "key,get p"},
			{"postfix", `log.push(o[k]++);`, "key,get p,set p 2,1"},
			{"prefix", `log.push(--o[k]);`, "key,get p,set p 0,0"},
			{"want", `log.push(o[k] -= v(1));`, "key,get p,value,set p 0,0"},
			{"local key", `(function (key) { o[key] *= 3; log.push(key === k); })(k);`, "key,get p,set p 3,true"},
			{"local key update", `(function (key) { key++; })(k); (function (key) { o[key]++; log.push(typeof key); })(k);`, "key,key,get q,set q 11,object"},
			{"null base", `try { var b = null; b[k] += v(1); } catch (e) { log.push(e.name); }`, "TypeError"},
			{"undefined base", `try { var b; b[k]++; } catch (e) { log.push(e.name); }`, "TypeError"},
			{"primitive base", `try { var b = 1; b[k] ^= v(1); } catch (e) { log.push(e.name); }`, map[bool]string{false: "key,value", true: "key,value,TypeError"}[mode != ""]},
			{"array", `var a = [1, 2]; var i = { toString() { log.push("i"); return "1"; } }; a[i] += 1; a[0]++; log.push(a.join("|"));`, "i,2|3"},
			{"number keys", `var a = [1, , 3]; for (var j = 0; j < 3; j++) { a[j] += 10; a[j]++; } var t = new Int8Array(2), m = 1; t[m] += 5; t[m]++; log.push(a.join("|"), t.join("|"));`, "12|NaN|14,0|6"},
			{"local number key", `(function (a, i) { a[i] += 10; a[i]++; log.push(a.join("|"), i); for (let j = 0; j < 4; j++) { a[j] += 1; a[j]++; } log.push(a.join("|")); })([1, 2, , 4], 1);`, "1|13||4,1,3|15|NaN|6"},
			{"number from toString", `var a = [1, 2], t = new Int8Array(2); var j = { toString() { log.push("j"); return 1; } }; a[j] += 5; t[j] -= 3; log.push(a.join("|"), t.join("|"));`, "j,j,1|7,0|-3"},
			{"symbol", `var s = Symbol(); var b = {}; b[s] = 1; b[{ [Symbol.toPrimitive]() { log.push("prim"); return s; } }] += 1; log.push(b[s]);`, "prim,2"},
		} {
			cases = append(cases, scriptCase{name(c.name), []string{mode + refKeyPrelude + c.src + "\nlog.join()"}, c.want})
		}
	}
	runScriptCases(t, cases)
}

// TestStrictGlobalReferenceOrder checks that a strict assignment to an
// undeclared name resolves the reference before evaluating the value: a
// global the value creates leaves it unresolvable, and one the value
// deletes is gone for SetMutableBinding.
func TestStrictGlobalReferenceOrder(t *testing.T) {
	runScriptCases(t, []scriptCase{
		{"created by the value", []string{`"use strict"; try { x1 = (this.x1 = 5); "no error" } catch (e) { e.name + " " + this.x1 }`}, "ReferenceError 5"},
		{"created by a call", []string{`"use strict"; function f() { globalThis.x2 = 1; return 2; } try { x2 = f(); "no error" } catch (e) { e.name + " " + x2 }`}, "ReferenceError 1"},
		{"function code", []string{`(function () { "use strict"; try { x3 = (globalThis.x3 = 1, 2); return "no error"; } catch (e) { return e.name + " " + x3; } })()`}, "ReferenceError 1"},
		{"sloppy", []string{`x4 = (this.x4 = 5, 6); x4`}, "6"},
		{"unresolvable", []string{`"use strict"; try { x5 = (() => 1)(); "no error" } catch (e) { e.name + " " + typeof x5 }`}, "ReferenceError undefined"},
		{"inert value", []string{`"use strict"; try { x6 = 5; "no error" } catch (e) { e.name }`}, "ReferenceError"},
		{"declared elsewhere", []string{`var y1 = 0;`, `"use strict"; y1 = (function () { return 7; })(); y1`}, "7"},
		{"deleted by the value", []string{`globalThis.y2 = 0;`, `"use strict"; try { y2 = (delete globalThis.y2, 1); "no error" } catch (e) { e.name + " " + ("y2" in globalThis) }`}, "ReferenceError false"},
		{"lexical", []string{`let y3 = 0;`, `"use strict"; y3 = (() => 3)(); y3`}, "3"},
		{"lexical before initialization", []string{`"use strict"; function g() { y4 = (() => 4)(); }`, `let y4 = g();`}, "ReferenceError: Cannot access 'y4' before initialization"},
		{"setter", []string{`var s; Object.defineProperty(globalThis, "y5", { set(v) { s = v; }, configurable: true });`, `"use strict"; y5 = (() => 5)(); s`}, "5"},
		{"read-only", []string{`Object.defineProperty(globalThis, "y6", { value: 0 });`, `"use strict"; try { y6 = (() => 6)(); } catch (e) { e.name + " " + y6 }`}, "TypeError 0"},
		{"loop", []string{`globalThis.y7 = 0;`, `"use strict"; for (var i = 0; i < 5; i++) y7 = i + y7; y7`}, "10"},
		{"value", []string{`globalThis.y8 = 0;`, `"use strict"; var w = (y8 = String(8)); w + y8`}, "88"},
	})
	runProtoCases(t, []protoCase{
		{"proxy", globalProxyTraps + `zzq = (() => 6)(); return take()`, "has,has,set:6"},
		{"proxy vanishing", globalProxyTraps + `try { zzr = (alive = () => false, 7) } catch (e) { return String(e) + '|' + take() }`, "ReferenceError: zzr is not defined|has,has"},
		{"proxy appearing", globalProxyTraps + `alive = () => false; try { zzr = (alive = () => true, 8) } catch (e) { return String(e) + '|' + take() }`, "ReferenceError: zzr is not defined|has"},
	})
}
