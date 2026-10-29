package engine

import (
	"strings"
	"testing"
)

func TestLazyDecodeStorage(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		mode       lazyStringMode
		want       string
	}{
		{"text-cjk", strings.Repeat("中😀", 16384), lazyText, strings.Repeat("中😀", 16384)},
		{"wtf8-cjk", strings.Repeat("中😀", 16384), lazyWTF8, strings.Repeat("中😀", 16384)},
		{"json-cjk", strings.Repeat("中\\n😀", 16384), lazyJSONContents, strings.Repeat("中\n😀", 16384)},
		{"json-late-wide", strings.Repeat("a\\n", 32768) + "中", lazyJSONContents, strings.Repeat("a\n", 32768) + "中"},
		{"json-escaped-ascii", strings.Repeat("a\\n", 32768), lazyJSONVerbatim, strings.Repeat("a\n", 32768)},
		{"json-unicode", strings.Repeat("\\u4e2d\\ud83d\\ude00", 16384), lazyJSONContents, strings.Repeat("中😀", 16384)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, retained := decodeLazyString(tc.text, tc.mode)
			if !s.Equals(FromGoString(tc.want)) {
				t.Fatal("decoded content differs")
			}
			bytes := s.Len()
			if s.kind == strUTF16 {
				bytes *= 2
			}
			if retained > bytes+stringHeaderSize {
				t.Fatalf("decoded storage = %d, content and header need %d", retained, bytes+stringHeaderSize)
			}
		})
	}
}

func BenchmarkLazyDecode(b *testing.B) {
	for _, tc := range []struct {
		name, text string
		mode       lazyStringMode
	}{
		{"wtf8-cjk", strings.Repeat("中😀", 16384), lazyWTF8},
		{"json-agent", strings.Repeat("text\\n工具返回了内容😀。", 8192), lazyJSONContents},
		{"json-late-wide", strings.Repeat("a\\n", 32768) + "中", lazyJSONContents},
		{"json-ascii", strings.Repeat("a\\n", 32768), lazyJSONContents},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				decodeLazyString(tc.text, tc.mode)
			}
		})
	}
}
