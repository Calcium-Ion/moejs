package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestObjectBuiltinShape(t *testing.T) {
	r := NewRealm()
	ctor := ObjectValue(r.ObjectCtor)
	assertNativeShape(t, r, ctor, "Object", 1)
	for _, c := range []struct {
		name   string
		length int
	}{
		{"assign", 2}, {"create", 2}, {"defineProperties", 2}, {"defineProperty", 3}, {"entries", 1}, {"freeze", 1}, {"fromEntries", 1},
		{"getOwnPropertyDescriptor", 2}, {"getOwnPropertyDescriptors", 1}, {"getOwnPropertyNames", 1}, {"getOwnPropertySymbols", 1}, {"getPrototypeOf", 1},
		{"groupBy", 2}, {"hasOwn", 2}, {"is", 2}, {"isExtensible", 1}, {"isFrozen", 1}, {"isSealed", 1}, {"keys", 1}, {"preventExtensions", 1}, {"seal", 1},
		{"setPrototypeOf", 2}, {"values", 1},
	} {
		assertNativeShape(t, r, jsGet(t, r, ctor, c.name), c.name, c.length)
		assertOwnAttrs(t, r, r.ObjectCtor, c.name, true, false, true)
	}
	proto := ObjectValue(r.ObjectPrototype)
	for _, c := range []struct {
		name   string
		length int
	}{{"__defineGetter__", 2}, {"__defineSetter__", 2}, {"__lookupGetter__", 1}, {"__lookupSetter__", 1},
		{"hasOwnProperty", 1}, {"isPrototypeOf", 1}, {"propertyIsEnumerable", 1}, {"toLocaleString", 0}, {"toString", 0}, {"valueOf", 0}} {
		assertNativeShape(t, r, jsGet(t, r, proto, c.name), c.name, c.length)
		assertOwnAttrs(t, r, r.ObjectPrototype, c.name, true, false, true)
	}
	assert.Empty(t, r.ObjectPrototype.OwnEnumerableStringKeys())
	assert.Nil(t, r.ObjectPrototype.Proto())
}

func TestObjectConstructor(t *testing.T) {
	r := NewRealm()
	ctor := ObjectValue(r.ObjectCtor)
	for _, v := range []Value{Undefined(), Null()} {
		o, err := r.Call(ctor, Undefined(), []Value{v})
		require.NoError(t, err)
		assert.Equal(t, ClassObject, o.AsObject().Class())
		assert.Same(t, r.ObjectPrototype, o.AsObject().Proto())
	}
	o, _ := r.Call(ctor, Undefined(), nil)
	assert.Equal(t, ClassObject, o.AsObject().Class())
	w, _ := r.Call(ctor, Undefined(), []Value{str("ab")})
	assert.Equal(t, ClassString, w.AsObject().Class())
	assert.Equal(t, IntValue(2), jsGet(t, r, w, "length"))
	existing := r.NewObject()
	same, _ := r.Construct(ctor, []Value{ObjectValue(existing)}, nil)
	assert.Same(t, existing, same.AsObject())
	n, _ := r.Construct(ctor, []Value{IntValue(3)}, nil)
	assert.Equal(t, ClassNumber, n.AsObject().Class())
	nt := r.NewNativeFunction(AtomEmpty, 0, nil)
	custom := r.NewObject()
	nt.DefineOwnDataFast(r, StringKey(AtomPrototype), ObjectValue(custom), attrHidden)
	sub, err := r.Construct(ctor, []Value{IntValue(3)}, nt)
	require.NoError(t, err)
	assert.Same(t, custom, sub.AsObject().Proto())
	assert.Equal(t, ClassObject, sub.AsObject().Class(), "subclass newTarget ignores the value")
}

// TestListedIndexKeysAreIndices: the index names key listing returns (cached
// per realm for small indices) find the elements again when used as keys.
func TestListedIndexKeysAreIndices(t *testing.T) {
	assert.Equal(t, "a,b|a,b|0,1|true,true|0:a,1:b", evalExpr(t, `(() => {
		const a = ["a", "b"], out = [];
		const ks = Object.keys(a), names = Object.getOwnPropertyNames(a);
		const o = {};
		for (const k in a) { out.push(a[k]); o[k] = a[k]; }
		return [ks.map(k => a[k]), out, Object.keys(o), [ks[0] in a, a.hasOwnProperty(names[1])],
			names.slice(0, 2).map(k => k + ":" + Object.getOwnPropertyDescriptor(a, k).value)].join("|");
	})()`))
}

