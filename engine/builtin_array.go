package engine

import (
	"math"
	"slices"
)

// installArray fills the Array constructor and Array.prototype. Every method
// is generic over array-likes per spec and has a fast path for arrays whose
// dense storage is authoritative.
func installArray(r *Realm) {
	// The reservations include what later installers add: @@species
	// (installSpecies), @@iterator and @@unscopables (installArrayIterator).
	r.ArrayCtor.ReserveSlots(r, len(arrayStaticMethods)+1)
	r.installBuiltins(r.ArrayCtor, arrayStaticMethods)
	r.ArrayPrototype.ReserveSlots(r, len(arrayPrototypeMethods)+2)
	r.installBuiltins(r.ArrayPrototype, arrayPrototypeMethods)
}

var arrayStaticMethods = []builtinDef{
	{AtomFrom, arrayFrom, 1},
	{AtomFromAsync, arrayFromAsync, 1},
	{AtomIsArray, arrayIsArray, 1},
	{AtomOf, arrayOf, 0},
}

var arrayPrototypeMethods = []builtinDef{
	{AtomAt, arrayProtoAt, 1},
	{AtomConcat, arrayProtoConcat, 1},
	{AtomCopyWithin, arrayProtoCopyWithin, 2},
	{AtomEntries, arrayProtoEntries, 0},
	{AtomEvery, arrayProtoEvery, 1},
	{AtomFill, arrayProtoFill, 1},
	{AtomFilter, arrayProtoFilter, 1},
	{AtomFind, arrayProtoFind, 1},
	{AtomFindIndex, arrayProtoFindIndex, 1},
	{AtomFindLast, arrayProtoFindLast, 1},
	{AtomFindLastIndex, arrayProtoFindLastIndex, 1},
	{AtomFlat, arrayProtoFlat, 0},
	{AtomFlatMap, arrayProtoFlatMap, 1},
	{AtomForEach, arrayProtoForEach, 1},
	{AtomIncludes, arrayProtoIncludes, 1},
	{AtomIndexOf, arrayProtoIndexOf, 1},
	{AtomJoin, arrayProtoJoin, 1},
	{AtomKeys, arrayProtoKeys, 0},
	{AtomLastIndexOf, arrayProtoLastIndexOf, 1},
	{AtomMap, arrayProtoMap, 1},
	{AtomPop, arrayProtoPop, 0},
	{AtomPush, arrayProtoPush, 1},
	{AtomReduce, arrayProtoReduce, 1},
	{AtomReduceRight, arrayProtoReduceRight, 1},
	{AtomReverse, arrayProtoReverse, 0},
	{AtomShift, arrayProtoShift, 0},
	{AtomSlice, arrayProtoSlice, 2},
	{AtomSome, arrayProtoSome, 1},
	{AtomSort, arrayProtoSort, 1},
	{AtomSplice, arrayProtoSplice, 2},
	{AtomToReversed, arrayProtoToReversed, 0},
	{AtomToSorted, arrayProtoToSorted, 1},
	{AtomToLocaleString, arrayProtoToLocaleString, 0},
	{AtomToSpliced, arrayProtoToSpliced, 2},
	{AtomToString, arrayProtoToString, 0},
	{AtomUnshift, arrayProtoUnshift, 1},
	{AtomValues, arrayProtoValues, 0},
	{AtomWith, arrayProtoWith, 2},
}

// interruptStride is how many loop iterations a long-running native performs
// between interrupt checks.
const interruptStride = 4096

// interruptEvery checks the interrupt flag once per interruptStride steps.
func interruptEvery(r *Realm, k int64) error {
	if k&(interruptStride-1) != interruptStride-1 {
		return nil
	}
	return r.CheckInterrupt()
}

var commaString = asciiString(",")

// --- constructor ---------------------------------------------------------------

// arrayCall implements Array(...) called without new.
func arrayCall(r *Realm, this Value, args []Value) (Value, error) {
	return arrayConstruct(r, args, nil)
}

// arrayConstruct implements new Array(len) / new Array(...items).
func arrayConstruct(r *Realm, args []Value, newTarget *Object) (Value, error) {
	proto, err := r.GetPrototypeFromConstructor(newTarget, r.ArrayCtor, r.ArrayPrototype)
	if err != nil {
		return Undefined(), err
	}
	var arr *Object
	switch {
	case len(args) == 0:
		arr = r.NewArrayLen(0)
	case len(args) == 1 && args[0].IsNumber():
		n := args[0].AsNumber()
		u := ToUint32Float(n)
		if float64(u) != n {
			return Undefined(), r.RangeError("Invalid array length")
		}
		arr = r.NewArrayLen(u)
	default:
		items := make([]Value, len(args))
		copy(items, args)
		arr = r.NewArrayFromSlice(items)
	}
	if proto != r.ArrayPrototype {
		arr.SetPrototypeOf(r, proto)
	}
	return ObjectValue(arr), nil
}

// --- shared prologue and indexed access ------------------------------------------

// arrayLikeThis performs the `O = ToObject(this); len = LengthOfArrayLike(O)`
// prologue of every Array.prototype method.
func arrayLikeThis(r *Realm, this Value) (*Object, int64, error) {
	o, err := r.ToObject(this)
	if err != nil {
		return nil, 0, err
	}
	n, err := r.LengthOfArrayLike(o)
	return o, n, err
}

// indexKey returns the property key of a non-negative integer index; beyond
// the array-index range the key is the decimal string.
func indexKey(r *Realm, i int64) PropertyKey {
	if i <= maxArrayIndex {
		return IndexKey(uint32(i))
	}
	return StringKey(r.Intern(NumberToString(float64(i))))
}

// denseElement returns o's own dense element i when that read is exact
// (present, not a String wrapper index).
func denseElement(o *Object, i int64) (Value, bool) {
	if i < int64(len(o.elements)) && o.class != ClassString {
		if v := o.elements[i]; !v.IsHole() {
			return v, true
		}
	}
	return Value{}, false
}

// getIndex is Get(O, ToString(i)).
func getIndex(r *Realm, o *Object, i int64) (Value, error) {
	if v, ok := denseElement(o, i); ok {
		return v, nil
	}
	return o.Get(r, indexKey(r, i), ObjectValue(o))
}

// hasIndex is HasProperty(O, ToString(i)).
func hasIndex(r *Realm, o *Object, i int64) (bool, error) {
	if _, ok := denseElement(o, i); ok {
		return true, nil
	}
	return r.hasProperty(o, indexKey(r, i))
}

// setIndex is Set(O, ToString(i), v, true).
func setIndex(r *Realm, o *Object, i int64, v Value) error {
	k := indexKey(r, i)
	ok, err := o.Set(r, k, v, ObjectValue(o))
	if err != nil {
		return err
	}
	if !ok {
		return r.readOnlyError(o, k)
	}
	return nil
}

// deleteIndex is DeletePropertyOrThrow(O, ToString(i)).
func deleteIndex(r *Realm, o *Object, i int64) error {
	return o.DeletePropertyOrThrow(r, indexKey(r, i))
}

// setLength is Set(O, "length", n, true).
func setLength(r *Realm, o *Object, n int64) error {
	if o.class != ClassArray {
		return o.SetProp(r, lengthKey, Int64Value(n))
	}
	// OrdinarySet rejects any write to a non-writable length, the unchanged
	// value included (pop/shift of a frozen empty array, push() with no
	// items), before ArraySetLength looks at the value.
	if !o.internal.(*ArrayData).lengthWritable {
		return r.TypeError("Cannot assign to read only property 'length' of object")
	}
	if n > math.MaxUint32 {
		return r.RangeError("Invalid array length")
	}
	ok, err := o.SetLength(r, uint32(n))
	if err != nil {
		return err
	}
	if !ok {
		return r.TypeError("Cannot assign to read only property 'length' of object")
	}
	return nil
}

