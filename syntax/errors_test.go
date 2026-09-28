package syntax

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEarlyErrors is the strict-mode early-error table: each source must
// fail at exactly the given line:col with the given message.
func TestEarlyErrors(t *testing.T) {
	tests := []struct {
		src       string
		line, col int
		msg       string
	}{
		// Redeclarations.
		{"let a; let a;", 1, 12, "Identifier 'a' has already been declared"},
		{"let a, a;", 1, 8, "Identifier 'a' has already been declared"},
		{"const a = 1; var a;", 1, 18, "Identifier 'a' has already been declared"},
		{"var a; let a;", 1, 12, "Identifier 'a' has already been declared"},
		{"function f(a) { let a; }", 1, 21, "Identifier 'a' has already been declared"},
		{"function f(a = 1) { let a; }", 1, 25, "Identifier 'a' has already been declared"},
		{"(a = 1) => { const a = 2; }", 1, 20, "Identifier 'a' has already been declared"},
		{"function f() { let x; var x; }", 1, 27, "Identifier 'x' has already been declared"},
		{"let x; { var x; }", 1, 14, "Identifier 'x' has already been declared"},
		{"for (let i;;) { var i; }", 1, 10, "Identifier 'i' has already been declared"},
		{"try {} catch (e) { let e; }", 1, 24, "Identifier 'e' has already been declared"},
		{"try {} catch ([e]) { var e; }", 1, 20, "Identifier 'e' has already been declared"},
		{"function f() {} function f() {}", 1, 26, "Identifier 'f' has already been declared"},
		{"var f; function f() {}", 1, 17, "Identifier 'f' has already been declared"},
		{"{ function f() {} function f() {} }", 1, 28, "Identifier 'f' has already been declared"},
		{"{ function f() {} var f; }", 1, 12, "Identifier 'f' has already been declared"},
		{"{ var f; function f() {} }", 1, 19, "Identifier 'f' has already been declared"},
		{"{ function f() {} { var f; } }", 1, 12, "Identifier 'f' has already been declared"},
		{"switch (0) { case 1: function f() {} default: var f; }", 1, 31, "Identifier 'f' has already been declared"},
		{"try {} catch (e) { var f; function f() {} }", 1, 36, "Identifier 'f' has already been declared"},
		{"class A {} let A;", 1, 16, "Identifier 'A' has already been declared"},
		// Binding names.
		{"let let = 1;", 1, 5, "let is disallowed as a lexically bound name"},
		{"const [let] = a;", 1, 8, "let is disallowed as a lexically bound name"},
		{"eval = 1;", 1, 1, "Unexpected eval or arguments in strict mode"},
		{"arguments = 1;", 1, 1, "Unexpected eval or arguments in strict mode"},
		{"let eval;", 1, 5, "Unexpected eval or arguments in strict mode"},
		{"function arguments() {}", 1, 10, "Unexpected eval or arguments in strict mode"},
		{"function f(eval) {}", 1, 12, "Unexpected eval or arguments in strict mode"},
		{"[eval] = a;", 1, 2, "Unexpected eval or arguments in strict mode"},
		{"({ arguments } = a);", 1, 4, "Unexpected eval or arguments in strict mode"},
		{"eval++;", 1, 1, "Unexpected eval or arguments in strict mode"},
		{"let await = 1;", 1, 5, "Unexpected reserved word"},
		{"let yield = 1;", 1, 5, "Unexpected strict mode reserved word"},
		{"var implements;", 1, 5, "Unexpected strict mode reserved word"},
		{"var static = 1;", 1, 5, "Unexpected strict mode reserved word"},
		{"var class = 1;", 1, 5, "Unexpected token 'class'"},
		{"var enum;", 1, 5, "Unexpected token 'enum'"},
		{"x = { if };", 1, 7, "Unexpected token 'if'"},
		{"x = { yield };", 1, 7, "Unexpected strict mode reserved word"},
		{"function f(a, a) {}", 1, 15, "Duplicate parameter name not allowed in this context"},
		{"(a, a) => 1;", 1, 5, "Duplicate parameter name not allowed in this context"},
		{"function f([a], { a }) {}", 1, 19, "Duplicate parameter name not allowed in this context"},
		// Statements.
		{"delete x;", 1, 1, "Delete of an unqualified identifier in strict mode."},
		{"delete (x);", 1, 1, "Delete of an unqualified identifier in strict mode."},
		{"with (a) {}", 1, 1, "Strict mode code may not include a with statement"},
		{"return 1;", 1, 1, "Illegal return statement"},
		{"break;", 1, 1, "Illegal break statement"},
		{"while (1) { function g() { break; } }", 1, 28, "Illegal break statement"},
		{"continue;", 1, 1, "Illegal continue statement: no surrounding iteration statement"},
		{"switch (a) { case 1: continue; }", 1, 22, "Illegal continue statement: no surrounding iteration statement"},
		{"l: { continue l; }", 1, 15, "Illegal continue statement: 'l' does not denote an iteration statement"},
		{"while (1) { break foo; }", 1, 19, "Undefined label 'foo'"},
		{"a: while (1) { function g() { continue a; } }", 1, 40, "Undefined label 'a'"},
		{"a: a: ;", 1, 4, "Label 'a' has already been declared"},
		{"a: { b: { a: ; } }", 1, 11, "Label 'a' has already been declared"},
		{"if (a) function f() {}", 1, 8, "In strict mode code, functions can only be declared at top level or inside a block."},
		{"l: function f() {}", 1, 4, "In strict mode code, functions can only be declared at top level or inside a block."},
		{"while (a) function f() {}", 1, 11, "In strict mode code, functions can only be declared at top level or inside a block."},
		{"if (a) let x = 1;", 1, 8, "Lexical declaration cannot appear in a single-statement context"},
		{"if (a) const x = 1;", 1, 8, "Lexical declaration cannot appear in a single-statement context"},
		{"if (a) class A {}", 1, 8, "Unexpected token 'class'"},
		{"switch (a) { default: default: }", 1, 23, "More than one default clause in switch statement"},
		{"try {}", 1, 7, "Missing catch or finally after try"},
		{"const x;", 1, 7, "Missing initializer in const declaration"},
		{"const [a] = 1, b;", 1, 16, "Missing initializer in const declaration"},
		{"let {a};", 1, 5, "Missing initializer in destructuring declaration"},
		{"for (const x;;) {}", 1, 12, "Missing initializer in const declaration"},
		{"for (const x = 1 of y) {}", 1, 6, "for-of loop variable declaration may not have an initializer."},
		{"for (var x = 1 in y) {}", 1, 6, "for-in loop variable declaration may not have an initializer."},
		{"for (let x, y of z) {}", 1, 6, "Invalid left-hand side in for-of loop: Must have a single binding."},
		{"for (let x of y, z) {}", 1, 16, "Unexpected token ','"},
		{"for (a + b of c) {}", 1, 6, "Invalid left-hand side in for-of loop"},
		{"for await (x in y) {}", 1, 1, "for await is only valid with of"},
		{"for await (x;;) {}", 1, 1, "for await is only valid with of"},
		{"throw\nerr;", 2, 1, "Illegal newline after throw"},
		{"new.target;", 1, 1, "new.target expression is not allowed here"},
		{"const f = () => new.target;", 1, 17, "new.target expression is not allowed here"},
		{"super;", 1, 1, "'super' keyword unexpected here"},
		{"new import(\"x\");", 1, 5, "Cannot use new with import"},
		{"import();", 1, 8, "Unexpected token ')'"},
		{"import(...a);", 1, 8, "Unexpected token '...'"},
		{"import(a, b, c);", 1, 14, "import() takes at most two arguments"},
		{"import(a, b, c,);", 1, 14, "import() takes at most two arguments"},
		{"typeof import;", 1, 14, "Unexpected token ';'"},
		{"import(\"x\") = 1;", 1, 1, "Invalid left-hand side in assignment"},
		{"import.meta = 1;", 1, 1, "Invalid left-hand side in assignment"},
		{"import.meta++;", 1, 1, "Invalid left-hand side expression in postfix operation"},
		{"import.metb;", 1, 8, "Unexpected identifier 'metb'"},
		// Expressions.
		{"a ?? b || c;", 1, 1, "Unexpected token '||'"},
		{"a || b ?? c;", 1, 1, "Unexpected token '??'"},
		{"x = 1 ?? 2 && 3;", 1, 10, "Unexpected token '??'"},
		{"-a ** b;", 1, 1, "Unary operator used immediately before exponentiation expression. Parenthesis must be used to disambiguate operator precedence"},
		{"x = -(-a) ** 2;", 1, 5, "Unary operator used immediately before exponentiation expression. Parenthesis must be used to disambiguate operator precedence"},
		{"typeof a ** 2;", 1, 1, "Unary operator used immediately before exponentiation expression. Parenthesis must be used to disambiguate operator precedence"},
		{"await a ** 2;", 1, 1, "Unary operator used immediately before exponentiation expression. Parenthesis must be used to disambiguate operator precedence"},
		{"x = 2 ** await a ** 2;", 1, 10, "Unary operator used immediately before exponentiation expression. Parenthesis must be used to disambiguate operator precedence"},
		{"async function f() { 1 + await a ** 2; }", 1, 26, "Unary operator used immediately before exponentiation expression. Parenthesis must be used to disambiguate operator precedence"},
		{"a?.b = 1;", 1, 1, "Invalid left-hand side in assignment"},
		{"a?.b++;", 1, 1, "Invalid left-hand side expression in postfix operation"},
		{"++a?.b;", 1, 3, "Invalid left-hand side expression in prefix operation"},
		{"new a?.b();", 1, 6, "Invalid optional chain from new expression"},
		{"a?.`t`;", 1, 4, "Invalid tagged template on optional chain"},
		{"a?.b`t`;", 1, 5, "Invalid tagged template on optional chain"},
		{"x = `\\01`;", 1, 6, "Octal escape sequences are not allowed in template strings."},
		{"x = `a${b}\\9`;", 1, 11, "\\8 and \\9 are not allowed in template strings."},
		{"x = `\\u{110000}`;", 1, 6, "Undefined Unicode code-point"},
		{"x = `a${`\\xg`}`;", 1, 10, "Invalid hexadecimal escape sequence"},
		{"var `\\u`;", 1, 6, "Invalid Unicode escape sequence"},
		{"x = tag`${`\\1`}`;", 1, 12, "Octal escape sequences are not allowed in template strings."},
		{"1 = 2;", 1, 1, "Invalid left-hand side in assignment"},
		{"(a, b) = 1;", 1, 1, "Invalid left-hand side in assignment"},
		{"a + b = 1;", 1, 1, "Invalid left-hand side in assignment"},
		{"f() = 1;", 1, 1, "Invalid left-hand side in assignment"},
		{"({a}) = 1;", 1, 2, "Invalid left-hand side in assignment"},
		{"[a] += 1;", 1, 1, "Invalid left-hand side in assignment"},
		{"({a = 1});", 1, 3, "Invalid shorthand property initializer"},
		{"x = { a = 1 }.a;", 1, 7, "Invalid shorthand property initializer"},
		{"f({ a = 1 });", 1, 5, "Invalid shorthand property initializer"},
		{"({ __proto__: 1, __proto__: 2 });", 1, 18, "Duplicate __proto__ fields are not allowed in object literals"},
		{"({ __proto__: 1, \"__proto__\": 2 });", 1, 18, "Duplicate __proto__ fields are not allowed in object literals"},
		{"({ a = { __proto__: 1, __proto__: 2 } } = c);", 1, 24, "Duplicate __proto__ fields are not allowed in object literals"},
		{"f({ __proto__: 1, __proto__: 2 });", 1, 19, "Duplicate __proto__ fields are not allowed in object literals"},
		{"({ get x(a) {} });", 1, 9, "Getter must not have any formal parameters."},
		{"({ get x(...a) {} });", 1, 9, "Getter must not have any formal parameters."},
		{"({ set x() {} });", 1, 9, "Setter must have exactly one formal parameter."},
		{"({ set x(a, b) {} });", 1, 9, "Setter must have exactly one formal parameter."},
		{"({ set x(...a) {} });", 1, 13, "Setter function argument must not be a rest parameter"},
		{"({ set x(a) { let a; } });", 1, 19, "Identifier 'a' has already been declared"},
		{"function f(a = 1) { \"use strict\"; }", 1, 21, "Illegal 'use strict' directive in function with non-simple parameter list"},
		{"function f(...a) { \"use strict\"; }", 1, 20, "Illegal 'use strict' directive in function with non-simple parameter list"},
		{"(a, [b]) => { 'use strict'; };", 1, 15, "Illegal 'use strict' directive in function with non-simple parameter list"},
		{"x = { m({ a }) { \"a\"; \"use strict\"; } };", 1, 23, "Illegal 'use strict' directive in function with non-simple parameter list"},
		{"function f(...a, b) {}", 1, 16, "Rest parameter must be last formal parameter"},
		{"function f(...a = []) {}", 1, 17, "Rest parameter may not have a default initializer"},
		{"(...a, b) => 1;", 1, 6, "Rest parameter must be last formal parameter"},
		{"[...a,] = b;", 1, 2, "Rest element must be last element"},
		{"[...a, b] = c;", 1, 2, "Rest element must be last element"},
		{"({...a, b} = c);", 1, 3, "Rest element must be last element"},
		{"({...a,} = c);", 1, 3, "Rest element must be last element"},
		{"for ({...a,} of c) ;", 1, 7, "Rest element must be last element"},
		{"let {...a,} = c;", 1, 10, "Rest element must be last element"},
		{"[...a = 1] = b;", 1, 2, "Rest element may not have a default initializer"},
		{"let [...a, b] = c;", 1, 10, "Rest element must be last element"},
		{"let {...a, b} = c;", 1, 10, "Rest element must be last element"},
		{"(a + b) => 1;", 1, 2, "Malformed arrow function parameter list"},
		{"((a)) => 1;", 1, 3, "Invalid destructuring assignment target"},
		{"(x, [(a)]) => 1;", 1, 7, "Invalid destructuring assignment target"},
		{"({ a: (b) = 1 }) => 1;", 1, 8, "Invalid destructuring assignment target"},
		{"((a) = 1) => 1;", 1, 3, "Invalid destructuring assignment target"},
		{"async ((a)) => 1;", 1, 9, "Invalid destructuring assignment target"},
		{"[(a = 1)] = b;", 1, 3, "Invalid destructuring assignment target"},
		{"({ a: (b = 1) } = c);", 1, 8, "Invalid destructuring assignment target"},
		{"([a.b]) => 1;", 1, 3, "Invalid destructuring assignment target"},
		{"(a, ...b,) => 1;", 1, 9, "Rest parameter must be last formal parameter"},
		// A parenthesized arrow function is an AssignmentExpression, never
		// an operand.
		{"x in () => {};", 1, 9, "Unexpected token '=>'"},
		{"!() => 1;", 1, 5, "Unexpected token '=>'"},
		{"typeof (a) => a;", 1, 12, "Unexpected token '=>'"},
		{"x + (a) => a;", 1, 9, "Unexpected token '=>'"},
		{"class C { #f; m() { #f in () => {}; } }", 1, 30, "Unexpected token '=>'"},
		{"new () => {};", 1, 8, "Unexpected token '=>'"},
		{"x = () => {} + 1;", 1, 14, "Unexpected token '+'"},
		{"() => {}(1);", 1, 9, "Unexpected token '('"},
		{"() => {}.x;", 1, 9, "Unexpected token '.'"},
		{"(a) => {}++;", 1, 10, "Unexpected token '++'"},
		// Its concise body in a for head excludes `in` as the head does.
		{"for (() => a in b;;);", 1, 6, "Invalid left-hand side in for-in loop"},
		{"for (async () => a in b;;);", 1, 6, "Invalid left-hand side in for-in loop"},
		{"for (x => a in b;;);", 1, 6, "Invalid left-hand side in for-in loop"},
		{"for (var f = () => a in b;;);", 1, 6, "for-in loop variable declaration may not have an initializer."},
		{"(a,);", 1, 4, "Unexpected token ')'"},
		{"();", 1, 2, "Unexpected token ')'"},
		{"(...a);", 1, 6, "Unexpected token ')'"},
		{"x = a b;", 1, 7, "Unexpected identifier 'b'"},
		{"x = ;", 1, 5, "Unexpected token ';'"},
		{"x = ", 1, 5, "Unexpected end of input"},
		{"x = \"s\" y;", 1, 9, "Unexpected identifier 'y'"},
		{"a b", 1, 3, "Unexpected identifier 'b'"},
		{"x = 1 2;", 1, 7, "Unexpected number"},
		{"x = 1 \"s\";", 1, 7, "Unexpected string"},
		{"x = { `t` };", 1, 7, "Unexpected template string"},
		{"x = { a: 1 b: 2 };", 1, 12, "Unexpected identifier 'b'"},
		{"a ? b;", 1, 6, "Unexpected token ';'"},
		{"x = { , };", 1, 7, "Unexpected token ','"},
		{"x = [1 2];", 1, 8, "Unexpected number"},
		{"x = a.;", 1, 7, "Unexpected token ';'"},
		{"x = a?.;", 1, 8, "Unexpected token ';'"},
		{"x = new;", 1, 8, "Unexpected token ';'"},
		{"function () {}", 1, 10, "Unexpected token '('"},
		{"function f( 1 ) {}", 1, 13, "Unexpected number"},
		{"class {}", 1, 7, "Unexpected token '{'"},
		{"x = `a${}`;", 1, 9, "Unexpected token '}'"},
		{"x = `a${b`;", 1, 10, "Unterminated template literal"},
		{"x = `a${", 1, 9, "Unexpected end of input"},
		// Lexical.
		{"x = 010;", 1, 5, "Octal literals are not allowed in strict mode."},
		{"x = 08;", 1, 5, "Decimals with leading zeros are not allowed in strict mode."},
		{"x = \"\\01\";", 1, 6, "Octal escape sequences are not allowed in strict mode."},
		{"x = \"\\8\";", 1, 6, "\\8 and \\9 are not allowed in strict mode."},
		{"x = /a/x;", 1, 8, "Invalid regular expression flags"},
		{"x = /a/gg;", 1, 8, "Invalid regular expression flags"},
		{"x = /a;", 1, 5, "Invalid regular expression: missing /"},
		{"\"unterminated", 1, 1, "Unterminated string constant"},
		{"/* x", 1, 1, "Unterminated comment"},
		{"`abc", 1, 1, "Unterminated template literal"},
		{"@", 1, 1, "Invalid or unexpected token"},
		{"x = 1_;", 1, 6, "Numeric separators are not allowed at the end of numeric literals"},
		{"3in x;", 1, 2, "Identifier directly after number"},
		{"x = \"\\u{110000}\";", 1, 6, "Undefined Unicode code-point"},
		{"var bre\\u0061k;", 1, 5, "Keyword must not contain escaped characters"},
		{"x = \\u0074his;", 1, 5, "Keyword must not contain escaped characters"},
		{"x = { \\u0069f };", 1, 7, "Keyword must not contain escaped characters"},
		{"({ g\\u0065t x() {} });", 1, 13, "Unexpected identifier 'x'"},
		{"const s = \"é\"; let s;", 1, 20, "Identifier 's' has already been declared"},
		{"let a = 1;\nlet b = 2;\n  let a = 3;", 3, 7, "Identifier 'a' has already been declared"},
		{"let a = 1;\r\n\r\nlet a;", 3, 5, "Identifier 'a' has already been declared"},
		// Modules.
		{"export const x = 1; export { x };", 1, 30, "Duplicate export of 'x'"},
		{"export const x = 1, y = 2; export { y as x };", 1, 37, "Duplicate export of 'x'"},
		{"export default 1; export default 2;", 1, 19, "Duplicate export of 'default'"},
		{"let x; export { x as default }; export default 1;", 1, 33, "Duplicate export of 'default'"},
		{"export { y };", 1, 10, "Export 'y' is not defined"},
		{"function f() { let y; } export { y };", 1, 34, "Export 'y' is not defined"},
		{"export { if };", 1, 10, "Unexpected token 'if'"},
		{"export { \"a\" };", 1, 10, "Unexpected token 'a'"},
		{"{ import x from \"y\"; }", 1, 3, "'import' and 'export' may only appear at the top level"},
		{"function f() { export const x = 1; }", 1, 16, "'import' and 'export' may only appear at the top level"},
		{"export x;", 1, 8, "Unexpected identifier 'x'"},
		{"export;", 1, 7, "Unexpected token ';'"},
		{"import { a b } from \"m\";", 1, 12, "Unexpected identifier 'b'"},
		{"import { a } \"m\";", 1, 14, "Unexpected string"},
		{"import { if } from \"m\";", 1, 10, "Unexpected token 'if'"},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			_, err := ParseModule("t.js", tt.src, Options{})
			require.Error(t, err)
			var se *Error
			require.ErrorAs(t, err, &se)
			assert.Equal(t, tt.msg, se.Message())
			assert.Equal(t, [2]int{tt.line, tt.col}, [2]int{se.Line, se.Col}, "position")
		})
	}
}

