package syntax

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAsyncErrors is the early-error table for async functions, async
// generators, await and for await, in modules unless script is set.
func TestAsyncErrors(t *testing.T) {
	const onlyAsync = "await is only valid in async functions and the top level bodies of modules"
	const inParams = "Illegal await-expression in formal parameters of async function"
	tests := []struct {
		script    bool
		src       string
		line, col int
		msg       string
	}{
		// await outside an async function body.
		{false, "function f() { await x; }", 1, 16, onlyAsync},
		{false, "function* g() { await x; }", 1, 17, onlyAsync},
		{false, "() => await x;", 1, 7, onlyAsync},
		{false, "(a = await x) => 1;", 1, 6, onlyAsync},
		{false, "function f(a = await x) {}", 1, 16, onlyAsync},
		{false, "function f() { for await (const x of y) ; }", 1, 16, onlyAsync},
		{false, "x = { m() { await x; } };", 1, 13, onlyAsync},
		{false, "async function f() { function g() { await x; } }", 1, 37, onlyAsync},
		{false, "async function f() { (a = await 1) => {}; }", 1, 27, onlyAsync},
		{true, "async function f() { () => await 1; }", 1, 34, "Unexpected number"},
		{true, "function f() { await x; }", 1, 22, "Unexpected identifier 'x'"},
		{true, "function f() { for await (x of y) ; }", 1, 20, "Unexpected identifier 'await'"},
		// await in the parameters of an async function.
		{false, "async function f(a = await x) {}", 1, 22, inParams},
		{false, "x = async function (a = [await x]) {};", 1, 26, inParams},
		{false, "x = { async m({ a = await x }) {} };", 1, 21, inParams},
		{false, "class A { async m(a = await x) {} }", 1, 23, inParams},
		{false, "async function* g(a = await x) {}", 1, 23, inParams},
		{false, "x = async (a = await 1) => 1;", 1, 16, inParams},
		{false, "async function f() { async (a = await 1) => 1; }", 1, 33, inParams},
		{false, "async (a = class { [await 1]() {} }) => 1;", 1, 21, inParams},
		{false, "x = async (a = class extends (await x) {}) => 1;", 1, 31, inParams},
		{true, "async function f() { async (a = class { [await 1]() {} }) => 1; }", 1, 42, inParams},
		{true, "async function f() { async (a = class extends (await 1) {}) => 1; }", 1, 48, inParams},
		// await is reserved inside async functions, and names them.
		{true, "async function f() { var await; }", 1, 26, "Unexpected reserved word"},
		{true, "async function f(await) {}", 1, 18, "Unexpected reserved word"},
		{true, "(async function await() {});", 1, 17, "Unexpected reserved word"},
		{true, "async function f() { await; }", 1, 27, "Unexpected token ';'"},
		{true, "async function f() { new await 1; }", 1, 26, "Unexpected reserved word"},
		// Async generators.
		{false, "async function* g(a = yield) {}", 1, 23, "Yield expression not allowed in formal parameter"},
		{true, "(async function* yield() {});", 1, 18, "Unexpected strict mode reserved word"},
		{true, "async function* g() { var yield; }", 1, 27, "Unexpected strict mode reserved word"},
		{true, "function* g() { async (a = yield) => 1; }", 1, 28, "Unexpected strict mode reserved word"},
		// Placement and line terminators.
		{true, "label: async function f() {}", 1, 8, "Async functions can only be declared at the top level or inside a block."},
		{true, "if (1) async function f() {}", 1, 8, "Async functions can only be declared at the top level or inside a block."},
		{true, "x = async () \n=> 1;", 2, 1, "Unexpected token '=>'"},
		{true, "x = async\n(a) => 1;", 2, 5, "Unexpected token '=>'"},
		{true, "x = async /*\n*/ (a) => 1;", 2, 8, "Unexpected token '=>'"},
		{true, "x = async x\n=> x;", 2, 1, "Unexpected token '=>'"},
		{true, "new async () => 1;", 1, 14, "Unexpected token '=>'"},
		// Method forms.
		{true, "class A { async constructor() {} }", 1, 17, "Class constructor may not be an async method"},
		{true, "class A { async *constructor() {} }", 1, 18, "Class constructor may not be an async method"},
		{true, "x = { async get m() {} };", 1, 17, "Unexpected identifier 'm'"},
		{true, "class A { async static m() {} }", 1, 24, "Unexpected identifier 'm'"},
		// Parameters.
		{true, "async function f(a = 1) { 'use strict'; }", 1, 27, "Illegal 'use strict' directive in function with non-simple parameter list"},
		{true, "x = async (a, a) => 1;", 1, 15, "Duplicate parameter name not allowed in this context"},
		{true, "async function f(a, a) {}", 1, 21, "Duplicate parameter name not allowed in this context"},
		{true, "x = async (...a,) => 1;", 1, 12, "Rest parameter must be last formal parameter"},
		{true, "x = async (...a = []) => 1;", 1, 12, "Rest parameter may not have a default initializer"},
		{true, "x = async (a = await => 1) => 1;", 1, 16, "Unexpected reserved word"},
		{true, "x = async (a = (await) => 1) => 1;", 1, 17, "Unexpected reserved word"},
		{true, "x = async (a = (...await) => 1) => 1;", 1, 20, "Unexpected reserved word"},
		{true, "var await; x = async (a = class { [await]() {} }) => 1;", 1, 36, "Unexpected reserved word"},
		{true, "var await; x = async (a = class extends await {}) => 1;", 1, 41, "Unexpected reserved word"},
		{true, "var await; x = async (a = class await {}) => 1;", 1, 33, "Unexpected reserved word"},
		{true, "var await; x = async (a = class { static [await] = 1 }) => 1;", 1, 43, "Unexpected reserved word"},
		// for await.
		{true, "async function f() { for (async of x) ; }", 1, 36, "Unexpected identifier 'x'"},
		{true, "async function f() { for await (x in y) ; }", 1, 22, "for await is only valid with of"},
		{true, "async function f() { for await (let x = 1;;) ; }", 1, 22, "for await is only valid with of"},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			var err error
			if tt.script {
				_, err = ParseScript("t.js", tt.src, Options{})
			} else {
				_, err = ParseModule("t.js", tt.src, Options{})
			}
			require.Error(t, err)
			var se *Error
			require.ErrorAs(t, err, &se)
			assert.Equal(t, tt.msg, se.Message())
			assert.Equal(t, [2]int{tt.line, tt.col}, [2]int{se.Line, se.Col}, "position")
		})
	}
}

