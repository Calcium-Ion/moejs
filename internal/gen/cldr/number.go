package main

// The tables of engine/intl_number_tables.go: the number symbols and
// patterns, currencies, units, compact patterns and plural rules of
// Intl.NumberFormat and Intl.PluralRules.
//
// The locale data is the data ICU 78 formats with, read from CLDR with the
// inheritance of UTS #35 (ldml.go): the symbols and patterns of each
// numbering system (root aliases most of them to latn), the currency
// spacing, the compact patterns of the short and long decimal and short
// currency formats (ICU uses neither the alphaNextToNumber nor the
// noCurrency alternatives, nor the approximately pattern), the range
// pattern, the currency names and the unit patterns. The plural rules are
// those of every CLDR language.

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// sanctionedUnits are ECMA-402's single units sanctioned for use in
// ECMAScript (Table 2), sorted.
var sanctionedUnits = []string{
	"acre", "bit", "byte", "celsius", "centimeter", "day", "degree", "fahrenheit",
	"fluid-ounce", "foot", "gallon", "gigabit", "gigabyte", "gram", "hectare", "hour",
	"inch", "kilobit", "kilobyte", "kilogram", "kilometer", "liter", "megabit", "megabyte",
	"meter", "microsecond", "mile", "mile-scandinavian", "milliliter", "millimeter",
	"millisecond", "minute", "month", "nanosecond", "ounce", "percent", "petabyte",
	"pound", "second", "stone", "terabit", "terabyte", "week", "yard", "year",
}

// pluralCats are CLDR's plural categories in order; pluralCodes are their
// one-letter codes in the tables.
var pluralCats = []string{"zero", "one", "two", "few", "many", "other"}

const pluralCodes = "zotfmx"

// catCode returns the code of a plural category or of an explicit count of
// the compact patterns ("0", "1").
func catCode(c string) byte {
	if i := slices.Index(pluralCats, c); i >= 0 {
		return pluralCodes[i]
	}
	if c == "0" || c == "1" {
		return c[0]
	}
	log.Fatalf("unknown plural count %q", c)
	return 0
}

// The separators of the records, from the outermost.
const (
	sepRecord = "\x1e"
	sepField  = "\x1f"
	sepItem   = "\x1d"
	sepSub    = "\x1c"
)

// numSymbols are the symbols of a numbering system a NumberFormat uses, in
// the order of the record's fields.
var numSymbols = []string{
	"decimal", "group", "percentSign", "plusSign", "minusSign", "approximatelySign",
	"exponential", "infinity", "nan", "currencyDecimal", "currencyGroup",
}

// pluralGroup is a <pluralRules> or <pluralRanges> element: its locales and
// its children's attributes and text.
type pluralGroup struct {
	locales []string
	items   []pluralItem
}

type pluralItem struct {
	attr map[string]string
	text string
}

// readPluralGroups reads the groups named group with children named item.
func readPluralGroups(data []byte, group, item string) []pluralGroup {
	if data == nil {
		log.Fatalf("missing file for <%s>", group)
	}
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Strict = false
	var out []pluralGroup
	var cur *pluralItem
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return out
		}
		if err != nil {
			log.Fatal(err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case group:
				out = append(out, pluralGroup{locales: strings.Fields(attrOf(t, "locales"))})
			case item:
				it := pluralItem{attr: map[string]string{}}
				for _, a := range t.Attr {
					it.attr[a.Name.Local] = a.Value
				}
				g := &out[len(out)-1]
				g.items = append(g.items, it)
				cur = &g.items[len(g.items)-1]
			}
		case xml.CharData:
			if cur != nil {
				cur.text += string(t)
			}
		case xml.EndElement:
			if t.Name.Local == item {
				cur = nil
			}
		}
	}
}

// pluralData is the plural rules of every CLDR language.
type pluralData struct {
	sets     []string       // rule sets, cardinal and ordinal, deduplicated
	cardinal map[string]int // CLDR id → index in sets
	ordinal  map[string]int
	cats     map[string][]string // CLDR id → its cardinal categories in order, other included
	ranges   []string            // range sets, "start end result" codes
	rangeOf  map[string]int      // language → index in ranges
}

