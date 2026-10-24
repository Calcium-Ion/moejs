package engine

// Ported from ICU's number formatter and NumberRangeFormatter
// (icu4c/source/i18n/number_*.cpp and numrange_impl.cpp; Unicode License v3;
// see THIRD_PARTY_NOTICES).

// The formatter of Intl.NumberFormat: a resolved NumberFormat formats an
// exact decimal (intl_decimal.go) as ICU 78 does (number_formatimpl.cpp):
// the number's digits with grouping and the locale's symbols, wrapped by
// up to three modifiers: the inner one (the exponent of scientific
// notation), the middle one (the pattern's affixes for the sign, or a
// compact pattern) and the outer one (a unit's or a currency's long name,
// by plural category). Ranges collapse the modifiers the two numbers share
// as ICU's NumberRangeFormatter does (numrange_impl.cpp).
//
// A formatter renders its affixes and long names in UTF-16 when it is
// constructed; formatting then writes into a stack buffer and allocates
// only the result.

import (
	"strings"
	"unicode/utf16"
	"unsafe"
)

// [[Style]].
const (
	intlStyleDecimal uint8 = iota
	intlStylePercent
	intlStyleCurrency
	intlStyleUnit
)

// [[CurrencyDisplay]].
const (
	intlCurrencySymbol uint8 = iota
	intlCurrencyCode
	intlCurrencyNarrow
	intlCurrencyName
)

// [[Notation]].
const (
	intlNotationStandard uint8 = iota
	intlNotationScientific
	intlNotationEngineering
	intlNotationCompact
)

// [[UseGrouping]].
const (
	intlGroupingAuto uint8 = iota
	intlGroupingMin2
	intlGroupingAlways
	intlGroupingOff
)

// [[SignDisplay]].
const (
	intlSignAuto uint8 = iota
	intlSignNever
	intlSignAlways
	intlSignExceptZero
	intlSignNegative
)

// [[ComputedRoundingPriority]].
const (
	intlPriorityAuto uint8 = iota
	intlPriorityMore
	intlPriorityLess
)

// intlNumberFormat is the internal of an Intl.NumberFormat: its resolved
// options and the machinery that formats with them.
type intlNumberFormat struct {
	locale   *String // [[Locale]]
	nu       string  // [[NumberingSystem]]
	style    uint8
	currency string // [[Currency]], upper case
	// The strings of resolvedOptions made at construction, charged once.
	nuString, currencyString, unitString *String
	currencyDisplay                      uint8
	accounting                           bool // [[CurrencySign]] is "accounting"
	unit                                 string
	unitDisplay                          int // intlUnitLong, intlUnitShort or intlUnitNarrow
	notation                             uint8
	compactLong                          bool // [[CompactDisplay]] is "long"
	useGrouping                          uint8
	signDisplay                          uint8
	priority                             uint8
	digits                               intlDigitOptions
	boundFormat                          *Object

	data      *intlNumLocale
	sys       *intlNumSystem
	digitSet  *[10]rune // nil for latn's ASCII digits
	decimal   []uint16  // the decimal separator (monetary for currencies)
	group     []uint16  // the grouping separator
	grouping1 int       // the primary grouping size, 0 for none
	grouping2 int
	minGroup  int
	pattern   intlPattern
	syms      intlAffixSymbols
	middle    [3]intlMod // by pattern sign type
	outer     *[6]intlMod
	outerByPl bool // whether the outer modifier depends on the plural category
	expSep    []uint16
	minus     []uint16
	nan, inf  []uint16
	compact   *intlCompactData
	compactMu []intlCompactMods // the modifiers of the compact patterns used
	approx    *[3]intlMod       // the middle modifiers with the approximately sign
	ranges    string            // the plural ranges of the locale's language
}

// intlMod is a modifier: the text before and after what it wraps, and the
// currency spacing inserted between them and the number when the number's
// adjacent character matches.
type intlMod struct {
	prefix, suffix       intlText
	padPrefix, padSuffix []uint16
	setPrefix, setSuffix string
	cps                  int  // code points of the prefix and the suffix
	noBody               bool // the pattern has no digits: the number is not written
}

// size returns the bytes a formatter keeps for itself, its struct and what
// it rendered at construction, and those its compact patterns' modifiers
// will take when it renders them on first use: what its construction
// charges to the realm's memory account.
func (nf *intlNumberFormat) size() int {
	n := int(unsafe.Sizeof(intlNumberFormat{}))
	for _, u := range [][]uint16{nf.decimal, nf.group, nf.expSep, nf.minus, nf.nan, nf.inf} {
		n += 2 * cap(u)
	}
	for i := range nf.middle {
		n += nf.middle[i].size() - int(unsafe.Sizeof(intlMod{}))
	}
	if nf.outer != nil {
		for i := range nf.outer {
			n += nf.outer[i].size()
		}
	}
	if nf.compact != nil {
		n += int(unsafe.Sizeof(intlCompactData{})) + nf.compact.unique*4*int(unsafe.Sizeof(intlMod{})+48)
	}
	// The middle modifiers with the approximately sign of formatRange.
	return n + 3*int(unsafe.Sizeof(intlMod{})+48)
}

