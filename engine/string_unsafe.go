package engine

import (
	"encoding/binary"
	"unsafe"
)

// isASCII reports whether every byte of s is < 0x80, scanning eight bytes at
// a time (SWAR).
//
// GC safety: unsafe.Slice(unsafe.StringData(s), len(s)) produces a []byte
// view over the string's own backing memory without copying. The view never
// outlives s (it is local to this function), it is never written to, and no
// pointer arithmetic is performed; the GC sees an ordinary pointer to the
// string's data, so this is safe. The view is read-only by construction.
func isASCII(s string) bool {
	if len(s) == 0 {
		return true
	}
	b := unsafe.Slice(unsafe.StringData(s), len(s))
	i := 0
	for ; i+8 <= len(b); i += 8 {
		if binary.LittleEndian.Uint64(b[i:])&0x8080808080808080 != 0 {
			return false
		}
	}
	for ; i < len(b); i++ {
		if b[i] >= 0x80 {
			return false
		}
	}
	return true
}

// bytesToString converts a byte slice to a string without copying. The
// caller must guarantee that b is never modified afterwards (StringBuilder
// only calls it on a buffer it is about to drop).
//
// GC safety: unsafe.String(&b[0], len(b)) yields a string header pointing at
// b's backing array; the array stays reachable through the string, and the
// caller relinquishes its own reference, so there is no aliasing of mutable
// data with an immutable string.
func bytesToString(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(b), len(b))
}

// Small ASCII strings are co-allocated with their header: the String and
// its bytes live in one allocation, so a short concatenation, case
// conversion or builder result costs one allocation instead of two.
// smallASCIIMax is the longest content that qualifies (a 64-byte payload
// puts the combined object in the 144-byte size class; longer payloads keep
// their own allocation).
const smallASCIIMax = 64

type asciiBuf16 struct {
	s String
	b [16]byte
}

type asciiBuf32 struct {
	s String
	b [32]byte
}

type asciiBuf64 struct {
	s String
	b [smallASCIIMax]byte
}

// newASCIIBuf returns an ASCII String of length n together with the byte
// slice that backs it. The caller fills every byte before the string is
// published (returned, stored, or read) and never writes to it afterwards;
// until then the string is private to the caller, so there is no aliasing
// of mutable data with a visible immutable string. For n > smallASCIIMax
// the bytes are a separate allocation.
//
// GC safety: for small sizes the string's data pointer is an interior
// pointer into the same allocation as the header; the collector keeps the
// whole object alive through either pointer and no pointer arithmetic is
// performed. The layout invariant "s.s aliases b" is never observable: the
// String is immutable once published.
func newASCIIBuf(n int) (*String, []byte) {
	if n == 0 {
		return emptyString, nil
	}
	var (
		s *String
		b []byte
	)
	switch {
	case n <= 16:
		x := &asciiBuf16{}
		s, b = &x.s, x.b[:n:n]
	case n <= 32:
		x := &asciiBuf32{}
		s, b = &x.s, x.b[:n:n]
	case n <= smallASCIIMax:
		x := &asciiBuf64{}
		s, b = &x.s, x.b[:n:n]
	default:
		s, b = &String{}, make([]byte, n)
	}
	*s = String{s: unsafe.String(unsafe.SliceData(b), n), n: int32(n), kind: strASCII}
	return s, b
}

// asciiStringCopy returns an ASCII String holding a copy of b (which the
// caller keeps ownership of); short contents are co-allocated with the
// header.
func asciiStringCopy(b []byte) *String {
	s, dst := newASCIIBuf(len(b))
	copy(dst, b)
	return s
}

// utf16Bytes returns a read-only []byte view of u's code units (native byte
// order), for hashing.
//
// GC safety: the view aliases u's backing array without copying; it is only
// passed to a hash function that reads it and never outlives the call, and
// no pointer arithmetic is performed beyond the slice header.
func utf16Bytes(u []uint16) []byte {
	if len(u) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(unsafe.SliceData(u))), 2*len(u))
}
