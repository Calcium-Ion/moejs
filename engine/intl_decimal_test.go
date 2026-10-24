package engine

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// intlDecimalString writes d canonically: NaN, [-]Infinity, [-]0 or
// [-]0.<digits>e<exp>.
func intlDecimalString(d *intlDecimal) string {
	sign := ""
	if d.neg {
		sign = "-"
	}
	switch {
	case d.kind == intlNaN:
		return "NaN"
	case d.kind == intlInfinity:
		return sign + "Infinity"
	case len(d.digits) == 0:
		return sign + "0"
	}
	return fmt.Sprintf("%s0.%se%d", sign, d.digits, d.exp)
}

// intlDecimalRat returns the absolute value of a finite d.
func intlDecimalRat(d *intlDecimal) *big.Rat {
	var n big.Int
	if len(d.digits) > 0 {
		n.SetString(string(d.digits), 10)
	}
	return new(big.Rat).Mul(new(big.Rat).SetInt(&n), intlPow10(d.exp-len(d.digits)))
}

func intlPow10(n int) *big.Rat {
	p := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(max(n, -n))), nil)
	if n < 0 {
		return new(big.Rat).SetFrac(big.NewInt(1), p)
	}
	return new(big.Rat).SetInt(p)
}

// checkIntlDecimal reports a d that breaks the representation's invariants.
func checkIntlDecimal(t testing.TB, d *intlDecimal, what string) {
	t.Helper()
	if d.kind != intlFinite {
		if len(d.digits) != 0 {
			t.Fatalf("%s: %s has digits %q", what, intlDecimalString(d), d.digits)
		}
		return
	}
	if len(d.digits) == 0 {
		if d.exp != 0 {
			t.Fatalf("%s: zero with exponent %d", what, d.exp)
		}
		return
	}
	for _, c := range d.digits {
		if c < '0' || c > '9' {
			t.Fatalf("%s: digits %q", what, d.digits)
		}
	}
	if d.digits[0] == '0' || d.digits[len(d.digits)-1] == '0' {
		t.Fatalf("%s: digits %q have a leading or trailing zero", what, d.digits)
	}
}