// size returns the bytes of a modifier and its text.
func (m *intlMod) size() int {
	return int(unsafe.Sizeof(intlMod{})) + 2*(cap(m.prefix.u)+cap(m.suffix.u)+cap(m.padPrefix)+cap(m.padSuffix)) +
		int(unsafe.Sizeof(intlRun{}))*(cap(m.prefix.runs)+cap(m.suffix.runs))
}

// intlCompactData is a locale's compact patterns of one format (ICU's
// CompactData): the patterns by magnitude and plural category (the six
// categories, then the explicit counts 0 and 1; "0" for the plain number)
// and the multipliers.
type intlCompactData struct {
	patterns    [20][8]string
	multipliers [20]int
	largest     int
	empty       bool
	unique      int // the patterns of numbers, which get modifiers
}

type intlCompactMods struct {
	pattern string
	mods    [3]intlMod
}

func parseIntlCompact(table string) *intlCompactData {
	c := &intlCompactData{empty: true, largest: -1}
	intlEachCompact(table, func(mag int, slot int, p string) {
		if p != "0" {
			c.unique++
		}
		c.patterns[mag][slot] = p
		c.empty = false
		if c.multipliers[mag] == 0 && p != "0" {
			if z := parseIntlPattern(p).zeros; z > 0 {
				c.multipliers[mag] = z - mag - 1
			}
		}
		c.largest = max(c.largest, mag)
	})
	return c
}

// intlEachCompact calls fn with each pattern of a compact table, its
// magnitude and its slot (a plural category, 6 for the count 0, 7 for 1).
func intlEachCompact(s string, fn func(mag, slot int, p string)) {
	for s != "" {
		end := strings.IndexByte(s, 0x1d)
		if end < 0 {
			end = len(s)
		}
		slot := 0
		switch c := s[1]; c {
		case '0':
			slot = 6
		case '1':
			slot = 7
		default:
			slot = int(intlPluralCode(c))
		}
		fn(int(s[0]-'a'), slot, s[2:end])
		s = s[min(end+1, len(s)):]
	}
}

// multiplier implements CompactData::getMultiplier.
func (c *intlCompactData) multiplier(mag int) int {
	if mag < 0 {
		return 0
	}
	return c.multipliers[min(mag, c.largest)]
}

// pattern implements CompactData::getPattern: "" for the plain number.
func (c *intlCompactData) pattern(mag int, d *intlDecimal, formatted []byte, rules *intlPluralRuleSet) string {
	if mag < 0 || c.largest < 0 {
		return ""
	}
	mag = min(mag, c.largest)
	p := ""
	if d.exp >= len(d.digits) {
		// An integer: the explicit counts first.
		switch {
		case d.isZero():
			p = c.patterns[mag][6]
		case len(d.digits) == 1 && d.digits[0] == '1' && d.exp == 1:
			p = c.patterns[mag][7]
		}
	}
	if p == "" {
		ops := intlOperands(formatted, 0)
		cat := rules.category(&ops)
		p = c.patterns[mag][cat]
		if p == "" && cat != intlPluralOther {
			p = c.patterns[mag][intlPluralOther]
		}
	}
	if p == "0" {
		return ""
	}
	return p
}

// utf16Of returns the UTF-16 units of a string.
func utf16Of(s string) []uint16 { return appendUTF16(nil, s) }

