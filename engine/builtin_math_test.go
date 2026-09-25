package engine

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMathConstantsAndShape(t *testing.T) {
	r := NewRealm()
	m := ObjectValue(r.Math)
	assert.False(t, IsCallable(m))
	assert.Same(t, r.ObjectPrototype, r.Math.Proto())
	assert.Equal(t, "[object Math]", jsString(t, jsCall(t, r, m, "toString")))
	for _, c := range []struct {
		name string
		want float64
	}{
		{"E", 2.718281828459045}, {"LN10", 2.302585092994046}, {"LN2", 0.6931471805599453}, {"LOG10E", 0.4342944819032518},
		{"LOG2E", 1.4426950408889634}, {"PI", 3.141592653589793}, {"SQRT1_2", 0.7071067811865476}, {"SQRT2", 1.4142135623730951},
	} {
		assertOwnAttrs(t, r, r.Math, c.name, false, false, false)
		assert.Equal(t, c.want, jsNumber(t, jsGet(t, r, m, c.name)), c.name)
	}
	for _, c := range []struct {
		name   string
		length int
	}{
		{"abs", 1}, {"acos", 1}, {"acosh", 1}, {"asin", 1}, {"asinh", 1}, {"atan", 1}, {"atanh", 1}, {"atan2", 2}, {"cbrt", 1},
		{"ceil", 1}, {"clz32", 1}, {"cos", 1}, {"cosh", 1}, {"exp", 1}, {"expm1", 1}, {"f16round", 1}, {"floor", 1}, {"fround", 1}, {"hypot", 2},
		{"imul", 2}, {"log", 1}, {"log1p", 1}, {"log10", 1}, {"log2", 1}, {"max", 2}, {"min", 2}, {"pow", 2}, {"random", 0},
		{"round", 1}, {"sign", 1}, {"sin", 1}, {"sinh", 1}, {"sqrt", 1}, {"sumPrecise", 1}, {"tan", 1}, {"tanh", 1},
		{"trunc", 1},
	} {
		assertNativeShape(t, r, jsGet(t, r, m, c.name), c.name, c.length)
		assertOwnAttrs(t, r, r.Math, c.name, true, false, true)
	}
	assert.Empty(t, r.Math.OwnEnumerableStringKeys())
	assert.Len(t, r.Math.OwnPropertyKeys(), 8+37+1) // + @@toStringTag
}

