package engine

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFromGoStringKinds(t *testing.T) {
	a := FromGoString("hello world, this is ascii text with 40+ bytes")
	assert.Equal(t, strASCII, a.kind)
	assert.True(t, a.IsASCII())
	assert.Equal(t, 46, a.Len())
	assert.Equal(t, uint16('h'), a.At(0))

	u := FromGoString("héllo")
	assert.Equal(t, strUTF16, u.kind)
	assert.Equal(t, 5, u.Len())
	assert.Equal(t, uint16(0xE9), u.At(1))

	e := FromGoString("😀")
	assert.Equal(t, 2, e.Len())
	assert.Equal(t, uint16(0xD83D), e.At(0))
	assert.Equal(t, uint16(0xDE00), e.At(1))
	assert.Equal(t, "😀", e.GoString())

	assert.Same(t, emptyString, FromGoString(""))
	// Invalid UTF-8 becomes U+FFFD, like Go's range loop.
	bad := FromGoString("a\xffb")
	assert.Equal(t, 3, bad.Len())
	assert.Equal(t, uint16(0xFFFD), bad.At(1))
}

func TestIsASCIISWAR(t *testing.T) {
	for n := range 40 {
		s := strings.Repeat("a", n)
		assert.True(t, isASCII(s), n)
		for pos := range n {
			b := []byte(s)
			b[pos] = 0x80
			assert.False(t, isASCII(string(b)), "n=%d pos=%d", n, pos)
			b[pos] = 0xFF
			assert.False(t, isASCII(string(b)), "n=%d pos=%d", n, pos)
		}
	}
}

func TestGoStringZeroCopy(t *testing.T) {
	g := "zero copy ascii"
	s := FromGoString(g)
	assert.Equal(t, g, s.GoString())
	got, ok := s.ASCII()
	assert.True(t, ok)
	assert.Equal(t, g, got)
}

func TestGoStringLoneSurrogates(t *testing.T) {
	cases := []struct {
		name string
		u    []uint16
		want string
	}{
		{"lone high", []uint16{'a', 0xD800, 'b'}, "a�b"},
		{"lone low", []uint16{0xDC00}, "�"},
		{"pair", []uint16{0xD83D, 0xDE00}, "😀"},
		{"high at end", []uint16{'x', 0xD83D}, "x�"},
		{"reversed pair", []uint16{0xDE00, 0xD83D}, "��"},
		{"bmp", []uint16{0x4E2D, 0x6587}, "中文"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, FromUTF16(c.u).GoString())
		})
	}
}

func TestFromUTF16NormalizesASCII(t *testing.T) {
	s := FromUTF16([]uint16{'a', 'b', 'c'})
	assert.Equal(t, strASCII, s.kind)
	assert.Equal(t, "abc", s.GoString())
	assert.True(t, s.Equals(FromGoString("abc")))
	assert.Equal(t, FromGoString("abc").Hash(), s.Hash())
}

func TestConcatAndRope(t *testing.T) {
	a := FromGoString("short")
	b := FromGoString("er")
	c := concat(a, b)
	assert.Equal(t, strASCII, c.kind, "short concat is eager")
	assert.Equal(t, "shorter", c.GoString())

	long1 := FromGoString(strings.Repeat("x", 40))
	long2 := FromGoString(strings.Repeat("y", 30))
	rope := concat(long1, long2)
	require.Equal(t, strRope, rope.kind)
	assert.Equal(t, 70, rope.Len())
	// At forces flattening in place.
	assert.Equal(t, uint16('y'), rope.At(45))
	assert.Equal(t, strASCII, rope.kind)
	assert.Nil(t, rope.left)
	assert.Nil(t, rope.right)
	assert.Equal(t, strings.Repeat("x", 40)+strings.Repeat("y", 30), rope.GoString())
	// Flatten is idempotent.
	rope.flatten()
	assert.Equal(t, strASCII, rope.kind)
	assert.Equal(t, 70, rope.Len())

	// Mixed kinds flatten to utf16.
	mixed := concat(long1, FromGoString(strings.Repeat("é", 30)))
	require.Equal(t, strRope, mixed.kind)
	assert.Equal(t, uint16(0xE9), mixed.At(50))
	assert.Equal(t, strUTF16, mixed.kind)

	// Empty operands are returned as-is.
	assert.Same(t, a, concat(a, emptyString))
	assert.Same(t, a, concat(emptyString, a))
}