// setup prepares the formatting machinery of a NumberFormat whose options
// are resolved (ICU's macrosToMicroGenerator).
func (nf *intlNumberFormat) setup(dataIndex int) {
	l := intlNumLocaleAt(dataIndex)
	nf.data = l
	nf.sys = l.system(nf.nu)
	sys := nf.sys
	if nf.nu != "latn" {
		nf.digitSet = intlDigits(nf.nu)
	}
	isCurrency := nf.style == intlStyleCurrency
	fullName := isCurrency && nf.currencyDisplay == intlCurrencyName
	isCompact := nf.notation == intlNotationCompact
	// ICU formats a percent in compact notation as the CLDR unit, and the
	// unit percent in its short and narrow widths with the percent pattern
	// (V8 then calls its percent sign a unit).
	percentUnit := nf.style == intlStylePercent && isCompact
	unitPercent := nf.style == intlStyleUnit && nf.unit == "percent" && nf.unitDisplay != intlUnitLong && !isCompact

	decimal, group := sys.sym[intlSymDecimal], sys.sym[intlSymGroup]
	pat := ""
	if isCurrency {
		decimal, group = sys.sym[intlSymCurrencyDecimal], sys.sym[intlSymCurrencyGroup]
		if p, dec, grp, ok := l.currencyOverride(nf.currency); ok {
			pat, decimal, group = p, dec, grp
		}
	}
	if pat == "" {
		switch {
		case unitPercent:
			pat = sys.percent
		case nf.style == intlStyleUnit || percentUnit:
			pat = sys.decimal
		case nf.style == intlStylePercent:
			pat = sys.percent
		case !isCurrency || fullName:
			pat = sys.decimal
		case nf.accounting:
			pat = sys.accounting
		default:
			pat = sys.currency
		}
	}
	nf.pattern = parseIntlPattern(pat)
	nf.decimal, nf.group = utf16Of(decimal), utf16Of(group)

	// Grouping (ICU's Grouper).
	g := nf.pattern.groups
	g1, g2 := g[0], g[1]
	if g2 == -1 {
		g1 = -1
		if nf.useGrouping == intlGroupingAlways {
			g1 = 3
		}
	}
	if g[2] == -1 {
		g2 = g1
	}
	switch nf.useGrouping {
	case intlGroupingOff:
		g1 = -1
	case intlGroupingAuto:
		nf.minGroup = l.minGrouping
	case intlGroupingMin2:
		nf.minGroup = max(2, l.minGrouping)
	case intlGroupingAlways:
		nf.minGroup = 1
	}
	if g1 > 0 {
		nf.grouping1, nf.grouping2 = g1, g2
	}

	nf.syms.sys = sys
	if isCurrency {
		names := l.currencyNames(nf.currency)
		nf.syms.code = nf.currency
		switch nf.currencyDisplay {
		case intlCurrencyCode:
			nf.syms.currency = nf.currency
		case intlCurrencyNarrow:
			nf.syms.currency = names.narrow
		default:
			nf.syms.currency = names.symbol
		}
		if fullName {
			var mods [6]intlMod
			for _, c := range l.cats {
				p := strings.Replace(l.currencyUnit[c], "{1}", names.names[c], 1)
				mods[c] = intlSimpleMod(p, intlFieldCurrency)
			}
			nf.setOuter(&mods)
		}
	}
	for st := range nf.middle {
		nf.middle[st] = nf.makeMod(&nf.pattern, st, false, intlFieldLiteral)
	}
	if nf.style == intlStyleUnit && !unitPercent || percentUnit {
		unit := nf.unit
		width := nf.unitDisplay
		if percentUnit {
			unit, width = "percent", intlUnitShort
		}
		var pats [6]string
		if k := intlUnitIndex(unit); k >= 0 {
			pats = l.unitForm(k, width).patterns
		} else {
			num, den, _ := strings.Cut(unit, "-per-")
			nu := l.unitForm(intlUnitIndex(num), width)
			per := l.unitForm(intlUnitIndex(den), width).per
			for _, c := range l.cats {
				if nu.patterns[c] == "" {
					pats[c] = per
				} else {
					pats[c] = strings.Replace(per, "{0}", nu.patterns[c], 1)
				}
			}
		}
		var mods [6]intlMod
		for _, c := range l.cats {
			mods[c] = intlSimpleMod(pats[c], intlFieldUnit)
		}
		nf.setOuter(&mods)
	}
	if nf.notation == intlNotationScientific || nf.notation == intlNotationEngineering {
		nf.expSep = utf16Of(sys.sym[intlSymExponential])
		nf.minus = utf16Of(sys.sym[intlSymMinus])
	}
	if isCompact {
		table := sys.compactShort
		switch {
		case isCurrency && !fullName:
			table = sys.compactCurrency
		case nf.compactLong && sys.compactLong != "":
			table = sys.compactLong
		}
		if table == "" {
			table = sys.compactShort
		}
		nf.compact = parseIntlCompact(table)
	}
	nf.nan = utf16Of(sys.sym[intlSymNaN])
	nf.inf = utf16Of(sys.sym[intlSymInfinity])
}

// setOuter sets the outer modifiers, noting whether they depend on the
// plural category.
func (nf *intlNumberFormat) setOuter(mods *[6]intlMod) {
	other := &mods[intlPluralOther]
	for _, c := range nf.data.cats {
		if c != intlPluralOther {
			mods[c].cps = mods[c].prefix.codePoints() + mods[c].suffix.codePoints()
			if !mods[c].prefix.equal(&other.prefix) || !mods[c].suffix.equal(&other.suffix) {
				nf.outerByPl = true
			}
		}
	}
	other.cps = other.prefix.codePoints() + other.suffix.codePoints()
	nf.outer = mods
}

// intlSimpleMod makes the modifier of a pattern with one placeholder
// ("{0} US dollars") whose text has one field.
func intlSimpleMod(p string, field uint8) intlMod {
	var m intlMod
	before, after, ok := strings.Cut(p, "{0}")
	if !ok {
		before, after = p, ""
	}
	before = strings.ReplaceAll(before, "''", "'")
	after = strings.ReplaceAll(after, "''", "'")
	m.prefix.add(before, field)
	m.suffix.add(after, field)
	m.cps = m.prefix.codePoints() + m.suffix.codePoints()
	return m
}

// makeMod renders a pattern's affixes for a sign type (ICU's
// createConstantModifier), with the currency spacing of
// CurrencySpacingEnabledModifier.
func (nf *intlNumberFormat) makeMod(pat *intlPattern, signType int, approx bool, field uint8) intlMod {
	var m intlMod
	nf.syms.render(&m.prefix, pat.affix(true, signType, approx), field)
	nf.syms.render(&m.suffix, pat.affix(false, signType, approx), field)
	if pat.hasCurrency() {
		sp := &nf.sys.spacing
		// A currency before the number takes the spacing after the
		// currency, one after it the spacing before.
		if n := len(m.prefix.runs); n > 0 && m.prefix.runs[n-1].field == intlFieldCurrency &&
			intlSpacingMatch(sp[3], intlLastRune(m.prefix.u)) {
			m.padPrefix, m.setPrefix = utf16Of(sp[5]), sp[4]
		}
		if len(m.suffix.runs) > 0 && m.suffix.runs[0].field == intlFieldCurrency &&
			intlSpacingMatch(sp[0], intlFirstRune(m.suffix.u)) {
			m.padSuffix, m.setSuffix = utf16Of(sp[2]), sp[1]
		}
	}
	m.cps = m.prefix.codePoints() + m.suffix.codePoints()
	m.noBody = !pat.hasBody
	return m
}

