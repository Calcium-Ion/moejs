package syntax

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseScript(t *testing.T, src string) *Script {
	t.Helper()
	s, err := ParseScript("t.js", src, Options{})
	require.NoError(t, err)
	return s
}

// scriptFunctions collects every named function of a script.
func scriptFunctions(s *Script) map[string]*Function {
	out := map[string]*Function{}
	Inspect(s, func(n Node) bool {
		if fn, ok := n.(*Function); ok && fn.Name != nil {
			out[fn.Name.Name] = fn
		}
		return true
	})
	return out
}

// TestSloppyNoErrors lists sloppy-mode scripts that strict code rejects.
func TestSloppyNoErrors(t *testing.T) {
	for _, src := range []string{
		// with.
		"with (a) b;",
		"with (a) { var b = 1; function f() { return c; } }",
		// Legacy literals and escapes.
		"x = 010 + 08 + 09.5 + 0777;",
		"x = '\\1' + '\\01' + '\\377' + '\\8' + '\\9' + '\\0';",
		"function f() { x = 010; 'use strict'; }",
		// Non-strict reserved words as identifiers.
		"var let = 1; let; let = 2; let.x; let();",
		"var static, implements, package, private, protected, public, interface, yield;",
		"yield = 1; function yield() {} function g(yield) { return yield; }",
		"implements: ; static: for (;;) break static;",
		"for (let in o) ;",
		"for (let.x of []) ;",
		"for (let\n[x] of []) ;",
		"let\nx = 1;",
		"let\n[a] = b;",
		"do let\nwhile (0)",
		"if (a) let\n;",
		"x = { let, static, yield };",
		"function* g() { function f() { var yield; } }",
		"function* yield() {}",
		// Parameters.
		"function f(a, a) {} x = function (b, b) {}; function* g(c, c) {} async function h(d, d) {}",
		"function f(a, b, a) { return a; }",
		// eval and arguments.
		"eval = 1; arguments = 2; var eval; function arguments() {} eval++; [eval] = x;",
		"function f(eval, arguments) {} try {} catch (eval) {} let arguments2;",
		// delete.
		"delete x; delete (x); delete ((x));",
		// Function declarations as statements.
		"if (a) function f() {}",
		"if (a) function f() {} else function g() {}",
		"if (a) ; else function g() {}",
		"l: function f() {}",
		"a: b: function f() {}",
		"{ l: function f() {} }",
		"{ function f() {} function f() {} }",
		"switch (0) { case 1: function f() {} case 2: function f() {} }",
		"function f() { { function g() {} function g() {} } }",
		"try {} catch (e) { var e; for (var e of []) ; for (var e in {}) ; }",
		// Initializers in for-in heads.
		"for (var x = 1 in o) ;",
		"for (var x = a ? b : c in o) ;",
		"for (var f = () => a in b) ; for (var f = (a) => a in b) ; for (var f = async () => a in b) ;",
		// Function calls as assignment targets.
		"f() = 1; f() += 1; f()++; --f(); for (f() in o) ; for (f() of o) ;",
		"(f()) = 1; (a.b)() = 1; (a?.b)() = 1; async() = 1; new C().m() = 1;",
		// Legacy literals past a strict function or class, whose closing
		// brace restores sloppy mode before the next token is scanned.
		"function f() { 'use strict'; } x = 010;",
		"(function () { 'use strict'; }); '\\01';",
		"class C {} x = 08;",
		"x = { m() { 'use strict'; } }; x = '\\8';",
		// HTML-like comments.
		"x = 1 <!-- comment\n--> comment\ny = 2;",
		"--> comment at the start\nx;",
		"/* a\n */ --> comment after a multi-line comment\nx;",
		"x = a <!--b;",
	} {
		t.Run(src, func(t *testing.T) {
			_, err := ParseScript("t.js", src, Options{})
			assert.NoError(t, err)
		})
	}
}

