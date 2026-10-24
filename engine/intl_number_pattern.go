package engine

// Ported from ICU's number pattern code (icu4c/source/i18n/
// number_patternstring.cpp, number_affixutils.cpp and
// number_patternmodifier.cpp; Unicode License v3; see THIRD_PARTY_NOTICES).

// The number patterns of CLDR as ICU reads them (number_patternstring.cpp,
// number_affixutils.cpp, number_patternmodifier.cpp): a pattern's positive
// and negative affixes and grouping sizes, and the rendering of an affix
// for a sign display, with the parts' fields formatToParts reports.

import (
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

// Fields of the formatted text (ICU's UNumberFormatFields, as V8 names the
// parts).
const (
	intlFieldLiteral uint8 = iota
	intlFieldInteger
	intlFieldGroup
	intlFieldDecimal
	intlFieldFraction
	intlFieldSign // plusSign or minusSign by its first character
	intlFieldPercent
	intlFieldCurrency
	intlFieldUnit
	intlFieldCompact
	intlFieldExpSeparator
	intlFieldExpMinus
	intlFieldExpInteger
	intlFieldApprox
	intlFieldNaN
	intlFieldInfinity
)

// intlRun is a run of formatted text with one field: it ends at end.
type intlRun struct {
	field uint8
	end   int32
}

// intlText is formatted text and the fields of its runs.
type intlText struct {
	u    []uint16
	runs []intlRun
}

// add appends text with a field.
func (t *intlText) add(s string, field uint8) {
	if s == "" {
		return
	}
	t.u = appendUTF16(t.u, s)
	t.mark(field)
}

// addRune appends a code point with a field.
func (t *intlText) addRune(r rune, field uint8) {
	if r < 0x10000 {
		t.u = append(t.u, uint16(r))
	} else {
		hi, lo := utf16.EncodeRune(r)
		t.u = append(t.u, uint16(hi), uint16(lo))
	}
	t.mark(field)
}

// mark ends a run of field at the end of the text, extending the last run
// when it has the same field.
func (t *intlText) mark(field uint8) {
	if n := len(t.runs); n > 0 && t.runs[n-1].field == field {
		t.runs[n-1].end = int32(len(t.u))
		return
	}
	t.runs = append(t.runs, intlRun{field, int32(len(t.u))})
}

// codePoints returns the number of code points of the text.
func (t *intlText) codePoints() int {
	n := 0
	for i := 0; i < len(t.u); i++ {
		if !utf16.IsSurrogate(rune(t.u[i])) || t.u[i] >= 0xdc00 {
			n++
		}
	}
	return n
}

func (t *intlText) equal(o *intlText) bool {
	if len(t.u) != len(o.u) {
		return false
	}
	for i := range t.u {
		if t.u[i] != o.u[i] {
			return false
		}
	}
	return true
}

// intlPattern is a parsed pattern (ICU's ParsedPatternInfo): the affix
// strings of its subpatterns, still quoted, and its grouping sizes.
type intlPattern struct {
	posPrefix, posSuffix string
	negPrefix, negSuffix string
	hasNegative          bool
	hasBody              bool // integer digits
	// groups are ICU's grouping sizes: the digits after the last ',', between
	// the last two and before them, -1 for none.
	groups [3]int
	zeros  int // the '0's of the first run of them (a compact pattern's scale)
}

// parseIntlPattern parses a CLDR number pattern.
func parseIntlPattern(p string) intlPattern {
	var pat intlPattern
	i := 0
	affix := func() string {
		start := i
		for i < len(p) {
			c := p[i]
			if c == '\'' {
				// A quoted run, '' being a quote.
				i++
				for i < len(p) && p[i] != '\'' {
					i++
				}
				i++
				continue
			}
			if c == '#' || c == '@' || c == ';' || c == '*' || c == '.' || c == ',' || c >= '0' && c <= '9' {
				break
			}
			i++
		}
		return p[start:min(i, len(p))]
	}
	body := func(groups *[3]int) bool {
		has := false
		g := [4]int{0, -1, -1, 0}
	integer:
		for i < len(p) {
			switch c := p[i]; {
			case c == ',':
				g[3], g[2], g[1], g[0] = g[2], g[1], g[0], 0
			case c == '#' || c == '@' || c >= '0' && c <= '9':
				g[0]++
				has = true
			default:
				break integer
			}
			i++
		}
		if i < len(p) && p[i] == '.' {
			i++
			for i < len(p) && (p[i] == '#' || p[i] >= '0' && p[i] <= '9') {
				i++
			}
		}
		if i < len(p) && p[i] == 'E' {
			i++
			if i < len(p) && p[i] == '+' {
				i++
			}
			for i < len(p) && p[i] == '0' {
				i++
			}
		}
		groups[0], groups[1], groups[2] = g[0], g[1], g[2]
		return has
	}
	pat.posPrefix = affix()
	pat.hasBody = body(&pat.groups)
	pat.posSuffix = affix()
	if i < len(p) && p[i] == ';' {
		i++
		pat.hasNegative = true
		pat.negPrefix = affix()
		var g [3]int
		body(&g)
		pat.negSuffix = affix()
	}
	// ICU's countZeros: the first run of '0's anywhere in the pattern.
	for j := 0; j < len(p); j++ {
		if p[j] == '0' {
			pat.zeros++
		} else if pat.zeros > 0 {
			break
		}
	}
	return pat
}

// Affix token types (AffixUtils).
const (
	intlTokLiteral = iota
	intlTokMinus
	intlTokPlus
	intlTokApprox
	intlTokPercent
	intlTokPermille
	intlTokCurrency // one ¤ (more count as the same here)
	intlTokCurrencyCode
	intlTokCurrencyName
)

// intlEachToken calls fn with each token of an affix pattern: its type and,
// for a literal, its code point.
func intlEachToken(affix string, fn func(tok int, r rune)) {
	quoted := false
	for i := 0; i < len(affix); {
		r, n := utf8.DecodeRuneInString(affix[i:])
		i += n
		if r == '\'' {
			if i < len(affix) && affix[i] == '\'' {
				i++
				fn(intlTokLiteral, '\'')
				continue
			}
			quoted = !quoted
			continue
		}
		if quoted {
			fn(intlTokLiteral, r)
			continue
		}
		switch r {
		case '-':
			fn(intlTokMinus, 0)
		case '+':
			fn(intlTokPlus, 0)
		case '~':
			fn(intlTokApprox, 0)
		case '%':
			fn(intlTokPercent, 0)
		case '‰':
			fn(intlTokPermille, 0)
		case '¤':
			k := 1
			for i+1 < len(affix) && affix[i:i+2] == "¤" {
				i += 2
				k++
			}
			switch k {
			case 1:
				fn(intlTokCurrency, 0)
			case 2:
				fn(intlTokCurrencyCode, 0)
			default:
				fn(intlTokCurrencyName, 0)
			}
		default:
			fn(intlTokLiteral, r)
		}
	}
}

func intlAffixHas(affix string, tok int) bool {
	has := false
	intlEachToken(affix, func(t int, _ rune) {
		if t == tok {
			has = true
		}
	})
	return has
}

// hasCurrency reports whether the pattern has a currency sign.
func (p *intlPattern) hasCurrency() bool {
	return intlAffixHas(p.posPrefix, intlTokCurrency) || intlAffixHas(p.posSuffix, intlTokCurrency) ||
		intlAffixHas(p.posPrefix, intlTokCurrencyCode) || intlAffixHas(p.posSuffix, intlTokCurrencyCode)
}

// Pattern sign types (ICU's PatternSignType).
const (
	intlSignPos     = iota // no sign
	intlSignPosSign        // a plus sign
	intlSignNeg            // a minus sign
)

// affix implements PatternStringUtils::patternInfoToStringBuilder: the
// affix pattern of the prefix or the suffix for a sign type, with a sign
// placeholder rewritten ('-' standing for the minus sign, '+' for the plus
// sign, '~' for the approximately sign).
func (p *intlPattern) affix(prefix bool, signType int, approx bool) string {
	posHasPlus := intlAffixHas(p.posPrefix, intlTokPlus) || intlAffixHas(p.posSuffix, intlTokPlus)
	negHasMinus := intlAffixHas(p.negPrefix, intlTokMinus) || intlAffixHas(p.negSuffix, intlTokMinus)
	plusReplacesMinus := signType == intlSignPosSign && !posHasPlus
	useNegative := p.hasNegative && (signType == intlSignNeg || negHasMinus && (plusReplacesMinus || approx))
	var src string
	switch {
	case useNegative && prefix:
		src = p.negPrefix
	case useNegative:
		src = p.negSuffix
	case prefix:
		src = p.posPrefix
	default:
		src = p.posSuffix
	}
	prependSign := false
	switch {
	case !prefix || useNegative:
	case signType == intlSignNeg:
		prependSign = true
	default:
		prependSign = plusReplacesMinus || approx
	}
	sign := "-"
	switch {
	case approx && plusReplacesMinus:
		sign = "~+"
	case approx && signType == intlSignNeg:
		sign = "~-"
	case approx:
		sign = "~"
	case plusReplacesMinus:
		sign = "+"
	}
	if prependSign {
		src = "-" + src
	}
	if sign == "-" {
		return src
	}
	out := make([]byte, 0, len(src)+2)
	for i := 0; i < len(src); i++ {
		if src[i] == '-' {
			out = append(out, sign...)
		} else {
			out = append(out, src[i])
		}
	}
	return string(out)
}

// intlAffixSymbols provides the strings of an affix's symbol tokens.
type intlAffixSymbols struct {
	sys      *intlNumSystem
	currency string // the currency in its display ("$", "USD")
	code     string
}

// render appends an affix pattern's text to t: symbols with their fields,
// literals with field (literal, or compact for a compact pattern).
func (s *intlAffixSymbols) render(t *intlText, affix string, field uint8) {
	intlEachToken(affix, func(tok int, r rune) {
		switch tok {
		case intlTokLiteral:
			t.addRune(r, field)
		case intlTokMinus:
			t.add(s.sys.sym[intlSymMinus], intlFieldSign)
		case intlTokPlus:
			t.add(s.sys.sym[intlSymPlus], intlFieldSign)
		case intlTokApprox:
			t.add(s.sys.sym[intlSymApprox], intlFieldApprox)
		case intlTokPercent:
			t.add(s.sys.sym[intlSymPercent], intlFieldPercent)
		case intlTokPermille:
			t.add("‰", intlFieldPercent)
		case intlTokCurrency:
			t.add(s.currency, intlFieldCurrency)
		case intlTokCurrencyCode:
			t.add(s.code, intlFieldCurrency)
		case intlTokCurrencyName:
			t.add("�", intlFieldCurrency)
		}
	})
}

// intlSpacingMatch reports whether a code point is in one of the currency
// spacing sets ICU special-cases (the only ones CLDR uses, which the
// generator checks).
func intlSpacingMatch(set string, r rune) bool {
	if set == "[:digit:]" {
		return unicode.Is(unicode.Nd, r)
	}
	// [[:^S:]&[:^Z:]]
	return !unicode.IsSymbol(r) && !unicode.Is(unicode.Z, r)
}

// lastRune and firstRune return the last and first code points of UTF-16
// text.
func intlLastRune(u []uint16) rune {
	n := len(u)
	if n >= 2 && utf16.IsSurrogate(rune(u[n-2])) && u[n-2] < 0xdc00 {
		return utf16.DecodeRune(rune(u[n-2]), rune(u[n-1]))
	}
	return rune(u[n-1])
}

func intlFirstRune(u []uint16) rune {
	if len(u) >= 2 && utf16.IsSurrogate(rune(u[0])) && u[0] < 0xdc00 {
		return utf16.DecodeRune(rune(u[0]), rune(u[1]))
	}
	return rune(u[0])
}

// intlIgnorable reports whether a code point is one ICU trims from the ends
// of a field ([[:Zs:][\t][:Bidi_Control:][:Variation_Selector:]]).
func intlIgnorable(r rune) bool {
	switch {
	case r == '\t' || unicode.Is(unicode.Zs, r):
		return true
	case r == 0x061c || r == 0x200e || r == 0x200f || r >= 0x202a && r <= 0x202e || r >= 0x2066 && r <= 0x2069:
		return true // Bidi_Control
	case r >= 0x180b && r <= 0x180d || r == 0x180f || r >= 0xfe00 && r <= 0xfe0f || r >= 0xe0100 && r <= 0xe01ef:
		return true // Variation_Selector
	}
	return false
}
