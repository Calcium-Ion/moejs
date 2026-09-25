package engine

// installReflect creates the Reflect namespace object (ES2023 28.1) and
// binds it on the global object. Every method is the corresponding internal
// method of its target, so a proxy target runs its trap.
func installReflect(r *Realm) {
	o := r.newIntrinsic(ClassObject, r.ObjectPrototype, r.deferredCap(len(reflectFunctions)+1, 0))
	o.flags &^= flagIsPrototype
	r.installKeys(o, reflectKeys)
	r.defineGlobalBinding(AtomReflect, ObjectValue(o))
}

var reflectKeys = newKeyTable(nil, reflectFunctions, AtomReflect)

var reflectFunctions = []builtinDef{
	{AtomApply, reflectApply, 3},
	{AtomConstruct, reflectConstruct, 2},
	{AtomDefineProperty, reflectDefineProperty, 3},
	{AtomDeleteProperty, reflectDeleteProperty, 2},
	{AtomGet, reflectGet, 2},
	{AtomGetOwnPropertyDescriptor, reflectGetOwnPropertyDescriptor, 2},
	{AtomGetPrototypeOf, reflectGetPrototypeOf, 1},
	{AtomHas, reflectHas, 2},
	{AtomIsExtensible, reflectIsExtensible, 1},
	{AtomOwnKeys, reflectOwnKeys, 1},
	{AtomPreventExtensions, reflectPreventExtensions, 1},
	{AtomSet, reflectSet, 3},
	{AtomSetPrototypeOf, reflectSetPrototypeOf, 2},
}

// defineGlobalBinding adds a global binding during installation: read-only
// in the shared template (lockdown model, see initGlobal), writable and
// configurable in mutable realms.
func (r *Realm) defineGlobalBinding(name *String, v Value) {
	attrs := attrHidden
	if r.buildingShared {
		attrs = attrFrozen
	}
	r.Global.ReserveSlots(r, 1)
	r.Global.DefineOwnDataFast(r, StringKey(name), v, attrs)
}

// reflectTarget returns the first argument when it is an object; every
// Reflect method except apply and construct requires one.
func reflectTarget(r *Realm, args []Value, method string) (*Object, error) {
	t := Arg(args, 0)
	if !t.IsObject() {
		return nil, r.TypeError("Reflect.%s called on non-object", method)
	}
	return t.AsObject(), nil
}

// reflectTargetKey is reflectTarget followed by ToPropertyKey of the second
// argument, in the spec's order.
func reflectTargetKey(r *Realm, args []Value, method string) (*Object, PropertyKey, error) {
	t, err := reflectTarget(r, args, method)
	if err != nil {
		return nil, PropertyKey{}, err
	}
	key, err := r.ToPropertyKey(Arg(args, 1))
	return t, key, err
}

// keyValue converts a property key to the value [[OwnPropertyKeys]]
// exposes: a string for index and string keys, the symbol otherwise.
func keyValue(r *Realm, k PropertyKey) Value {
	if k.IsSymbol() {
		return SymbolValue(k.Symbol())
	}
	return StringValue(k.ToJSString(r))
}

// reflectApply implements Reflect.apply(target, thisArgument, argumentsList).
func reflectApply(r *Realm, this Value, args []Value) (Value, error) {
	target := Arg(args, 0)
	if !IsCallable(target) {
		return Undefined(), r.TypeError("Reflect.apply target %s is not a function", r.DisplayString(target))
	}
	list, err := r.CreateListFromArrayLike(Arg(args, 2))
	if err != nil {
		return Undefined(), err
	}
	return r.CallObject(target.AsObject(), Arg(args, 1), list)
}

// reflectConstruct implements Reflect.construct(target, argumentsList,
// newTarget); newTarget defaults to target and must be a constructor.
func reflectConstruct(r *Realm, this Value, args []Value) (Value, error) {
	target := Arg(args, 0)
	if !IsConstructor(target) {
		return Undefined(), r.TypeError("Reflect.construct target %s is not a constructor", r.DisplayString(target))
	}
	newTarget := target
	if len(args) > 2 {
		if newTarget = args[2]; !IsConstructor(newTarget) {
			return Undefined(), r.TypeError("Reflect.construct newTarget %s is not a constructor", r.DisplayString(newTarget))
		}
	}
	list, err := r.CreateListFromArrayLike(Arg(args, 1))
	if err != nil {
		return Undefined(), err
	}
	return r.Construct(target, list, newTarget.AsObject())
}

// reflectDefineProperty implements Reflect.defineProperty: the result of
// [[DefineOwnProperty]] instead of a TypeError.
func reflectDefineProperty(r *Realm, this Value, args []Value) (Value, error) {
	t, key, err := reflectTargetKey(r, args, "defineProperty")
	if err != nil {
		return Undefined(), err
	}
	desc, err := r.toPropertyDescriptor(Arg(args, 2))
	if err != nil {
		return Undefined(), err
	}
	ok, err := t.DefineOwnProperty(r, key, desc)
	return Bool(ok), err
}

