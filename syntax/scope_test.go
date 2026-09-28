package syntax

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// findBinding returns the first binding named name anywhere in the scope
// tree, depth first.
func findBinding(s *Scope, name string) *Binding {
	if b := s.Lookup(name); b != nil {
		return b
	}
	for _, c := range s.Children {
		if b := findBinding(c, name); b != nil {
			return b
		}
	}
	return nil
}

// functions collects every function in the module keyed by name.
func functions(m *Module) map[string]*Function {
	out := map[string]*Function{}
	Inspect(m, func(n Node) bool {
		switch n := n.(type) {
		case *Function:
			if n.Name != nil {
				out[n.Name.Name] = n
			}
		case *Declarator:
			if fn, ok := n.Init.(*Function); ok && fn.Name == nil {
				out[n.Target.(*Ident).Name] = fn
			}
		}
		return true
	})
	return out
}

func TestScopeCapture(t *testing.T) {
	m := parseModule(t, "const a = 1; function f() { return a; } const b = 2; b; let c = 1; const g = () => c; function h(p) { return p; }")
	assert.True(t, m.Scope.Lookup("a").Captured)
	assert.False(t, m.Scope.Lookup("b").Captured)
	assert.True(t, m.Scope.Lookup("c").Captured)
	assert.False(t, m.Scope.Lookup("f").Captured)
	assert.Equal(t, BindFunction, m.Scope.Lookup("f").Kind)
	h := functions(m)["h"]
	assert.False(t, h.Scope.Lookup("p").Captured)

	m = parseModule(t, "function outer(x) { function inner() { return x; } { let y = 1; const z = () => y; } return inner; }")
	outer := functions(m)["outer"]
	assert.True(t, outer.Scope.Lookup("x").Captured)
	assert.False(t, outer.Scope.Lookup("inner").Captured)
	assert.True(t, findBinding(outer.Scope, "y").Captured)
	assert.False(t, findBinding(outer.Scope, "z").Captured)
}

