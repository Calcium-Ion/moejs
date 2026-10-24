package whatwgurl

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// hostFlags classifies ASCII bytes for the host parser.
const (
	forbiddenHost   uint8 = 1 << iota // a forbidden host code point
	forbiddenDomain                   // a forbidden domain code point
	upperASCII                        // A-Z
)

var hostBytes = func() (t [128]uint8) {
	for _, c := range "\x00\t\n\r #/:<>?@[\\]^|" {
		t[c] |= forbiddenHost | forbiddenDomain
	}
	for c := 0; c < 0x20; c++ {
		t[c] |= forbiddenDomain
	}
	t['%'] |= forbiddenDomain
	t[0x7f] |= forbiddenDomain
	for c := 'A'; c <= 'Z'; c++ {
		t[c] |= upperASCII
	}
	return t
}()

// parseHost runs the host parser (URL Standard §3.5) on input and returns
// the host, serialized.
func (o *Options) parseHost(input string, isOpaque bool) (string, bool) {
	if input != "" && input[0] == '[' {
		if input[len(input)-1] != ']' || len(input) < 2 {
			return "", false
		}
		a, ok := parseIPv6(input[1 : len(input)-1])
		if !ok {
			return "", false
		}
		var buf [48]byte
		b := append(buf[:0], '[')
		b = appendIPv6(b, &a)
		b = append(b, ']')
		if string(b) == input {
			return input, true
		}
		return string(b), true
	}
	if isOpaque {
		return parseOpaqueHost(input)
	}
	domain := input
	if strings.IndexByte(input, '%') >= 0 {
		domain = string(appendDecoded(make([]byte, 0, len(input)), input, false))
	}
	// A domain that is not UTF-8 holds U+FFFD after decoding, which the
	// domain parser fails on.
	var ascii string
	if isASCII(domain) {
		ascii = domain
		var flags uint8
		for i := 0; i < len(domain); i++ {
			flags |= hostBytes[domain[i]]
		}
		if flags&forbiddenDomain != 0 {
			return "", false
		}
		if flags&upperASCII != 0 {
			ascii = strings.ToLower(domain)
		}
	} else {
		if !utf8.ValidString(domain) {
			return "", false
		}
		var ok bool
		if ascii, ok = o.domainToASCII(domain); !ok || ascii == "" {
			return "", false
		}
		for i := 0; i < len(ascii); i++ {
			if c := ascii[i]; c >= 0x80 || hostBytes[c]&forbiddenDomain != 0 {
				return "", false
			}
		}
	}
	if endsInNumber(ascii) {
		a, ok := parseIPv4(ascii)
		if !ok {
			return "", false
		}
		var buf [15]byte
		b := appendIPv4(buf[:0], a)
		if string(b) == ascii {
			return ascii, true
		}
		return string(b), true
	}
	return ascii, true
}

// parseOpaqueHost runs the opaque-host parser.
func parseOpaqueHost(input string) (string, bool) {
	for i := 0; i < len(input); i++ {
		if c := input[i]; c < 0x80 && hostBytes[c]&forbiddenHost != 0 {
			return "", false
		}
	}
	return encode(input, c0Set), true
}

// endsInNumber runs the ends in a number checker on the ASCII string s.
func endsInNumber(s string) bool {
	if s != "" && s[len(s)-1] == '.' {
		s = s[:len(s)-1]
	}
	last := s[strings.LastIndexByte(s, '.')+1:]
	if last == "" {
		return false
	}
	digits := true
	for i := 0; i < len(last); i++ {
		if last[i] < '0' || last[i] > '9' {
			digits = false
			break
		}
	}
	if digits {
		return true
	}
	_, _, ok := parseIPv4Number(last)
	return ok
}

// parseIPv4Number runs the IPv4 number parser. A value above 2^32 is
// reported as 2^32 + 1: the IPv4 parser fails on it either way.
func parseIPv4Number(s string) (n uint64, nonDecimal, ok bool) {
	if s == "" {
		return 0, false, false
	}
	radix := uint64(10)
	if len(s) >= 2 && s[0] == '0' && s[1]|0x20 == 'x' {
		s, radix, nonDecimal = s[2:], 16, true
	} else if len(s) >= 2 && s[0] == '0' {
		s, radix, nonDecimal = s[1:], 8, true
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		var d uint64
		switch {
		case '0' <= c && c <= '9':
			d = uint64(c - '0')
		case radix == 16 && isHex(c):
			d = uint64(unhex(c))
		default:
			return 0, false, false
		}
		if d >= radix {
			return 0, false, false
		}
		if n = n*radix + d; n > 1<<32 {
			n = 1<<32 + 1
		}
	}
	return n, nonDecimal, true
}

