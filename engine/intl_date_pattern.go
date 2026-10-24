package engine

// Ported from ICU's DateTimePatternGenerator (icu4c/source/i18n/dtptngen.cpp;
// Unicode License v3; see THIRD_PARTY_NOTICES).

// The pattern generator of Intl.DateTimeFormat: a port of ICU 78's
// DateTimePatternGenerator (dtptngen.cpp), which V8 asks for the best
// pattern of the skeleton it builds from the options. A locale's pattern
// map holds the canonical items, the style patterns and availableFormats,
// added as ICU's initData adds them (their order decides the ties of the
// distance); getBestPattern finds the nearest skeleton, adjusts the widths
// of the fields to the requested ones and, when no single pattern has every
// field, joins the best date and time patterns with the locale's
// date-time glue, appending what is still missing with appendItems.

import (
	"strconv"
	"strings"
)

// The fields of a skeleton (UDateTimePatternField).
const (
	dtEra = iota
	dtYear
	dtQuarter
	dtMonth
	dtWeekOfYear
	dtWeekOfMonth
	dtWeekday
	dtDayOfYear
	dtDayOfWeekInMonth
	dtDay
	dtDayPeriod
	dtHour
	dtMinute
	dtSecond
	dtFractional
	dtZone
	dtFieldCount
)

// The types of dtTypes and the distances of getDistance.
const (
	dtNarrow       = -0x101
	dtShorter      = -0x102
	dtShort        = -0x103
	dtLong         = -0x104
	dtNumeric      = 0x100
	dtDelta        = 0x10
	dtExtraField   = 0x10000
	dtMissingField = 0x1000

	dtDateMask       = 1<<dtDayPeriod - 1
	dtTimeMask       = 1<<dtFieldCount - 1 - dtDateMask
	dtFractionalMask = 1 << dtFractional
	dtSecondFracMask = 1<<dtSecond | 1<<dtFractional
)

// The flags of getBestPattern and the options of V8's call.
const (
	dtFixFractional   = 1
	dtUsesCapJ        = 2
	dtMatchHourLength = 1 << dtHour // UDATPG_MATCH_HOUR_FIELD_LENGTH
)

// dtType is a row of ICU's dtTypes: a pattern letter from a minimum length,
// its field and the type the distance compares.
type dtType struct {
	ch     byte
	field  int8
	typ    int16
	minLen int8
}