func TestScopeTDZ(t *testing.T) {
	tests := []struct {
		src  string
		name string
		want bool
	}{
		{"f(); let x = 1; function f() { return x; }", "x", true},
		{"let x = 1; function f() { return x; }", "x", false},
		{"let x = 1; function f() { return x; } f();", "x", false},
		{"const y = y;", "y", true},
		{"let z = 1; const g = () => z;", "z", false},
		{"const h = () => w; let w = 1;", "w", true},
		{"switch (a) { case 1: let s = 1; s; }", "s", true},
		{"for (let q of q) {}", "q", true},
		{"for (let i = 0; i < 3; i++) { i; }", "i", false},
		{"for (const e of xs) { e; }", "e", false},
		{"g(); const x = 1; function g() { f(); } function f() { x; }", "x", true},
		{"const x = 1; function g() { f(); } function f() { x; } g();", "x", false},
		{"typeof t; let t = 1;", "t", true},
		{"{ let b = 1; function f() { b; } }", "b", false},
		{"let m = 1; export function f() { m; }", "m", false},
		{"export function f() { m; } let m = 1;", "m", false},
		// The host calls the exports of a module with top-level await while
		// it awaits: they read early from the first await on.
		{"export function f() { m; } let m = 1; await 0;", "m", false},
		{"export function f() { m; } await 0; let m = 1;", "m", true},
		{"function f() { m; } await 0; let m = 1;", "m", false},
		{"export default function () { m; } await 0; let m = 1;", "m", true},
		{"export function f() { g(); } function g() { m; } await 0; let m = 1;", "m", true},
		{"function f() { m; } export { f as g }; await 0; let m = 1;", "m", true},
		{"export function f() { new K(); } await 0; class K {}", "K", true},
		{"export let m = await 0; export function f() { m; }", "m", true},
		{"for await (const x of xs); export function f() { m; } let m = 1;", "m", true},
		{"async function a() { await 0; } export function f() { m; } let m = 1;", "m", false},
		{"const arr = [1].map(() => k); let k = 1;", "k", true},
		{"let k = 1; const arr = [1].map(() => k);", "k", false},
		{"function outer() { let v = 1; function inner() { v; } inner(); }", "v", false},
		{"function outer() { inner(); let v = 1; function inner() { v; } }", "v", true},
		{"let p = 1; const q = function () { p; };", "p", false},
		{"const q = function () { p; }; let p = 1;", "p", true},
		{"function f() { x; } const x = 1; f();", "x", false},
		{"const o = { m() { return c; } }; const c = 1;", "c", true},
		{"const c = 1; const o = { m() { return c; } };", "c", false},
		{"let a = 1; { a; }", "a", false},
		{"{ a; } let a = 1;", "a", true},
		{"let r = 1; while (r) { const u = () => r; }", "r", false},
		{"const s = 1; f(); function f() { g(); } function g() { s; }", "s", false},
		{"function w() { return s2; } let s2 = 1; w();", "s2", false},
		{"function w() { return s3; } w(); let s3 = 1;", "s3", true},
		{"let lit = 1; class C {}", "lit", false},
		{"const [d = d2, d2] = a;", "d2", true},
		{"function f(a = b, b) {}", "b", true},
		{"function f(a = a) {}", "a", true},
		{"function f(a = () => b, b) {}", "b", true},
		{"function f(a, b = a) {}", "a", false},
		{"function f(a, b = () => a) {}", "a", false},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			m, err := ParseModule("t.js", tt.src, Options{AllowUnsupported: true})
			require.NoError(t, err)
			b := findBinding(m.Scope, tt.name)
			require.NotNil(t, b, tt.name)
			assert.Equal(t, tt.want, b.NeedsTDZ)
		})
	}
	// With EarlyExports, or imports, the exports read early from the start:
	// a module of an import cycle may call them before the body runs.
	for _, tt := range []struct {
		src  string
		opts Options
		want bool
	}{
		{"let m = 1; export function f() { m; }", Options{EarlyExports: true}, true},
		{"let m = 1; export function f() { g(); } function g() { m; }", Options{EarlyExports: true}, true},
		{"let m = 1; function f() { m; }", Options{EarlyExports: true}, false},
		{"let m = 1; export const f = () => m;", Options{EarlyExports: true}, false},
		{"let m = 1; export function f() { m; }", Options{}, false},
		{"import 'x'; let m = 1; export function f() { m; }", Options{}, true},
	} {
		m, err := ParseModule("t.js", tt.src, tt.opts)
		require.NoError(t, err)
		assert.Equal(t, tt.want, m.Scope.Lookup("m").NeedsTDZ, tt.src)
	}
	// var, param and function bindings never need TDZ checks.
	m := parseModule(t, "v; var v = 1; function f(p) { p; f; }")
	assert.False(t, m.Scope.Lookup("v").NeedsTDZ)
	assert.False(t, m.Scope.Lookup("f").NeedsTDZ)
	assert.False(t, functions(m)["f"].Scope.Lookup("p").NeedsTDZ)
}

func TestScopeParamBody(t *testing.T) {
	// The body's var scope stays merged with the parameters unless a body
	// declaration shadows a parameter or a name the parameter list uses
	// (FunctionDeclarationInstantiation step 28).
	tests := []struct {
		src   string
		split bool
	}{
		{"function f(a, b) { var a; }", false},
		{"function f(a = 1) { var x; let y; function g() {} }", false},
		{"function f(a = 1) { var a; }", true},
		{"function f(a = x) { var x; }", true},
		{"function f(a = () => x) { var x; }", true},
		{"function f(a = 1) { function a() {} }", true},
		{"function f({ [k]: a }) { let k; }", true},
		{"function f(a = y) { var x; }", false},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			fn := functions(parseModule(t, tt.src))["f"]
			if !tt.split {
				assert.Nil(t, fn.Body.Scope)
				return
			}
			require.NotNil(t, fn.Body.Scope)
			assert.Same(t, fn.Scope, fn.Body.Scope.Parent)
			assert.Equal(t, ScopeFunction, fn.Body.Scope.Kind)
		})
	}
	fn := functions(parseModule(t, "function f(a = x) { var x; let y; }"))["f"]
	assert.Nil(t, fn.Scope.Lookup("x"))
	assert.Equal(t, BindVar, fn.Body.Scope.Lookup("x").Kind)
	assert.Equal(t, BindLet, fn.Body.Scope.Lookup("y").Kind)
	assert.Equal(t, BindParam, fn.Body.Scope.Resolve("a").Kind)
}

