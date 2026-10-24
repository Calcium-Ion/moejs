package engine

import (
	"cmp"
	"sort"
	"strings"
)

//go:generate go run ../internal/gen/cldr -o intl_locale_tables.go

// Unicode BCP 47 locale identifiers (UTS #35 Part 1 §3, unicode_bcp47_locale_id)
// as ECMA-402 §6.2 restricts them: parsing (IsStructurallyValidLanguageTag),
// canonicalization (CanonicalizeUnicodeLocaleId: UTS #35 Annex C over the CLDR
// alias data of intl_locale_tables.go) and the likely subtags of
// Intl.Locale.prototype.maximize and minimize. The CLDR tables are decoded
// on first use and shared by the process (langtag_data.go).
//
// A langTag keeps its subtags as substrings of the parsed string where their
// case is already canonical, so parsing and canonicalizing a canonical tag
// such as "en-US" allocates nothing beyond the tag, and String returns the
// parsed string itself while it is still the tag's form.

// langTag is a Unicode BCP 47 locale identifier (UTS #35 unicode_locale_id
// as ECMA-402 restricts it). Its subtags are in canonical case; canonicalize
// applies the rest of Annex C.
type langTag struct {
	src string // the parsed string
	id  ltID
	// The extensions, lowercase, each without its singleton and the '-'
	// after it: t (tlang and tfields), u (attributes and keywords) and x
	// (private use). other holds the other extension sequences, singletons
	// included ("a-foo-b-bar"), sorted by singleton once canonicalized.
	t, u, x, other string
}

// ltID is a unicode_language_id: a language (lowercase, "und" when
// unknown), a script (title case) and a region (uppercase), "" when absent,
// and the variants (lowercase, '-'-joined, sorted once canonicalized).
type ltID struct {
	lang, script, region, variants string
}

// ltLower returns s with ASCII letters lowercased, s itself when it has
// none in uppercase.
func ltLower(s string) string {
	for i := range len(s) {
		if c := s[i]; 'A' <= c && c <= 'Z' {
			b := []byte(s)
			for j := i; j < len(b); j++ {
				if c := b[j]; 'A' <= c && c <= 'Z' {
					b[j] = c + 'a' - 'A'
				}
			}
			return string(b)
		}
	}
	return s
}

func ltIsAlpha(c byte) bool { c |= 0x20; return 'a' <= c && c <= 'z' }
func ltIsDigit(c byte) bool { return '0' <= c && c <= '9' }
func ltIsAlnum(c byte) bool { return ltIsAlpha(c) || ltIsDigit(c) }

func ltAllAlpha(s string) bool {
	for i := range len(s) {
		if !ltIsAlpha(s[i]) {
			return false
		}
	}
	return true
}

func ltAllAlnum(s string) bool {
	for i := range len(s) {
		if !ltIsAlnum(s[i]) {
			return false
		}
	}
	return true
}

func isLangTagLanguage(s string) bool {
	return (len(s) >= 2 && len(s) <= 3 || len(s) >= 5 && len(s) <= 8) && ltAllAlpha(s)
}

func isLangTagScript(s string) bool { return len(s) == 4 && ltAllAlpha(s) }

func isLangTagRegion(s string) bool {
	return len(s) == 2 && ltAllAlpha(s) || len(s) == 3 && ltIsDigit(s[0]) && ltIsDigit(s[1]) && ltIsDigit(s[2])
}

func isLangTagVariant(s string) bool {
	return (len(s) >= 5 && len(s) <= 8 || len(s) == 4 && ltIsDigit(s[0])) && ltAllAlnum(s)
}

// isLangTagVariants reports whether s is variant ("-" variant)* without
// duplicates (case-insensitively).
func isLangTagVariants(s string) bool {
	if !ltWellSeparated(s) {
		return false
	}
	for i := 0; i < len(s); {
		v, j := ltSubtag(s, i)
		if !isLangTagVariant(v) {
			return false
		}
		i = j
	}
	return !ltHasDuplicate(ltLower(s))
}

// isLangTagType reports whether s matches the type nonterminal: 3-8
// alphanumerics, '-'-separated.
func isLangTagType(s string) bool {
	if !ltWellSeparated(s) {
		return false
	}
	for i := 0; i < len(s); {
		v, j := ltSubtag(s, i)
		if len(v) < 3 || len(v) > 8 || !ltAllAlnum(v) {
			return false
		}
		i = j
	}
	return true
}

