package engine

import (
	"slices"
	"sync"
	"unicode"
)

//go:generate go run ../internal/gen/decomp -o decomp_tables.go

// String.prototype.localeCompare without Intl. The host locale is en-US,
// whose collation is the CLDR root order; this is a reduced form of the
// Unicode Collation Algorithm over that order, not the full DUCET:
//
//   - Strings are compared on three levels: base characters first, then
//     accents, then case and variant forms. There is no fourth (identical)
//     level, so strings that differ only in ignorable characters compare
//     equal, as they do under ICU's default strength.
//   - Both strings are decomposed (canonical and compatibility mappings,
//     Hangul algorithmically) and combining marks are put in canonical
//     order, so canonically equivalent strings always compare equal.
//     Compatibility forms (ﬁ, ², ｱ, the no-break space) sort as their
//     decomposition with a tertiary difference.
//   - Base characters sort by group: whitespace, punctuation, symbols,
//     currency signs, digits (by value, all scripts alike), Latin letters,
//     other letters, Han ideographs, and last unassigned and private-use
//     code points and lone surrogates. The common ASCII, Latin-1 and CJK
//     punctuation and symbols follow CLDR root's order (the lists below);
//     other members of each group follow by code point.
//   - Latin and Cyrillic follow CLDR root: đ ð ħ ł ø ґ are accent variants
//     of d h l o г, æ œ ß sort as ae oe ss after every accented spelling,
//     and the letters in collLatinOrder and collCyrillicOrder have their own
//     place (þ after z, ґ before ғ). The Cyrillic letters that decompose
//     but are letters of their own (й ї ӓ ў ...) are contractions of their
//     base letter and mark. Other letters sort by the code point of their
//     lowercase form, which keeps Greek in alphabetical order.
//   - Accents sort in CLDR root's secondary order for the marks of the
//     Combining Diacritical Marks block (collMarkOrder), the rest by code
//     point. Lowercase sorts before uppercase; hiragana before katakana
//     before halfwidth katakana, and small kana are variants of the
//     full-size ones.
//   - Other contractions and language tailorings are not implemented: the
//     locales and options arguments are ignored. Letter case and categories
//     come from Go's unicode package, decompositions from Unicode 17.0.0.
//
// The ASCII-only comparison is a table walk that allocates nothing.

// collElem is a collation element; a zero weight is skipped at its level.
type collElem struct {
	p uint32 // primary: the collation group << 24 | the key within it
	s uint32 // secondary
	t uint8  // tertiary
}

// Collation groups, the top byte of a primary weight, in collation order.
const (
	collSpace = 1 + iota
	collPunct
	collSymbol
	collCurrency
	collDigit
	collLatin
	collLetter
	collHan
	collUnassigned
)

// collUnlisted is added to the code point of a whitespace, punctuation,
// symbol or currency character that is not in the ordered lists, so it
// sorts after the listed members of its group.
const collUnlisted = 0x1000

// Secondary weights. A mark listed in collMarkOrder at index k weighs
// 2+2k; one more than U+0361's weight is the variant weight of ð æ œ ß ґ.
const (
	collSecBase  = 1     // every base character
	collSecOther = 0x100 // + code point: a mark missing from collMarkOrder
)

// Tertiary weights, after UTS #10 table 14 with CLDR root's kana order.
const (
	collTerSmallHiragana  = 0x0D
	collTerHiragana       = 0x0E
	collTerSmallKatakana  = 0x0F
	collTerKatakana       = 0x11
	collTerNarrowKatakana = 0x12
	collTerFinal          = 0x19
)

// collTagTertiary is the tertiary weight of a lowercase or uncased
// character reached through a decomposition with that tag; an uppercase
// one weighs 6 more when the result is at most 6.
var collTagTertiary = [...]uint8{
	decompCanonical: 2, decompWide: 3, decompNarrow: 3, decompCompat: 4, decompFont: 5, decompCircle: 6,
	decompSmall: 0x0D, decompSuper: 0x14, decompSub: 0x15, decompVertical: 0x16, decompInitial: 0x17,
	decompMedial: 0x18, decompFinal: 0x19, decompIsolated: 0x1A, decompNoBreak: 0x1B, decompSquare: 0x1C,
	decompFraction: 0x1E,
}

