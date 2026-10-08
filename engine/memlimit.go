package engine

import (
	"errors"
	"slices"
	"strconv"
	"sync/atomic"
	"unsafe"

	"github.com/Calcium-Ion/moejs/bytecode"
)

// Memory limit (docs/DESIGN.md §4.4).
//
// A realm with a memory limit charges what it allocates for JavaScript to
// an account behind r.lazy: objects by the size of their struct, the
// storage of their properties and elements as it grows, strings by their
// header and units, Map and Set tables, ArrayBuffers, environments,
// suspended generator frames, BigInt results, the code eval and the
// Function constructors compile, and the wrappers FromGo and JSON.parse
// build. The count is approximate: a producer charges what it asks the Go
// allocator for, not the size class it gets, and the places that grow a
// slice without one of the charge points below are not counted, nor are
// the register stack and frame table, which the call depth limit bounds. Nothing is ever credited back: the account counts
// allocation since the last reset (ReleaseCallData, ResetAllocation), and
// live memory is at most that.
//
// When lastLive plus the bytes allocated since the last reset pass the
// limit, the realm interrupts itself with a *MemoryLimitError. Allocation
// cannot fail (initObject and initArray return no error), and an
// interrupt unwinds without running any catch or finally and drops the
// queued jobs, so a script cannot catch the hit and keep allocating. The
// producers that know a result's size before they allocate it, and throw
// a RangeError for a size past the engine's limits (String.prototype.repeat,
// new ArrayBuffer, a BigInt shift), throw the same RangeError for a size
// past what is left of the budget, which the script can catch.
//
// With no limit set r.lazy.mem is nil, and a charge point costs the loads
// of r.lazy and its mem field: the account, and every counter it keeps,
// exists only while a limit is set.

// ErrMemoryLimit is the error a *MemoryLimitError unwraps to, so that
// errors.Is finds it through the *InterruptedError a hit stops the realm
// with.
var ErrMemoryLimit = errors.New("memory limit exceeded")

// MemoryLimitError is the value a realm interrupts itself with when what
// it allocated since the last reset passes its memory limit.
type MemoryLimitError struct {
	// Limit is the limit that was passed, in bytes.
	Limit int64
	// Allocated is what was charged since the last reset, the allocation
	// that passed the limit included.
	Allocated int64
	// Live is the retained size the last measurement of the realm's heap
	// found, which the limit counts with Allocated; 0 while none was made.
	Live int64
	// Stack is the JavaScript stack at the allocation, the "    at ..."
	// lines of an Error's stack; "" when no JavaScript ran.
	Stack string
}

// Error implements error.
func (e *MemoryLimitError) Error() string {
	return "memory limit exceeded: " + strconv.FormatInt(e.Live+e.Allocated, 10) + " bytes allocated, limit " + strconv.FormatInt(e.Limit, 10)
}

// Unwrap returns ErrMemoryLimit.
func (e *MemoryLimitError) Unwrap() error { return ErrMemoryLimit }

// Stats are counters of a realm, each read in constant time. The
// allocation counters (AllocatedBytes to MemoryLimitHits) count only while
// a memory limit is set: they are the limit's account, which a realm
// without a limit does not keep. A host that wants them without a limit
// sets one it never reaches (math.MaxInt64).
type Stats struct {
	// AllocatedBytes is what was charged since the limit was set.
	AllocatedBytes int64
	// RequestAllocatedBytes is what was charged since the last reset
	// (ReleaseCallData, ResetAllocation), which the limit bounds.
	RequestAllocatedBytes int64
	// Objects, Strings and Shapes count the objects, strings and shapes
	// charged since the limit was set.
	Objects int64
	Strings int64
	Shapes  int64
	// MemoryLimitHits counts the times the limit stopped the realm.
	MemoryLimitHits int64
	// LastMemoryLimitError is the error of the last hit since the last
	// reset, or nil. The interrupt a hit raises is the realm's single
	// pending interrupt, which a concurrent Interrupt can replace (a host's
	// timeout): the hit is still counted, and its error kept, here.
	LastMemoryLimitError *MemoryLimitError
	// Interrupts counts the calls of Interrupt, memory limit hits included.
	Interrupts int64
	// ICEntries is the number of inline cache entries of the functions the
	// realm has run.
	ICEntries int
	// PendingJobs is the number of queued promise jobs and microtasks.
	PendingJobs int
	// RegisterStackBytes is the size of the interpreter's register stack.
	RegisterStackBytes int64
}

