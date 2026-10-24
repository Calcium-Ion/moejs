package whatwgurl

import (
	"strings"
	"unicode/utf8"
)

//go:generate go run ../../../internal/gen/idna -o idna_tables.go

// Options holds what the parser cannot do on its own. The zero value, and a
// nil *Options, parse without Unicode normalization.
//
// A URL keeps the Options it was parsed with, and its setters use them.
type Options struct {
	// NFC, when not nil, returns the Unicode Normalization Form C of s. Domain
	// to ASCII (UTS #46) normalizes a host with non-ASCII input to NFC after
	// mapping, and fails a label that Punycode decodes to a string that is not
	// in NFC. Without NFC both steps are skipped; that changes the result only
	// for a host whose mapped input is not in NFC. NFC is called only while
	// parsing a host that holds non-ASCII input, on the goroutine that parses.
	NFC func(s string) string
}

// The kinds of an idnaTable range, as internal/gen/idna describes them.
const (
	kindDisallowed = iota
	kindIgnored
	kindValid
	kindPairs
	kindDelta
	kindStrings
)

// The properties of a valid code point in idnaProps.
const (
	bidiL = iota
	bidiR
	bidiAL
	bidiAN
	bidiEN
	bidiES
	bidiCS
	bidiET
	bidiON
	bidiBN
	bidiNSM
	bidiOther
)

const (
	joinU = iota
	joinL
	joinR
	joinD
	joinT
)

const (
	propJoinShift = 4
	propMark      = 1 << 7
	propVirama    = 1 << 8
	propBits      = 9
)

// idnaLookup returns the kind of r's range, its first code point and data.
func idnaLookup(r rune) (kind int, start rune, data uint32) {
	key := uint32(r)<<11 | (1<<11 - 1)
	lo, hi := 0, len(idnaTable)
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		if idnaTable[m] <= key {
			lo = m + 1
		} else {
			hi = m
		}
	}
	e := idnaTable[lo-1]
	start = rune(e >> 11)
	if e&(1<<10) == 0 {
		return int(e & 3), start, 0
	}
	data = idnaData[e&(1<<10-1)]
	if data&(1<<31) == 0 {
		return kindDelta, start, data
	}
	return kindStrings, start, data &^ (1 << 31)
}

// idnaProp returns the properties of the valid code point r.
func idnaProp(r rune) uint32 {
	key := uint32(r)<<propBits | (1<<propBits - 1)
	lo, hi := 0, len(idnaProps)
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		if idnaProps[m] <= key {
			lo = m + 1
		} else {
			hi = m
		}
	}
	return idnaProps[lo-1] & (1<<propBits - 1)
}

// idnaValid reports whether r has status valid or deviation.
func idnaValid(r rune) bool {
	kind, start, _ := idnaLookup(r)
	return kind == kindValid || kind == kindPairs && (r-start)&1 == 1
}

// domainToASCII runs Unicode ToASCII (UTS #46 §4.2) on the domain s, which
// is valid UTF-8 and not all ASCII, with CheckHyphens false, CheckBidi true,
// CheckJoiners true, UseSTD3ASCIIRules false, Transitional_Processing false,
// VerifyDnsLength false and IgnoreInvalidPunycode false: the URL Standard's
// domain parser ToASCII with beStrict false. It reports false where UTS #46
// records an error.
func (o *Options) domainToASCII(s string) (string, bool) {
	// Map (UTS #46 §4 step 1). A disallowed code point fails the label it is
	// in, and with it the domain.
	mapped := make([]byte, 0, len(s)+8)
	for _, r := range s {
		kind, start, data := idnaLookup(r)
		switch kind {
		case kindDisallowed:
			return "", false
		case kindIgnored:
		case kindValid:
			mapped = utf8.AppendRune(mapped, r)
		case kindPairs:
			mapped = utf8.AppendRune(mapped, r+1-(r-start)&1)
		case kindDelta:
			mapped = utf8.AppendRune(mapped, r+rune(data)-1<<21)
		case kindStrings:
			off, stride, n := int(data>>16), int(data>>8&0xff), int(data&0xff)
			off += int(r-start) * stride
			mapped = append(mapped, idnaPool[off:off+n]...)
		}
	}
	// Normalize (step 2).
	m := string(mapped)
	if o != nil && o.NFC != nil {
		m = o.NFC(m)
	}
	// Break and Convert/Validate (steps 3 and 4).
	var labels [][]rune
	bidi := false
	for {
		l, rest, more := strings.Cut(m, ".")
		var label []rune
		if strings.HasPrefix(l, "xn--") {
			if !isASCII(l) {
				return "", false
			}
			var ok bool
			label, ok = punyDecode(nil, l[4:])
			if !ok || isASCIIRunes(label) {
				return "", false
			}
			if o != nil && o.NFC != nil {
				if d := string(label); o.NFC(d) != d {
					return "", false
				}
			}
		} else {
			label = []rune(l)
		}
		if !validLabel(label) {
			return "", false
		}
		for _, r := range label {
			if r >= 0x80 {
				if b := idnaProp(r) & 0xf; b == bidiR || b == bidiAL || b == bidiAN {
					bidi = true
				}
			}
		}
		labels = append(labels, label)
		if !more {
			break
		}
		m = rest
	}
	if bidi {
		for _, label := range labels {
			if len(label) > 0 && !bidiRule(label) {
				return "", false
			}
		}
	}
	// ToASCII step 3: the labels with non-ASCII code points in Punycode.
	out := make([]byte, 0, len(s)+16)
	for i, label := range labels {
		if i > 0 {
			out = append(out, '.')
		}
		if isASCIIRunes(label) {
			for _, r := range label {
				out = append(out, byte(r))
			}
			continue
		}
		out = append(out, "xn--"...)
		var ok bool
		if out, ok = punyEncode(out, label); !ok {
			return "", false
		}
	}
	return string(out), true
}

