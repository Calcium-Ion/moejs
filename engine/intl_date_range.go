package engine

// Ported from ICU's DateIntervalFormat and DateIntervalInfo
// (icu4c/source/i18n/dtitvfmt.cpp and dtitvinf.cpp; Unicode License v3; see
// THIRD_PARTY_NOTICES).

// formatRange and formatRangeToParts of Intl.DateTimeFormat: a port of ICU
// 78's DateIntervalFormat as V8 uses it. V8 makes the interval formatter
// from the skeleton of the formatter's pattern, in its locale with the
// formatter's hour cycle as the hc keyword; ICU finds the
// intervalFormats skeleton nearest to it (DateIntervalInfo), splits each
// interval pattern into the parts that format the first and the second
// date, and falls back to the locale's "{0} – {1}" with the full pattern
// when no interval pattern covers the largest field that differs. V8 uses
// the formatter's own format when the two dates are equal in every field
// the interval formatter looks at (the result has no range spans).

import (
	"strings"
	"unsafe"
)

// The calendar fields of a range (UCalendarDateFields) in the order of the
// interval pattern indexes (DateIntervalInfo's kIPI_*), their pattern
// letters (fgCalendarFieldToPatternLetter) and levels
// (fgCalendarFieldToLevel).
const (
	itvEra = iota
	itvYear
	itvMonth
	itvDate
	itvAmPm
	itvHour
	itvMinute
	itvSecond
	itvMillisecond
	itvCount
)

var (
	itvLetters = [itvCount]byte{'G', 'y', 'M', 'd', 'a', 'h', 'm', 's', 'S'}
	itvLevels  = [itvCount]int{0, 10, 20, 30, 40, 50, 60, 70, 80}
)

// intlCharLevel is SimpleDateFormat::getLevelFromChar.
func intlCharLevel(c byte) int {
	switch c {
	case 'G', 'O', 'V', 'X', 'Z', 'g', 'l', 'v', 'x', 'z':
		return 0
	case 'U', 'Y', 'r', 'u', 'y':
		return 10
	case 'D', 'L', 'M', 'Q', 'q', 'w':
		return 20
	case 'E', 'F', 'W', 'c', 'd', 'e':
		return 30
	case 'A', 'a':
		return 40
	case 'H', 'K', 'h', 'k':
		return 50
	case 'm':
		return 60
	case 's':
		return 70
	case 'S':
		return 80
	}
	return -1
}

// intlFieldUnitIgnored is SimpleDateFormat::isFieldUnitIgnored: whether no
// field of a pattern (or skeleton) is as small as the calendar field.
func intlFieldUnitIgnored(p string, field int) bool {
	level := itvLevels[field]
	inQuote := false
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c == '\'':
			if i+1 < len(p) && p[i+1] == '\'' {
				i++
			} else {
				inQuote = !inQuote
			}
		case !inQuote && dtIsLetter(c):
			if level <= intlCharLevel(c) {
				return false
			}
		}
	}
	return true
}

// intlItvPattern is an interval pattern split into the parts of the first
// and the second date (PatternInfo); a fallback has only second, the full
// pattern of each date.
type intlItvPattern struct {
	first, second           string
	firstItems, secondItems []intlDateItem
	laterFirst              bool
}

// intlDateRange is the interval formatter of a DateTimeFormat.
type intlDateRange struct {
	d        *intlDateData
	hourChar byte
	decimal  string
	skeleton string // fSkeleton

	// pattern is the formatter's own pattern (fDateFormat).
	pattern string
	items   []intlDateItem

	patterns     [itvCount]intlItvPattern
	datePattern  string // fDatePattern, "" when absent
	timePattern  string // fTimePattern
	dateItems    []intlDateItem
	timeItems    []intlDateItem
	dateTime     string // fDateTimeFormat, "" when absent
	fallback     string
	defaultLater bool
	info         *intlItvInfo
}

// intlItvInfo is a locale's DateIntervalInfo: the interval patterns by
// skeleton and interval pattern index, the skeletons in order.
type intlItvInfo struct {
	m    map[string]*[itvCount]string
	keys []string
}

