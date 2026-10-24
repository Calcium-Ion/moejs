package engine

import (
	"cmp"
	"compress/gzip"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

// ltTestLocale emulates the Intl.Locale constructor (ECMA-402 §15.1.1,
// UpdateLanguageId and MakeLocaleRecord) on the langTag API, the way the
// Intl bindings use it: tag and its options ("key=value" pairs joined by
// ';'); ok is false for a RangeError.
func ltTestLocale(tag, options string) (*langTag, bool) {
	t, ok := parseLangTag(tag)
	if !ok {
		return nil, false
	}
	t.canonicalize()
	opts := map[string]string{}
	var keys []string
	for kv := range strings.SplitSeq(options, ";") {
		if k, v, ok := strings.Cut(kv, "="); ok {
			opts[k] = v
			keys = append(keys, k)
		}
	}
	valid := map[string]func(string) bool{
		"language": isLangTagLanguage, "script": isLangTagScript, "region": isLangTagRegion,
		"variants": isLangTagVariants,
		"calendar": isLangTagType, "collation": isLangTagType, "numberingSystem": isLangTagType,
		"firstDayOfWeek": isLangTagType,
		"hourCycle":      func(s string) bool { return s == "h11" || s == "h12" || s == "h23" || s == "h24" },
		"caseFirst":      func(s string) bool { return s == "upper" || s == "lower" || s == "false" },
		"numeric":        func(s string) bool { return true },
	}
	for _, k := range keys {
		v := opts[k]
		if k == "firstDayOfWeek" {
			// WeekdayToUValue.
			if i := strings.Index("01234567", v); len(v) == 1 && i >= 0 {
				v = [...]string{"sun", "mon", "tue", "wed", "thu", "fri", "sat", "sun"}[i]
			}
		}
		if !valid[k](v) {
			return nil, false
		}
		switch k {
		case "language":
			t.setLanguage(v)
		case "script":
			t.setScript(v)
		case "region":
			t.setRegion(v)
		case "variants":
			t.setVariants(v)
		default:
			key := map[string]string{"calendar": "ca", "collation": "co", "firstDayOfWeek": "fw", "hourCycle": "hc",
				"caseFirst": "kf", "numeric": "kn", "numberingSystem": "nu"}[k]
			t.setUKeyword(key, langTagCanonicalUValue(key, v))
		}
	}
	t.canonicalize()
	return t, true
}

// ltTestGet applies the methods and getter of chain ('.'-joined) to t the
// way Intl.Locale does; "-" stands for undefined.
func ltTestGet(t *langTag, chain string) string {
	for step := range strings.SplitSeq(chain, ".") {
		switch step {
		case "maximize", "minimize":
			t = t.clone()
			if step == "maximize" {
				t.maximize()
			} else {
				t.minimize()
			}
			// Construct(%Intl.Locale%, « maximal ») canonicalizes again.
			t, _ = parseLangTag(t.String())
			t.canonicalize()
		case "toString":
			return t.String()
		case "baseName":
			return t.baseName()
		case "language":
			return t.language()
		case "script", "region", "variants":
			v := map[string]string{"script": t.script(), "region": t.region(), "variants": t.variants()}[step]
			if v == "" {
				return "-"
			}
			return v
		case "numeric":
			v, ok := t.uKeyword("kn")
			if ok && (v == "" || v == "true") {
				return "true"
			}
			return "false"
		default:
			key := map[string]string{"calendar": "ca", "collation": "co", "hourCycle": "hc", "caseFirst": "kf",
				"numberingSystem": "nu", "firstDayOfWeek": "fw"}[step]
			v, ok := t.uKeyword(key)
			if !ok {
				return "-"
			}
			return v
		}
	}
	return "?"
}

func TestLangTagTest262Tags(t *testing.T) {
	for _, c := range ltTest262Tags {
		got, ok := canonicalizeLangTag(c.in)
		if _, parsed := parseLangTag(c.in); parsed != ok {
			t.Errorf("%q: parseLangTag %v, canonicalizeLangTag %v", c.in, parsed, ok)
		}
		switch {
		case c.want == "" && ok:
			t.Errorf("%q: got %q, want a RangeError", c.in, got)
		case c.want != "" && got != c.want:
			t.Errorf("%q: got %q (valid %v), want %q", c.in, got, ok, c.want)
		}
	}
}

func TestLangTagTest262Locales(t *testing.T) {
	for _, c := range ltTest262Locales {
		tag, ok := ltTestLocale(c.tag, c.options)
		got := "!"
		if ok {
			if c.chain == "" {
				got = tag.String()
			} else {
				got = ltTestGet(tag, c.chain)
			}
		}
		if got != c.want {
			t.Errorf("new Intl.Locale(%q, {%s}).%s: got %q, want %q", c.tag, c.options, c.chain, got, c.want)
		}
	}
}

// TestLangTagTest262UValues is test262 Locale/constructor-unicode-extension-
// uvalue-true.js and -yes.js: every key drops "true", and the keys whose
// "yes" CLDR aliases to "true" drop "yes".
func TestLangTagTest262UValues(t *testing.T) {
	const alpha = "abcdefghijklmnopqrstuvwxyz"
	for _, c0 := range alpha + "0123456789" {
		for _, c1 := range alpha {
			key := string(c0) + string(c1)
			if got, _ := canonicalizeLangTag("en-u-" + key + "-true"); got != "en-u-"+key {
				t.Errorf("en-u-%s-true: got %q", key, got)
			}
			want := "en-u-" + key + "-yes"
			if key == "kb" || key == "kc" || key == "kh" || key == "kk" || key == "kn" {
				want = "en-u-" + key
			}
			if got, _ := canonicalizeLangTag("en-u-" + key + "-yes"); got != want {
				t.Errorf("en-u-%s-yes: got %q, want %q", key, got, want)
			}
		}
	}
}

// TestLangTagTest262Constructors covers language-tags-valid.js,
// language-tags-canonicalized.js and language-tags-with-underscore.js,
// which test262 runs through every Intl constructor: the canonical forms are
// those of language-tags-canonicalized.js, and for the other valid tags
// those of the spec.
func TestLangTagTest262Constructors(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"de", "de"}, {"de-DE", "de-DE"}, {"DE-de", "de-DE"}, {"cmn", "zh"}, {"cmn-Hans", "zh-Hans"},
		{"CMN-hANS", "zh-Hans"}, {"cmn-hans-cn", "zh-Hans-CN"}, {"es-419", "es-419"},
		{"es-419-u-nu-latn", "es-419-u-nu-latn"}, {"es-419-u-nu-latn-cu-bob", "es-419-u-cu-bob-nu-latn"},
		{"cmn-hans-cn-t-ca-u-ca-x-t-u", "zh-Hans-CN-t-ca-u-ca-x-t-u"}, {"de-gregory-u-ca-gregory", "de-gregory-u-ca-gregory"},
		{"aa-a-foo-x-a-foo-bar", "aa-a-foo-x-a-foo-bar"}, {"sgn-GR", "gss"}, {"ji", "yi"}, {"de-DD", "de-DE"}, {"in", "id"},
		{"de_DE", ""}, {"DE_de", ""}, {"cmn_Hans", ""}, {"cmn-hans_cn", ""}, {"es_419", ""},
		{"es-419-u-nu-latn-cu_bob", ""}, {"i_klingon", ""}, {"cmn-hans-cn-t-ca-u-ca-x_t-u", ""},
		{"enochian_enochian", ""}, {"de-gregory_u-ca-gregory", ""}, {"de-tester-Tester", ""},
		{"de-DE-u-kn-true-U-kn-true", ""},
	} {
		got, ok := canonicalizeLangTag(c.in)
		if c.want == "" && ok || c.want != "" && got != c.want {
			t.Errorf("%q: got %q (valid %v), want %q", c.in, got, ok, c.want)
		}
	}
}

// TestLangTagTest262GrandfatheredExtras is the second half of test262
// Locale/likely-subtags-grandfathered.js: the regular grandfathered tags
// with a variant or an extension after them maximize and minimize like
// their canonical form.
func TestLangTagTest262GrandfatheredExtras(t *testing.T) {
	get := func(tag, chain string) string {
		loc, ok := ltTestLocale(tag, "")
		if !ok {
			return "!"
		}
		return ltTestGet(loc, chain)
	}
	for _, g := range [][2]string{{"art-lojban", "jbo"}, {"cel-gaulish", "xtg"}, {"zh-guoyu", "zh"}, {"zh-hakka", "hak"}, {"zh-xiang", "hsn"}} {
		tag, canonical := g[0], g[1]
		tagMax := strings.TrimSuffix(get(canonical+"-x-0", "maximize.toString"), "-x-0")
		tagMin := strings.TrimSuffix(get(canonical+"-x-0", "minimize.toString"), "-x-0")
		for _, extra := range []string{"fonipa", "a-not-assigned", "u-attr", "u-co", "u-co-phonebk", "x-private"} {
			for _, c := range [][2]string{
				{"toString", canonical + "-" + extra},
				{"maximize.toString", tagMax + "-" + extra}, {"maximize.maximize.toString", tagMax + "-" + extra},
				{"minimize.toString", tagMin + "-" + extra}, {"minimize.minimize.toString", tagMin + "-" + extra},
				{"maximize.minimize.toString", tagMin + "-" + extra}, {"minimize.maximize.toString", tagMax + "-" + extra},
			} {
				if got := get(tag+"-"+extra, c[0]); got != c[1] {
					t.Errorf("new Intl.Locale(%q).%s: got %q, want %q", tag+"-"+extra, c[0], got, c[1])
				}
			}
		}
	}
}

// ltICUExceptions lists the tags where moejs and ICU 78.3 (Node 24.22)
// differ, with the reason; test262 covers none of them, the spec (UTS #35
// over the CLDR 48 data) decides each.
var ltICUExceptions = func() map[string]string {
	const (
		alias = "ICU 78.3's alias data lacks CLDR 48's macrolanguage aliases bh→bho and tw→ak and maps sgn-NO to nsl, " +
			"where supplementalMetadata.xml has nsi"
		und = "ICU leaves these und tags as they are when minimizing; UTS #35 Remove Likely Subtags returns the " +
			"first trial (language, language-region, language-script) that maximizes to the same tag, as ICU does " +
			"for the same tags with their language (pap-CW → pap, zh-Hant → zh-TW)"
		posix = "ICU turns the variant posix into -u-va-posix, the conversion UTS #35 §3.8.2 (Legacy Variants) " +
			"gives for old CLDR ids such as en_US_POSIX; Annex C has no rule for a BCP 47 posix variant"
		available = "aa and jbo are CLDR locales (common/main) that ICU's data leaves out"
	)
	m := map[string]string{"en-US-posix": posix}
	for _, tag := range []string{"bh", "tw", "sgn-NO"} {
		m[tag] = alias
	}
	for _, tag := range []string{"und-530", "und-531", "und-532", "und-585", "und-598", "und-608", "und-772",
		"und-798", "und-AN", "und-Arab-MM", "und-CW", "und-Hant", "und-Mong", "und-PG", "und-PH", "und-PW",
		"und-TK", "und-TV"} {
		m[tag] = und
	}
	for _, tag := range []string{"aa-a-foo-x-a-foo-bar", "aar-x-private", "art-lojban", "jbo"} {
		m[tag] = available
	}
	return m
}()

// TestLangTagICU compares canonicalization, maximize, minimize and the
// locale negotiation of Intl.Segmenter with ICU 78.3 (CLDR 48) as Node 24.22
// reports them for every alias type and likely-subtags key of CLDR 48 and a
// curated list (testdata/langtag_icu.js writes testdata/langtag_icu.json.gz).
func TestLangTagICU(t *testing.T) {
	f, err := os.Open("testdata/langtag_icu.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		ICU, CLDR string
		Entries   []string
	}
	if err := json.NewDecoder(zr).Decode(&data); err != nil {
		t.Fatal(err)
	}
	if data.CLDR != cldrVersion {
		t.Fatalf("the reference is CLDR %s, the tables CLDR %s", data.CLDR, cldrVersion)
	}
	if len(data.Entries) < 19000 {
		t.Fatalf("only %d entries", len(data.Entries))
	}
	differ := map[string]bool{}
	for _, e := range data.Entries {
		fields := strings.Split(e, " ")
		tag, want := fields[0], fields[1:]
		got := []string{"!", "!", "!"}
		if c, ok := canonicalizeLangTag(tag); ok {
			got[0] = c
			loc, _ := ltTestLocale(tag, "")
			got[1], got[2] = ltTestGet(loc, "maximize.toString"), ltTestGet(loc, "minimize.toString")
			for i := 1; i < 3; i++ {
				if got[i] == c {
					got[i] = "="
				}
			}
		}
		if len(want) == 4 {
			got = append(got, ltTestSupported(tag))
		}
		if slices.Equal(got, want) {
			if _, ok := ltICUExceptions[tag]; ok {
				t.Errorf("%q agrees with ICU but is listed as an exception", tag)
			}
			continue
		}
		differ[tag] = true
		if _, ok := ltICUExceptions[tag]; !ok {
			t.Errorf("%q: got %q, ICU %q", tag, got, want)
		}
	}
	for tag := range ltICUExceptions {
		if !differ[tag] {
			t.Errorf("exception %q is not in the reference", tag)
		}
	}
}

// ltTestSupported emulates Intl.Segmenter.supportedLocalesOf(tag) on
// langTagAvailable: ECMA-402 LookupSupportedLocales, whose
// BestAvailableLocale strips subtags from the right; "!" for a RangeError.
func ltTestSupported(tag string) string {
	t, ok := parseLangTag(tag)
	if !ok {
		return "!"
	}
	t.canonicalize()
	for candidate := t.noUnicodeExtension(); ; {
		if langTagAvailable(candidate) {
			return t.String()
		}
		pos := strings.LastIndexByte(candidate, '-')
		if pos < 0 {
			return ""
		}
		if pos >= 2 && candidate[pos-2] == '-' {
			pos -= 2
		}
		candidate = candidate[:pos]
	}
}