// ltWellSeparated reports whether s is a non-empty '-'-separated list
// without empty elements.
func ltWellSeparated(s string) bool {
	return s != "" && s[0] != '-' && s[len(s)-1] != '-' && !strings.Contains(s, "--")
}

// ltSubtag returns the subtag of the well-separated s starting at i and the
// index of the next one (len(s) after the last).
func ltSubtag(s string, i int) (string, int) {
	if j := strings.IndexByte(s[i:], '-'); j >= 0 {
		return s[i : i+j], i + j + 1
	}
	return s[i:], len(s)
}

// ltSpan returns the subtags of s from index start to the subtag at end
// (len(s) for all the rest).
func ltSpan(s string, start, end int) string {
	if end <= start {
		return ""
	}
	if end < len(s) {
		end--
	}
	return s[start:end]
}

// ltHasDuplicate reports whether the '-'-joined list has a subtag twice.
// Tags may be long and come from scripts, so a long list is sorted rather
// than compared pairwise.
func ltHasDuplicate(list string) bool {
	if n := strings.Count(list, "-"); n >= 8 {
		vs := strings.Split(list, "-")
		sort.Sort(sort.StringSlice(vs))
		for i := 1; i < len(vs); i++ {
			if vs[i] == vs[i-1] {
				return true
			}
		}
		return false
	}
	for i := 0; i < len(list); {
		v, j := ltSubtag(list, i)
		if ltHasSubtag(list[j:], v) {
			return true
		}
		i = j
	}
	return false
}

// parseLangTag parses s (case-insensitive) and reports whether it is a
// structurally valid language tag (ECMA-402 IsStructurallyValidLanguageTag:
// unicode_bcp47_locale_id, no duplicate variants, no duplicate singletons,
// a valid tlang in -t- without duplicate variants, ...).
func parseLangTag(s string) (*langTag, bool) {
	t := new(langTag)
	if !t.parse(s) {
		return nil, false
	}
	return t, true
}

// canonicalizeLangTag is parseLangTag + canonicalize + String.
func canonicalizeLangTag(s string) (string, bool) {
	var t langTag
	if !t.parse(s) {
		return "", false
	}
	t.canonicalize()
	return t.String(), true
}

func (t *langTag) parse(s string) bool {
	*t = langTag{src: s}
	if !ltWellSeparated(s) {
		return false
	}
	for i := range len(s) {
		if c := s[i]; !ltIsAlnum(c) && c != '-' {
			return false
		}
	}
	// The subtags are classified ignoring case; the language, variants and
	// extensions are taken from a lowercase copy only when s has an
	// uppercase letter outside the script and region.
	lang, i := ltSubtag(s, 0)
	if !isLangTagLanguage(lang) {
		return false
	}
	sub, j := ltSubtag(s, i)
	script, region := "", ""
	if i < len(s) && isLangTagScript(sub) {
		script, i = sub, j
		sub, j = ltSubtag(s, i)
	}
	if i < len(s) && isLangTagRegion(sub) {
		region, i = sub, j
		sub, j = ltSubtag(s, i)
	}
	vstart := i
	for i < len(s) && isLangTagVariant(sub) {
		i = j
		sub, j = ltSubtag(s, i)
	}
	low := s
	if ltHasUpper(s[:len(lang)]) || ltHasUpper(s[vstart:]) {
		low = ltLower(s)
	}
	t.id.lang = low[:len(lang)]
	if script != "" {
		t.id.script = ltTitleScript(script)
	}
	if region != "" {
		t.id.region = ltUpperRegion(region)
	}
	if t.id.variants = ltSpan(low, vstart, i); ltHasDuplicate(t.id.variants) {
		return false
	}
	var seen uint64 // the singletons so far
	for i < len(low) {
		if len(sub) != 1 || j == len(low) {
			return false
		}
		c := low[i]
		if c == 'x' {
			// Private use: alphanum{1,8} subtags up to the end.
			for k := j; k < len(low); {
				sub, k = ltSubtag(low, k)
				if len(sub) > 8 {
					return false
				}
			}
			t.x = low[j:]
			return true
		}
		bit := uint64(1) << (c - '0')
		if ltIsAlpha(c) {
			bit = 1 << (c - 'a' + 10)
		}
		if seen&bit != 0 {
			return false
		}
		seen |= bit
		start, ok := j, false
		switch c {
		case 'u':
			i, ok = ltParseU(low, start)
		case 't':
			i, ok = ltParseT(low, start)
		default:
			i, ok = ltParseOther(low, start)
		}
		if !ok {
			return false
		}
		switch ext := ltSpan(low, start, i); c {
		case 'u':
			t.u = ext
		case 't':
			t.t = ext
		default:
			if seq := low[start-2 : start+len(ext)]; t.other == "" {
				t.other = seq
			} else {
				t.other += "-" + seq
			}
		}
		sub, j = ltSubtag(low, i)
	}
	return true
}

