package engine

import (
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// String kinds (String.kind).
const (
	strASCII uint8 = iota // s holds bytes < 0x80; byte index == code unit index
	strUTF16              // u holds code units, at least one >= 0x80
	strRope               // left/right children; flattened lazily in place
)

// ropeThreshold is the combined length at or above which Concat builds a
// rope instead of copying.
const ropeThreshold = 64

// MaxStringLength is the maximum JavaScript string length in code units.
// Every *String satisfies Len() <= MaxStringLength, which is what lets
// Concat add two int32 lengths as an int without overflow on any platform;
// producers whose output can outgrow their inputs check
// maxStringLength before allocating and raise RangeError: Invalid string
// length (Realm.invalidStringLength).
const MaxStringLength = 1<<30 - 24

// maxStringLength is the limit the checks compare against; a variable only
// so tests can lower it and exercise every producer at small sizes.
var maxStringLength = MaxStringLength

// String is an immutable JavaScript string (a UTF-16 code unit sequence).
// Strings are shared freely; the only mutation is the one-time in-place
// flattening of a rope and the caching of hash/atom, both of which preserve
// the logical value.
type String struct {
	s     string   // strASCII payload
	u     []uint16 // strUTF16 payload (never all-ASCII)
	left  *String  // strRope children
	right *String
	n     int32  // length in code units
	hash  uint32 // 0 = not computed
	atom  uint32 // 0 = not interned
	kind  uint8
	// numeric marks an atom that is a canonical numeric string other than
	// an array index ("-0", "1.5", "NaN"): the keys a typed array answers
	// itself (typedArrayKey). It is set before the atom is published.
	numeric bool
}

// emptyString is the shared "" value.
var emptyString = &String{kind: strASCII}

// EmptyString returns the shared empty string.
func EmptyString() *String { return emptyString }

// FromGoString converts a Go (UTF-8) string. ASCII input is zero-copy;
// anything else is decoded to UTF-16 once, with invalid UTF-8 bytes becoming
// U+FFFD (Go's range semantics).
func FromGoString(s string) *String {
	if len(s) == 0 {
		return emptyString
	}
	if isASCII(s) {
		return &String{s: s, n: int32(len(s)), kind: strASCII}
	}
	u := appendUTF16(make([]uint16, 0, len(s)), s)
	return &String{u: u, n: int32(len(u)), kind: strUTF16}
}

// appendUTF16 appends the UTF-16 encoding of the UTF-8 string s to u
// (invalid bytes become U+FFFD, as Go's range does).
func appendUTF16(u []uint16, s string) []uint16 {
	for _, r := range s {
		if r < 0x10000 {
			u = append(u, uint16(r))
			continue
		}
		hi, lo := utf16.EncodeRune(r)
		u = append(u, uint16(hi), uint16(lo))
	}
	return u
}

// FromUTF16 builds a string from code units. The slice is retained when the
// content is not ASCII, so callers must not modify it afterwards.
func FromUTF16(u []uint16) *String {
	if len(u) == 0 {
		return emptyString
	}
	for _, c := range u {
		if c >= 0x80 {
			return &String{u: u, n: int32(len(u)), kind: strUTF16}
		}
	}
	b := make([]byte, len(u))
	for i, c := range u {
		b[i] = byte(c)
	}
	return &String{s: string(b), n: int32(len(b)), kind: strASCII}
}

// asciiString wraps a Go string already known to be ASCII.
func asciiString(s string) *String {
	if len(s) == 0 {
		return emptyString
	}
	return &String{s: s, n: int32(len(s)), kind: strASCII}
}

// Len returns the length in UTF-16 code units.
func (s *String) Len() int { return int(s.n) }

// IsASCII reports whether the string (after flattening) is ASCII-only.
func (s *String) IsASCII() bool {
	if s.kind == strRope {
		s.flatten()
	}
	return s.kind == strASCII
}

// IsInterned reports whether s is an atom.
func (s *String) IsInterned() bool { return s.atom != 0 }

// At returns the code unit at index i (0 <= i < Len).
func (s *String) At(i int) uint16 {
	switch s.kind {
	case strASCII:
		return uint16(s.s[i])
	case strUTF16:
		return s.u[i]
	}
	s.flatten()
	return s.At(i)
}

// ASCII returns the underlying Go string and true when the string is ASCII.
func (s *String) ASCII() (string, bool) {
	if s.kind == strRope {
		s.flatten()
	}
	if s.kind == strASCII {
		return s.s, true
	}
	return "", false
}

// UTF16 returns the code units. For ASCII strings a fresh slice is built;
// for UTF-16 strings the internal slice is returned and must not be modified.
func (s *String) UTF16() []uint16 {
	if s.kind == strRope {
		s.flatten()
	}
	if s.kind == strUTF16 {
		return s.u
	}
	u := make([]uint16, len(s.s))
	for i := range len(s.s) {
		u[i] = uint16(s.s[i])
	}
	return u
}

// GoString exports to a Go string. ASCII is zero-copy; UTF-16 is encoded to
// UTF-8 with lone surrogates replaced by U+FFFD.
func (s *String) GoString() string {
	if s.kind == strRope {
		s.flatten()
	}
	if s.kind == strASCII {
		return s.s
	}
	return utf16ToGoString(s.u)
}

func utf16ToGoString(u []uint16) string {
	var b strings.Builder
	b.Grow(len(u) + len(u)/2)
	for i := 0; i < len(u); i++ {
		c := u[i]
		if c < 0x80 {
			b.WriteByte(byte(c))
			continue
		}
		if utf16.IsSurrogate(rune(c)) {
			if c < 0xDC00 && i+1 < len(u) && u[i+1] >= 0xDC00 && u[i+1] < 0xE000 {
				b.WriteRune(utf16.DecodeRune(rune(c), rune(u[i+1])))
				i++
				continue
			}
			b.WriteRune(utf8.RuneError)
			continue
		}
		b.WriteRune(rune(c))
	}
	return b.String()
}

// flatten collapses a rope in place. It is iterative so that left-deep ropes
// built by `s += x` loops cannot exhaust the Go stack.
func (s *String) flatten() {
	if s.kind != strRope {
		return
	}
	ascii := true
	stack := []*String{s}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch cur.kind {
		case strRope:
			stack = append(stack, cur.right, cur.left)
		case strUTF16:
			ascii = false
		}
	}
	if ascii {
		b := make([]byte, 0, s.n)
		stack = append(stack[:0], s)
		for len(stack) > 0 {
			cur := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if cur.kind == strRope {
				stack = append(stack, cur.right, cur.left)
				continue
			}
			b = append(b, cur.s...)
		}
		s.s = string(b)
		s.kind = strASCII
	} else {
		u := make([]uint16, 0, s.n)
		stack = append(stack[:0], s)
		for len(stack) > 0 {
			cur := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			switch cur.kind {
			case strRope:
				stack = append(stack, cur.right, cur.left)
			case strASCII:
				for i := range len(cur.s) {
					u = append(u, uint16(cur.s[i]))
				}
			case strUTF16:
				u = append(u, cur.u...)
			}
		}
		s.u = u
		s.kind = strUTF16
	}
	s.left, s.right = nil, nil
}