func TestScopeHoistingOrder(t *testing.T) {
	m := parseModule(t, "function f(p, [q]) { var v = 1; let l = 2; function g() {} const { c } = o; if (v) { var w; } }")
	fn := m.Body[0].(*FuncDecl).Func
	require.NotNil(t, fn.Scope)
	assert.Equal(t, ScopeFunction, fn.Scope.Kind)
	assert.Same(t, fn, fn.Scope.Func)
	assert.Same(t, m.Scope, fn.Scope.Parent)
	assert.Same(t, fn, fn.Scope.Node)
	var names []string
	var kinds []BindKind
	for i, b := range fn.Scope.Bindings {
		names = append(names, b.Name)
		kinds = append(kinds, b.Kind)
		assert.Equal(t, i, b.Slot)
		assert.Same(t, fn.Scope, b.Scope)
	}
	// Parameters first, then var/function declarations in source order,
	// then body-level lexical declarations.
	assert.Equal(t, []string{"p", "q", "v", "g", "w", "l", "c"}, names)
	assert.Equal(t, []BindKind{BindParam, BindParam, BindVar, BindFunction, BindVar, BindLet, BindConst}, kinds)
	require.Len(t, fn.Scope.Funcs, 1)
	assert.Equal(t, "g", fn.Scope.Funcs[0].Func.Name.Name)
	// The if-block declares nothing lexical, so it has no scope.
	assert.Nil(t, fn.Body.Body[4].(*IfStmt).Then.(*BlockStmt).Scope)

	m = parseModule(t, "x = 1; var x; function h() {} let y; export const z = 1;")
	var modNames []string
	for _, b := range m.Scope.Bindings {
		modNames = append(modNames, b.Name)
	}
	assert.Equal(t, []string{"x", "h", "y", "z"}, modNames)
	assert.Equal(t, ScopeModule, m.Scope.Kind)
	assert.Nil(t, m.Scope.Parent)
	assert.Nil(t, m.Scope.Func)
	require.Len(t, m.Scope.Funcs, 1)
	ref := m.Body[0].(*ExprStmt).X.(*AssignExpr).Target.(*Ident)
	assert.Same(t, m.Scope.Lookup("x"), ref.Binding)
	assert.Equal(t, BindVar, ref.Binding.Kind)
}

func TestScopeBlocks(t *testing.T) {
	m := parseModule(t, "{ let a = 1; a; } a; { b; } { function f() {} f(); }")
	first := m.Body[0].(*BlockStmt)
	require.NotNil(t, first.Scope)
	assert.Equal(t, ScopeBlock, first.Scope.Kind)
	assert.Same(t, first, first.Scope.Node)
	inner := first.Body[1].(*ExprStmt).X.(*Ident)
	assert.Same(t, first.Scope.Lookup("a"), inner.Binding)
	assert.Equal(t, BindLet, inner.Binding.Kind)
	outer := m.Body[1].(*ExprStmt).X.(*Ident)
	assert.Nil(t, outer.Binding)
	assert.Nil(t, m.Body[2].(*BlockStmt).Scope)

	third := m.Body[3].(*BlockStmt)
	require.NotNil(t, third.Scope)
	require.Len(t, third.Scope.Funcs, 1)
	call := third.Body[1].(*ExprStmt).X.(*CallExpr).Callee.(*Ident)
	assert.Same(t, third.Scope.Lookup("f"), call.Binding)
	assert.Equal(t, BindFunction, call.Binding.Kind)
	assert.Same(t, third.Body[0].(*FuncDecl).Func.Name.Binding, call.Binding)
	assert.Equal(t, []*Scope{first.Scope, third.Scope, third.Scope.Funcs[0].Func.Scope}, []*Scope{m.Scope.Children[0], m.Scope.Children[1], third.Scope.Children[0]})
}

func TestScopeShadowing(t *testing.T) {
	m := parseModule(t, "let a = 1; function f(a) { return a; } a; { let a = 2; a; } const g = (a) => a;")
	f := functions(m)["f"]
	ret := f.Body.Body[0].(*ReturnStmt).Result.(*Ident)
	assert.Equal(t, BindParam, ret.Binding.Kind)
	assert.Same(t, f.Scope.Lookup("a"), ret.Binding)
	moduleA := m.Scope.Lookup("a")
	assert.False(t, moduleA.Captured)
	assert.Same(t, moduleA, m.Body[2].(*ExprStmt).X.(*Ident).Binding)
	block := m.Body[3].(*BlockStmt)
	assert.Same(t, block.Scope.Lookup("a"), block.Body[1].(*ExprStmt).X.(*Ident).Binding)
	g := functions(m)["g"]
	assert.Same(t, g.Scope.Lookup("a"), g.ExprBody.(*Ident).Binding)
}

