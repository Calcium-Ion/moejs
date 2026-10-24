// Command cldr generates engine/intl_locale_tables.go, the CLDR data of the
// engine's Unicode BCP 47 locale identifiers (engine/langtag.go): the alias
// rules of UTS #35 Annex C (languageAlias, scriptAlias, territoryAlias,
// variantAlias and subdivisionAlias of supplementalMetadata.xml), the aliases
// and preferred values of the -u- and -t- keyword values (common/bcp47), the
// likely subtags (likelySubtags.xml) and the available locales (the locales of
// common/main and the defaultContent locales).
//
// The CLDR release is downloaded from unicode.org at generation time only
// (-cache keeps a copy of the zip, which is checked against its published
// SHA-512); the output is deterministic and committed. The version is pinned
// to the CLDR of the ICU in Node 24 (ICU 78, CLDR 48.0).
//
// Usage (from engine/, see the go:generate line in langtag.go):
//
//	go run ../internal/gen/cldr -o intl_locale_tables.go [-cache dir]
package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"encoding/xml"
	"flag"
	"go/format"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

const version = "48.0"

// notice follows the generated-code line of each output: the source and
// license of its data.
const notice = "// The tables hold Unicode CLDR " + version + " data, under the Unicode License v3;\n" +
	"// see THIRD_PARTY_NOTICES.\n\n"

const (
	zipURL    = "https://unicode.org/Public/cldr/48/cldr-common-48.zip"
	zipSHA512 = "07c69a6472b6446f2cf940fb2b22e48718e53ec458dc9733487869a851519f77" +
		"df9f393f797191c04285b2f6532dd1521315bde2b25a237b664da96e152d6b4c"
)

func main() {
	out := flag.String("o", "intl_locale_tables.go", "output file")
	cache := flag.String("cache", "", "directory for the downloaded CLDR zip (reused when present)")
	flag.Parse()

	files := readZip(fetch(*cache))
	var g gen
	g.loadAliases(files["common/supplemental/supplementalMetadata.xml"])
	g.loadKeywords(files)
	g.loadLikely(files["common/supplemental/likelySubtags.xml"])
	g.loadAvailable(files)
	g.ldml = newLDMLData(files)
	g.prepareLocales(files)
	write(*out, g.emit())
	// The tables of the Intl constructors with locale data, next to it: each
	// emitter returns nil until it has something to write.
	dir := filepath.Dir(*out)
	for name, emit := range map[string]func(map[string][]byte) []byte{
		"intl_supp_tables.go":   g.emitSupp,
		"intl_number_tables.go": g.emitNumber,
		"intl_date_tables.go":   g.emitDate,
		"intl_tz_tables.go":     g.emitTZ,
	} {
		if src := emit(files); src != nil {
			write(filepath.Join(dir, name), src)
		}
	}
}

// write formats Go source and writes it to a file.
func write(name string, src []byte) {
	src, err := format.Source(src)
	if err != nil {
		log.Fatalf("%s: %v", name, err)
	}
	if err := os.WriteFile(name, src, 0o644); err != nil {
		log.Fatal(err)
	}
}

