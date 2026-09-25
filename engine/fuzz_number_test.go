package engine

import (
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// Reference implementations of the spec's number conversions, written from
// ECMA-262 independently of numconv.go: exact decimal arithmetic in math/big
// for the digit-count formats, the Go regexp engine for the grammars, and
// strconv only for correctly rounded decimal-to-binary conversion and
// shortest round-trip digits.

// fuzzShortest returns the shortest round-trip digits of finite f > 0 and
// the decimal exponent of the first digit.
func fuzzShortest(f float64) (string, int) {
	s := strconv.FormatFloat(f, 'e', -1, 64)
	mant, exp, _ := strings.Cut(s, "e")
	e, _ := strconv.Atoi(exp)
	return strings.Replace(mant, ".", "", 1), e
}

// fuzzJSNumberString is Number::toString(x) with radix 10 (ES2023 6.1.6.1.20).
func fuzzJSNumberString(x float64) string {
	switch {
	case math.IsNaN(x):
		return "NaN"
	case x == 0:
		return "0"
	case x < 0:
		return "-" + fuzzJSNumberString(-x)
	case math.IsInf(x, 1):
		return "Infinity"
	}
	digits, e := fuzzShortest(x)
	k, n := len(digits), e+1
	switch {
	case k <= n && n <= 21:
		return digits + strings.Repeat("0", n-k)
	case 0 < n && n <= 21:
		return digits[:n] + "." + digits[n:]
	case -6 < n && n <= 0:
		return "0." + strings.Repeat("0", -n) + digits
	}
	return fuzzExpForm(digits, n-1)
}

// fuzzExpForm renders d[0].d[1:]e±exp.
func fuzzExpForm(digits string, exp int) string {
	s := digits[:1]
	if len(digits) > 1 {
		s += "." + digits[1:]
	}
	if exp < 0 {
		return s + "e-" + strconv.Itoa(-exp)
	}
	return s + "e+" + strconv.Itoa(exp)
}

func fuzzPow10(e int) *big.Rat {
	p := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(max(e, -e))), nil)
	if e < 0 {
		return new(big.Rat).SetFrac(big.NewInt(1), p)
	}
	return new(big.Rat).SetInt(p)
}

// fuzzRoundHalfUp is floor(q + 1/2) for q >= 0: the nearest integer, ties
// to the larger one ("if there are two such n, pick the larger n").
func fuzzRoundHalfUp(q *big.Rat) *big.Int {
	t := new(big.Rat).Add(q, big.NewRat(1, 2))
	return new(big.Int).Quo(t.Num(), t.Denom())
}

// fuzzDigits returns the p digits n (10^(p-1) <= n < 10^p) and exponent e
// for which n * 10^(e-p+1) is closest to x > 0, ties to the larger.
func fuzzDigits(x float64, p int) (string, int) {
	q := new(big.Rat).SetFloat64(x)
	e := int(math.Floor(math.Log10(x)))
	for fuzzPow10(e).Cmp(q) > 0 {
		e--
	}
	for fuzzPow10(e+1).Cmp(q) <= 0 {
		e++
	}
	n := fuzzRoundHalfUp(new(big.Rat).Mul(q, fuzzPow10(p-1-e)))
	s := n.String()
	if len(s) > p { // rounded up to 10^p
		s, e = s[:p], e+1
	}
	return s, e
}

// fuzzToFixed is Number.prototype.toFixed (ES2023 21.1.3.3); ok is false
// for the RangeError.
func fuzzToFixed(x float64, f int) (string, bool) {
	if f < 0 || f > 100 {
		return "", false
	}
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return fuzzJSNumberString(x), true
	}
	s := ""
	if x < 0 {
		s, x = "-", -x
	}
	if x >= 1e21 {
		return s + fuzzJSNumberString(x), true
	}
	m := fuzzRoundHalfUp(new(big.Rat).Mul(new(big.Rat).SetFloat64(x), fuzzPow10(f))).String()
	if f != 0 {
		if len(m) <= f {
			m = strings.Repeat("0", f+1-len(m)) + m
		}
		m = m[:len(m)-f] + "." + m[len(m)-f:]
	}
	return s + m, true
}

// fuzzToExponential is Number.prototype.toExponential (ES2023 21.1.3.2);
// f < 0 with undef set means fractionDigits is undefined.
func fuzzToExponential(x float64, f int, undef bool) (string, bool) {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return fuzzJSNumberString(x), true
	}
	if !undef && (f < 0 || f > 100) {
		return "", false
	}
	s := ""
	if x < 0 {
		s, x = "-", -x
	}
	var digits string
	var e int
	switch {
	case x == 0 && undef:
		digits = "0"
	case x == 0:
		digits = strings.Repeat("0", f+1)
	case undef:
		digits, e = fuzzShortest(x)
	default:
		digits, e = fuzzDigits(x, f+1)
	}
	return s + fuzzExpForm(digits, e), true
}

