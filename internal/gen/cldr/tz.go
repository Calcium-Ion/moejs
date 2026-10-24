package main

// The tables of engine/intl_tz_tables.go: the time zone identifiers of
// Intl.DateTimeFormat with their primary identifiers (ECMA-402 §6.5), the
// region and the metazones of each primary zone, the metazones' reference
// zones, and the time zone names of each data locale (CLDR's timeZoneNames:
// the localized GMT format, the metazone and zone names, the exemplar
// cities and the territory names of the generic location format).
//
// The identifiers are the Zone and Link names of the IANA Time Zone
// Database release of Node 24's ICU (tzdata, downloaded like the CLDR zip:
// -cache, SHA-512 checked) without "Factory". The primary identifier of
// each is CLDR's (bcp47/timezone.xml: the type's iana attribute, else its
// first alias; "UTC" for Etc/UTC, Etc/GMT and their links), which the
// generator checks against the algorithm of AvailableNamedTimeZoneIdentifiers
// (zone.tab, backzone). The metazones are those of metaZones.xml with ICU's
// defaults for an open range (from 1970-01-01 00:00, to 9999-12-31 23:59).

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	tzdataVersion = "2026c"
	tzdataURL     = "https://data.iana.org/time-zones/releases/tzdata" + tzdataVersion + ".tar.gz"
	tzdataSHA512  = "e0b4b7044b66fbc27bc21d13d18063abcdf78ab58d5ba5fd64bd1a88d86e9d495" +
		"f45add4d8e65bb6c40249f9c94ca29b72c8ebba8d0e4c468f2965ac77932ef0"
)

// tzSources are the tzdata files with the Zone and Link lines of the default
// build (backzone's zones are links there).
var tzSources = []string{
	"africa", "antarctica", "asia", "australasia", "europe", "northamerica",
	"southamerica", "etcetera", "backward", "factory",
}

// tzdb is the part of tzdata the tables use.
type tzdb struct {
	zones     map[string]bool
	links     map[string]string // Link name → target
	zoneTab   map[string]string // zone.tab: TZ → country code
	backLinks map[string]string // backzone's Link lines: name → target
	// negative holds the zones (of the default build and of backzone) with
	// a negative DST save in the main data form, which Go's TZif files keep
	// and ICU's rearguard data turns into positive DST.
	negative map[string]bool
}

// resolve follows a Link to its Zone.
func (db *tzdb) resolve(name string) string {
	for range 8 {
		t, ok := db.links[name]
		if !ok {
			return name
		}
		name = t
	}
	log.Fatalf("tzdata: link loop at %s", name)
	return ""
}

// fetchTZData returns the tzdata release, from the cache directory of the
// CLDR zip when it holds a copy.
func fetchTZData() []byte {
	cache := flag.Lookup("cache").Value.String()
	name := path.Base(tzdataURL)
	var b []byte
	if cache != "" {
		b, _ = os.ReadFile(filepath.Join(cache, name))
	}
	if b == nil {
		resp, err := http.Get(tzdataURL)
		if err != nil {
			log.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			log.Fatalf("%s: %s", tzdataURL, resp.Status)
		}
		if b, err = io.ReadAll(resp.Body); err != nil {
			log.Fatal(err)
		}
		if cache != "" {
			if err := os.MkdirAll(cache, 0o755); err != nil {
				log.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(cache, name), b, 0o644); err != nil {
				log.Fatal(err)
			}
		}
	}
	if sum := sha512.Sum512(b); hex.EncodeToString(sum[:]) != tzdataSHA512 {
		log.Fatalf("%s: SHA-512 mismatch", name)
	}
	return b
}

