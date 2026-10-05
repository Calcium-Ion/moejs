package engine

import (
	"encoding/binary"
	"math/bits"
	"slices"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"
)

// JSON string scanning eight bytes at a time (SWAR). JSON quoting escapes
// control characters (< 0x20), '"' and '\\'; parsing stops at the same bytes
// and, until a string turns out not to be ASCII, at bytes >= 0x80. A run of
// other bytes is copied whole, so a long string without escapes (a base64
// image) costs one scan and one copy.

const (
	swarLSB = 0x0101010101010101
	swarMSB = 0x8080808080808080
)

// jsonSpecial returns the top bit of each byte of x (little-endian) that is a
// control character, '"' or '\\', or >= 0x80 when high is swarMSB (high is 0
// otherwise). A borrow can also flag bytes above a flagged one, so only the
// lowest flag is exact: that is the one jsonScan reads.
func jsonSpecial(x, high uint64) uint64 {
	q := x ^ ('"' * swarLSB)
	b := x ^ ('\\' * swarLSB)
	return ((x-0x20*swarLSB)&^x | (q-swarLSB)&^q | (b-swarLSB)&^b | x&high) & swarMSB
}

// jsonScan returns the index of the first byte of s at or after i that is a
// control character, '"' or '\\', or >= 0x80 when high is swarMSB; len(s)
// when there is none. The parser scans the rest of the text, so the first
// word is tested alone: most strings end in it.
//
// GC safety: as in isASCII, the []byte view of s is read-only and local.
func jsonScan(s string, i int, high uint64) int {
	b := unsafe.Slice(unsafe.StringData(s), len(s))[i:]
	if len(b) >= 8 {
		if m := jsonSpecial(binary.LittleEndian.Uint64(b), high); m != 0 {
			return i + bits.TrailingZeros64(m)>>3
		}
		b = b[8:]
	}
	for len(b) >= 32 {
		m0 := jsonSpecial(binary.LittleEndian.Uint64(b), high)
		m1 := jsonSpecial(binary.LittleEndian.Uint64(b[8:]), high)
		m2 := jsonSpecial(binary.LittleEndian.Uint64(b[16:]), high)
		m3 := jsonSpecial(binary.LittleEndian.Uint64(b[24:]), high)
		if m0|m1|m2|m3 != 0 {
			at := len(s) - len(b)
			switch {
			case m0 != 0:
				return at + bits.TrailingZeros64(m0)>>3
			case m1 != 0:
				return at + 8 + bits.TrailingZeros64(m1)>>3
			case m2 != 0:
				return at + 16 + bits.TrailingZeros64(m2)>>3
			}
			return at + 24 + bits.TrailingZeros64(m3)>>3
		}
		b = b[32:]
	}
	for len(b) >= 8 {
		if m := jsonSpecial(binary.LittleEndian.Uint64(b), high); m != 0 {
			return len(s) - len(b) + bits.TrailingZeros64(m)>>3
		}
		b = b[8:]
	}
	for j, c := range b {
		if c < 0x20 || c == '"' || c == '\\' || c&byte(high) != 0 {
			return len(s) - len(b) + j
		}
	}
	return len(s)
}

// jsonPlainMin is the length from which FromGo scans a string for the bytes
// JSON quoting escapes along with its ASCII test (jsonASCII): a payload
// (an image, a document) a hook forwards. A shorter string costs isASCII
// only, and rescanning it when it is quoted costs little.
const jsonPlainMin = 64 << 10

// jsonASCII reports whether g is ASCII and whether it has no byte JSON
// quoting escapes: one scan for both, where quoting the string would scan
// it again.
func jsonASCII(g string) (ascii, plain bool) {
	i := jsonScan(g, 0, swarMSB)
	if i == len(g) {
		return true, true
	}
	return g[i] < 0x80 && isASCII(g[i:]), false
}

// jsonShortMax is the longest literal JSON.parse shares between repeats.
const jsonShortMax = 16

// plain returns the String of a literal with no escape and no byte >= 0x80,
// one per content among the recent short ones.
func (p *jsonParser) plain(lit string) *String {
	if len(lit) == 0 {
		return emptyString
	}
	if len(lit) > jsonShortMax {
		return &String{s: lit, n: int32(len(lit)), kind: strASCII, jsonPlain: true}
	}
	slot := strSlot(lit)
	if s := p.short[slot]; s != nil && s.s == lit {
		return s
	}
	s := &String{s: lit, n: int32(len(lit)), kind: strASCII, jsonPlain: true}
	p.short[slot] = s
	return s
}

