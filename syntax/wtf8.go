package syntax

import "unicode/utf8"

// String literal values are stored as WTF-8: ordinary UTF-8 for every scalar
// value, plus the generalized three-byte encoding (ED A0 80 .. ED BF BF) for
// lone surrogates that the source spelled with \uD800-style escapes. Go's
// utf8 package treats those sequences as errors, so callers that need code
// units use DecodeWTF8; callers that only need ASCII can use IsASCII and keep
// the Go string as-is.

// IsASCII reports whether every byte of s is below 0x80.
func IsASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// DecodeWTF8 converts a WTF-8 string (as produced for StringLit.Value and
// template cooked text) to UTF-16 code units. Lone surrogates round-trip.
func DecodeWTF8(s string) []uint16 {
	out := make([]uint16, 0, len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			out = append(out, uint16(c))
			i++
			continue
		}
		if c == 0xED && i+2 < len(s) && s[i+1] >= 0xA0 && s[i+1] <= 0xBF {
			// Generalized encoding of a surrogate code point.
			r := rune(c&0x0F)<<12 | rune(s[i+1]&0x3F)<<6 | rune(s[i+2]&0x3F)
			out = append(out, uint16(r))
			i += 3
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		if r >= 0x10000 {
			r -= 0x10000
			out = append(out, uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF)))
			continue
		}
		out = append(out, uint16(r))
	}
	return out
}

// appendWTF8 appends the WTF-8 encoding of r, which may be a surrogate.
func appendWTF8(b []byte, r rune) []byte {
	if r >= 0xD800 && r <= 0xDFFF {
		return append(b, 0xED, byte(0x80|(r>>6)&0x3F), byte(0x80|r&0x3F))
	}
	return utf8.AppendRune(b, r)
}