var dtTypes = [...]dtType{
	{'G', dtEra, dtShort, 1},
	{'G', dtEra, dtLong, 4},
	{'G', dtEra, dtNarrow, 5},

	{'y', dtYear, dtNumeric, 1},
	{'Y', dtYear, dtNumeric + dtDelta, 1},
	{'u', dtYear, dtNumeric + 2*dtDelta, 1},
	{'r', dtYear, dtNumeric + 3*dtDelta, 1},
	{'U', dtYear, dtShort, 1},
	{'U', dtYear, dtLong, 4},
	{'U', dtYear, dtNarrow, 5},

	{'Q', dtQuarter, dtNumeric, 1},
	{'Q', dtQuarter, dtShort, 3},
	{'Q', dtQuarter, dtLong, 4},
	{'Q', dtQuarter, dtNarrow, 5},
	{'q', dtQuarter, dtNumeric + dtDelta, 1},
	{'q', dtQuarter, dtShort - dtDelta, 3},
	{'q', dtQuarter, dtLong - dtDelta, 4},
	{'q', dtQuarter, dtNarrow - dtDelta, 5},

	{'M', dtMonth, dtNumeric, 1},
	{'M', dtMonth, dtShort, 3},
	{'M', dtMonth, dtLong, 4},
	{'M', dtMonth, dtNarrow, 5},
	{'L', dtMonth, dtNumeric + dtDelta, 1},
	{'L', dtMonth, dtShort - dtDelta, 3},
	{'L', dtMonth, dtLong - dtDelta, 4},
	{'L', dtMonth, dtNarrow - dtDelta, 5},
	{'l', dtMonth, dtNumeric + dtDelta, 1},

	{'w', dtWeekOfYear, dtNumeric, 1},

	{'W', dtWeekOfMonth, dtNumeric, 1},

	{'E', dtWeekday, dtShort, 1},
	{'E', dtWeekday, dtLong, 4},
	{'E', dtWeekday, dtNarrow, 5},
	{'E', dtWeekday, dtShorter, 6},
	{'c', dtWeekday, dtNumeric + 2*dtDelta, 1},
	{'c', dtWeekday, dtShort - 2*dtDelta, 3},
	{'c', dtWeekday, dtLong - 2*dtDelta, 4},
	{'c', dtWeekday, dtNarrow - 2*dtDelta, 5},
	{'c', dtWeekday, dtShorter - 2*dtDelta, 6},
	{'e', dtWeekday, dtNumeric + dtDelta, 1},
	{'e', dtWeekday, dtShort - dtDelta, 3},
	{'e', dtWeekday, dtLong - dtDelta, 4},
	{'e', dtWeekday, dtNarrow - dtDelta, 5},
	{'e', dtWeekday, dtShorter - dtDelta, 6},

	{'d', dtDay, dtNumeric, 1},
	{'g', dtDay, dtNumeric + dtDelta, 1},

	{'D', dtDayOfYear, dtNumeric, 1},

	{'F', dtDayOfWeekInMonth, dtNumeric, 1},

	{'a', dtDayPeriod, dtShort, 1},
	{'a', dtDayPeriod, dtLong, 4},
	{'a', dtDayPeriod, dtNarrow, 5},
	{'b', dtDayPeriod, dtShort - dtDelta, 1},
	{'b', dtDayPeriod, dtLong - dtDelta, 4},
	{'b', dtDayPeriod, dtNarrow - dtDelta, 5},
	{'B', dtDayPeriod, dtShort - 3*dtDelta, 1},
	{'B', dtDayPeriod, dtLong - 3*dtDelta, 4},
	{'B', dtDayPeriod, dtNarrow - 3*dtDelta, 5},

	{'H', dtHour, dtNumeric + 10*dtDelta, 1},
	{'k', dtHour, dtNumeric + 11*dtDelta, 1},
	{'h', dtHour, dtNumeric, 1},
	{'K', dtHour, dtNumeric + dtDelta, 1},
	{'J', dtHour, dtNumeric + 5*dtDelta, 1},
	{'j', dtHour, dtNumeric + 6*dtDelta, 1},
	{'C', dtHour, dtNumeric + 7*dtDelta, 1},

	{'m', dtMinute, dtNumeric, 1},

	{'s', dtSecond, dtNumeric, 1},
	{'A', dtSecond, dtNumeric + dtDelta, 1},

	{'S', dtFractional, dtNumeric, 1},

	{'v', dtZone, dtShort - 2*dtDelta, 1},
	{'v', dtZone, dtLong - 2*dtDelta, 4},
	{'z', dtZone, dtShort, 1},
	{'z', dtZone, dtLong, 4},
	{'Z', dtZone, dtNarrow - dtDelta, 1},
	{'Z', dtZone, dtLong - dtDelta, 4},
	{'Z', dtZone, dtShort - dtDelta, 5},
	{'O', dtZone, dtShort - dtDelta, 1},
	{'O', dtZone, dtLong - dtDelta, 4},
	{'V', dtZone, dtShort - dtDelta, 1},
	{'V', dtZone, dtLong - dtDelta, 2},
	{'V', dtZone, dtLong - 1 - dtDelta, 3},
	{'V', dtZone, dtLong - 2 - dtDelta, 4},
	{'X', dtZone, dtNarrow - dtDelta, 1},
	{'X', dtZone, dtShort - dtDelta, 2},
	{'X', dtZone, dtLong - dtDelta, 4},
	{'x', dtZone, dtNarrow - dtDelta, 1},
	{'x', dtZone, dtShort - dtDelta, 2},
	{'x', dtZone, dtLong - dtDelta, 4},
}