// TestAsyncNoErrors lists valid async forms, in scripts and modules.
func TestAsyncNoErrors(t *testing.T) {
	srcs := []string{
		"async function f(a, b = 1, ...c) { await a; await await b; return await -1 + (await 2) ** 2; }",
		"x = async function () {}; y = async function f() { await f; }; z = async () => 1; w = async a => await a; v = async (a, { b }) => { await b; };",
		"x = { async m() { super.m(); await 1; }, async *g() { yield await 1; yield* y; }, async [k]() {}, async 'q'() {}, async 0() {} };",
		"class A { async m() {} static async s() {} async *g() {} static async *[k]() {} async #p() {} }",
		"async function* g() { yield; yield 1; yield\nawait 1; for await (const x of y) yield x; }",
		"async function f() { for await (x of y) ; for await (let x of y) ; for await ([a] of y) ; for await (async of y) ; for await (x.y of z) ; }",
		"async function f() { delete await 1; typeof await 1; void await 1; !await 1; }",
		"async function f(a = async () => await 1) {} x = async (a = async () => await 1) => 1;",
		"async function f() { class A { [await 0]() {} } }",
		"async function f() { return () => arguments; } x = async () => arguments;",
		"async\nfunction f() {}",
		"for (async of => {}; false;) ;",
		"async function f() { 'use strict'; }",
		"x = async (...a) => 1; y = async (a, ...[b]) => 1; f(...a,); async(...a,);",
		"x = async (a = () => 1, b = (c) => c) => 1;",
		"x = async (a = class { async m(b = async () => 1) { await 1; } static async [k]() {} }) => 1;",
	}
	for _, src := range srcs {
		t.Run(src, func(t *testing.T) {
			_, err := ParseScript("t.js", src, Options{})
			assert.NoError(t, err, "script")
			_, err = ParseModule("t.js", src, Options{})
			assert.NoError(t, err, "module")
		})
	}
	// In scripts outside async functions await stays an identifier.
	for _, src := range []string{
		"async function await() {}",
		"async function f() { function g(await) {} (function await() {}); }",
		"x = async (a = class { m(b = await) { return await; } x = await; static { var y; } [b.await]() {} await() {} static await = 1; #await; }) => 1;",
		"x = async (a = function await() {}) => 1;",
	} {
		t.Run(src, func(t *testing.T) {
			_, err := ParseScript("t.js", src, Options{})
			assert.NoError(t, err)
		})
	}
}

// TestModuleAsync checks that only an await outside every function makes a
// module evaluate asynchronously.
func TestModuleAsync(t *testing.T) {
	for src, want := range map[string]bool{
		"await x;":                              true,
		"export const x = await y;":             true,
		"if (a) { for await (const x of y) ; }": true,
		"try {} finally { f(await x); }":        true,
		"async function f() { await x; }":       false,
		"x = async () => await y;":              false,
		"class A { async m() { await x; } }":    false,
		"x = 1;":                                false,
	} {
		m, err := ParseModule("t.js", src, Options{})
		require.NoError(t, err, src)
		assert.Equal(t, want, m.Async, src)
	}
}
