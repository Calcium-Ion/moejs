package syntax

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseModule(t *testing.T, src string) *Module {
	t.Helper()
	m, err := ParseModule("t.js", src, Options{})
	require.NoError(t, err)
	return m
}

// TestGoldenDump checks the parsed shape of representative constructs
// through the S-expression dumper.
func TestGoldenDump(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"const", `const x = 1, y = "s";`, "(module\n  (const (x 1) (y \"s\")))"},
		{"let-assign", `let a; a = 2;`, "(module\n  (let (a))\n  (= a 2))"},
		{"function-params", `function f(a, b = 1, ...c) { return a + b; }`,
			"(module\n  (function f (a (= b 1) ...c)\n    (return (+ a b))))"},
		{"arrow-expr", `const f = (a) => a * 2;`, "(module\n  (const (f (=> (a) (* a 2)))))"},
		{"arrow-block", `const g = () => { return 1; };`, "(module\n  (const (g (=> ()\n    (return 1)))))"},
		{"arrow-single-param", `const h = x => x;`, "(module\n  (const (h (=> (x) x))))"},
		{"object", `const o = { a, b: 2, [k]: 3, ...s, m() {}, "q": 4, 5: 6, get: 1, set: 2, async: 3 };`,
			"(module\n  (const (o (object a (b 2) ([k] 3) (... s) (method m ()) (\"q\" 4) (5 6) (get 1) (set 2) (async 3)))))"},
		{"array", `x = [1, , 2, ...r];`, "(module\n  (= x (array 1 <hole> 2 (... r))))"},
		{"object-pattern", `const { a, b: { c }, d = 1, ...rest } = o;`,
			"(module\n  (const ((opat (a a) (b (opat (c c))) (d (= d 1)) (... rest)) o)))"},
		{"array-pattern", `let [x, , y = 2, ...z] = arr;`, "(module\n  (let ((apat x <hole> (= y 2) (... z)) arr)))"},
		{"optional-chain", `a?.b.c?.(d)?.[e];`, "(module\n  (chain (?[] (?call (. (?. a b) c) d) [e])))"},
		{"template", "x = `a${b}c${d}`;", "(module\n  (= x (template \"a\" b \"c\" d \"\")))"},
		{"regex-member", `/ab+c/gi.test(s);`, "(module\n  (call (. /ab+c/gi test) s))"},
		{"nullish-paren", `a ?? (b || c);`, "(module\n  (?? a (|| b c)))"},
		{"ternary-chain", `a ? b : c ? d : e;`, "(module\n  (? a b (? c d e)))"},
		{"exponent-right-assoc", `2 ** 3 ** 2;`, "(module\n  (** 2 (** 3 2)))"},
		{"exponent-paren-unary", `x = (-a) ** 2;`, "(module\n  (= x (** (- a) 2)))"},
		{"update", `i++; --j;`, "(module\n  (post++ i)\n  (pre-- j))"},
		{"labels-loops", `outer: for (let i = 0; i < n; i++) { for (const x of xs) { if (x) continue outer; else break; } }`,
			"(module\n  (label outer (for (let (i 0)) (< i n) (post++ i) (block\n        (for-of (const (x)) xs (block\n            (if x (continue outer) (break))))))))"},
		{"switch", `switch (v) { case 1: f(); break; case 2: default: g(); }`,
			"(module\n  (switch v\n    (case 1\n      (call f)\n      (break))\n    (case 2)\n    (default\n      (call g))))"},
		{"try-catch-finally", `try { f(); } catch (e) { g(e); } finally { h(); }`,
			"(module\n  (try (block\n      (call f)) (catch e (block\n      (call g e))) (finally (block\n      (call h)))))"},
		{"try-optional-catch", `try { f(); } catch { g(); }`, "(module\n  (try (block\n      (call f)) (catch (block\n      (call g)))))"},
		{"do-while", `do { i--; } while (i > 0); while (x) { y(); }`,
			"(module\n  (do (block\n      (post-- i)) (> i 0))\n  (while x (block\n      (call y))))"},
		{"for-in-delete", `for (const k in obj) { delete obj[k]; }`, "(module\n  (for-in (const (k)) obj (block\n      (delete ([] obj [k])))))"},
		{"for-empty", `for (;;) {}`, "(module\n  (for _ _ _ (block)))"},
		{"for-expr-init", `for (i = 0, j = 1; i < j; i++, j--) ;`, "(module\n  (for (seq (= i 0) (= j 1)) (< i j) (seq (post++ i) (post-- j)) (empty)))"},
		{"throw-new", `throw new Error("x" + y);`, "(module\n  (throw (new Error (+ \"x\" y))))"},
		{"unary", `typeof x === "string" && void 0 !== null;`, "(module\n  (&& (=== (typeof x) \"string\") (!== (void 0) null)))"},
		{"in-instanceof", `a in b, c instanceof D;`, "(module\n  (seq (in a b) (instanceof c D)))"},
		{"compound-assign", `x += 1; y ??= 2; z ||= 3; w &&= 4; v **= 2; u >>>= 1;`,
			"(module\n  (+= x 1)\n  (??= y 2)\n  (||= z 3)\n  (&&= w 4)\n  (**= v 2)\n  (>>>= u 1))"},
		{"bitwise", `a & b | c ^ d << 1 >> 2 >>> 3 & ~e;`, "(module\n  (| (& a b) (^ c (& (>>> (>> (<< d 1) 2) 3) (~ e)))))"},
		{"exports", `export const a = 1; export function f() {} export { a as b, f as default };`,
			"(module\n  (export (const (a 1)))\n  (export (function f ()))\n  (export {a as b, f as default}))"},
		{"export-default-anon", `export default function () {}`, "(module\n  (export-default (function ())))"},
		{"export-default-expr", `export default 1 + 2;`, "(module\n  (export-default (+ 1 2)))"},
		{"spread-call", `f(...args, 1);`, "(module\n  (call f (... args) 1))"},
		{"new-forms", `new Foo(1, 2).bar; new Foo; new new A()();`, "(module\n  (. (new Foo 1 2) bar)\n  (new Foo)\n  (new (new A)))"},
		{"this", `this; function f() { return this.x; }`, "(module\n  this\n  (function f ()\n    (return (. this x))))"},
		{"string-escapes", `x = "\x41\n\x42\u{1F600}";`, "(module\n  (= x \"A\\nB😀\"))"},
		{"numbers", `x = 0x1F + 0b101 + 0o17 + 1_000 + .5 + 5. + 1e3 + 1n + 0x10n;`,
			"(module\n  (= x (+ (+ (+ (+ (+ (+ (+ (+ 31 5) 15) 1000) 0.5) 5) 1000) 1n) 16n)))"},
		{"unicode-ident", `const ünïcödé = 1; ünïcödé;`, "(module\n  (const (ünïcödé 1))\n  ünïcödé)"},
		{"named-func-expr", `a = function g() { return g; };`, "(module\n  (= a (function g ()\n    (return g))))"},
		{"destructuring-assign", `({ a: [b, { c }] } = o);`, "(module\n  (= (opat (a (apat b (opat (c c))))) o))"},
		{"array-assign-member", `[a, b.c, ...d] = arr;`, "(module\n  (= (apat a (. b c) (... d)) arr))"},
		{"label-block", `label: { break label; }`, "(module\n  (label label (block\n      (break label))))"},
		{"if-else-chain", `if (a) { b; } else if (c) d; else e;`, "(module\n  (if a (block\n      b) (if c d e)))"},
		{"sequence-paren", `x = (1, 2, 3);`, "(module\n  (= x (seq 1 2 3)))"},
		{"conditional-fraction", `x = a ? .5 : b;`, "(module\n  (= x (? a 0.5 b)))"},
		{"var-debugger", `var v = 1; debugger;`, "(module\n  (var (v 1))\n  (debugger))"},
		{"proto-computed-ok", `x = { __proto__: 1, ['__proto__']: 2 };`, "(module\n  (= x (object (__proto__ 1) ([\"__proto__\"] 2))))"},
		{"directive", `'use strict'; x;`, "(module\n  \"use strict\"\n  x)"},
		{"keyword-property-names", `x = { if: 1, class: 2 }.if.class; y.default;`, "(module\n  (= x (. (. (object (if 1) (class 2)) if) class))\n  (. y default))"},
		{"comma-in-args", `f(a, (b, c));`, "(module\n  (call f a (seq b c)))"},
		{"nested-template", "x = `a${`b${c}`}`;", "(module\n  (= x (template \"a\" (template \"b\" c \"\") \"\")))"},
		{"hashbang", "#!/usr/bin/env node\nx;", "(module\n  x)"},
		{"comments", "/* a */ x; // b\n/** c\n */ y;", "(module\n  x\n  y)"},
		{"arrow-in-conditional", `x = a ? b => b : () => 1;`, "(module\n  (= x (? a (=> (b) b) (=> () 1))))"},
		{"arrow-object-body", `x = () => ({ a });`, "(module\n  (= x (=> () (object a))))"},
		{"arrow-destructured-params", `x = ({ a, b = 1 }, [c], ...d) => a;`, "(module\n  (= x (=> ((opat (a a) (b (= b 1))) (apat c) ...d) a)))"},
		{"new-member-call", `new a.b.C(1)(2);`, "(module\n  (call (new (. (. a b) C) 1) 2))"},
		{"optional-call-args", `a?.(1, ...b);`, "(module\n  (chain (?call a 1 (... b))))"},
		{"in-operator-in-for-body", `for (let k in o) if ("x" in o) f();`, "(module\n  (for-in (let (k)) o (if (in \"x\" o) (call f))))"},
		{"empty-statements", `;;`, "(module\n  (empty)\n  (empty))"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := parseModule(t, tt.src)
			assert.Equal(t, tt.want, Dump(m))
		})
	}
}

