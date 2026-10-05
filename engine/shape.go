package engine

import (
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"weak"
)

// Property attribute bits stored in Shape nodes and dictionary entries.
const (
	attrWritable     uint8 = 1 << 0
	attrEnumerable   uint8 = 1 << 1
	attrConfigurable uint8 = 1 << 2
	// attrAccessor marks the slot as holding an accessorValue instead of data.
	attrAccessor uint8 = 1 << 3
	// attrLazy marks a data slot that may still hold the internal hole
	// marker in place of a value created on first access (the `prototype`
	// of an ordinary function, interp.go). It is part of the transition key,
	// so a shape with a lazy slot is never shared with an object whose same
	// keyed property holds a real value, and an inline cache entry filled on
	// the latter cannot match the former. Readers see the attribute only
	// through Shape.Lookup; getOwnCell materializes the value and strips the
	// bit, so it never reaches a descriptor.
	attrLazy uint8 = 1 << 4

	// attrDefault is the attribute set of a property created by assignment.
	attrDefault = attrWritable | attrEnumerable | attrConfigurable
	// attrHidden is the attribute set of builtin methods: writable and
	// configurable but not enumerable.
	attrHidden = attrWritable | attrConfigurable
	// attrFrozen is neither writable nor configurable (Object.freeze).
	attrFrozen uint8 = 0
)

// Shape limits.
const (
	// shapeTableThreshold is the property count above which a Shape builds
	// its hash table instead of walking the parent chain on lookup.
	shapeTableThreshold = 8
	// maxShapeProps is the named-property count above which an object
	// switches to dictionary mode.
	maxShapeProps = 64
	// shapeSmallTransitions is the transition count kept in a slice before a
	// Shape switches to a map.
	shapeSmallTransitions = 4
	// shapeStrongTransitions is the transition count a shape of a realm's own
	// tree holds strongly; past it a child is held weakly (strongChild), so the
	// shapes of objects keyed by data (ids, user values, host map keys) die
	// with the objects instead of growing a pooled realm's tree request after
	// request. shapeWeakSweep is the slack of the weak children before their
	// dead entries are swept.
	shapeStrongTransitions = 8
	shapeWeakSweep         = 64
	// maxSharedTransitions bounds the process-wide transition cache of one
	// shared shape and maxSharedShapes the shapes published in all of them;
	// past either bound a transition from a shared shape continues in the
	// realm's own tree (Realm.localize).
	maxSharedTransitions = 1024
	maxSharedShapes      = 1 << 14
	// maxSharedDataTransitions bounds the children of one shared shape whose
	// key is not bounded by code (codeKey): names that only data supplies
	// (computed member names, JSON.parse, host maps) take at most these many
	// and then continue in the realm's own tree, so objects used as
	// dictionaries cannot crowd the shapes of object literals out of the
	// shared tree.
	maxSharedDataTransitions = 32
	// maxCodeKeys bounds the process-wide set of code-bound names.
	maxCodeKeys = 1 << 16
	// sharedSmallTransitions is the snapshot size scanned linearly before
	// the snapshot switches to a map.
	sharedSmallTransitions = 8
)

// sharedTransMu serializes publication into the shared shape tree (reads are
// lock-free snapshots); sharedShapeCount counts the published shapes.
var (
	sharedTransMu    sync.Mutex
	sharedShapeCount int
)

// codeKeys holds the property-name constants of compiled code (materialize).
// With the static atoms and the well-known symbols they are the keys bounded
// by code size rather than by data. Guarded by sharedTransMu.
var codeKeys = map[*String]struct{}{}

// addCodeKeys records the property-name constants of one template.
func addCodeKeys(keys []PropertyKey) {
	sharedTransMu.Lock()
	defer sharedTransMu.Unlock()
	for _, k := range keys {
		if k.IsString() && !isStaticAtom(k.String()) && len(codeKeys) < maxCodeKeys {
			codeKeys[k.String()] = struct{}{}
		}
	}
}

func isStaticAtom(s *String) bool { return s.atom != 0 && s.atom <= staticAtomCount }

