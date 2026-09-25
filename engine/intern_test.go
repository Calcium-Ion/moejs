package engine

import (
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"weak"

	"github.com/Calcium-Ion/moejs/compiler"
	"github.com/Calcium-Ion/moejs/syntax"
	"github.com/stretchr/testify/require"
)

// TestInternCrossRealmKeysAfterManyNames is an audit reproducer,
// generalized: after the process-wide table has absorbed far more names
// than a per-shard cap would hold, a property key that one realm's module
// materializes into the shared funcMeta must be the same pointer every
// other realm produces through FromGo, so `o.prop` reads the value in each
// realm. With the cap, the first realm's key was realm-local and the second
// realm read undefined.
func TestInternCrossRealmKeysAfterManyNames(t *testing.T) {
	const prop = "internTestProp"
	filler := NewRealmWith(RealmOptions{SharedIntrinsics: true})
	names := make([]string, 40_000)
	for i := range names {
		names[i] = "intern_fill_" + strconv.Itoa(i)
		filler.InternGoString(names[i])
	}
	m, err := syntax.ParseModule("intern.js", "export function get(o) { return o."+prop+"; }", syntax.Options{})
	require.NoError(t, err)
	code, err := compiler.CompileModule(m)
	require.NoError(t, err)
	call := func(r *Realm) any {
		env, err := r.EvaluateModule(code)
		require.NoError(t, err)
		fn, _ := env.GetBindingValue("get")
		arg, err := r.FromGo(map[string]any{prop: 42})
		require.NoError(t, err)
		res, err := r.Call(fn, Undefined(), []Value{arg})
		require.NoError(t, err)
		return r.ToGo(res)
	}
	r1 := NewRealmWith(RealmOptions{SharedIntrinsics: true})
	r2 := NewRealmWith(RealmOptions{SharedIntrinsics: true})
	require.Equal(t, int64(42), call(r1), "first realm")
	require.Equal(t, int64(42), call(r2), "second realm reads through the shared funcMeta")
	// Every name has one process-wide identity.
	for _, i := range []int{0, 1, 999, 20_000, 39_999} {
		require.Same(t, filler.InternGoString(names[i]), r2.InternGoString(names[i]), names[i])
	}
	require.Same(t, r1.InternGoString(prop), r2.InternGoString(prop))
}

// TestInternTableConcurrent interns the same names from many realms on
// many goroutines at once: every realm must end up with the same pointer
// for each name (the insert race resolves to one winner).
func TestInternTableConcurrent(t *testing.T) {
	const goroutines, names = 16, 2000
	results := make([][]*String, goroutines)
	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := NewRealmWith(RealmOptions{SharedIntrinsics: true})
			out := make([]*String, names)
			for i := range names {
				out[i] = r.InternGoString("intern_race_" + strconv.Itoa(i))
			}
			results[g] = out
		}()
	}
	wg.Wait()
	for i := range names {
		for g := 1; g < goroutines; g++ {
			require.Same(t, results[0][i], results[g][i], "goroutine %d name %d", g, i)
		}
		require.True(t, results[0][i].IsInterned())
	}
}

// internOnce interns name in a throwaway realm and returns only a weak
// pointer to the atom, so nothing keeps it alive.
//
//go:noinline
func internOnce(name string) weak.Pointer[String] {
	r := NewRealm()
	return weak.Make(r.InternGoString(name))
}

// TestInternTableReleasesUnusedNames checks that the table does not pin
// names: an atom referenced by no realm is collected, and the name interns
// again afterwards (the dead entry is replaced or already dropped).
func TestInternTableReleasesUnusedNames(t *testing.T) {
	const name = "intern_released_only_here"
	wp := internOnce(name)
	runtime.GC()
	runtime.GC()
	require.Nil(t, wp.Value(), "an atom no realm references must be collectable")
	r := NewRealm()
	again := r.InternGoString(name)
	require.True(t, again.IsInterned())
	require.Same(t, again, NewRealm().InternGoString(name))
	require.Same(t, again, globalInternASCII.lookup(name))
}

// TestAtomIDWrapSkipsStaticIDs checks that a wrapped atom counter resumes
// above the static atoms rather than handing out their ids again.
func TestAtomIDWrapSkipsStaticIDs(t *testing.T) {
	var c atomic.Uint32
	c.Store(^uint32(0) - 1)
	require.Equal(t, ^uint32(0), takeAtomID(&c))
	require.Equal(t, uint32(staticAtomCount+1), takeAtomID(&c))
	require.Equal(t, uint32(staticAtomCount+2), takeAtomID(&c))
}
