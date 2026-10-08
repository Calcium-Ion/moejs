package engine

import "unsafe"

// arrayObject co-allocates an array object with its length payload so that
// creating an array costs one allocation besides its element storage.
type arrayObject struct {
	obj Object
	ad  ArrayData
}

// initArray fills ao as a dense array over items with the realm's array
// shape. The payload lives inside ao; o.internal points at it (an interior
// pointer, which the collector handles like any other).
//
// It charges the array and the capacity of items (memlimit.go), which the
// array owns from now on: element storage is charged once, here or where
// it grows.
func (r *Realm) initArray(ao *arrayObject, items []Value, length uint32) *Object {
	r.chargeObject(unsafe.Sizeof(arrayObject{}) + uintptr(cap(items)*valueSize))
	o := &ao.obj
	o.shape = r.arrayShape()
	o.proto = o.shape.proto
	o.class = ClassArray
	o.flags = flagExtensible
	o.elements = items
	ao.ad = ArrayData{length: length, lengthWritable: true}
	o.internal = &ao.ad
	return o
}

// Small arrays co-allocate their element storage with the object, so an
// array literal, an Object.keys result or a map result of up to
// smallArrayMax elements is one allocation. The buckets land in the 144-,
// 176-, 208- and 240-byte size classes, the same bytes a separate element
// slice would cost.
const smallArrayMax = 8

type arrayObject2 struct {
	arrayObject
	buf [2]Value
}

type arrayObject4 struct {
	arrayObject
	buf [4]Value
}

type arrayObject6 struct {
	arrayObject
	buf [6]Value
}

type arrayObject8 struct {
	arrayObject
	buf [smallArrayMax]Value
}

// newArrayStorage returns an array object and element storage of length n
// with capacity for at least c >= n elements, co-allocated when c is small.
func (r *Realm) newArrayStorage(n, c int) (*arrayObject, []Value) {
	switch {
	case c <= 2:
		x := &arrayObject2{}
		return &x.arrayObject, x.buf[:n:2]
	case c <= 4:
		x := &arrayObject4{}
		return &x.arrayObject, x.buf[:n:4]
	case c <= 6:
		x := &arrayObject6{}
		return &x.arrayObject, x.buf[:n:6]
	case c <= smallArrayMax:
		x := &arrayObject8{}
		return &x.arrayObject, x.buf[:n:smallArrayMax]
	}
	return &arrayObject{}, make([]Value, n, c)
}

// NewArray creates a dense array holding items (the slice is retained).
func (r *Realm) NewArray(items ...Value) *Object {
	return r.NewArrayFromSlice(items)
}

// NewArrayFromSlice creates a dense array that takes ownership of items.
func (r *Realm) NewArrayFromSlice(items []Value) *Object {
	return r.initArray(&arrayObject{}, items, uint32(len(items)))
}

// NewArrayCap creates an empty array with room for n elements (an array
// literal about to be filled by ArrayPush). An empty literal gets room for
// four elements because it is almost always pushed to afterwards.
func (r *Realm) NewArrayCap(n int) *Object {
	if n == 0 {
		n = 4
	}
	ao, items := r.newArrayStorage(0, n)
	return r.initArray(ao, items, 0)
}

// NewArrayLen creates an array of the given length filled with holes; dense
// storage is preallocated only for modest lengths.
func (r *Realm) NewArrayLen(n uint32) *Object {
	if n > denseGrowLimit {
		return r.initArray(&arrayObject{}, nil, n)
	}
	ao, items := r.newArrayStorage(int(n), int(n))
	hole := Hole()
	for i := range items {
		items[i] = hole
	}
	return r.initArray(ao, items, n)
}

// IsArray implements the IsArray abstract operation (there are no proxies yet).
func IsArray(v Value) bool {
	return v.IsObject() && v.AsObject().class == ClassArray
}

// ArrayLength returns the length of an array object (0 for non-arrays).
func (o *Object) ArrayLength() uint32 {
	if o.class != ClassArray {
		return 0
	}
	return o.internal.(*ArrayData).length
}

// Elements exposes dense storage for fast iteration; entries may be holes and
// the slice must not be retained across a call into user code. An
// unmaterialized host array (hostlazy.go) is materialized first.
func (o *Object) Elements() []Value {
	o.materializeHost()
	return o.elements
}

// IsDenseArray reports whether every index < length is present in dense
// storage (no holes, no sparse entries), enabling index-loop fast paths.
func (o *Object) IsDenseArray() bool {
	if o.class != ClassArray {
		return false
	}
	o.materializeHost()
	ad := o.internal.(*ArrayData)
	if int(ad.length) != len(o.elements) || (o.dict != nil && len(o.dict.sparse) != 0) {
		return false
	}
	for _, v := range o.elements {
		if v.IsHole() {
			return false
		}
	}
	return true
}

