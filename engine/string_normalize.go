package engine

import (
	"slices"
	"sync"
	"unicode/utf16"
)

//go:generate go run ../internal/gen/ucd -norm unicode_norm_tables.go -normtest testdata/normalization_test.txt.gz

// String.prototype.normalize (ES2024 §22.1.3.15): the Unicode Normalization
// Forms of UAX #15 over the tables in unicode_norm_tables.go, generated from
// the same pinned UCD version as the regexp tables (never Go's or x/text's
// data). The tables are decoded on first use and shared by the process;
// realms carry nothing.
//
// A string the quick check (UAX #15 §9) accepts is returned as is: ASCII
// strings always are, other strings are scanned once without allocating.
// Otherwise every code point is replaced by its full canonical or
// compatibility decomposition, every run of non-starters is sorted stably by
// combining class, and NFC and NFKC then compose canonically. The work is
// done one segment (a starter and its non-starters) at a time straight into
// the result, so memory stays proportional to the result. Lone surrogates are
// code points of class 0 without mappings and pass through unchanged.

// normForm is a normalization form.
type normForm uint8

const (
	formNFC normForm = iota
	formNFD
	formNFKC
	formNFKD
)

var normFormNames = [...]string{"NFC", "NFD", "NFKC", "NFKD"}

func (f normForm) compat() bool  { return f >= formNFKC }
func (f normForm) compose() bool { return f == formNFC || f == formNFKC }

// Quick-check values.
const (
	qcYes uint8 = iota
	qcMaybe
	qcNo
)

// Hangul syllables decompose and compose algorithmically (Unicode §3.12);
// the other constants are in decomp.go.
const (
	hangulLCount = 19
	hangulVCount = 21
)

// normEntry is the normalization data of one code point; code points without
// an entry have class 0, no decomposition and pass every quick check.
type normEntry struct {
	ccc     uint8
	qc      uint8  // two bits per form, in normForm order
	canonN  uint8  // length of the full canonical decomposition, 0 for none
	compatN uint8  // length of the full compatibility decomposition, 0 for the canonical one
	off     uint32 // index of the decompositions in normTables.chars
}

func (e normEntry) quick(f normForm) uint8 { return e.qc >> (2 * f) & 3 }

// normTables is the decoded form of the generated tables. Decompositions are
// stored as normChars.
type normTables struct {
	entries  map[rune]normEntry
	chars    []uint32
	compose  map[uint64]rune // primary composites by first<<21 | second
	minCheck [4]rune         // code points below pass the quick check of the form with class 0
}

// A normChar packs a code point and its combining class: c | ccc<<24.
func normChar(c rune, ccc uint8) uint32 { return uint32(c) | uint32(ccc)<<24 }

var (
	normOnce sync.Once
	normData normTables
)

// normalization returns the decoded normalization tables.
func normalization() *normTables {
	normOnce.Do(normData.decode)
	return &normData
}

// uvarintAt decodes the uvarint at s[i] and returns it with the index after
// it.
func uvarintAt(s string, i int) (uint64, int) {
	var v uint64
	for shift := 0; ; shift += 7 {
		b := s[i]
		i++
		v |= uint64(b&0x7F) << shift
		if b < 0x80 {
			return v, i
		}
	}
}

// fullDecomposition appends the full canonical (compat false) or
// compatibility decomposition of c to dst: the one-level mappings of
// decomp_tables.go applied recursively, Hangul syllables included.
func fullDecomposition(dst []rune, c rune, compat bool) []rune {
	if isHangulSyllable(c) {
		l, v, t := hangulJamo(c)
		dst = append(dst, l, v)
		if t != 0 {
			dst = append(dst, t)
		}
		return dst
	}
	tag, m := decomposition(c)
	if m == nil || tag != decompCanonical && !compat {
		return append(dst, c)
	}
	for _, x := range m {
		dst = fullDecomposition(dst, x, compat)
	}
	return dst
}

