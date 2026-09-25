package syntax

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestFilePositionIndex checks the column index against the naive count for
// multi-byte characters straddling every runeStride boundary, on each line
// of a multi-line source and on an all-ASCII source (which has no index).
func TestFilePositionIndex(t *testing.T) {
	var sb strings.Builder
	for i := range 6 * runeStride {
		switch i % 7 {
		case 0:
			sb.WriteString("é") // 2 bytes
		case 3:
			sb.WriteString("中") // 3 bytes
		case 5:
			sb.WriteString("😀") // 4 bytes
		default:
			sb.WriteByte('a')
		}
		if i%50 == 49 {
			sb.WriteByte('\n')
		}
	}
	src := sb.String()
	f := newFile("t.js", src)
	for i := range len(src) {
		if src[i] == '\n' {
			f.addLine(i + 1)
		}
	}
	// Token offsets are always character boundaries.
	lineStart := 0
	line := 1
	for pos := 0; pos <= len(src); pos++ {
		if pos < len(src) && !utf8.RuneStart(src[pos]) {
			continue
		}
		wantLine, wantCol := line, utf8.RuneCountInString(src[lineStart:pos])+1
		gotLine, gotCol := f.Position(pos)
		if gotLine != wantLine || gotCol != wantCol {
			t.Fatalf("Position(%d) = %d:%d, want %d:%d", pos, gotLine, gotCol, wantLine, wantCol)
		}
		if pos < len(src) && src[pos] == '\n' {
			line++
			lineStart = pos + 1
		}
	}

	ascii := newFile("a.js", strings.Repeat("x", 3*runeStride)+"\nyz")
	if ascii.runeCounts != nil {
		t.Fatal("an ASCII source needs no column index")
	}
	ascii.addLine(3*runeStride + 1)
	if l, c := ascii.Position(3*runeStride + 2); l != 2 || c != 2 {
		t.Fatalf("Position = %d:%d, want 2:2", l, c)
	}
}
