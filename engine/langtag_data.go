package engine

import (
	"slices"
	"strings"
	"sync"
)

// The CLDR tables of langtag.go, generated into intl_locale_tables.go. The
// alias and available-locale tables are ';'-terminated records, indexed on
// first use by their offsets; the likely subtags are decoded on first use
// into sorted packed entries. Each group is decoded separately, so
// canonicalizing a tag never decodes the likely subtags unless a region
// alias with several replacements needs them.

// ltAliasTables indexes the records of the alias tables (ltIndex). The
// language rules are searched by their packed language (the generator
// checks that every one has two or three letters); most regions and keyword
// keys have no record, and bit sets of those that do spare the searches.
type ltAliasTables struct {
	rules     [4][]uint32 // ltLanguageAliases, ltScriptAliases, ltRegionAliases, ltVariantAliases
	langCodes []uint16    // ltPackLang of the keys of rules[0]
	subdivs   []uint32
	keywords  []uint32

	regions [(26*26 + 1000 + 63) / 64]uint64 // by ltRegionBit
	keys    [(36*36 + 63) / 64]uint64        // by ltKeyBit
}

func ltBit(set []uint64, i uint32) bool { return set[i/64]&(1<<(i%64)) != 0 }

// ltRegionBit numbers the canonical regions: two letters, then three
// digits.
func ltRegionBit(r string) uint32 {
	if len(r) == 2 {
		return uint32(r[0]-'A')*26 + uint32(r[1]-'A')
	}
	return 26*26 + uint32(r[0]-'0')*100 + uint32(r[1]-'0')*10 + uint32(r[2]-'0')
}

// ltKeyBit numbers the two-character keyword keys (lowercase alphanumerics).
func ltKeyBit(k string) uint32 {
	n := func(c byte) uint32 {
		if c <= '9' {
			return uint32(c - '0')
		}
		return uint32(c-'a') + 10
	}
	return n(k[0])*36 + n(k[1])
}

// ltRuleTables are the alias rule tables in ltAliasTables.rules order.
var ltRuleTables = [4]string{ltLanguageAliases, ltScriptAliases, ltRegionAliases, ltVariantAliases}

var (
	ltAliasOnce sync.Once
	ltAliasData ltAliasTables

	ltLikelyOnce sync.Once
	ltLikelyData []uint64 // key<<32 | value, sorted

	ltAvailableOnce sync.Once
	ltAvailableData []uint32
)

func ltAliases() *ltAliasTables {
	ltAliasOnce.Do(func() {
		for i, table := range ltRuleTables {
			// A rule of group i starts with i empty fields; its key is the
			// first subtag after them.
			ltAliasData.rules[i] = ltIndex(table, i, ",>-")
		}
		ltAliasData.subdivs = ltIndex(ltSubdivisionAliases, 0, ">")
		ltAliasData.keywords = ltIndex(ltKeywordAliases, 0, ">")
		a := &ltAliasData
		a.langCodes = make([]uint16, len(a.rules[0]))
		for i, e := range a.rules[0] {
			l, _ := ltPackLang(ltKey(ltLanguageAliases, e, 0))
			a.langCodes[i] = uint16(l)
		}
		for _, e := range a.rules[2] {
			b := ltRegionBit(ltKey(ltRegionAliases, e, 2))
			a.regions[b/64] |= 1 << (b % 64)
		}
		for _, e := range a.keywords {
			b := ltKeyBit(ltKey(ltKeywordAliases, e, 0))
			a.keys[b/64] |= 1 << (b % 64)
		}
	})
	return &ltAliasData
}

// ltIndex returns an index of the ';'-terminated records of table: for each,
// its offset<<8 | the length of its key, which starts lead bytes into the
// record and ends at the first byte of stop.
func ltIndex(table string, lead int, stop string) []uint32 {
	index := make([]uint32, 0, strings.Count(table, ";"))
	for i := 0; i < len(table); {
		k := i + lead
		for strings.IndexByte(stop, table[k]) < 0 {
			k++
		}
		index = append(index, uint32(i)<<8|uint32(k-i-lead))
		i += strings.IndexByte(table[i:], ';') + 1
	}
	return index
}

