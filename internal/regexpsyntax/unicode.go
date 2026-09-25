package regexpsyntax

import (
	"encoding/binary"
	"slices"
	"sort"
	"sync"
)

//go:generate go run ../gen/ucd -o unicode_tables.go

// Unicode data of the regexp engine. The tables in
// unicode_tables.go are generated from one pinned UCD version
// (unicodeVersion) and stored as compact varint strings; nothing is decoded
// until a pattern needs it, and a decoded table is shared by the whole
// process. Both regexp paths use these tables, never Go's unicode package
// (which follows the Unicode version of the Go release), so a property escape
// matches the same code points on RE2 and on the backtracking engine.

// ucdTable is one generated range table in decodeRanges format.
type ucdTable struct {
	name string
	data string
}

// ucdAlias maps a property or value alias to its canonical name.
type ucdAlias struct{ alias, name string }

// ucdStringTable is a property of strings: its single code points and its
// multi-code-point sequences.
type ucdStringTable struct {
	name  string
	chars string
	seqs  string
}

// UnicodeVersion is the UCD version of the generated tables.
const UnicodeVersion = unicodeVersion

// DecodeRanges decodes a table in the generator's range encoding, which
// engine/unicode_norm_tables.go shares.
func DecodeRanges(s string) Set { return decodeRanges(s) }

// decodeRanges decodes the generator's range encoding into a sorted rune
// set of inclusive [lo, hi] pairs.
func decodeRanges(s string) Set {
	out := make(Set, 0, len(s))
	next := uint64(0)
	for i := 0; i < len(s); {
		gap, n := binary.Uvarint([]byte(s[i:min(len(s), i+binary.MaxVarintLen32)]))
		i += n
		length, m := binary.Uvarint([]byte(s[i:min(len(s), i+binary.MaxVarintLen32)]))
		i += m
		lo := next + gap
		out = append(out, rune(lo), rune(lo+length))
		next = lo + length + 1
	}
	return slices.Clip(out)
}

// decodeMap decodes the generator's code point mapping encoding into
// parallel sorted key and value slices.
func decodeMap(s string) (keys, vals []rune) {
	next := uint64(0)
	for i := 0; i < len(s); {
		gap, n := binary.Uvarint([]byte(s[i:min(len(s), i+binary.MaxVarintLen32)]))
		i += n
		d, m := binary.Varint([]byte(s[i:min(len(s), i+binary.MaxVarintLen32)]))
		i += m
		k := next + gap
		keys = append(keys, rune(k))
		vals = append(vals, rune(int64(k)+d))
		next = k + 1
	}
	return keys, vals
}

// decodeSequences decodes the generator's sequence list encoding.
func decodeSequences(s string) [][]rune {
	var out [][]rune
	read := func(i int) (uint64, int) {
		v, n := binary.Uvarint([]byte(s[i:min(len(s), i+binary.MaxVarintLen32)]))
		return v, i + n
	}
	for i := 0; i < len(s); {
		var n uint64
		n, i = read(i)
		seq := make([]rune, n)
		for j := range seq {
			var c uint64
			c, i = read(i)
			seq[j] = rune(c)
		}
		out = append(out, seq)
	}
	return out
}

func lookupAlias(tab []ucdAlias, name string) (string, bool) {
	i := sort.Search(len(tab), func(i int) bool { return tab[i].alias >= name })
	if i < len(tab) && tab[i].alias == name {
		return tab[i].name, true
	}
	return "", false
}

func lookupTable(tab []ucdTable, name string) (int, bool) {
	i := sort.Search(len(tab), func(i int) bool { return tab[i].name >= name })
	return i, i < len(tab) && tab[i].name == name
}

// ucdCache holds the decoded tables; each entry is filled once.
var ucdCache struct {
	sync.Mutex
	sets map[string]Set
}

// cachedSet returns the decoded set for key, building it with build on
// first use.
func cachedSet(key string, build func() Set) Set {
	ucdCache.Lock()
	defer ucdCache.Unlock()
	if s, ok := ucdCache.sets[key]; ok {
		return s
	}
	if ucdCache.sets == nil {
		ucdCache.sets = map[string]Set{}
	}
	s := build()
	ucdCache.sets[key] = s
	return s
}

// IsIDStart and IsIDContinue report the ID_Start and ID_Continue properties
// at unicodeVersion. The lexer uses them for non-ASCII identifier characters,
// so identifiers follow the same Unicode version as property escapes.
func IsIDStart(r rune) bool    { return idStartSet().Has(r) }
func IsIDContinue(r rune) bool { return idContinueSet().Has(r) }