// fetch returns the CLDR zip, from the cache directory when it holds a copy.
func fetch(cache string) []byte {
	name := path.Base(zipURL)
	var b []byte
	if cache != "" {
		b, _ = os.ReadFile(filepath.Join(cache, name))
	}
	if b == nil {
		resp, err := http.Get(zipURL)
		if err != nil {
			log.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			log.Fatalf("%s: %s", zipURL, resp.Status)
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
	if sum := sha512.Sum512(b); hex.EncodeToString(sum[:]) != zipSHA512 {
		log.Fatalf("%s: SHA-512 mismatch", name)
	}
	return b
}

// readZip returns the files of common/main, common/collation,
// common/supplemental and common/bcp47 and common/properties/scriptMetadata.txt
// by path.
func readZip(b []byte) map[string][]byte {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		log.Fatal(err)
	}
	files := map[string][]byte{}
	for _, f := range zr.File {
		dir, name := path.Split(f.Name)
		if !strings.HasSuffix(name, ".xml") && f.Name != "common/properties/scriptMetadata.txt" {
			continue
		}
		switch dir {
		case "common/main/", "common/collation/", "common/supplemental/", "common/bcp47/", "common/properties/":
			rc, err := f.Open()
			if err != nil {
				log.Fatal(err)
			}
			if files[f.Name], err = io.ReadAll(rc); err != nil {
				log.Fatal(err)
			}
			rc.Close()
		}
	}
	return files
}

// eachElement calls fn with the attributes of every element of data named
// name.
func eachElement(data []byte, name string, fn func(attr func(string) string)) {
	if data == nil {
		log.Fatalf("missing file for <%s>", name)
	}
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Strict = false
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return
		}
		if err != nil {
			log.Fatal(err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != name {
			continue
		}
		fn(func(key string) string {
			for _, a := range se.Attr {
				if a.Name.Local == key {
					return a.Value
				}
			}
			return ""
		})
	}
}

// langID is a unicode_language_id in canonical case; "und" is "".
type langID struct {
	lang, script, region string
	variants             []string // sorted
}

func isAlpha(s string) bool {
	for i := range len(s) {
		if c := s[i] | 0x20; c < 'a' || c > 'z' {
			return false
		}
	}
	return true
}

func isDigit(s string) bool {
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func isAlnum(s string) bool {
	for i := range len(s) {
		if c := s[i] | 0x20; (c < 'a' || c > 'z') && (s[i] < '0' || s[i] > '9') {
			return false
		}
	}
	return true
}

func isLanguage(s string) bool {
	return (len(s) >= 2 && len(s) <= 3 || len(s) >= 5 && len(s) <= 8) && isAlpha(s)
}
func isScript(s string) bool { return len(s) == 4 && isAlpha(s) }
func isRegion(s string) bool { return len(s) == 2 && isAlpha(s) || len(s) == 3 && isDigit(s) }
func isVariant(s string) bool {
	return isAlnum(s) && (len(s) >= 5 && len(s) <= 8 || len(s) == 4 && isDigit(s[:1]))
}

// isType reports whether s is a -u- type or -t- tvalue: alphanum{3,8}
// separated by '-'.
func isType(s string) bool {
	for sub := range strings.SplitSeq(s, "-") {
		if len(sub) < 3 || len(sub) > 8 || !isAlnum(sub) {
			return false
		}
	}
	return true
}

func title(s string) string { return strings.ToUpper(s[:1]) + strings.ToLower(s[1:]) }

// parseLangID parses a CLDR languageId ('_' or '-' separated); ok is false
// for one that is not a valid unicode_language_id (such as "i_mingo" or the
// three-letter regions of territoryAlias), which Annex C discards.
func parseLangID(s string) (id langID, ok bool) {
	subs := strings.FieldsFunc(s, func(r rune) bool { return r == '_' || r == '-' })
	if len(subs) == 0 || !isLanguage(subs[0]) {
		return id, false
	}
	if l := strings.ToLower(subs[0]); l != "und" {
		id.lang = l
	}
	subs = subs[1:]
	if len(subs) > 0 && isScript(subs[0]) {
		id.script, subs = title(subs[0]), subs[1:]
	}
	if len(subs) > 0 && isRegion(subs[0]) {
		id.region, subs = strings.ToUpper(subs[0]), subs[1:]
	}
	for _, v := range subs {
		if !isVariant(v) {
			return id, false
		}
		v = strings.ToLower(v)
		if slices.Contains(id.variants, v) {
			return id, false
		}
		id.variants = append(id.variants, v)
	}
	slices.Sort(id.variants)
	return id, true
}

// String returns id with "und" for an empty language.
func (id langID) String() string {
	lang := id.lang
	if lang == "" {
		lang = "und"
	}
	parts := []string{lang}
	for _, s := range []string{id.script, id.region} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(append(parts, id.variants...), "-")
}

// positional renders id as "language,script,region,variants" (variants
// '-'-joined), trailing empty fields dropped: the field format of the alias
// rule tables.
func (id langID) positional() string {
	s := strings.Join([]string{id.lang, id.script, id.region, strings.Join(id.variants, "-")}, ",")
	return strings.TrimRight(s, ",")
}

// record is a table record and the key the runtime binary-searches it by;
// records are sorted by key (then by text).
type record struct{ key, text string }

func sortRecords(list []record) {
	slices.SortFunc(list, func(a, b record) int {
		if c := strings.Compare(a.key, b.key); c != 0 {
			return c
		}
		return strings.Compare(a.text, b.text)
	})
}

// lsr is a languageId without variants.
type lsr struct{ lang, script, region string }

// aliasRule is one alias rule of Annex C: a type and its replacement
// languageIds; regions holds all replacement regions of a territoryAlias.
type aliasRule struct {
	from, to langID
	regions  []string
}

type gen struct {
	rules     [4][]record // encoded rules by group: language, script, region, variant
	nRules    int
	discarded int
	subdivs   []record            // "from>to"
	keywords  []record            // "key:from>to"
	likely    map[lsr]lsr         // from → to
	available map[string][]string // language → the rest of each available locale
	main      []string            // the ids of common/main
	defaults  int                 // the number of defaultContent ids
	// droppedDefaults counts the defaultContent ids without an available parent.
	droppedDefaults int
	// scriptless counts the language-region locales added for the
	// language-script-region ones.
	scriptless int
	// defaultContent lists the defaultContent ids, parents first.
	defaultContent []string

	ldml *ldmlData // the locale data of common/main (ldml.go)

	files   map[string][]byte // the CLDR release's files by path
	in      *localeInputs     // what the locale records are built from (sublocale.go)
	locales *localeData       // the records of every data index (sublocale.go)
}

// loadAliases reads the alias rules and the subdivision aliases.
func (g *gen) loadAliases(data []byte) {
	add := func(kind string, attr func(string) string) {
		typ, repl := attr("type"), attr("replacement")
		if kind != "languageAlias" {
			typ = "und_" + typ
		}
		from, ok := parseLangID(typ)
		if !ok {
			g.discarded++
			return
		}
		r := aliasRule{from: from}
		if kind == "territoryAlias" {
			r.regions = strings.Fields(repl)
			for i, reg := range r.regions {
				if !isRegion(reg) {
					log.Fatalf("territoryAlias %s: bad replacement %q", typ, repl)
				}
				r.regions[i] = strings.ToUpper(reg)
			}
			r.to.region = r.regions[0]
		} else {
			if kind != "languageAlias" {
				repl = "und_" + repl
			}
			if r.to, ok = parseLangID(repl); !ok || strings.Contains(repl, " ") {
				log.Fatalf("%s %s: bad replacement %q", kind, typ, repl)
			}
		}
		g.addRule(r)
	}
	for _, kind := range []string{"languageAlias", "scriptAlias", "territoryAlias", "variantAlias"} {
		eachElement(data, kind, func(attr func(string) string) { add(kind, attr) })
	}
	for i := range g.rules {
		sortRecords(g.rules[i])
	}

	eachElement(data, "subdivisionAlias", func(attr func(string) string) {
		from, to := strings.ToLower(attr("type")), strings.Fields(attr("replacement"))[0]
		// UTS #35 Annex C: a region replacement gets "zzzz" to stay a
		// syntactically valid subdivision.
		if isRegion(to) {
			to += "zzzz"
		}
		to = strings.ToLower(to)
		if !isType(from) || !isType(to) {
			log.Fatalf("subdivisionAlias %s: bad replacement %q", from, to)
		}
		g.subdivs = append(g.subdivs, record{from, from + ">" + to})
	})
	sortRecords(g.subdivs)
}

// addRule files r under the first non-empty field of its type: the runtime
// finds the candidate rules of a languageId by its language, script,
// region and each variant.
func (g *gen) addRule(r aliasRule) {
	group, key := 0, r.from.lang
	switch {
	case r.from.lang != "":
		// The runtime searches the language rules by packed language.
		if len(r.from.lang) > 3 {
			log.Fatalf("alias rule for a language of more than three letters: %+v", r)
		}
	case r.from.script != "":
		group, key = 1, r.from.script
	case r.from.region != "":
		group, key = 2, r.from.region
	case len(r.from.variants) > 0:
		group, key = 3, r.from.variants[0]
	default:
		log.Fatalf("alias rule without a type: %+v", r)
	}
	to := r.to.positional()
	if len(r.regions) > 1 {
		to = ",," + strings.Join(r.regions, " ")
	}
	if r.from.positional() == to {
		log.Fatalf("alias rule without effect: %+v", r)
	}
	g.rules[group] = append(g.rules[group], record{key, r.from.positional() + ">" + to})
	g.nRules++
}

// loadKeywords reads the aliases and preferred values of the -u- and -t-
// keyword values from common/bcp47: a deprecated value maps to its preferred
// value, a value alias that is itself a syntactically valid type (such as
// "ethiopic-amete-alem", "yes" or the slash-free IANA time zone names like
// "eire") to its value.
func (g *gen) loadKeywords(files map[string][]byte) {
	type kv struct{ key, from string }
	targets := map[kv]string{}
	names := map[kv]bool{}
	var order []kv
	set := func(k kv, to string) {
		if old, ok := targets[k]; ok {
			if old != to {
				log.Fatalf("keyword %s-%s: %s or %s", k.key, k.from, old, to)
			}
			return
		}
		targets[k] = to
		order = append(order, k)
	}
	var paths []string
	for p := range files {
		if strings.HasPrefix(p, "common/bcp47/") {
			paths = append(paths, p)
		}
	}
	slices.Sort(paths)
	type typ struct{ key, name, alias, preferred string }
	var types []typ
	for _, p := range paths {
		d := xml.NewDecoder(bytes.NewReader(files[p]))
		var key string
		for {
			tok, err := d.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				log.Fatal(err)
			}
			se, ok := tok.(xml.StartElement)
			if !ok {
				continue
			}
			attr := func(name string) string {
				for _, a := range se.Attr {
					if a.Name.Local == name {
						return a.Value
					}
				}
				return ""
			}
			switch se.Name.Local {
			case "key":
				key = attr("name")
			case "type":
				name := attr("name")
				if strings.ToLower(name) != name || !isType(name) {
					continue // an indirect type such as CODEPOINTS
				}
				names[kv{key, name}] = true
				types = append(types, typ{key, name, strings.ToLower(attr("alias")), attr("preferred")})
			}
		}
	}
	for _, t := range types {
		to := t.name
		if t.preferred != "" {
			to = t.preferred
			set(kv{t.key, t.name}, to)
		}
		for a := range strings.FieldsSeq(t.alias) {
			if a == to || !isType(a) || names[kv{t.key, a}] {
				continue
			}
			set(kv{t.key, a}, to)
		}
	}
	for _, k := range order {
		// A preferred value may itself be deprecated.
		to := targets[k]
		for range 8 {
			next, ok := targets[kv{k.key, to}]
			if !ok {
				break
			}
			to = next
		}
		if _, ok := targets[kv{k.key, to}]; ok {
			log.Fatalf("keyword %s-%s: cycle", k.key, k.from)
		}
		g.keywords = append(g.keywords, record{k.key + ":" + k.from, k.key + ":" + k.from + ">" + to})
	}
	sortRecords(g.keywords)
}

// loadLikely reads likelySubtags.xml.
func (g *gen) loadLikely(data []byte) {
	g.likely = map[lsr]lsr{}
	eachElement(data, "likelySubtag", func(attr func(string) string) {
		from, ok1 := parseLangID(attr("from"))
		to, ok2 := parseLangID(attr("to"))
		if !ok1 || !ok2 || len(from.variants)+len(to.variants) > 0 || to.script == "" || to.region == "" {
			log.Fatalf("likelySubtag %s → %s", attr("from"), attr("to"))
		}
		if len(from.lang) > 3 || len(to.lang) > 3 {
			log.Fatalf("likelySubtag %s → %s: language of more than three letters", attr("from"), attr("to"))
		}
		g.likely[lsr{from.lang, from.script, from.region}] = lsr{to.lang, to.script, to.region}
	})
}

// loadAvailable collects the locales of common/main, the defaultContent
// locales whose parent locale is available (CLDR 48 lists some, such as
// ife_TG, for languages without data in common/main), and the
// language-region form of each language-script-region locale, which
// ECMA-402 §9.1 requires along with it ("az-Latn-AZ" brings "az-AZ").
func (g *gen) loadAvailable(files map[string][]byte) {
	g.available = map[string][]string{}
	has := func(lid langID) bool {
		return slices.Contains(g.available[lid.lang], strings.TrimPrefix(lid.String(), lid.lang))
	}
	add := func(id string, needParent bool) {
		// en_US_POSIX is en-US-u-va-posix in BCP 47; without extensions it is
		// en-US, which CLDR has.
		id = strings.TrimSuffix(id, "_POSIX")
		lid, ok := parseLangID(id)
		if !ok || lid.lang == "" {
			log.Fatalf("available locale %q", id)
		}
		// A defaultContent language (nb, of no) has no parent to check.
		if i := strings.LastIndexByte(id, '_'); needParent && i > 0 {
			parent, _ := parseLangID(id[:i])
			if !has(parent) {
				g.droppedDefaults++
				return
			}
		}
		if !has(lid) {
			g.available[lid.lang] = append(g.available[lid.lang], strings.TrimPrefix(lid.String(), lid.lang))
		}
	}
	for p := range files {
		if id := strings.TrimSuffix(path.Base(p), ".xml"); strings.HasPrefix(p, "common/main/") && id != "root" {
			g.main = append(g.main, id)
			add(id, false)
		}
	}
	var defaults []string
	eachElement(files["common/supplemental/supplementalMetadata.xml"], "defaultContent", func(attr func(string) string) {
		defaults = append(defaults, strings.Fields(attr("locales"))...)
	})
	// Parents first.
	slices.SortFunc(defaults, func(a, b string) int { return strings.Count(a, "_") - strings.Count(b, "_") })
	for _, id := range defaults {
		add(id, true)
	}
	g.defaults = len(defaults)
	g.defaultContent = defaults
	var scriptless []langID
	for lang, rest := range g.available {
		for _, r := range rest {
			if lid, _ := parseLangID(lang + r); lid.script != "" && lid.region != "" {
				lid.script = ""
				scriptless = append(scriptless, lid)
			}
		}
	}
	for _, lid := range scriptless {
		if !has(lid) {
			g.available[lid.lang] = append(g.available[lid.lang], strings.TrimPrefix(lid.String(), lid.lang))
			g.scriptless++
		}
	}
	for lang, rest := range g.available {
		if !slices.Contains(rest, "") {
			log.Fatalf("available locales of %s without %s itself", lang, lang)
		}
		slices.Sort(rest)
	}
}