func collTertiary(tag uint8, upper bool) uint8 {
	t := collTagTertiary[tag]
	if upper && t <= 6 {
		t += 6
	}
	return t
}

// The CLDR root order of the whitespace, punctuation, symbol and currency
// characters of ASCII, Latin-1, General Punctuation and CJK Symbols and
// Punctuation that have no decomposition.
var (
	collSpaceOrder = []rune{
		0x0009, 0x000A, 0x000B, 0x000C, 0x000D, 0x0085, 0x2028, 0x2029, 0x0020,
	}
	collPunctOrder = []rune{
		0x005F, 0x002D, 0x2010, 0x2012, 0x2013, 0x2014, 0x2015, 0x2053, 0x301C, 0x3030, 0x002C, 0x3001,
		0x003B, 0x204F, 0x003A, 0x0021, 0x00A1, 0x003F, 0x00BF, 0x203D, 0x002E, 0x3002, 0x00B7, 0x2055,
		0x2056, 0x2058, 0x2059, 0x205A, 0x205B, 0x205C, 0x205D, 0x205E, 0x0027, 0x2018, 0x2019, 0x201A,
		0x201B, 0x2039, 0x203A, 0x0022, 0x201C, 0x201D, 0x201E, 0x201F, 0x301D, 0x301E, 0x301F, 0x00AB,
		0x00BB, 0x0028, 0x0029, 0x005B, 0x005D, 0x007B, 0x007D, 0x2045, 0x2046, 0x3008, 0x3009, 0x300A,
		0x300B, 0x300C, 0x300D, 0x300E, 0x300F, 0x3010, 0x3011, 0x3014, 0x3015, 0x3016, 0x3017, 0x3018,
		0x3019, 0x301A, 0x301B, 0x2016, 0x00A7, 0x00B6, 0x204B, 0x0040, 0x002A, 0x204E, 0x2051, 0x002F,
		0x005C, 0x0026, 0x204A, 0x0023, 0x0025, 0x2030, 0x2031, 0x2020, 0x2021, 0x2022, 0x2023, 0x2027,
		0x2043, 0x204C, 0x204D, 0x2032, 0x2035, 0x3003, 0x303D, 0x2038, 0x203B, 0x203F, 0x2054, 0x2040,
		0x2050, 0x2041, 0x2042,
	}
	collSymbolOrder = []rune{
		0x0060, 0x005E, 0x00B0, 0x00A9, 0x00AE, 0x002B, 0x00B1, 0x00F7, 0x00D7, 0x003C, 0x003D, 0x003E,
		0x00AC, 0x007C, 0x00A6, 0x007E, 0x2052, 0x2044, 0x3004, 0x3012, 0x3013, 0x3020, 0x3037, 0x303E,
		0x303F,
	}
	collCurrencyOrder = []rune{0x00A4, 0x00A2, 0x0024, 0x00A3, 0x00A5}
)

// collLatinOrder lists the lowercase Latin letters with a primary weight of
// their own in CLDR root order: the basic letters and the Latin Extended
// letters that sort after them.
var collLatinOrder = []rune{
	'a', 0x2C65, 'b', 0x0180, 0x0253, 0x0183, 'c', 0x023C, 0x0188, 'd', 0x0238, 0x0256, 0x0257, 0x018C,
	0x0221, 'e', 0x0247, 0x01DD, 0x0259, 0x025B, 'f', 0x0192, 'g', 0x01E5, 0x0260, 0x0263, 0x01A3, 'h',
	0x0195, 'i', 0x0131, 0x0268, 0x0269, 'j', 0x0237, 0x0249, 'k', 0x0199, 'l', 0x019A, 0x0234, 0x019B,
	'm', 'n', 0x0272, 0x019E, 0x0235, 0x014B, 'o', 0x0254, 0x0275, 0x0223, 'p', 0x01A5, 'q', 0x0239,
	0x024B, 0x0138, 'r', 0x0280, 0x024D, 's', 0x023F, 0x0283, 0x01AA, 't', 0x01BE, 0x0167, 0x2C66, 0x01AB,
	0x01AD, 0x0288, 0x0236, 'u', 0x0289, 0x026F, 0x028A, 'v', 0x028B, 0x028C, 'w', 'x', 'y', 0x024F,
	0x01B4, 0x021D, 'z', 0x018D, 0x01B6, 0x0225, 0x0240, 0x0292, 0x01B9, 0x01BA, 0x00FE, 0x01BF, 0x01BB,
	0x01A8, 0x01BD, 0x0185, 0x0242, 0x01C0, 0x01C1, 0x01C2, 0x01C3,
}