// signType implements PatternStringUtils::resolveSignDisplay.
func (nf *intlNumberFormat) signType(neg, zero bool) int {
	switch nf.signDisplay {
	case intlSignNever:
		return intlSignPos
	case intlSignAlways:
		if neg {
			return intlSignNeg
		}
		return intlSignPosSign
	case intlSignExceptZero:
		switch {
		case zero:
			return intlSignPos
		case neg:
			return intlSignNeg
		}
		return intlSignPosSign
	case intlSignNegative:
		if neg && !zero {
			return intlSignNeg
		}
		return intlSignPos
	}
	if neg {
		return intlSignNeg
	}
	return intlSignPos
}

// compactMods returns the middle modifiers of a compact pattern, rendering
// them on first use.
func (nf *intlNumberFormat) compactMods(p string) *[3]intlMod {
	for i := range nf.compactMu {
		if nf.compactMu[i].pattern == p {
			return &nf.compactMu[i].mods
		}
	}
	pat := parseIntlPattern(p)
	cm := intlCompactMods{pattern: p}
	for st := range cm.mods {
		cm.mods[st] = nf.makeMod(&pat, st, false, intlFieldCompact)
	}
	nf.compactMu = append(nf.compactMu, cm)
	return &nf.compactMu[len(nf.compactMu)-1].mods
}

// intlNum is a number formatted up to its assembly: its rounded value, its
// digits as FormatNumericToString writes them, its exponent and the
// modifiers that wrap it.
type intlNum struct {
	d        intlDecimal
	str      []byte
	exponent int
	middle   *intlMod
	outer    *intlMod
	plural   intlPluralCategory // the outer modifier's category
	signType int
	pattern  string // the compact pattern ("" for none)
}

// exponentFor implements ComputeExponentForMagnitude.
func (nf *intlNumberFormat) exponentFor(mag int) int {
	switch nf.notation {
	case intlNotationScientific:
		return mag
	case intlNotationEngineering:
		if mag < 0 {
			return -((-mag + 2) / 3) * 3
		}
		return mag / 3 * 3
	case intlNotationCompact:
		return -nf.compact.multiplier(mag)
	}
	return 0
}

// computeExponent implements ComputeExponent for a finite number other
// than zero.
func (nf *intlNumberFormat) computeExponent(d *intlDecimal) int {
	mag := d.magnitude()
	e := nf.exponentFor(mag)
	var db [40]byte
	var sb [64]byte
	t := intlDecimal{digits: append(db[:0], d.digits...), exp: d.exp, neg: d.neg, kind: d.kind}
	t.scale(-e)
	nf.digits.formatNumericToString(&t, sb[:0])
	if t.isZero() || t.magnitude() == mag-e {
		return e
	}
	return nf.exponentFor(mag + 1)
}

// prepare formats a number up to its assembly. d is consumed (rounded in
// place); n.str appends to str. It returns its result rather than filling
// a pointer so that the caller's stack buffers stay on the stack.
func (nf *intlNumberFormat) prepare(d *intlDecimal, str []byte) (n intlNum) {
	n.str = str
	if d.kind != intlFinite {
		n.d = *d
		// NaN counts as positive zero (ICU's signum).
		n.signType = nf.signType(d.kind == intlInfinity && d.neg, d.kind == intlNaN)
		n.middle = &nf.middle[n.signType]
		if nf.outer != nil {
			// NaN and the infinities are other.
			n.plural = intlPluralOther
			n.outer = &nf.outer[n.plural]
		}
		return n
	}
	if nf.style == intlStylePercent {
		d.scale(2)
	}
	// The unscaled value, for the plural category of a compact number's
	// outer modifier (ICU computes it before the compact scaling).
	var ub [40]byte
	var unscaled intlDecimal
	keep := nf.compact != nil && nf.outerByPl
	if keep {
		unscaled = intlDecimal{digits: append(ub[:0], d.digits...), exp: d.exp, neg: d.neg, kind: d.kind}
	}
	if nf.notation != intlNotationStandard && !d.isZero() {
		n.exponent = nf.computeExponent(d)
		d.scale(-n.exponent)
	}
	n.str = nf.digits.formatNumericToString(d, n.str)
	n.d = *d
	n.signType = nf.signType(d.neg, d.isZero())
	n.middle = &nf.middle[n.signType]
	if nf.compact != nil {
		mag := 0
		if !d.isZero() {
			mag = d.magnitude() + n.exponent
		}
		if p := nf.compact.pattern(mag, d, n.str, nf.data.cardinal); p != "" {
			n.pattern = p
			n.middle = &nf.compactMods(p)[n.signType]
		}
	}
	if nf.outer != nil {
		n.plural = intlPluralOther
		if nf.outerByPl {
			var ops intlPluralOperands
			if keep {
				var sb [64]byte
				ops = intlOperands(nf.digits.formatNumericToString(&unscaled, sb[:0]), 0)
			} else {
				ops = intlOperands(n.str, n.exponent)
			}
			n.plural = nf.data.cardinal.category(&ops)
		}
		n.outer = &nf.outer[n.plural]
	}
	return n
}

