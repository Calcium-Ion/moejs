package syntax

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestASI covers automatic semicolon insertion, restricted productions and
// regex-versus-division decisions across line breaks.
func TestASI(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"return-newline", "function f() { return\n1 }", "(module\n  (function f ()\n    (return)\n    1))"},
		{"prefix-increment-next-line", "a\n++b", "(module\n  a\n  (pre++ b))"},
		{"increment-alone-on-line", "a\n++\nb", "(module\n  a\n  (pre++ b))"},
		{"postfix-then-prefix", "a = b\n++c\n--d", "(module\n  (= a b)\n  (pre++ c)\n  (pre-- d))"},
		{"call-across-newline", "a\n(b)", "(module\n  (call a b))"},
		{"call-then-index", "x = a\n(b)\n[c]", "(module\n  (= x ([] (call a b) [c])))"},
		{"let-on-own-line", "let\nx = 1", "(module\n  (let (x 1)))"},
		{"division-not-regex-after-ident", "a = b\n/re/g.test(c)", "(module\n  (= a (/ (/ b re) (call (. g test) c))))"},
		{"division-after-paren", "(a)\n/b/g", "(module\n  (/ (/ a b) g))"},
		{"regex-after-paren-of-if", "if (x) /re/.test(y)", "(module\n  (if x (call (. /re/ test) y)))"},
		{"regex-after-return", "function f() { return /re/ }", "(module\n  (function f ()\n    (return /re/)))"},
		{"regex-after-operator", "x = a &&\n/re/.test(b)", "(module\n  (= x (&& a (call (. /re/ test) b))))"},
		{"regex-statement-start-with-semicolon", "/re/.test(a);\n/re2/.test(b)", "(module\n  (call (. /re/ test) a)\n  (call (. /re2/ test) b))"},
		{"member-across-newline", "a\n.b", "(module\n  (. a b))"},
		{"optional-chain-across-newline", "a\n?.b", "(module\n  (chain (?. a b)))"},
		{"two-declarations", "const x = 1\nconst y = 2", "(module\n  (const (x 1))\n  (const (y 2)))"},
		{"do-while-no-semicolon", "do x++\nwhile (y) z", "(module\n  (do (post++ x) y)\n  z)"},
		{"do-while-same-line", "do {} while (y) z", "(module\n  (do (block) y)\n  z)"},
		{"array-literal-is-index", "var a = 1\n[1].map(f)", "(module\n  (var (a (call (. ([] 1 [1]) map) f))))"},
		{"object-literal-then-index", "x = {}\n[1]", "(module\n  (= x ([] (object) [1])))"},
		{"binary-continues", "x = y\n+z", "(module\n  (= x (+ y z)))"},
		{"arrow-body-then-statement", "a = () => 1\nb = 2", "(module\n  (= a (=> () 1))\n  (= b 2))"},
		{"arrow-then-paren", "() => {}\n(a)", "(module\n  (=> ())\n  a)"},
		{"arrow-field-then-computed-field", "class C { a = () => {}\n['b'] = 1 }", "(module\n  (class C\n    (field a (=> ()))\n    (field [\"b\"] 1)))"},
		{"break-newline-label", "l: while (1) { break\nl }", "(module\n  (label l (while 1 (block\n        (break)\n        l))))"},
		{"continue-newline", "while (1) { continue\nx }", "(module\n  (while 1 (block\n      (continue)\n      x)))"},
		{"template-continues", "f(a)\n`t`", "(module\n  (tagged (call f a) (template \"t\")))"},
		{"line-separator-2028", "x = 1" + string(rune(0x2028)) + "y = 2", "(module\n  (= x 1)\n  (= y 2))"},
		{"paragraph-separator-2029", "x" + string(rune(0x2029)) + "++y", "(module\n  x\n  (pre++ y))"},
		{"keyword-before-2028", "var" + string(rune(0x2028)) + "x" + string(rune(0x2029)) + "=" + string(rune(0xA0)) + "1", "(module\n  (var (x 1)))"},
		{"break-before-2029", "l: while (1) { break" + string(rune(0x2029)) + "l }", "(module\n  (label l (while 1 (block\n        (break)\n        l))))"},
		{"crlf", "x = 1\r\ny = 2", "(module\n  (= x 1)\n  (= y 2))"},
		{"block-comment-with-newline", "x /*\n*/ ++y", "(module\n  x\n  (pre++ y))"},
		{"block-comment-without-newline", "x /* */ ++\ny", "(module\n  (post++ x)\n  y)"},
		{"if-else-newline", "if (a) b\nelse c", "(module\n  (if a b c))"},
		{"closing-brace-terminates", "let x = 1; { let y = 2\n}", "(module\n  (let (x 1))\n  (block\n    (let (y 2))))"},
		{"eof-terminates", "x", "(module\n  x)"},
		{"semicolon-alone", "x\n;", "(module\n  x)"},
		{"throw-same-line", "throw a\n+ b", "(module\n  (throw (+ a b)))"},
		{"yield-like-identifier-of", "for (const of of ofs) of", "(module\n  (for-of (const (of)) ofs of))"},
		{"async-newline-is-identifier", "async\nfunction f() {}", "(module\n  async\n  (function f ()))"},
		{"async-arrow-same-line-with-flag", "x = async\n(a)", "(module\n  (= x (call async a)))"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := ParseModule("t.js", tt.src, Options{AllowUnsupported: true})
			require.NoError(t, err)
			assert.Equal(t, tt.want, Dump(m))
		})
	}
}

// TestASIErrors covers places where a line break does not rescue the parse.
func TestASIErrors(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"throw-newline", "throw\nerr", "t.js:2:1: SyntaxError: Illegal newline after throw"},
		{"arrow-newline-before-arrow", "(a)\n=> 1", "t.js:2:1: SyntaxError: Unexpected token '=>'"},
		{"ident-arrow-newline", "a\n=> 1", "t.js:2:1: SyntaxError: Unexpected token '=>'"},
		{"no-asi-same-line", "x = a b", "t.js:1:7: SyntaxError: Unexpected identifier 'b'"},
		{"for-header-needs-semicolons", "for (a\nb) {}", "t.js:2:1: SyntaxError: Unexpected identifier 'b'"},
		{"postfix-then-garbage", "a++ b", "t.js:1:5: SyntaxError: Unexpected identifier 'b'"},
		{"empty-parens", "()\n=> 1", "t.js:1:2: SyntaxError: Unexpected token ')'"},
		{"regex-continues-as-division", "/re/.test(a)\n/re2/.test(b)", "t.js:2:6: SyntaxError: Unexpected token '.'"},
		{"block-comment-is-not-a-line-break", "x /* */ ++y", "t.js:1:11: SyntaxError: Unexpected identifier 'y'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseModule("t.js", tt.src, Options{AllowUnsupported: true})
			require.Error(t, err)
			assert.Equal(t, tt.want, err.Error())
		})
	}
}