// Concat returns a + b, building a rope when the result is long. A result
// longer than the string length limit is RangeError: Invalid string length,
// raised before anything is allocated (`s = s + s` builds ropes without
// copying, so the limit must be enforced on the length arithmetic itself).
func (r *Realm) Concat(a, b *String) (*String, error) {
	if a.n == 0 {
		return b, nil
	}
	if b.n == 0 {
		return a, nil
	}
	n := int(a.n) + int(b.n) // both <= MaxStringLength: cannot overflow
	if n > maxStringLength {
		return nil, r.invalidStringLength()
	}
	if n >= ropeThreshold {
		return &String{left: a, right: b, n: int32(n), kind: strRope}, nil
	}
	if a.kind == strRope {
		a.flatten()
	}
	if b.kind == strRope {
		b.flatten()
	}
	if a.kind == strASCII && b.kind == strASCII {
		// n < ropeThreshold <= smallASCIIMax: header and bytes share one
		// allocation.
		s, buf := newASCIIBuf(n)
		copy(buf[copy(buf, a.s):], b.s)
		return s, nil
	}
	u := make([]uint16, 0, n)
	u = appendUnits(u, a)
	u = appendUnits(u, b)
	return &String{u: u, n: int32(n), kind: strUTF16}, nil
}

func appendUnits(u []uint16, s *String) []uint16 {
	if s.kind == strASCII {
		for i := range len(s.s) {
			u = append(u, uint16(s.s[i]))
		}
		return u
	}
	return append(u, s.u...)
}