// intlRangeKey keys the interval formatters a locale shares: a formatter's
// interval formatter depends on its pattern's skeleton, hour letter and
// decimal separator only.
type intlRangeKey struct {
	skeleton, decimal string
	hourChar          byte
}

// best is DateFormat::getBestPattern in the formatter's locale.
func (g *intlDateRange) best(skeleton string) string {
	return g.d.bestPatternFor(true, g.hourChar, g.decimal, skeleton, 0)
}

// dateRange returns the formatter's interval formatter, made on first use:
// the one the locale shares for its pattern, or once the locale's cache is
// full, its own, charged to r.
func (f *intlDateTimeFormat) dateRange(r *Realm) *intlDateRange {
	if f.rng != nil {
		return f.rng
	}
	d := f.data
	key := intlRangeKey{intlStaticSkeleton(f.pattern), f.decimal, f.hourChar}
	if f.hourCycle != 0 {
		key.hourChar = f.hourCycle
	}
	if v, ok := d.ranges.Load(key); ok {
		f.rng = v.(*intlDateRange)
		return f.rng
	}
	g := &intlDateRange{d: d, hourChar: key.hourChar, decimal: key.decimal, skeleton: key.skeleton}
	g.pattern = g.best(g.skeleton)
	g.items = d.compile(r, g.pattern)
	g.fallback = d.intervalFallback
	g.defaultLater = strings.Index(g.fallback, "{0}") > strings.Index(g.fallback, "{1}")
	g.info = d.intervalInfo()
	g.initialize()
	n := int(unsafe.Sizeof(intlDateRange{})) + len(g.datePattern) + len(g.timePattern)
	items := len(g.dateItems) + len(g.timeItems)
	for _, p := range g.patterns {
		n += len(p.first) + len(p.second)
		items += len(p.firstItems) + len(p.secondItems)
	}
	n += items * int(unsafe.Sizeof(intlDateItem{}))
	if c := n + int(unsafe.Sizeof(key)) + len(key.skeleton) + len(key.decimal); intlCacheTake(c) {
		if v, loaded := d.ranges.LoadOrStore(key, g); loaded {
			intlCacheRefund(c)
			g = v.(*intlDateRange)
		}
	} else {
		r.charge(n)
	}
	f.rng = g
	return g
}

// intervalInfo returns the locale's DateIntervalInfo, built on first use.
func (d *intlDateData) intervalInfo() *intlItvInfo {
	d.itvOnce.Do(func() { d.itv = intlBuildIntervalInfo(d) })
	return d.itv
}

// intlBuildIntervalInfo builds DateIntervalInfo from the locale's
// intervalFormats.
func intlBuildIntervalInfo(d *intlDateData) *intlItvInfo {
	info := &intlItvInfo{m: make(map[string]*[itvCount]string, len(d.intervals))}
	for _, it := range d.intervals {
		var ps [itvCount]string
		for j, c := range it.diffs {
			field := -1
			switch c {
			case 'G':
				field = itvEra
			case 'y':
				field = itvYear
			case 'M':
				field = itvMonth
			case 'd':
				field = itvDate
			case 'a', 'B':
				field = itvAmPm
			case 'h', 'H':
				field = itvHour
			case 'm':
				field = itvMinute
			}
			if field >= 0 && ps[field] == "" {
				ps[field] = it.patterns[j]
			}
		}
		info.m[it.skeleton] = &ps
		info.keys = append(info.keys, it.skeleton)
	}
	return info
}

func (g *intlDateRange) intervalPattern(skeleton string, field int) string {
	if ps := g.info.m[skeleton]; ps != nil {
		return ps[field]
	}
	return ""
}

// intlSkeletonWidths counts the letters of a skeleton (parseSkeleton).
func intlSkeletonWidths(s string, w *[58]int) {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= 'A' && c <= 'z' {
			w[c-'A']++
		}
	}
}

