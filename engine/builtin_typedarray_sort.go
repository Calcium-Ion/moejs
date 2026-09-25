package engine

import "slices"

// %TypedArray%.prototype.sort and toSorted (ECMA-262 §23.2.3.29, 33). The
// default order compares Numbers (BigInts) with -0 before +0 and NaN last:
// sortKey maps each element's bits to a key whose unsigned order is that
// order, so the 1- and 2-byte types sort by counting keys and the wider
// ones sort their keys, by radix past sortSmall of them. Every NaN becomes
// the canonical NaN, one of the encodings the spec lets an implementation
// write. A comparator sorts the element bits in a stable merge sort. The
// scratch space is at most twice the array's bytes.

// sortSmall is the element count up to which sorting keys in place beats
// counting or radix passes.
const sortSmall = 4096

// typedArraySort implements %TypedArray%.prototype.sort(comparefn).
func typedArraySort(r *Realm, this Value, args []Value) (Value, error) {
	cmp := Arg(args, 0)
	if !cmp.IsUndefined() && !IsCallable(cmp) {
		return Undefined(), r.TypeError("The comparison function must be either a function or undefined")
	}
	ta, n, err := r.validateTypedArray(this, "%TypedArray%.prototype.sort")
	if err != nil {
		return Undefined(), err
	}
	if err := r.sortTypedArray(ta, n, cmp); err != nil {
		return Undefined(), err
	}
	return this, nil
}

// typedArrayToSorted implements %TypedArray%.prototype.toSorted(comparefn):
// the comparator cannot reach the copy, so it is sorted in place.
func typedArrayToSorted(r *Realm, this Value, args []Value) (Value, error) {
	cmp := Arg(args, 0)
	if !cmp.IsUndefined() && !IsCallable(cmp) {
		return Undefined(), r.TypeError("The comparison function must be either a function or undefined")
	}
	ta, n, err := r.validateTypedArray(this, "%TypedArray%.prototype.toSorted")
	if err != nil {
		return Undefined(), err
	}
	o, nta, err := r.typedArrayCreateSameType(ta.kind, n)
	if err != nil {
		return Undefined(), err
	}
	if err := r.copyBytes(nta.bytes(0, n), ta.bytes(0, n)); err != nil {
		return Undefined(), err
	}
	if err := r.sortTypedArray(nta, n, cmp); err != nil {
		return Undefined(), err
	}
	return ObjectValue(o), nil
}

// sortTypedArray sorts the first n elements of ta, by cmp unless it is
// undefined.
func (r *Realm) sortTypedArray(ta *typedArray, n int, cmp Value) error {
	if !cmp.IsUndefined() {
		switch elemSize[ta.kind] {
		case 1:
			return sortTypedArrayWith[uint8](r, ta, n, cmp.AsObject())
		case 2:
			return sortTypedArrayWith[uint16](r, ta, n, cmp.AsObject())
		case 4:
			return sortTypedArrayWith[uint32](r, ta, n, cmp.AsObject())
		}
		return sortTypedArrayWith[uint64](r, ta, n, cmp.AsObject())
	}
	switch elemSize[ta.kind] {
	case 1:
		return r.countingSort(ta, n, 8)
	case 2:
		if n > sortSmall {
			return r.countingSort(ta, n, 16)
		}
		return sortTypedArrayKeys[uint16](r, ta, n, 16)
	case 4:
		return sortTypedArrayKeys[uint32](r, ta, n, 32)
	}
	return sortTypedArrayKeys[uint64](r, ta, n, 64)
}

// sortTypedArrayWith sorts the elements of ta by the comparator fn
// (SortCompare): the element bits, as T, in a stable merge sort. Only the
// indices still valid once the comparator ran are written back.
func sortTypedArrayWith[T uint8 | uint16 | uint32 | uint64](r *Realm, ta *typedArray, n int, fn *Object) error {
	vals := make([]T, n)
	for i := range vals {
		if err := interruptEvery(r, int64(i)); err != nil {
			return err
		}
		vals[i] = T(ta.raw(i))
	}
	kind := ta.kind
	argv := r.pushArgs(2)
	defer r.popArgs(2)
	var calls int64
	err := mergeSort(vals, make([]T, n), func(a, b T) (int, error) {
		if err := interruptEvery(r, calls); err != nil {
			return 0, err
		}
		calls++
		argv[0], argv[1] = rawToValue(kind, uint64(a)), rawToValue(kind, uint64(b))
		v, err := r.CallObject(fn, Undefined(), argv)
		if err != nil {
			return 0, err
		}
		f, err := r.ToNumber(v)
		switch {
		case f < 0:
			return -1, err
		case f > 0:
			return 1, err
		}
		return 0, err // also NaN
	})
	if err != nil {
		return err
	}
	for i := range min(n, ta.length()) {
		if err := interruptEvery(r, int64(i)); err != nil {
			return err
		}
		ta.setRaw(i, uint64(vals[i]))
	}
	return nil
}

