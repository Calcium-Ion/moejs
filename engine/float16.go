package engine

import "math"

// IEEE 754 binary16 conversions for DataView.prototype.getFloat16 and
// setFloat16: Go has no float16 type. A binary16 value has 1 sign bit, 5
// exponent bits (bias 15) and 10 mantissa bits.

// float64ToFloat16 rounds f to the nearest binary16 value, ties to even
// (IEEE 754 roundTiesToEven, which the spec's Float16 conversion uses), and
// returns its bits. Values beyond the largest finite binary16 (65504) round
// to Infinity from 65520 up; NaN becomes the quiet NaN 0x7e00.
func float64ToFloat16(f float64) uint16 {
	b := math.Float64bits(f)
	sign := uint16(b>>48) & 0x8000
	exp := int(b>>52) & 0x7ff
	mant := b & (1<<52 - 1)
	switch exp {
	case 0x7ff:
		if mant != 0 {
			return sign | 0x7e00
		}
		return sign | 0x7c00
	case 0:
		return sign // ±0, or a float64 subnormal: far below half the smallest binary16
	}
	e := exp - 1023
	m := mant | 1<<52 // 53 significant bits, value m × 2^(e-52)
	// A normal binary16 keeps 11 significant bits; a subnormal one counts
	// in units of 2^-24, so it keeps fewer.
	shift := 42
	if e < -14 {
		shift = 28 - e
		if shift > 53 {
			return sign // below half the smallest subnormal: rounds to zero
		}
	}
	q := m >> shift
	rem, half := m&(1<<shift-1), uint64(1)<<(shift-1)
	if rem > half || rem == half && q&1 != 0 {
		q++
	}
	if e < -14 {
		// A subnormal; rounding up to 1024 carries into the smallest normal.
		return sign | uint16(q)
	}
	// q includes the implicit bit (1024), so adding it to the biased
	// exponent minus one yields the fields, and a rounding carry to 2048
	// bumps the exponent.
	if bits := (e+14)<<10 + int(q); bits < 0x7c00 {
		return sign | uint16(bits)
	}
	return sign | 0x7c00
}

// float16ToFloat64 returns the value of the binary16 bits h (exactly
// representable as a float64).
func float16ToFloat64(h uint16) float64 {
	exp := int(h>>10) & 0x1f
	mant := float64(h & 0x3ff)
	var f float64
	switch exp {
	case 0:
		f = math.Ldexp(mant, -24)
	case 0x1f:
		if mant != 0 {
			return math.NaN()
		}
		f = math.Inf(1)
	default:
		f = math.Ldexp(mant+1024, exp-25)
	}
	if h&0x8000 != 0 {
		f = -f
	}
	return f
}
