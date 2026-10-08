package engine

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"unsafe"

	"github.com/Calcium-Ion/moejs/compiler"
	"github.com/Calcium-Ion/moejs/syntax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runScriptValue runs src in r and returns its completion value or error.
func runScriptValue(t *testing.T, r *Realm, src string) (Value, error) {
	t.Helper()
	s, err := syntax.ParseScript("mem.js", src, syntax.Options{})
	require.NoError(t, err)
	code, err := compiler.CompileScript(s)
	require.NoError(t, err)
	return r.RunScript(code)
}

// TestMemoryLimitRopeFlatten: a rope costs its node when it is made and
// its units when it is flattened, so a chain of prepends charges little
// until the string is read, and reading it charges the copy.
func TestMemoryLimitRopeFlatten(t *testing.T) {
	r := NewRealmWith(RealmOptions{MemoryLimit: 1 << 40})
	_, err := runScriptValue(t, r, `var big = "x".repeat(1 << 20)`)
	require.NoError(t, err)
	before := r.Stats().RequestAllocatedBytes
	v, err := runScriptValue(t, r, `var s = big; for (let i = 0; i < 100; i++) s = "y" + s; s.length`)
	require.NoError(t, err)
	assert.Equal(t, float64(1<<20+100), v.AsNumber())
	ropes := r.Stats().RequestAllocatedBytes - before
	assert.Less(t, ropes, int64(1<<14), "a hundred rope nodes")
	_, err = runScriptValue(t, r, `s.charCodeAt(5)`)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, r.Stats().RequestAllocatedBytes-before-ropes, int64(1<<20))

	// A rope of a realm with a limit is a ropeNode; its flattening charges
	// the units it copies and nothing else.
	for _, src := range []string{`"y" + "x".repeat(5000)`, `"y" + "é".repeat(5000)`} {
		v, err = runScriptValue(t, r, src)
		require.NoError(t, err)
		rope := v.AsString()
		require.Equal(t, strRope, rope.kind, src)
		require.NotZero(t, rope.flags&strCharged, src)
		assert.Same(t, r.mem(), (*ropeNode)(unsafe.Pointer(rope)).m)
		units := 1
		if !rope.ropeASCII() {
			units = 2
		}
		before := r.Stats().RequestAllocatedBytes
		rope.flatten()
		assert.Equal(t, int64(units*5001), r.Stats().RequestAllocatedBytes-before, src)
		assert.Equal(t, "y", rope.Substring(0, 1).GoString())
	}
	assert.Equal(t, uintptr(48), unsafe.Sizeof(String{}))
	assert.Equal(t, uintptr(56), unsafe.Sizeof(ropeNode{}), "a ropeNode stays in the 64-byte class")
}

// TestMemoryLimitRopeBeforeLimit: a rope made before the realm had a limit
// is a plain String, whose flattening is not charged (an accepted
// undercount), and a realm without a limit makes plain ropes.
func TestMemoryLimitRopeBeforeLimit(t *testing.T) {
	r := NewRealm()
	v, err := runScriptValue(t, r, `"y" + "x".repeat(5000)`)
	require.NoError(t, err)
	rope := v.AsString()
	require.Equal(t, strRope, rope.kind)
	assert.Zero(t, rope.flags&strCharged)
	r.SetMemoryLimit(1 << 40)
	rope.flatten()
	assert.Zero(t, r.Stats().RequestAllocatedBytes)
	assert.Equal(t, "y", rope.Substring(0, 1).GoString())
}

// TestMemoryLimitRopeOtherGoroutine: a rope the realm made may flatten on
// another goroutine while the realm runs (a value the host kept); the
// account takes the bytes atomically and the next charge counts them.
func TestMemoryLimitRopeOtherGoroutine(t *testing.T) {
	r := NewRealmWith(RealmOptions{MemoryLimit: 1 << 40})
	v, err := runScriptValue(t, r, `var s = "x".repeat(1 << 17); for (let i = 0; i < 10; i++) s = "y" + s; s`)
	require.NoError(t, err)
	rope := v.AsString()
	require.Equal(t, strRope, rope.kind)
	before := r.Stats().AllocatedBytes
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		rope.flatten()
	}()
	_, err = runScriptValue(t, r, `const a = []; for (let i = 0; i < 10000; i++) a.push({i}); a.length`)
	require.NoError(t, err)
	wg.Wait()
	assert.GreaterOrEqual(t, r.Stats().AllocatedBytes-before, int64(1<<17))
}

// TestMemoryLimitPreChecks: the producers that know their size first throw
// their RangeError for a size past the budget, which the script catches,
// and fill reserves its storage before it allocates it.
func TestMemoryLimitPreChecks(t *testing.T) {
	cases := []struct{ src, want string }{
		{`"x".repeat(1 << 22)`, "RangeError: Invalid string length"},
		{`"x".padStart(1 << 22)`, "RangeError: Invalid string length"},
		{`new ArrayBuffer(1 << 22).byteLength`, "RangeError: Array buffer allocation failed"},
		{`new Uint8Array(1 << 22).length`, "RangeError: Invalid typed array length: 4194304"},
		{`new Float64Array(1 << 20).length`, "RangeError: Invalid typed array length: 1048576"},
		{`btoa("x".repeat(1 << 20))`, "RangeError: Invalid string length"},
	}
	for _, c := range cases {
		r := NewRealmWith(RealmOptions{MemoryLimit: 2 << 20})
		v, err := runScriptValue(t, r, `try { `+c.src+` } catch (e) { e.name + ": " + e.message }`)
		require.NoError(t, err, c.src)
		assert.Equal(t, c.want, v.String(), c.src)
		assert.Zero(t, r.Stats().MemoryLimitHits, c.src)
	}

	r := NewRealmWith(RealmOptions{MemoryLimit: 2 << 20})
	_, err := runScriptValue(t, r, `try { new Array(1e7).fill(0) } finally { globalThis.ran = true }`)
	require.ErrorIs(t, err, ErrMemoryLimit)
	assert.Less(t, r.Stats().RequestAllocatedBytes, int64(200<<20), "fill stopped before it allocated")
	_, ok := r.Global.GetOwnDataValue(key(r, "ran"))
	assert.False(t, ok)
}

