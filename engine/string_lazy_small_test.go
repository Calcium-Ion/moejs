package engine

import (
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLazySmallDecodeAllocations(t *testing.T) {
	for _, n := range []int{1, 8, 9, 16, 17, 24, 25, 32, 33, utf16BufMax, utf16BufMax + 1} {
		for _, mode := range []lazyStringMode{lazyText, lazyWTF8, lazyJSONContents} {
			text := strings.Repeat("中", n)
			if mode == lazyJSONContents {
				text = strings.Repeat(`\u4e2d`, n)
			}
			var decoded *String
			allocs := testing.AllocsPerRun(50, func() {
				decoded, _ = decodeLazyString(text, mode)
			})
			want := 1.0
			if n > utf16BufMax {
				want = 2
			}
			require.Equal(t, want, allocs, "n=%d, mode=%d", n, mode)
			require.Equal(t, strings.Repeat("中", n), decoded.GoString())
		}
	}
}

func TestLazySmallDecodeSurrogatesAndLifetime(t *testing.T) {
	for _, n := range []int{utf16BufMax, utf16BufMax + 1} {
		// A pair and a lone surrogate straddle the small-buffer cutoff.
		text := strings.Repeat("a", n-3) + "😀" + "\xed\xa0\x80"
		s, retained := decodeLazyString(text, lazyWTF8)
		require.Equal(t, n, s.Len())
		require.Equal(t, stringHeaderSize+2*n, retained)
		sub := s.Substring(n-3, n)
		s = nil
		runtime.GC()
		require.Equal(t, []uint16{0xd83d, 0xde00, 0xd800}, sub.UTF16())
	}
}

func TestLazySmallDecodeChargeOnce(t *testing.T) {
	for _, n := range []int{1, 8, 9, 16, 17, 24, 25, 32, 33, utf16BufMax, utf16BufMax + 1} {
		r := NewRealmWith(RealmOptions{MemoryLimit: 1 << 20})
		s := r.hostString(strings.Repeat("中", n))
		require.True(t, s.isLazy())
		require.Nil(t, s.lazyPublished())
		require.Equal(t, n, s.Len())
		want := int64(stringHeaderSize + 2*n)
		require.Equal(t, want, r.mem().flat.Load())
		_ = s.Len()
		require.Equal(t, want, r.mem().flat.Load(), "decoded storage is charged only once")
	}
}
