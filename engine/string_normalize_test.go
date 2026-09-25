package engine

import (
	"bufio"
	"compress/gzip"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// normTestLine is one NormalizationTest.txt line: columns c1..c5.
type normTestLine struct {
	part int
	cols [5][]uint16
}

// loadNormalizationTest reads testdata/normalization_test.txt.gz, written
// by go generate (internal/gen/ucd -normtest).
func loadNormalizationTest(t *testing.T) []normTestLine {
	t.Helper()
	f, err := os.Open("testdata/normalization_test.txt.gz")
	require.NoError(t, err)
	defer f.Close()
	zr, err := gzip.NewReader(f)
	require.NoError(t, err)
	var lines []normTestLine
	part := -1
	sc := bufio.NewScanner(zr)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "#"):
			continue
		case strings.HasPrefix(line, "@Part"):
			part, err = strconv.Atoi(line[len("@Part"):])
			require.NoError(t, err)
			continue
		}
		cols := strings.Split(line, ";")
		require.Len(t, cols, 5, line)
		var l normTestLine
		l.part = part
		for i, c := range cols {
			if len(c) == 1 {
				l.cols[i] = l.cols[c[0]-'1']
				continue
			}
			var runes []rune
			for h := range strings.FieldsSeq(c) {
				v, err := strconv.ParseUint(h, 16, 32)
				require.NoError(t, err)
				runes = append(runes, rune(v))
			}
			l.cols[i] = utf16.Encode(runes)
		}
		lines = append(lines, l)
	}
	require.NoError(t, sc.Err())
	return lines
}

func normalizeUnits(t *testing.T, r *Realm, u []uint16, f normForm) []uint16 {
	t.Helper()
	s, err := r.normalizeString(FromUTF16(u), f)
	require.NoError(t, err)
	return s.UTF16()
}

// TestNormalizationConformance checks the invariants of NormalizationTest.txt
// for every line, and that every code point outside Part 1 is invariant under
// all four forms.
func TestNormalizationConformance(t *testing.T) {
	assert.Equal(t, unicodeVersion, ucdNormVersion)
	lines := loadNormalizationTest(t)
	require.Greater(t, len(lines), 19000)
	r := NewRealm()
	// c2 == NFC(c1..c3), c4 == NFC(c4, c5); c3 == NFD(c1..c3), c5 == NFD(c4,
	// c5); c4 == NFKC(c1..c5); c5 == NFKD(c1..c5).
	want := [4][5]int{
		formNFC:  {2, 2, 2, 4, 4},
		formNFD:  {3, 3, 3, 5, 5},
		formNFKC: {4, 4, 4, 4, 4},
		formNFKD: {5, 5, 5, 5, 5},
	}
	part1 := map[rune]bool{}
	bad := 0
	for _, l := range lines {
		if l.part == 1 {
			c, _ := decodeUnitAt(l.cols[0], 0)
			part1[c] = true
		}
		for f := range want {
			for i, w := range want[f] {
				got := normalizeUnits(t, r, l.cols[i], normForm(f))
				if !assert.Equal(t, l.cols[w-1], got, "%s(c%d) of %X", normFormNames[f], i+1, utf16.Decode(l.cols[0])) {
					if bad++; bad > 20 {
						t.Fatal("too many failures")
					}
				}
			}
		}
	}
	for c := rune(0); c <= maxCodePoint; c++ {
		if part1[c] {
			continue
		}
		var u []uint16
		if c < 0x10000 {
			u = []uint16{uint16(c)}
		} else {
			hi, lo := utf16.EncodeRune(c)
			u = []uint16{uint16(hi), uint16(lo)}
		}
		s := FromUTF16(u)
		for f := range normFormNames {
			got, err := r.normalizeString(s, normForm(f))
			require.NoError(t, err)
			if got != s {
				t.Fatalf("%U is not invariant under %s", c, normFormNames[f])
			}
		}
	}
}