func TestObjectKeysValuesEntries(t *testing.T) {
	r := NewRealm()
	ctor := ObjectValue(r.ObjectCtor)
	o := r.NewObject()
	for _, n := range []string{"b", "2", "a", "10", "1"} {
		mustSet(t, r, o, n, str("v"+n))
	}
	o.DefineOwnDataFast(r, key(r, "hidden"), IntValue(0), attrHidden)
	o.DefineOwnDataFast(r, SymbolKey(SymIterator), IntValue(0), attrDefault)
	assert.Equal(t, []any{"1", "2", "10", "b", "a"}, r.ToGo(jsCall(t, r, ctor, "keys", ObjectValue(o))))
	assert.Equal(t, []any{"v1", "v2", "v10", "vb", "va"}, r.ToGo(jsCall(t, r, ctor, "values", ObjectValue(o))))
	assert.Equal(t, []any{[]any{"1", "v1"}, []any{"2", "v2"}, []any{"10", "v10"}, []any{"b", "vb"}, []any{"a", "va"}}, r.ToGo(jsCall(t, r, ctor, "entries", ObjectValue(o))))
	// Shape-only fast path and dictionary mode agree.
	plain := r.NewObject()
	mustSet(t, r, plain, "x", IntValue(1))
	mustSet(t, r, plain, "y", IntValue(2))
	plain.DefineOwnDataFast(r, key(r, "z"), IntValue(3), attrHidden)
	assert.Equal(t, []any{"x", "y"}, r.ToGo(jsCall(t, r, ctor, "keys", ObjectValue(plain))))
	assert.True(t, plain.Delete(r, key(r, "x")))
	assert.True(t, plain.IsDictionaryMode())
	assert.Equal(t, []any{"y"}, r.ToGo(jsCall(t, r, ctor, "keys", ObjectValue(plain))))
	assert.Equal(t, []any{int64(2)}, r.ToGo(jsCall(t, r, ctor, "values", ObjectValue(plain))))
	// Primitives are wrapped: strings expose indices, numbers nothing.
	assert.Equal(t, []any{"0", "1"}, r.ToGo(jsCall(t, r, ctor, "keys", str("ab"))))
	assert.Equal(t, []any{"a", "b"}, r.ToGo(jsCall(t, r, ctor, "values", str("ab"))))
	assert.Equal(t, []any{[]any{"0", "a"}}, r.ToGo(jsCall(t, r, ctor, "entries", str("a"))))
	assert.Equal(t, []any{}, r.ToGo(jsCall(t, r, ctor, "keys", IntValue(5))))
	assert.Equal(t, []any{"0", "1"}, r.ToGo(jsCall(t, r, ctor, "keys", jsArray(r, IntValue(7), IntValue(8)))))
	holes := r.NewArrayLen(3)
	mustSet(t, r, holes, "1", IntValue(1))
	assert.Equal(t, []any{"1"}, r.ToGo(jsCall(t, r, ctor, "keys", ObjectValue(holes))))
	for _, bad := range []Value{Undefined(), Null()} {
		for _, m := range []string{"keys", "values", "entries"} {
			assert.EqualError(t, jsCallErr(t, r, ctor, m, bad), "TypeError: Cannot convert undefined or null to object")
		}
	}
	assert.EqualError(t, jsCallErr(t, r, ctor, "keys"), "TypeError: Cannot convert undefined or null to object")
	// A getter that deletes a later key: the later key is skipped.
	g := r.NewObject()
	g.DefineOwnAccessorFast(r, key(r, "first"), nativeFn(r, func(this Value, _ []Value) (Value, error) {
		this.AsObject().Delete(r, key(r, "second"))
		return IntValue(1), nil
	}).AsObject(), nil, attrEnumerable|attrConfigurable)
	mustSet(t, r, g, "second", IntValue(2))
	assert.Equal(t, []any{int64(1)}, r.ToGo(jsCall(t, r, ctor, "values", ObjectValue(g))))
	mustSet(t, r, g, "second", IntValue(2))
	assert.Equal(t, []any{[]any{"first", int64(1)}}, r.ToGo(jsCall(t, r, ctor, "entries", ObjectValue(g))))
	// Getter errors propagate.
	e := r.NewObject()
	e.DefineOwnAccessorFast(r, key(r, "boom"), throwingFn(r, "getter").AsObject(), nil, attrEnumerable)
	assert.EqualError(t, jsCallErr(t, r, ctor, "values", ObjectValue(e)), "TypeError: getter")
	// Entries are fresh, independent arrays.
	entries := jsCall(t, r, ctor, "entries", ObjectValue(plain)).AsObject()
	first, _ := entries.GetIndex(r, 0)
	assert.True(t, first.AsObject().Push(r, IntValue(9)))
	assert.Equal(t, []any{"y", int64(2), int64(9)}, r.ToGo(first))
}

