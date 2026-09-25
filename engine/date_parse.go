package engine

import "math"

// Date.parse (ES2024 §21.4.3.2) accepts the Date Time String Format and,
// as the spec allows, implementation-specific formats. moejs accepts exactly
// what V8 accepts: this file is a port of V8's DateParser (src/date/
// dateparser*.{h,cc}), so every string round-trips as it does in Node and
// Chrome, including the outputs of toString, toUTCString and toISOString,
// RFC 2822 dates, "2024/01/02", "1/2/2024 10:00 PM" and month names.
//
// The grammar, over a stream of tokens:
//
//   - Tokens: an unsigned decimal number (leading zeros skipped, at most nine
//     significant digits kept), one of the symbols : - + . ), a word (a run
//     of code units >= 'A' that are not white space; only its first three
//     letters matter, case-insensitively), a single white-space or line
//     terminator unit, a parenthesized comment (nested parentheses balance;
//     it is ignored) and any other unit (ignored). A NUL unit ends the input.
//   - Keywords: month names (jan..dec; longer words such as "January" or
//     "Janxyz" also match), am and pm, the zone names ut, utc, gmt and z
//     (+00:00) and est edt cst cdt mst mdt pst pdt, and t (the ISO time
//     separator). Other words are garbage.
//   - First the ISO form is tried: [+-YYYYYY | YYYY] [-MM [-DD]]
//     [THH:mm [:ss [.s+]] [Z | +-HH:mm | +-HHmm]], where HH may be 24 only
//     with zero minutes, seconds and milliseconds. A complete ISO string
//     without an offset is UTC when it has no time and local time otherwise.
//     A prefix that stops before the T (for example "2024-01-02 10:00" or
//     "2024/01/02") hands the numbers read so far to the legacy grammar.
//   - Legacy grammar: a number followed by ':' is an hour or minute (a
//     trailing "::" also sets zero minutes), a number followed by '.' after
//     a minute is seconds with milliseconds after it, the number after a
//     minute or second completes the time and must be followed by the end,
//     white space, 'z' or a sign. am/pm apply to a 0-12 hour. A sign after a
//     time or after a UTC zone name starts an offset: +-H, +-HH, +-HMM,
//     +-HHMM or +-H[H]:MM. Other numbers are date components, each optionally
//     followed by '-'. Garbage words may only precede the first number, and
//     the first number must be separated from a garbage word.
//   - Date components: with a month name, the first number is the day when it
//     is 1..31 (then the next is the year) and the year otherwise; without
//     one, three numbers whose first is not 1..31 are Y/M/D, otherwise M/D/Y.
//     Missing month and day are 1 and the missing year is 2001 (V8 keeps
//     KJS's default of year 1). Years 0-49 add 2000 and 50-99 add 1900.
//   - Without an offset or zone name the result is local time; the hour may
//     be 24 only with zero minutes, seconds and milliseconds.
//
// Out-of-range components make the result NaN, as does anything the grammar
// does not accept. Time values outside the TimeClip range are NaN.

// dateParseNone is V8's kNone, the "not set" marker of the composers.
const dateParseNone = math.MaxInt32

// dateSmiMax bounds a parsed UTC offset in seconds (Smi::kMaxValue of the
// 64-bit V8 in Node, which has 32-bit Smis).
const dateSmiMax = 1<<31 - 1

// Token tags. Word tokens use the keyword types (>= 0), as in V8.
const (
	dtInvalid    = -6
	dtUnknown    = -5
	dtWhiteSpace = -4
	dtNumber     = -3
	dtSymbol     = -2
	dtEnd        = -1
)

// Keyword types.
const (
	kwInvalid = iota
	kwMonthName
	kwTimeZoneName
	kwTimeSeparator
	kwAMPM
)

type dateToken struct {
	tag    int
	length int
	value  int
}

