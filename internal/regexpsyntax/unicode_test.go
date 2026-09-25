package regexpsyntax

import (
	"slices"
	"testing"
	"unicode"
)

func TestRuneSetOperations(t *testing.T) {
	a := Set{5, 9, 1, 3}.Normalize()
	if !slices.Equal(a, Set{1, 3, 5, 9}) {
		t.Fatalf("normalize = %v", a)
	}
	if got := (Set{1, 3, 4, 6}).Normalize(); !slices.Equal(got, Set{1, 6}) {
		t.Fatalf("adjacent ranges not merged: %v", got)
	}
	b := Set{2, 6}
	for _, tc := range []struct {
		name      string
		got, want Set
	}{
		{"union", a.Union(b), Set{1, 9}},
		{"intersect", a.Intersect(b), Set{2, 3, 5, 6}},
		{"subtract", a.Subtract(b), Set{1, 1, 7, 9}},
		{"complementIn", a.ComplementIn(10), Set{0, 0, 4, 4, 10, 10}},
		{"complement", Set{0, 0x41}.Complement(), Set{0x42, MaxCodePoint}},
	} {
		if !slices.Equal(tc.got, tc.want) {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
	for c, want := range map[rune]bool{0: false, 1: true, 3: true, 4: false, 9: true, 10: false} {
		if a.Has(c) != want {
			t.Errorf("has(%d) = %v", c, !want)
		}
	}
}

func TestUnicodeTablesCoverGeneralCategories(t *testing.T) {
	var all Set
	for _, tab := range ucdGeneralCategories {
		if len(tab.name) != 2 || tab.name == "LC" {
			continue // groups
		}
		s := decodeRanges(tab.data)
		if len(all.Intersect(s)) != 0 {
			t.Fatalf("category %s overlaps another", tab.name)
		}
		all = all.Union(s)
	}
	if !slices.Equal(all, Set{0, MaxCodePoint}) {
		t.Fatalf("categories do not cover the code space: %v", all)
	}
}

func TestUnicodePropertySets(t *testing.T) {
	type member struct {
		c  rune
		in bool
	}
	gc := func(v string) Set {
		s, ok := generalCategorySet(v)
		if !ok {
			t.Fatalf("gc %s missing", v)
		}
		return s
	}
	sc := func(v string, ext bool) Set {
		s, ok := scriptSet(v, ext)
		if !ok {
			t.Fatalf("script %s missing", v)
		}
		return s
	}
	bin := func(v string) Set {
		s, ok := binaryPropertySet(v)
		if !ok {
			t.Fatalf("binary property %s missing", v)
		}
		return s
	}
	for _, tc := range []struct {
		name string
		set  Set
		want []member
	}{
		{"gc=Lu", gc("Lu"), []member{{'A', true}, {'a', false}}},
		{"gc=Uppercase_Letter", gc("Uppercase_Letter"), []member{{'Z', true}}},
		{"gc=L", gc("Letter"), []member{{'a', true}, {0xAA, true}, {'1', false}}},
		{"gc=LC", gc("Cased_Letter"), []member{{'a', true}, {0xAA, false}}},
		{"gc=digit", gc("digit"), []member{{'7', true}, {0x660, true}, {'x', false}}},
		{"gc=Cn", gc("Cn"), []member{{0x378, true}, {'a', false}, {MaxCodePoint, true}}},
		{"sc=Han", sc("Han", false), []member{{0x4E00, true}, {0x9FA5, true}, {'a', false}}},
		{"sc=Hani", sc("Hani", false), []member{{0x3007, true}}},
		{"sc=Latin", sc("Latn", false), []member{{'A', true}, {0x3B1, false}}},
		{"sc=Arab", sc("Arabic", false), []member{{0x660, true}}},
		{"sc=Thaa", sc("Thaana", false), []member{{0x660, false}, {0x780, true}}},
		{"scx=Thaa", sc("Thaana", true), []member{{0x660, true}, {0x780, true}}},
		{"sc=Zyyy", sc("Common", false), []member{{0x30FC, true}, {'1', true}}},
		{"scx=Zyyy", sc("Common", true), []member{{0x30FC, false}, {'1', true}}},
		{"scx=Hira", sc("Hiragana", true), []member{{0x30FC, true}}},
		{"sc=Zzzz", sc("Unknown", false), []member{{0x378, true}, {'a', false}}},
		{"sc=Hrkt", sc("Katakana_Or_Hiragana", false), nil},
		{"Any", bin("Any"), []member{{0, true}, {MaxCodePoint, true}}},
		{"ASCII", bin("ASCII"), []member{{0x7F, true}, {0x80, false}}},
		{"Assigned", bin("Assigned"), []member{{'a', true}, {0x378, false}}},
		{"Alpha", bin("Alpha"), []member{{'a', true}, {'1', false}}},
		{"Emoji_Presentation", bin("EPres"), []member{{0x1F600, true}, {'#', false}}},
		{"Extended_Pictographic", bin("Extended_Pictographic"), []member{{0x1F600, true}}},
		{"CWKCF", bin("CWKCF"), []member{{'A', true}, {'a', false}}},
		{"ID_Start", bin("IDS"), []member{{'a', true}, {'$', false}}},
	} {
		for _, m := range tc.want {
			if tc.set.Has(m.c) != m.in {
				t.Errorf("%s has U+%04X = %v, want %v", tc.name, m.c, !m.in, m.in)
			}
		}
		if tc.want == nil && len(tc.set) != 0 {
			t.Errorf("%s = %v, want empty", tc.name, tc.set)
		}
	}
	for _, p := range []string{"Script", "sc", "Letter", "Lowercase_Letter", "Bogus", "gc"} {
		if _, ok := binaryPropertySet(p); ok {
			t.Errorf("binary property %q resolved", p)
		}
	}
	if _, ok := generalCategorySet("Latin"); ok {
		t.Error("gc=Latin resolved")
	}
	if _, ok := scriptSet("Lu", false); ok {
		t.Error("sc=Lu resolved")
	}
	if len(ucdBinaryProperties)+3 != 53 {
		t.Errorf("%d binary properties, want the 53 of table 67", len(ucdBinaryProperties)+3)
	}
}

// \s is WhiteSpace plus LineTerminator: Zs, TAB, VT, FF, ZWNBSP, LF, CR, LS
// and PS (not U+0085, which Unicode White_Space includes).
func TestSpaceSetMatchesUnicode(t *testing.T) {
	zs, _ := generalCategorySet("Zs")
	want := zs.Union(Set{'\t', '\r', 0xFEFF, 0xFEFF, 0x2028, 0x2029}.Normalize())
	if !slices.Equal(spaceSet, want) {
		t.Fatalf("spaceSet = %x, want %x", spaceSet, want)
	}
	ws, _ := binaryPropertySet("White_Space")
	if got := ws.Subtract(Set{0x85, 0x85}).Union(Set{0xFEFF, 0xFEFF}); !slices.Equal(got, spaceSet) {
		t.Fatalf("White_Space - U+0085 + U+FEFF = %x, want %x", got, spaceSet)
	}
}

func TestCanonicalize(t *testing.T) {
	nonU, u := canonNonUnicode(), simpleFolding()
	for _, tc := range []struct {
		m       *CaseMap
		c, want rune
	}{
		{nonU, 'k', 'K'},
		{nonU, 'K', 'K'},
		{nonU, 0x212A, 0x212A}, // KELVIN SIGN is already uppercase
		{nonU, 0x17F, 0x17F},   // ſ uppercases to ASCII S: kept
		{nonU, 0x131, 0x131},   // dotless i uppercases to ASCII I: kept
		{nonU, 0xB5, 0x39C},    // micro sign uppercases to Greek MU
		{nonU, 0xDF, 0xDF},     // ß uppercases to "SS": kept
		{nonU, 0x1F80, 0x1F80}, // full uppercase is two code points
		{nonU, 0x1C5, 0x1C4},
		{nonU, 0xD800, 0xD800},
		{u, 'K', 'k'},
		{u, 0x212A, 'k'},
		{u, 0x17F, 's'},
		{u, 0x1E9E, 0xDF},
		{u, 0x10400, 0x10428},
		{u, 0x1F88, 0x1F80},
	} {
		if got := tc.m.Canonicalize(tc.c); got != tc.want {
			t.Errorf("fold=%v canonicalize(U+%04X) = U+%04X, want U+%04X", tc.m.fold, tc.c, got, tc.want)
		}
	}
	for _, tc := range []struct {
		m        *CaseMap
		in, want Set
	}{
		{nonU, Set{'k', 'k'}, Set{'K', 'K', 'k', 'k'}},
		{nonU, Set{0x17F, 0x17F}, Set{0x17F, 0x17F}},
		{nonU, Set{0xB5, 0xB5}, Set{0xB5, 0xB5, 0x39C, 0x39C, 0x3BC, 0x3BC}},
		{nonU, Set{0xDF, 0xDF}, Set{0xDF, 0xDF}},
		{u, Set{'k', 'k'}, Set{'K', 'K', 'k', 'k', 0x212A, 0x212A}},
		{u, Set{'s', 's'}, Set{'S', 'S', 's', 's', 0x17F, 0x17F}},
		{u, Set{0xDF, 0xDF}, Set{0xDF, 0xDF, 0x1E9E, 0x1E9E}},
		{u, Set{'0', '9'}, Set{'0', '9'}},
		{u, Set{'a', 'z'}, Set{'A', 'Z', 'a', 'z', 0x17F, 0x17F, 0x212A, 0x212A}},
	} {
		if got := tc.m.closure(tc.in); !slices.Equal(got, tc.want) {
			t.Errorf("fold=%v closure(%x) = %x, want %x", tc.m.fold, tc.in, got, tc.want)
		}
	}
	// MaybeSimpleCaseFolding (v flag): the image under scf.
	if got := u.simpleFoldSet(Set{'A', 'C', 0x212A, 0x212A}); !slices.Equal(got, Set{'a', 'c', 'k', 'k'}) {
		t.Errorf("simpleFoldSet = %x", got)
	}
}

func TestStringProperties(t *testing.T) {
	has := func(p *stringProperty, seq ...rune) bool {
		if len(seq) == 1 {
			return p.chars.Has(seq[0])
		}
		_, ok := slices.BinarySearchFunc(p.seqs, seq, slices.Compare[[]rune])
		return ok
	}
	rgi, ok := stringPropertySet("RGI_Emoji")
	if !ok {
		t.Fatal("RGI_Emoji missing")
	}
	keycap, _ := stringPropertySet("Emoji_Keycap_Sequence")
	flags, _ := stringPropertySet("RGI_Emoji_Flag_Sequence")
	basic, _ := stringPropertySet("Basic_Emoji")
	for _, tc := range []struct {
		name string
		p    *stringProperty
		seq  []rune
		in   bool
	}{
		{"RGI_Emoji", rgi, []rune{0x1F600}, true},
		{"RGI_Emoji", rgi, []rune{'#', 0xFE0F, 0x20E3}, true},
		{"RGI_Emoji", rgi, []rune{0x1F1FA, 0x1F1F8}, true},
		{"RGI_Emoji", rgi, []rune{0x1F468, 0x200D, 0x1F469, 0x200D, 0x1F467}, true},
		{"RGI_Emoji", rgi, []rune{'#'}, false},
		{"Emoji_Keycap_Sequence", keycap, []rune{'0', 0xFE0F, 0x20E3}, true},
		{"Emoji_Keycap_Sequence", keycap, []rune{0x1F600}, false},
		{"RGI_Emoji_Flag_Sequence", flags, []rune{0x1F1FA, 0x1F1F8}, true},
		{"Basic_Emoji", basic, []rune{0x231A}, true},
		{"Basic_Emoji", basic, []rune{0xA9, 0xFE0F}, true},
		{"Basic_Emoji", basic, []rune{0xA9}, false},
	} {
		if has(tc.p, tc.seq...) != tc.in {
			t.Errorf("%s has %x = %v, want %v", tc.name, tc.seq, !tc.in, tc.in)
		}
	}
	if len(keycap.seqs) != 12 || len(keycap.chars) != 0 {
		t.Errorf("Emoji_Keycap_Sequence: %d sequences, %d char ranges", len(keycap.seqs), len(keycap.chars)/2)
	}
	if _, ok := stringPropertySet("Emoji"); ok {
		t.Error("Emoji resolved as a property of strings")
	}
}

// TestScriptAliasesCoverGoScripts checks the generated script tables against
// Go's: every script Go's unicode package knows resolves by its long name,
// so the Unicode tables are never older than the Go release's.
func TestScriptAliasesCoverGoScripts(t *testing.T) {
	for long := range unicode.Scripts {
		_, ok := scriptSet(long, false)
		if !ok {
			t.Errorf("script %s does not resolve", long)
		}
	}
}
