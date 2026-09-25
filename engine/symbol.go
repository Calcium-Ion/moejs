package engine

import (
	"runtime"
	"sync"
	"weak"
)

// Symbol is a JavaScript symbol. Symbols are immutable after creation, so a
// symbol value may be shared by any number of realms and goroutines.
type Symbol struct {
	description *String
	// registered marks the symbols of the global registry (Symbol.for).
	// They cannot be held weakly (CanBeHeldWeakly): a registered symbol is
	// recreated on demand, so it has no identity a weak collection could
	// observe the loss of.
	registered bool
	// wellKnown marks the process-wide well-known symbols, which no realm
	// may write: weak collections keep their entries for them on the
	// collection (builtin_weak.go).
	wellKnown bool
	// weak holds the entries of the weak collections keyed by this symbol
	// (builtin_weak.go).
	weak *weakSide
}

// NewSymbol creates a fresh symbol with the given description (may be nil).
func NewSymbol(description *String) *Symbol { return &Symbol{description: description} }

// Description returns the description or nil.
func (s *Symbol) Description() *String { return s.description }

func (s *Symbol) descriptiveString() string {
	if s.description == nil {
		return "Symbol()"
	}
	return "Symbol(" + s.description.GoString() + ")"
}

// descriptiveJSString implements SymbolDescriptiveString, keeping the
// description's code units as they are.
func (s *Symbol) descriptiveJSString() *String {
	var sb StringBuilder
	sb.WriteGoString("Symbol(")
	if s.description != nil {
		sb.WriteString(s.description)
	}
	sb.WriteGoString(")")
	return sb.String()
}

// wellKnownSymbol creates one of the well-known symbols below.
func wellKnownSymbol(description *String) *Symbol {
	return &Symbol{description: description, wellKnown: true}
}

// Well-known symbols shared by all realms. They are immutable, so sharing is
// safe; their descriptions are static atoms (flat, hashed, read-only).
var (
	SymAsyncIterator      = wellKnownSymbol(staticAtom("Symbol.asyncIterator"))
	SymHasInstance        = wellKnownSymbol(staticAtom("Symbol.hasInstance"))
	SymIsConcatSpreadable = wellKnownSymbol(staticAtom("Symbol.isConcatSpreadable"))
	SymIterator           = wellKnownSymbol(staticAtom("Symbol.iterator"))
	SymMatch              = wellKnownSymbol(staticAtom("Symbol.match"))
	SymMatchAll           = wellKnownSymbol(staticAtom("Symbol.matchAll"))
	SymReplace            = wellKnownSymbol(staticAtom("Symbol.replace"))
	SymSearch             = wellKnownSymbol(staticAtom("Symbol.search"))
	SymSpecies            = wellKnownSymbol(staticAtom("Symbol.species"))
	SymSplit              = wellKnownSymbol(staticAtom("Symbol.split"))
	SymToPrimitive        = wellKnownSymbol(staticAtom("Symbol.toPrimitive"))
	SymToStringTag        = wellKnownSymbol(staticAtom("Symbol.toStringTag"))
	SymUnscopables        = wellKnownSymbol(staticAtom("Symbol.unscopables"))
)

// --- global symbol registry ---------------------------------------------------

// The GlobalSymbolRegistry is agent-wide in the spec; here it is
// process-wide, like the intern table (intern.go), so Symbol.for returns
// the same symbol in every realm, as it does across the realms of one V8
// isolate. It is keyed by the description's atom and holds its symbols
// weakly: a registered symbol nothing references any more is collected
// and a cleanup drops its entry, so a script calling Symbol.for with fresh
// keys in a loop does not grow the process. Dropping an unreferenced entry
// is unobservable: nothing holds the old symbol to compare with, and
// registered symbols cannot be WeakMap keys or WeakRef targets.
var symbolRegistry sync.Map // *String (atom) -> weak.Pointer[Symbol]

type symbolRegistryEntry struct {
	key *String
	wp  weak.Pointer[Symbol]
}

// registeredSymbol implements the lookup-or-create step of Symbol.for for an
// interned key.
func registeredSymbol(key *String) *Symbol {
	for {
		old, ok := symbolRegistry.Load(key)
		if ok {
			if s := old.(weak.Pointer[Symbol]).Value(); s != nil {
				return s
			}
		}
		s := &Symbol{description: key, registered: true}
		wp := weak.Make(s)
		if ok {
			if !symbolRegistry.CompareAndSwap(key, old, wp) {
				continue
			}
		} else if _, loaded := symbolRegistry.LoadOrStore(key, wp); loaded {
			continue
		}
		runtime.AddCleanup(s, func(e symbolRegistryEntry) {
			symbolRegistry.CompareAndDelete(e.key, e.wp)
		}, symbolRegistryEntry{key, wp})
		return s
	}
}