func TestLangTagAccessors(t *testing.T) {
	tag, ok := parseLangTag("ZH-hant-tw-1606NICT-u-CA-chinese-co-pinyin-kn-x-Priv")
	if !ok {
		t.Fatal("not parsed")
	}
	tag.canonicalize()
	for _, c := range [][2]string{
		{tag.String(), "zh-Hant-TW-1606nict-u-ca-chinese-co-pinyin-kn-x-priv"},
		{tag.baseName(), "zh-Hant-TW-1606nict"},
		{tag.language(), "zh"}, {tag.script(), "Hant"}, {tag.region(), "TW"}, {tag.variants(), "1606nict"},
		{tag.noUnicodeExtension(), "zh-Hant-TW-1606nict-x-priv"},
	} {
		if c[0] != c[1] {
			t.Errorf("got %q, want %q", c[0], c[1])
		}
	}
	for _, c := range []struct {
		key, value string
		ok         bool
	}{{"ca", "chinese", true}, {"co", "pinyin", true}, {"kn", "", true}, {"nu", "", false}, {"ch", "", false}} {
		if v, ok := tag.uKeyword(c.key); v != c.value || ok != c.ok {
			t.Errorf("uKeyword(%q) = %q, %v", c.key, v, ok)
		}
	}

	c := tag.clone()
	c.setUKeyword("co", "")
	c.setUKeyword("kn", "false")
	c.setUKeyword("nu", "hanidec")
	c.setUKeyword("ca", "gregory")
	c.setLanguage("YUE")
	c.setScript("hans")
	c.setRegion("cn")
	c.setVariants("")
	c.canonicalize()
	if got, want := c.String(), "yue-Hans-CN-u-ca-gregory-co-kn-false-nu-hanidec-x-priv"; got != want {
		t.Errorf("after the setters: got %q, want %q", got, want)
	}
	if got := tag.String(); got != "zh-Hant-TW-1606nict-u-ca-chinese-co-pinyin-kn-x-priv" {
		t.Errorf("clone shares state: %q", got)
	}
	c.setScript("")
	c.setRegion("")
	c.setVariants("Valencia-1994")
	c.canonicalize()
	if got, want := c.String(), "yue-1994-valencia-u-ca-gregory-co-kn-false-nu-hanidec-x-priv"; got != want {
		t.Errorf("after removing subtags: got %q, want %q", got, want)
	}

	plain, _ := parseLangTag("en-u-attr")
	plain.setUKeyword("ca", "buddhist")
	plain.setUKeyword("ca", "japanese")
	if got, want := plain.String(), "en-u-attr-ca-japanese"; got != want {
		t.Errorf("setUKeyword on attributes: got %q, want %q", got, want)
	}
	plain, _ = parseLangTag("en")
	plain.setUKeyword("kn", "")
	if got, want := plain.String(), "en-u-kn"; got != want {
		t.Errorf("setUKeyword without a -u- extension: got %q, want %q", got, want)
	}
	if got := plain.noUnicodeExtension(); got != "en" {
		t.Errorf("noUnicodeExtension: got %q", got)
	}
}

func TestLangTagPredicates(t *testing.T) {
	for _, c := range []struct {
		f    func(string) bool
		name string
		yes  []string
		no   []string
	}{
		{isLangTagLanguage, "language", []string{"en", "EN", "yue", "enochian", "abcdefgh"}, []string{"", "e", "root", "abcdefghi", "e1", "en-"}},
		{isLangTagScript, "script", []string{"Latn", "hans", "ZZZZ"}, []string{"", "Lat", "Latin", "La1n"}},
		{isLangTagRegion, "region", []string{"US", "us", "419", "001"}, []string{"", "U", "USA", "41", "4199", "1a"}},
		{isLangTagVariants, "variants", []string{"fonipa", "1996", "1996-fonipa", "ABCDEFGH"}, []string{"", "-", "abc", "1996-", "-1996", "a996", "fonipa-FONIPA", "1996--fonipa", "abcdefghi"}},
		{isLangTagType, "type", []string{"gregory", "islamic-civil", "abc", "12345678-abc"}, []string{"", "ab", "abcdefghi", "gregory-", "islamic--civil", "a_b"}},
	} {
		for _, s := range c.yes {
			if !c.f(s) {
				t.Errorf("%s(%q) = false", c.name, s)
			}
		}
		for _, s := range c.no {
			if c.f(s) {
				t.Errorf("%s(%q) = true", c.name, s)
			}
		}
	}
	for _, c := range []struct{ key, value, want string }{
		{"ca", "Ethiopic-Amete-Alem", "ethioaa"}, {"ca", "islamicc", "islamic-civil"}, {"ks", "PRIMARY", "level1"},
		{"tz", "Eire", "iedub"}, {"rg", "fi01", "axzzzz"}, {"sd", "no23", "no50"}, {"kn", "yes", ""},
		{"ca", "Gregory", "gregory"}, {"ms", "imperial", "uksystem"}, {"co", "phonebk", "phonebk"},
	} {
		if got := langTagCanonicalUValue(c.key, c.value); got != c.want {
			t.Errorf("langTagCanonicalUValue(%q, %q) = %q, want %q", c.key, c.value, got, c.want)
		}
	}
}

func TestLangTagAvailable(t *testing.T) {
	for _, c := range []struct {
		tag  string
		want bool
	}{
		{"en", true}, {"en-US", true}, {"en-001", true}, {"zh-Hant-TW", true}, {"zh-Hant", true}, {"zh-TW", true}, {"az-AZ", true}, {"sr-RS", true},
		{"sr-Latn", true}, {"sr-Latn-RS", true}, {"ca-ES-valencia", true}, {"nb", true}, {"no", true},
		{"es-419", true}, {"de-CH", true}, {"und", false}, {"root", false}, {"zxx", false}, {"en-ZZ", false},
		{"ife-TG", false}, {"en-US-posix", false}, {"x", false}, {"", false},
	} {
		if got := langTagAvailable(c.tag); got != c.want {
			t.Errorf("langTagAvailable(%q) = %v", c.tag, got)
		}
	}
}

// TestLangTagCLDRAliases runs every alias rule type and every likely-subtags
// key through canonicalize and maximize: each terminates, gives a valid tag
// and is idempotent.
func TestLangTagCLDRAliases(t *testing.T) {
	var inputs []string
	for _, table := range ltRuleTables {
		for rec := range strings.SplitSeq(strings.TrimSuffix(table, ";"), ";") {
			from, _, _ := strings.Cut(rec, ">")
			id := ltPositional(from)
			id.lang = cmp.Or(id.lang, "und")
			inputs = append(inputs, id.String())
		}
	}
	for _, e := range ltLikely() {
		k := uint32(e >> 32)
		id := ltID{lang: ltUnpackLang(k >> 16)}
		if s := k >> 8 & 0xFF; s != 0 {
			id.script = ltScriptAt(s)
		}
		if r := k & 0xFF; r != 0 {
			id.region = ltRegionAt(r)
		}
		inputs = append(inputs, id.String())
	}
	if len(inputs) < 8000 {
		t.Fatalf("only %d inputs", len(inputs))
	}
	for _, in := range inputs {
		out, ok := canonicalizeLangTag(in)
		if !ok {
			t.Errorf("%q: invalid", in)
			continue
		}
		if again, _ := canonicalizeLangTag(out); again != out {
			t.Errorf("%q → %q → %q", in, out, again)
		}
		tag, _ := parseLangTag(out)
		tag.maximize()
		if max, ok := canonicalizeLangTag(tag.String()); !ok || max != tag.String() {
			t.Errorf("%q: maximize gives %q, canonical %q", in, tag.String(), max)
		}
	}
}

// ltUnpackLang reverses ltPackLang.
func ltUnpackLang(c uint32) string {
	b := []byte{byte(c / 729), byte(c / 27 % 27), byte(c % 27)}
	if b[2] == 0 {
		b = b[:2]
	}
	for i := range b {
		b[i] += 'a' - 1
	}
	return string(b)
}

func TestLangTagAllocs(t *testing.T) {
	canonicalizeLangTag("en-US") // decode the tables
	for _, c := range []struct {
		in     string
		allocs float64 // canonicalizeLangTag
	}{
		{"en-US", 0}, {"zh-Hans-CN", 0}, {"ja-JP-u-ca-japanese", 0}, {"en-us", 1}, {"de-DE-u-co-phonebk-x-private", 0},
	} {
		if n := testing.AllocsPerRun(100, func() { canonicalizeLangTag(c.in) }); n != c.allocs {
			t.Errorf("canonicalizeLangTag(%q): %v allocations, want %v", c.in, n, c.allocs)
		}
		if n := testing.AllocsPerRun(100, func() {
			tag, _ := parseLangTag(c.in)
			tag.canonicalize()
			_ = tag.String()
		}); n > c.allocs+1 {
			t.Errorf("parseLangTag+canonicalize+String(%q): %v allocations, want at most %v", c.in, n, c.allocs+1)
		}
	}
	tag, _ := parseLangTag("zh-TW")
	tag.maximize() // decode the likely subtags
	for _, c := range []string{"en", "en-US", "zh-TW", "und-Hant"} {
		if n := testing.AllocsPerRun(100, func() {
			var tag langTag
			tag.parse(c)
			tag.maximize()
			tag.minimize()
		}); n != 0 {
			t.Errorf("maximize+minimize(%q): %v allocations", c, n)
		}
	}
}

func FuzzLangTag(f *testing.F) {
	for _, s := range []string{
		"en-US", "zh-Hans-CN", "ja-JP-u-ca-japanese", "sh", "ru-SU", "und-Armn-SU", "art-lojban", "hy-arevmda",
		"ja-latn-hepburn-heploc", "und-u-ca-ethiopic-amete-alem-kn-yes-rg-fi01-tz-eire", "en-t-iw-m0-names",
		"DE-T-M0-DIN-K0-QWERTZ", "cmn-hans-cn-u-ca-t-ca-x-t-u", "sl-rozaj-biske-1994", "en-b-foo-a-bar-x-u-foo",
		"en-u-attr2-attr1-ca-gregory-ca-buddhist-attr1", "und-Zzzz-ZZ", "zz-ZZ", "x-foo", "en-t-en-latn-gb-emodeng",
		"und-hepburn-heploc-fonipa", "sgn-GR-u-sd-grcrete", "en-0-abc-z-def", "cel-gaulish-x-c",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		tag, ok := parseLangTag(s)
		if !ok {
			if _, ok := canonicalizeLangTag(s); ok {
				t.Fatalf("%q: canonicalizeLangTag accepts what parseLangTag rejects", s)
			}
			return
		}
		tag.canonicalize()
		out := tag.String()
		again, ok := canonicalizeLangTag(out)
		if !ok || again != out {
			t.Fatalf("%q → %q → %q (valid %v)", s, out, again, ok)
		}
		if !strings.HasPrefix(out, tag.baseName()) {
			t.Fatalf("%q: baseName %q", out, tag.baseName())
		}
		for _, op := range []func(*langTag){(*langTag).maximize, (*langTag).minimize} {
			c := tag.clone()
			op(c)
			if _, ok := parseLangTag(c.String()); !ok {
				t.Fatalf("%q: %q is not valid", out, c.String())
			}
		}
	})
}

func BenchmarkLangTagCanonicalize(b *testing.B) {
	for _, s := range []string{"en-US", "zh-Hans-CN", "ja-JP-u-ca-japanese", "en-us", "zh-hant-tw-u-nu-hanidec-ca-chinese"} {
		b.Run(s, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				canonicalizeLangTag(s)
			}
		})
	}
}

func BenchmarkLangTagMaximize(b *testing.B) {
	for _, s := range []string{"en", "en-US", "zh-TW", "und-Hant"} {
		b.Run(s, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var tag langTag
				tag.parse(s)
				tag.maximize()
			}
		})
	}
}

func BenchmarkLangTagMinimize(b *testing.B) {
	for _, s := range []string{"en-Latn-US", "zh-Hant-TW"} {
		b.Run(s, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var tag langTag
				tag.parse(s)
				tag.minimize()
			}
		})
	}
}

// BenchmarkLangTagDecode measures the one-time decoding of the tables.
func BenchmarkLangTagDecode(b *testing.B) {
	b.Run("likely", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			ltDecodeLikely()
		}
	})
	b.Run("aliases", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			for i, table := range ltRuleTables {
				ltIndex(table, i, ",>-")
			}
			ltIndex(ltSubdivisionAliases, 0, ">")
			ltIndex(ltKeywordAliases, 0, ">")
		}
	})
	b.Run("available", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			ltIndex(ltAvailableLocales, 0, ":")
		}
	})
}

// The cases below are generated from test262 (045bf6f) test/intl402 by
// running the tests against a recording Intl: Intl/getCanonicalLocales,
// language-tags-*.js and Locale.

