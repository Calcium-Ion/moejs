package main

// The tables of engine/intl_date_tables.go: the Gregorian calendar data of
// Intl.DateTimeFormat for each data locale, as ICU's DateFormatSymbols,
// SimpleDateFormat, DateTimePatternGenerator and DateIntervalFormat read
// them: month, weekday, era and day period names, the day period rules,
// the date, time and date-time patterns (the "atTime" glue when the locale
// or a parent other than root has one, as ICU's %atTime resource), the
// availableFormats in ICU's enumeration order (the locale's own skeletons
// sorted, then each parent's that it lacks), the appendItems with the field
// names they use, and the intervalFormats with their fallback.
//
// Every string goes into one pool, terminated by NUL and ordered by how
// often the records refer to it, so the strings the locales share are
// stored once; a list of names (the 12 months of one width) is one string
// with its items separated by \x01. Each locale's record is a sequence of
// uvarints: byte offsets into the pool and counts.

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"slices"
	"strings"
)

const gregorian = "dates/calendars/calendar[@type='gregorian']/"

// The fields of DateTimePatternGenerator (UDateTimePatternField), whose
// order the appendItems and field names follow.
var dtpgAppendItems = [16]string{
	"Era", "Year", "Quarter", "Month", "Week", "", "Day-Of-Week",
	"", "", "Day", "", "Hour", "Minute", "Second", "", "Timezone",
}

var dtpgFieldNames = [16]string{
	"era", "year", "quarter", "month", "week", "weekOfMonth", "weekday",
	"dayOfYear", "weekdayOfMonth", "day", "dayperiod", "hour", "minute", "second", "", "zone",
}

// dayPeriodKeys are ICU's day periods in the order of its DayPeriod enum
// (DayPeriodRules): midnight 0 ... night2 9, then am 10 and pm 11.
var dayPeriodKeys = []string{
	"midnight", "noon", "morning1", "afternoon1", "evening1", "night1",
	"morning2", "afternoon2", "evening2", "night2", "am", "pm",
}

// dateRecord is one data locale's record as it is built.
type dateRecord struct {
	tag   string
	items []dateItem
}

// dateItem is a reference to a pool string or a count.
type dateItem struct {
	s     string
	count int
	isRef bool
}

func (r *dateRecord) ref(s string) { r.items = append(r.items, dateItem{s: s, isRef: true}) }
func (r *dateRecord) num(n int)    { r.items = append(r.items, dateItem{count: n}) }