func TestIntlDecimalSetString(t *testing.T) {
	hi, lo := intlMVBounds()
	hi1 := hi[:len(hi)-1] + string(hi[len(hi)-1]-1) // hi ends in 2, 4, 6 or 8
	for _, c := range []struct{ in, want string }{
		{"", "0"},
		{"   ", "0"},
		{"0", "0"},
		{"-0", "-0"},
		{"+0", "0"},
		{"-.0", "-0"},
		{"-0.000e5", "-0"},
		{"00012.3400", "0.1234e2"},
		{"0.1000000000000000000001", "0.1000000000000000000001e0"},
		{"123456789012345678901234567890", "0.12345678901234567890123456789e30"},
		{"-987654321987654321", "-0.987654321987654321e18"},
		{"1.0000000000000001", "0.10000000000000001e1"},
		{"1e400", "Infinity"},
		{"-1e400", "-Infinity"},
		{"1e308", "0.1e309"},
		{"1e-323", "0.1e-322"},
		{"1e-400", "0"},
		{"-1e-400", "-0"},
		{"1e1000000000", "Infinity"},
		{"1e-1000000000", "0"},
		{"1e99999999999999999999999999", "Infinity"},
		{"0.0000001e99999999999999999999999999", "Infinity"},
		{"1e-99999999999999999999999999", "0"},
		{hi, "Infinity"},
		{hi + ".0000001", "Infinity"},
		{"0." + hi + "e309", "Infinity"},
		{hi1, "0." + hi1 + "e309"},
		{hi1 + ".9999", "0." + hi1 + "9999e309"},
		{"-" + hi1, "-0." + hi1 + "e309"},
		{"0." + strings.Repeat("0", 323) + lo, "0"},
		{"0." + strings.Repeat("0", 323) + lo + "1", "0." + lo + "1e-323"},
		{"-0." + strings.Repeat("0", 323) + lo, "-0"},
		{"1.7976931348623159e308", "Infinity"},
		{"1.797693134862315807937289714053e308", "0.1797693134862315807937289714053e309"},
		{"2.4703282292062327e-324", "0"},
		{"2.4703282292062328e-324", "0.24703282292062328e-323"},
		{" \t\n\v\f\r\u00A0\u1680\u2000 \u2028\u2029\u202F\u205F\u3000\uFEFF12 \uFEFF", "0.12e2"},
		{"\uFEFF-12.5e-1\u3000", "-0.125e1"},
		{"\u200B12", "NaN"},
		{"1\u00A02", "NaN"},
		{"0x1F", "0.31e2"},
		{"0X1f", "0.31e2"},
		{"0o17", "0.15e2"},
		{"0O17", "0.15e2"},
		{"0b101", "0.5e1"},
		{"0B101", "0.5e1"},
		{"0x0", "0"},
		{"0x000", "0"},
		{"0xFFFFFFFFFFFFFFFF", "0.18446744073709551615e20"},
		{"0x10000000000000000", "0.18446744073709551616e20"},
		{"0x" + strings.Repeat("f", 256), "Infinity"},
		{"0x" + strings.Repeat("0", 300) + "1", "0.1e1"},
		{"0xfffffffffffffc" + strings.Repeat("0", 242), "Infinity"},
		{"0xfffffffffffffb" + strings.Repeat("f", 242), "0." + hi1 + "e309"},
		{"0x", "NaN"},
		{"0x1g", "NaN"},
		{"0b102", "NaN"},
		{"0o8", "NaN"},
		{"-0x1", "NaN"},
		{"+0x1", "NaN"},
		{"1_000", "NaN"},
		{"1,000", "NaN"},
		{"+.5e1", "0.5e1"},
		{"5.", "0.5e1"},
		{".5", "0.5e0"},
		{".", "NaN"},
		{"+", "NaN"},
		{"-", "NaN"},
		{"e5", "NaN"},
		{".e5", "NaN"},
		{"1e", "NaN"},
		{"1e+", "NaN"},
		{"1E-2", "0.1e-1"},
		{"1e+2", "0.1e3"},
		{"--1", "NaN"},
		{"+-1", "NaN"},
		{"1 2", "NaN"},
		{"Infinity", "Infinity"},
		{"+Infinity", "Infinity"},
		{"-Infinity", "-Infinity"},
		{" -Infinity ", "-Infinity"},
		{"infinity", "NaN"},
		{"INFINITY", "NaN"},
		{"Inf", "NaN"},
		{"NaN", "NaN"},
		{"- 1", "NaN"},
		{"1n", "NaN"},
		{"\uFF10", "NaN"},
	} {
		var d intlDecimal
		d.setString(FromGoString(c.in))
		checkIntlDecimal(t, &d, c.in)
		if got := intlDecimalString(&d); got != c.want {
			t.Errorf("setString(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

// TestIntlDecimalSetStringRandom checks setString on random literals
// against their exact value and strconv's Number.
func TestIntlDecimalSetStringRandom(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	digits := func(n int) string {
		b := make([]byte, n)
		for i := range b {
			b[i] = "0123456789000999"[r.IntN(16)]
		}
		return string(b)
	}
	for range 100000 {
		var lit strings.Builder
		neg := false
		switch r.IntN(3) {
		case 1:
			lit.WriteByte('+')
		case 2:
			lit.WriteByte('-')
			neg = true
		}
		ip, fp := digits(r.IntN(25)), ""
		lit.WriteString(ip)
		if r.IntN(2) == 0 || ip == "" {
			fp = digits(r.IntN(25))
			if ip == "" && fp == "" {
				fp = digits(1)
			}
			lit.WriteByte('.')
			lit.WriteString(fp)
		}
		e := 0
		if r.IntN(2) == 0 {
			e = r.IntN(600) - 300
			lit.WriteByte("eE"[r.IntN(2)])
			if e >= 0 && r.IntN(2) == 0 {
				lit.WriteByte('+')
			}
			lit.WriteString(strconv.Itoa(e))
		}
		s := lit.String()
		var d intlDecimal
		d.setString(asciiString(s))
		checkIntlDecimal(t, &d, s)
		// The exact value: the digits as an integer times 10^(e - len(fp)).
		var n big.Int
		n.SetString("0"+ip+fp, 10)
		want := new(big.Rat).Mul(new(big.Rat).SetInt(&n), intlPow10(e-len(fp)))
		f, _ := strconv.ParseFloat(s, 64)
		switch {
		case math.IsInf(f, 0):
			if d.kind != intlInfinity || d.neg != neg {
				t.Fatalf("setString(%q) = %s, want an infinity", s, intlDecimalString(&d))
			}
		case f == 0:
			if !d.isZero() || d.neg != neg {
				t.Fatalf("setString(%q) = %s, want a zero", s, intlDecimalString(&d))
			}
		default:
			if d.kind != intlFinite || d.neg != neg || intlDecimalRat(&d).Cmp(want) != 0 {
				t.Fatalf("setString(%q) = %s, want %s", s, intlDecimalString(&d), want.FloatString(30))
			}
			if g, _ := strconv.ParseFloat(intlDecimalString(&d), 64); g != f {
				t.Fatalf("setString(%q) = %s, a Number of %v, want %v", s, intlDecimalString(&d), g, f)
			}
		}
	}
}

// TestIntlDecimalSetRadix checks 0x, 0o and 0b literals against math/big.
func TestIntlDecimalSetRadix(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	limit := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 1024), new(big.Int).Lsh(big.NewInt(1), 970))
	for range 20000 {
		base, prefix := [...]int{2, 8, 16}[r.IntN(3)], ""
		switch base {
		case 2:
			prefix = "0b"
		case 8:
			prefix = "0o"
		default:
			prefix = "0x"
		}
		n := 1 + r.IntN([...]int{40, 400}[r.IntN(2)])
		b := make([]byte, n)
		for i := range b {
			b[i] = "0123456789abcdefABCDEF"[r.IntN(min(base+6*(base/16), 22))]
		}
		s := prefix + string(b)
		var d intlDecimal
		d.setString(asciiString(s))
		checkIntlDecimal(t, &d, s)
		v, _ := new(big.Int).SetString(string(b), base)
		switch {
		case v.Cmp(limit) >= 0:
			if d.kind != intlInfinity || d.neg {
				t.Fatalf("setString(%q) = %s, want Infinity", s, intlDecimalString(&d))
			}
		case d.kind != intlFinite || d.neg || intlDecimalRat(&d).Cmp(new(big.Rat).SetInt(v)) != 0:
			t.Fatalf("setString(%q) = %s, want %s", s, intlDecimalString(&d), v)
		}
	}
}

// TestIntlDecimalSetFloat checks setFloat's digits against Number::toString.
func TestIntlDecimalSetFloat(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	check := func(f float64) {
		var d, want intlDecimal
		d.setFloat(f)
		checkIntlDecimal(t, &d, "setFloat")
		want.setLiteral(NumberToGoString(f))
		if f == 0 && math.Signbit(f) {
			want.neg = true // Number::toString(-0) is "0"
		}
		if got, w := intlDecimalString(&d), intlDecimalString(&want); got != w {
			t.Fatalf("setFloat(%v) = %s, want %s", f, got, w)
		}
	}
	for _, f := range []float64{0, math.Copysign(0, -1), math.NaN(), math.Inf(1), math.Inf(-1),
		1, -1, 10, 1250, -1000, 0.1, 9.995, 999.5, 1e21, 1e-7, 123456789012345680000, math.MaxFloat64,
		-math.MaxFloat64, math.SmallestNonzeroFloat64, 2.2250738585072014e-308, 5e-324, 1<<53 - 1, 1 << 53, 1<<53 + 2,
		-(1<<53 - 1), 1e15, 1e16, 9007199254740990} {
		check(f)
	}
	for range 200000 {
		var f float64
		switch r.IntN(4) {
		case 0:
			f = math.Float64frombits(r.Uint64())
		case 3:
			f = float64(r.Int64N(1<<53)>>r.IntN(53)) * float64(1-2*r.IntN(2))
		case 1:
			f = float64(r.Int64N(2000000)-1000000) / math.Pow10(r.IntN(12))
		default:
			f = r.NormFloat64() * math.Pow10(r.IntN(60)-30)
		}
		check(f)
	}
	var d intlDecimal
	if d.setFloat(math.Copysign(0, -1)); !d.neg || !d.isZero() {
		t.Errorf("setFloat(-0) = %s", intlDecimalString(&d))
	}
}

func TestIntlDecimalSetBigInt(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 8))
	for _, s := range []string{"0", "1", "-1", "10", "-1000", "18446744073709551615", "18446744073709551616",
		"-9223372036854775808", "1" + strings.Repeat("0", 400), "-123456789" + strings.Repeat("0", 50)} {
		b, _ := NewBigIntFromDecimal(s)
		var d intlDecimal
		d.setBigInt(b)
		checkIntlDecimal(t, &d, s)
		v, _ := new(big.Int).SetString(s, 10)
		if d.neg != (v.Sign() < 0) || intlDecimalRat(&d).Cmp(new(big.Rat).SetInt(new(big.Int).Abs(v))) != 0 {
			t.Errorf("setBigInt(%s) = %s", s, intlDecimalString(&d))
		}
	}
	for range 5000 {
		buf := make([]byte, 1+r.IntN(64))
		for i := range buf {
			buf[i] = byte(r.Uint32())
		}
		v := new(big.Int).SetBytes(buf)
		if r.IntN(2) == 0 {
			v.Neg(v)
		}
		b, _ := NewBigIntFromBig(v)
		var d intlDecimal
		d.setBigInt(b)
		checkIntlDecimal(t, &d, v.String())
		if d.neg != (v.Sign() < 0) || intlDecimalRat(&d).Cmp(new(big.Rat).SetInt(new(big.Int).Abs(v))) != 0 {
			t.Fatalf("setBigInt(%s) = %s", v, intlDecimalString(&d))
		}
	}
}

func TestIntlDecimalCmp(t *testing.T) {
	r := rand.New(rand.NewPCG(9, 10))
	values := []string{"-Infinity", "-1e300", "-12.5", "-12.4999", "-1", "-0.001", "-0", "0", "1e-300", "0.5",
		"0.50001", "1", "1.0000000000000000000001", "9.99", "10", "123456789012345678901234567890", "Infinity"}
	for range 3000 {
		values = append(values, strconv.FormatFloat(r.NormFloat64()*math.Pow10(r.IntN(10)-5), 'g', 1+r.IntN(5), 64))
	}
	rat := func(d *intlDecimal) (*big.Rat, int) { // value and infinity sign
		if d.kind == intlInfinity {
			return new(big.Rat), d.sign()
		}
		x := intlDecimalRat(d)
		if d.neg {
			x.Neg(x)
		}
		return x, 0
	}
	for i := range 20000 {
		a, b := values[i%len(values)], values[r.IntN(len(values))]
		if i < len(values)*len(values) && i < 289 {
			a, b = values[i/17], values[i%17]
		}
		var x, y intlDecimal
		x.setString(asciiString(a))
		y.setString(asciiString(b))
		xr, xi := rat(&x)
		yr, yi := rat(&y)
		want := xr.Cmp(yr)
		if xi != yi {
			want = -1
			if xi > yi {
				want = 1
			}
		} else if xi != 0 {
			want = 0
		}
		if got := x.cmp(&y); got != want {
			t.Fatalf("cmp(%s, %s) = %d, want %d", a, b, got, want)
		}
	}
}

