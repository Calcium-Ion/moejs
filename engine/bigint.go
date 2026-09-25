package engine

import (
	"math"
	"math/big"
	"math/bits"
)

// BigInt is a JavaScript BigInt value: an immutable arbitrary-precision
// integer. The operators live in bigint_ops.go and the BigInt builtins in
// builtin_bigint.go; math/big runs only when one of them does.
type BigInt struct {
	v big.Int
}

// maxBigIntBits bounds the magnitude of every BigInt (SpiderMonkey's limit,
// 2^20 bits): an operation whose result would be larger throws a RangeError,
// so no single math/big call runs long enough to need an interrupt check.
// The lexer rejects a larger literal (syntax/lexer.go keeps the same bound).
const maxBigIntBits = 1 << 20

// Converting a BigInt to a string or back takes microseconds past these
// sizes and tens of milliseconds at the size limit (decimal parsing is
// quadratic), while a native loop such as forEach checks the interrupt only
// every interruptStride calls of its callback: conversions of a BigInt of
// more than bigintSlowWords words, or of a string of more than
// bigintSlowChars characters, check it first.
const (
	bigintSlowWords = 4096 / bits.UintSize
	bigintSlowChars = 1024
)

// bigintDisplayBits bounds the BigInts Value.String shows, and so the error
// messages the engine builds, which check no interrupt: a larger one reads
// "<a very large BigInt>", as V8's messages show a BigInt of more than 100
// 64-bit digits (NoSideEffectsToString). A native loop such as
// arr.map(Promise.all, Promise) builds one message per callback.
const bigintDisplayBits = 100 * 64

// checkBigIntFormat checks the interrupt before b is converted to a string
// when b is large.
func (r *Realm) checkBigIntFormat(b *BigInt) error {
	if len(b.v.Bits()) <= bigintSlowWords {
		return nil
	}
	return r.CheckInterrupt()
}

// checkBigIntParse checks the interrupt before s is parsed as a BigInt when
// s is long.
func (r *Realm) checkBigIntParse(s *String) error {
	if s.Len() <= bigintSlowChars {
		return nil
	}
	return r.CheckInterrupt()
}

// NewBigIntFromDecimal parses decimal text such as the digits of a `123n`
// literal (no sign, no suffix). ok is false on malformed input.
func NewBigIntFromDecimal(text string) (*BigInt, bool) {
	b := &BigInt{}
	if _, ok := b.v.SetString(text, 10); !ok {
		return nil, false
	}
	return b, true
}

// newBigIntConst parses the text of a BigInt constant: decimal digits, or
// hex digits after "0x", which a large 0x, 0o or 0b literal keeps so that it
// converts in linear time.
func newBigIntConst(text string) (*BigInt, bool) {
	if len(text) > 2 && text[:2] == "0x" {
		b := &BigInt{}
		if _, ok := b.v.SetString(text[2:], 16); !ok {
			return nil, false
		}
		return b, true
	}
	return NewBigIntFromDecimal(text)
}

// NewBigIntFromInt64 wraps an int64.
func NewBigIntFromInt64(i int64) *BigInt {
	b := &BigInt{}
	b.v.SetInt64(i)
	return b
}

// NewBigIntFromUint64 wraps a uint64.
func NewBigIntFromUint64(u uint64) *BigInt {
	b := &BigInt{}
	b.v.SetUint64(u)
	return b
}

// NewBigIntFromBig copies x (for host import). ok is false when x exceeds
// the BigInt size limit (2^20 bits).
func NewBigIntFromBig(x *big.Int) (*BigInt, bool) {
	if x.BitLen() > maxBigIntBits {
		return nil, false
	}
	b := &BigInt{}
	b.v.Set(x)
	return b, true
}

// ToString returns the decimal representation without the `n` suffix.
func (b *BigInt) ToString() string { return b.v.String() }

// displayString is b as Value.String shows it: the decimal digits and the
// `n` suffix, or "<a very large BigInt>" past bigintDisplayBits.
func (b *BigInt) displayString() string {
	if b.v.BitLen() > bigintDisplayBits {
		return "<a very large BigInt>"
	}
	return b.v.String() + "n"
}

// StrictEquals reports mathematical equality.
func (b *BigInt) StrictEquals(o *BigInt) bool { return b == o || b.v.Cmp(&o.v) == 0 }

// IsZero reports whether the value is 0n (used by ToBoolean).
func (b *BigInt) IsZero() bool { return b.v.Sign() == 0 }

// Big returns a copy of the underlying math/big value (for host export).
func (b *BigInt) Big() *big.Int { return new(big.Int).Set(&b.v) }

// Uint64 returns b modulo 2^64, the value ToBigUint64 stores.
func (b *BigInt) Uint64() uint64 {
	var u uint64
	w := b.v.Bits() // the magnitude, least significant word first
	if bits.UintSize == 64 {
		if len(w) > 0 {
			u = uint64(w[0])
		}
	} else {
		for i := range min(len(w), 2) {
			u |= uint64(w[i]) << (32 * i)
		}
	}
	if b.v.Sign() < 0 {
		u = -u
	}
	return u
}

