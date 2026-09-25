package engine

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSharedRealmBuiltinsB checks that the String, RegExp, JSON, Date and URI
// intrinsics are part of the frozen shared template and work from shared
// realms, including from several goroutines at once.
func TestSharedRealmBuiltinsB(t *testing.T) {
	r1, r2 := newShared(), newShared()
	assert.Same(t, r1.StringPrototype, r2.StringPrototype)
	assert.Same(t, r1.RegExpCtor, r2.RegExpCtor)
	assert.Same(t, r1.JSON, r2.JSON)
	assert.Same(t, r1.DatePrototype, r2.DatePrototype)
	for _, o := range []*Object{r1.StringPrototype, r1.StringCtor, r1.RegExpPrototype, r1.RegExpCtor, r1.JSON, r1.DatePrototype, r1.DateCtor} {
		assert.True(t, o.IsShared(), o.debugString())
		assert.True(t, o.IsFrozen(), o.debugString())
		for _, k := range o.OwnPropertyKeys() {
			d, _ := o.GetOwnProperty(k)
			assert.False(t, d.Configurable(), "%s.%s", o.debugString(), k.GoString())
			if d.IsAccessorDescriptor() {
				assert.True(t, d.GetterObject().IsShared())
				continue
			}
			assert.False(t, d.Writable(), "%s.%s", o.debugString(), k.GoString())
			if d.Value.IsObject() {
				assert.True(t, d.Value.AsObject().IsShared(), "%s.%s", o.debugString(), k.GoString())
				if fd := d.Value.AsObject().FunctionData(); fd != nil {
					assert.Nil(t, fd.Realm())
				}
			}
		}
	}
	for _, name := range []string{"encodeURIComponent", "decodeURIComponent", "encodeURI", "decodeURI"} {
		v, err := r1.Global.GetProp(r1, r1.KeyFromGoString(name))
		require.NoError(t, err)
		assert.True(t, v.AsObject().IsShared(), name)
		gv, _ := r2.Global.GetProp(r2, r2.KeyFromGoString(name))
		assert.Same(t, v.AsObject(), gv.AsObject())
	}
	// Every listed String.prototype method is present.
	for _, name := range []string{"charAt", "charCodeAt", "codePointAt", "indexOf", "lastIndexOf", "includes", "startsWith", "endsWith", "slice", "substring", "substr", "toLowerCase", "toUpperCase", "trim", "trimStart", "trimEnd", "split", "replace", "replaceAll", "match", "matchAll", "repeat", "padStart", "padEnd", "concat", "at", "localeCompare", "normalize", "toString", "valueOf"} {
		assert.True(t, r1.StringPrototype.HasOwnProperty(r1.KeyFromGoString(name)), name)
	}
	// Writes to the shared intrinsics are rejected; realm-local state works.
	assert.ErrorContains(t, r1.StringPrototype.SetProp(r1, StringKey(AtomTrim), IntValue(1)), "shared intrinsic")
	assert.ErrorContains(t, r1.JSON.SetProp(r1, StringKey(AtomParse), IntValue(1)), "shared intrinsic")

	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := newShared()
			for i := range 100 {
				s := callMethod(t, r, str("  Hello, World  "), "trim")
				assert.Equal(t, "Hello, World", s.AsString().GoString())
				parts := callMethod(t, r, s, "split", str(", "))
				assert.Equal(t, []any{"Hello", "World"}, jsArrayB(r, parts))
				rx := newRegExp(t, r, `o(\w)`, "g")
				rep := callMethod(t, r, s, "replace", rx, str("0$1"))
				assert.Equal(t, "Hello, W0rld", rep.AsString().GoString())
				assert.Equal(t, "0", propString(t, r, rx, "lastIndex"))
				v, err := callMethodErr(r, ObjectValue(r.JSON), "parse", str(`{"g":`+NumberToGoString(float64(g))+`,"i":`+NumberToGoString(float64(i))+`}`))
				require.NoError(t, err)
				out, err := callMethodErr(r, ObjectValue(r.JSON), "stringify", v)
				require.NoError(t, err)
				assert.Equal(t, `{"g":`+NumberToGoString(float64(g))+`,"i":`+NumberToGoString(float64(i))+`}`, out.AsString().GoString())
				d, err := r.Construct(ObjectValue(r.DateCtor), []Value{IntValue(i)}, nil)
				require.NoError(t, err)
				assert.Equal(t, float64(i), callMethod(t, r, d, "getTime").AsNumber())
				enc, err := callGlobal(t, r, "encodeURIComponent", str("a b"))
				require.NoError(t, err)
				assert.Equal(t, "a%20b", enc.AsString().GoString())
			}
		}()
	}
	wg.Wait()
	// Regexp caches are per realm even with shared intrinsics.
	newRegExp(t, r1, `x`, "")
	assert.NotNil(t, r1.regexps)
	assert.Nil(t, r2.regexps)
}

func TestBuiltinsBRealmCreationStaysCheap(t *testing.T) {
	NewRealm()
	allocs := testing.AllocsPerRun(50, func() { NewRealm() })
	t.Logf("allocs per mutable realm: %.0f", allocs)
	// A mutable realm materializes ~300 builtin function objects, plus
	// Symbol, the iterator prototypes and the keyed collections (Map, Set,
	// the weak collections, WeakRef). The plugin host uses shared
	// intrinsics (asserted below).
	assert.LessOrEqual(t, allocs, 1100.0, "mutable realm allocation budget")
	newShared()
	shared := testing.AllocsPerRun(50, func() { newShared() })
	assert.LessOrEqual(t, shared, 20.0)
}