// quoteASCII writes the quoted body of the ASCII string str. The interrupt
// is checked every interruptStride bytes.
func (js *jsonStringifier) quoteASCII(str string) error {
	sb := &js.sb
	if len(str) < 16 { // most keys and many values: bytes are quicker
		run := 0
		for i := range len(str) {
			if c := str[i]; c < 0x20 || c == '"' || c == '\\' {
				writeASCIIString(sb, str[run:i])
				js.escapeByte(c)
				run = i + 1
			}
		}
		writeASCIIString(sb, str[run:])
		return nil
	}
	sb.Grow(len(str) + 1)
	run := 0
	for base := 0; base < len(str); base += interruptStride {
		end := min(base+interruptStride, len(str))
		if base > 0 {
			if err := js.r.CheckInterrupt(); err != nil {
				return err
			}
		}
		for i := jsonScan(str[:end], base, 0); i < end; i = jsonScan(str[:end], i+1, 0) {
			writeASCIIString(sb, str[run:i])
			js.escapeByte(str[i])
			run = i + 1
		}
	}
	writeASCIIString(sb, str[run:])
	return nil
}

// quoteUTF16 writes the quoted body of the UTF-16 string u: the runs between
// units to escape are appended whole, a surrogate pair stays, a lone
// surrogate is escaped.
func (js *jsonStringifier) quoteUTF16(u []uint16) error {
	sb := &js.sb
	run := 0
	for i := 0; i < len(u); i++ {
		if err := interruptEvery(js.r, int64(i)); err != nil {
			return err
		}
		c := u[i]
		if c >= 0x20 && c != '"' && c != '\\' && (c < 0xD800 || c >= 0xE000) {
			continue
		}
		if c >= 0xD800 && c < 0xDC00 && i+1 < len(u) && u[i+1] >= 0xDC00 && u[i+1] < 0xE000 {
			i++
			continue
		}
		sb.WriteUTF16(u[run:i])
		run = i + 1
		if c < 0x80 {
			js.escapeByte(byte(c))
			continue
		}
		sb.writeASCIIBytes(`\u`)
		sb.WriteASCII(lowerHex[c>>12])
		sb.WriteASCII(lowerHex[c>>8&15])
		sb.WriteASCII(lowerHex[c>>4&15])
		sb.WriteASCII(lowerHex[c&15])
	}
	sb.WriteUTF16(u[run:])
	return nil
}

// quoteUTF8 is quoteUTF16 for AppendJSON: it encodes the units as UTF-8
// into sb.b, which a raw stringifier always has (AppendJSON).
func (js *jsonStringifier) quoteUTF8(u []uint16) error {
	sb := &js.sb
	b := slices.Grow(sb.b, len(u)+len(u)/2+1)
	for i := 0; i < len(u); i++ {
		if err := interruptEvery(js.r, int64(i)); err != nil {
			sb.b = b
			return err
		}
		switch c := u[i]; {
		case c < 0x80:
			if c < 0x20 || c == '"' || c == '\\' {
				sb.b = b
				js.escapeByte(byte(c))
				b = sb.b
				continue
			}
			b = append(b, byte(c))
		case c < 0x800:
			b = append(b, 0xC0|byte(c>>6), 0x80|byte(c)&0x3F)
		case c < 0xD800 || c >= 0xE000:
			b = append(b, 0xE0|byte(c>>12), 0x80|byte(c>>6)&0x3F, 0x80|byte(c)&0x3F)
		case c < 0xDC00 && i+1 < len(u) && u[i+1] >= 0xDC00 && u[i+1] < 0xE000:
			b = utf8.AppendRune(b, utf16.DecodeRune(rune(c), rune(u[i+1])))
			i++
		default:
			b = append(b, '\\', 'u', lowerHex[c>>12], lowerHex[c>>8&15], lowerHex[c>>4&15], lowerHex[c&15])
		}
	}
	sb.b = b
	return nil
}