// TestMemoryLimitCoverage: each kind of storage a script can grow without
// bound is charged, so a loop growing it passes the limit.
func TestMemoryLimitCoverage(t *testing.T) {
	cases := []string{
		`const a = []; for (;;) a.push(a.length)`,
		`const a = []; for (let i = 0; ; i++) a[i] = i`,
		`const o = {}; for (let i = 0; ; i++) o["k" + i] = i`,
		`const a = []; for (let i = 0; ; i += 2) a[i * 1000] = i`,
		`const m = new Map(); for (let i = 0; ; i++) m.set(i, i)`,
		`const s = new Set(); for (let i = 0; ; i++) s.add(i)`,
		`let s = ""; for (;;) s += "abcdefgh"`,
		`const a = []; for (;;) a.push("x".repeat(1000) + a.length)`,
		`const a = []; for (;;) a.push(() => a.length)`,
		`const a = []; for (;;) a.push(new Uint8Array(1000))`,
		`const a = []; for (;;) a.push([1, 2, 3].join(",".repeat(100)))`,
		`const a = []; for (;;) a.push(JSON.stringify({a: [1, 2, 3], b: "xyz"}))`,
		`const a = []; for (;;) a.push(JSON.parse('{"a": [1, 2, 3], "b": "xyz"}'))`,
		`const a = []; for (;;) a.push(Object.keys({a: 1, b: 2}))`,
		`let b = 1n; const a = []; for (;;) a.push(b++ * 12345678901234567890n)`,
		`const a = []; function* g() { for (;;) yield a.length } for (const x of g()) a.push(x)`,
		`const a = []; for (;;) a.push(new Error("e"))`,
		`const a = []; for (;;) a.push(new Promise(() => {}))`,
		`const a = []; for (;;) a.push(Symbol.iterator.toString().slice(1))`,
		`const a = []; for (;;) a.push("é".repeat(20).toUpperCase())`,
		`class P { constructor(i) { this.i = i } } const a = []; for (;;) a.push(new P(a.length))`,
	}
	for _, src := range cases {
		r := NewRealmWith(RealmOptions{MemoryLimit: 1 << 20})
		_, err := runScriptValue(t, r, src)
		if ex, ok := err.(*Exception); ok {
			// A sized producer (repeat, a typed array) refused what was
			// left of the budget with its RangeError.
			assert.Equal(t, "RangeError", errorDisplayString(ex.Value)[:10], src)
			assert.Greater(t, r.Stats().RequestAllocatedBytes, int64(1<<20-1<<14), src)
			continue
		}
		assert.ErrorIs(t, err, ErrMemoryLimit, src)
		var mle *MemoryLimitError
		if assert.True(t, errors.As(err, &mle), src) {
			assert.Contains(t, mle.Stack, "mem.js:1:", src)
		}
	}
}

// TestMemoryLimitNoAccount: a realm without a limit keeps no account.
func TestMemoryLimitNoAccount(t *testing.T) {
	r := NewRealm()
	_, err := runScriptValue(t, r, `const a = []; for (let i = 0; i < 1000; i++) a.push({i}); a.length`)
	require.NoError(t, err)
	assert.Nil(t, r.mem())
	assert.Equal(t, Stats{ICEntries: len(r.ic), RegisterStackBytes: r.Stats().RegisterStackBytes}, r.Stats())
}

// TestMemoryLimitRopeConcurrentFlatten: goroutines flattening one charged
// rope at once each charge the copy they made to the rope's account (the
// losers' too, an accepted overcount) and never write into the string.
func TestMemoryLimitRopeConcurrentFlatten(t *testing.T) {
	const readers = 8
	for range 20 {
		r := NewRealmWith(RealmOptions{MemoryLimit: 1 << 40})
		v, err := runScriptValue(t, r, `"y" + "x".repeat(4000)`)
		require.NoError(t, err)
		rope := v.AsString()
		require.NotZero(t, rope.flags&strCharged)
		before := r.Stats().AllocatedBytes
		var wg sync.WaitGroup
		got := make([]string, readers)
		for i := range readers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				got[i] = rope.GoString()
			}()
		}
		wg.Wait()
		want := "y" + strings.Repeat("x", 4000)
		for _, g := range got {
			require.Equal(t, want, g)
		}
		charged := r.Stats().AllocatedBytes - before
		assert.GreaterOrEqual(t, charged, int64(4001))
		assert.LessOrEqual(t, charged, int64(readers*4001))
		assert.Zero(t, charged%4001, "whole copies only")
	}
}
