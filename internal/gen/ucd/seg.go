package main

// The -seg output: the text segmentation properties of Intl.Segmenter
// (UAX #29) for engine/unicode_seg_tables.go, and with -segtest trimmed
// copies of the three break tests for the engine's tests.

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"go/format"
	"log"
	"math/bits"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// segClass is one class of a segmentation table: the UCD property value it
// stands for ("" for a class the generator derives) and the name of its
// engine constant.
type segClass struct{ value, name string }

// The classes of the three tables, in the order of their values (the low
// bits of an entry). The engine relies on the order of some of them; see the
// rules in engine/segment.go.
var (
	segGraphemeClasses = []segClass{
		{"Other", "segGOther"}, {"CR", "segGCR"}, {"LF", "segGLF"}, {"Control", "segGControl"},
		{"Extend", "segGExtend"}, {"ZWJ", "segGZWJ"}, {"Regional_Indicator", "segGRI"},
		{"Prepend", "segGPrepend"}, {"SpacingMark", "segGSpacingMark"}, {"L", "segGL"},
		{"V", "segGV"}, {"T", "segGT"}, {"LV", "segGLV"}, {"LVT", "segGLVT"},
		{"", "segGExtPict"}, {"", "segGConsonant"},
	}
	segWordClasses = []segClass{
		{"Other", "segWOther"}, {"CR", "segWCR"}, {"LF", "segWLF"}, {"Newline", "segWNewline"},
		{"Extend", "segWExtend"}, {"Format", "segWFormat"}, {"ZWJ", "segWZWJ"},
		{"Regional_Indicator", "segWRI"}, {"Katakana", "segWKatakana"},
		{"Hebrew_Letter", "segWHebrewLetter"}, {"ALetter", "segWALetter"}, {"Numeric", "segWNumeric"},
		{"ExtendNumLet", "segWExtendNumLet"}, {"Single_Quote", "segWSingleQuote"},
		{"MidNumLet", "segWMidNumLet"}, {"MidLetter", "segWMidLetter"}, {"MidNum", "segWMidNum"},
		{"Double_Quote", "segWDoubleQuote"}, {"WSegSpace", "segWWSegSpace"},
	}
	segSentenceClasses = []segClass{
		{"Other", "segSOther"}, {"CR", "segSCR"}, {"LF", "segSLF"}, {"Sep", "segSSep"},
		{"Extend", "segSExtend"}, {"Format", "segSFormat"}, {"Sp", "segSSp"}, {"Lower", "segSLower"},
		{"Upper", "segSUpper"}, {"OLetter", "segSOLetter"}, {"Numeric", "segSNumeric"},
		{"ATerm", "segSATerm"}, {"STerm", "segSSTerm"}, {"SContinue", "segSSContinue"},
		{"Close", "segSClose"},
	}
)

// Flags above the class bits.
const (
	segGLinker     = 1 << 4 // grapheme: Indic_Conjunct_Break=Linker
	segGInCBExtend = 1 << 5 // grapheme: Indic_Conjunct_Break=Extend
	segWExtPict    = 1 << 5 // word: Extended_Pictographic
	segWWordLike   = 1 << 6 // word: ICU's status of a word ending here is letter, number, kana or ideo
	segWBare       = 1 << 7 // word: only when no Extend or Format follows (Hangul syllables)
)

// segBlock is the number of code points of a data block, segBMP the number
// of blocks below U+10000 and segSup the number of 4096-code-point index
// blocks above it.
const (
	segBlock = 64
	segBMP   = 0x10000 / segBlock
	segSup   = (maxRune + 1 - 0x10000) >> 12
)

// segProp reads the property file path into vals by the classes' values; a
// code point it does not list keeps class 0 (Other).
func segProp(path string, classes []segClass) []uint8 {
	byValue := map[string]uint8{}
	for i, c := range classes {
		if c.value != "" {
			byValue[c.value] = uint8(i)
		}
	}
	vals := make([]uint8, maxRune+1)
	eachLine(fetch(path), func(f []string) {
		v, ok := byValue[f[1]]
		if !ok {
			log.Fatalf("%s: unknown value %q", path, f[1])
		}
		lo, hi := parseRange(f[0])
		for c := lo; c <= hi; c++ {
			vals[c] = v
		}
	})
	return vals
}

