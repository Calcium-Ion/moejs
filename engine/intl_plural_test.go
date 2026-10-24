package engine

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestIntlPluralOperands(t *testing.T) {
	// UTS #35 part 3 §5.1.1's examples, and more.
	for _, c := range []struct {
		s       string
		e       int
		i       uint64
		v, w    int
		f, t    uint64
		integer bool // n is an integer
	}{
		{"1", 0, 1, 0, 0, 0, 0, true},
		{"1.0", 0, 1, 1, 0, 0, 0, true},
		{"1.00", 0, 1, 2, 0, 0, 0, true},
		{"1.3", 0, 1, 1, 1, 3, 3, false},
		{"1.30", 0, 1, 2, 1, 30, 3, false},
		{"1.03", 0, 1, 2, 2, 3, 3, false},
		{"1.230", 0, 1, 3, 2, 230, 23, false},
		{"1200000", 0, 1200000, 0, 0, 0, 0, true},
		{"1.2", 6, 1200000, 0, 0, 0, 0, true},
		{"123", 6, 123000000, 0, 0, 0, 0, true},
		{"123", 5, 12300000, 0, 0, 0, 0, true},
		{"1200.50", 0, 1200, 2, 1, 50, 5, false},
		{"1.20050", 3, 1200, 2, 1, 50, 5, false},
		{"0", 0, 0, 0, 0, 0, 0, true},
		{"0.00", 0, 0, 2, 0, 0, 0, true},
		{"007", 0, 7, 0, 0, 0, 0, true},
		{"1.5", -2, 0, 3, 3, 15, 15, false},
		{"0.0000001", 6, 0, 1, 1, 1, 1, false},
		{"1.0000001", 6, 1000000, 1, 1, 1, 1, false},
	} {
		o := intlOperands([]byte(c.s), c.e)
		if o.i != c.i || o.v != c.v || o.w != c.w || o.f != c.f || o.t != c.t || o.e != c.e || o.large != 0 || (o.w == 0) != c.integer {
			t.Errorf("intlOperands(%s, %d) = %+v", c.s, c.e, o)
		}
	}
	// Beyond 10^18: the operands keep their value modulo 10^18.
	s := "123456789012345678901234567890.1234567890123456789012340"
	o := intlOperands([]byte(s), 0)
	if o.i != 345678901234567890 || o.large != intlPluralLargeI|intlPluralLargeF|intlPluralLargeT ||
		o.v != 25 || o.w != 24 || o.f != 890123456789012340 || o.t != 789012345678901234 {
		t.Errorf("intlOperands(%s) = %+v", s, o)
	}
	rules := parseIntlPluralRules("one:i % 10 = 1 and i % 100 != 11;two:i = 123456789;few:n % 1000000 = 567890;many:t % 100 = 34")
	for _, c := range []struct {
		s    string
		want intlPluralCategory
	}{
		{"123456789012345678901", intlPluralOne},
		{"123456789012345678911", intlPluralOther},
		{"123456789", intlPluralTwo},
		{"1000000000000000000123456789", intlPluralOther}, // i is not 123456789
		{"123456789012345678901234567890", intlPluralFew},
		{"123456789012345678901234567890.0", intlPluralFew},
		{"123456789012345678901234567890.5", intlPluralOther}, // n is not an integer
		{"0.123456789012345678901234", intlPluralMany},
	} {
		o := intlOperands([]byte(c.s), 0)
		if got := rules.category(&o); got != c.want {
			t.Errorf("category(%s) = %s, want %s", c.s, intlPluralNames[got], intlPluralNames[c.want])
		}
	}
}

