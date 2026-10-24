package engine

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestShapeIndexLookup: a shape past shapeTableThreshold properties finds
// every string and symbol key at its slot through its index, misses an
// absent one, and keeps the index at most half full.
func TestShapeIndexLookup(t *testing.T) {
	r := NewRealm()
	o := r.NewObject()
	var keys []PropertyKey
	for i := range 40 {
		k := key(r, fmt.Sprintf("p%d", i))
		if i%5 == 4 {
			k = SymbolKey(NewSymbol(FromGoString(fmt.Sprint(i))))
		}
		keys = append(keys, k)
		o.DefineOwnDataFast(r, k, IntValue(i), attrDefault)
	}
	s := o.Shape()
	for i, k := range keys {
		slot, attrs, ok := s.Lookup(k)
		require.True(t, ok, "key %d", i)
		assert.Equal(t, uint32(i), slot)
		assert.Equal(t, uint8(attrDefault), attrs)
		assert.True(t, s.has(k))
	}
	for _, k := range []PropertyKey{key(r, "absent"), SymbolKey(NewSymbol(FromGoString("p1"))), IndexKey(3)} {
		_, _, ok := s.Lookup(k)
		assert.False(t, ok)
		assert.False(t, s.has(k))
	}
	require.NotNil(t, s.table)
	used := 0
	for _, e := range s.table.slots {
		if e != 0 {
			used++
		}
	}
	assert.Equal(t, len(keys), used)
	assert.LessOrEqual(t, 2*used, len(s.table.slots))
}

// TestShapeIndexFromJavaScript: computed keys over a wide object, in a
// realm of its own and over the shared intrinsics, whose shapes keep the
// index in their snapshot.
func TestShapeIndexFromJavaScript(t *testing.T) {
	src := `export function f() {
  const o = {};
  for (let i = 0; i < 30; i++) o["k" + i] = i;
  let s = 0;
  for (let i = 0; i < 30; i++) s += o["k" + i];
  return s + o.k29 + ("k30" in o ? 1000 : 0) + Object.keys(o).length;
}`
	for _, shared := range []bool{false, true} {
		f := evalModuleWith(t, src, RealmOptions{SharedIntrinsics: shared})
		for range 3 {
			assert.Equal(t, int64(435+29+30), f.call("f"), "shared %v", shared)
		}
	}
}
