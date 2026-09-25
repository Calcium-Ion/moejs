package engine

import (
	"testing"
	"unicode/utf16"
)

// fuzzCollString turns fuzz input into a JS string: UTF-8 decoded to UTF-16,
// with U+F000..U+F7FF standing for the lone surrogates U+D800..U+DFFF.
func fuzzCollString(s string) *String {
	units := make([]uint16, 0, len(s))
	for _, c := range s {
		if c >= 0xF000 && c < 0xF800 {
			units = append(units, uint16(0xD800+c-0xF000))
			continue
		}
		units = utf16.AppendRune(units, c)
	}
	return FromUTF16(units)
}

// fuzzCollSwap returns s with its first pair of adjacent BMP combining
// marks of different non-zero classes swapped, a canonically equivalent
// string, or nil when s has no such pair.
func fuzzCollSwap(s *String) *String {
	units := append([]uint16(nil), s.UTF16()...)
	for i := 0; i+1 < len(units); i++ {
		a, b := combiningClass(rune(units[i])), combiningClass(rune(units[i+1]))
		if a != 0 && b != 0 && a != b {
			units[i], units[i+1] = units[i+1], units[i]
			return FromUTF16(units)
		}
	}
	return nil
}

// FuzzLocaleCompare checks that localeCompare's collation is a total
// preorder that respects canonical equivalence: compare(a, b) is
// -compare(b, a), the order is transitive, swapping two adjacent marks of
// different classes keeps a string equal, and the ASCII fast path agrees
// with the general comparison.
func FuzzLocaleCompare(f *testing.F) {
	for _, s := range [][3]string{
		{"a", "b", "c"}, {"résumé", "resume", "Resume"}, {"ä", "ä", "z"},
		{"й", "й", "й̣"}, {"ӝ̧", "ӝ̧̧", "ж"},
		{"æ", "ae", "aé"}, {"đ", "d̵", "ð"}, {"ﬁ", "fi", "FI"}, {"가", "가", "각"},
		{"", "", "\U0001F600"}, {"item_1", "item-1", "Item1"}, {"a\x00b", "ab", "a­b"},
		{"x̛̣", "x̛̣", "ự"}, {"一", "丁", "ｱ"}, {"", " ", "\t"},
	} {
		f.Add(s[0], s[1], s[2])
	}
	f.Fuzz(func(t *testing.T, x, y, z string) {
		if len(x)+len(y)+len(z) > 256 {
			return
		}
		r := NewRealm()
		cmp := func(a, b *String) int {
			c, err := collateCompare(r, a, b)
			if err != nil {
				t.Fatal(err)
			}
			return c
		}
		a, b, c := fuzzCollString(x), fuzzCollString(y), fuzzCollString(z)
		ab, ba, bc, ac := cmp(a, b), cmp(b, a), cmp(b, c), cmp(a, c)
		if ab != -ba {
			t.Fatalf("compare(%q, %q) = %d but the reverse is %d", x, y, ab, ba)
		}
		if ab <= 0 && bc <= 0 && ac > 0 || ab >= 0 && bc >= 0 && ac < 0 {
			t.Fatalf("not transitive: %q %q %q: %d %d %d", x, y, z, ab, bc, ac)
		}
		if cmp(a, a) != 0 {
			t.Fatalf("compare(%q, itself) != 0", x)
		}
		if s := fuzzCollSwap(a); s != nil && cmp(a, s) != 0 {
			t.Fatalf("%q is not equal to its reordered marks %v", x, s.UTF16())
		}
		if as, ok := a.ASCII(); ok {
			if bs, ok := b.ASCII(); ok {
				fast, _ := collateASCII(r, as, bs)
				slow, _ := collateLevels(r, a, b)
				if fast != slow {
					t.Fatalf("ASCII %q vs %q: fast path %d, general %d", as, bs, fast, slow)
				}
			}
		}
	})
}