// dtCanonicalIndex returns the row of dtTypes of a run of one pattern
// letter (FormatParser::getCanonicalIndex), -1 for a letter it lacks.
func dtCanonicalIndex(tok string) int {
	ch, n := tok[0], len(tok)
	for i := 0; i < len(dtTypes); i++ {
		if dtTypes[i].ch != ch {
			continue
		}
		if i+1 == len(dtTypes) || dtTypes[i+1].ch != ch || int(dtTypes[i+1].minLen) > n {
			return i
		}
	}
	return -1
}

// dtMaxTokens is FormatParser's limit on the items of a pattern.
const dtMaxTokens = 50

// dtTokens splits a pattern as FormatParser does: runs of one ASCII letter
// and every other character alone, at most dtMaxTokens. It returns the
// tokens as offsets: token i is p[ends[i-1]:ends[i]].
func dtTokens(p string, ends *[dtMaxTokens]int) int {
	n, i := 0, 0
	for i < len(p) && n < dtMaxTokens {
		c := p[i]
		j := i + 1
		if dtIsLetter(c) {
			for j < len(p) && p[j] == c {
				j++
			}
		} else if c >= 0x80 {
			for j < len(p) && p[j]&0xC0 == 0x80 {
				j++
			}
		}
		ends[n] = j
		n++
		i = j
	}
	return n
}

func dtIsLetter(c byte) bool { return c|0x20 >= 'a' && c|0x20 <= 'z' }

// dtQuoteEnd returns the index of the token that ends the quoted literal
// starting at token i (FormatParser::getQuoteLiteral), n when it is open.
func dtQuoteEnd(p string, ends *[dtMaxTokens]int, n, i int) int {
	if p[dtTokenStart(ends, i)] == '\'' {
		i++
	}
	for i < n {
		if p[dtTokenStart(ends, i)] == '\'' {
			if i+1 < n && p[ends[i]] == '\'' {
				i += 2
				continue
			}
			return i
		}
		i++
	}
	return i
}

func dtTokenStart(ends *[dtMaxTokens]int, i int) int {
	if i == 0 {
		return 0
	}
	return ends[i-1]
}

// dtFields is ICU's SkeletonFields: the letter and length of each field.
type dtFields struct {
	chars [dtFieldCount]byte
	lens  [dtFieldCount]int8
}

func (f *dtFields) set(field int, ch byte, n int) { f.chars[field], f.lens[field] = ch, int8(n) }
func (f *dtFields) clear(field int)               { f.chars[field], f.lens[field] = 0, 0 }
func (f *dtFields) empty(field int) bool          { return f.lens[field] == 0 }

func (f *dtFields) appendField(b []byte, field int) []byte {
	for range f.lens[field] {
		b = append(b, f.chars[field])
	}
	return b
}

func (f *dtFields) appendTo(b []byte) []byte {
	for i := range dtFieldCount {
		b = f.appendField(b, i)
	}
	return b
}

// dtSkeleton is ICU's PtnSkeleton: the type of each field, the fields as
// given (original) and canonical (base).
type dtSkeleton struct {
	typ                   [dtFieldCount]int16
	original, base        dtFields
	addedDefaultDayPeriod bool
}

