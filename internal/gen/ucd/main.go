// Command ucd generates internal/regexpsyntax/unicode_tables.go, the Unicode
// Character Database tables of the regular-expression engine: General_Category,
// Script and Script_Extensions values, the binary properties of ECMA-262 table
// 67, the properties of strings of the v flag, simple case folding and the
// non-u Canonicalize mapping. With -norm it generates
// engine/unicode_norm_tables.go instead, the normalization data of
// String.prototype.normalize (norm.go).
//
// The UCD files are downloaded from unicode.org at generation time only
// (-cache keeps a local copy); the output is deterministic and committed.
// The version is pinned to the one tc39/test262 uses for
// test/built-ins/RegExp/property-escapes/generated/.
//
// Usage (from internal/regexpsyntax/, see the go:generate line in unicode.go):
//
//	go run ../gen/ucd -o unicode_tables.go [-cache dir]
//
// and from engine/ (see the go:generate line in string_normalize.go):
//
//	go run ../internal/gen/ucd -norm unicode_norm_tables.go [-normtest file] [-cache dir]
package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"go/format"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
)

const version = "17.0.0"

const baseURL = "https://www.unicode.org/Public/" + version + "/"

const maxRune = 0x10FFFF

// binaryProps is ECMA-262 table 67 (binary Unicode property aliases): the
// canonical name and its alias ("" when it has none). Any, ASCII and
// Assigned are derived, the rest come from the UCD files below.
var binaryProps = [][2]string{
	{"ASCII", ""}, {"ASCII_Hex_Digit", "AHex"}, {"Alphabetic", "Alpha"}, {"Any", ""}, {"Assigned", ""},
	{"Bidi_Control", "Bidi_C"}, {"Bidi_Mirrored", "Bidi_M"}, {"Case_Ignorable", "CI"}, {"Cased", ""},
	{"Changes_When_Casefolded", "CWCF"}, {"Changes_When_Casemapped", "CWCM"}, {"Changes_When_Lowercased", "CWL"},
	{"Changes_When_NFKC_Casefolded", "CWKCF"}, {"Changes_When_Titlecased", "CWT"}, {"Changes_When_Uppercased", "CWU"},
	{"Dash", ""}, {"Default_Ignorable_Code_Point", "DI"}, {"Deprecated", "Dep"}, {"Diacritic", "Dia"},
	{"Emoji", ""}, {"Emoji_Component", "EComp"}, {"Emoji_Modifier", "EMod"}, {"Emoji_Modifier_Base", "EBase"},
	{"Emoji_Presentation", "EPres"}, {"Extended_Pictographic", "ExtPict"}, {"Extender", "Ext"},
	{"Grapheme_Base", "Gr_Base"}, {"Grapheme_Extend", "Gr_Ext"}, {"Hex_Digit", "Hex"},
	{"IDS_Binary_Operator", "IDSB"}, {"IDS_Trinary_Operator", "IDST"}, {"ID_Continue", "IDC"}, {"ID_Start", "IDS"},
	{"Ideographic", "Ideo"}, {"Join_Control", "Join_C"}, {"Logical_Order_Exception", "LOE"}, {"Lowercase", "Lower"},
	{"Math", ""}, {"Noncharacter_Code_Point", "NChar"}, {"Pattern_Syntax", "Pat_Syn"}, {"Pattern_White_Space", "Pat_WS"},
	{"Quotation_Mark", "QMark"}, {"Radical", ""}, {"Regional_Indicator", "RI"}, {"Sentence_Terminal", "STerm"},
	{"Soft_Dotted", "SD"}, {"Terminal_Punctuation", "Term"}, {"Unified_Ideograph", "UIdeo"}, {"Uppercase", "Upper"},
	{"Variation_Selector", "VS"}, {"White_Space", "space"}, {"XID_Continue", "XIDC"}, {"XID_Start", "XIDS"},
}

// binaryPropFiles lists the files the non-derived binary properties are read
// from.
var binaryPropFiles = []string{
	"ucd/PropList.txt", "ucd/DerivedCoreProperties.txt", "ucd/extracted/DerivedBinaryProperties.txt",
	"ucd/DerivedNormalizationProps.txt", "ucd/emoji/emoji-data.txt",
}

// stringProps are the properties of strings of the v flag (ECMA-262 table
// 69); RGI_Emoji is the union of the other six.
var stringProps = []string{
	"Basic_Emoji", "Emoji_Keycap_Sequence", "RGI_Emoji_Modifier_Sequence", "RGI_Emoji_Flag_Sequence",
	"RGI_Emoji_Tag_Sequence", "RGI_Emoji_ZWJ_Sequence",
}

