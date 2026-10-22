package engine

import (
	"sync"
	"sync/atomic"
	"unicode/utf8"
	"unsafe"
)

// lazyStringMode fixes how retained bytes are decoded. Reads may populate
// caches, but never change the mode or the retained bytes. The text modes
// come first (lazyStringData.textMode).
type lazyStringMode uint8

const (
	// lazyText holds Go text: invalid UTF-8 bytes decode to U+FFFD, as
	// FromGoString replaces them.
	lazyText lazyStringMode = iota

	// lazyWTF8 holds text that may encode lone UTF-16 surrogates, which
	// decoding preserves.
	lazyWTF8

	// lazyJSONContents holds JSON contents without quotes; strSlow checked
	// their JSON syntax. Byte runs may be WTF-8.
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
// together, to concurrent readers through once. facts and hash are caches
// that racing readers may fill twice with the same value.
//
// mem is the account of the realm that created the string, when that realm
// had a memory limit. It is written once, before the string is published,
// and resolve adds the decoded storage to mem.flat. Decoding can run on any
// goroutine, so it does not touch the account's other fields. The realm's
// goroutine folds flat in and applies the limit at the next loop back-edge,
// call return or charge. A string created with no limit keeps mem nil and
// its decoding is not charged. One whose realm later drops the limit still
// adds to the old account, which nothing reads.
type lazyStringData struct {
	once    sync.Once
	facts   atomic.Uint32
	hash    atomic.Uint32 // 0 = not computed
	mode    lazyStringMode
	mem     *memAccount
	decoded *String
}

func (d *lazyStringData) textMode() bool { return d.mode <= lazyWTF8 }

// validUTF8 reports whether text, the retained bytes, is the UTF-8 of the
// string's content: text mode bytes that are valid UTF-8 (which encodes no
// surrogates). The check runs once.
func (d *lazyStringData) validUTF8(text string) bool {
	if !d.textMode() {
		return false
	}
	f := d.facts.Load()
	if f&(factUTF8Valid|factUTF8Invalid) == 0 {
		f = factUTF8Invalid
		if utf8.ValidString(text) {
			f = factUTF8Valid
		}
		d.facts.Or(f)
	}
	return f&factUTF8Valid != 0
}

// plainJSON reports whether text can be copied between JSON quotes as ASCII
// without escaping. Only text mode strings of at least jsonPlainMin bytes
// are scanned; false can also mean the mode or length is ineligible.
func (d *lazyStringData) plainJSON(text string) bool {
	if len(text) < jsonPlainMin || !d.textMode() {
		return false
	}
	f := d.facts.Load()
	if f&(factJSONPlain|factJSONEscapes) == 0 {
		ascii, plain := jsonASCII(text)
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
// the original String keeps its source and n == -1. The decoded storage is
// charged to mem, when the string was created under a memory limit.
func (d *lazyStringData) resolve(text string) *String {
	d.once.Do(func() {
		var n int
		d.decoded, n = decodeLazyString(text, d.mode)
		if d.textMode() && d.decoded.kind == strASCII {
			d.facts.Or(factUTF8Valid)
		}
		d.facts.Or(factDecoded)
		if d.mem != nil {
			d.mem.flat.Add(int64(n))
		}
	})
	return d.decoded
}

// published returns the decoded String once resolve has set it, without
// waiting for a resolve in progress (the live measurement).
func (d *lazyStringData) published() *String {
	if d.facts.Load()&factDecoded == 0 {
		return nil
	}
	return d.decoded
}

// decodeLazyString reuses the existing decoders. JSON modes require
// syntax-checked contents. The integer is the heap storage the result
// retains, header included, which a memory limit charges.
func decodeLazyString(text string, mode lazyStringMode) (*String, int) {
	if mode == lazyJSONContents || mode == lazyJSONVerbatim {
		var sb StringBuilder
		decodeJSONContents(&sb, text)
		n := sb.retained()
		return sb.String(), n + stringHeaderSize
	}
	if isASCIILong(text) {
		return asciiString(text), stringHeaderSize
	}
	u := make([]uint16, 0, len(text)) // a byte decodes to at most one unit
	if mode == lazyWTF8 {
		u = appendWTF8Units(u, text)
	} else {
		u = appendUTF16(u, text)
	}
	return FromUTF16(u), 2*cap(u) + stringHeaderSize
}

// lazyStringBlock keeps a String and its cache in one allocation.
type lazyStringBlock struct {
	value String
	data  lazyStringData
}

func newLazyString(text string, mode lazyStringMode) *String {
	if len(text) == 0 {
		return emptyString
	}
	b := &lazyStringBlock{}
	b.data.mode = mode
	b.value = String{s: text, p: unsafe.Pointer(&b.data), n: -1, kind: strLazy}
	return &b.value
}

func (s *String) isLazy() bool { return s.kind == strLazy }

func (s *String) lazyData() *lazyStringData { return (*lazyStringData)(s.p) }
func (s *String) resolved() *String         { return s.lazyData().resolve(s.s) }

// sourceUTF8 returns the retained bytes of a lazy string when they are the
// UTF-8 of its content, which exports then share without decoding.
func (s *String) sourceUTF8() (string, bool) {
	if s.isLazy() && s.lazyData().validUTF8(s.s) {
		return s.s, true
	}
	return "", false
}

// sameLazyContent reports that two lazy strings retaining the same bytes
// text decode to the same content: one mode, or text modes and valid UTF-8,
// which the text modes decode alike.
func sameLazyContent(a, b *lazyStringData, text string) bool {
	return a.mode == b.mode || b.textMode() && a.validUTF8(text)
}

// lazyHash is Hash of a lazy string, cached in its holder: the String's
// own hash field is never written once a lazy string is shared.
func (s *String) lazyHash() uint32 {
	d := s.lazyData()
	if h := d.hash.Load(); h != 0 {
		return h
	}
	h := d.resolve(s.s).contentHash()
	d.hash.Store(h)
	return h
}

// FromHostString is FromGoString with the decoding deferred: a string
// shorter than jsonPlainMin that is ASCII is converted as FromGoString
// converts it, and any other keeps its bytes until its content or length is
// first read. GoString of it shares the bytes when they are valid UTF-8.
func FromHostString(text string) *String {
	if len(text) < jsonPlainMin && isASCII(text) {
		return asciiString(text)
	}
	return newLazyString(text, lazyText)
}

// hostString is FromHostString under the realm's memory limit: a lazy
// string's block is charged now and its decoded storage on first read.
func (r *Realm) hostString(text string) *String {
	s := FromHostString(text)
	r.accountLazy(s)
	return s
}

// lazyString retains parser-created strings, charged as hostString's. Under
// a memory limit the parse's text is their share root (shareText), as for
// plain's strings, so the live measurement counts the text once.
func (r *Realm) lazyString(text string, mode lazyStringMode) *String {
	s := newLazyString(text, mode)
	r.accountLazy(s)
	if m := r.mem(); m != nil && s.isLazy() {
		s.right = m.jsonText
	}
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
	s.lazyData().mem = m
	r.chargeString(int(unsafe.Sizeof(lazyStringBlock{})) - stringHeaderSize)
}