// set parses a pattern or a skeleton (DateTimeMatcher::set).
func (s *dtSkeleton) set(p string) {
	*s = dtSkeleton{}
	var ends [dtMaxTokens]int
	n := dtTokens(p, &ends)
	for i := 0; i < n; i++ {
		start := dtTokenStart(&ends, i)
		tok := p[start:ends[i]]
		if tok[0] == '\'' {
			i = dtQuoteEnd(p, &ends, n, i)
			continue
		}
		ci := dtCanonicalIndex(tok)
		if ci < 0 {
			continue
		}
		row := &dtTypes[ci]
		f := int(row.field)
		s.original.set(f, tok[0], len(tok))
		s.base.set(f, row.ch, int(row.minLen))
		t := row.typ
		if t > 0 {
			t += int16(len(tok))
		}
		s.typ[f] = t
	}
	// Minutes and fractional seconds without seconds get seconds.
	if !s.original.empty(dtMinute) && !s.original.empty(dtFractional) && s.original.empty(dtSecond) {
		s.original.set(dtSecond, 's', 1)
		s.base.set(dtSecond, 's', 1)
		s.typ[dtSecond] = dtNumeric + 1
	}
	// A 12-hour cycle without a day period gets "a"; a 24-hour one loses
	// its day period.
	if !s.original.empty(dtHour) {
		if c := s.original.chars[dtHour]; c == 'h' || c == 'K' {
			if s.original.empty(dtDayPeriod) {
				s.original.set(dtDayPeriod, 'a', 1)
				s.base.set(dtDayPeriod, 'a', 1)
				s.typ[dtDayPeriod] = dtShort
				s.addedDefaultDayPeriod = true
			}
		} else {
			s.original.clear(dtDayPeriod)
			s.base.clear(dtDayPeriod)
			s.typ[dtDayPeriod] = 0
		}
	}
}

// skeleton returns the skeleton string (PtnSkeleton::getSkeleton): the
// original fields without a day period set added.
func (s *dtSkeleton) skeleton() string {
	b := s.original.appendTo(make([]byte, 0, 16))
	if s.addedDefaultDayPeriod {
		for i, c := range b {
			if c == 'a' {
				b = append(b[:i], b[i+1:]...)
				break
			}
		}
	}
	return string(b)
}

// fieldMask returns the fields the skeleton has.
func (s *dtSkeleton) fieldMask() int {
	m := 0
	for i, t := range s.typ {
		if t != 0 {
			m |= 1 << i
		}
	}
	return m
}

// distance is DateTimeMatcher::getDistance from s to other over the
// fields of include, with the fields other lacks (missing) and has beyond
// s (extra).
func (s *dtSkeleton) distance(other *dtSkeleton, include int) (d, missing, extra int) {
	for i := range dtFieldCount {
		my := int(s.typ[i])
		if include&(1<<i) == 0 {
			my = 0
		}
		o := int(other.typ[i])
		switch {
		case my == o:
		case my == 0:
			d += dtExtraField
			extra |= 1 << i
		case o == 0:
			d += dtMissingField
			missing |= 1 << i
		case my > o:
			d += my - o
		default:
			d += o - my
		}
	}
	return d, missing, extra
}

// dtEntry is an entry of a pattern map (PtnElem).
type dtEntry struct {
	base      string
	skel      dtSkeleton
	pattern   string
	specified bool
}

// intlPatternMap is a locale's DateTimePatternGenerator without the parts
// that depend on the formatter (the hour cycle, the decimal separator).
type intlPatternMap struct {
	entries []dtEntry
	// boot lists the entries by the first letter of their base pattern
	// (A-Z, a-z), in the order they were added; order is the concatenation,
	// the order of ICU's PatternMapIterator.
	boot  [52][]int32
	order []int32

	appendItems [dtFieldCount]string
	fieldNames  [dtFieldCount]string
	dateTime    [4]string
}

func dtBootIndex(c byte) int {
	switch {
	case c >= 'A' && c <= 'Z':
		return int(c - 'A')
	case c >= 'a' && c <= 'z':
		return 26 + int(c-'a')
	}
	return -1
}

// patternMap returns the locale's pattern map, with the style patterns
// (std) or without them (ICU's createInstanceNoStdPat).
func (d *intlDateData) patternMap(std bool) *intlPatternMap {
	i := 0
	if !std {
		i = 1
	}
	d.genOnce[i].Do(func() { d.buildPatternMap(&d.gen[i], std) })
	return &d.gen[i]
}