func TestObjectAssign(t *testing.T) {
	r := NewRealm()
	ctor := ObjectValue(r.ObjectCtor)
	target := r.NewObject()
	mustSet(t, r, target, "a", IntValue(1))
	src1 := r.NewObject()
	mustSet(t, r, src1, "b", IntValue(2))
	mustSet(t, r, src1, "a", IntValue(10))
	src1.DefineOwnDataFast(r, key(r, "hidden"), IntValue(0), attrHidden)
	src1.DefineOwnDataFast(r, SymbolKey(SymIterator), str("sym"), attrDefault)
	src2 := r.NewObject()
	mustSet(t, r, src2, "c", IntValue(3))
	mustSet(t, r, src2, "0", IntValue(0))
	res := jsCall(t, r, ctor, "assign", ObjectValue(target), ObjectValue(src1), Null(), Undefined(), ObjectValue(src2))
	assert.Same(t, target, res.AsObject())
	assert.Equal(t, []string{"0", "a", "b", "c", "Symbol(Symbol.iterator)"}, keyNames(target.OwnPropertyKeys()))
	assert.Equal(t, "sym", jsString(t, func() Value { v, _ := target.GetProp(r, SymbolKey(SymIterator)); return v }()))
	assert.Equal(t, map[string]any{"0": int64(0), "a": int64(10), "b": int64(2), "c": int64(3)}, r.ToGo(ObjectValue(target)))
	assert.False(t, target.HasOwnProperty(key(r, "hidden")))
	// Strings spread their indices; other primitives contribute nothing.
	t2 := jsCall(t, r, ctor, "assign", ObjectValue(r.NewObject()), str("xy"), IntValue(5), True())
	assert.Equal(t, map[string]any{"0": "x", "1": "y"}, r.ToGo(t2))
	// Arrays as sources copy their elements.
	t3 := jsCall(t, r, ctor, "assign", ObjectValue(r.NewObject()), jsArray(r, str("p"), str("q")))
	assert.Equal(t, map[string]any{"0": "p", "1": "q"}, r.ToGo(t3))
	// Getters on the source are invoked; setters on the target receive values.
	var log []string
	src := r.NewObject()
	src.DefineOwnAccessorFast(r, key(r, "g"), nativeFn(r, func(Value, []Value) (Value, error) {
		log = append(log, "get g")
		return IntValue(7), nil
	}).AsObject(), nil, attrEnumerable|attrConfigurable)
	tgt := r.NewObject()
	tgt.DefineOwnAccessorFast(r, key(r, "g"), nil, nativeFn(r, func(_ Value, args []Value) (Value, error) {
		log = append(log, "set g="+args[0].String())
		return Undefined(), nil
	}).AsObject(), attrEnumerable|attrConfigurable)
	jsCall(t, r, ctor, "assign", ObjectValue(tgt), ObjectValue(src))
	assert.Equal(t, []string{"get g", "set g=7"}, log)
	// A rejected [[Set]] is a TypeError (frozen target, read-only property).
	frozen := r.NewObject()
	mustSet(t, r, frozen, "a", IntValue(1))
	frozen.Freeze(r)
	err := jsCallErr(t, r, ctor, "assign", ObjectValue(frozen), ObjectValue(src1))
	assert.EqualError(t, err, "TypeError: Cannot assign to read only property 'b' of object")
	ro := r.NewObject()
	ro.DefineOwnDataFast(r, key(r, "a"), IntValue(1), attrEnumerable)
	assert.ErrorContains(t, jsCallErr(t, r, ctor, "assign", ObjectValue(ro), ObjectValue(src1)), "read only property 'a'")
	// Primitive targets are wrapped and returned as the wrapper.
	w := jsCall(t, r, ctor, "assign", IntValue(1), ObjectValue(src2))
	assert.Equal(t, ClassNumber, w.AsObject().Class())
	assert.Equal(t, IntValue(3), jsGet(t, r, w, "c"))
	for _, bad := range []Value{Undefined(), Null()} {
		assert.EqualError(t, jsCallErr(t, r, ctor, "assign", bad), "TypeError: Cannot convert undefined or null to object")
	}
	assert.Equal(t, ClassObject, jsCall(t, r, ctor, "assign", ObjectValue(r.NewObject())).AsObject().Class())
	// Dictionary-mode sources work through the generic path and keep order.
	dict := r.NewObject()
	for _, n := range []string{"k1", "k2", "k3"} {
		mustSet(t, r, dict, n, str(n))
	}
	dict.Delete(r, key(r, "k1"))
	mustSet(t, r, dict, "k1", str("again"))
	t4 := jsCall(t, r, ctor, "assign", ObjectValue(r.NewObject()), ObjectValue(dict))
	assert.Equal(t, []string{"k2", "k3", "k1"}, keyNames(t4.AsObject().OwnEnumerableStringKeys()))
	// Source getters that throw propagate.
	assert.EqualError(t, jsCallErr(t, r, ctor, "assign", ObjectValue(r.NewObject()), func() Value {
		e := r.NewObject()
		e.DefineOwnAccessorFast(r, key(r, "x"), throwingFn(r, "src").AsObject(), nil, attrEnumerable)
		return ObjectValue(e)
	}()), "TypeError: src")
}