func (g *gen) loadPlurals(files map[string][]byte) *pluralData {
	p := &pluralData{cardinal: map[string]int{}, ordinal: map[string]int{}, cats: map[string][]string{}, rangeOf: map[string]int{}}
	setIndex := map[string]int{}
	add := func(file string, into map[string]int, cardinal bool) {
		for _, grp := range readPluralGroups(files["common/supplemental/"+file], "pluralRules", "pluralRule") {
			var src strings.Builder
			var cats []string
			for _, it := range grp.items {
				count := it.attr["count"]
				cats = append(cats, count)
				if count == "other" {
					continue
				}
				cond, _, _ := strings.Cut(it.text, "@")
				cond = strings.Join(strings.Fields(cond), " ")
				if cond == "" {
					log.Fatalf("%s: empty condition for %s of %v", file, count, grp.locales)
				}
				fmt.Fprintf(&src, "%s:%s;", count, cond)
			}
			if !slices.Contains(cats, "other") {
				log.Fatalf("%s: no other for %v", file, grp.locales)
			}
			slices.SortFunc(cats, func(a, b string) int { return slices.Index(pluralCats, a) - slices.Index(pluralCats, b) })
			s := src.String()
			i, ok := setIndex[s]
			if !ok {
				i = len(p.sets)
				setIndex[s] = i
				p.sets = append(p.sets, s)
			}
			for _, l := range grp.locales {
				if _, dup := into[l]; dup {
					log.Fatalf("%s: %s twice", file, l)
				}
				into[l] = i
				if cardinal {
					p.cats[l] = cats
				}
			}
		}
	}
	add("plurals.xml", p.cardinal, true)
	add("ordinals.xml", p.ordinal, false)
	if _, ok := p.cardinal["root"]; !ok {
		log.Fatal("plurals.xml has no root")
	}
	rangeIndex := map[string]int{}
	for _, grp := range readPluralGroups(files["common/supplemental/pluralRanges.xml"], "pluralRanges", "pluralRange") {
		var b strings.Builder
		for _, it := range grp.items {
			b.WriteByte(catCode(it.attr["start"]))
			b.WriteByte(catCode(it.attr["end"]))
			b.WriteByte(catCode(it.attr["result"]))
		}
		s := b.String()
		i, ok := rangeIndex[s]
		if !ok {
			i = len(p.ranges)
			rangeIndex[s] = i
			p.ranges = append(p.ranges, s)
		}
		for _, l := range grp.locales {
			// ICU looks the ranges up by language (pt_PT gets pt's).
			if strings.Contains(l, "_") {
				continue
			}
			p.rangeOf[l] = i
		}
	}
	return p
}

// catsOf returns the cardinal categories of a CLDR id (truncated until
// plurals.xml lists it, else root's).
func (p *pluralData) catsOf(id string) []string {
	for id != "" {
		if c, ok := p.cats[id]; ok {
			return c
		}
		i := strings.LastIndexByte(id, '_')
		if i < 0 {
			break
		}
		id = id[:i]
	}
	return p.cats["root"]
}

// cardinalOf returns the cardinal rule set of a CLDR id (truncated until
// plurals.xml lists it, else root's).
func (p *pluralData) cardinalOf(id string) int {
	for id != "" {
		if i, ok := p.cardinal[id]; ok {
			return i
		}
		i := strings.LastIndexByte(id, '_')
		if i < 0 {
			break
		}
		id = id[:i]
	}
	return p.cardinal["root"]
}

// pluralLocales returns intlPluralLocales.
func (p *pluralData) pluralLocales() string {
	ids := map[string]bool{}
	for id := range p.cardinal {
		ids[id] = true
	}
	for id := range p.ordinal {
		ids[id] = true
	}
	for id := range p.rangeOf {
		ids[id] = true
	}
	var b strings.Builder
	for _, id := range sortedKeys(ids) {
		field := func(m map[string]int) string {
			if i, ok := m[id]; ok {
				return strconv.Itoa(i)
			}
			return ""
		}
		fmt.Fprintf(&b, "%s:%s,%s,%s;", strings.ReplaceAll(id, "_", "-"), field(p.cardinal), field(p.ordinal), field(p.rangeOf))
	}
	return b.String()
}

// pluralCoded encodes values by plural category (or explicit count) as
// "<code><text>" items, in the order of codes.
func pluralCoded(m map[string]string) string {
	var items []string
	for _, c := range []string{"0", "1", "zero", "one", "two", "few", "many", "other"} {
		if v, ok := m[c]; ok {
			items = append(items, string(catCode(c))+v)
		}
	}
	return strings.Join(items, sepItem)
}