// buildPatternMap runs ICU's initData.
func (d *intlDateData) buildPatternMap(m *intlPatternMap, std bool) {
	m.entries = make([]dtEntry, 0, 16+8+len(d.avail)/2)
	for _, c := range "GyQMwWEDFdaHmsSv" {
		m.add(string(c), "", false, false)
	}
	if std {
		for _, p := range d.timeFormats {
			m.add(p, "", false, false)
		}
		for _, p := range d.dateFormats {
			m.add(p, "", false, false)
		}
	}
	for i := 0; i < len(d.avail); i += 2 {
		m.add(d.avail[i+1], d.avail[i], true, true)
	}
	for i := range dtFieldCount {
		m.appendItems[i] = d.appendItems[i]
		if m.appendItems[i] == "" {
			m.appendItems[i] = "{0} ├{2}: {1}┤"
		}
		m.fieldNames[i] = d.fieldNames[i]
		if m.fieldNames[i] == "" {
			m.fieldNames[i] = "F" + strconv.Itoa(i)
		}
	}
	m.dateTime = d.atTime
	n := 0
	for _, l := range m.boot {
		n += len(l)
	}
	m.order = make([]int32, 0, n)
	for _, l := range m.boot {
		m.order = append(m.order, l...)
	}
}

// add is addPatternWithOptionalSkeleton.
func (m *intlPatternMap) add(pattern, skeleton string, hasSkeleton, override bool) {
	var s dtSkeleton
	if hasSkeleton {
		s.set(skeleton)
	} else {
		s.set(pattern)
	}
	base := string(s.base.appendTo(make([]byte, 0, 16)))
	if dup, specified := m.byBase(base); dup >= 0 && (!specified || hasSkeleton && !override) {
		if !override {
			return
		}
	}
	if dup := m.bySkeleton(&s); dup >= 0 {
		if !override || hasSkeleton && m.entries[dup].specified {
			return
		}
	}
	// PatternMap::add.
	if base == "" {
		return
	}
	b := dtBootIndex(base[0])
	if b < 0 {
		return
	}
	for _, i := range m.boot[b] {
		if e := &m.entries[i]; e.base == base && e.skel.typ == s.typ {
			e.pattern, e.specified = pattern, hasSkeleton
			return
		}
	}
	m.boot[b] = append(m.boot[b], int32(len(m.entries)))
	m.entries = append(m.entries, dtEntry{base: base, skel: s, pattern: pattern, specified: hasSkeleton})
}

// byBase is getPatternFromBasePattern: the entry of a base pattern and
// whether its skeleton was given, -1 when there is none.
func (m *intlPatternMap) byBase(base string) (int, bool) {
	if base == "" {
		return -1, false
	}
	if b := dtBootIndex(base[0]); b >= 0 {
		for _, i := range m.boot[b] {
			if m.entries[i].base == base {
				return int(i), m.entries[i].specified
			}
		}
	}
	return -1, false
}

// bySkeleton is getPatternFromSkeleton: the entry whose original fields
// are those of s, -1 when there is none.
func (m *intlPatternMap) bySkeleton(s *dtSkeleton) int {
	first := byte(0)
	for i := range dtFieldCount {
		if s.base.lens[i] != 0 {
			first = s.base.chars[i]
			break
		}
	}
	if b := dtBootIndex(first); b >= 0 {
		for _, i := range m.boot[b] {
			if m.entries[i].skel.original == s.original {
				return int(i)
			}
		}
	}
	return -1
}

// intlDTPG is a pattern generator: a pattern map with the hour cycle and
// decimal separator of a formatter.
type intlDTPG struct {
	m *intlPatternMap
	// hourChar is fDefaultHourFormatChar: h, H, K or k.
	hourChar byte
	decimal  string
	// req is the skeleton asked for (dtMatcher); missing and extra are the
	// distance info of the last bestRaw.
	req            dtSkeleton
	missing, extra int
}