// collMarkOrder is CLDR root's secondary order of the combining marks of
// U+0300..U+036F that have no decomposition (U+034F is ignorable).
var collMarkOrder = [...]rune{
	0x0332, 0x0313, 0x0314, 0x0301, 0x0300, 0x0306, 0x0302, 0x030C, 0x030A, 0x0342, 0x0308, 0x030B,
	0x0303, 0x0307, 0x0338, 0x0327, 0x0328, 0x0304, 0x030D, 0x030E, 0x0312, 0x0315, 0x031A, 0x033D,
	0x033E, 0x033F, 0x0346, 0x034A, 0x034B, 0x034C, 0x0350, 0x0351, 0x0352, 0x0357, 0x035B, 0x035D,
	0x035E, 0x0316, 0x0317, 0x0318, 0x0319, 0x031C, 0x031D, 0x031E, 0x031F, 0x0320, 0x0329, 0x032A,
	0x032B, 0x032C, 0x032F, 0x0333, 0x033A, 0x033B, 0x033C, 0x0347, 0x0348, 0x0349, 0x034D, 0x034E,
	0x0353, 0x0354, 0x0355, 0x0356, 0x0359, 0x035A, 0x035C, 0x035F, 0x0362, 0x0336, 0x0337, 0x0305,
	0x0309, 0x030F, 0x0310, 0x0311, 0x031B, 0x0321, 0x0322, 0x0323, 0x0324, 0x0325, 0x0326, 0x032D,
	0x032E, 0x0330, 0x0331, 0x0334, 0x0335, 0x0339, 0x0345, 0x0358, 0x0360, 0x0361, 0x0363, 0x0368,
	0x0369, 0x0364, 0x036A, 0x0365, 0x036B, 0x0366, 0x036C, 0x036D, 0x0367, 0x036E, 0x036F,
}

// collCyrillicOrder lists the lowercase Cyrillic letters with a primary
// weight of their own in CLDR root order. Some of them (й ӓ ї ...) have a
// canonical decomposition; their base letter and mark form a contraction.
var collCyrillicOrder = []rune{
	0x0430, 0x04D1, 0x04D3, 0x04D9, 0x04DB, 0x04D5, 0x0431, 0x0432, 0x0433, 0x0493, 0x04FB, 0x0495,
	0x04F7, 0x0434, 0x0501, 0xA681, 0x0452, 0xA663, 0x0503, 0x0453, 0x0499, 0x0435, 0x04D7, 0x0454,
	0x0436, 0xA685, 0x04DD, 0x0497, 0x0437, 0xA641, 0x0505, 0x0511, 0x04DF, 0xA643, 0x0455, 0xA645,
	0x04E1, 0xA689, 0x0507, 0xA683, 0x0438, 0x048B, 0x04E5, 0x0456, 0xA647, 0x0457, 0x0439, 0x0458,
	0xA649, 0x043A, 0x049B, 0x04C4, 0x04A1, 0x049F, 0x049D, 0x051F, 0x051B, 0x043B, 0x04C6, 0x0513,
	0x0521, 0x0459, 0xA665, 0x0509, 0x0515, 0x043C, 0x04CE, 0xA667, 0x043D, 0x04CA, 0x04A3, 0x04C8,
	0x0523, 0x04A5, 0x045A, 0x050B, 0x043E, 0x04E7, 0x04E9, 0x04EB, 0x043F, 0x0525, 0x04A7, 0x0481,
	0x0440, 0x048F, 0x0517, 0x0441, 0x050D, 0x04AB, 0x0442, 0xA68D, 0x050F, 0x04AD, 0xA68B, 0x045B,
	0x045C, 0x0443, 0x045E, 0x04F1, 0x04F3, 0x04AF, 0x04B1, 0xA64B, 0x0479, 0x0444, 0x0445, 0x04FD,
	0x04FF, 0x04B3, 0x04BB, 0x0527, 0xA695, 0x0461, 0x047F, 0xA64D, 0x047D, 0x047B, 0x0446, 0xA661,
	0xA68F, 0x04B5, 0xA691, 0x0447, 0xA693, 0x04F5, 0x04B7, 0x04CC, 0x04B9, 0xA687, 0x04BD, 0x04BF,
	0x045F, 0x0448, 0xA697, 0x0449, 0xA64F, 0x044A, 0xA651, 0x044B, 0x04F9, 0x044C, 0x048D, 0x0463,
	0xA653, 0x044D, 0x04ED, 0x044E, 0xA655, 0xA657, 0x044F, 0x0519, 0x0465, 0x0467, 0xA659, 0x046B,
	0xA65B, 0x0469, 0xA65D, 0x046D, 0x046F, 0x0471, 0x0473, 0x0475, 0x0477, 0xA65F, 0x04A9, 0x051D,
	0x04CF,
}