func TestMathUnaryTable(t *testing.T) {
	r := NewRealm()
	m := ObjectValue(r.Math)
	nan, inf, nz := math.NaN(), posInf, math.Copysign(0, -1)
	cases := []struct {
		fn   string
		in   float64
		want float64
	}{
		{"abs", -3, 3}, {"abs", nz, 0}, {"abs", -inf, inf}, {"abs", nan, nan},
		{"ceil", 0.5, 1}, {"ceil", -0.5, nz}, {"ceil", -1.5, -1}, {"ceil", nz, nz}, {"ceil", 3, 3}, {"ceil", -inf, -inf},
		{"floor", 0.5, 0}, {"floor", -0.5, -1}, {"floor", nz, nz}, {"floor", 1.9999, 1}, {"floor", -0.0000001, -1},
		{"trunc", 0.9, 0}, {"trunc", -0.9, nz}, {"trunc", -1.9, -1}, {"trunc", 1e300, 1e300}, {"trunc", nan, nan},
		{"round", 0.5, 1}, {"round", 1.5, 2}, {"round", 2.5, 3}, {"round", -0.5, nz}, {"round", -1.5, -1}, {"round", -2.5, -2},
		{"round", -0.2, nz}, {"round", 0.2, 0}, {"round", 0.49999999999999994, 0}, {"round", -0.50000000000000001, nz},
		{"round", 4503599627370495.5, 4503599627370496}, {"round", -4503599627370495.5, -4503599627370495}, {"round", 1e300, 1e300},
		{"round", nz, nz}, {"round", inf, inf}, {"round", nan, nan}, {"round", -0.6, -1}, {"round", 2.4999999999999996, 2},
		{"sign", 5, 1}, {"sign", -5, -1}, {"sign", 0, 0}, {"sign", nz, nz}, {"sign", nan, nan}, {"sign", -inf, -1},
		{"sqrt", 4, 2}, {"sqrt", -1, nan}, {"sqrt", nz, nz}, {"sqrt", inf, inf}, {"sqrt", 2, 1.4142135623730951},
		{"cbrt", -8, -2}, {"cbrt", 27, 3}, {"cbrt", nz, nz}, {"cbrt", -inf, -inf},
		{"exp", 0, 1}, {"exp", 1, math.E}, {"exp", -inf, 0}, {"exp", inf, inf}, {"exp", nan, nan},
		{"expm1", 0, 0}, {"expm1", nz, nz}, {"expm1", -inf, -1}, {"expm1", 1e-10, 1.00000000005e-10},
		{"log", 1, 0}, {"log", 0, -inf}, {"log", nz, -inf}, {"log", -1, nan}, {"log", math.E, 1}, {"log", inf, inf},
		{"log1p", 0, 0}, {"log1p", nz, nz}, {"log1p", -1, -inf}, {"log1p", -2, nan},
		{"log2", 8, 3}, {"log2", 1024, 10}, {"log2", 1, 0}, {"log2", 0, -inf}, {"log2", 5e-324, -1074}, {"log2", 3, 1.5849625007211563},
		{"log10", 1000, 3}, {"log10", 1e15, 15}, {"log10", 1e21, 21}, {"log10", 1e-7, -7}, {"log10", 1, 0}, {"log10", 2, 0.3010299956639812}, {"log10", 0, -inf}, {"log10", -1, nan},
		{"sin", 0, 0}, {"sin", nz, nz}, {"sin", inf, nan}, {"cos", 0, 1}, {"cos", nz, 1}, {"cos", -inf, nan}, {"tan", 0, 0}, {"tan", nz, nz}, {"tan", inf, nan},
		{"asin", 1, math.Pi / 2}, {"asin", 2, nan}, {"asin", nz, nz}, {"acos", 1, 0}, {"acos", 0.5, 1.0471975511965976}, {"acos", -2, nan},
		{"atan", 0, 0}, {"atan", nz, nz}, {"atan", inf, math.Pi / 2}, {"atan", -inf, -math.Pi / 2},
		{"sinh", 0, 0}, {"sinh", nz, nz}, {"sinh", inf, inf}, {"cosh", 0, 1}, {"cosh", nz, 1}, {"cosh", -inf, inf},
		{"tanh", 0, 0}, {"tanh", nz, nz}, {"tanh", inf, 1}, {"tanh", -inf, -1},
		{"asinh", 0, 0}, {"asinh", nz, nz}, {"asinh", -inf, -inf}, {"acosh", 1, 0}, {"acosh", 0.5, nan}, {"acosh", inf, inf},
		{"atanh", 0, 0}, {"atanh", nz, nz}, {"atanh", 1, inf}, {"atanh", -1, -inf}, {"atanh", 2, nan},
		{"fround", 5.5, 5.5}, {"fround", 5.05, 5.050000190734863}, {"fround", 1e40, inf}, {"fround", -1e40, -inf}, {"fround", nz, nz}, {"fround", nan, nan}, {"fround", 1.7976931348623157e308, inf},
		{"clz32", 1, 31}, {"clz32", 0, 32}, {"clz32", -1, 0}, {"clz32", 0.5, 32}, {"clz32", 4294967296, 32}, {"clz32", 65536, 15}, {"clz32", nan, 32},
	}
	for _, c := range cases {
		t.Run(c.fn, func(t *testing.T) {
			got := jsNumber(t, jsCall(t, r, m, c.fn, NumberValue(c.in)))
			assert.True(t, sameNumber(c.want, got), "Math.%s(%v) = %v, want %v", c.fn, c.in, got, c.want)
		})
	}
	// Arguments are coerced with ToNumber; a missing argument is NaN.
	assert.Equal(t, 3.0, jsNumber(t, jsCall(t, r, m, "abs", str("-3"))))
	assert.Equal(t, 0.0, jsNumber(t, jsCall(t, r, m, "floor", Null())))
	assert.True(t, math.IsNaN(jsNumber(t, jsCall(t, r, m, "sqrt"))))
	assert.True(t, math.IsNaN(jsNumber(t, jsCall(t, r, m, "abs", Undefined()))))
	assert.Equal(t, 1.0, jsNumber(t, jsCall(t, r, m, "trunc", True())))
	assert.Equal(t, 31.0, jsNumber(t, jsCall(t, r, m, "clz32", str("1"))))
	var log []string
	assert.Equal(t, 2.0, jsNumber(t, jsCall(t, r, m, "round", objWithValueOf(r, NumberValue(1.5), "v", &log))))
	assert.Equal(t, []string{"v"}, log)
	assert.ErrorContains(t, jsCallErr(t, r, m, "abs", SymbolValue(SymIterator)), "Symbol")
}

