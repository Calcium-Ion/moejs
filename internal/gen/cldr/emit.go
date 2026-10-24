package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"slices"
	"strings"
)

// packLang packs a language subtag of two or three lowercase letters into
// 15 bits, base 27 with 0 for a missing third letter: codes sort like the
// subtags. The empty language is "und".
func packLang(s string) uint32 {
	if s == "" {
		s = "und"
	}
	var c uint32
	for i := range 3 {
		c *= 27
		if i < len(s) {
			c += uint32(s[i]-'a') + 1
		}
	}
	return c
}

// likelyHeader bits of an encoded likely-subtags entry.
const (
	lhScript     = 1 << iota // the key has a script
	lhRegion                 // the key has a region
	lhLang                   // the language changes
	lhToScript               // the script differs from the key's
	lhToRegion               // the region differs from the key's
	lhDeltaShift = 5         // bits 5-7: the language delta, 7 for a uvarint after the header
)

// encodeLikely encodes the likely subtags sorted by key (packed language,
// script index, region index) and returns them with the script, region and
// language tables they index.
func (g *gen) encodeLikely() (data string, scripts, regions, langs []string) {
	scriptSet, regionSet, langSet := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for from, to := range g.likely {
		for _, id := range []lsr{from, to} {
			if id.script != "" {
				scriptSet[id.script] = true
			}
			if id.region != "" {
				regionSet[id.region] = true
			}
		}
		if to.lang != from.lang {
			langSet[to.lang] = true
		}
	}
	scripts, regions, langs = sortedKeys(scriptSet), sortedKeys(regionSet), sortedKeys(langSet)
	if len(scripts) > 255 || len(regions) > 255 {
		log.Fatalf("%d scripts and %d regions do not fit a byte", len(scripts), len(regions))
	}
	index := func(list []string, s string) uint32 {
		if s == "" {
			return 0
		}
		i, _ := slices.BinarySearch(list, s)
		return uint32(i) + 1
	}
	type entry struct {
		key, script, region uint32
		lang, toScript      uint32
		toRegion, toLang    uint32
	}
	var entries []entry
	for from, to := range g.likely {
		e := entry{lang: packLang(from.lang), script: index(scripts, from.script), region: index(regions, from.region),
			toScript: index(scripts, to.script), toRegion: index(regions, to.region)}
		if to.lang != from.lang {
			e.toLang = index(langs, to.lang)
		}
		e.key = e.lang<<16 | e.script<<8 | e.region
		entries = append(entries, e)
	}
	slices.SortFunc(entries, func(a, b entry) int { return int(a.key) - int(b.key) })

	var b []byte
	prev := uint32(0)
	for _, e := range entries {
		h := byte(0)
		if e.script != 0 {
			h |= lhScript
		}
		if e.region != 0 {
			h |= lhRegion
		}
		if e.toLang != 0 {
			h |= lhLang
		}
		if e.toScript != e.script {
			h |= lhToScript
		}
		if e.toRegion != e.region {
			h |= lhToRegion
		}
		delta := e.lang - prev
		prev = e.lang
		if delta < 7 {
			b = append(b, h|byte(delta)<<lhDeltaShift)
		} else {
			b = append(b, h|7<<lhDeltaShift)
			b = binary.AppendUvarint(b, uint64(delta))
		}
		if e.script != 0 {
			b = append(b, byte(e.script))
		}
		if e.region != 0 {
			b = append(b, byte(e.region))
		}
		if e.toLang != 0 {
			b = binary.AppendUvarint(b, uint64(e.toLang))
		}
		if e.toScript != e.script {
			b = append(b, byte(e.toScript))
		}
		if e.toRegion != e.region {
			b = append(b, byte(e.toRegion))
		}
	}
	return string(b), scripts, regions, langs
}

// fixed joins list with every element padded with spaces to width.
func fixed(list []string, width int) string {
	var sb strings.Builder
	for _, s := range list {
		sb.WriteString(s + strings.Repeat(" ", width-len(s)))
	}
	return sb.String()
}