func (t *normTables) decode() {
	decompTables.once.Do(loadDecompTables)
	d := &decompTables
	t.entries = make(map[rune]normEntry, 7000)
	for _, run := range d.ccc {
		for c := run.lo; c <= run.hi; c++ {
			t.entries[c] = normEntry{ccc: run.class}
		}
	}
	var canon, compat []rune
	for _, de := range d.entries {
		canon, compat = canon[:0], fullDecomposition(compat[:0], de.c, true)
		if de.tag == decompCanonical {
			canon = fullDecomposition(canon, de.c, false)
		}
		e := t.entries[de.c]
		e.off = uint32(len(t.chars))
		e.canonN = uint8(len(canon))
		if !slices.Equal(canon, compat) {
			e.compatN = uint8(len(compat))
		}
		for _, x := range canon {
			t.chars = append(t.chars, uint32(x)) // classes filled in below
		}
		if e.compatN != 0 {
			for _, x := range compat {
				t.chars = append(t.chars, uint32(x))
			}
		}
		if e.canonN != 0 {
			e.qc |= qcNo << (2 * formNFD)
		}
		e.qc |= qcNo << (2 * formNFKD)
		t.entries[de.c] = e
	}
	for i, x := range t.chars {
		t.chars[i] = normChar(rune(x), t.entries[rune(x)].ccc)
	}
	for _, tab := range ucdQuickCheck {
		var f normForm
		var v uint8
		switch tab.name {
		case "NFC_QC=M":
			f, v = formNFC, qcMaybe
		case "NFC_QC=N":
			f, v = formNFC, qcNo
		case "NFKC_QC=M":
			f, v = formNFKC, qcMaybe
		case "NFKC_QC=N":
			f, v = formNFKC, qcNo
		default:
			panic("normalize: unknown quick-check table " + tab.name)
		}
		set := decodeRanges(tab.data)
		for j := 0; j < len(set); j += 2 {
			for c := set[j]; c <= set[j+1]; c++ {
				e := t.entries[c]
				e.qc |= v << (2 * f)
				t.entries[c] = e
			}
		}
	}
	t.compose = make(map[uint64]rune, 1024)
	s := ucdCompositions
	next := rune(0)
	for i := 0; i < len(s); {
		var gap, a, b uint64
		gap, i = uvarintAt(s, i)
		a, i = uvarintAt(s, i)
		b, i = uvarintAt(s, i)
		c := next + rune(gap)
		next = c + 1
		t.compose[a<<21|b] = c
	}
	for f := range t.minCheck {
		m := rune(maxCodePoint + 1)
		for c, e := range t.entries {
			if c < m && (e.ccc != 0 || e.quick(normForm(f)) != qcYes) {
				m = c
			}
		}
		t.minCheck[f] = m
	}
}

// entry returns the data of c.
func (t *normTables) entry(c rune) normEntry {
	if isHangulSyllable(c) {
		return normEntry{qc: qcNo<<(2*formNFD) | qcNo<<(2*formNFKD)}
	}
	return t.entries[c]
}

// quickCheck is the quick check of UAX #15 §9.1 over u; interrupts are
// checked every interruptStride code points.
func (t *normTables) quickCheck(r *Realm, u []uint16, f normForm) (uint8, error) {
	result := qcYes
	var last uint8
	lo := t.minCheck[f]
	for i, k := 0, int64(0); i < len(u); k++ {
		if err := interruptEvery(r, k); err != nil {
			return 0, err
		}
		c, n := decodeUnitAt(u, i)
		i += n
		if c < lo {
			last = 0
			continue
		}
		e := t.entry(c)
		if e.ccc != 0 && last > e.ccc {
			return qcNo, nil
		}
		switch e.quick(f) {
		case qcNo:
			return qcNo, nil
		case qcMaybe:
			result = qcMaybe
		}
		last = e.ccc
	}
	return result, nil
}

// composePair returns the primary composite of a and b.
func (t *normTables) composePair(a, b rune) (rune, bool) {
	switch {
	case a >= hangulLBase && a < hangulLBase+hangulLCount && b >= hangulVBase && b < hangulVBase+hangulVCount:
		return hangulSBase + ((a-hangulLBase)*hangulVCount+b-hangulVBase)*hangulTCount, true
	case isHangulSyllable(a) && (a-hangulSBase)%hangulTCount == 0 && b > hangulTBase && b < hangulTBase+hangulTCount:
		return a + b - hangulTBase, true
	}
	c, ok := t.compose[uint64(a)<<21|uint64(b)]
	return c, ok
}

// normalizer builds a normalized string one segment at a time.
type normalizer struct {
	t    *normTables
	form normForm
	out  []uint16
	seg  []uint32 // pending normChars
}

// add appends one normChar of the full decomposition.
func (z *normalizer) add(ch uint32) {
	if ch>>24 == 0 && len(z.seg) > 0 {
		z.flush(false)
	}
	z.seg = append(z.seg, ch)
}

// flush orders, composes and writes the pending chars. Unless final, a
// trailing starter stays pending: the next starter may compose with it.
func (z *normalizer) flush(final bool) {
	seg := z.seg
	sortNonStarters(seg)
	keep := 0
	if z.form.compose() {
		seg = z.t.composeInPlace(seg)
		if !final && seg[len(seg)-1]>>24 == 0 {
			keep = 1
		}
	}
	for _, ch := range seg[:len(seg)-keep] {
		c := rune(ch & 0xFFFFFF)
		if c < 0x10000 {
			z.out = append(z.out, uint16(c))
		} else {
			hi, lo := utf16.EncodeRune(c)
			z.out = append(z.out, uint16(hi), uint16(lo))
		}
	}
	z.seg = append(z.seg[:0], seg[len(seg)-keep:]...)
}

