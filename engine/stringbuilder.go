package engine

import "unicode/utf16"

// StringBuilder accumulates code units and produces a *String. It stores
// ASCII bytes until the first unit >= 0x80 is written, then upgrades to
// UTF-16 storage once.
//
// The first smallASCIIMax ASCII bytes are written into an array inside the
// builder itself, addressed by index and never through a slice, so a
// builder declared as a local variable stays on the stack (nothing points
// into it), builds a short result without touching the heap, and String
// then hands out one co-allocated string. Longer results spill into a heap
// slice that the result aliases, as before.
type StringBuilder struct {
	b      []byte   // heap ASCII storage once the inline array overflowed; nil before
	u      []uint16 // UTF-16 storage after the upgrade; nil before
	n      int      // bytes used in inline while b == nil && u == nil
	inline [smallASCIIMax]byte
}

// Grow reserves capacity for n more code units.
func (sb *StringBuilder) Grow(n int) {
	if sb.u != nil {
		if cap(sb.u)-len(sb.u) < n {
			nu := make([]uint16, len(sb.u), 2*cap(sb.u)+n)
			copy(nu, sb.u)
			sb.u = nu
		}
		return
	}
	if sb.b == nil {
		if sb.n+n > len(sb.inline) {
			sb.spill(sb.n + n)
		}
		return
	}
	if cap(sb.b)-len(sb.b) < n {
		nb := make([]byte, len(sb.b), 2*cap(sb.b)+n)
		copy(nb, sb.b)
		sb.b = nb
	}
}

// spill moves the inline bytes to a heap slice of capacity c.
func (sb *StringBuilder) spill(c int) {
	nb := make([]byte, sb.n, c)
	copy(nb, sb.inline[:sb.n])
	sb.b = nb
	sb.n = 0
}

// growFor reserves n code units in the storage kind of s, so that copying
// from a UTF-16 string never goes through the ASCII buffer and an upgrade.
func (sb *StringBuilder) growFor(s *String, n int) {
	if s.kind == strRope {
		s.flatten()
	}
	if s.kind == strUTF16 && sb.u == nil && sb.b == nil && sb.n == 0 {
		sb.u = make([]uint16, 0, n)
		return
	}
	sb.Grow(n)
}

// checkLength enforces the string length limit on a builder: producers
// whose output can outgrow their inputs (join, replace, JSON.stringify, case
// conversion) call it after each unbounded append, so the limit is raised
// as RangeError: Invalid string length before String would build a *String
// whose int32 length could not hold the result.
func (sb *StringBuilder) checkLength(r *Realm) error {
	if sb.Len() > maxStringLength {
		return r.stringTooLong()
	}
	return nil
}

// stringTooLong is invalidStringLength kept out of line, so that
// checkLength inlines into the loops that call it.
//
//go:noinline
func (r *Realm) stringTooLong() error { return r.invalidStringLength() }

// Len returns the number of code units written so far. Only one of the
// three stores holds them (spill and upgrade empty the one they leave), so
// the sum is the length of that one, without branches.
func (sb *StringBuilder) Len() int {
	return len(sb.u) + len(sb.b) + sb.n
}

// ascii returns the ASCII bytes written so far (valid only while u == nil;
// the slice must not be retained across a write).
func (sb *StringBuilder) ascii() []byte {
	if sb.b != nil {
		return sb.b
	}
	return sb.inline[:sb.n]
}

func (sb *StringBuilder) upgrade() {
	if sb.u != nil {
		return
	}
	src := sb.ascii()
	u := make([]uint16, len(src), max(2*len(src), 16))
	for i, c := range src {
		u[i] = uint16(c)
	}
	sb.u = u
	sb.b = nil
	sb.n = 0
}

// WriteUnit appends one UTF-16 code unit.
func (sb *StringBuilder) WriteUnit(c uint16) {
	if sb.u == nil {
		if c < 0x80 {
			sb.WriteASCII(byte(c))
			return
		}
		sb.upgrade()
	}
	sb.u = append(sb.u, c)
}

// WriteASCII appends one byte that must be < 0x80.
func (sb *StringBuilder) WriteASCII(c byte) {
	if sb.u != nil {
		sb.u = append(sb.u, uint16(c))
		return
	}
	if sb.b == nil {
		if sb.n < len(sb.inline) {
			sb.inline[sb.n] = c
			sb.n++
			return
		}
		sb.spill(2 * len(sb.inline))
	}
	sb.b = append(sb.b, c)
}

// writeASCIIBytes appends bytes that are all < 0x80.
func (sb *StringBuilder) writeASCIIBytes(s string) {
	if sb.u != nil {
		for i := range len(s) {
			sb.u = append(sb.u, uint16(s[i]))
		}
		return
	}
	if sb.b == nil {
		if sb.n+len(s) <= len(sb.inline) {
			sb.n += copy(sb.inline[sb.n:], s)
			return
		}
		sb.spill(max(2*len(sb.inline), sb.n+len(s)))
	}
	sb.b = append(sb.b, s...)
}

// WriteRune appends a code point, encoding surrogate pairs as needed.
func (sb *StringBuilder) WriteRune(r rune) {
	if r < 0x10000 {
		sb.WriteUnit(uint16(r))
		return
	}
	hi, lo := utf16.EncodeRune(r)
	sb.WriteUnit(uint16(hi))
	sb.WriteUnit(uint16(lo))
}

// WriteGoString appends a UTF-8 Go string.
func (sb *StringBuilder) WriteGoString(s string) {
	if sb.u == nil && isASCII(s) {
		sb.writeASCIIBytes(s)
		return
	}
	for _, r := range s {
		sb.WriteRune(r)
	}
}

// WriteString appends a JavaScript string.
func (sb *StringBuilder) WriteString(s *String) {
	if s.kind == strRope {
		s.flatten()
	}
	if s.kind == strASCII {
		sb.writeASCIIBytes(s.s)
		return
	}
	sb.upgrade()
	sb.u = append(sb.u, s.u...)
}

// WriteUTF16 appends raw code units.
func (sb *StringBuilder) WriteUTF16(u []uint16) {
	for _, c := range u {
		sb.WriteUnit(c)
	}
}

// String finishes the builder and returns the result. The builder must not be
// reused without Reset.
func (sb *StringBuilder) String() *String {
	if sb.u != nil {
		u := sb.u
		sb.u = nil
		return FromUTF16(u)
	}
	if sb.b != nil {
		b := sb.b
		sb.b = nil
		return &String{s: bytesToString(b), n: int32(len(b)), kind: strASCII}
	}
	if sb.n == 0 {
		return emptyString
	}
	s := asciiStringCopy(sb.inline[:sb.n])
	sb.n = 0
	return s
}

// Reset clears the builder for reuse, keeping no reference to the old buffer.
func (sb *StringBuilder) Reset() {
	sb.b = nil
	sb.u = nil
	sb.n = 0
}