// serialize writes v as JSONStringify and AppendJSON do, with no replacer
// and no indentation; ok is false when v has no JSON representation or on
// an error, which leaves the call depth as it found it.
func (js *jsonStringifier) serialize(v Value) (ok bool, err error) {
	js.stack = js.stackBuf[:0]
	rv, err := js.resolve(v, StringKey(AtomEmpty), nil)
	if err != nil {
		return false, err
	}
	depth := js.r.callDepth
	if ok, err = js.write(rv); err != nil || !ok {
		js.r.callDepth = depth // the levels an error leaves entered (push)
		return false, err
	}
	return true, nil
}

// AppendJSON appends JSONStringify(v) to dst as UTF-8 and reports whether v
// has a JSON representation; dst comes back unchanged when it has none or on
// an error. The output is written into dst's spare capacity, grown once to
// the last output's length plus an eighth (jsonSizeHint; after one very
// large output, a small dst is grown that much again), so a result that fits
// costs no copy; it never goes through UTF-16. Its limit is maxStringLength
// counted in bytes, which UTF-8 of text that is not ASCII reaches before
// JSONStringify's units do. Before any JavaScript runs (a toJSON, a getter,
// a proxy's trap) the output moves to a buffer of its own (own), so a host
// function that code calls may use dst: it is appended to when AppendJSON
// returns, as append(dst, text...) would.
func (r *Realm) AppendJSON(dst []byte, v Value) ([]byte, bool, error) {
	js := jsonStringifier{r: r, raw: true}
	out := slices.Grow(dst, max(256, int(r.jsonSizeHint)+int(r.jsonSizeHint)/8))
	js.sb.b = out[len(out):]
	js.inDst = unsafe.SliceData(out) == unsafe.SliceData(dst)
	if ok, err := js.serialize(v); !ok {
		return dst, false, err
	}
	b := js.sb.b
	r.jsonSizeHint = int32(len(b))
	switch {
	case unsafe.SliceData(b) == unsafe.SliceData(out[len(out):]):
		return out[:len(out)+len(b)], true, nil
	case len(dst) == 0 && cap(dst) < len(b):
		return b, true, nil // dst could not hold it: the output's own buffer
	}
	return append(dst, b...), true, nil // into dst, whose capacity the host keeps
}

// JobsPending reports whether jobs are queued for the end of the outermost
// call (or of the hold of HoldJobs): what a call or a conversion that ran
// code left to run, nothing when no code ran. The root AppendJSON asks it
// before it lets its output into dst (its jobs may use dst). It sits here,
// after (*String).flatten and out of line, for layout (realm_calldata.go):
// a symbol of one 32-byte slot in every binary, which with Unmarshal's
// growth keeps (*Runtime).Call on its 64-byte phase.
//
//go:noinline
func (r *Realm) JobsPending() bool { return r.jobsPending }

// own moves AppendJSON's output out of dst's spare capacity, once, before
// code runs that could call the host. The buffer holds twice what is
// written so far (at least 256 bytes) and grows as the rest is written:
// neither dst's spare capacity, which a reused buffer makes as large as the
// largest result it ever held, nor the last output's length (jsonSizeHint),
// which a host alternating large and small results makes just as large,
// says anything about this output.
func (js *jsonStringifier) own() {
	if js.inDst {
		js.inDst = false
		b := make([]byte, len(js.sb.b), ownCap(len(js.sb.b)))
		copy(b, js.sb.b)
		js.sb.b = b
	}
}

// ownCap sizes own's buffer: twice the bytes written so far, at least 256.
// It is out of line, and so is own then, for layout (realm_calldata.go):
// the stringifier's walk, which calls own in four places, is smaller by it,
// and proxy's placement keeps the walk's 64-byte phases. own runs only when
// code runs.
//
//go:noinline
func ownCap(n int) int { return max(256, 2*n) }

// JSONParseGoString is JSONParse of the UTF-8 text, whose invalid bytes read
// as U+FFFD as FromGoString reads them. ASCII and valid UTF-8 text is parsed
// in place, never through UTF-16; the strings of the result may alias text.
func (r *Realm) JSONParseGoString(text string) (Value, error) {
	p := jsonParser{r: r}
	if isASCII(text) || utf8.ValidString(text) {
		return p.parseText(text)
	}
	return p.parse(FromGoString(text))
}