// plainArray reports whether o is an array whose dense storage is the whole
// truth about its indexed properties: storage covers the length, there is
// no sparse map, and nothing on the prototype chain has indexed properties
// (so a hole really reads as undefined and HasProperty is false). A host
// placeholder is materialized first: every caller is about to read the
// elements. So is one on the prototype chain, when it is what fails the
// check: a Go map has index keys or not only once its keys are properties,
// and the slow path, whose HasProperty, Get and Set of the indices a dense
// array owns never reach the prototype, would not materialize it.
func plainArray(o *Object) bool {
	if o.class != ClassArray {
		return false
	}
	o.materializeHost()
	if int(o.internal.(*ArrayData).length) != len(o.elements) || (o.dict != nil && len(o.dict.sparse) != 0) {
		return false
	}
	return indexFreeProtos(o) || materializeProtos(o) && plainArray(o)
}

// materializeProtos materializes the host placeholders on o's prototype
// chain and reports whether there were any.
func materializeProtos(o *Object) bool {
	found := false
	for p := o.proto; p != nil && p.class != ClassProxy; p = p.proto {
		if p.materializeHost() {
			found = true
		}
	}
	return found
}

// indexFreeProtos reports whether nothing on o's prototype chain has
// indexed properties, so an index o lacks reads as undefined and a Set of
// it meets no inherited setter or read-only property. A proxy may have any
// index through its traps, and the spec's HasProperty, Get and Set of one
// run them; a typed array's indices are its elements. A host placeholder's
// indices are not elements until it is materialized (see plainArray).
func indexFreeProtos(o *Object) bool {
	for p := o.proto; p != nil; p = p.proto {
		if len(p.elements) != 0 || p.class == ClassString || p.class == ClassProxy || p.class == ClassTypedArray || (p.dict != nil && len(p.dict.sparse) != 0) || p.flags&flagHasLazy != 0 && p.shape.key == hostSentinelKey {
			return false
		}
	}
	return true
}

// mutableArray reports whether o is a plainArray whose storage may be
// rewritten in place: extensible, not sealed, frozen or shared, writable
// length.
func mutableArray(o *Object) bool {
	return plainArray(o) && o.flags&(flagSealed|flagFrozen|flagShared|flagExtensible) == flagExtensible && o.internal.(*ArrayData).lengthWritable
}

// relativeIndex clamps ToIntegerOrInfinity(v) into [0, length], counting
// negative values from the end; dflt is used when v is undefined.
func relativeIndex(r *Realm, v Value, length, dflt int64) (int64, error) {
	if v.IsUndefined() {
		return dflt, nil
	}
	f, err := r.ToIntegerOrInfinity(v)
	if err != nil {
		return 0, err
	}
	switch {
	case f < 0:
		return max(int64(float64(length)+f), 0), nil // -∞ yields 0
	case f > float64(length):
		return length, nil
	}
	return int64(f), nil
}

// callbackArg returns args[0] as the callback object or the spec's
// TypeError.
func callbackArg(r *Realm, args []Value) (*Object, error) {
	fn := Arg(args, 0)
	if !IsCallable(fn) {
		return nil, r.TypeError("%s is not a function", r.DisplayString(fn))
	}
	return fn.AsObject(), nil
}

// --- static methods -----------------------------------------------------------------

// arrayIsArray implements Array.isArray.
func arrayIsArray(r *Realm, this Value, args []Value) (Value, error) {
	ok, err := r.isArray(Arg(args, 0))
	return Bool(ok), err
}

// arrayCreateFrom implements the `IsConstructor(C) ? Construct(C, args) :
// ArrayCreate(len)` step of Array.from and Array.of. usesList is true when
// the result is a fresh %Array% instance the caller may fill directly.
func arrayCreateFrom(r *Realm, c Value, length int64, withLength bool) (result *Object, usesList bool, err error) {
	if IsConstructor(c) && c.AsObject() != r.ArrayCtor {
		var cargs []Value
		if withLength {
			cargs = []Value{Int64Value(length)}
		}
		v, err := r.Construct(c, cargs, nil)
		if err != nil {
			return nil, false, err
		}
		if !v.IsObject() {
			return nil, false, r.TypeError("constructor did not return an object")
		}
		return v.AsObject(), false, nil
	}
	if !withLength {
		return r.NewArrayLen(0), true, nil
	}
	if length > math.MaxUint32 {
		return nil, false, r.RangeError("Invalid array length")
	}
	return r.NewArrayLen(uint32(length)), true, nil
}

// arrayFrom implements Array.from(items, mapFn, thisArg): iterables go
// through the iteration protocol (arrays and strings by index while it is
// unobservable), everything else is treated as an array-like.
func arrayFrom(r *Realm, this Value, args []Value) (Value, error) {
	items, mapFn, thisArg := Arg(args, 0), Arg(args, 1), Arg(args, 2)
	var mapper *Object
	if !mapFn.IsUndefined() {
		if !IsCallable(mapFn) {
			return Undefined(), r.TypeError("%s is not a function", r.DisplayString(mapFn))
		}
		mapper = mapFn.AsObject()
	}
	argv := r.pushArgs(2)
	defer r.popArgs(2)
	mapValue := func(v Value, k int64) (Value, error) {
		if mapper == nil {
			return v, nil
		}
		argv[0], argv[1] = v, Int64Value(k)
		return r.CallObject(mapper, thisArg, argv)
	}
	if items.IsNullish() {
		return Undefined(), r.TypeError("Cannot convert undefined or null to object")
	}
	fast := r.fastIterable(items)
	var usingIterator Value
	if !fast {
		var err error
		if usingIterator, err = r.GetMethod(items, iteratorKey); err != nil {
			return Undefined(), err
		}
	}
	if fast || !usingIterator.IsUndefined() {
		a, usesList, err := arrayCreateFrom(r, this, 0, false)
		if err != nil {
			return Undefined(), err
		}
		ir := iterRecord{items, IntValue(0)}
		if !fast {
			if ir.it, ir.pos, err = r.iterFromMethod(items, usingIterator); err != nil {
				return Undefined(), err
			}
		}
		var list []Value
		if usesList && ir.it.IsObject() && ir.pos.IsNumber() {
			src := ir.it.AsObject()
			list = make([]Value, 0, min(int(src.ArrayLength()), len(src.elements)))
		}
		for k := int64(0); ; k++ {
			if err := interruptEvery(r, k); err != nil {
				return Undefined(), err
			}
			v, done, err := ir.step(r)
			if err != nil {
				return Undefined(), err
			}
			if done {
				if usesList {
					return ObjectValue(r.NewArrayFromSlice(list)), nil
				}
				if err := setLength(r, a, k); err != nil {
					return Undefined(), err
				}
				return ObjectValue(a), nil
			}
			if v, err = mapValue(v, k); err == nil {
				if usesList {
					list = append(list, v)
				} else {
					err = a.CreateDataPropertyOrThrow(r, indexKey(r, k), v)
				}
			}
			if err != nil {
				return Undefined(), ir.closeThrow(r, err)
			}
		}
	}
	arrayLike, err := r.ToObject(items)
	if err != nil {
		return Undefined(), err
	}
	n, err := r.LengthOfArrayLike(arrayLike)
	if err != nil {
		return Undefined(), err
	}
	a, usesList, err := arrayCreateFrom(r, this, n, true)
	if err != nil {
		return Undefined(), err
	}
	for k := int64(0); k < n; k++ {
		v, err := getIndex(r, arrayLike, k)
		if err != nil {
			return Undefined(), err
		}
		if v, err = mapValue(v, k); err != nil {
			return Undefined(), err
		}
		if err := a.CreateDataPropertyOrThrow(r, indexKey(r, k), v); err != nil {
			return Undefined(), err
		}
		if err := interruptEvery(r, k); err != nil {
			return Undefined(), err
		}
	}
	if !usesList {
		if err := setLength(r, a, n); err != nil {
			return Undefined(), err
		}
	}
	return ObjectValue(a), nil
}

// arrayOf implements Array.of(...items).
func arrayOf(r *Realm, this Value, args []Value) (Value, error) {
	n := int64(len(args))
	a, usesList, err := arrayCreateFrom(r, this, n, true)
	if err != nil {
		return Undefined(), err
	}
	if usesList {
		items := make([]Value, len(args))
		copy(items, args)
		return ObjectValue(r.NewArrayFromSlice(items)), nil
	}
	for k, v := range args {
		if err := a.CreateDataPropertyOrThrow(r, indexKey(r, int64(k)), v); err != nil {
			return Undefined(), err
		}
	}
	if err := setLength(r, a, n); err != nil {
		return Undefined(), err
	}
	return ObjectValue(a), nil
}