// ltTest262Tags maps the tags of Intl.getCanonicalLocales tests (and those
// harness/testIntl.js lists as invalid) to their canonical form, "" for a
// RangeError.
var ltTest262Tags = [...]struct{ in, want string }{
	// Intl/getCanonicalLocales/canonicalized-tags.js
	{"de", "de"},
	{"DE-de", "de-DE"},
	{"de-DE", "de-DE"},
	{"cmn", "zh"},
	{"CMN-hANS", "zh-Hans"},
	{"cmn-hans-cn", "zh-Hans-CN"},
	{"es-419", "es-419"},
	{"es-419-u-nu-latn", "es-419-u-nu-latn"},
	{"cmn-hans-cn-u-ca-t-ca-x-t-u", "zh-Hans-CN-t-ca-u-ca-x-t-u"},
	{"de-gregory-u-ca-gregory", "de-gregory-u-ca-gregory"},
	{"sgn-GR", "gss"},
	{"ji", "yi"},
	{"de-DD", "de-DE"},
	{"in", "id"},
	{"sr-cyrl-ekavsk", "sr-Cyrl-ekavsk"},
	{"en-ca-newfound", "en-CA-newfound"},
	{"sl-rozaj-biske-1994", "sl-1994-biske-rozaj"},
	{"da-u-attr", "da-u-attr"},
	{"da-u-attr-co-search", "da-u-attr-co-search"},
	// Intl/getCanonicalLocales/complex-language-subtag-replacement.js
	{"sh", "sr-Latn"},
	{"sh-Cyrl", "sr-Cyrl"},
	{"cnr", "sr-ME"},
	{"cnr-BA", "sr-BA"},
	// Intl/getCanonicalLocales/complex-region-subtag-replacement.js
	{"ru-SU", "ru-RU"},
	{"ru-810", "ru-RU"},
	{"en-SU", "en-RU"},
	{"en-810", "en-RU"},
	{"und-SU", "und-RU"},
	{"und-810", "und-RU"},
	{"und-Latn-SU", "und-Latn-RU"},
	{"und-Latn-810", "und-Latn-RU"},
	{"hy-SU", "hy-AM"},
	{"hy-810", "hy-AM"},
	{"und-Armn-SU", "und-Armn-AM"},
	{"und-Armn-810", "und-Armn-AM"},
	{"sr-CS", "sr-RS"},
	{"sr-Latn-CS", "sr-Latn-RS"},
	{"sr-Cyrl-CS", "sr-Cyrl-RS"},
	{"az-NT", "az-SA"},
	// Intl/getCanonicalLocales/elements-not-reordered.js
	{"zu", "zu"},
	{"af", "af"},
	// Intl/getCanonicalLocales/error-cases.js
	{"en-us-", ""},
	{"-en-us", ""},
	{"en-us-en-us", ""},
	{"--", ""},
	{"-", ""},
	{"", ""},
	{"-e-", ""},
	// Intl/getCanonicalLocales/grandfathered.js
	{"art-lojban", "jbo"},
	{"zh-guoyu", "zh"},
	{"zh-hakka", "hak"},
	{"zh-xiang", "hsn"},
	// Intl/getCanonicalLocales/invalid-tags.js
	{"i", ""},
	{"x", ""},
	{"u", ""},
	{"419", ""},
	{"u-nu-latn-cu-bob", ""},
	{"hans-cmn-cn", ""},
	{"cmn-hans-cn-u-u", ""},
	{"cmn-hans-cn-t-u-ca-u", ""},
	{"de-gregory-gregory", ""},
	{"*", ""},
	{"de-*", ""},
	{"\u4e2d\u6587", ""},
	{"en-\u00df", ""},
	{"\u0131d", ""},
	{"es-Latn-latn", ""},
	{"pl-PL-pl", ""},
	{"u-ca-gregory", ""},
	{"de-1996-1996", ""},
	{"pt-u-ca-gregory-u-nu-latn", ""},
	{"no-nyn", ""},
	{"i-klingon", ""},
	{"zh-hak-CN", ""},
	{"sgn-ils", ""},
	{"x-foo", ""},
	{"x-en-US-12345", ""},
	{"x-12345-12345-en-US", ""},
	{"x-en-US-12345-12345", ""},
	{"x-en-u-foo", ""},
	{"x-en-u-foo-u-bar", ""},
	{"x-u-foo", ""},
	{"de_DE", ""},
	{"DE_de", ""},
	{"cmn_Hans", ""},
	{"cmn-hans_cn", ""},
	{"es_419", ""},
	{"es-419-u-nu-latn-cu_bob", ""},
	{"i_klingon", ""},
	{"cmn-hans-cn-t-ca-u-ca-x_t-u", ""},
	{"enochian_enochian", ""},
	{"de-gregory_u-ca-gregory", ""},
	{"en\u0000", ""},
	{" en", ""},
	{"en ", ""},
	{"it-IT-Latn", ""},
	{"de-u", ""},
	{"de-u-", ""},
	{"de-u-ca-", ""},
	{"de-u-ca-gregory-", ""},
	{"si-x", ""},
	{"x-", ""},
	{"x-y-", ""},
	// Intl/getCanonicalLocales/main.js
	{"ab-cd", "ab-CD"},
	{"FF", "ff"},
	{"th-th-u-nu-thai", "th-TH-u-nu-thai"},
	// Intl/getCanonicalLocales/non-iana-canon.js
	{"mo", "ro"},
	{"es-ES-preeuro", "es-ES-preeuro"},
	{"uz-UZ-cyrillic", "uz-UZ-cyrillic"},
	{"posix", "posix"},
	{"hi-direct", "hi-direct"},
	{"zh-pinyin", "zh-pinyin"},
	{"zh-stroke", "zh-stroke"},
	{"aar-x-private", "aa-x-private"},
	{"heb-x-private", "he-x-private"},
	{"de-u-kf", "de-u-kf"},
	{"ces", "cs"},
	{"hy-arevela", "hy"},
	{"hy-arevmda", "hyw"},
	// Intl/getCanonicalLocales/overriden-arg-length.js
	{"en-US", "en-US"},
	// Intl/getCanonicalLocales/preferred-grandfathered.js
	{"cel-gaulish", "xtg"},
	// Intl/getCanonicalLocales/preferred-variant.js
	{"ja-latn-hepburn-heploc", "ja-Latn-alalc97"},
	// Intl/getCanonicalLocales/transformed-ext-canonical.js
	{"sl-t-sl-rozaj-biske-1994", "sl-t-sl-1994-biske-rozaj"},
	{"DE-T-M0-DIN-K0-QWERTZ", "de-t-k0-qwertz-m0-din"},
	{"en-t-m0-true", "en-t-m0-true"},
	{"en-t-iw", "en-t-he"},
	{"und-Latn-t-und-hani-m0-names", "und-Latn-t-und-hani-m0-prprname"},
	// Intl/getCanonicalLocales/transformed-ext-invalid.js
	{"en-t", ""},
	{"en-t-a", ""},
	{"en-t-x", ""},
	{"en-t-0", ""},
	{"en-t-", ""},
	{"en-t-en-", ""},
	{"en-t-0x-", ""},
	{"en-t-root", ""},
	{"en-t-abcdefghi", ""},
	{"en-t-ar-aao", ""},
	{"en-t-en-lat0", ""},
	{"en-t-en-latn-latn", ""},
	{"en-t-en-0", ""},
	{"en-t-en-00", ""},
	{"en-t-en-0x", ""},
	{"en-t-en-x0", ""},
	{"en-t-en-latn-0", ""},
	{"en-t-en-latn-00", ""},
	{"en-t-en-latn-xyz", ""},
	{"en-t-en-abcdefghi", ""},
	{"en-t-en-latn-gb-ab", ""},
	{"en-t-en-latn-gb-abc", ""},
	{"en-t-en-latn-gb-abcd", ""},
	{"en-t-en-latn-gb-abcdefghi", ""},
	{"en-t-d0", ""},
	{"en-t-d0-m0", ""},
	{"en-t-d0-x-private", ""},
	// Intl/getCanonicalLocales/transformed-ext-valid.js
	{"en-t-en", "en-t-en"},
	{"en-t-en-i0-handwrit", "en-t-en-i0-handwrit"},
	{"en-t-en-s0-accents-publish", "en-t-en-s0-accents-publish"},
	{"en-t-en-latn", "en-t-en-latn"},
	{"en-t-en-latn-i0-handwrit", "en-t-en-latn-i0-handwrit"},
	{"en-t-en-latn-s0-accents-publish", "en-t-en-latn-s0-accents-publish"},
	{"en-t-en-ca", "en-t-en-ca"},
	{"en-t-en-ca-i0-handwrit", "en-t-en-ca-i0-handwrit"},
	{"en-t-en-ca-s0-accents-publish", "en-t-en-ca-s0-accents-publish"},
	{"en-t-en-latn-ca", "en-t-en-latn-ca"},
	{"en-t-en-latn-ca-i0-handwrit", "en-t-en-latn-ca-i0-handwrit"},
	{"en-t-en-latn-ca-s0-accents-publish", "en-t-en-latn-ca-s0-accents-publish"},
	{"en-t-en-emodeng", "en-t-en-emodeng"},
	{"en-t-en-emodeng-i0-handwrit", "en-t-en-emodeng-i0-handwrit"},
	{"en-t-en-emodeng-s0-accents-publish", "en-t-en-emodeng-s0-accents-publish"},
	{"en-t-en-latn-emodeng", "en-t-en-latn-emodeng"},
	{"en-t-en-latn-emodeng-i0-handwrit", "en-t-en-latn-emodeng-i0-handwrit"},
	{"en-t-en-latn-emodeng-s0-accents-publish", "en-t-en-latn-emodeng-s0-accents-publish"},
	{"en-t-en-ca-emodeng", "en-t-en-ca-emodeng"},
	{"en-t-en-ca-emodeng-i0-handwrit", "en-t-en-ca-emodeng-i0-handwrit"},
	{"en-t-en-ca-emodeng-s0-accents-publish", "en-t-en-ca-emodeng-s0-accents-publish"},
	{"en-t-en-latn-ca-emodeng", "en-t-en-latn-ca-emodeng"},
	{"en-t-en-latn-ca-emodeng-i0-handwrit", "en-t-en-latn-ca-emodeng-i0-handwrit"},
	{"en-t-en-latn-ca-emodeng-s0-accents-publish", "en-t-en-latn-ca-emodeng-s0-accents-publish"},
	{"en-t-d0-ascii", "en-t-d0-ascii"},
	{"en-t-d0-ascii-i0-handwrit", "en-t-d0-ascii-i0-handwrit"},
	{"en-t-d0-ascii-s0-accents-publish", "en-t-d0-ascii-s0-accents-publish"},
	// Intl/getCanonicalLocales/unicode-ext-canonicalize-calendar.js
	{"und-u-ca-ethiopic-amete-alem", "und-u-ca-ethioaa"},
	{"und-u-ca-islamicc", "und-u-ca-islamic-civil"},
	// Intl/getCanonicalLocales/unicode-ext-canonicalize-col-strength.js
	{"und-u-ks-primary", "und-u-ks-level1"},
	{"und-u-ks-tertiary", "und-u-ks-level3"},
	// Intl/getCanonicalLocales/unicode-ext-canonicalize-measurement-system.js
	{"und-u-ms-imperial", "und-u-ms-uksystem"},
	// Intl/getCanonicalLocales/unicode-ext-canonicalize-region.js
	{"und-u-rg-no23", "und-u-rg-no50"},
	{"und-u-rg-cn11", "und-u-rg-cnbj"},
	{"und-u-rg-cz10a", "und-u-rg-cz110"},
	{"und-u-rg-fra", "und-u-rg-frges"},
	{"und-u-rg-frg", "und-u-rg-frges"},
	{"und-u-rg-lud", "und-u-rg-lucl"},
	// Intl/getCanonicalLocales/unicode-ext-canonicalize-subdivision.js
	{"und-NO-u-sd-no23", "und-NO-u-sd-no50"},
	{"und-CN-u-sd-cn11", "und-CN-u-sd-cnbj"},
	{"und-CZ-u-sd-cz10a", "und-CZ-u-sd-cz110"},
	{"und-FR-u-sd-fra", "und-FR-u-sd-frges"},
	{"und-FR-u-sd-frg", "und-FR-u-sd-frges"},
	{"und-LU-u-sd-lud", "und-LU-u-sd-lucl"},
	// Intl/getCanonicalLocales/unicode-ext-canonicalize-timezone.js
	{"und-u-tz-cnckg", "und-u-tz-cnsha"},
	{"und-u-tz-eire", "und-u-tz-iedub"},
	{"und-u-tz-est", "und-u-tz-papty"},
	{"und-u-tz-gmt0", "und-u-tz-gmt"},
	{"und-u-tz-uct", "und-u-tz-utc"},
	{"und-u-tz-zulu", "und-u-tz-utc"},
	// Intl/getCanonicalLocales/unicode-ext-canonicalize-yes-to-true.js
	{"und-u-kb-yes", "und-u-kb"},
	{"und-u-kc-yes", "und-u-kc"},
	{"und-u-kh-yes", "und-u-kh"},
	{"und-u-kk-yes", "und-u-kk"},
	{"und-u-kn-yes", "und-u-kn"},
	{"und-u-ka-yes", "und-u-ka-yes"},
	{"und-u-kf-yes", "und-u-kf-yes"},
	{"und-u-kr-yes", "und-u-kr-yes"},
	{"und-u-ks-yes", "und-u-ks-yes"},
	{"und-u-kv-yes", "und-u-kv-yes"},
	// Intl/getCanonicalLocales/unicode-ext-key-with-digit.js
	{"en-u-c0", ""},
	{"en-u-00", ""},
	{"en-u-0c", "en-u-0c"},
	// Intl/getCanonicalLocales/weird-cases.js
	{"en-x-u-foo", "en-x-u-foo"},
	{"en-a-bar-x-u-foo", "en-a-bar-x-u-foo"},
	{"en-x-u-foo-a-bar", "en-x-u-foo-a-bar"},
	{"en-a-bar-u-baz-x-u-foo", "en-a-bar-u-baz-x-u-foo"},
	// Locale/canonicalize-locale-list-take-locale.js
	{"zh", "zh"},
}

