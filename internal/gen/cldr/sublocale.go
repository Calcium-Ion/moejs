package main

// The regional locales of the data locales' languages (en_001, en_GB,
// de_CH, zh_Hant_HK, ...) with the inheritance of UTS #35, as the data
// locales have theirs: a regional locale whose data, as the engine's
// locale tables hold it, differs from its nearest ancestor's with data gets
// a data index after the data locales' and, in each locale table, a delta
// of that ancestor's record (in effect the overrides of its CLDR file and of
// the files between); one whose data is the ancestor's shares its data
// index.

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"io"
	"log"
	"slices"
	"strconv"
	"strings"
)

// localeRecs are a locale's records in the engine's locale tables and what
// else its formatters take from its data index.
type localeRecs struct {
	number, currency, unit, tz string
	date                       *dateRecord
	dateKey                    string // the date record's items, for comparing
	numbering                  string // the default numbering system
	cardinal                   int    // the cardinal plural rule set
}

func (r *localeRecs) equal(o *localeRecs) bool {
	return r.number == o.number && r.currency == o.currency && r.unit == o.unit && r.tz == o.tz &&
		r.dateKey == o.dateKey && r.numbering == o.numbering && r.cardinal == o.cardinal
}

// localeData is the data of every data index: the data locales', then the
// regional locales' with data of their own.
type localeData struct {
	tags   []string // BCP 47
	parent []int    // the data index a regional locale's deltas apply to, -1 for a data locale
	recs   []*localeRecs
	// index maps the CLDR id of each data locale and regional locale to
	// its data index.
	index map[string]int
}

// nData returns the number of data indexes.
func (d *localeData) nData() int { return len(d.tags) }

// subs returns the data indexes of the regional locales.
func (d *localeData) subs() []int {
	var s []int
	for i := len(dataLocales); i < len(d.tags); i++ {
		s = append(s, i)
	}
	return s
}

// base returns the data locale a data index descends from.
func (d *localeData) base(i int) int {
	for d.parent[i] >= 0 {
		i = d.parent[i]
	}
	return i
}

// localeInputs are what the records of every locale are built from.
type localeInputs struct {
	plurals    *pluralData
	systems    []string // the numeric numbering systems, sorted
	codes      []string // the currency codes
	unitIDs    map[string]string
	units      []string
	dayRules   map[string]string
	numberings []string // the numeric numbering systems but latn
	tz         *tzTables
	defNu      map[string]string // CLDR id → its own defaultNumberingSystem
	currencyOf map[string]string // region → the currency in use
}

// prepareLocales builds the records of the data locales and of the
// regional locales of their languages (g.locales).
func (g *gen) prepareLocales(files map[string][]byte) {
	g.files = files
	// ICU's departures from CLDR, before any record reads the data.
	for _, p := range append(icuDatePatches, icuNumberPatches...) {
		f := g.ldml.file(p.id)
		path := normPath(p.path)
		if v, ok := f.values[path]; ok == p.add || v == p.value {
			log.Fatalf("ICU patch of %s %s: CLDR has %q", p.id, p.path, v)
		}
		if p.value == "" {
			delete(f.values, path)
			continue
		}
		f.values[path] = p.value
	}
	in := &localeInputs{plurals: g.loadPlurals(files)}
	eachElement(files["common/supplemental/numberingSystems.xml"], "numberingSystem", func(attr func(string) string) {
		if attr("type") == "numeric" {
			in.systems = append(in.systems, attr("id"))
			if attr("id") != "latn" {
				in.numberings = append(in.numberings, attr("id"))
			}
		}
	})
	slices.Sort(in.systems)
	slices.Sort(in.numberings)
	in.dayRules = g.dayPeriodRules(files["common/supplemental/dayPeriods.xml"])
	in.tz = g.tzPrepare(files)
	in.defNu = g.ownNumbering()
	in.currencyOf = regionCurrencies(files["common/supplemental/supplementalData.xml"])
	regional := g.regionalIDs()
	tags := slices.Clone(dataLocales)
	for _, id := range regional {
		tags = append(tags, bcp47(id))
	}
	in.codes = g.currencyCodes(tags)
	in.unitIDs, in.units = g.unitIDs()
	g.in = in

	d := &localeData{index: map[string]int{}}
	for i, tag := range dataLocales {
		d.tags = append(d.tags, tag)
		d.parent = append(d.parent, -1)
		d.recs = append(d.recs, g.localeRecords(tag, i))
		d.index[cldrID(tag)] = i
	}
	// The regional locales' time zone names refer to the data locales'
	// strings only.
	tzFrozen = true
	shared := 0
	for _, id := range regional {
		anc := -1
		for p := g.ldml.parent(id); anc < 0; p = g.ldml.parent(p) {
			if i, ok := d.index[p]; ok {
				anc = i
			}
		}
		rec := g.localeRecords(bcp47(id), d.base(anc))
		if rec.numbering != d.recs[anc].numbering {
			log.Fatalf("%s: default numbering system %s, its parent's %s", id, rec.numbering, d.recs[anc].numbering)
		}
		if rec.equal(d.recs[anc]) {
			d.index[id] = anc
			shared++
			continue
		}
		d.index[id] = len(d.tags)
		d.tags = append(d.tags, bcp47(id))
		d.parent = append(d.parent, anc)
		d.recs = append(d.recs, rec)
	}
	log.Printf("regional locales: %d, %d with data of their own, %d sharing their parent's", len(regional), len(d.tags)-len(dataLocales), shared)
	g.locales = d
}

