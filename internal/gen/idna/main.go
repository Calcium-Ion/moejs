// Command idna generates webapi/internal/whatwgurl/idna_tables.go, the UTS #46
// data of the URL parser's domain to ASCII: the IDNA mapping table, and for
// every code point a label may keep, the properties the validity criteria
// test: Bidi_Class (CheckBidi), Joining_Type and Canonical_Combining_Class
// Virama (CheckJoiners) and General_Category Mark (a label must not begin
// with a combining mark).
//
// The files are downloaded from unicode.org at generation time only (-cache
// keeps a local copy); the output is deterministic and committed. The version
// is the one internal/gen/ucd pins.
//
// Usage (from webapi/internal/whatwgurl/, see the go:generate line in idna.go):
//
//	go run ../../../internal/gen/idna -o idna_tables.go [-cache dir]
//
// idnaTable is a sorted list of ranges, one uint32 each: the first code
// point of the range above bit 11, then either the kind in bits 0-1 (bit 10
// clear) or an index into idnaData in bits 0-9 (bit 10 set). A range runs up
// to the next one. The kinds (the kind* constants below):
//
//	disallowed  the code points are disallowed
//	ignored     the code points are removed
//	valid       the code points are valid (or deviations, which nontransitional
//	            processing keeps)
//	pairs       a code point at an even offset from the start maps to the next
//	            one, which is valid
//
// An idnaData entry with bit 31 clear is a delta range: every code point maps
// to itself plus the entry minus 2^21. With bit 31 set it is a strings range:
// the code point at offset i maps to idnaPool[off+i*stride:][:n], where the
// entry holds off in bits 16-30, stride in bits 8-15 and n in bits 0-7
// (stride 0: every code point maps to the same string).
//
// idnaProps holds the properties of the valid code points, one uint32 per
// run: the first code point of the run above bit 9, the properties (prop*
// below) in bits 0-8. A run runs up to the next one and through code points
// that are not valid, whose properties do not matter.
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

const version = "17.0.0"

const baseURL = "https://www.unicode.org/Public/" + version + "/"

const maxRune = 0x10FFFF

// The kinds of a range, as idna.go reads them.
const (
	kindDisallowed = iota
	kindIgnored
	kindValid
	kindPairs
	kindDelta   // in idnaData
	kindStrings // in idnaData
)

// The properties of a kept code point: the Bidi_Class in the low four bits,
// the Joining_Type in the next three, then the Mark and Virama flags.
const (
	bidiL = iota
	bidiR
	bidiAL
	bidiAN
	bidiEN
	bidiES
	bidiCS
	bidiET
	bidiON
	bidiBN
	bidiNSM
	bidiOther
)

const (
	joinU = iota // and Join_Causing, which the CONTEXTJ rules do not name
	joinL
	joinR
	joinD
	joinT
)

const (
	propJoinShift = 4
	propMark      = 1 << 7
	propVirama    = 1 << 8
	propBits      = 9
)

var bidiNames = map[string]int{
	"L": bidiL, "R": bidiR, "AL": bidiAL, "AN": bidiAN, "EN": bidiEN, "ES": bidiES, "CS": bidiCS,
	"ET": bidiET, "ON": bidiON, "BN": bidiBN, "NSM": bidiNSM,
}

var joinNames = map[string]int{"L": joinL, "R": joinR, "D": joinD, "T": joinT}

const (
	stDisallowed = iota
	stIgnored
	stValid
	stMapped
)

var (
	out   = flag.String("o", "idna_tables.go", "output file")
	cache = flag.String("cache", "", "directory to cache the downloaded files in")
)

