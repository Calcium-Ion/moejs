package engine

// The Annex B accessor methods of Object.prototype (B.2.2.2-B.2.2.5).

// objectProtoDefineGetter implements Object.prototype.__defineGetter__(P,
// getter).
func objectProtoDefineGetter(r *Realm, this Value, args []Value) (Value, error) {
	return defineLegacyAccessor(r, this, args, true)
}

// objectProtoDefineSetter implements Object.prototype.__defineSetter__(P,
// setter).
func objectProtoDefineSetter(r *Realm, this Value, args []Value) (Value, error) {
	return defineLegacyAccessor(r, this, args, false)
}

// defineLegacyAccessor defines an enumerable, configurable accessor half.
func defineLegacyAccessor(r *Realm, this Value, args []Value, getter bool) (Value, error) {
	o, err := r.ToObject(this)
	if err != nil {
		return Undefined(), err
	}
	fn := Arg(args, 1)
	if !IsCallable(fn) {
		return Undefined(), r.TypeError("Object.prototype.__define%s__: Expecting function", legacyAccessorKind(getter))
	}
	var desc PropertyDescriptor
	if getter {
		desc.SetGet(fn)
	} else {
		desc.SetSet(fn)
	}
	desc.SetEnumerable(true)
	desc.SetConfigurable(true)
	key, err := r.ToPropertyKey(Arg(args, 0))
	if err != nil {
		return Undefined(), err
	}
	return Undefined(), o.DefinePropertyOrThrow(r, key, desc)
}

// objectProtoLookupGetter implements Object.prototype.__lookupGetter__(P).
func objectProtoLookupGetter(r *Realm, this Value, args []Value) (Value, error) {
	return lookupLegacyAccessor(r, this, args, true)
}

// objectProtoLookupSetter implements Object.prototype.__lookupSetter__(P).
func objectProtoLookupSetter(r *Realm, this Value, args []Value) (Value, error) {
	return lookupLegacyAccessor(r, this, args, false)
}

// lookupLegacyAccessor walks the prototype chain to the first own property
// named P and returns its getter or setter (undefined for a data property).
func lookupLegacyAccessor(r *Realm, this Value, args []Value, getter bool) (Value, error) {
	o, err := r.ToObject(this)
	if err != nil {
		return Undefined(), err
	}
	key, err := r.ToPropertyKey(Arg(args, 0))
	if err != nil {
		return Undefined(), err
	}
	for i := int64(0); o != nil; i++ {
		d, ok, err := r.getOwnProperty(o, key)
		if err != nil {
			return Undefined(), err
		}
		if ok {
			if !d.IsAccessorDescriptor() {
				return Undefined(), nil
			}
			if getter {
				return d.Get, nil
			}
			return d.Set, nil
		}
		if o, err = r.getPrototypeOf(o); err != nil {
			return Undefined(), err
		}
		if err := interruptEvery(r, i); err != nil {
			return Undefined(), err
		}
	}
	return Undefined(), nil
}

func legacyAccessorKind(getter bool) string {
	if getter {
		return "Getter"
	}
	return "Setter"
}