// sortKey maps the bits u of an element of type t to its default-order
// key: the signed types flip the sign bit, the float types flip every bit
// of a negative value and set the sign bit of the others, and a NaN maps to
// the largest key, which no other value has.
func sortKey(t elemType, u uint64) uint64 {
	switch t {
	case elemInt8:
		return u ^ 0x80
	case elemInt16:
		return u ^ 0x8000
	case elemInt32:
		return u ^ 0x8000_0000
	case elemBigInt64:
		return u ^ 1<<63
	case elemFloat16:
		return floatSortKey(u, 16, 10)
	case elemFloat32:
		return floatSortKey(u, 32, 23)
	case elemFloat64:
		return floatSortKey(u, 64, 52)
	}
	return u
}

// floatSortKey is sortKey for a float of width bits with a mant-bit
// significand.
func floatSortKey(u uint64, width, mant uint) uint64 {
	all := uint64(1)<<(width-1)<<1 - 1 // width ones, 64 included
	sign := uint64(1) << (width - 1)
	if exp := (sign - 1) &^ (1<<mant - 1); u&exp == exp && u&(1<<mant-1) != 0 {
		return all
	}
	if u&sign != 0 {
		return ^u & all
	}
	return u | sign
}

// unsortKey inverts sortKey; the NaN key yields the canonical NaN.
func unsortKey(t elemType, k uint64) uint64 {
	switch t {
	case elemFloat16:
		return floatUnsortKey(k, 16, 0x7E00)
	case elemFloat32:
		return floatUnsortKey(k, 32, 0x7FC0_0000)
	case elemFloat64:
		return floatUnsortKey(k, 64, 0x7FF8_0000_0000_0000)
	}
	return sortKey(t, k) // an involution for the integer types
}

func floatUnsortKey(k uint64, width uint, nan uint64) uint64 {
	all := uint64(1)<<(width-1)<<1 - 1
	sign := uint64(1) << (width - 1)
	switch {
	case k == all:
		return nan
	case k&sign != 0:
		return k &^ sign
	}
	return ^k & all
}

// countingSort sorts the n elements of ta, of a type of 8 or 16 bits, by
// counting the elements of each key.
func (r *Realm) countingSort(ta *typedArray, n int, bits uint) error {
	counts := make([]uint32, 1<<bits)
	for i := range n {
		if err := interruptEvery(r, int64(i)); err != nil {
			return err
		}
		counts[sortKey(ta.kind, ta.raw(i))]++
	}
	i := 0
	for k, c := range counts {
		if c == 0 {
			continue
		}
		u := unsortKey(ta.kind, uint64(k))
		for range c {
			if err := interruptEvery(r, int64(i)); err != nil {
				return err
			}
			ta.setRaw(i, u)
			i++
		}
	}
	return nil
}

// sortTypedArrayKeys sorts the n elements of ta through their keys, of the
// given width.
func sortTypedArrayKeys[T uint16 | uint32 | uint64](r *Realm, ta *typedArray, n int, width uint) error {
	keys := make([]T, n)
	for i := range keys {
		if err := interruptEvery(r, int64(i)); err != nil {
			return err
		}
		keys[i] = T(sortKey(ta.kind, ta.raw(i)))
	}
	if n <= sortSmall {
		slices.Sort(keys)
	} else if err := radixSort(r, keys, width); err != nil {
		return err
	}
	for i, k := range keys {
		if err := interruptEvery(r, int64(i)); err != nil {
			return err
		}
		ta.setRaw(i, unsortKey(ta.kind, uint64(k)))
	}
	return nil
}

// radixSort sorts keys of the given width by their bytes, least significant
// first, skipping a byte every key shares.
func radixSort[T uint16 | uint32 | uint64](r *Realm, keys []T, width uint) error {
	src, dst := keys, make([]T, len(keys))
	var tick int64
	for shift := uint(0); shift < width; shift += 8 {
		var counts [256]int
		for _, k := range src {
			if err := interruptEvery(r, tick); err != nil {
				return err
			}
			tick++
			counts[byte(k>>shift)]++
		}
		if counts[byte(src[0]>>shift)] == len(src) {
			continue
		}
		pos := 0
		for b, c := range counts {
			counts[b] = pos
			pos += c
		}
		for _, k := range src {
			if err := interruptEvery(r, tick); err != nil {
				return err
			}
			tick++
			b := byte(k >> shift)
			dst[counts[b]] = k
			counts[b]++
		}
		src, dst = dst, src
	}
	if &src[0] != &keys[0] {
		copy(keys, src)
	}
	return nil
}