// readTZData parses the tzdata release.
func readTZData(b []byte) *tzdb {
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		log.Fatal(err)
	}
	files := map[string][]byte{}
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		if files[h.Name], err = io.ReadAll(tr); err != nil {
			log.Fatal(err)
		}
	}
	if v := strings.TrimSpace(string(files["version"])); v != tzdataVersion {
		log.Fatalf("tzdata version %q", v)
	}
	db := &tzdb{zones: map[string]bool{}, links: map[string]string{}, zoneTab: map[string]string{},
		backLinks: map[string]string{}, negative: map[string]bool{}}
	negRules := map[string]bool{}
	type zoneRules struct{ zone, rules string }
	var uses []zoneRules
	scan := func(name string, fn func(f []string, cont bool)) {
		data, ok := files[name]
		if !ok {
			log.Fatalf("tzdata: no file %s", name)
		}
		for line := range strings.SplitSeq(string(data), "\n") {
			if name == "backzone" {
				line = strings.TrimPrefix(line, "#PACKRATLIST zone.tab ")
			}
			cont := line != "" && (line[0] == ' ' || line[0] == '\t')
			if i := strings.IndexByte(line, '#'); i >= 0 {
				line = line[:i]
			}
			if f := strings.Fields(line); len(f) > 0 {
				fn(f, cont)
			}
		}
	}
	// zoneLines collects the RULES field of every Zone line and continuation.
	zoneLines := func(name string, main bool) {
		zone := ""
		scan(name, func(f []string, cont bool) {
			switch {
			case f[0] == "Rule" && !cont:
				if strings.HasPrefix(f[8], "-") {
					negRules[f[1]] = true
				}
			case f[0] == "Zone" && !cont:
				zone = f[1]
				if main {
					db.zones[zone] = true
				}
				uses = append(uses, zoneRules{zone, f[3]})
			case f[0] == "Link" && !cont:
				if main {
					db.links[f[2]] = f[1]
				} else {
					db.backLinks[f[2]] = f[1]
				}
			case cont && zone != "":
				uses = append(uses, zoneRules{zone, f[1]})
			}
		})
	}
	for _, name := range tzSources {
		zoneLines(name, true)
	}
	zoneLines("backzone", false)
	for _, u := range uses {
		if negRules[u.rules] || strings.HasPrefix(u.rules, "-") && len(u.rules) > 1 {
			db.negative[u.zone] = true
		}
	}
	scan("zone.tab", func(f []string, cont bool) { db.zoneTab[f[2]] = f[0] })
	for l := range db.links {
		if db.zones[l] {
			log.Fatalf("tzdata: %s is a Zone and a Link", l)
		}
	}
	return db
}

// cldrTZ is a type of CLDR's bcp47/timezone.xml.
type cldrTZ struct {
	name    string   // the BCP 47 type ("usnyc")
	aliases []string // the first is the CLDR canonical identifier
	iana    string   // the iana attribute
}

// tzZone is a primary time zone.
type tzZone struct {
	id      string // the primary identifier
	cldr    string // CLDR's canonical identifier (the names' key)
	region  string // "001" for none
	primary bool   // the zone of its region in the generic location format
	uses    []mzUse
}

// mzUse is a usesMetazone of metaZones.xml in minutes since the epoch.
type mzUse struct {
	mz       int
	from, to int64
}

// The ICU defaults for an open range of usesMetazone.
var (
	mzDefaultFrom = mzMinutes("1970-01-01 00:00")
	mzDefaultTo   = mzMinutes("9999-12-31 23:59")
)

func mzMinutes(s string) int64 {
	t, err := time.Parse("2006-01-02 15:04", s)
	if err != nil {
		log.Fatalf("metaZones.xml: date %q", s)
	}
	return t.Unix() / 60
}

// tzTables is what emitTZ writes.
type tzTables struct {
	ids     []string       // the identifiers, sorted by their ASCII lowercase
	index   map[string]int // identifier → index
	primary []int          // the index of each identifier's primary
	target  []int          // the index of the Zone each identifier links to
	zones   map[int]*tzZone
	mzs     []string      // metazone ids, sorted
	golden  [][][2]string // per metazone: (territory, zone) with 001 first
	regions []string      // the regions with zones, sorted
	byCLDR  map[string]*tzZone
	// needCity and needRegion tell which exemplar cities and territory
	// names the generic location and partial location formats use.
	needCity   map[*tzZone]bool
	needRegion map[string]bool
	db         *tzdb
}

// tzICUDST lists the spans (UTC, "" for open) in which ICU's DST flag is not
// that of tzdata's main form, which Go's TZif files keep, with ICU's standard
// offset there: in a span ICU has daylight time exactly when the offset is
// not that one. For the zones with a negative DST save (tzdb.negative) ICU's
// data is built from the rearguard form, which has daylight time where the
// main form has standard time and the reverse (ziguard.awk of tzdata 2026c,
// checked with zdump); ICU's own 2026c data (zoneinfo64 of icu-data's
// tzdata/icunew/2026c, which Node 24.22 carries) keeps British Columbia and
// Alberta in daylight time (PDT, MDT) where tzdata has their new standard
// time (-07 MST, -06 CST). The standard offsets keep the flags right for a
// zoneinfo of another release (Go's embedded copy, older system data). The
// generator fails when the zones with a negative save change, so that a new
// release gets reviewed.
var tzICUDST = map[string][]tzSpan{
	"Africa/Casablanca": {{"2018-10-28 02:00", "", "0:00"}},
	"Africa/El_Aaiun":   {{"2018-10-28 02:00", "", "0:00"}},
	"Africa/Windhoek":   {{"1994-03-20 22:00", "2017-10-23 22:00", "1:00"}},
	"Europe/Dublin":     {{"1971-10-31 02:00", "", "0:00"}},
	"Europe/Prague":     {{"1946-12-01 02:00", "1947-02-23 02:00", "0:00"}},
	"America/Vancouver": {{"2026-11-01 09:00", "", "-8:00"}},
	"America/Edmonton":  {{"2026-11-01 08:00", "", "-7:00"}},
}

// tzSpan is a span of tzICUDST: from, to and ICU's standard offset.
type tzSpan struct{ from, to, std string }