// TestSloppyErrors checks that sloppy scripts still reject what sloppy
// code rejects, and that strict functions and directives apply strict
// rules, retroactively for the prologue.
func TestSloppyErrors(t *testing.T) {
	tests := []struct {
		src       string
		line, col int
		msg       string
	}{
		{"'use strict'; with (a) b;", 1, 15, "Strict mode code may not include a with statement"},
		{"'use strict'; x = 010;", 1, 19, "Octal literals are not allowed in strict mode."},
		{"'use strict'; x = 08;", 1, 19, "Decimals with leading zeros are not allowed in strict mode."},
		{"'use strict'; x = '\\01';", 1, 20, "Octal escape sequences are not allowed in strict mode."},
		{"'use strict'; x = '\\8';", 1, 20, "\\8 and \\9 are not allowed in strict mode."},
		{"'\\01'; 'use strict';", 1, 2, "Octal escape sequences are not allowed in strict mode."},
		{"function f() { 'use strict'; x = 010; }", 1, 34, "Octal literals are not allowed in strict mode."},
		{"function f() { '\\01'; 'use strict'; }", 1, 17, "Octal escape sequences are not allowed in strict mode."},
		// Tokens scanned before the directive switched the mode.
		{"'a\\\\01\\08'; 'use strict';", 1, 7, "Octal escape sequences are not allowed in strict mode."},
		{"'" + strings.Repeat("x", 70000) + "\\9'; 'use strict';", 1, 70002, "\\8 and \\9 are not allowed in strict mode."},
		{"'use strict'\n'\\1';", 2, 2, "Octal escape sequences are not allowed in strict mode."},
		{"'use strict'\n08;", 2, 1, "Decimals with leading zeros are not allowed in strict mode."},
		{"class C { m() { return 010; } }", 1, 24, "Octal literals are not allowed in strict mode."},
		{"class C extends (010) {}", 1, 18, "Octal literals are not allowed in strict mode."},
		{"'use strict'; async\n010;", 2, 1, "Octal literals are not allowed in strict mode."},
		{"'use strict'; x = y in 010;", 1, 24, "Octal literals are not allowed in strict mode."},
		// The first error of the token is reported.
		{"'use strict'; x = '\\01\\x';", 1, 20, "Octal escape sequences are not allowed in strict mode."},
		{"'use strict'; x = 010a;", 1, 19, "Octal literals are not allowed in strict mode."},
		{"function f(a, a) { 'use strict'; }", 1, 15, "Duplicate parameter name not allowed in this context"},
		{"function eval() { 'use strict'; }", 1, 10, "Unexpected eval or arguments in strict mode"},
		{"function f(arguments) { 'use strict'; }", 1, 12, "Unexpected eval or arguments in strict mode"},
		{"function static() { 'use strict'; }", 1, 10, "Unexpected strict mode reserved word"},
		{"function f() { 'use strict'; delete x; }", 1, 30, "Delete of an unqualified identifier in strict mode."},
		{"class C { m() { with (a) ; } }", 1, 17, "Strict mode code may not include a with statement"},
		{"class C { m(a, a) {} }", 1, 16, "Duplicate parameter name not allowed in this context"},
		{"function f(a = 1, a) {}", 1, 19, "Duplicate parameter name not allowed in this context"},
		{"function f({ a }, a) {}", 1, 19, "Duplicate parameter name not allowed in this context"},
		{"function f(a, ...a) {}", 1, 18, "Duplicate parameter name not allowed in this context"},
		{"(a, a) => 1;", 1, 5, "Duplicate parameter name not allowed in this context"},
		{"x = { m(a, a) {} };", 1, 12, "Duplicate parameter name not allowed in this context"},
		{"function* g() { var yield; }", 1, 21, "Unexpected strict mode reserved word"},
		{"let let = 1;", 1, 5, "let is disallowed as a lexically bound name"},
		{"let\nlet = 1;", 2, 1, "let is disallowed as a lexically bound name"},
		{"for (let let of []) ;", 1, 10, "let is disallowed as a lexically bound name"},
		{"if (a) let\n[b] = c;", 1, 8, "Lexical declaration cannot appear in a single-statement context"},
		{"while (a) function f() {}", 1, 11, "In non-strict mode code, functions can only be declared at top level, inside a block, or as the body of an if statement."},
		{"with (a) function f() {}", 1, 10, "In non-strict mode code, functions can only be declared at top level, inside a block, or as the body of an if statement."},
		{"if (a) l: function f() {}", 1, 11, "In non-strict mode code, functions can only be declared at top level, inside a block, or as the body of an if statement."},
		{"l: function* g() {}", 1, 4, "Generators can only be declared at the top level or inside a block."},
		{"l: async function f() {}", 1, 4, "Async functions can only be declared at the top level or inside a block."},
		{"if (a) async function f() {}", 1, 8, "Async functions can only be declared at the top level or inside a block."},
		{"{ function* f() {} function f() {} }", 1, 29, "Identifier 'f' has already been declared"},
		{"{ async function f() {} function f() {} }", 1, 34, "Identifier 'f' has already been declared"},
		{"{ function f() {} let f; }", 1, 23, "Identifier 'f' has already been declared"},
		{"let[0] = 1;", 1, 5, "Unexpected number"},
		{"x = function* yield() {};", 1, 15, "Unexpected strict mode reserved word"},
		{"function f() { 'use strict'; { function g() {} function g() {} } }", 1, 57, "Identifier 'g' has already been declared"},
		{"'use strict'; for (var x = 1 in o) ;", 1, 20, "for-in loop variable declaration may not have an initializer."},
		{"for (let x = 1 in o) ;", 1, 6, "for-in loop variable declaration may not have an initializer."},
		{"for (var [x] = 1 in o) ;", 1, 6, "for-in loop variable declaration may not have an initializer."},
		{"for (var x = 1 of o) ;", 1, 6, "for-of loop variable declaration may not have an initializer."},
		{"'use strict'; for (var f = () => a in b) ;", 1, 20, "for-in loop variable declaration may not have an initializer."},
		{"for (let f = () => a in b;;);", 1, 6, "for-in loop variable declaration may not have an initializer."},
		// An arrow's concise body in a for head excludes `in`.
		{"for (var f = () => a in b;;);", 1, 26, "Unexpected token ';'"},
		{"for (var f = (a, b) => a in b;;);", 1, 30, "Unexpected token ';'"},
		{"for (var f = async x => a in b;;);", 1, 31, "Unexpected token ';'"},
		{"for (var f = () => a in b in c;;);", 1, 31, "Unexpected token ';'"},
		{"for (var f = () => () => a in b;;);", 1, 32, "Unexpected token ';'"},
		{"for (var f = () => a ? b : c in d;;);", 1, 34, "Unexpected token ';'"},
		{"for (var f = () => a = b in c;;);", 1, 30, "Unexpected token ';'"},
		{"'use strict'; f() = 1;", 1, 15, "Invalid left-hand side in assignment"},
		{"function g() { 'use strict'; f()++; }", 1, 30, "Invalid left-hand side expression in postfix operation"},
		{"class C { m() { for (f() in o) ; } }", 1, 22, "Invalid left-hand side in for-in loop"},
		{"f() &&= 1;", 1, 1, "Invalid left-hand side in assignment"},
		{"f() ??= 1;", 1, 1, "Invalid left-hand side in assignment"},
		{"f()`` = 1;", 1, 1, "Invalid left-hand side in assignment"},
		{"a?.b() = 1;", 1, 1, "Invalid left-hand side in assignment"},
		{"class C extends B { constructor() { super() = 1; } }", 1, 37, "Invalid left-hand side in assignment"},
		{"[f()] = [];", 1, 2, "Invalid destructuring assignment target"},
		{"({ a: f() = 1 } = {});", 1, 7, "Invalid destructuring assignment target"},
		{"(f() = 1) => 0;", 1, 2, "Invalid destructuring assignment target"},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			_, err := ParseScript("t.js", tt.src, Options{})
			require.Error(t, err)
			var se *Error
			require.ErrorAs(t, err, &se)
			assert.Equal(t, tt.msg, se.Message())
			assert.Equal(t, [2]int{tt.line, tt.col}, [2]int{se.Line, se.Col}, "position")
		})
	}
	// HTML-like comments, for-in initializers and call targets are
	// script-only.
	for _, src := range []string{"<!-- comment\nx;", "for (var x = 1 in o) ;", "f() = 1;"} {
		_, err := ParseModule("t.js", src, Options{})
		assert.Error(t, err, src)
	}
}