// sortNonStarters sorts every run of non-starters stably by class
// (canonical ordering).
func sortNonStarters(seg []uint32) {
	for i := 0; i < len(seg); {
		if seg[i]>>24 == 0 {
			i++
			continue
		}
		j, sorted := i+1, true
		for ; j < len(seg) && seg[j]>>24 != 0; j++ {
			if seg[j-1]>>24 > seg[j]>>24 {
				sorted = false
			}
		}
		if !sorted {
			slices.SortStableFunc(seg[i:j], func(a, b uint32) int { return int(a>>24) - int(b>>24) })
		}
		i = j
	}
}

// composeInPlace is the canonical composition algorithm (UAX #15 §3.11 /
// D117) over canonically ordered chars: each char composes with the last
// starter when nothing between them blocks it. The kept chars after the
// starter are in class order, so only the last one can block.
func (t *normTables) composeInPlace(seg []uint32) []uint32 {
	out := seg[:0]
	starter := -1
	for _, ch := range seg {
		ccc := uint8(ch >> 24)
		if starter >= 0 && (starter == len(out)-1 || uint8(out[len(out)-1]>>24) < ccc) {
			if p, ok := t.composePair(rune(out[starter]&0xFFFFFF), rune(ch&0xFFFFFF)); ok {
				out[starter] = normChar(p, 0)
				continue
			}
		}
		if ccc == 0 {
			starter = len(out)
		}
		out = append(out, ch)
	}
	return out
}

// normalize returns the code units of u in form f; the length is limited to
// maxStringLength.
func (t *normTables) normalize(r *Realm, u []uint16, f normForm) ([]uint16, error) {
	z := normalizer{t: t, form: f, out: make([]uint16, 0, len(u)+len(u)/8)}
	var buf [8]uint32
	z.seg = buf[:0]
	df := f | 1 // the decomposition of NFC is NFD's, of NFKC NFKD's
	lo := t.minCheck[df]
	for i, k := 0, int64(0); i < len(u); k++ {
		c, n := decodeUnitAt(u, i)
		i += n
		switch {
		case c < lo:
			z.add(uint32(c))
		case isHangulSyllable(c):
			s := c - hangulSBase
			z.add(uint32(hangulLBase + s/hangulNCount))
			z.add(uint32(hangulVBase + s%hangulNCount/hangulTCount))
			if tt := s % hangulTCount; tt != 0 {
				z.add(uint32(hangulTBase + tt))
			}
		default:
			e := t.entries[c]
			d := t.chars[e.off : e.off+uint32(e.canonN)]
			if f.compat() && e.compatN != 0 {
				d = t.chars[e.off+uint32(e.canonN) : e.off+uint32(e.canonN)+uint32(e.compatN)]
			}
			if len(d) == 0 {
				z.add(normChar(c, e.ccc))
			}
			for _, ch := range d {
				z.add(ch)
			}
		}
		if err := interruptEvery(r, k); err != nil {
			return nil, err
		}
		if len(z.out) > maxStringLength {
			return nil, r.invalidStringLength()
		}
	}
	if len(z.seg) > 0 {
		z.flush(true)
	}
	if len(z.out) > maxStringLength {
		return nil, r.invalidStringLength()
	}
	return z.out, nil
}

// normalizeString returns s itself when it is already in form f.
func (r *Realm) normalizeString(s *String, f normForm) (*String, error) {
	if _, ok := s.ASCII(); ok {
		return s, nil
	}
	t := normalization()
	u := s.UTF16()
	qc, err := t.quickCheck(r, u, f)
	if err != nil || qc == qcYes {
		return s, err
	}
	out, err := t.normalize(r, u, f)
	if err != nil {
		return nil, err
	}
	if slices.Equal(out, u) {
		return s, nil
	}
	return FromUTF16(out), nil
}

func stringProtoNormalize(r *Realm, this Value, args []Value) (Value, error) {
	s, err := thisStringValue(r, this, "normalize")
	if err != nil {
		return Undefined(), err
	}
	f := formNFC
	if v := Arg(args, 0); !v.IsUndefined() {
		name, err := r.ToString(v)
		if err != nil {
			return Undefined(), err
		}
		i := slices.IndexFunc(normFormNames[:], name.EqualsGoString)
		if i < 0 {
			return Undefined(), r.RangeError("The normalization form should be one of NFC, NFD, NFKC, NFKD.")
		}
		f = normForm(i)
	}
	out, err := r.normalizeString(s, f)
	if err != nil {
		return Undefined(), err
	}
	return StringValue(out), nil
}