// memKind is what a charge counts besides its bytes.
type memKind uint8

const (
	memBytes memKind = iota
	memObject
	memString
	memShape
	memKinds
)

// memAccount is the account of a realm with a memory limit (r.lazy.mem).
// Only the goroutine running the realm uses it, but flat.
type memAccount struct {
	limit     int64
	allocated int64 // charged since the last reset
	// lastLive is the retained size the last idle measurement of the heap
	// found. Nothing measures it yet; the limit counts it with allocated so
	// that one can.
	lastLive int64
	total    int64 // charged since the limit was set
	counts   [memKinds]int64
	hits     int64
	last     *MemoryLimitError // the last hit's, since the last reset
	hitting  bool              // memoryLimitHit runs: its own charges do not hit again
	// flat is what flattening this realm's ropes allocated since the last
	// charge: a rope flattens wherever it is first read, which may be on
	// another goroutine than the realm's (a value the host kept), so the
	// rope (ropeNode) adds here atomically and the next charge takes it
	// over.
	flat atomic.Int64
}

// Sizes charged for storage that grows.
const (
	// stringHeaderSize is what a String costs besides its units.
	stringHeaderSize = int(unsafe.Sizeof(String{}))
	// valueSize is a slot, an element or a register.
	valueSize = int(unsafe.Sizeof(Value{}))
	// dictEntrySize is a named property of an object in dictionary mode:
	// its entry and its index entry.
	dictEntrySize = int(unsafe.Sizeof(dictEntry{})) + 32
	// sparseEntrySize is an element outside dense storage, a map entry.
	sparseEntrySize = int(unsafe.Sizeof(propCell{})) + 16
	// shapeSize is a shape and the record of its transition.
	shapeSize = int(unsafe.Sizeof(Shape{})) + 32
)

// appendValue appends v to s as append does, charging the storage when s
// grows.
func (r *Realm) appendValue(s []Value, v Value) []Value {
	if len(s) == cap(s) {
		s = r.growValues(s, 1)
	}
	return append(s, v)
}

// growValues returns s with room for n more values, grown as append grows
// it, and charges the new storage.
func (r *Realm) growValues(s []Value, n int) []Value {
	s = slices.Grow(s, n)
	r.charge(cap(s) * valueSize)
	return s
}

// SetMemoryLimit sets the number of bytes the realm may allocate for
// JavaScript between resets (ReleaseCallData, ResetAllocation) before it
// interrupts itself with a *MemoryLimitError; n <= 0 removes the limit and
// its counters. Changing a limit keeps what was counted.
func (r *Realm) SetMemoryLimit(n int64) {
	if n <= 0 {
		if r.lazy != nil {
			r.lazy.mem = nil
		}
		return
	}
	lz := r.lazyState()
	if lz.mem == nil {
		lz.mem = &memAccount{}
	}
	lz.mem.limit = n
}

// MemoryLimit returns the realm's memory limit, 0 for none.
func (r *Realm) MemoryLimit() int64 {
	if m := r.mem(); m != nil {
		return m.limit
	}
	return 0
}

// ResetAllocation starts a new budget: the bytes charged since the last
// reset go back to zero, and the last hit's error is forgotten. A pending
// interrupt, the hit's included, stays pending (ClearInterrupt).
// ReleaseCallData resets too; a host that does not pool calls this where a
// request starts. It may be called from a host function, which gives the
// running script a new budget.
func (r *Realm) ResetAllocation() {
	m := r.mem()
	if m == nil {
		return
	}
	if f := m.flat.Swap(0); f != 0 {
		m.total += f
	}
	m.allocated = 0
	m.last = nil
}