// leafKey parses the relative path of a leaf below a prefix into its
// element name and its predicates by attribute.
func leafKey(rel string) (string, map[string]string) {
	segs := splitPath(rel)
	name, preds := splitPreds(segs[len(segs)-1])
	attrs := map[string]string{}
	for _, p := range preds {
		// [@name='value']
		k, v, ok := strings.Cut(strings.TrimSuffix(strings.TrimPrefix(p, "[@"), "]"), "=")
		if !ok {
			log.Fatalf("bad predicate %q", p)
		}
		attrs[k] = strings.Trim(v, "'")
	}
	if len(segs) > 1 {
		attrs["@parent"] = strings.Join(segs[:len(segs)-1], "/")
	}
	return name, attrs
}

// compactTable returns the compact patterns below prefix as
// "<magnitude><code><pattern>" items, the magnitude being 'a' + the
// exponent of the pattern's type, sorted; "0" patterns (no compact form)
// are kept.
func (g *gen) compactTable(tag, prefix string) string {
	var items []string
	for rel, v := range g.ldml.leaves(tag, prefix) {
		name, attrs := leafKey(rel)
		if name != "pattern" || attrs["alt"] != "" || attrs["@parent"] != "" {
			continue
		}
		t := attrs["type"]
		if t == "" || strings.Trim(t, "0") != "1" {
			log.Fatalf("%s: compact pattern type %q", tag, t)
		}
		count := attrs["count"]
		if count == "" {
			count = "other"
		}
		items = append(items, string(rune('a'+len(t)-1))+string(catCode(count))+v)
	}
	// Sort by magnitude, then by code order.
	slices.SortFunc(items, func(a, b string) int {
		if a[0] != b[0] {
			return int(a[0]) - int(b[0])
		}
		return strings.IndexByte("01"+pluralCodes, a[1]) - strings.IndexByte("01"+pluralCodes, b[1])
	})
	return strings.Join(items, sepItem)
}

// systemRecord returns the fields of a numbering system's record.
func (g *gen) systemRecord(tag, nu string) []string {
	var f []string
	ns := "[@numberSystem='" + nu + "']"
	for _, s := range numSymbols {
		v, ok := g.ldml.value(tag, "numbers/symbols"+ns+"/"+s)
		if !ok {
			// The separators of currency amounts (de_AT's group, fr_CH's
			// decimal) default to the others.
			switch s {
			case "currencyDecimal":
				v = f[0]
			case "currencyGroup":
				v = f[1]
			default:
				log.Fatalf("%s: no symbol %s of %s", tag, s, nu)
			}
		}
		f = append(f, v)
	}
	f = append(f,
		g.ldml.mustValue(tag, "numbers/decimalFormats"+ns+"/decimalFormatLength/decimalFormat/pattern"),
		g.ldml.mustValue(tag, "numbers/percentFormats"+ns+"/percentFormatLength/percentFormat/pattern"),
		g.ldml.mustValue(tag, "numbers/scientificFormats"+ns+"/scientificFormatLength/scientificFormat/pattern"),
		g.ldml.mustValue(tag, "numbers/currencyFormats"+ns+"/currencyFormatLength/currencyFormat[@type='standard']/pattern"),
		g.ldml.mustValue(tag, "numbers/currencyFormats"+ns+"/currencyFormatLength/currencyFormat[@type='accounting']/pattern"),
	)
	for _, side := range []string{"beforeCurrency", "afterCurrency"} {
		for _, k := range []string{"currencyMatch", "surroundingMatch", "insertBetween"} {
			v := g.ldml.mustValue(tag, "numbers/currencyFormats"+ns+"/currencySpacing/"+side+"/"+k)
			// The engine knows the two sets ICU special-cases.
			if k != "insertBetween" && v != "[[:^S:]&[:^Z:]]" && v != "[:digit:]" {
				log.Fatalf("%s %s: currency spacing set %q", tag, nu, v)
			}
			f = append(f, v)
		}
	}
	rng, ok := g.ldml.value(tag, "numbers/miscPatterns"+ns+"/pattern[@type='range']")
	if !ok {
		if rng, ok = g.ldml.value(tag, "numbers/miscPatterns[@numberSystem='latn']/pattern[@type='range']"); !ok {
			rng = "{0}–{1}"
		}
	}
	f = append(f, rng,
		g.compactTable(tag, "numbers/decimalFormats"+ns+"/decimalFormatLength[@type='short']/decimalFormat"),
		g.compactTable(tag, "numbers/decimalFormats"+ns+"/decimalFormatLength[@type='long']/decimalFormat"),
		g.compactTable(tag, "numbers/currencyFormats"+ns+"/currencyFormatLength[@type='short']/currencyFormat[@type='standard']"),
	)
	for i, v := range f {
		bad := sepRecord + sepField + sepSub
		if i < len(f)-3 {
			bad += sepItem
		}
		// A compact table may be empty: ICU then falls back to latn's,
		// and for the long format to the short one.
		if v == "" && i < len(f)-3 || strings.ContainsAny(v, bad) {
			log.Fatalf("%s %s: field %d is %q", tag, nu, i, v)
		}
	}
	return f
}