func TestObjectFreezeAndPrototypes(t *testing.T) {
	r := NewRealm()
	ctor := ObjectValue(r.ObjectCtor)
	o := r.NewObject()
	mustSet(t, r, o, "a", IntValue(1))
	assert.Equal(t, False(), jsCall(t, r, ctor, "isFrozen", ObjectValue(o)))
	assert.Same(t, o, jsCall(t, r, ctor, "freeze", ObjectValue(o)).AsObject())
	assert.Equal(t, True(), jsCall(t, r, ctor, "isFrozen", ObjectValue(o)))
	assert.EqualError(t, o.SetProp(r, key(r, "a"), IntValue(2)), "TypeError: Cannot assign to read only property 'a' of object")
	assert.EqualError(t, o.SetProp(r, key(r, "b"), IntValue(2)), "TypeError: Cannot assign to read only property 'b' of object")
	assert.False(t, o.Delete(r, key(r, "a")))
	assert.Equal(t, IntValue(1), jsGet(t, r, ObjectValue(o), "a"))
	arr := r.NewArray(IntValue(1))
	jsCall(t, r, ctor, "freeze", ObjectValue(arr))
	assert.False(t, arr.Push(r, IntValue(2)))
	assert.True(t, arr.IsFrozen())
	for _, prim := range []Value{IntValue(1), str("s"), Undefined(), Null(), True()} {
		assert.Equal(t, prim, jsCall(t, r, ctor, "freeze", prim))
		assert.Equal(t, True(), jsCall(t, r, ctor, "isFrozen", prim))
	}
	assert.Equal(t, False(), jsCall(t, r, ctor, "isFrozen", ObjectValue(r.NewObject())))
	empty := r.NewObject()
	empty.PreventExtensions(r)
	assert.Equal(t, True(), jsCall(t, r, ctor, "isFrozen", ObjectValue(empty)))
	fn := nativeFn(r, func(Value, []Value) (Value, error) { return Undefined(), nil })
	jsCall(t, r, ctor, "freeze", fn)
	assert.True(t, fn.AsObject().IsFrozen())

	// getPrototypeOf / setPrototypeOf.
	assert.Same(t, r.ObjectPrototype, jsCall(t, r, ctor, "getPrototypeOf", ObjectValue(r.NewObject())).AsObject())
	assert.Same(t, r.StringPrototype, jsCall(t, r, ctor, "getPrototypeOf", str("s")).AsObject())
	assert.Same(t, r.NumberPrototype, jsCall(t, r, ctor, "getPrototypeOf", IntValue(1)).AsObject())
	assert.Same(t, r.FunctionPrototype, jsCall(t, r, ctor, "getPrototypeOf", ctor).AsObject())
	assert.True(t, jsCall(t, r, ctor, "getPrototypeOf", ObjectValue(r.ObjectPrototype)).IsNull())
	assert.EqualError(t, jsCallErr(t, r, ctor, "getPrototypeOf", Null()), "TypeError: Cannot convert undefined or null to object")
	p := r.NewObject()
	mustSet(t, r, p, "inherited", IntValue(42))
	c := r.NewObject()
	assert.Same(t, c, jsCall(t, r, ctor, "setPrototypeOf", ObjectValue(c), ObjectValue(p)).AsObject())
	assert.Same(t, p, c.Proto())
	assert.Equal(t, IntValue(42), jsGet(t, r, ObjectValue(c), "inherited"))
	jsCall(t, r, ctor, "setPrototypeOf", ObjectValue(c), Null())
	assert.Nil(t, c.Proto())
	assert.Equal(t, IntValue(7), jsCall(t, r, ctor, "setPrototypeOf", IntValue(7), Null()), "primitives pass through")
	assert.EqualError(t, jsCallErr(t, r, ctor, "setPrototypeOf", Undefined(), Null()), "TypeError: Object.setPrototypeOf called on null or undefined")
	assert.EqualError(t, jsCallErr(t, r, ctor, "setPrototypeOf", ObjectValue(c), IntValue(1)), "TypeError: Object prototype may only be an Object or null: 1")
	assert.EqualError(t, jsCallErr(t, r, ctor, "setPrototypeOf", ObjectValue(c), Undefined()), "TypeError: Object prototype may only be an Object or null: undefined")
	assert.EqualError(t, jsCallErr(t, r, ctor, "setPrototypeOf", ObjectValue(p), ObjectValue(p)), "TypeError: Cyclic __proto__ value")
	jsCall(t, r, ctor, "setPrototypeOf", ObjectValue(c), ObjectValue(p))
	assert.EqualError(t, jsCallErr(t, r, ctor, "setPrototypeOf", ObjectValue(p), ObjectValue(c)), "TypeError: Cyclic __proto__ value")
	assert.EqualError(t, jsCallErr(t, r, ctor, "setPrototypeOf", ObjectValue(o), Null()), "TypeError: [object Object] is not extensible")
	assert.Same(t, o, jsCall(t, r, ctor, "setPrototypeOf", ObjectValue(o), ObjectValue(r.ObjectPrototype)).AsObject(), "unchanged prototype is fine on frozen objects")
}

