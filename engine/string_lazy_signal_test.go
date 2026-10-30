package engine

import (
	"runtime"
	"strings"
	"sync"
	"testing"
	"weak"

	"github.com/stretchr/testify/require"
)

func TestLazyHashAndEqualityPublishConcurrently(t *testing.T) {
	text := strings.Repeat("中😀a", 1024)
	s := FromHostString(text)
	eager := FromGoString(text)
	want := eager.Hash()
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				if s.Hash() != want || !s.Equals(eager) {
					t.Error("concurrent hash or equality changed the content")
				}
			}
		}()
	}
	wg.Wait()
}

func TestLazyLiveStorageMatchesChargedBlock(t *testing.T) {
	text := strings.Repeat("中", 1024)
	for _, charged := range []bool{false, true} {
		var mem *memAccount
		if charged {
			mem = new(memAccount)
		}
		s := newAccountedLazyString(text, lazyText, mem)
		var seen liveSet
		w := liveWalker{seen: &seen}
		w.str(s)
		want := int64(s.lazyBlockSize() + len(text))
		require.Equal(t, want, w.size, "charged=%v, before decoding", charged)
		w.seen.release()
		_ = s.Len()
		w = liveWalker{seen: &seen}
		w.str(s)
		want += int64(stringHeaderSize + 2*s.Len())
		require.Equal(t, want, w.size, "charged=%v, after decoding", charged)
		w.str(s)
		require.Equal(t, want, w.size, "the same string counts once")
		w.seen.release()
	}
}

func TestLazyDecodeNotificationSurvivesHostInterrupt(t *testing.T) {
	r := NewRealmWith(RealmOptions{MemoryLimit: 4096})
	s := r.hostString(strings.Repeat("中", 65536))
	_ = s.Len()
	require.NoError(t, r.CheckInterrupt(), "a decode notification is not an interruption")
	r.Interrupt("timeout")
	r.ClearInterrupt()
	_, err := runScriptValue(t, r, `let n = 0; for (let i = 0; i < 2; i++) n += i; n`)
	require.ErrorIs(t, err, ErrMemoryLimit)
	runtime.KeepAlive(s)
}

func TestLazyDecodeOldAccountDoesNotChargeNewLimit(t *testing.T) {
	r := NewRealmWith(RealmOptions{MemoryLimit: 4096})
	s := r.hostString(strings.Repeat("中", 65536))
	r.SetMemoryLimit(0)
	r.SetMemoryLimit(1 << 20)
	_ = s.Len()
	_, err := runScriptValue(t, r, `0`)
	require.NoError(t, err)
	require.Less(t, r.Stats().RequestAllocatedBytes, int64(65536))
	require.Zero(t, r.Stats().MemoryLimitHits)
	runtime.KeepAlive(s)
}

func TestLazyStringDoesNotKeepRealmAlive(t *testing.T) {
	s, owner := func() (*String, weak.Pointer[Realm]) {
		r := NewRealmWith(RealmOptions{MemoryLimit: 1 << 20})
		return r.hostString(strings.Repeat("中", 1024)), weak.Make(r)
	}()
	for range 20 {
		runtime.GC()
		if owner.Value() == nil {
			break
		}
	}
	require.Nil(t, owner.Value(), "a retained string must not retain its realm")
	require.Equal(t, 1024, s.Len(), "decoding still works after the realm is gone")
	runtime.KeepAlive(s)
}

// A fold notification shares the interrupt flag with a waiting measurement,
// and a decode on another goroutine may set it at any time: the checks that
// claim the measurement clear flagMeasure alone and still make it.
func TestMeasureClaimKeepsFoldNotification(t *testing.T) {
	r := NewRealmWith(RealmOptions{MemoryLimit: 1 << 20})
	r.interruptFlag.Store(flagMeasure | flagFlat)
	require.True(t, measureRequested(r.interruptFlag.Load()))
	require.True(t, r.takeMeasure())
	require.Equal(t, int32(flagFlat), r.interruptFlag.Load(), "the fold stays pending")
	require.False(t, r.takeMeasure())

	r.interruptFlag.Store(flagInterrupt | flagMeasure | flagFlat)
	require.False(t, measureRequested(r.interruptFlag.Load()), "an interrupt replaces a measurement")
	require.False(t, r.takeMeasure())
	r.interruptFlag.Store(0)

	// The end of a run makes the measurement its charges left pending.
	m := r.mem()
	m.allocated = m.room + 1
	r.interruptFlag.Store(flagMeasure | flagFlat)
	require.NoError(t, r.endRun(nil))
	require.Zero(t, m.allocated, "the run's pending measurement was made")
	require.Zero(t, r.interruptFlag.Load())
}