// numberRecord returns a data locale's record of intlNumberLocaleData: its
// locale fields, then its numbering systems, latn first, then those whose
// record differs from latn's, with "" for each field equal to latn's.
func (g *gen) numberRecord(tag string, p *pluralData, systems []string) string {
	cats := p.catsOf(cldrID(tag))
	var locale []string
	locale = append(locale, g.ldml.mustValue(tag, "numbers/minimumGroupingDigits"))
	// The currency long name patterns, at the locale level as ICU has
	// them (latn's), every category of the locale filled from other.
	up := map[string]string{}
	for rel, v := range g.ldml.leaves(tag, "numbers/currencyFormats[@numberSystem='latn']") {
		if name, attrs := leafKey(rel); name == "unitPattern" && attrs["@parent"] == "" && attrs["alt"] == "" && attrs["case"] == "" {
			up[attrs["count"]] = v
		}
	}
	if up["other"] == "" {
		log.Fatalf("%s: no currency unit pattern", tag)
	}
	for _, c := range cats {
		if _, ok := up[c]; !ok {
			up[c] = up["other"]
		}
	}
	for c := range up {
		if !slices.Contains(cats, c) {
			delete(up, c)
		}
	}
	locale = append(locale, pluralCoded(up))
	// Currencies with a pattern or separators of their own: ICU's
	// Currencies entries of three items (pattern, decimal, group), which a
	// locale inherits whole from the file that has them, what that file
	// leaves out being its locale's (en_150's EUR pattern comes with
	// en_150's separators in en_NL; de_LU's LUF has de_LU's currency
	// pattern, standard or accounting).
	codes := map[string]bool{}
	for rel := range g.ldml.leaves(tag, "numbers/currencies") {
		name, attrs := leafKey(rel)
		if name == "pattern" || name == "decimal" || name == "group" {
			codes[strings.TrimSuffix(strings.TrimPrefix(attrs["@parent"], "currency[@type='"), "']")] = true
		}
	}
	var ov []string
	for _, code := range sortedKeys(codes) {
		prefix := normPath("numbers/currencies/currency[@type='" + code + "']/")
		owner := ""
		for id := cldrID(tag); id != "" && owner == ""; id = g.ldml.parent(id) {
			if f := g.ldml.file(id); f != nil {
				for _, k := range []string{"pattern", "decimal", "group"} {
					if _, ok := f.values[prefix+k]; ok {
						owner = bcp47(id)
					}
				}
			}
		}
		pattern, ok := g.ldml.value(owner, prefix+"pattern")
		if !ok {
			pattern = g.ldml.mustValue(owner, "numbers/currencyFormats[@numberSystem='latn']/currencyFormatLength/currencyFormat[@type='standard']/pattern")
		}
		sep := func(k string) string {
			if v, ok := g.ldml.value(owner, prefix+k); ok {
				return v
			}
			return g.ldml.mustValue(owner, "numbers/symbols[@numberSystem='latn']/"+k)
		}
		ov = append(ov, code+sepItem+pattern+sepItem+sep("decimal")+sepItem+sep("group"))
	}
	// ICU sets the override of the locale's default currency (its region's,
	// else its likely region's) at construction, and a currency without
	// one keeps it: "*" (en_NL formats dollars with en_150's euro pattern,
	// pt_CV euros with its escudo's decimal separator).
	lid, _ := parseLangID(cldrID(tag))
	region := lid.region
	if region == "" {
		region = g.maximize(lid).region
	}
	if def := g.in.currencyOf[region]; def != "" {
		for _, o := range ov {
			if strings.HasPrefix(o, def+sepItem) {
				ov = append([]string{"*" + strings.TrimPrefix(o, def)}, ov...)
				break
			}
		}
	}
	locale = append(locale, strings.Join(ov, sepSub))
	recs := []string{strings.Join(locale, sepField)}
	latn := g.systemRecord(tag, "latn")
	recs = append(recs, "latn"+sepField+strings.Join(latn, sepField))
	for _, nu := range systems {
		if nu == "latn" {
			continue
		}
		rec := g.systemRecord(tag, nu)
		if slices.Equal(rec, latn) {
			continue
		}
		for i := range rec {
			if rec[i] == latn[i] {
				rec[i] = ""
			}
		}
		recs = append(recs, nu+sepField+strings.Join(rec, sepField))
	}
	return strings.Join(recs, sepRecord)
}