// --- mutators -----------------------------------------------------------------------

// arrayProtoPush implements Array.prototype.push. Real arrays append in
// place; the generic path also produces the strict-mode TypeErrors for
// frozen arrays and read-only lengths.
func arrayProtoPush(r *Realm, this Value, args []Value) (Value, error) {
	if this.IsObject() {
		o := this.AsObject()
		if o.class == ClassArray && o.flags&flagShared == 0 {
			ad := o.internal.(*ArrayData)
			if uint64(ad.length)+uint64(len(args)) <= math.MaxUint32 && indexFreeProtos(o) && o.Push(r, args...) {
				return Uint32Value(ad.length), nil
			}
		}
	}
	o, n, err := arrayLikeThis(r, this)
	if err != nil {
		return Undefined(), err
	}
	if n+int64(len(args)) > maxSafeInteger {
		return Undefined(), r.TypeError("Pushing %d elements on an array-like of length %d is disallowed, as the total surpasses 2**53-1", len(args), n)
	}
	for _, v := range args {
		if err := setIndex(r, o, n, v); err != nil {
			return Undefined(), err
		}
		n++
	}
	if err := setLength(r, o, n); err != nil {
		return Undefined(), err
	}
	return Int64Value(n), nil
}

// arrayProtoPop implements Array.prototype.pop.
func arrayProtoPop(r *Realm, this Value, args []Value) (Value, error) {
	o, n, err := arrayLikeThis(r, this)
	if err != nil {
		return Undefined(), err
	}
	if n == 0 {
		return Undefined(), setLength(r, o, 0)
	}
	v, err := getIndex(r, o, n-1)
	if err != nil {
		return Undefined(), err
	}
	if err := deleteIndex(r, o, n-1); err != nil {
		return Undefined(), err
	}
	if err := setLength(r, o, n-1); err != nil {
		return Undefined(), err
	}
	return v, nil
}

// arrayProtoShift implements Array.prototype.shift.
func arrayProtoShift(r *Realm, this Value, args []Value) (Value, error) {
	o, n, err := arrayLikeThis(r, this)
	if err != nil {
		return Undefined(), err
	}
	if n == 0 {
		return Undefined(), setLength(r, o, 0)
	}
	if mutableArray(o) {
		first := o.elements[0]
		if first.IsHole() {
			first = Undefined()
		}
		copy(o.elements, o.elements[1:])
		o.elements[n-1] = Value{}
		o.elements = o.elements[:n-1]
		o.internal.(*ArrayData).length = uint32(n - 1)
		r.bumpEpoch(o)
		return first, nil
	}
	first, err := getIndex(r, o, 0)
	if err != nil {
		return Undefined(), err
	}
	for k := int64(1); k < n; k++ {
		if err := moveIndex(r, o, k, k-1); err != nil {
			return Undefined(), err
		}
		if err := interruptEvery(r, k); err != nil {
			return Undefined(), err
		}
	}
	if err := deleteIndex(r, o, n-1); err != nil {
		return Undefined(), err
	}
	if err := setLength(r, o, n-1); err != nil {
		return Undefined(), err
	}
	return first, nil
}

// moveIndex is the shared "if HasProperty(from) Set(to, Get(from)) else
// DeletePropertyOrThrow(to)" step of shift, unshift and splice.
func moveIndex(r *Realm, o *Object, from, to int64) error {
	if has, err := hasIndex(r, o, from); err != nil {
		return err
	} else if !has {
		return deleteIndex(r, o, to)
	}
	v, err := getIndex(r, o, from)
	if err != nil {
		return err
	}
	return setIndex(r, o, to, v)
}

// arrayProtoUnshift implements Array.prototype.unshift.
func arrayProtoUnshift(r *Realm, this Value, args []Value) (Value, error) {
	o, n, err := arrayLikeThis(r, this)
	if err != nil {
		return Undefined(), err
	}
	argc := int64(len(args))
	if argc > 0 {
		if n+argc > maxSafeInteger {
			return Undefined(), r.TypeError("Unshifting %d elements on an array-like of length %d is disallowed, as the total surpasses 2**53-1", argc, n)
		}
		if mutableArray(o) && n+argc <= math.MaxUint32 {
			ne := growWithHoles(o.elements, int(n+argc))
			copy(ne[argc:], ne[:n])
			copy(ne, args)
			o.elements = ne
			o.internal.(*ArrayData).length = uint32(n + argc)
			r.bumpEpoch(o)
			return Int64Value(n + argc), nil
		}
		for k := n; k > 0; k-- {
			if err := moveIndex(r, o, k-1, k+argc-1); err != nil {
				return Undefined(), err
			}
			if err := interruptEvery(r, k); err != nil {
				return Undefined(), err
			}
		}
		for j, v := range args {
			if err := setIndex(r, o, int64(j), v); err != nil {
				return Undefined(), err
			}
		}
	}
	if err := setLength(r, o, n+argc); err != nil {
		return Undefined(), err
	}
	return Int64Value(n + argc), nil
}

// arrayProtoSplice implements Array.prototype.splice(start, deleteCount,
// ...items).
func arrayProtoSplice(r *Realm, this Value, args []Value) (Value, error) {
	o, n, err := arrayLikeThis(r, this)
	if err != nil {
		return Undefined(), err
	}
	start, err := relativeIndex(r, Arg(args, 0), n, 0)
	if err != nil {
		return Undefined(), err
	}
	var insertCount, delCount int64
	switch {
	case len(args) == 0:
	case len(args) == 1:
		delCount = n - start
	default:
		insertCount = int64(len(args) - 2)
		dc, err := r.ToIntegerOrInfinity(args[1])
		if err != nil {
			return Undefined(), err
		}
		delCount = int64(min(max(dc, 0), float64(n-start)))
	}
	if n+insertCount-delCount > maxSafeInteger {
		return Undefined(), r.TypeError("Splicing the array-like would surpass 2**53-1 elements")
	}
	var items []Value
	if len(args) > 2 {
		items = args[2:]
	}
	newLen := n - delCount + insertCount
	species, err := r.arraySpeciesCtor(o)
	if err != nil {
		return Undefined(), err
	}
	// The coercions above ran user code that may have resized the array;
	// the in-place rewrite is only valid while n describes the storage.
	if species == nil && mutableArray(o) && int64(len(o.elements)) == n && newLen <= math.MaxUint32 {
		removed := make([]Value, delCount)
		copy(removed, o.elements[start:start+delCount])
		switch {
		case insertCount == delCount:
			copy(o.elements[start:], items)
		case insertCount < delCount:
			copy(o.elements[start+insertCount:], o.elements[start+delCount:])
			clear(o.elements[newLen:])
			o.elements = o.elements[:newLen]
			copy(o.elements[start:], items)
		default:
			ne := growWithHoles(o.elements, int(newLen))
			copy(ne[start+insertCount:], ne[start+delCount:n])
			copy(ne[start:], items)
			o.elements = ne
		}
		o.internal.(*ArrayData).length = uint32(newLen)
		r.bumpEpoch(o)
		return ObjectValue(r.NewArrayFromSlice(removed)), nil
	}
	var a *Object
	switch {
	case species != nil:
		if a, err = r.speciesCreate(species, delCount); err != nil {
			return Undefined(), err
		}
	case delCount > math.MaxUint32:
		return Undefined(), r.RangeError("Invalid array length")
	default:
		a = r.NewArrayLen(uint32(delCount))
	}
	for k := int64(0); k < delCount; k++ {
		if has, err := hasIndex(r, o, start+k); err != nil {
			return Undefined(), err
		} else if has {
			v, err := getIndex(r, o, start+k)
			if err != nil {
				return Undefined(), err
			}
			if err := a.CreateDataPropertyOrThrow(r, indexKey(r, k), v); err != nil {
				return Undefined(), err
			}
		}
		if err := interruptEvery(r, k); err != nil {
			return Undefined(), err
		}
	}
	if species != nil {
		if err := setLength(r, a, delCount); err != nil {
			return Undefined(), err
		}
	}
	switch {
	case insertCount < delCount:
		for k := start; k < n-delCount; k++ {
			if err := moveIndex(r, o, k+delCount, k+insertCount); err != nil {
				return Undefined(), err
			}
			if err := interruptEvery(r, k); err != nil {
				return Undefined(), err
			}
		}
		for k := n; k > newLen; k-- {
			if err := deleteIndex(r, o, k-1); err != nil {
				return Undefined(), err
			}
			if err := interruptEvery(r, k); err != nil {
				return Undefined(), err
			}
		}
	case insertCount > delCount:
		for k := n - delCount; k > start; k-- {
			if err := moveIndex(r, o, k+delCount-1, k+insertCount-1); err != nil {
				return Undefined(), err
			}
			if err := interruptEvery(r, k); err != nil {
				return Undefined(), err
			}
		}
	}
	for i, v := range items {
		if err := setIndex(r, o, start+int64(i), v); err != nil {
			return Undefined(), err
		}
	}
	if err := setLength(r, o, newLen); err != nil {
		return Undefined(), err
	}
	return ObjectValue(a), nil
}