var cacheDir string

func main() {
	out := flag.String("o", "unicode_tables.go", "output file")
	flag.StringVar(&cacheDir, "cache", "", "directory for downloaded UCD files (reused when present)")
	norm := flag.String("norm", "", "write the normalization tables to this file instead")
	normTest := flag.String("normtest", "", "with -norm, also write the trimmed NormalizationTest.txt (gzip) here")
	flag.Parse()
	if *norm != "" {
		writeNorm(*norm, *normTest)
		return
	}

	var g gen
	g.load()
	src, err := format.Source(g.emit())
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, src, 0o644); err != nil {
		log.Fatal(err)
	}
}

// fetch returns the contents of a UCD file (path relative to baseURL).
func fetch(path string) []byte {
	if cacheDir != "" {
		if b, err := os.ReadFile(filepath.Join(cacheDir, filepath.FromSlash(path))); err == nil {
			return b
		}
	}
	resp, err := http.Get(baseURL + path)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("%s: %s", path, resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	if cacheDir != "" {
		p := filepath.Join(cacheDir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			log.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			log.Fatal(err)
		}
	}
	return b
}

// eachLine calls fn with the ';'-separated, trimmed fields of every data line
// (comments stripped).
func eachLine(data []byte, fn func(fields []string)) {
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, ";")
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		fn(fields)
	}
	if err := sc.Err(); err != nil {
		log.Fatal(err)
	}
}

func parseHex(s string) rune {
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil || v > maxRune {
		log.Fatalf("bad code point %q", s)
	}
	return rune(v)
}

// parseRange parses "XXXX" or "XXXX..YYYY".
func parseRange(s string) (lo, hi rune) {
	a, b, ok := strings.Cut(s, "..")
	lo = parseHex(a)
	if !ok {
		return lo, lo
	}
	return lo, parseHex(b)
}

func parseSeq(s string) []rune {
	var seq []rune
	for f := range strings.FieldsSeq(s) {
		seq = append(seq, parseHex(f))
	}
	return seq
}

// set is a code point set as a bitmap over the whole code space.
type set []uint64

func newSet() set { return make(set, (maxRune+64)/64) }

func (s set) add(lo, hi rune) {
	for c := lo; c <= hi; c++ {
		s[c>>6] |= 1 << (c & 63)
	}
}

func (s set) has(c rune) bool { return s[c>>6]&(1<<(c&63)) != 0 }

// ranges returns the set as sorted inclusive [lo, hi] pairs.
func (s set) ranges() [][2]rune {
	var out [][2]rune
	for c := rune(0); c <= maxRune; c++ {
		if !s.has(c) {
			continue
		}
		lo := c
		for c+1 <= maxRune && s.has(c+1) {
			c++
		}
		out = append(out, [2]rune{lo, c})
	}
	return out
}

type gen struct {
	gc           map[string]set    // two-letter General_Category -> set
	gcAliases    map[string]string // any gc value alias -> canonical short name
	gcGroups     map[string][]string
	scripts      map[string]set    // short script name -> set
	scAliases    map[string]string // any script value alias -> short name
	scx          []scxEntry
	binary       map[string]set
	scf          map[rune]rune // simple case folding (C + S)
	canonNonU    map[rune]rune // non-u Canonicalize, identity entries omitted
	strChars     map[string]set
	strSequences map[string][][]rune
}

type scxEntry struct {
	lo, hi  rune
	scripts []string // short names, sorted
}

func (g *gen) load() {
	g.loadGC()
	g.loadScripts()
	g.loadBinary()
	g.loadCase()
	g.loadStrings()
}