// icuDatePatches are the values in which ICU 78.3's data (the reference
// of Intl.DateTimeFormat, through Node 24) departs from the CLDR 48.0
// release: the availableFormats of the skeleton Hv and an intervalFormat of it,
// zh-Hant's date-time glue (with a space; a thin space for short), the
// separator of one interval of de, and ko's abbreviated am/pm, which ICU
// has (오전, 오후) where CLDR inherits root's AM and PM; in the regional
// locales, es_419's Hv (which its children inherit), fr_CA's Hv (which
// ICU leaves out, fr's applying) and zh_Hant_HK's date-time glue. Each is
// set in its locale's file before the records are built; add is set for a
// value CLDR lacks, and an empty value deletes CLDR's.
var icuDatePatches = []struct {
	id, path, value string
	add             bool
}{
	{"root", gregorian + "dateTimeFormats/availableFormats/dateFormatItem[@id='Hv']", "HH v", false},
	{"en", gregorian + "dateTimeFormats/availableFormats/dateFormatItem[@id='Hv']", "HH v", false},
	{"es", gregorian + "dateTimeFormats/availableFormats/dateFormatItem[@id='Hv']", "H v", false},
	{"pt", gregorian + "dateTimeFormats/availableFormats/dateFormatItem[@id='Hv']", "HH v", false},
	{"ru", gregorian + "dateTimeFormats/availableFormats/dateFormatItem[@id='Hv']", "HH v", false},
	{"ru", gregorian + "dateTimeFormats/intervalFormats/intervalFormatItem[@id='Hv']/greatestDifference[@id='H']", "HH–HH v", false},
	{"it", gregorian + "dateTimeFormats/availableFormats/dateFormatItem[@id='Hv']", "HH v", false},
	{"zh_Hant", gregorian + "dateTimeFormats/availableFormats/dateFormatItem[@id='Hv']", "H時 v", true},
	{"zh_Hant", gregorian + "dateTimeFormats/dateTimeFormatLength[@type='full']/dateTimeFormat/pattern", "{1} {0}", false},
	{"zh_Hant", gregorian + "dateTimeFormats/dateTimeFormatLength[@type='long']/dateTimeFormat/pattern", "{1} {0}", false},
	{"zh_Hant", gregorian + "dateTimeFormats/dateTimeFormatLength[@type='medium']/dateTimeFormat/pattern", "{1} {0}", false},
	{"zh_Hant", gregorian + "dateTimeFormats/dateTimeFormatLength[@type='short']/dateTimeFormat/pattern", "{1}\u2009{0}", false},
	{"zh_Hant", gregorian + "dateTimeFormats/dateTimeFormatLength[@type='full']/dateTimeFormat[@type='atTime']/pattern", "{1} {0}", false},
	{"zh_Hant", gregorian + "dateTimeFormats/dateTimeFormatLength[@type='long']/dateTimeFormat[@type='atTime']/pattern", "{1} {0}", false},
	{"zh_Hant", gregorian + "dateTimeFormats/dateTimeFormatLength[@type='medium']/dateTimeFormat[@type='atTime']/pattern", "{1} {0}", false},
	{"zh_Hant", gregorian + "dateTimeFormats/dateTimeFormatLength[@type='short']/dateTimeFormat[@type='atTime']/pattern", "{1}\u2009{0}", true},
	{"de", gregorian + "dateTimeFormats/intervalFormats/intervalFormatItem[@id='GyMMMEd']/greatestDifference[@id='G']", "E, d. MMM y G\u2009–\u2009E, d. MMM y G", false},
	{"ko", gregorian + "dayPeriods/dayPeriodContext[@type='format']/dayPeriodWidth[@type='abbreviated']/dayPeriod[@type='am']", "오전", true},
	{"ko", gregorian + "dayPeriods/dayPeriodContext[@type='format']/dayPeriodWidth[@type='abbreviated']/dayPeriod[@type='pm']", "오후", true},
	{"es_419", gregorian + "dateTimeFormats/availableFormats/dateFormatItem[@id='Hv']", "HH v", true},
	{"fr_CA", gregorian + "dateTimeFormats/availableFormats/dateFormatItem[@id='Hv']", "", false},
	{"zh_Hant_HK", gregorian + "dateTimeFormats/dateTimeFormatLength[@type='full']/dateTimeFormat/pattern", "{1} {0}", false},
	{"zh_Hant_HK", gregorian + "dateTimeFormats/dateTimeFormatLength[@type='long']/dateTimeFormat/pattern", "{1} {0}", false},
	{"zh_Hant_HK", gregorian + "dateTimeFormats/dateTimeFormatLength[@type='medium']/dateTimeFormat/pattern", "{1} {0}", false},
	{"zh_Hant_HK", gregorian + "dateTimeFormats/dateTimeFormatLength[@type='short']/dateTimeFormat/pattern", "{1} {0}", false},
}

// icuNumberPatches are icuDatePatches' kind for the number data: fr_CH's
// group separator, which ICU has (as de_CH and it_CH do) and CLDR 48.0's
// fr_CH lacks.
var icuNumberPatches = []struct {
	id, path, value string
	add             bool
}{
	{"fr_CH", "numbers/symbols[@numberSystem='latn']/group", "'", true},
}