func TestScopeCatchAndFor(t *testing.T) {
	m := parseModule(t, "try {} catch (e) { e; } try {} catch ({ message, ...rest }) { message; } for (let i = 0; i < 3; i++) { fns.push(() => i); } for (const k in o) k; for (x of y) x;")
	try1 := m.Body[0].(*TryStmt)
	require.NotNil(t, try1.CatchScope)
	assert.Equal(t, ScopeCatch, try1.CatchScope.Kind)
	e := try1.CatchScope.Lookup("e")
	assert.Equal(t, BindCatch, e.Kind)
	assert.Same(t, e, try1.Handler.Body[0].(*ExprStmt).X.(*Ident).Binding)
	assert.Same(t, e, try1.Param.(*Ident).Binding)
	assert.Nil(t, try1.Handler.Scope)

	try2 := m.Body[1].(*TryStmt)
	assert.Equal(t, 2, len(try2.CatchScope.Bindings))
	assert.NotNil(t, try2.CatchScope.Lookup("rest"))

	loop := m.Body[2].(*ForStmt)
	require.NotNil(t, loop.Scope)
	assert.Equal(t, ScopeFor, loop.Scope.Kind)
	i := loop.Scope.Lookup("i")
	assert.True(t, i.Captured)
	assert.False(t, i.NeedsTDZ)
	assert.Equal(t, BindLet, i.Kind)

	forIn := m.Body[3].(*ForInOfStmt)
	require.NotNil(t, forIn.Scope)
	assert.Equal(t, BindConst, forIn.Scope.Lookup("k").Kind)
	assert.Same(t, forIn.Scope.Lookup("k"), forIn.Body.(*ExprStmt).X.(*Ident).Binding)

	forOf := m.Body[4].(*ForInOfStmt)
	assert.Nil(t, forOf.Scope)
	assert.Nil(t, forOf.Left.(*Ident).Binding)
}

func TestScopeFunctionFlags(t *testing.T) {
	m, err := ParseModule("t.js", `
function a() { this; }
function b() { const inner = () => this; }
const c = () => this;
function d() { const nested = () => () => this; }
function e() { return 1; }
function f() { const g = function () { this; }; }
this;
`, Options{})
	require.NoError(t, err)
	fns := functions(m)
	assert.True(t, fns["a"].UsesThis)
	assert.True(t, fns["b"].UsesThis)
	assert.True(t, fns["inner"].UsesThis)
	assert.True(t, fns["c"].UsesThis)
	assert.True(t, fns["d"].UsesThis)
	assert.True(t, fns["nested"].UsesThis)
	assert.False(t, fns["e"].UsesThis)
	assert.False(t, fns["f"].UsesThis, "a nested non-arrow function owns its own this")
	assert.True(t, fns["g"].UsesThis)
	for _, fn := range fns {
		assert.True(t, fn.IsStrict)
		assert.False(t, fn.UsesArguments)
		assert.False(t, fn.HasDirectEval)
	}
}

// TestScopeArguments checks the arguments binding: declared in the scope of
// the innermost non-arrow function on first reference, shared (and captured)
// by nested arrows, absent from functions that never mention it, and an
// ordinary global reference at the top level.
func TestScopeArguments(t *testing.T) {
	m := parseModule(t, `
function a() { return arguments[0]; }
function b(x) { const g = () => () => arguments.length; return g; }
function c() { return function d() { return arguments; }; }
function e(p = arguments) { return p; }
const top = () => arguments;
function none() { return 1; }
`)
	fns := functions(m)
	a := fns["a"]
	require.NotNil(t, a.ArgumentsBinding())
	assert.Equal(t, BindArgs, a.ArgumentsBinding().Kind)
	assert.Same(t, a.Scope, a.ArgumentsBinding().Scope)
	assert.Same(t, a.ArgumentsBinding(), a.Scope.Lookup("arguments"))
	assert.False(t, a.ArgumentsBinding().Captured)
	assert.True(t, a.UsesArguments)

	b, g := fns["b"], fns["g"]
	require.NotNil(t, b.ArgumentsBinding())
	assert.True(t, b.ArgumentsBinding().Captured, "read from a nested arrow")
	assert.Nil(t, g.ArgumentsBinding(), "arrows have no arguments object of their own")
	assert.True(t, g.UsesArguments)
	inner := g.ExprBody.(*Function)
	assert.Same(t, b.ArgumentsBinding(), inner.ExprBody.(*MemberExpr).Object.(*Ident).Binding)

	c, d := fns["c"], fns["d"]
	assert.Nil(t, c.ArgumentsBinding(), "a nested ordinary function owns its arguments")
	assert.False(t, c.UsesArguments)
	require.NotNil(t, d.ArgumentsBinding())

	assert.NotNil(t, fns["e"].ArgumentsBinding(), "parameter initialisers see the arguments object")
	assert.Nil(t, fns["top"].ExprBody.(*Ident).Binding)
	assert.Nil(t, fns["none"].ArgumentsBinding())
	assert.Nil(t, m.Scope.Lookup("arguments"))
}

