package engine

import (
	"math"
	"unsafe"
)

// GC safety of Value.
//
// Value is a 16-byte struct {ptr unsafe.Pointer; bits uint64}. The garbage
// collector treats ptr as a pointer word, so ptr MUST always hold either nil
// or a real, live Go pointer obtained from a pointer conversion (*String,
// *Object, *Symbol, *BigInt, *Accessor). Integers, tagged pointers or
// pointer arithmetic are never stored in ptr; every tag lives in bits. With
// that invariant the GC always sees a valid pointer word and this
// representation is exactly as safe as a plain interface value.
//
// This file is the only place in the package that names unsafe.Pointer for
// values. All other files use the constructors and accessors below.

// Value is the engine's universal JavaScript value. Numbers, booleans,
// undefined, null and the internal hole marker are stored inline (ptr == nil);
// strings, objects, symbols and bigints are pointers with a type tag in bits.
// The zero Value is the number +0.
type Value struct {
	ptr  unsafe.Pointer
	bits uint64
}

// Reserved NaN payloads (ptr == nil). The top 16 bits 0x7FFA select the
// reserved space; a canonical NaN uses 0x7FF8 so the two never collide.
const (
	reservedPrefix = uint64(0x7FFA) << 48
	reservedMask   = uint64(0xFFFF) << 48

	bitsFalse     = reservedPrefix | 0
	bitsTrue      = reservedPrefix | 1
	bitsUndefined = reservedPrefix | 2
	bitsNull      = reservedPrefix | 3
	bitsHole      = reservedPrefix | 4

	// canonicalNaN is math.NaN()'s bit pattern; NumberValue folds every NaN
	// to it so a NaN can never alias a reserved payload.
	canonicalNaN = uint64(0x7FF8000000000001)
)

// Pointer tags (ptr != nil).
const (
	tagString uint64 = iota + 1
	tagObject
	tagSymbol
	tagBigInt
	// tagAccessor is internal: it only ever appears inside property storage
	// (object slots and dictionary entries) whose attribute bits carry
	// attrAccessor, and never escapes to JavaScript code.
	tagAccessor
	// tagPrivate is internal: a *PrivateName, held by the registers and
	// environment slots of class code and used as a property key (private.go).
	tagPrivate
)

// NumberValue boxes a float64, canonicalizing NaN.
func NumberValue(f float64) Value {
	bits := math.Float64bits(f)
	if bits&0x7FF0000000000000 == 0x7FF0000000000000 && bits&0x000FFFFFFFFFFFFF != 0 {
		bits = canonicalNaN
	}
	return Value{bits: bits}
}

// IntValue boxes an integer as a JavaScript number.
func IntValue(i int) Value { return Value{bits: math.Float64bits(float64(i))} }

// Int64Value boxes an int64 as a JavaScript number (may lose precision above 2^53).
func Int64Value(i int64) Value { return Value{bits: math.Float64bits(float64(i))} }

// Uint32Value boxes a uint32 as a JavaScript number.
func Uint32Value(i uint32) Value { return Value{bits: math.Float64bits(float64(i))} }

// Bool boxes a boolean.
func Bool(b bool) Value {
	if b {
		return Value{bits: bitsTrue}
	}
	return Value{bits: bitsFalse}
}

// Undefined returns the undefined value.
func Undefined() Value { return Value{bits: bitsUndefined} }

// Null returns the null value.
func Null() Value { return Value{bits: bitsNull} }

// True returns the boolean true.
func True() Value { return Value{bits: bitsTrue} }

// False returns the boolean false.
func False() Value { return Value{bits: bitsFalse} }

// NaN returns the canonical NaN number.
func NaN() Value { return Value{bits: canonicalNaN} }

// Hole returns the internal array-hole / uninitialized-binding marker. It
// must never be observable from JavaScript.
func Hole() Value { return Value{bits: bitsHole} }

// StringValue boxes a string. s must not be nil.
func StringValue(s *String) Value { return Value{ptr: unsafe.Pointer(s), bits: tagString} }

// ObjectValue boxes an object. o must not be nil.
func ObjectValue(o *Object) Value { return Value{ptr: unsafe.Pointer(o), bits: tagObject} }

// SymbolValue boxes a symbol. s must not be nil.
func SymbolValue(s *Symbol) Value { return Value{ptr: unsafe.Pointer(s), bits: tagSymbol} }