func TestGoldenUnsupportedShapes(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"class", `class A extends B { static x = 1; #p; constructor() { super(); } get y() { return 1; } static { } m() {} static s() {} }`,
			"(module\n  (class A (extends B)\n    (static field x 1)\n    (field #p)\n    (method constructor ()\n      (call super))\n    (get y ()\n      (return 1))\n    (static block)\n    (method m ())\n    (static (method s ()))))"},
		{"generator", `function* g() { yield 1; yield* h(); yield; }`, "(module\n  (function* g ()\n    (yield 1)\n    (yield* (call h))\n    (yield)))"},
		{"async", `async function f() { await x; } const g = async (a) => a; const h = async a => a;`,
			"(module\n  (async function f ()\n    (await x))\n  (const (g (async => (a) a)))\n  (const (h (async => (a) a))))"},
		{"tagged", "tag`a${b}c`;", "(module\n  (tagged tag (template \"a\" b \"c\")))"},
		{"tagged-invalid-escape", "tag`\\01${b}\\u{`;", "(module\n  (tagged tag (template undefined b undefined)))"},
		{"tagged-member", "a.b`x`;", "(module\n  (tagged (. a b) (template \"x\")))"},
		{"tagged-call", "f()`x`;", "(module\n  (tagged (call f) (template \"x\")))"},
		{"tagged-chain", "t`a``b`;", "(module\n  (tagged (tagged t (template \"a\")) (template \"b\")))"},
		{"new-tagged", "new t`x`;", "(module\n  (new (tagged t (template \"x\"))))"},
		{"new-tagged-args", "new t`x`(1);", "(module\n  (new (tagged t (template \"x\")) 1))"},
		{"imports", `import d, { a as b, c } from "m"; import * as ns from "n"; import "side";`,
			"(module\n  (import d {a as b, c} from \"m\")\n  (import * as ns from \"n\")\n  (import from \"side\"))"},
		{"re-exports", `export * as ns from "m"; export { x } from "m";`, "(module\n  (export * as ns from \"m\")\n  (export {x} from \"m\"))"},
		{"getters-setters", `x = { get a() { return 1; }, set a(v) {} };`, "(module\n  (= x (object (get a ()\n    (return 1)) (set a (v)))))"},
		{"new-target-super", `x = { f() { new.target; super.x; } };`, "(module\n  (= x (object (method f ()\n    new.target\n    (. super x)))))"},
		{"import-meta-call", `import.meta; import("x");`, "(module\n  import.meta\n  (import \"x\"))"},
		{"new-import-meta", `new import.meta; new import.meta.C(1);`, "(module\n  (new import.meta)\n  (new (. import.meta C) 1))"},
		{"arguments", `function f() { return arguments; }`, "(module\n  (function f ()\n    (return arguments)))"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := ParseModule("t.js", tt.src, Options{AllowUnsupported: true})
			require.NoError(t, err)
			assert.Equal(t, tt.want, Dump(m))
		})
	}
}

