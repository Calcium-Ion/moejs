package engine

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStringConstantsAreConstAtoms: a short ASCII string constant of shared
// code, a property name's or a value's, is the atom of its content, marked
// as a constant, so a computed key it names needs no interning; a value
// whose atom exists unmarked, a long one and a static atom's name keep a
// String of their own. The live measurement counts none of them.
func TestStringConstantsAreConstAtoms(t *testing.T) {
	pre := InternKey("zqPreInternedName").String()
	long := strings.Repeat("y", maxSharedDataKey+1)
	_, r, fn := callAllocs(t, `export function f() { const o = { zqBothName: 1 }; return ["zqConstName", "zqConstName", "length", "zqPreInternedName", "`+long+`", "zqBothName", o.zqBothName]; }`)
	v, err := r.Call(fn, Undefined(), nil)
	require.NoError(t, err)
	o := v.AsObject()
	elem := func(i uint32) *String {
		x, err := o.GetProp(r, IndexKey(i))
		require.NoError(t, err)
		return x.AsString()
	}

	a := elem(0)
	assert.Same(t, a, elem(1), "one atom for both")
	assert.NotZero(t, a.atom)
	assert.NotZero(t, a.flags&strConst)
	assert.Same(t, a, InternKey("zqConstName").String(), "the canonical atom")
	assert.Equal(t, StringKey(a), r.KeyFromString(a))

	for i, name := range []string{"length", "zqPreInternedName", "long"} {
		s := elem(uint32(2 + i))
		assert.Zero(t, s.atom, name)
		assert.NotZero(t, s.flags&strConst, name)
	}
	assert.NotSame(t, pre, elem(3))
	both := elem(5)
	assert.NotZero(t, both.flags&strConst, "a key's atom made by the template")
	assert.Same(t, both, InternKey("zqBothName").String())
	assert.Equal(t, StringKey(pre), r.KeyFromString(elem(3)))

	x, err := o.GetProp(r, key(r, "length"))
	require.NoError(t, err)
	assert.Equal(t, IntValue(7), x)
	empty := r.MeasureLive(nil)
	got := r.MeasureLive(func(add func(Value)) {
		for i := range uint32(6) {
			add(StringValue(elem(i)))
		}
	})
	assert.Equal(t, empty, got, "constants count for nothing")
}

// TestComputedKeyFromConstant: o[k] over a literal's keys finds and makes
// the properties the names make.
func TestComputedKeyFromConstant(t *testing.T) {
	_, r, fn := callAllocs(t, `export function f() {
  const src = { zqUrl: "u", zqWeight: 2 }, out = {};
  for (const k of ["zqUrl", "zqWeight"]) out[k] = src[k];
  return out.zqUrl + out.zqWeight;
}`)
	v, err := r.Call(fn, Undefined(), nil)
	require.NoError(t, err)
	assert.Equal(t, "u2", v.AsString().GoString())
}