func TestScopeSelfBinding(t *testing.T) {
	m := parseModule(t, "const f = function g() { return g; }; const f2 = function g(g) { return g; }; const f3 = function g() { var g; return g; }; function h() { return h; }")
	g := m.Body[0].(*VarDecl).Decls[0].Init.(*Function)
	require.NotNil(t, g.SelfBinding)
	assert.Equal(t, BindFuncName, g.SelfBinding.Kind)
	assert.Same(t, g.Scope, g.SelfBinding.Scope)
	assert.Same(t, g.SelfBinding, g.Body.Body[0].(*ReturnStmt).Result.(*Ident).Binding)
	assert.Same(t, g.SelfBinding, g.Name.Binding)

	g2 := m.Body[1].(*VarDecl).Decls[0].Init.(*Function)
	assert.Nil(t, g2.SelfBinding)
	assert.Equal(t, BindParam, g2.Body.Body[0].(*ReturnStmt).Result.(*Ident).Binding.Kind)

	g3 := m.Body[2].(*VarDecl).Decls[0].Init.(*Function)
	assert.Nil(t, g3.SelfBinding)
	assert.Equal(t, BindVar, g3.Body.Body[1].(*ReturnStmt).Result.(*Ident).Binding.Kind)

	h := m.Body[3].(*FuncDecl).Func
	assert.Nil(t, h.SelfBinding)
	assert.Same(t, m.Scope.Lookup("h"), h.Body.Body[0].(*ReturnStmt).Result.(*Ident).Binding)
	assert.Same(t, m.Scope.Lookup("h"), h.Name.Binding)
}

func TestScopeExports(t *testing.T) {
	m := parseModule(t, "export const a = 1, { b } = o; export function f() {} export { a as c, f as \"d e\" }; let hidden; export var v; export let l = 1;")
	var names, locals []string
	for _, e := range m.Exports {
		names = append(names, e.Name)
		locals = append(locals, e.Local)
		assert.NotNil(t, e.Binding, e.Name)
		assert.True(t, e.Binding.Exported)
		assert.Same(t, m.Scope, e.Binding.Scope)
	}
	assert.Equal(t, []string{"a", "b", "f", "c", "d e", "v", "l"}, names)
	assert.Equal(t, []string{"a", "b", "f", "a", "f", "v", "l"}, locals)
	assert.Same(t, m.Scope.Lookup("a"), m.Export("c"))
	assert.Same(t, m.Scope.Lookup("f"), m.Export("d e"))
	assert.Nil(t, m.Export("hidden"))
	assert.False(t, m.Scope.Lookup("hidden").Exported)
	assert.Equal(t, BindConst, m.Export("a").Kind)
	assert.Equal(t, BindFunction, m.Export("f").Kind)
	assert.Equal(t, BindVar, m.Export("v").Kind)
	assert.Equal(t, BindLet, m.Export("l").Kind)

	m = parseModule(t, "export default function () {}")
	require.Len(t, m.Exports, 1)
	assert.Equal(t, "default", m.Exports[0].Name)
	assert.Equal(t, "*default*", m.Exports[0].Local)
	assert.Equal(t, BindFunction, m.Exports[0].Binding.Kind)
	require.Len(t, m.Scope.Funcs, 1)
	assert.Same(t, m.Body[0].(*ExportDefault).Decl.(*FuncDecl), m.Scope.Funcs[0])

	m = parseModule(t, "export default 1 + 2;")
	assert.Equal(t, "*default*", m.Exports[0].Local)
	assert.Equal(t, BindConst, m.Exports[0].Binding.Kind)
	assert.Same(t, m.Scope.Lookup("*default*"), m.Exports[0].Binding)

	m = parseModule(t, "export default function f() {} f();")
	assert.Equal(t, "f", m.Exports[0].Local)
	assert.Same(t, m.Scope.Lookup("f"), m.Exports[0].Binding)
	assert.Same(t, m.Exports[0].Binding, m.Body[1].(*ExprStmt).X.(*CallExpr).Callee.(*Ident).Binding)

	// Exports read a live binding, which an exported function may update.
	m = parseModule(t, "export let counter = 0; export function bump() { counter++; }")
	assert.True(t, m.Export("counter").Captured)
}

