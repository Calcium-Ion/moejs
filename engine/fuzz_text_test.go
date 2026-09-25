package engine

import (
	"bytes"
	"slices"
	"testing"
	"unicode/utf16"
	"unicode/utf8"
)

// FuzzText checks decodeUTF8 against refDecoder over the input cut into
// stream calls (each cut byte: its high bits the length, its low bit
// whether the call streams; the rest goes in a last, flushing call), and
// encodeUTF8 against unicode/utf16, whole and into a destination that
// flags cuts short.
func FuzzText(f *testing.F) {
	for _, s := range []string{"", "a", "\xEF\xBB\xBFx", "\xF0\x9F\x98", "\xE0\x80", "\xED\xA0\x80", "\xC2", "h\xC3\xA9llo", "\xF4\x90\x80\x80", "\xFF\xFE"} {
		f.Add([]byte(s), []byte{3, 2}, byte(0), s)
		f.Add([]byte(s), []byte{1, 1, 1}, byte(0x1F), s)
	}
	f.Fuzz(func(t *testing.T, in, cuts []byte, flags byte, s string) {
		if len(in)+len(cuts)+len(s) > 1024 {
			return
		}
		r := NewRealm()
		fatal, ignoreBOM := flags&1 != 0, flags&2 != 0
		d := &textDecoder{fatal: fatal, ignoreBOM: ignoreBOM}
		ref := &refDecoder{fatal: fatal, ignoreBOM: ignoreBOM}
		rest := in
		for i := 0; i <= len(cuts); i++ {
			chunk, stream := rest, false
			if i < len(cuts) {
				chunk, stream = rest[:int(cuts[i]>>1)%(len(rest)+1)], cuts[i]&1 != 0
			}
			rest = rest[len(chunk):]
			want, ok := ref.decode(chunk, stream)
			got, err := r.decodeUTF8(d, chunk, stream)
			if ok != (err == nil) {
				t.Fatalf("call %d of %x (cuts %x, flags %x): error %v, want ok %v", i, in, cuts, flags, err, ok)
			}
			if ok && !slices.Equal(got.UTF16(), want) {
				t.Fatalf("call %d of %x (cuts %x, flags %x) = %x, want %x", i, in, cuts, flags, got.UTF16(), want)
			}
		}

		js := fuzzCollString(s)
		units := js.UTF16()
		want := []byte(string(utf16.Decode(units)))
		if n, err := r.utf8Length(js); err != nil || n != len(want) {
			t.Fatalf("utf8Length(%x) = %d, %v, want %d", units, n, err, len(want))
		}
		dst := make([]byte, len(want))
		read, written, err := r.encodeUTF8(dst, js)
		if err != nil || read != len(units) || written != len(want) || !bytes.Equal(dst, want) {
			t.Fatalf("encodeUTF8(%x) = %d, %d, %x, %v, want %x", units, read, written, dst, err, want)
		}
		short := make([]byte, int(flags>>2)%(len(want)+1))
		read, written, err = r.encodeUTF8(short, js)
		if err != nil || written > len(short) || string(utf16.Decode(units[:read])) != string(want[:written]) ||
			!bytes.Equal(short[:written], want[:written]) {
			t.Fatalf("encodeUTF8(%x) into %d bytes = %d, %d, %v", units, len(short), read, written, err)
		}
		if read < len(units) {
			if next := utf16.Decode(units[read:min(read+2, len(units))])[0]; utf8.RuneLen(next) <= len(short)-written {
				t.Fatalf("encodeUTF8(%x) into %d bytes stopped at %d, before %U that fits", units, len(short), read, next)
			}
		}
	})
}