// ltKey returns the key of the record of table at index entry e.
func ltKey(table string, e uint32, lead int) string {
	o := int(e>>8) + lead
	return table[o : o+int(e&0xFF)]
}

// ltRecord returns the record of table at index entry e, without its ';'.
func ltRecord(table string, e uint32) string {
	rec := table[e>>8:]
	return rec[:strings.IndexByte(rec, ';')]
}

// ltSearch returns the index entries of the records of table whose key is
// key.
func ltSearch(table string, index []uint32, lead int, key string) []uint32 {
	lo, found := slices.BinarySearchFunc(index, key, func(e uint32, key string) int {
		return strings.Compare(ltKey(table, e, lead), key)
	})
	if !found {
		return nil
	}
	hi := lo + 1
	for hi < len(index) && ltKey(table, index[hi], lead) == key {
		hi++
	}
	return index[lo:hi]
}

// ltRule is an alias rule: its type and replacement (empty fields for
// missing subtags and "und") and, for a region alias with several
// replacements, all of them (space-separated).
type ltRule struct {
	from, to ltID
	regions  string
}

func ltRuleAt(table string, e uint32) ltRule {
	from, to, _ := strings.Cut(ltRecord(table, e), ">")
	r := ltRule{from: ltPositional(from), to: ltPositional(to)}
	if first, _, ok := strings.Cut(r.to.region, " "); ok {
		r.regions, r.to.region = r.to.region, first
	}
	return r
}

// ltPositional parses "language,script,region,variants".
func ltPositional(s string) ltID {
	var id ltID
	id.lang, s, _ = strings.Cut(s, ",")
	id.script, s, _ = strings.Cut(s, ",")
	id.region, id.variants, _ = strings.Cut(s, ",")
	return id
}

// ltTypeMatches reports whether every field of the type of the rule record
// at the start of rec is in id (Annex C, Matches), without parsing the
// record.
func ltTypeMatches(rec string, id *ltID) bool {
	for i, field, start := 0, 0, 0; ; i++ {
		c := rec[i]
		if c != ',' && c != '>' {
			continue
		}
		if f := rec[start:i]; f != "" {
			switch field {
			case 0:
				if f != id.lang {
					return false
				}
			case 1:
				if f != id.script {
					return false
				}
			case 2:
				if f != id.region {
					return false
				}
			default:
				for k := 0; k < len(f); {
					v, j := ltSubtag(f, k)
					if !ltHasSubtag(id.variants, v) {
						return false
					}
					k = j
				}
			}
		}
		if c == '>' {
			return true
		}
		field, start = field+1, i+1
	}
}

// size is the number of subtags of a rule type.
func (id *ltID) size() int {
	n := 0
	for _, s := range [...]string{id.lang, id.script, id.region} {
		if s != "" {
			n++
		}
	}
	if id.variants != "" {
		n += 1 + strings.Count(id.variants, "-")
	}
	return n
}

// ltRuleBefore reports whether the rule type a comes before b in the order
// of Annex C, Preprocessing: more subtags first, then by which fields are
// non-empty (language, script, region, variants), then by their values.
func ltRuleBefore(a, b *ltID) bool {
	if na, nb := a.size(), b.size(); na != nb {
		return na > nb
	}
	fa := [...]string{a.lang, a.script, a.region, a.variants}
	fb := [...]string{b.lang, b.script, b.region, b.variants}
	for i := range fa {
		if (fa[i] == "") != (fb[i] == "") {
			return fa[i] != ""
		}
	}
	for i := range fa {
		if fa[i] != fb[i] {
			// '-' sorts before the subtags' characters, so the joined
			// variants compare like their lists.
			return fa[i] < fb[i]
		}
	}
	return false
}