// collCyrillicKey is the key within collLetter of the first entry of
// collCyrillicOrder: Cyrillic sorts after Greek and before the letters
// whose code points follow it.
const collCyrillicKey = 0x400 << 2

// collVariant returns how CLDR root sorts a lowercase letter that has no
// decomposition but is not a letter of its own: a base letter followed by
// the secondary weight of mark (the variant weight when mark is 0) and, for
// æ œ ß, a second letter.
func collVariant(c rune) (base, second, mark rune, ok bool) {
	switch c {
	case 0x00F8: // ø
		return 'o', 0, 0x0338, true
	case 0x0111: // đ
		return 'd', 0, 0x0335, true
	case 0x0127: // ħ
		return 'h', 0, 0x0335, true
	case 0x0142: // ł
		return 'l', 0, 0x0335, true
	case 0x00F0: // ð
		return 'd', 0, 0, true
	case 0x00E6: // æ
		return 'a', 'e', 0, true
	case 0x0153: // œ
		return 'o', 'e', 0, true
	case 0x00DF: // ß
		return 's', 's', 0, true
	case 0x0491: // ґ
		return 0x0433, 0, 0, true
	}
	return 0, 0, 0, false
}

// collTables holds the decoded order lists, built on first use and shared
// by every realm.
var collTables struct {
	once    sync.Once
	ascii   [128]collElem
	ranked  map[rune]uint32  // primary of every character in the order lists
	marks   [0x70]uint8      // 1 + index in collMarkOrder of U+0300..U+036F, or 0
	variant uint32           // the secondary weight of ð and the middle of æ
	contr   map[[2]rune]rune // a Cyrillic base letter and mark: the letter they spell
}

func loadCollTables() {
	t := &collTables
	t.ranked = make(map[rune]uint32, len(collSpaceOrder)+len(collPunctOrder)+len(collSymbolOrder)+
		len(collCurrencyOrder)+len(collLatinOrder)+len(collCyrillicOrder))
	for _, g := range [...]struct {
		group uint32
		list  []rune
	}{
		{collSpace, collSpaceOrder}, {collPunct, collPunctOrder}, {collSymbol, collSymbolOrder},
		{collCurrency, collCurrencyOrder}, {collLatin, collLatinOrder},
	} {
		for i, c := range g.list {
			t.ranked[c] = g.group<<24 | uint32(i+1)
		}
	}
	t.contr = make(map[[2]rune]rune)
	for i, c := range collCyrillicOrder {
		t.ranked[c] = collLetter<<24 | uint32(collCyrillicKey+i)
		if _, m := decomposition(c); m != nil {
			t.contr[[2]rune{m[0], m[1]}] = c
		}
	}
	for k, c := range collMarkOrder {
		t.marks[c-0x300] = uint8(k + 1)
	}
	t.variant = collMarkWeight(0x0361) + 1
	for c := range rune(0x80) {
		switch {
		case c >= '0' && c <= '9':
			t.ascii[c] = collElem{p: collDigit<<24 | uint32(c-'0'), s: collSecBase, t: collTertiary(0, false)}
		case c >= 'A' && c <= 'Z':
			t.ascii[c] = collElem{p: t.ranked[c+'a'-'A'], s: collSecBase, t: collTertiary(0, true)}
		default:
			if p, ok := t.ranked[c]; ok {
				t.ascii[c] = collElem{p: p, s: collSecBase, t: collTertiary(0, false)}
			}
			// The remaining controls are ignorable and stay zero.
		}
	}
}

