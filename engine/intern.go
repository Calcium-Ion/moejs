package engine

import (
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"weak"
)

// Interning has three levels:
//
//  1. the static atom table (atoms.go), built at package init and immutable;
//  2. a per-realm cache (plain maps, single goroutine);
//  3. the process-wide table (internTable): lock-free reads, amortized O(1)
//     inserts, weak entries.
//
// Atom identity is process-wide for every name: realm-local shapes, shared
// intrinsic shapes and the materialized constants of compiled functions
// (funcMeta, interp.go, built once per template and shared by every realm)
// all hold the same pointer for a name, and interned strings compare by
// pointer. The table therefore has no capacity limit: a per-shard cap with a
// realm-local fallback would let the first realm to materialize a template
// publish keys no other realm could produce, so their property reads would
// return undefined. Memory stays bounded because the
// entries are weak: a name that no realm, shape or funcMeta references any
// more is collected with them and a cleanup drops its entry.

// staticAtoms maps ASCII builtin names to their process-wide *String. It is
// fully populated during package initialization and read-only afterwards.
var staticAtoms = map[string]*String{}

// staticAtomCount is the number of static atoms; dynamic atom ids start
// above it.
var staticAtomCount uint32

// atomCounter hands out dynamic atom ids.
var atomCounter atomic.Uint32

func init() {
	atomCounter.Store(staticAtomCount)
}

// staticAtom interns a builtin name at package init time.
func staticAtom(name string) *String {
	if s, ok := staticAtoms[name]; ok {
		return s
	}
	staticAtomCount++
	s := &String{s: name, n: int32(len(name)), kind: strASCII, atom: staticAtomCount, numeric: canonicalNumericName(name)}
	if name == "" {
		s = emptyString
		s.atom = staticAtomCount
	}
	s.Hash() // precomputed so concurrent readers never write
	staticAtoms[name] = s
	return s
}

// LookupStaticAtom returns the static atom for an ASCII name, if any.
func LookupStaticAtom(name string) (*String, bool) {
	s, ok := staticAtoms[name]
	return s, ok
}

// nextAtom hands out a dynamic atom id.
func nextAtom() uint32 { return takeAtomID(&atomCounter) }

// takeAtomID increments the atom counter c and returns it. Should c ever
// wrap, it skips 0, which means "not interned", and the static ids, so ids
// 1..staticAtomCount stay unique to the static atoms (isStaticAtom,
// coldGlobalSlot).
func takeAtomID(c *atomic.Uint32) uint32 {
	id := c.Add(1)
	for id <= staticAtomCount {
		c.CompareAndSwap(id, staticAtomCount) // concurrent callers retry
		id = c.Add(1)
	}
	return id
}

// internTable is the process-wide string table: content -> weak pointer to
// the canonical *String. Reads are lock-free and inserts are amortized O(1)
// (sync.Map); an insert of a name that is already present returns the
// existing string, so identity is unique per content for as long as any
// reference to the string exists.
type internTable struct {
	m sync.Map // string -> weak.Pointer[String]
}

var (
	globalInternASCII internTable
	globalInternUTF16 internTable
)

// lookup returns the live canonical string for key, or nil.
func (t *internTable) lookup(key string) *String {
	if wp, ok := t.m.Load(key); ok {
		return wp.(weak.Pointer[String]).Value()
	}
	return nil
}

// intern publishes s, whose content is key and into which nothing else
// points, as the canonical string for key, unless a live entry exists,
// which is returned instead. s must be complete (hash and atom set) before
// it is published: readers on other goroutines see it the moment it is in
// the table and never take a lock.
func (t *internTable) intern(key string, s *String) *String {
	if s.atom == 0 {
		s.atom = nextAtom() // wasted if another goroutine wins the race
	}
	wp := weak.Make(s)
	for {
		actual, loaded := t.m.LoadOrStore(key, wp)
		if loaded {
			old := actual.(weak.Pointer[String])
			if e := old.Value(); e != nil {
				return e
			}
			// The entry's string was collected and its cleanup has not run
			// yet: take its place.
			if !t.m.CompareAndSwap(key, old, wp) {
				continue
			}
		}
		runtime.AddCleanup(s, dropInternEntry, internEntry{t, key, wp})
		return s
	}
}

// internEntry identifies one table entry for its cleanup: the table, the key
// (whose bytes the string shares) and the weak pointer, never the string
// itself, so the cleanup argument does not keep the string alive.
type internEntry struct {
	t   *internTable
	key string
	wp  weak.Pointer[String]
}

// dropInternEntry removes a collected string's entry unless a live string
// has already taken its place.
func dropInternEntry(e internEntry) { e.t.m.CompareAndDelete(e.key, e.wp) }