func TestIntlPluralParse(t *testing.T) {
	s := parseIntlPluralRules("many:e = 0 and i != 0 and i % 1000000 = 0 and v = 0 or e != 0..5;one:i = 0,1;")
	if got := s.categories(); len(got) != 3 || got[0] != intlPluralOne || got[1] != intlPluralMany || got[2] != intlPluralOther {
		t.Errorf("categories = %v", got)
	}
	if got := parseIntlPluralRules("").categories(); len(got) != 1 || got[0] != intlPluralOther {
		t.Errorf("categories of no rules = %v", got)
	}
	if got := parseIntlPluralRules("other:;").categories(); len(got) != 1 || got[0] != intlPluralOther {
		t.Errorf("categories of other = %v", got)
	}
	for _, c := range []struct {
		s    string
		e    int
		want intlPluralCategory
	}{
		{"1", 0, intlPluralOne},
		{"0", 0, intlPluralOne},
		{"1.5", 0, intlPluralOne},
		{"2", 0, intlPluralOther},
		{"1000000", 0, intlPluralMany},
		{"1000000.0", 0, intlPluralOther},
		{"1", 6, intlPluralMany},
		{"1.2", 3, intlPluralOther},
		{"1", -3, intlPluralMany},
	} {
		o := intlOperands([]byte(c.s), c.e)
		if got := s.category(&o); got != c.want {
			t.Errorf("category(%sc%d) = %s, want %s", c.s, c.e, intlPluralNames[got], intlPluralNames[c.want])
		}
	}
	for _, bad := range []string{"one:", "one:i", "one:i = ", "one:x = 1", "one:i == 1", "one:i = 1 and", "one:i = 2..1",
		"one:i % 0 = 1", "one:i % 7 = 1", "one:i = 1 xor v = 0", "uno:i = 1", "one i = 1", "one:i = 1000000000000000000",
		"one:i = 1,", "one:i = 1..", "one:i in 1"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("parseIntlPluralRules(%q) did not panic", bad)
				}
			}()
			parseIntlPluralRules(bad)
		}()
	}
}

// intlCLDRSupplemental returns plurals.xml or ordinals.xml of CLDR 48: from
// the generator's cached zip when present (and then checks the copy in
// testdata against it), else from the copy.
func intlCLDRSupplemental(t *testing.T, name string) []byte {
	gz, err := os.ReadFile(filepath.Join("testdata", "intl_"+name+".gz"))
	if err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		t.Fatal(err)
	}
	copied, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	z, err := zip.OpenReader(filepath.Join(home, ".cache", "moejs", "cldr", "cldr-common-48.zip"))
	if err != nil {
		return copied
	}
	defer z.Close()
	f, err := z.Open("common/supplemental/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, copied) {
		t.Errorf("testdata/intl_%s.gz differs from CLDR 48's", name)
	}
	return data
}

// TestIntlPluralCLDR checks every sample of every rule of CLDR 48's
// plurals.xml and ordinals.xml: each gives its category.
func TestIntlPluralCLDR(t *testing.T) {
	var doc struct {
		Plurals []struct {
			Type  string `xml:"type,attr"`
			Rules []struct {
				Locales string `xml:"locales,attr"`
				Rule    []struct {
					Count string `xml:"count,attr"`
					Text  string `xml:",chardata"`
				} `xml:"pluralRule"`
			} `xml:"pluralRules"`
		} `xml:"plurals"`
	}
	sets, samples := 0, 0
	for _, name := range []string{"plurals.xml", "ordinals.xml"} {
		if err := xml.Unmarshal(intlCLDRSupplemental(t, name), &doc); err != nil {
			t.Fatal(err)
		}
		for _, p := range doc.Plurals {
			for _, rs := range p.Rules {
				var src strings.Builder
				var cats []intlPluralCategory
				for _, r := range rs.Rule {
					cond, _, _ := strings.Cut(r.Text, "@")
					src.WriteString(r.Count + ":" + strings.TrimSpace(cond) + ";")
					cats = append(cats, intlPluralCategoryNamed(t, r.Count))
				}
				set := parseIntlPluralRules(src.String())
				sets++
				if got := set.categories(); !equalPluralCategories(got, cats) {
					t.Errorf("%s %s: categories %v, want %v", p.Type, rs.Locales, got, cats)
				}
				for _, r := range rs.Rule {
					want := intlPluralCategoryNamed(t, r.Count)
					_, sampleText, _ := strings.Cut(r.Text, "@")
					for _, part := range strings.Split(sampleText, "@") {
						_, list, _ := strings.Cut(part, " ")
						for _, sample := range strings.Split(list, ",") {
							intlPluralSamples(t, strings.TrimSpace(sample), func(s string, e int) {
								samples++
								o := intlOperands([]byte(s), e)
								if got := set.category(&o); got != want {
									t.Errorf("%s %s: %s (c%d) is %s, want %s", p.Type, rs.Locales, s, e, intlPluralNames[got], r.Count)
								}
							})
						}
					}
				}
			}
		}
		doc.Plurals = nil
	}
	t.Logf("%d rule sets, %d samples", sets, samples)
	if sets < 65 || samples < 3863 { // CLDR 48's

		t.Errorf("only %d rule sets and %d samples", sets, samples)
	}
}