// fuzzToPrecision is Number.prototype.toPrecision (ES2023 21.1.3.5).
func fuzzToPrecision(x float64, p int, undef bool) (string, bool) {
	if undef || math.IsNaN(x) || math.IsInf(x, 0) {
		return fuzzJSNumberString(x), true
	}
	if p < 1 || p > 100 {
		return "", false
	}
	s := ""
	if x < 0 {
		s, x = "-", -x
	}
	m, e := strings.Repeat("0", p), 0
	if x != 0 {
		m, e = fuzzDigits(x, p)
	}
	switch {
	case e < -6 || e >= p:
		return s + fuzzExpForm(m, e), true
	case e == p-1:
		return s + m, true
	case e >= 0:
		return s + m[:e+1] + "." + m[e+1:], true
	}
	return s + "0." + strings.Repeat("0", -(e+1)) + m, true
}

// fuzzCallNumber calls a Number.prototype native on x and returns the
// string result, or ok=false for a RangeError.
func fuzzCallNumber(t *testing.T, r *Realm, fn NativeFunc, x float64, args []Value) (string, bool) {
	t.Helper()
	v, err := fn(r, NumberValue(x), args)
	if err != nil {
		if name := auditBErrName(err); !strings.HasPrefix(name, "RangeError:") {
			t.Fatalf("error %v, want a RangeError", err)
		}
		return "", false
	}
	return v.String(), true
}

// FuzzNumberFormat checks Number::toString and toFixed, toExponential and
// toPrecision over every float64 bit pattern and digit argument against
// the exact references above; toString must also read back as the same
// number.
func FuzzNumberFormat(f *testing.F) {
	for _, x := range []float64{0, math.Copysign(0, -1), 1, -1, 0.5, 1.5, 2.5, -2.5, 1.005, 1.45, 0.000001, 1e-7,
		123.456, 1e21, 999999999999999900000, 1e21 - 65536, 5e-324, math.MaxFloat64, 1 << 53, 9007199254740993,
		0.1, 0.3, 1 / 3.0, 25, 1.25e-5, 4.35, 1.0000000000000002, math.Inf(1), math.Inf(-1), math.NaN()} {
		for _, d := range []int{-1, 0, 1, 2, 5, 20, 21, 100, 101} {
			f.Add(math.Float64bits(x), d)
		}
	}
	r := NewRealm()
	f.Fuzz(func(t *testing.T, bits uint64, digits int) {
		x := math.Float64frombits(bits)
		want := fuzzJSNumberString(x)
		if got := NumberToGoString(x); got != want {
			t.Fatalf("NumberToGoString(%v) = %q, want %q", x, got, want)
		}
		if got := NumberToString(x).GoString(); got != want {
			t.Fatalf("NumberToString(%v) = %q, want %q", x, got, want)
		}
		if back, err := strconv.ParseFloat(want, 64); !math.IsNaN(x) && (err != nil || back != x) {
			t.Fatalf("toString(%v) = %q reads back as %v (%v)", x, want, back, err)
		}
		d := digits % 128
		dv := []Value{NumberValue(float64(d))}
		check := func(name string, fn NativeFunc, args []Value, want string, wantOK bool) {
			t.Helper()
			got, ok := fuzzCallNumber(t, r, fn, x, args)
			if ok != wantOK || got != want {
				t.Fatalf("(%v).%s(%v) = %q (ok=%v), want %q (ok=%v)", x, name, args, got, ok, want, wantOK)
			}
		}
		want, ok := fuzzToFixed(x, d)
		check("toFixed", numberProtoToFixed, dv, want, ok)
		want, ok = fuzzToExponential(x, d, false)
		check("toExponential", numberProtoToExponential, dv, want, ok)
		want, ok = fuzzToExponential(x, -1, true)
		check("toExponential", numberProtoToExponential, nil, want, ok)
		want, ok = fuzzToPrecision(x, d, false)
		check("toPrecision", numberProtoToPrecision, dv, want, ok)
		want, ok = fuzzToPrecision(x, 0, true)
		check("toPrecision", numberProtoToPrecision, nil, want, ok)
	})
}

// fuzzRadixPow is radix^e as an exact rational.
func fuzzRadixPow(radix, e int) *big.Rat {
	p := new(big.Int).Exp(big.NewInt(int64(radix)), big.NewInt(int64(max(e, -e))), nil)
	if e < 0 {
		return new(big.Rat).SetFrac(big.NewInt(1), p)
	}
	return new(big.Rat).SetInt(p)
}