// intlTestOptions applies SetNumberFormatDigitOptions (ECMA-402 §16.1.2)
// with the decimal style's defaults (0 and 3 fraction digits, standard
// notation) to options written "name:value ...".
func intlTestOptions(t testing.TB, spec string) intlDigitOptions {
	opts := map[string]string{}
	for _, f := range strings.Fields(spec) {
		k, v, _ := strings.Cut(f, ":")
		opts[k] = v
	}
	num := func(k string) int {
		v, ok := opts[k]
		if !ok {
			return -1
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("option %s:%s", k, v)
		}
		return n
	}
	o := intlDigitOptions{minInt: max(num("minimumIntegerDigits"), 1), increment: max(num("roundingIncrement"), 1),
		mode: intlRoundHalfExpand, stripIfInteger: opts["trailingZeroDisplay"] == "stripIfInteger"}
	if m, ok := opts["roundingMode"]; ok {
		modes := []string{"ceil", "floor", "expand", "trunc", "halfCeil", "halfFloor", "halfExpand", "halfTrunc", "halfEven"}
		i := 0
		for i < len(modes) && modes[i] != m {
			i++
		}
		if i == len(modes) {
			t.Fatalf("roundingMode %s", m)
		}
		o.mode = intlRoundingMode(i)
	}
	mnfd, mxfd, mnsd, mxsd := num("minimumFractionDigits"), num("maximumFractionDigits"),
		num("minimumSignificantDigits"), num("maximumSignificantDigits")
	mnfdDefault, mxfdDefault := 0, 3
	if o.increment != 1 {
		mxfdDefault = mnfdDefault
	}
	priority := opts["roundingPriority"]
	hasSd, hasFd := mnsd >= 0 || mxsd >= 0, mnfd >= 0 || mxfd >= 0
	needSd, needFd := true, true
	if priority == "" || priority == "auto" {
		needSd = hasSd
		needFd = !needSd
	}
	if needSd {
		o.minSig, o.maxSig = 1, 21
		if hasSd {
			o.minSig = max(mnsd, 1)
			if o.maxSig = mxsd; mxsd < 0 {
				o.maxSig = 21
			}
		}
	}
	o.minFrac, o.maxFrac = mnfdDefault, mxfdDefault
	if needFd && hasFd {
		switch {
		case mnfd < 0:
			mnfd = min(mnfdDefault, mxfd)
		case mxfd < 0:
			mxfd = max(mxfdDefault, mnfd)
		}
		o.minFrac, o.maxFrac = mnfd, mxfd
	}
	switch {
	case priority == "morePrecision":
		o.roundingType = intlMorePrecision
	case priority == "lessPrecision":
		o.roundingType = intlLessPrecision
	case hasSd:
		o.roundingType = intlSignificantDigits
	default:
		o.roundingType = intlFractionDigits
	}
	return o
}

// intlFormatTest formats the String s as Intl.NumberFormat does with
// useGrouping false in en: a '-' for a negative sign, then the digits.
func intlFormatTest(o *intlDigitOptions, s string) string {
	var d intlDecimal
	d.setString(asciiString(s))
	if d.kind != intlFinite {
		return intlDecimalString(&d)
	}
	b := o.formatNumericToString(&d, nil)
	if d.neg {
		return "-" + string(b)
	}
	return string(b)
}