func TestParseScript(t *testing.T) {
	s, err := ParseScript("t.js", "var a = 1; function f() { return a; } f();", Options{})
	require.NoError(t, err)
	assert.Equal(t, "(script\n  (var (a 1))\n  (function f ()\n    (return a))\n  (call f))", Dump(s))
	assert.Equal(t, ScopeScript, s.Scope.Kind)
	assert.NotNil(t, s.Scope.Lookup("a"))
}

func TestSpans(t *testing.T) {
	src := "const f = function named(a) { return a; };\nconst g = (a, b) => a + b;\nx = { m(v) { return v; } };"
	m := parseModule(t, src)
	text := func(n Node) string {
		pos, end := n.Range()
		return src[pos:end]
	}
	fn := m.Body[0].(*VarDecl).Decls[0].Init.(*Function)
	assert.Equal(t, "function named(a) { return a; }", text(fn))
	assert.Equal(t, "named", text(fn.Name))
	assert.Equal(t, "return a;", text(fn.Body.Body[0]))
	assert.Equal(t, "const f = function named(a) { return a; };", text(m.Body[0]))

	arrow := m.Body[1].(*VarDecl).Decls[0].Init.(*Function)
	assert.Equal(t, "(a, b) => a + b", text(arrow))
	assert.Equal(t, "a + b", text(arrow.ExprBody))

	method := m.Body[2].(*ExprStmt).X.(*AssignExpr).Value.(*ObjectLit).Props[0].Value.(*Function)
	assert.Equal(t, "m(v) { return v; }", text(method))
	assert.Equal(t, FuncMethod, method.Kind)
	assert.Equal(t, "m", method.Name.Name)

	line, col := m.File.Position(arrow.Pos)
	assert.Equal(t, [2]int{2, 11}, [2]int{line, col})
	assert.Equal(t, len(src), m.End)
	assert.Equal(t, 0, m.Pos)
}