// tzICUMetazones are the metazone uses ICU's 2026c data (metaZones of
// icu-data's tzdata/icunew/2026c) adds to CLDR 48's metaZones.xml for
// tzdata 2026c: Morocco's permanent UTC from 2026-09-20.
var tzICUMetazones = map[string][][3]string{
	"Africa/Casablanca": {{"Europe_Western", "2026-09-20 01:00", ""}},
	"Africa/El_Aaiun":   {{"Europe_Western", "2026-09-20 01:00", ""}},
}

// tzPrepare reads tzdata and computes the identifiers, zones and
// metazones the time zone names of every locale refer to.
func (g *gen) tzPrepare(files map[string][]byte) *tzTables {
	db := readTZData(fetchTZData())
	for z := range db.negative {
		if tzICUDST[z] == nil {
			log.Fatalf("tzdata %s: %s has a negative DST save; review tzICUDST", tzdataVersion, z)
		}
	}
	t := g.tzIdentifiers(db, files["common/bcp47/timezone.xml"])
	g.tzMetazones(t, files["common/supplemental/metaZones.xml"])
	g.tzNeeds(t)
	t.db = db
	return t
}

func (g *gen) emitTZ(files map[string][]byte) []byte {
	t := g.in.tz
	db := t.db

	var b bytes.Buffer
	p := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	p("// Code generated by internal/gen/cldr from CLDR %s and tzdata %s; DO NOT EDIT.\n\n", version, tzdataVersion)
	p("// The tables hold Unicode CLDR %s data, under the Unicode License v3, and\n", version)
	p("// IANA tz database %s data, in the public domain; see THIRD_PARTY_NOTICES.\n\n", tzdataVersion)
	p("package engine\n\n")
	p("// tzdataVersion is the IANA Time Zone Database release of the identifiers.\n")
	p("const tzdataVersion = %q\n\n", tzdataVersion)

	p("// intlTZNames lists the available named time zone identifiers (%d: the\n", len(t.ids))
	p("// Zone and Link names of tzdata %s but \"Factory\"), each followed by a\n", tzdataVersion)
	p("// ';', sorted by their ASCII lowercase.\n")
	p("const intlTZNames = %s\n\n", quote(strings.Join(t.ids, ";")+";"))
	p("// intlTZCount is the number of identifiers.\n")
	p("const intlTZCount = %d\n\n", len(t.ids))

	var links []byte
	for i := range t.ids {
		links = binary.BigEndian.AppendUint16(links, uint16(t.primary[i]))
		links = binary.BigEndian.AppendUint16(links, uint16(t.target[i]))
	}
	p("// intlTZLinks holds two big-endian uint16 per identifier of intlTZNames:\n")
	p("// the index of its primary identifier and the index of the Zone it is a\n")
	p("// Link to (its own for a Zone).\n")
	p("const intlTZLinks = %s\n\n", quote(string(links)))

	zones, nprim := g.tzZoneRecords(t, db)
	p("// intlTZZones holds a record per identifier of intlTZNames, empty but for\n")
	p("// the %d primary ones: its length (a byte), the region (two letters,\n", nprim)
	p("// \"00\" for none), flags ('@' | tzPrimary: the zone of its region in the\n")
	p("// generic location format, | tzCLDR: CLDR's identifier differs and\n")
	p("// follows, ';'-terminated, | tzSpans: a byte count and the spans, each\n")
	p("// a varint start and a uvarint length in minutes since 1970 (0: open) and\n")
	p("// ICU's standard offset in minutes as a varint: there ICU has daylight\n")
	p("// time when the offset is another), then\n")
	p("// each metazone the zone uses: uvarint(index<<2 | from<<1 | to) and the\n")
	p("// bounds it gives as uvarints in minutes since 1970 (the defaults:\n")
	p("// 1970-01-01 00:00 and 9999-12-31 23:59).\n")
	p("const intlTZZones = %s\n\n", quote(zones))

	p("// intlTZMetazoneCount is the number of CLDR's metazones.\n")
	p("const intlTZMetazoneCount = %d\n\n", len(t.mzs))
	p("// intlTZMetazoneGMT is the index of the metazone GMT.\n")
	p("const intlTZMetazoneGMT = %d\n\n", slices.Index(t.mzs, "GMT"))

	p("// intlTZGolden holds the reference zones of each metazone (mapZone): per\n")
	p("// metazone a byte count, then per zone the territory (two letters, \"00\"\n")
	p("// for 001, first) and the zone's identifier index as a big-endian uint16.\n")
	p("const intlTZGolden = %s\n\n", quote(g.tzGolden(t)))

	p("// intlTZRegions lists the regions with zones, two letters each, sorted:\n")
	p("// the territory names of intlTZLocaleNames follow its order.\n")
	p("const intlTZRegions = %s\n\n", quote(strings.Join(t.regions, "")))

	p("// intlTZLocaleNames holds the time zone names of each data locale\n")
	p("// (intlDataLocales), its fields NUL-terminated: the localized GMT format\n")
	p("// (prefix, suffix, the offset patterns +H +H:mm +H:mm:ss and\n")
	p("// their negative forms), the region and fallback formats and the region\n")
	p("// of the locale's likely subtags; per metazone a byte 0x80 | the names that\n")
	p("// follow (bit 0 long generic, 1 long standard, 2 long daylight, 3 short\n")
	p("// generic, 4 short standard, 5 short daylight); the zone records, each its\n")
	p("// identifier index as two bytes 0x80|high7 0x80|low7, a byte 0x80 | the\n")
	p("// fields that follow (bit 6: the exemplar city, when it is not the\n")
	p("// identifier's last part, before the names), ended by a NUL; and the\n")
	p("// territory name of each region of intlTZRegions (empty when unused). A\n")
	p("// name may be a byte 0x01, the length of a prefix and of a suffix it\n")
	p("// shares with the record's first name (each plus one, a byte), and the\n")
	p("// part between them, or a reference to an earlier locale's same field:\n")
	p("// 0x02, 0x80 | the data index, the offset as three bytes 0x80 | 7 bits.\n")
	p("var intlTZLocaleNames = [intlDataLocaleCount]string{\n")
	total := 0
	d := g.locales
	for i, tag := range dataLocales {
		s := d.recs[i].tz
		total += len(s)
		log.Printf("tz names %s: %d bytes", tag, len(s))
		p("\t// %s\n\t%s,\n", tag, quote(s))
	}
	p("}\n\n")
	p("// intlTZSubNames holds the time zone names of each regional locale with\n")
	p("// data of its own (intlSubLocales) as a delta of its parent's record\n")
	p("// (intlApplyDelta); its references are to the data locales' records.\n")
	p("var intlTZSubNames = [intlSubLocaleCount]string{\n")
	sub := 0
	for _, i := range d.subs() {
		s := delta(d.recs[d.parent[i]].tz, d.recs[i].tz, textTokens)
		sub += len(s)
		p("\t// %s\n\t%s,\n", d.tags[i], quote(s))
	}
	p("}\n")
	log.Printf("tz names of the regional locales: %d bytes", sub)
	log.Printf("tz: %d identifiers (%d bytes), %d primary, %d metazones; zones %d bytes, golden %d; names %d bytes",
		len(t.ids), len(strings.Join(t.ids, ";"))+1, nprim, len(t.mzs), len(zones), len(g.tzGolden(t)), total)
	return b.Bytes()
}

