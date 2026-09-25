package engine

import (
	"math"
	"math/big"
	"math/bits"
	"math/rand/v2"
)

// installMath fills the Math namespace object (every ES2023 function and
// constant) on first touch in a mutable realm (installDeferred).
func installMath(r *Realm) {
	r.installKeys(r.Math, mathKeys)
}

var mathKeys = newKeyTable(mathConstants, mathFunctions, AtomMath)

var mathConstants = []valueDef{
	{AtomE, NumberValue(math.E), attrFrozen},
	{AtomLN10, NumberValue(math.Ln10), attrFrozen},
	{AtomLN2, NumberValue(math.Ln2), attrFrozen},
	{AtomLOG10E, NumberValue(math.Log10E), attrFrozen},
	{AtomLOG2E, NumberValue(math.Log2E), attrFrozen},
	{AtomPI, NumberValue(math.Pi), attrFrozen},
	{AtomSqrt12, NumberValue(math.Sqrt2 / 2), attrFrozen},
	{AtomSQRT2, NumberValue(math.Sqrt2), attrFrozen},
}

// mathFunctions is built once per process; the unary wrappers capture only
// the Go math function, never a realm.
var mathFunctions = []builtinDef{
	{AtomAbs, mathUnary(math.Abs), 1},
	{AtomAcos, mathUnary(math.Acos), 1},
	{AtomAcosh, mathUnary(math.Acosh), 1},
	{AtomAsin, mathUnary(math.Asin), 1},
	{AtomAsinh, mathUnary(math.Asinh), 1},
	{AtomAtan, mathUnary(math.Atan), 1},
	{AtomAtanh, mathUnary(math.Atanh), 1},
	{AtomAtan2, mathAtan2, 2},
	{AtomCbrt, mathUnary(math.Cbrt), 1},
	{AtomCeil, mathUnary(math.Ceil), 1},
	{AtomClz32, mathClz32, 1},
	{AtomCos, mathUnary(math.Cos), 1},
	{AtomCosh, mathUnary(math.Cosh), 1},
	{AtomExp, mathUnary(math.Exp), 1},
	{AtomExpm1, mathUnary(math.Expm1), 1},
	{AtomF16round, mathUnary(mathF16round), 1},
	{AtomFloor, mathUnary(math.Floor), 1},
	{AtomFround, mathUnary(mathFround), 1},
	{AtomHypot, mathHypot, 2},
	{AtomImul, mathImul, 2},
	{AtomLog, mathUnary(math.Log), 1},
	{AtomLog1p, mathUnary(math.Log1p), 1},
	{AtomLog10, mathUnary(mathLog10), 1},
	{AtomLog2, mathUnary(math.Log2), 1},
	{AtomMax, mathMax, 2},
	{AtomMin, mathMin, 2},
	{AtomPow, mathPow, 2},
	{AtomRandom, mathRandom, 0},
	{AtomRound, mathUnary(mathRoundHalfUp), 1},
	{AtomSign, mathUnary(mathSign), 1},
	{AtomSin, mathUnary(math.Sin), 1},
	{AtomSinh, mathUnary(math.Sinh), 1},
	{AtomSqrt, mathUnary(math.Sqrt), 1},
	{AtomSumPrecise, mathSumPrecise, 1},
	{AtomTan, mathUnary(math.Tan), 1},
	{AtomTanh, mathUnary(math.Tanh), 1},
	{AtomTrunc, mathUnary(math.Trunc), 1},
}

// mathUnary adapts a float64 function to a one-argument Math method that
// coerces its argument with ToNumber.
func mathUnary(fn func(float64) float64) NativeFunc {
	return func(r *Realm, this Value, args []Value) (Value, error) {
		x, err := toNumberArg(r, args, 0)
		if err != nil {
			return Undefined(), err
		}
		return NumberValue(fn(x)), nil
	}
}

// toNumberArg is ToNumber(args[i]) with a number fast path.
func toNumberArg(r *Realm, args []Value, i int) (float64, error) {
	v := Arg(args, i)
	if v.IsNumber() {
		return v.AsNumber(), nil
	}
	return r.ToNumber(v)
}

// mathRoundHalfUp implements Math.round: the closest integer, ties toward
// +∞, and the sign of zero preserved for results in (-0.5, 0].
func mathRoundHalfUp(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) || x == math.Trunc(x) {
		return x
	}
	if x > 0 && x < 0.5 {
		return 0
	}
	if x < 0 && x >= -0.5 {
		return negativeZero
	}
	f := math.Floor(x)
	if x-f >= 0.5 {
		return f + 1
	}
	return f
}