// Int64 returns b modulo 2^64 as a two's complement int64, the value
// ToBigInt64 stores.
func (b *BigInt) Int64() int64 { return int64(b.Uint64()) }

// Float64 returns the number nearest b (Number(bigint)).
func (b *BigInt) Float64() float64 {
	f, _ := b.v.Float64()
	return f
}

// ToBigInt implements ToBigInt: a bigint is returned as is, booleans and
// strings convert, and undefined, null, numbers and symbols throw a
// TypeError. A string that is not an integer literal is a SyntaxError.
func (r *Realm) ToBigInt(v Value) (*BigInt, error) {
	p, err := r.ToPrimitive(v, HintNumber)
	if err != nil {
		return nil, err
	}
	switch p.Type() {
	case TypeBigInt:
		return p.AsBigInt(), nil
	case TypeBoolean:
		if p.AsBool() {
			return NewBigIntFromInt64(1), nil
		}
		return NewBigIntFromInt64(0), nil
	case TypeString:
		if err := r.checkBigIntParse(p.AsString()); err != nil {
			return nil, err
		}
		b, over, ok := parseBigInt(p.AsString())
		if !ok {
			return nil, r.SyntaxError("Cannot convert %s to a BigInt", p.String())
		}
		if over != 0 {
			return nil, errBigIntTooBig(r)
		}
		return b, nil
	}
	return nil, r.TypeError("Cannot convert %s to a BigInt", p.String())
}

// ToBigInt64 implements ToBigInt64: ToBigInt modulo 2^64, as a signed value.
func (r *Realm) ToBigInt64(v Value) (int64, error) {
	b, err := r.ToBigInt(v)
	if err != nil {
		return 0, err
	}
	return b.Int64(), nil
}

// ToBigUint64 implements ToBigUint64: ToBigInt modulo 2^64.
func (r *Realm) ToBigUint64(v Value) (uint64, error) {
	b, err := r.ToBigInt(v)
	if err != nil {
		return 0, err
	}
	return b.Uint64(), nil
}

// numberToBigInt implements NumberToBigInt: a non-integral number is a
// RangeError.
func (r *Realm) numberToBigInt(f float64) (*BigInt, error) {
	if f != math.Trunc(f) || math.IsInf(f, 0) {
		return nil, r.RangeError("The number %s cannot be converted to a BigInt because it is not an integer", NumberToGoString(f))
	}
	if math.Abs(f) < 1<<63 {
		return NewBigIntFromInt64(int64(f)), nil
	}
	b := &BigInt{}
	big.NewFloat(f).Int(&b.v)
	return b, nil
}

func errBigIntTooBig(r *Realm) error { return r.RangeError("Maximum BigInt size exceeded") }

// StringToBigInt implements StringToBigInt (decimal, hex, octal and binary
// literals; empty string is 0n). ok is false when the string is not a valid
// StringIntegerLiteral or its value exceeds the BigInt size limit.
func StringToBigInt(s *String) (*BigInt, bool) {
	b, over, ok := parseBigInt(s)
	return b, ok && over == 0
}

// parseBigInt is StringToBigInt with the size limit apart: a valid literal
// whose value has more than 2^20 bits gives b == nil and over, its sign. Such
// a literal is larger in magnitude than every BigInt, which is all a
// comparison needs, and parsing it would take time proportional to the
// square of its length.
func parseBigInt(s *String) (b *BigInt, over int, ok bool) {
	t := TrimJSWhitespace(s)
	a, ok := t.ASCII()
	if !ok {
		return nil, 0, false
	}
	b = &BigInt{}
	if a == "" {
		return b, 0, true
	}
	base, digits, sign := 10, a, 1
	if len(a) > 2 && a[0] == '0' {
		switch a[1] {
		case 'x', 'X':
			base = 16
		case 'o', 'O':
			base = 8
		case 'b', 'B':
			base = 2
		}
	}
	if base != 10 {
		digits = a[2:]
	} else if a[0] == '+' || a[0] == '-' {
		if a[0] == '-' {
			sign = -1
		}
		digits = a[1:]
	}
	if digits == "" {
		return nil, 0, false
	}
	for i := range len(digits) {
		if d := digitValue(digits[i]); d < 0 || d >= base {
			return nil, 0, false
		}
	}
	// A literal of n significant digits has more than (n-1)*log2(base) bits.
	n := len(digits)
	for n > 1 && digits[len(digits)-n] == '0' {
		n--
	}
	if float64(n-1)*math.Log2(float64(base)) >= maxBigIntBits {
		return nil, sign, true
	}
	if _, ok := b.v.SetString(digits, base); !ok {
		return nil, 0, false
	}
	if b.v.BitLen() > maxBigIntBits {
		return nil, sign, true
	}
	if sign < 0 {
		b.v.Neg(&b.v)
	}
	return b, 0, true
}