// fuzzToStringRadix is Number::toString(x, radix) for radix != 10 by brute
// force: with radix^(n-1) <= |x| < radix^n, the least k for which one of the
// two k-digit multiples of radix^(n-k) around |x| reads back as x (the
// closer if both do, the even one if they tie; Note 2), in positional
// notation.
func fuzzToStringRadix(x float64, radix int) string {
	switch {
	case math.IsNaN(x):
		return "NaN"
	case x == 0:
		return "0"
	case math.IsInf(x, 0):
		if x < 0 {
			return "-Infinity"
		}
		return "Infinity"
	}
	sign := ""
	if x < 0 {
		sign, x = "-", -x
	}
	v := new(big.Rat).SetFloat64(x)
	n := int(math.Floor(math.Log(x)/math.Log(float64(radix)))) + 1
	for fuzzRadixPow(radix, n-1).Cmp(v) > 0 {
		n--
	}
	for fuzzRadixPow(radix, n).Cmp(v) <= 0 {
		n++
	}
	for k := 1; ; k++ {
		unit := fuzzRadixPow(radix, n-k)
		q := new(big.Rat).Quo(v, unit)
		lo := new(big.Int).Quo(q.Num(), q.Denom())
		hi := new(big.Int).Add(lo, big.NewInt(1))
		var found []*big.Int
		for _, m := range []*big.Int{lo, hi} {
			if f, _ := new(big.Rat).Mul(new(big.Rat).SetInt(m), unit).Float64(); f == x {
				found = append(found, m)
			}
		}
		if len(found) == 0 {
			continue
		}
		m := found[0]
		if len(found) == 2 {
			dlo := new(big.Rat).Sub(q, new(big.Rat).SetInt(lo))
			dhi := new(big.Rat).Sub(new(big.Rat).SetInt(hi), q)
			if c := dlo.Cmp(dhi); c > 0 || c == 0 && lo.Bit(0) == 1 {
				m = hi
			}
		}
		digits := m.Text(radix)
		if len(digits) > k { // m = radix^k: the value is radix^n
			digits, n = "1", n+1
		}
		digits = strings.TrimRight(digits, "0")
		k = len(digits)
		switch {
		case n >= k:
			return sign + digits + strings.Repeat("0", n-k)
		case n > 0:
			return sign + digits[:n] + "." + digits[n:]
		}
		return sign + "0." + strings.Repeat("0", -n) + digits
	}
}

// FuzzNumberRadix checks Number.prototype.toString(radix) for radix != 10
// against fuzzToStringRadix over every float64 bit pattern.
func FuzzNumberRadix(f *testing.F) {
	for _, x := range []float64{0, 1, -1, 0.5, 0.1, 1 / 3.0, 123.456, 255, 1e21, 1 << 53, 1<<53 + 2, 1 << 60,
		9007199254740993, 5e-324, 2.2250738585072014e-308, math.MaxFloat64, 1e300, -1e-300, math.Inf(1), math.NaN()} {
		for _, radix := range []uint8{2, 3, 7, 8, 16, 32, 35, 36} {
			f.Add(math.Float64bits(x), radix)
		}
	}
	r := NewRealm()
	f.Fuzz(func(t *testing.T, bits uint64, radix uint8) {
		x := math.Float64frombits(bits)
		rad := 2 + int(radix)%35
		if rad == 10 {
			rad = 36
		}
		want := fuzzToStringRadix(x, rad)
		if got := NumberToStringRadix(x, rad); got != want {
			t.Fatalf("NumberToStringRadix(%v, %d) = %q, want %q", x, rad, got, want)
		}
		got, ok := fuzzCallNumber(t, r, numberProtoToString, x, []Value{NumberValue(float64(rad))})
		if !ok || got != want {
			t.Fatalf("(%v).toString(%d) = %q (ok=%v), want %q", x, rad, got, ok, want)
		}
	})
}

// fuzzStrDecimal is StrDecimalLiteral; Longest makes the prefix match of
// parseFloat the longest one.
var fuzzStrDecimal = func() *regexp.Regexp {
	re := regexp.MustCompile(`^[+-]?(?:Infinity|(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?)`)
	re.Longest()
	return re
}()

// fuzzIsStrWhiteSpace is StrWhiteSpaceChar: WhiteSpace (TAB, VT, FF,
// ZWNBSP, any Zs) or LineTerminator.
func fuzzIsStrWhiteSpace(c rune) bool {
	switch c {
	case '\t', '\v', '\f', '\ufeff', '\n', '\r', '\u2028', '\u2029':
		return true
	}
	return unicode.Is(unicode.Zs, c)
}

// fuzzDigitVal is the value of an ASCII alphanumeric digit, or 99.
func fuzzDigitVal(c rune) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'z':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'Z':
		return int(c-'A') + 10
	}
	return 99
}