func (g *gen) loadGC() {
	g.gc = map[string]set{}
	eachLine(fetch("ucd/extracted/DerivedGeneralCategory.txt"), func(f []string) {
		lo, hi := parseRange(f[0])
		s := g.gc[f[1]]
		if s == nil {
			s = newSet()
			g.gc[f[1]] = s
		}
		s.add(lo, hi)
	})
	// Every code point has exactly one category.
	all := newSet()
	for _, s := range g.gc {
		for i := range s {
			if all[i]&s[i] != 0 {
				log.Fatal("overlapping general categories")
			}
			all[i] |= s[i]
		}
	}
	if r := all.ranges(); len(r) != 1 || r[0] != [2]rune{0, maxRune} {
		log.Fatal("general categories do not cover the code space")
	}
	g.gcAliases = map[string]string{}
	g.gcGroups = map[string][]string{}
	pva := fetch("ucd/PropertyValueAliases.txt")
	sc := bufio.NewScanner(bytes.NewReader(pva))
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "gc ") {
			continue
		}
		body, comment, _ := strings.Cut(line, "#")
		fields := strings.Split(body, ";")
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		short := fields[1]
		for _, a := range fields[1:] {
			if a != "" {
				g.gcAliases[a] = short
			}
		}
		if comment = strings.TrimSpace(comment); comment != "" {
			for m := range strings.SplitSeq(comment, "|") {
				g.gcGroups[short] = append(g.gcGroups[short], strings.TrimSpace(m))
			}
		} else if g.gc[short] == nil {
			log.Fatalf("gc %s has no code points", short)
		}
	}
	for short, members := range g.gcGroups {
		s := newSet()
		for _, m := range members {
			for i := range s {
				s[i] |= g.gc[m][i]
			}
		}
		g.gc[short] = s
	}
}

func (g *gen) loadScripts() {
	g.scAliases = map[string]string{}
	long2short := map[string]string{}
	eachLine(fetch("ucd/PropertyValueAliases.txt"), func(f []string) {
		if f[0] != "sc" {
			return
		}
		for _, a := range f[1:] {
			g.scAliases[a] = f[1]
		}
		long2short[f[2]] = f[1]
	})
	g.scripts = map[string]set{}
	assigned := newSet()
	eachLine(fetch("ucd/Scripts.txt"), func(f []string) {
		lo, hi := parseRange(f[0])
		short, ok := long2short[f[1]]
		if !ok {
			log.Fatalf("unknown script %s", f[1])
		}
		s := g.scripts[short]
		if s == nil {
			s = newSet()
			g.scripts[short] = s
		}
		s.add(lo, hi)
		assigned.add(lo, hi)
	})
	unknown := newSet()
	for i := range unknown {
		unknown[i] = ^assigned[i]
	}
	g.scripts["Zzzz"] = unknown
	// Values without code points (Hrkt) are valid and match nothing.
	for _, short := range g.scAliases {
		if g.scripts[short] == nil {
			g.scripts[short] = newSet()
		}
	}
	eachLine(fetch("ucd/ScriptExtensions.txt"), func(f []string) {
		lo, hi := parseRange(f[0])
		names := strings.Fields(f[1])
		for _, n := range names {
			if g.scripts[n] == nil {
				log.Fatalf("unknown script extension %s", n)
			}
		}
		slices.Sort(names)
		g.scx = append(g.scx, scxEntry{lo, hi, names})
	})
	slices.SortFunc(g.scx, func(a, b scxEntry) int { return int(a.lo - b.lo) })
}

func (g *gen) loadBinary() {
	want := map[string]bool{}
	for _, p := range binaryProps {
		want[p[0]] = true
	}
	g.binary = map[string]set{}
	for _, file := range binaryPropFiles {
		eachLine(fetch(file), func(f []string) {
			if len(f) != 2 || !want[f[1]] {
				return
			}
			s := g.binary[f[1]]
			if s == nil {
				s = newSet()
				g.binary[f[1]] = s
			}
			lo, hi := parseRange(f[0])
			s.add(lo, hi)
		})
	}
	// Check the aliases against PropertyAliases.txt.
	aliases := map[string][]string{}
	eachLine(fetch("ucd/PropertyAliases.txt"), func(f []string) {
		if len(f) >= 2 {
			aliases[f[1]] = f
		}
	})
	for _, p := range binaryProps {
		switch p[0] {
		case "Any", "ASCII", "Assigned":
			continue
		}
		if g.binary[p[0]] == nil {
			log.Fatalf("binary property %s not found", p[0])
		}
		if p[1] != "" && !slices.Contains(aliases[p[0]], p[1]) {
			log.Fatalf("alias %s of %s not in PropertyAliases.txt", p[1], p[0])
		}
	}
}