func TestObjectCreateAndDefineProperty(t *testing.T) {
	r := NewRealm()
	ctor := ObjectValue(r.ObjectCtor)
	nul := jsCall(t, r, ctor, "create", Null())
	assert.Nil(t, nul.AsObject().Proto())
	assert.True(t, jsGet(t, r, nul, "toString").IsUndefined())
	p := r.NewObject()
	mustSet(t, r, p, "x", IntValue(1))
	// Descriptor objects.
	desc := func(fields map[string]Value) Value {
		d := r.NewObject()
		for _, k := range []string{"value", "writable", "enumerable", "configurable", "get", "set"} {
			if v, ok := fields[k]; ok {
				mustSet(t, r, d, k, v)
			}
		}
		return ObjectValue(d)
	}
	props := r.NewObject()
	mustSet(t, r, props, "a", desc(map[string]Value{"value": IntValue(1), "enumerable": True()}))
	mustSet(t, r, props, "b", desc(map[string]Value{"value": IntValue(2)}))
	var log []string
	getter := nativeFn(r, func(this Value, _ []Value) (Value, error) {
		log = append(log, "get")
		return IntValue(3), nil
	})
	setter := nativeFn(r, func(_ Value, args []Value) (Value, error) {
		log = append(log, "set "+args[0].String())
		return Undefined(), nil
	})
	mustSet(t, r, props, "c", desc(map[string]Value{"get": getter, "set": setter, "enumerable": True(), "configurable": True()}))
	props.DefineOwnDataFast(r, key(r, "skipped"), desc(map[string]Value{"value": IntValue(9)}), attrHidden)
	o := jsCall(t, r, ctor, "create", ObjectValue(p), ObjectValue(props))
	oo := o.AsObject()
	assert.Same(t, p, oo.Proto())
	assert.Equal(t, IntValue(1), jsGet(t, r, o, "x"))
	assertOwnAttrs(t, r, oo, "a", false, true, false)
	assertOwnAttrs(t, r, oo, "b", false, false, false)
	assert.False(t, oo.HasOwnProperty(key(r, "skipped")))
	assert.Equal(t, IntValue(3), jsGet(t, r, o, "c"))
	require.NoError(t, oo.SetProp(r, key(r, "c"), IntValue(4)))
	assert.Equal(t, []string{"get", "set 4"}, log)
	d, _ := oo.GetOwnProperty(key(r, "c"))
	assert.True(t, d.IsAccessorDescriptor())
	assert.Same(t, getter.AsObject(), d.GetterObject())
	assert.Same(t, setter.AsObject(), d.SetterObject())
	assert.Equal(t, []any{"a", "c"}, r.ToGo(jsCall(t, r, ctor, "keys", o)), "enumerable accessors are keys")
	// Validation happens before any definition.
	bad := r.NewObject()
	mustSet(t, r, bad, "ok", desc(map[string]Value{"value": IntValue(1)}))
	mustSet(t, r, bad, "nope", IntValue(5))
	err := jsCallErr(t, r, ctor, "create", Null(), ObjectValue(bad))
	assert.EqualError(t, err, "TypeError: Property description must be an object: 5")
	assert.EqualError(t, jsCallErr(t, r, ctor, "create", IntValue(1)), "TypeError: Object prototype may only be an Object or null: 1")
	assert.EqualError(t, jsCallErr(t, r, ctor, "create"), "TypeError: Object prototype may only be an Object or null: undefined")
	assert.EqualError(t, jsCallErr(t, r, ctor, "create", Null(), Null()), "TypeError: Cannot convert undefined or null to object")

	// defineProperty.
	target := r.NewObject()
	assert.Same(t, target, jsCall(t, r, ctor, "defineProperty", ObjectValue(target), str("v"), desc(map[string]Value{"value": IntValue(1)})).AsObject())
	assertOwnAttrs(t, r, target, "v", false, false, false)
	assert.EqualError(t, target.SetProp(r, key(r, "v"), IntValue(2)), "TypeError: Cannot assign to read only property 'v' of object")
	assert.EqualError(t, jsCallErr(t, r, ctor, "defineProperty", ObjectValue(target), str("v"), desc(map[string]Value{"value": IntValue(2)})), "TypeError: Cannot redefine property: v")
	jsCall(t, r, ctor, "defineProperty", ObjectValue(target), str("v"), desc(map[string]Value{"value": IntValue(1)})) // same value is allowed
	jsCall(t, r, ctor, "defineProperty", ObjectValue(target), IntValue(0), desc(map[string]Value{"value": str("idx"), "writable": True(), "enumerable": True(), "configurable": True()}))
	assert.Equal(t, "idx", jsString(t, jsGet(t, r, ObjectValue(target), "0")))
	jsCall(t, r, ctor, "defineProperty", ObjectValue(target), SymbolValue(SymIterator), desc(map[string]Value{"value": IntValue(1)}))
	assert.True(t, target.HasOwnProperty(SymbolKey(SymIterator)))
	// Accessor with only a getter; converting configurable data -> accessor.
	jsCall(t, r, ctor, "defineProperty", ObjectValue(target), str("w"), desc(map[string]Value{"value": IntValue(1), "configurable": True()}))
	jsCall(t, r, ctor, "defineProperty", ObjectValue(target), str("w"), desc(map[string]Value{"get": getter}))
	assert.Equal(t, IntValue(3), jsGet(t, r, ObjectValue(target), "w"))
	ok, err := target.Set(r, key(r, "w"), IntValue(1), ObjectValue(target))
	require.NoError(t, err)
	assert.False(t, ok, "getter-only accessor rejects writes")
	// Descriptor errors.
	assert.EqualError(t, jsCallErr(t, r, ctor, "defineProperty", IntValue(1), str("x"), desc(nil)), "TypeError: Object.defineProperty called on non-object")
	assert.EqualError(t, jsCallErr(t, r, ctor, "defineProperty", ObjectValue(target), str("x"), str("nope")), "TypeError: Property description must be an object: \"nope\"")
	assert.EqualError(t, jsCallErr(t, r, ctor, "defineProperty", ObjectValue(target), str("x"), desc(map[string]Value{"get": IntValue(1)})), "TypeError: Getter must be a function: 1")
	assert.EqualError(t, jsCallErr(t, r, ctor, "defineProperty", ObjectValue(target), str("x"), desc(map[string]Value{"set": str("s")})), "TypeError: Setter must be a function: s")
	assert.EqualError(t, jsCallErr(t, r, ctor, "defineProperty", ObjectValue(target), str("x"), desc(map[string]Value{"get": getter, "value": IntValue(1)})), "TypeError: Invalid property descriptor. Cannot both specify accessors and a value or writable attribute")
	assert.EqualError(t, jsCallErr(t, r, ctor, "defineProperty", ObjectValue(target), str("x"), desc(map[string]Value{"set": setter, "writable": False()})), "TypeError: Invalid property descriptor. Cannot both specify accessors and a value or writable attribute")
	// Descriptor fields are read in spec order through the prototype chain,
	// and get: undefined is accepted.
	inherited := r.NewObjectWithProto(desc(map[string]Value{"enumerable": True()}).AsObject())
	jsCall(t, r, ctor, "defineProperty", ObjectValue(target), str("inh"), ObjectValue(inherited))
	assertOwnAttrs(t, r, target, "inh", false, true, false)
	jsCall(t, r, ctor, "defineProperty", ObjectValue(target), str("undef"), desc(map[string]Value{"get": Undefined(), "configurable": True()}))
	assert.True(t, jsGet(t, r, ObjectValue(target), "undef").IsUndefined())
	// Array length and index interplay.
	arr := r.NewArray(IntValue(1), IntValue(2), IntValue(3))
	jsCall(t, r, ctor, "defineProperty", ObjectValue(arr), str("length"), desc(map[string]Value{"value": IntValue(1)}))
	assert.Equal(t, uint32(1), arr.ArrayLength())
	assert.EqualError(t, jsCallErr(t, r, ctor, "defineProperty", ObjectValue(arr), str("length"), desc(map[string]Value{"value": NumberValue(1.5)})), "RangeError: Invalid array length")
	jsCall(t, r, ctor, "defineProperty", ObjectValue(arr), str("length"), desc(map[string]Value{"writable": False()}))
	assert.False(t, arr.Push(r, IntValue(1)))
	// Non-extensible targets reject new keys.
	sealed := r.NewObject()
	sealed.PreventExtensions(r)
	assert.EqualError(t, jsCallErr(t, r, ctor, "defineProperty", ObjectValue(sealed), str("n"), desc(map[string]Value{"value": IntValue(1)})), "TypeError: Cannot redefine property: n")
}

