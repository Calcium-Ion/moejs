package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSimpleClassRecognizer pins which patterns take the single-class path.
func TestSimpleClassRecognizer(t *testing.T) {
	accepted := []string{`a`, `&`, `"`, `-`, `a+`, `<{2}`, `\/`, `\.`, `\$+`, `\]`, `é`, `\s+`, `\d`, `\S`, `\w{2,4}`, `[a-z]+`, `[^a-z0-9]+`, `[\s,]+`, `[\t\n ]`, `[-a]`, `[a-]`, `[\d-x]`, `[\x41-\x5a]{3}`, `[ ]+`, `[]`, `[^]`, `[\]\-\\]+`, `\d{2,}`, `[\b]`, `[\0]`, `[\s--1]`, `[\d-\s-a]`}
	for _, p := range accepted {
		assert.NotNil(t, compileSimpleClass(FromGoString(p).UTF16(), regexpFlags{global: true}), p)
	}
	rejected := []string{``, `ab`, `a*`, `]`, `{`, `}`, `|`, `$`, `^`, `\-`, `\uD83D`, `.`, `\s*`, `\s?`, `\s+?`, `\d{0,3}`, `\b`, `\s+\d`, `(\s)`, `[a-\d]`, `[z-a]`, `[\uD83D]`, `\p{L}`, `[\q]`, `\01`, `\0`, `\d{1001}`, `[a-z`, `\s+x`}
	for _, p := range rejected {
		assert.Nil(t, compileSimpleClass(FromGoString(p).UTF16(), regexpFlags{global: true}), p)
	}
	assert.Nil(t, compileSimpleClass(FromGoString(`\s+`).UTF16(), regexpFlags{ignoreCase: true}), "i flag")
	assert.Nil(t, compileSimpleClass(FromGoString(`[^a]+`).UTF16(), regexpFlags{unicode: true}), "negated class in u mode")
	assert.Nil(t, compileSimpleClass(FromGoString(`\S`).UTF16(), regexpFlags{unicode: true}), "complement escape in u mode")
	assert.NotNil(t, compileSimpleClass(FromGoString(`\s+`).UTF16(), regexpFlags{unicode: true}))
}

// TestSimpleClassMatchesTranslator runs replace, test and match with the
// single-class matcher and with the translated RE2 program on the same
// patterns and subjects (ASCII, UTF-16, surrogates) and requires identical
// results.
func TestSimpleClassMatchesTranslator(t *testing.T) {
	r := NewRealm()
	patterns := []struct{ src, flags string }{
		{`\s+`, "g"}, {`\s+`, ""}, {`\s+`, "gu"}, {`\s`, "g"}, {`\S+`, "g"}, {`\d+`, "g"}, {`\d{2,3}`, "g"}, {`\d{2}`, "g"},
		{`\d{2,}`, "g"}, {`\w+`, "g"}, {`\W+`, "g"}, {`[a-z]+`, "g"}, {`[^a-z]+`, "g"}, {`[^\s]+`, "g"}, {`[\s,;]+`, "g"},
		{`[\x00-\x1f]+`, "g"}, {`[ 　]+`, "g"}, {`[^]`, "g"}, {`[]`, "g"}, {`[éü]+`, "g"}, {`[Ѐ-ӿ]+`, "gu"},
		{`[-.]+`, "g"}, {`[.\-]`, "g"}, {`\s+`, "gm"}, {`\d+`, "gs"},
		{`[\s--1]`, "g"}, {`[\d-x]+`, "g"}, {`[\d-\s-a]+`, "g"}, {`[\w-.]+`, "g"},
		{`&`, "g"}, {`"`, "g"}, {`e`, ""}, {`\.`, "g"}, {`-+`, "g"}, {`ü`, "gu"}, {`\/`, "g"},
	}
	var subjects []*String
	for _, g := range []string{
		"", " ", "a", "hello   world\t\n\r\v\f  end", "  leading and trailing  ", "12345 67 8 9012 x1", "a1b22c333d4444",
		"tabs\tand nbsp　ideographic sep", "héllo wörld  über  straße", "Привет  мир 42", "emoji 😀  pair 😀😀 end",
		"no-match-here", "--dots...and-dashes--", "\x00\x01 ctrl \x1f",
	} {
		subjects = append(subjects, FromGoString(g))
	}
	lone := append(FromGoString("lone ").UTF16(), 0xD83D)
	lone = append(lone, FromGoString(" surrogate ").UTF16()...)
	lone = append(lone, 0xDC00)
	subjects = append(subjects, FromUTF16(append(lone, FromGoString(" here").UTF16()...)))
	templates := []string{" ", "", "$&", "[$&]", "$`", "$'", "$$", "$1", "x$"}
	for _, p := range patterns {
		rx, err := r.NewRegExp(FromGoString(p.src), FromGoString(p.flags))
		require.NoError(t, err, p.src)
		d := rx.RegExpData()
		require.NotNil(t, d.c.simple, "%s/%s should be recognized", p.src, p.flags)
		simple := d.c.simple
		run := func(fast bool, f func() (Value, error)) any {
			if fast {
				d.c.simple = simple
			} else {
				d.c.simple = nil
			}
			if err := r.setRegExpLastIndex(rx, 0); err != nil {
				t.Fatal(err)
			}
			v, err := f()
			require.NoError(t, err)
			return r.ToGo(v)
		}
		for _, subject := range subjects {
			s := StringValue(subject)
			subj := subject.GoString()
			for _, tmpl := range templates {
				f := func() (Value, error) { return regexpReplace(r, rx, d, s.AsString(), StringValue(FromGoString(tmpl))) }
				assert.Equal(t, run(false, f), run(true, f), "replace /%s/%s on %q with %q", p.src, p.flags, subj, tmpl)
			}
			fn, _ := r.FromGo(NativeFunc(func(_ *Realm, _ Value, args []Value) (Value, error) {
				m, _ := r.ToString(Arg(args, 0))
				pos, _ := r.ToString(Arg(args, 1))
				return StringValue(concat(concat(asciiString("<"), m), concat(asciiString("@"), pos))), nil
			}))
			f := func() (Value, error) { return regexpReplace(r, rx, d, s.AsString(), fn) }
			assert.Equal(t, run(false, f), run(true, f), "functional replace /%s/%s on %q", p.src, p.flags, subj)
			f = func() (Value, error) { return regexpMatch(r, rx, d, s.AsString()) }
			assert.Equal(t, run(false, f), run(true, f), "match /%s/%s on %q", p.src, p.flags, subj)
			if p.flags == "" {
				f = func() (Value, error) { return regexpProtoTest(r, ObjectValue(rx), []Value{s}) }
				assert.Equal(t, run(false, f), run(true, f), "test /%s/ on %q", p.src, subj)
			}
		}
		d.c.simple = simple
	}
}

// TestSimpleReplaceAllocations guards the hot path: a global whitespace
// collapse on a UTF-16 subject allocates the result and nothing else.
func TestSimpleReplaceAllocations(t *testing.T) {
	r := NewRealm()
	rx, err := r.NewRegExp(FromGoString(`\s+`), FromGoString("g"))
	require.NoError(t, err)
	s := FromGoString("Привет   мир,\n\tкак  дела? 请根据   用户描述  生成图片。")
	space := asciiString(" ")
	allocs := testing.AllocsPerRun(20, func() {
		if _, err := regexpReplace(r, rx, rx.RegExpData(), s, StringValue(space)); err != nil {
			t.Fatal(err)
		}
	})
	assert.LessOrEqual(t, allocs, 2.0) // the UTF-16 buffer and the String header
}