// currencyCodes returns the codes with currency data in any of the
// locales, sorted.
func (g *gen) currencyCodes(tags []string) []string {
	set := map[string]bool{}
	for _, tag := range tags {
		for rel := range g.ldml.leaves(tag, "numbers/currencies") {
			_, attrs := leafKey(rel)
			code := strings.TrimSuffix(strings.TrimPrefix(attrs["@parent"], "currency[@type='"), "']")
			if len(code) != 3 {
				log.Fatalf("%s: currency path %s", tag, rel)
			}
			set[code] = true
		}
	}
	return sortedKeys(set)
}

// currencyRecord returns a data locale's record of intlCurrencyNames: for
// each code of codes, its symbol ("" for the code), narrow symbol ("" for
// the symbol) and plural names, other's first ("" for the code), then each
// other category of the locale ("" for other's).
func (g *gen) currencyRecord(tag string, p *pluralData, codes []string) string {
	cats := p.catsOf(cldrID(tag))
	leaves := g.ldml.leaves(tag, "numbers/currencies")
	get := func(code, rel string) string { return leaves["currency[@type='"+code+"']/"+rel] }
	var entries []string
	for _, code := range codes {
		sym := get(code, "symbol")
		resolvedSym := sym
		if resolvedSym == "" {
			resolvedSym = code
		}
		narrow := get(code, "symbol[@alt='narrow']")
		if narrow == resolvedSym {
			narrow = ""
		}
		if sym == code {
			sym = ""
		}
		other := get(code, "displayName[@count='other']")
		if other == "" {
			other = get(code, "displayName")
		}
		if other == "" {
			other = code
		}
		f := []string{sym, narrow}
		if other == code {
			f = append(f, "")
		} else {
			f = append(f, other)
		}
		for _, c := range cats {
			if c == "other" {
				continue
			}
			n := get(code, "displayName[@count='"+c+"']")
			if n == other {
				n = ""
			}
			f = append(f, n)
		}
		for _, v := range f {
			if strings.ContainsAny(v, sepRecord+sepField) {
				log.Fatalf("%s %s: bad name %q", tag, code, v)
			}
		}
		e := strings.Join(f, sepField)
		e = strings.TrimRight(e, sepField)
		entries = append(entries, e)
	}
	return strings.Join(entries, sepRecord)
}

// unitIDs returns the CLDR ids of the sanctioned units and of the built-in
// units that are a sanctioned unit per another ("speed-kilometer-per-hour"),
// and the unit list: the sanctioned units, then those built-in ones.
func (g *gen) unitIDs() (ids map[string]string, list []string) {
	ids = map[string]string{}
	all := map[string]bool{}
	for rel := range g.ldml.leaves("en", "units/unitLength[@type='long']") {
		segs := splitPath(rel)
		if name, preds := splitPreds(segs[0]); name == "unit" && len(preds) == 1 {
			all[strings.TrimSuffix(strings.TrimPrefix(preds[0], "[@type='"), "']")] = true
		}
	}
	for id := range all {
		cat, unit, _ := strings.Cut(id, "-")
		_ = cat
		if slices.Contains(sanctionedUnits, unit) {
			ids[unit] = id
			continue
		}
		if num, den, ok := strings.Cut(unit, "-per-"); ok && slices.Contains(sanctionedUnits, num) && slices.Contains(sanctionedUnits, den) {
			ids[unit] = id
		}
	}
	for _, u := range sanctionedUnits {
		if ids[u] == "" {
			log.Fatalf("no CLDR unit for %s", u)
		}
	}
	list = slices.Clone(sanctionedUnits)
	var compound []string
	for u := range ids {
		if !slices.Contains(sanctionedUnits, u) {
			compound = append(compound, u)
		}
	}
	slices.Sort(compound)
	return ids, append(list, compound...)
}