// intlOut is formatted text being assembled, with its runs' fields when
// parts are wanted. Its methods take and return it by value, which keeps
// a caller's stack buffer on the stack.
type intlOut struct {
	u     []uint16
	runs  []intlRun
	parts bool
}

// mark ends a run of field at the end of the text.
func (o intlOut) mark(field uint8) intlOut {
	if !o.parts {
		return o
	}
	if n := len(o.runs); n > 0 && o.runs[n-1].field == field {
		o.runs[n-1].end = int32(len(o.u))
		return o
	}
	o.runs = append(o.runs, intlRun{field, int32(len(o.u))})
	return o
}

// put appends units with a field.
func (o intlOut) put(u []uint16, field uint8) intlOut {
	if len(u) == 0 {
		return o
	}
	o.u = append(o.u, u...)
	return o.mark(field)
}

// text appends formatted text.
func (o intlOut) text(t *intlText) intlOut {
	if len(t.u) == 0 {
		return o
	}
	base := int32(len(o.u))
	o.u = append(o.u, t.u...)
	if !o.parts {
		return o
	}
	for _, r := range t.runs {
		end := base + r.end
		if n := len(o.runs); n > 0 && o.runs[n-1].field == r.field {
			o.runs[n-1].end = end
			continue
		}
		o.runs = append(o.runs, intlRun{r.field, end})
	}
	return o
}

// digit appends an ASCII digit in the numbering system.
func (nf *intlNumberFormat) digit(o intlOut, c byte) intlOut {
	if nf.digitSet == nil {
		o.u = append(o.u, uint16(c))
		return o
	}
	r := nf.digitSet[c-'0']
	if r < 0x10000 {
		o.u = append(o.u, uint16(r))
		return o
	}
	hi, lo := utf16.EncodeRune(r)
	o.u = append(o.u, uint16(hi), uint16(lo))
	return o
}

// writeNumber appends the number itself (ICU's writeNumber) and its
// exponent (the inner modifier).
func (nf *intlNumberFormat) writeNumber(o intlOut, n *intlNum) intlOut {
	switch n.d.kind {
	case intlNaN:
		return o.put(nf.nan, intlFieldNaN)
	case intlInfinity:
		return o.put(nf.inf, intlFieldInfinity)
	}
	s := n.str
	intEnd := len(s)
	for i, c := range s {
		if c == '.' {
			intEnd = i
			break
		}
	}
	grouped := nf.grouping1 > 0 && intEnd-nf.grouping1 >= nf.minGroup
	for i := 0; i < intEnd; i++ {
		o = nf.digit(o, s[i])
		pos := intEnd - 1 - i // the position of the next digit
		if grouped && i+1 < intEnd && pos >= nf.grouping1 && (pos-nf.grouping1)%nf.grouping2 == 0 {
			o = o.mark(intlFieldInteger)
			o = o.put(nf.group, intlFieldGroup)
		}
	}
	o = o.mark(intlFieldInteger)
	if intEnd < len(s) {
		o = o.put(nf.decimal, intlFieldDecimal)
		for i := intEnd + 1; i < len(s); i++ {
			o = nf.digit(o, s[i])
		}
		o = o.mark(intlFieldFraction)
	}
	if nf.expSep != nil {
		o = o.put(nf.expSep, intlFieldExpSeparator)
		e := n.exponent
		if e < 0 {
			o = o.put(nf.minus, intlFieldExpMinus)
			e = -e
		}
		var b [8]byte
		k := len(b)
		for {
			k--
			b[k] = byte('0' + e%10)
			e /= 10
			if e == 0 {
				break
			}
		}
		for _, c := range b[k:] {
			o = nf.digit(o, c)
		}
		o = o.mark(intlFieldExpInteger)
	}
	return o
}

// assemble appends a formatted number with its modifiers.
func (nf *intlNumberFormat) assemble(o intlOut, n *intlNum) intlOut {
	if n.outer != nil {
		o = o.text(&n.outer.prefix)
	}
	m := n.middle
	o = o.text(&m.prefix)
	start := len(o.u)
	if !m.noBody {
		o = nf.writeNumber(o, n)
	}
	o = nf.space(o, m, start)
	o = o.text(&m.suffix)
	if n.outer != nil {
		o = o.text(&n.outer.suffix)
	}
	return o
}

// space inserts a modifier's currency spacing around the text from start,
// which it wraps.
func (nf *intlNumberFormat) space(o intlOut, m *intlMod, start int) intlOut {
	if m.padSuffix != nil && len(o.u) > start && intlSpacingMatch(m.setSuffix, intlLastRune(o.u[start:])) {
		o = o.put(m.padSuffix, intlFieldLiteral)
	}
	if m.padPrefix != nil && len(o.u) > start && intlSpacingMatch(m.setPrefix, intlFirstRune(o.u[start:])) {
		o = o.insert(start, m.padPrefix)
	}
	return o
}