// joinRecords joins records with ';' after each. The runtime indexes each
// record by its offset<<8 | the length of its key in a uint32.
func joinRecords(list []record) string {
	var sb strings.Builder
	for _, r := range list {
		if len(r.key) > 0xFF {
			log.Fatalf("key %q is too long", r.key)
		}
		sb.WriteString(r.text)
		sb.WriteByte(';')
	}
	if sb.Len() > 1<<24 {
		log.Fatalf("a table of %d bytes does not fit 24-bit offsets", sb.Len())
	}
	return sb.String()
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
	p("// Code generated by internal/gen/cldr from CLDR %s; DO NOT EDIT.\n\n", version)
	p("%s", notice)
	p("package engine\n\n")
	p("// cldrVersion is the CLDR release of the locale tables.\n")
	p("const cldrVersion = %q\n\n", version)

	p("// The alias rules of UTS #35 Annex C (%d, from languageAlias, scriptAlias,\n", g.nRules)
	p("// territoryAlias and variantAlias; %d with a type that is no valid\n", g.discarded)
	p("// unicode_language_id discarded), each \"type>replacement\" and a ';'. A\n")
	p("// type or replacement is \"language,script,region,variants\" with empty\n")
	p("// fields for missing subtags (and \"und\"), trailing commas dropped and the\n")
	p("// variants sorted and joined by '-'; a territoryAlias with several\n")
	p("// replacement regions lists them all, space-separated, in its region field.\n")
	p("// The rules are grouped by the first non-empty field of their type and\n")
	p("// sorted by it.\n")
	for i, name := range []string{"Language", "Script", "Region", "Variant"} {
		p("const lt%sAliases = %s\n\n", name, quote(joinRecords(g.rules[i])))
	}

	p("// ltSubdivisionAliases maps subdivisionAlias types to their (first)\n")
	p("// replacement, lowercase, a region replacement followed by \"zzzz\":\n")
	p("// \"type>replacement;\" sorted by type.\n")
	p("const ltSubdivisionAliases = %s\n\n", quote(joinRecords(g.subdivs)))

	p("// ltKeywordAliases maps -u- and -t- keyword values to their canonical\n")
	p("// form (common/bcp47: a deprecated value to its preferred value, a value\n")
	p("// alias to its value): \"key:value>canonical;\" sorted by key and value.\n")
	p("const ltKeywordAliases = %s\n\n", quote(joinRecords(g.keywords)))

	data, scripts, regions, langs := g.encodeLikely()
	p("// ltScripts lists the scripts of the likely subtags, four letters each,\n")
	p("// sorted; ltRegions the regions, three bytes each (a two-letter region\n")
	p("// followed by a space), sorted.\n")
	p("const (\n")
	p("ltScripts = %s\n", quote(fixed(scripts, 4)))
	p("ltRegions = %s\n", quote(fixed(regions, 3)))
	p(")\n\n")
	p("// ltLikelyLanguages lists the languages the likely subtags replace a\n")
	p("// key's language with (for \"und\" and the deprecated codes CLDR keeps),\n")
	p("// three bytes each (a two-letter language followed by a space), sorted.\n")
	p("const ltLikelyLanguages = %s\n\n", quote(fixed(langs, 3)))
	p("// ltLikelySubtags holds the %d likelySubtag entries sorted by key\n", len(g.likely))
	p("// (language, script, region). Each is a header byte (bit 0: the key has a\n")
	p("// script, 1: a region, 2: the language changes, 3: the script differs\n")
	p("// from the key's, 4: the region does; bits 5-7: the difference of the\n")
	p("// key's packed language from the previous entry's, 7 when a uvarint\n")
	p("// follows), then the\n")
	p("// key's script and region (1 + index in ltScripts and ltRegions, one byte\n")
	p("// each, when present), and the replacement: the language (uvarint, 1 +\n")
	p("// index in ltLikelyLanguages) when it changes, the script and the region\n")
	p("// when they differ from the key's.\n")
	p("const ltLikelySubtags = %s\n\n", quote(data))
	p("// ltLikelyCount is the number of entries of ltLikelySubtags.\n")
	p("const ltLikelyCount = %d\n\n", len(g.likely))

	var avail []record
	maxSubtags := 1
	for _, lang := range sortedKeys(g.available) {
		rest := g.available[lang][1:] // [0] is "" (lang itself)
		for i := range rest {
			rest[i] = rest[i][1:]
			maxSubtags = max(maxSubtags, 2+strings.Count(rest[i], "-"))
		}
		avail = append(avail, record{lang, lang + ":" + strings.Join(rest, " ")})
	}
	p("// ltAvailableLocales lists the %d CLDR locales (%d of common/main, root\n", countAvailable(g.available), len(g.main))
	p("// excluded, those of the %d defaultContent locales whose parent is\n", g.defaults)
	p("// available, %d not, and the %d language-region forms of\n", g.droppedDefaults, g.scriptless)
	p("// language-script-region locales) as BCP 47 tags, grouped by\n")
	p("// language: \"language:rest rest ...;\" where each rest is a locale\n")
	p("// without its language and the first '-', sorted by language.\n")
	p("const ltAvailableLocales = %s\n\n", quote(joinRecords(avail)))
	p("// ltAvailableMaxSubtags is the number of subtags of the longest locale\n")
	p("// of ltAvailableLocales.\n")
	p("const ltAvailableMaxSubtags = %d\n", maxSubtags)
	return b.Bytes()
}

func countAvailable(m map[string][]string) int {
	n := 0
	for _, rest := range m {
		n += len(rest)
	}
	return n
}