func TestMathMaxMin(t *testing.T) {
	r := NewRealm()
	m := ObjectValue(r.Math)
	nz := negZero()
	cases := []struct {
		args     []Value
		max, min float64
	}{
		{nil, negInf, posInf},
		{[]Value{IntValue(3)}, 3, 3},
		{[]Value{IntValue(1), IntValue(3), IntValue(2)}, 3, 1},
		{[]Value{IntValue(-1), IntValue(-3)}, -1, -3},
		{[]Value{IntValue(0), nz}, 0, math.Copysign(0, -1)},
		{[]Value{nz, IntValue(0)}, 0, math.Copysign(0, -1)},
		{[]Value{nz, nz}, math.Copysign(0, -1), math.Copysign(0, -1)},
		{[]Value{IntValue(1), NaN(), IntValue(3)}, math.NaN(), math.NaN()},
		{[]Value{NaN()}, math.NaN(), math.NaN()},
		{[]Value{str("5"), IntValue(2)}, 5, 2},
		{[]Value{Undefined(), IntValue(1)}, math.NaN(), math.NaN()},
		{[]Value{Null(), IntValue(1)}, 1, 0},
		{[]Value{NumberValue(posInf), IntValue(1)}, posInf, 1},
		{[]Value{NumberValue(negInf), IntValue(1)}, 1, negInf},
	}
	for _, c := range cases {
		gotMax := jsNumber(t, jsCall(t, r, m, "max", c.args...))
		gotMin := jsNumber(t, jsCall(t, r, m, "min", c.args...))
		assert.True(t, sameNumber(c.max, gotMax), "max(%v) = %v want %v", c.args, gotMax, c.max)
		assert.True(t, sameNumber(c.min, gotMin), "min(%v) = %v want %v", c.args, gotMin, c.min)
	}
	// Every argument is coerced in order before NaN short-circuits.
	var log []string
	a := objWithValueOf(r, IntValue(1), "a", &log)
	b := objWithValueOf(r, IntValue(2), "b", &log)
	assert.True(t, math.IsNaN(jsNumber(t, jsCall(t, r, m, "max", a, NaN(), b))))
	assert.Equal(t, []string{"a", "b"}, log)
	log = nil
	err := jsCallErr(t, r, m, "min", a, throwingObj(r), b)
	assert.ErrorContains(t, err, "Cannot convert object to primitive value")
	assert.Equal(t, []string{"a"}, log)
}

// throwingObj is an object whose ToPrimitive fails.
func throwingObj(r *Realm) Value {
	o := r.NewObjectWithProto(nil)
	return ObjectValue(o)
}

