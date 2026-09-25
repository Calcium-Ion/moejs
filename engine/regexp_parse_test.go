package engine

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A pattern nested beyond the parser's limit is a SyntaxError.
func TestRegExpNestingLimit(t *testing.T) {
	r := NewRealm()
	_, err := r.NewRegExp(FromGoString(strings.Repeat("(?:", 5000)+"a"+strings.Repeat(")", 5000)), AtomEmpty)
	assertErrorKind(t, err, KindSyntaxError, "too deeply nested")
}

// Annex B: in a non-u class, a class escape, '-' and the next atom are a
// union, and that atom cannot start a range. Expected values are node's.
func TestRegExpClassEscapeDash(t *testing.T) {
	for _, c := range []auditJSCase{
		{js: "/[\\d-0-A]/.test(\"@\")", want: "false"},
		{js: "/[\\d-0-A]/.test(\"-\")", want: "true"},
		{js: "/[\\d-0-A]/.test(\"A\")", want: "true"},
		{js: "/[\\s-0-A]/.test(\"1\")", want: "false"},
		{js: "/[\\s--1]/.test(\"0\")", want: "false"},
		{js: "/[\\s--1]/.test(\"-\")", want: "true"},
		{js: "/[\\w-a-z]/.test(\"-\")", want: "true"},
		{js: "/[a-\\d-z]/.test(\"b\")", want: "false"},
		{js: "/[\\d-\\d-z]/.test(\"q\")", want: "false"},
		{js: "/[\\d-\\s-a]+/.exec(\"x1 -a\")[0]", want: "\"1 -a\""},
		{js: "\"0-1@A:\".replace(/[\\d-0-A]/g, \"x\")", want: "\"xxx@x:\""},
		{js: "/[\\D-0-A]+/.exec(\"12@z\")[0]", want: "\"@z\""},
	} {
		assert.Equal(t, c.expected(), auditEvalJS(t, c.js), "%s", c.js)
	}
}

// Property escapes resolve from the Unicode tables, including the
// Script_Extensions and binary properties RE2 has no syntax for.
func TestRegExpPropertyEscapesOnRE2(t *testing.T) {
	for _, c := range []auditJSCase{
		{js: `/\p{L}/u.test("é")`, want: "true"},
		{js: `/\p{L}/u.test("1")`, want: "false"},
		{js: `/\P{L}/u.test("1")`, want: "true"},
		{js: `/[^\p{L}]/u.test("é")`, want: "false"},
		{js: `/\p{scx=Hira}/u.test("ー")`, want: "true"},
		{js: `/\p{sc=Hira}/u.test("ー")`, want: "false"},
		{js: `/\p{Script_Extensions=Thaana}/u.test("٠")`, want: "true"},
		{js: `/\p{Emoji_Presentation}/u.test("\u{1F600}")`, want: "true"},
		{js: `/^\p{RI}{2}$/u.test("\u{1F1FA}\u{1F1F8}")`, want: "true"},
		{js: `/\p{Assigned}/u.test("͸")`, want: "false"},
		{js: `/\p{ID_Start}/u.test("$")`, want: "false"},
		{js: `/\p{Lowercase}/iu.test("A")`, want: "true"},
		{js: `/\P{Lowercase}/iu.test("a")`, want: "true"},
		// u (not v) inverts after the closure: \P{Lowercase} holds "A".
		{js: `/[^\P{Lowercase}]/iu.test("A")`, want: "false"},
		{js: `/\p{Any}/u.test("\ud800")`, want: "true"},
		{js: `/^\p{Any}$/u.test("\u{10FFFF}")`, want: "true"},
		// i closures beyond RE2's folding: without u Canonicalize is toUppercase.
		{js: `/µ/i.test("Μ")`, want: "true"},
		{js: `/ß/i.test("ẞ")`, want: "false"},
		{js: `/ß/iu.test("ẞ")`, want: "true"},
		{js: `/ς/i.test("Σ")`, want: "true"},
		{js: `/[Ā-ſ]/i.test("Ÿ")`, want: "true"},
		{js: `/s/i.test("ſ")`, want: "false"},
		{js: `/s/iu.test("ſ")`, want: "true"},
	} {
		assert.Equal(t, c.want, auditEvalJS(t, c.js), "%s", c.js)
	}
}