// mathSign implements Math.sign (NaN and ±0 pass through).
func mathSign(x float64) float64 {
	switch {
	case x > 0:
		return 1
	case x < 0:
		return -1
	}
	return x
}

// mathFround rounds to the nearest float32 value.
func mathFround(x float64) float64 { return float64(float32(x)) }

// mathF16round implements Math.f16round: x rounded to the nearest
// binary16 value, ties to even.
func mathF16round(x float64) float64 { return float16ToFloat64(float64ToFloat16(x)) }

// mathLog10 is math.Log10 corrected to return exact integers for exact
// powers of ten (Go's Log10 is off by one ulp for some, e.g. 1e15).
func mathLog10(x float64) float64 {
	l := math.Log10(x)
	if n := math.Round(l); math.Abs(l-n) < 1e-9 && n >= -323 && n <= 308 && math.Pow10(int(n)) == x {
		return n
	}
	return l
}

func mathAtan2(r *Realm, this Value, args []Value) (Value, error) {
	y, err := toNumberArg(r, args, 0)
	if err != nil {
		return Undefined(), err
	}
	x, err := toNumberArg(r, args, 1)
	if err != nil {
		return Undefined(), err
	}
	return NumberValue(math.Atan2(y, x)), nil
}

func mathPow(r *Realm, this Value, args []Value) (Value, error) {
	base, err := toNumberArg(r, args, 0)
	if err != nil {
		return Undefined(), err
	}
	exponent, err := toNumberArg(r, args, 1)
	if err != nil {
		return Undefined(), err
	}
	return NumberValue(NumberExponentiate(base, exponent)), nil
}

// NumberExponentiate implements Number::exponentiate (also the `**`
// operator): math.Pow with the cases where IEEE pow and ECMAScript differ
// fixed up.
func NumberExponentiate(base, exponent float64) float64 {
	switch {
	case exponent != exponent:
		return math.NaN()
	case exponent == 0:
		return 1
	case (base == 1 || base == -1) && math.IsInf(exponent, 0):
		return math.NaN()
	case base == 10 && exponent == math.Trunc(exponent) && exponent >= -323 && exponent <= 308:
		return math.Pow10(int(exponent))
	}
	return math.Pow(base, exponent)
}

// mathMax implements Math.max: every argument is coerced first (in order),
// NaN wins, and +0 is larger than -0.
func mathMax(r *Realm, this Value, args []Value) (Value, error) {
	highest := negInf
	sawNaN := false
	for i := range args {
		n, err := toNumberArg(r, args, i)
		if err != nil {
			return Undefined(), err
		}
		switch {
		case n != n:
			sawNaN = true
		case n > highest, n == 0 && highest == 0 && !math.Signbit(n) && math.Signbit(highest):
			highest = n
		}
	}
	if sawNaN {
		return NaN(), nil
	}
	return NumberValue(highest), nil
}

// mathMin mirrors mathMax with -0 smaller than +0.
func mathMin(r *Realm, this Value, args []Value) (Value, error) {
	lowest := posInf
	sawNaN := false
	for i := range args {
		n, err := toNumberArg(r, args, i)
		if err != nil {
			return Undefined(), err
		}
		switch {
		case n != n:
			sawNaN = true
		case n < lowest, n == 0 && lowest == 0 && math.Signbit(n) && !math.Signbit(lowest):
			lowest = n
		}
	}
	if sawNaN {
		return NaN(), nil
	}
	return NumberValue(lowest), nil
}

// mathHypot implements Math.hypot: all arguments coerced first, any
// infinity wins over NaN, and the sum of squares is scaled by the largest
// magnitude to avoid overflow and accumulated with Kahan compensation (the
// same approximation V8 uses).
func mathHypot(r *Realm, this Value, args []Value) (Value, error) {
	var buf [8]float64
	nums := buf[:0]
	sawInf, sawNaN := false, false
	maxAbs := 0.0
	for i := range args {
		n, err := toNumberArg(r, args, i)
		if err != nil {
			return Undefined(), err
		}
		switch {
		case math.IsInf(n, 0):
			sawInf = true
		case n != n:
			sawNaN = true
		default:
			n = math.Abs(n)
			maxAbs = max(maxAbs, n)
			nums = append(nums, n)
		}
	}
	switch {
	case sawInf:
		return NumberValue(posInf), nil
	case sawNaN:
		return NaN(), nil
	case maxAbs == 0:
		return IntValue(0), nil
	}
	sum, compensation := 0.0, 0.0
	for _, n := range nums {
		q := n / maxAbs
		summand := q*q - compensation
		preliminary := sum + summand
		compensation = (preliminary - sum) - summand
		sum = preliminary
	}
	return NumberValue(math.Sqrt(sum) * maxAbs), nil
}