// ltHasUpper reports whether s has an ASCII uppercase letter.
func ltHasUpper(s string) bool {
	for i := range len(s) {
		if c := s[i]; 'A' <= c && c <= 'Z' {
			return true
		}
	}
	return false
}

// ltParseU checks the unicode_locale_extensions subtags of s from index i
// (attributes, then keywords: a key, alphanum alpha, and its type subtags)
// and returns the index of the next singleton (len(s) at the end).
func ltParseU(s string, i int) (int, bool) {
	start := i
	for i < len(s) {
		sub, j := ltSubtag(s, i)
		switch {
		case len(sub) == 1:
			return i, i > start
		case len(sub) == 2:
			if !ltIsAlpha(sub[1]) {
				return 0, false
			}
		case len(sub) > 8:
			return 0, false
		}
		i = j
	}
	return i, i > start
}

// ltParseT checks the transformed_extensions subtags of s from index i (an
// optional tlang, then tfields: a tkey, alpha digit, and its tvalue
// subtags) and returns the index of the next singleton (len(s) at the end).
func ltParseT(s string, i int) (int, bool) {
	sub, j := ltSubtag(s, i)
	if isLangTagLanguage(sub) {
		i = j
		sub, j = ltSubtag(s, i)
		if i < len(s) && isLangTagScript(sub) {
			i = j
			sub, j = ltSubtag(s, i)
		}
		if i < len(s) && isLangTagRegion(sub) {
			i = j
			sub, j = ltSubtag(s, i)
		}
		vstart := i
		for i < len(s) && isLangTagVariant(sub) {
			i = j
			sub, j = ltSubtag(s, i)
		}
		if ltHasDuplicate(ltSpan(s, vstart, i)) {
			return 0, false
		}
	} else if len(sub) == 1 {
		return 0, false
	}
	for i < len(s) && len(sub) != 1 {
		if len(sub) != 2 || !ltIsAlpha(sub[0]) || !ltIsDigit(sub[1]) {
			return 0, false
		}
		n := 0
		for i = j; i < len(s); i = j {
			if sub, j = ltSubtag(s, i); len(sub) < 3 || len(sub) > 8 {
				break
			}
			n++
		}
		if n == 0 {
			return 0, false
		}
	}
	return i, true
}

// ltParseOther checks the subtags of an other_extensions sequence of s from
// index i (alphanum{2,8}) and returns the index of the next singleton.
func ltParseOther(s string, i int) (int, bool) {
	start := i
	for i < len(s) {
		sub, j := ltSubtag(s, i)
		if len(sub) == 1 {
			break
		}
		if len(sub) > 8 {
			return 0, false
		}
		i = j
	}
	return i, i > start
}

// ltTitleScript returns the script subtag s in title case: s itself when it
// is, else the entry of ltScripts, else a new string.
func ltTitleScript(s string) string {
	if 'A' <= s[0] && s[0] <= 'Z' && !ltHasUpper(s[1:]) {
		return s
	}
	if i := ltFindFixed(ltScripts, 4, s); i >= 0 {
		return ltScripts[4*i : 4*i+4]
	}
	b := []byte(ltLower(s))
	b[0] -= 'a' - 'A'
	return string(b)
}

// ltUpperRegion returns the region subtag s in uppercase: s itself when it
// is, else the entry of ltRegions, else a new string.
func ltUpperRegion(s string) string {
	if !ltIsAlpha(s[0]) || s[0] <= 'Z' && s[1] <= 'Z' {
		return s
	}
	if i := ltFindFixed(ltRegions, 3, s); i >= 0 {
		return ltRegionAt(uint32(i) + 1)
	}
	return string([]byte{s[0] &^ 0x20, s[1] &^ 0x20})
}

