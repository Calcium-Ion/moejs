package whatwgurl

import (
	"strings"
	"unicode/utf8"
)

// Pair is a name-value tuple of application/x-www-form-urlencoded.
type Pair struct{ Name, Value string }

// ParseForm runs the application/x-www-form-urlencoded parser (URL Standard
// §5.1) on the UTF-8 encoding s. A name or value holds U+FFFD for bytes that
// do not decode as UTF-8 after percent-decoding.
func ParseForm(s string) []Pair {
	if s == "" {
		return nil
	}
	out := make([]Pair, 0, strings.Count(s, "&")+1)
	for s != "" {
		seq := s
		if i := strings.IndexByte(s, '&'); i >= 0 {
			seq, s = s[:i], s[i+1:]
		} else {
			s = ""
		}
		if seq == "" {
			continue
		}
		name, value, _ := strings.Cut(seq, "=")
		out = append(out, Pair{formDecode(name), formDecode(value)})
	}
	return out
}

// formDecode replaces + with a space, percent-decodes and UTF-8 decodes s;
// it returns s itself when that changes nothing.
func formDecode(s string) string {
	if strings.IndexByte(s, '%') < 0 && strings.IndexByte(s, '+') < 0 && utf8.ValidString(s) {
		return s
	}
	b := appendDecoded(make([]byte, 0, len(s)), s, true)
	if utf8.Valid(b) {
		return string(b)
	}
	return string(appendUTF8Decode(nil, b))
}

// AppendForm appends the application/x-www-form-urlencoded serialization
// (URL Standard §5.2) of pairs to dst. Names and values must be valid UTF-8.
func AppendForm(dst []byte, pairs []Pair) []byte {
	for i, p := range pairs {
		if i > 0 {
			dst = append(dst, '&')
		}
		dst = appendFormEncoded(dst, p.Name)
		dst = append(dst, '=')
		dst = appendFormEncoded(dst, p.Value)
	}
	return dst
}

func appendFormEncoded(dst []byte, s string) []byte {
	for i := 0; i < len(s); i++ {
		switch b := s[i]; {
		case b == ' ':
			dst = append(dst, '+')
		case pctSets[b]&formSet != 0:
			dst = append(dst, '%', upperHex[b>>4], upperHex[b&15])
		default:
			dst = append(dst, b)
		}
	}
	return dst
}
