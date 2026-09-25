package engine

import (
	"math"
	"math/big"
	"math/rand/v2"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGlobalBindings(t *testing.T) {
	for _, shared := range []bool{false, true} {
		r := NewRealmWith(RealmOptions{SharedIntrinsics: shared})
		g := r.Global
		// Value globals: never writable, enumerable or configurable.
		for _, n := range []string{"undefined", "NaN", "Infinity"} {
			assertOwnAttrs(t, r, g, n, false, false, false)
		}
		assert.True(t, jsGlobal(t, r, "undefined").IsUndefined())
		assert.True(t, math.IsNaN(jsNumber(t, jsGlobal(t, r, "NaN"))))
		assert.Equal(t, posInf, jsNumber(t, jsGlobal(t, r, "Infinity")))
		assert.Same(t, g, jsGlobal(t, r, "globalThis").AsObject())
		// Function globals: writable/configurable in mutable realms, frozen
		// bindings in the lockdown (shared) model; never enumerable.
		for _, c := range []struct {
			name   string
			length int
		}{{"parseInt", 2}, {"parseFloat", 1}, {"isNaN", 1}, {"isFinite", 1}} {
			assertOwnAttrs(t, r, g, c.name, !shared, false, !shared)
			assertNativeShape(t, r, jsGlobal(t, r, c.name), c.name, c.length)
		}
		assertOwnAttrs(t, r, g, "globalThis", !shared, false, !shared)
		assert.Empty(t, g.OwnEnumerableStringKeys())
		// Number.parseInt / parseFloat are the same function objects.
		assert.Same(t, jsGlobal(t, r, "parseInt").AsObject(), jsGet(t, r, ObjectValue(r.NumberCtor), "parseInt").AsObject())
		assert.Same(t, jsGlobal(t, r, "parseFloat").AsObject(), jsGet(t, r, ObjectValue(r.NumberCtor), "parseFloat").AsObject())
	}
}

func TestParseInt(t *testing.T) {
	r := NewRealm()
	nan := math.NaN()
	nz := math.Copysign(0, -1)
	cases := []struct {
		in    string
		radix Value
		want  float64
	}{
		{"42", Undefined(), 42},
		{"  42abc", Undefined(), 42},
		{"\t\n\u00a0\ufeff\u3000 7", Undefined(), 7},
		{"-42", Undefined(), -42},
		{"+42", Undefined(), 42},
		{"-0", Undefined(), nz},
		{"0", Undefined(), 0},
		{"-", Undefined(), nan},
		{"+", Undefined(), nan},
		{"", Undefined(), nan},
		{"   ", Undefined(), nan},
		{"abc", Undefined(), nan},
		{"0x1F", Undefined(), 31},
		{"0X1f", Undefined(), 31},
		{"-0x10", Undefined(), -16},
		{"0x1F", IntValue(16), 31},
		{"0x1F", IntValue(10), 0},
		{"0x", Undefined(), nan},
		{"0x", IntValue(16), nan},
		{"0b11", Undefined(), 0},
		{"0o17", Undefined(), 0},
		{"017", Undefined(), 17},
		{"1e3", Undefined(), 1},
		{"3.99", Undefined(), 3},
		{".5", Undefined(), nan},
		{"12", IntValue(1), nan},
		{"12", IntValue(37), nan},
		{"12", IntValue(-1), nan},
		{"z", IntValue(36), 35},
		{"Z", IntValue(36), 35},
		{"zz", IntValue(36), 1295},
		{"101", IntValue(2), 5},
		{"102", IntValue(2), 2},
		{"77", IntValue(8), 63},
		{"ff", IntValue(16), 255},
		{"11", NumberValue(2.9), 3},        // radix ToInt32 truncates
		{"11", NumberValue(4294967298), 3}, // radix ToInt32 wraps to 2
		{"11", str("16"), 17},              // radix coerced from string
		{"11", Null(), 11},                 // ToInt32(null) = 0 -> 10
		{"11", NaN(), 11},                  // ToInt32(NaN) = 0 -> 10
		{"9007199254740993", Undefined(), 9007199254740992},
		{"123456789012345678901234567890", Undefined(), 1.2345678901234568e29},
		{"ffffffffffffffffff", IntValue(16), 4.722366482869645e21},
		{"1111111111111111111111111111111111111111111111111111111", IntValue(2), 3.602879701896397e16},
		{"Infinity", Undefined(), nan},
		{"1" + string(make([]byte, 0)) + "0000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000", Undefined(), posInf},
		{"１２", Undefined(), nan}, // fullwidth digits are not digits
		{"12٣", Undefined(), 12},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got := jsNumber(t, jsCall(t, r, ObjectValue(r.Global), "parseInt", str(c.in), c.radix))
			assert.True(t, sameNumber(c.want, got), "parseInt(%q, %s) = %v, want %v", c.in, c.radix.String(), got, c.want)
		})
	}
	// Non-string first arguments are coerced with ToString.
	assert.Equal(t, 15.0, jsNumber(t, jsCall(t, r, ObjectValue(r.Global), "parseInt", NumberValue(15.99))))
	assert.Equal(t, 1.0, jsNumber(t, jsCall(t, r, ObjectValue(r.Global), "parseInt", NumberValue(1e21)))) // "1e+21"
	assert.True(t, math.IsNaN(jsNumber(t, jsCall(t, r, ObjectValue(r.Global), "parseInt"))))
	assert.True(t, math.IsNaN(jsNumber(t, jsCall(t, r, ObjectValue(r.Global), "parseInt", Null()))))
	// The string is coerced before the radix, and errors propagate.
	var log []string
	s := r.NewObject()
	s.DefineOwnDataFast(r, StringKey(AtomToString), nativeFn(r, func(Value, []Value) (Value, error) {
		log = append(log, "string")
		return str("10"), nil
	}), attrHidden)
	radix := objWithValueOf(r, IntValue(2), "radix", &log)
	got := jsNumber(t, jsCall(t, r, ObjectValue(r.Global), "parseInt", ObjectValue(s), radix))
	assert.Equal(t, 2.0, got)
	assert.Equal(t, []string{"string", "radix"}, log)
	err := jsCallErr(t, r, ObjectValue(r.Global), "parseInt", str("1"), SymbolValue(SymIterator))
	assert.ErrorContains(t, err, "Cannot convert a Symbol value to a number")
	err = jsCallErr(t, r, ObjectValue(r.Global), "parseInt", SymbolValue(SymIterator), throwingFn(r, "unreached"))
	assert.ErrorContains(t, err, "Cannot convert a Symbol value to a string")
}