// codeKey reports whether key is bounded by code (sharedTransMu held): the
// well-known symbols are process-wide and fixed; other symbols and private
// names are realm-local and never published.
func codeKey(key PropertyKey) bool {
	if !key.IsString() {
		return key.IsSymbol() && key.Symbol().wellKnown
	}
	if isStaticAtom(key.String()) {
		return true
	}
	_, ok := codeKeys[key.String()]
	return ok
}

// The bits of Shape.noFill, which fillGetIC tests in refuseFill.
const (
	noFillAll     uint8 = 1 << iota // no entry is filled on the shape
	noFillStatics                   // none for a static while %RegExp% has the shape with them pending
)

// Shape is an immutable node of a realm's transition tree. A shape encodes
// the object's [[Prototype]] (through its root) and the ordered list of named
// properties with their attributes; the slot of a property is its index in
// the chain, so slot == position in Object.slots.
type Shape struct {
	parent *Shape
	proto  *Object // [[Prototype]] shared by every object with this shape
	key    PropertyKey
	slot   uint32
	attrs  uint8
	isDict bool // the process-wide dictionary sentinel
	shared bool // immutable and readable from any goroutine (the shared tree)
	// propsLent records that the first published child extended this
	// shape's props in place (publish); guarded by sharedTransMu.
	propsLent bool
	count     uint32 // named properties in the chain
	// noFill marks the shape of an object with properties pending, which
	// an inline cache filled on another object of this shape would hit:
	// noFillAll for a deferred install (deferInstall), noFillStatics for
	// %RegExp% with its statics pending (installRegExpStatics).
	noFill uint8

	transSmall []transition
	transMap   map[transitionKey]*Shape
	// sharedTrans caches the transitions of a shared shape process-wide:
	// realms over the shared intrinsics build their objects, literals, host
	// objects and globals from the same shared roots, so the children (with
	// their lookup tables) are built once and are themselves shared. The
	// snapshot is immutable and replaced copy-on-write under sharedTransMu.
	// A shape of a realm's own tree keeps there the children it holds weakly
	// (sharedTransitions.weak), written by the realm's goroutine only.
	sharedTrans atomic.Pointer[sharedTransitions]

	table map[PropertyKey]uint32 // key -> slot; built lazily when count > shapeTableThreshold
	props []shapeProp            // ordered properties; built lazily, eagerly for shared shapes
}

type transitionKey struct {
	key   PropertyKey
	attrs uint8
}

type transition struct {
	key   PropertyKey
	attrs uint8
	shape *Shape
}

// sharedTransitions is one snapshot of a shared shape's published children:
// a slice scanned like transSmall while small, a map beyond; data counts the
// children whose key is not code-bound. table is the lookup table of a shape
// published without one, built on its first lookup (Shape.lookupTable).
//
// For a shape of a realm's own tree it holds instead the children past
// shapeStrongTransitions (weak), swept of the collected ones once they reach
// sweep entries.
type sharedTransitions struct {
	small []transition
	m     map[transitionKey]*Shape
	data  int
	table map[PropertyKey]uint32
	weak  map[transitionKey]weak.Pointer[Shape]
	sweep int
}

func (t *sharedTransitions) lookup(key PropertyKey, attrs uint8) *Shape {
	if t.m != nil {
		return t.m[transitionKey{key, attrs}]
	}
	for i := range t.small {
		if t.small[i].key == key && t.small[i].attrs == attrs {
			return t.small[i].shape
		}
	}
	return nil
}

// shapeProp is one entry of Shape.props; its index is the slot.
type shapeProp struct {
	key   PropertyKey
	attrs uint8
}

// rootKey marks a root shape's key field; it can never equal a real key.
var rootKey = PropertyKey{Value{bits: bitsHole}}

// newRootShape creates the empty shape for objects whose prototype is proto.
func newRootShape(proto *Object) *Shape {
	return &Shape{proto: proto, key: rootKey}
}

// Count returns the number of named properties.
func (s *Shape) Count() int { return int(s.count) }