// bestRaw is getBestRaw: the pattern of the nearest skeleton over the
// fields of include and its skeleton when it was given.
func (g *intlDTPG) bestRaw(include int) (string, *dtSkeleton) {
	best, bestMissing := 0x7fffffff, -1
	var pattern string
	var spec *dtSkeleton
	for _, i := range g.m.order {
		e := &g.m.entries[i]
		d, missing, extra := g.req.distance(&e.skel, include)
		if d < best || d == best && bestMissing < missing {
			best, bestMissing = d, missing
			pattern, spec = e.pattern, nil
			if e.specified {
				spec = &e.skel
			}
			g.missing, g.extra = missing, extra
			if d == 0 {
				break
			}
		}
	}
	return pattern, spec
}

// adjust is adjustFieldTypes: the pattern with the letters and widths of
// its fields set from the requested skeleton.
func (g *intlDTPG) adjust(pattern string, spec *dtSkeleton, flags, options int) string {
	var ends [dtMaxTokens]int
	n := dtTokens(pattern, &ends)
	out := make([]byte, 0, len(pattern)+8)
	for i := 0; i < n; i++ {
		start := dtTokenStart(&ends, i)
		tok := pattern[start:ends[i]]
		if tok[0] == '\'' {
			end := dtQuoteEnd(pattern, &ends, n, i)
			stop := len(pattern)
			if end < n {
				stop = ends[end]
			} else if n > 0 {
				stop = ends[n-1]
			}
			out = append(out, pattern[start:stop]...)
			i = end
			continue
		}
		ci := -1
		if dtIsLetter(tok[0]) {
			ci = dtCanonicalIndex(tok)
		}
		if ci < 0 {
			out = append(out, tok...)
			continue
		}
		row := &dtTypes[ci]
		field := int(row.field)
		req := &g.req
		switch {
		case flags&dtFixFractional != 0 && field == dtSecond:
			out = append(out, tok...)
			out = append(out, g.decimal...)
			out = req.original.appendField(out, dtFractional)
			continue
		case req.typ[field] == 0:
			out = append(out, tok...)
			continue
		}
		reqChar := req.original.chars[field]
		reqLen := int(req.original.lens[field])
		if reqChar == 'E' && reqLen < 3 {
			reqLen = 3
		}
		adjLen := reqLen
		if field == dtHour && options&dtMatchHourLength == 0 ||
			field == dtMinute || field == dtSecond {
			adjLen = len(tok)
		} else if spec != nil && reqChar != 'c' && reqChar != 'e' {
			skelLen := int(spec.original.lens[field])
			patNumeric, reqNumeric := row.typ > 0, req.typ[field] > 0
			if skelLen == reqLen || patNumeric != reqNumeric {
				adjLen = len(tok)
			}
		}
		c := reqChar
		if field == dtHour || field == dtMonth || field == dtWeekday || field == dtYear && reqChar != 'Y' {
			c = tok[0]
		}
		if c == 'E' && adjLen < 3 {
			c = 'e'
		}
		if field == dtHour && g.hourChar != 0 {
			switch {
			case flags&dtUsesCapJ != 0 || reqChar == g.hourChar:
				c = g.hourChar
			case reqChar == 'h' && g.hourChar == 'K':
				c = 'K'
			case reqChar == 'H' && g.hourChar == 'k':
				c = 'k'
			case reqChar == 'k' && g.hourChar == 'H':
				c = 'H'
			case reqChar == 'K' && g.hourChar == 'h':
				c = 'h'
			}
		}
		for range adjLen {
			out = append(out, c)
		}
	}
	return string(out)
}

// bestAppending is getBestAppending: the pattern of the fields missing,
// built from the nearest pattern and appendItems for what it lacks.
func (g *intlDTPG) bestAppending(missingFields, flags, options int) string {
	if missingFields == 0 {
		return ""
	}
	p, spec := g.bestRaw(missingFields)
	result := g.adjust(p, spec, flags, options)
	last := 0
	for g.missing != 0 {
		if last == g.missing {
			break
		}
		if g.missing&dtSecondFracMask == dtFractionalMask && missingFields&dtSecondFracMask == dtSecondFracMask {
			result = g.adjust(result, spec, flags|dtFixFractional, options)
			g.missing &^= dtFractionalMask
			continue
		}
		starting := g.missing
		p, spec = g.bestRaw(g.missing)
		p = g.adjust(p, spec, flags, options)
		top := dtTopBit(starting &^ g.missing)
		if item := g.m.appendItems[top]; item != "" {
			result = intlSimpleFormat(item, result, p, "'"+g.m.fieldNames[top]+"'")
		}
		last = g.missing
	}
	return result
}