// Stats returns the realm's counters. Like every method but Interrupt and
// ClearInterrupt, it runs on the goroutine using the realm, and it may be
// called from a host function.
func (r *Realm) Stats() Stats {
	s := Stats{
		ICEntries:          len(r.ic),
		RegisterStackBytes: int64(len(r.interp.stack)) * int64(unsafe.Sizeof(Value{})),
	}
	if p := r.interruptValue.Load(); p != nil {
		s.Interrupts = p.n
	}
	lz := r.lazy
	if lz == nil {
		return s
	}
	if j := lz.jobs; j != nil {
		s.PendingJobs = len(j.queue) - j.head
	}
	if m := lz.mem; m != nil {
		f := m.flat.Load()
		s.AllocatedBytes = m.total + f
		s.RequestAllocatedBytes = m.allocated + f
		s.Objects = m.counts[memObject]
		s.Strings = m.counts[memString]
		s.Shapes = m.counts[memShape]
		s.MemoryLimitHits = m.hits
		s.LastMemoryLimitError = m.last
	}
	return s
}

// mem returns the realm's memory account, nil while no limit is set.
func (r *Realm) mem() *memAccount {
	if lz := r.lazy; lz != nil {
		return lz.mem
	}
	return nil
}

// charge charges n bytes of storage to the realm's memory account. With no
// limit set it costs two loads and two branches; the account is updated
// out of line, so callers keep their inlining.
func (r *Realm) charge(n int) {
	if lz := r.lazy; lz != nil && lz.mem != nil {
		r.chargeSlow(n, memBytes)
	}
}

// chargeObject charges an object of n bytes (its struct and what is
// allocated with it).
func (r *Realm) chargeObject(n uintptr) {
	if lz := r.lazy; lz != nil && lz.mem != nil {
		r.chargeSlow(int(n), memObject)
	}
}

// chargeString charges a string whose units take n bytes besides its
// header, and the header.
func (r *Realm) chargeString(n int) {
	if lz := r.lazy; lz != nil && lz.mem != nil {
		r.chargeSlow(stringHeaderSize+n, memString)
	}
}

// reserve charges n bytes its caller is about to allocate and returns the
// interrupt's error when the charge passed the limit, or a host
// interrupted, so that the caller stops before it allocates them.
func (r *Realm) reserve(n int) error {
	if lz := r.lazy; lz != nil && lz.mem != nil {
		r.chargeSlow(n, memBytes)
		return r.CheckInterrupt()
	}
	return nil
}

// chargeShape charges a shape of the realm's own tree and its transition
// record.
func (r *Realm) chargeShape() {
	if lz := r.lazy; lz != nil && lz.mem != nil {
		r.chargeSlow(shapeSize, memShape)
	}
}

// overBudget reports whether n more bytes would pass the memory limit:
// a producer that knows its size before it allocates throws its RangeError
// rather than allocate them (see the file comment).
func (r *Realm) overBudget(n int) bool {
	if lz := r.lazy; lz != nil && lz.mem != nil {
		return r.overBudgetSlow(n)
	}
	return false
}

//go:noinline
func (r *Realm) overBudgetSlow(n int) bool {
	m := r.lazy.mem
	return m.lastLive+m.allocated+m.flat.Load()+int64(n) > m.limit
}

// chargeSlow adds n bytes of kind to the account and interrupts the realm
// when they pass the limit.
//
//go:noinline
func (r *Realm) chargeSlow(size int, kind memKind) {
	m := r.lazy.mem
	n := int64(size)
	if m.flat.Load() != 0 {
		n += m.flat.Swap(0)
	}
	m.allocated += n
	m.total += n
	m.counts[kind]++
	if m.lastLive+m.allocated > m.limit {
		r.memoryLimitHit(m)
	}
}

// chargeMany is chargeSlow for count objects or strings of size bytes in
// all.
func (r *Realm) chargeMany(size int, kind memKind, count int) {
	r.chargeSlow(size, kind)
	r.lazy.mem.counts[kind] += int64(count - 1)
}