func (g *gen) emitDate(files map[string][]byte) []byte {
	d := g.locales
	var recs []*dateRecord
	for _, r := range d.recs {
		recs = append(recs, r.date)
	}

	// The pool: the most referenced strings first, then by value.
	refs := map[string]int{}
	for _, r := range recs {
		for _, it := range r.items {
			if it.isRef {
				refs[it.s]++
			}
		}
	}
	strs := sortedKeys(refs)
	slices.SortStableFunc(strs, func(a, b string) int { return refs[b] - refs[a] })
	offset := map[string]int{}
	var pool strings.Builder
	for _, s := range strs {
		if strings.ContainsRune(s, 0) {
			log.Fatalf("date string %q holds NUL", s)
		}
		offset[s] = pool.Len()
		pool.WriteString(s)
		pool.WriteByte(0)
	}
	encoded := make([]string, len(recs))
	for i, r := range recs {
		var enc []byte
		for _, it := range r.items {
			v := it.count
			if it.isRef {
				v = offset[it.s]
			}
			enc = binary.AppendUvarint(enc, uint64(v))
		}
		encoded[i] = string(enc)
	}

	var b bytes.Buffer
	p := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	p("// Code generated by internal/gen/cldr from CLDR %s; DO NOT EDIT.\n\n", version)
	p("%s", notice)
	p("package engine\n\n")
	p("// intlDateStrings is the pool of the strings of the date records: each\n")
	p("// string ends with a NUL; a list of names separates its items with \\x01\n")
	p("// (%d strings, %d bytes).\n", len(strs), pool.Len())
	p("const intlDateStrings = %s\n\n", quote(pool.String()))
	p("// intlDateRecords holds the Gregorian calendar data of each data locale in\n")
	p("// the order of the data indexes, as uvarints: byte offsets into\n")
	p("// intlDateStrings and counts, in the order intlDecodeDate reads them.\n")
	p("var intlDateRecords = [intlDataLocaleCount]string{\n")
	total := 0
	for i, r := range recs[:len(dataLocales)] {
		// The size of a locale: its record and the strings only it uses.
		own := 0
		for s := range ownStrings(r, recs) {
			own += len(s) + 1
		}
		p("\t// %s: %d bytes, %d of strings of its own\n", r.tag, len(encoded[i]), own)
		p("\t%s,\n", quote(encoded[i]))
		total += len(encoded[i])
	}
	p("}\n\n")
	p("// intlDateSubRecords holds the Gregorian calendar data of each regional\n")
	p("// locale with data of its own (intlSubLocales) as a delta of its parent's\n")
	p("// record (intlApplyDelta).\n")
	p("var intlDateSubRecords = [intlSubLocaleCount]string{\n")
	sub := 0
	for _, i := range d.subs() {
		s := delta(encoded[d.parent[i]], encoded[i], uvarintTokens)
		sub += len(s)
		own := 0
		for s := range ownStrings(recs[i], recs) {
			own += len(s) + 1
		}
		p("\t// %s: %d bytes, %d of strings of its own\n", d.tags[i], len(s), own)
		p("\t%s,\n", quote(s))
	}
	p("}\n\n")
	p("// The date data take %d bytes: the pool %d, the records %d, the regional\n", pool.Len()+total+sub, pool.Len(), total)
	p("// locales' deltas %d.\n", sub)
	return b.Bytes()
}

// ownStrings returns the strings only r refers to.
func ownStrings(r *dateRecord, recs []*dateRecord) map[string]bool {
	own := map[string]bool{}
	for _, it := range r.items {
		if it.isRef {
			own[it.s] = true
		}
	}
	for _, o := range recs {
		if o == r {
			continue
		}
		for _, it := range o.items {
			if it.isRef {
				delete(own, it.s)
			}
		}
	}
	return own
}

