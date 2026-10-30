package engine

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestHostChildDeferredAccount checks the actual FromGo container path. The
// container materializes first; the string's bytes remain undecoded until Len.
func TestHostChildDeferredAccount(t *testing.T) {
	r := NewRealmWith(RealmOptions{MemoryLimit: 1 << 20})
	v, err := r.FromGo(map[string]any{"text": strings.Repeat("中", 8192)})
	require.NoError(t, err)
	require.True(t, v.AsObject().materializeHost(), "limited FromGo uses the host heap")
	x, err := v.AsObject().Get(r, key(r, "text"), v)
	require.NoError(t, err)
	s := x.AsString()
	require.True(t, s.isLazy())
	require.Nil(t, s.lazyPublished())
	before := r.mem().flat.Load()
	require.Equal(t, 8192, s.Len())
	require.Equal(t, int64(stringHeaderSize+2*8192), r.mem().flat.Load()-before,
		"a host container's deferred decode must reach its realm's account")
}

func TestHostChildDeferredLimit(t *testing.T) {
	r := NewRealmWith(RealmOptions{MemoryLimit: 64 << 10})
	env := evalModuleOn(t, r, `export function read(v) { return v.text.length; }`)
	r.ResetAllocation()
	v, err := r.FromGo(map[string]any{"text": strings.Repeat("中", 65536)})
	require.NoError(t, err)
	remove := r.AddLiveRoots(func(add func(Value)) { add(v) })
	defer remove()
	fn, ok := env.GetBindingValue("read")
	require.True(t, ok)
	_, err = r.Call(fn, Undefined(), []Value{v})
	require.ErrorIs(t, err, ErrMemoryLimit)
}