// intlTest262Format are the tables of test262's intl402/NumberFormat/
// prototype/format/format-{rounding-*,fraction-digits*,
// significant-digits*,max-min-fraction-significant-digits}.js (045bf6f),
// options without useGrouping:false, then input and output pairs; printed
// by testdata/intl_decimal_test262.js.
var intlTest262Format = []struct {
	options string
	cases   []string
}{
	// format-fraction-digits-precision.js
	{"minimumIntegerDigits:3 minimumFractionDigits:1 maximumFractionDigits:3", []string{"12344501000000000000000000000000000", "12344501000000000000000000000000000.0", "-12344501000000000000000000000000000", "-12344501000000000000000000000000000.0"}},
	// format-fraction-digits.js
	{"minimumIntegerDigits:3 minimumFractionDigits:1 maximumFractionDigits:3", []string{"0", "000.0", "123", "123.0", "12345", "12345.0", "-0", "-000.0", "-123", "-123.0", "-12345", "-12345.0", "123.45", "123.45", "-123.45", "-123.45", "123.444499", "123.444", "-123.444499", "-123.444", "123.444500", "123.445", "-123.444500", "-123.445", "123.44501", "123.445", "-123.44501", "-123.445", "0.001234", "000.001", "-0.001234", "-000.001", "0.00000000123", "000.0", "-0.00000000123", "-000.0", "0.00000000000000000000000000000123", "000.0", "-0.00000000000000000000000000000123", "-000.0", "1.2", "001.2", "-1.2", "-001.2", "0.0000000012344501", "000.0", "-0.0000000012344501", "-000.0", "123445.01", "123445.01", "-123445.01", "-123445.01"}},
	// format-max-min-fraction-significant-digits.js
	{"minimumFractionDigits:1 maximumFractionDigits:4 minimumSignificantDigits:1 maximumSignificantDigits:2", []string{"1.23456", "1.2"}},
	{"minimumFractionDigits:2 maximumFractionDigits:4 minimumSignificantDigits:1 maximumSignificantDigits:2", []string{"1.23456", "1.2"}},
	{"minimumFractionDigits:2 maximumFractionDigits:4 minimumSignificantDigits:2 maximumSignificantDigits:3", []string{"1.23456", "1.23"}},
	{"minimumFractionDigits:1 maximumFractionDigits:2 minimumSignificantDigits:1 maximumSignificantDigits:4", []string{"1.23456", "1.235"}},
	{"minimumFractionDigits:1 maximumFractionDigits:2 minimumSignificantDigits:2 maximumSignificantDigits:4", []string{"1.23456", "1.235"}},
	// format-rounding-increment-1.js
	{"roundingIncrement:1 maximumFractionDigits:1", []string{"1.100", "1.1", "1.125", "1.1", "1.150", "1.2", "1.175", "1.2", "1.200", "1.2"}},
	{"roundingIncrement:1 maximumFractionDigits:2", []string{"1.0100", "1.01", "1.0125", "1.01", "1.0150", "1.02", "1.0175", "1.02", "1.0200", "1.02"}},
	// format-rounding-increment-10.js
	{"roundingIncrement:10 maximumFractionDigits:2 minimumFractionDigits:2", []string{"1.100", "1.10", "1.125", "1.10", "1.150", "1.20", "1.175", "1.20", "1.200", "1.20"}},
	{"roundingIncrement:10 maximumFractionDigits:3 minimumFractionDigits:3", []string{"1.0100", "1.010", "1.0125", "1.010", "1.0150", "1.020", "1.0175", "1.020", "1.0200", "1.020"}},
	// format-rounding-increment-100.js
	{"roundingIncrement:100 maximumFractionDigits:3 minimumFractionDigits:3", []string{"1.100", "1.100", "1.125", "1.100", "1.150", "1.200", "1.175", "1.200", "1.200", "1.200"}},
	{"roundingIncrement:100 maximumFractionDigits:4 minimumFractionDigits:4", []string{"1.0100", "1.0100", "1.0125", "1.0100", "1.0150", "1.0200", "1.0175", "1.0200", "1.0200", "1.0200"}},
	// format-rounding-increment-1000.js
	{"roundingIncrement:1000 maximumFractionDigits:4 minimumFractionDigits:4", []string{"1.100", "1.1000", "1.125", "1.1000", "1.150", "1.2000", "1.175", "1.2000", "1.200", "1.2000"}},
	{"roundingIncrement:1000 maximumFractionDigits:5 minimumFractionDigits:5", []string{"1.0100", "1.01000", "1.0125", "1.01000", "1.0150", "1.02000", "1.0175", "1.02000", "1.0200", "1.02000"}},
	// format-rounding-increment-2.js
	{"roundingIncrement:2 maximumFractionDigits:1 minimumFractionDigits:1", []string{"1.20", "1.2", "1.25", "1.2", "1.30", "1.4", "1.35", "1.4", "1.40", "1.4"}},
	{"roundingIncrement:2 maximumFractionDigits:2 minimumFractionDigits:2", []string{"1.020", "1.02", "1.025", "1.02", "1.030", "1.04", "1.035", "1.04", "1.040", "1.04"}},
	// format-rounding-increment-20.js
	{"roundingIncrement:20 maximumFractionDigits:2 minimumFractionDigits:2", []string{"1.20", "1.20", "1.25", "1.20", "1.30", "1.40", "1.35", "1.40", "1.40", "1.40"}},
	{"roundingIncrement:20 maximumFractionDigits:3 minimumFractionDigits:3", []string{"1.020", "1.020", "1.025", "1.020", "1.030", "1.040", "1.035", "1.040", "1.040", "1.040"}},
	// format-rounding-increment-200.js
	{"roundingIncrement:200 maximumFractionDigits:3 minimumFractionDigits:3", []string{"1.20", "1.200", "1.25", "1.200", "1.30", "1.400", "1.35", "1.400", "1.40", "1.400"}},
	{"roundingIncrement:200 maximumFractionDigits:4 minimumFractionDigits:4", []string{"1.020", "1.0200", "1.025", "1.0200", "1.030", "1.0400", "1.035", "1.0400", "1.040", "1.0400"}},
	// format-rounding-increment-2000.js
	{"roundingIncrement:2000 maximumFractionDigits:4 minimumFractionDigits:4", []string{"1.20", "1.2000", "1.25", "1.2000", "1.30", "1.4000", "1.35", "1.4000", "1.40", "1.4000"}},
	{"roundingIncrement:2000 maximumFractionDigits:5 minimumFractionDigits:5", []string{"1.020", "1.02000", "1.025", "1.02000", "1.030", "1.04000", "1.035", "1.04000", "1.040", "1.04000"}},
	// format-rounding-increment-25.js
	{"roundingIncrement:25 maximumFractionDigits:2 minimumFractionDigits:2", []string{"1.2500", "1.25", "1.3125", "1.25", "1.3750", "1.50", "1.4375", "1.50", "1.5000", "1.50"}},
	{"roundingIncrement:25 maximumFractionDigits:3 minimumFractionDigits:3", []string{"1.02500", "1.025", "1.03125", "1.025", "1.03750", "1.050", "1.04375", "1.050", "1.05000", "1.050"}},
	// format-rounding-increment-250.js
	{"roundingIncrement:250 maximumFractionDigits:3 minimumFractionDigits:3", []string{"1.2500", "1.250", "1.3125", "1.250", "1.3750", "1.500", "1.4375", "1.500", "1.5000", "1.500"}},
	{"roundingIncrement:250 maximumFractionDigits:4 minimumFractionDigits:4", []string{"1.02500", "1.0250", "1.03125", "1.0250", "1.03750", "1.0500", "1.04375", "1.0500", "1.05000", "1.0500"}},
	// format-rounding-increment-2500.js
	{"roundingIncrement:2500 maximumFractionDigits:4 minimumFractionDigits:4", []string{"1.2500", "1.2500", "1.3125", "1.2500", "1.3750", "1.5000", "1.4375", "1.5000", "1.5000", "1.5000"}},
	{"roundingIncrement:2500 maximumFractionDigits:5 minimumFractionDigits:5", []string{"1.02500", "1.02500", "1.03125", "1.02500", "1.03750", "1.05000", "1.04375", "1.05000", "1.05000", "1.05000"}},
	// format-rounding-increment-5.js
	{"roundingIncrement:5 maximumFractionDigits:1 minimumFractionDigits:1", []string{"1.500", "1.5", "1.625", "1.5", "1.750", "2.0", "1.875", "2.0", "2.000", "2.0"}},
	{"roundingIncrement:5 maximumFractionDigits:2 minimumFractionDigits:2", []string{"1.0500", "1.05", "1.0625", "1.05", "1.0750", "1.10", "1.0875", "1.10", "1.1000", "1.10"}},
	// format-rounding-increment-50.js
	{"roundingIncrement:50 maximumFractionDigits:2 minimumFractionDigits:2", []string{"1.500", "1.50", "1.625", "1.50", "1.750", "2.00", "1.875", "2.00", "2.000", "2.00"}},
	{"roundingIncrement:50 maximumFractionDigits:3 minimumFractionDigits:3", []string{"1.0500", "1.050", "1.0625", "1.050", "1.0750", "1.100", "1.0875", "1.100", "1.1000", "1.100"}},
	// format-rounding-increment-500.js
	{"roundingIncrement:500 maximumFractionDigits:3 minimumFractionDigits:3", []string{"1.500", "1.500", "1.625", "1.500", "1.750", "2.000", "1.875", "2.000", "2.000", "2.000"}},
	{"roundingIncrement:500 maximumFractionDigits:4 minimumFractionDigits:4", []string{"1.0500", "1.0500", "1.0625", "1.0500", "1.0750", "1.1000", "1.0875", "1.1000", "1.1000", "1.1000"}},
	// format-rounding-increment-5000.js
	{"roundingIncrement:5000 maximumFractionDigits:4 minimumFractionDigits:4", []string{"1.500", "1.5000", "1.625", "1.5000", "1.750", "2.0000", "1.875", "2.0000", "2.000", "2.0000"}},
	{"roundingIncrement:5000 maximumFractionDigits:5 minimumFractionDigits:5", []string{"1.0500", "1.05000", "1.0625", "1.05000", "1.0750", "1.10000", "1.0875", "1.10000", "1.1000", "1.10000"}},
	// format-rounding-mode-ceil.js
	{"roundingMode:ceil maximumSignificantDigits:2", []string{"0", "0", "1.101", "1.2", "1.15", "1.2", "1.1999", "1.2", "1.25", "1.3", "-0", "-0", "-1.101", "-1.1", "-1.15", "-1.1", "-1.1999", "-1.1", "-1.25", "-1.2"}},
	// format-rounding-mode-expand.js
	{"roundingMode:expand maximumSignificantDigits:2", []string{"0", "0", "1.101", "1.2", "1.15", "1.2", "1.1999", "1.2", "1.25", "1.3", "-0", "-0", "-1.101", "-1.2", "-1.15", "-1.2", "-1.1999", "-1.2", "-1.25", "-1.3"}},
	// format-rounding-mode-floor.js
	{"roundingMode:floor maximumSignificantDigits:2", []string{"0", "0", "1.101", "1.1", "1.15", "1.1", "1.1999", "1.1", "1.25", "1.2", "-0", "-0", "-1.101", "-1.2", "-1.15", "-1.2", "-1.1999", "-1.2", "-1.25", "-1.3"}},
	// format-rounding-mode-half-ceil.js
	{"roundingMode:halfCeil maximumSignificantDigits:2", []string{"0", "0", "1.101", "1.1", "1.15", "1.2", "1.1999", "1.2", "1.25", "1.3", "-0", "-0", "-1.101", "-1.1", "-1.15", "-1.1", "-1.1999", "-1.2", "-1.25", "-1.2"}},
	// format-rounding-mode-half-even.js
	{"roundingMode:halfEven maximumSignificantDigits:2", []string{"0", "0", "1.101", "1.1", "1.15", "1.2", "1.1999", "1.2", "1.25", "1.2", "-0", "-0", "-1.101", "-1.1", "-1.15", "-1.2", "-1.1999", "-1.2", "-1.25", "-1.2"}},
	// format-rounding-mode-half-expand.js
	{"roundingMode:halfExpand maximumSignificantDigits:2", []string{"0", "0", "1.101", "1.1", "1.15", "1.2", "1.1999", "1.2", "1.25", "1.3", "-0", "-0", "-1.101", "-1.1", "-1.15", "-1.2", "-1.1999", "-1.2", "-1.25", "-1.3"}},
	// format-rounding-mode-half-floor.js
	{"roundingMode:halfFloor maximumSignificantDigits:2", []string{"0", "0", "1.101", "1.1", "1.15", "1.1", "1.1999", "1.2", "1.25", "1.2", "-0", "-0", "-1.101", "-1.1", "-1.15", "-1.2", "-1.1999", "-1.2", "-1.25", "-1.3"}},
	// format-rounding-mode-half-trunc.js
	{"roundingMode:halfTrunc maximumSignificantDigits:2", []string{"0", "0", "1.101", "1.1", "1.15", "1.1", "1.1999", "1.2", "1.25", "1.2", "-0", "-0", "-1.101", "-1.1", "-1.15", "-1.1", "-1.1999", "-1.2", "-1.25", "-1.2"}},
	// format-rounding-mode-trunc.js
	{"roundingMode:trunc maximumSignificantDigits:2", []string{"0", "0", "1.101", "1.1", "1.15", "1.1", "1.1999", "1.1", "1.25", "1.2", "-0", "-0", "-1.101", "-1.1", "-1.15", "-1.1", "-1.1999", "-1.1", "-1.25", "-1.2"}},
	// format-rounding-priority-auto.js
	{"roundingPriority:auto minimumSignificantDigits:2 minimumFractionDigitsDigits:2", []string{"1", "1.0"}},
	{"roundingPriority:auto minimumSignificantDigits:3 minimumFractionDigitsDigits:2", []string{"1", "1.00"}},
	{"roundingPriority:auto maximumSignificantDigits:2 maximumFractionDigitsDigits:2", []string{"1.23", "1.2"}},
	{"roundingPriority:auto maximumSignificantDigits:3 maximumFractionDigitsDigits:1", []string{"1.234", "1.23"}},
	// format-rounding-priority-less-precision.js
	{"roundingPriority:lessPrecision minimumSignificantDigits:2 minimumFractionDigits:2", []string{"1", "1.00"}},
	{"roundingPriority:lessPrecision minimumSignificantDigits:3 minimumFractionDigits:1", []string{"1", "1.0"}},
	{"roundingPriority:lessPrecision maximumSignificantDigits:2 maximumFractionDigits:2", []string{"1.23", "1.2"}},
	{"roundingPriority:lessPrecision maximumSignificantDigits:3 maximumFractionDigits:1", []string{"1.234", "1.2"}},
	// format-rounding-priority-more-precision.js
	{"roundingPriority:morePrecision minimumSignificantDigits:2 minimumFractionDigits:2", []string{"1", "1.0"}},
	{"roundingPriority:morePrecision minimumSignificantDigits:3 minimumFractionDigits:2", []string{"1", "1.00"}},
	{"roundingPriority:morePrecision maximumSignificantDigits:2 maximumFractionDigits:2", []string{"1.23", "1.23"}},
	{"roundingPriority:morePrecision maximumSignificantDigits:3 maximumFractionDigits:1", []string{"1.234", "1.23"}},
	// format-significant-digits-precision.js
	{"minimumSignificantDigits:3 maximumSignificantDigits:5", []string{"123.44500", "123.45", "-123.44500", "-123.45"}},
	// format-significant-digits.js
	{"minimumSignificantDigits:3 maximumSignificantDigits:5", []string{"0", "0.00", "123", "123", "12345", "12345", "-0", "-0.00", "-123", "-123", "-12345", "-12345", "123.45", "123.45", "-123.45", "-123.45", "123.44499", "123.44", "-123.44499", "-123.44", "123.44501", "123.45", "-123.44501", "-123.45", "0.001234", "0.001234", "-0.001234", "-0.001234", "0.00000000123", "0.00000000123", "-0.00000000123", "-0.00000000123", "0.00000000000000000000000000000123", "0.00000000000000000000000000000123", "-0.00000000000000000000000000000123", "-0.00000000000000000000000000000123", "1.2", "1.20", "-1.2", "-1.20", "0.0000000012344501", "0.0000000012345", "-0.0000000012344501", "-0.0000000012345", "123445.01", "123450", "-123445.01", "-123450", "12344501000000000000000000000000000", "12345000000000000000000000000000000", "-12344501000000000000000000000000000", "-12345000000000000000000000000000000"}},
}