// collMarkWeight returns the secondary weight of the combining mark c.
func collMarkWeight(c rune) uint32 {
	if c >= 0x300 && c < 0x370 {
		if k := collTables.marks[c-0x300]; k != 0 {
			return 2 * uint32(k)
		}
	}
	return collSecOther + uint32(c)
}

// collIgnorableMark reports whether the mark c is completely ignorable:
// the combining grapheme joiner and the variation selectors.
func collIgnorableMark(c rune) bool {
	return c == 0x034F || c >= 0x180B && c <= 0x180F && c != 0x180E || c >= 0xFE00 && c <= 0xFE0F ||
		c >= 0xE0100 && c <= 0xE01EF
}

// collKana returns the hiragana key and the tertiary weight of the kana c.
func collKana(c rune, tag uint8) (rune, uint8) {
	kata := c >= 0x30A1
	if kata {
		c -= 0x60
	}
	small := true
	switch c {
	case 0x3041, 0x3043, 0x3045, 0x3047, 0x3049, 0x3063, 0x3083, 0x3085, 0x3087, 0x308E:
		c++
	case 0x3095:
		c = 0x304B
	case 0x3096:
		c = 0x3051
	default:
		small = false
	}
	switch {
	case !kata && small:
		return c, collTerSmallHiragana
	case !kata:
		return c, collTerHiragana
	case tag == decompNarrow:
		return c, collTerNarrowKatakana
	case small:
		return c, collTerSmallKatakana
	}
	return c, collTerKatakana
}

// collDigitValue returns the value of the decimal digit c: Nd characters come
// in contiguous runs from 0 to 9.
func collDigitValue(c rune) uint32 {
	start := c
	for start > 0 && c-start < 60 && unicode.IsDigit(start-1) {
		start--
	}
	return uint32(c-start) % 10
}

// collAppend appends the collation elements of the fully decomposed code
// point c, reached through a decomposition tagged tag, to out.
func collAppend(out []collElem, c rune, tag, ccc uint8) []collElem {
	t := &collTables
	if c < 0x80 {
		e := t.ascii[c]
		if e.p == 0 {
			return out
		}
		e.t = collTertiary(tag, c >= 'A' && c <= 'Z')
		return append(out, e)
	}
	if ccc != 0 || unicode.In(c, unicode.Mn, unicode.Me) {
		if collIgnorableMark(c) {
			return out
		}
		return append(out, collElem{s: collMarkWeight(c), t: collTertiary(tag, false)})
	}
	upper := unicode.IsUpper(c) || unicode.IsTitle(c)
	lower := c
	if upper {
		lower = unicode.ToLower(c)
	}
	ter := collTertiary(tag, upper)
	if base, second, mark, ok := collVariant(lower); ok {
		sec := t.variant
		if mark != 0 {
			sec = collMarkWeight(mark)
		}
		out = append(out, collElem{p: t.ranked[base], s: collSecBase, t: ter}, collElem{s: sec, t: ter})
		if second != 0 {
			out = append(out, collElem{p: t.ranked[second], s: collSecBase, t: ter})
		}
		return out
	}
	if p, ok := t.ranked[lower]; ok {
		return append(out, collElem{p: p, s: collSecBase, t: ter})
	}
	var p uint32
	switch {
	case lower >= 0x3041 && lower <= 0x3096 || lower >= 0x30A1 && lower <= 0x30F6:
		var key rune
		key, ter = collKana(lower, tag)
		p = collLetter<<24 | uint32(key)<<2
	case lower == 0x03C2: // final sigma, a variant of σ
		p, ter = collLetter<<24|0x03C3<<2, collTerFinal
	case unicode.Is(unicode.Unified_Ideograph, c):
		p = collHan<<24 | uint32(c)
	case unicode.IsLetter(c) || unicode.Is(unicode.Mc, c):
		p = collLetter<<24 | uint32(lower)<<2
	case unicode.IsDigit(c):
		p = collDigit<<24 | collDigitValue(c)
	case unicode.In(c, unicode.Nl, unicode.No):
		p = collDigit<<24 | (16 + uint32(c))
	case unicode.In(c, unicode.Zs, unicode.Zl, unicode.Zp):
		p = collSpace<<24 | (collUnlisted + uint32(c))
	case unicode.In(c, unicode.Cc, unicode.Cf):
		return out
	case unicode.IsPunct(c):
		p = collPunct<<24 | (collUnlisted + uint32(c))
	case unicode.Is(unicode.Sc, c):
		p = collCurrency<<24 | (collUnlisted + uint32(c))
	case unicode.IsSymbol(c):
		p = collSymbol<<24 | (collUnlisted + uint32(c))
	default: // unassigned, private use or a lone surrogate
		p = collUnassigned<<24 | uint32(c)
	}
	return append(out, collElem{p: p, s: collSecBase, t: ter})
}

