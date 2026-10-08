package engine

// Appending in place.
//
// `s += x` in a loop made a rope node per step: the chain stayed alive until
// it was read, every collection marked all of it, and reading it flattened
// it. Concat instead appends a short piece to a long flat string in an
// append buffer: an allocation holding an appendBuf (string_unsafe.go) and
// the code units after it, ASCII bytes or UTF-16 units. Every string with
// the flag strInBuf is a prefix of the units of its buffer; used counts the
// units written, and only a string of length used, the one ending there,
// may write past it, by moving used with a compare-and-swap. Any other string copies
// on append, as a string from FromGoString or JSON.parse copies on its first
// one. The units a string covers are never written again, so it stays
// immutable, can be shared between goroutines like any other string, and
// exposes its own length only: s, units and GoString cover [0, n).
//
// A new buffer is as long as the result, up to the next block size, when a
// was not in a buffer (a one-off concatenation is not overallocated), and
// up to twice as long when it was (appending in a loop costs O(1) a unit):
// a buffer holds at most twice its longest string. A buffer of up to appendBlockMax bytes is allocated with the
// String of the result (strBlock), so a step that grows the buffer costs one
// allocation, like a step that writes in place or a rope node.
//
// Retention: a string keeps its whole buffer alive, at most about twice its
// length, and so does a substring of it, as a substring of a flat string
// keeps that string's bytes; a rope kept every piece and node. Atoms are
// always copies (globalASCIIAtom), so a property name made by a
// concatenation pins no buffer.

// appendPieceMax bounds the piece appended to a string that is not in an
// append buffer yet: a longer one makes a rope, which copies nothing until
// the string is read, for joins of big pieces.
const appendPieceMax = 4096

// appendString returns a + b, n units long, in an append buffer: a's own
// when b fits after it and a ends it, a new one otherwise. a is flat and
// b.n <= a.n. It charges the String and a new buffer.
func (r *Realm) appendString(a, b *String, n int) *String {
	bASCII := b.kind == strASCII || b.kind == strRope && b.ropeASCII()
	inBuf := a.flags&strInBuf != 0
	if inBuf && (bASCII || a.kind == strUTF16) {
		h := a.appendBuf()
		if n <= int(h.cap) && h.used.CompareAndSwap(a.n, int32(n)) {
			// [a.n, n) is this call's alone now.
			r.chargeString(0)
			if a.kind == strASCII {
				if d := h.bytes()[a.n:n]; b.kind == strASCII {
					copy(d, b.s)
				} else {
					b.copyBytes(d)
				}
				return &String{s: h.ascii(n), n: int32(n), kind: strASCII, flags: strInBuf}
			}
			b.copyUnits(h.units()[a.n:n])
			return &String{p: a.p, n: int32(n), kind: strUTF16, flags: strInBuf}
		}
	}
	capacity := n
	if inBuf {
		capacity = 2 * n
	}
	if a.kind == strASCII && bASCII {
		s, d := newAppendASCII(n, capacity)
		r.chargeString(int(s.appendBuf().cap))
		if copy(d, a.s); b.kind == strASCII {
			copy(d[a.n:], b.s)
		} else {
			b.copyBytes(d[a.n:])
		}
		return s
	}
	s, u := newAppendUTF16(n, capacity)
	r.chargeString(2 * int(s.appendBuf().cap))
	a.copyUnits(u[:a.n])
	b.copyUnits(u[a.n:])
	return s
}