// arrayProtoReverse implements Array.prototype.reverse.
func arrayProtoReverse(r *Realm, this Value, args []Value) (Value, error) {
	o, n, err := arrayLikeThis(r, this)
	if err != nil {
		return Undefined(), err
	}
	if mutableArray(o) {
		slices.Reverse(o.elements)
		r.bumpEpoch(o)
		return ObjectValue(o), nil
	}
	for lower := int64(0); lower < n/2; lower++ {
		upper := n - 1 - lower
		// The lower Get may run a getter, so upper is probed after it.
		var lowerValue, upperValue Value
		lowerExists, err := hasIndex(r, o, lower)
		if err != nil {
			return Undefined(), err
		}
		if lowerExists {
			if lowerValue, err = getIndex(r, o, lower); err != nil {
				return Undefined(), err
			}
		}
		upperExists, err := hasIndex(r, o, upper)
		if err != nil {
			return Undefined(), err
		}
		if upperExists {
			if upperValue, err = getIndex(r, o, upper); err != nil {
				return Undefined(), err
			}
		}
		switch {
		case lowerExists && upperExists:
			if err := setIndex(r, o, lower, upperValue); err != nil {
				return Undefined(), err
			}
			if err := setIndex(r, o, upper, lowerValue); err != nil {
				return Undefined(), err
			}
		case upperExists:
			if err := setIndex(r, o, lower, upperValue); err != nil {
				return Undefined(), err
			}
			if err := deleteIndex(r, o, upper); err != nil {
				return Undefined(), err
			}
		case lowerExists:
			if err := deleteIndex(r, o, lower); err != nil {
				return Undefined(), err
			}
			if err := setIndex(r, o, upper, lowerValue); err != nil {
				return Undefined(), err
			}
		}
		if err := interruptEvery(r, lower); err != nil {
			return Undefined(), err
		}
	}
	return ObjectValue(o), nil
}

// arrayProtoFill implements Array.prototype.fill(value, start, end).
func arrayProtoFill(r *Realm, this Value, args []Value) (Value, error) {
	o, n, err := arrayLikeThis(r, this)
	if err != nil {
		return Undefined(), err
	}
	value := Arg(args, 0)
	k, err := relativeIndex(r, Arg(args, 1), n, 0)
	if err != nil {
		return Undefined(), err
	}
	final, err := relativeIndex(r, Arg(args, 2), n, n)
	if err != nil {
		return Undefined(), err
	}
	// A start/end valueOf may have shrunk the array below final; writes past
	// the storage take the generic path, which grows it again per spec.
	if mutableArray(o) && final <= int64(len(o.elements)) {
		for i := k; i < final; i++ {
			o.elements[i] = value
		}
		return ObjectValue(o), nil
	}
	for ; k < final; k++ {
		if err := setIndex(r, o, k, value); err != nil {
			return Undefined(), err
		}
		if err := interruptEvery(r, k); err != nil {
			return Undefined(), err
		}
	}
	return ObjectValue(o), nil
}

// sortItem pairs a value with its default-comparator key.
type sortItem struct {
	v   Value
	key *String
}

// arrayProtoSort implements Array.prototype.sort: present values are
// collected, stably merge-sorted (undefined last, comparator exceptions
// propagate immediately) and written back with the holes moved to the end.
func arrayProtoSort(r *Realm, this Value, args []Value) (Value, error) {
	cmp := Arg(args, 0)
	if !cmp.IsUndefined() && !IsCallable(cmp) {
		return Undefined(), r.TypeError("The comparison function must be either a function or undefined")
	}
	o, n, err := arrayLikeThis(r, this)
	if err != nil {
		return Undefined(), err
	}
	items := make([]Value, 0, min(n, int64(len(o.elements))+interruptStride))
	for k := int64(0); k < n; k++ {
		if v, ok := denseElement(o, k); ok {
			items = append(items, v)
		} else if has, err := hasIndex(r, o, k); err != nil {
			return Undefined(), err
		} else if has {
			v, err := getIndex(r, o, k)
			if err != nil {
				return Undefined(), err
			}
			items = append(items, v)
		}
		if err := interruptEvery(r, k); err != nil {
			return Undefined(), err
		}
	}
	if err := sortValues(r, items, cmp); err != nil {
		return Undefined(), err
	}
	if mutableArray(o) && int64(len(o.elements)) == n {
		copy(o.elements, items)
		for j := len(items); j < len(o.elements); j++ {
			o.elements[j] = Hole()
		}
		o.trimTrailingHoles()
		r.bumpEpoch(o)
		return ObjectValue(o), nil
	}
	for j, v := range items {
		if err := setIndex(r, o, int64(j), v); err != nil {
			return Undefined(), err
		}
	}
	for j := int64(len(items)); j < n; j++ {
		if err := deleteIndex(r, o, j); err != nil {
			return Undefined(), err
		}
		if err := interruptEvery(r, j); err != nil {
			return Undefined(), err
		}
	}
	return ObjectValue(o), nil
}

// sortValues implements SortIndexedProperties' ordering of a value list:
// undefineds sink to the end, then the rest are sorted by the comparator or,
// when it is undefined, by code units of their string conversions
// (computed once per element).
func sortValues(r *Realm, items []Value, cmp Value) error {
	live := 0
	for _, v := range items {
		if !v.IsUndefined() {
			items[live] = v
			live++
		}
	}
	for j := live; j < len(items); j++ {
		items[j] = Undefined()
	}
	vals := items[:live]
	if len(vals) < 2 {
		return nil
	}
	steps := int64(0)
	if !cmp.IsUndefined() {
		fn := cmp.AsObject()
		argv := r.pushArgs(2)
		defer r.popArgs(2)
		return mergeSort(vals, make([]Value, len(vals)), func(a, b Value) (int, error) {
			argv[0], argv[1] = a, b
			res, err := r.CallObject(fn, Undefined(), argv)
			if err != nil {
				return 0, err
			}
			f, err := r.ToNumber(res)
			if err != nil {
				return 0, err
			}
			switch {
			case f < 0:
				return -1, nil
			case f > 0:
				return 1, nil
			}
			return 0, nil // NaN and 0
		})
	}
	keyed := make([]sortItem, len(vals))
	for i, v := range vals {
		s, err := r.ToString(v)
		if err != nil {
			return err
		}
		keyed[i] = sortItem{v: v, key: s}
	}
	err := mergeSort(keyed, make([]sortItem, len(keyed)), func(a, b sortItem) (int, error) {
		steps++
		if err := interruptEvery(r, steps); err != nil {
			return 0, err
		}
		return a.key.Compare(b.key), nil
	})
	if err != nil {
		return err
	}
	for i := range keyed {
		vals[i] = keyed[i].v
	}
	return nil
}