// collRune is a code point of a decomposed string.
type collRune struct {
	c   rune
	tag uint8 // the first compatibility tag on the way to c
	ccc uint8
}

// appendCollDecomposed appends the full canonical and compatibility
// decomposition of c to dst. The recursion depth is bounded by the
// decomposition data, not by the input.
func appendCollDecomposed(dst []collRune, c rune, tag uint8) []collRune {
	if isHangulSyllable(c) {
		l, v, t := hangulJamo(c)
		dst = append(dst, collRune{c: l, tag: tag}, collRune{c: v, tag: tag})
		if t != 0 {
			dst = append(dst, collRune{c: t, tag: tag})
		}
		return dst
	}
	t, m := decomposition(c)
	if m == nil {
		return append(dst, collRune{c: c, tag: tag, ccc: combiningClass(c)})
	}
	if tag == decompCanonical {
		tag = t
	}
	for _, d := range m {
		dst = appendCollDecomposed(dst, d, tag)
	}
	return dst
}

// collIter produces the collation elements of a string one segment (a
// starter and the combining marks after it) at a time.
type collIter struct {
	s     *String
	i     int        // next code unit of s
	steps int64      // code points read, for interrupt checks
	seg   []collRune // the current segment, decomposed and reordered
	carry []collRune // the decomposed starter that begins the next segment
	out   []collElem // the current segment's elements
	k     int        // next element of out
}

func (it *collIter) reset() {
	it.i, it.k = 0, 0
	it.out, it.carry = it.out[:0], it.carry[:0]
}

// fill decomposes the next segment into it.out; it reports false at the end
// of the string.
func (it *collIter) fill(r *Realm) (bool, error) {
	seg := append(it.seg[:0], it.carry...)
	it.carry = it.carry[:0]
	n := it.s.Len()
	for it.i < n {
		c := rune(it.s.At(it.i))
		it.i++
		if c >= 0xD800 && c < 0xDC00 && it.i < n {
			if d := rune(it.s.At(it.i)); d >= 0xDC00 && d < 0xE000 {
				c = 0x10000 + (c-0xD800)<<10 + (d - 0xDC00)
				it.i++
			}
		}
		it.steps++
		if err := interruptEvery(r, it.steps); err != nil {
			return false, err
		}
		start := len(seg)
		seg = appendCollDecomposed(seg, c, decompCanonical)
		if start > 0 && seg[start].ccc == 0 {
			it.carry = append(it.carry, seg[start:]...)
			seg = seg[:start]
			break
		}
	}
	it.seg = seg
	if len(seg) == 0 {
		return false, nil
	}
	// Canonical ordering: a stable sort of each run of non-starters by class.
	for i := 0; i < len(seg); {
		if seg[i].ccc == 0 {
			i++
			continue
		}
		j := i + 1
		for j < len(seg) && seg[j].ccc != 0 {
			j++
		}
		if j-i > 1 {
			slices.SortStableFunc(seg[i:j], func(a, b collRune) int { return int(a.ccc) - int(b.ccc) })
		}
		i = j
	}
	seg = collContract(seg)
	it.seg = seg
	it.out, it.k = it.out[:0], 0
	for _, x := range seg {
		it.out = collAppend(it.out, x.c, x.tag, x.ccc)
	}
	return true, nil
}