// ltFindAlias returns the first alias rule (Annex C order) that matches id.
// Every rule is filed under the first non-empty field of its type, so the
// candidates are those filed under id's language, script, region and
// variants.
func ltFindAlias(id *ltID) (best ltRule, found bool) {
	a := ltAliases()
	group := 0
	consider := func(e uint32) {
		table := ltRuleTables[group]
		if !ltTypeMatches(table[e>>8:], id) {
			return
		}
		if r := ltRuleAt(table, e); !found || ltRuleBefore(&r.from, &best.from) {
			best, found = r, true
		}
	}
	if l, ok := ltPackLang(id.lang); ok {
		i, _ := slices.BinarySearch(a.langCodes, uint16(l))
		for ; i < len(a.langCodes) && a.langCodes[i] == uint16(l); i++ {
			consider(a.rules[0][i])
		}
	}
	if group = 1; id.script != "" {
		for _, e := range ltSearch(ltScriptAliases, a.rules[1], 1, id.script) {
			consider(e)
		}
	}
	if group = 2; id.region != "" && ltBit(a.regions[:], ltRegionBit(id.region)) {
		for _, e := range ltSearch(ltRegionAliases, a.rules[2], 2, id.region) {
			consider(e)
		}
	}
	group = 3
	for i := 0; i < len(id.variants); {
		v, j := ltSubtag(id.variants, i)
		for _, e := range ltSearch(ltVariantAliases, a.rules[3], 3, v) {
			consider(e)
		}
		i = j
	}
	return best, found
}

// ltSubdivisionAlias returns the replacement of the subdivision alias s.
func ltSubdivisionAlias(s string) (string, bool) {
	es := ltSearch(ltSubdivisionAliases, ltAliases().subdivs, 0, s)
	if es == nil {
		return "", false
	}
	_, to, _ := strings.Cut(ltRecord(ltSubdivisionAliases, es[0]), ">")
	return to, true
}

// ltKeywordAlias returns the canonical form of value for the -u- or -t-
// key (both lowercase) when CLDR has an alias or a preferred value for it.
func ltKeywordAlias(key, value string) (string, bool) {
	if len(key) != 2 || !ltIsAlnum(key[0]) || !ltIsAlnum(key[1]) || ltHasUpper(key) {
		return "", false
	}
	a := ltAliases()
	if !ltBit(a.keys[:], ltKeyBit(key)) {
		return "", false
	}
	index := a.keywords
	// The keys are "key:value" with two-letter keys.
	i, found := slices.BinarySearchFunc(index, value, func(e uint32, value string) int {
		k := ltKey(ltKeywordAliases, e, 0)
		if c := strings.Compare(k[:2], key); c != 0 {
			return c
		}
		return strings.Compare(k[3:], value)
	})
	if !found {
		return "", false
	}
	_, to, _ := strings.Cut(ltRecord(ltKeywordAliases, index[i]), ">")
	return to, true
}

// ltLikely returns the decoded likely subtags: key<<32 | value sorted by
// key, where a key is the packed language<<16 | script<<8 | region (1 +
// index in ltScripts and ltRegions, 0 for none) and a value is the
// replacement language (1 + index in ltLikelyLanguages, 0 for the key's)
// <<16 | script<<8 | region.
func ltLikely() []uint64 {
	ltLikelyOnce.Do(func() { ltLikelyData = ltDecodeLikely() })
	return ltLikelyData
}

func ltDecodeLikely() []uint64 {
	s := ltLikelySubtags
	out := make([]uint64, 0, ltLikelyCount)
	lang := uint64(0)
	for i := 0; i < len(s); {
		h := s[i]
		i++
		d := uint64(h >> 5)
		if d == 7 {
			d, i = uvarintAt(s, i)
		}
		lang += d
		var script, region, toLang uint64
		if h&1 != 0 {
			script = uint64(s[i])
			i++
		}
		if h&2 != 0 {
			region = uint64(s[i])
			i++
		}
		if h&4 != 0 {
			toLang, i = uvarintAt(s, i)
		}
		toScript, toRegion := script, region
		if h&8 != 0 {
			toScript = uint64(s[i])
			i++
		}
		if h&16 != 0 {
			toRegion = uint64(s[i])
			i++
		}
		out = append(out, (lang<<16|script<<8|region)<<32|toLang<<16|toScript<<8|toRegion)
	}
	return out
}

// ltPackLang packs a language subtag of two or three lowercase letters like
// the generator: base 27, 0 for a missing third letter.
func ltPackLang(s string) (uint32, bool) {
	if len(s) < 2 || len(s) > 3 {
		return 0, false
	}
	var c uint32
	for i := range 3 {
		c *= 27
		if i < len(s) {
			c += uint32(s[i]-'a') + 1
		}
	}
	return c, true
}

