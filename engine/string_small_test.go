package engine

import (
	"strconv"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
)

// TestSmallASCIICoallocation checks that strings built through the
// co-allocated small-string path (Concat, StringBuilder, case conversion,
// NumberToString) hold the right content across the size buckets and the
// boundary to separately allocated payloads.
func TestSmallASCIICoallocation(t *testing.T) {
	r := NewRealm()
	for n := 1; n <= smallASCIIMax+8; n++ {
		src := strings.Repeat("Ab", n/2) + strings.Repeat("c", n%2)
		a, b := src[:n/2], src[n/2:]
		got := concat(asciiString(a), asciiString(b))
		assert.Equal(t, src, got.GoString(), "concat n=%d", n)
		assert.Equal(t, n, got.Len())

		var sb StringBuilder
		for i := range n {
			sb.WriteASCII(src[i])
		}
		assert.Equal(t, src, sb.String().GoString(), "builder n=%d", n)
		sb.Reset()
		sb.WriteGoString(src)
		assert.Equal(t, src, sb.String().GoString(), "builder string n=%d", n)

		lower, _ := stringToLower(r, asciiString(src))
		assert.Equal(t, strings.ToLower(src), lower.GoString(), "lower n=%d", n)
		upper, _ := stringToUpper(r, asciiString(src))
		assert.Equal(t, strings.ToUpper(src), upper.GoString(), "upper n=%d", n)
		// The source is untouched by the in-place case conversion.
		assert.Equal(t, strings.Repeat("Ab", n/2)+strings.Repeat("c", n%2), src)
	}
	// A builder that upgrades to UTF-16 after inline ASCII bytes keeps them.
	var sb StringBuilder
	sb.WriteGoString("abc")
	sb.WriteUnit(0x4E2D)
	sb.WriteGoString("def")
	assert.Equal(t, "abc中def", sb.String().GoString())
	// A builder that spills past the inline array keeps the prefix.
	sb.Reset()
	long := strings.Repeat("x", smallASCIIMax-1) + "yz" + strings.Repeat("w", 100)
	for i := range len(long) {
		sb.WriteASCII(long[i])
	}
	assert.Equal(t, len(long), sb.Len())
	assert.Equal(t, long, sb.String().GoString())

	for _, f := range []float64{0, 7, 10, -3, 12345, 1e21, 0.5, -1.25e-7, 1234567890123456} {
		assert.Equal(t, NumberToGoString(f), NumberToString(f).GoString(), "number %v", f)
	}
	assert.Equal(t, "42", NumberToString(42).GoString())
	assert.Equal(t, strconv.FormatInt(1<<40, 10), NumberToString(1<<40).GoString())
}

func TestSmallASCIIAllocations(t *testing.T) {
	a, b := asciiString("Content-"), asciiString("Type")
	assert.Equal(t, 1.0, testing.AllocsPerRun(50, func() { concat(a, b) }))
	mixed := asciiString("X-Request-Id")
	r := NewRealm()
	assert.Equal(t, 1.0, testing.AllocsPerRun(50, func() { stringToLower(r, mixed) }))
	assert.Equal(t, 1.0, testing.AllocsPerRun(50, func() { NumberToString(370) }))
	assert.Equal(t, 1.0, testing.AllocsPerRun(50, func() {
		var sb StringBuilder
		sb.WriteString(a)
		sb.WriteASCII(':')
		sb.WriteString(b)
		sb.String()
	}))
}

// TestStringSize pins String to the 48-byte size class, with the small
// co-allocated strings filling theirs: a field added to String moves every
// string, rope node and split piece up a class.
func TestStringSize(t *testing.T) {
	assert.Equal(t, uintptr(48), unsafe.Sizeof(String{}))
	assert.Equal(t, uintptr(64), unsafe.Sizeof(asciiBuf16{}))
	assert.Equal(t, uintptr(80), unsafe.Sizeof(asciiBuf32{}))
	assert.Equal(t, uintptr(112), unsafe.Sizeof(asciiBuf64{}))
}