func TestIntlFormatNumericToStringTest262(t *testing.T) {
	for _, c := range intlTest262Format {
		o := intlTestOptions(t, c.options)
		for i := 0; i < len(c.cases); i += 2 {
			if got := intlFormatTest(&o, c.cases[i]); got != c.cases[i+1] {
				t.Errorf("{%s}: %s formats as %s, want %s", c.options, c.cases[i], got, c.cases[i+1])
			}
		}
	}
}

func TestIntlFormatNumericToString(t *testing.T) {
	for _, c := range []struct{ options, in, want string }{
		{"minimumFractionDigits:2 maximumFractionDigits:2", "9.995", "10.00"},
		{"maximumSignificantDigits:3", "999.5", "1000"},
		{"maximumSignificantDigits:3", "99.95", "100"},
		{"maximumSignificantDigits:3", "0.0009995", "0.001"},
		{"minimumSignificantDigits:3 maximumSignificantDigits:3", "0.0009995", "0.00100"},
		{"maximumFractionDigits:0", "0.5", "1"},
		{"maximumFractionDigits:0", "-0.5", "-1"},
		{"maximumFractionDigits:0 roundingMode:halfEven", "0.5", "0"},
		{"maximumFractionDigits:0 roundingMode:halfEven", "1.5", "2"},
		{"maximumFractionDigits:0 roundingMode:halfEven", "2.5", "2"},
		{"maximumFractionDigits:0 roundingMode:halfEven", "-2.5", "-2"},
		{"maximumFractionDigits:2", "-0.0001", "-0"},
		{"maximumFractionDigits:2", "0.0049999999999", "0"},
		{"maximumFractionDigits:2", "0.005", "0.01"},
		{"maximumFractionDigits:2 roundingMode:ceil", "0.0000001", "0.01"},
		{"maximumFractionDigits:2 roundingMode:ceil", "-0.0000001", "-0"},
		{"maximumFractionDigits:2 roundingMode:floor", "-0.0000001", "-0.01"},
		{"maximumFractionDigits:2 roundingMode:expand", "1e-300", "0.01"},
		{"maximumFractionDigits:0 roundingMode:expand", "1e-300", "1"},
		{"maximumFractionDigits:3", "1e21", "1000000000000000000000"},
		{"maximumFractionDigits:3", "123456789012345678901234567890.12345", "123456789012345678901234567890.123"},
		{"maximumFractionDigits:3", "999999999999999999999999999999.9995", "1000000000000000000000000000000"},
		{"maximumFractionDigits:0", "1250", "1250"}, // the issue's ￥1,250 (JPY has no fraction digits)
		{"minimumIntegerDigits:3", "7", "007"},
		{"minimumIntegerDigits:3 minimumFractionDigits:2", "0.5", "000.50"},
		{"minimumIntegerDigits:21", "1234", "000000000000000001234"},
		{"minimumFractionDigits:2 trailingZeroDisplay:stripIfInteger", "1", "1"},
		{"minimumFractionDigits:2 trailingZeroDisplay:stripIfInteger", "1.004", "1.004"},
		{"minimumFractionDigits:2 maximumFractionDigits:2 trailingZeroDisplay:stripIfInteger", "1.004", "1"},
		{"minimumFractionDigits:2 maximumFractionDigits:2 trailingZeroDisplay:stripIfInteger", "1.005", "1.01"},
		{"minimumFractionDigits:2 trailingZeroDisplay:stripIfInteger", "1.1", "1.10"},
		{"roundingIncrement:2000 maximumFractionDigits:15 roundingMode:halfEven", "9.9e-11", "0.0000000001"},
		{"roundingIncrement:2000 maximumFractionDigits:15 roundingMode:halfEven", "9.7e-11", "0.000000000096"},
		{"roundingIncrement:2000 maximumFractionDigits:15 roundingMode:halfEven", "9.5e-11", "0.000000000096"},
		{"minimumSignificantDigits:4 trailingZeroDisplay:stripIfInteger", "12", "12"},
		{"minimumSignificantDigits:4", "12", "12.00"},
		{"minimumSignificantDigits:4", "0", "0.000"},
		{"minimumSignificantDigits:4", "-0", "-0.000"},
		{"maximumSignificantDigits:1", "0", "0"},
		{"maximumSignificantDigits:2", "123456", "120000"},
		{"maximumSignificantDigits:21", "0.1000000000000000000001", "0.1"},
		{"maximumSignificantDigits:21", "0.100000000000000000001", "0.100000000000000000001"},
		{"maximumFractionDigits:100", "1e-100", "0." + strings.Repeat("0", 99) + "1"},
		{"maximumFractionDigits:100", "1e-101", "0"},
		{"maximumFractionDigits:100", "5e-101", "0." + strings.Repeat("0", 99) + "1"},
		{"roundingIncrement:5000 maximumFractionDigits:0", "2499", "0"},
		{"roundingIncrement:5000 maximumFractionDigits:0", "2500", "5000"},
		{"roundingIncrement:5000 maximumFractionDigits:0", "7500", "10000"},
		{"roundingIncrement:5000 maximumFractionDigits:0 roundingMode:halfEven", "7500", "10000"},
		{"roundingIncrement:5000 maximumFractionDigits:0 roundingMode:halfEven", "12500", "10000"},
		{"roundingIncrement:5000 maximumFractionDigits:0 roundingMode:halfEven", "17500", "20000"},
		{"roundingIncrement:25 maximumFractionDigits:2 minimumFractionDigits:2", "1.1", "1.00"},
		{"roundingIncrement:25 maximumFractionDigits:2 minimumFractionDigits:2", "1.13", "1.25"},
		{"roundingIncrement:25 maximumFractionDigits:2 minimumFractionDigits:2", "99999.875", "100000.00"},
		{"roundingIncrement:25 maximumFractionDigits:2 minimumFractionDigits:2", "0.12", "0.00"},
		{"roundingIncrement:25 maximumFractionDigits:2 minimumFractionDigits:2", "0.125", "0.25"},
		{"roundingIncrement:25 maximumFractionDigits:2 minimumFractionDigits:2 roundingMode:halfEven", "0.125", "0.00"},
		{"roundingIncrement:25 maximumFractionDigits:2 minimumFractionDigits:2 roundingMode:halfEven", "0.375", "0.50"},
		{"roundingPriority:morePrecision maximumSignificantDigits:2 maximumFractionDigits:0", "99.5", "100"},
		{"roundingPriority:lessPrecision maximumSignificantDigits:2 maximumFractionDigits:0", "99.5", "100"},
		// ToRawPrecision's magnitude is its result's: 9.96 → 10 (0, not -1).
		{"roundingPriority:lessPrecision maximumSignificantDigits:2 minimumFractionDigits:1 maximumFractionDigits:1", "9.96", "10"},
		{"roundingPriority:morePrecision maximumSignificantDigits:2 minimumFractionDigits:1 maximumFractionDigits:1", "9.96", "10.0"},
	} {
		o := intlTestOptions(t, c.options)
		if got := intlFormatTest(&o, c.in); got != c.want {
			t.Errorf("{%s}: %s formats as %s, want %s", c.options, c.in, got, c.want)
		}
	}
}