func TestDeepRopeFlatten(t *testing.T) {
	// Left-deep rope like `s += "ab"` in a loop; must not recurse deeply.
	s := FromGoString(strings.Repeat("a", 64))
	for range 20000 {
		s = concat(s, FromGoString("ab"))
	}
	assert.Equal(t, 64+40000, s.Len())
	assert.Equal(t, uint16('b'), s.At(64+1))
	assert.Equal(t, strASCII, s.kind)
}

func TestSubstringAcrossKinds(t *testing.T) {
	a := FromGoString("hello world")
	sub := a.Substring(6, 11)
	assert.Equal(t, "world", sub.GoString())
	assert.Equal(t, strASCII, sub.kind)
	assert.Same(t, a, a.Substring(0, 11))
	assert.Same(t, emptyString, a.Substring(3, 3))

	u := FromGoString("héllo wörld")
	assert.Equal(t, "wörld", u.Substring(6, 11).GoString())
	ascPart := u.Substring(2, 5)
	assert.Equal(t, strASCII, ascPart.kind, "ascii-only substring of utf16 normalizes")
	assert.Equal(t, "llo", ascPart.GoString())

	rope := concat(FromGoString(strings.Repeat("a", 40)), FromGoString(strings.Repeat("b", 40)))
	assert.Equal(t, "ab", rope.Substring(39, 41).GoString())
}

func TestCompareAndEquals(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"a", "b", -1},
		{"b", "a", 1},
		{"abc", "abc", 0},
		{"ab", "abc", -1},
		{"abc", "ab", 1},
		{"", "a", -1},
		{"é", "z", 1},       // 0xE9 > 'z'
		{"😀", "\uFFFF", -1}, // surrogate 0xD83D < 0xFFFF (code unit order, not code point)
		{"a😀", "aé", 1},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, FromGoString(c.a).Compare(FromGoString(c.b)), "%q vs %q", c.a, c.b)
		assert.Equal(t, c.want == 0, FromGoString(c.a).Equals(FromGoString(c.b)))
	}
	assert.True(t, FromGoString("héllo").EqualsGoString("héllo"))
	assert.False(t, FromGoString("hello").EqualsGoString("héllo"))
	assert.Equal(t, 0, FromGoString("é").Compare(FromUTF16([]uint16{0xE9})))
}

func TestIndexOf(t *testing.T) {
	s := FromGoString("hello hello")
	sub := FromGoString("llo")
	assert.Equal(t, 2, s.IndexOf(sub, 0))
	assert.Equal(t, 8, s.IndexOf(sub, 3))
	assert.Equal(t, -1, s.IndexOf(sub, 9))
	assert.Equal(t, 3, s.IndexOf(emptyString, 3))
	assert.Equal(t, 11, s.IndexOf(emptyString, 50))
	u := FromGoString("héllo héllo")
	assert.Equal(t, 7, u.IndexOf(FromGoString("éll"), 2))
	assert.Equal(t, -1, FromGoString("abc").IndexOf(FromGoString("é"), 0))
}

func TestStringBuilder(t *testing.T) {
	var sb StringBuilder
	sb.WriteGoString("abc")
	sb.WriteASCII('d')
	sb.WriteUnit('e')
	assert.Equal(t, 5, sb.Len())
	s := sb.String()
	assert.Equal(t, strASCII, s.kind)
	assert.Equal(t, "abcde", s.GoString())

	sb.Reset()
	sb.WriteGoString("abc")
	sb.WriteRune('é')
	sb.WriteRune('😀')
	sb.WriteString(FromGoString("xyz"))
	sb.WriteString(FromGoString("ü"))
	assert.Equal(t, 10, sb.Len())
	u := sb.String()
	assert.Equal(t, strUTF16, u.kind)
	assert.Equal(t, "abcé😀xyzü", u.GoString())

	var empty StringBuilder
	assert.Same(t, emptyString, empty.String())

	var grow StringBuilder
	grow.Grow(100)
	grow.WriteGoString("x")
	assert.Equal(t, "x", grow.String().GoString())
}

