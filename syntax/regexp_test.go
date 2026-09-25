package syntax

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An invalid regexp literal is an early error at its '/', with the message
// the RegExp constructor gives (flags in canonical order).
func TestRegexpLiteralErrors(t *testing.T) {
	tests := []struct {
		src       string
		line, col int
		msg       string
	}{
		{"x = /(/;", 1, 5, "Invalid regular expression: /(/: Unterminated group"},
		{"x = /a**/;", 1, 5, "Invalid regular expression: /a**/: Nothing to repeat"},
		{"x = /[z-a]/;", 1, 5, "Invalid regular expression: /[z-a]/: Range out of order in character class"},
		{"x = 1;\n  y = /\\p{Foo}/u;", 2, 7, "Invalid regular expression: /\\p{Foo}/u: Invalid property name"},
		{"x = /\\u{110000}/u;", 1, 5, "Invalid regular expression: /\\u{110000}/u: Invalid Unicode escape"},
		{"x = /{/u;", 1, 5, "Invalid regular expression: /{/u: Lone quantifier brackets"},
		{"x = /(?<a>x)(?<a>y)/;", 1, 5, "Invalid regular expression: /(?<a>x)(?<a>y)/: Duplicate capture group name"},
		{"x = /\\k<b>(?<a>x)/;", 1, 5, "Invalid regular expression: /\\k<b>(?<a>x)/: Invalid named capture referenced"},
		{"x = /[a&&&b]/v;", 1, 5, "Invalid regular expression: /[a&&&b]/v: Invalid set operation in character class"},
		{"x = /(?ii:a)/;", 1, 5, "Invalid regular expression: /(?ii:a)/: Repeated flag in flag group"},
		{"x = /a{2,1}/ysig;", 1, 5, "Invalid regular expression: /a{2,1}/gisy: numbers out of order in {} quantifier"},
		{"f(`${/)/}`);", 1, 6, "Invalid regular expression: /)/: Unmatched ')'"},
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

// Annex B patterns stay valid without u, and a division is never checked.
func TestRegexpLiteralValid(t *testing.T) {
	for _, src := range []string{
		"x = /{/; y = /a{/; z = /]/; w = /\\8/; v = /[\\d-x]/; u = /\\cz\\c1/;",
		"x = /(?=a)*/; y = /\\k/; z = /(a)\\2/;",
		"x = /[\\u{1F600}-\\u{1F602}]/v; y = /\\p{RGI_Emoji}/v; z = /[\\p{L}--[a-z]]/v;",
		"x = /(?<a>x)|(?<a>y)/; y = /(?i-m:a)/; z = /\\u{1F600}/u;",
		"x = a / (b) / c; y = a /= 2 / 3;",
		"x = /é[😀]/iu; y = /\\p{Script=Greek}/u;",
	} {
		t.Run(src, func(t *testing.T) {
			_, err := ParseModule("t.js", src, Options{})
			assert.NoError(t, err)
		})
	}
}