func TestStringNormalize(t *testing.T) {
	cases := [][2]string{
		{`"ẛ̣".normalize()`, `"ẛ̣"`},
		{`"ẛ̣".normalize("NFC")`, `"ẛ̣"`},
		{`"ẛ̣".normalize("NFD")`, `"ẛ̣"`},
		{`"ẛ̣".normalize("NFKC")`, `"ṩ"`},
		{`"ẛ̣".normalize("NFKD")`, `"ṩ"`},
		{`"Å".normalize("NFD")`, `"Å"`},
		{`"Å".normalize()`, `"Å"`},
		{`"Å".normalize()`, `"Å"`},
		{`"ﬁ".normalize("NFKC")`, `"fi"`},
		{`"ＡＢ".normalize("NFKD")`, `"AB"`},
		{`"ﷺ".normalize("NFKD").length`, `18`},
		{`"가".normalize("NFD")`, `"가"`},
		{`"각".normalize("NFD")`, `"각"`},
		{`"각".normalize()`, `"각"`},
		{`"가".normalize()`, `"가"`},
		{`"각".normalize()`, `"각"`},
		{`"㈎".normalize("NFKC")`, `"(가)"`},
		// Canonical ordering is stable within equal classes.
		{`"á̖̀".normalize("NFD")`, `"á̖̀"`},
		{`"á̖̀".normalize()`, `"á̖̀"`},
		// A second mark of the same class is blocked; composites compose on.
		{`"à̀".normalize()`, `"à̀"`},
		{`"ȩ́".normalize()`, `"ȩ́"`},
		// Starters that compose with each other.
		{`"ୋ".normalize()`, `"ୋ"`},
		{`"େ́ା".normalize()`, `"େ́ା"`},
		// Composition exclusions.
		{`"क़".normalize()`, `"क़"`},
		{`"̈́".normalize()`, `"̈́"`},
		// Supplementary code points and lone surrogates.
		{`"\u{1D15E}".normalize()`, `"\u{1D157}\u{1D165}"`},
		{`"\u{2F800}".normalize()`, `"丽"`},
		{`"\uD800á\uDC00".normalize()`, `"\uD800á\uDC00"`},
		{`"̖́".normalize()`, `"̖́"`},
		{`"".normalize("NFKD")`, `""`},
		// Coercions.
		{`String.prototype.normalize.call(123, "NFD")`, `"123"`},
		{`"Å".normalize({ toString() { return "NFD"; } })`, `"Å"`},
		{`"Å".normalize(undefined)`, `"Å"`},
		{`String.prototype.normalize.length`, `0`},
	}
	for _, c := range cases {
		assert.Equal(t, evalExpr(t, c[1]), evalExpr(t, c[0]), c[0])
	}
	for _, c := range [][2]string{
		{`"a".normalize("nfc")`, "RangeError: The normalization form should be one of NFC, NFD, NFKC, NFKD."},
		{`"a".normalize(null)`, "RangeError: The normalization form should be one of NFC, NFD, NFKC, NFKD."},
		{`"a".normalize("NFC ")`, "RangeError: The normalization form should be one of NFC, NFD, NFKC, NFKD."},
		{`String.prototype.normalize.call(null)`, "TypeError: String.prototype.normalize called on null or undefined"},
		{`String.prototype.normalize.call(undefined, "x")`, "TypeError: String.prototype.normalize called on null or undefined"},
	} {
		_, err := evalModule(t, "export function f() { return "+c[0]+"; }").callErr("f")
		require.Error(t, err, c[0])
		assert.Equal(t, c[1], errMessage(t, err), c[0])
	}
}

// TestStringNormalizeFastPath checks that ASCII strings and strings that pass
// the quick check are returned as is without allocating, and that a string
// the full check proves normalized is returned too.
func TestStringNormalizeFastPath(t *testing.T) {
	r := NewRealm()
	normalization()
	for _, s := range []string{"hello world", "caf\u00E9 \u4E2D\u6587 \U0001F600", "\uAC00\uAC01"} {
		v := str(s)
		for _, form := range []Value{Undefined(), str("NFC"), str("NFKC")} {
			args := []Value{form}
			var got Value
			allocs := testing.AllocsPerRun(100, func() {
				got, _ = stringProtoNormalize(r, v, args)
			})
			assert.Same(t, v.AsString(), got.AsString(), "%q %v", s, form)
			assert.Zero(t, allocs, "%q %v", s, form)
		}
	}
	for _, c := range [][2]string{
		{"hello", "NFD"},
		{"hello", "NFKD"},
		{"\u00E9\u0300", "NFC"},     // U+0300 is NFC_QC Maybe
		{"q\u0316\u0300", "NFKC"},   // Maybe, already ordered and uncomposable
		{"\u4E2D\u0300", "NFD"},     // no decomposition, one mark
		{"\U0001F600\u00C5", "NFC"}, // supplementary then Yes
	} {
		v := str(c[0])
		got, err := callMethodErr(r, v, "normalize", str(c[1]))
		require.NoError(t, err)
		assert.Same(t, v.AsString(), got.AsString(), "%q %s", c[0], c[1])
	}
}

func TestStringNormalizeLimits(t *testing.T) {
	r := NewRealm()
	lowerMaxStringLength(t, 1000)
	s := FromGoString(strings.Repeat("ﷺ", 100))
	_, err := r.normalizeString(s, formNFKD)
	assertErrorKind(t, err, KindRangeError, "Invalid string length")
	out, err := r.normalizeString(FromGoString(strings.Repeat("ﷺ", 50)), formNFKD)
	require.NoError(t, err)
	assert.Equal(t, 900, out.Len())
	// NFC of a string at the limit whose decomposition is longer than it.
	s = FromGoString(strings.Repeat("é", 999) + "̀")
	out, err = r.normalizeString(s, formNFC)
	require.NoError(t, err)
	assert.Equal(t, 1000, out.Len())
	// A long run of marks is sorted in O(n log n).
	marks := strings.Repeat("̖́", 200_000)
	lowerMaxStringLength(t, MaxStringLength)
	out, err = r.normalizeString(FromGoString("a"+marks), formNFD)
	require.NoError(t, err)
	u := out.UTF16()
	assert.Equal(t, uint16('a'), u[0])
	assert.Equal(t, uint16(0x0316), u[1])
	assert.Equal(t, uint16(0x0301), u[len(u)-1])
}

func TestStringNormalizeInterrupt(t *testing.T) {
	for _, s := range []string{strings.Repeat("é", 1e5), strings.Repeat("̀", 1e5), strings.Repeat("À", 1e5)} {
		for f := range normFormNames {
			r := NewRealm()
			r.Interrupt("stop")
			_, err := r.normalizeString(FromGoString(s), normForm(f))
			var ie *InterruptedError
			assert.True(t, errors.As(err, &ie), "%s %U...", normFormNames[f], []rune(s)[0])
		}
	}
}