// insert inserts literal units at i.
func (o intlOut) insert(i int, u []uint16) intlOut {
	n := len(u)
	o.u = append(o.u, u...)
	copy(o.u[i+n:], o.u[i:len(o.u)-n])
	copy(o.u[i:], u)
	if o.parts {
		o.runs = intlShiftRuns(o.runs, i, n)
	}
	return o
}

// intlShiftRuns makes room for n literal units inserted at i in runs.
func intlShiftRuns(old []intlRun, i, n int) []intlRun {
	runs := make([]intlRun, 0, len(old)+2)
	add := func(field uint8, end int32) {
		if k := len(runs); k > 0 {
			if runs[k-1].end >= end {
				return
			}
			if runs[k-1].field == field {
				runs[k-1].end = end
				return
			}
		}
		runs = append(runs, intlRun{field, end})
	}
	prev := int32(0)
	inserted := false
	for _, r := range old {
		switch {
		case !inserted && int(r.end) > i:
			// The run spans i: split it.
			if int(prev) < i {
				add(r.field, int32(i))
			}
			add(intlFieldLiteral, int32(i+n))
			add(r.field, r.end+int32(n))
			inserted = true
		case inserted:
			add(r.field, r.end+int32(n))
		default:
			add(r.field, r.end)
		}
		prev = r.end
	}
	if !inserted {
		add(intlFieldLiteral, int32(i+n))
	}
	return runs
}

// intlU16 are UTF-16 strings co-allocated with their String, one size
// class each.
type intlU16x8 struct {
	s String
	u [8]uint16
}

type intlU16x16 struct {
	s String
	u [16]uint16
}

type intlU16x24 struct {
	s String
	u [24]uint16
}

type intlU16x32 struct {
	s String
	u [32]uint16
}

type intlU16x48 struct {
	s String
	u [48]uint16
}

// intlStringOf returns the formatted text as a String: ASCII text through
// asciiStringCopy-like co-allocation, UTF-16 text co-allocated with its
// String up to 48 units.
func intlStringOf(u []uint16) *String {
	ascii := true
	for _, c := range u {
		if c >= 0x80 {
			ascii = false
			break
		}
	}
	n := len(u)
	if ascii {
		s, b := newASCIIBuf(n)
		for i, c := range u {
			b[i] = byte(c)
		}
		return s
	}
	var s *String
	var dst []uint16
	switch {
	case n <= 8:
		x := &intlU16x8{}
		s, dst = &x.s, x.u[:n:n]
	case n <= 16:
		x := &intlU16x16{}
		s, dst = &x.s, x.u[:n:n]
	case n <= 24:
		x := &intlU16x24{}
		s, dst = &x.s, x.u[:n:n]
	case n <= 32:
		x := &intlU16x32{}
		s, dst = &x.s, x.u[:n:n]
	case n <= 48:
		x := &intlU16x48{}
		s, dst = &x.s, x.u[:n:n]
	default:
		return utf16String(append([]uint16(nil), u...))
	}
	copy(dst, u)
	*s = String{p: unitsPtr(dst), n: int32(n), kind: strUTF16}
	return s
}

// formatDecimal formats a number (consumed) to a String.
func (nf *intlNumberFormat) formatDecimal(d *intlDecimal) *String {
	var ub [96]uint16
	var sb [64]byte
	n := nf.prepare(d, sb[:0])
	return intlStringOf(nf.appendFormatted(ub[:0], &n))
}

// appendFormatted appends a formatted number with its modifiers: assemble
// without the parts' runs, the path of format.
func (nf *intlNumberFormat) appendFormatted(u []uint16, n *intlNum) []uint16 {
	if n.outer != nil {
		u = append(u, n.outer.prefix.u...)
	}
	m := n.middle
	u = append(u, m.prefix.u...)
	start := len(u)
	if !m.noBody {
		u = nf.appendNumber(u, n)
	}
	if m.padSuffix != nil && len(u) > start && intlSpacingMatch(m.setSuffix, intlLastRune(u[start:])) {
		u = append(u, m.padSuffix...)
	}
	if m.padPrefix != nil && len(u) > start && intlSpacingMatch(m.setPrefix, intlFirstRune(u[start:])) {
		k := len(m.padPrefix)
		u = append(u, m.padPrefix...)
		copy(u[start+k:], u[start:len(u)-k])
		copy(u[start:], m.padPrefix)
	}
	u = append(u, m.suffix.u...)
	if n.outer != nil {
		u = append(u, n.outer.suffix.u...)
	}
	return u
}