// reflectDeleteProperty implements Reflect.deleteProperty.
func reflectDeleteProperty(r *Realm, this Value, args []Value) (Value, error) {
	t, key, err := reflectTargetKey(r, args, "deleteProperty")
	if err != nil {
		return Undefined(), err
	}
	ok, err := r.deleteProperty(t, key)
	return Bool(ok), err
}

// reflectGet implements Reflect.get(target, key, receiver); the receiver
// defaults to target.
func reflectGet(r *Realm, this Value, args []Value) (Value, error) {
	t, key, err := reflectTargetKey(r, args, "get")
	if err != nil {
		return Undefined(), err
	}
	receiver := ObjectValue(t)
	if len(args) > 2 {
		receiver = args[2]
	}
	return t.Get(r, key, receiver)
}

// reflectGetOwnPropertyDescriptor implements
// Reflect.getOwnPropertyDescriptor.
func reflectGetOwnPropertyDescriptor(r *Realm, this Value, args []Value) (Value, error) {
	t, key, err := reflectTargetKey(r, args, "getOwnPropertyDescriptor")
	if err != nil {
		return Undefined(), err
	}
	d, ok, err := r.getOwnProperty(t, key)
	if err != nil || !ok {
		return Undefined(), err
	}
	return r.FromPropertyDescriptor(d), nil
}

// reflectGetPrototypeOf implements Reflect.getPrototypeOf.
func reflectGetPrototypeOf(r *Realm, this Value, args []Value) (Value, error) {
	t, err := reflectTarget(r, args, "getPrototypeOf")
	if err != nil {
		return Undefined(), err
	}
	proto, err := r.getPrototypeOf(t)
	if err != nil || proto == nil {
		return Null(), err
	}
	return ObjectValue(proto), nil
}

// reflectHas implements Reflect.has.
func reflectHas(r *Realm, this Value, args []Value) (Value, error) {
	t, key, err := reflectTargetKey(r, args, "has")
	if err != nil {
		return Undefined(), err
	}
	ok, err := r.hasProperty(t, key)
	return Bool(ok), err
}

// reflectIsExtensible implements Reflect.isExtensible.
func reflectIsExtensible(r *Realm, this Value, args []Value) (Value, error) {
	t, err := reflectTarget(r, args, "isExtensible")
	if err != nil {
		return Undefined(), err
	}
	ok, err := r.isExtensible(t)
	return Bool(ok), err
}

// reflectOwnKeys implements Reflect.ownKeys: integer keys ascending, then
// strings, then symbols, each in insertion order ([[OwnPropertyKeys]]).
func reflectOwnKeys(r *Realm, this Value, args []Value) (Value, error) {
	t, err := reflectTarget(r, args, "ownKeys")
	if err != nil {
		return Undefined(), err
	}
	keys, err := r.ownPropertyKeys(t)
	if err != nil {
		return Undefined(), err
	}
	ao, items := r.newArrayStorage(len(keys), len(keys))
	for i, k := range keys {
		items[i] = keyValue(r, k)
	}
	return ObjectValue(r.initArray(ao, items, uint32(len(keys)))), nil
}

// reflectPreventExtensions implements Reflect.preventExtensions (always
// true for ordinary objects).
func reflectPreventExtensions(r *Realm, this Value, args []Value) (Value, error) {
	t, err := reflectTarget(r, args, "preventExtensions")
	if err != nil {
		return Undefined(), err
	}
	ok, err := r.preventExtensions(t)
	return Bool(ok), err
}

// reflectSet implements Reflect.set(target, key, value, receiver): the
// result of [[Set]] instead of a TypeError. The receiver defaults to target.
func reflectSet(r *Realm, this Value, args []Value) (Value, error) {
	t, key, err := reflectTargetKey(r, args, "set")
	if err != nil {
		return Undefined(), err
	}
	receiver := ObjectValue(t)
	if len(args) > 3 {
		receiver = args[3]
	}
	ok, err := t.Set(r, key, Arg(args, 2), receiver)
	return Bool(ok), err
}

// reflectSetPrototypeOf implements Reflect.setPrototypeOf: false instead of
// a TypeError when [[SetPrototypeOf]] refuses.
func reflectSetPrototypeOf(r *Realm, this Value, args []Value) (Value, error) {
	t, err := reflectTarget(r, args, "setPrototypeOf")
	if err != nil {
		return Undefined(), err
	}
	proto, err := protoArgument(r, Arg(args, 1))
	if err != nil {
		return Undefined(), err
	}
	ok, err := r.setPrototypeOf(t, proto)
	return Bool(ok), err
}