// bestSkeleton is DateIntervalInfo::getBestSkeleton: the nearest skeleton
// with interval patterns and how it differs (0 equal, 1 widths, 2 only
// z/v, h/K or H/k, -1 fields).
func (g *intlDateRange) bestSkeleton(skeleton string) (string, int) {
	replaced := false
	if strings.ContainsAny(skeleton, "zkKab") {
		r := strings.NewReplacer("z", "v", "k", "H", "K", "h", "a", "", "b", "")
		skeleton = r.Replace(skeleton)
		replaced = true
	}
	var in [58]int
	intlSkeletonWidths(skeleton, &in)
	best, bestDist, info := "", int(^uint(0)>>1), 0
	found := false
	for _, k := range g.info.keys {
		var w [58]int
		intlSkeletonWidths(k, &w)
		dist, diff := 0, 1
		for i := range w {
			a, b := in[i], w[i]
			switch {
			case a == b:
			case a == 0 || b == 0:
				diff = -1
				dist += 0x1000
			case i+'A' == 'M' && (a <= 2 && b > 2 || a > 2 && b <= 2):
				dist += 0x100
			case a > b:
				dist += a - b
			default:
				dist += b - a
			}
		}
		if dist < bestDist {
			best, bestDist, info, found = k, dist, diff, true
		}
		if dist == 0 {
			info = 0
			break
		}
	}
	if !found {
		return "", 0
	}
	if replaced && info != -1 {
		info = 2
	}
	return best, info
}

// initialize is DateIntervalFormat::initializePattern.
func (g *intlDateRange) initialize() {
	for i := range g.patterns {
		g.patterns[i].laterFirst = g.defaultLater
	}
	converted := g.normalizeHour(g.skeleton)
	date, normDate, time, normTime := intlDateTimeSkeleton(converted)
	if time != "" && date != "" {
		g.dateTime = g.d.glue[2]
	}
	found := g.setSeparate(normDate, normTime)
	if !found || date == "" {
		if time != "" && date == "" {
			p := g.best("yMd" + time)
			for _, f := range []int{itvDate, itvMonth, itvYear} {
				g.setInfo(f, nil, &p, g.defaultLater)
			}
			p = g.best("GyMd" + time)
			g.setInfo(itvEra, nil, &p, g.defaultLater)
		}
		return
	}
	if time == "" {
		return
	}
	skel := g.skeleton
	for _, f := range []int{itvDate, itvMonth, itvYear, itvEra} {
		if !strings.ContainsRune(date, rune(itvLetters[f])) {
			skel = string(itvLetters[f]) + skel
			p := g.best(skel)
			g.setInfo(f, nil, &p, g.defaultLater)
		}
	}
	if g.dateTime == "" {
		return
	}
	dp := g.best(date)
	for _, f := range []int{itvAmPm, itvHour, itvMinute} {
		g.concatDateTime(dp, f)
	}
}

// normalizeHour is normalizeHourMetacharacters.
func (g *intlDateRange) normalizeHour(s string) string {
	var hourMeta, periodChar byte
	hourStart, hourLen, periodStart, periodLen := 0, 0, 0, 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case 'j', 'J', 'C', 'h', 'H', 'k', 'K':
			if hourMeta == 0 {
				hourMeta, hourStart = c, i
			}
			hourLen++
		case 'a', 'b', 'B':
			if periodChar == 0 {
				periodChar, periodStart = c, i
			}
			periodLen++
		default:
			if hourMeta != 0 && periodChar != 0 {
				i = len(s)
			}
		}
	}
	if hourMeta == 0 {
		return s
	}
	hourChar := byte('H')
	p := g.best(string(hourMeta))
	// Strip the quoted literals.
	for {
		q := strings.IndexByte(p, '\'')
		if q < 0 {
			break
		}
		q2 := strings.IndexByte(p[q+1:], '\'')
		if q2 < 0 {
			q2 = q
		} else {
			q2 += q + 1
		}
		p = p[:q] + p[q2+1:]
	}
	switch {
	case strings.IndexByte(p, 'h') >= 0:
		hourChar = 'h'
	case strings.IndexByte(p, 'K') >= 0:
		hourChar = 'K'
	case strings.IndexByte(p, 'k') >= 0:
		hourChar = 'k'
	}
	switch {
	case strings.IndexByte(p, 'b') >= 0:
		periodChar = 'b'
	case strings.IndexByte(p, 'B') >= 0:
		periodChar = 'B'
	case periodChar == 0:
		periodChar = 'a'
	}
	repl := string(hourChar)
	if hourChar != 'H' && hourChar != 'k' {
		n := 1
		switch {
		case periodLen >= 5 || hourLen >= 5:
			n = 5
		case periodLen >= 3 || hourLen >= 3:
			n = 3
		}
		repl += strings.Repeat(string(periodChar), n)
	}
	s = s[:hourStart] + repl + s[hourStart+hourLen:]
	if periodStart > hourStart {
		periodStart += len(repl) - hourLen
	}
	return s[:periodStart] + s[periodStart+periodLen:]
}

