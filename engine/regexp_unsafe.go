package engine

import "unsafe"

// asciiBytes returns a read-only []byte view of an ASCII string so that the
// regexp engine, and the base64 and hex decoders, can run on the string's
// own bytes (byte index == UTF-16 code unit index) without copying.
//
// GC safety: unsafe.Slice(unsafe.StringData(s), len(s)) points at the
// string's backing array, which stays reachable through the slice; the GC
// sees an ordinary pointer. The view is never written to: it is only passed
// to regexp.Regexp matching methods and to fromBase64 and fromHex, which
// read their input and copy out whatever they return. No pointer
// arithmetic is performed.
func asciiBytes(s string) []byte {
	if len(s) == 0 {
		return nil
	}
	return unsafe.Slice(unsafe.StringData(s), len(s))
}