func (g *gen) loadCase() {
	g.scf = map[rune]rune{}
	eachLine(fetch("ucd/CaseFolding.txt"), func(f []string) {
		if f[1] == "C" || f[1] == "S" {
			g.scf[parseHex(f[0])] = parseHex(f[2])
		}
	})
	upper := map[rune][]rune{}
	eachLine(fetch("ucd/UnicodeData.txt"), func(f []string) {
		if f[12] != "" {
			upper[parseHex(f[0])] = []rune{parseHex(f[12])}
		}
	})
	// Unconditional full mappings replace the simple ones (the Default Case
	// Conversion algorithm of toUppercase).
	eachLine(fetch("ucd/SpecialCasing.txt"), func(f []string) {
		if len(f) > 5 && f[4] != "" {
			return // conditional
		}
		c := parseHex(f[0])
		if u := parseSeq(f[3]); len(u) != 1 || u[0] != c {
			upper[c] = u
		} else {
			delete(upper, c)
		}
	})
	// Canonicalize (ECMA-262 22.2.2.7.3) without u/v: toUppercase of the
	// code unit, kept only when it is a single code unit and does not map a
	// non-ASCII unit to ASCII.
	g.canonNonU = map[rune]rune{}
	for c := rune(0); c <= 0xFFFF; c++ {
		u, ok := upper[c]
		if !ok || c >= 0xD800 && c < 0xE000 {
			continue
		}
		if len(utf16.Encode(u)) != 1 {
			continue
		}
		cu := u[0]
		if c >= 128 && cu < 128 {
			continue
		}
		if cu != c {
			g.canonNonU[c] = cu
		}
	}
}

func (g *gen) loadStrings() {
	g.strChars = map[string]set{}
	g.strSequences = map[string][][]rune{}
	for _, p := range stringProps {
		g.strChars[p] = newSet()
	}
	add := func(f []string) {
		p := f[1]
		if g.strChars[p] == nil {
			return
		}
		if lo, hi, ok := strings.Cut(f[0], ".."); ok {
			g.strChars[p].add(parseHex(lo), parseHex(hi))
			return
		}
		seq := parseSeq(f[0])
		if len(seq) == 1 {
			g.strChars[p].add(seq[0], seq[0])
			return
		}
		g.strSequences[p] = append(g.strSequences[p], seq)
	}
	eachLine(fetch("emoji/emoji-sequences.txt"), add)
	eachLine(fetch("emoji/emoji-zwj-sequences.txt"), add)
	for _, p := range stringProps {
		slices.SortFunc(g.strSequences[p], slices.Compare[[]rune])
		if len(g.strSequences[p]) == 0 && p != "Basic_Emoji" {
			log.Fatalf("no sequences for %s", p)
		}
	}
}

// encodeRanges encodes sorted inclusive ranges as uvarint pairs (gap from
// the previous range end, length - 1), the format decodeRanges reads.
func encodeRanges(r [][2]rune) string {
	var b []byte
	next := rune(0)
	for _, x := range r {
		b = binary.AppendUvarint(b, uint64(x[0]-next))
		b = binary.AppendUvarint(b, uint64(x[1]-x[0]))
		next = x[1] + 1
	}
	return string(b)
}