// mergeSort is a stable top-down merge sort with insertion sort for short
// runs; cmp errors abort immediately. buf must be as long as vals.
func mergeSort[T any](vals, buf []T, cmp func(a, b T) (int, error)) error {
	n := len(vals)
	if n <= 12 {
		for i := 1; i < n; i++ {
			v := vals[i]
			j := i
			for j > 0 {
				c, err := cmp(vals[j-1], v)
				if err != nil {
					return err
				}
				if c <= 0 {
					break
				}
				vals[j] = vals[j-1]
				j--
			}
			vals[j] = v
		}
		return nil
	}
	mid := n / 2
	if err := mergeSort(vals[:mid], buf[:mid], cmp); err != nil {
		return err
	}
	if err := mergeSort(vals[mid:], buf[mid:], cmp); err != nil {
		return err
	}
	c, err := cmp(vals[mid-1], vals[mid])
	if err != nil {
		return err
	}
	if c <= 0 {
		return nil // already in order
	}
	copy(buf, vals)
	i, j, k := 0, mid, 0
	for i < mid && j < n {
		c, err := cmp(buf[j], buf[i])
		if err != nil {
			return err
		}
		if c < 0 {
			vals[k] = buf[j]
			j++
		} else {
			vals[k] = buf[i]
			i++
		}
		k++
	}
	k += copy(vals[k:], buf[i:mid])
	copy(vals[k:], buf[j:n])
	return nil
}

// --- accessors ----------------------------------------------------------------------

// arrayProtoAt implements Array.prototype.at.
func arrayProtoAt(r *Realm, this Value, args []Value) (Value, error) {
	o, n, err := arrayLikeThis(r, this)
	if err != nil {
		return Undefined(), err
	}
	rel, err := r.ToIntegerOrInfinity(Arg(args, 0))
	if err != nil {
		return Undefined(), err
	}
	k := rel
	if rel < 0 {
		k = float64(n) + rel
	}
	if k < 0 || k >= float64(n) {
		return Undefined(), nil
	}
	return getIndex(r, o, int64(k))
}

// arrayProtoSlice implements Array.prototype.slice(start, end).
func arrayProtoSlice(r *Realm, this Value, args []Value) (Value, error) {
	o, n, err := arrayLikeThis(r, this)
	if err != nil {
		return Undefined(), err
	}
	k, err := relativeIndex(r, Arg(args, 0), n, 0)
	if err != nil {
		return Undefined(), err
	}
	final, err := relativeIndex(r, Arg(args, 1), n, n)
	if err != nil {
		return Undefined(), err
	}
	count := max(final-k, 0)
	species, err := r.arraySpeciesCtor(o)
	if err != nil {
		return Undefined(), err
	}
	// A start/end valueOf may have shrunk the array: indices past the storage
	// are holes now, which only the generic path reproduces.
	if species == nil && plainArray(o) && k+count <= int64(len(o.elements)) {
		items := make([]Value, count)
		copy(items, o.elements[k:k+count])
		return ObjectValue(r.NewArrayFromSlice(items)), nil
	}
	var a *Object
	switch {
	case species != nil:
		if a, err = r.speciesCreate(species, count); err != nil {
			return Undefined(), err
		}
	case count > math.MaxUint32:
		return Undefined(), r.RangeError("Invalid array length")
	default:
		a = r.NewArrayLen(uint32(count))
	}
	i := int64(0)
	for ; k < final; k, i = k+1, i+1 {
		if has, err := hasIndex(r, o, k); err != nil {
			return Undefined(), err
		} else if has {
			v, err := getIndex(r, o, k)
			if err != nil {
				return Undefined(), err
			}
			if err := a.CreateDataPropertyOrThrow(r, indexKey(r, i), v); err != nil {
				return Undefined(), err
			}
		}
		if err := interruptEvery(r, i); err != nil {
			return Undefined(), err
		}
	}
	if species != nil {
		if err := setLength(r, a, i); err != nil {
			return Undefined(), err
		}
	}
	return ObjectValue(a), nil
}

// arrayProtoConcat implements Array.prototype.concat: spreadable items
// (arrays, or objects whose @@isConcatSpreadable says so) are spread with
// holes preserved, everything else is appended as one element.
func arrayProtoConcat(r *Realm, this Value, args []Value) (Value, error) {
	o, err := r.ToObject(this)
	if err != nil {
		return Undefined(), err
	}
	species, err := r.arraySpeciesCtor(o)
	if err != nil {
		return Undefined(), err
	}
	// The fast path needs every item's spreadability known without reading
	// @@isConcatSpreadable and every spread item a plain array.
	total, fast := int64(0), species == nil
	clean := fast && r.spreadableProtosClean()
	for i := -1; fast && i < len(args); i++ {
		e := ObjectValue(o)
		if i >= 0 {
			e = args[i]
		}
		switch {
		case !e.IsObject():
			total++
		case !r.lacksSpreadable(e.AsObject(), clean) || IsArray(e) && !plainArray(e.AsObject()):
			fast = false
		case IsArray(e):
			total += int64(len(e.AsObject().elements))
		default:
			total++
		}
	}
	if fast && total <= math.MaxUint32 {
		items := make([]Value, 0, total)
		for i := -1; i < len(args); i++ {
			e := ObjectValue(o)
			if i >= 0 {
				e = args[i]
			}
			if IsArray(e) {
				items = append(items, e.AsObject().elements...)
			} else {
				items = append(items, e)
			}
		}
		return ObjectValue(r.NewArrayFromSlice(items)), nil
	}
	var a *Object
	if species != nil {
		if a, err = r.speciesCreate(species, 0); err != nil {
			return Undefined(), err
		}
	} else {
		a = r.NewArrayLen(0)
	}
	n := int64(0)
	for i := -1; i < len(args); i++ {
		e := ObjectValue(o)
		if i >= 0 {
			e = args[i]
		}
		spread, err := r.isConcatSpreadable(e)
		if err != nil {
			return Undefined(), err
		}
		if !spread {
			if n >= maxSafeInteger {
				return Undefined(), r.TypeError("Array length would surpass 2**53-1")
			}
			if err := a.CreateDataPropertyOrThrow(r, indexKey(r, n), e); err != nil {
				return Undefined(), err
			}
			n++
			continue
		}
		src := e.AsObject()
		srcLen, err := r.LengthOfArrayLike(src)
		if err != nil {
			return Undefined(), err
		}
		if n+srcLen > maxSafeInteger {
			return Undefined(), r.TypeError("Array length would surpass 2**53-1")
		}
		for k := int64(0); k < srcLen; k, n = k+1, n+1 {
			if has, err := hasIndex(r, src, k); err != nil {
				return Undefined(), err
			} else if has {
				v, err := getIndex(r, src, k)
				if err != nil {
					return Undefined(), err
				}
				if err := a.CreateDataPropertyOrThrow(r, indexKey(r, n), v); err != nil {
					return Undefined(), err
				}
			}
			if err := interruptEvery(r, k); err != nil {
				return Undefined(), err
			}
		}
	}
	if err := setLength(r, a, n); err != nil {
		return Undefined(), err
	}
	return ObjectValue(a), nil
}

// isConcatSpreadable implements IsConcatSpreadable(v).
func (r *Realm) isConcatSpreadable(v Value) (bool, error) {
	if !v.IsObject() {
		return false, nil
	}
	o := v.AsObject()
	if !r.lacksSpreadable(o, r.spreadableProtosClean()) {
		s, err := o.GetProp(r, spreadableKey)
		if err != nil {
			return false, err
		}
		if !s.IsUndefined() {
			return ToBoolean(s), nil
		}
	}
	return r.isArray(v)
}

// arrayProtoJoin implements Array.prototype.join(separator); nullish
// elements contribute nothing, the loop is interrupt-checked and the builder
// is pre-sized from the dense storage. An object already being joined (by
// join or toLocaleString) further up the stack joins as "", as in V8, so
// cyclic arrays terminate. A join that can run no JavaScript cannot re-enter
// itself, so it stays off that stack.
func arrayProtoJoin(r *Realm, this Value, args []Value) (Value, error) {
	o, err := r.ToObject(this)
	if err != nil {
		return Undefined(), err
	}
	if r.joining(o) {
		return StringValue(AtomEmpty), nil
	}
	if o.class == ClassArray && !Arg(args, 0).IsObject() {
		if size, ok := primitiveElements(o); ok {
			return join(r, o, args, size)
		}
	}
	return joinMarked(r, o, args)
}

// joinMarked is join with o on the stack of objects being joined.
func joinMarked(r *Realm, o *Object, args []Value) (Value, error) {
	r.pushJoin(o)
	defer r.popJoin()
	return join(r, o, args, -1)
}