func TestSloppyStrictness(t *testing.T) {
	tests := []struct {
		src    string
		strict bool
		fns    map[string]bool // function name -> IsStrict
	}{
		{"function f() {}", false, map[string]bool{"f": false}},
		{"'use strict'; function f() {}", true, map[string]bool{"f": true}},
		{"'a'; 'use strict'; function f() {}", true, map[string]bool{"f": true}},
		{"x; 'use strict'; function f() {}", false, map[string]bool{"f": false}},
		{"'use\\x20strict'; function f() {}", false, map[string]bool{"f": false}},
		{"function f() { 'use strict'; function g() {} } function h() {}", false, map[string]bool{"f": true, "g": true, "h": false}},
		{"class C { m() { function g() {} } } function h() {}", false, map[string]bool{"m": true, "g": true, "h": false}},
		{"x = function k() { return () => function j() {}; };", false, map[string]bool{"k": false, "j": false}},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			s := parseScript(t, tt.src)
			assert.Equal(t, tt.strict, s.Strict)
			fns := scriptFunctions(s)
			for name, strict := range tt.fns {
				require.Contains(t, fns, name)
				assert.Equal(t, strict, fns[name].IsStrict, name)
			}
		})
	}
}

func TestSloppyWithScope(t *testing.T) {
	s := parseScript(t, "var v; with (o) { v; g; function f() { return h; } } w;")
	var ws *WithStmt
	Inspect(s, func(n Node) bool {
		if w, ok := n.(*WithStmt); ok {
			ws = w
		}
		return true
	})
	require.NotNil(t, ws)
	require.NotNil(t, ws.Scope)
	assert.Equal(t, ScopeWith, ws.Scope.Kind)
	assert.Same(t, s.Scope, ws.Scope.Parent)
	require.Len(t, ws.Scope.Bindings, 1)
	assert.Equal(t, BindHidden, ws.Scope.Bindings[0].Kind)
	assert.True(t, ws.Scope.Bindings[0].Captured, "f reads h through the with object")
	// Names keep resolving statically; the with scope declares none.
	assert.Same(t, s.Scope.Lookup("v"), ws.Body.(*BlockStmt).Body[0].(*ExprStmt).X.(*Ident).Binding)

	s = parseScript(t, "with (o) { x; } function f() { with (p) { let y; () => y; } }")
	Inspect(s, func(n Node) bool {
		if w, ok := n.(*WithStmt); ok {
			assert.False(t, w.Scope.Bindings[0].Captured, "no function reads a name through it")
		}
		return true
	})
}

