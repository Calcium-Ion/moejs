package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// coreRoots lists the intrinsic objects the core builtin installers install
// into or create, as reached from a realm.
func coreRoots(r *Realm) map[string]*Object {
	roots := map[string]*Object{
		"Object": r.ObjectCtor, "Object.prototype": r.ObjectPrototype, "Function.prototype": r.FunctionPrototype,
		"Array": r.ArrayCtor, "Array.prototype": r.ArrayPrototype, "%ArrayIteratorPrototype%": r.ArrayIteratorPrototype,
		"Number": r.NumberCtor, "Number.prototype": r.NumberPrototype, "Boolean": r.BooleanCtor, "Boolean.prototype": r.BooleanPrototype,
		"Math": r.Math,
	}
	for k := KindError; k < numErrorKinds; k++ {
		roots[k.Name().GoString()] = r.ErrorConstructorFor(k)
		roots[k.Name().GoString()+".prototype"] = r.ErrorPrototypeFor(k)
	}
	for _, n := range []string{"parseInt", "parseFloat", "isNaN", "isFinite"} {
		v, _ := r.Global.GetOwnDataValue(r.KeyFromGoString(n))
		roots[n] = v.AsObject()
	}
	return roots
}

func TestBuiltinsSharedFrozenAndIdentical(t *testing.T) {
	r1 := newShared()
	r2 := newShared()
	roots1, roots2 := coreRoots(r1), coreRoots(r2)
	seen := map[*Object]string{}
	var walk func(o *Object, path string)
	walk = func(o *Object, path string) {
		if o == nil {
			return
		}
		if _, ok := seen[o]; ok {
			return
		}
		seen[o] = path
		require.True(t, o.IsShared(), "%s is not shared", path)
		require.True(t, o.IsFrozen(), "%s is not frozen", path)
		require.False(t, o.IsExtensible(), "%s is extensible", path)
		require.True(t, o.Shape().IsShared(), "%s has a realm-local shape", path)
		if fd := o.FunctionData(); fd != nil {
			assert.Nil(t, fd.Realm(), "%s captured a realm", path)
		}
		for _, k := range o.OwnPropertyKeys() {
			d, _ := o.GetOwnProperty(k)
			assert.False(t, d.Configurable(), "%s.%s is configurable", path, k.GoString())
			if d.IsAccessorDescriptor() {
				walk(d.GetterObject(), path+"."+k.GoString()+"[get]")
				walk(d.SetterObject(), path+"."+k.GoString()+"[set]")
				continue
			}
			assert.False(t, d.Writable(), "%s.%s is writable", path, k.GoString())
			if d.Value.IsObject() {
				walk(d.Value.AsObject(), path+"."+k.GoString())
			}
		}
		walk(o.Proto(), path+".__proto__")
	}
	for name, o := range roots1 {
		walk(o, name)
		assert.Same(t, o, roots2[name], "%s differs between shared realms", name)
	}
	assert.Greater(t, len(seen), 120, "every installed function object is reachable and shared")
	// Function bindings on the global are frozen in the lockdown model and
	// point at the shared objects.
	for _, n := range []string{"parseInt", "parseFloat", "isNaN", "isFinite"} {
		assertOwnAttrs(t, r1, r1.Global, n, false, false, false)
		assert.Same(t, roots1[n], jsGlobal(t, r2, n).AsObject())
	}
	// Writes through JavaScript-visible paths fail with a TypeError.
	for _, c := range []struct {
		o    *Object
		name string
	}{{r1.ArrayPrototype, "push"}, {r1.ObjectCtor, "keys"}, {r1.Math, "PI"}, {r1.NumberCtor, "MAX_VALUE"}, {r1.ArrayIteratorPrototype, "next"}} {
		assert.ErrorContains(t, c.o.SetProp(r1, key(r1, c.name), IntValue(1)), "shared intrinsic", c.name)
		desc := r1.NewObject()
		require.NoError(t, desc.SetProp(r1, key(r1, "value"), IntValue(1)))
		assert.ErrorContains(t, jsCallErr(t, r1, ObjectValue(r1.ObjectCtor), "defineProperty", ObjectValue(c.o), str(c.name), ObjectValue(desc)), "shared intrinsic")
		// An empty descriptor changes nothing, so it succeeds.
		assert.Same(t, c.o, jsCall(t, r1, ObjectValue(r1.ObjectCtor), "defineProperty", ObjectValue(c.o), str(c.name), ObjectValue(r1.NewObject())).AsObject())
		assert.False(t, c.o.Delete(r1, key(r1, c.name)))
	}
	assert.EqualError(t, jsCallErr(t, r1, ObjectValue(r1.ObjectCtor), "setPrototypeOf", ObjectValue(r1.ArrayPrototype), Null()), "TypeError: [object Array] is not extensible")
	assert.Same(t, r1.ArrayPrototype, jsCall(t, r1, ObjectValue(r1.ObjectCtor), "freeze", ObjectValue(r1.ArrayPrototype)).AsObject())
	assert.Equal(t, True(), jsCall(t, r1, ObjectValue(r1.ObjectCtor), "isFrozen", ObjectValue(r1.Math)))
}