// ltTest262Locales are the Intl.Locale cases: the tag, the constructor's
// options ("key=value" pairs joined by ';'), the methods and getter applied
// in turn ('.'-joined, none for the constructor alone) and the result: "!"
// for a RangeError, "-" for undefined.
var ltTest262Locales = [...]struct{ tag, options, chain, want string }{
	// Locale/constructor-apply-options-canonicalizes-twice.js
	{"und-Armn-SU", "language=ru", "toString", "ru-Armn-AM"},
	// Locale/constructor-locale-object.js
	{"en-US", "", "toString", "en-US"},
	{"zh-u-co-unihan", "", "toString", "zh-u-co-unihan"},
	{"zh-u-co-unihan", "", "collation", "unihan"},
	// Locale/constructor-non-iana-canon.js
	{"mo", "", "toString", "ro"},
	{"mo", "", "maximize.toString", "ro-Latn-RO"},
	{"mo", "", "minimize.toString", "ro"},
	{"es-ES-preeuro", "", "toString", "es-ES-preeuro"},
	{"es-ES-preeuro", "", "maximize.toString", "es-Latn-ES-preeuro"},
	{"es-ES-preeuro", "", "minimize.toString", "es-preeuro"},
	{"uz-UZ-cyrillic", "", "toString", "uz-UZ-cyrillic"},
	{"uz-UZ-cyrillic", "", "maximize.toString", "uz-Latn-UZ-cyrillic"},
	{"uz-UZ-cyrillic", "", "minimize.toString", "uz-cyrillic"},
	{"posix", "", "toString", "posix"},
	{"posix", "", "maximize.toString", "posix"},
	{"posix", "", "minimize.toString", "posix"},
	{"hi-direct", "", "toString", "hi-direct"},
	{"hi-direct", "", "maximize.toString", "hi-Deva-IN-direct"},
	{"hi-direct", "", "minimize.toString", "hi-direct"},
	{"zh-pinyin", "", "toString", "zh-pinyin"},
	{"zh-pinyin", "", "maximize.toString", "zh-Hans-CN-pinyin"},
	{"zh-pinyin", "", "minimize.toString", "zh-pinyin"},
	{"zh-stroke", "", "toString", "zh-stroke"},
	{"zh-stroke", "", "maximize.toString", "zh-Hans-CN-stroke"},
	{"zh-stroke", "", "minimize.toString", "zh-stroke"},
	{"aar-x-private", "", "toString", "aa-x-private"},
	{"aar-x-private", "", "maximize.toString", "aa-Latn-ET-x-private"},
	{"aar-x-private", "", "minimize.toString", "aa-x-private"},
	{"heb-x-private", "", "toString", "he-x-private"},
	{"heb-x-private", "", "maximize.toString", "he-Hebr-IL-x-private"},
	{"heb-x-private", "", "minimize.toString", "he-x-private"},
	{"de-u-kf", "", "toString", "de-u-kf"},
	{"de-u-kf", "", "maximize.toString", "de-Latn-DE-u-kf"},
	{"de-u-kf", "", "minimize.toString", "de-u-kf"},
	{"ces", "", "toString", "cs"},
	{"ces", "", "maximize.toString", "cs-Latn-CZ"},
	{"ces", "", "minimize.toString", "cs"},
	{"hy-arevela", "", "toString", "hy"},
	{"hy-arevela", "", "maximize.toString", "hy-Armn-AM"},
	{"hy-arevela", "", "minimize.toString", "hy"},
	{"hy-arevmda", "", "toString", "hyw"},
	{"hy-arevmda", "", "maximize.toString", "hyw-Armn-AM"},
	{"hy-arevmda", "", "minimize.toString", "hyw"},
	// Locale/constructor-options-calendar-invalid.js
	{"en", "calendar=", "", "!"},
	{"en", "calendar=a", "", "!"},
	{"en", "calendar=ab", "", "!"},
	{"en", "calendar=abcdefghi", "", "!"},
	{"en", "calendar=abc-abcdefghi", "", "!"},
	// Locale/constructor-options-calendar-valid.js
	{"en", "calendar=abc", "toString", "en-u-ca-abc"},
	{"en-u-ca-gregory", "calendar=abc", "toString", "en-u-ca-abc"},
	{"en", "calendar=abcd", "toString", "en-u-ca-abcd"},
	{"en-u-ca-gregory", "calendar=abcd", "toString", "en-u-ca-abcd"},
	{"en", "calendar=abcde", "toString", "en-u-ca-abcde"},
	{"en-u-ca-gregory", "calendar=abcde", "toString", "en-u-ca-abcde"},
	{"en", "calendar=abcdef", "toString", "en-u-ca-abcdef"},
	{"en-u-ca-gregory", "calendar=abcdef", "toString", "en-u-ca-abcdef"},
	{"en", "calendar=abcdefg", "toString", "en-u-ca-abcdefg"},
	{"en-u-ca-gregory", "calendar=abcdefg", "toString", "en-u-ca-abcdefg"},
	{"en", "calendar=abcdefgh", "toString", "en-u-ca-abcdefgh"},
	{"en-u-ca-gregory", "calendar=abcdefgh", "toString", "en-u-ca-abcdefgh"},
	{"en", "calendar=12345678", "toString", "en-u-ca-12345678"},
	{"en-u-ca-gregory", "calendar=12345678", "toString", "en-u-ca-12345678"},
	{"en", "calendar=1234abcd", "toString", "en-u-ca-1234abcd"},
	{"en-u-ca-gregory", "calendar=1234abcd", "toString", "en-u-ca-1234abcd"},
	{"en", "calendar=1234abcd-abc123", "toString", "en-u-ca-1234abcd-abc123"},
	{"en-u-ca-gregory", "calendar=1234abcd-abc123", "toString", "en-u-ca-1234abcd-abc123"},
	// Locale/constructor-options-canonicalized.js
	{"en-u-ca-islamic-civil", "", "calendar", "islamic-civil"},
	{"en", "calendar=islamic-civil", "calendar", "islamic-civil"},
	{"en-u-ca-islamicc", "", "calendar", "islamic-civil"},
	{"en", "calendar=islamicc", "calendar", "islamic-civil"},
	{"en-u-ca-ethioaa", "", "calendar", "ethioaa"},
	{"en", "calendar=ethioaa", "calendar", "ethioaa"},
	{"en-u-ca-ethiopic-amete-alem", "", "calendar", "ethioaa"},
	{"en", "calendar=ethiopic-amete-alem", "calendar", "ethioaa"},
	// Locale/constructor-options-casefirst-invalid.js
	{"en", "caseFirst=", "", "!"},
	{"en", "caseFirst=u", "", "!"},
	{"en", "caseFirst=Upper", "", "!"},
	{"en", "caseFirst=upper\u0000", "", "!"},
	{"en", "caseFirst=uppercase", "", "!"},
	{"en", "caseFirst=true", "", "!"},
	// Locale/constructor-options-casefirst-valid.js
	{"en", "caseFirst=upper", "toString", "en-u-kf-upper"},
	{"en-u-kf-lower", "caseFirst=upper", "toString", "en-u-kf-upper"},
	{"en-u-kf-lower", "caseFirst=upper", "caseFirst", "upper"},
	{"en", "caseFirst=lower", "toString", "en-u-kf-lower"},
	{"en-u-kf-lower", "caseFirst=lower", "toString", "en-u-kf-lower"},
	{"en-u-kf-lower", "caseFirst=lower", "caseFirst", "lower"},
	{"en", "caseFirst=false", "toString", "en-u-kf-false"},
	{"en-u-kf-lower", "caseFirst=false", "toString", "en-u-kf-false"},
	{"en-u-kf-lower", "caseFirst=false", "caseFirst", "false"},
	// Locale/constructor-options-collation-invalid.js
	{"en", "collation=", "", "!"},
	{"en", "collation=a", "", "!"},
	{"en", "collation=ab", "", "!"},
	{"en", "collation=abcdefghi", "", "!"},
	{"en", "collation=abc-abcdefghi", "", "!"},
	// Locale/constructor-options-collation-valid.js
	{"en", "collation=abc", "toString", "en-u-co-abc"},
	{"en-u-co-gregory", "collation=abc", "toString", "en-u-co-abc"},
	{"en", "collation=abcd", "toString", "en-u-co-abcd"},
	{"en-u-co-gregory", "collation=abcd", "toString", "en-u-co-abcd"},
	{"en", "collation=abcde", "toString", "en-u-co-abcde"},
	{"en-u-co-gregory", "collation=abcde", "toString", "en-u-co-abcde"},
	{"en", "collation=abcdef", "toString", "en-u-co-abcdef"},
	{"en-u-co-gregory", "collation=abcdef", "toString", "en-u-co-abcdef"},
	{"en", "collation=abcdefg", "toString", "en-u-co-abcdefg"},
	{"en-u-co-gregory", "collation=abcdefg", "toString", "en-u-co-abcdefg"},
	{"en", "collation=abcdefgh", "toString", "en-u-co-abcdefgh"},
	{"en-u-co-gregory", "collation=abcdefgh", "toString", "en-u-co-abcdefgh"},
	{"en", "collation=12345678", "toString", "en-u-co-12345678"},
	{"en-u-co-gregory", "collation=12345678", "toString", "en-u-co-12345678"},
	{"en", "collation=1234abcd", "toString", "en-u-co-1234abcd"},
	{"en-u-co-gregory", "collation=1234abcd", "toString", "en-u-co-1234abcd"},
	{"en", "collation=1234abcd-abc123", "toString", "en-u-co-1234abcd-abc123"},
	{"en-u-co-gregory", "collation=1234abcd-abc123", "toString", "en-u-co-1234abcd-abc123"},
	// Locale/constructor-options-firstDayOfWeek-invalid.js
	{"en", "firstDayOfWeek=", "", "!"},
	{"en", "firstDayOfWeek=m", "", "!"},
	{"en", "firstDayOfWeek=mo", "", "!"},
	{"en", "firstDayOfWeek=longerThan8Chars", "", "!"},
	{"en", "firstDayOfWeek=abc\u0000abc", "", "!"},
	{"en", "firstDayOfWeek=abc?", "", "!"},
	{"en", "firstDayOfWeek=\u00e4\u00f6\u00fc", "", "!"},
	{"en", "firstDayOfWeek=\u6161bc", "", "!"},
	{"en", "firstDayOfWeek=8", "", "!"},
	{"en", "firstDayOfWeek=9", "", "!"},
	{"en", "firstDayOfWeek=10", "", "!"},
	{"en", "firstDayOfWeek=-1", "", "!"},
	{"en", "firstDayOfWeek=-10", "", "!"},
	{"en", "firstDayOfWeek=-100", "", "!"},
	{"en", "firstDayOfWeek=-1000", "", "!"},
	// Locale/constructor-options-firstDayOfWeek-valid.js
	{"en", "firstDayOfWeek=mon", "toString", "en-u-fw-mon"},
	{"en-u-fw-WED", "firstDayOfWeek=mon", "toString", "en-u-fw-mon"},
	{"en", "firstDayOfWeek=MON", "toString", "en-u-fw-mon"},
	{"en", "firstDayOfWeek=tue", "toString", "en-u-fw-tue"},
	{"en-u-fw-WED", "firstDayOfWeek=tue", "toString", "en-u-fw-tue"},
	{"en", "firstDayOfWeek=TUE", "toString", "en-u-fw-tue"},
	{"en", "firstDayOfWeek=wed", "toString", "en-u-fw-wed"},
	{"en-u-fw-WED", "firstDayOfWeek=wed", "toString", "en-u-fw-wed"},
	{"en", "firstDayOfWeek=WED", "toString", "en-u-fw-wed"},
	{"en", "firstDayOfWeek=thu", "toString", "en-u-fw-thu"},
	{"en-u-fw-WED", "firstDayOfWeek=thu", "toString", "en-u-fw-thu"},
	{"en", "firstDayOfWeek=THU", "toString", "en-u-fw-thu"},
	{"en", "firstDayOfWeek=fri", "toString", "en-u-fw-fri"},
	{"en-u-fw-WED", "firstDayOfWeek=fri", "toString", "en-u-fw-fri"},
	{"en", "firstDayOfWeek=FRI", "toString", "en-u-fw-fri"},
	{"en", "firstDayOfWeek=sat", "toString", "en-u-fw-sat"},
	{"en-u-fw-WED", "firstDayOfWeek=sat", "toString", "en-u-fw-sat"},
	{"en", "firstDayOfWeek=SAT", "toString", "en-u-fw-sat"},
	{"en", "firstDayOfWeek=sun", "toString", "en-u-fw-sun"},
	{"en-u-fw-WED", "firstDayOfWeek=sun", "toString", "en-u-fw-sun"},
	{"en", "firstDayOfWeek=SUN", "toString", "en-u-fw-sun"},
	{"en", "firstDayOfWeek=1", "toString", "en-u-fw-mon"},
	{"en-u-fw-WED", "firstDayOfWeek=1", "toString", "en-u-fw-mon"},
	{"en", "firstDayOfWeek=2", "toString", "en-u-fw-tue"},
	{"en-u-fw-WED", "firstDayOfWeek=2", "toString", "en-u-fw-tue"},
	{"en", "firstDayOfWeek=3", "toString", "en-u-fw-wed"},
	{"en-u-fw-WED", "firstDayOfWeek=3", "toString", "en-u-fw-wed"},
	{"en", "firstDayOfWeek=4", "toString", "en-u-fw-thu"},
	{"en-u-fw-WED", "firstDayOfWeek=4", "toString", "en-u-fw-thu"},
	{"en", "firstDayOfWeek=5", "toString", "en-u-fw-fri"},
	{"en-u-fw-WED", "firstDayOfWeek=5", "toString", "en-u-fw-fri"},
	{"en", "firstDayOfWeek=6", "toString", "en-u-fw-sat"},
	{"en-u-fw-WED", "firstDayOfWeek=6", "toString", "en-u-fw-sat"},
	{"en", "firstDayOfWeek=7", "toString", "en-u-fw-sun"},
	{"en-u-fw-WED", "firstDayOfWeek=7", "toString", "en-u-fw-sun"},
	{"en", "firstDayOfWeek=0", "toString", "en-u-fw-sun"},
	{"en-u-fw-WED", "firstDayOfWeek=0", "toString", "en-u-fw-sun"},
	{"en", "firstDayOfWeek=100", "toString", "en-u-fw-100"},
	{"en-u-fw-WED", "firstDayOfWeek=100", "toString", "en-u-fw-100"},
	{"en", "firstDayOfWeek=1000", "toString", "en-u-fw-1000"},
	{"en-u-fw-WED", "firstDayOfWeek=1000", "toString", "en-u-fw-1000"},
	{"en", "firstDayOfWeek=NAN", "toString", "en-u-fw-nan"},
	{"en", "firstDayOfWeek=INFINITY", "toString", "en-u-fw-infinity"},
	{"en", "firstDayOfWeek=true", "toString", "en-u-fw"},
	{"en-u-fw-WED", "firstDayOfWeek=true", "toString", "en-u-fw"},
	{"en", "firstDayOfWeek=TRUE", "toString", "en-u-fw"},
	{"en", "firstDayOfWeek=false", "toString", "en-u-fw-false"},
	{"en-u-fw-WED", "firstDayOfWeek=false", "toString", "en-u-fw-false"},
	{"en", "firstDayOfWeek=FALSE", "toString", "en-u-fw-false"},
	{"en", "firstDayOfWeek=NULL", "toString", "en-u-fw-null"},
	{"en", "firstDayOfWeek=yes", "toString", "en-u-fw-yes"},
	{"en-u-fw-WED", "firstDayOfWeek=yes", "toString", "en-u-fw-yes"},
	{"en", "firstDayOfWeek=YES", "toString", "en-u-fw-yes"},
	{"en", "firstDayOfWeek=primidi", "toString", "en-u-fw-primidi"},
	{"en-u-fw-WED", "firstDayOfWeek=primidi", "toString", "en-u-fw-primidi"},
	{"en", "firstDayOfWeek=PRIMIDI", "toString", "en-u-fw-primidi"},
	{"en", "firstDayOfWeek=duodi", "toString", "en-u-fw-duodi"},
	{"en-u-fw-WED", "firstDayOfWeek=duodi", "toString", "en-u-fw-duodi"},
	{"en", "firstDayOfWeek=DUODI", "toString", "en-u-fw-duodi"},
	{"en", "firstDayOfWeek=tridi", "toString", "en-u-fw-tridi"},
	{"en-u-fw-WED", "firstDayOfWeek=tridi", "toString", "en-u-fw-tridi"},
	{"en", "firstDayOfWeek=TRIDI", "toString", "en-u-fw-tridi"},
	{"en", "firstDayOfWeek=quartidi", "toString", "en-u-fw-quartidi"},
	{"en-u-fw-WED", "firstDayOfWeek=quartidi", "toString", "en-u-fw-quartidi"},
	{"en", "firstDayOfWeek=QUARTIDI", "toString", "en-u-fw-quartidi"},
	{"en", "firstDayOfWeek=quintidi", "toString", "en-u-fw-quintidi"},
	{"en-u-fw-WED", "firstDayOfWeek=quintidi", "toString", "en-u-fw-quintidi"},
	{"en", "firstDayOfWeek=QUINTIDI", "toString", "en-u-fw-quintidi"},
	{"en", "firstDayOfWeek=sextidi", "toString", "en-u-fw-sextidi"},
	{"en-u-fw-WED", "firstDayOfWeek=sextidi", "toString", "en-u-fw-sextidi"},
	{"en", "firstDayOfWeek=SEXTIDI", "toString", "en-u-fw-sextidi"},
	{"en", "firstDayOfWeek=septidi", "toString", "en-u-fw-septidi"},
	{"en-u-fw-WED", "firstDayOfWeek=septidi", "toString", "en-u-fw-septidi"},
	{"en", "firstDayOfWeek=SEPTIDI", "toString", "en-u-fw-septidi"},
	{"en", "firstDayOfWeek=octidi", "toString", "en-u-fw-octidi"},
	{"en-u-fw-WED", "firstDayOfWeek=octidi", "toString", "en-u-fw-octidi"},
	{"en", "firstDayOfWeek=OCTIDI", "toString", "en-u-fw-octidi"},
	{"en", "firstDayOfWeek=nonidi", "toString", "en-u-fw-nonidi"},
	{"en-u-fw-WED", "firstDayOfWeek=nonidi", "toString", "en-u-fw-nonidi"},
	{"en", "firstDayOfWeek=NONIDI", "toString", "en-u-fw-nonidi"},
	{"en", "firstDayOfWeek=decadi", "toString", "en-u-fw-decadi"},
	{"en-u-fw-WED", "firstDayOfWeek=decadi", "toString", "en-u-fw-decadi"},
	{"en", "firstDayOfWeek=DECADI", "toString", "en-u-fw-decadi"},
	{"en", "firstDayOfWeek=frank", "toString", "en-u-fw-frank"},
	{"en-u-fw-WED", "firstDayOfWeek=frank", "toString", "en-u-fw-frank"},
	{"en", "firstDayOfWeek=FRANK", "toString", "en-u-fw-frank"},
	{"en", "firstDayOfWeek=yungfong", "toString", "en-u-fw-yungfong"},
	{"en-u-fw-WED", "firstDayOfWeek=yungfong", "toString", "en-u-fw-yungfong"},
	{"en", "firstDayOfWeek=YUNGFONG", "toString", "en-u-fw-yungfong"},
	{"en", "firstDayOfWeek=yung-fong", "toString", "en-u-fw-yung-fong"},
	{"en-u-fw-WED", "firstDayOfWeek=yung-fong", "toString", "en-u-fw-yung-fong"},
	{"en", "firstDayOfWeek=YUNG-FONG", "toString", "en-u-fw-yung-fong"},
	{"en", "firstDayOfWeek=tang", "toString", "en-u-fw-tang"},
	{"en-u-fw-WED", "firstDayOfWeek=tang", "toString", "en-u-fw-tang"},
	{"en", "firstDayOfWeek=TANG", "toString", "en-u-fw-tang"},
	{"en", "firstDayOfWeek=frank-yung-fong-tang", "toString", "en-u-fw-frank-yung-fong-tang"},
	{"en-u-fw-WED", "firstDayOfWeek=frank-yung-fong-tang", "toString", "en-u-fw-frank-yung-fong-tang"},
	{"en", "firstDayOfWeek=FRANK-YUNG-FONG-TANG", "toString", "en-u-fw-frank-yung-fong-tang"},
	// Locale/constructor-options-hourcycle-invalid.js
	{"en", "hourCycle=", "", "!"},
	{"en", "hourCycle=h", "", "!"},
	{"en", "hourCycle=h00", "", "!"},
	{"en", "hourCycle=h01", "", "!"},
	{"en", "hourCycle=h10", "", "!"},
	{"en", "hourCycle=h13", "", "!"},
	{"en", "hourCycle=h22", "", "!"},
	{"en", "hourCycle=h25", "", "!"},
	{"en", "hourCycle=h48", "", "!"},
	{"en", "hourCycle=h012", "", "!"},
	{"en", "hourCycle=h120", "", "!"},
	{"en", "hourCycle=h12\u0000", "", "!"},
	{"en", "hourCycle=H12", "", "!"},
	// Locale/constructor-options-hourcycle-valid.js
	{"en", "hourCycle=h11", "toString", "en-u-hc-h11"},
	{"en-u-hc-h00", "hourCycle=h11", "toString", "en-u-hc-h11"},
	{"en-u-hc-h12", "hourCycle=h11", "toString", "en-u-hc-h11"},
	{"en-u-hc-h00", "hourCycle=h11", "hourCycle", "h11"},
	{"en", "hourCycle=h12", "toString", "en-u-hc-h12"},
	{"en-u-hc-h00", "hourCycle=h12", "toString", "en-u-hc-h12"},
	{"en-u-hc-h12", "hourCycle=h12", "toString", "en-u-hc-h12"},
	{"en-u-hc-h00", "hourCycle=h12", "hourCycle", "h12"},
	{"en", "hourCycle=h23", "toString", "en-u-hc-h23"},
	{"en-u-hc-h00", "hourCycle=h23", "toString", "en-u-hc-h23"},
	{"en-u-hc-h12", "hourCycle=h23", "toString", "en-u-hc-h23"},
	{"en-u-hc-h00", "hourCycle=h23", "hourCycle", "h23"},
	{"en", "hourCycle=h24", "toString", "en-u-hc-h24"},
	{"en-u-hc-h00", "hourCycle=h24", "toString", "en-u-hc-h24"},
	{"en-u-hc-h12", "hourCycle=h24", "toString", "en-u-hc-h24"},
	{"en-u-hc-h00", "hourCycle=h24", "hourCycle", "h24"},
	// Locale/constructor-options-language-grandfathered.js
	{"nb", "language=no-bok", "", "!"},
	{"nb", "language=no-bok;region=NO", "", "!"},
	// Locale/constructor-options-language-invalid.js
	{"en", "language=", "", "!"},
	{"en", "language=a", "", "!"},
	{"en", "language=ab7", "", "!"},
	{"en", "language=notalanguage", "", "!"},
	{"en", "language=undefined", "", "!"},
	{"en", "language=root", "", "!"},
	{"en", "language=fr-Latn", "", "!"},
	{"en", "language=fr-FR", "", "!"},
	{"en", "language=sa-vaidika", "", "!"},
	{"en", "language=fr-a-asdf", "", "!"},
	{"en", "language=fr-x-private", "", "!"},
	{"en", "language=i-klingon", "", "!"},
	{"en", "language=zh-min", "", "!"},
	{"en", "language=zh-min-nan", "", "!"},
	{"en", "language=abcd-US", "", "!"},
	{"en", "language=abcde-US", "", "!"},
	{"en", "language=abcdef-US", "", "!"},
	{"en", "language=abcdefg-US", "", "!"},
	{"en", "language=abcdefgh-US", "", "!"},
	{"en", "language=7", "", "!"},
	// Locale/constructor-options-language-valid.js
	{"en", "language=zh-cmn", "", "!"},
	{"en-US", "language=zh-cmn", "", "!"},
	{"en-els", "language=zh-cmn", "", "!"},
	{"en", "language=ZH-CMN", "", "!"},
	{"en-US", "language=ZH-CMN", "", "!"},
	{"en-els", "language=ZH-CMN", "", "!"},
	{"en", "language=abcd", "", "!"},
	{"en-US", "language=abcd", "", "!"},
	{"en-els", "language=abcd", "", "!"},
	// Locale/constructor-options-language-valid-undefined.js
	{"en", "", "toString", "en"},
	{"en-els", "", "", "!"},
	// Locale/constructor-options-numberingsystem-invalid.js
	{"en", "numberingSystem=", "", "!"},
	{"en", "numberingSystem=a", "", "!"},
	{"en", "numberingSystem=ab", "", "!"},
	{"en", "numberingSystem=abcdefghi", "", "!"},
	{"en", "numberingSystem=abc-abcdefghi", "", "!"},
	{"en", "numberingSystem=!invalid!", "", "!"},
	{"en", "numberingSystem=-latn-", "", "!"},
	{"en", "numberingSystem=latn-", "", "!"},
	{"en", "numberingSystem=latn--", "", "!"},
	{"en", "numberingSystem=latn-ca", "", "!"},
	{"en", "numberingSystem=latn-ca-", "", "!"},
	{"en", "numberingSystem=latn-ca-gregory", "", "!"},
	// Locale/constructor-options-numberingsystem-valid.js
	{"en", "numberingSystem=abc", "toString", "en-u-nu-abc"},
	{"en-u-nu-latn", "numberingSystem=abc", "toString", "en-u-nu-abc"},
	{"en", "numberingSystem=abcd", "toString", "en-u-nu-abcd"},
	{"en-u-nu-latn", "numberingSystem=abcd", "toString", "en-u-nu-abcd"},
	{"en", "numberingSystem=abcde", "toString", "en-u-nu-abcde"},
	{"en-u-nu-latn", "numberingSystem=abcde", "toString", "en-u-nu-abcde"},
	{"en", "numberingSystem=abcdef", "toString", "en-u-nu-abcdef"},
	{"en-u-nu-latn", "numberingSystem=abcdef", "toString", "en-u-nu-abcdef"},
	{"en", "numberingSystem=abcdefg", "toString", "en-u-nu-abcdefg"},
	{"en-u-nu-latn", "numberingSystem=abcdefg", "toString", "en-u-nu-abcdefg"},
	{"en", "numberingSystem=abcdefgh", "toString", "en-u-nu-abcdefgh"},
	{"en-u-nu-latn", "numberingSystem=abcdefgh", "toString", "en-u-nu-abcdefgh"},
	{"en", "numberingSystem=12345678", "toString", "en-u-nu-12345678"},
	{"en-u-nu-latn", "numberingSystem=12345678", "toString", "en-u-nu-12345678"},
	{"en", "numberingSystem=1234abcd", "toString", "en-u-nu-1234abcd"},
	{"en-u-nu-latn", "numberingSystem=1234abcd", "toString", "en-u-nu-1234abcd"},
	{"en", "numberingSystem=1234abcd-abc123", "toString", "en-u-nu-1234abcd-abc123"},
	{"en-u-nu-latn", "numberingSystem=1234abcd-abc123", "toString", "en-u-nu-1234abcd-abc123"},
	// Locale/constructor-options-numeric-undefined.js
	{"en-u-kn-true", "", "toString", "en-u-kn"},
	{"en-u-kf-lower", "", "numeric", "false"},
	// Locale/constructor-options-numeric-valid.js
	{"en", "numeric=false", "toString", "en-u-kn-false"},
	{"en-u-kn-true", "numeric=false", "toString", "en-u-kn-false"},
	{"en-u-kf-lower", "numeric=false", "numeric", "false"},
	{"en", "numeric=true", "toString", "en-u-kn"},
	{"en-u-kn-true", "numeric=true", "toString", "en-u-kn"},
	{"en-u-kf-lower", "numeric=true", "numeric", "true"},
	// Locale/constructor-options-region-invalid.js
	{"en", "region=", "", "!"},
	{"en", "region=a", "", "!"},
	{"en", "region=abc", "", "!"},
	{"en", "region=a7", "", "!"},
	{"en", "region=notaregion", "", "!"},
	{"en", "region=SA-vaidika", "", "!"},
	{"en", "region=SA-a-asdf", "", "!"},
	{"en", "region=SA-x-private", "", "!"},
	{"en", "region=ary-Arab", "", "!"},
	{"en", "region=Latn-SA", "", "!"},
	{"en", "region=Latn-vaidika", "", "!"},
	{"en", "region=Latn-a-asdf", "", "!"},
	{"en", "region=Latn-x-private", "", "!"},
	{"en", "region=7", "", "!"},
	// Locale/constructor-options-region-valid.js
	{"en-u-ca-gregory", "", "toString", "en-u-ca-gregory"},
	{"en-US-u-ca-gregory", "", "toString", "en-US-u-ca-gregory"},
	{"en", "region=FR", "toString", "en-FR"},
	{"en-US", "region=FR", "toString", "en-FR"},
	{"en-u-ca-gregory", "region=FR", "toString", "en-FR-u-ca-gregory"},
	{"en-US-u-ca-gregory", "region=FR", "toString", "en-FR-u-ca-gregory"},
	{"en", "region=554", "toString", "en-NZ"},
	{"en-US", "region=554", "toString", "en-NZ"},
	{"en-u-ca-gregory", "region=554", "toString", "en-NZ-u-ca-gregory"},
	{"en-US-u-ca-gregory", "region=554", "toString", "en-NZ-u-ca-gregory"},
	// Locale/constructor-options-script-invalid.js
	{"en", "script=", "", "!"},
	{"en", "script=a", "", "!"},
	{"en", "script=ab", "", "!"},
	{"en", "script=abc", "", "!"},
	{"en", "script=abc7", "", "!"},
	{"en", "script=notascript", "", "!"},
	{"en", "script=undefined", "", "!"},
	{"en", "script=Bal\u0130", "", "!"},
	{"en", "script=Bal\u0131", "", "!"},
	{"en", "script=ary-Arab", "", "!"},
	{"en", "script=Latn-SA", "", "!"},
	{"en", "script=Latn-vaidika", "", "!"},
	{"en", "script=Latn-a-asdf", "", "!"},
	{"en", "script=Latn-x-private", "", "!"},
	{"en", "script=7", "", "!"},
	// Locale/constructor-options-script-valid.js
	{"en", "script=bali", "toString", "en-Bali"},
	{"en-DK", "script=bali", "toString", "en-Bali-DK"},
	{"en-Cyrl", "script=bali", "toString", "en-Bali"},
	{"en", "script=Bali", "toString", "en-Bali"},
	{"en-DK", "script=Bali", "toString", "en-Bali-DK"},
	{"en-Cyrl", "script=Bali", "toString", "en-Bali"},
	{"en", "script=bALI", "toString", "en-Bali"},
	{"en-DK", "script=bALI", "toString", "en-Bali-DK"},
	{"en-Cyrl", "script=bALI", "toString", "en-Bali"},
	// Locale/constructor-options-script-valid-undefined.js
	{"en-DK", "", "toString", "en-DK"},
	{"en-Cyrl", "", "toString", "en-Cyrl"},
	// Locale/constructor-options-variants-invalid.js
	{"en", "variants=", "", "!"},
	{"en", "variants=a", "", "!"},
	{"en", "variants=1", "", "!"},
	{"en", "variants=ab", "", "!"},
	{"en", "variants=2x", "", "!"},
	{"en", "variants=abc", "", "!"},
	{"en", "variants=3xy", "", "!"},
	{"en", "variants=abcd", "", "!"},
	{"en", "variants=abcdefghi", "", "!"},
	{"en", "variants=GB-scouse", "", "!"},
	{"en", "variants=fonipa-fonipa", "", "!"},
	{"en", "variants=fonipa-valencia-Fonipa", "", "!"},
	{"en", "variants=-", "", "!"},
	{"en", "variants=-spanglis", "", "!"},
	{"en", "variants=spanglis-", "", "!"},
	{"en", "variants=-spanglis-oxendict", "", "!"},
	{"en", "variants=spanglis-oxendict-", "", "!"},
	{"en", "variants=spanglis--oxendict", "", "!"},
	// Locale/constructor-options-variants-valid.js
	{"en-fonipa", "", "toString", "en-fonipa"},
	{"en-fonipa-u-ca-gregory", "", "toString", "en-fonipa-u-ca-gregory"},
	{"en", "variants=spanglis", "toString", "en-spanglis"},
	{"en-fonipa", "variants=spanglis", "toString", "en-spanglis"},
	{"en-u-ca-gregory", "variants=spanglis", "toString", "en-spanglis-u-ca-gregory"},
	{"en-fonipa-u-ca-gregory", "variants=spanglis", "toString", "en-spanglis-u-ca-gregory"},
	{"xx", "variants=1xyz", "toString", "xx-1xyz"},
	{"xx-fonipa", "variants=1xyz", "toString", "xx-1xyz"},
	{"xx-u-ca-gregory", "variants=1xyz", "toString", "xx-1xyz-u-ca-gregory"},
	{"xx-fonipa-u-ca-gregory", "variants=1xyz", "toString", "xx-1xyz-u-ca-gregory"},
	{"xx", "variants=1234", "toString", "xx-1234"},
	{"xx-fonipa", "variants=1234", "toString", "xx-1234"},
	{"xx-u-ca-gregory", "variants=1234", "toString", "xx-1234-u-ca-gregory"},
	{"xx-fonipa-u-ca-gregory", "variants=1234", "toString", "xx-1234-u-ca-gregory"},
	{"xx", "variants=abcde", "toString", "xx-abcde"},
	{"xx-fonipa", "variants=abcde", "toString", "xx-abcde"},
	{"xx-u-ca-gregory", "variants=abcde", "toString", "xx-abcde-u-ca-gregory"},
	{"xx-fonipa-u-ca-gregory", "variants=abcde", "toString", "xx-abcde-u-ca-gregory"},
	{"xx", "variants=12345678", "toString", "xx-12345678"},
	{"xx-fonipa", "variants=12345678", "toString", "xx-12345678"},
	{"xx-u-ca-gregory", "variants=12345678", "toString", "xx-12345678-u-ca-gregory"},
	{"xx-fonipa-u-ca-gregory", "variants=12345678", "toString", "xx-12345678-u-ca-gregory"},
	{"xx", "variants=1xyz-1234-abcde-12345678", "toString", "xx-1234-12345678-1xyz-abcde"},
	{"xx-fonipa", "variants=1xyz-1234-abcde-12345678", "toString", "xx-1234-12345678-1xyz-abcde"},
	{"xx-u-ca-gregory", "variants=1xyz-1234-abcde-12345678", "toString", "xx-1234-12345678-1xyz-abcde-u-ca-gregory"},
	{"xx-fonipa-u-ca-gregory", "variants=1xyz-1234-abcde-12345678", "toString", "xx-1234-12345678-1xyz-abcde-u-ca-gregory"},
	{"en", "variants=spanglis-oxendict", "toString", "en-oxendict-spanglis"},
	{"en-fonipa", "variants=spanglis-oxendict", "toString", "en-oxendict-spanglis"},
	{"en-u-ca-gregory", "variants=spanglis-oxendict", "toString", "en-oxendict-spanglis-u-ca-gregory"},
	{"en-fonipa-u-ca-gregory", "variants=spanglis-oxendict", "toString", "en-oxendict-spanglis-u-ca-gregory"},
	// Locale/constructor-parse-twice.js
	{"en-GB-oxendict", "region=US;calendar=gregory", "toString", "en-US-oxendict-u-ca-gregory"},
	{"no-bok", "region=NO;calendar=gregory", "", "!"},
	{"no-bok", "region=SE;calendar=gregory", "", "!"},
	{"no-bok-NO", "region=SE;calendar=gregory", "", "!"},
	{"no-bok-SE", "region=NO;calendar=gregory", "", "!"},
	// Locale/constructor-tag.js
	{"eN", "", "toString", "en"},
	{"en-GB", "", "toString", "en-GB"},
	{"en-gb", "", "toString", "en-GB"},
	{"it-Latn-IT", "", "toString", "it-Latn-IT"},
	{"IT-LATN-iT", "", "toString", "it-Latn-IT"},
	{"th-TH-u-nu-thai", "", "toString", "th-TH-u-nu-thai"},
	{"th-th-u-nu-thai", "", "toString", "th-TH-u-nu-thai"},
	{"en-x-u-foo", "", "toString", "en-x-u-foo"},
	{"en-a-bar-x-u-foo", "", "toString", "en-a-bar-x-u-foo"},
	{"en-x-u-foo-a-bar", "", "toString", "en-x-u-foo-a-bar"},
	{"en-a-bar-u-baz-x-u-foo", "", "toString", "en-a-bar-u-baz-x-u-foo"},
	{"en-u-baz-a-bar-x-u-foo", "", "toString", "en-a-bar-u-baz-x-u-foo"},
	{"de-1996", "", "toString", "de-1996"},
	{"DE-1996", "", "toString", "de-1996"},
	{"sl-1994-biske-rozaj", "", "toString", "sl-1994-biske-rozaj"},
	{"sl-ROZAJ-BISKE-1994", "", "toString", "sl-1994-biske-rozaj"},
	{"zh-Latn-pinyin-pinyin2", "", "toString", "zh-Latn-pinyin-pinyin2"},
	{"zh-latn-pinyin-pinyin2", "", "toString", "zh-Latn-pinyin-pinyin2"},
	{"X-u-foo", "", "", "!"},
	{"Flob", "", "", "!"},
	{"ZORK", "", "", "!"},
	{"Blah-latn", "", "", "!"},
	{"QuuX-latn-us", "", "", "!"},
	{"SPAM-gb-x-Sausages-BACON-eggs", "", "", "!"},
	// Locale/constructor-unicode-ext-invalid.js
	{"da-u", "", "", "!"},
	{"da-u-", "", "", "!"},
	{"da-u--", "", "", "!"},
	{"da-u-t-latn", "", "", "!"},
	{"da-u-x-priv", "", "", "!"},
	{"da-u-ca-gregory-u-ca-buddhist", "", "", "!"},
	// Locale/constructor-unicode-ext-valid.js
	{"da-u-ca-gregory-ca-buddhist", "", "toString", "da-u-ca-gregory"},
	{"zh-u-nu-hans-ca-chinese", "", "toString", "zh-u-ca-chinese-nu-hans"},
	{"zh-u-ca-chinese-nu-hans", "", "toString", "zh-u-ca-chinese-nu-hans"},
	{"de-u-cu-eur-nu-latn", "", "toString", "de-u-cu-eur-nu-latn"},
	{"de-u-nu-latn-cu-eur", "", "toString", "de-u-cu-eur-nu-latn"},
	{"pt-u-attr-ca-gregory", "", "toString", "pt-u-attr-ca-gregory"},
	{"pt-u-attr1-attr2-ca-gregory", "", "toString", "pt-u-attr1-attr2-ca-gregory"},
	{"pt-u-attr2-attr1-ca-gregory", "", "toString", "pt-u-attr1-attr2-ca-gregory"},
	// Locale/extensions-grandfathered.js
	{"cel-gaulish", "language=fr;script=Cyrl;region=FR;numberingSystem=latn", "toString", "fr-Cyrl-FR-u-nu-latn"},
	{"cel-gaulish", "language=fr;script=Cyrl;region=FR;numberingSystem=latn", "language", "fr"},
	{"cel-gaulish", "language=fr;script=Cyrl;region=FR;numberingSystem=latn", "script", "Cyrl"},
	{"cel-gaulish", "language=fr;script=Cyrl;region=FR;numberingSystem=latn", "region", "FR"},
	{"cel-gaulish", "language=fr;script=Cyrl;region=FR;numberingSystem=latn", "numberingSystem", "latn"},
	{"art-lojban", "language=fr;script=Cyrl;region=ZZ;numberingSystem=latn", "toString", "fr-Cyrl-ZZ-u-nu-latn"},
	{"art-lojban", "language=fr;script=Cyrl;region=ZZ;numberingSystem=latn", "language", "fr"},
	{"art-lojban", "language=fr;script=Cyrl;region=ZZ;numberingSystem=latn", "script", "Cyrl"},
	{"art-lojban", "language=fr;script=Cyrl;region=ZZ;numberingSystem=latn", "region", "ZZ"},
	{"art-lojban", "language=fr;script=Cyrl;region=ZZ;numberingSystem=latn", "numberingSystem", "latn"},
	{"i-default", "language=fr;script=Cyrl;region=DE;numberingSystem=latn", "", "!"},
	{"en-gb-oed", "language=fr;script=Cyrl;region=US;numberingSystem=latn", "", "!"},
	// Locale/extensions-private.js
	{"x-default", "language=fr;script=Cyrl;region=DE;numberingSystem=latn", "", "!"},
	// Locale/getters-grandfathered.js
	{"cel-gaulish", "", "baseName", "xtg"},
	{"cel-gaulish", "", "language", "xtg"},
	{"cel-gaulish", "", "script", "-"},
	{"cel-gaulish", "", "region", "-"},
	{"cel-gaulish", "", "variants", "-"},
	{"cel", "variants=gaulish", "baseName", "xtg"},
	{"cel", "variants=gaulish", "language", "xtg"},
	{"cel", "variants=gaulish", "script", "-"},
	{"cel", "variants=gaulish", "region", "-"},
	{"cel", "variants=gaulish", "variants", "-"},
	{"zh-min", "", "", "!"},
	{"i-default", "", "", "!"},
	// Locale/getters.js
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "", "toString", "de-Latn-DE-1996-fonipa-u-ca-gregory-co-phonebk-hc-h23-kf-kn-false-nu-latn"},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "", "baseName", "de-Latn-DE-1996-fonipa"},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "", "language", "de"},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "", "script", "Latn"},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "", "region", "DE"},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "", "variants", "1996-fonipa"},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "", "calendar", "gregory"},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "", "collation", "phonebk"},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "", "hourCycle", "h23"},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "", "caseFirst", ""},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "", "numeric", "false"},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "", "numberingSystem", "latn"},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "language=ja;script=jpan;region=jp;variants=Hepburn;calendar=japanese;collation=search;hourCycle=h24;caseFirst=false;numeric=true;numberingSystem=jpanfin", "toString", "ja-Jpan-JP-hepburn-u-ca-japanese-co-search-hc-h24-kf-false-kn-nu-jpanfin"},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "language=ja;script=jpan;region=jp;variants=Hepburn;calendar=japanese;collation=search;hourCycle=h24;caseFirst=false;numeric=true;numberingSystem=jpanfin", "baseName", "ja-Jpan-JP-hepburn"},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "language=ja;script=jpan;region=jp;variants=Hepburn;calendar=japanese;collation=search;hourCycle=h24;caseFirst=false;numeric=true;numberingSystem=jpanfin", "language", "ja"},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "language=ja;script=jpan;region=jp;variants=Hepburn;calendar=japanese;collation=search;hourCycle=h24;caseFirst=false;numeric=true;numberingSystem=jpanfin", "script", "Jpan"},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "language=ja;script=jpan;region=jp;variants=Hepburn;calendar=japanese;collation=search;hourCycle=h24;caseFirst=false;numeric=true;numberingSystem=jpanfin", "region", "JP"},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "language=ja;script=jpan;region=jp;variants=Hepburn;calendar=japanese;collation=search;hourCycle=h24;caseFirst=false;numeric=true;numberingSystem=jpanfin", "variants", "hepburn"},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "language=ja;script=jpan;region=jp;variants=Hepburn;calendar=japanese;collation=search;hourCycle=h24;caseFirst=false;numeric=true;numberingSystem=jpanfin", "calendar", "japanese"},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "language=ja;script=jpan;region=jp;variants=Hepburn;calendar=japanese;collation=search;hourCycle=h24;caseFirst=false;numeric=true;numberingSystem=jpanfin", "collation", "search"},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "language=ja;script=jpan;region=jp;variants=Hepburn;calendar=japanese;collation=search;hourCycle=h24;caseFirst=false;numeric=true;numberingSystem=jpanfin", "hourCycle", "h24"},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "language=ja;script=jpan;region=jp;variants=Hepburn;calendar=japanese;collation=search;hourCycle=h24;caseFirst=false;numeric=true;numberingSystem=jpanfin", "caseFirst", "false"},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "language=ja;script=jpan;region=jp;variants=Hepburn;calendar=japanese;collation=search;hourCycle=h24;caseFirst=false;numeric=true;numberingSystem=jpanfin", "numeric", "true"},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "language=ja;script=jpan;region=jp;variants=Hepburn;calendar=japanese;collation=search;hourCycle=h24;caseFirst=false;numeric=true;numberingSystem=jpanfin", "numberingSystem", "jpanfin"},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "language=fr;region=ca;collation=standard;hourCycle=h11", "toString", "fr-Latn-CA-1996-fonipa-u-ca-gregory-co-standard-hc-h11-kf-kn-false-nu-latn"},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "language=fr;region=ca;collation=standard;hourCycle=h11", "baseName", "fr-Latn-CA-1996-fonipa"},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "language=fr;region=ca;collation=standard;hourCycle=h11", "language", "fr"},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "language=fr;region=ca;collation=standard;hourCycle=h11", "script", "Latn"},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "language=fr;region=ca;collation=standard;hourCycle=h11", "region", "CA"},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "language=fr;region=ca;collation=standard;hourCycle=h11", "variants", "1996-fonipa"},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "language=fr;region=ca;collation=standard;hourCycle=h11", "calendar", "gregory"},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "language=fr;region=ca;collation=standard;hourCycle=h11", "collation", "standard"},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "language=fr;region=ca;collation=standard;hourCycle=h11", "hourCycle", "h11"},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "language=fr;region=ca;collation=standard;hourCycle=h11", "caseFirst", ""},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "language=fr;region=ca;collation=standard;hourCycle=h11", "numeric", "false"},
	{"de-latn-de-fonipa-1996-u-ca-gregory-co-phonebk-hc-h23-kf-true-kn-false-nu-latn", "language=fr;region=ca;collation=standard;hourCycle=h11", "numberingSystem", "latn"},
	{"und", "", "toString", "und"},
	{"und", "", "baseName", "und"},
	{"und", "", "language", "und"},
	{"und", "", "script", "-"},
	{"und", "", "region", "-"},
	{"und", "", "variants", "-"},
	{"und-US-u-co-emoji", "", "toString", "und-US-u-co-emoji"},
	{"und-US-u-co-emoji", "", "baseName", "und-US"},
	{"und-US-u-co-emoji", "", "language", "und"},
	{"und-US-u-co-emoji", "", "script", "-"},
	{"und-US-u-co-emoji", "", "region", "US"},
	{"und-US-u-co-emoji", "", "variants", "-"},
	{"und-US-u-co-emoji", "", "collation", "emoji"},
	// Locale/getters-missing.js
	{"sv", "", "baseName", "sv"},
	{"sv", "", "language", "sv"},
	{"sv", "", "script", "-"},
	{"sv", "", "region", "-"},
	{"sv", "", "variants", "-"},
	{"sv-Latn", "", "baseName", "sv-Latn"},
	{"sv-Latn", "", "language", "sv"},
	{"sv-Latn", "", "script", "Latn"},
	{"sv-Latn", "", "region", "-"},
	{"sv-Latn", "", "variants", "-"},
	{"sv-SE", "", "baseName", "sv-SE"},
	{"sv-SE", "", "language", "sv"},
	{"sv-SE", "", "script", "-"},
	{"sv-SE", "", "region", "SE"},
	{"sv-SE", "", "variants", "-"},
	{"de-1901", "", "baseName", "de-1901"},
	{"de-1901", "", "language", "de"},
	{"de-1901", "", "script", "-"},
	{"de-1901", "", "region", "-"},
	{"de-1901", "", "variants", "1901"},
	// Locale/invalid-tag-throws.js
	{"", "", "", "!"},
	{"i", "", "", "!"},
	{"x", "", "", "!"},
	{"u", "", "", "!"},
	{"419", "", "", "!"},
	{"u-nu-latn-cu-bob", "", "", "!"},
	{"hans-cmn-cn", "", "", "!"},
	{"cmn-hans-cn-u-u", "", "", "!"},
	{"cmn-hans-cn-t-u-ca-u", "", "", "!"},
	{"de-gregory-gregory", "", "", "!"},
	{"*", "", "", "!"},
	{"de-*", "", "", "!"},
	{"\u4e2d\u6587", "", "", "!"},
	{"en-\u00df", "", "", "!"},
	{"\u0131d", "", "", "!"},
	{"es-Latn-latn", "", "", "!"},
	{"pl-PL-pl", "", "", "!"},
	{"u-ca-gregory", "", "", "!"},
	{"de-1996-1996", "", "", "!"},
	{"pt-u-ca-gregory-u-nu-latn", "", "", "!"},
	{"no-nyn", "", "", "!"},
	{"i-klingon", "", "", "!"},
	{"zh-hak-CN", "", "", "!"},
	{"sgn-ils", "", "", "!"},
	{"x-foo", "", "", "!"},
	{"x-en-US-12345", "", "", "!"},
	{"x-12345-12345-en-US", "", "", "!"},
	{"x-en-US-12345-12345", "", "", "!"},
	{"x-en-u-foo", "", "", "!"},
	{"x-en-u-foo-u-bar", "", "", "!"},
	{"x-u-foo", "", "", "!"},
	{"de_DE", "", "", "!"},
	{"DE_de", "", "", "!"},
	{"cmn_Hans", "", "", "!"},
	{"cmn-hans_cn", "", "", "!"},
	{"es_419", "", "", "!"},
	{"es-419-u-nu-latn-cu_bob", "", "", "!"},
	{"i_klingon", "", "", "!"},
	{"cmn-hans-cn-t-ca-u-ca-x_t-u", "", "", "!"},
	{"enochian_enochian", "", "", "!"},
	{"de-gregory_u-ca-gregory", "", "", "!"},
	{"en\u0000", "", "", "!"},
	{" en", "", "", "!"},
	{"en ", "", "", "!"},
	{"it-IT-Latn", "", "", "!"},
	{"de-u", "", "", "!"},
	{"de-u-", "", "", "!"},
	{"de-u-ca-", "", "", "!"},
	{"de-u-ca-gregory-", "", "", "!"},
	{"si-x", "", "", "!"},
	{"x-", "", "", "!"},
	{"x-y-", "", "", "!"},
	// Locale/likely-subtags-grandfathered.js
	{"en-GB-oed", "", "", "!"},
	{"i-ami", "", "", "!"},
	{"i-bnn", "", "", "!"},
	{"i-enochian", "", "", "!"},
	{"i-hak", "", "", "!"},
	{"i-lux", "", "", "!"},
	{"i-mingo", "", "", "!"},
	{"i-navajo", "", "", "!"},
	{"i-pwn", "", "", "!"},
	{"i-tao", "", "", "!"},
	{"i-tay", "", "", "!"},
	{"i-tsu", "", "", "!"},
	{"sgn-BE-FR", "", "", "!"},
	{"sgn-BE-NL", "", "", "!"},
	{"sgn-CH-DE", "", "", "!"},
	{"art-lojban", "", "toString", "jbo"},
	{"art-lojban", "", "maximize.toString", "jbo-Latn-001"},
	{"art-lojban", "", "maximize.maximize.toString", "jbo-Latn-001"},
	{"art-lojban", "", "minimize.toString", "jbo"},
	{"art-lojban", "", "minimize.minimize.toString", "jbo"},
	{"art-lojban", "", "maximize.minimize.toString", "jbo"},
	{"art-lojban", "", "minimize.maximize.toString", "jbo-Latn-001"},
	{"cel-gaulish", "", "toString", "xtg"},
	{"cel-gaulish", "", "maximize.toString", "xtg"},
	{"cel-gaulish", "", "maximize.maximize.toString", "xtg"},
	{"cel-gaulish", "", "minimize.toString", "xtg"},
	{"cel-gaulish", "", "minimize.minimize.toString", "xtg"},
	{"cel-gaulish", "", "maximize.minimize.toString", "xtg"},
	{"cel-gaulish", "", "minimize.maximize.toString", "xtg"},
	{"zh-guoyu", "", "toString", "zh"},
	{"zh-guoyu", "", "maximize.toString", "zh-Hans-CN"},
	{"zh-guoyu", "", "maximize.maximize.toString", "zh-Hans-CN"},
	{"zh-guoyu", "", "minimize.toString", "zh"},
	{"zh-guoyu", "", "minimize.minimize.toString", "zh"},
	{"zh-guoyu", "", "maximize.minimize.toString", "zh"},
	{"zh-guoyu", "", "minimize.maximize.toString", "zh-Hans-CN"},
	{"zh-hakka", "", "toString", "hak"},
	{"zh-hakka", "", "maximize.toString", "hak-Hans-CN"},
	{"zh-hakka", "", "maximize.maximize.toString", "hak-Hans-CN"},
	{"zh-hakka", "", "minimize.toString", "hak"},
	{"zh-hakka", "", "minimize.minimize.toString", "hak"},
	{"zh-hakka", "", "maximize.minimize.toString", "hak"},
	{"zh-hakka", "", "minimize.maximize.toString", "hak-Hans-CN"},
	{"zh-xiang", "", "toString", "hsn"},
	{"zh-xiang", "", "maximize.toString", "hsn-Hans-CN"},
	{"zh-xiang", "", "maximize.maximize.toString", "hsn-Hans-CN"},
	{"zh-xiang", "", "minimize.toString", "hsn"},
	{"zh-xiang", "", "minimize.minimize.toString", "hsn"},
	{"zh-xiang", "", "maximize.minimize.toString", "hsn"},
	{"zh-xiang", "", "minimize.maximize.toString", "hsn-Hans-CN"},
	{"no-bok", "", "", "!"},
	{"zh-min-nan", "", "", "!"},
	{"art-lojban-fonipa", "", "toString", "jbo-fonipa"},
	{"art-lojban-a-not-assigned", "", "toString", "jbo-a-not-assigned"},
	{"art-lojban-u-attr", "", "toString", "jbo-u-attr"},
	{"art-lojban-u-co", "", "toString", "jbo-u-co"},
	{"art-lojban-u-co-phonebk", "", "toString", "jbo-u-co-phonebk"},
	{"art-lojban-x-private", "", "toString", "jbo-x-private"},
	{"cel-gaulish-fonipa", "", "toString", "xtg-fonipa"},
	{"cel-gaulish-a-not-assigned", "", "toString", "xtg-a-not-assigned"},
	{"cel-gaulish-u-attr", "", "toString", "xtg-u-attr"},
	{"cel-gaulish-u-co", "", "toString", "xtg-u-co"},
	{"cel-gaulish-u-co-phonebk", "", "toString", "xtg-u-co-phonebk"},
	{"cel-gaulish-x-private", "", "toString", "xtg-x-private"},
	{"zh-guoyu-fonipa", "", "toString", "zh-fonipa"},
	{"zh-guoyu-a-not-assigned", "", "toString", "zh-a-not-assigned"},
	{"zh-guoyu-u-attr", "", "toString", "zh-u-attr"},
	{"zh-guoyu-u-co", "", "toString", "zh-u-co"},
	{"zh-guoyu-u-co-phonebk", "", "toString", "zh-u-co-phonebk"},
	{"zh-guoyu-x-private", "", "toString", "zh-x-private"},
	{"zh-hakka-fonipa", "", "toString", "hak-fonipa"},
	{"zh-hakka-a-not-assigned", "", "toString", "hak-a-not-assigned"},
	{"zh-hakka-u-attr", "", "toString", "hak-u-attr"},
	{"zh-hakka-u-co", "", "toString", "hak-u-co"},
	{"zh-hakka-u-co-phonebk", "", "toString", "hak-u-co-phonebk"},
	{"zh-hakka-x-private", "", "toString", "hak-x-private"},
	{"zh-xiang-fonipa", "", "toString", "hsn-fonipa"},
	{"zh-xiang-a-not-assigned", "", "toString", "hsn-a-not-assigned"},
	{"zh-xiang-u-attr", "", "toString", "hsn-u-attr"},
	{"zh-xiang-u-co", "", "toString", "hsn-u-co"},
	{"zh-xiang-u-co-phonebk", "", "toString", "hsn-u-co-phonebk"},
	{"zh-xiang-x-private", "", "toString", "hsn-x-private"},
	// Locale/likely-subtags.js
	{"en-Latn-US", "", "maximize.toString", "en-Latn-US"},
	{"en", "", "maximize.toString", "en-Latn-US"},
	{"en-fonipa", "", "maximize.toString", "en-Latn-US-fonipa"},
	{"en-a-not-assigned", "", "maximize.toString", "en-Latn-US-a-not-assigned"},
	{"en-u-attr", "", "maximize.toString", "en-Latn-US-u-attr"},
	{"en-u-co", "", "maximize.toString", "en-Latn-US-u-co"},
	{"en-u-co-phonebk", "", "maximize.toString", "en-Latn-US-u-co-phonebk"},
	{"en-x-private", "", "maximize.toString", "en-Latn-US-x-private"},
	{"en-Latn", "", "maximize.toString", "en-Latn-US"},
	{"en-Latn-fonipa", "", "maximize.toString", "en-Latn-US-fonipa"},
	{"en-Latn-a-not-assigned", "", "maximize.toString", "en-Latn-US-a-not-assigned"},
	{"en-Latn-u-attr", "", "maximize.toString", "en-Latn-US-u-attr"},
	{"en-Latn-u-co", "", "maximize.toString", "en-Latn-US-u-co"},
	{"en-Latn-u-co-phonebk", "", "maximize.toString", "en-Latn-US-u-co-phonebk"},
	{"en-Latn-x-private", "", "maximize.toString", "en-Latn-US-x-private"},
	{"en-Shaw-GB", "", "maximize.toString", "en-Shaw-GB"},
	{"en-Shaw", "", "maximize.toString", "en-Shaw-GB"},
	{"en-Shaw-fonipa", "", "maximize.toString", "en-Shaw-GB-fonipa"},
	{"en-Shaw-a-not-assigned", "", "maximize.toString", "en-Shaw-GB-a-not-assigned"},
	{"en-Shaw-u-attr", "", "maximize.toString", "en-Shaw-GB-u-attr"},
	{"en-Shaw-u-co", "", "maximize.toString", "en-Shaw-GB-u-co"},
	{"en-Shaw-u-co-phonebk", "", "maximize.toString", "en-Shaw-GB-u-co-phonebk"},
	{"en-Shaw-x-private", "", "maximize.toString", "en-Shaw-GB-x-private"},
	{"en-Arab-US", "", "maximize.toString", "en-Arab-US"},
	{"en-Arab", "", "maximize.toString", "en-Arab-US"},
	{"en-Arab-fonipa", "", "maximize.toString", "en-Arab-US-fonipa"},
	{"en-Arab-a-not-assigned", "", "maximize.toString", "en-Arab-US-a-not-assigned"},
	{"en-Arab-u-attr", "", "maximize.toString", "en-Arab-US-u-attr"},
	{"en-Arab-u-co", "", "maximize.toString", "en-Arab-US-u-co"},
	{"en-Arab-u-co-phonebk", "", "maximize.toString", "en-Arab-US-u-co-phonebk"},
	{"en-Arab-x-private", "", "maximize.toString", "en-Arab-US-x-private"},
	{"en-US", "", "maximize.toString", "en-Latn-US"},
	{"en-US-fonipa", "", "maximize.toString", "en-Latn-US-fonipa"},
	{"en-US-a-not-assigned", "", "maximize.toString", "en-Latn-US-a-not-assigned"},
	{"en-US-u-attr", "", "maximize.toString", "en-Latn-US-u-attr"},
	{"en-US-u-co", "", "maximize.toString", "en-Latn-US-u-co"},
	{"en-US-u-co-phonebk", "", "maximize.toString", "en-Latn-US-u-co-phonebk"},
	{"en-US-x-private", "", "maximize.toString", "en-Latn-US-x-private"},
	{"en-Latn-GB", "", "maximize.toString", "en-Latn-GB"},
	{"en-GB", "", "maximize.toString", "en-Latn-GB"},
	{"en-GB-fonipa", "", "maximize.toString", "en-Latn-GB-fonipa"},
	{"en-GB-a-not-assigned", "", "maximize.toString", "en-Latn-GB-a-not-assigned"},
	{"en-GB-u-attr", "", "maximize.toString", "en-Latn-GB-u-attr"},
	{"en-GB-u-co", "", "maximize.toString", "en-Latn-GB-u-co"},
	{"en-GB-u-co-phonebk", "", "maximize.toString", "en-Latn-GB-u-co-phonebk"},
	{"en-GB-x-private", "", "maximize.toString", "en-Latn-GB-x-private"},
	{"en-Latn-FR", "", "maximize.toString", "en-Latn-FR"},
	{"en-FR", "", "maximize.toString", "en-Latn-FR"},
	{"en-FR-fonipa", "", "maximize.toString", "en-Latn-FR-fonipa"},
	{"en-FR-a-not-assigned", "", "maximize.toString", "en-Latn-FR-a-not-assigned"},
	{"en-FR-u-attr", "", "maximize.toString", "en-Latn-FR-u-attr"},
	{"en-FR-u-co", "", "maximize.toString", "en-Latn-FR-u-co"},
	{"en-FR-u-co-phonebk", "", "maximize.toString", "en-Latn-FR-u-co-phonebk"},
	{"en-FR-x-private", "", "maximize.toString", "en-Latn-FR-x-private"},
	{"it-Kana-CA", "", "maximize.toString", "it-Kana-CA"},
	{"it-Kana-CA-fonipa", "", "maximize.toString", "it-Kana-CA-fonipa"},
	{"it-Kana-CA-a-not-assigned", "", "maximize.toString", "it-Kana-CA-a-not-assigned"},
	{"it-Kana-CA-u-attr", "", "maximize.toString", "it-Kana-CA-u-attr"},
	{"it-Kana-CA-u-co", "", "maximize.toString", "it-Kana-CA-u-co"},
	{"it-Kana-CA-u-co-phonebk", "", "maximize.toString", "it-Kana-CA-u-co-phonebk"},
	{"it-Kana-CA-x-private", "", "maximize.toString", "it-Kana-CA-x-private"},
	{"und", "", "maximize.toString", "en-Latn-US"},
	{"und-fonipa", "", "maximize.toString", "en-Latn-US-fonipa"},
	{"und-a-not-assigned", "", "maximize.toString", "en-Latn-US-a-not-assigned"},
	{"und-u-attr", "", "maximize.toString", "en-Latn-US-u-attr"},
	{"und-u-co", "", "maximize.toString", "en-Latn-US-u-co"},
	{"und-u-co-phonebk", "", "maximize.toString", "en-Latn-US-u-co-phonebk"},
	{"und-x-private", "", "maximize.toString", "en-Latn-US-x-private"},
	{"th-Thai-TH", "", "maximize.toString", "th-Thai-TH"},
	{"und-Thai", "", "maximize.toString", "th-Thai-TH"},
	{"und-Thai-fonipa", "", "maximize.toString", "th-Thai-TH-fonipa"},
	{"und-Thai-a-not-assigned", "", "maximize.toString", "th-Thai-TH-a-not-assigned"},
	{"und-Thai-u-attr", "", "maximize.toString", "th-Thai-TH-u-attr"},
	{"und-Thai-u-co", "", "maximize.toString", "th-Thai-TH-u-co"},
	{"und-Thai-u-co-phonebk", "", "maximize.toString", "th-Thai-TH-u-co-phonebk"},
	{"und-Thai-x-private", "", "maximize.toString", "th-Thai-TH-x-private"},
	{"es-Latn-419", "", "maximize.toString", "es-Latn-419"},
	{"und-419", "", "maximize.toString", "es-Latn-419"},
	{"und-419-fonipa", "", "maximize.toString", "es-Latn-419-fonipa"},
	{"und-419-a-not-assigned", "", "maximize.toString", "es-Latn-419-a-not-assigned"},
	{"und-419-u-attr", "", "maximize.toString", "es-Latn-419-u-attr"},
	{"und-419-u-co", "", "maximize.toString", "es-Latn-419-u-co"},
	{"und-419-u-co-phonebk", "", "maximize.toString", "es-Latn-419-u-co-phonebk"},
	{"und-419-x-private", "", "maximize.toString", "es-Latn-419-x-private"},
	{"en-Latn-150", "", "maximize.toString", "en-Latn-150"},
	{"und-150", "", "maximize.toString", "en-Latn-150"},
	{"und-150-fonipa", "", "maximize.toString", "en-Latn-150-fonipa"},
	{"und-150-a-not-assigned", "", "maximize.toString", "en-Latn-150-a-not-assigned"},
	{"und-150-u-attr", "", "maximize.toString", "en-Latn-150-u-attr"},
	{"und-150-u-co", "", "maximize.toString", "en-Latn-150-u-co"},
	{"und-150-u-co-phonebk", "", "maximize.toString", "en-Latn-150-u-co-phonebk"},
	{"und-150-x-private", "", "maximize.toString", "en-Latn-150-x-private"},
	{"de-Latn-AT", "", "maximize.toString", "de-Latn-AT"},
	{"und-AT", "", "maximize.toString", "de-Latn-AT"},
	{"und-AT-fonipa", "", "maximize.toString", "de-Latn-AT-fonipa"},
	{"und-AT-a-not-assigned", "", "maximize.toString", "de-Latn-AT-a-not-assigned"},
	{"und-AT-u-attr", "", "maximize.toString", "de-Latn-AT-u-attr"},
	{"und-AT-u-co", "", "maximize.toString", "de-Latn-AT-u-co"},
	{"und-AT-u-co-phonebk", "", "maximize.toString", "de-Latn-AT-u-co-phonebk"},
	{"und-AT-x-private", "", "maximize.toString", "de-Latn-AT-x-private"},
	{"bg-Cyrl-RO", "", "maximize.toString", "bg-Cyrl-RO"},
	{"und-Cyrl-RO", "", "maximize.toString", "bg-Cyrl-RO"},
	{"und-Cyrl-RO-fonipa", "", "maximize.toString", "bg-Cyrl-RO-fonipa"},
	{"und-Cyrl-RO-a-not-assigned", "", "maximize.toString", "bg-Cyrl-RO-a-not-assigned"},
	{"und-Cyrl-RO-u-attr", "", "maximize.toString", "bg-Cyrl-RO-u-attr"},
	{"und-Cyrl-RO-u-co", "", "maximize.toString", "bg-Cyrl-RO-u-co"},
	{"und-Cyrl-RO-u-co-phonebk", "", "maximize.toString", "bg-Cyrl-RO-u-co-phonebk"},
	{"und-Cyrl-RO-x-private", "", "maximize.toString", "bg-Cyrl-RO-x-private"},
	{"en-Latn-AQ", "", "maximize.toString", "en-Latn-AQ"},
	{"und-AQ", "", "maximize.toString", "en-Latn-AQ"},
	{"und-AQ-fonipa", "", "maximize.toString", "en-Latn-AQ-fonipa"},
	{"und-AQ-a-not-assigned", "", "maximize.toString", "en-Latn-AQ-a-not-assigned"},
	{"und-AQ-u-attr", "", "maximize.toString", "en-Latn-AQ-u-attr"},
	{"und-AQ-u-co", "", "maximize.toString", "en-Latn-AQ-u-co"},
	{"und-AQ-u-co-phonebk", "", "maximize.toString", "en-Latn-AQ-u-co-phonebk"},
	{"und-AQ-x-private", "", "maximize.toString", "en-Latn-AQ-x-private"},
	{"en", "", "minimize.toString", "en"},
	{"en-fonipa", "", "minimize.toString", "en-fonipa"},
	{"en-a-not-assigned", "", "minimize.toString", "en-a-not-assigned"},
	{"en-u-attr", "", "minimize.toString", "en-u-attr"},
	{"en-u-co", "", "minimize.toString", "en-u-co"},
	{"en-u-co-phonebk", "", "minimize.toString", "en-u-co-phonebk"},
	{"en-x-private", "", "minimize.toString", "en-x-private"},
	{"en-Latn", "", "minimize.toString", "en"},
	{"en-Latn-fonipa", "", "minimize.toString", "en-fonipa"},
	{"en-Latn-a-not-assigned", "", "minimize.toString", "en-a-not-assigned"},
	{"en-Latn-u-attr", "", "minimize.toString", "en-u-attr"},
	{"en-Latn-u-co", "", "minimize.toString", "en-u-co"},
	{"en-Latn-u-co-phonebk", "", "minimize.toString", "en-u-co-phonebk"},
	{"en-Latn-x-private", "", "minimize.toString", "en-x-private"},
	{"ar", "", "minimize.toString", "ar"},
	{"ar-Arab", "", "minimize.toString", "ar"},
	{"ar-Arab-fonipa", "", "minimize.toString", "ar-fonipa"},
	{"ar-Arab-a-not-assigned", "", "minimize.toString", "ar-a-not-assigned"},
	{"ar-Arab-u-attr", "", "minimize.toString", "ar-u-attr"},
	{"ar-Arab-u-co", "", "minimize.toString", "ar-u-co"},
	{"ar-Arab-u-co-phonebk", "", "minimize.toString", "ar-u-co-phonebk"},
	{"ar-Arab-x-private", "", "minimize.toString", "ar-x-private"},
	{"en-US", "", "minimize.toString", "en"},
	{"en-US-fonipa", "", "minimize.toString", "en-fonipa"},
	{"en-US-a-not-assigned", "", "minimize.toString", "en-a-not-assigned"},
	{"en-US-u-attr", "", "minimize.toString", "en-u-attr"},
	{"en-US-u-co", "", "minimize.toString", "en-u-co"},
	{"en-US-u-co-phonebk", "", "minimize.toString", "en-u-co-phonebk"},
	{"en-US-x-private", "", "minimize.toString", "en-x-private"},
	{"en-GB", "", "minimize.toString", "en-GB"},
	{"en-GB-fonipa", "", "minimize.toString", "en-GB-fonipa"},
	{"en-GB-a-not-assigned", "", "minimize.toString", "en-GB-a-not-assigned"},
	{"en-GB-u-attr", "", "minimize.toString", "en-GB-u-attr"},
	{"en-GB-u-co", "", "minimize.toString", "en-GB-u-co"},
	{"en-GB-u-co-phonebk", "", "minimize.toString", "en-GB-u-co-phonebk"},
	{"en-GB-x-private", "", "minimize.toString", "en-GB-x-private"},
	{"en-Latn-US", "", "minimize.toString", "en"},
	{"en-Latn-US-fonipa", "", "minimize.toString", "en-fonipa"},
	{"en-Latn-US-a-not-assigned", "", "minimize.toString", "en-a-not-assigned"},
	{"en-Latn-US-u-attr", "", "minimize.toString", "en-u-attr"},
	{"en-Latn-US-u-co", "", "minimize.toString", "en-u-co"},
	{"en-Latn-US-u-co-phonebk", "", "minimize.toString", "en-u-co-phonebk"},
	{"en-Latn-US-x-private", "", "minimize.toString", "en-x-private"},
	{"en-Shaw", "", "minimize.toString", "en-Shaw"},
	{"en-Shaw-GB", "", "minimize.toString", "en-Shaw"},
	{"en-Shaw-GB-fonipa", "", "minimize.toString", "en-Shaw-fonipa"},
	{"en-Shaw-GB-a-not-assigned", "", "minimize.toString", "en-Shaw-a-not-assigned"},
	{"en-Shaw-GB-u-attr", "", "minimize.toString", "en-Shaw-u-attr"},
	{"en-Shaw-GB-u-co", "", "minimize.toString", "en-Shaw-u-co"},
	{"en-Shaw-GB-u-co-phonebk", "", "minimize.toString", "en-Shaw-u-co-phonebk"},
	{"en-Shaw-GB-x-private", "", "minimize.toString", "en-Shaw-x-private"},
	{"en-Arab", "", "minimize.toString", "en-Arab"},
	{"en-Arab-US", "", "minimize.toString", "en-Arab"},
	{"en-Arab-US-fonipa", "", "minimize.toString", "en-Arab-fonipa"},
	{"en-Arab-US-a-not-assigned", "", "minimize.toString", "en-Arab-a-not-assigned"},
	{"en-Arab-US-u-attr", "", "minimize.toString", "en-Arab-u-attr"},
	{"en-Arab-US-u-co", "", "minimize.toString", "en-Arab-u-co"},
	{"en-Arab-US-u-co-phonebk", "", "minimize.toString", "en-Arab-u-co-phonebk"},
	{"en-Arab-US-x-private", "", "minimize.toString", "en-Arab-x-private"},
	{"en-Latn-GB", "", "minimize.toString", "en-GB"},
	{"en-Latn-GB-fonipa", "", "minimize.toString", "en-GB-fonipa"},
	{"en-Latn-GB-a-not-assigned", "", "minimize.toString", "en-GB-a-not-assigned"},
	{"en-Latn-GB-u-attr", "", "minimize.toString", "en-GB-u-attr"},
	{"en-Latn-GB-u-co", "", "minimize.toString", "en-GB-u-co"},
	{"en-Latn-GB-u-co-phonebk", "", "minimize.toString", "en-GB-u-co-phonebk"},
	{"en-Latn-GB-x-private", "", "minimize.toString", "en-GB-x-private"},
	{"en-FR", "", "minimize.toString", "en-FR"},
	{"en-Latn-FR", "", "minimize.toString", "en-FR"},
	{"en-Latn-FR-fonipa", "", "minimize.toString", "en-FR-fonipa"},
	{"en-Latn-FR-a-not-assigned", "", "minimize.toString", "en-FR-a-not-assigned"},
	{"en-Latn-FR-u-attr", "", "minimize.toString", "en-FR-u-attr"},
	{"en-Latn-FR-u-co", "", "minimize.toString", "en-FR-u-co"},
	{"en-Latn-FR-u-co-phonebk", "", "minimize.toString", "en-FR-u-co-phonebk"},
	{"en-Latn-FR-x-private", "", "minimize.toString", "en-FR-x-private"},
	{"it-Kana-CA", "", "minimize.toString", "it-Kana-CA"},
	{"it-Kana-CA-fonipa", "", "minimize.toString", "it-Kana-CA-fonipa"},
	{"it-Kana-CA-a-not-assigned", "", "minimize.toString", "it-Kana-CA-a-not-assigned"},
	{"it-Kana-CA-u-attr", "", "minimize.toString", "it-Kana-CA-u-attr"},
	{"it-Kana-CA-u-co", "", "minimize.toString", "it-Kana-CA-u-co"},
	{"it-Kana-CA-u-co-phonebk", "", "minimize.toString", "it-Kana-CA-u-co-phonebk"},
	{"it-Kana-CA-x-private", "", "minimize.toString", "it-Kana-CA-x-private"},
	{"th", "", "minimize.toString", "th"},
	{"th-Thai-TH", "", "minimize.toString", "th"},
	{"th-Thai-TH-fonipa", "", "minimize.toString", "th-fonipa"},
	{"th-Thai-TH-a-not-assigned", "", "minimize.toString", "th-a-not-assigned"},
	{"th-Thai-TH-u-attr", "", "minimize.toString", "th-u-attr"},
	{"th-Thai-TH-u-co", "", "minimize.toString", "th-u-co"},
	{"th-Thai-TH-u-co-phonebk", "", "minimize.toString", "th-u-co-phonebk"},
	{"th-Thai-TH-x-private", "", "minimize.toString", "th-x-private"},
	{"es-419", "", "minimize.toString", "es-419"},
	{"es-Latn-419", "", "minimize.toString", "es-419"},
	{"es-Latn-419-fonipa", "", "minimize.toString", "es-419-fonipa"},
	{"es-Latn-419-a-not-assigned", "", "minimize.toString", "es-419-a-not-assigned"},
	{"es-Latn-419-u-attr", "", "minimize.toString", "es-419-u-attr"},
	{"es-Latn-419-u-co", "", "minimize.toString", "es-419-u-co"},
	{"es-Latn-419-u-co-phonebk", "", "minimize.toString", "es-419-u-co-phonebk"},
	{"es-Latn-419-x-private", "", "minimize.toString", "es-419-x-private"},
	{"ru", "", "minimize.toString", "ru"},
	{"ru-Cyrl-RU", "", "minimize.toString", "ru"},
	{"ru-Cyrl-RU-fonipa", "", "minimize.toString", "ru-fonipa"},
	{"ru-Cyrl-RU-a-not-assigned", "", "minimize.toString", "ru-a-not-assigned"},
	{"ru-Cyrl-RU-u-attr", "", "minimize.toString", "ru-u-attr"},
	{"ru-Cyrl-RU-u-co", "", "minimize.toString", "ru-u-co"},
	{"ru-Cyrl-RU-u-co-phonebk", "", "minimize.toString", "ru-u-co-phonebk"},
	{"ru-Cyrl-RU-x-private", "", "minimize.toString", "ru-x-private"},
	{"de-AT", "", "minimize.toString", "de-AT"},
	{"de-Latn-AT", "", "minimize.toString", "de-AT"},
	{"de-Latn-AT-fonipa", "", "minimize.toString", "de-AT-fonipa"},
	{"de-Latn-AT-a-not-assigned", "", "minimize.toString", "de-AT-a-not-assigned"},
	{"de-Latn-AT-u-attr", "", "minimize.toString", "de-AT-u-attr"},
	{"de-Latn-AT-u-co", "", "minimize.toString", "de-AT-u-co"},
	{"de-Latn-AT-u-co-phonebk", "", "minimize.toString", "de-AT-u-co-phonebk"},
	{"de-Latn-AT-x-private", "", "minimize.toString", "de-AT-x-private"},
	{"bg-RO", "", "minimize.toString", "bg-RO"},
	{"bg-Cyrl-RO", "", "minimize.toString", "bg-RO"},
	{"bg-Cyrl-RO-fonipa", "", "minimize.toString", "bg-RO-fonipa"},
	{"bg-Cyrl-RO-a-not-assigned", "", "minimize.toString", "bg-RO-a-not-assigned"},
	{"bg-Cyrl-RO-u-attr", "", "minimize.toString", "bg-RO-u-attr"},
	{"bg-Cyrl-RO-u-co", "", "minimize.toString", "bg-RO-u-co"},
	{"bg-Cyrl-RO-u-co-phonebk", "", "minimize.toString", "bg-RO-u-co-phonebk"},
	{"bg-Cyrl-RO-x-private", "", "minimize.toString", "bg-RO-x-private"},
	{"en-AQ", "", "minimize.toString", "en-AQ"},
	{"und-Latn-AQ", "", "minimize.toString", "en-AQ"},
	{"und-Latn-AQ-fonipa", "", "minimize.toString", "en-AQ-fonipa"},
	{"und-Latn-AQ-a-not-assigned", "", "minimize.toString", "en-AQ-a-not-assigned"},
	{"und-Latn-AQ-u-attr", "", "minimize.toString", "en-AQ-u-attr"},
	{"und-Latn-AQ-u-co", "", "minimize.toString", "en-AQ-u-co"},
	{"und-Latn-AQ-u-co-phonebk", "", "minimize.toString", "en-AQ-u-co-phonebk"},
	{"und-Latn-AQ-x-private", "", "minimize.toString", "en-AQ-x-private"},
	{"x-private", "", "", "!"},
	// Locale/reject-duplicate-variants-in-tlang.js
	{"de-t-en-emodeng-emodeng", "", "", "!"},
	{"de-t-en-Emodeng-emodeng", "", "", "!"},
	{"de-t-en-emodeng-Emodeng", "", "", "!"},
	{"de-t-en-variant-emodeng-emodeng", "", "", "!"},
	{"de-t-en-variant-Emodeng-emodeng", "", "", "!"},
	{"de-t-en-variant-emodeng-Emodeng", "", "", "!"},
	{"de-t-en-emodeng-variant-emodeng", "", "", "!"},
	{"de-t-en-Emodeng-variant-emodeng", "", "", "!"},
	{"de-t-en-emodeng-variant-Emodeng", "", "", "!"},
	{"de-t-en-emodeng-emodeng-variant", "", "", "!"},
	{"de-t-en-Emodeng-emodeng-variant", "", "", "!"},
	{"de-t-en-emodeng-Emodeng-variant", "", "", "!"},
	// Locale/reject-duplicate-variants.js
	{"en-emodeng-emodeng", "", "", "!"},
	{"en-Emodeng-emodeng", "", "", "!"},
	{"en-emodeng-Emodeng", "", "", "!"},
	{"en-variant-emodeng-emodeng", "", "", "!"},
	{"en-variant-Emodeng-emodeng", "", "", "!"},
	{"en-variant-emodeng-Emodeng", "", "", "!"},
	{"en-emodeng-variant-emodeng", "", "", "!"},
	{"en-Emodeng-variant-emodeng", "", "", "!"},
	{"en-emodeng-variant-Emodeng", "", "", "!"},
	{"en-emodeng-emodeng-variant", "", "", "!"},
	{"en-Emodeng-emodeng-variant", "", "", "!"},
	{"en-emodeng-Emodeng-variant", "", "", "!"},
	// Locale/subclassing.js
	{"de", "", "toString", "de"},
}