// Proto returns the [[Prototype]] encoded by the shape.
func (s *Shape) Proto() *Object { return s.proto }

// IsDictionary reports whether s is the dictionary sentinel.
func (s *Shape) IsDictionary() bool { return s.isDict }

// Lookup finds key in the shape and returns its slot and attributes.
func (s *Shape) Lookup(key PropertyKey) (slot uint32, attrs uint8, ok bool) {
	if s.count == 0 || s.isDict {
		return 0, 0, false
	}
	if s.count > shapeTableThreshold {
		table := s.table
		if table == nil {
			table = s.lookupTable()
		}
		slot, ok = table[key]
		if !ok {
			return 0, 0, false
		}
		return slot, s.props[slot].attrs, true
	}
	for n := s; n.count != 0; n = n.parent {
		if n.key == key {
			return n.slot, n.attrs, true
		}
	}
	return 0, 0, false
}

// has reports whether key is in the chain without building the lookup table
// (used to probe the intermediate shapes of an object literal, which are
// never looked up again once the literal is complete).
func (s *Shape) has(key PropertyKey) bool {
	if s.table != nil {
		_, ok := s.table[key]
		return ok
	}
	for n := s; n.count != 0; n = n.parent {
		if n.key == key {
			return true
		}
	}
	return false
}

// Props returns the properties in insertion (slot) order. The slice is owned
// by the shape and must not be modified.
func (s *Shape) Props() []shapeProp {
	if s.props == nil && s.count != 0 {
		props := make([]shapeProp, s.count)
		for n := s; n.count != 0; n = n.parent {
			props[n.slot] = shapeProp{key: n.key, attrs: n.attrs}
		}
		s.props = props
	}
	return s.props
}

func (s *Shape) buildTable() { s.table = newShapeTable(s.Props()) }

func newShapeTable(props []shapeProp) map[PropertyKey]uint32 {
	table := make(map[PropertyKey]uint32, len(props))
	for i, p := range props {
		table[p.key] = uint32(i)
	}
	return table
}

// lookupTable returns the hash table of s, building it on first use. A
// shared shape is never written, so its table joins its transition snapshot:
// most shapes of the shared tree are the intermediate shapes of literals and
// are never looked up.
func (s *Shape) lookupTable() map[PropertyKey]uint32 {
	if !s.shared {
		s.buildTable()
		return s.table
	}
	if t := s.sharedTrans.Load(); t != nil && t.table != nil {
		return t.table
	}
	table := newShapeTable(s.props)
	sharedTransMu.Lock()
	defer sharedTransMu.Unlock()
	var next sharedTransitions
	if old := s.sharedTrans.Load(); old != nil {
		if old.table != nil {
			return old.table
		}
		next = *old
	}
	next.table = table
	s.sharedTrans.Store(&next)
	return table
}

// transition returns the child shape for key/attrs cached strongly, or nil.
// Past the strong children (strongChild) it may be in the weak tier instead
// (weakTransition).
func (s *Shape) transition(key PropertyKey, attrs uint8) *Shape {
	if s.transMap != nil {
		return s.transMap[transitionKey{key, attrs}]
	}
	for _, t := range s.transSmall {
		if t.key == key && t.attrs == attrs {
			return t.shape
		}
	}
	return nil
}

// addProperty returns the child shape with key appended, creating and caching
// the transition on first use. A shared shape is never written: its child
// comes from the process-wide tree (publish) or, past its bounds, from r's
// own tree (localize).
func (s *Shape) addProperty(r *Realm, key PropertyKey, attrs uint8) *Shape {
	if s.shared {
		if child := s.publish(key, attrs); child != nil {
			return child
		}
		return r.localize(s).addProperty(r, key, attrs)
	}
	if child := s.transition(key, attrs); child != nil {
		return child
	}
	if !r.strongChild(s) {
		return s.addWeakProperty(key, attrs)
	}
	child := r.allocShape()
	*child = Shape{
		parent: s,
		proto:  s.proto,
		key:    key,
		slot:   s.count,
		attrs:  attrs,
		count:  s.count + 1,
	}
	if s.transSmall == nil && s.transMap == nil {
		s.transSmall = r.allocTransition()
	}
	s.addTransition(key, attrs, child)
	return child
}

