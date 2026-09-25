package engine

// Atomics (ECMA-262 §25.4): read-modify-write operations on the elements of
// integer typed arrays. The engine runs one agent per realm and shares no
// buffer across goroutines, so every operation is a plain access and the
// agent can never block: Atomics.wait validates its arguments, then throws
// ([[CanBlock]] is false), notify wakes nobody, and waitAsync is absent.

// atomicsTable builds the Atomics namespace (binaryTables).
func atomicsTable(a *binaryAtoms) *keyTable {
	return newKeyTable(nil, []builtinDef{
		{AtomAdd, atomicsRMW("Atomics.add", func(old, v uint64) uint64 { return old + v }), 3},
		{a.and, atomicsRMW("Atomics.and", func(old, v uint64) uint64 { return old & v }), 3},
		{a.compareExchange, atomicsCompareExchange, 4},
		{a.exchange, atomicsRMW("Atomics.exchange", func(old, v uint64) uint64 { return v }), 3},
		{a.isLockFree, atomicsIsLockFree, 1},
		{a.load, atomicsLoad, 2},
		{a.notify, atomicsNotify, 3},
		{a.or, atomicsRMW("Atomics.or", func(old, v uint64) uint64 { return old | v }), 3},
		{a.store, atomicsStore, 3},
		{AtomSub, atomicsRMW("Atomics.sub", func(old, v uint64) uint64 { return old - v }), 3},
		{a.wait, atomicsWait, 4},
		{a.xor, atomicsRMW("Atomics.xor", func(old, v uint64) uint64 { return old ^ v }), 3},
	}, AtomAtomics)
}

// validateIntegerTypedArray implements ValidateIntegerTypedArray(v,
// waitable): a typed array in bounds of an integer type other than
// Uint8Clamped, Int32 or BigInt64 when waitable.
func (r *Realm) validateIntegerTypedArray(v Value, waitable bool, method string) (*typedArray, int, error) {
	ta, n, err := r.validateTypedArray(v, method)
	if err != nil {
		return nil, 0, err
	}
	if waitable {
		if ta.kind != elemInt32 && ta.kind != elemBigInt64 {
			return nil, 0, r.TypeError("%s: the typed array must be an Int32Array or a BigInt64Array", method)
		}
	} else if ta.kind == elemUint8Clamped || ta.kind == elemFloat16 || ta.kind == elemFloat32 || ta.kind == elemFloat64 {
		return nil, 0, r.TypeError("%s: the typed array must be of an integer type other than Uint8Clamped", method)
	}
	return ta, n, nil
}

// validateAtomicAccess implements ValidateAtomicAccess: the element index
// ToIndex(index) must be below the length n.
func (r *Realm) validateAtomicAccess(n int, index Value) (int, error) {
	i, err := r.ToIndex(index)
	if err != nil {
		return 0, err
	}
	if i >= int64(n) {
		return 0, r.RangeError("Invalid atomic access index")
	}
	return int(i), nil
}

// atomicAccess implements ValidateAtomicAccessOnIntegerTypedArray(v, index).
func (r *Realm) atomicAccess(v, index Value, method string) (*typedArray, int, error) {
	ta, n, err := r.validateIntegerTypedArray(v, false, method)
	if err != nil {
		return nil, 0, err
	}
	i, err := r.validateAtomicAccess(n, index)
	return ta, i, err
}

// revalidateAtomicAccess implements RevalidateAtomicAccess after the value
// coercions ran: the array must still be in bounds and hold element i.
func (r *Realm) revalidateAtomicAccess(ta *typedArray, i int, method string) error {
	n := ta.length()
	if n < 0 {
		return outOfBoundsTypedArray(r, method)
	}
	if i >= n {
		return r.RangeError("Invalid atomic access index")
	}
	return nil
}

// atomicValue converts v for an element of ta's type: ToBigInt, or
// 𝔽(ToIntegerOrInfinity(v)). It returns the converted value and its bits.
func (r *Realm) atomicValue(ta *typedArray, v Value) (Value, uint64, error) {
	if ta.kind.isBigInt() {
		b, err := r.ToBigInt(v)
		if err != nil {
			return Undefined(), 0, err
		}
		return BigIntValue(b), b.Uint64(), nil
	}
	f, err := r.ToIntegerOrInfinity(v)
	if err != nil {
		return Undefined(), 0, err
	}
	return NumberValue(f), numberToRaw(ta.kind, f), nil
}

// atomicsRMW returns the Atomics method that stores op(old, v) in the element
// and returns the old value (AtomicReadModifyWrite). op works on the bits;
// setRaw keeps the element's width of the result, the wrap-around the
// integer types specify.
func atomicsRMW(method string, op func(old, v uint64) uint64) NativeFunc {
	return func(r *Realm, this Value, args []Value) (Value, error) {
		ta, i, err := r.atomicAccess(Arg(args, 0), Arg(args, 1), method)
		if err != nil {
			return Undefined(), err
		}
		_, u, err := r.atomicValue(ta, Arg(args, 2))
		if err != nil {
			return Undefined(), err
		}
		if err := r.revalidateAtomicAccess(ta, i, method); err != nil {
			return Undefined(), err
		}
		old := ta.raw(i)
		ta.setRaw(i, op(old, u))
		return rawToValue(ta.kind, old), nil
	}
}