// tzIdentifiers computes the identifiers, their primaries and the zones.
func (g *gen) tzIdentifiers(db *tzdb, data []byte) *tzTables {
	cl := map[string]*cldrTZ{}
	eachElement(data, "type", func(attr func(string) string) {
		al := strings.Fields(attr("alias"))
		if len(al) == 0 {
			return // a deprecated type, its aliases are its preferred type's
		}
		c := &cldrTZ{name: attr("name"), aliases: al, iana: attr("iana")}
		for _, a := range al {
			if cl[a] != nil {
				log.Fatalf("timezone.xml: %s in %s and %s", a, cl[a].name, c.name)
			}
			cl[a] = c
		}
	})
	isUTC := func(id string) bool { return id == "Etc/UTC" || id == "Etc/GMT" || id == "GMT" }
	var names []string
	for z := range db.zones {
		if z != "Factory" {
			names = append(names, z)
		}
	}
	for l := range db.links {
		names = append(names, l)
	}
	lower := func(s string) string { return strings.ToLower(s) }
	slices.SortFunc(names, func(a, b string) int { return strings.Compare(lower(a), lower(b)) })
	t := &tzTables{ids: names, index: map[string]int{}, zones: map[int]*tzZone{}, byCLDR: map[string]*tzZone{}}
	for i, n := range names {
		if i > 0 && lower(names[i-1]) == lower(n) {
			log.Fatalf("tzdata: %s and %s differ only in case", names[i-1], n)
		}
		if strings.ContainsAny(n, ";\x00") || len(n) > 64 {
			log.Fatalf("tzdata: name %q", n)
		}
		t.index[n] = i
	}
	// The primary of each name by CLDR, checked against ECMA-402's
	// AvailableNamedTimeZoneIdentifiers.
	countryLines := map[string][]string{}
	for z, cc := range db.zoneTab {
		countryLines[cc] = append(countryLines[cc], z)
	}
	// The country of a name: zone.tab's, else that of an alias of its CLDR
	// type in zone.tab (a Link names the place of its type), else the
	// type's (the two letters of a five-letter BCP 47 type).
	country := func(id string) string {
		if cc, ok := db.zoneTab[id]; ok {
			return cc
		}
		c := cl[id]
		if c == nil {
			return ""
		}
		for _, a := range c.aliases {
			if cc, ok := db.zoneTab[a]; ok {
				return cc
			}
		}
		if len(c.name) == 5 {
			return strings.ToUpper(c.name[:2])
		}
		return ""
	}
	specPrimary := func(id string) string {
		p := id
		if _, link := db.links[id]; link {
			if _, inTab := db.zoneTab[id]; !inTab {
				zone := db.resolve(id)
				switch ic := country(id); {
				case strings.HasPrefix(zone, "Etc/"):
					p = zone
				case ic != "" && ic == country(zone):
					p = zone
				case len(countryLines[ic]) == 1:
					p = countryLines[ic][0]
				default:
					p = db.backLinks[id]
					if p == "" {
						log.Fatalf("AvailableNamedTimeZoneIdentifiers: no backzone link for %s", id)
					}
				}
			}
		}
		if isUTC(p) {
			p = "UTC"
		}
		return p
	}
	t.primary = make([]int, len(names))
	t.target = make([]int, len(names))
	for i, n := range names {
		c := cl[n]
		if c == nil {
			log.Fatalf("timezone.xml has no type for %s", n)
		}
		p := c.iana
		if p == "" {
			p = c.aliases[0]
		}
		if isUTC(p) {
			p = "UTC"
		}
		if sp := specPrimary(n); sp != p {
			log.Fatalf("%s: CLDR's primary %s, AvailableNamedTimeZoneIdentifiers' %s", n, p, sp)
		}
		pi, ok := t.index[p]
		if !ok {
			log.Fatalf("%s: primary %s is no tzdata name", n, p)
		}
		t.primary[i] = pi
		t.target[i] = t.index[db.resolve(n)]
		if db.resolve(n) == "Factory" {
			log.Fatalf("%s links to Factory", n)
		}
	}
	for i, n := range names {
		if t.primary[i] != i {
			if t.primary[t.primary[i]] != t.primary[i] {
				log.Fatalf("%s: the primary %s is not primary", n, names[t.primary[i]])
			}
			continue
		}
		c := cl[n]
		if n == "UTC" {
			c = cl["Etc/UTC"]
		}
		z := &tzZone{id: n, cldr: c.aliases[0], region: country(n)}
		if z.region == "" || strings.HasPrefix(n, "Etc/") || n == "UTC" {
			z.region = "001"
		}
		t.zones[i] = z
		if t.byCLDR[z.cldr] != nil {
			log.Fatalf("CLDR id %s for %s and %s", z.cldr, t.byCLDR[z.cldr].id, n)
		}
		t.byCLDR[z.cldr] = z
	}
	// The CLDR ids of the deprecated types and other aliases resolve to
	// their zone for metaZones.xml and the locale data.
	for a, c := range cl {
		if _, ok := t.index[a]; !ok {
			continue
		}
		if z := t.zones[t.primary[t.index[a]]]; z != nil && t.byCLDR[c.aliases[0]] == nil {
			t.byCLDR[c.aliases[0]] = z
		}
	}
	regions := map[string]bool{}
	for _, z := range t.zones {
		if z.region != "001" {
			regions[z.region] = true
		}
	}
	t.regions = sortedKeys(regions)
	return t
}