// perCase returns the grammatical case of the denominator of a per unit in
// a language (grammaticalFeatures.xml's deriveComponent case per value1, as
// ICU loads it: the language's rule, else root's when the language has no
// derivations).
func perCase(files map[string][]byte, lang string) string {
	var cur []string
	rules := map[string]string{}
	has := map[string]bool{}
	d := xml.NewDecoder(bytes.NewReader(files["common/supplemental/grammaticalFeatures.xml"]))
	d.Strict = false
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		if se, ok := tok.(xml.StartElement); ok {
			switch se.Name.Local {
			case "grammaticalDerivations":
				cur = strings.Fields(attrOf(se, "locales"))
				for _, l := range cur {
					has[l] = true
				}
			case "deriveComponent":
				if attrOf(se, "feature") == "case" && attrOf(se, "structure") == "per" {
					for _, l := range cur {
						rules[l] = attrOf(se, "value1")
					}
				}
			}
		}
	}
	if has[lang] {
		return rules[lang]
	}
	return rules["root"]
}

// unitRecord returns a data locale's record of intlUnitData: for each unit
// of list, its long, short and narrow forms ("" for narrow when it equals
// short), each the patterns of other and of the locale's other categories
// ("" for other's) and, for a sanctioned unit, its per form: the
// perUnitPattern, else the per compound pattern with the unit's singular
// name ("{0}/s", "{0} per second").
func (g *gen) unitRecord(files map[string][]byte, tag string, p *pluralData, ids map[string]string, list []string) string {
	cats := p.catsOf(cldrID(tag))
	lang, _, _ := strings.Cut(cldrID(tag), "_")
	pc := perCase(files, lang)
	var entries []string
	widths := []string{"long", "short", "narrow"}
	leaves := map[string]map[string]string{}
	for _, w := range widths {
		leaves[w] = g.ldml.leaves(tag, "units/unitLength[@type='"+w+"']")
	}
	for _, u := range list {
		var forms []string
		for wi, w := range widths {
			lv := leaves[w]
			base := "unit[@type='" + ids[u] + "']/"
			pat := func(count, cs string) string {
				if cs != "" {
					return lv[base+"unitPattern[@case='"+cs+"'][@count='"+count+"']"]
				}
				return lv[base+"unitPattern[@count='"+count+"']"]
			}
			other := pat("other", "")
			if other == "" {
				log.Fatalf("%s %s %s: no other pattern", tag, w, u)
			}
			f := []string{other}
			for _, c := range cats {
				if c == "other" {
					continue
				}
				v := pat(c, "")
				if v == other {
					v = ""
				}
				f = append(f, v)
			}
			if slices.Contains(sanctionedUnits, u) {
				per := lv[base+"perUnitPattern"]
				if per == "" {
					compound, ok := g.ldml.value(tag, "units/unitLength[@type='"+w+"']/compoundUnit[@type='per']/compoundUnitPattern")
					if !ok {
						compound = g.ldml.mustValue(tag, "units/unitLength[@type='short']/compoundUnit[@type='per']/compoundUnitPattern")
					}
					one := ""
					if w == "long" && pc != "" {
						if one = pat("one", pc); one == "" {
							one = pat("other", pc)
						}
					}
					if one == "" {
						if one = pat("one", ""); one == "" {
							one = other
						}
					}
					name := strings.TrimFunc(strings.Replace(one, "{0}", "", 1), func(r rune) bool { return unicode.Is(unicode.Z, r) })
					per = strings.Replace(strings.Replace(compound, "{0}", "\x00", 1), "{1}", name, 1)
					per = strings.Replace(per, "\x00", "{0}", 1)
				}
				f = append(f, per)
			}
			for _, v := range f {
				if strings.ContainsAny(v, sepRecord+sepField+sepItem) {
					log.Fatalf("%s %s: bad pattern %q", tag, u, v)
				}
			}
			form := strings.Join(f, sepField)
			if wi == 2 && form == forms[1] {
				form = ""
			}
			forms = append(forms, form)
		}
		entries = append(entries, strings.Join(forms, sepItem))
	}
	return strings.Join(entries, sepRecord)
}