// bcp47 converts a CLDR id to a BCP 47 tag ("zh_Hant_HK" → "zh-Hant-HK").
func bcp47(id string) string { return strings.ReplaceAll(id, "_", "-") }

// regionalIDs returns the CLDR ids of common/main that descend from a data
// locale by CLDR's parent locales, the data locales and en_US_POSIX (a
// variant, en-US-u-va-posix) apart, parents before their children.
func (g *gen) regionalIDs() []string {
	isData := map[string]bool{}
	for _, tag := range dataLocales {
		isData[cldrID(tag)] = true
	}
	depth := map[string]int{}
	var ids []string
	for _, id := range g.main {
		if isData[id] || strings.Contains(id, "_POSIX") {
			continue
		}
		n := 1
		for p := g.ldml.parent(id); p != "" && p != "root"; p = g.ldml.parent(p) {
			if isData[p] {
				depth[id] = n
				ids = append(ids, id)
				break
			}
			n++
		}
	}
	slices.SortFunc(ids, func(a, b string) int {
		if depth[a] != depth[b] {
			return depth[a] - depth[b]
		}
		return strings.Compare(a, b)
	})
	return ids
}

// localeRecords builds a locale's records; base is the data locale whose
// time zone names its own refer to.
func (g *gen) localeRecords(tag string, base int) *localeRecs {
	in := g.in
	r := &localeRecs{
		number:    g.numberRecord(tag, in.plurals, in.systems),
		currency:  g.currencyRecord(tag, in.plurals, in.codes),
		unit:      g.unitRecord(g.files, tag, in.plurals, in.unitIDs, in.units),
		tz:        g.tzLocaleNames(in.tz, tag, base),
		date:      g.dateRecord(tag, in.dayRules, in.numberings),
		numbering: g.numberingOf(cldrID(tag)),
		cardinal:  in.plurals.cardinalOf(cldrID(tag)),
	}
	var k strings.Builder
	for _, it := range r.date.items {
		if it.isRef {
			k.WriteString("r" + it.s + "\x00")
		} else {
			k.WriteString("n" + strconv.Itoa(it.count) + "\x00")
		}
	}
	r.dateKey = k.String()
	return r
}

// delta encodes child as a delta of parent, which the engine's
// intlApplyDelta applies: runs of three uvarints, the bytes of parent
// copied, the bytes of parent skipped and the length of an insertion,
// then the insertion's bytes; the rest of parent follows the last run. The
// runs come from a diff of the records' tokens, cut by split. An unchanged
// record's delta is empty.
func delta(parent, child string, split func(string) []string) string {
	if parent == child {
		return ""
	}
	a, b := split(parent), split(child)
	ops := diffTokens(a, b)
	var out []byte
	keep, drop := 0, 0
	var ins []byte
	flush := func() {
		if drop == 0 && len(ins) == 0 {
			return
		}
		out = binary.AppendUvarint(out, uint64(keep))
		out = binary.AppendUvarint(out, uint64(drop))
		out = binary.AppendUvarint(out, uint64(len(ins)))
		out = append(out, ins...)
		keep, drop, ins = 0, 0, ins[:0]
	}
	x, y := 0, 0
	for _, op := range ops {
		switch op {
		case '=':
			if drop != 0 || len(ins) != 0 {
				flush()
			}
			keep += len(a[x])
			x++
			y++
		case '-':
			drop += len(a[x])
			x++
		case '+':
			ins = append(ins, b[y]...)
			y++
		}
	}
	flush()
	if got := applyDelta(parent, string(out)); got != child {
		log.Fatalf("delta does not rebuild the record (%d bytes, want %d)", len(got), len(child))
	}
	return string(out)
}

