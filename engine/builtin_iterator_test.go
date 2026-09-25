package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// iterNext calls it.next() and returns (value, done).
func iterNext(t *testing.T, r *Realm, it Value) (Value, bool) {
	t.Helper()
	res := jsCall(t, r, it, "next")
	require.True(t, res.IsObject())
	assert.Equal(t, []string{"value", "done"}, keyNames(res.AsObject().OwnPropertyKeys()))
	assertOwnAttrs(t, r, res.AsObject(), "value", true, true, true)
	assertOwnAttrs(t, r, res.AsObject(), "done", true, true, true)
	assert.Same(t, r.ObjectPrototype, res.AsObject().Proto())
	done := jsGet(t, r, res, "done")
	require.True(t, done.IsBool())
	return jsGet(t, r, res, "value"), done.AsBool()
}

func TestArrayIteratorPrototype(t *testing.T) {
	r := NewRealm()
	proto := r.ArrayIteratorPrototype
	require.NotNil(t, proto)
	assert.Same(t, r.IteratorPrototype, proto.Proto())
	assert.Same(t, r.ObjectPrototype, r.IteratorPrototype.Proto())
	assert.True(t, proto.IsPrototypeObject())
	assertNativeShape(t, r, jsGet(t, r, ObjectValue(proto), "next"), "next", 0)
	assertOwnAttrs(t, r, proto, "next", true, false, true)
	assert.Equal(t, []string{"next", "Symbol(Symbol.toStringTag)"}, keyNames(proto.OwnPropertyKeys()))
	assert.Equal(t, "[object Array Iterator]", jsString(t, jsCall(t, r, ObjectValue(proto), "toString")))

	arr := r.NewArray(str("a"), str("b"))
	for _, c := range []struct {
		method string
		kind   IterKind
	}{{"keys", IterKeys}, {"values", IterValues}, {"entries", IterEntries}} {
		it := jsCall(t, r, ObjectValue(arr), c.method)
		io := it.AsObject()
		assert.Equal(t, ClassArrayIterator, io.Class())
		assert.Equal(t, "Array Iterator", io.ClassName())
		assert.Same(t, proto, io.Proto())
		d, ok := io.Internal().(*IteratorData)
		require.True(t, ok, "Internal() is the shared IteratorData contract")
		assert.Equal(t, c.kind, d.Kind)
		assert.Same(t, arr, d.Target.AsObject())
		assert.Equal(t, 0, d.Index)
		assert.False(t, d.Done)
		assert.Empty(t, io.OwnPropertyKeys())
	}
}