// tzMetazones reads metaZones.xml: the metazones of each zone, the reference
// zones of each metazone and the primary zones of the regions with several.
func (g *gen) tzMetazones(t *tzTables, data []byte) {
	type use struct {
		zone, mz, from, to string
	}
	var uses []use
	var mapZones [][3]string // metazone, territory, zone
	primaryZones := map[string]string{}
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Strict = false
	zone, pzRegion := "", ""
	inPrimary := false
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		switch tk := tok.(type) {
		case xml.StartElement:
			a := func(n string) string { return attrOf(tk, n) }
			switch tk.Name.Local {
			case "timezone":
				zone = a("type")
			case "usesMetazone":
				uses = append(uses, use{zone, a("mzone"), a("from"), a("to")})
			case "mapZone":
				mapZones = append(mapZones, [3]string{a("other"), a("territory"), a("type")})
			case "primaryZone":
				inPrimary, pzRegion = true, a("iso3166")
			}
		case xml.CharData:
			if inPrimary {
				primaryZones[pzRegion] += string(tk)
			}
		case xml.EndElement:
			if tk.Name.Local == "primaryZone" {
				inPrimary = false
			}
		}
	}
	mzSet := map[string]bool{}
	for zone, list := range tzICUMetazones {
		for _, u := range list {
			uses = append(uses, use{zone, u[0], u[1], u[2]})
		}
	}
	slices.SortStableFunc(uses, func(a, b use) int { return strings.Compare(a.zone, b.zone) })
	for _, u := range uses {
		mzSet[u.mz] = true
	}
	for _, m := range mapZones {
		mzSet[m[0]] = true
	}
	t.mzs = sortedKeys(mzSet)
	mzIndex := map[string]int{}
	for i, m := range t.mzs {
		mzIndex[m] = i
	}
	for _, u := range uses {
		z := t.byCLDR[u.zone]
		if z == nil {
			log.Printf("metaZones.xml: zone %s is no identifier; skipped", u.zone)
			continue
		}
		if z.cldr != u.zone {
			// Etc/GMT: its identifiers are UTC's, which has Etc/UTC's names.
			log.Printf("metaZones.xml: %s is not CLDR's id of %s; skipped", u.zone, z.id)
			continue
		}
		r := mzUse{mz: mzIndex[u.mz], from: mzDefaultFrom, to: mzDefaultTo}
		if u.from != "" {
			r.from = mzMinutes(u.from)
		}
		if u.to != "" {
			r.to = mzMinutes(u.to)
		}
		z.uses = append(z.uses, r)
	}
	for _, z := range t.zones {
		slices.SortFunc(z.uses, func(a, b mzUse) int { return int(a.from - b.from) })
	}
	t.golden = make([][][2]string, len(t.mzs))
	for _, m := range mapZones {
		z := t.byCLDR[m[2]]
		if z == nil {
			log.Fatalf("mapZone %s %s: no zone %s", m[0], m[1], m[2])
		}
		i := mzIndex[m[0]]
		if m[1] == "001" {
			t.golden[i] = slices.Insert(t.golden[i], 0, [2]string{m[1], z.id})
		} else {
			t.golden[i] = append(t.golden[i], [2]string{m[1], z.id})
		}
	}
	for i, gz := range t.golden {
		if len(gz) == 0 || gz[0][0] != "001" {
			log.Fatalf("metazone %s without a 001 zone", t.mzs[i])
		}
	}
	// A zone is its region's in the generic location format when it is the
	// only one there or the region's primaryZone (ICU's getCanonicalCountry).
	count := map[string]int{}
	for _, z := range t.zones {
		count[z.region]++
	}
	for _, z := range t.zones {
		z.primary = z.region != "001" && (count[z.region] == 1 || primaryZones[z.region] == z.cldr)
	}
	for r, id := range primaryZones {
		if z := t.byCLDR[id]; z == nil || z.region != r {
			log.Fatalf("primaryZone %s %s", r, id)
		}
	}
}