func main() {
	log.SetFlags(0)
	flag.Parse()

	status := make([]uint8, maxRune+1)
	mapping := make([]string, maxRune+1)
	props := make([]uint16, maxRune+1)

	ranges(fetch("idna/IdnaMappingTable.txt"), func(lo, hi rune, f []string) {
		var st uint8
		switch f[0] {
		case "valid", "deviation":
			st = stValid
		case "ignored":
			st = stIgnored
		case "mapped":
			st = stMapped
		case "disallowed":
			st = stDisallowed
		default:
			log.Fatalf("IdnaMappingTable: status %q", f[0])
		}
		var m string
		if st == stMapped {
			if len(f) < 2 || f[1] == "" {
				log.Fatalf("IdnaMappingTable: %04X mapped to nothing", lo)
			}
			m = codePoints(f[1])
		}
		for r := lo; r <= hi; r++ {
			status[r], mapping[r] = st, m
		}
	})
	ranges(fetch("ucd/extracted/DerivedBidiClass.txt"), func(lo, hi rune, f []string) {
		b, ok := bidiNames[f[0]]
		if !ok {
			b = bidiOther
		}
		for r := lo; r <= hi; r++ {
			props[r] = props[r]&^0xf | uint16(b)
		}
	})
	ranges(fetch("ucd/extracted/DerivedJoiningType.txt"), func(lo, hi rune, f []string) {
		j := joinNames[f[0]]
		for r := lo; r <= hi; r++ {
			props[r] |= uint16(j) << propJoinShift
		}
	})
	ranges(fetch("ucd/extracted/DerivedGeneralCategory.txt"), func(lo, hi rune, f []string) {
		if f[0] == "Mn" || f[0] == "Mc" || f[0] == "Me" {
			for r := lo; r <= hi; r++ {
				props[r] |= propMark
			}
		}
	})
	ranges(fetch("ucd/extracted/DerivedCombiningClass.txt"), func(lo, hi rune, f []string) {
		if f[0] == "9" {
			for r := lo; r <= hi; r++ {
				props[r] |= propVirama
			}
		}
	})

	// single reports the target of a code point mapped to one code point.
	single := func(r rune) (rune, bool) {
		if status[r] != stMapped {
			return 0, false
		}
		t, n := utf8.DecodeRuneInString(mapping[r])
		return t, n == len(mapping[r])
	}
	var table []uint32
	var data []uint32
	dataAt := map[uint32]uint32{}
	var pool strings.Builder
	intern := func(m string) int {
		at := strings.Index(pool.String(), m)
		if at < 0 {
			at = pool.Len()
			pool.WriteString(m)
		}
		return at
	}
	// emit appends a range; a delta or strings range takes an index into data
	// for its data, which ranges with the same data share.
	poolRef := func(off, stride, n int) uint32 {
		if off >= 1<<15 {
			log.Fatalf("pool of %d bytes", off)
		}
		return uint32(off)<<16 | uint32(stride)<<8 | uint32(n)
	}
	emit := func(lo rune, kind int, d uint32) {
		v := uint32(kind)
		switch kind {
		case kindDelta, kindStrings:
			if kind == kindStrings {
				d |= 1 << 31
			}
			at, ok := dataAt[d]
			if !ok {
				at = uint32(len(data))
				data = append(data, d)
				dataAt[d] = at
			}
			v = 1<<10 | at
		}
		if v >= 1<<11 {
			log.Fatalf("U+%04X: %d data entries", lo, len(data))
		}
		table = append(table, uint32(lo)<<11|v)
	}
	for r := rune(0); r <= maxRune; {
		switch status[r] {
		case stDisallowed, stIgnored, stValid:
			e := r + 1
			for e <= maxRune && status[e] == status[r] {
				e++
			}
			emit(r, []int{stDisallowed: kindDisallowed, stIgnored: kindIgnored, stValid: kindValid}[status[r]], 0)
			r = e
			continue
		}
		// A run of pairs: r maps to r+1, which is valid, and so on.
		p := r
		for p+1 <= maxRune && status[p+1] == stValid {
			if t, ok := single(p); !ok || t != p+1 {
				break
			}
			p += 2
		}
		if t, ok := single(p); ok && t == p+1 && p > r {
			p++ // the run ends on a mapped code point
		}
		// A run of the same delta.
		d := r
		if t, ok := single(r); ok {
			for d <= maxRune {
				if u, ok := single(d); !ok || u-d != t-r {
					break
				}
				d++
			}
		}
		// A run of the same mapping.
		s := r + 1
		for s <= maxRune && status[s] == stMapped && mapping[s] == mapping[r] {
			s++
		}
		// A run of mappings of the same length.
		f := r + 1
		for f <= maxRune && status[f] == stMapped && len(mapping[f]) == len(mapping[r]) {
			f++
		}
		m := mapping[r]
		if len(m) > 0xff {
			log.Fatalf("U+%04X: mapping of %d bytes", r, len(m))
		}
		switch n := max(p-r, d-r, s-r, f-r); {
		case p-r == n && n >= 2:
			emit(r, kindPairs, 0)
			r = p
		case d-r == n:
			t, _ := single(r)
			emit(r, kindDelta, uint32(t-r+1<<21))
			r = d
		case s-r == n:
			emit(r, kindStrings, poolRef(intern(m), 0, len(m)))
			r = s
		default:
			var b strings.Builder
			for i := r; i < f; i++ {
				b.WriteString(mapping[i])
			}
			emit(r, kindStrings, poolRef(intern(b.String()), len(m), len(m)))
			r = f
		}
	}

	// The properties of the valid code points, in runs that run through the
	// code points that are not valid.
	var propTable []uint32
	for r := rune(0); r <= maxRune; r++ {
		if status[r] == stValid && (len(propTable) == 0 || uint16(propTable[len(propTable)-1]&(1<<propBits-1)) != props[r]) {
			propTable = append(propTable, uint32(r)<<propBits|uint32(props[r]))
		}
	}
	if propTable[0]>>propBits != 0 {
		propTable[0] &= 1<<propBits - 1
	}

	var b bytes.Buffer
	p := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	p("// Code generated by internal/gen/idna from the Unicode %s IDNA mapping table and UCD; DO NOT EDIT.\n\n", version)
	p("// The tables hold Unicode %s IDNA mapping table and Unicode Character\n", version)
	p("// Database data, under the Unicode License v3; see THIRD_PARTY_NOTICES.\n\n")
	p("package whatwgurl\n\n")
	p("// idnaUnicodeVersion is the Unicode version of idnaTable, idnaProps and idnaPool.\n")
	p("const idnaUnicodeVersion = %q\n\n", version)
	p("// idnaTable is the UTS #46 mapping table as internal/gen/idna describes it:\n")
	p("// %d ranges.\n", len(table))
	p("var idnaTable = [...]uint32{")
	for i, e := range table {
		if i%6 == 0 {
			p("\n")
		} else {
			p(" ")
		}
		p("%#08x,", e)
	}
	p("\n}\n\n")
	p("// idnaData holds the data of the delta and strings ranges of idnaTable.\n")
	p("var idnaData = [...]uint32{")
	for i, e := range data {
		if i%6 == 0 {
			p("\n")
		} else {
			p(" ")
		}
		p("%#08x,", e)
	}
	p("\n}\n\n")
	p("// idnaProps holds the properties of the valid code points as\n")
	p("// internal/gen/idna describes them: %d runs.\n", len(propTable))
	p("var idnaProps = [...]uint32{")
	for i, e := range propTable {
		if i%6 == 0 {
			p("\n")
		} else {
			p(" ")
		}
		p("%#08x,", e)
	}
	p("\n}\n\n")
	p("// idnaPool holds the mappings of the strings ranges of idnaTable.\n")
	p("const idnaPool = ")
	ps := pool.String()
	for len(ps) > 0 {
		n := min(len(ps), 48)
		for n < len(ps) && !utf8.RuneStart(ps[n]) {
			n++
		}
		p("%s", quote(ps[:n]))
		ps = ps[n:]
		if len(ps) > 0 {
			p(" +\n\t")
		}
	}
	p("\n")
	src, err := format.Source(b.Bytes())
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, src, 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("%d ranges, %d data, %d property runs (%d bytes), pool %d bytes", len(table), len(data), len(propTable), 4*(len(table)+len(data)+len(propTable)), pool.Len())
}