// Substring returns the code units in [start, end). Bounds must be valid.
func (s *String) Substring(start, end int) *String {
	if start <= 0 && end >= int(s.n) {
		return s
	}
	if start >= end {
		return emptyString
	}
	if s.kind == strRope {
		s.flatten()
	}
	if s.kind == strASCII {
		return &String{s: s.s[start:end], n: int32(end - start), kind: strASCII}
	}
	return FromUTF16(s.u[start:end])
}

// Hash returns the cached content hash (never 0).
func (s *String) Hash() uint32 {
	if s.hash != 0 {
		return s.hash
	}
	if s.kind == strRope {
		s.flatten()
	}
	h := uint32(2166136261)
	if s.kind == strASCII {
		for i := range len(s.s) {
			h = (h ^ uint32(s.s[i])) * 16777619
		}
	} else {
		for _, c := range s.u {
			h = (h ^ uint32(c)) * 16777619
		}
	}
	if h == 0 {
		h = 1
	}
	s.hash = h
	return h
}

// Equals reports code-unit equality.
func (s *String) Equals(t *String) bool {
	if s == t {
		return true
	}
	if s.n != t.n {
		return false
	}
	if s.hash != 0 && t.hash != 0 && s.hash != t.hash {
		return false
	}
	if s.kind == strRope {
		s.flatten()
	}
	if t.kind == strRope {
		t.flatten()
	}
	if s.kind != t.kind {
		return false // utf16 strings always contain a unit >= 0x80
	}
	if s.kind == strASCII {
		return s.s == t.s
	}
	for i, c := range s.u {
		if t.u[i] != c {
			return false
		}
	}
	return true
}

// EqualsGoString compares against an ASCII/UTF-8 Go string.
func (s *String) EqualsGoString(g string) bool {
	if s.kind == strRope {
		s.flatten()
	}
	if s.kind == strASCII {
		return s.s == g
	}
	if isASCII(g) {
		return false
	}
	return s.GoString() == g && utf8.ValidString(g)
}

// Compare orders by code units: -1, 0 or 1.
func (s *String) Compare(t *String) int {
	if s == t {
		return 0
	}
	if s.kind == strRope {
		s.flatten()
	}
	if t.kind == strRope {
		t.flatten()
	}
	if s.kind == strASCII && t.kind == strASCII {
		return strings.Compare(s.s, t.s)
	}
	n := min(int(s.n), int(t.n))
	for i := range n {
		a, b := s.At(i), t.At(i)
		if a != b {
			if a < b {
				return -1
			}
			return 1
		}
	}
	switch {
	case s.n < t.n:
		return -1
	case s.n > t.n:
		return 1
	}
	return 0
}

// IndexOf returns the first index >= from at which sub occurs, or -1.
func (s *String) IndexOf(sub *String, from int) int {
	if from < 0 {
		from = 0
	}
	if int(sub.n) == 0 {
		return min(from, int(s.n))
	}
	if from+int(sub.n) > int(s.n) {
		return -1
	}
	if s.kind == strRope {
		s.flatten()
	}
	if sub.kind == strRope {
		sub.flatten()
	}
	if s.kind == strASCII && sub.kind == strASCII {
		i := strings.Index(s.s[from:], sub.s)
		if i < 0 {
			return -1
		}
		return from + i
	}
	if s.kind == strASCII && sub.kind == strUTF16 {
		return -1
	}
	first := sub.At(0)
	last := int(s.n) - int(sub.n)
outer:
	for i := from; i <= last; i++ {
		if s.u[i] != first {
			continue
		}
		for j := 1; j < int(sub.n); j++ {
			if s.u[i+j] != sub.At(j) {
				continue outer
			}
		}
		return i
	}
	return -1
}

// String implements fmt.Stringer for debugging.
func (s *String) String() string { return s.GoString() }

// IsWellFormed reports whether the string has no lone surrogates.
func (s *String) IsWellFormed() bool {
	if s.kind == strRope {
		s.flatten()
	}
	if s.kind == strASCII {
		return true
	}
	for i := 0; i < len(s.u); i++ {
		c := s.u[i]
		if c < 0xD800 || c >= 0xE000 {
			continue
		}
		if c >= 0xDC00 || i+1 >= len(s.u) || s.u[i+1] < 0xDC00 || s.u[i+1] >= 0xE000 {
			return false
		}
		i++
	}
	return true
}