// dateRecord builds a data locale's record.
func (g *gen) dateRecord(tag string, rules map[string]string, numberings []string) *dateRecord {
	r := &dateRecord{tag: tag}
	val := func(path string) string { return g.ldml.mustValue(tag, path) }
	list := func(prefix string, keys []string) string {
		items := make([]string, len(keys))
		for i, k := range keys {
			items[i] = val(prefix + "[@type='" + k + "']")
		}
		return strings.Join(items, "\x01")
	}
	months := make([]string, 12)
	for i := range months {
		months[i] = fmt.Sprint(i + 1)
	}
	days := []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}
	// Months and weekdays: format abbreviated, wide, narrow, then
	// stand-alone abbreviated, wide, narrow.
	for _, ctx := range []string{"format", "stand-alone"} {
		for _, w := range []string{"abbreviated", "wide", "narrow"} {
			r.ref(list(gregorian+"months/monthContext[@type='"+ctx+"']/monthWidth[@type='"+w+"']/month", months))
		}
	}
	for _, ctx := range []string{"format", "stand-alone"} {
		for _, w := range []string{"abbreviated", "wide", "narrow"} {
			r.ref(list(gregorian+"days/dayContext[@type='"+ctx+"']/dayWidth[@type='"+w+"']/day", days))
		}
	}
	// Eras: abbreviated, wide, narrow.
	for _, w := range []string{"eraAbbr", "eraNames", "eraNarrow"} {
		r.ref(list(gregorian+"eras/"+w+"/era", []string{"0", "1"}))
	}
	// Day periods of the format context, abbreviated, wide, narrow: am and
	// pm (ICU's AmPmMarkersAbbr, AmPmMarkers, AmPmMarkersNarrow), then the
	// periods of dayPeriodKeys, "" for those the locale lacks.
	for _, w := range []string{"abbreviated", "wide", "narrow"} {
		prefix := gregorian + "dayPeriods/dayPeriodContext[@type='format']/dayPeriodWidth[@type='" + w + "']/dayPeriod"
		r.ref(list(prefix, []string{"am", "pm"}))
		items := make([]string, 10)
		for i, k := range dayPeriodKeys[:10] {
			items[i], _ = g.ldml.value(tag, prefix+"[@type='"+k+"']")
		}
		r.ref(strings.Join(items, "\x01"))
	}
	r.ref(dayPeriodRulesOf(tag, rules))

	// Time patterns full to short, date patterns full to short.
	for _, kind := range []string{"time", "date"} {
		for _, l := range []string{"full", "long", "medium", "short"} {
			r.ref(val(gregorian + kind + "Formats/" + kind + "FormatLength[@type='" + l + "']/" + kind + "Format/pattern"))
		}
	}
	// The date-time glue, full to short, then the atTime glue. ICU has an
	// %atTime resource only for a locale that has atTime patterns (root's
	// are aliases to the standard ones); without one, it uses the standard
	// glue.
	// A locale without atTime patterns of its own inherits the whole
	// resource of the nearest locale of its chain that has some, which
	// that locale's standard glue fills (fr_ML has fr's short glue, not
	// its own).
	var std, at [4]string
	owner := ""
	for id := cldrID(tag); id != "" && owner == ""; id = g.ldml.parent(id) {
		if f := g.ldml.file(id); f != nil {
			for p := range f.values {
				if strings.HasPrefix(p, gregorian+"dateTimeFormats/dateTimeFormatLength") && strings.HasSuffix(p, "[@type='atTime']/pattern") {
					owner = bcp47(id)
					break
				}
			}
		}
	}
	for i, l := range []string{"full", "long", "medium", "short"} {
		prefix := gregorian + "dateTimeFormats/dateTimeFormatLength[@type='" + l + "']/dateTimeFormat"
		std[i] = val(prefix + "/pattern")
		at[i] = std[i]
		if owner != "" {
			// Root's atTime patterns are aliases of the standard ones
			// ("[@type='standard']", which the files leave out).
			var ok bool
			if at[i], ok = g.ldml.value(owner, prefix+"[@type='atTime']/pattern"); !ok {
				at[i] = g.ldml.mustValue(owner, prefix+"/pattern")
			}
		}
	}
	for _, s := range std {
		r.ref(s)
	}
	for _, s := range at {
		r.ref(s)
	}

	// The appendItems and the field names, by DTPG field.
	var app, names [16]string
	for i, req := range dtpgAppendItems {
		if req != "" {
			app[i], _ = g.ldml.value(tag, gregorian+"dateTimeFormats/appendItems/appendItem[@request='"+req+"']")
		}
	}
	for i, f := range dtpgFieldNames {
		if f != "" {
			names[i], _ = g.ldml.value(tag, "dates/fields/field[@type='"+f+"']/displayName")
		}
	}
	r.ref(strings.Join(app[:], "\x01"))
	r.ref(strings.Join(names[:], "\x01"))
	// The decimal separator of the latn digits, for fractional seconds
	// appended to a pattern, then those of the other numbering systems
	// where they differ: "arab:٫;".
	dec := val("numbers/symbols[@numberSystem='latn']/decimal")
	r.ref(dec)
	var other strings.Builder
	for _, ns := range numberings {
		if v, ok := g.ldml.value(tag, "numbers/symbols[@numberSystem='"+ns+"']/decimal"); ok && v != dec {
			other.WriteString(ns + ":" + v + ";")
		}
	}
	r.ref(other.String())

	// availableFormats in ICU's order: the locale's own items sorted by
	// skeleton, then each parent's it lacks. Items with a count (MMMMW and yw,
	// with week fields Intl.DateTimeFormat never asks for) are left out.
	const avail = gregorian + "dateTimeFormats/availableFormats/dateFormatItem"
	var seen []string
	var pairs []string
	for _, f := range g.ldml.chain(tag) {
		var ids []string
		for p := range f.values {
			rest, ok := strings.CutPrefix(p, avail+"[@id='")
			if !ok {
				continue
			}
			id, tail, _ := strings.Cut(rest, "']")
			if tail != "" || slices.Contains(seen, id) || slices.Contains(ids, id) {
				continue
			}
			ids = append(ids, id)
		}
		slices.Sort(ids)
		for _, id := range ids {
			seen = append(seen, id)
			pairs = append(pairs, id, val(avail+"[@id='"+id+"']"))
		}
	}
	r.num(len(pairs) / 2)
	for _, s := range pairs {
		r.ref(s)
	}

	// intervalFormats: the fallback, then each skeleton, sorted, with its
	// greatest differences, sorted.
	const itv = gregorian + "dateTimeFormats/intervalFormats/"
	r.ref(val(itv + "intervalFormatFallback"))
	byID := map[string]map[string]string{}
	for rel, v := range g.ldml.leaves(tag, strings.TrimSuffix(itv, "/")) {
		rest, ok := strings.CutPrefix(rel, "intervalFormatItem[@id='")
		if !ok || strings.Contains(rest, "[@alt=") {
			continue // ICU has no alt variants (en_CA's d/M forms)
		}
		id, rest, _ := strings.Cut(rest, "']/greatestDifference[@id='")
		diff, tail, _ := strings.Cut(rest, "']")
		if tail != "" || len(diff) != 1 {
			log.Fatalf("%s: interval format %q", tag, rel)
		}
		if byID[id] == nil {
			byID[id] = map[string]string{}
		}
		byID[id][diff] = v
	}
	r.num(len(byID))
	for _, id := range sortedKeys(byID) {
		r.ref(id)
		diffs := sortedKeys(byID[id])
		r.num(len(diffs))
		for _, d := range diffs {
			r.num(int(d[0]))
			r.ref(byID[id][d])
		}
	}
	return r
}