// quote quotes s with \u escapes for everything outside printable ASCII, so
// the generated file shows no invisible or combining characters.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r >= 0x20 && r < 0x7f:
			b.WriteRune(r)
		case r < 0x10000:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			fmt.Fprintf(&b, `\U%08x`, r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// codePoints decodes a space-separated list of hexadecimal code points.
func codePoints(s string) string {
	var b strings.Builder
	for _, f := range strings.Fields(s) {
		r, err := strconv.ParseUint(f, 16, 32)
		if err != nil {
			log.Fatal(err)
		}
		b.WriteRune(rune(r))
	}
	return b.String()
}

// ranges calls fn for every data line of a UCD file with the code point range
// and the trimmed fields after it.
func ranges(data []byte, fn func(lo, hi rune, fields []string)) {
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line, _, _ := strings.Cut(sc.Text(), "#")
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, ";")
		for i := range f {
			f[i] = strings.TrimSpace(f[i])
		}
		a, z, ok := strings.Cut(f[0], "..")
		if !ok {
			z = a
		}
		lo, err1 := strconv.ParseUint(a, 16, 32)
		hi, err2 := strconv.ParseUint(z, 16, 32)
		if err1 != nil || err2 != nil || lo > hi || hi > maxRune {
			log.Fatalf("bad range %q", f[0])
		}
		fn(rune(lo), rune(hi), f[1:])
	}
	if err := sc.Err(); err != nil {
		log.Fatal(err)
	}
}

// fetch returns a file of the pinned version, from the cache when it has it.
func fetch(name string) []byte {
	var cached string
	if *cache != "" {
		cached = filepath.Join(*cache, version, filepath.FromSlash(name))
		if b, err := os.ReadFile(cached); err == nil {
			return b
		}
	}
	resp, err := http.Get(baseURL + name)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("%s: %s", name, resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	if cached != "" {
		if err := os.MkdirAll(filepath.Dir(cached), 0o755); err != nil {
			log.Fatal(err)
		}
		if err := os.WriteFile(cached, b, 0o644); err != nil {
			log.Fatal(err)
		}
	}
	return b
}