var (
	idStartSet    = sync.OnceValue(func() Set { s, _ := binaryPropertySet("ID_Start"); return s })
	idContinueSet = sync.OnceValue(func() Set { s, _ := binaryPropertySet("ID_Continue"); return s })
)

// generalCategorySet returns the set of a General_Category value (any alias).
func generalCategorySet(value string) (Set, bool) {
	short, ok := lookupAlias(ucdGeneralCategoryAliases[:], value)
	if !ok {
		return nil, false
	}
	i, ok := lookupTable(ucdGeneralCategories[:], short)
	if !ok {
		return nil, false
	}
	return cachedSet("gc="+short, func() Set { return decodeRanges(ucdGeneralCategories[i].data) }), true
}

// scriptSet returns the set of a Script value (any alias), or of the
// Script_Extensions value when ext is set.
func scriptSet(value string, ext bool) (Set, bool) {
	short, ok := lookupAlias(ucdScriptAliases[:], value)
	if !ok {
		return nil, false
	}
	i, ok := lookupTable(ucdScripts[:], short)
	if !ok {
		return nil, false
	}
	sc := cachedSet("sc="+short, func() Set { return decodeRanges(ucdScripts[i].data) })
	if !ext {
		return sc, true
	}
	return cachedSet("scx="+short, func() Set {
		// scx(X) = (sc(X) minus every code point with an explicit extension
		// list) plus the explicit ranges that list X.
		var listed, withX Set
		s := ucdScriptExtensions
		read := func(j int) (uint64, int) {
			v, n := binary.Uvarint([]byte(s[j:min(len(s), j+binary.MaxVarintLen32)]))
			return v, j + n
		}
		next := uint64(0)
		for j := 0; j < len(s); {
			var gap, length, count uint64
			gap, j = read(j)
			length, j = read(j)
			count, j = read(j)
			lo := next + gap
			hi := lo + length
			next = hi + 1
			listed = append(listed, rune(lo), rune(hi))
			for range count {
				var idx uint64
				idx, j = read(j)
				if int(idx) == i {
					withX = append(withX, rune(lo), rune(hi))
				}
			}
		}
		return sc.Subtract(listed).Union(withX)
	}), true
}

// binaryPropertySet returns the set of a binary property of table 67 (name
// or alias).
func binaryPropertySet(name string) (Set, bool) {
	canon, ok := lookupAlias(ucdBinaryAliases[:], name)
	if !ok {
		return nil, false
	}
	switch canon {
	case "Any":
		return Set{0, MaxCodePoint}, true
	case "ASCII":
		return Set{0, 0x7F}, true
	case "Assigned":
		cn, _ := generalCategorySet("Cn")
		return cachedSet("Assigned", func() Set { return cn.Complement() }), true
	}
	i, ok := lookupTable(ucdBinaryProperties[:], canon)
	if !ok {
		return nil, false
	}
	return cachedSet(canon, func() Set { return decodeRanges(ucdBinaryProperties[i].data) }), true
}

// stringProperty is a decoded property of strings.
type stringProperty struct {
	chars Set
	seqs  [][]rune // sorted, each of length >= 2
}

var stringPropertyCache struct {
	sync.Mutex
	m map[string]*stringProperty
}

// stringPropertySet returns a property of strings (v flag) by name.
func stringPropertySet(name string) (*stringProperty, bool) {
	var parts []int
	if name == "RGI_Emoji" {
		for i := range ucdStringProperties {
			parts = append(parts, i)
		}
	} else {
		for i := range ucdStringProperties {
			if ucdStringProperties[i].name == name {
				parts = append(parts, i)
			}
		}
	}
	if len(parts) == 0 {
		return nil, false
	}
	stringPropertyCache.Lock()
	defer stringPropertyCache.Unlock()
	if p, ok := stringPropertyCache.m[name]; ok {
		return p, true
	}
	p := &stringProperty{}
	for _, i := range parts {
		p.chars = p.chars.Union(decodeRanges(ucdStringProperties[i].chars))
		p.seqs = append(p.seqs, decodeSequences(ucdStringProperties[i].seqs)...)
	}
	slices.SortFunc(p.seqs, slices.Compare[[]rune])
	p.seqs = slices.CompactFunc(p.seqs, slices.Equal[[]rune])
	if stringPropertyCache.m == nil {
		stringPropertyCache.m = map[string]*stringProperty{}
	}
	stringPropertyCache.m[name] = p
	return p, true
}

// CaseMap is a decoded code point mapping (identity outside keys).
type CaseMap struct {
	fold       bool // simple case folding (u/v) rather than non-u Canonicalize
	keys, vals []rune
	// classes groups the code points that the mapping makes equivalent:
	// member -> index into groups; groups lists members, sorted.
	members []rune // sorted
	groupOf []int32
	groups  [][]rune
	keySet  Set // the code points the mapping changes
}