// TestStringBuilderLen checks Len through every move between the builder's
// stores (inline, heap ASCII, UTF-16): Len sums all three, which holds only
// while each move empties the store it leaves.
func TestStringBuilderLen(t *testing.T) {
	long := strings.Repeat("a", smallASCIIMax+1)
	for _, c := range []struct {
		name  string
		write func(*StringBuilder)
		want  string
	}{
		{"inline", func(sb *StringBuilder) { sb.WriteGoString("abc") }, "abc"},
		{"spill by a byte", func(sb *StringBuilder) {
			sb.WriteGoString(long[:smallASCIIMax])
			sb.WriteASCII('b')
		}, long[:smallASCIIMax] + "b"},
		{"spill by a string", func(sb *StringBuilder) { sb.WriteGoString("x"); sb.WriteGoString(long) }, "x" + long},
		{"spill by Grow", func(sb *StringBuilder) { sb.WriteGoString("xy"); sb.Grow(smallASCIIMax); sb.WriteASCII('z') }, "xyz"},
		{"upgrade from inline", func(sb *StringBuilder) { sb.WriteGoString("ab"); sb.WriteUnit('é') }, "abé"},
		{"upgrade from heap", func(sb *StringBuilder) { sb.WriteGoString(long); sb.WriteRune('😀') }, long + "😀"},
		{"UTF-16 from the start", func(sb *StringBuilder) {
			sb.growFor(FromGoString("é"), 4)
			sb.WriteString(FromGoString("éa"))
		}, "éa"},
		{"UTF-16 slice after ASCII", func(sb *StringBuilder) {
			sb.WriteGoString("ab")
			writeSlice(sb, FromGoString("xüy"), 1, 3)
		}, "abüy"},
	} {
		var sb StringBuilder
		c.write(&sb)
		want := FromGoString(c.want)
		assert.Equal(t, want.Len(), sb.Len(), c.name)
		assert.Equal(t, c.want, sb.String().GoString(), c.name)
		sb.Reset()
		assert.Zero(t, sb.Len(), c.name)
	}
}

func TestHash(t *testing.T) {
	a := FromGoString("property")
	b := FromGoString("property")
	assert.Equal(t, a.Hash(), b.Hash())
	assert.NotZero(t, a.Hash())
	assert.NotEqual(t, a.Hash(), FromGoString("propertx").Hash())
	rope := concat(FromGoString(strings.Repeat("p", 40)), FromGoString(strings.Repeat("q", 40)))
	flat := FromGoString(strings.Repeat("p", 40) + strings.Repeat("q", 40))
	assert.Equal(t, flat.Hash(), rope.Hash())
}

func TestIsWellFormed(t *testing.T) {
	assert.True(t, FromGoString("abc").IsWellFormed())
	assert.True(t, FromGoString("😀").IsWellFormed())
	assert.False(t, FromUTF16([]uint16{0xD800}).IsWellFormed())
	assert.False(t, FromUTF16([]uint16{0xDC00, 'a'}).IsWellFormed())
}

func TestBigInt(t *testing.T) {
	b, ok := NewBigIntFromDecimal("123456789012345678901234567890")
	require.True(t, ok)
	assert.Equal(t, "123456789012345678901234567890", b.ToString())
	c, _ := NewBigIntFromDecimal("123456789012345678901234567890")
	assert.True(t, b.StrictEquals(c))
	assert.False(t, b.StrictEquals(NewBigIntFromInt64(1)))
	assert.True(t, NewBigIntFromInt64(0).IsZero())
	_, ok = NewBigIntFromDecimal("12x")
	assert.False(t, ok)
	assert.Equal(t, "bigint", TypeOf(BigIntValue(b)).GoString())
}