func (t dateToken) isNumber() bool           { return t.tag == dtNumber }
func (t dateToken) isSymbol(c byte) bool     { return t.tag == dtSymbol && t.value == int(c) }
func (t dateToken) isEnd() bool              { return t.tag == dtEnd }
func (t dateToken) isWhiteSpace() bool       { return t.tag == dtWhiteSpace }
func (t dateToken) isKeyword() bool          { return t.tag >= 0 }
func (t dateToken) isInvalid() bool          { return t.tag == dtInvalid }
func (t dateToken) isFixedNumber(n int) bool { return t.tag == dtNumber && t.length == n }
func (t dateToken) isSign() bool {
	return t.tag == dtSymbol && (t.value == '-' || t.value == '+')
}
func (t dateToken) sign() int { return 44 - t.value } // '+' -> 1, '-' -> -1
func (t dateToken) isKeywordZ() bool {
	return t.tag == kwTimeZoneName && t.length == 1 && t.value == 0
}

// dateKeywords is V8's KeywordTable: a three-letter lower-case prefix, the
// keyword type and its value.
var dateKeywords = [...]struct {
	prefix [3]byte
	kind   int
	value  int
}{
	{[3]byte{'j', 'a', 'n'}, kwMonthName, 1},
	{[3]byte{'f', 'e', 'b'}, kwMonthName, 2},
	{[3]byte{'m', 'a', 'r'}, kwMonthName, 3},
	{[3]byte{'a', 'p', 'r'}, kwMonthName, 4},
	{[3]byte{'m', 'a', 'y'}, kwMonthName, 5},
	{[3]byte{'j', 'u', 'n'}, kwMonthName, 6},
	{[3]byte{'j', 'u', 'l'}, kwMonthName, 7},
	{[3]byte{'a', 'u', 'g'}, kwMonthName, 8},
	{[3]byte{'s', 'e', 'p'}, kwMonthName, 9},
	{[3]byte{'o', 'c', 't'}, kwMonthName, 10},
	{[3]byte{'n', 'o', 'v'}, kwMonthName, 11},
	{[3]byte{'d', 'e', 'c'}, kwMonthName, 12},
	{[3]byte{'a', 'm', 0}, kwAMPM, 0},
	{[3]byte{'p', 'm', 0}, kwAMPM, 12},
	{[3]byte{'u', 't', 0}, kwTimeZoneName, 0},
	{[3]byte{'u', 't', 'c'}, kwTimeZoneName, 0},
	{[3]byte{'z', 0, 0}, kwTimeZoneName, 0},
	{[3]byte{'g', 'm', 't'}, kwTimeZoneName, 0},
	{[3]byte{'c', 'd', 't'}, kwTimeZoneName, -5},
	{[3]byte{'c', 's', 't'}, kwTimeZoneName, -6},
	{[3]byte{'e', 'd', 't'}, kwTimeZoneName, -4},
	{[3]byte{'e', 's', 't'}, kwTimeZoneName, -5},
	{[3]byte{'m', 'd', 't'}, kwTimeZoneName, -6},
	{[3]byte{'m', 's', 't'}, kwTimeZoneName, -7},
	{[3]byte{'p', 'd', 't'}, kwTimeZoneName, -7},
	{[3]byte{'p', 's', 't'}, kwTimeZoneName, -8},
	{[3]byte{'t', 0, 0}, kwTimeSeparator, 0},
}

func lookupDateKeyword(prefix [3]uint32, length int) (kind, value int) {
	for _, k := range dateKeywords {
		if prefix[0] == uint32(k.prefix[0]) && prefix[1] == uint32(k.prefix[1]) && prefix[2] == uint32(k.prefix[2]) &&
			(length <= 3 || k.kind == kwMonthName) {
			return k.kind, k.value
		}
	}
	return kwInvalid, 0
}

// dateInput is V8's InputReader over the code units of the string.
type dateInput struct {
	a   string   // ASCII source, or
	u   []uint16 // UTF-16 source
	n   int
	pos int // index of the unit after ch
	ch  uint32
}

