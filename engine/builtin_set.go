package engine

import "math"

// Set, %SetIteratorPrototype% and the ES2025 set methods (§24.2). The
// elements live in a collection (builtin_map_table.go) with Undefined
// values.

var setProtoMethods = []builtinDef{
	{AtomClear, setProtoClear, 0},
	{AtomDelete, setProtoDelete, 1},
	{AtomDifference, setProtoDifference, 1},
	{AtomEntries, setProtoEntries, 0},
	{AtomForEach, setProtoForEach, 1},
	{atomCollHas, setProtoHas, 1},
	{AtomIntersection, setProtoIntersection, 1},
	{AtomIsDisjointFrom, setProtoIsDisjointFrom, 1},
	{AtomIsSubsetOf, setProtoIsSubsetOf, 1},
	{AtomIsSupersetOf, setProtoIsSupersetOf, 1},
	{AtomSymmetricDifference, setProtoSymmetricDifference, 1},
	{AtomUnion, setProtoUnion, 1},
}

var setProtoGetters = []getterDef{newGetterDef(AtomSize, setProtoSize)}

func installSet(r *Realm) {
	r.SetPrototype = r.newIntrinsic(ClassObject, r.ObjectPrototype, r.deferredCap(setProtoProps+1, 1))
	r.SetCtor = r.newConstructor(AtomSetName, 0, setCall, setConstruct, r.SetPrototype)
	r.SetCtor.ReserveSlots(r, 1)
	r.installSpeciesGetter(r.SetCtor)
	r.SetIteratorPrototype = r.newIntrinsic(ClassObject, r.IteratorPrototype, r.deferredCap(2, 0))
	r.installDeferred(r.SetPrototype, installSetPrototype)
	r.bindGlobal(AtomSetName, ObjectValue(r.SetCtor))
}

// setProtoProps counts the properties installSetPrototype defines on
// Set.prototype.
var setProtoProps = len(setProtoMethods) + 6

// installSetPrototype defines Set.prototype and %SetIteratorPrototype%
// (installDeferred).
func installSetPrototype(r *Realm, p *Object) {
	p.ReserveSlots(r, setProtoProps)
	r.setAddFn = r.NewNativeFunction(AtomAdd, 1, setProtoAdd)
	r.installOrReplace(p, StringKey(AtomAdd), propCell{value: ObjectValue(r.setAddFn), attrs: attrHidden})
	r.installBuiltins(p, setProtoMethods)
	r.installGetters(p, setProtoGetters)
	values := ObjectValue(r.NewNativeFunction(AtomValues, 0, setProtoValues))
	r.installOrReplace(p, StringKey(AtomValues), propCell{value: values, attrs: attrHidden})
	r.installOrReplace(p, StringKey(AtomKeys), propCell{value: values, attrs: attrHidden})
	r.installOrReplace(p, iteratorKey, propCell{value: values, attrs: attrHidden})
	r.installToStringTag(p, AtomSetName)

	ip := r.SetIteratorPrototype
	ip.ReserveSlots(r, 2)
	r.setIterNextFn = r.NewNativeFunction(AtomNext, 0, setIteratorNext)
	r.installOrReplace(ip, nextKey, propCell{value: ObjectValue(r.setIterNextFn), attrs: attrHidden})
	r.installToStringTag(ip, AtomSetIterator)
}

// setConstruct implements new Set(iterable).
func setConstruct(r *Realm, args []Value, newTarget *Object) (Value, error) {
	proto, err := r.GetPrototypeFromConstructor(newTarget, r.SetCtor, r.SetPrototype)
	if err != nil {
		return Undefined(), err
	}
	o, c := r.newCollectionObject(proto, ClassSet)
	return ObjectValue(o), r.fillFromValues(o, Arg(args, 0), AtomAdd, &r.setAddFn, func(v Value) error {
		c.add(v)
		return nil
	})
}

func setProtoAdd(r *Realm, this Value, args []Value) (Value, error) {
	c, err := thisCollection(r, this, ClassSet, "Set.prototype.add")
	if err != nil {
		return Undefined(), err
	}
	c.add(Arg(args, 0))
	return this, nil
}

func setProtoClear(r *Realm, this Value, args []Value) (Value, error) {
	c, err := thisCollection(r, this, ClassSet, "Set.prototype.clear")
	if err != nil {
		return Undefined(), err
	}
	c.clear()
	return Undefined(), nil
}

func setProtoDelete(r *Realm, this Value, args []Value) (Value, error) {
	c, err := thisCollection(r, this, ClassSet, "Set.prototype.delete")
	if err != nil {
		return Undefined(), err
	}
	return Bool(c.delete(Arg(args, 0))), nil
}

func setProtoHas(r *Realm, this Value, args []Value) (Value, error) {
	c, err := thisCollection(r, this, ClassSet, "Set.prototype.has")
	if err != nil {
		return Undefined(), err
	}
	return Bool(c.has(Arg(args, 0))), nil
}