func TestMathBinaryAndVariadic(t *testing.T) {
	r := NewRealm()
	m := ObjectValue(r.Math)
	nan, inf, nz := math.NaN(), posInf, math.Copysign(0, -1)
	call := func(fn string, args ...Value) float64 { return jsNumber(t, jsCall(t, r, m, fn, args...)) }
	num := func(f float64) Value { return NumberValue(f) }

	// pow / Number::exponentiate special cases.
	for _, c := range []struct{ b, e, want float64 }{
		{2, 10, 1024}, {2, 0.5, 1.4142135623730951}, {2, -1, 0.5}, {10, 2, 100}, {10, -2, 0.01}, {10, 21, 1e21}, {10, 308, 1e308}, {10, -5, 1e-5},
		{nan, 0, 1}, {nan, nz, 1}, {1, nan, nan}, {1, inf, nan}, {-1, inf, nan}, {-1, -inf, nan}, {1, 1, 1},
		{2, inf, inf}, {0.5, inf, 0}, {2, -inf, 0}, {0.5, -inf, inf}, {inf, 1, inf}, {inf, -1, 0}, {-inf, 3, -inf}, {-inf, 2, inf}, {-inf, -3, nz}, {-inf, -2, 0},
		{0, 3, 0}, {0, -3, inf}, {nz, 3, nz}, {nz, 2, 0}, {nz, -3, -inf}, {nz, -2, inf}, {-8, 1.0 / 3, nan}, {-2, 3, -8}, {-2, 2, 4}, {3, nan, nan}, {0, 0, 1},
	} {
		got := call("pow", num(c.b), num(c.e))
		assert.True(t, sameNumber(c.want, got), "pow(%v, %v) = %v want %v", c.b, c.e, got, c.want)
	}
	assert.Equal(t, 8.0, call("pow", str("2"), str("3")))
	assert.True(t, math.IsNaN(call("pow", num(2))))

	// atan2 special cases (y, x order).
	for _, c := range []struct{ y, x, want float64 }{
		{1, 1, math.Pi / 4}, {0, 0, 0}, {nz, 0, nz}, {0, nz, math.Pi}, {nz, nz, -math.Pi}, {1, 0, math.Pi / 2}, {-1, 0, -math.Pi / 2},
		{0, -1, math.Pi}, {nz, -1, -math.Pi}, {inf, inf, math.Pi / 4}, {-inf, inf, -math.Pi / 4}, {inf, -inf, 3 * math.Pi / 4}, {1, nan, nan},
		{nan, 1, nan}, {1, inf, 0}, {-1, inf, nz}, {1, -inf, math.Pi}, {-1, -inf, -math.Pi},
	} {
		got := call("atan2", num(c.y), num(c.x))
		assert.True(t, sameNumber(c.want, got), "atan2(%v, %v) = %v want %v", c.y, c.x, got, c.want)
	}
	var log []string
	call("atan2", objWithValueOf(r, IntValue(1), "y", &log), objWithValueOf(r, IntValue(1), "x", &log))
	assert.Equal(t, []string{"y", "x"}, log)

	// hypot.
	for _, c := range []struct {
		args []float64
		want float64
	}{
		{nil, 0}, {[]float64{3, 4}, 5}, {[]float64{-3, 4}, 5}, {[]float64{3, 4, 12}, 13}, {[]float64{5}, 5}, {[]float64{-5}, 5},
		{[]float64{0, nz}, 0}, {[]float64{nz}, 0}, {[]float64{nan, inf}, inf}, {[]float64{inf, nan}, inf}, {[]float64{-inf, 1}, inf},
		{[]float64{nan, 1}, nan}, {[]float64{1e200, 1e200}, 1.414213562373095e200}, {[]float64{1e-200, 1e-200}, 1.414213562373095e-200},
		{[]float64{5, 12}, 13}, {[]float64{8, 15}, 17}, {[]float64{2, 3, 6}, 7},
	} {
		args := make([]Value, len(c.args))
		for i, a := range c.args {
			args[i] = num(a)
		}
		got := call("hypot", args...)
		assert.True(t, sameNumber(c.want, got), "hypot(%v) = %v want %v", c.args, got, c.want)
	}
	log = nil
	assert.Equal(t, posInf, call("hypot", objWithValueOf(r, NumberValue(posInf), "a", &log), objWithValueOf(r, NaN(), "b", &log)))
	assert.Equal(t, []string{"a", "b"}, log, "all arguments are coerced even after an infinity")
	assert.Equal(t, 5.0, call("hypot", str("3"), str("4")))

	// imul.
	for _, c := range []struct{ a, b, want float64 }{
		{3, 4, 12}, {-5, 12, -60}, {0xffffffff, 5, -5}, {2147483647, 2, -2}, {0xfffffffe, 5, -10}, {1.9, 2.9, 2}, {nan, 3, 0}, {inf, 3, 0}, {65536, 65536, 0}, {0x7fffffff, 0x7fffffff, 1},
	} {
		assert.Equal(t, c.want, call("imul", num(c.a), num(c.b)), "imul(%v, %v)", c.a, c.b)
	}
	assert.Equal(t, 0.0, call("imul"))
	assert.Equal(t, 6.0, call("imul", str("2"), str("3")))
}

func TestMathRandom(t *testing.T) {
	r1 := NewRealm()
	r2 := NewRealmWith(RealmOptions{SharedIntrinsics: true})
	seen := map[float64]bool{}
	for _, r := range []*Realm{r1, r2} {
		for range 64 {
			v := jsNumber(t, jsCall(t, r, ObjectValue(r.Math), "random"))
			require.GreaterOrEqual(t, v, 0.0)
			require.Less(t, v, 1.0)
			seen[v] = true
		}
	}
	assert.Greater(t, len(seen), 100, "a fresh generator per realm yields distinct values")
	assert.NotNil(t, r1.lazy.rng)
	assert.NotNil(t, r2.lazy.rng)
	assert.NotSame(t, r1.lazy.rng, r2.lazy.rng)
	// Arguments are ignored.
	jsCall(t, r1, ObjectValue(r1.Math), "random", str("x"), IntValue(1))
}