func TestSloppyAnnexBFunctions(t *testing.T) {
	annexB := func(s *Script) map[string]bool {
		out := map[string]bool{}
		Inspect(s, func(n Node) bool {
			if fd, ok := n.(*FuncDecl); ok && fd.Func.Name.Binding != nil && fd.Func.Name.Binding.Scope.Kind == ScopeBlock {
				out[fd.Func.Name.Name] = fd.Func.Name.Binding.AnnexB
			}
			return true
		})
		return out
	}
	tests := []struct {
		src  string
		want map[string]bool
	}{
		{"{ function f() {} } if (a) function g() {} switch (0) { case 0: function h() {} }", map[string]bool{"f": true, "g": true, "h": true}},
		{"let f; { function f() {} }", map[string]bool{"f": false}},
		{"{ let f; { function f() {} } }", map[string]bool{"f": false}},
		{"for (let f;;) { function f() {} }", map[string]bool{"f": false}},
		{"for (const f of []) { function f() {} }", map[string]bool{"f": false}},
		{"try {} catch (f) { { function f() {} } }", map[string]bool{"f": true}},
		{"try {} catch ([f]) { { function f() {} } }", map[string]bool{"f": false}},
		{"switch (0) { case 0: let f; case 1: { function f() {} } }", map[string]bool{"f": false}},
		{"{ function* f() {} async function g() {} }", map[string]bool{"f": false, "g": false}},
		{"{ l: function f() {} }", map[string]bool{"f": false}},
		{"'use strict'; { function f() {} }", map[string]bool{"f": false}},
		{"function o(f) { { function f() {} } }", map[string]bool{"f": false}},
		{"function o() { let f; { function f() {} } }", map[string]bool{"f": false}},
		{"function o() { { function f() {} } { function arguments() {} } }", map[string]bool{"f": true, "arguments": false}},
		{"function o() { 'use strict'; { function f() {} } }", map[string]bool{"f": false}},
		{"function o(a = 1) { { function f() {} } }", map[string]bool{"f": true}},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			assert.Equal(t, tt.want, annexB(parseScript(t, tt.src)))
		})
	}

	// The var binding exists from the start of the function or script.
	s := parseScript(t, "function o() { f; { function f() {} } } g; { function g() {} } var h; { function h() {} }")
	o := scriptFunctions(s)["o"]
	require.NotNil(t, o.Scope.Lookup("f"))
	assert.Equal(t, BindVar, o.Scope.Lookup("f").Kind)
	assert.False(t, o.Scope.Lookup("f").AnnexB)
	require.NotNil(t, s.Scope.Lookup("g"))
	assert.True(t, s.Scope.Lookup("g").AnnexB, "a script var only Annex B creates")
	assert.False(t, s.Scope.Lookup("h").AnnexB)
}