// parseIPv4 runs the IPv4 parser on the ASCII string s.
func parseIPv4(s string) (uint32, bool) {
	if s != "" && s[len(s)-1] == '.' && len(s) > 1 {
		s = s[:len(s)-1]
	}
	if strings.Count(s, ".") > 3 {
		return 0, false
	}
	var numbers [4]uint64
	n := 0
	for {
		part, rest, more := strings.Cut(s, ".")
		v, _, ok := parseIPv4Number(part)
		if !ok {
			return 0, false
		}
		numbers[n] = v
		n++
		if !more {
			break
		}
		s = rest
	}
	for _, v := range numbers[:n-1] {
		if v > 255 {
			return 0, false
		}
	}
	ipv4 := numbers[n-1]
	if ipv4 >= 1<<(8*(5-n)) {
		return 0, false
	}
	for i, v := range numbers[:n-1] {
		ipv4 += v << (8 * (3 - i))
	}
	return uint32(ipv4), true
}

func appendIPv4(b []byte, a uint32) []byte {
	for i := 3; i >= 0; i-- {
		b = strconv.AppendUint(b, uint64(a>>(8*i)&0xff), 10)
		if i > 0 {
			b = append(b, '.')
		}
	}
	return b
}

// parseIPv6 runs the IPv6 parser.
func parseIPv6(s string) (a [8]uint16, ok bool) {
	pieceIndex, compress := 0, -1
	p := 0
	c := func() int {
		if p < len(s) {
			return int(s[p])
		}
		return -1
	}
	if c() == ':' {
		if p+1 >= len(s) || s[p+1] != ':' {
			return a, false
		}
		p += 2
		pieceIndex++
		compress = pieceIndex
	}
	for c() != -1 {
		if pieceIndex == 8 {
			return a, false
		}
		if c() == ':' {
			if compress != -1 {
				return a, false
			}
			p++
			pieceIndex++
			compress = pieceIndex
			continue
		}
		value, length := 0, 0
		for length < 4 && p < len(s) && isHex(s[p]) {
			value = value<<4 | int(unhex(s[p]))
			p++
			length++
		}
		if c() == '.' {
			if length == 0 {
				return a, false
			}
			p -= length
			if pieceIndex > 6 {
				return a, false
			}
			numbersSeen := 0
			for c() != -1 {
				ipv4Piece := -1
				if numbersSeen > 0 {
					if c() == '.' && numbersSeen < 4 {
						p++
					} else {
						return a, false
					}
				}
				if d := c(); d < '0' || d > '9' {
					return a, false
				}
				for d := c(); '0' <= d && d <= '9'; d = c() {
					number := d - '0'
					switch ipv4Piece {
					case -1:
						ipv4Piece = number
					case 0:
						return a, false
					default:
						ipv4Piece = ipv4Piece*10 + number
					}
					if ipv4Piece > 255 {
						return a, false
					}
					p++
				}
				a[pieceIndex] = a[pieceIndex]<<8 | uint16(ipv4Piece)
				numbersSeen++
				if numbersSeen == 2 || numbersSeen == 4 {
					pieceIndex++
				}
			}
			if numbersSeen != 4 {
				return a, false
			}
			break
		} else if c() == ':' {
			p++
			if c() == -1 {
				return a, false
			}
		} else if c() != -1 {
			return a, false
		}
		a[pieceIndex] = uint16(value)
		pieceIndex++
	}
	if compress != -1 {
		swaps := pieceIndex - compress
		pieceIndex = 7
		for pieceIndex != 0 && swaps > 0 {
			a[pieceIndex], a[compress+swaps-1] = a[compress+swaps-1], a[pieceIndex]
			pieceIndex--
			swaps--
		}
	} else if pieceIndex != 8 {
		return a, false
	}
	return a, true
}

// appendIPv6 runs the IPv6 serializer.
func appendIPv6(b []byte, a *[8]uint16) []byte {
	// The compressed piece index: the first longest run of two or more
	// zero pieces.
	compress, longest := -1, 1
	for i := 0; i < 8; {
		if a[i] != 0 {
			i++
			continue
		}
		j := i
		for j < 8 && a[j] == 0 {
			j++
		}
		if j-i > longest {
			compress, longest = i, j-i
		}
		i = j
	}
	for i := 0; i < 8; i++ {
		if i == compress {
			if i == 0 {
				b = append(b, "::"...)
			} else {
				b = append(b, ':')
			}
			i += longest - 1
			continue
		}
		b = strconv.AppendUint(b, uint64(a[i]), 16)
		if i != 7 {
			b = append(b, ':')
		}
	}
	return b
}