// dayPeriodRules reads the dayPeriodRuleSet of dayPeriods.xml (the one
// without a type): each locale's rules as ICU's DayPeriodRules hold them, a
// string of 26 digits from '0': whether the rules have midnight and noon,
// then the day period (ICU's DayPeriod: midnight 0 ... night2 9, am 10, pm
// 11) of each hour.
func (g *gen) dayPeriodRules(data []byte) map[string]string {
	out := map[string]string{}
	var locales []string
	var rule [26]byte
	inSet, typed := false, false
	flush := func() {
		if len(locales) == 0 {
			return
		}
		for h := 2; h < 26; h++ {
			if rule[h] == 0 {
				log.Fatalf("day period rules of %v leave hour %d", locales, h-2)
			}
		}
		for _, l := range locales {
			out[l] = string(rule[:])
		}
		locales = nil
	}
	hour := func(s string) int {
		var h, m int
		if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil || m != 0 {
			log.Fatalf("day period time %q", s)
		}
		return h
	}
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Strict = false
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		var name string
		start := false
		var attr func(string) string
		switch t := tok.(type) {
		case xml.StartElement:
			name, start = t.Name.Local, true
			attr = func(k string) string { return attrOf(t, k) }
		case xml.EndElement:
			name = t.Name.Local
			attr = func(string) string { return "" }
		default:
			continue
		}
		switch name {
		case "dayPeriodRuleSet":
			inSet, typed = start, attr("type") != ""
		case "dayPeriodRules":
			if !inSet || typed {
				continue
			}
			if start {
				locales = strings.Fields(attr("locales"))
				rule = [26]byte{'0', '0'}
			} else {
				flush()
			}
		case "dayPeriodRule":
			if !start || !inSet || typed {
				continue
			}
			code := slices.Index(dayPeriodKeys, attr("type"))
			if code < 0 {
				log.Fatalf("day period %q", attr("type"))
			}
			if at := attr("at"); at != "" {
				switch code {
				case 0:
					rule[0] = '1'
				case 1:
					rule[1] = '1'
				default:
					log.Fatalf("day period %q at a time", attr("type"))
				}
				continue
			}
			from, before := hour(attr("from")), hour(attr("before"))
			for h := from; h != before%24 || h == from && rule[2+h] == 0; h = (h + 1) % 24 {
				rule[2+h] = byte('0' + code)
			}
		}
	}
	return out
}

// dayPeriodRulesOf returns the rules of a data locale: those of its CLDR id,
// else of the id truncated (ICU's uloc_getParent), else root's.
func dayPeriodRulesOf(tag string, rules map[string]string) string {
	id := cldrID(tag)
	for {
		if r, ok := rules[id]; ok {
			return r
		}
		i := strings.LastIndexByte(id, '_')
		if i < 0 {
			return rules["root"]
		}
		id = id[:i]
	}
}
