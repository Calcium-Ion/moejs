package main

// The locale data of the Intl constructors (number, date and time zone
// formats): the files of common/main flattened to leaf values by path, with
// the inheritance of UTS #35 (part 1, §4.1): a locale's parents up to root
// (supplementalData.xml's parentLocales, else truncation), the inheritance marker "↑↑↑" and the
// aliases (root's <alias source="locale" path="..."/>), which restart the
// lookup at the requesting locale with the rewritten path.

import (
	"bytes"
	"encoding/xml"
	"io"
	"log"
	"slices"
	"strings"
)

// dataLocales are the locales whose number, date and time zone data the
// engine carries, as BCP 47 tags, in the order of the engine's data
// indexes. Each one's data is its CLDR file's, inherited from its parents up
// to root; the locales that descend from them (en_GB, zh_Hant_HK) carry
// theirs as deltas (sublocale.go). Adding a locale is adding it here.
var dataLocales = []string{
	"en", "zh-Hans", "zh-Hant", "ja", "ko", "de", "fr", "es", "pt", "ru", "it",
}

// ldmlFile is one file of common/main flattened: values maps the path of
// every element with text to the text, and of every value attribute to the
// attribute's value (the path then ends with "/@name"); aliases maps the
// path of an element holding an <alias> to the alias's path attribute.
//
// A path is the element names from below <ldml>, '/'-separated, each with
// its distinguishing attributes as [@name='value'], sorted by name:
// "numbers/symbols[@numberSystem='latn']/decimal",
// "dates/calendars/calendar[@type='gregorian']/months/monthContext[@type='format']/monthWidth[@type='wide']/month[@type='1']".
// Elements whose draft attribute is "unconfirmed" or "provisional" are
// left out, as in ICU's data, and so are values "↑↑↑".
type ldmlFile struct {
	values  map[string]string
	aliases map[string]string
}

// ldmlValueAttrs are the attributes that are values rather than part of
// the path (UTS #35's non-distinguishing attributes that the data uses).
var ldmlValueAttrs = map[string]bool{
	"draft": true, "references": true, "validSubLocales": true, "standard": true,
	"numbers": true, "cldrVersion": true, "version": true,
}

// parseLDML flattens a file of common/main.
func parseLDML(data []byte) *ldmlFile {
	f := &ldmlFile{values: map[string]string{}, aliases: map[string]string{}}
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Strict = false
	type frame struct {
		path     string
		text     strings.Builder
		skip     bool
		hasChild bool
	}
	var stack []*frame
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			parent := ""
			skip := false
			if n := len(stack); n > 0 {
				parent, skip = stack[n-1].path, stack[n-1].skip
				stack[n-1].hasChild = true
			}
			switch t.Name.Local {
			case "ldml":
				stack = append(stack, &frame{})
				continue
			case "alias":
				if !skip {
					f.aliases[parent] = normPath(attrOf(t, "path"))
				}
				stack = append(stack, &frame{path: parent, skip: true})
				continue
			}
			var dist []string
			for _, a := range t.Attr {
				if !ldmlValueAttrs[a.Name.Local] && a.Name.Space == "" {
					dist = append(dist, "[@"+a.Name.Local+"='"+a.Value+"']")
				}
			}
			slices.Sort(dist)
			p := t.Name.Local + strings.Join(dist, "")
			if parent != "" {
				p = parent + "/" + p
			}
			if dr := attrOf(t, "draft"); dr == "unconfirmed" || dr == "provisional" {
				skip = true
			}
			if !skip {
				for _, a := range t.Attr {
					if ldmlValueAttrs[a.Name.Local] && a.Name.Local != "draft" && a.Name.Local != "references" {
						f.values[p+"/@"+a.Name.Local] = a.Value
					}
				}
			}
			stack = append(stack, &frame{path: p, skip: skip})
		case xml.CharData:
			if n := len(stack); n > 0 {
				stack[n-1].text.Write(t)
			}
		case xml.EndElement:
			fr := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			// A leaf's text is its value, whitespace included (fr's
			// grouping separator is U+202F).
			if !fr.skip && !fr.hasChild && fr.path != "" && fr.text.String() != "↑↑↑" {
				f.values[fr.path] = fr.text.String()
			}
		}
	}
	return f
}