func TestScopeVarHoistingAcrossBlocks(t *testing.T) {
	m := parseModule(t, "function f() { if (a) { var v = 1; } for (var i = 0;;) {} return v + i; }")
	fn := m.Body[0].(*FuncDecl).Func
	v := fn.Scope.Lookup("v")
	require.NotNil(t, v)
	assert.Equal(t, BindVar, v.Kind)
	sum := fn.Body.Body[2].(*ReturnStmt).Result.(*BinaryExpr)
	assert.Same(t, v, sum.X.(*Ident).Binding)
	assert.Same(t, fn.Scope.Lookup("i"), sum.Y.(*Ident).Binding)
	assert.Nil(t, fn.Body.Body[0].(*IfStmt).Then.(*BlockStmt).Scope)
	assert.Nil(t, fn.Body.Body[1].(*ForStmt).Scope)
}

func TestScopeGlobals(t *testing.T) {
	m := parseModule(t, "foo; undefined; console.log(x); Object.keys(o);")
	Inspect(m, func(n Node) bool {
		if id, ok := n.(*Ident); ok {
			assert.Nil(t, id.Binding, id.Name)
		}
		return true
	})
}

func TestScopeDeclarationIdentsAreBound(t *testing.T) {
	m := parseModule(t, "const a = 1; let { b, c: [d] } = o; function f(p, { q } = {}, ...r) {}")
	assert.Same(t, m.Scope.Lookup("a"), m.Body[0].(*VarDecl).Decls[0].Target.(*Ident).Binding)
	pat := m.Body[1].(*VarDecl).Decls[0].Target.(*ObjectPattern)
	assert.Same(t, m.Scope.Lookup("b"), pat.Props[0].Value.(*Ident).Binding)
	assert.Same(t, m.Scope.Lookup("d"), pat.Props[1].Value.(*ArrayPattern).Elems[0].(*Ident).Binding)
	fn := m.Body[2].(*FuncDecl).Func
	assert.Same(t, m.Scope.Lookup("f"), fn.Name.Binding)
	assert.Same(t, fn.Scope.Lookup("p"), fn.Params[0].(*Ident).Binding)
	assert.Same(t, fn.Scope.Lookup("q"), fn.Params[1].(*AssignPattern).Target.(*ObjectPattern).Props[0].Value.(*Ident).Binding)
	assert.Same(t, fn.Scope.Lookup("r"), fn.Rest.(*Ident).Binding)
	for _, id := range BoundNames(fn.Params[1]) {
		assert.Equal(t, "q", id.Name)
	}
}

func TestScopeSwitch(t *testing.T) {
	m := parseModule(t, "switch (v) { case 1: let a = 1; a; break; case 2: const b = 2; b; default: a; }")
	sw := m.Body[0].(*SwitchStmt)
	require.NotNil(t, sw.Scope)
	assert.Equal(t, ScopeBlock, sw.Scope.Kind)
	assert.Same(t, sw, sw.Scope.Node)
	assert.True(t, sw.Scope.Lookup("a").NeedsTDZ)
	assert.True(t, sw.Scope.Lookup("b").NeedsTDZ)
	assert.Same(t, sw.Scope.Lookup("a"), sw.Cases[2].Body[0].(*ExprStmt).X.(*Ident).Binding)

	m = parseModule(t, "switch (v) { case 1: f(); }")
	assert.Nil(t, m.Body[0].(*SwitchStmt).Scope)
}

func TestScopeLookupLargeScope(t *testing.T) {
	src := ""
	for _, c := range "abcdefghijklmnopqrstuvwxyz" {
		src += "let " + string(c) + " = 1; "
	}
	m := parseModule(t, src+"z; a; m;")
	assert.Len(t, m.Scope.Bindings, 26)
	for i, c := range "abcdefghijklmnopqrstuvwxyz" {
		b := m.Scope.Lookup(string(c))
		require.NotNil(t, b)
		assert.Equal(t, i, b.Slot)
	}
	assert.Nil(t, m.Scope.Lookup("zz"))
	assert.Same(t, m.Scope.Lookup("m"), m.Body[28].(*ExprStmt).X.(*Ident).Binding)
	assert.Same(t, m.Scope.Lookup("m"), m.Scope.Resolve("m"))
}