// Flags of an intlTZZones record.
const (
	tzPrimary = 1 << iota // the zone of its region in the generic location format
	tzCLDR                // CLDR's identifier differs and follows
	tzSpans               // spans with the DST flag reversed follow
)

// tzZoneRecords encodes intlTZZones.
func (g *gen) tzZoneRecords(t *tzTables, db *tzdb) (string, int) {
	var out []byte
	n := 0
	for i, id := range t.ids {
		z := t.zones[i]
		if z == nil {
			out = append(out, 0)
			continue
		}
		n++
		var r []byte
		if z.region == "001" {
			r = append(r, "00"...)
		} else {
			r = append(r, z.region...)
		}
		flags := byte('@')
		if z.primary {
			flags |= tzPrimary
		}
		if z.cldr != id {
			flags |= tzCLDR
		}
		// A primary Link (Europe/Bratislava) has its Zone's rules.
		spans := tzICUDST[id]
		if spans == nil {
			spans = tzICUDST[t.ids[t.target[i]]]
		}
		if spans != nil {
			flags |= tzSpans
		}
		r = append(r, flags)
		if z.cldr != id {
			r = append(r, z.cldr+";"...)
		}
		if spans != nil {
			r = append(r, byte(len(spans)))
			for _, s := range spans {
				from := mzMinutes(s.from)
				r = binary.AppendVarint(r, from)
				var n uint64
				if s.to != "" {
					n = uint64(mzMinutes(s.to) - from)
				}
				r = binary.AppendUvarint(r, n)
				h, m, ok := strings.Cut(strings.TrimPrefix(s.std, "-"), ":")
				hh, mm := atoi(h), atoi(m)
				if !ok || hh < 0 || mm < 0 {
					log.Fatalf("tzICUDST %s: offset %q", id, s.std)
				}
				std := int64(hh*60 + mm)
				if strings.HasPrefix(s.std, "-") {
					std = -std
				}
				r = binary.AppendVarint(r, std)
			}
		}
		for _, u := range z.uses {
			h := uint64(u.mz) << 2
			if u.from != mzDefaultFrom {
				h |= 2
			}
			if u.to != mzDefaultTo {
				h |= 1
			}
			r = binary.AppendUvarint(r, h)
			if u.from != mzDefaultFrom {
				r = binary.AppendUvarint(r, uint64(u.from))
			}
			if u.to != mzDefaultTo {
				r = binary.AppendUvarint(r, uint64(u.to))
			}
		}
		if len(r) > 255 {
			log.Fatalf("intlTZZones: the record of %s has %d bytes", id, len(r))
		}
		out = append(out, byte(len(r)))
		out = append(out, r...)
	}
	return string(out), n
}