func setProtoSize(r *Realm, this Value, args []Value) (Value, error) {
	c, err := thisCollection(r, this, ClassSet, "get Set.prototype.size")
	if err != nil {
		return Undefined(), err
	}
	return IntValue(c.size()), nil
}

func setProtoForEach(r *Realm, this Value, args []Value) (Value, error) {
	c, err := thisCollection(r, this, ClassSet, "Set.prototype.forEach")
	if err != nil {
		return Undefined(), err
	}
	return collectionForEach(r, this, args, c, true)
}

func setProtoEntries(r *Realm, this Value, args []Value) (Value, error) {
	return setIterator(r, this, IterEntries, "Set.prototype.entries")
}

func setProtoValues(r *Realm, this Value, args []Value) (Value, error) {
	return setIterator(r, this, IterValues, "Set.prototype.values")
}

// setIterator implements CreateSetIterator(this, kind).
func setIterator(r *Realm, this Value, kind IterKind, method string) (Value, error) {
	c, err := thisCollection(r, this, ClassSet, method)
	if err != nil {
		return Undefined(), err
	}
	return ObjectValue(r.newCollIterator(r.SetIteratorPrototype, ClassSetIterator, c, kind)), nil
}

// setIteratorNext implements %SetIteratorPrototype%.next.
func setIteratorNext(r *Realm, this Value, args []Value) (Value, error) {
	return collIteratorNext(r, this, ClassSetIterator)
}

// --- set methods --------------------------------------------------------------

// setRecord is a Set Record (§24.2.1.2).
type setRecord struct {
	obj       *Object
	size      float64 // an integer or +∞
	has, keys *Object
}

// getSetRecord implements GetSetRecord(obj).
func (r *Realm) getSetRecord(v Value) (setRecord, error) {
	if !v.IsObject() {
		return setRecord{}, r.TypeError("%s is not a set-like object", r.DisplayString(v))
	}
	o := v.AsObject()
	raw, err := o.GetProp(r, StringKey(AtomSize))
	if err != nil {
		return setRecord{}, err
	}
	n, err := r.ToNumber(raw)
	if err != nil {
		return setRecord{}, err
	}
	if n != n {
		return setRecord{}, r.TypeError("The 'size' property of a set-like object must be a number")
	}
	n = math.Trunc(n)
	if n < 0 {
		return setRecord{}, r.RangeError("The 'size' property of a set-like object must not be negative")
	}
	rec := setRecord{obj: o, size: n}
	for _, m := range [...]struct {
		key *String
		dst **Object
	}{{atomCollHas, &rec.has}, {AtomKeys, &rec.keys}} {
		f, err := o.GetProp(r, StringKey(m.key))
		if err != nil {
			return setRecord{}, err
		}
		if !IsCallable(f) {
			return setRecord{}, r.TypeError("The '%s' property of a set-like object must be a function", m.key.String())
		}
		*m.dst = f.AsObject()
	}
	return rec, nil
}

// keysIterator implements GetIteratorFromMethod(rec.[[SetObject]],
// rec.[[Keys]]).
func (rec *setRecord) keysIterator(r *Realm) (iterRecord, error) {
	it, pos, err := r.iterFromMethod(ObjectValue(rec.obj), ObjectValue(rec.keys))
	return iterRecord{it, pos}, err
}

// hasCall returns ToBoolean(Call(rec.[[Has]], rec.[[SetObject]], «e»)).
func (rec *setRecord) hasCall(r *Realm, argv []Value, e Value) (bool, error) {
	argv[0] = e
	v, err := r.CallObject(rec.has, ObjectValue(rec.obj), argv)
	return ToBoolean(v), err
}

// forEachKey steps the keys iterator of rec, passing each canonicalized
// value to f until f reports stop (the iterator is then closed) or the
// iterator is exhausted.
func (rec *setRecord) forEachKey(r *Realm, f func(v Value) (stop bool)) (stopped bool, err error) {
	ir, err := rec.keysIterator(r)
	if err != nil {
		return false, err
	}
	for k := int64(0); ; k++ {
		if err := interruptEvery(r, k); err != nil {
			return false, err
		}
		v, done, err := ir.step(r)
		if err != nil || done {
			return false, err
		}
		if f(canonicalCollKey(v)) {
			return true, ir.close(r)
		}
	}
}

// forEachThis walks this set's live list, calling rec.[[Has]] on each
// element until f reports stop.
func (rec *setRecord) forEachThis(r *Realm, c *collection, f func(e Value, inOther bool) (stop bool)) error {
	argv := r.pushArgs(1)
	defer r.popArgs(1)
	cur := c.cursor()
	for k := int64(0); ; k++ {
		if err := interruptEvery(r, k); err != nil {
			return err
		}
		e, _, ok := cur.next()
		if !ok {
			return nil
		}
		in, err := rec.hasCall(r, argv, e)
		if err != nil {
			return err
		}
		if f(e, in) {
			return nil
		}
	}
}