func (in *dateInput) next() {
	if in.pos < in.n {
		if in.u != nil {
			in.ch = uint32(in.u[in.pos])
		} else {
			in.ch = uint32(in.a[in.pos])
		}
	} else {
		in.ch = 0
	}
	in.pos++
}

func (in *dateInput) skip(c uint32) bool {
	if in.ch == c {
		in.next()
		return true
	}
	return false
}

func (in *dateInput) isDigit() bool { return in.ch >= '0' && in.ch <= '9' }

// isWord reports whether ch continues a word: a unit >= 'A' that is not
// ECMAScript WhiteSpace (line terminators above 'A' do continue words, as
// in V8).
func (in *dateInput) isWord() bool {
	c := in.ch
	return c >= 'A' && (c > 0xFFFF || c == 0x2028 || c == 0x2029 || !isJSWhitespace(uint16(c)))
}

// readNumber reads a run of digits, keeping at most nine significant ones.
func (in *dateInput) readNumber() int {
	for in.ch == '0' {
		in.next()
	}
	n, i := 0, 0
	for in.isDigit() {
		if i < 9 {
			n = n*10 + int(in.ch-'0')
		}
		i++
		in.next()
	}
	return n
}

// readWord reads a word, returning its lower-case three-unit prefix
// (zero-padded) and its length.
func (in *dateInput) readWord() (prefix [3]uint32, length int) {
	for ; in.isWord(); in.next() {
		if length < 3 {
			c := in.ch
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			prefix[length] = c
		}
		length++
	}
	return prefix, length
}

func (in *dateInput) skipParentheses() bool {
	if in.ch != '(' {
		return false
	}
	balance := 0
	for {
		switch in.ch {
		case ')':
			balance--
		case '(':
			balance++
		}
		in.next()
		if balance <= 0 || in.ch == 0 {
			return true
		}
	}
}

// dateTokenizer is V8's DateStringTokenizer: a one-token lookahead.
type dateTokenizer struct {
	in   dateInput
	peek dateToken
}

func (s *dateTokenizer) scan() dateToken {
	in := &s.in
	pre := in.pos
	switch {
	case in.ch == 0:
		return dateToken{tag: dtEnd}
	case in.isDigit():
		n := in.readNumber()
		return dateToken{tag: dtNumber, length: in.pos - pre, value: n}
	}
	for _, c := range [...]uint32{':', '-', '+', '.', ')'} {
		if in.skip(c) {
			return dateToken{tag: dtSymbol, length: 1, value: int(c)}
		}
	}
	if in.isWord() {
		prefix, length := in.readWord()
		kind, value := lookupDateKeyword(prefix, length)
		return dateToken{tag: kind, length: length, value: value}
	}
	if in.ch < 0x10000 && isJSWhitespace(uint16(in.ch)) {
		in.next()
		return dateToken{tag: dtWhiteSpace, length: in.pos - pre}
	}
	if in.skipParentheses() {
		return dateToken{tag: dtUnknown, length: 1, value: -1}
	}
	in.next()
	return dateToken{tag: dtUnknown, length: 1, value: -1}
}

func (s *dateTokenizer) next() dateToken {
	t := s.peek
	s.peek = s.scan()
	return t
}

func (s *dateTokenizer) skipSymbol(c byte) bool {
	if s.peek.isSymbol(c) {
		s.next()
		return true
	}
	return false
}

// dayComposer collects date components (V8's DayComposer).
type dayComposer struct {
	comp       [3]int
	index      int
	namedMonth int
	isISODate  bool
}

func (d *dayComposer) add(n int) bool {
	if d.index == len(d.comp) {
		return false
	}
	d.comp[d.index] = n
	d.index++
	return true
}

func isDateMonth(x int) bool { return x >= 1 && x <= 12 }
func isDateDay(x int) bool   { return x >= 1 && x <= 31 }