// The reference: FormatNumericToString, ToRawPrecision, ToRawFixed and
// ApplyUnsignedRoundingMode (ECMA-402 §16.5) step by step with rationals.

type intlRefRaw struct {
	s        string
	x        *big.Rat
	intCount int
	rm       int
}

func intlRefUnsigned(mode intlRoundingMode, neg bool) string {
	switch mode {
	case intlRoundCeil:
		return map[bool]string{false: "infinity", true: "zero"}[neg]
	case intlRoundFloor:
		return map[bool]string{false: "zero", true: "infinity"}[neg]
	case intlRoundExpand:
		return "infinity"
	case intlRoundTrunc:
		return "zero"
	case intlRoundHalfCeil:
		return map[bool]string{false: "half-infinity", true: "half-zero"}[neg]
	case intlRoundHalfFloor:
		return map[bool]string{false: "half-zero", true: "half-infinity"}[neg]
	case intlRoundHalfExpand:
		return "half-infinity"
	case intlRoundHalfTrunc:
		return "half-zero"
	}
	return "half-even"
}

func intlRefApply(x, r1, r2 *big.Rat, um string) *big.Rat {
	if x.Cmp(r1) == 0 {
		return r1
	}
	switch um {
	case "zero":
		return r1
	case "infinity":
		return r2
	}
	d1 := new(big.Rat).Sub(x, r1)
	d2 := new(big.Rat).Sub(r2, x)
	switch d1.Cmp(d2) {
	case -1:
		return r1
	case 1:
		return r2
	}
	switch um {
	case "half-zero":
		return r1
	case "half-infinity":
		return r2
	}
	c := new(big.Rat).Quo(r1, new(big.Rat).Sub(r2, r1))
	if !c.IsInt() {
		panic("cardinality")
	}
	if new(big.Int).Mod(c.Num(), big.NewInt(2)).Sign() == 0 {
		return r1
	}
	return r2
}