// segSet reads the code points whose field 1 (field 2 when value is not "")
// matches name in the file path.
func segSet(path, name, value string) set {
	s := newSet()
	eachLine(fetch(path), func(f []string) {
		if f[1] == name && (value == "" || len(f) > 2 && f[2] == value) {
			lo, hi := parseRange(f[0])
			s.add(lo, hi)
		}
	})
	return s
}

type segGen struct {
	grapheme, word, sentence []uint8
}

// segValue returns the value of the class whose constant is name.
func segValue(classes []segClass, name string) uint8 {
	for i, c := range classes {
		if c.name == name {
			return uint8(i)
		}
	}
	log.Fatalf("no class %s", name)
	return 0
}

func (g *segGen) load() {
	extPict := segSet("ucd/emoji/emoji-data.txt", "Extended_Pictographic", "")
	g.grapheme = segProp("ucd/auxiliary/GraphemeBreakProperty.txt", segGraphemeClasses)
	gcb := slices.Clone(g.grapheme)
	consonant := segSet("ucd/DerivedCoreProperties.txt", "InCB", "Consonant")
	linker := segSet("ucd/DerivedCoreProperties.txt", "InCB", "Linker")
	incbExtend := segSet("ucd/DerivedCoreProperties.txt", "InCB", "Extend")
	gc := func(name string) uint8 { return segValue(segGraphemeClasses, name) }
	for c := range rune(maxRune + 1) {
		v := gcb[c]
		switch {
		case extPict.has(c):
			// GB11 matches Extended_Pictographic as a class of its own.
			if v != gc("segGOther") || consonant.has(c) {
				log.Fatalf("%U: Extended_Pictographic that is not Other", c)
			}
			v = gc("segGExtPict")
		case consonant.has(c):
			if v != gc("segGOther") {
				log.Fatalf("%U: InCB=Consonant that is not Other", c)
			}
			v = gc("segGConsonant")
		case linker.has(c):
			// The engine looks for GB9c's linkers and extenders among the
			// characters GB9 never breaks before.
			if v != gc("segGExtend") {
				log.Fatalf("%U: InCB=Linker that is not Extend", c)
			}
			v |= segGLinker
		case incbExtend.has(c):
			if v != gc("segGExtend") && v != gc("segGZWJ") {
				log.Fatalf("%U: InCB=Extend that is not Extend or ZWJ", c)
			}
			v |= segGInCBExtend
		}
		g.grapheme[c] = v
	}

	// isWordLike follows ICU's word rules (icu4c/source/data/brkitr/rules/
	// word.txt): the status of a word is that of its final base character,
	// letter for $ALetterPlus, $Hebrew_Letter and $HangulSyllable, number
	// for $Numeric, kana or ideo for $Katakana, $Hiragana and $Ideographic,
	// none for the rest. $ALetterPlus is ALetter less the CJK dictionary
	// characters (Han, Hiragana, Katakana, Hangul syllables) plus the
	// Line_Break=Complex_Context characters that are no Extend (less Han)
	// or Control. $HangulSyllable's rule takes no Extend or Format after it.
	g.word = segProp("ucd/auxiliary/WordBreakProperty.txt", segWordClasses)
	wc := func(name string) uint8 { return segValue(segWordClasses, name) }
	han := segSet("ucd/Scripts.txt", "Han", "")
	hiragana := segSet("ucd/Scripts.txt", "Hiragana", "")
	ideographic := segSet("ucd/PropList.txt", "Ideographic", "")
	sa := segSet("ucd/LineBreak.txt", "SA", "")
	for c := range rune(maxRune + 1) {
		v := g.word[c]
		hangul := c >= 0xAC00 && c <= 0xD7A3
		icuExtend := v == wc("segWExtend") && !han.has(c)
		aLetterPlus := v == wc("segWALetter") && !han.has(c) && !hiragana.has(c) && !hangul ||
			sa.has(c) && !icuExtend && gcb[c] != gc("segGControl")
		var like uint8
		switch {
		case v == wc("segWExtend") || v == wc("segWFormat") || v == wc("segWZWJ"):
			// They take the status of what they follow.
		case hangul:
			like = segWWordLike | segWBare
		case aLetterPlus, hiragana.has(c), ideographic.has(c),
			v == wc("segWHebrewLetter"), v == wc("segWNumeric"), v == wc("segWKatakana"):
			like = segWWordLike
		}
		if extPict.has(c) {
			v |= segWExtPict
		}
		g.word[c] = v | like
	}

	g.sentence = segProp("ucd/auxiliary/SentenceBreakProperty.txt", segSentenceClasses)
}