func (d *dayComposer) write(out *dateParsed) bool {
	if d.index < 1 {
		return false
	}
	for d.index < len(d.comp) {
		d.comp[d.index] = 1
		d.index++
	}
	year, month, day := 0, dateParseNone, dateParseNone
	if d.namedMonth == dateParseNone {
		if d.isISODate || !isDateDay(d.comp[0]) {
			year, month, day = d.comp[0], d.comp[1], d.comp[2]
		} else {
			month, day, year = d.comp[0], d.comp[1], d.comp[2]
		}
	} else {
		month = d.namedMonth
		if !isDateDay(d.comp[0]) {
			year, day = d.comp[0], d.comp[1]
		} else {
			day, year = d.comp[0], d.comp[1]
		}
	}
	if !d.isISODate {
		switch {
		case year >= 0 && year <= 49:
			year += 2000
		case year >= 50 && year <= 99:
			year += 1900
		}
	}
	if !isDateMonth(month) || !isDateDay(day) {
		return false
	}
	out.year, out.month, out.day = year, month-1, day
	return true
}

// timeComposer collects hour, minute, second and millisecond.
type timeComposer struct {
	comp       [4]int
	index      int
	hourOffset int
}

func isDateHour(x int) bool   { return x >= 0 && x <= 23 }
func isDateMinute(x int) bool { return x >= 0 && x <= 59 }

func (t *timeComposer) isEmpty() bool { return t.index == 0 }

func (t *timeComposer) isExpecting(n int) bool {
	return (t.index == 1 && isDateMinute(n)) || (t.index == 2 && isDateMinute(n)) || (t.index == 3 && n >= 0 && n <= 999)
}

func (t *timeComposer) add(n int) bool {
	if t.index == len(t.comp) {
		return false
	}
	t.comp[t.index] = n
	t.index++
	return true
}

func (t *timeComposer) addFinal(n int) bool {
	if !t.add(n) {
		return false
	}
	for t.index < len(t.comp) {
		t.comp[t.index] = 0
		t.index++
	}
	return true
}

func (t *timeComposer) write(out *dateParsed) bool {
	for t.index < len(t.comp) {
		t.comp[t.index] = 0
		t.index++
	}
	hour, minute, second, ms := t.comp[0], t.comp[1], t.comp[2], t.comp[3]
	if t.hourOffset != dateParseNone {
		if hour < 0 || hour > 12 {
			return false
		}
		hour = hour%12 + t.hourOffset
	}
	if !isDateHour(hour) || !isDateMinute(minute) || !isDateMinute(second) || ms < 0 || ms > 999 {
		if hour != 24 || minute != 0 || second != 0 || ms != 0 {
			return false
		}
	}
	out.hour, out.minute, out.second, out.ms = hour, minute, second, ms
	return true
}

// zoneComposer collects an explicit UTC offset.
type zoneComposer struct {
	sign, hour, minute int
}

func (z *zoneComposer) set(offsetHours int) {
	z.sign = 1
	if offsetHours < 0 {
		z.sign = -1
	}
	z.hour = offsetHours * z.sign
	z.minute = 0
}

func (z *zoneComposer) setSign(sign int) {
	if sign < 0 {
		z.sign = -1
	} else {
		z.sign = 1
	}
}

func (z *zoneComposer) isExpecting(n int) bool {
	return z.hour != dateParseNone && z.minute == dateParseNone && isDateMinute(n)
}

func (z *zoneComposer) isUTC() bool   { return z.hour == 0 && z.minute == 0 }
func (z *zoneComposer) isEmpty() bool { return z.hour == dateParseNone }

func (z *zoneComposer) write(out *dateParsed) bool {
	if z.sign == dateParseNone {
		out.local = true
		return true
	}
	if z.hour == dateParseNone {
		z.hour = 0
	}
	if z.minute == dateParseNone {
		z.minute = 0
	}
	// V8 computes the offset in unsigned 32-bit arithmetic, so an absurd
	// "+999999:00" wraps before the range check.
	total := uint32(z.hour)*3600 + uint32(z.minute)*60
	if total > dateSmiMax {
		return false
	}
	out.offset = int64(total)
	if z.sign < 0 {
		out.offset = -out.offset
	}
	return true
}