func TestScriptErrors(t *testing.T) {
	tests := []struct {
		src       string
		line, col int
		msg       string
	}{
		{"import x from \"y\";", 1, 1, "Cannot use import statement outside a module"},
		{"x = import.meta;", 1, 5, "Cannot use 'import.meta' outside a module"},
		{"function f() { return () => import.meta; }", 1, 29, "Cannot use 'import.meta' outside a module"},
		{"export const x = 1;", 1, 1, "Unexpected token 'export'"},
		{"return;", 1, 1, "Illegal return statement"},
		{"'use strict'; var yield;", 1, 19, "Unexpected strict mode reserved word"},
		{"async function f() { var await; }", 1, 26, "Unexpected reserved word"},
		{`async function f() { var \u0061wait; }`, 1, 26, "Keyword must not contain escaped characters"},
		{"x = async (a) => { let await; };", 1, 24, "Unexpected reserved word"},
		{"x = async (a = await) => 1;", 1, 16, "Unexpected reserved word"},
		{"x = async ({ b: [await] }) => 1;", 1, 18, "Unexpected reserved word"},
		{"x = async ({ await = 1 }) => 1;", 1, 14, "Unexpected reserved word"},
		{"x = async await => 1;", 1, 11, "Unexpected reserved word"},
		// A static block reserves await as a module does, through arrow
		// parameters but not bodies or other functions.
		{"class C { static { var await; } }", 1, 24, "Unexpected reserved word"},
		{"class C { static { ({ await }); } }", 1, 23, "Unexpected reserved word"},
		{"class C { static { await: 0; } }", 1, 25, "Unexpected token ':'"},
		{"class C { static { (await => 0); } }", 1, 27, "Unexpected token '=>'"},
		{"class C { static { (class await {}); } }", 1, 27, "Unexpected reserved word"},
		{"async function f() { (async function await() {}); }", 1, 38, "Unexpected reserved word"},
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
	// Outside modules and async functions await is an identifier.
	for _, src := range []string{
		"var await; await = 1; function g(await) { await: ; return await; } x = { await, await() {} };",
		`\u0061wait: ; var \u0061wait; x = () => await;`,
		"async function f() { function g(await) { var await; } } await; x = async () => 1; await;",
		"x = async (a = b.await, { await: c, [d]: e } = { await: 1 }, f = function (await) {}) => 1; y = async(await);",
		"class C { static { (() => { class await {} }); (function await(await) {}); } x = await; } await;",
		"async function f() { (function await() {}); }",
		"var await; x = await ** 2; y = 2 ** await ** 2;",
		"import('x'); x = () => import(y,); function f() { return import(await); }",
	} {
		t.Run(src, func(t *testing.T) {
			_, err := ParseScript("t.js", src, Options{AllowUnsupported: true})
			assert.NoError(t, err)
		})
	}
}

// TestNoErrors lists constructs that superficially resemble early errors
// but are valid.
func TestNoErrors(t *testing.T) {
	srcs := []string{
		"x = { a: 1, a: 2 };",
		"for (var f = (() => a in b);;); for (var f = () => (a in b);;); for (var f = () => { a in b };;);",
		"for (var f = x => x ? a in b : 1;;); for (var f = (a = b in c) => a;;); for (var f = function () { return a in b };;);",
		"for (() => {}; ;); for (let f = () => a;;);",
		"x = { __proto__: 1, ['__proto__']: 2, __proto__() {} };",
		"try {} catch (e) { var e; }",
		"function f() { var a; var a; function g() {} function g() {} }",
		"function f(a) { var a; function a() {} }",
		"let a; { let a; { let a; } }",
		"function f() { { let x; } var x; }",
		"function f() { { function g() {} } var g; } { function h() {} } var h; switch (0) { case 0: var k; } { function k() {} }",
		"(a) = 1; (a.b) = 2; [(a)] = [1]; ({ x: (a) } = {});",
		"a: b: while (1) { continue a; break b; }",
		"a: { b: { break a; } }",
		"x = { get: 1, set: 2, async: 3, static: 4, of: 5, let: 6, await: 7, yield: 8 };",
		"x = a.get.set.async.static.of.let.await.yield.if.class.function;",
		"const { if: a, class: b, let: c } = o;",
		"export { x as if, x as \"string name\" }; let x;",
		"for (const x of y) for (const z in w) ;",
		"for (x.y of z) ;",
		"for ([a, b] of c) ;",
		"for ({ a } of c) ;",
		"x = { ...a, }; y = [...a,]; ({ ...a } = c);",
		"[(a), (b.c), (d) = 1] = e; ({ a: (b), c: (d) = 1 } = e); for ([(a)] of e) ; x = ((a)) + (b = 1);",
		"x = (a) ? (b) : (c) => d;",
		"x = a ? () => 1 : (b) => 2; y = (() => 1); z = [...() => 1]; f(() => 1, (a) => 2); w = (() => {})(); v = `${() => 1}`; u = () => (a) => a;",
		"x = (a)++ + (b) * (c) ? (d) : (e) in f; y = (a, b) ? c : d; z = ((a)) = 1;",
		"x = a ? b : c => d;",
		"x = a || b || (c ?? undefined) ? 1 : 2;",
		"x = (a ?? b) || c; y = a ?? (b || c); z = a && b || c;",
		"x = 2 ** -2; y = (-2) ** 2; z = -(2 ** 2); w = 2 ** 3 ** 2;",
		"x = (await a) ** 2; y = 2 ** await a; z = await (a ** 2); w = async () => (await a) ** 2;",
		"x = a\n++\nb;",
		"if (a) ; else ;",
		"do ; while (0)",
		"do ; while (0) x;",
		"x = { async *[a]() {}, get [b]() {}, set [c](v) {}, async d() {}, *e() {} };",
		"label: for (;;) break label;",
		"x = 1..toString(); y = 1.5.toFixed(1); z = 1 .toString();",
		"x = a\n/b/g;",
		"debugger",
		"x = { \"__proto__\": 1 }; y = { __proto__: 1 }; z = { __proto__ };",
		"x = async; y = async(1); z = async.x; w = { async };",
		"x = of; y = get; z = set;",
		"let \\u{61}; \\u{61} = 1;",
		"x = 1_000.5_5e1_0;",
		"x = 0.5.toFixed(); y = .5.toFixed();",
		"({ __proto__: a, __proto__: b } = c); [{ __proto__: a, \"__proto__\": b }] = c;",
		"x = { get 1() {}, get \"s\"() {}, get [k]() {}, set x(v) {}, get x() {}, set y([a, b] = []) {}, set z({ c }) {} };",
		"x = { get() {}, set() {}, get get() {}, set set(v) {} };",
		"function f(a = 1) { ('use strict'); 'use\\x20strict'; } function g(a) { 'use strict'; } x = { set y([a]) { f(); 'use strict'; } };",
	}
	for _, src := range srcs {
		t.Run(src, func(t *testing.T) {
			_, err := ParseModule("t.js", src, Options{AllowUnsupported: true})
			assert.NoError(t, err)
		})
	}
}

// An escaped reserved word is an IdentifierName (property names, member
// access, export names) but never an identifier, and escaped contextual
// keywords never act as keywords.
func TestEscapedKeywords(t *testing.T) {
	const msg = "Keyword must not contain escaped characters"
	errs := []struct {
		src string
		col int
		msg string
	}{
		{`\u0069f (x) ;`, 1, msg},
		{`var \u0069f;`, 5, msg},
		{`x = \u0074rue;`, 5, msg},
		{`x = n\u0065w X();`, 5, msg},
		{`l\u0065t x = 1;`, 1, msg},
		{`var yi\u0065ld;`, 5, msg},
		{`x = { \u0069f };`, 7, msg},
		{`({ \u0069f = 1 } = o);`, 4, msg},
		{`const { \u0069f } = o;`, 9, msg},
		{`\u0069f: ;`, 1, msg},
		{`a: for (;;) break \u0069f;`, 19, msg},
		{`import { \u0069f } from "m";`, 10, msg},
		{`let x; export { \u0069f as x };`, 17, msg},
		{`\u0061sync function f() {}`, 12, "Unexpected token 'function'"},
		{`x = \u0061sync () => 1;`, 19, "Unexpected token '=>'"},
		{`x = { g\u0065t y() {} };`, 16, "Unexpected identifier 'y'"},
		{`for (x \u006ff y) ;`, 8, `Unexpected identifier '\u006ff'`},
		{`function f() { return new.t\u0061rget; }`, 27, `Unexpected identifier 't\u0061rget'`},
		{`import x fr\u006fm "m";`, 10, `Unexpected identifier 'fr\u006fm'`},
		{`export * \u0061s y from "m";`, 10, `Unexpected identifier '\u0061s'`},
	}
	for _, tt := range errs {
		t.Run(tt.src, func(t *testing.T) {
			_, err := ParseModule("t.js", tt.src, Options{})
			require.Error(t, err)
			var se *Error
			require.ErrorAs(t, err, &se)
			assert.Equal(t, tt.msg, se.Message())
			assert.Equal(t, tt.col, se.Col, "column")
		})
	}
	for _, src := range []string{
		`x = { \u0069f: 1, n\u0065w() {}, get \u0074his() {} }; y = x.\u0069f + x?.cl\u0061ss;`,
		`const { \u0069f: a } = o; export { a as \u0069f };`,
		`import { \u0069f as b } from "m"; b;`,
		`var \u0061sync = 1; \u0061sync; x = { \u0061sync() {} };`,
		`x = { g\u0065t: 1, s\u0065t() {} };`,
		`var \u{6F}f = [], a\u0062c; for (\u{6F}f of of) ;`,
	} {
		t.Run(src, func(t *testing.T) {
			_, err := ParseModule("t.js", src, Options{AllowUnsupported: true})
			assert.NoError(t, err)
		})
	}
}