// memoryLimitHit interrupts the realm with a *MemoryLimitError carrying
// the JavaScript stack. While an interrupt is pending, a host's or an
// earlier hit's, the realm is stopping already, so it does nothing; once
// the host clears it, the next charge past the limit hits again until a
// reset.
func (r *Realm) memoryLimitHit(m *memAccount) {
	if m.hitting || r.interruptFlag.Load() != 0 {
		return
	}
	m.hitting = true
	e := &MemoryLimitError{Limit: m.limit, Allocated: m.allocated, Live: m.lastLive}
	// The interpreter stores each frame's pc before the instruction runs,
	// so the stack is exact at any allocation. Formatting reads data
	// properties only and runs no JavaScript.
	if frames := captureStackInto(r, nil, nil, defaultStackTraceLimit); len(frames) != 0 {
		e.Stack = formatStack(r, frames)
	}
	m.hitting = false
	m.hits++
	m.last = e
	r.Interrupt(e)
}

// substring is s.Substring charging the String it makes: a substring of
// an ASCII string shares its bytes, one of a UTF-16 string copies its
// units.
func (r *Realm) substring(s *String, start, end int) *String {
	if lz := r.lazy; lz != nil && lz.mem != nil && start < end && (start > 0 || end < s.Len()) {
		// A rope's kind never changes: its contents are read through a
		// view (flat), which only its kind is read of, so it stays on the
		// stack.
		f := s
		if s.kind == strRope {
			f = s.flat(new(String))
		}
		n := 0
		if f.kind == strUTF16 {
			n = 2 * (end - start)
		}
		r.chargeSlow(stringHeaderSize+n, memString)
	}
	return s.Substring(start, end)
}

// compiledSize estimates the memory of code compiled at run time and of
// the meta tree the realm keeps for it: its functions, instructions,
// constants, handlers and line tables.
func compiledSize(fn *bytecode.Function) int {
	n := int(unsafe.Sizeof(bytecode.Function{})) + int(unsafe.Sizeof(funcMeta{})) +
		4*len(fn.Code) + int(unsafe.Sizeof(bytecode.Const{}))*len(fn.Consts) +
		int(unsafe.Sizeof(bytecode.Handler{}))*len(fn.Handlers) + int(unsafe.Sizeof(bytecode.LineEntry{}))*len(fn.LineTable)
	for _, c := range fn.Children {
		n += compiledSize(c)
	}
	return n
}

// ropeNode is a rope of a realm with a memory limit: the String and the
// account its flattening charges, in one allocation (56 bytes, the 64-byte
// class) marked strCharged. A rope is flattened wherever it is first read,
// on any goroutine, where no realm is at hand. The flag and m are written
// once, before the rope is published, and never again, so two goroutines
// flattening one rope (a race of its own, on the String's fields) at worst
// both charge the right account.
//
// Undercounts accepted: a rope made before the realm had a limit is a plain
// String and its flattening is not charged; one made under an account that
// SetMemoryLimit(0) then dropped charges that account, which nothing reads.
type ropeNode struct {
	s String
	m *memAccount
}

// newChargedRope is Concat's rope of a and b, n units long, for a realm
// with an account: a ropeNode, charged its size.
func (r *Realm) newChargedRope(a, b *String, n int) *String {
	x := &ropeNode{
		s: String{p: unsafe.Pointer(a), right: b, n: int32(n), kind: strRope, flags: strCharged},
		m: r.lazy.mem,
	}
	r.chargeString(int(unsafe.Sizeof(ropeNode{})) - stringHeaderSize)
	return &x.s
}

// chargeFlatten charges the n bytes the flattening of the rope s allocates
// to the account of the realm that made it, if it had a limit (ropeNode).
// It may run on any goroutine.
func (s *String) chargeFlatten(n int) {
	if s.flags&strCharged != 0 {
		(*ropeNode)(unsafe.Pointer(s)).m.flat.Add(int64(n))
	}
}

// ChargeMemory charges n bytes a host function allocates for JavaScript,
// such as a buffer it hands to NewArrayBuffer, to the realm's memory limit
// (none: a no-op). It returns the *InterruptedError of a pending interrupt,
// which the charge raises when it passes the limit, so that the host
// function can return it before it allocates.
func (r *Realm) ChargeMemory(n int) error {
	if n <= 0 {
		return nil
	}
	return r.reserve(n)
}