func TestObjectNamesFromEntriesHasOwn(t *testing.T) {
	r := NewRealm()
	ctor := ObjectValue(r.ObjectCtor)
	o := r.NewObject()
	mustSet(t, r, o, "b", IntValue(1))
	mustSet(t, r, o, "1", IntValue(1))
	o.DefineOwnDataFast(r, key(r, "hidden"), IntValue(0), attrHidden)
	o.DefineOwnDataFast(r, SymbolKey(SymIterator), IntValue(0), attrDefault)
	assert.Equal(t, []any{"1", "b", "hidden"}, r.ToGo(jsCall(t, r, ctor, "getOwnPropertyNames", ObjectValue(o))))
	assert.Equal(t, []any{"0", "1", "length"}, r.ToGo(jsCall(t, r, ctor, "getOwnPropertyNames", jsArray(r, IntValue(1), IntValue(2)))))
	assert.Equal(t, []any{"0", "1", "length"}, r.ToGo(jsCall(t, r, ctor, "getOwnPropertyNames", str("ab"))))
	assert.Equal(t, []any{"length", "name"}, r.ToGo(jsCall(t, r, ctor, "getOwnPropertyNames", jsGlobal(t, r, "parseInt"))))
	assert.Equal(t, []any{"length", "name", "prototype"}, r.ToGo(jsCall(t, r, ctor, "getOwnPropertyNames", ObjectValue(r.BooleanCtor))))
	assert.Equal(t, []any{"message", "stack"}, r.ToGo(jsCall(t, r, ctor, "getOwnPropertyNames", ObjectValue(r.NewError(KindError, "m")))))
	assert.EqualError(t, jsCallErr(t, r, ctor, "getOwnPropertyNames", Undefined()), "TypeError: Cannot convert undefined or null to object")

	// fromEntries over arrays of pairs.
	pairs := jsArray(r, jsArray(r, str("a"), IntValue(1)), jsArray(r, IntValue(2), str("two")), jsArray(r, str("a"), IntValue(3)))
	res := jsCall(t, r, ctor, "fromEntries", pairs)
	assert.Equal(t, map[string]any{"a": int64(3), "2": "two"}, r.ToGo(res))
	assert.Equal(t, []string{"2", "a"}, keyNames(res.AsObject().OwnPropertyKeys()))
	assert.Same(t, r.ObjectPrototype, res.AsObject().Proto())
	assert.Equal(t, map[string]any{}, r.ToGo(jsCall(t, r, ctor, "fromEntries", jsArray(r))))
	// Entries may be array-likes; missing slots read as undefined.
	entry := r.NewObject()
	mustSet(t, r, entry, "0", str("k"))
	res = jsCall(t, r, ctor, "fromEntries", jsArray(r, ObjectValue(entry)))
	assert.True(t, jsGet(t, r, res, "k").IsUndefined())
	assert.True(t, res.AsObject().HasOwnProperty(key(r, "k")))
	// Round trip with entries, including a key coerced through ToPropertyKey.
	src := r.NewObject()
	mustSet(t, r, src, "x", IntValue(1))
	mustSet(t, r, src, "y", str("z"))
	back := jsCall(t, r, ctor, "fromEntries", jsCall(t, r, ctor, "entries", ObjectValue(src)))
	assert.Equal(t, r.ToGo(ObjectValue(src)), r.ToGo(back))
	// Errors.
	assert.EqualError(t, jsCallErr(t, r, ctor, "fromEntries", jsArray(r, IntValue(1))), "TypeError: Iterator value 1 is not an entry object")
	assert.EqualError(t, jsCallErr(t, r, ctor, "fromEntries", ObjectValue(r.NewArrayLen(1))), "TypeError: Iterator value undefined is not an entry object")
	assert.EqualError(t, jsCallErr(t, r, ctor, "fromEntries", Undefined()), "TypeError: Object.fromEntries requires an iterable, got undefined")
	assert.EqualError(t, jsCallErr(t, r, ctor, "fromEntries", Null()), "TypeError: Object.fromEntries requires an iterable, got null")
	err := jsCallErr(t, r, ctor, "fromEntries", ObjectValue(r.NewObject()))
	assert.ErrorContains(t, err, "TypeError", "plain objects are not iterable")
	// A key whose ToPropertyKey throws propagates.
	badKey := r.NewObjectWithProto(nil)
	assert.ErrorContains(t, jsCallErr(t, r, ctor, "fromEntries", jsArray(r, jsArray(r, ObjectValue(badKey), IntValue(1)))), "Cannot convert object to primitive value")

	// hasOwn: ToObject first, then the key.
	assert.Equal(t, True(), jsCall(t, r, ctor, "hasOwn", ObjectValue(o), str("b")))
	assert.Equal(t, True(), jsCall(t, r, ctor, "hasOwn", ObjectValue(o), IntValue(1)))
	assert.Equal(t, True(), jsCall(t, r, ctor, "hasOwn", ObjectValue(o), str("hidden")))
	assert.Equal(t, False(), jsCall(t, r, ctor, "hasOwn", ObjectValue(o), str("toString")))
	assert.Equal(t, True(), jsCall(t, r, ctor, "hasOwn", str("ab"), IntValue(1)))
	assert.Equal(t, False(), jsCall(t, r, ctor, "hasOwn", str("ab"), IntValue(2)))
	assert.Equal(t, True(), jsCall(t, r, ctor, "hasOwn", str("ab"), str("length")))
	assert.Equal(t, False(), jsCall(t, r, ctor, "hasOwn", IntValue(1), str("toString")))
	assert.EqualError(t, jsCallErr(t, r, ctor, "hasOwn", Null(), ObjectValue(badKey)), "TypeError: Cannot convert undefined or null to object")
	assert.ErrorContains(t, jsCallErr(t, r, ctor, "hasOwn", ObjectValue(o), ObjectValue(badKey)), "Cannot convert object to primitive value")
}