// TestRadixDigitsLongStrings checks that parseInt and StringToNumber read a
// long digit string no further than its leading significant digits: past
// 1024 of them the value overflows in every radix, while math/big's
// conversion of the whole string is quadratic outside radixes 2, 4 and 16
// (seconds for 2M digits, in one native call that checks no interrupt). A
// call allocates as little for 1M digits as for a few. Below the bound the
// result is still correctly rounded.
func TestRadixDigitsLongStrings(t *testing.T) {
	allocated := func(f func()) uint64 {
		var m0, m1 runtime.MemStats
		runtime.ReadMemStats(&m0)
		f()
		runtime.ReadMemStats(&m1)
		return m1.TotalAlloc - m0.TotalAlloc
	}
	const n = 1 << 20
	for _, c := range []struct {
		digit string
		radix int
	}{{"z", 36}, {"7", 8}, {"2", 3}, {"9", 10}, {"1", 2}, {"f", 16}} {
		for _, src := range []string{strings.Repeat(c.digit, n), strings.Repeat("0", n) + strings.Repeat(c.digit, 1100)} {
			s := FromGoString(src)
			var got float64
			assert.Less(t, allocated(func() { got = parseIntString(s, c.radix) }), uint64(16<<10), "parseInt radix %d", c.radix)
			assert.Equal(t, posInf, got, "parseInt radix %d", c.radix)
		}
	}
	for _, c := range []struct {
		prefix, digit string
	}{{"0x", "f"}, {"0o", "7"}, {"0b", "1"}} {
		for _, src := range []string{c.prefix + strings.Repeat(c.digit, n), c.prefix + strings.Repeat("0", n) + strings.Repeat(c.digit, 1100)} {
			s := FromGoString(src)
			var got float64
			assert.Less(t, allocated(func() { got = StringToNumber(s) }), uint64(16<<10), "Number(%q...)", c.prefix)
			assert.Equal(t, posInf, got, "Number(%q...)", c.prefix)
		}
	}

	// The exact conversion of the whole digit string, rounded once.
	want := func(digits string, radix int) float64 {
		b, ok := new(big.Int).SetString(digits, radix)
		require.True(t, ok)
		f, _ := new(big.Float).SetInt(b).Float64()
		return f
	}
	maxHalf := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 1024), new(big.Int).Lsh(big.NewInt(1), 970))
	bounds := []*big.Int{
		new(big.Int).Sub(maxHalf, big.NewInt(1)), // rounds to MaxFloat64
		maxHalf,                                  // the tie rounds to even: Infinity
		new(big.Int).Lsh(big.NewInt(1), 1024),
		new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 1024), big.NewInt(1)),
	}
	rng := rand.New(rand.NewPCG(1, 2))
	for radix := 2; radix <= 36; radix++ {
		var cases []string
		for _, b := range bounds {
			cases = append(cases, b.Text(radix))
		}
		for range 20 {
			d := make([]byte, 900+rng.IntN(200))
			for i := range d {
				d[i] = "0123456789abcdefghijklmnopqrstuvwxyz"[rng.IntN(radix)]
			}
			d[0] = '1'
			cases = append(cases, string(d))
		}
		for _, digits := range cases {
			for _, zeros := range []int{0, 3000} {
				src := strings.Repeat("0", zeros) + digits
				assert.Equal(t, want(digits, radix), parseIntString(FromGoString(src), radix), "parseInt radix %d, %d zeros, %d digits", radix, zeros, len(digits))
				prefix := map[int]string{2: "0b", 8: "0o", 16: "0x"}[radix]
				if prefix != "" {
					assert.Equal(t, want(digits, radix), StringToNumber(FromGoString(prefix+src)), "Number radix %d, %d zeros, %d digits", radix, zeros, len(digits))
				}
			}
		}
	}
	assert.Equal(t, 0.0, StringToNumber(FromGoString("0x"+strings.Repeat("0", 2000))))
	assert.Equal(t, 0.0, parseIntString(FromGoString(strings.Repeat("0", 2000)), 7))
}