// fuzzIntValue is the Number value of the integer spelled by digits (all valid in radix):
// exact, then rounded to nearest, ties to even.
func fuzzIntValue(digits string, radix int) float64 {
	b, _ := new(big.Int).SetString(digits, radix)
	f, _ := new(big.Float).SetInt(b).Float64()
	return f
}

// fuzzDecimalValue is the rounded value of a StrDecimalLiteral.
func fuzzDecimalValue(lit string) float64 {
	f, _ := strconv.ParseFloat(lit, 64) // ErrRange yields ±Inf or ±0, as the spec rounds
	return f
}

// fuzzStringToNumber is StringToNumber (ES2023 7.1.4.1.1).
func fuzzStringToNumber(s string) float64 {
	t := strings.TrimFunc(s, fuzzIsStrWhiteSpace)
	if t == "" {
		return 0
	}
	if len(t) > 2 && t[0] == '0' && strings.ContainsRune("xXoObB", rune(t[1])) {
		radix := map[byte]int{'x': 16, 'o': 8, 'b': 2}[t[1]|0x20]
		for _, c := range t[2:] {
			if fuzzDigitVal(c) >= radix {
				return math.NaN()
			}
		}
		return fuzzIntValue(t[2:], radix)
	}
	if m := fuzzStrDecimal.FindString(t); m == t {
		return fuzzDecimalValue(t)
	}
	return math.NaN()
}

// fuzzParseInt is parseInt (ES2023 19.2.5) after the argument coercions.
func fuzzParseInt(s string, radix int) float64 {
	rs := []rune(strings.TrimLeftFunc(s, fuzzIsStrWhiteSpace))
	sign := 1.0
	if len(rs) > 0 && rs[0] == '-' {
		sign = -1
	}
	if len(rs) > 0 && (rs[0] == '-' || rs[0] == '+') {
		rs = rs[1:]
	}
	strip := true
	if radix != 0 {
		if radix < 2 || radix > 36 {
			return math.NaN()
		}
		strip = radix == 16
	} else {
		radix = 10
	}
	if strip && len(rs) >= 2 && rs[0] == '0' && (rs[1] == 'x' || rs[1] == 'X') {
		rs, radix = rs[2:], 16
	}
	end := 0
	for end < len(rs) && fuzzDigitVal(rs[end]) < radix {
		end++
	}
	if end == 0 {
		return math.NaN()
	}
	return sign * fuzzIntValue(string(rs[:end]), radix)
}

// fuzzParseFloat is parseFloat (ES2023 19.2.4).
func fuzzParseFloat(s string) float64 {
	m := fuzzStrDecimal.FindString(strings.TrimLeftFunc(s, fuzzIsStrWhiteSpace))
	if m == "" {
		return math.NaN()
	}
	return fuzzDecimalValue(m)
}

func fuzzSameNumber(a, b float64) bool {
	return math.Float64bits(a) == math.Float64bits(b) || math.IsNaN(a) && math.IsNaN(b)
}

// FuzzStringNumber checks ToNumber(string), parseInt and parseFloat against
// the references above. moejs converts every radix exactly, so parseInt is
// held to the exact value even where the spec allows an approximation
// (radixes other than 2, 4, 8, 10, 16, 32, and digits after the 20th in
// radix 10).
func FuzzStringNumber(f *testing.F) {
	for _, s := range []string{"", " ", "0", "-0", "+0", "  12  ", "1e3", "1E-3", ".5", "5.", ".", "+.5e+2", "-.e1",
		"Infinity", "-Infinity", "+Infinity", "infinity", "Infinityx", "0x1F", "0X", "0x", "-0x10", "0o17", "0b102",
		"0b", "1_000", "1e", "1e+", "12abc", "\u00a0\ufeff42\u2028", "\u180e1", "1e400", "-1e-400", "0.0000001",
		"123456789012345678901234567890", "9007199254740993", "0x20000000000001", "٣", "１", "\xff1", "0.1e-999999999999"} {
		for _, radix := range []int{0, 2, 8, 10, 16, 36, 1, 37, -1} {
			f.Add(s, radix)
		}
	}
	f.Fuzz(func(t *testing.T, s string, radix int) {
		js := FromGoString(s)
		if got, want := StringToNumber(js), fuzzStringToNumber(s); !fuzzSameNumber(got, want) {
			t.Fatalf("ToNumber(%q) = %v, want %v", s, got, want)
		}
		radix %= 40
		if got, want := parseIntString(js, radix), fuzzParseInt(s, radix); !fuzzSameNumber(got, want) {
			t.Fatalf("parseInt(%q, %d) = %v, want %v", s, radix, got, want)
		}
		if got, want := parseFloatString(js), fuzzParseFloat(s); !fuzzSameNumber(got, want) {
			t.Fatalf("parseFloat(%q) = %v, want %v", s, got, want)
		}
	})
}