// canonicalize applies ECMA-402 CanonicalizeUnicodeLocaleId: UTS #35
// Annex C (case, alias replacement incl. complex language/region/variant
// replacement, sorted variants, -u- keywords sorted and their values
// canonicalized and aliased, "true" values dropped, -t- canonicalized and
// sorted, extension order).
func (t *langTag) canonicalize() {
	t.id.canonicalize()
	if t.other != "" {
		t.other = ltSortExtensions(t.other)
	}
	if t.t != "" {
		t.t = ltCanonicalT(t.t)
	}
	if t.u != "" {
		t.u = ltCanonicalU(t.u)
	}
}

// canonicalize sorts the variants of id and applies the alias rules until
// none matches (Annex C, Processing LanguageIds).
func (id *ltID) canonicalize() {
	id.variants = ltSortSubtags(id.variants)
	// The CLDR rules terminate; the bound only guards against a cycle.
	for range 32 {
		r, ok := ltFindAlias(id)
		if !ok {
			return
		}
		id.apply(&r)
	}
}

// apply replaces the fields of id that r's type has by those of its
// replacement and fills empty fields from the replacement (Annex C,
// Replacement).
func (id *ltID) apply(r *ltRule) {
	region := r.to.region
	if r.regions != "" {
		region = ltTerritoryException(id, r.regions)
	}
	if r.from.lang != "" {
		id.lang = cmp.Or(r.to.lang, "und")
	} else if id.lang == "und" && r.to.lang != "" {
		id.lang = r.to.lang
	}
	if r.from.script != "" || id.script == "" {
		id.script = r.to.script
	}
	if r.from.region != "" || id.region == "" {
		id.region = region
	}
	if r.from.variants != "" {
		id.variants = ltReplaceVariants(id.variants, r.from.variants, r.to.variants)
	} else if id.variants == "" {
		id.variants = r.to.variants
	}
}

// ltTerritoryException picks the replacement of a region alias with several
// replacements (space-separated in regions): the likely region of id's
// language and script when it is one of them, else the first (Annex C,
// Territory Exception).
func ltTerritoryException(id *ltID, regions string) string {
	first, _, _ := strings.Cut(regions, " ")
	probe := ltID{lang: id.lang, script: id.script}
	if !probe.addLikely() {
		return first
	}
	for r := range strings.FieldsSeq(regions) {
		if r == probe.region {
			return r
		}
	}
	return first
}

// ltReplaceVariants returns the sorted union of add and the variants of
// list not in remove ('-'-joined lists).
func ltReplaceVariants(list, remove, add string) string {
	var vs []string
	for i := 0; i < len(list); {
		v, j := ltSubtag(list, i)
		if !ltHasSubtag(remove, v) {
			vs = append(vs, v)
		}
		i = j
	}
	for i := 0; i < len(add); {
		v, j := ltSubtag(add, i)
		if !ltHasSubtag(list, v) || ltHasSubtag(remove, v) {
			vs = append(vs, v)
		}
		i = j
	}
	sort.Sort(sort.StringSlice(vs))
	return strings.Join(vs, "-")
}

// ltHasSubtag reports whether the '-'-joined list has sub.
func ltHasSubtag(list, sub string) bool {
	for i := 0; i < len(list); {
		v, j := ltSubtag(list, i)
		if v == sub {
			return true
		}
		i = j
	}
	return false
}

// ltSortSubtags returns the '-'-joined list sorted, list itself when it is.
func ltSortSubtags(list string) string {
	prev := ""
	for i := 0; i < len(list); {
		v, j := ltSubtag(list, i)
		if v < prev {
			vs := strings.Split(list, "-")
			sort.Sort(sort.StringSlice(vs))
			return strings.Join(vs, "-")
		}
		prev, i = v, j
	}
	return list
}