func dtTopBit(mask int) int {
	if mask == 0 {
		return 0
	}
	i := 0
	for mask != 0 {
		mask >>= 1
		i++
	}
	return min(i-1, dtZone)
}

// bestPattern is getBestPattern.
func (g *intlDTPG) bestPattern(skeleton string, options int) string {
	flags := 0
	skeleton = g.mapMetaCharacters(skeleton, &flags)
	g.req.set(skeleton)
	p, spec := g.bestRaw(-1)
	if g.missing == 0 && g.extra == 0 {
		return g.adjust(p, spec, flags, options)
	}
	needed := g.req.fieldMask()
	date := g.bestAppending(needed&dtDateMask, flags, options)
	time := g.bestAppending(needed&dtTimeMask, flags, options)
	switch {
	case date == "":
		return time
	case time == "":
		return date
	}
	style := 3 // short
	switch g.req.base.lens[dtMonth] {
	case 4:
		style = 1
		if g.req.base.lens[dtWeekday] > 0 {
			style = 0
		}
	case 3:
		style = 2
	}
	return intlSimpleFormat(g.m.dateTime[style], time, date)
}

// mapMetaCharacters is mapSkeletonMetacharacters for j and J (V8 and
// SimpleDateFormat ask for no C).
func (g *intlDTPG) mapMetaCharacters(skeleton string, flags *int) string {
	if strings.IndexAny(skeleton, "jJ'") < 0 {
		return skeleton
	}
	var b []byte
	inQuote := false
	for i := 0; i < len(skeleton); i++ {
		c := skeleton[i]
		switch {
		case c == '\'':
			inQuote = !inQuote
		case inQuote:
		case c == 'j':
			extra := 0
			for i+1 < len(skeleton) && skeleton[i+1] == 'j' {
				extra++
				i++
			}
			hourLen := 1 + extra&1
			periodLen := 1
			if extra >= 2 {
				periodLen = 3 + extra>>1
			}
			if g.hourChar == 'H' || g.hourChar == 'k' {
				periodLen = 0
			}
			for range periodLen {
				b = append(b, 'a')
			}
			for range hourLen {
				b = append(b, g.hourChar)
			}
		case c == 'J':
			b = append(b, 'H')
			*flags |= dtUsesCapJ
		default:
			b = append(b, c)
		}
	}
	return string(b)
}

// intlSimpleFormat applies ICU's SimpleFormatter pattern: {n} takes
// args[n]; an apostrophe before a brace starts a quoted literal, a doubled
// one is one apostrophe, any other is itself.
func intlSimpleFormat(pattern string, args ...string) string {
	var b strings.Builder
	inQuote := false
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch {
		case c == '\'':
			switch {
			case i+1 < len(pattern) && pattern[i+1] == '\'':
				i++
				b.WriteByte('\'')
			case inQuote:
				inQuote = false
			case i+1 < len(pattern) && (pattern[i+1] == '{' || pattern[i+1] == '}'):
				i++
				inQuote = true
				b.WriteByte(pattern[i])
			default:
				b.WriteByte('\'')
			}
		case !inQuote && c == '{' && i+2 < len(pattern) && pattern[i+2] == '}' && pattern[i+1] >= '0' && int(pattern[i+1]-'0') < len(args):
			b.WriteString(args[pattern[i+1]-'0'])
			i += 2
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// staticSkeleton is DateTimePatternGenerator::staticGetSkeleton.
func intlStaticSkeleton(pattern string) string {
	var s dtSkeleton
	s.set(pattern)
	return s.skeleton()
}
