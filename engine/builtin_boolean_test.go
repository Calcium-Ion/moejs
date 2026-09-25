package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBooleanBuiltin(t *testing.T) {
	r := NewRealm()
	ctor := ObjectValue(r.BooleanCtor)
	assertNativeShape(t, r, ctor, "Boolean", 1)
	// Boolean(value) applies ToBoolean; no argument is false.
	for _, c := range []struct {
		in   Value
		want bool
	}{
		{True(), true}, {False(), false}, {IntValue(0), false}, {negZero(), false}, {NaN(), false}, {IntValue(2), true},
		{str(""), false}, {str("0"), true}, {str("false"), true}, {Undefined(), false}, {Null(), false},
		{ObjectValue(r.NewObject()), true}, {jsArray(r), true},
	} {
		v, err := r.Call(ctor, Undefined(), []Value{c.in})
		require.NoError(t, err)
		assert.Equal(t, Bool(c.want), v, c.in.String())
	}
	v, err := r.Call(ctor, Undefined(), nil)
	require.NoError(t, err)
	assert.Equal(t, False(), v)

	// new Boolean(value) wraps; the wrapper itself is truthy.
	w, err := r.Construct(ctor, []Value{str("")}, nil)
	require.NoError(t, err)
	wo := w.AsObject()
	assert.Equal(t, ClassBoolean, wo.Class())
	assert.Same(t, r.BooleanPrototype, wo.Proto())
	pv, ok := wo.PrimitiveValue()
	assert.True(t, ok)
	assert.Equal(t, False(), pv)
	assert.True(t, ToBoolean(w))
	assert.Empty(t, wo.OwnPropertyKeys())

	// Prototype methods on primitives and wrappers.
	proto := ObjectValue(r.BooleanPrototype)
	assertNativeShape(t, r, jsGet(t, r, proto, "toString"), "toString", 0)
	assertNativeShape(t, r, jsGet(t, r, proto, "valueOf"), "valueOf", 0)
	assert.Equal(t, "true", jsString(t, jsCall(t, r, True(), "toString")))
	assert.Equal(t, "false", jsString(t, jsCall(t, r, False(), "toString")))
	assert.Equal(t, "false", jsString(t, jsCall(t, r, w, "toString")))
	assert.Equal(t, True(), jsCall(t, r, True(), "valueOf"))
	assert.Equal(t, False(), jsCall(t, r, w, "valueOf"))
	assert.Equal(t, False(), jsCall(t, r, proto, "valueOf"), "Boolean.prototype is itself a Boolean wrapper of false")
	assert.Equal(t, "false", jsString(t, jsCall(t, r, proto, "toString")))
	assert.Same(t, r.BooleanCtor, jsGet(t, r, proto, "constructor").AsObject())
	// Wrong receivers.
	for _, bad := range []Value{IntValue(1), str("true"), Undefined(), ObjectValue(r.NewObject())} {
		ts := jsGet(t, r, proto, "toString")
		_, err := r.Call(ts, bad, nil)
		assert.ErrorContains(t, err, "requires that 'this' be a Boolean")
		vo := jsGet(t, r, proto, "valueOf")
		_, err = r.Call(vo, bad, nil)
		assert.EqualError(t, err, "TypeError: Boolean.prototype.valueOf requires that 'this' be a Boolean")
	}
	// String conversion of wrappers goes through toString.
	s, err := r.ToString(w)
	require.NoError(t, err)
	assert.Equal(t, "false", s.GoString())
	// A subclass-style newTarget is honoured.
	nt := r.NewNativeFunction(AtomEmpty, 0, nil)
	custom := r.NewObjectWithProto(r.BooleanPrototype)
	nt.DefineOwnDataFast(r, StringKey(AtomPrototype), ObjectValue(custom), attrHidden)
	w2, err := r.Construct(ctor, []Value{True()}, nt)
	require.NoError(t, err)
	assert.Same(t, custom, w2.AsObject().Proto())
	assert.Equal(t, "true", jsString(t, jsCall(t, r, w2, "toString")))
}