// primitiveElements reports whether each element of the array o below its
// length is a present primitive in the dense storage, so that joining o
// reads no accessor and converts no object, and returns the joined length
// of those strings and numbers (24 bytes for a number).
func primitiveElements(o *Object) (int64, bool) {
	n := int64(o.internal.(*ArrayData).length)
	if n == 0 {
		return 0, true
	}
	o.materializeHost()
	if n > int64(len(o.elements)) {
		return 0, false
	}
	var size int64
	for _, v := range o.elements[:n] {
		switch {
		case v.IsString():
			size += int64(v.AsString().Len())
		case v.IsNumber():
			size += 24
		case v.IsHole(), v.IsObject():
			return 0, false
		}
	}
	return size, true
}

// join is Array.prototype.join after the cycle check; size is the joined
// length of o's elements from primitiveElements, or -1.
func join(r *Realm, o *Object, args []Value, size int64) (Value, error) {
	n, err := r.LengthOfArrayLike(o)
	if err != nil {
		return Undefined(), err
	}
	sep := commaString
	if sv := Arg(args, 0); !sv.IsUndefined() {
		if sep, err = r.ToString(sv); err != nil {
			return Undefined(), err
		}
	}
	if n == 0 {
		return StringValue(AtomEmpty), nil
	}
	var sb StringBuilder
	if size < 0 && o.class == ClassArray {
		size = 0
		for _, v := range o.elements {
			switch {
			case v.IsString():
				size += int64(v.AsString().Len())
			case v.IsNumber():
				size += 24
			}
		}
	}
	if size >= 0 {
		if size += (n - 1) * int64(sep.Len()); size <= int64(maxStringLength) {
			sb.Grow(int(size))
		}
	}
	for k := int64(0); k < n; k++ {
		if k > 0 {
			sb.WriteString(sep)
		}
		v, ok := denseElement(o, k)
		if !ok {
			if v, err = getIndex(r, o, k); err != nil {
				return Undefined(), err
			}
		}
		switch {
		case v.IsString():
			sb.WriteString(v.AsString())
		case v.IsNullish():
		default:
			s, err := r.ToString(v)
			if err != nil {
				return Undefined(), err
			}
			sb.WriteString(s)
		}
		if err := sb.checkLength(r); err != nil {
			return Undefined(), err
		}
		if err := interruptEvery(r, k); err != nil {
			return Undefined(), err
		}
	}
	return StringValue(sb.String()), nil
}

// joining reports whether o is on the stack of objects being joined.
func (r *Realm) joining(o *Object) bool {
	if r.lazy != nil {
		for _, j := range r.lazy.joins {
			if j == o {
				return true
			}
		}
	}
	return false
}

// pushJoin puts o on the stack of objects being joined; every push is
// paired with a deferred popJoin, so the stack unwinds on every exit.
func (r *Realm) pushJoin(o *Object) {
	l := r.lazyState()
	if l.joins == nil {
		l.joins = make([]*Object, 0, 8)
	}
	l.joins = append(l.joins, o)
}

// popJoin takes the innermost object off the stack of objects being joined.
func (r *Realm) popJoin() {
	l := r.lazy
	l.joins[len(l.joins)-1] = nil
	l.joins = l.joins[:len(l.joins)-1]
}

// arrayProtoToLocaleString implements Array.prototype.toLocaleString
// without ECMA-402: the separator is ",", and each element's
// toLocaleString is invoked with no arguments (on the primitive itself,
// not a wrapper). Like join, it treats an object already being joined as
// "".
func arrayProtoToLocaleString(r *Realm, this Value, args []Value) (Value, error) {
	o, err := r.ToObject(this)
	if err != nil {
		return Undefined(), err
	}
	if r.joining(o) {
		return StringValue(AtomEmpty), nil
	}
	r.pushJoin(o)
	defer r.popJoin()
	n, err := r.LengthOfArrayLike(o)
	if err != nil {
		return Undefined(), err
	}
	var sb StringBuilder
	for k := int64(0); k < n; k++ {
		if k > 0 {
			sb.WriteString(commaString)
		}
		v, err := getIndex(r, o, k)
		if err != nil {
			return Undefined(), err
		}
		if !v.IsNullish() {
			fn, err := r.GetV(v, StringKey(AtomToLocaleString))
			if err != nil {
				return Undefined(), err
			}
			sv, err := r.Call(fn, v, nil)
			if err != nil {
				return Undefined(), err
			}
			s, err := r.ToString(sv)
			if err != nil {
				return Undefined(), err
			}
			sb.WriteString(s)
		}
		if err := sb.checkLength(r); err != nil {
			return Undefined(), err
		}
		if err := interruptEvery(r, k); err != nil {
			return Undefined(), err
		}
	}
	return StringValue(sb.String()), nil
}

// arrayProtoToString implements Array.prototype.toString: join when it is
// callable, else Object.prototype.toString.
func arrayProtoToString(r *Realm, this Value, args []Value) (Value, error) {
	o, err := r.ToObject(this)
	if err != nil {
		return Undefined(), err
	}
	fn, err := o.GetProp(r, StringKey(AtomJoin))
	if err != nil {
		return Undefined(), err
	}
	if !IsCallable(fn) {
		return objectProtoToString(r, ObjectValue(o), nil)
	}
	return r.CallObject(fn.AsObject(), ObjectValue(o), nil)
}

// --- searching ----------------------------------------------------------------------

// arrayProtoIndexOf implements Array.prototype.indexOf (strict equality,
// holes skipped).
func arrayProtoIndexOf(r *Realm, this Value, args []Value) (Value, error) {
	o, n, err := arrayLikeThis(r, this)
	if err != nil {
		return Undefined(), err
	}
	if n == 0 {
		return IntValue(-1), nil
	}
	k, err := searchStart(r, Arg(args, 1), n)
	if err != nil || k < 0 {
		return IntValue(-1), err
	}
	target := Arg(args, 0)
	for ; k < n; k++ {
		if v, ok := denseElement(o, k); ok {
			if StrictEquals(v, target) {
				return Int64Value(k), nil
			}
		} else if has, err := hasIndex(r, o, k); err != nil {
			return Undefined(), err
		} else if has {
			v, err := getIndex(r, o, k)
			if err != nil {
				return Undefined(), err
			}
			if StrictEquals(v, target) {
				return Int64Value(k), nil
			}
		}
		if err := interruptEvery(r, k); err != nil {
			return Undefined(), err
		}
	}
	return IntValue(-1), nil
}

// searchStart computes the first index for indexOf/includes from fromIndex:
// -1 means the search cannot match (fromIndex is +∞).
func searchStart(r *Realm, fromIndex Value, n int64) (int64, error) {
	if fromIndex.IsUndefined() {
		return 0, nil
	}
	f, err := r.ToIntegerOrInfinity(fromIndex)
	if err != nil {
		return 0, err
	}
	switch {
	case f >= float64(n):
		return -1, nil
	case f >= 0:
		return int64(f), nil
	}
	return max(int64(float64(n)+f), 0), nil // -∞ yields 0
}

// arrayProtoLastIndexOf implements Array.prototype.lastIndexOf. The
// fromIndex default (len-1) applies only when the argument is absent.
func arrayProtoLastIndexOf(r *Realm, this Value, args []Value) (Value, error) {
	o, n, err := arrayLikeThis(r, this)
	if err != nil {
		return Undefined(), err
	}
	if n == 0 {
		return IntValue(-1), nil
	}
	k := n - 1
	if len(args) > 1 {
		f, err := r.ToIntegerOrInfinity(args[1])
		if err != nil {
			return Undefined(), err
		}
		if f >= 0 {
			k = int64(min(f, float64(n-1)))
		} else {
			k = int64(float64(n) + f) // -∞ makes k negative: no match
			if f == negInf {
				return IntValue(-1), nil
			}
		}
	}
	target := Arg(args, 0)
	for ; k >= 0; k-- {
		if v, ok := denseElement(o, k); ok {
			if StrictEquals(v, target) {
				return Int64Value(k), nil
			}
		} else if has, err := hasIndex(r, o, k); err != nil {
			return Undefined(), err
		} else if has {
			v, err := getIndex(r, o, k)
			if err != nil {
				return Undefined(), err
			}
			if StrictEquals(v, target) {
				return Int64Value(k), nil
			}
		}
		if err := interruptEvery(r, k); err != nil {
			return Undefined(), err
		}
	}
	return IntValue(-1), nil
}

