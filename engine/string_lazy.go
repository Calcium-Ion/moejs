package engine

import (
	"sync"
	"sync/atomic"
	"unicode/utf8"
	"unsafe"
)

// lazyStringMode fixes how retained bytes are decoded. Reads may populate
// caches, but never change the mode or the retained bytes. The text modes
// come first (String.lazyTextMode).
type lazyStringMode uint8

const (
	// lazyText holds Go text: invalid UTF-8 bytes decode to U+FFFD, as
	// FromGoString replaces them.
	lazyText lazyStringMode = iota

	// lazyWTF8 holds text that may encode lone UTF-16 surrogates, which
	// decoding preserves. The text must be well-formed WTF-8, as the JSON
	// parser's source is (valid UTF-8, or wtf8FromUTF16 of a JS string):
	// decoding sizes it with wtf8Units, which counts lead bytes and does
	// not see invalid ones.
	lazyWTF8

	// lazyJSONContents holds JSON contents without quotes; strSlow checked
	// their JSON syntax. Byte runs may be WTF-8, well-formed as lazyWTF8's.
	lazyJSONContents

	// lazyJSONVerbatim holds valid UTF-8 JSON contents in stringify's exact
	// escape spelling. AppendJSON can copy them; content reads still unescape.
	lazyJSONVerbatim
)

// Facts about the retained bytes of a text mode string, cached in
// lazyStringData.facts. A fact pair with neither bit set is unchecked.
const (
	factUTF8Valid uint32 = 1 << iota
	factUTF8Invalid
	factJSONPlain   // ASCII with nothing JSON quoting escapes
	factJSONEscapes // not factJSONPlain
	factDecoded     // decoded is set (published)
)

// lazyStringData publishes one decoded flat String, content and length
// together, to concurrent readers through once. The String's right field
// publishes the cache, hash caches its hash atomically, and immutable flags
// and negative n carry the mode and optional JSON plan. With no limit this
// leaves one 64-byte allocation, rather than 96 bytes for a short value.
type lazyStringData struct {
	once  sync.Once
	facts atomic.Uint32
}

func (s *String) lazyMode() lazyStringMode { return lazyStringMode(s.flags >> lazyModeShift & 3) }
func (s *String) lazyTextMode() bool       { return s.lazyMode() <= lazyWTF8 }

// validUTF8 reports whether text, the retained bytes, is the UTF-8 of the
// string's content: text mode bytes that are valid UTF-8 (which encodes no
// surrogates). The check runs once.
func (s *String) validLazyUTF8() bool {
	if !s.lazyTextMode() {
		return false
	}
	d := s.lazyData()
	f := d.facts.Load()
	if f&(factUTF8Valid|factUTF8Invalid) == 0 {
		f = factUTF8Invalid
		if utf8.ValidString(s.s) {
			f = factUTF8Valid
		}
		d.facts.Or(f)
	}
	return f&factUTF8Valid != 0
}

// plainJSON reports whether text can be copied between JSON quotes as ASCII
// without escaping. Only text mode strings of at least jsonPlainMin bytes
// are scanned; false can also mean the mode or length is ineligible.
func (s *String) plainLazyJSON() bool {
	if len(s.s) < jsonPlainMin || !s.lazyTextMode() {
		return false
	}
	d := s.lazyData()
	f := d.facts.Load()
	if f&(factJSONPlain|factJSONEscapes) == 0 {
		ascii, plain := jsonASCII(s.s)
		f = factJSONEscapes
		if ascii {
			f = factUTF8Valid | factJSONEscapes
			if plain {
				f = factUTF8Valid | factJSONPlain
			}
		}
		d.facts.Or(f)
	}
	return f&factJSONPlain != 0
}

// resolve decodes text once; every caller waits for the same result, and
// the original String keeps its source and negative n. The decoded storage is
// charged to mem, when the string was created under a memory limit.
func (s *String) resolved() *String {
	d := s.lazyData()
	d.once.Do(func() {
		saved := -1
		if s.n < -1 {
			saved = -int(s.n) - 2
		}
		decoded, n := decodeLazyStringPlanned(s.s, s.lazyMode(), saved, s.flags&strLazyJSONWide != 0)
		atomic.StorePointer(s.rightWord(), unsafe.Pointer(decoded))
		if s.lazyTextMode() && decoded.kind == strASCII {
			d.facts.Or(factUTF8Valid)
		}
		d.facts.Or(factDecoded)
		if m := s.lazyAccount(); m != nil {
			m.addFlat(int64(n))
		}
	})
	return (*String)(atomic.LoadPointer(s.rightWord()))
}

// published returns the decoded String once resolve has set it, without
// waiting for a resolve in progress (the live measurement).
func (s *String) lazyPublished() *String {
	if s.lazyData().facts.Load()&factDecoded == 0 {
		return nil
	}
	return (*String)(atomic.LoadPointer(s.rightWord()))
}

// decodeLazyString reuses the existing decoders. JSON modes require
// syntax-checked contents. The integer is the heap storage the result
// retains, header included, which a memory limit charges.
func decodeLazyString(text string, mode lazyStringMode) (*String, int) {
	return decodeLazyStringPlanned(text, mode, -1, false)
}