// setMethodArgs resolves the receiver and GetSetRecord(other).
func setMethodArgs(r *Realm, this Value, args []Value, method string) (*collection, setRecord, error) {
	c, err := thisCollection(r, this, ClassSet, method)
	if err != nil {
		return nil, setRecord{}, err
	}
	rec, err := r.getSetRecord(Arg(args, 0))
	return c, rec, err
}

func (r *Realm) newSetResult() (*Object, *collection) {
	return r.newCollectionObject(r.SetPrototype, ClassSet)
}

func setProtoUnion(r *Realm, this Value, args []Value) (Value, error) {
	c, rec, err := setMethodArgs(r, this, args, "Set.prototype.union")
	if err != nil {
		return Undefined(), err
	}
	ir, err := rec.keysIterator(r)
	if err != nil {
		return Undefined(), err
	}
	o, res := r.newSetResult()
	res.copyFrom(c)
	for k := int64(0); ; k++ {
		if err := interruptEvery(r, k); err != nil {
			return Undefined(), err
		}
		v, done, err := ir.step(r)
		if err != nil {
			return Undefined(), err
		}
		if done {
			return ObjectValue(o), nil
		}
		res.add(v)
	}
}

func setProtoIntersection(r *Realm, this Value, args []Value) (Value, error) {
	c, rec, err := setMethodArgs(r, this, args, "Set.prototype.intersection")
	if err != nil {
		return Undefined(), err
	}
	o, res := r.newSetResult()
	if float64(c.size()) <= rec.size {
		err = rec.forEachThis(r, c, func(e Value, in bool) bool {
			if in {
				res.add(e)
			}
			return false
		})
	} else {
		_, err = rec.forEachKey(r, func(v Value) bool {
			if c.has(v) {
				res.add(v)
			}
			return false
		})
	}
	if err != nil {
		return Undefined(), err
	}
	return ObjectValue(o), nil
}

func setProtoDifference(r *Realm, this Value, args []Value) (Value, error) {
	c, rec, err := setMethodArgs(r, this, args, "Set.prototype.difference")
	if err != nil {
		return Undefined(), err
	}
	o, res := r.newSetResult()
	res.copyFrom(c)
	if float64(c.size()) <= rec.size {
		// The walk is over the copy (which only this loop mutates): the
		// spec reads resultSetData[index] up to the original size.
		err = rec.forEachThis(r, res, func(e Value, in bool) bool {
			if in {
				res.delete(e)
			}
			return false
		})
	} else {
		_, err = rec.forEachKey(r, func(v Value) bool {
			res.delete(v)
			return false
		})
	}
	if err != nil {
		return Undefined(), err
	}
	return ObjectValue(o), nil
}

func setProtoSymmetricDifference(r *Realm, this Value, args []Value) (Value, error) {
	c, rec, err := setMethodArgs(r, this, args, "Set.prototype.symmetricDifference")
	if err != nil {
		return Undefined(), err
	}
	ir, err := rec.keysIterator(r)
	if err != nil {
		return Undefined(), err
	}
	o, res := r.newSetResult()
	res.copyFrom(c)
	for k := int64(0); ; k++ {
		if err := interruptEvery(r, k); err != nil {
			return Undefined(), err
		}
		v, done, err := ir.step(r)
		if err != nil {
			return Undefined(), err
		}
		if done {
			return ObjectValue(o), nil
		}
		v = canonicalCollKey(v)
		if c.has(v) {
			res.delete(v)
		} else {
			res.add(v)
		}
	}
}

func setProtoIsSubsetOf(r *Realm, this Value, args []Value) (Value, error) {
	c, rec, err := setMethodArgs(r, this, args, "Set.prototype.isSubsetOf")
	if err != nil {
		return Undefined(), err
	}
	if float64(c.size()) > rec.size {
		return False(), nil
	}
	result := true
	err = rec.forEachThis(r, c, func(e Value, in bool) bool {
		result = in
		return !in
	})
	return Bool(result), err
}

func setProtoIsSupersetOf(r *Realm, this Value, args []Value) (Value, error) {
	c, rec, err := setMethodArgs(r, this, args, "Set.prototype.isSupersetOf")
	if err != nil {
		return Undefined(), err
	}
	if float64(c.size()) < rec.size {
		return False(), nil
	}
	stopped, err := rec.forEachKey(r, func(v Value) bool { return !c.has(v) })
	return Bool(!stopped), err
}

func setProtoIsDisjointFrom(r *Realm, this Value, args []Value) (Value, error) {
	c, rec, err := setMethodArgs(r, this, args, "Set.prototype.isDisjointFrom")
	if err != nil {
		return Undefined(), err
	}
	if float64(c.size()) <= rec.size {
		result := true
		err = rec.forEachThis(r, c, func(e Value, in bool) bool {
			result = !in
			return in
		})
		return Bool(result), err
	}
	stopped, err := rec.forEachKey(r, func(v Value) bool { return c.has(v) })
	return Bool(!stopped), err
}