// arrayProtoIncludes implements Array.prototype.includes (SameValueZero,
// holes read as undefined).
func arrayProtoIncludes(r *Realm, this Value, args []Value) (Value, error) {
	o, n, err := arrayLikeThis(r, this)
	if err != nil {
		return Undefined(), err
	}
	if n == 0 {
		return False(), nil
	}
	k, err := searchStart(r, Arg(args, 1), n)
	if err != nil || k < 0 {
		return False(), err
	}
	target := Arg(args, 0)
	for ; k < n; k++ {
		v, ok := denseElement(o, k)
		if !ok {
			if v, err = getIndex(r, o, k); err != nil {
				return Undefined(), err
			}
		}
		if SameValueZero(v, target) {
			return True(), nil
		}
		if err := interruptEvery(r, k); err != nil {
			return Undefined(), err
		}
	}
	return False(), nil
}

// findIndexed implements find/findIndex/findLast/findLastIndex: every index
// is visited (holes read as undefined) and the predicate result is coerced
// with ToBoolean.
func findIndexed(r *Realm, this Value, args []Value, fromEnd bool) (value Value, index int64, err error) {
	o, n, err := arrayLikeThis(r, this)
	if err != nil {
		return Undefined(), -1, err
	}
	fn, err := callbackArg(r, args)
	if err != nil {
		return Undefined(), -1, err
	}
	thisArg := Arg(args, 1)
	argv := r.pushArgs(3)
	defer r.popArgs(3)
	argv[2] = ObjectValue(o)
	for i := int64(0); i < n; i++ {
		if err := interruptEvery(r, i); err != nil {
			return Undefined(), -1, err
		}
		k := i
		if fromEnd {
			k = n - 1 - i
		}
		v, err := getIndex(r, o, k)
		if err != nil {
			return Undefined(), -1, err
		}
		argv[0], argv[1] = v, Int64Value(k)
		res, err := r.CallObject(fn, thisArg, argv)
		if err != nil {
			return Undefined(), -1, err
		}
		if ToBoolean(res) {
			return v, k, nil
		}
	}
	return Undefined(), -1, nil
}

func arrayProtoFind(r *Realm, this Value, args []Value) (Value, error) {
	v, _, err := findIndexed(r, this, args, false)
	return v, err
}

func arrayProtoFindIndex(r *Realm, this Value, args []Value) (Value, error) {
	_, k, err := findIndexed(r, this, args, false)
	return Int64Value(k), err
}

func arrayProtoFindLast(r *Realm, this Value, args []Value) (Value, error) {
	v, _, err := findIndexed(r, this, args, true)
	return v, err
}

func arrayProtoFindLastIndex(r *Realm, this Value, args []Value) (Value, error) {
	_, k, err := findIndexed(r, this, args, true)
	return Int64Value(k), err
}

// --- iteration methods --------------------------------------------------------------

// presentElement returns element k when HasProperty(O, k) holds.
func presentElement(r *Realm, o *Object, k int64) (Value, bool, error) {
	if v, ok := denseElement(o, k); ok {
		return v, true, nil
	}
	if has, err := hasIndex(r, o, k); err != nil || !has {
		return Undefined(), false, err
	}
	v, err := getIndex(r, o, k)
	return v, err == nil, err
}

// arrayProtoForEach implements Array.prototype.forEach.
func arrayProtoForEach(r *Realm, this Value, args []Value) (Value, error) {
	o, n, err := arrayLikeThis(r, this)
	if err != nil {
		return Undefined(), err
	}
	fn, err := callbackArg(r, args)
	if err != nil {
		return Undefined(), err
	}
	thisArg := Arg(args, 1)
	argv := r.pushArgs(3)
	defer r.popArgs(3)
	argv[2] = ObjectValue(o)
	for k := int64(0); k < n; k++ {
		if err := interruptEvery(r, k); err != nil {
			return Undefined(), err
		}
		v, present, err := presentElement(r, o, k)
		if err != nil {
			return Undefined(), err
		}
		if !present {
			continue
		}
		argv[0], argv[1] = v, Int64Value(k)
		if _, err := r.CallObject(fn, thisArg, argv); err != nil {
			return Undefined(), err
		}
	}
	return Undefined(), nil
}

// arrayProtoMap implements Array.prototype.map; holes stay holes.
func arrayProtoMap(r *Realm, this Value, args []Value) (Value, error) {
	o, n, err := arrayLikeThis(r, this)
	if err != nil {
		return Undefined(), err
	}
	fn, err := callbackArg(r, args)
	if err != nil {
		return Undefined(), err
	}
	species, err := r.arraySpeciesCtor(o)
	if err != nil {
		return Undefined(), err
	}
	// The result is created up front as a hole-filled array (it is not
	// reachable by the callback, so its dense storage is written directly);
	// array-likes with a huge length go through sparse storage. A species
	// result is written with CreateDataPropertyOrThrow.
	var a *Object
	dense := false
	switch {
	case species != nil:
		if a, err = r.speciesCreate(species, n); err != nil {
			return Undefined(), err
		}
	case n > math.MaxUint32:
		return Undefined(), r.RangeError("Invalid array length")
	default:
		a = r.NewArrayLen(uint32(n))
		dense = n <= int64(len(o.elements))+denseGrowLimit
		if dense && len(a.elements) != int(n) {
			a.elements = growWithHoles(a.elements, int(n))
		}
	}
	thisArg := Arg(args, 1)
	argv := r.pushArgs(3)
	defer r.popArgs(3)
	argv[2] = ObjectValue(o)
	for k := int64(0); k < n; k++ {
		if err := interruptEvery(r, k); err != nil {
			return Undefined(), err
		}
		v, present, err := presentElement(r, o, k)
		if err != nil {
			return Undefined(), err
		}
		if !present {
			continue
		}
		argv[0], argv[1] = v, Int64Value(k)
		res, err := r.CallObject(fn, thisArg, argv)
		if err != nil {
			return Undefined(), err
		}
		if dense {
			a.elements[k] = res
		} else if err := a.CreateDataPropertyOrThrow(r, indexKey(r, k), res); err != nil {
			return Undefined(), err
		}
	}
	return ObjectValue(a), nil
}

// arrayProtoFilter implements Array.prototype.filter.
func arrayProtoFilter(r *Realm, this Value, args []Value) (Value, error) {
	o, n, err := arrayLikeThis(r, this)
	if err != nil {
		return Undefined(), err
	}
	fn, err := callbackArg(r, args)
	if err != nil {
		return Undefined(), err
	}
	species, err := r.arraySpeciesCtor(o)
	if err != nil {
		return Undefined(), err
	}
	var a *Object // the species result, written as elements are selected
	if species != nil {
		if a, err = r.speciesCreate(species, 0); err != nil {
			return Undefined(), err
		}
	}
	thisArg := Arg(args, 1)
	argv := r.pushArgs(3)
	defer r.popArgs(3)
	argv[2] = ObjectValue(o)
	var selected []Value
	if a == nil {
		selected = make([]Value, 0, min(n, 32))
	}
	to := int64(0)
	for k := int64(0); k < n; k++ {
		if err := interruptEvery(r, k); err != nil {
			return Undefined(), err
		}
		v, present, err := presentElement(r, o, k)
		if err != nil {
			return Undefined(), err
		}
		if !present {
			continue
		}
		argv[0], argv[1] = v, Int64Value(k)
		res, err := r.CallObject(fn, thisArg, argv)
		if err != nil {
			return Undefined(), err
		}
		switch {
		case !ToBoolean(res):
		case a != nil:
			if err := a.CreateDataPropertyOrThrow(r, indexKey(r, to), v); err != nil {
				return Undefined(), err
			}
			to++
		default:
			selected = append(selected, v)
		}
	}
	if a != nil {
		return ObjectValue(a), nil
	}
	return ObjectValue(r.NewArrayFromSlice(selected)), nil
}