// intlDateTimeSkeleton is getDateTimeSkeleton: a skeleton's date and time
// fields, as given and normalized.
func intlDateTimeSkeleton(s string) (date, normDate, time, normTime string) {
	var d, nd, t, nt []byte
	var e, dc, mc, yc, minC, vc, zc int
	var hour byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case 'E':
			d = append(d, c)
			e++
		case 'd':
			d = append(d, c)
			dc++
		case 'M':
			d = append(d, c)
			mc++
		case 'y':
			d = append(d, c)
			yc++
		case 'G', 'Y', 'u', 'Q', 'q', 'L', 'l', 'W', 'w', 'D', 'F', 'g', 'e', 'c', 'U', 'r':
			nd = append(nd, c)
			d = append(d, c)
		case 'h', 'H', 'k', 'K':
			t = append(t, c)
			if hour == 0 {
				hour = c
			}
		case 'm':
			t = append(t, c)
			minC++
		case 'z':
			zc++
			t = append(t, c)
		case 'v':
			vc++
			t = append(t, c)
		case 'a', 'V', 'Z', 'j', 's', 'S', 'A', 'b', 'B':
			t = append(t, c)
			nt = append(nt, c)
		}
	}
	for range yc {
		nd = append(nd, 'y')
	}
	if mc > 0 {
		if mc < 3 {
			nd = append(nd, 'M')
		} else {
			for range min(mc, 5) {
				nd = append(nd, 'M')
			}
		}
	}
	if e > 0 {
		if e <= 3 {
			nd = append(nd, 'E')
		} else {
			for range min(e, 5) {
				nd = append(nd, 'E')
			}
		}
	}
	if dc > 0 {
		nd = append(nd, 'd')
	}
	if hour != 0 {
		nt = append(nt, hour)
	}
	if minC > 0 {
		nt = append(nt, 'm')
	}
	if zc > 0 {
		nt = append(nt, 'z')
	}
	if vc > 0 {
		nt = append(nt, 'v')
	}
	return string(d), string(nd), string(t), string(nt)
}

// setSeparate is setSeparateDateTimePtn.
func (g *intlDateRange) setSeparate(date, time string) bool {
	skeleton := date
	if time != "" {
		skeleton = time
	}
	best, diff := g.bestSkeleton(skeleton)
	if best == "" {
		return false
	}
	if date != "" {
		g.datePattern = g.best(date)
		g.dateItems = intlCompilePattern(g.datePattern)
	}
	if time != "" {
		g.timePattern = g.best(time)
		g.timeItems = intlCompilePattern(g.timePattern)
	}
	if diff == -1 {
		return false
	}
	if time == "" {
		// As in ICU, skeleton and best point at ext and extBest once the
		// month extended them, so the year's extension carries on to the
		// era's.
		var ext, extBest string
		skel, bestp := &skeleton, &best
		g.setItv(itvDate, skel, bestp, diff, &ext, &extBest)
		if g.setItv(itvMonth, skel, bestp, diff, &ext, &extBest) {
			skel, bestp = &ext, &extBest
		}
		g.setItv(itvYear, skel, bestp, diff, &ext, &extBest)
		g.setItv(itvEra, skel, bestp, diff, &ext, &extBest)
	} else {
		g.setItv(itvMinute, &skeleton, &best, diff, nil, nil)
		g.setItv(itvHour, &skeleton, &best, diff, nil, nil)
		g.setItv(itvAmPm, &skeleton, &best, diff, nil, nil)
	}
	return true
}