// applyDelta is the engine's intlApplyDelta.
func applyDelta(parent, d string) string {
	var b []byte
	p := 0
	for i := 0; i < len(d); {
		var v [3]int
		for k := range v {
			u, n := binary.Uvarint([]byte(d[i:]))
			v[k], i = int(u), i+n
		}
		b = append(b, parent[p:p+v[0]]...)
		p += v[0] + v[1]
		b = append(b, d[i:i+v[2]]...)
		i += v[2]
	}
	return string(append(b, parent[p:]...))
}

// textTokens cuts a record of fields at its separators (bytes below 0x20,
// NUL included), each token ending with its separator.
func textTokens(s string) []string {
	var t []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 {
			t = append(t, s[start:i+1])
			start = i + 1
		}
	}
	if start < len(s) {
		t = append(t, s[start:])
	}
	return t
}

// uvarintTokens cuts a record of uvarints into them.
func uvarintTokens(s string) []string {
	var t []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] < 0x80 {
			t = append(t, s[start:i+1])
			start = i + 1
		}
	}
	if start < len(s) {
		log.Fatal("a record ends inside a uvarint")
	}
	return t
}

// diffTokens returns a shortest edit script from a to b (Myers' O(ND)
// algorithm): '=' for a token both keep, '-' for one of a dropped, '+' for
// one of b inserted.
func diffTokens(a, b []string) []byte {
	n, m := len(a), len(b)
	max := n + m
	off := max + 1
	v := make([]int, 2*max+3)
	// trace[d] holds v[off-d-1 : off+d+2] as it was before step d.
	var trace [][]int
	for d := 0; d <= max; d++ {
		trace = append(trace, slices.Clone(v[off-d-1:off+d+2]))
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || k != d && v[off+k-1] < v[off+k+1] {
				x = v[off+k+1]
			} else {
				x = v[off+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[off+k] = x
			if x >= n && y >= m {
				return backtrack(trace, n, m)
			}
		}
	}
	log.Fatal("diffTokens: no path")
	return nil
}

func backtrack(trace [][]int, n, m int) []byte {
	var ops []byte
	x, y := n, m
	for d := len(trace) - 1; d > 0; d-- {
		w := trace[d] // v[off-d-1 : off+d+2] before step d
		at := func(k int) int { return w[k+d+1] }
		k := x - y
		prev := k - 1
		if k == -d || k != d && at(k-1) < at(k+1) {
			prev = k + 1
		}
		px := at(prev)
		py := px - prev
		// The snake after the move.
		sx := px
		if prev == k-1 {
			sx = px + 1
		}
		for x > sx {
			ops = append(ops, '=')
			x--
			y--
		}
		if prev == k+1 {
			ops = append(ops, '+')
		} else {
			ops = append(ops, '-')
		}
		x, y = px, py
	}
	for x > 0 && y > 0 {
		ops = append(ops, '=')
		x--
		y--
	}
	slices.Reverse(ops)
	return ops
}

// regionCurrencies returns the currency in use in each region, as ICU's
// ucurr_forLocale finds it: the first of the region's currencies in
// supplementalData's currencyData without an end date that is tender.
func regionCurrencies(data []byte) map[string]string {
	out := map[string]string{}
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Strict = false
	region := ""
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
			case "region":
				region = attrOf(t, "iso3166")
			case "currency":
				if region != "" && out[region] == "" && attrOf(t, "to") == "" && attrOf(t, "tender") != "false" {
					out[region] = attrOf(t, "iso4217")
				}
			}
		case xml.EndElement:
			if t.Name.Local == "region" {
				region = ""
			}
		}
	}
}