func TestBuiltinsWorkInSharedRealms(t *testing.T) {
	r1 := newShared()
	r2 := newShared()
	// Realm-local results: arrays, iterators and result objects are mutable
	// and belong to the calling realm.
	a1 := jsCall(t, r1, mkArray(t, r1, 3, 1, 2), "sort")
	assert.Equal(t, []any{int64(1), int64(2), int64(3)}, r1.ToGo(a1))
	assert.False(t, a1.AsObject().IsShared())
	assert.Same(t, r1.ArrayPrototype, a1.AsObject().Proto())
	keys := jsCall(t, r1, ObjectValue(r1.ObjectCtor), "keys", func() Value {
		o := r1.NewObject()
		mustSet(t, r1, o, "k", IntValue(1))
		return ObjectValue(o)
	}())
	assert.Equal(t, []any{"k"}, r1.ToGo(keys))
	assert.True(t, keys.AsObject().Push(r1, str("more")))
	it := jsCall(t, r2, mkArray(t, r2, "x"), "entries")
	res := jsCall(t, r2, it, "next")
	assert.False(t, res.AsObject().IsShared())
	assert.Equal(t, []any{int64(0), "x"}, r2.ToGo(jsGet(t, r2, res, "value")))
	mapped := jsCall(t, r2, mkArray(t, r2, 1, 2), "map", nativeFn(r2, func(_ Value, args []Value) (Value, error) {
		return NumberValue(args[0].AsNumber() + 1), nil
	}))
	assert.Equal(t, []any{int64(2), int64(3)}, r2.ToGo(mapped))
	assert.Equal(t, "1.50", jsString(t, jsCall(t, r2, NumberValue(1.5), "toFixed", IntValue(2))))
	assert.Equal(t, 3.0, jsNumber(t, jsCall(t, r1, ObjectValue(r1.Math), "max", IntValue(1), IntValue(3))))
	// Errors created in shared realms have mutable own properties.
	e, err := r1.Construct(ObjectValue(r1.ErrorConstructorFor(KindTypeError)), []Value{str("m")}, nil)
	require.NoError(t, err)
	require.NoError(t, e.AsObject().SetProp(r1, key(r1, "name"), str("Mine")))
	assert.Equal(t, "Mine: m", jsString(t, jsCall(t, r1, e, "toString")))
	assert.Equal(t, "TypeError", jsString(t, jsGet(t, r2, ObjectValue(r2.ErrorPrototypeFor(KindTypeError)), "name")))
	// Math.random state is per realm even though Math itself is shared.
	jsCall(t, r1, ObjectValue(r1.Math), "random")
	jsCall(t, r2, ObjectValue(r2.Math), "random")
	assert.NotSame(t, r1.lazy.rng, r2.lazy.rng)
	// Bound functions and Object.create in shared realms are realm-local.
	bound := jsCall(t, r1, ObjectValue(r1.ArrayCtor), "bind", Undefined(), IntValue(2))
	assert.False(t, bound.AsObject().IsShared())
	created := jsCall(t, r1, ObjectValue(r1.ObjectCtor), "create", ObjectValue(r1.ArrayPrototype))
	assert.Same(t, r1.ArrayPrototype, created.AsObject().Proto())
	require.NoError(t, created.AsObject().SetProp(r1, key(r1, "x"), IntValue(1)))
}
