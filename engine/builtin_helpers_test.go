package engine

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// Test helpers shared by the builtin_*_test.go files. Builtins are driven
// through Realm.Call directly, without compiling any JavaScript.

// jsGet reads property name of v (GetV semantics).
func jsGet(t *testing.T, r *Realm, v Value, name string) Value {
	t.Helper()
	res, err := r.GetV(v, key(r, name))
	require.NoError(t, err)
	return res
}

// jsCall invokes recv[name](...args) and requires success.
func jsCall(t *testing.T, r *Realm, recv Value, name string, args ...Value) Value {
	t.Helper()
	fn := jsGet(t, r, recv, name)
	require.True(t, IsCallable(fn), "%s is not callable", name)
	res, err := r.Call(fn, recv, args)
	require.NoError(t, err)
	return res
}

// jsCallErr invokes recv[name](...args) and returns the error.
func jsCallErr(t *testing.T, r *Realm, recv Value, name string, args ...Value) error {
	t.Helper()
	fn := jsGet(t, r, recv, name)
	require.True(t, IsCallable(fn), "%s is not callable", name)
	_, err := r.Call(fn, recv, args)
	return err
}

// jsGlobal reads a global binding.
func jsGlobal(t *testing.T, r *Realm, name string) Value {
	t.Helper()
	return jsGet(t, r, ObjectValue(r.Global), name)
}

// jsArray builds a dense array value.
func jsArray(r *Realm, items ...Value) Value {
	return ObjectValue(r.NewArrayFromSlice(append([]Value(nil), items...)))
}

// jsString returns the Go text of a string value, failing on other types.
func jsString(t *testing.T, v Value) string {
	t.Helper()
	require.True(t, v.IsString(), "expected a string, got %s", v.String())
	return v.AsString().GoString()
}

// jsNumber returns the float of a number value, failing on other types.
func jsNumber(t *testing.T, v Value) float64 {
	t.Helper()
	require.True(t, v.IsNumber(), "expected a number, got %s", v.String())
	return v.AsNumber()
}

// negZero is the -0 number value (Go constant -0.0 folds to +0).
func negZero() Value { return NumberValue(math.Copysign(0, -1)) }

// isNegZero reports whether v is exactly -0.
func isNegZero(v Value) bool { return v.IsNumber() && v.AsNumber() == 0 && math.Signbit(v.AsNumber()) }

// sameNumber compares numbers with SameValue semantics (NaN == NaN, ±0 distinct).
func sameNumber(want, got float64) bool {
	if math.IsNaN(want) {
		return math.IsNaN(got)
	}
	return want == got && math.Signbit(want) == math.Signbit(got)
}

// nativeFn wraps a Go closure as an anonymous native function object.
func nativeFn(r *Realm, fn func(this Value, args []Value) (Value, error)) Value {
	return ObjectValue(r.NewNativeFunction(AtomEmpty, 0, func(r *Realm, this Value, args []Value) (Value, error) {
		return fn(this, args)
	}))
}

// throwingFn returns a native that throws a TypeError with msg.
func throwingFn(r *Realm, msg string) Value {
	return nativeFn(r, func(Value, []Value) (Value, error) { return Undefined(), r.TypeError("%s", msg) })
}

// objWithValueOf creates an object whose valueOf returns v and records
// each call by appending tag to *log.
func objWithValueOf(r *Realm, v Value, tag string, log *[]string) Value {
	o := r.NewObject()
	o.DefineOwnDataFast(r, StringKey(AtomValueOf), nativeFn(r, func(Value, []Value) (Value, error) {
		*log = append(*log, tag)
		return v, nil
	}), attrHidden)
	return ObjectValue(o)
}

// assertOwnAttrs checks the attribute triple of an own data property.
func assertOwnAttrs(t *testing.T, r *Realm, o *Object, name string, writable, enumerable, configurable bool) {
	t.Helper()
	d, ok := o.GetOwnProperty(key(r, name))
	require.True(t, ok, "missing own property %s", name)
	require.Equal(t, writable, d.Writable(), "%s writable", name)
	require.Equal(t, enumerable, d.Enumerable(), "%s enumerable", name)
	require.Equal(t, configurable, d.Configurable(), "%s configurable", name)
}

// assertNativeShape checks that fn is a native function object with the
// spec's own length/name properties (non-writable, non-enumerable,
// configurable; shared intrinsics are frozen, so there configurable is
// false as well).
func assertNativeShape(t *testing.T, r *Realm, fn Value, name string, length int) {
	t.Helper()
	require.True(t, IsCallable(fn), "%s is not callable", name)
	o := fn.AsObject()
	assertOwnAttrs(t, r, o, "length", false, false, !o.IsShared())
	assertOwnAttrs(t, r, o, "name", false, false, !o.IsShared())
	lv, _ := o.GetOwnDataValue(lengthKey)
	require.Equal(t, IntValue(length), lv, "%s.length", name)
	nv, _ := o.GetOwnDataValue(StringKey(AtomName))
	require.Equal(t, name, nv.AsString().GoString(), "%s.name", name)
}