// segTrie encodes vals, one per code point, in the format of the engine's
// segTrie: data blocks of segBlock entries, each stored once; the data block
// of each block below U+10000; and for each 4096 code points above it an
// index block, each stored once, of the data blocks of its blocks. Blocks
// are numbered in the order they first appear in the BMP index then the
// index blocks.
//
// The encoding is: uvarint palette length and the palette (the distinct
// values); uvarint data and index block counts; the data as runs, each a
// uvarint (length - 1) << bits | palette position, bits the width of a
// palette position; then the block numbers of the BMP index, of the index
// blocks and of the supplementary index, as uvarints n << 1 for n blocks
// that each appear for the first time and n << 1 | 1, k for n times block
// k. It also returns a description of the sizes.
func segTrie(vals []uint8) (string, string) {
	var data []uint8
	dataAt := map[string]int{}
	dataBlock := func(lo rune) int {
		b := vals[lo : lo+segBlock]
		k, ok := dataAt[string(b)]
		if !ok {
			k = len(data) / segBlock
			dataAt[string(b)] = k
			data = append(data, b...)
		}
		return k
	}
	bmp := make([]int, segBMP)
	for b := range bmp {
		bmp[b] = dataBlock(rune(b) * segBlock)
	}
	// The engine reads ASCII from the first 128 data entries.
	if bmp[0] != 0 || bmp[1] != 1 {
		log.Fatal("ASCII is not in the first two data blocks")
	}
	var index []int
	indexAt := map[string]int{}
	sup := make([]int, segSup)
	for k := range sup {
		ib := make([]int, 4096/segBlock)
		for j := range ib {
			ib[j] = dataBlock(0x10000 + rune(k)<<12 + rune(j)*segBlock)
		}
		key := fmt.Sprint(ib)
		n, ok := indexAt[key]
		if !ok {
			n = len(index) / len(ib)
			indexAt[key] = n
			index = append(index, ib...)
		}
		sup[k] = n
	}
	// The engine keeps data and index offsets in uint16s.
	if len(data) > 1<<16 || len(index) > 1<<16 {
		log.Fatal("segmentation table too large")
	}

	var palette []uint8
	for _, v := range data {
		if !slices.Contains(palette, v) {
			palette = append(palette, v)
		}
	}
	slices.Sort(palette)
	width := bits.Len(uint(len(palette) - 1))
	b := binary.AppendUvarint(nil, uint64(len(palette)))
	b = append(b, palette...)
	b = binary.AppendUvarint(b, uint64(len(data)/segBlock))
	b = binary.AppendUvarint(b, uint64(len(index)/(4096/segBlock)))
	for i := 0; i < len(data); {
		j := i + 1
		for j < len(data) && data[j] == data[i] {
			j++
		}
		b = binary.AppendUvarint(b, uint64(j-i-1)<<width|uint64(slices.Index(palette, data[i])))
		i = j
	}
	fresh := 0
	blocks := func(nums []int) {
		for i := 0; i < len(nums); {
			j := i
			for j < len(nums) && nums[j] == fresh {
				j++
				fresh++
			}
			if j > i {
				b = binary.AppendUvarint(b, uint64(j-i)<<1)
				i = j
				continue
			}
			for j < len(nums) && nums[j] == nums[i] {
				j++
			}
			b = binary.AppendUvarint(b, uint64(j-i)<<1|1)
			b = binary.AppendUvarint(b, uint64(nums[i]))
			i = j
		}
	}
	blocks(bmp)
	blocks(index)
	fresh = 0
	blocks(sup)
	decoded := len(data) + 2*(segBMP+segSup+len(index))
	desc := fmt.Sprintf("%d bytes for %d data blocks and %d index blocks, %d bytes decoded",
		len(b), len(data)/segBlock, len(index)/(4096/segBlock), decoded)
	return string(b), desc
}

