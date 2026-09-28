package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRegExpCompile covers RegExp.prototype.compile (B.2.4.1) in both kinds
// of realm, and the paths that hold a RegExp's payload while user code
// that may recompile it runs (staging/sm/RegExp and annexB's
// Symbol.split).
func TestRegExpCompile(t *testing.T) {
	runProtoCases(t, []protoCase{
		{"descriptor", `const d = Object.getOwnPropertyDescriptor(RegExp.prototype, "compile");
			return [typeof d.value, d.writable === d.configurable, d.enumerable, d.value.length, d.value.name].join()`,
			"function,true,false,2,compile"},
		{"recompiles in place", `const re = /a/g; re.lastIndex = 3; const r = re.compile("b+", "i");
			return [r === re, re.source, re.flags, re.lastIndex, re.test("xBB"), re.test("a")].join()`,
			"true,b+,i,0,true,false"},
		{"pattern RegExp", `const re = /a/; re.compile(/x(y)/gi); return [re.source, re.flags, re.exec("XY")[1]].join()`,
			"x(y),gi,Y"},
		{"pattern RegExp itself", `const re = /a/m; re.compile(re); return [re.source, re.flags].join()`, "a,m"},
		{"pattern RegExp and flags", `/a/.compile(/x/, "g")`, "!TypeError"},
		{"undefined", `const re = /a/g; re.compile(); return [re.source, re.flags, re.test("")].join()`, "(?:),,true"},
		{"coerced", `const re = /a/; re.compile({toString() { return "b" }}, {toString() { return "y" }}); return [re.source, re.flags].join()`,
			"b,y"},
		{"flags order", `const re = /(?:)/; re.compile("(?:)", "imsuyg"); return re.flags`, "gimsuy"},
		{"invalid flags leave it", `const re = /a/g; try { re.compile("b", "gg"); } catch (e) { return [e.name, re.source, re.flags].join(); }`,
			"SyntaxError,a,g"},
		{"invalid pattern leaves it", `const re = /a/g; try { re.compile("("); } catch (e) { return [e.name, re.source, re.flags].join(); }`,
			"SyntaxError,a,g"},
		{"u pattern", `const re = /a/; try { re.compile("\\-", "u"); } catch (e) { return e.name; }`, "SyntaxError"},
		{"RegExp called", `const re = RegExp("a"); re.compile("b"); return re.source`, "b"},
		{"new RegExp", `const re = new RegExp("a"); re.compile("b"); return re.source`, "b"},
		{"String.prototype.match's RegExp", `let re; "a".match({[Symbol.match]: undefined, toString() { return "a"; }});
			re = RegExp.prototype[Symbol.matchAll].call(/a/g, "a").next().value; return re[0]`, "a"},
		{"subclass instance", `class R extends RegExp {} const re = new R("a");
			try { re.compile("b"); } catch (e) { return [e.name, re.source].join(); }`, "TypeError,a"},
		{"other newTarget", `const re = Reflect.construct(RegExp, ["a"], Object);
			try { RegExp.prototype.compile.call(re, "b"); } catch (e) { return [e.name, RegExp.prototype.source.call === undefined].join(); }`,
			"TypeError,true"},
		{"not a RegExp", `RegExp.prototype.compile.call({})`, "!TypeError"},
		{"the prototype", `RegExp.prototype.compile.call(RegExp.prototype)`, "!TypeError"},
		{"primitive", `RegExp.prototype.compile.call("a")`, "!TypeError"},
		{"non-writable lastIndex", `const re = /foo/i; Object.defineProperty(re, "lastIndex", {value: 42, writable: false});
			try { re.compile("bar"); } catch (e) { return [e.name, re.source, re.flags, re.lastIndex, re.test("bar"), re.test("BAR")].join(); }`,
			"TypeError,bar,,42,true,false"},

		// RegExpBuiltinExec reads the flags and matcher after lastIndex.
		{"exec: valueOf recompiles", `const re = /a/; re.lastIndex = {valueOf() { re.compile("b", "g"); return 0; }};
			const m = re.exec("ab"); return [m[0], m.index, re.lastIndex].join()`, "b,1,2"},
		{"test: valueOf recompiles", `const out = [];
			for (const flag of ["", "y", "g"]) {
				const re = new RegExp("a", flag); re.lastIndex = {valueOf() { re.compile("b"); return 0; }};
				out.push(re.test("b"));
			}
			return out.join()`, "true,true,true"},
		{"test: valueOf adds g", `const re = /a/; re.lastIndex = {valueOf() { re.compile("a", "g"); return 0; }};
			return [re.test("aa"), re.lastIndex].join()`, "true,1"},
		{"test: simple class", `const re = /\s/; re.lastIndex = {valueOf() { re.compile("x"); return 0; }}; return re.test("a b")`, "false"},
		{"match: valueOf recompiles", `const out = [];
			for (const flag of ["", "y"]) {
				const re = new RegExp("a", flag); re.lastIndex = {valueOf() { re.compile("b"); return 0; }};
				out.push(re[Symbol.match]("b") !== null);
			}
			return out.join()`, "true,true"},
		{"match: valueOf adds g", `const re = /a/; re.lastIndex = {valueOf() { re.compile("a", "g"); return 0; }};
			re[Symbol.match]("a"); return re.lastIndex`, "1"},
		{"match: valueOf drops y", `const re = /a/y; re.lastIndex = {valueOf() { re.compile("a", ""); re.lastIndex = 9000; return 0; }};
			re[Symbol.match]("a"); return re.lastIndex`, "9000"},
		{"match: groups of the new program", `const re = /(?<x>a)/; re.lastIndex = {valueOf() { re.compile("(?<y>b)"); return 0; }};
			const m = "ab".match(re); return [m[0], m.groups.y, "x" in m.groups].join()`, "b,b,false"},
		{"replace: valueOf recompiles", `const out = [];
			for (const flag of ["", "y"]) {
				const re = new RegExp("a", flag); re.lastIndex = {valueOf() { re.compile("b"); return 0; }};
				out.push(re[Symbol.replace]("b", "pass"));
			}
			return out.join()`, "pass,pass"},
		{"replace: valueOf adds g", `const re = /a/; re.lastIndex = {valueOf() { re.compile("a", "g"); return 0; }};
			return [re[Symbol.replace]("aa", "_"), re.lastIndex].join()`, "_a,1"},
		{"replace: valueOf drops y", `const re = /a/y; re.lastIndex = {valueOf() { re.compile("a", ""); re.lastIndex = 9000; return 0; }};
			re[Symbol.replace]("a", ""); return re.lastIndex`, "9000"},
		{"replace: simple class", `const re = /\s/; re.lastIndex = {valueOf() { re.compile("x"); return 0; }}; return "a x".replace(re, "_")`, "a _"},

		// The spec finds every match before the first replacer call.
		{"replacer recompiles", `const rx = RegExp("a", "g");
			const a = rx[Symbol.replace]("abba", () => { rx.compile("b", "g"); return "?"; });
			const rx2 = RegExp("a", "g");
			const b = "abba".replace(rx2, () => { rx2.compile("b", "g"); return "?"; });
			return [a, b].join()`, "?bb?,?bb?"},
		{"replacer recompiles: simple class", `const rx = /\s/g; return " a b".replace(rx, () => { rx.compile("a", "g"); return "_"; })`, "_a_b"},
		{"replacer recompiles: elem base", `const rx = /a/g; const b = {get a() { rx.compile("b"); return "A"; }};
			return rx[Symbol.replace]("aaa", a => b[a])`, "AAA"},
		{"replacer recompiles: groups", `const rx = /(?<x>a)/g;
			return "aa".replace(rx, (...args) => { const g = args[args.length - 1]; rx.compile("(?<y>b)", "g"); return g.x; })`, "aa"},
		{"replacer recompiles: captures", `const rx = /(a)(b)?/g;
			return "aab".replace(rx, (m, p1, p2) => { rx.compile("(c)", "g"); return "[" + p1 + (p2 ?? "-") + "]"; })`, "[a-][ab]"},

		// The splitter and matcher are made before the limit and lastIndex.
		{"split: @@match recompiles", `const re = /a/; Object.defineProperty(re, Symbol.match, {get() { re.compile("b"); }});
			return JSON.stringify(re[Symbol.split]("abba"))`, `["a","","a"]`},
		{"split: limit recompiles", `const re = /a/; const limit = {valueOf() { re.compile("b"); return -1; }};
			return JSON.stringify(re[Symbol.split]("abba", limit))`, `["","bb",""]`},
		{"matchAll: lastIndex recompiles", `const re = /a/g; re.lastIndex = {valueOf() { re.compile("b", "g"); return 0; }};
			return [...re[Symbol.matchAll]("ab")].map(m => m[0]).join()`, "a"},
	})
}

