package engine

import "math/big"

// BigInt.prototype's toString and valueOf exist from realm creation, so a
// method call on a bigint never falls through to Object.prototype. The
// BigInt constructor, BigInt.asIntN/asUintN and the prototype's constructor
// and toLocaleString are a late global: a mutable realm installs them when
// the program names BigInt or first reaches BigInt.prototype through a
// bigint (GetV, ToObject).

func installBigInt(r *Realm) {
	if r.BigIntPrototype != nil {
		r.installBuiltins(r.BigIntPrototype, bigintPrototypeMethods)
	}
}

var bigintPrototypeMethods = []builtinDef{
	{AtomToString, bigintProtoToString, 0},
	{AtomValueOf, bigintProtoValueOf, 0},
}

func init() { lateGlobal(StringKey(AtomBigInt), installBigIntGlobal) }

func installBigIntGlobal(r *Realm) {
	if r.BigIntPrototype == nil {
		// The shared template leaves %BigInt.prototype% to the BigInt group
		// (initPrototypes): build it as a mutable realm has it by now.
		p := r.newIntrinsic(ClassObject, r.ObjectPrototype, 5)
		r.installBuiltins(p, bigintPrototypeMethods)
		r.installToStringTag(p, AtomBigInt)
		r.BigIntPrototype = p
	}
	c := r.newConstructor(AtomBigInt, 1, bigintCall, bigintConstruct, r.BigIntPrototype)
	r.installBuiltins(c, bigintCtorMethods)
	r.installBuiltins(r.BigIntPrototype, bigintLatePrototypeMethods)
	r.bindGlobal(AtomBigInt, ObjectValue(c))
}

var bigintCtorMethods = []builtinDef{
	{AtomAsIntN, bigintAsIntN, 2},
	{AtomAsUintN, bigintAsUintN, 2},
}

var bigintLatePrototypeMethods = []builtinDef{
	{AtomToLocaleString, bigintProtoToLocaleString, 0},
}

// bigintCall implements BigInt(value): a number converts with
// NumberToBigInt, anything else with ToBigInt.
func bigintCall(r *Realm, this Value, args []Value) (Value, error) {
	p, err := r.ToPrimitive(Arg(args, 0), HintNumber)
	if err != nil {
		return Undefined(), err
	}
	var b *BigInt
	if p.IsNumber() {
		b, err = r.numberToBigInt(p.AsNumber())
	} else {
		b, err = r.ToBigInt(p)
	}
	if err != nil {
		return Undefined(), err
	}
	return BigIntValue(b), nil
}

// bigintConstruct rejects `new BigInt()`: BigInt is a constructor (it may
// appear in an extends clause) whose [[Construct]] always throws.
func bigintConstruct(r *Realm, args []Value, newTarget *Object) (Value, error) {
	return Undefined(), r.TypeError("BigInt is not a constructor")
}

// bigintAsN converts the arguments of BigInt.asIntN and BigInt.asUintN.
func bigintAsN(r *Realm, args []Value) (int64, *BigInt, error) {
	bits, err := r.ToIndex(Arg(args, 0))
	if err != nil {
		return 0, nil, err
	}
	x, err := r.ToBigInt(Arg(args, 1))
	return bits, x, err
}

// bigintAsIntN implements BigInt.asIntN(bits, bigint): bigint modulo
// 2^bits, as a signed value.
func bigintAsIntN(r *Realm, this Value, args []Value) (Value, error) {
	bits, x, err := bigintAsN(r, args)
	if err != nil {
		return Undefined(), err
	}
	switch {
	case bits == 0:
		return BigIntValue(NewBigIntFromInt64(0)), nil
	case int64(x.v.BitLen()) < bits: // -2^(bits-1) < x < 2^(bits-1)
		return BigIntValue(x), nil
	}
	// bits <= BitLen(x), so the modulus is no larger than x.
	z := &BigInt{}
	m := new(big.Int).Lsh(big.NewInt(1), uint(bits))
	z.v.And(&x.v, new(big.Int).Sub(m, big.NewInt(1)))
	if z.v.Bit(int(bits-1)) == 1 {
		z.v.Sub(&z.v, m)
	}
	return BigIntValue(z), nil
}

// bigintAsUintN implements BigInt.asUintN(bits, bigint): bigint modulo
// 2^bits.
func bigintAsUintN(r *Realm, this Value, args []Value) (Value, error) {
	bits, x, err := bigintAsN(r, args)
	if err != nil {
		return Undefined(), err
	}
	switch {
	case x.v.Sign() >= 0 && int64(x.v.BitLen()) <= bits:
		return BigIntValue(x), nil
	case bits > maxBigIntBits: // a negative x modulo 2^bits has about bits bits
		return Undefined(), errBigIntTooBig(r)
	}
	z := &BigInt{}
	m := new(big.Int).Lsh(big.NewInt(1), uint(bits))
	z.v.And(&x.v, m.Sub(m, big.NewInt(1)))
	return BigIntValue(z), nil
}

// thisBigIntValue implements ThisBigIntValue: a bigint or a BigInt wrapper
// object.
func thisBigIntValue(r *Realm, this Value, method string) (*BigInt, error) {
	if this.IsBigInt() {
		return this.AsBigInt(), nil
	}
	if this.IsObject() && this.AsObject().class == ClassBigInt {
		pv, _ := this.AsObject().PrimitiveValue()
		return pv.AsBigInt(), nil
	}
	return nil, r.TypeError("BigInt.prototype.%s requires that 'this' be a BigInt", method)
}

// bigintProtoToString implements BigInt.prototype.toString(radix).
func bigintProtoToString(r *Realm, this Value, args []Value) (Value, error) {
	b, err := thisBigIntValue(r, this, "toString")
	if err != nil {
		return Undefined(), err
	}
	radix := 10.0
	if rv := Arg(args, 0); !rv.IsUndefined() {
		if radix, err = r.ToIntegerOrInfinity(rv); err != nil {
			return Undefined(), err
		}
		if radix < 2 || radix > 36 {
			return Undefined(), r.RangeError("toString() radix must be between 2 and 36")
		}
	}
	if err := r.checkBigIntFormat(b); err != nil {
		return Undefined(), err
	}
	return StringValue(FromGoString(b.v.Text(int(radix)))), nil
}

// bigintProtoToLocaleString implements BigInt.prototype.toLocaleString
// without ECMA-402: the decimal form, as Number.prototype.toLocaleString.
func bigintProtoToLocaleString(r *Realm, this Value, args []Value) (Value, error) {
	b, err := thisBigIntValue(r, this, "toLocaleString")
	if err == nil {
		err = r.checkBigIntFormat(b)
	}
	if err != nil {
		return Undefined(), err
	}
	return StringValue(asciiString(b.ToString())), nil
}

// bigintProtoValueOf implements BigInt.prototype.valueOf.
func bigintProtoValueOf(r *Realm, this Value, args []Value) (Value, error) {
	b, err := thisBigIntValue(r, this, "valueOf")
	if err != nil {
		return Undefined(), err
	}
	return BigIntValue(b), nil
}