// BigIntValue boxes a BigInt. b must not be nil.
func BigIntValue(b *BigInt) Value { return Value{ptr: unsafe.Pointer(b), bits: tagBigInt} }

// accessorValue boxes an accessor pair for property storage (internal).
func accessorValue(a *Accessor) Value { return Value{ptr: unsafe.Pointer(a), bits: tagAccessor} }

// privateValue boxes a private name (internal).
func privateValue(p *PrivateName) Value { return Value{ptr: unsafe.Pointer(p), bits: tagPrivate} }

// IsNumber reports whether v is a JavaScript number.
func (v Value) IsNumber() bool { return v.ptr == nil && v.bits&reservedMask != reservedPrefix }

// IsUndefined reports whether v is undefined.
func (v Value) IsUndefined() bool { return v.ptr == nil && v.bits == bitsUndefined }

// IsNull reports whether v is null.
func (v Value) IsNull() bool { return v.ptr == nil && v.bits == bitsNull }

// IsNullish reports whether v is undefined or null.
func (v Value) IsNullish() bool {
	return v.ptr == nil && (v.bits == bitsUndefined || v.bits == bitsNull)
}

// IsBool reports whether v is a boolean.
func (v Value) IsBool() bool { return v.ptr == nil && (v.bits == bitsTrue || v.bits == bitsFalse) }

// IsTrue reports whether v is the boolean true.
func (v Value) IsTrue() bool { return v.ptr == nil && v.bits == bitsTrue }

// IsFalse reports whether v is the boolean false.
func (v Value) IsFalse() bool { return v.ptr == nil && v.bits == bitsFalse }

// IsHole reports whether v is the internal hole marker.
func (v Value) IsHole() bool { return v.ptr == nil && v.bits == bitsHole }

// IsString reports whether v is a string.
func (v Value) IsString() bool { return v.ptr != nil && v.bits == tagString }

// IsObject reports whether v is an object.
func (v Value) IsObject() bool { return v.ptr != nil && v.bits == tagObject }

// IsSymbol reports whether v is a symbol.
func (v Value) IsSymbol() bool { return v.ptr != nil && v.bits == tagSymbol }

// IsBigInt reports whether v is a bigint.
func (v Value) IsBigInt() bool { return v.ptr != nil && v.bits == tagBigInt }

// IsPrimitive reports whether v is not an object (hole included).
func (v Value) IsPrimitive() bool { return v.ptr == nil || v.bits != tagObject }

func (v Value) isAccessor() bool { return v.ptr != nil && v.bits == tagAccessor }

func (v Value) isPrivate() bool { return v.ptr != nil && v.bits == tagPrivate }

// asPrivate returns the *PrivateName payload. Only valid when isPrivate.
func (v Value) asPrivate() *PrivateName { return (*PrivateName)(v.ptr) }

// AsNumber returns the float64 payload. Only valid when IsNumber.
func (v Value) AsNumber() float64 { return math.Float64frombits(v.bits) }

// AsBool returns the boolean payload. Only valid when IsBool.
func (v Value) AsBool() bool { return v.bits == bitsTrue }

// AsString returns the *String payload. Only valid when IsString.
func (v Value) AsString() *String { return (*String)(v.ptr) }

// AsObject returns the *Object payload. Only valid when IsObject.
func (v Value) AsObject() *Object { return (*Object)(v.ptr) }

// AsSymbol returns the *Symbol payload. Only valid when IsSymbol.
func (v Value) AsSymbol() *Symbol { return (*Symbol)(v.ptr) }

// AsBigInt returns the *BigInt payload. Only valid when IsBigInt.
func (v Value) AsBigInt() *BigInt { return (*BigInt)(v.ptr) }

func (v Value) asAccessor() *Accessor { return (*Accessor)(v.ptr) }

// Bits exposes the raw payload for hashing and debugging.
func (v Value) Bits() uint64 { return v.bits }

// ptrEqual reports whether two pointer-kind values share the same pointer.
func (v Value) ptrEqual(w Value) bool { return v.ptr == w.ptr }

// identityBits returns the address of a pointer-kind value's payload as an
// integer, for identity hashing (the keyed collections). The collector does
// not move heap objects, so the address is stable for the payload's
// lifetime; the integer is never converted back to a pointer.
func (v Value) identityBits() uint64 { return uint64(uintptr(v.ptr)) }