// tzGolden encodes intlTZGolden.
func (g *gen) tzGolden(t *tzTables) string {
	var out []byte
	for _, gz := range t.golden {
		out = append(out, byte(len(gz)))
		for _, e := range gz {
			if e[0] == "001" {
				out = append(out, "00"...)
			} else {
				out = append(out, e[0]...)
			}
			out = binary.BigEndian.AppendUint16(out, uint16(t.index[e[1]]))
		}
	}
	return string(out)
}

// tzReference is ICU's getReferenceZoneID: the reference zone of a metazone
// in a region, else in 001.
func (t *tzTables) tzReference(mz int, region string) string {
	for _, e := range t.golden[mz] {
		if e[0] == region {
			return e[1]
		}
	}
	return t.golden[mz][0][1]
}

// tzNeeds computes the exemplar cities and territory names the generic
// location format (a region's zone: the territory, another zone: its city)
// and the partial location format (the metazone's reference zone in the
// zone's region: the territory, another zone: its city) use.
func (g *gen) tzNeeds(t *tzTables) {
	t.needCity, t.needRegion = map[*tzZone]bool{}, map[string]bool{}
	for _, z := range t.zones {
		if z.region == "001" {
			if len(z.uses) > 0 {
				log.Fatalf("%s: a zone without a region uses metazones", z.id)
			}
			continue
		}
		if z.primary {
			t.needRegion[z.region] = true
		} else {
			t.needCity[z] = true
		}
		for _, u := range z.uses {
			if t.tzReference(u.mz, z.region) == z.id {
				t.needRegion[z.region] = true
			} else {
				t.needCity[z] = true
			}
		}
	}
}

