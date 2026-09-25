package engine

import (
	"encoding/binary"
	"math"
	"math/big"
	"testing"
)

// FuzzSumPrecise checks Math.sumPrecise against an exact big.Rat sum
// rounded by big.Rat.Float64. The input bytes are float64 bit patterns;
// when the first byte is odd the exponents are squeezed into a narrow band
// so that sums cancel and carry.
func FuzzSumPrecise(f *testing.F) {
	for _, xs := range [][]float64{
		{1, 2, 3}, {0.1, 0.2}, {1e308, 1e308, -1e308}, {math.MaxFloat64, 0x1p970}, {math.MaxFloat64, 0x1p970, -5e-324},
		{1, 0x1p-53}, {1, 0x1p-53, 5e-324}, {5e-324, -5e-324}, {math.Inf(1), 1}, {math.NaN()}, {math.Copysign(0, -1)},
	} {
		for _, mode := range []byte{0, 1} {
			b := []byte{mode}
			for _, x := range xs {
				b = binary.LittleEndian.AppendUint64(b, math.Float64bits(x))
			}
			f.Add(b)
		}
	}
	r := NewRealm()
	fn, err := r.Math.GetProp(r, StringKey(AtomSumPrecise))
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 || len(data) > 8*256 {
			return
		}
		narrow := data[0]&1 != 0
		var xs []Value
		var exact big.Rat
		var tmp big.Rat
		nan, pos, neg, finite := false, false, false, false
		for b := data[1:]; len(b) >= 8; b = b[8:] {
			bits := binary.LittleEndian.Uint64(b)
			if narrow {
				// Keep the sign and 52 mantissa bits; the exponent lands
				// within 2^±16 of 1.
				e := 1023 - 16 + (bits>>52)&31
				bits = bits&(1<<63|1<<52-1) | e<<52
			}
			x := math.Float64frombits(bits)
			xs = append(xs, NumberValue(x))
			switch {
			case math.IsNaN(x):
				nan = true
			case math.IsInf(x, 1):
				pos = true
			case math.IsInf(x, -1):
				neg = true
			case x != 0 || !math.Signbit(x):
				finite = true
				exact.Add(&exact, tmp.SetFloat64(x))
			}
		}
		var want float64
		switch {
		case nan || pos && neg:
			want = math.NaN()
		case pos:
			want = math.Inf(1)
		case neg:
			want = math.Inf(-1)
		case !finite:
			want = math.Copysign(0, -1)
		default:
			want, _ = exact.Float64()
		}
		v, err := r.Call(fn, Undefined(), []Value{ObjectValue(r.NewArray(xs...))})
		if err != nil {
			t.Fatal(err)
		}
		if got := v.AsNumber(); !fuzzSameNumber(got, want) {
			t.Fatalf("sumPrecise(%v) = %v, want %v", xs, got, want)
		}
	})
}