func TestParseFloat(t *testing.T) {
	r := NewRealm()
	nan := math.NaN()
	nz := math.Copysign(0, -1)
	cases := []struct {
		in   string
		want float64
	}{
		{"3.14", 3.14},
		{"  3.14abc", 3.14},
		{"\u2028-.5e2x", -50},
		{"+1.5", 1.5},
		{"-0", nz},
		{"-0.0e5", nz},
		{"0", 0},
		{"5.", 5},
		{"1.e3", 1000},
		{".5", 0.5},
		{".", nan},
		{".e5", nan},
		{"+", nan},
		{"-", nan},
		{"", nan},
		{"  ", nan},
		{"abc", nan},
		{"1e", 1},
		{"1e+", 1},
		{"1e-", 1},
		{"1e3", 1000},
		{"1E-3", 0.001},
		{"1.5e+2", 150},
		{"1.2.3", 1.2},
		{"0x10", 0},
		{"1_000", 1},
		{"Infinity", posInf},
		{"+Infinity", posInf},
		{"-Infinity", negInf},
		{"Infinityx", posInf},
		{"-Infinity5", negInf},
		{"infinity", nan},
		{"Inf", nan},
		{"1e400", posInf},
		{"-1e400", negInf},
		{"1e-400", 0},
		{"0.1", 0.1},
		{"123456789012345678901234567890", 1.2345678901234568e29},
		{"１２", nan},
		{"12３", 12},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got := jsNumber(t, jsCall(t, r, ObjectValue(r.Global), "parseFloat", str(c.in)))
			assert.True(t, sameNumber(c.want, got), "parseFloat(%q) = %v, want %v", c.in, got, c.want)
		})
	}
	assert.Equal(t, 12.5, jsNumber(t, jsCall(t, r, ObjectValue(r.Global), "parseFloat", NumberValue(12.5))))
	assert.True(t, math.IsNaN(jsNumber(t, jsCall(t, r, ObjectValue(r.Global), "parseFloat"))))
	assert.ErrorContains(t, jsCallErr(t, r, ObjectValue(r.Global), "parseFloat", SymbolValue(SymIterator)), "Symbol")
}

func TestIsNaNIsFinite(t *testing.T) {
	r := NewRealm()
	cases := []struct {
		in              Value
		isNaN, isFinite bool
	}{
		{NaN(), true, false},
		{IntValue(1), false, true},
		{NumberValue(posInf), false, false},
		{NumberValue(negInf), false, false},
		{str("12"), false, true},
		{str("abc"), true, false},
		{str(""), false, true},
		{Undefined(), true, false},
		{Null(), false, true},
		{True(), false, true},
		{ObjectValue(r.NewObject()), true, false},
	}
	for _, c := range cases {
		t.Run(c.in.String(), func(t *testing.T) {
			assert.Equal(t, Bool(c.isNaN), jsCall(t, r, ObjectValue(r.Global), "isNaN", c.in))
			assert.Equal(t, Bool(c.isFinite), jsCall(t, r, ObjectValue(r.Global), "isFinite", c.in))
		})
	}
	assert.Equal(t, True(), jsCall(t, r, ObjectValue(r.Global), "isNaN"))
	assert.Equal(t, False(), jsCall(t, r, ObjectValue(r.Global), "isFinite"))
	var log []string
	o := objWithValueOf(r, IntValue(3), "v", &log)
	assert.Equal(t, False(), jsCall(t, r, ObjectValue(r.Global), "isNaN", o))
	assert.Equal(t, []string{"v"}, log)
	require.ErrorContains(t, jsCallErr(t, r, ObjectValue(r.Global), "isFinite", SymbolValue(SymIterator)), "Symbol")
}