// TestRegExpCompileMutable covers the pending compile of a mutable realm's
// %RegExp.prototype%: every way to observe the prototype defines it where
// the shared template has it.
func TestRegExpCompileMutable(t *testing.T) {
	runMutableProtoCases(t, []protoCase{
		{"descriptor", `const d = Object.getOwnPropertyDescriptor(RegExp.prototype, "compile"); return [d.writable, d.configurable].join()`,
			"true,true"},
		{"write first", `RegExp.prototype.foo = 1; return Object.getOwnPropertyNames(RegExp.prototype).slice(-2).join()`, "compile,foo"},
		{"define first", `Object.defineProperty(RegExp.prototype, "foo", {value: 1});
			return Object.getOwnPropertyNames(RegExp.prototype).slice(-2).join()`, "compile,foo"},
		{"delete first", `delete RegExp.prototype.exec; return [typeof RegExp.prototype.compile, "exec" in RegExp.prototype].join()`, "function,false"},
		{"delete it", `delete RegExp.prototype.compile; return typeof /a/.compile`, "undefined"},
		{"replace it", `RegExp.prototype.compile = 1; return /a/.compile`, "1"},
		{"has", `return "compile" in /a/`, "true"},
		{"own descriptor", `return Object.getOwnPropertyDescriptor(RegExp.prototype, "compile").value.name`, "compile"},
		{"hasOwn", `return Object.hasOwn(RegExp.prototype, "compile")`, "true"},
		{"shadows Object.prototype", `Object.prototype.compile = 1; return typeof /a/.compile`, "function"},
		{"cache filled before", `Object.prototype.compile = 1; const get = o => o.compile;
			get({}); get({}); return [get({}), typeof get(/a/), typeof get(/b/)].join()`, "1,function,function"},
		{"prevent extensions", `Object.preventExtensions(RegExp.prototype); return typeof RegExp.prototype.compile`, "function"},
		{"freeze", `Object.freeze(RegExp.prototype); return Object.isFrozen(RegExp.prototype) && typeof /a/.compile`, "function"},
		{"new prototype", `Object.setPrototypeOf(RegExp.prototype, null); return typeof /a/.compile`, "function"},
		{"pristine paths", `const s = "a-b".replace(/-/, "+") + "a-b".split(/-/).join("+") + /b/.test("b");
			/x/.compile("y"); return s + "a-b".replace(/-/, "+") + "a-b".split(/-/).join("+") + /b/.test("b")`, "a+ba+btruea+ba+btrue"},
		{"String conversions", `return [String(/a/), "" + /b/g, Object.prototype.toString.call(/c/), typeof RegExp.prototype.compile].join()`,
			"/a/,/b/g,[object RegExp],function"},
	})
}