// validLabel checks the validity criteria of UTS #46 §4.1 for
// nontransitional processing with CheckHyphens false and CheckJoiners true,
// but for NFC, which the caller checks. Every code point must be valid,
// which also excludes U+002E (.).
func validLabel(label []rune) bool {
	if len(label) == 0 {
		return true
	}
	if len(label) >= 4 && label[0] == 'x' && label[1] == 'n' && label[2] == '-' && label[3] == '-' {
		return false
	}
	if idnaProp(label[0])&propMark != 0 {
		return false
	}
	for i, r := range label {
		if !idnaValid(r) {
			return false
		}
		if r != 0x200c && r != 0x200d {
			continue
		}
		// The CONTEXTJ rules of RFC 5892 Appendix A.1 and A.2.
		if i > 0 && idnaProp(label[i-1])&propVirama != 0 {
			continue
		}
		if r == 0x200d {
			return false
		}
		j := i - 1
		for j >= 0 && joinType(label[j]) == joinT {
			j--
		}
		if j < 0 || joinType(label[j]) != joinL && joinType(label[j]) != joinD {
			return false
		}
		k := i + 1
		for k < len(label) && joinType(label[k]) == joinT {
			k++
		}
		if k == len(label) || joinType(label[k]) != joinR && joinType(label[k]) != joinD {
			return false
		}
	}
	return true
}

func joinType(r rune) uint32 { return idnaProp(r) >> propJoinShift & 7 }

// bidiRule checks the six conditions of the Bidi Rule (RFC 5893 §2) on a
// non-empty label of a Bidi domain name.
func bidiRule(label []rune) bool {
	var allowed uint32
	rtl := false
	switch idnaProp(label[0]) & 0xf {
	case bidiL:
		allowed = 1<<bidiL | 1<<bidiEN | 1<<bidiES | 1<<bidiCS | 1<<bidiET | 1<<bidiON | 1<<bidiBN | 1<<bidiNSM
	case bidiR, bidiAL:
		rtl = true
		allowed = 1<<bidiR | 1<<bidiAL | 1<<bidiAN | 1<<bidiEN | 1<<bidiES | 1<<bidiCS | 1<<bidiET | 1<<bidiON | 1<<bidiBN | 1<<bidiNSM
	default:
		return false
	}
	var seen uint32
	last := uint32(bidiL)
	for _, r := range label {
		b := idnaProp(r) & 0xf
		seen |= 1 << b
		if b != bidiNSM {
			last = b
		}
	}
	if seen&^allowed != 0 {
		return false
	}
	if rtl {
		if seen&(1<<bidiEN) != 0 && seen&(1<<bidiAN) != 0 {
			return false
		}
		return last == bidiR || last == bidiAL || last == bidiEN || last == bidiAN
	}
	return last == bidiL || last == bidiEN
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func isASCIIRunes(rs []rune) bool {
	for _, r := range rs {
		if r >= 0x80 {
			return false
		}
	}
	return true
}