// Intern returns the process-wide canonical *String for the content of s.
// Interned strings compare by pointer, which is what PropertyKey equality
// relies on.
func (r *Realm) Intern(s *String) *String {
	if s.atom != 0 {
		return s
	}
	if s.kind == strRope {
		s.flatten()
	}
	if s.kind == strASCII {
		return r.internASCII(s.s)
	}
	return r.internUTF16(s)
}

// InternGoString interns a Go string.
func (r *Realm) InternGoString(g string) *String {
	if a, ok := staticAtoms[g]; ok {
		return a
	}
	if a, ok := r.internCacheASCII[g]; ok {
		return a
	}
	if !isASCII(g) {
		return r.internUTF16(FromGoString(g))
	}
	return r.internASCII(g)
}

// internASCII interns ASCII content.
func (r *Realm) internASCII(key string) *String {
	if a, ok := staticAtoms[key]; ok {
		return a
	}
	if a, ok := r.internCacheASCII[key]; ok {
		return a
	}
	a := globalASCIIAtom(key)
	if r.internCacheASCII == nil {
		if !r.cacheInterning() {
			return a
		}
		r.internCacheASCII = make(map[string]*String)
	}
	// Keyed by the atom's own content, never by key, which may be a slice
	// of a JSON text or of a large string (builtin_json.go, Substring).
	r.internCacheASCII[a.s] = a
	return a
}

// globalASCIIAtom interns ASCII content in the process-wide table.
func globalASCIIAtom(key string) *String {
	a := globalInternASCII.lookup(key)
	if a == nil {
		// The table gets its own String header and its own copy of the
		// content, never the caller's: a header carved from a FromGo slab
		// would otherwise pin the whole slab, and content sharing a JSON text
		// or a large string would pin that, for as long as the name is in use
		// (hostconv.go, builtin_json.go). Once per new atom.
		key = strings.Clone(key)
		s := asciiString(key)
		s.hash = hashASCII(key)
		s.numeric = canonicalNumericName(key)
		a = globalInternASCII.intern(key, s)
	}
	return a
}

// globalUTF16Atom interns UTF-16 content in the process-wide table.
func globalUTF16Atom(key string, s *String) *String {
	a := globalInternUTF16.lookup(key)
	if a == nil {
		// Fresh header and code units for the table, as in globalASCIIAtom:
		// a substring shares its string's units (Substring). key is the
		// caller's own copy (utf16Key).
		u := make([]uint16, len(s.u))
		copy(u, s.u)
		a = globalInternUTF16.intern(key, &String{u: u, n: s.n, hash: s.Hash(), kind: strUTF16})
	}
	return a
}

// InternKey is Realm.KeyFromGoString without a realm: the key comes from
// the process-wide tables, so it is valid in every realm. A host that
// resolves property names once (a hook path, a struct field) keeps the key
// and with it the atom alive.
func InternKey(g string) PropertyKey {
	if i, ok := parseArrayIndex(g); ok {
		return IndexKey(i)
	}
	if a, ok := staticAtoms[g]; ok {
		return StringKey(a)
	}
	if !isASCII(g) {
		s := FromGoString(g)
		return StringKey(globalUTF16Atom(utf16Key(s.u), s))
	}
	return StringKey(globalASCIIAtom(g))
}

// internCacheAfter is the number of names a realm interns through the
// process-wide tables alone before it creates its caches: a runtime that
// only binds a few host globals never pays for the maps (256 bytes), and one
// that runs code gets them within its first lookups.
const internCacheAfter = 8

// cacheInterning reports whether the realm, which has no cache for the kind
// of name at hand, should create it now.
func (r *Realm) cacheInterning() bool {
	if r.internLookups < internCacheAfter {
		r.internLookups++
		return false
	}
	return true
}

func (r *Realm) internUTF16(s *String) *String {
	key := utf16Key(s.u)
	if a, ok := r.internCacheUTF16[key]; ok {
		return a
	}
	a := globalUTF16Atom(key, s)
	if r.internCacheUTF16 == nil {
		if !r.cacheInterning() {
			return a
		}
		r.internCacheUTF16 = make(map[string]*String, 8)
	}
	r.internCacheUTF16[key] = a
	return a
}

// hashASCII is String.Hash for ASCII content (FNV-1a over code units).
func hashASCII(s string) uint32 {
	h := uint32(2166136261)
	for i := range len(s) {
		h = (h ^ uint32(s[i])) * 16777619
	}
	if h == 0 {
		h = 1
	}
	return h
}

// utf16Key encodes code units as a map key. UTF-16 strings are kept in a
// separate table from ASCII strings, so the little-endian byte encoding cannot
// collide with an ASCII key and lone surrogates stay distinguishable (a
// UTF-8 key would fold them into U+FFFD).
func utf16Key(u []uint16) string {
	b := make([]byte, 2*len(u))
	for i, c := range u {
		b[2*i] = byte(c)
		b[2*i+1] = byte(c >> 8)
	}
	return bytesToString(b)
}
