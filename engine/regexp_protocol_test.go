package engine

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// callExec wraps RegExp.prototype.exec so a regexp takes the generic
// (observable) protocol path while matching like a pristine one.
const callExec = `re.exec = function (s) { return RegExp.prototype.exec.call(this, s); }; `

// subRegExp is R, a function "subclass" of RegExp whose instances are
// RegExps with R.prototype and which logs the flags it is constructed with.
const subRegExp = `function R(p, f) { const o = new RegExp(p, f); Object.setPrototypeOf(o, R.prototype); R.calls.push(f); return o; } R.calls = []; R.prototype = Object.create(RegExp.prototype); R.prototype.constructor = R; R[Symbol.species] = R; `

func TestRegExpSymbolMethods(t *testing.T) {
	runProtoCases(t, []protoCase{
		{"installed", `const P = RegExp.prototype; return [typeof P[Symbol.match], P[Symbol.replace].length, P[Symbol.split].name, P[Symbol.matchAll].name, P[Symbol.search].length, Object.getOwnPropertyDescriptor(P, Symbol.split).enumerable].join()`, "function,2,[Symbol.split],[Symbol.matchAll],1,false"},
		{"non-object receiver", `return RegExp.prototype[Symbol.match].call(1, "a")`, "!TypeError"},
		{"own exec replace", `const re = /a/g; let n = 0; re.exec = function (s) { n++; return RegExp.prototype.exec.call(this, s); }; return ["aXa".replace(re, "b"), n].join()`, "bXb,3"},
		{"own exec match", `const re = /b/; re.exec = () => ({0: "zz", length: 1, index: 7}); const m = "abc".match(re); return [m[0], m.index].join()`, "zz,7"},
		{"exec returns primitive", `const re = /a/; re.exec = () => 1; return re.test("a")`, "!TypeError"},
		{"non-callable exec uses builtin", `const re = /a/; re.exec = 1; return re.test("a")`, "true"},
		{"test on plain object", `return [RegExp.prototype.test.call({exec: () => ({})}, "x"), RegExp.prototype.test.call({exec: () => null}, "x")].join()`, "true,false"},
		{"plain object without exec", `return RegExp.prototype.test.call({exec: 1}, "x")`, "!TypeError"},
		{"replace on plain object", `return RegExp.prototype[Symbol.replace].call({exec() { return null; }, flags: ""}, "abc", "x")`, "abc"},
		{"RegExp.prototype-derived non-regexp", `const o = Object.create(RegExp.prototype); Object.defineProperty(o, "lastIndex", {value: 0, writable: true}); return RegExp.prototype[Symbol.replace].call(o, "abc", "x")`, "!TypeError"},
		{"own flags", `const re = /a/y; Object.defineProperty(re, "flags", {value: "g"}); return ["aaa".replace(re, "b"), "aaa".match(re).length].join()`, "bbb,3"},
		{"own global", `const re = /a/y; Object.defineProperty(re, "global", {value: true}); return "aaa".replace(re, "b")`, "bbb"},
		{"flags read once", `const re = /a/g; let n = 0; Object.defineProperty(re, "flags", {get() { n++; return "g"; }}); "aaa".replace(re, "b"); "aaa".match(re); return n`, "2"},

		// match
		{"match global generic", `const re = /a/g; ` + callExec + `return ["aba".match(re).join(), "xyz".match(re)].join("|")`, "a,a|"},
		{"match resets lastIndex", `const re = /a/g; re.lastIndex = 2; const n = "aaa".match(re).length; return [n, re.lastIndex].join()`, "3,0"},
		{"match coerces empty result", `const re = /a/g; let k = 0; re.exec = function () { return k++ < 2 ? {0: {toString() { return ""; }}} : null; }; return ["xx".match(re).length, re.lastIndex].join()`, "2,2"},
		{"match string arg", `return ["a.c".match(".")[0], "abc".match()[0] === ""].join()`, "a,true"},

		// replace
		{"replace clamps position", `const re = /x/; re.exec = function () { if (this.done) return null; this.done = true; return {0: "Q", length: 1, index: 100}; }; return "abc".replace(re, "[$&]")`, "abc[Q]"},
		{"replace substitutions", `const re = /x/; re.exec = function () { if (this.n) return null; this.n = 1; return {0: "b", 1: "B", length: 2, index: 1, groups: {k: "K"}}; }; return "abc".replace(re, "<$1$<k>$'>")`, "a<BKc>c"},
		{"replace function args", `const re = /x/; re.exec = function () { if (this.n) return null; this.n = 1; return {0: "b", 1: undefined, length: 2, index: 1, groups: {k: "K"}}; }; return "abc".replace(re, (...a) => JSON.stringify(a))`, `a["b",null,1,"abc",{"k":"K"}]c`},
		{"replace overlapping results", `const re = /x/g; const rs = [{0: "bc", index: 1}, {0: "b", index: 1}, {0: "a", index: 0}, null]; let i = 0; re.exec = () => rs[i++]; return "abcd".replace(re, "[$&]")`, "a[bc]d"},
		{"replace empty match advance", `const re = /(?:)/g; ` + callExec + `return "ab".replace(re, "-")`, "-a-b-"},
		{"replace unicode advance", `const a = /(?:)/gu, b = /(?:)/g; a.exec = b.exec = function (s) { return RegExp.prototype.exec.call(this, s); }; return ["\u{1F600}".replace(a, "-").length, "\u{1F600}".replace(b, "-").length].join()`, "4,5"},
		{"replace value coerced before exec", `const log = []; const re = /a/; re.exec = function (s) { log.push("exec"); return null; }; "a".replace(re, {toString() { log.push("str"); return "x"; }}); return log.join()`, "str,exec"},
		{"replaceAll non-global", `return "aa".replaceAll(/a/, "b")`, "!TypeError: replaceAll must be called with a global RegExp"},
		{"replaceAll global", `return "aa".replaceAll(/a/g, "b")`, "bb"},
		{"replaceAll IsRegExp object", `const o = {[Symbol.match]: true, flags: "g", [Symbol.replace]() { return "x"; }}; return "aa".replaceAll(o, "b")`, "x"},
		{"replaceAll IsRegExp non-global", `return "aa".replaceAll({[Symbol.match]: true, flags: "", [Symbol.replace]() { return "x"; }}, "b")`, "!TypeError"},
		{"replaceAll null flags", `return "aa".replaceAll({[Symbol.match]: true, flags: null}, "b")`, "!TypeError"},

		// search
		{"search", `return ["abc".search(/c/), "abc".search("b"), "abc".search(), "a.c".search("."), "abc".search(/x/)].join()`, "2,1,0,0,-1"},
		{"search restores lastIndex", `const re = /b/g; re.lastIndex = 2; const i = "abc".search(re); return [i, re.lastIndex].join()`, "1,2"},
		{"search sticky", `const re = /b/y; return ["abc".search(re), "bc".search(re)].join()`, "-1,0"},
		{"search custom exec", `const re = /b/; const seen = []; re.exec = function () { seen.push(this.lastIndex); this.lastIndex = 5; return {index: 9}; }; re.lastIndex = 3; return ["abc".search(re), re.lastIndex, seen.join()].join("|")`, "9|3|0"},

		// split
		{"split", `return [JSON.stringify("a1b2c".split(/\d/)), JSON.stringify("a1b2c".split(/(\d)/, 3)), "a-b".split(/-/, 0).length, "a-b-c".split(/-/, -1).length].join("|")`, `["a","b","c"]|["a","1","b"]|0|3`},
		{"split generic", `const re = /(-)/; ` + callExec + `return [JSON.stringify("a-b-".split(re)), JSON.stringify("ab".split(/(?:)/))].join("|")`, `["a","-","b","-",""]|["a","b"]`},
		{"split species", subRegExp + `const re = new R("-", ""); R.calls.length = 0; const parts = "a-b-c".split(re); return [parts.join(), R.calls.join()].join("|")`, "a,b,c|y"},
		{"split species not a constructor", `const re = /-/; re.constructor = {[Symbol.species]: () => 0}; return "a-b".split(re)`, "!TypeError"},
		{"split species returns non-regexp", `const re = /-/; re.constructor = {[Symbol.species]: function () { return {exec() { return null; }, lastIndex: 0}; }}; return JSON.stringify("a-b".split(re))`, `["a-b"]`},
		{"split limit order", `const log = []; const re = /-/; re.constructor = {[Symbol.species]: function (p, f) { log.push("new " + f); return new RegExp(p, f); }}; const lim = {valueOf() { log.push("lim"); return 2; }}; const r = "a-b-c".split(re, lim); return [r.join(), log.join()].join("|")`, "a,b|new y,lim"},
		{"split limit object pristine", `const lim = {valueOf() { return 2; }}; return "a-b-c".split(/-/, lim).join()`, "a,b"},
		{"split this coerced after dispatch", `const log = []; const sep = {[Symbol.split](s, l) { log.push(typeof s); return []; }}; String.prototype.split.call(1, sep); return log.join()`, "number"},
		{"split null this", `return String.prototype.split.call(null, {})`, "!TypeError"},

		// dispatch on objects only
		{"object dispatch", `return ["abc".replace({[Symbol.replace](s, r) { return s + r; }}, "!"), "abc".split({[Symbol.split](s, l) { return [s, l]; }}, 3).join(), "x".match({[Symbol.match]: s => "m:" + s}), "x".search({[Symbol.search]: s => 42}), "x".matchAll({[Symbol.matchAll]: s => "ma:" + s})].join("|")`, "abc!|abc,3|m:x|42|ma:x"},
		{"null method falls through", `return "a,b".split({[Symbol.split]: null, toString() { return ","; }}).length`, "2"},
		{"non-callable method throws", `return "a,b".split({[Symbol.split]: 1})`, "!TypeError"},
		{"IsRegExp", `const re = /a/; re[Symbol.match] = false; return ["/a/".startsWith(re), "a".includes({[Symbol.match]: false, toString() { return "a"; }})].join()`, "true,true"},
		{"IsRegExp throws for includes", `return "a".includes(/a/)`, "!TypeError"},

		// matchAll
		{"matchAll", `return [..."a1b22".matchAll(/\d+/g)].map(m => m[0] + "@" + m.index).join()`, "1@1,22@3"},
		{"matchAll iterator", `const it = "a".matchAll(/a/g); const P = Object.getPrototypeOf(it); return [P[Symbol.toStringTag], Object.getPrototypeOf(P) === Object.getPrototypeOf(Object.getPrototypeOf([][Symbol.iterator]())), typeof P.next, P.next.length, it[Symbol.iterator]() === it, Object.prototype.toString.call(it)].join()`, "RegExp String Iterator,true,function,0,true,[object RegExp String Iterator]"},
		{"matchAll non-global", `return "a".matchAll(/a/)`, "!TypeError: matchAll must be called with a global RegExp"},
		{"matchAll string arg", `return [..."a.a".matchAll(".")].length`, "3"},
		{"matchAll keeps lastIndex", `const re = /a/g; re.lastIndex = 1; const n = [..."aaa".matchAll(re)].length; return [n, re.lastIndex].join()`, "2,1"},
		{"matchAll bad receiver", `const it = "a".matchAll(/a/g); return it.next.call({})`, "!TypeError"},
		{"matchAll flags getter", `const re = /a/; Object.defineProperty(re, "flags", {value: "g"}); return [..."aa".matchAll(re)].length`, "2"},
		{"matchAll non-global iterator", `const it = /a/[Symbol.matchAll]("aa"); const a = it.next(), b = it.next(); return [a.value[0], a.done, b.done, b.value].join()`, "a,false,true,"},
		{"matchAll empty matches", `return [..."ab".matchAll(/(?:)/g)].map(m => m.index).join()`, "0,1,2"},
		{"matchAll species", subRegExp + `const re = new R("a", "g"); R.calls.length = 0; const n = [..."aXa".matchAll(re)].length; return [n, R.calls.join()].join("|")`, "2|g"},
		{"matchAll re-entrant next", `let it; const m = /a/g; m.exec = function () { return it.next(); }; const re = /a/g; re.constructor = {[Symbol.species]: function () { return m; }}; it = re[Symbol.matchAll]("a"); return it.next()`, "!TypeError: RegExp String Iterator is already running"},
		{"matchAll error completes", `let n = 0; const m = /a/g; m.exec = function () { if (n++ === 0) throw new RangeError("x"); return null; }; const re = /a/g; re.constructor = {[Symbol.species]: function () { return m; }}; const it = re[Symbol.matchAll]("a"); let caught = ""; try { it.next(); } catch (e) { caught = e.name; } return [caught, it.next().done, n].join()`, "RangeError,true,1"},

		// RegExp constructor
		{"RegExp IsRegExp object", `const o = {[Symbol.match]: true, source: "b+", flags: "g", constructor: RegExp}; const a = RegExp(o), b = new RegExp(o); return [a === o, b === o, b.source, b.flags, b.test("abb")].join()`, "true,false,b+,g,true"},
		{"RegExp constructor order", `const log = []; const o = {get [Symbol.match]() { log.push("match"); return true; }, get constructor() { log.push("ctor"); return RegExp; }, get source() { log.push("source"); return "a"; }, get flags() { log.push("flags"); return ""; }}; RegExp(o, "g"); log.push("|"); new RegExp(o); log.push("|"); RegExp(o); return log.join()`, "match,source,|,match,source,flags,|,match,ctor"},
		{"RegExp from regexp", `const re = /a/gi; const c = RegExp(re); const d = RegExp(re, "m"); return [c === re, d === re, d.flags, d.source].join()`, "true,false,m,a"},
	})

	runMutableProtoCases(t, []protoCase{
		{"replaced exec", `const orig = RegExp.prototype.exec; let n = 0; RegExp.prototype.exec = function (s) { n++; return orig.call(this, s); }; const r = ["abc".replace(/b/, "x"), /b/.test("abc"), "abc".search(/c/), "a-b".split(/-/).length, "aa".match(/a/g).length, [..."aa".matchAll(/a/g)].length].join(); RegExp.prototype.exec = orig; const before = n; "abc".replace(/b/, "x"); /b/.test("b"); return [r, before, n].join("|")`, "axc,true,2,2,2,2|12|12"},
		{"replaced flags getter", `const d = Object.getOwnPropertyDescriptor(RegExp.prototype, "flags"); Object.defineProperty(RegExp.prototype, "flags", {get() { return "g"; }, configurable: true}); const r = "aaa".replace(/a/y, "b"); Object.defineProperty(RegExp.prototype, "flags", d); return [r, "aaa".replace(/a/y, "b")].join()`, "bbb,baa"},
		{"replaced global getter", `const d = Object.getOwnPropertyDescriptor(RegExp.prototype, "global"); Object.defineProperty(RegExp.prototype, "global", {get() { return true; }, configurable: true}); const r = ["aaa".replace(/a/y, "b"), "aaa".match(/a/y).length]; Object.defineProperty(RegExp.prototype, "global", d); return [r.join(), "aaa".replace(/a/y, "b")].join()`, "bbb,3,baa"},
		{"replaced Symbol.replace", `const orig = RegExp.prototype[Symbol.replace]; RegExp.prototype[Symbol.replace] = function (s) { return "R:" + s; }; const a = "abc".replace(/b/, "x"); RegExp.prototype[Symbol.replace] = orig; return [a, "abc".replace(/b/, "x")].join()`, "R:abc,axc"},
		{"replaced symbol methods", `const saved = {}; for (const k of ["match", "matchAll", "search", "split"]) { saved[k] = RegExp.prototype[Symbol[k]]; RegExp.prototype[Symbol[k]] = function () { return k; }; } const r = ["a".match(/a/), "a".matchAll(/a/g), "a".search(/a/), "a".split(/a/)].join(); for (const k in saved) RegExp.prototype[Symbol[k]] = saved[k]; return [r, "a".search(/a/), "a-b".split(/-/).length].join("|")`, "match,matchAll,search,split|0|2"},
		{"deleted Symbol.split", `const orig = RegExp.prototype[Symbol.split]; delete RegExp.prototype[Symbol.split]; const r = "a/a/b".split(/a/); RegExp.prototype[Symbol.split] = orig; return [r.join("|"), "a/a/b".split(/a/).join("|")].join(",")`, "a|b,|/|/b"},
		{"replaced RegExp species", `const d = Object.getOwnPropertyDescriptor(RegExp, Symbol.species); let n = 0; Object.defineProperty(RegExp, Symbol.species, {get() { n++; return RegExp; }, configurable: true}); const r = ["a-b".split(/-/).length, [..."aa".matchAll(/a/g)].length]; Object.defineProperty(RegExp, Symbol.species, d); "a-b".split(/-/); return [r.join(), n].join("|")`, "2,2|2"},
		{"replaced RegExp.prototype.constructor", `function C(p, f) { return new RegExp(p, f + "i"); } RegExp.prototype.constructor = {[Symbol.species]: C}; const r = "aA".split(/a/).length; RegExp.prototype.constructor = RegExp; return [r, "aA".split(/a/).length].join()`, "3,2"},
		{"primitive arguments do not dispatch", `String.prototype[Symbol.split] = () => "hijacked"; Number.prototype[Symbol.replace] = () => "h"; const r = ["a,b".split(",").join("|"), "a1".replace(1, "x")]; delete String.prototype[Symbol.split]; delete Number.prototype[Symbol.replace]; return r.join()`, "a|b,ax"},
		{"prototype edited and restored", `RegExp.prototype.foo = 1; delete RegExp.prototype.foo; return ["abc".replace(/b/, "x"), "a-b".split(/-/).join(), /a/.test("a"), "a".search(/a/), [..."aa".matchAll(/a/g)].length].join()`, "axc,a,b,true,0,2"},
		{"exec on prototype chain", `const P = Object.create(RegExp.prototype); P.exec = function () { return null; }; const re = /a/; Object.setPrototypeOf(re, P); return ["a".replace(re, "x"), re.test("a")].join()`, "a,false"},
	})
}

// The generic @@replace and @@split loops check for interrupts.
func TestRegExpProtocolInterrupt(t *testing.T) {
	for _, method := range []string{"replace", "split"} {
		r := NewRealm()
		rx := newRegExp(t, r, ``, "g")
		// An own property takes the regexp off its pristine shape.
		require.NoError(t, rx.AsObject().SetProp(r, StringKey(r.InternGoString("x")), Int64Value(1)))
		args := []Value{rx, str("x")}
		if method == "split" {
			args = args[:1]
		}
		r.Interrupt("stop")
		_, err := callMethodErr(r, str(string(make([]byte, 5000))), method, args...)
		var ie *InterruptedError
		require.ErrorAs(t, err, &ie, method)
		r.ClearInterrupt()
	}
}