// ltSortExtensions returns the other extension sequences of other sorted by
// singleton, other itself when they are.
func ltSortExtensions(other string) string {
	var seqs []string
	sorted := true
	for i := 0; i < len(other); {
		end := ltNextSingleton(other, i+2)
		seq := ltSpan(other, i, end)
		if len(seqs) > 0 && seq[0] < seqs[len(seqs)-1][0] {
			sorted = false
		}
		seqs = append(seqs, seq)
		i = end
	}
	if sorted {
		return other
	}
	// The singletons differ, so the sequences sort like them.
	sort.Sort(sort.StringSlice(seqs))
	return strings.Join(seqs, "-")
}

// ltNextSingleton returns the index of the first singleton subtag of s at
// or after i, len(s) when there is none.
func ltNextSingleton(s string, i int) int {
	for i < len(s) {
		sub, j := ltSubtag(s, i)
		if len(sub) == 1 {
			return i
		}
		i = j
	}
	return len(s)
}

// ltKeyword is a -u- keyword or a -t- tfield; value is "" for a key alone.
type ltKeyword struct {
	key, value string
	pos        int // its position in the extension
}

// ltByKey sorts keywords by key, those of a key by position.
type ltByKey []ltKeyword

func (k ltByKey) Len() int      { return len(k) }
func (k ltByKey) Swap(i, j int) { k[i], k[j] = k[j], k[i] }
func (k ltByKey) Less(i, j int) bool {
	if k[i].key != k[j].key {
		return k[i].key < k[j].key
	}
	return k[i].pos < k[j].pos
}

// ltCanonicalU returns the -u- extension u (lowercase) in canonical form:
// the attributes sorted without duplicates, then the keywords sorted by key,
// only the first of each key, their values canonicalized and "true" values
// dropped. It returns u itself when it is canonical.
func ltCanonicalU(u string) string {
	var abuf [4]string
	var kbuf [8]ltKeyword
	attrs, kws := abuf[:0], kbuf[:0]
	changed := false
	i := 0
	for i < len(u) {
		sub, j := ltSubtag(u, i)
		if len(sub) == 2 {
			break
		}
		if len(attrs) > 0 && sub <= attrs[len(attrs)-1] {
			changed = true
		}
		attrs = append(attrs, sub)
		i = j
	}
	for i < len(u) {
		key, j := ltSubtag(u, i)
		start := j
		for i = j; i < len(u); i = j {
			var sub string
			if sub, j = ltSubtag(u, i); len(sub) == 2 {
				break
			}
		}
		kw := ltKeyword{key, ltSpan(u, start, i), len(kws)}
		if v := ltCanonicalKeywordValue(key, kw.value); v != kw.value || v == "true" {
			kw.value, changed = v, true
		}
		if len(kws) > 0 && key <= kws[len(kws)-1].key {
			changed = true
		}
		kws = append(kws, kw)
	}
	if !changed {
		return u
	}
	// Sorting copies, so that the arrays of the canonical case stay on the
	// stack.
	sortedAttrs := append([]string(nil), attrs...)
	sortedKws := append([]ltKeyword(nil), kws...)
	sort.Sort(sort.StringSlice(sortedAttrs))
	sort.Sort(ltByKey(sortedKws))
	b := make([]byte, 0, len(u))
	for i, a := range sortedAttrs {
		if i == 0 || a != sortedAttrs[i-1] {
			b = append(append(b, a...), '-')
		}
	}
	for i, kw := range sortedKws {
		if i > 0 && kw.key == sortedKws[i-1].key {
			continue // only the first of a key counts
		}
		b = append(b, kw.key...)
		if kw.value != "" && kw.value != "true" {
			b = append(append(b, '-'), kw.value...)
		}
		b = append(b, '-')
	}
	return string(b[:len(b)-1])
}