// TestAssignParenTarget checks that a parenthesised identifier target is
// flagged: it is not an IdentifierRef, so the value is not named after it.
func TestAssignParenTarget(t *testing.T) {
	m := parseModule(t, "let a, b, c; a = 1; (b) = 2; ((c)) ||= 3; (a.b) = 4; (a = 5);")
	want := []bool{false, true, true, false, false}
	for i, w := range want {
		e := m.Body[i+1].(*ExprStmt).X.(*AssignExpr)
		assert.Equal(t, w, e.ParenTarget, i)
	}
}

func TestFunctionSummaryFields(t *testing.T) {
	m := parseModule(t, "function a(x, y = 1, ...z) {} const b = (p, q) => 1; function c({ d }, [e]) {} function f(g, h = 2, i) {}")
	fa := m.Body[0].(*FuncDecl).Func
	assert.Equal(t, 2, fa.ParamCount)
	assert.Equal(t, 1, fa.Length)
	assert.False(t, fa.HasSimpleParams)
	assert.True(t, fa.HasRest)
	assert.True(t, fa.IsStrict)
	assert.False(t, fa.IsArrow)

	fb := m.Body[1].(*VarDecl).Decls[0].Init.(*Function)
	assert.Equal(t, 2, fb.ParamCount)
	assert.Equal(t, 2, fb.Length)
	assert.True(t, fb.HasSimpleParams)
	assert.False(t, fb.HasRest)
	assert.True(t, fb.IsArrow)
	assert.Equal(t, FuncArrow, fb.Kind)

	fc := m.Body[2].(*FuncDecl).Func
	assert.Equal(t, 2, fc.ParamCount)
	assert.Equal(t, 2, fc.Length)
	assert.False(t, fc.HasSimpleParams)

	ff := m.Body[3].(*FuncDecl).Func
	assert.Equal(t, 3, ff.ParamCount)
	assert.Equal(t, 1, ff.Length)
}

func TestWalkVisitsEveryNode(t *testing.T) {
	m, err := ParseModule("t.js", "export function f(a = 1, { b }, ...c) { for (const x of a) { if (x) continue; } try { g`t`; } catch ({ e }) {} finally {} return [a, ...c, { b, [c]: 1, m() {} }] ?? this; }", Options{AllowUnsupported: true})
	require.NoError(t, err)
	var count, idents int
	Inspect(m, func(n Node) bool {
		count++
		if _, ok := n.(*Ident); ok {
			idents++
		}
		return true
	})
	assert.Greater(t, count, 40)
	assert.Greater(t, idents, 12)

	// Returning false skips children.
	var seen int
	Inspect(m, func(n Node) bool {
		seen++
		_, isFunc := n.(*Function)
		return !isFunc
	})
	assert.Equal(t, 4, seen) // module, export, funcdecl, function
}

func TestParseErrorType(t *testing.T) {
	_, err := ParseModule("plugin.js", "let a;\nlet a;", Options{})
	require.Error(t, err)
	var se *Error
	require.ErrorAs(t, err, &se)
	assert.Equal(t, "plugin.js:2:5: SyntaxError: Identifier 'a' has already been declared", err.Error())
	assert.Equal(t, "Identifier 'a' has already been declared", se.Message())
	assert.Equal(t, "plugin.js", se.Name)
	assert.Equal(t, 11, se.Pos)
	assert.Equal(t, 2, se.Line)
	assert.Equal(t, 5, se.Col)
}