// collContract replaces a Cyrillic base letter and the mark after it that
// spell a letter of their own (и and U+0306 spell й) with that letter. The
// mark need not follow the letter directly: marks of a lower combining
// class may come between (UTS #10 discontiguous matching), and seg is in
// canonical order.
func collContract(seg []collRune) []collRune {
	b := seg[0].c
	if len(seg) < 2 || b < 0x400 || b > 0x52F && (b < 0xA640 || b > 0xA69F) {
		return seg
	}
	lower := unicode.ToLower(b)
	for j := 1; j < len(seg) && seg[j].ccc != 0; j++ {
		if j > 1 && seg[j-1].ccc >= seg[j].ccc {
			continue // blocked by a mark of the same class before it
		}
		if c, ok := collTables.contr[[2]rune{lower, seg[j].c}]; ok {
			if lower != b {
				c = unicode.ToUpper(c)
			}
			seg[0].c = c
			return slices.Delete(seg, j, j+1)
		}
	}
	return seg
}

// next returns the next non-zero weight at level (0 primary, 1 secondary,
// 2 tertiary), or false at the end of the string.
func (it *collIter) next(r *Realm, level int) (uint32, bool, error) {
	for {
		for it.k < len(it.out) {
			e := it.out[it.k]
			it.k++
			var w uint32
			switch level {
			case 0:
				w = e.p
			case 1:
				w = e.s
			default:
				w = uint32(e.t)
			}
			if w != 0 {
				return w, true, nil
			}
		}
		ok, err := it.fill(r)
		if !ok || err != nil {
			return 0, false, err
		}
	}
}

// collateCompare orders a and b for localeCompare: -1, 0 or 1.
func collateCompare(r *Realm, a, b *String) (int, error) {
	if a.Equals(b) {
		return 0, nil
	}
	collTables.once.Do(loadCollTables)
	if as, ok := a.ASCII(); ok {
		if bs, ok := b.ASCII(); ok {
			return collateASCII(r, as, bs)
		}
	}
	return collateLevels(r, a, b)
}

// collateLevels is collateCompare's comparison of the collation elements,
// level by level.
func collateLevels(r *Realm, a, b *String) (int, error) {
	ia, ib := collIter{s: a}, collIter{s: b}
	for level := range 3 {
		ia.reset()
		ib.reset()
		for {
			x, okA, err := ia.next(r, level)
			if err != nil {
				return 0, err
			}
			y, okB, err := ib.next(r, level)
			if err != nil {
				return 0, err
			}
			if !okA || !okB {
				if okA {
					return 1, nil
				}
				if okB {
					return -1, nil
				}
				break
			}
			if x != y {
				if x < y {
					return -1, nil
				}
				return 1, nil
			}
		}
	}
	return 0, nil
}

// collateASCII is collateCompare for two ASCII strings. Every ASCII
// character that is not ignorable has the base secondary weight, so only
// the primary and tertiary levels can differ.
func collateASCII(r *Realm, a, b string) (int, error) {
	w := &collTables.ascii
	for level := range 2 {
		i, j := 0, 0
		for k := int64(0); ; k++ {
			for i < len(a) && w[a[i]].p == 0 {
				i++
			}
			for j < len(b) && w[b[j]].p == 0 {
				j++
			}
			if i == len(a) || j == len(b) {
				if i < len(a) {
					return 1, nil
				}
				if j < len(b) {
					return -1, nil
				}
				break
			}
			x, y := w[a[i]].p, w[b[j]].p
			if level == 1 {
				x, y = uint32(w[a[i]].t), uint32(w[b[j]].t)
			}
			if x != y {
				if x < y {
					return -1, nil
				}
				return 1, nil
			}
			i++
			j++
			if err := interruptEvery(r, k); err != nil {
				return 0, err
			}
		}
	}
	return 0, nil
}