// ltCanonicalT returns the -t- extension t (lowercase) in canonical form:
// the tlang canonicalized like a language id but lowercase, then the tfields
// sorted by tkey with their tvalues canonicalized.
func ltCanonicalT(t string) string {
	var b []byte
	i := 0
	if sub, j := ltSubtag(t, 0); isLangTagLanguage(sub) {
		id := ltID{lang: sub}
		i = j
		if sub, j = ltSubtag(t, i); i < len(t) && isLangTagScript(sub) {
			id.script = ltTitleScript(sub)
			i = j
		}
		if sub, j = ltSubtag(t, i); i < len(t) && isLangTagRegion(sub) {
			id.region = ltUpperRegion(sub)
			i = j
		}
		vstart := i
		for sub, j = ltSubtag(t, i); i < len(t) && isLangTagVariant(sub); sub, j = ltSubtag(t, i) {
			i = j
		}
		if i > vstart {
			id.variants = ltSpan(t, vstart, i)
		}
		id.canonicalize()
		b = append(b, ltLower(id.String())...)
	}
	var fields []ltKeyword
	for i < len(t) {
		key, j := ltSubtag(t, i)
		start := j
		for i = j; i < len(t); i = j {
			var sub string
			if sub, j = ltSubtag(t, i); len(sub) == 2 {
				break
			}
		}
		value := ltSpan(t, start, i)
		fields = append(fields, ltKeyword{key, ltCanonicalKeywordValue(key, value), len(fields)})
	}
	sort.Sort(ltByKey(fields))
	for _, f := range fields {
		if len(b) > 0 {
			b = append(b, '-')
		}
		b = append(append(append(b, f.key...), '-'), f.value...)
	}
	if string(b) == t {
		return t
	}
	return string(b)
}

// ltCanonicalKeywordValue returns the value (lowercase) of the -u- or -t-
// keyword key in canonical form: its CLDR alias replaced, and for the
// subdivision keys rg and sd the subdivision alias.
func ltCanonicalKeywordValue(key, value string) string {
	if key == "rg" || key == "sd" {
		if v, ok := ltSubdivisionAlias(value); ok {
			return v
		}
	}
	if v, ok := ltKeywordAlias(key, value); ok {
		return v
	}
	return value
}

// langTagCanonicalUValue implements CanonicalizeUValue (ECMA-402 §6.2.1):
// the value of -u- keyword key canonicalized the way canonicalize would
// inside a tag (lowercase, CLDR aliases, "true" removed), for Intl.Locale's
// calendar/collation/numberingSystem/... options and ResolveLocale.
func langTagCanonicalUValue(key, value string) string {
	if v := ltCanonicalKeywordValue(key, ltLower(value)); v != "true" {
		return v
	}
	return ""
}

func (t *langTag) String() string {
	var buf [64]byte
	b := t.appendTo(buf[:0], true)
	if string(b) == t.src {
		return t.src
	}
	return string(b)
}

// appendTo appends the tag to b, its extensions in singleton order, the -u-
// extension only when withU is set.
func (t *langTag) appendTo(b []byte, withU bool) []byte {
	b = t.id.appendTo(b)
	other := t.other
	for other != "" && other[0] < 't' {
		end := ltNextSingleton(other, 2)
		b = append(append(b, '-'), ltSpan(other, 0, end)...)
		other = other[end:]
	}
	if t.t != "" {
		b = append(append(b, "-t-"...), t.t...)
	}
	if t.u != "" && withU {
		b = append(append(b, "-u-"...), t.u...)
	}
	if other != "" {
		b = append(append(b, '-'), other...)
	}
	if t.x != "" {
		b = append(append(b, "-x-"...), t.x...)
	}
	return b
}

func (id *ltID) appendTo(b []byte) []byte {
	b = append(b, id.lang...)
	for _, s := range [...]string{id.script, id.region, id.variants} {
		if s != "" {
			b = append(append(b, '-'), s...)
		}
	}
	return b
}

func (id ltID) String() string { return string(id.appendTo(nil)) }

func (t *langTag) clone() *langTag {
	c := *t
	return &c
}

// baseName returns language[-script][-region][-variants].
func (t *langTag) baseName() string { return t.id.String() }

// size returns the bytes of the tag's subtags and extensions, which parsing
// and canonicalization may have copied: what a tag that is kept costs
// besides its struct.
func (t *langTag) size() int {
	return len(t.id.lang) + len(t.id.script) + len(t.id.region) + len(t.id.variants) +
		len(t.t) + len(t.u) + len(t.x) + len(t.other)
}

func (t *langTag) language() string { return t.id.lang }

// script returns the script subtag, "" when absent.
func (t *langTag) script() string { return t.id.script }

// region returns the region subtag, "" when absent.
func (t *langTag) region() string { return t.id.region }

// variants returns the variants joined with "-", "" when none.
func (t *langTag) variants() string { return t.id.variants }