func attrOf(t xml.StartElement, name string) string {
	for _, a := range t.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// normPath sorts the predicates of every element of a path and normalizes
// their quotes to '.
func normPath(p string) string {
	if p == "" {
		return p
	}
	segs := splitPath(p)
	for i, s := range segs {
		name, preds := splitPreds(s)
		slices.Sort(preds)
		segs[i] = name + strings.Join(preds, "")
	}
	return strings.Join(segs, "/")
}

// splitPath splits a path at the '/' outside predicates.
func splitPath(p string) []string {
	var segs []string
	depth, start := 0, 0
	for i := 0; i < len(p); i++ {
		switch p[i] {
		case '[':
			depth++
		case ']':
			depth--
		case '/':
			if depth == 0 {
				segs = append(segs, p[start:i])
				start = i + 1
			}
		}
	}
	return append(segs, p[start:])
}

// splitPreds splits an element of a path into its name and its predicates,
// each as [@name='value'].
func splitPreds(s string) (string, []string) {
	i := strings.IndexByte(s, '[')
	if i < 0 {
		return s, nil
	}
	name, rest := s[:i], s[i:]
	var preds []string
	for rest != "" {
		end := strings.IndexByte(rest, ']')
		if end < 0 {
			log.Fatalf("bad path element %q", s)
		}
		preds = append(preds, strings.ReplaceAll(rest[:end+1], `"`, "'"))
		rest = rest[end+1:]
	}
	return name, preds
}

// joinRel applies a relative alias path ("../x", "../../y[@type='z']") to
// the path of the element holding the alias.
func joinRel(base, rel string) string {
	segs := splitPath(base)
	for _, s := range splitPath(rel) {
		switch s {
		case "..":
			segs = segs[:len(segs)-1]
		case ".", "":
		default:
			segs = append(segs, s)
		}
	}
	return strings.Join(segs, "/")
}

// ldmlData holds the parsed files of the locales the generator resolves.
type ldmlData struct {
	files   map[string][]byte // common/main/<id>.xml contents by path
	parsed  map[string]*ldmlFile
	parents map[string]string // parentLocales of supplementalData.xml: id → parent id
	// compParents holds the parentLocales of the components (collations,
	// plurals, ...): component → id → parent id.
	compParents map[string]map[string]string
}

func newLDMLData(files map[string][]byte) *ldmlData {
	d := &ldmlData{files: files, parsed: map[string]*ldmlFile{}, parents: map[string]string{}, compParents: map[string]map[string]string{}}
	// The <parentLocales> without a component attribute, and those of the
	// components (segmentations, plurals, collations, ...).
	dec := xml.NewDecoder(bytes.NewReader(files["common/supplemental/supplementalData.xml"]))
	dec.Strict = false
	inMain := false
	var comps []string
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "parentLocales":
				comps = strings.Fields(attrOf(t, "component"))
				inMain = len(comps) == 0
			case "parentLocale":
				for _, l := range strings.Fields(attrOf(t, "locales")) {
					if inMain {
						d.parents[l] = attrOf(t, "parent")
					}
					for _, c := range comps {
						if d.compParents[c] == nil {
							d.compParents[c] = map[string]string{}
						}
						d.compParents[c][l] = attrOf(t, "parent")
					}
				}
			}
		case xml.EndElement:
			if t.Name.Local == "parentLocales" {
				inMain, comps = false, nil
			}
		}
	}
	if d.parents["zh_Hant"] != "root" {
		log.Fatal("parentLocales: zh_Hant's parent is not root")
	}
	return d
}

// cldrID converts a BCP 47 tag to a CLDR file id ("zh-Hant" → "zh_Hant").
func cldrID(tag string) string { return strings.ReplaceAll(tag, "-", "_") }

// file returns the parsed file of a CLDR id, nil when common/main has none.
func (d *ldmlData) file(id string) *ldmlFile {
	if f, ok := d.parsed[id]; ok {
		return f
	}
	var f *ldmlFile
	if b, ok := d.files["common/main/"+id+".xml"]; ok {
		if b == nil {
			log.Fatalf("common/main/%s.xml was not read", id)
		}
		f = parseLDML(b)
	}
	d.parsed[id] = f
	return f
}