func mathImul(r *Realm, this Value, args []Value) (Value, error) {
	a, err := r.ToInt32(Arg(args, 0))
	if err != nil {
		return Undefined(), err
	}
	b, err := r.ToInt32(Arg(args, 1))
	if err != nil {
		return Undefined(), err
	}
	return IntValue(int(a * b)), nil
}

func mathClz32(r *Realm, this Value, args []Value) (Value, error) {
	n, err := r.ToUint32(Arg(args, 0))
	if err != nil {
		return Undefined(), err
	}
	return IntValue(bits.LeadingZeros32(n)), nil
}

// mathRandom draws from the realm's generator, seeded from the runtime's
// random source on first use.
func mathRandom(r *Realm, this Value, args []Value) (Value, error) {
	l := r.lazyState()
	if l.rng == nil {
		l.rng = rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	}
	return NumberValue(l.rng.Float64()), nil
}

// Math.sumPrecise states (ES2026 21.3.2.34).
const (
	sumMinusZero = iota
	sumFinite
	sumPlusInfinity
	sumMinusInfinity
	sumNaN
)

// mathSumPrecise implements Math.sumPrecise(items): the exact sum of the
// iterated Numbers, rounded once. Elements are not coerced; anything but a
// Number is a TypeError that closes the iterator. The spec's RangeError at
// 2^53 elements is not reachable in practice and is left out.
func mathSumPrecise(r *Realm, this Value, args []Value) (Value, error) {
	items := Arg(args, 0)
	if items.IsNullish() {
		return Undefined(), r.TypeError("%s is not iterable", r.DisplayString(items))
	}
	ir, err := r.getIterator(items)
	if err != nil {
		return Undefined(), err
	}
	state := sumMinusZero
	var sum exactSum
	for k := int64(0); ; k++ {
		if err := interruptEvery(r, k); err != nil {
			return Undefined(), err
		}
		v, done, err := ir.step(r)
		if err != nil {
			return Undefined(), err
		}
		if done {
			break
		}
		if !v.IsNumber() {
			return Undefined(), ir.closeThrow(r, r.TypeError("Math.sumPrecise: %s is not a number", r.DisplayString(v)))
		}
		if state == sumNaN {
			continue
		}
		switch n := v.AsNumber(); {
		case n != n:
			state = sumNaN
		case math.IsInf(n, 1):
			if state == sumMinusInfinity {
				state = sumNaN
			} else {
				state = sumPlusInfinity
			}
		case math.IsInf(n, -1):
			if state == sumPlusInfinity {
				state = sumNaN
			} else {
				state = sumMinusInfinity
			}
		case state == sumMinusZero || state == sumFinite:
			if n != 0 || !math.Signbit(n) {
				state = sumFinite
				sum.add(n)
			}
		}
	}
	switch state {
	case sumNaN:
		return NaN(), nil
	case sumPlusInfinity:
		return NumberValue(math.Inf(1)), nil
	case sumMinusInfinity:
		return NumberValue(math.Inf(-1)), nil
	case sumMinusZero:
		return NumberValue(math.Copysign(0, -1)), nil
	}
	return NumberValue(sum.float64()), nil
}

// exactSum adds finite doubles without rounding: the total is kept as an
// integer count of 2^-1074, the smallest subnormal, of which every double
// is a multiple. Intermediate sums beyond the double range stay exact.
type exactSum struct{ acc, t big.Int }

func (s *exactSum) add(x float64) {
	b := math.Float64bits(x)
	exp := uint(b>>52) & 0x7FF
	mant := b & (1<<52 - 1)
	if exp == 0 {
		exp = 1 // subnormal: mant * 2^-1074
	} else {
		mant |= 1 << 52
	}
	// |x| = mant * 2^(exp-1075) = mant << (exp-1) units.
	s.t.SetUint64(mant)
	s.t.Lsh(&s.t, exp-1)
	if b>>63 != 0 {
		s.acc.Sub(&s.acc, &s.t)
	} else {
		s.acc.Add(&s.acc, &s.t)
	}
}

// float64 rounds the sum to the nearest double, ties to even; a sum at or
// beyond 2^1024 - 2^970 rounds to an infinity (ES2026 6.1.6.1).
func (s *exactSum) float64() float64 {
	var f big.Float
	f.SetInt(&s.acc) // precision 0 becomes the bit length: exact
	f.SetMantExp(&f, -1074)
	x, _ := f.Float64()
	return x
}