// addWeakProperty is addProperty past the strong children of s: the child
// comes from, or goes to, the weak tier. s has a transMap, and the realm is
// past its bootstrap (strongChild).
func (s *Shape) addWeakProperty(key PropertyKey, attrs uint8) *Shape {
	if child := s.weakTransition(key, attrs); child != nil {
		return child
	}
	child := &Shape{
		parent: s,
		proto:  s.proto,
		key:    key,
		slot:   s.count,
		attrs:  attrs,
		count:  s.count + 1,
	}
	s.addWeakChild(key, attrs, child)
	return child
}

// allocShape returns a zeroed shape of r's own tree, from the bootstrap slab
// while the intrinsics of a mutable realm are being built.
func (r *Realm) allocShape() *Shape {
	if b := r.boot; b != nil && len(b.shapes) < cap(b.shapes) {
		n := len(b.shapes)
		b.shapes = b.shapes[:n+1]
		return &b.shapes[n]
	}
	return new(Shape)
}

// allocTransition returns room for a shape's first transition record: an
// empty slice of capacity one from the bootstrap slab, nil (append
// allocates) after the bootstrap.
func (r *Realm) allocTransition() []transition {
	if b := r.boot; b != nil && len(b.trans) < cap(b.trans) {
		n := len(b.trans)
		b.trans = b.trans[:n+1]
		return b.trans[n : n : n+1]
	}
	return nil
}

// publish returns the shared child of the shared shape s for key/attrs from
// the process-wide cache, building it (props materialized, marked immutable)
// and publishing it under the lock on first use. It returns nil for a
// realm-local key (codeKey), and past maxSharedTransitions, maxSharedShapes
// or, for a string not bounded by code, maxSharedDataTransitions.
func (s *Shape) publish(key PropertyKey, attrs uint8) *Shape {
	if t := s.sharedTrans.Load(); t != nil {
		if child := t.lookup(key, attrs); child != nil {
			return child
		}
	}
	sharedTransMu.Lock()
	defer sharedTransMu.Unlock()
	old := s.sharedTrans.Load()
	var next sharedTransitions
	if old != nil {
		if child := old.lookup(key, attrs); child != nil {
			return child
		}
		next = *old
	}
	n := len(next.small) + len(next.m)
	code := codeKey(key)
	if n >= maxSharedTransitions || sharedShapeCount >= maxSharedShapes ||
		!code && (!key.IsString() || next.data >= maxSharedDataTransitions) {
		return nil
	}
	if !code {
		next.data++
	}
	child := &Shape{
		parent: s,
		proto:  s.proto,
		key:    key,
		slot:   s.count,
		attrs:  attrs,
		count:  s.count + 1,
		shared: true,
	}
	// The first child extends the parent's props in place: readers of the
	// parent never look past its own length, so a chain of n published
	// shapes costs O(n) property entries rather than O(n²).
	if s.propsLent {
		child.props = append(s.props[:s.count:s.count], shapeProp{key, attrs})
	} else {
		child.props = append(s.props, shapeProp{key, attrs})
		s.propsLent = true
	}
	switch {
	case next.m != nil:
		m := make(map[transitionKey]*Shape, n+1)
		maps.Copy(m, next.m)
		m[transitionKey{key, attrs}] = child
		next.m = m
	case n < sharedSmallTransitions:
		next.small = append(next.small[:n:n], transition{key, attrs, child})
	default:
		m := make(map[transitionKey]*Shape, n+1)
		for _, t := range next.small {
			m[transitionKey{t.key, t.attrs}] = t.shape
		}
		m[transitionKey{key, attrs}] = child
		next.small, next.m = nil, m
	}
	s.sharedTrans.Store(&next)
	sharedShapeCount++
	return child
}

