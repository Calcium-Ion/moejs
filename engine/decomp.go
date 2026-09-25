package engine

import (
	"encoding/binary"
	"slices"
	"sync"
)

// Character decomposition (Unicode 17.0.0, decomp_tables.go): one-level
// Decomposition_Mapping lookups, Canonical_Combining_Class and the
// algorithmic Hangul syllable decomposition. localeCompare builds its
// canonical-equivalence handling on these; String.prototype.normalize can
// reuse them.

// The compatibility formatting tags of ucdDecompositions, in the order of
// internal/gen/decomp's tags list; decompCanonical marks a canonical
// mapping.
const (
	decompCanonical = iota
	decompFont
	decompNoBreak
	decompInitial
	decompMedial
	decompFinal
	decompIsolated
	decompCircle
	decompSuper
	decompSub
	decompVertical
	decompWide
	decompNarrow
	decompSmall
	decompSquare
	decompFraction
	decompCompat
)

// Hangul syllable composition constants (Unicode §3.12).
const (
	hangulSBase  = 0xAC00
	hangulLBase  = 0x1100
	hangulVBase  = 0x1161
	hangulTBase  = 0x11A7
	hangulTCount = 28
	hangulNCount = 21 * hangulTCount
	hangulSCount = 19 * hangulNCount
)

type decompEntry struct {
	c   rune
	off uint16 // index of the mapping in decompTables.runes
	n   uint8
	tag uint8
}

type cccRun struct {
	lo, hi rune
	class  uint8
}

// decompTables is the decoded form of decomp_tables.go, built on first use
// and shared by every realm.
var decompTables struct {
	once    sync.Once
	entries []decompEntry
	runes   []rune
	ccc     []cccRun
}

func loadDecompTables() {
	d := &decompTables
	d.entries = make([]decompEntry, 0, ucdDecompositionCount)
	s := ucdDecompositions
	next := rune(0)
	for i := 0; i < len(s); {
		gap, n := binary.Uvarint([]byte(s[i:min(len(s), i+binary.MaxVarintLen32)]))
		i += n
		c := next + rune(gap)
		e := decompEntry{c: c, off: uint16(len(d.runes)), tag: s[i], n: s[i+1]}
		i += 2
		for range e.n {
			v, n := binary.Uvarint([]byte(s[i:min(len(s), i+binary.MaxVarintLen32)]))
			i += n
			d.runes = append(d.runes, rune(v))
		}
		d.entries = append(d.entries, e)
		next = c + 1
	}
	s = ucdCombiningClasses
	next = 0
	for i := 0; i < len(s); {
		gap, n := binary.Uvarint([]byte(s[i:min(len(s), i+binary.MaxVarintLen32)]))
		i += n
		length, m := binary.Uvarint([]byte(s[i:min(len(s), i+binary.MaxVarintLen32)]))
		i += m
		lo := next + rune(gap)
		hi := lo + rune(length)
		d.ccc = append(d.ccc, cccRun{lo: lo, hi: hi, class: s[i]})
		i++
		next = hi + 1
	}
}

// decomposition returns the one-level Decomposition_Mapping of c and its
// tag, or nil when c has none. Hangul syllables are not included; see
// appendHangulJamo. The slice is shared and must not be modified.
func decomposition(c rune) (uint8, []rune) {
	if c < 0xA0 {
		return 0, nil
	}
	decompTables.once.Do(loadDecompTables)
	d := &decompTables
	i, ok := slices.BinarySearchFunc(d.entries, c, func(e decompEntry, c rune) int { return int(e.c - c) })
	if !ok {
		return 0, nil
	}
	e := d.entries[i]
	return e.tag, d.runes[e.off : int(e.off)+int(e.n)]
}

// combiningClass returns the Canonical_Combining_Class of c.
func combiningClass(c rune) uint8 {
	if c < 0x300 {
		return 0
	}
	decompTables.once.Do(loadDecompTables)
	runs := decompTables.ccc
	i, ok := slices.BinarySearchFunc(runs, c, func(r cccRun, c rune) int {
		switch {
		case r.hi < c:
			return -1
		case r.lo > c:
			return 1
		}
		return 0
	})
	if !ok {
		return 0
	}
	return runs[i].class
}

// isHangulSyllable reports whether c is a precomposed Hangul syllable.
func isHangulSyllable(c rune) bool { return c >= hangulSBase && c < hangulSBase+hangulSCount }

// hangulJamo returns the canonical decomposition of the Hangul syllable c:
// its leading consonant, vowel and, when t is non-zero, trailing consonant.
func hangulJamo(c rune) (l, v, t rune) {
	s := c - hangulSBase
	l = hangulLBase + s/hangulNCount
	v = hangulVBase + (s%hangulNCount)/hangulTCount
	if ti := s % hangulTCount; ti != 0 {
		t = hangulTBase + ti
	}
	return l, v, t
}