func intlRefFloor(x *big.Rat) *big.Int {
	return new(big.Int).Div(x.Num(), x.Denom()) // Euclidean: floor for a positive denominator
}

func intlRefCut(m string, cut int) string {
	for cut > 0 && m[len(m)-1] == '0' {
		m = m[:len(m)-1]
		cut--
	}
	return strings.TrimSuffix(m, ".")
}

func intlRefToRawPrecision(x *big.Rat, minP, maxP int, um string) intlRefRaw {
	p := maxP
	var m string
	var e int
	var xf *big.Rat
	if x.Sign() == 0 {
		m, e, xf = strings.Repeat("0", p), 0, new(big.Rat)
	} else {
		e1 := 0
		for x.Cmp(intlPow10(e1)) < 0 {
			e1--
		}
		for x.Cmp(intlPow10(e1+1)) >= 0 {
			e1++
		}
		unit := intlPow10(e1 - p + 1)
		n1 := intlRefFloor(new(big.Rat).Quo(x, unit))
		r1 := new(big.Rat).Mul(new(big.Rat).SetInt(n1), unit)
		n2, e2, r2 := n1, e1, r1
		if r1.Cmp(x) != 0 {
			n2 = new(big.Int).Add(n1, big.NewInt(1))
			r2 = new(big.Rat).Mul(new(big.Rat).SetInt(n2), unit)
			if len(n2.String()) > p {
				n2.Div(n2, big.NewInt(10))
				e2++
			}
		}
		xf = intlRefApply(x, r1, r2, um)
		n := n2
		if e = e2; xf == r1 {
			n, e = n1, e1
		}
		m = n.String()
	}
	var intCount int
	switch {
	case e >= p-1:
		m += strings.Repeat("0", e-p+1)
		intCount = e + 1
	case e >= 0:
		m = m[:e+1] + "." + m[e+1:]
		intCount = e + 1
	default:
		m = "0." + strings.Repeat("0", -(e+1)) + m
		intCount = 1
	}
	if strings.Contains(m, ".") && maxP > minP {
		m = intlRefCut(m, maxP-minP)
	}
	return intlRefRaw{m, xf, intCount, e - p + 1}
}

func intlRefToRawFixed(x *big.Rat, minF, maxF, inc int, um string) intlRefRaw {
	f := maxF
	unit := intlPow10(-f)
	bi := big.NewInt(int64(inc))
	n1 := intlRefFloor(new(big.Rat).Quo(x, unit))
	n1.Sub(n1, new(big.Int).Mod(n1, bi))
	r1 := new(big.Rat).Mul(new(big.Rat).SetInt(n1), unit)
	n2, r2 := n1, r1
	if r1.Cmp(x) != 0 {
		n2 = new(big.Int).Add(n1, bi)
		r2 = new(big.Rat).Mul(new(big.Rat).SetInt(n2), unit)
	}
	xf := intlRefApply(x, r1, r2, um)
	n := n2
	if xf == r1 {
		n = n1
	}
	m := n.String()
	var intCount int
	if f != 0 {
		k := len(m)
		if k <= f {
			m = strings.Repeat("0", f+1-k) + m
			k = f + 1
		}
		a, b := m[:k-f], m[k-f:]
		m = a + "." + b
		intCount = len(a)
	} else {
		intCount = len(m)
	}
	return intlRefRaw{intlRefCut(m, maxF-minF), xf, intCount, -f}
}

// intlRefFormat returns FormatNumericToString's string and rounded value
// (its absolute value) of a sign and an absolute value.
func intlRefFormat(o *intlDigitOptions, neg bool, x *big.Rat) (string, *big.Rat) {
	um := intlRefUnsigned(o.mode, neg)
	var res intlRefRaw
	switch o.roundingType {
	case intlSignificantDigits:
		res = intlRefToRawPrecision(x, o.minSig, o.maxSig, um)
	case intlFractionDigits:
		res = intlRefToRawFixed(x, o.minFrac, o.maxFrac, o.increment, um)
	default:
		sr := intlRefToRawPrecision(x, o.minSig, o.maxSig, um)
		fr := intlRefToRawFixed(x, o.minFrac, o.maxFrac, o.increment, um)
		fixedIsMorePrecise := fr.rm < sr.rm
		switch {
		case o.roundingType == intlMorePrecision && fixedIsMorePrecise:
			res = fr
		case o.roundingType == intlLessPrecision && !fixedIsMorePrecise:
			res = fr
		default:
			res = sr
		}
	}
	s := res.s
	if o.stripIfInteger && res.x.IsInt() {
		s, _, _ = strings.Cut(s, ".")
	}
	if res.intCount < o.minInt {
		s = strings.Repeat("0", o.minInt-res.intCount) + s
	}
	return s, res.x
}

var intlIncrements = [...]int{1, 2, 5, 10, 20, 25, 50, 100, 200, 250, 500, 1000, 2000, 2500, 5000}

// intlRefCase formats digits × 10^exp (digits without leading zeros) with
// options drawn from opt, and compares the result with the reference.
func intlRefCase(t *testing.T, digits []byte, exp int, opt uint64) {
	next := func(n uint64) int {
		v := int(opt % n)
		opt /= n
		return v
	}
	o := intlDigitOptions{mode: intlRoundingMode(next(9)), roundingType: intlRoundingType(next(4)),
		minInt: 1 + next(21), stripIfInteger: next(4) == 0, increment: 1}
	if next(2) == 0 {
		o.increment = intlIncrements[next(uint64(len(intlIncrements)))]
	}
	switch next(3) {
	case 0:
		o.maxFrac = next(101)
	case 1:
		o.maxFrac = next(8)
	default:
		o.maxFrac = next(30)
	}
	o.minFrac = next(uint64(o.maxFrac) + 1)
	o.maxSig = 1 + next(21)
	o.minSig = 1 + next(uint64(o.maxSig))
	neg := next(2) == 0

	d := intlDecimal{digits: append([]byte(nil), digits...), exp: exp, neg: neg}
	d.trim()
	in := intlDecimalString(&d)
	x := intlDecimalRat(&d)
	wantS, wantX := intlRefFormat(&o, neg, x)
	gotS := string(o.formatNumericToString(&d, nil))
	checkIntlDecimal(t, &d, in)
	if gotS != wantS || intlDecimalRat(&d).Cmp(wantX) != 0 || d.neg != neg {
		t.Fatalf("%+v: %s formats as %s (%s), want %s (%s)", o, in, gotS, intlDecimalString(&d), wantS, wantX.FloatString(40))
	}
}

// intlFuzzDigits makes digits from raw bytes, nines and fives often.
func intlFuzzDigits(raw []byte) []byte {
	b := make([]byte, 0, min(len(raw), 60))
	for _, c := range raw[:min(len(raw), 60)] {
		b = append(b, "0123456789999550"[c%16])
	}
	for len(b) > 0 && b[0] == '0' {
		b = b[1:]
	}
	return b
}

func TestIntlFormatNumericToStringReference(t *testing.T) {
	r := rand.New(rand.NewPCG(11, 12))
	raw := make([]byte, 60)
	for range 300000 {
		for i := range raw {
			raw[i] = byte(r.Uint32())
		}
		n := 1 + r.IntN([...]int{3, 8, 25, 60}[r.IntN(4)])
		intlRefCase(t, intlFuzzDigits(raw[:n]), r.IntN(80)-40, r.Uint64())
	}
}