// localize returns the shape of r's own tree equivalent to the shared shape
// s (same prototype, properties and slots), for a transition the shared
// tree no longer takes. The replay is cached through ordinary transitions.
func (r *Realm) localize(s *Shape) *Shape {
	n := r.localRoot(s.proto)
	for _, p := range s.Props() {
		n = n.addProperty(r, p.key, p.attrs)
	}
	return n
}

// addChain returns the shape reached from s by appending keys in order, all
// with attrs. Existing transitions are followed; the shapes that do not
// exist yet come from one slab with their transition records in another, so
// a host object's key set costs two allocations per realm instead of two per
// key. Shared shapes take the keys one at a time through addProperty.
func (s *Shape) addChain(r *Realm, keys []PropertyKey, attrs uint8) *Shape {
	for len(keys) > 0 && !s.shared {
		child := s.transition(keys[0], attrs)
		if child == nil {
			if r.strongChild(s) {
				break
			}
			if child = s.weakTransition(keys[0], attrs); child == nil {
				break
			}
		}
		s, keys = child, keys[1:]
	}
	if len(keys) == 0 {
		return s
	}
	if s.shared || len(keys) == 1 {
		for i, k := range keys {
			s = s.addProperty(r, k, attrs)
			if !s.shared {
				return s.addChain(r, keys[i+1:], attrs)
			}
		}
		return s
	}
	nodes := make([]Shape, len(keys))
	trans := make([]transition, len(keys)-1)
	parent := s
	for i, k := range keys {
		n := &nodes[i]
		*n = Shape{parent: parent, proto: parent.proto, key: k, slot: parent.count, attrs: attrs, count: parent.count + 1}
		if i == 0 {
			if r.strongChild(parent) {
				parent.addTransition(k, attrs, n)
			} else {
				parent.addWeakChild(k, attrs, n)
			}
		} else {
			trans[i-1] = transition{k, attrs, n}
			parent.transSmall = trans[i-1 : i : i]
		}
		parent = n
	}
	return parent
}

// strongChild reports whether the next child of s, a shape of r's own tree,
// is cached strongly (addTransition) or weakly (addWeakChild): strongly for
// the first shapeStrongTransitions children, weakly past them. A collected
// child is unreachable from everything that could observe it (objects,
// inline caches, host shape caches and predictions all hold their shapes),
// so a transition that finds it gone builds an equivalent one and no holder
// ever sees two shapes for one transition. The intrinsics' shapes stay
// strong: a mutable realm's bootstrap and the shared builders keep every
// child. The callers test it inline, so that a bootstrap transition costs
// what it did before the weak tier.
//
// A shape for which it holds has no weak children (the bootstrap comes
// first, transMap never shrinks and the shared builders' realms never leave
// it), with one exception. The deferred Date.prototype install of a mutable
// realm (installDatePrototypeDeferred) sets r.boot after the program has
// run, so its children stay strong too: on a shape of Date.prototype's chain
// that already has eight children, it adds a ninth strong one without
// looking in the weak tier, which may hold an equivalent child. Those
// children are Date.prototype's own shapes, which it keeps anyway, so the
// cost is at most a transMap entry and a duplicate shape for each of the
// install's 47 transitions, once per realm, and only on shapes a program
// reaches by defining the same keys in the same order with the builtins'
// attributes (a class whose methods are named like Date's). Both shapes are
// valid, each holder keeps its own, and an inline cache that sees both is
// polymorphic. Telling the install apart here would cost a load on every
// bootstrap transition (a flag in its slabs), a second slab test in every
// allocator (its slabs in another field) or a larger size class (a flag in
// the Realm, which has no free byte).
func (r *Realm) strongChild(s *Shape) bool {
	return r.boot != nil || len(s.transMap) < shapeStrongTransitions || r.buildingShared
}

// addWeakChild caches the transition from s to child in the weak tier of
// s's transitions.
func (s *Shape) addWeakChild(key PropertyKey, attrs uint8, child *Shape) {
	t := s.sharedTrans.Load()
	if t == nil {
		t = &sharedTransitions{sweep: shapeWeakSweep}
		s.sharedTrans.Store(t)
	}
	if len(t.weak) >= t.sweep {
		t.sweepWeak()
	} else if t.weak == nil {
		t.weak = make(map[transitionKey]weak.Pointer[Shape])
	}
	t.weak[transitionKey{key, attrs}] = weak.Make(child)
}