// someEvery implements Array.prototype.some (want == true) and every
// (want == false): the first callback result whose truthiness equals want
// decides.
func someEvery(r *Realm, this Value, args []Value, want bool) (Value, error) {
	o, n, err := arrayLikeThis(r, this)
	if err != nil {
		return Undefined(), err
	}
	fn, err := callbackArg(r, args)
	if err != nil {
		return Undefined(), err
	}
	thisArg := Arg(args, 1)
	argv := r.pushArgs(3)
	defer r.popArgs(3)
	argv[2] = ObjectValue(o)
	for k := int64(0); k < n; k++ {
		if err := interruptEvery(r, k); err != nil {
			return Undefined(), err
		}
		v, present, err := presentElement(r, o, k)
		if err != nil {
			return Undefined(), err
		}
		if !present {
			continue
		}
		argv[0], argv[1] = v, Int64Value(k)
		res, err := r.CallObject(fn, thisArg, argv)
		if err != nil {
			return Undefined(), err
		}
		if ToBoolean(res) == want {
			return Bool(want), nil
		}
	}
	return Bool(!want), nil
}

func arrayProtoSome(r *Realm, this Value, args []Value) (Value, error) {
	return someEvery(r, this, args, true)
}

func arrayProtoEvery(r *Realm, this Value, args []Value) (Value, error) {
	return someEvery(r, this, args, false)
}

// reduceIndexed implements reduce (fromEnd == false) and reduceRight.
func reduceIndexed(r *Realm, this Value, args []Value, fromEnd bool) (Value, error) {
	o, n, err := arrayLikeThis(r, this)
	if err != nil {
		return Undefined(), err
	}
	fn, err := callbackArg(r, args)
	if err != nil {
		return Undefined(), err
	}
	step := int64(1)
	k, end := int64(0), n
	if fromEnd {
		step, k, end = -1, n-1, -1
	}
	var acc Value
	if len(args) >= 2 {
		acc = args[1]
	} else {
		found := false
		for ; k != end && !found; k += step {
			if err := interruptEvery(r, k); err != nil {
				return Undefined(), err
			}
			var present bool
			if acc, present, err = presentElement(r, o, k); err != nil {
				return Undefined(), err
			}
			found = present
		}
		if !found {
			return Undefined(), r.TypeError("Reduce of empty array with no initial value")
		}
	}
	argv := r.pushArgs(4)
	defer r.popArgs(4)
	argv[3] = ObjectValue(o)
	for ; k != end; k += step {
		if err := interruptEvery(r, k); err != nil {
			return Undefined(), err
		}
		v, present, err := presentElement(r, o, k)
		if err != nil {
			return Undefined(), err
		}
		if !present {
			continue
		}
		argv[0], argv[1], argv[2] = acc, v, Int64Value(k)
		if acc, err = r.CallObject(fn, Undefined(), argv); err != nil {
			return Undefined(), err
		}
	}
	return acc, nil
}

func arrayProtoReduce(r *Realm, this Value, args []Value) (Value, error) {
	return reduceIndexed(r, this, args, false)
}

func arrayProtoReduceRight(r *Realm, this Value, args []Value) (Value, error) {
	return reduceIndexed(r, this, args, true)
}

// flatTarget is the target of FlattenIntoArray: the items of a fresh array,
// or a species result written with CreateDataPropertyOrThrow.
type flatTarget struct {
	items []Value
	obj   *Object
	n     int64
}

func (t *flatTarget) add(r *Realm, v Value) error {
	if t.n >= maxSafeInteger {
		return r.TypeError("Array length would surpass 2**53-1")
	}
	if t.obj != nil {
		if err := t.obj.CreateDataPropertyOrThrow(r, indexKey(r, t.n), v); err != nil {
			return err
		}
	} else {
		t.items = append(t.items, v)
	}
	t.n++
	return nil
}

// flattenIntoArray implements FlattenIntoArray, appending to target. The
// recursion is accounted as calls so a cyclic array ends in the call-depth
// RangeError instead of exhausting the Go stack.
func flattenIntoArray(r *Realm, target *flatTarget, source *Object, sourceLen int64, depth float64, mapper *Object, thisArg Value) error {
	if err := r.EnterCall(); err != nil {
		return err
	}
	defer r.ExitCall()
	argv := r.pushArgs(3)
	defer r.popArgs(3)
	argv[2] = ObjectValue(source)
	for k := int64(0); k < sourceLen; k++ {
		if err := interruptEvery(r, k); err != nil {
			return err
		}
		v, present, err := presentElement(r, source, k)
		if err != nil {
			return err
		}
		if !present {
			continue
		}
		if mapper != nil {
			argv[0], argv[1] = v, Int64Value(k)
			if v, err = r.CallObject(mapper, thisArg, argv); err != nil {
				return err
			}
		}
		if depth > 0 {
			isArr, err := r.isArray(v)
			if err != nil {
				return err
			}
			if isArr {
				inner := v.AsObject()
				n := int64(inner.ArrayLength())
				if inner.class == ClassProxy {
					if n, err = r.LengthOfArrayLike(inner); err != nil {
						return err
					}
				}
				if err := flattenIntoArray(r, target, inner, n, depth-1, nil, Undefined()); err != nil {
					return err
				}
				continue
			}
		}
		if err := target.add(r, v); err != nil {
			return err
		}
	}
	return nil
}

// newFlatTarget implements the ArraySpeciesCreate(o, 0) of flat and
// flatMap.
func newFlatTarget(r *Realm, o *Object, n int64) (flatTarget, error) {
	species, err := r.arraySpeciesCtor(o)
	if err != nil {
		return flatTarget{}, err
	}
	if species != nil {
		a, err := r.speciesCreate(species, 0)
		return flatTarget{obj: a}, err
	}
	return flatTarget{items: make([]Value, 0, flatCapacity(o, n))}, nil
}

func (t *flatTarget) result(r *Realm) Value {
	if t.obj != nil {
		return ObjectValue(t.obj)
	}
	return ObjectValue(r.NewArrayFromSlice(t.items))
}

// arrayProtoFlat implements Array.prototype.flat(depth).
func arrayProtoFlat(r *Realm, this Value, args []Value) (Value, error) {
	o, n, err := arrayLikeThis(r, this)
	if err != nil {
		return Undefined(), err
	}
	depth := 1.0
	if d := Arg(args, 0); !d.IsUndefined() {
		if depth, err = r.ToIntegerOrInfinity(d); err != nil {
			return Undefined(), err
		}
		depth = max(depth, 0)
	}
	t, err := newFlatTarget(r, o, n)
	if err != nil {
		return Undefined(), err
	}
	if err := flattenIntoArray(r, &t, o, n, depth, nil, Undefined()); err != nil {
		return Undefined(), err
	}
	return t.result(r), nil
}

// flatCapacity sizes the result of flat/flatMap over an array-like of
// length n: the dense storage bounds how many elements can be present, so
// a user-controlled length ({length: 2**53-1}, new Array(2**32-1)) never
// turns into an allocation.
func flatCapacity(o *Object, n int64) int {
	return int(min(n, int64(len(o.elements))))
}

// arrayProtoFlatMap implements Array.prototype.flatMap(mapper, thisArg).
func arrayProtoFlatMap(r *Realm, this Value, args []Value) (Value, error) {
	o, n, err := arrayLikeThis(r, this)
	if err != nil {
		return Undefined(), err
	}
	mapper := Arg(args, 0)
	if !IsCallable(mapper) {
		return Undefined(), r.TypeError("flatMap mapper function is not callable")
	}
	t, err := newFlatTarget(r, o, n)
	if err != nil {
		return Undefined(), err
	}
	if err := flattenIntoArray(r, &t, o, n, 1, mapper.AsObject(), Arg(args, 1)); err != nil {
		return Undefined(), err
	}
	return t.result(r), nil
}

// --- iterators ----------------------------------------------------------------------

func arrayProtoKeys(r *Realm, this Value, args []Value) (Value, error) {
	o, err := r.ToObject(this)
	if err != nil {
		return Undefined(), err
	}
	return ObjectValue(r.newArrayIterator(o, IterKeys)), nil
}

func arrayProtoValues(r *Realm, this Value, args []Value) (Value, error) {
	o, err := r.ToObject(this)
	if err != nil {
		return Undefined(), err
	}
	return ObjectValue(r.newArrayIterator(o, IterValues)), nil
}

func arrayProtoEntries(r *Realm, this Value, args []Value) (Value, error) {
	o, err := r.ToObject(this)
	if err != nil {
		return Undefined(), err
	}
	return ObjectValue(r.newArrayIterator(o, IterEntries)), nil
}