// setItv is the setIntervalPattern of a field from DateIntervalInfo,
// extending the skeleton by the field's letter when the best one lacks a
// pattern for it; it reports whether ext is set. skeleton and best may
// point at ext and extBest, as in ICU.
func (g *intlDateRange) setItv(field int, skeleton, best *string, diff int, ext, extBest *string) bool {
	pattern := g.intervalPattern(*best, field)
	if pattern == "" {
		if intlFieldUnitIgnored(*best, field) {
			return false
		}
		if field == itvAmPm {
			if pattern = g.intervalPattern(*best, itvHour); pattern != "" {
				g.setPattern(field, intlAdjustFieldWidth(*skeleton, *best, pattern, diff, strings.IndexByte(g.skeleton, 'J') >= 0), g.defaultLater)
			}
			return false
		}
		if ext != nil {
			letter := string(itvLetters[field])
			s, b := *skeleton, *best
			*ext, *extBest = letter+s, letter+b
			pattern = g.intervalPattern(*extBest, field)
			if pattern == "" && diff == 0 {
				if tmp, d := g.bestSkeleton(*extBest); tmp != "" && d != -1 {
					diff = d
					pattern = g.intervalPattern(tmp, field)
					best = &tmp
				}
			}
		}
	}
	if pattern == "" {
		return false
	}
	suppress := strings.IndexByte(g.skeleton, 'J') >= 0
	if diff != 0 || suppress {
		pattern = intlAdjustFieldWidth(*skeleton, *best, pattern, diff, suppress)
	}
	g.setPattern(field, pattern, g.defaultLater)
	return ext != nil && *ext != ""
}

// setPattern is setIntervalPattern(field, pattern, laterDateFirst): the
// "latestFirst:" and "earliestFirst:" prefixes, then the split.
func (g *intlDateRange) setPattern(field int, pattern string, later bool) {
	if p, ok := strings.CutPrefix(pattern, "latestFirst:"); ok {
		pattern, later = p, true
	} else if p, ok := strings.CutPrefix(pattern, "earliestFirst:"); ok {
		pattern, later = p, false
	}
	split := intlSplitInterval(pattern)
	first, second := pattern[:split], pattern[split:]
	g.setInfo(field, &first, &second, later)
}

func (g *intlDateRange) setInfo(field int, first, second *string, later bool) {
	p := &g.patterns[field]
	if first != nil {
		p.first = *first
		p.firstItems = intlCompilePattern(p.first)
	}
	if second != nil {
		p.second = *second
		p.secondItems = intlCompilePattern(p.second)
	}
	p.laterFirst = later
}

// intlSplitInterval is splitPatternInto2Part: the offset of the first
// pattern letter seen again, which starts the second date's part.
func intlSplitInterval(p string) int {
	var repeated [58]bool
	inQuote := false
	var prev byte
	count, i := 0, 0
	found := false
	for i = 0; i < len(p); i++ {
		c := p[i]
		if c != prev && count > 0 {
			if repeated[prev-'A'] {
				found = true
				break
			}
			repeated[prev-'A'] = true
			count = 0
		}
		switch {
		case c == '\'':
			if i+1 < len(p) && p[i+1] == '\'' {
				i++
			} else {
				inQuote = !inQuote
			}
		case !inQuote && dtIsLetter(c):
			prev = c
			count++
		}
	}
	if count > 0 && !found && !repeated[prev-'A'] {
		count = 0
	}
	return i - count
}