func (m *CaseMap) lookup(c rune) rune {
	if i, ok := slices.BinarySearch(m.keys, c); ok {
		return m.vals[i]
	}
	return c
}

// buildClasses computes the equivalence classes {c : f(c) = k} of the
// mapping for every k with more than one member.
func (m *CaseMap) buildClasses() {
	byTarget := map[rune][]rune{}
	for i, k := range m.keys {
		byTarget[m.vals[i]] = append(byTarget[m.vals[i]], k)
	}
	targets := make([]rune, 0, len(byTarget))
	for t := range byTarget {
		targets = append(targets, t)
	}
	slices.Sort(targets)
	type member struct {
		c     rune
		group int32
	}
	var ms []member
	for _, t := range targets {
		g := byTarget[t]
		if m.lookup(t) == t {
			g = append(g, t)
		}
		slices.Sort(g)
		if len(g) < 2 {
			continue
		}
		idx := int32(len(m.groups))
		m.groups = append(m.groups, g)
		for _, c := range g {
			ms = append(ms, member{c, idx})
		}
	}
	slices.SortFunc(ms, func(a, b member) int { return int(a.c - b.c) })
	m.members = make([]rune, len(ms))
	m.groupOf = make([]int32, len(ms))
	for i, x := range ms {
		m.members[i] = x.c
		m.groupOf[i] = x.group
	}
}

var (
	simpleFoldOnce, canonNonUOnce sync.Once
	simpleFoldMap, canonNonUMap   CaseMap
)

// simpleFolding returns the simple case folding map (Canonicalize with u/v).
func simpleFolding() *CaseMap {
	simpleFoldOnce.Do(func() {
		simpleFoldMap.fold = true
		simpleFoldMap.keys, simpleFoldMap.vals = decodeMap(ucdSimpleFolding)
		simpleFoldMap.buildClasses()
		for _, k := range simpleFoldMap.keys {
			simpleFoldMap.keySet = append(simpleFoldMap.keySet, k, k)
		}
		simpleFoldMap.keySet = simpleFoldMap.keySet.Normalize()
	})
	return &simpleFoldMap
}

// canonNonUnicode returns the Canonicalize map without u/v.
func canonNonUnicode() *CaseMap {
	canonNonUOnce.Do(func() {
		canonNonUMap.keys, canonNonUMap.vals = decodeMap(ucdCanonNonUnicode)
		canonNonUMap.buildClasses()
	})
	return &canonNonUMap
}

// Canonicalizer returns the Canonicalize map for the flags (u or v select
// simple case folding).
func Canonicalizer(unicodeMode bool) *CaseMap {
	if unicodeMode {
		return simpleFolding()
	}
	return canonNonUnicode()
}

// Canonicalize is Canonicalize(rer, ch) for an ignoreCase pattern.
func (m *CaseMap) Canonicalize(c rune) rune {
	if c < 0x80 {
		if m.fold {
			if c >= 'A' && c <= 'Z' {
				return c + 'a' - 'A'
			}
			return c
		}
		if c >= 'a' && c <= 'z' {
			return c - ('a' - 'A')
		}
		return c
	}
	return m.lookup(c)
}

// closure returns {c : Canonicalize(c) = Canonicalize(a) for some a in s},
// the set of characters a case-insensitive class matches.
func (m *CaseMap) closure(s Set) Set {
	var add Set
	j := 0
	for i := 0; i < len(s); i += 2 {
		lo, hi := s[i], s[i+1]
		for j < len(m.members) && m.members[j] < lo {
			j++
		}
		for k := j; k < len(m.members) && m.members[k] <= hi; k++ {
			for _, c := range m.groups[m.groupOf[k]] {
				add = append(add, c, c)
			}
		}
	}
	if len(add) == 0 {
		return s
	}
	return s.Union(add.Normalize())
}

// simpleFoldSet is {scf(c) : c in s}, the MaybeSimpleCaseFolding step of
// the v flag (m is simpleFolding()).
func (m *CaseMap) simpleFoldSet(s Set) Set {
	var out Set
	j := 0
	for i := 0; i < len(s); i += 2 {
		lo, hi := s[i], s[i+1]
		out = append(out, lo, hi)
		for j < len(m.keys) && m.keys[j] < lo {
			j++
		}
		for k := j; k < len(m.keys) && m.keys[k] <= hi; k++ {
			out = append(out, m.vals[k], m.vals[k])
		}
	}
	// Remove the folded-away code points: scf(c) != c for every key.
	return out.Normalize().Subtract(m.keySet)
}