// uKeyword returns the value of the -u- keyword key ("" for a key without
// a type) and whether the tag has it.
func (t *langTag) uKeyword(key string) (value string, ok bool) {
	_, start, end, ok := ltFindKeyword(t.u, key)
	if !ok {
		return "", false
	}
	return t.u[start:end], true
}

// ltFindKeyword returns the index of the first keyword key in the -u-
// extension u, and the bounds of its value (start == end for none) and of
// the keyword.
func ltFindKeyword(u, key string) (at, start, end int, ok bool) {
	for i := 0; i < len(u); {
		sub, j := ltSubtag(u, i)
		if len(sub) != 2 {
			i = j // an attribute
			continue
		}
		end = i + 2
		for j < len(u) {
			v, next := ltSubtag(u, j)
			if len(v) == 2 {
				break
			}
			end, j = j+len(v), next
		}
		if sub == key {
			return i, min(i+3, end), end, true
		}
		i = j
	}
	return 0, 0, 0, false
}

// setUKeyword adds or replaces the -u- keyword key ("" value = key only).
func (t *langTag) setUKeyword(key, value string) {
	kw := key
	if value != "" {
		kw += "-" + value
	}
	at, _, end, ok := ltFindKeyword(t.u, key)
	switch {
	case ok:
		t.u = t.u[:at] + kw + t.u[end:]
	case t.u == "":
		t.u = kw
	default:
		t.u += "-" + kw
	}
}

// noUnicodeExtension returns the tag without its -u- extension sequence
// (ECMA-402 locale negotiation compares these), as a string.
func (t *langTag) noUnicodeExtension() string {
	if t.u == "" {
		return t.String()
	}
	var buf [64]byte
	return string(t.appendTo(buf[:0], false))
}

// setLanguage sets the language subtag; callers validate s with
// isLangTagLanguage.
func (t *langTag) setLanguage(s string) { t.id.lang = ltLower(s) }

// setScript sets the script subtag ("" removes it); callers validate s with
// isLangTagScript.
func (t *langTag) setScript(s string) {
	if s != "" {
		s = ltTitleScript(s)
	}
	t.id.script = s
}

// setRegion sets the region subtag ("" removes it); callers validate s with
// isLangTagRegion.
func (t *langTag) setRegion(s string) {
	if s != "" {
		s = ltUpperRegion(s)
	}
	t.id.region = s
}

// setVariants sets the variants ("a-b"; "" removes all); callers validate v
// with isLangTagVariants.
func (t *langTag) setVariants(v string) { t.id.variants = ltLower(v) }

// maximize and minimize apply UTS #35 Add Likely Subtags and Remove Likely
// Subtags (favoring region, as ECMA-402 Intl.Locale.prototype.minimize
// requires) to the base name, keeping extensions. A tag the likely subtags
// have no entry for stays as it is (ECMA-402: "If an error is signaled").
func (t *langTag) maximize() { t.id.addLikely() }

func (t *langTag) minimize() { t.id.removeLikely() }

// addLikely applies Add Likely Subtags to id, which is canonical, and
// reports whether the likely subtags had an entry for it.
func (id *ltID) addLikely() bool {
	script, region := id.script, id.region
	if script == "Zzzz" {
		script = ""
	}
	if region == "ZZ" {
		region = ""
	}
	if id.lang != "und" && script != "" && region != "" {
		id.script, id.region = script, region
		return true
	}
	v, ok := ltLikelyLookup(id.lang, script, region)
	if !ok {
		return false
	}
	if l := v >> 16; id.lang == "und" && l != 0 {
		id.lang = ltLikelyLanguage(l)
	}
	if script == "" {
		script = ltScriptAt(v >> 8 & 0xFF)
	}
	if region == "" {
		region = ltRegionAt(v & 0xFF)
	}
	id.script, id.region = script, region
	return true
}

// removeLikely applies Remove Likely Subtags to id (favoring region) and
// reports whether the likely subtags had an entry for it.
func (id *ltID) removeLikely() bool {
	m := ltID{lang: id.lang, script: id.script, region: id.region}
	if !m.addLikely() {
		return false
	}
	for _, trial := range [...]ltID{{lang: m.lang}, {lang: m.lang, region: m.region}, {lang: m.lang, script: m.script}} {
		if x := trial; x.addLikely() && x == m {
			m = trial
			break
		}
	}
	id.lang, id.script, id.region = m.lang, m.script, m.region
	return true
}