func TestMathSumPrecise(t *testing.T) {
	accTable(t, [][2]string{
		{`return [Math.sumPrecise([1, 2, 3]), Math.sumPrecise([0.1, 0.2]), 0.1 + 0.2, Math.sumPrecise([1e30, 0.1, -1e30])];`,
			`[6,0.30000000000000004,0.30000000000000004,0.1]`},
		// Intermediate sums beyond the double range stay exact.
		{`return [Math.sumPrecise([1e308, 1e308, -1e308]), Math.sumPrecise([1.7976931348623157e308, 1.7976931348623157e308, -1.7976931348623157e308, 1])];`,
			`[1e+308,1.7976931348623157e+308]`},
		// Rounding at the top of the range: half an ulp past MAX_VALUE is Infinity.
		{`var max = Number.MAX_VALUE, halfUlp = Math.pow(2, 970);
		  return [Math.sumPrecise([max, halfUlp]), Math.sumPrecise([max, halfUlp, -Number.MIN_VALUE]), Math.sumPrecise([max, max, -max])].map(String);`,
			`["Infinity","1.7976931348623157e+308","1.7976931348623157e+308"]`},
		// Ties to even and subnormals.
		{`return [Math.sumPrecise([1, Math.pow(2, -53)]), Math.sumPrecise([1, Math.pow(2, -53), Math.pow(2, -1074)]),
		    Math.sumPrecise([Number.MIN_VALUE, Number.MIN_VALUE]) === 2 * Number.MIN_VALUE, Math.sumPrecise([Number.MIN_VALUE, -Number.MIN_VALUE])];`,
			`[1,1.0000000000000002,true,0]`},
		{`return [Object.is(Math.sumPrecise([]), -0), Object.is(Math.sumPrecise([-0, -0]), -0), Object.is(Math.sumPrecise([-0, 0]), 0),
		    Object.is(Math.sumPrecise([1, -1]), 0), Object.is(Math.sumPrecise([-1, 1, -0]), 0)];`, `[true,true,true,true,true]`},
		{`return [Math.sumPrecise([Infinity, 1]), Math.sumPrecise([-Infinity, 1e308, 1e308]), Math.sumPrecise([Infinity, -Infinity]),
		    Math.sumPrecise([NaN, 1]), Math.sumPrecise([1e308, 1e308]), Math.sumPrecise([-1e308, -1e308])].map(String);`,
			`["Infinity","-Infinity","NaN","NaN","Infinity","-Infinity"]`},
		// No coercion, and a non-Number is an error even after NaN.
		{`var log = []; var o = {valueOf: function () { log.push("valueOf"); return 1; }};
		  return [[o], [NaN, "1"], [Infinity, -Infinity, null], [1, 1n]].map(function (a) {
		    try { return Math.sumPrecise(a); } catch (e) { return e.name; } }).concat(log);`,
			`["TypeError","TypeError","TypeError","TypeError"]`},
		{`return [undefined, null, 1, {}, "12", "", true].map(function (a) {
		    try { return Math.sumPrecise(a); } catch (e) { return e.name; } });`,
			`["TypeError","TypeError","TypeError","TypeError","TypeError",0,"TypeError"]`},
		{`return [Math.sumPrecise([1, 2].values()), Math.sumPrecise([1, 2, 3], "ignored")];`, `[3,6]`},
		// A hole reads as undefined, which is not a Number.
		{`try { Math.sumPrecise([, 1]); } catch (e) { return e.message; }`, `"Math.sumPrecise: undefined is not a number"`},
		{`try { Math.sumPrecise(); } catch (e) { return e.message; }`, `"undefined is not iterable"`},
		{`try { Math.sumPrecise([1, "x"]); } catch (e) { return e.message; }`, `"Math.sumPrecise: \"x\" is not a number"`},
		{`return [Math.sumPrecise.length, Math.sumPrecise.name, Object.keys(Math).indexOf("sumPrecise")];`, `[1,"sumPrecise",-1]`},
		{`try { new Math.sumPrecise([]); } catch (e) { return e.name; }`, `"TypeError"`},
	})
}
