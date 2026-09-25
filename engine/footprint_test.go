package engine

import (
	"runtime"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
)

// bytesPerObject measures the average heap bytes retained by one value built
// by mk, over n instances kept alive. Background work left by earlier tests
// (weak-pointer cleanups of the intern table, finalizers) can free memory
// between the two samples and make the delta negative; unsigned subtraction
// would then wrap to ~2^64/n, so the delta is computed signed and the sample
// is retried a few times when it is not positive.
func bytesPerObject(n int, mk func(r *Realm) *Object) float64 {
	r := NewRealm()
	mk(r) // warm caches (shapes, atoms)
	var delta int64
	for attempt := 0; attempt < 5; attempt++ {
		keep := make([]*Object, 0, n)
		var before, after runtime.MemStats
		runtime.GC()
		runtime.GC()
		runtime.ReadMemStats(&before)
		for range n {
			keep = append(keep, mk(r))
		}
		runtime.GC()
		runtime.ReadMemStats(&after)
		runtime.KeepAlive(keep)
		delta = int64(after.HeapAlloc) - int64(before.HeapAlloc)
		if delta > 0 {
			break
		}
	}
	return float64(delta) / float64(n)
}

func TestObjectFootprint(t *testing.T) {
	const n = 4096
	t.Logf("sizeof Object=%d Shape=%d String=%d Value=%d", unsafe.Sizeof(Object{}), unsafe.Sizeof(Shape{}), unsafe.Sizeof(String{}), unsafe.Sizeof(Value{}))
	empty := bytesPerObject(n, func(r *Realm) *Object { return r.NewObject() })
	five := bytesPerObject(n, func(r *Realm) *Object {
		o := r.NewObject()
		for _, k := range [...]*String{AtomModel, AtomPrompt, AtomSize, AtomDuration, AtomSeed} {
			o.DefineOwnDataFast(r, StringKey(k), IntValue(1), attrDefault)
		}
		return o
	})
	array100 := bytesPerObject(n, func(r *Realm) *Object {
		items := make([]Value, 100)
		for i := range items {
			items[i] = IntValue(i)
		}
		return r.NewArrayFromSlice(items)
	})
	t.Logf("bytes per object: empty=%.0f fiveProps=%.0f array100=%.0f", empty, five, array100)
	assert.Less(t, empty, 160.0)
	assert.Less(t, five, 320.0)
	assert.Less(t, array100, 2200.0)
}
