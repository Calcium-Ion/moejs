package regexpsyntax

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The ASCII shortcut of caseClosure must agree with the table closure.
func TestCaseClosureASCIIShortcut(t *testing.T) {
	for _, u := range []bool{false, true} {
		m := Canonicalizer(u)
		for lo := rune(0); lo < 0x80; lo++ {
			for _, hi := range []rune{lo, min(lo+5, 0x7F), 0x7F} {
				in := Set{lo, hi}
				if got, want := caseClosure(in, u), m.closure(in); !slices.Equal(got, want) {
					t.Fatalf("u=%v caseClosure(%x) = %x, want %x", u, in, got, want)
				}
			}
		}
		in := Set{'0', '9', 'K', 'K', 's', 's'}
		if got, want := caseClosure(in, u), m.closure(in); !slices.Equal(got, want) {
			t.Fatalf("u=%v caseClosure(%x) = %x, want %x", u, in, got, want)
		}
	}
}

func TestRegExpParseTree(t *testing.T) {
	ast, err := Parse(utf16.Encode([]rune(`(a)(?:(b)|c)*(?<n>d)?\k<n>`)), Flags{})
	require.NoError(t, err)
	seq := ast.Root
	require.Equal(t, OpSeq, seq.Op)
	require.Len(t, seq.Subs, 4)
	star := seq.Subs[1]
	assert.Equal(t, OpRepeat, star.Op)
	assert.Equal(t, [2]int{2, 3}, [2]int{star.CapLo, star.CapHi}, "groups inside the star")
	opt := seq.Subs[2]
	assert.Equal(t, [2]int{3, 4}, [2]int{opt.CapLo, opt.CapHi})
	assert.Equal(t, []int{3}, seq.Subs[3].Refs)
	assert.Equal(t, []string{"", "", "", "n"}, ast.Names)
}

// Nesting is bounded so that no pass over the tree recurses without limit.
func TestRegExpNestingLimit(t *testing.T) {
	ok := strings.Repeat("(", reMaxNesting) + strings.Repeat(")", reMaxNesting)
	_, err := Parse(utf16.Encode([]rune(ok)), Flags{})
	require.NoError(t, err)
	for _, open := range []string{"(", "(?:", "(?=", "(?<!", "(?i:"} {
		deep := strings.Repeat(open, reMaxNesting+1) + strings.Repeat(")", reMaxNesting+1)
		_, err = Parse(utf16.Encode([]rune(deep)), Flags{})
		require.Error(t, err, open)
		assert.Equal(t, "Regular expression too deeply nested", err.Error())
	}
}

// checkSeeds covers every construct of the grammar, valid and invalid.
var checkSeeds = []string{
	``, `a`, `a|b|`, `(a)|b`, `^\s+|\s+$`, `\bfoo\b`, `\B.`, `(?:a|b)*?c`, `a{2,3}`, `a{2,}?`, `x{`, `{`, `a{,5}`,
	`[a-z\d_]+`, `[^\s\S]`, `[]`, `[^]`, `[\b\-\]]`, `[a-]`, `[\w-z]`, `[z-a]`, `[\s-0-A]`, `\d+(?=px)`, `(?!x)y`,
	`(?<=a)b`, `(?<!a)b`, `(?<=a)*`, `(?=a)*`, `(?<year>\d{4})-(?<m>\d\d)\k<m>`, `\k<x>`, `\k<x>(?<x>)`, `\k`,
	`(a)\1\2`, `\01\8`, `\cA\c1\c`, `\x4g\x41`, `\u{1F600}`, `😀`, `\uD83D`, `[\uD83D-\uDBFF]`,
	`\p{L}+\P{Nd}`, `\p{Script=Greek}`, `\p{Foo}`, `[\p{L}]`, `.`, `(?:)`, `()`, `(`, `)`, `a)`, `[`, `\`, `*`, `a**`,
	`+a`, `a|*`, `(*)`, `(?)`, `(?:a)+`, `a{1001}`, `a{3,2}`, `((((((((((a))))))))))`, `$^`, `\s\S\w\W\d\D`,
	`\/\.\*`, `\q`, `[\q]`, `\u`, `\x`, `a+?b??c*?`, `(?i:a)`, `(?i-i:a)`, `(?ii:a)`, `(?-:a)`, `(?i-m:[a-z])`,
	`(?<a>x)|(?<a>y)`, `(?<a>x)(?<a>y)`, `(?<𝒜>x)`, `(?<A>x)`, `(?<1>x)`, `\0`, `[\0-\x1f]{2}`, `(a)|\1b`,
	`[\s--1]`, `[\d-x]+`, `[\p{L}--[a-z]]`, `[\q{abc|ab}x]`, `[^\q{ab}]`, `[\p{RGI_Emoji}]`, `[^\p{RGI_Emoji}]`,
	`[a&&b]`, `[[a]&&[b]]`, `[a--]`, `^$|\r`, `}`, `]`, `\-`, `\ka`, `[\B]`, `[\k]`,
}

// A Checker (reused, as the syntax package reuses it) reports exactly the
// errors of Parse.
func FuzzCheck(f *testing.F) {
	for _, p := range checkSeeds {
		for _, fl := range []string{"", "i", "u", "iu", "v", "iv", "ms"} {
			f.Add(p, fl)
		}
	}
	var c Checker
	f.Fuzz(func(t *testing.T, pattern, flags string) {
		if len(pattern) > 256 {
			return
		}
		var fl Flags
		for _, x := range flags {
			switch x {
			case 'i':
				fl.IgnoreCase = true
			case 'm':
				fl.Multiline = true
			case 's':
				fl.DotAll = true
			case 'u':
				fl.Unicode = true
			case 'v':
				fl.Unicode, fl.UnicodeSets = true, true
			}
		}
		units := utf16.Encode([]rune(pattern))
		_, want := Parse(units, fl)
		got := c.Check(units, fl)
		if (got == nil) != (want == nil) || got != nil && got.Error() != want.Error() {
			t.Fatalf("/%s/ %+v: Check gives %v, Parse gives %v", pattern, fl, got, want)
		}
	})
}

// A warm Checker allocates nothing for the patterns plugins use.
func TestCheckerAllocs(t *testing.T) {
	patterns := [][]uint16{}
	for _, s := range []string{`^\s+|\s+$`, `[a-z\d_]+`, `(\d{4})-(\d\d)`, `(?:https?:)?\/\/[^\/]+`, `\bfoo\b(?=x)`, `[^\s,;]+`, `.`} {
		patterns = append(patterns, utf16.Encode([]rune(s)))
	}
	var c Checker
	check := func() {
		for _, p := range patterns {
			if err := c.Check(p, Flags{IgnoreCase: true}); err != nil {
				t.Fatal(err)
			}
		}
	}
	check()
	assert.Zero(t, testing.AllocsPerRun(10, check))
}