func intlPluralCategoryNamed(t *testing.T, name string) intlPluralCategory {
	for c, n := range intlPluralNames {
		if n == name {
			return intlPluralCategory(c)
		}
	}
	t.Fatalf("plural category %q", name)
	return 0
}

func equalPluralCategories(a, b []intlPluralCategory) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// intlPluralSamples calls f with each number of a sample of plurals.xml:
// "1", "1.5", "1.2c6" (formatted digits and exponent) or a range "0.0~1.5"
// of the numbers with as many fraction digits; "…" and "" have none.
func intlPluralSamples(t *testing.T, sample string, f func(string, int)) {
	if sample == "" || sample == "…" {
		return
	}
	split := func(s string) (string, int) {
		if i := strings.IndexAny(s, "ce"); i >= 0 {
			e, err := strconv.Atoi(s[i+1:])
			if err != nil {
				t.Fatalf("sample %q", sample)
			}
			return s[:i], e
		}
		return s, 0
	}
	lo, hi, isRange := strings.Cut(sample, "~")
	if !isRange {
		s, e := split(sample)
		f(s, e)
		return
	}
	ls, le := split(lo)
	hs, he := split(hi)
	_, lf, _ := strings.Cut(ls, ".")
	_, hf, _ := strings.Cut(hs, ".")
	if le != he || len(lf) != len(hf) {
		t.Fatalf("sample range %q", sample)
	}
	a, err1 := strconv.Atoi(strings.Replace(ls, ".", "", 1))
	b, err2 := strconv.Atoi(strings.Replace(hs, ".", "", 1))
	if err1 != nil || err2 != nil || a > b {
		t.Fatalf("sample range %q", sample)
	}
	for n := a; n <= b; n++ {
		s := strconv.Itoa(n)
		if v := len(lf); v > 0 {
			s = strings.Repeat("0", max(v+1-len(s), 0)) + s
			s = s[:len(s)-v] + "." + s[len(s)-v:]
		}
		f(s, le)
	}
}

// The cardinal rules of fr and the ordinal rules of en in CLDR 48.
const (
	intlPluralFr  = "one:i = 0,1;many:e = 0 and i != 0 and i % 1000000 = 0 and v = 0 or e != 0..5;"
	intlOrdinalEn = "one:n % 10 = 1 and n % 100 != 11;two:n % 10 = 2 and n % 100 != 12;few:n % 10 = 3 and n % 100 != 13;"
)

var intlPluralCategorySink intlPluralCategory

// TestIntlPluralNoAlloc checks that selecting a category allocates nothing.
func TestIntlPluralNoAlloc(t *testing.T) {
	fr, en := parseIntlPluralRules(intlPluralFr), parseIntlPluralRules(intlOrdinalEn)
	formatted := []byte("1234567.125")
	if n := testing.AllocsPerRun(100, func() {
		o := intlOperands(formatted, 0)
		intlPluralCategorySink += fr.category(&o) + en.category(&o)
	}); n != 0 {
		t.Errorf("selecting a category allocates %v times", n)
	}
}

func BenchmarkIntlPlural(b *testing.B) {
	fr, en := parseIntlPluralRules(intlPluralFr), parseIntlPluralRules(intlOrdinalEn)
	b.Run("parse/fr", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			fr = parseIntlPluralRules(intlPluralFr)
		}
	})
	for _, c := range []struct {
		name string
		s    string
		set  *intlPluralRuleSet
	}{{"fr/1", "1", fr}, {"fr/1000000", "1000000", fr}, {"fr/2.50", "2.50", fr}, {"en-ordinal/23", "23", en}, {"en-ordinal/111", "111", en}} {
		b.Run(c.name, func(b *testing.B) {
			formatted := []byte(c.s)
			b.ReportAllocs()
			for b.Loop() {
				o := intlOperands(formatted, 0)
				intlPluralCategorySink += c.set.category(&o)
			}
		})
	}
}
