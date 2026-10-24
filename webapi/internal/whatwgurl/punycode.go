package whatwgurl

// Punycode (RFC 3492) with the parameters of IDNA.

const (
	pcBase        = 36
	pcTMin        = 1
	pcTMax        = 26
	pcSkew        = 38
	pcDamp        = 700
	pcInitialBias = 72
	pcInitialN    = 128
	pcMaxInt      = 1<<31 - 1
)

func pcAdapt(delta, numPoints int, first bool) int {
	if first {
		delta /= pcDamp
	} else {
		delta /= 2
	}
	delta += delta / numPoints
	k := 0
	for delta > (pcBase-pcTMin)*pcTMax/2 {
		delta /= pcBase - pcTMin
		k += pcBase
	}
	return k + (pcBase-pcTMin+1)*delta/(delta+pcSkew)
}

func pcThreshold(k, bias int) int {
	switch {
	case k <= bias:
		return pcTMin
	case k >= bias+pcTMax:
		return pcTMax
	}
	return k - bias
}

// punyDecode appends the code points s encodes to dst. It fails on a
// character that is no Punycode digit, an overflow, and a code point that
// is a surrogate or above U+10FFFF.
func punyDecode(dst []rune, s string) ([]rune, bool) {
	start := len(dst)
	in := 0
	for b := len(s) - 1; b > 0; b-- {
		if s[b] == '-' {
			for i := range b {
				if s[i] >= 0x80 {
					return dst, false
				}
				dst = append(dst, rune(s[i]))
			}
			in = b + 1
			break
		}
	}
	n, i, bias := pcInitialN, 0, pcInitialBias
	for in < len(s) {
		oldi, w := i, 1
		for k := pcBase; ; k += pcBase {
			if in >= len(s) {
				return dst, false
			}
			c := s[in]
			in++
			var digit int
			switch {
			case '0' <= c && c <= '9':
				digit = int(c-'0') + 26
			case 'a' <= c|0x20 && c|0x20 <= 'z':
				digit = int((c | 0x20) - 'a')
			default:
				return dst, false
			}
			if digit > (pcMaxInt-i)/w {
				return dst, false
			}
			i += digit * w
			t := pcThreshold(k, bias)
			if digit < t {
				break
			}
			if w > pcMaxInt/(pcBase-t) {
				return dst, false
			}
			w *= pcBase - t
		}
		out := len(dst) - start + 1
		bias = pcAdapt(i-oldi, out, oldi == 0)
		if i/out > pcMaxInt-n {
			return dst, false
		}
		n += i / out
		i %= out
		if n > 0x10ffff || 0xd800 <= n && n <= 0xdfff {
			return dst, false
		}
		dst = append(dst, 0)
		copy(dst[start+i+1:], dst[start+i:])
		dst[start+i] = rune(n)
		i++
	}
	return dst, true
}

// punyEncode appends the Punycode encoding of label to dst. It fails only on
// an overflow, which a label of fewer than about 4,000 code points cannot
// cause.
func punyEncode(dst []byte, label []rune) ([]byte, bool) {
	b := 0
	for _, r := range label {
		if r < 0x80 {
			dst = append(dst, byte(r))
			b++
		}
	}
	if b > 0 {
		dst = append(dst, '-')
	}
	n, delta, bias := pcInitialN, 0, pcInitialBias
	for h := b; h < len(label); {
		m := pcMaxInt
		for _, r := range label {
			if int(r) >= n && int(r) < m {
				m = int(r)
			}
		}
		if m-n > (pcMaxInt-delta)/(h+1) {
			return dst, false
		}
		delta += (m - n) * (h + 1)
		n = m
		for _, r := range label {
			if int(r) < n {
				if delta++; delta > pcMaxInt-1 {
					return dst, false
				}
			}
			if int(r) != n {
				continue
			}
			q := delta
			for k := pcBase; ; k += pcBase {
				t := pcThreshold(k, bias)
				if q < t {
					break
				}
				dst = append(dst, pcDigit(t+(q-t)%(pcBase-t)))
				q = (q - t) / (pcBase - t)
			}
			dst = append(dst, pcDigit(q))
			bias = pcAdapt(delta, h+1, h == b)
			delta = 0
			h++
		}
		delta++
		n++
	}
	return dst, true
}

func pcDigit(d int) byte {
	if d < 26 {
		return byte('a' + d)
	}
	return byte('0' + d - 26)
}