func TestSloppyArguments(t *testing.T) {
	s := parseScript(t, `
function plain(a, b) { return arguments; }
function unused(a) { return a; }
function param(arguments) { return arguments; }
function fn() { function arguments() {} return arguments; }
function lex() { let arguments = 1; return arguments; }
function vr() { var arguments; return arguments; }
function block() { { let arguments; arguments; } return arguments; }
function nonSimple(a = 1) { return arguments; }
function strict(a) { 'use strict'; return arguments; }
function body(a = arguments) { var arguments; return arguments; }
`)
	fns := scriptFunctions(s)

	plain := fns["plain"]
	require.NotNil(t, plain.ArgumentsBinding())
	assert.True(t, plain.Scope.Lookup("a").Captured, "mapped onto the arguments object")
	assert.True(t, plain.Scope.Lookup("b").Captured)
	assert.False(t, fns["unused"].Scope.Lookup("a").Captured)

	assert.Nil(t, fns["param"].ArgumentsBinding(), "a parameter named arguments")
	assert.Equal(t, BindParam, fns["param"].Scope.Lookup("arguments").Kind)
	assert.Nil(t, fns["fn"].ArgumentsBinding(), "a function named arguments")
	assert.Nil(t, fns["lex"].ArgumentsBinding(), "a lexical named arguments")

	vr := fns["vr"]
	require.NotNil(t, vr.ArgumentsBinding(), "a var named arguments is the object")
	assert.Same(t, vr.Scope.Lookup("arguments"), vr.ArgumentsBinding())

	block := fns["block"]
	require.NotNil(t, block.ArgumentsBinding(), "a block binding only shadows it inside the block")

	nonSimple := fns["nonSimple"]
	require.NotNil(t, nonSimple.ArgumentsBinding())
	assert.False(t, nonSimple.Scope.Lookup("a").Captured, "non-simple parameters are not mapped")
	strict := fns["strict"]
	require.NotNil(t, strict.ArgumentsBinding())
	assert.False(t, strict.Scope.Lookup("a").Captured, "strict functions are not mapped")

	body := fns["body"]
	require.NotNil(t, body.ArgumentsBinding())
	require.NotNil(t, body.Body.Scope, "the body var is kept apart")
	assert.Equal(t, BindVar, body.Body.Scope.Lookup("arguments").Kind)
}