// ltLikelyLookup looks up language_script_region, language_script,
// language_region and language in the likely subtags, in that order (UTS
// #35 Likely Subtags, Lookup), skipping those with an empty field, and
// returns the value of the first entry found.
func ltLikelyLookup(lang, script, region string) (uint32, bool) {
	l, ok := ltPackLang(lang)
	if !ok {
		return 0, false
	}
	// A script or region missing from the tables is in no key.
	var si, ri uint32
	if script != "" {
		si = uint32(ltFindFixed(ltScripts, 4, script) + 1)
	}
	if region != "" {
		ri = uint32(ltFindFixed(ltRegions, 3, region) + 1)
	}
	var keys [4]uint32
	n := 0
	if si != 0 && ri != 0 {
		keys[n] = l<<16 | si<<8 | ri
		n++
	}
	if si != 0 {
		keys[n] = l<<16 | si<<8
		n++
	}
	if ri != 0 {
		keys[n] = l<<16 | ri
		n++
	}
	keys[n] = l << 16
	entries := ltLikely()
	for _, key := range keys[:n+1] {
		i, found := slices.BinarySearchFunc(entries, key, func(e uint64, key uint32) int {
			return int(e>>32) - int(key)
		})
		if found {
			return uint32(entries[i]), true
		}
	}
	return 0, false
}

// ltScriptAt returns script i (1-based) of ltScripts.
func ltScriptAt(i uint32) string { return ltScripts[4*(i-1) : 4*i] }

// ltRegionAt returns region i (1-based) of ltRegions.
func ltRegionAt(i uint32) string {
	r := ltRegions[3*(i-1) : 3*i]
	if r[2] == ' ' {
		r = r[:2]
	}
	return r
}

// ltLikelyLanguage returns language i (1-based) of ltLikelyLanguages.
func ltLikelyLanguage(i uint32) string {
	l := ltLikelyLanguages[3*(i-1) : 3*i]
	if l[2] == ' ' {
		l = l[:2]
	}
	return l
}

// ltFindFixed returns the index of key in table, a sorted list of entries
// of width bytes (padded with spaces), ignoring ASCII case; -1 when absent.
func ltFindFixed(table string, width int, key string) int {
	if len(key) > width {
		return -1
	}
	n := len(table) / width
	lo, hi := 0, n
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		if ltCompareFold(table[m*width:m*width+width], key) < 0 {
			lo = m + 1
		} else {
			hi = m
		}
	}
	if lo < n && ltCompareFold(table[lo*width:lo*width+width], key) == 0 {
		return lo
	}
	return -1
}

// ltCompareFold compares the space-padded entry e with key, ignoring ASCII
// case.
func ltCompareFold(e, key string) int {
	for i := range len(e) {
		var c byte = ' '
		if i < len(key) {
			c = key[i] | 0x20
		}
		if d := int(e[i]|0x20) - int(c); d != 0 {
			return d
		}
	}
	return 0
}

// langTagAvailable reports whether the locale (a canonical tag without
// extensions, e.g. "en-US", "zh-Hant-TW", "sr-Latn") is one of CLDR's
// locales: the locales of common/main plus the defaultContent locales of
// supplementalMetadata.xml, written as BCP 47 tags ("en_US_POSIX" style ids
// converted; "root" excluded).
func langTagAvailable(tag string) bool {
	ltAvailableOnce.Do(func() { ltAvailableData = ltIndex(ltAvailableLocales, 0, ":") })
	lang, rest, _ := strings.Cut(tag, "-")
	es := ltSearch(ltAvailableLocales, ltAvailableData, 0, lang)
	if es == nil {
		return false
	}
	if rest == "" {
		return true
	}
	_, list, _ := strings.Cut(ltRecord(ltAvailableLocales, es[0]), ":")
	// rest as a whole word of the space-separated list.
	for i := 0; ; {
		j := strings.Index(list[i:], rest)
		if j < 0 {
			return false
		}
		j += i
		if end := j + len(rest); (j == 0 || list[j-1] == ' ') && (end == len(list) || list[end] == ' ') {
			return true
		}
		i = j + 1
	}
}