// sweepWeak drops the collected children from the weak tier of a local
// shape's transitions. It is kept out of addWeakChild, which also keeps the
// functions linked after this file, (*String).flatten and the moejs
// package's (*Runtime).Call among them, in dev's 64-byte phase
// (realm_calldata.go).
func (t *sharedTransitions) sweepWeak() {
	live := 0
	for _, p := range t.weak {
		if p.Value() != nil {
			live++
		}
	}
	// A new map: a map does not shrink, and the one that held a burst of
	// children would keep its size for the realm's lifetime.
	m := make(map[transitionKey]weak.Pointer[Shape], live+1)
	for k, p := range t.weak {
		if p.Value() != nil {
			m[k] = p
		}
	}
	t.weak, t.sweep = m, 2*live+shapeWeakSweep
}

// weakTransition returns the weakly held child of s for key/attrs if it is
// still alive, or nil. A child it finds collected sweeps the weak tier: the
// sweep that set the tier's threshold may have counted children that a
// collection cleared since, and their entries would keep their keys until
// the tier grew to it. That sweep runs at most once per collection that
// cleared a child of s.
func (s *Shape) weakTransition(key PropertyKey, attrs uint8) *Shape {
	if t := s.sharedTrans.Load(); t != nil {
		p, ok := t.weak[transitionKey{key, attrs}]
		if child := p.Value(); child != nil || !ok {
			return child
		}
		t.sweepWeak()
	}
	return nil
}

func (s *Shape) addTransition(key PropertyKey, attrs uint8, child *Shape) {
	if s.transMap != nil {
		s.transMap[transitionKey{key, attrs}] = child
		return
	}
	if len(s.transSmall) < shapeSmallTransitions {
		s.transSmall = append(s.transSmall, transition{key, attrs, child})
		return
	}
	m := make(map[transitionKey]*Shape, 2*shapeSmallTransitions)
	for _, t := range s.transSmall {
		m[transitionKey{t.key, t.attrs}] = t.shape
	}
	m[transitionKey{key, attrs}] = child
	s.transMap = m
	s.transSmall = nil
}

// prepareShared materializes the lazily built lookup structures of s and
// marks s and its ancestors immutable, so that objects with this shape can be
// read from any goroutine without ever writing to a Shape. Every shared shape
// has its props: the ancestors take prefixes of s's.
func (s *Shape) prepareShared() {
	if s.shared && sharedTpl != nil {
		// Published: other goroutines may be reading it (buildLateGroup).
		return
	}
	if s.count != 0 {
		props := s.Props()
		for n := s.parent; n.count != 0 && !n.shared; n = n.parent {
			if n.props == nil {
				n.props = props[:n.count:n.count]
			}
		}
		if s.count > shapeTableThreshold && s.table == nil {
			s.buildTable()
		}
	}
	for n := s; n != nil && !n.shared; n = n.parent {
		n.shared = true
	}
}

// IsShared reports whether the shape belongs to the shared intrinsics.
func (s *Shape) IsShared() bool { return s.shared }

// root returns the root shape of the chain.
func (s *Shape) root() *Shape {
	n := s
	for n.count != 0 {
		n = n.parent
	}
	return n
}

// replaceAttrs returns a shape with the same property order in which key has
// attrs. Slots are preserved, so Object.slots needs no change. The chain is
// replayed from the root; the result is cached through ordinary transitions,
// so repeated freezes of same-shaped objects converge on one shape.
func (s *Shape) replaceAttrs(r *Realm, key PropertyKey, attrs uint8) *Shape {
	props := s.Props()
	n := s.root()
	for _, p := range props {
		a := p.attrs
		if p.key == key {
			a = attrs
		}
		n = n.addProperty(r, p.key, a)
	}
	return n
}