// atoi parses a decimal number, -1 when s is none.
func atoi(s string) int {
	if s == "" || len(s) > 4 {
		return -1
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// tzDefaultCity is ICU's exemplar city of a zone without one in the locale
// data: the identifier's last part with spaces for '_'.
func tzDefaultCity(id string) string {
	if strings.HasPrefix(id, "Etc/") || strings.HasPrefix(id, "SystemV/") || strings.Contains(id, "Riyadh8") {
		return ""
	}
	i := strings.LastIndexByte(id, '/')
	if i <= 0 || i+1 == len(id) {
		return ""
	}
	return strings.ReplaceAll(id[i+1:], "_", " ")
}

// tzNameTypes are the six names of a metazone or zone in their order.
var tzNameTypes = []string{"long/generic", "long/standard", "long/daylight", "short/generic", "short/standard", "short/daylight"}

// tzSeen holds the literal fields of the locales encoded so far (their
// data index and offset) for the references of tzField; once tzFrozen is
// set, the fields of the locales encoded next (the regional ones, whose
// records are deltas) are not added.
var (
	tzSeen   = map[string][2]int{}
	tzFrozen bool
)

// tzField appends a literal field of locale loc: a reference 0x02, 0x80 |
// data index, offset as three bytes 0x80|7 bits when an earlier locale has
// the same field and that is shorter.
func tzField(b []byte, loc int, v string) []byte {
	if r, ok := tzSeen[v]; ok && r[0] != loc && len(v) > 5 {
		b = append(b, 2, 0x80|byte(r[0]), 0x80|byte(r[1]>>14), 0x80|byte(r[1]>>7&0x7f), 0x80|byte(r[1]&0x7f), 0)
		return b
	}
	if _, ok := tzSeen[v]; !ok && v != "" && !tzFrozen {
		tzSeen[v] = [2]int{loc, len(b)}
	}
	b = append(b, v...)
	return append(b, 0)
}

// tzNames appends the fields of a record's names: each the name or, when
// shorter, its affixes shared with the record's first name.
func tzNames(b []byte, loc int, names []string) []byte {
	first := ""
	for _, v := range names {
		if v == "" {
			continue
		}
		if first == "" {
			first = v
			b = tzField(b, loc, v)
			continue
		}
		pre := 0
		for pre < len(v) && pre < len(first) && v[pre] == first[pre] && pre < 254 {
			pre++
		}
		suf := 0
		for suf < len(v)-pre && suf < len(first)-pre && v[len(v)-1-suf] == first[len(first)-1-suf] && suf < 254 {
			suf++
		}
		// Whole UTF-8 sequences only, so the middle is valid text.
		for pre > 0 && pre < len(v) && v[pre]&0xC0 == 0x80 {
			pre--
		}
		for suf > 0 && v[len(v)-suf]&0xC0 == 0x80 {
			suf--
		}
		if pre+suf > 3 {
			b = append(b, 1, byte(pre+1), byte(suf+1))
			b = append(b, v[pre:len(v)-suf]...)
			b = append(b, 0)
		} else {
			b = tzField(b, loc, v)
		}
	}
	return b
}

// tzLocaleNames encodes a locale's record of intlTZLocaleNames, its fields
// encoded as those of the data locale loc.
func (g *gen) tzLocaleNames(t *tzTables, tag string, loc int) string {
	leaves := g.ldml.leaves(tag, "dates/timeZoneNames")
	var b []byte
	field := func(s string) {
		if strings.IndexByte(s, 0) >= 0 || s != "" && s[0] < 0x20 {
			log.Fatalf("%s: name %q", tag, s)
		}
		b = tzField(b, loc, s)
	}
	must := func(k string) string {
		v, ok := leaves[k]
		if !ok {
			log.Fatalf("%s: no timeZoneNames/%s", tag, k)
		}
		return v
	}
	gmt := must("gmtFormat")
	prefix, suffix, ok := strings.Cut(gmt, "{0}")
	if !ok {
		log.Fatalf("%s: gmtFormat %q", tag, gmt)
	}
	field(prefix)
	field(suffix)
	pos, neg, ok := strings.Cut(must("hourFormat"), ";")
	if !ok {
		log.Fatalf("%s: hourFormat %q", tag, must("hourFormat"))
	}
	for _, hm := range []string{pos, neg} {
		field(tzTruncateOffset(tag, hm))
		field(hm)
		field(tzExpandOffset(tag, hm))
	}
	field(must("regionFormat"))
	field(must("fallbackFormat"))
	lid, _ := parseLangID(tag)
	likely, ok := g.likely[lsr{lid.lang, lid.script, ""}]
	if !ok {
		likely, ok = g.likely[lsr{lid.lang, "", ""}]
	}
	if !ok || likely.region == "" {
		log.Fatalf("%s: no likely region", tag)
	}
	field(likely.region)
	mzBytes := len(b)
	for _, mz := range t.mzs {
		var names []string
		mask := byte(0x80)
		for i, typ := range tzNameTypes {
			v := leaves["metazone[@type='"+mz+"']/"+typ]
			if v != "" {
				mask |= 1 << i
			}
			names = append(names, v)
		}
		b = append(b, mask)
		b = tzNames(b, loc, names)
	}
	mzBytes = len(b) - mzBytes
	zoneBytes := len(b)
	for i, id := range t.ids {
		z := t.zones[i]
		if z == nil {
			continue
		}
		key := "zone[@type='" + z.cldr + "']/"
		city := ""
		if t.needCity[z] {
			var ok bool
			if city, ok = leaves[key+"exemplarCity"]; !ok {
				city = tzDefaultCity(z.cldr)
			}
			if city == tzDefaultCity(id) {
				city = ""
			}
		}
		mask := byte(0x80)
		if city != "" {
			mask |= 1 << 6
		}
		var names []string
		for j, typ := range tzNameTypes {
			v := leaves[key+typ]
			if v != "" {
				mask |= 1 << j
			}
			names = append(names, v)
		}
		if mask == 0x80 {
			continue
		}
		b = append(b, 0x80|byte(i>>7), 0x80|byte(i&0x7f), mask)
		if city != "" {
			field(city)
		}
		b = tzNames(b, loc, names)
	}
	b = append(b, 0)
	zoneBytes = len(b) - zoneBytes
	terrBytes := len(b)
	for _, r := range t.regions {
		v := ""
		if t.needRegion[r] {
			var ok bool
			if v, ok = g.ldml.value(tag, "localeDisplayNames/territories/territory[@type='"+r+"']"); !ok {
				log.Fatalf("%s: no territory name for %s", tag, r)
			}
		}
		field(v)
	}
	terrBytes = len(b) - terrBytes
	if !tzFrozen {
		log.Printf("tz names %s: metazones %d, zones %d, territories %d bytes", tag, mzBytes, zoneBytes, terrBytes)
	}
	if len(b) > 65535 {
		log.Fatalf("%s: %d bytes of time zone names do not fit uint16 offsets", tag, len(b))
	}
	return string(b)
}

// tzTruncateOffset is ICU's truncateOffsetPattern: the hour pattern of an
// hour-minute offset pattern ("+HH:mm" → "+HH").
func tzTruncateOffset(tag, hm string) string {
	mm := strings.Index(hm, "mm")
	if mm < 0 {
		log.Fatalf("%s: hourFormat %q without mm", tag, hm)
	}
	if h := strings.LastIndex(hm[:mm], "HH"); h >= 0 {
		return hm[:h+2]
	}
	if h := strings.LastIndexByte(hm[:mm], 'H'); h >= 0 {
		return hm[:h+1]
	}
	log.Fatalf("%s: hourFormat %q without H", tag, hm)
	return ""
}

// tzExpandOffset is ICU's expandOffsetPattern: the hour-minute-second
// pattern of an hour-minute offset pattern ("+HH:mm" → "+HH:mm:ss").
func tzExpandOffset(tag, hm string) string {
	mm := strings.Index(hm, "mm")
	if mm < 0 {
		log.Fatalf("%s: hourFormat %q without mm", tag, hm)
	}
	sep := ""
	if h := strings.LastIndexByte(hm[:mm], 'H'); h >= 0 {
		sep = hm[h+1 : mm]
	}
	return hm[:mm+2] + sep + "ss" + hm[mm+2:]
}