// intlAdjustFieldWidth is adjustFieldWidth: an interval pattern of the best
// skeleton with the field widths of the skeleton asked for.
func intlAdjustFieldWidth(input, best, pattern string, diff int, suppressDayPeriod bool) string {
	var inW, bestW [58]int
	intlSkeletonWidths(input, &inW)
	intlSkeletonWidths(best, &bestW)
	p := pattern
	if suppressDayPeriod {
		for _, s := range []string{"\u00a0a", "\u202fa", "a\u00a0", "a\u202f", "a"} {
			p = intlReplaceUnquoted(p, s, "")
		}
		p = strings.TrimSpace(intlReplaceUnquoted(p, "  ", " "))
	}
	if diff == 2 {
		if strings.IndexByte(input, 'z') >= 0 {
			p = intlReplaceUnquoted(p, "v", "z")
		}
		if strings.IndexByte(input, 'K') >= 0 {
			p = intlReplaceUnquoted(p, "h", "K")
		}
		if strings.IndexByte(input, 'k') >= 0 {
			p = intlReplaceUnquoted(p, "H", "k")
		}
		if strings.IndexByte(input, 'b') >= 0 {
			p = intlReplaceUnquoted(p, "a", "b")
		}
	}
	if strings.IndexByte(p, 'a') >= 0 && bestW['a'-'A'] == 0 {
		bestW['a'-'A'] = 1
	}
	if strings.IndexByte(p, 'b') >= 0 && bestW['b'-'A'] == 0 {
		bestW['b'-'A'] = 1
	}
	b := []byte(p)
	inQuote := false
	var prev byte
	count := 0
	extend := func(at int) int {
		sc := prev
		if sc == 'L' {
			sc = 'M'
		}
		n, in := bestW[sc-'A'], inW[sc-'A']
		if n == count && in > n {
			add := in - n
			ins := make([]byte, add)
			for j := range ins {
				ins[j] = prev
			}
			b = append(b[:at], append(ins, b[at:]...)...)
			return add
		}
		return 0
	}
	for i := 0; i < len(b); i++ {
		c := b[i]
		if c != prev && count > 0 {
			i += extend(i)
			c = b[i]
			count = 0
		}
		switch {
		case c == '\'':
			if i+1 < len(b) && b[i+1] == '\'' {
				i++
			} else {
				inQuote = !inQuote
			}
		case !inQuote && dtIsLetter(c):
			prev = c
			count++
		}
	}
	if count > 0 {
		extend(len(b))
	}
	return string(b)
}

// intlReplaceUnquoted is findReplaceInPattern: s replaced outside quotes.
func intlReplaceUnquoted(p, old, repl string) string {
	q := strings.IndexByte(p, '\'')
	if q < 0 {
		return strings.ReplaceAll(p, old, repl)
	}
	var b strings.Builder
	src := p
	for q >= 0 {
		q2 := strings.IndexByte(src[q+1:], '\'')
		if q2 < 0 {
			q2 = len(src) - 1
		} else {
			q2 += q + 1
		}
		b.WriteString(strings.ReplaceAll(src[:q], old, repl))
		b.WriteString(src[q : q2+1])
		src = src[q2+1:]
		q = strings.IndexByte(src, '\'')
	}
	b.WriteString(strings.ReplaceAll(src, old, repl))
	return b.String()
}

// concatDateTime is concatSingleDate2TimeInterval: the time interval
// pattern of a field glued with the date pattern.
func (g *intlDateRange) concatDateTime(datePattern string, field int) {
	p := &g.patterns[field]
	if p.first == "" {
		return
	}
	combined := intlSimpleFormat(g.dateTime, p.first+p.second, datePattern)
	g.setPattern(field, combined, p.laterFirst)
}

// intlRangeOut is the output of a range: the bytes, the field spans and
// the spans of the two dates (firstIndex is -1 for a single date).
type intlRangeOut struct {
	b          []byte
	spans      []intlDateSpan
	firstIndex int
}