// mapAttrs returns a shape with every property's attributes rewritten by fn
// (used by freeze and seal).
func (s *Shape) mapAttrs(r *Realm, fn func(attrs uint8) uint8) *Shape {
	props := s.Props()
	n := s.root()
	for _, p := range props {
		n = n.addProperty(r, p.key, fn(p.attrs))
	}
	return n
}

// rebase replays the chain onto a different root (prototype change).
func (s *Shape) rebase(r *Realm, root *Shape) *Shape {
	n := root
	for _, p := range s.Props() {
		n = n.addProperty(r, p.key, p.attrs)
	}
	return n
}

// ICEntry is one inline-cache entry for a named property access. It is valid
// for a receiver iff the receiver's shape is Shape and the realm's prototype
// epoch still equals Epoch; then the property lives in slot Slot() of the
// object HolderDepth() prototypes up from the receiver (0 = own property).
// The epoch advances in steps of two, so it is always even: an entry for an
// accessor property stores the epoch with the low bit set, never passes the
// data check on the interpreter's hit path, and is validated by accessorIC
// in the slow paths instead. Slot and depth share one word so the entry is 16 bytes: a realm's IC table
// (one entry per property-access site of every function it instantiated) is
// the largest single item it retains.
type ICEntry struct {
	Shape *Shape
	Epoch uint32
	loc   uint32 // slot in the low 24 bits, holder depth in the high 8
}

// newICEntry builds an entry for a property found at slot, depth prototypes
// up from the receiver's shape.
func newICEntry(shape *Shape, epoch uint32, slot uint32, depth uint8) ICEntry {
	return ICEntry{Shape: shape, Epoch: epoch, loc: slot&0xFFFFFF | uint32(depth)<<24}
}

// Slot returns the property's slot in the holder.
func (e *ICEntry) Slot() uint32 { return e.loc & 0xFFFFFF }

// HolderDepth returns how many prototypes up from the receiver the holder is.
func (e *ICEntry) HolderDepth() uint8 { return uint8(e.loc >> 24) }

// Valid reports whether the entry can be used for o in realm r.
func (e *ICEntry) Valid(r *Realm, o *Object) bool {
	return o.shape == e.Shape && r.protoEpoch == e.Epoch
}

// Holder walks HolderDepth prototypes up from o. Only meaningful after Valid.
func (e *ICEntry) Holder(o *Object) *Object {
	for range e.loc >> 24 {
		o = o.proto
	}
	return o
}

// ProtoEpoch returns the realm's prototype validity counter.
func (r *Realm) ProtoEpoch() uint32 { return r.protoEpoch }

// rootShapeFor returns the empty shape for objects with the given prototype:
// the process-wide shared root when the prototype is a shared intrinsic.
func (r *Realm) rootShapeFor(proto *Object) *Shape {
	if proto == nil {
		return r.nullProtoRoot
	}
	if proto == r.ObjectPrototype {
		return r.plainRoot
	}
	if proto.flags&flagShared != 0 {
		return sharedRootFor(proto)
	}
	return r.localRoot(proto)
}

// localRoot returns r's own root shape for proto. For a shared prototype it
// is the root of the realm-local continuation of the shared tree (localize),
// never handed out by rootShapeFor.
//
// The root of a prototype the program created lives in the prototype itself
// (dictProps.root), so it, its transition tree and the names in it are
// collected with the prototype: a pooled runtime that runs `new F()` with a
// fresh F, a class declaration or Object.create(fresh) on every request
// keeps none of them. rootShapes holds only the roots that live as long as
// the realm anyway: those of the null prototype, of shared prototypes, of
// the intrinsics of a mutable realm (made while r.boot is set) and those the
// shared template and the late groups collect (buildSharedTemplate,
// addSharedRoots).
func (r *Realm) localRoot(proto *Object) *Shape {
	if proto != nil && proto.dict != nil && proto.dict.root != nil {
		return proto.dict.root
	}
	return r.newLocalRoot(proto)
}