// atomicsCompareExchange implements Atomics.compareExchange(typedArray,
// index, expectedValue, replacementValue): the element bits are compared
// with the expected value's at the element's width.
func atomicsCompareExchange(r *Realm, this Value, args []Value) (Value, error) {
	const method = "Atomics.compareExchange"
	ta, i, err := r.atomicAccess(Arg(args, 0), Arg(args, 1), method)
	if err != nil {
		return Undefined(), err
	}
	_, expected, err := r.atomicValue(ta, Arg(args, 2))
	if err != nil {
		return Undefined(), err
	}
	_, replacement, err := r.atomicValue(ta, Arg(args, 3))
	if err != nil {
		return Undefined(), err
	}
	if err := r.revalidateAtomicAccess(ta, i, method); err != nil {
		return Undefined(), err
	}
	if size := elemSize[ta.kind]; size < 8 {
		expected &= 1<<(8*size) - 1
	}
	old := ta.raw(i)
	if old == expected {
		ta.setRaw(i, replacement)
	}
	return rawToValue(ta.kind, old), nil
}

// atomicsLoad implements Atomics.load(typedArray, index).
func atomicsLoad(r *Realm, this Value, args []Value) (Value, error) {
	const method = "Atomics.load"
	ta, i, err := r.atomicAccess(Arg(args, 0), Arg(args, 1), method)
	if err != nil {
		return Undefined(), err
	}
	if err := r.revalidateAtomicAccess(ta, i, method); err != nil {
		return Undefined(), err
	}
	return ta.at(i), nil
}

// atomicsStore implements Atomics.store(typedArray, index, value): it
// returns the converted value, not the stored bits.
func atomicsStore(r *Realm, this Value, args []Value) (Value, error) {
	const method = "Atomics.store"
	ta, i, err := r.atomicAccess(Arg(args, 0), Arg(args, 1), method)
	if err != nil {
		return Undefined(), err
	}
	v, u, err := r.atomicValue(ta, Arg(args, 2))
	if err != nil {
		return Undefined(), err
	}
	if err := r.revalidateAtomicAccess(ta, i, method); err != nil {
		return Undefined(), err
	}
	ta.setRaw(i, u)
	return v, nil
}

// atomicsIsLockFree implements Atomics.isLockFree(size): every element size
// is.
func atomicsIsLockFree(r *Realm, this Value, args []Value) (Value, error) {
	n, err := r.ToIntegerOrInfinity(Arg(args, 0))
	if err != nil {
		return Undefined(), err
	}
	return Bool(n == 1 || n == 2 || n == 4 || n == 8), nil
}

// atomicsWait implements Atomics.wait(typedArray, index, value, timeout)
// (DoWait) for an agent whose [[CanBlock]] is false: the validation and
// coercion steps run, then it throws.
func atomicsWait(r *Realm, this Value, args []Value) (Value, error) {
	const method = "Atomics.wait"
	ta, n, err := r.validateIntegerTypedArray(Arg(args, 0), true, method)
	if err != nil {
		return Undefined(), err
	}
	if ta.view.buf.class != ClassSharedArrayBuffer {
		return Undefined(), r.TypeError("%s: the typed array must view a SharedArrayBuffer", method)
	}
	if _, err := r.validateAtomicAccess(n, Arg(args, 1)); err != nil {
		return Undefined(), err
	}
	if ta.kind == elemBigInt64 {
		_, err = r.ToBigInt64(Arg(args, 2))
	} else {
		_, err = r.ToInt32(Arg(args, 2))
	}
	if err != nil {
		return Undefined(), err
	}
	if _, err := r.ToNumber(Arg(args, 3)); err != nil {
		return Undefined(), err
	}
	return Undefined(), r.TypeError("Atomics.wait cannot be called in this context")
}

// atomicsNotify implements Atomics.notify(typedArray, index, count): no
// agent ever waits, so it wakes none.
func atomicsNotify(r *Realm, this Value, args []Value) (Value, error) {
	_, n, err := r.validateIntegerTypedArray(Arg(args, 0), true, "Atomics.notify")
	if err != nil {
		return Undefined(), err
	}
	if _, err := r.validateAtomicAccess(n, Arg(args, 1)); err != nil {
		return Undefined(), err
	}
	if c := Arg(args, 2); !c.IsUndefined() {
		if _, err := r.ToIntegerOrInfinity(c); err != nil {
			return Undefined(), err
		}
	}
	return IntValue(0), nil
}