// dateParsed is the parser's output.
type dateParsed struct {
	year, month, day         int
	hour, minute, second, ms int
	offset                   int64 // seconds east of UTC, unless local
	local                    bool
}

// readMilliseconds keeps the first three significant digits of a fraction
// token, inferring leading zeros from its length.
func readMilliseconds(t dateToken) int {
	n, length := t.value, t.length
	switch {
	case length == 1:
		n *= 100
	case length == 2:
		n *= 10
	case length > 3:
		if length > 9 {
			length = 9
		}
		factor := 1
		for ; length > 3; length-- {
			factor *= 10
		}
		n /= factor
	}
	return n
}

// parseISO is V8's ParseES5DateTime. It returns the first token it did not
// consume (end of input on success, an invalid token on a malformed time
// part).
func parseISO(s *dateTokenizer, day *dayComposer, tm *timeComposer, tz *zoneComposer) dateToken {
	if s.peek.isSign() {
		signTok := s.next()
		if !s.peek.isFixedNumber(6) {
			return signTok
		}
		sign := signTok.sign()
		year := s.next().value
		if sign < 0 && year == 0 {
			return signTok
		}
		day.add(sign * year)
	} else if s.peek.isFixedNumber(4) {
		day.add(s.next().value)
	} else {
		return s.next()
	}
	if s.skipSymbol('-') {
		if !s.peek.isFixedNumber(2) || !isDateMonth(s.peek.value) {
			return s.next()
		}
		day.add(s.next().value)
		if s.skipSymbol('-') {
			if !s.peek.isFixedNumber(2) || !isDateDay(s.peek.value) {
				return s.next()
			}
			day.add(s.next().value)
		}
	}
	invalid := dateToken{tag: dtInvalid}
	if s.peek.tag != kwTimeSeparator {
		if !s.peek.isEnd() {
			return s.next()
		}
	} else {
		s.next()
		if !s.peek.isFixedNumber(2) || s.peek.value > 24 {
			return invalid
		}
		hourIs24 := s.peek.value == 24
		tm.add(s.next().value)
		if !s.skipSymbol(':') {
			return invalid
		}
		if !s.peek.isFixedNumber(2) || !isDateMinute(s.peek.value) || (hourIs24 && s.peek.value > 0) {
			return invalid
		}
		tm.add(s.next().value)
		if s.skipSymbol(':') {
			if !s.peek.isFixedNumber(2) || !isDateMinute(s.peek.value) || (hourIs24 && s.peek.value > 0) {
				return invalid
			}
			tm.add(s.next().value)
			if s.skipSymbol('.') {
				if !s.peek.isNumber() || (hourIs24 && s.peek.value > 0) {
					return invalid
				}
				tm.add(readMilliseconds(s.next()))
			}
		}
		if s.peek.isKeywordZ() {
			s.next()
			tz.set(0)
		} else if s.peek.isSign() {
			tz.setSign(s.next().sign())
			if s.peek.isFixedNumber(4) {
				hm := s.next().value
				hour, minute := hm/100, hm%100
				if !isDateHour(hour) || !isDateMinute(minute) {
					return invalid
				}
				tz.hour, tz.minute = hour, minute
			} else {
				if !s.peek.isFixedNumber(2) || !isDateHour(s.peek.value) {
					return invalid
				}
				tz.hour = s.next().value
				if !s.skipSymbol(':') {
					return invalid
				}
				if !s.peek.isFixedNumber(2) || !isDateMinute(s.peek.value) {
					return invalid
				}
				tz.minute = s.next().value
			}
		}
		if !s.peek.isEnd() {
			return invalid
		}
	}
	// A complete date-only form is UTC; a date-time form without an offset
	// is local time.
	if tz.isEmpty() && tm.isEmpty() {
		tz.set(0)
	}
	day.isISODate = true
	return dateToken{tag: dtEnd}
}