// newLocalRoot is localRoot for a prototype whose root is not in the
// prototype itself.
func (r *Realm) newLocalRoot(proto *Object) *Shape {
	if s, ok := r.rootShapes[proto]; ok {
		return s
	}
	s := newRootShape(proto)
	if proto == nil || proto.flags&flagShared != 0 || r.buildingShared || r.boot != nil {
		if r.rootShapes == nil {
			r.rootShapes = make(map[*Object]*Shape, 8)
		}
		r.rootShapes[proto] = s
		return s
	}
	if proto.dict == nil {
		proto.dict = &dictProps{}
	}
	proto.dict.root = s
	return s
}

// sharedHostShapes caches process-wide the host shapes (hostShapeFor) that
// landed in the shared tree, so a shared realm converting a Go map with a
// known key set neither interns the keys nor keeps a cache of its own: an
// immutable snapshot replaced under sharedTransMu, with at most
// maxSharedHostShapes entries (the shapes themselves are bounded by the
// shared tree).
var (
	sharedHostShapes     atomic.Pointer[map[uint64]*hostShapeEntry]
	sharedHostShapeCount int
)

const maxSharedHostShapes = 4096

// sharedHostShapeChain returns the process-wide host-shape chain for hash h.
func sharedHostShapeChain(h uint64) *hostShapeEntry {
	if m := sharedHostShapes.Load(); m != nil {
		return (*m)[h]
	}
	return nil
}

// publishHostShape adds the shared shape for the sorted key list keys (hash
// h) to the process-wide host-shape cache and returns its entry there, or nil
// when the cache is full.
func publishHostShape(h uint64, keys []string, shape *Shape) *hostShapeEntry {
	sharedTransMu.Lock()
	defer sharedTransMu.Unlock()
	old := sharedHostShapes.Load()
	var chain *hostShapeEntry
	if old != nil {
		chain = (*old)[h]
	}
	for e := chain; e != nil; e = e.next {
		if slices.Equal(e.keys, keys) {
			return e
		}
	}
	if sharedHostShapeCount >= maxSharedHostShapes {
		return nil
	}
	next := make(map[uint64]*hostShapeEntry, sharedHostShapeCount+1)
	if old != nil {
		maps.Copy(next, *old)
	}
	e := &hostShapeEntry{keys: keys, shape: shape, next: chain}
	next[h] = e
	sharedHostShapes.Store(&next)
	sharedHostShapeCount++
	return e
}

// sharedRoots holds the process-wide root shapes of the shared intrinsics
// used as prototypes: an immutable snapshot, seeded with the template's roots
// (buildSharedTemplate) and replaced under sharedTransMu.
var sharedRoots atomic.Pointer[map[*Object]*Shape]

// sharedRootFor returns the shared root shape for a shared prototype.
func sharedRootFor(proto *Object) *Shape {
	if m := sharedRoots.Load(); m != nil {
		if s, ok := (*m)[proto]; ok {
			return s
		}
	}
	sharedTransMu.Lock()
	defer sharedTransMu.Unlock()
	old := sharedRoots.Load()
	if old != nil {
		if s, ok := (*old)[proto]; ok {
			return s
		}
	}
	next := make(map[*Object]*Shape, 16)
	if old != nil {
		maps.Copy(next, *old)
	}
	s := &Shape{proto: proto, key: rootKey, shared: true}
	next[proto] = s
	sharedRoots.Store(&next)
	return s
}

// addSharedRoots adds the roots a late group built (buildLateGroup) for its
// now shared prototypes to the shared roots, as buildSharedTemplate does for
// the template's; a prototype that has one already keeps it.
func addSharedRoots(roots map[*Object]*Shape) {
	sharedTransMu.Lock()
	defer sharedTransMu.Unlock()
	old := *sharedRoots.Load()
	var next map[*Object]*Shape
	for proto, root := range roots {
		if _, ok := old[proto]; ok || proto == nil || proto.flags&flagShared == 0 {
			continue
		}
		if next == nil {
			next = maps.Clone(old)
		}
		root.prepareShared()
		next[proto] = root
	}
	if next != nil {
		sharedRoots.Store(&next)
	}
}