func TestObjectPrototypeMethods(t *testing.T) {
	r := NewRealm()
	o := r.NewObject()
	mustSet(t, r, o, "own", IntValue(1))
	mustSet(t, r, o, "3", IntValue(1))
	o.DefineOwnDataFast(r, key(r, "hidden"), IntValue(0), attrHidden)
	ov := ObjectValue(o)
	// hasOwnProperty: key coerced before this.
	assert.Equal(t, True(), jsCall(t, r, ov, "hasOwnProperty", str("own")))
	assert.Equal(t, True(), jsCall(t, r, ov, "hasOwnProperty", IntValue(3)))
	assert.Equal(t, True(), jsCall(t, r, ov, "hasOwnProperty", str("3")))
	assert.Equal(t, True(), jsCall(t, r, ov, "hasOwnProperty", str("hidden")))
	assert.Equal(t, False(), jsCall(t, r, ov, "hasOwnProperty", str("toString")))
	assert.Equal(t, False(), jsCall(t, r, ov, "hasOwnProperty"))
	mustSet(t, r, o, "undefined", IntValue(1))
	assert.Equal(t, True(), jsCall(t, r, ov, "hasOwnProperty"), "missing key is the string \"undefined\"")
	hop := jsGet(t, r, ObjectValue(r.ObjectPrototype), "hasOwnProperty")
	call := func(this Value, args ...Value) (Value, error) { return r.Call(hop, this, args) }
	v, _ := call(str("abc"), IntValue(1))
	assert.Equal(t, True(), v)
	v, _ = call(str("abc"), IntValue(3))
	assert.Equal(t, False(), v)
	v, _ = call(str("abc"), str("length"))
	assert.Equal(t, True(), v)
	v, _ = call(str("abc"), str("charAt"))
	assert.Equal(t, False(), v)
	v, _ = call(IntValue(1), str("toFixed"))
	assert.Equal(t, False(), v)
	v, _ = call(True(), str("valueOf"))
	assert.Equal(t, False(), v)
	_, err := call(Undefined(), str("x"))
	assert.EqualError(t, err, "TypeError: Cannot convert undefined or null to object")
	badKey := r.NewObjectWithProto(nil)
	_, err = call(Undefined(), ObjectValue(badKey))
	assert.ErrorContains(t, err, "Cannot convert object to primitive value", "the key is coerced first")
	v, _ = call(ObjectValue(r.NewError(KindError, "m")), str("stack"))
	assert.Equal(t, True(), v)
	v, _ = call(ObjectValue(r.ArrayPrototype), str("length"))
	assert.Equal(t, True(), v)

	// isPrototypeOf.
	child := r.NewObjectWithProto(o)
	grand := r.NewObjectWithProto(child)
	assert.Equal(t, True(), jsCall(t, r, ov, "isPrototypeOf", ObjectValue(grand)))
	assert.Equal(t, True(), jsCall(t, r, ObjectValue(r.ObjectPrototype), "isPrototypeOf", ObjectValue(grand)))
	assert.Equal(t, False(), jsCall(t, r, ObjectValue(grand), "isPrototypeOf", ov))
	assert.Equal(t, False(), jsCall(t, r, ov, "isPrototypeOf", ov))
	assert.Equal(t, False(), jsCall(t, r, ov, "isPrototypeOf", IntValue(1)))
	assert.Equal(t, False(), jsCall(t, r, ov, "isPrototypeOf"))
	ipo := jsGet(t, r, ObjectValue(r.ObjectPrototype), "isPrototypeOf")
	v, err = r.Call(ipo, Undefined(), []Value{IntValue(1)})
	require.NoError(t, err)
	assert.Equal(t, False(), v, "primitive argument short-circuits before ToObject(this)")
	_, err = r.Call(ipo, Undefined(), []Value{ov})
	assert.EqualError(t, err, "TypeError: Cannot convert undefined or null to object")
	assert.Equal(t, True(), jsCall(t, r, ObjectValue(r.FunctionPrototype), "isPrototypeOf", ObjectValue(r.ArrayCtor)))

	// propertyIsEnumerable.
	assert.Equal(t, True(), jsCall(t, r, ov, "propertyIsEnumerable", str("own")))
	assert.Equal(t, True(), jsCall(t, r, ov, "propertyIsEnumerable", IntValue(3)))
	assert.Equal(t, False(), jsCall(t, r, ov, "propertyIsEnumerable", str("hidden")))
	assert.Equal(t, False(), jsCall(t, r, ov, "propertyIsEnumerable", str("toString")))
	assert.Equal(t, False(), jsCall(t, r, ObjectValue(grand), "propertyIsEnumerable", str("own")), "inherited properties are not own")
	pie := jsGet(t, r, ObjectValue(r.ObjectPrototype), "propertyIsEnumerable")
	v, _ = r.Call(pie, str("ab"), []Value{IntValue(0)})
	assert.Equal(t, True(), v)
	v, _ = r.Call(pie, str("ab"), []Value{str("length")})
	assert.Equal(t, False(), v)
	v, _ = r.Call(pie, IntValue(1), []Value{str("x")})
	assert.Equal(t, False(), v)
	_, err = r.Call(pie, Null(), []Value{str("x")})
	assert.EqualError(t, err, "TypeError: Cannot convert undefined or null to object")
	arr := r.NewArray(IntValue(1))
	assert.Equal(t, False(), jsCall(t, r, ObjectValue(arr), "propertyIsEnumerable", str("length")))
	assert.Equal(t, True(), jsCall(t, r, ObjectValue(arr), "propertyIsEnumerable", IntValue(0)))

	// toString tags and toLocaleString delegation.
	ts := jsGet(t, r, ObjectValue(r.ObjectPrototype), "toString")
	for _, c := range []struct {
		this Value
		want string
	}{
		{Undefined(), "[object Undefined]"}, {Null(), "[object Null]"}, {IntValue(1), "[object Number]"}, {str("s"), "[object String]"},
		{True(), "[object Boolean]"}, {ObjectValue(arr), "[object Array]"}, {ObjectValue(r.ArrayPrototype), "[object Array]"},
		{ObjectValue(r.ObjectCtor), "[object Function]"}, {ObjectValue(r.NewError(KindTypeError, "x")), "[object Error]"},
		{ObjectValue(r.ErrorPrototype), "[object Object]"}, {ov, "[object Object]"}, {ObjectValue(r.Math), "[object Math]"},
		{ObjectValue(r.JSON), "[object JSON]"}, {ObjectValue(r.NumberPrototype), "[object Number]"}, {ObjectValue(r.NewObjectWithProto(nil)), "[object Object]"},
		{SymbolValue(SymIterator), "[object Symbol]"},
	} {
		res, err := r.Call(ts, c.this, nil)
		require.NoError(t, err)
		assert.Equal(t, c.want, jsString(t, res))
	}
	assert.Equal(t, "[object Object]", jsString(t, jsCall(t, r, ov, "toLocaleString")))
	o.DefineOwnDataFast(r, StringKey(AtomToString), nativeFn(r, func(this Value, _ []Value) (Value, error) {
		return str("custom:" + this.String()), nil
	}), attrHidden)
	assert.Equal(t, "custom:[object Object]", jsString(t, jsCall(t, r, ov, "toLocaleString")))
	assert.Equal(t, "1", jsString(t, jsCall(t, r, IntValue(1), "toLocaleString")))
	tls := jsGet(t, r, ObjectValue(r.ObjectPrototype), "toLocaleString")
	_, err = r.Call(tls, Undefined(), nil)
	assert.ErrorContains(t, err, "Cannot read property 'toString' of undefined")
	o.DefineOwnDataFast(r, key(r, "notfn"), IntValue(1), attrHidden)
	require.NoError(t, o.SetProp(r, StringKey(AtomToString), IntValue(1)))
	assert.EqualError(t, jsCallErr(t, r, ov, "toLocaleString"), "TypeError: 1 is not a function")
	// valueOf returns the object itself (wrapped for primitives).
	assert.Same(t, o, jsCall(t, r, ov, "valueOf").AsObject())
	vo := jsGet(t, r, ObjectValue(r.ObjectPrototype), "valueOf")
	w, _ := r.Call(vo, str("s"), nil)
	assert.Equal(t, ClassString, w.AsObject().Class())
}