// appendNumber appends the number itself and its exponent: writeNumber
// without the runs.
func (nf *intlNumberFormat) appendNumber(u []uint16, n *intlNum) []uint16 {
	switch n.d.kind {
	case intlNaN:
		return append(u, nf.nan...)
	case intlInfinity:
		return append(u, nf.inf...)
	}
	s := n.str
	intEnd := len(s)
	for i, c := range s {
		if c == '.' {
			intEnd = i
			break
		}
	}
	if g1 := nf.grouping1; g1 > 0 && intEnd-g1 >= nf.minGroup {
		// The digits before the first separator, then each group.
		g2 := nf.grouping2
		first := intEnd - g1
		if first > 0 {
			first = (first-1)%g2 + 1
		}
		u = nf.appendDigits(u, s[:first])
		for i := first; i < intEnd-g1; i += g2 {
			u = append(u, nf.group...)
			u = nf.appendDigits(u, s[i:i+g2])
		}
		u = append(u, nf.group...)
		u = nf.appendDigits(u, s[intEnd-g1:intEnd])
	} else {
		u = nf.appendDigits(u, s[:intEnd])
	}
	if intEnd < len(s) {
		u = append(u, nf.decimal...)
		u = nf.appendDigits(u, s[intEnd+1:])
	}
	if nf.expSep != nil {
		u = append(u, nf.expSep...)
		e := n.exponent
		if e < 0 {
			u = append(u, nf.minus...)
			e = -e
		}
		var b [8]byte
		k := len(b)
		for {
			k--
			b[k] = byte('0' + e%10)
			e /= 10
			if e == 0 {
				break
			}
		}
		u = nf.appendDigits(u, b[k:])
	}
	return u
}

// appendDigits appends ASCII digits in the numbering system.
func (nf *intlNumberFormat) appendDigits(u []uint16, s []byte) []uint16 {
	if nf.digitSet == nil {
		for _, c := range s {
			u = append(u, uint16(c))
		}
		return u
	}
	for _, c := range s {
		if r := nf.digitSet[c-'0']; r < 0x10000 {
			u = append(u, uint16(r))
		} else {
			hi, lo := utf16.EncodeRune(r)
			u = append(u, uint16(hi), uint16(lo))
		}
	}
	return u
}

// formatFloat formats a Number, allocating only the result.
func (nf *intlNumberFormat) formatFloat(f float64) *String {
	var db [32]byte
	d := intlDecimal{digits: db[:0]}
	d.setFloat(f)
	return nf.formatDecimal(&d)
}

// intlPart is a part of formatToParts: its field and its text's range.
type intlPart struct {
	field      uint8
	start, end int32
	source     uint8 // formatRangeToParts: 0 shared, 1 startRange, 2 endRange
}

// intlPartsOf turns runs into parts as V8 reads ICU's fields: every field
// but a grouping separator loses its leading and trailing whitespace, bidi
// controls and variation selectors (ICU's trimming), which become literal
// text, and adjacent literal text joins.
func intlPartsOf(parts []intlPart, u []uint16, runs []intlRun) []intlPart {
	add := func(field uint8, s, e int32) {
		if s >= e {
			return
		}
		if n := len(parts); n > 0 && field == intlFieldLiteral && parts[n-1].field == intlFieldLiteral && parts[n-1].end == s {
			parts[n-1].end = e
			return
		}
		parts = append(parts, intlPart{field: field, start: s, end: e})
	}
	start := int32(0)
	for _, r := range runs {
		s, e := start, r.end
		start = r.end
		if r.field == intlFieldLiteral || r.field == intlFieldGroup {
			add(r.field, s, e)
			continue
		}
		s2, e2 := s, e
		for s2 < e2 {
			c, w := intlRuneAt(u, s2)
			if !intlIgnorable(c) {
				break
			}
			s2 += w
		}
		for e2 > s2 {
			c, w := intlRuneBefore(u, e2)
			if !intlIgnorable(c) {
				break
			}
			e2 -= w
		}
		if s2 == e2 {
			add(intlFieldLiteral, s, e)
			continue
		}
		add(intlFieldLiteral, s, s2)
		add(r.field, s2, e2)
		add(intlFieldLiteral, e2, e)
	}
	return parts
}

func intlRuneAt(u []uint16, i int32) (rune, int32) {
	if c := rune(u[i]); utf16.IsSurrogate(c) && c < 0xdc00 && int(i)+1 < len(u) {
		return utf16.DecodeRune(c, rune(u[i+1])), 2
	}
	return rune(u[i]), 1
}

func intlRuneBefore(u []uint16, i int32) (rune, int32) {
	if c := rune(u[i-1]); utf16.IsSurrogate(c) && c >= 0xdc00 && i >= 2 {
		return utf16.DecodeRune(rune(u[i-2]), c), 2
	}
	return rune(u[i-1]), 1
}

// formatToParts formats a number (consumed) to its text and parts,
// appended to u, runs (the fields' runs) and parts.
func (nf *intlNumberFormat) formatToParts(d *intlDecimal, u []uint16, runs []intlRun, parts []intlPart) ([]uint16, []intlPart) {
	var sb [64]byte
	n := nf.prepare(d, sb[:0])
	o := nf.assemble(intlOut{u: u, runs: runs, parts: true}, &n)
	return o.u, intlPartsOf(parts, o.u, o.runs)
}

// approxMod returns the middle modifier with the approximately sign for a
// sign type (ICU's approximately formatter), rendering them on first use.
func (nf *intlNumberFormat) approxMod(n *intlNum) *intlMod {
	if n.pattern != "" {
		pat := parseIntlPattern(n.pattern)
		m := nf.makeMod(&pat, n.signType, true, intlFieldCompact)
		return &m
	}
	if nf.approx == nil {
		var mods [3]intlMod
		for st := range mods {
			mods[st] = nf.makeMod(&nf.pattern, st, true, intlFieldLiteral)
		}
		nf.approx = &mods
	}
	return &nf.approx[n.signType]
}