// format is DateIntervalFormat::formatImpl.
func (g *intlDateRange) format(f *intlDateTimeFormat, x, y float64, out *intlRangeOut) {
	out.firstIndex = -1
	_, offX, _ := f.zone.at(x)
	_, offY, _ := f.zone.at(y)
	a := splitTime(int64(x) + int64(offX)*1000)
	b := splitTime(int64(y) + int64(offY)*1000)
	field := itvCount
	eraOf := func(t *dateFields) int64 {
		if t.year <= 0 {
			return 0
		}
		return 1
	}
	yearOf := func(t *dateFields) int64 {
		if t.year <= 0 {
			return 1 - t.year
		}
		return t.year
	}
	switch {
	case eraOf(&a) != eraOf(&b):
		field = itvEra
	case yearOf(&a) != yearOf(&b):
		field = itvYear
	case a.month != b.month:
		field = itvMonth
	case a.day != b.day:
		field = itvDate
	case a.hour/12 != b.hour/12:
		field = itvAmPm
	case a.hour%12 != b.hour%12:
		field = itvHour
	case a.minute != b.minute:
		field = itvMinute
	case a.second != b.second:
		field = itvSecond
	case a.ms != b.ms:
		field = itvMillisecond
	}
	if field == itvCount {
		out.b = f.appendFormat(out.b, g.items, x, &out.spans)
		return
	}
	sameDay := field >= itvAmPm
	p := &g.patterns[field]
	if p.first == "" && p.second == "" {
		if intlFieldUnitIgnored(g.pattern, field) {
			out.b = f.appendFormat(out.b, g.items, x, &out.spans)
			return
		}
		g.fallbackFormat(f, x, y, sameDay, g.items, out)
		return
	}
	if p.first == "" {
		g.fallbackFormat(f, x, y, sameDay, p.secondItems, out)
		return
	}
	first, second := x, y
	out.firstIndex = 0
	if p.laterFirst {
		first, second = y, x
		out.firstIndex = 1
	}
	out.b = f.appendFormat(out.b, p.firstItems, first, &out.spans)
	if p.second != "" {
		out.b = f.appendFormat(out.b, p.secondItems, second, &out.spans)
	}
}

// fallbackFormat formats the two dates with the fallback pattern, the
// time range after the date when they are on the same day.
func (g *intlDateRange) fallbackFormat(f *intlDateTimeFormat, x, y float64, sameDay bool, items []intlDateItem, out *intlRangeOut) {
	if sameDay && g.datePattern != "" && g.timePattern != "" {
		timeItems, dateItems := g.timeItems, g.dateItems
		body, o0, o1 := intlSimpleBody(g.dateTime)
		if o0 < o1 {
			out.b = append(out.b, body[:o0]...)
			g.fallbackRange(f, x, y, timeItems, out)
			out.b = append(out.b, body[o0:o1]...)
			out.b = f.appendFormat(out.b, dateItems, x, &out.spans)
			out.b = append(out.b, body[o1:]...)
		} else {
			out.b = append(out.b, body[:o1]...)
			out.b = f.appendFormat(out.b, dateItems, x, &out.spans)
			out.b = append(out.b, body[o1:o0]...)
			g.fallbackRange(f, x, y, timeItems, out)
			out.b = append(out.b, body[o0:]...)
		}
		return
	}
	g.fallbackRange(f, x, y, items, out)
}

// fallbackRange is fallbackFormatRange: "{0} – {1}" with the two dates.
func (g *intlDateRange) fallbackRange(f *intlDateTimeFormat, x, y float64, items []intlDateItem, out *intlRangeOut) {
	body, o0, o1 := intlSimpleBody(g.fallback)
	if o0 < o1 {
		out.firstIndex = 0
		out.b = append(out.b, body[:o0]...)
		out.b = f.appendFormat(out.b, items, x, &out.spans)
		out.b = append(out.b, body[o0:o1]...)
		out.b = f.appendFormat(out.b, items, y, &out.spans)
		out.b = append(out.b, body[o1:]...)
		return
	}
	out.firstIndex = 1
	out.b = append(out.b, body[:o1]...)
	out.b = f.appendFormat(out.b, items, y, &out.spans)
	out.b = append(out.b, body[o1:o0]...)
	out.b = f.appendFormat(out.b, items, x, &out.spans)
	out.b = append(out.b, body[o0:]...)
}

// intlSimpleBody is SimpleFormatter::getTextWithNoArguments for a pattern
// with {0} and {1}: the text without them and their offsets in it.
func intlSimpleBody(pattern string) (string, int, int) {
	const m0, m1 = "\x00", "\x01"
	s := intlSimpleFormat(pattern, m0, m1)
	o0, o1 := strings.Index(s, m0), strings.Index(s, m1)
	body := strings.Replace(strings.Replace(s, m0, "", 1), m1, "", 1)
	if o1 > o0 {
		o1--
	} else {
		o0--
	}
	return body, o0, o1
}

