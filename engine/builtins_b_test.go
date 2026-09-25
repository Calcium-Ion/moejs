package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helpers shared by the builtins_b_*_test.go tests.

// callMethod looks up this[name] and calls it, failing the test on error.
func callMethod(t *testing.T, r *Realm, this Value, name string, args ...Value) Value {
	t.Helper()
	v, err := callMethodErr(r, this, name, args...)
	require.NoError(t, err, "%s", name)
	return v
}

// callMethodErr looks up this[name] and calls it.
func callMethodErr(r *Realm, this Value, name string, args ...Value) (Value, error) {
	fn, err := r.GetV(this, r.KeyFromGoString(name))
	if err != nil {
		return Undefined(), err
	}
	return r.Call(fn, this, args)
}

// callGlobal calls a global function by name.
func callGlobal(t *testing.T, r *Realm, name string, args ...Value) (Value, error) {
	t.Helper()
	fn, err := r.Global.GetProp(r, r.KeyFromGoString(name))
	require.NoError(t, err)
	return r.Call(fn, Undefined(), args)
}

// newRegExp constructs a RegExp through the constructor.
func newRegExp(t *testing.T, r *Realm, pattern, flags string) Value {
	t.Helper()
	v, err := r.Construct(ObjectValue(r.RegExpCtor), []Value{str(pattern), str(flags)}, nil)
	require.NoError(t, err, "/%s/%s", pattern, flags)
	return v
}

// jsArray renders an array value as a []any through ToGo (undefined -> nil).
func jsArrayB(r *Realm, v Value) []any {
	if !v.IsObject() {
		return nil
	}
	out, _ := r.ToGo(v).([]any)
	return out
}

// utf16Str builds a string from raw code units (for lone surrogates).
func utf16Str(units ...uint16) Value { return StringValue(FromUTF16(units)) }

// propString reads a property of an object as a Go string ("undefined" for undefined).
func propString(t *testing.T, r *Realm, o Value, name string) string {
	t.Helper()
	v, err := r.GetV(o, r.KeyFromGoString(name))
	require.NoError(t, err)
	if v.IsUndefined() {
		return "undefined"
	}
	if v.IsNull() {
		return "null"
	}
	s, err := r.ToString(v)
	require.NoError(t, err)
	return s.GoString()
}

func assertErrorKind(t *testing.T, err error, kind ErrorKind, contains string) {
	t.Helper()
	require.Error(t, err)
	ex, ok := err.(*Exception)
	require.True(t, ok, "expected *Exception, got %T: %v", err, err)
	require.True(t, ex.Value.IsObject())
	assert.Equal(t, kind.Name().GoString(), propStringNoRealm(ex.Value.AsObject(), AtomName))
	assert.Contains(t, err.Error(), contains)
}

func propStringNoRealm(o *Object, name *String) string {
	v, ok := lookupDataNoThrow(o, StringKey(name))
	if !ok || !v.IsString() {
		return ""
	}
	return v.AsString().GoString()
}