func intlUnitsEqual(a, b []uint16) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// intlPatternWhiteSpace reports whether a code unit is Pattern_White_Space.
func intlPatternWhiteSpace(c uint16) bool {
	switch c {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x85, 0x200e, 0x200f, 0x2028, 0x2029:
		return true
	}
	return false
}

// formatRange implements PartitionNumberRangePattern for numbers that are
// not NaN (both consumed) as ICU's NumberRangeFormatter does, with V8's
// sources: the text and, when parts is set, the parts.
func (nf *intlNumberFormat) formatRange(x, y *intlDecimal, parts bool) ([]uint16, []intlPart) {
	var sbx, sby [64]byte
	nx := nf.prepare(x, sbx[:0])
	ny := nf.prepare(y, sby[:0])
	o := intlOut{parts: parts}
	// Identical formatted numbers: the approximately form of the first.
	ox := nf.assemble(intlOut{}, &nx)
	oy := nf.assemble(intlOut{}, &ny)
	if intlUnitsEqual(ox.u, oy.u) {
		ax := nx
		ax.middle = nf.approxMod(&nx)
		o = nf.assemble(o, &ax)
		if !parts {
			return o.u, nil
		}
		return o.u, intlPartsOf(nil, o.u, o.runs)
	}

	// Collapsing (UNUM_RANGE_COLLAPSE_AUTO): the outer modifiers, which
	// one formatter makes for both numbers, whatever their plural
	// categories; the middle ones when they are the same and longer than a
	// code point; never the inner ones.
	m := nx.middle
	collapseMiddle := m.prefix.equal(&ny.middle.prefix) && m.suffix.equal(&ny.middle.suffix) && m.cps > 1
	pre, rest, _ := strings.Cut(nf.sys.rangePattern, "{0}")
	infix, post, _ := strings.Cut(rest, "{1}")
	infixU := utf16Of(infix)
	repeatInner := nx.d.kind == intlFinite && nf.expSep != nil
	repeatMiddle := !collapseMiddle && m.cps > 0
	spaceBefore, spaceAfter := false, false
	if repeatInner || repeatMiddle {
		spaceBefore = len(infixU) == 0 || !intlPatternWhiteSpace(infixU[0])
		spaceAfter = len(infixU) == 0 || !intlPatternWhiteSpace(infixU[len(infixU)-1])
	}
	var outer *intlMod
	if nx.outer != nil {
		if nf.ranges == "" {
			nf.ranges = intlPluralRangesOf(intlDataLocale(nf.data.index))
		}
		outer = &nf.outer[intlPluralRange(nf.ranges, nx.plural, ny.plural)]
	}
	o = o.put(utf16Of(pre), intlFieldLiteral)
	if outer != nil {
		o = o.text(&outer.prefix)
	}
	midStart := len(o.u)
	if collapseMiddle {
		o = o.text(&m.prefix)
		midStart = len(o.u)
	}
	s0 := len(o.u)
	o = nf.assembleRangeSide(o, &nx, !collapseMiddle)
	e0 := len(o.u)
	if spaceBefore {
		o = o.put([]uint16{' '}, intlFieldLiteral)
	}
	o = o.put(infixU, intlFieldLiteral)
	if spaceAfter {
		o = o.put([]uint16{' '}, intlFieldLiteral)
	}
	s1 := len(o.u)
	o = nf.assembleRangeSide(o, &ny, !collapseMiddle)
	e1 := len(o.u)
	o = o.put(utf16Of(post), intlFieldLiteral)
	if collapseMiddle {
		// The collapsed middle modifier wraps the whole range: its currency
		// spacing looks at the first number's start and the range's end.
		if m.padSuffix != nil && intlSpacingMatch(m.setSuffix, intlLastRune(o.u[midStart:])) {
			o = o.put(m.padSuffix, intlFieldLiteral)
		}
		o = o.text(&m.suffix)
		if m.padPrefix != nil && intlSpacingMatch(m.setPrefix, intlFirstRune(o.u[midStart:])) {
			// ICU counts the spans from the end of the collapsed prefix
			// without its spacing, so they do not move (and V8 reports the
			// sources one unit early).
			o = o.insert(midStart, m.padPrefix)
		}
	}
	if outer != nil {
		o = o.text(&outer.suffix)
	}
	if !parts {
		return o.u, nil
	}
	ps := intlPartsOf(nil, o.u, o.runs)
	for i := range ps {
		switch p := &ps[i]; {
		case int(p.start) >= s0 && int(p.end) <= e0:
			p.source = 1
		case int(p.start) >= s1 && int(p.end) <= e1:
			p.source = 2
		}
	}
	return o.u, ps
}

// assembleRangeSide appends one number of a range with the modifiers that
// are not collapsed.
func (nf *intlNumberFormat) assembleRangeSide(o intlOut, n *intlNum, middle bool) intlOut {
	if !middle {
		return nf.writeNumber(o, n)
	}
	m := n.middle
	o = o.text(&m.prefix)
	start := len(o.u)
	if !m.noBody {
		o = nf.writeNumber(o, n)
	}
	o = nf.space(o, m, start)
	return o.text(&m.suffix)
}