func (g *segGen) emit() []byte {
	var b bytes.Buffer
	p := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	p("// Code generated by internal/gen/ucd -seg from the Unicode %s UCD; DO NOT EDIT.\n\n", version)
	p("%s", notice)
	p("package engine\n\n")
	p("// ucdSegVersion is the UCD version of the segmentation tables.\n")
	p("const ucdSegVersion = %q\n\n", version)
	consts := func(doc string, classes []segClass) {
		p("%s\nconst (\n", doc)
		for i, c := range classes {
			if i == 0 {
				p("%s uint8 = iota", c.name)
			} else {
				p("%s", c.name)
			}
			if c.value != "" {
				p(" // %s", c.value)
			}
			p("\n")
		}
		p(")\n\n")
	}
	consts("// Grapheme classes: Grapheme_Cluster_Break, with Extended_Pictographic and\n"+
		"// Indic_Conjunct_Break=Consonant (all Other) split off Other.", segGraphemeClasses)
	consts("// Word classes: Word_Break.", segWordClasses)
	consts("// Sentence classes: Sentence_Break.", segSentenceClasses)
	p("// Flags above the class bits.\n")
	p("const (\n")
	for _, f := range []struct {
		name string
		v    int
		doc  string
	}{
		{"segGLinker", segGLinker, "grapheme: Indic_Conjunct_Break=Linker (all Extend)"},
		{"segGInCBExtend", segGInCBExtend, "grapheme: Indic_Conjunct_Break=Extend (all Extend or ZWJ)"},
		{"segWExtPict", segWExtPict, "word: Extended_Pictographic"},
		{"segWWordLike", segWWordLike, "word: ICU gives a word ending in it a letter, number, kana or ideo status"},
		{"segWBare", segWBare, "word: only when no Extend, Format or ZWJ follows it (Hangul syllables)"},
	} {
		p("%s = 1 << %d // %s\n", f.name, bits.Len(uint(f.v))-1, f.doc)
	}
	p(")\n\n")
	for _, t := range []struct {
		name, what string
		vals       []uint8
	}{
		{"segGraphemeTrie", "grapheme", g.grapheme},
		{"segWordTrie", "word", g.word},
		{"segSentenceTrie", "sentence", g.sentence},
	} {
		enc, desc := segTrie(t.vals)
		p("// %s is the %s table in segTrie encoding: %s.\n", t.name, t.what, desc)
		p("const %s = %s\n\n", t.name, quote(enc))
	}
	return b.Bytes()
}

// segTestFixture trims one of the break tests: comments and blank lines go,
// but for the copyright line.
func segTestFixture(name string) []byte {
	var b bytes.Buffer
	data := fetch("ucd/auxiliary/" + name + ".txt")
	fmt.Fprintf(&b, "# %s-%s.txt trimmed by internal/gen/ucd -segtest: comments are dropped.\n", name, version)
	fmt.Fprintf(&b, "%s\n# Unicode License v3; see THIRD_PARTY_NOTICES.\n", copyrightLine(data))
	for line := range strings.Lines(string(data)) {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		if line = strings.TrimSpace(line); line != "" {
			b.WriteString(line + "\n")
		}
	}
	var z bytes.Buffer
	zw, err := gzip.NewWriterLevel(&z, gzip.BestCompression)
	if err != nil {
		log.Fatal(err)
	}
	zw.Write(b.Bytes())
	if err := zw.Close(); err != nil {
		log.Fatal(err)
	}
	return z.Bytes()
}

func writeSeg(out, testDir string) {
	var g segGen
	g.load()
	src, err := format.Source(g.emit())
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(out, src, 0o644); err != nil {
		log.Fatal(err)
	}
	if testDir == "" {
		return
	}
	for name, file := range map[string]string{
		"GraphemeBreakTest": "grapheme_break_test.txt.gz",
		"WordBreakTest":     "word_break_test.txt.gz",
		"SentenceBreakTest": "sentence_break_test.txt.gz",
	} {
		if err := os.WriteFile(filepath.Join(testDir, file), segTestFixture(name), 0o644); err != nil {
			log.Fatal(err)
		}
	}
}
