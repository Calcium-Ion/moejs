package whatwgurl

import (
	"strings"
	"unicode/utf8"
)

// The percent-encode sets of the URL Standard (§1.3), as bits of pctSets. A
// set holds every byte from 0x80 up: the UTF-8 encoding of a code point
// above U+007E is percent-encoded byte by byte.
const (
	c0Set           uint8 = 1 << iota // C0 control percent-encode set
	fragmentSet                       // fragment percent-encode set
	querySet                          // query percent-encode set
	specialQuerySet                   // special-query percent-encode set
	pathSet                           // path percent-encode set
	userinfoSet                       // userinfo percent-encode set
	componentSet                      // component percent-encode set
	formSet                           // application/x-www-form-urlencoded percent-encode set
)

// pctSets holds, for every byte, the sets it is in.
var pctSets = func() (t [256]uint8) {
	for b := 0; b < 256; b++ {
		var s uint8
		if b < 0x20 || b > 0x7e {
			s = c0Set
		}
		if s != 0 || strings.IndexByte(" \"<>`", byte(b)) >= 0 {
			s |= fragmentSet
		}
		q := s&c0Set != 0 || strings.IndexByte(" \"#<>", byte(b)) >= 0
		if q {
			s |= querySet
		}
		if q || b == '\'' {
			s |= specialQuerySet
		}
		p := q || strings.IndexByte("?^`{}", byte(b)) >= 0
		if p {
			s |= pathSet
		}
		u := p || strings.IndexByte("/:;=@[\\]|", byte(b)) >= 0
		if u {
			s |= userinfoSet
		}
		c := u || strings.IndexByte("$%&+,", byte(b)) >= 0
		if c {
			s |= componentSet
		}
		if c || strings.IndexByte("!'()~", byte(b)) >= 0 {
			s |= formSet
		}
		t[b] = s
	}
	return t
}()

const upperHex = "0123456789ABCDEF"

// needsEncoding returns the index of the first byte of s in set, or -1.
func needsEncoding(s string, set uint8) int {
	for i := 0; i < len(s); i++ {
		if pctSets[s[i]]&set != 0 {
			return i
		}
	}
	return -1
}

// appendEncoded appends s to dst, UTF-8 percent-encoding every byte in set.
func appendEncoded(dst []byte, s string, set uint8) []byte {
	for i := 0; i < len(s); i++ {
		b := s[i]
		if pctSets[b]&set != 0 {
			dst = append(dst, '%', upperHex[b>>4], upperHex[b&15])
		} else {
			dst = append(dst, b)
		}
	}
	return dst
}

// encode returns s with every byte in set UTF-8 percent-encoded; s itself
// when it has none.
func encode(s string, set uint8) string {
	i := needsEncoding(s, set)
	if i < 0 {
		return s
	}
	n := len(s)
	for j := i; j < len(s); j++ {
		if pctSets[s[j]]&set != 0 {
			n += 2
		}
	}
	b := make([]byte, 0, n)
	b = append(b, s[:i]...)
	return string(appendEncoded(b, s[i:], set))
}

func isHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c|0x20 && c|0x20 <= 'f'
}

func unhex(c byte) byte {
	if c <= '9' {
		return c - '0'
	}
	return (c | 0x20) - 'a' + 10
}

// appendDecoded appends the percent-decoding of s to dst (URL Standard
// §1.3); with plus, a 0x2B (+) becomes a 0x20 (SP) first.
func appendDecoded(dst []byte, s string, plus bool) []byte {
	for i := 0; i < len(s); i++ {
		b := s[i]
		switch {
		case b == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]):
			dst = append(dst, unhex(s[i+1])<<4|unhex(s[i+2]))
			i += 2
		case b == '+' && plus:
			dst = append(dst, ' ')
		default:
			dst = append(dst, b)
		}
	}
	return dst
}

// appendUTF8Decode appends b decoded as UTF-8 without BOM to dst, replacing
// every maximal ill-formed subsequence with U+FFFD, as the Encoding
// Standard's UTF-8 decoder does (which is not what a Go range loop does).
func appendUTF8Decode(dst, b []byte) []byte {
	if utf8.Valid(b) {
		return append(dst, b...)
	}
	var cp rune
	needed, seen := 0, 0
	lower, upper := byte(0x80), byte(0xbf)
	for i := 0; i < len(b); i++ {
		x := b[i]
		if needed == 0 {
			switch {
			case x < 0x80:
				dst = append(dst, x)
			case 0xc2 <= x && x <= 0xdf:
				needed, cp = 1, rune(x&0x1f)
			case 0xe0 <= x && x <= 0xef:
				if x == 0xe0 {
					lower = 0xa0
				} else if x == 0xed {
					upper = 0x9f
				}
				needed, cp = 2, rune(x&0xf)
			case 0xf0 <= x && x <= 0xf4:
				if x == 0xf0 {
					lower = 0x90
				} else if x == 0xf4 {
					upper = 0x8f
				}
				needed, cp = 3, rune(x&7)
			default:
				dst = utf8.AppendRune(dst, utf8.RuneError)
			}
			continue
		}
		if x < lower || x > upper {
			needed, seen, cp = 0, 0, 0
			lower, upper = 0x80, 0xbf
			dst = utf8.AppendRune(dst, utf8.RuneError)
			i--
			continue
		}
		lower, upper = 0x80, 0xbf
		cp = cp<<6 | rune(x&0x3f)
		if seen++; seen == needed {
			dst = utf8.AppendRune(dst, cp)
			needed, seen, cp = 0, 0, 0
		}
	}
	if needed != 0 {
		dst = utf8.AppendRune(dst, utf8.RuneError)
	}
	return dst
}
