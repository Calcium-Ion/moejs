package syntax

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestYieldEarlyErrors checks that yield is an expression only in the body
// of a generator.
func TestYieldEarlyErrors(t *testing.T) {
	tests := []struct {
		src       string
		line, col int
		msg       string
	}{
		{"yield;", 1, 1, "Unexpected strict mode reserved word"},
		{"function f() { yield 1; }", 1, 16, "Unexpected strict mode reserved word"},
		{"function* g() { const f = () => yield 1; }", 1, 33, "Unexpected strict mode reserved word"},
		{"function* g() { (a = yield) => 1; }", 1, 22, "Unexpected strict mode reserved word"},
		{"function* g() { function h() { yield; } }", 1, 32, "Unexpected strict mode reserved word"},
		{"function* g() { class A { x = yield; } }", 1, 31, "Unexpected strict mode reserved word"},
		{"function* g() { class A { static { yield; } } }", 1, 36, "Unexpected strict mode reserved word"},
		{"function* g(a = yield) {}", 1, 17, "Yield expression not allowed in formal parameter"},
		{"function* g({ [yield]: a }) {}", 1, 16, "Yield expression not allowed in formal parameter"},
		{"x = { *m(a = yield 1) {} };", 1, 14, "Yield expression not allowed in formal parameter"},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			_, err := ParseModule("t.js", tt.src, Options{})
			require.Error(t, err)
			var se *Error
			require.ErrorAs(t, err, &se)
			assert.Equal(t, "SyntaxError: "+tt.msg, se.Msg)
			assert.Equal(t, [2]int{tt.line, tt.col}, [2]int{se.Line, se.Col}, "position")
		})
	}
	for _, src := range []string{
		"function* g() { yield; yield 1; yield* g(); const x = yield, y = (yield) + 1; }",
		"function* g(a = function* () { yield; }) { class A { [yield]() {} } }",
		"x = { *m() { yield; } }; class A { *m() { yield; } static *s() { yield; } *[Symbol.iterator]() {} }",
	} {
		_, err := ParseModule("t.js", src, Options{})
		assert.NoError(t, err, src)
	}
}