// parent returns the parent of a CLDR id: parentLocales', else the id
// truncated, else root ("" for root).
func (d *ldmlData) parent(id string) string {
	if p, ok := d.parents[id]; ok {
		return p
	}
	if id == "root" {
		return ""
	}
	if i := strings.LastIndexByte(id, '_'); i > 0 {
		return id[:i]
	}
	return "root"
}

// compParent returns the parent of a CLDR id for a component's data
// (collations, plurals, ...): the component's parentLocales, else the id
// truncated, else root ("" for root).
func (d *ldmlData) compParent(comp, id string) string {
	if p, ok := d.compParents[comp][id]; ok {
		return p
	}
	if id == "root" {
		return ""
	}
	if i := strings.LastIndexByte(id, '_'); i > 0 {
		return id[:i]
	}
	return "root"
}

// chain returns the files a locale inherits from, the locale's own first
// and root last.
func (d *ldmlData) chain(tag string) []*ldmlFile {
	var c []*ldmlFile
	for id := cldrID(tag); id != ""; id = d.parent(id) {
		if f := d.file(id); f != nil {
			c = append(c, f)
		}
	}
	return c
}

// value returns the resolved value of a path in a locale. A value
// "∅∅∅" (none, which the regional locales use to drop a name their parent
// has, as en_001 does US time zone abbreviations) is no value.
func (d *ldmlData) value(tag, path string) (string, bool) {
	path = normPath(path)
	chain := d.chain(tag)
	for range 32 {
		for _, f := range chain {
			if v, ok := f.values[path]; ok {
				if v == "∅∅∅" {
					return "", false
				}
				return v, true
			}
		}
		p, ok := dealias(chain, path)
		if !ok {
			return "", false
		}
		path = p
	}
	log.Fatalf("alias loop at %s in %s", path, tag)
	return "", false
}

// mustValue is value for data every locale has through root.
func (d *ldmlData) mustValue(tag, path string) string {
	v, ok := d.value(tag, path)
	if !ok {
		log.Fatalf("%s: no value for %s", tag, path)
	}
	return v
}

// dealias rewrites a path through the alias of its longest prefix that has
// one in the chain, the most specific file first.
func dealias(chain []*ldmlFile, path string) (string, bool) {
	segs := splitPath(path)
	for n := len(segs); n > 0; n-- {
		prefix := strings.Join(segs[:n], "/")
		for _, f := range chain {
			if rel, ok := f.aliases[prefix]; ok {
				return normPath(strings.Join(append(splitPath(joinRel(prefix, rel)), segs[n:]...), "/")), true
			}
		}
	}
	return "", false
}

// leaves returns the resolved values of every path below prefix in a
// locale, by the part of the path after prefix and its '/'. The paths are
// those of every file of the chain, including what aliases at, above or
// below prefix make visible there.
func (d *ldmlData) leaves(tag, prefix string) map[string]string {
	prefix = normPath(prefix)
	rels := map[string]bool{}
	d.collect(d.chain(tag), prefix, "", rels, 0)
	out := map[string]string{}
	for rel := range rels {
		if v, ok := d.value(tag, prefix+"/"+rel); ok {
			out[rel] = v
		}
	}
	return out
}

// collect adds to rels the paths below prefix (relative to it, after
// under) in the chain's files and behind the aliases that cover them.
func (d *ldmlData) collect(chain []*ldmlFile, prefix, under string, rels map[string]bool, depth int) {
	if depth > 16 {
		log.Fatalf("alias loop below %s", prefix)
	}
	join := func(rel string) string {
		if under == "" {
			return rel
		}
		return under + "/" + rel
	}
	for _, f := range chain {
		for p := range f.values {
			if rel, ok := strings.CutPrefix(p, prefix+"/"); ok {
				rels[join(rel)] = true
			}
		}
		for p, rel := range f.aliases {
			switch {
			case p == prefix:
				d.collect(chain, normPath(joinRel(p, rel)), under, rels, depth+1)
			case strings.HasPrefix(p, prefix+"/"):
				d.collect(chain, normPath(joinRel(p, rel)), join(strings.TrimPrefix(p, prefix+"/")), rels, depth+1)
			}
		}
	}
	// An alias at or above prefix.
	if p, ok := dealias(chain, prefix); ok {
		d.collect(chain, p, under, rels, depth+1)
	}
}