// TestRegExpCompileKeyOrder: a mutable realm lists %RegExp.prototype%'s keys
// in the shared template's order, however the pending compile is defined.
func TestRegExpCompileKeyOrder(t *testing.T) {
	keys := `return Reflect.ownKeys(RegExp.prototype).map(String).join()`
	want := evalProtoBody(t, keys, true)
	assert.Contains(t, want, "hasIndices,compile,Symbol(Symbol.match)")
	assert.Equal(t, want, evalProtoBody(t, keys, false), "listed first")
	assert.Equal(t, want, evalProtoBody(t, `/a/.compile; `+keys, false), "looked up first")
	assert.Equal(t, want, evalProtoBody(t, `/a/.exec("a"); "a".replace(/a/, "b"); `+keys, false), "after the pristine paths")
}

// TestRegExpCompileLazy: a mutable realm defines compile on first use only,
// keeping the pristine paths meanwhile; a shared realm has it from the
// template.
func TestRegExpCompileLazy(t *testing.T) {
	r := NewRealm()
	p := r.RegExpPrototype
	require.IsType(t, (*pendingCompile)(nil), p.internal)
	assert.True(t, p.shape.noFill)
	rx, err := r.NewRegExp(FromGoString("a"), AtomEmpty)
	require.NoError(t, err)
	assert.NotNil(t, r.pristineRegExp(rx, rxExec|rxFlags|rxMatch|rxReplace), "a pending compile keeps the pristine paths")
	assert.True(t, r.lacksWellKnown(rx, SymbolKey(SymToPrimitive)))
	_, _, ok := p.shape.Lookup(compileKey)
	assert.False(t, ok)

	v, err := rx.GetProp(r, compileKey)
	require.NoError(t, err)
	require.True(t, IsCallable(v))
	assert.Nil(t, p.internal)
	assert.Zero(t, p.flags&flagHasLazy)
	assert.NotNil(t, r.pristineRegExp(rx, rxExec|rxFlags|rxMatch|rxReplace))
	_, err = r.Call(v, ObjectValue(rx), []Value{StringValue(FromGoString("b")), StringValue(FromGoString("g"))})
	require.NoError(t, err)
	assert.Equal(t, "b", rx.RegExpData().Source().GoString())
	assert.NotNil(t, r.pristineRegExp(rx, rxExec|rxFlags|rxMatch|rxReplace), "compile keeps the instance shape")

	s := newShared()
	assert.Nil(t, s.RegExpPrototype.internal)
	_, _, ok = s.RegExpPrototype.shape.Lookup(compileKey)
	assert.True(t, ok)
}

// TestRegExpCompileRealm: a RegExp a host hands to another realm is not
// that realm's to recompile.
func TestRegExpCompileRealm(t *testing.T) {
	for _, shared := range []bool{false, true} {
		a, b := NewRealmWith(RealmOptions{SharedIntrinsics: shared}), NewRealmWith(RealmOptions{SharedIntrinsics: shared})
		rx, err := a.NewRegExp(FromGoString("a"), AtomEmpty)
		require.NoError(t, err)
		compile, err := b.RegExpPrototype.GetProp(b, compileKey)
		require.NoError(t, err)
		_, err = b.Call(compile, ObjectValue(rx), []Value{StringValue(FromGoString("b"))})
		assert.Equal(t, "TypeError: RegExp.prototype.compile requires a RegExp created by this realm's RegExp constructor", errorString(err))
		assert.Equal(t, "a", rx.RegExpData().Source().GoString())
		compile, err = a.RegExpPrototype.GetProp(a, compileKey)
		require.NoError(t, err)
		_, err = a.Call(compile, ObjectValue(rx), []Value{StringValue(FromGoString("b"))})
		require.NoError(t, err)
		assert.Equal(t, "b", rx.RegExpData().Source().GoString())
	}
}