func decodeLazyStringPlanned(text string, mode lazyStringMode, saved int, wide bool) (*String, int) {
	if mode == lazyJSONContents || mode == lazyJSONVerbatim {
		var sb StringBuilder
		coallocated := decodeJSONContents(&sb, text, saved, wide)
		n := sb.retained()
		if coallocated != nil {
			return coallocated, n + stringHeaderSize
		}
		return sb.String(), n + stringHeaderSize
	}
	if isASCIILong(text) {
		return asciiString(text), stringHeaderSize
	}
	// Reserve code units, not source bytes: UTF-8 CJK takes three bytes
	// for one unit. The count must equal what the append below writes:
	// wtf8Units holds for well-formed WTF-8 only (lazyWTF8), and Go text,
	// which may be invalid, is counted with range's replacement semantics,
	// as appendUTF16 decodes it.
	n := 0
	if mode == lazyWTF8 {
		n = wtf8Units(text)
	} else {
		for _, r := range text {
			n++
			if r >= 0x10000 {
				n++
			}
		}
	}
	s, u := newUTF16Buf(n)
	if mode == lazyWTF8 {
		appendWTF8Units(u[:0], text)
	} else {
		appendUTF16(u[:0], text)
	}
	return s, 2*cap(u) + stringHeaderSize
}

// lazyStringBlock keeps a String and its cache in one allocation.
type lazyStringBlock struct {
	value String
	data  lazyStringData
}

// chargedLazyData is the data of a lazy string a limited realm made
// (strLazyCharged): the account its decoding adds to and, for a JSON
// value, its share root. Both are immutable after publication; decoding
// adds solely to the atomic account. They are reached through p, so they
// do not depend on where the String header lives.
type chargedLazyData struct {
	lazyStringData
	mem  *memAccount
	root *String
}

type chargedLazyStringBlock struct {
	value String
	data  chargedLazyData
}

func newLazyString(text string, mode lazyStringMode) *String {
	return newAccountedLazyString(text, mode, nil)
}

func newAccountedLazyString(text string, mode lazyStringMode, mem *memAccount) *String {
	if len(text) == 0 {
		return emptyString
	}
	if mem != nil {
		b := &chargedLazyStringBlock{data: chargedLazyData{mem: mem}}
		b.value = String{s: text, p: unsafe.Pointer(&b.data), n: -1, kind: strLazy, flags: uint8(mode)<<lazyModeShift | strLazyCharged}
		return &b.value
	}
	b := &lazyStringBlock{}
	b.value = String{s: text, p: unsafe.Pointer(&b.data), n: -1, kind: strLazy, flags: uint8(mode) << lazyModeShift}
	return &b.value
}

func (s *String) isLazy() bool { return s.kind == strLazy }

func (s *String) lazyData() *lazyStringData { return (*lazyStringData)(s.p) }

// chargedData is the chargedLazyData of a lazy string a limited realm
// made, nil for any other.
func (s *String) chargedData() *chargedLazyData {
	if s.flags&strLazyCharged != 0 {
		return (*chargedLazyData)(s.p)
	}
	return nil
}

func (s *String) lazyAccount() *memAccount {
	if d := s.chargedData(); d != nil {
		return d.mem
	}
	return nil
}

func (s *String) lazyShareRoot() *String {
	if d := s.chargedData(); d != nil {
		return d.root
	}
	return nil
}

func (s *String) lazyBlockSize() int {
	if s.flags&strLazyCharged != 0 {
		return int(unsafe.Sizeof(chargedLazyStringBlock{}))
	}
	return int(unsafe.Sizeof(lazyStringBlock{}))
}

// sourceUTF8 returns the retained bytes of a lazy string when they are the
// UTF-8 of its content, which exports then share without decoding.
func (s *String) sourceUTF8() (string, bool) {
	if s.isLazy() && s.validLazyUTF8() {
		return s.s, true
	}
	return "", false
}

// sameLazyContent reports that two lazy strings retaining the same bytes
// text decode to the same content: one mode, both JSON modes (one decoder),
// or text modes and valid UTF-8, which the text modes decode alike.
func sameLazyContent(a, b *String) bool {
	if a.lazyMode() == b.lazyMode() {
		return true
	}
	if a.lazyTextMode() != b.lazyTextMode() {
		return false
	}
	return !a.lazyTextMode() || a.validLazyUTF8()
}

// lazyHash caches Hash atomically in the String's existing hash slot.
func (s *String) lazyHash() uint32 {
	if h := atomic.LoadUint32(&s.hash); h != 0 {
		return h
	}
	h := s.resolved().contentHash()
	atomic.StoreUint32(&s.hash, h)
	return h
}

// FromHostString is FromGoString with the decoding deferred: a string
// shorter than jsonPlainMin that is ASCII is converted as FromGoString
// converts it, and any other keeps its bytes until its content or length is
// first read. GoString of it shares the bytes when they are valid UTF-8.
func FromHostString(text string) *String {
	return fromAccountedHostString(text, nil)
}

func fromAccountedHostString(text string, mem *memAccount) *String {
	if len(text) < jsonPlainMin && isASCII(text) {
		return asciiString(text)
	}
	return newAccountedLazyString(text, lazyText, mem)
}

// hostString is FromHostString under the realm's memory limit: a lazy
// string's block is charged now and its decoded storage on first read.
func (r *Realm) hostString(text string) *String {
	s := fromAccountedHostString(text, r.mem())
	r.accountLazy(s)
	return s
}

// lazyString retains parser-created strings, charged as hostString's. Under
// a memory limit the parse's text is their share root (shareText), as for
// plain's strings, so the live measurement counts the text once.
func (r *Realm) lazyString(text string, mode lazyStringMode) *String {
	s := newAccountedLazyString(text, mode, r.mem())
	if m := r.mem(); m != nil && s.isLazy() {
		s.chargedData().root = m.jsonText
	}
	r.accountLazy(s)
	return s
}

// accountLazy charges a lazy s's block to the realm's memory limit and
// records the account on it. Other strings and realms without a limit are
// unchanged.
func (r *Realm) accountLazy(s *String) {
	m := r.mem()
	if m == nil || !s.isLazy() {
		return
	}
	r.chargeString(s.lazyBlockSize() - stringHeaderSize)
}