// currencyInfo returns intlCurrencyDigits (the codes whose standard digits
// are not 2, as "CODEd", sorted) and the codes of the currencies in use
// (supplementalData.xml's region currencies without an end date that are
// tender), sorted.
func currencyInfo(files map[string][]byte) (digits string, current []string) {
	sd := files["common/supplemental/supplementalData.xml"]
	var recs []string
	eachElement(sd, "info", func(attr func(string) string) {
		code, d := attr("iso4217"), attr("digits")
		if code == "DEFAULT" {
			if d != "2" {
				log.Fatal("currencyData DEFAULT digits are not 2")
			}
			return
		}
		if len(d) != 1 || d[0] < '0' || d[0] > '9' {
			log.Fatalf("currency %s digits %q", code, d)
		}
		if d != "2" {
			recs = append(recs, code+d)
		}
	})
	slices.Sort(recs)
	set := map[string]bool{}
	eachElement(sd, "currency", func(attr func(string) string) {
		code := attr("iso4217")
		if code == "" || attr("to") != "" || attr("tender") == "false" {
			return
		}
		set[code] = true
	})
	return strings.Join(recs, ""), sortedKeys(set)
}

// emitNumber returns the source of the tables.
func (g *gen) emitNumber(files map[string][]byte) []byte {
	var b bytes.Buffer
	pr := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	pl, codes, units := g.in.plurals, g.in.codes, g.in.units
	digits, current := currencyInfo(files)
	d := g.locales
	// subTable writes the deltas of the regional locales' records.
	subTable := func(name, what string, rec func(*localeRecs) string) {
		pr("// %s holds the %s of each regional locale with data of its own\n", name, what)
		pr("// (intlSubLocales) as a delta of its parent's record (intlApplyDelta).\n")
		pr("var %s = [intlSubLocaleCount]string{\n", name)
		n := 0
		for _, i := range d.subs() {
			s := delta(rec(d.recs[d.parent[i]]), rec(d.recs[i]), textTokens)
			n += len(s)
			pr("\t// %s\n\t%s,\n", d.tags[i], quote(s))
		}
		pr("}\n\n")
		log.Printf("%s: %d bytes", name, n)
	}

	pr("// Code generated by internal/gen/cldr from CLDR %s; DO NOT EDIT.\n\n", version)
	pr("%s", notice)
	pr("package engine\n\n")

	pr("// intlNumberLocaleData holds the number data of each data locale, by data\n")
	pr("// index: records separated by 0x1E, fields by 0x1F. The first record has the\n")
	pr("// locale's fields: its minimum grouping digits, its currency long name\n")
	pr("// patterns by plural category and its currencies with a pattern and\n")
	pr("// separators of their own (\"CODE pattern decimal group\", items separated\n")
	pr("// by 0x1D, entries by 0x1C; CODE \"*\" for the default currency's, which the\n")
	pr("// others without one take). Each next record is a numbering system's, latn\n")
	pr("// first, then those whose data differs, an empty field being latn's: its\n")
	pr("// id, the symbols\n")
	pr("// %s, the decimal, percent, scientific, currency and accounting patterns,\n", strings.Join(numSymbols, ", "))
	pr("// the currency spacing before and after the currency (currencyMatch,\n")
	pr("// surroundingMatch, insertBetween), the range pattern and the compact\n")
	pr("// patterns of the short and long decimal and short currency formats as\n")
	pr("// 0x1D-separated items \"<magnitude><category><pattern>\", magnitude 'a' + the\n")
	pr("// exponent. Plural categories are coded %q (zero ... other), explicit\n", pluralCodes)
	pr("// counts \"0\" and \"1\".\n")
	pr("var intlNumberLocaleData = [intlDataLocaleCount]string{\n")
	for i, tag := range dataLocales {
		pr("\t// %s\n\t%s,\n", tag, quote(d.recs[i].number))
	}
	pr("}\n\n")
	subTable("intlNumberSubData", "number data", func(r *localeRecs) string { return r.number })

	pr("// intlCurrencyList lists the codes of the currencies with names or symbols\n")
	pr("// in the data locales or the regional ones, three letters each, sorted.\n")
	pr("const intlCurrencyList = %s\n\n", quote(strings.Join(codes, "")))

	pr("// intlCurrencyNames holds the currency names of each data locale: an entry\n")
	pr("// per code of intlCurrencyList, separated by 0x1E, of fields separated by\n")
	pr("// 0x1F (trailing empty ones dropped): the symbol (empty: the code), the\n")
	pr("// narrow symbol (empty: the symbol), the plural name of other (empty: the\n")
	pr("// code) and of each other category of the locale in order (empty: other's).\n")
	pr("var intlCurrencyNames = [intlDataLocaleCount]string{\n")
	for i, tag := range dataLocales {
		pr("\t// %s\n\t%s,\n", tag, quote(d.recs[i].currency))
	}
	pr("}\n\n")
	subTable("intlCurrencySubNames", "currency names", func(r *localeRecs) string { return r.currency })

	pr("// intlCurrencyDigits lists the currencies whose standard number of fraction\n")
	pr("// digits (currencyData) is not 2, as \"CODEd\", sorted.\n")
	pr("const intlCurrencyDigits = %s\n\n", quote(digits))

	pr("// intlCurrencyCurrent lists the currencies in use (supplementalData.xml's\n")
	pr("// region currencies without an end date that are tender), three letters\n")
	pr("// each, sorted.\n")
	pr("const intlCurrencyCurrent = %s\n\n", quote(strings.Join(current, "")))

	pr("// intlUnitList lists the units of intlUnitData, ','-separated: the sanctioned\n")
	pr("// single units (ECMA-402 Table 2), sorted, then the built-in units that are a\n")
	pr("// sanctioned unit per another, sorted.\n")
	pr("const intlUnitList = %q\n\n", strings.Join(units, ","))
	pr("// intlSanctionedUnitCount is the number of sanctioned single units.\n")
	pr("const intlSanctionedUnitCount = %d\n\n", len(sanctionedUnits))

	pr("// intlUnitData holds the unit patterns of each data locale: an entry per\n")
	pr("// unit of intlUnitList, separated by 0x1E, of the long, short and narrow\n")
	pr("// forms separated by 0x1D (narrow empty when it is short's), each of fields\n")
	pr("// separated by 0x1F: the pattern of other, of each other category of the\n")
	pr("// locale in order (empty: other's) and, for a single unit, its per form\n")
	pr("// (the perUnitPattern, else the per compound pattern with the unit's\n")
	pr("// singular name).\n")
	pr("var intlUnitData = [intlDataLocaleCount]string{\n")
	for i, tag := range dataLocales {
		pr("\t// %s\n\t%s,\n", tag, quote(d.recs[i].unit))
	}
	pr("}\n\n")
	subTable("intlUnitSubData", "unit patterns", func(r *localeRecs) string { return r.unit })

	pr("// intlPluralRuleSets lists the plural rule sets of plurals.xml and\n")
	pr("// ordinals.xml, '\\n'-separated, each as parseIntlPluralRules reads them.\n")
	pr("const intlPluralRuleSets = %s\n\n", quote(strings.Join(pl.sets, "\n")))
	pr("// intlPluralRuleSetCount is the number of rule sets of intlPluralRuleSets.\n")
	pr("const intlPluralRuleSetCount = %d\n\n", len(pl.sets))
	pr("// intlPluralLocales maps the CLDR locales of the plural rules to their\n")
	pr("// cardinal and ordinal rule sets (indexes in intlPluralRuleSets) and, for\n")
	pr("// languages, their plural ranges (indexes in intlPluralRanges), as\n")
	pr("// \"tag:cardinal,ordinal,ranges;\" sorted by tag, an empty index for none.\n")
	pr("const intlPluralLocales = %s\n\n", quote(pl.pluralLocales()))
	pr("// intlPluralRanges lists the plural range sets of pluralRanges.xml,\n")
	pr("// '\\n'-separated, each as triples of category codes (start, end, result).\n")
	pr("const intlPluralRanges = %s\n", quote(strings.Join(pl.ranges, "\n")))
	return b.Bytes()
}