// overlapSpans is addOverlapSpans: the spans of the two dates, from the
// fields that occur twice; ok is false when none does.
func (o *intlRangeOut) overlapSpans() (s [2][2]int, ok bool) {
	s1a, s1b, s2a, s2b := int(^uint(0)>>1), 0, int(^uint(0)>>1), 0
	for i, a := range o.spans {
		for _, b := range o.spans[i+1:] {
			if a.ch != b.ch {
				continue
			}
			s1a, s1b = min(s1a, a.start), max(s1b, a.end)
			s2a, s2b = min(s2a, b.start), max(s2b, b.end)
			break
		}
	}
	if s1a == int(^uint(0)>>1) {
		return s, false
	}
	first := o.firstIndex
	s[first] = [2]int{s1a, s1b}
	s[1-first] = [2]int{s2a, s2b}
	return s, true
}

// intlRangeArgs implements the steps of formatRange and formatRangeToParts
// before the formatting: both dates given, ToNumber, TimeClip.
func (r *Realm) intlRangeArgs(args []Value) (float64, float64, error) {
	a, b := intlArg(args, 0), intlArg(args, 1)
	if a.IsUndefined() || b.IsUndefined() {
		return 0, 0, r.TypeError("startDate and endDate are required")
	}
	x, err := r.ToNumber(a)
	if err != nil {
		return 0, 0, err
	}
	y, err := r.ToNumber(b)
	if err != nil {
		return 0, 0, err
	}
	x, y = timeClip(x), timeClip(y)
	if x != x || y != y {
		return 0, 0, r.RangeError("Invalid time value")
	}
	return x, y, nil
}

// intlDateTimeFormatFormatRange implements
// Intl.DateTimeFormat.prototype.formatRange(startDate, endDate).
func intlDateTimeFormatFormatRange(r *Realm, this Value, args []Value) (Value, error) {
	f, err := thisDateTimeFormat(r, this, "Intl.DateTimeFormat.prototype.formatRange")
	if err != nil {
		return Undefined(), err
	}
	x, y, err := r.intlRangeArgs(args)
	if err != nil {
		return Undefined(), err
	}
	var buf [192]byte
	var spanBuf [32]intlDateSpan
	out := intlRangeOut{b: buf[:0], spans: spanBuf[:0]}
	f.dateRange(r).format(f, x, y, &out)
	if _, ok := out.overlapSpans(); out.firstIndex < 0 || !ok {
		return StringValue(r.intlCharged(f.format(x))), nil
	}
	return StringValue(r.intlCharged(intlUTF8String(out.b))), nil
}

// intlDateTimeFormatFormatRangeToParts implements
// Intl.DateTimeFormat.prototype.formatRangeToParts(startDate, endDate).
func intlDateTimeFormatFormatRangeToParts(r *Realm, this Value, args []Value) (Value, error) {
	f, err := thisDateTimeFormat(r, this, "Intl.DateTimeFormat.prototype.formatRangeToParts")
	if err != nil {
		return Undefined(), err
	}
	x, y, err := r.intlRangeArgs(args)
	if err != nil {
		return Undefined(), err
	}
	a := intlDateNames()
	var buf [192]byte
	var spanBuf [32]intlDateSpan
	out := intlRangeOut{b: buf[:0], spans: spanBuf[:0]}
	f.dateRange(r).format(f, x, y, &out)
	ranges, ok := out.overlapSpans()
	if out.firstIndex < 0 || !ok {
		spans := spanBuf[:0]
		b := f.appendFormat(buf[:0], f.items, x, &spans)
		return ObjectValue(r.intlDateParts(b, spans, func(int, int) *String { return a.shared })), nil
	}
	// V8's FormatRangeSourceTracker: a part is the start range's when the
	// first date's span holds it, else the end range's when the second's
	// does.
	return ObjectValue(r.intlDateParts(out.b, out.spans, func(start, end int) *String {
		switch {
		case ranges[0][0] <= start && end <= ranges[0][1]:
			return a.startRange
		case ranges[1][0] <= start && end <= ranges[1][1]:
			return a.endRange
		}
		return a.shared
	})), nil
}