func TestArrayIteratorNext(t *testing.T) {
	r := NewRealm()
	arr := r.NewArrayLen(3)
	mustSet(t, r, arr, "0", str("x"))
	mustSet(t, r, arr, "2", str("z"))
	values := jsCall(t, r, ObjectValue(arr), "values")
	v, done := iterNext(t, r, values)
	assert.Equal(t, "x", jsString(t, v))
	assert.False(t, done)
	v, done = iterNext(t, r, values)
	assert.True(t, v.IsUndefined(), "holes read as undefined")
	assert.False(t, done)
	// Growth during iteration is observed.
	require.True(t, arr.Push(r, str("w")))
	v, _ = iterNext(t, r, values)
	assert.Equal(t, "z", jsString(t, v))
	v, _ = iterNext(t, r, values)
	assert.Equal(t, "w", jsString(t, v))
	v, done = iterNext(t, r, values)
	assert.True(t, v.IsUndefined())
	assert.True(t, done)
	d := values.AsObject().Internal().(*IteratorData)
	assert.True(t, d.Done)
	assert.True(t, d.Target.IsUndefined(), "the target is released when exhausted")
	require.True(t, arr.Push(r, str("late")))
	_, done = iterNext(t, r, values)
	assert.True(t, done, "an exhausted iterator stays exhausted")

	keys := jsCall(t, r, ObjectValue(arr), "keys")
	for i := range 5 {
		v, done := iterNext(t, r, keys)
		assert.Equal(t, IntValue(i), v)
		assert.False(t, done)
	}
	_, done = iterNext(t, r, keys)
	assert.True(t, done)

	entries := jsCall(t, r, ObjectValue(arr), "entries")
	v, done = iterNext(t, r, entries)
	assert.False(t, done)
	assert.Equal(t, []any{int64(0), "x"}, r.ToGo(v))
	assert.Same(t, r.ArrayPrototype, v.AsObject().Proto())
	v, _ = iterNext(t, r, entries)
	assert.Equal(t, []any{int64(1), nil}, r.ToGo(v))

	// Result objects share one realm shape.
	r1 := jsCall(t, r, keys, "next")
	r2 := jsCall(t, r, values, "next")
	assert.Same(t, r1.AsObject().Shape(), r2.AsObject().Shape())
	assert.Same(t, r1.AsObject().Shape(), r.iterResultShape())

	// Array-likes work through call, reading length each step.
	al := r.NewObject()
	mustSet(t, r, al, "length", IntValue(2))
	mustSet(t, r, al, "0", IntValue(10))
	mustSet(t, r, al, "1", IntValue(11))
	valuesFn := jsGet(t, r, ObjectValue(r.ArrayPrototype), "values")
	it, err := r.Call(valuesFn, ObjectValue(al), nil)
	require.NoError(t, err)
	v, _ = iterNext(t, r, it)
	assert.Equal(t, IntValue(10), v)
	mustSet(t, r, al, "length", IntValue(1))
	_, done = iterNext(t, r, it)
	assert.True(t, done)
	sit, err := r.Call(valuesFn, str("hi"), nil)
	require.NoError(t, err)
	v, _ = iterNext(t, r, sit)
	assert.Equal(t, "h", jsString(t, v))
	// A length getter that throws propagates through next.
	bad := r.NewObject()
	bad.DefineOwnAccessorFast(r, StringKey(AtomLength), throwingFn(r, "len").AsObject(), nil, attrConfigurable)
	it, err = r.Call(valuesFn, ObjectValue(bad), nil)
	require.NoError(t, err)
	assert.EqualError(t, jsCallErr(t, r, it, "next"), "TypeError: len")

	// Wrong receivers.
	next := jsGet(t, r, ObjectValue(r.ArrayIteratorPrototype), "next")
	for _, badThis := range []Value{Undefined(), ObjectValue(r.NewObject()), ObjectValue(arr), ObjectValue(r.ArrayIteratorPrototype)} {
		_, err := r.Call(next, badThis, nil)
		assert.ErrorContains(t, err, "next method called on incompatible receiver")
	}
	_, err = r.Call(valuesFn, Null(), nil)
	assert.EqualError(t, err, "TypeError: Cannot convert undefined or null to object")
}

func TestArrayIteratorSharedRealm(t *testing.T) {
	r1 := newShared()
	r2 := newShared()
	assert.Same(t, r1.ArrayIteratorPrototype, r2.ArrayIteratorPrototype)
	assert.True(t, r1.ArrayIteratorPrototype.IsShared())
	assert.True(t, r1.ArrayIteratorPrototype.IsFrozen())
	it := jsCall(t, r1, jsArray(r1, IntValue(1)), "values")
	assert.False(t, it.AsObject().IsShared())
	assert.True(t, it.AsObject().Shape().IsShared(), "the iterator's shape is the shared root")
	assert.Same(t, it.AsObject().Shape(), jsCall(t, r2, jsArray(r2, IntValue(1)), "values").AsObject().Shape())
	assert.Same(t, r1.ArrayIteratorPrototype, it.AsObject().Proto())
	v, done := iterNext(t, r1, it)
	assert.Equal(t, IntValue(1), v)
	assert.False(t, done)
	_, done = iterNext(t, r1, it)
	assert.True(t, done)
	assert.ErrorContains(t, r1.ArrayIteratorPrototype.SetProp(r1, key(r1, "x"), IntValue(1)), "shared intrinsic")
}