// parseDate runs the V8 grammar over the code units of str.
func parseDate(str *String) (dateParsed, bool) {
	var out dateParsed
	var s dateTokenizer
	if a, ok := str.ASCII(); ok {
		s.in = dateInput{a: a, n: len(a)}
	} else {
		u := str.UTF16()
		s.in = dateInput{u: u, n: len(u)}
	}
	s.in.next()
	s.peek = s.scan()

	day := dayComposer{namedMonth: dateParseNone}
	tm := timeComposer{hourOffset: dateParseNone}
	tz := zoneComposer{sign: dateParseNone, hour: dateParseNone, minute: dateParseNone}

	tok := parseISO(&s, &day, &tm, &tz)
	if tok.isInvalid() {
		return out, false
	}
	hasReadNumber := day.index != 0
	for ; !tok.isEnd(); tok = s.next() {
		switch {
		case tok.isNumber():
			hasReadNumber = true
			n := tok.value
			switch {
			case s.skipSymbol(':'):
				if s.skipSymbol(':') {
					if !tm.isEmpty() {
						return out, false
					}
					tm.add(n)
					tm.add(0)
				} else {
					if !tm.add(n) {
						return out, false
					}
					if s.peek.isSymbol('.') {
						s.next()
					}
				}
			case s.skipSymbol('.') && tm.isExpecting(n):
				// The '.' is consumed even when the time does not expect n.
				tm.add(n)
				if !s.peek.isNumber() {
					return out, false
				}
				tm.addFinal(readMilliseconds(s.next()))
			case tz.isExpecting(n):
				tz.minute = n
			case tm.isExpecting(n):
				tm.addFinal(n)
				if p := s.peek; !p.isEnd() && !p.isWhiteSpace() && !p.isKeywordZ() && !p.isSign() {
					return out, false
				}
			default:
				if !day.add(n) {
					return out, false
				}
				s.skipSymbol('-')
			}
		case tok.isKeyword():
			switch {
			case tok.tag == kwAMPM && !tm.isEmpty():
				tm.hourOffset = tok.value
			case tok.tag == kwMonthName:
				day.namedMonth = tok.value
				s.skipSymbol('-')
			case tok.tag == kwTimeZoneName && hasReadNumber:
				tz.set(tok.value)
			default:
				if hasReadNumber || s.peek.isNumber() {
					return out, false
				}
			}
		case tok.isSign() && (tz.isUTC() || !tm.isEmpty()):
			tz.setSign(tok.sign())
			n, length := 0, 0
			if s.peek.isNumber() {
				t := s.next()
				n, length = t.value, t.length
			}
			hasReadNumber = true
			switch {
			case s.peek.isSymbol(':'):
				tz.hour, tz.minute = n, dateParseNone
			case length == 1 || length == 2:
				tz.hour, tz.minute = n, 0
			case length == 3 || length == 4:
				tz.hour, tz.minute = n/100, n%100
			default:
				return out, false
			}
		case (tok.isSign() || tok.isSymbol(')')) && hasReadNumber:
			return out, false
		}
	}
	if !day.write(&out) || !tm.write(&out) || !tz.write(&out) {
		return out, false
	}
	return out, true
}

// parseDateString implements the parse step shared by Date.parse and the
// Date constructor, in realm r's local time zone.
func (r *Realm) parseDateString(str *String) float64 {
	p, ok := parseDate(str)
	if !ok {
		return math.NaN()
	}
	date := makeDate(makeDay(float64(p.year), float64(p.month), float64(p.day)),
		makeTime(float64(p.hour), float64(p.minute), float64(p.second), float64(p.ms)))
	if p.local {
		return r.utc(date)
	}
	return timeClip(date - float64(p.offset)*msPerSecond)
}