// encodeMap encodes a code point mapping as uvarint (gap from the previous
// key + 1) and zigzag varint (value - key) pairs, in key order.
func encodeMap(m map[rune]rune) string {
	keys := make([]rune, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b []byte
	next := rune(0)
	for _, k := range keys {
		b = binary.AppendUvarint(b, uint64(k-next))
		b = binary.AppendVarint(b, int64(m[k]-k))
		next = k + 1
	}
	return string(b)
}

// encodeSequences encodes each sequence as uvarint length then uvarint code
// points.
func encodeSequences(seqs [][]rune) string {
	var b []byte
	for _, s := range seqs {
		b = binary.AppendUvarint(b, uint64(len(s)))
		for _, c := range s {
			b = binary.AppendUvarint(b, uint64(c))
		}
	}
	return string(b)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// quote renders data as a Go string literal split into lines of about 100
// source bytes.
func quote(data string) string {
	if data == "" {
		return `""`
	}
	var sb strings.Builder
	line := 0
	sb.WriteString("\"")
	for i := 0; i < len(data); i++ {
		c := data[i]
		var piece string
		switch {
		case c == '"' || c == '\\':
			piece = `\` + string(c)
		case c >= 0x20 && c < 0x7F:
			piece = string(c)
		default:
			piece = fmt.Sprintf(`\x%02x`, c)
		}
		if line+len(piece) > 100 {
			sb.WriteString("\" +\n\t\"")
			line = 0
		}
		sb.WriteString(piece)
		line += len(piece)
	}
	sb.WriteString("\"")
	return sb.String()
}

func (g *gen) emit() []byte {
	var b bytes.Buffer
	p := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	p("// Code generated by internal/gen/ucd from the Unicode %s UCD; DO NOT EDIT.\n\n", version)
	p("package regexpsyntax\n\n")
	p("// unicodeVersion is the UCD version of every table below; it matches the\n")
	p("// version tc39/test262 generates its property-escape tests from.\n")
	p("const unicodeVersion = %q\n\n", version)

	p("// ucdGeneralCategories holds the General_Category values by short name\n")
	p("// (groups such as L and LC included) in decodeRanges format.\n")
	p("var ucdGeneralCategories = [...]ucdTable{\n")
	for _, k := range sortedKeys(g.gc) {
		p("{%q, %s},\n", k, quote(encodeRanges(g.gc[k].ranges())))
	}
	p("}\n\n")

	p("// ucdGeneralCategoryAliases maps every General_Category value alias of\n")
	p("// PropertyValueAliases.txt to its short name.\n")
	p("var ucdGeneralCategoryAliases = [...]ucdAlias{\n")
	for _, k := range sortedKeys(g.gcAliases) {
		p("{%q, %q},\n", k, g.gcAliases[k])
	}
	p("}\n\n")

	p("// ucdScripts holds the Script values by short name (Zzzz is Unknown).\n")
	p("var ucdScripts = [...]ucdTable{\n")
	for _, k := range sortedKeys(g.scripts) {
		p("{%q, %s},\n", k, quote(encodeRanges(g.scripts[k].ranges())))
	}
	p("}\n\n")

	p("// ucdScriptAliases maps every Script value alias to its short name.\n")
	p("var ucdScriptAliases = [...]ucdAlias{\n")
	for _, k := range sortedKeys(g.scAliases) {
		p("{%q, %q},\n", k, g.scAliases[k])
	}
	p("}\n\n")

	scriptIndex := map[string]int{}
	for i, k := range sortedKeys(g.scripts) {
		scriptIndex[k] = i
	}
	var scx []byte
	next := rune(0)
	for _, e := range g.scx {
		scx = binary.AppendUvarint(scx, uint64(e.lo-next))
		scx = binary.AppendUvarint(scx, uint64(e.hi-e.lo))
		scx = binary.AppendUvarint(scx, uint64(len(e.scripts)))
		for _, s := range e.scripts {
			scx = binary.AppendUvarint(scx, uint64(scriptIndex[s]))
		}
		next = e.hi + 1
	}
	p("// ucdScriptExtensions lists the ScriptExtensions.txt entries: uvarint gap,\n")
	p("// length - 1, script count and ucdScripts indices per range. Code points\n")
	p("// without an entry have Script_Extensions = {Script}.\n")
	p("const ucdScriptExtensions = %s\n\n", quote(string(scx)))

	p("// ucdBinaryProperties holds the binary properties of ECMA-262 table 67\n")
	p("// by canonical name, except the derived Any, ASCII and Assigned.\n")
	p("var ucdBinaryProperties = [...]ucdTable{\n")
	for _, k := range sortedKeys(g.binary) {
		p("{%q, %s},\n", k, quote(encodeRanges(g.binary[k].ranges())))
	}
	p("}\n\n")

	aliases := map[string]string{}
	for _, bp := range binaryProps {
		aliases[bp[0]] = bp[0]
		if bp[1] != "" {
			aliases[bp[1]] = bp[0]
		}
	}
	p("// ucdBinaryAliases maps the names and aliases of table 67 to the\n")
	p("// canonical name.\n")
	p("var ucdBinaryAliases = [...]ucdAlias{\n")
	for _, k := range sortedKeys(aliases) {
		p("{%q, %q},\n", k, aliases[k])
	}
	p("}\n\n")

	p("// ucdSimpleFolding is simple case folding (CaseFolding.txt C and S) in\n")
	p("// decodeMap format.\n")
	p("const ucdSimpleFolding = %s\n\n", quote(encodeMap(g.scf)))
	p("// ucdCanonNonUnicode is Canonicalize without u/v (toUppercase restricted\n")
	p("// to single code units that do not map non-ASCII to ASCII) in decodeMap\n")
	p("// format; unlisted code units map to themselves.\n")
	p("const ucdCanonNonUnicode = %s\n\n", quote(encodeMap(g.canonNonU)))

	p("// ucdStringProperties holds the properties of strings of the v flag: the\n")
	p("// single code points (decodeRanges) and the sequences (decodeSequences).\n")
	p("var ucdStringProperties = [...]ucdStringTable{\n")
	props := slices.Clone(stringProps)
	slices.Sort(props)
	for _, k := range props {
		p("{%q, %s, %s},\n", k, quote(encodeRanges(g.strChars[k].ranges())), quote(encodeSequences(g.strSequences[k])))
	}
	p("}\n")
	return b.Bytes()
}