// Push appends values to an array as CreateDataProperty does (Array.prototype
// .push semantics when no prototype has indexed properties). It returns
// false when length is not writable or the array is not extensible.
func (o *Object) Push(r *Realm, values ...Value) bool {
	if o.flags&flagHasLazy != 0 {
		o.materializeHost()
	}
	ad := o.internal.(*ArrayData)
	if !ad.lengthWritable || !o.IsExtensible() || o.flags&flagShared != 0 {
		return false
	}
	for _, v := range values {
		o.addIndex(r, ad.length, propCell{value: v, attrs: o.elementAttrs()})
	}
	return true
}

// arraySetLength implements ArraySetLength.
func (o *Object) arraySetLength(r *Realm, desc PropertyDescriptor) (bool, error) {
	ad := o.internal.(*ArrayData)
	if !desc.HasValue() {
		cur, _ := o.GetOwnProperty(lengthKey)
		return o.applyLengthAttrs(desc, &cur), nil
	}
	// ArraySetLength converts the value twice (ToUint32, then ToNumber), so a
	// valueOf runs twice and a value that changes between them is rejected.
	newLen, err := r.ToUint32(desc.Value)
	if err != nil {
		return false, err
	}
	numberLen, err := r.ToNumber(desc.Value)
	if err != nil {
		return false, err
	}
	if float64(newLen) != numberLen {
		return false, r.RangeError("Invalid array length")
	}
	// The conversions ran user code, so the old length descriptor is read
	// only now.
	cur, _ := o.GetOwnProperty(lengthKey)
	if newLen >= ad.length {
		if !ad.lengthWritable && newLen != ad.length {
			return false, nil
		}
		if !o.applyLengthAttrs(desc, &cur) {
			return false, nil
		}
		ad.length = newLen
		return true, nil
	}
	if !ad.lengthWritable {
		return false, nil
	}
	newWritable := !desc.HasWritable() || desc.Writable()
	var check PropertyDescriptor
	check.present = desc.present &^ (descHasValue | descHasWritable)
	check.attrs = desc.attrs
	if !ValidateAndApplyPropertyDescriptor(r, nil, lengthKey, o.IsExtensible(), check, &cur) {
		return false, nil
	}
	ok := o.truncate(r, newLen)
	if !newWritable {
		ad.lengthWritable = false
	}
	return ok, nil
}

// applyLengthAttrs validates a length descriptor without a value and applies
// a writable -> false change.
func (o *Object) applyLengthAttrs(desc PropertyDescriptor, cur *PropertyDescriptor) bool {
	if !ValidateAndApplyPropertyDescriptor(nil, nil, lengthKey, o.IsExtensible(), desc, cur) {
		return false
	}
	if desc.HasWritable() && !desc.Writable() {
		o.internal.(*ArrayData).lengthWritable = false
	}
	return true
}

// truncate deletes indices >= newLen from the highest down and sets length.
// It stops at the first non-configurable element (setting length just above
// it) and returns false, as ArraySetLength requires.
func (o *Object) truncate(r *Realm, newLen uint32) bool {
	ad := o.internal.(*ArrayData)
	denseConfigurable := o.elementAttrs()&attrConfigurable != 0
	var sparse []uint32
	if o.dict != nil {
		sparse = o.dict.sparseKeys()
	}
	for i := len(sparse) - 1; i >= 0; i-- {
		k := sparse[i]
		if k < newLen {
			break
		}
		if stop, ok := o.dropDense(int(k)+1, denseConfigurable); !ok {
			ad.length = stop
			r.bumpEpoch(o)
			return false
		}
		if o.dict.sparse[k].attrs&attrConfigurable == 0 {
			ad.length = k + 1
			r.bumpEpoch(o)
			return false
		}
		delete(o.dict.sparse, k)
	}
	if stop, ok := o.dropDense(int(newLen), denseConfigurable); !ok {
		ad.length = stop
		r.bumpEpoch(o)
		return false
	}
	ad.length = newLen
	r.bumpEpoch(o)
	return true
}

// dropDense removes dense elements at indices >= from. When dense elements
// are not configurable and one is present in that range, only the holes above
// the highest present element are removed and (itsIndex+1, false) is returned.
func (o *Object) dropDense(from int, configurable bool) (uint32, bool) {
	if from >= len(o.elements) {
		return 0, true
	}
	if !configurable {
		for i := len(o.elements) - 1; i >= from; i-- {
			if !o.elements[i].IsHole() {
				clear(o.elements[i+1:])
				o.elements = o.elements[:i+1]
				return uint32(i) + 1, false
			}
		}
	}
	clear(o.elements[from:])
	o.elements = o.elements[:from]
	return 0, true
}

// SetLength sets an array's length as `a.length = n` would, returning false
// when rejected (strict callers throw).
func (o *Object) SetLength(r *Realm, n uint32) (bool, error) {
	if o.flags&flagShared != 0 {
		return false, r.sharedWriteError(o, lengthKey)
	}
	if o.flags&flagHasLazy != 0 {
		o.materializeHost()
	}
	var d PropertyDescriptor
	d.SetValue(Uint32Value(n))
	return o.arraySetLength(r, d)
}