func FuzzIntlDecimal(f *testing.F) {
	f.Add([]byte("9995"), int16(1), uint64(0))
	f.Add([]byte("125"), int16(0), uint64(12345678))
	f.Add([]byte("99999875"), int16(5), uint64(987654321987))
	f.Add([]byte("1"), int16(-30), uint64(1<<40))
	f.Add([]byte(""), int16(0), uint64(77))
	f.Fuzz(func(t *testing.T, raw []byte, exp int16, opt uint64) {
		intlRefCase(t, intlFuzzDigits(raw), int(exp%120), opt)
	})
}

var intlDecimalSink int

// TestIntlDecimalNoAlloc checks that formatting a Number into stack
// buffers allocates nothing.
func TestIntlDecimalNoAlloc(t *testing.T) {
	cases := []struct {
		f float64
		o intlDigitOptions
	}{
		{1234.5, intlDigitOptions{minInt: 1, maxFrac: 3, mode: intlRoundHalfExpand, increment: 1}},
		{0.000123456, intlDigitOptions{minInt: 1, minSig: 1, maxSig: 3, roundingType: intlSignificantDigits, mode: intlRoundHalfExpand, increment: 1}},
		{1e21, intlDigitOptions{minInt: 1, maxFrac: 3, mode: intlRoundHalfExpand, increment: 1}},
		{-9.995, intlDigitOptions{minInt: 1, minFrac: 2, maxFrac: 2, mode: intlRoundHalfEven, increment: 25}},
		{123456.789, intlDigitOptions{minInt: 1, maxFrac: 2, minSig: 1, maxSig: 4, roundingType: intlMorePrecision, mode: intlRoundCeil, increment: 1}},
	}
	for _, c := range cases {
		if n := testing.AllocsPerRun(100, func() {
			var digits [40]byte
			var out [64]byte
			d := intlDecimal{digits: digits[:0]}
			d.setFloat(c.f)
			intlDecimalSink += len(c.o.formatNumericToString(&d, out[:0]))
		}); n != 0 {
			t.Errorf("formatting %v allocates %v times", c.f, n)
		}
	}
}

func BenchmarkIntlDecimal(b *testing.B) {
	frac3 := intlDigitOptions{minInt: 1, maxFrac: 3, mode: intlRoundHalfExpand, increment: 1}
	sig3 := intlDigitOptions{minInt: 1, minSig: 1, maxSig: 3, roundingType: intlSignificantDigits, mode: intlRoundHalfExpand, increment: 1}
	number := func(f float64, o intlDigitOptions) func(b *testing.B) {
		return func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var digits [40]byte
				var out [64]byte
				d := intlDecimal{digits: digits[:0]}
				d.setFloat(f)
				intlDecimalSink += len(o.formatNumericToString(&d, out[:0]))
			}
		}
	}
	b.Run("1234.5/maxFrac3", number(1234.5, frac3))
	b.Run("0.000123456/sig3", number(0.000123456, sig3))
	b.Run("1e21/maxFrac3", number(1e21, frac3))
	b.Run("1250/maxFrac0", number(1250, intlDigitOptions{minInt: 1, mode: intlRoundHalfExpand, increment: 1}))
	b.Run("string30/maxFrac3", func(b *testing.B) {
		s := asciiString("123456789012345678901234567890.123456")
		b.ReportAllocs()
		for b.Loop() {
			var digits [40]byte
			var out [64]byte
			d := intlDecimal{digits: digits[:0]}
			d.setString(s)
			intlDecimalSink += len(frac3.formatNumericToString(&d, out[:0]))
		}
	})
	b.Run("bigint/maxFrac3", func(b *testing.B) {
		v, _ := NewBigIntFromDecimal("123456789012345678901234567890")
		b.ReportAllocs()
		for b.Loop() {
			var digits [40]byte
			var out [64]byte
			d := intlDecimal{digits: digits[:0]}
			d.setBigInt(v)
			intlDecimalSink += len(frac3.formatNumericToString(&d, out[:0]))
		}
	})
}

// TestIntlDecimalNode compares FormatNumericToString with Node 24.22's
// Intl.NumberFormat (ICU 78.3) on the cases intl_decimal_node.js wrote:
// random Numbers, Strings and BigInts with random digit options.
func TestIntlDecimalNode(t *testing.T) {
	gz, err := os.ReadFile(filepath.Join("testdata", "intl_decimal_node.json.gz"))
	if err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		t.Fatal(err)
	}
	var cases [][4]string
	if err := json.NewDecoder(zr).Decode(&cases); err != nil {
		t.Fatal(err)
	}
	// The differences, each decided by the specification over ICU, and
	// checked against the reference.
	allowed := []struct {
		reason string
		match  func(kind, text string, o *intlDigitOptions, d *intlDecimal) bool
		count  int
	}{{
		// ICU 78 rounds to an increment other than 1 through DecNum
		// arithmetic and doubles, which keep 34 or 15-17 significant
		// digits: ToRawFixed is exact, so a multiple stays itself.
		reason: "roundingIncrement other than 1 with at least 15 significant digits",
		match: func(kind, text string, o *intlDigitOptions, d *intlDecimal) bool {
			return o.increment != 1 && len(d.digits) >= 15
		},
	}, {
		// Node keeps such tiny strings exact; ToIntlMathematicalValue's
		// RoundMVResult step makes a value whose Number is a zero that zero.
		reason: "a String below 2^-1075 is zero",
		match: func(kind, text string, o *intlDigitOptions, d *intlDecimal) bool {
			return kind == "s" && (text == "1e-400" || text == "2.4703282292062327e-324")
		},
	}}
	failures := 0
	for _, c := range cases {
		kind, text, options, want := c[0], c[1], c[2], c[3]
		o := intlTestOptions(t, options)
		var d intlDecimal
		switch kind {
		case "n":
			f, err := strconv.ParseFloat(text, 64)
			if err != nil {
				t.Fatalf("number %q", text)
			}
			d.setFloat(f)
		case "s":
			d.setString(FromGoString(text))
		case "b":
			b, ok := NewBigIntFromDecimal(text)
			if !ok {
				t.Fatalf("bigint %q", text)
			}
			d.setBigInt(b)
		}
		sign := ""
		if d.neg {
			sign = "-"
		}
		var got, ref string
		switch d.kind {
		case intlNaN:
			got, ref = "NaN", "NaN"
		case intlInfinity:
			got, ref = sign+"∞", sign+"∞"
		default:
			refS, _ := intlRefFormat(&o, d.neg, intlDecimalRat(&d))
			ref = sign + refS
			in := d
			in.digits = append([]byte(nil), d.digits...)
			got = sign + string(o.formatNumericToString(&d, nil))
			d = in // the input, for the allowed differences
		}
		key := kind + " " + text + " {" + options + "}"
		if got != ref {
			t.Errorf("%s: %s, the reference %s", key, got, ref)
		}
		if got == want {
			continue
		}
		explained := false
		for i := range allowed {
			if allowed[i].match(kind, text, &o, &d) {
				allowed[i].count++
				explained = true
				break
			}
		}
		if !explained {
			if failures++; failures <= 50 {
				t.Errorf("%s: %s, Node %s", key, got, want)
			}
		}
	}
	for _, a := range allowed {
		t.Logf("%d differences: %s", a.count, a.reason)
	}
	if failures > 0 {
		t.Errorf("%d of %d cases differ from Node's", failures, len(cases))
	}
}
