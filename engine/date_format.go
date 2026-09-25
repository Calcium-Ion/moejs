package engine

// Date string forms, byte-compatible with V8 except for the time zone name:
//
//	toString            Tue Jan 02 2024 10:05:07 GMT-0500 (EST)
//	toDateString        Tue Jan 02 2024
//	toTimeString        10:05:07 GMT-0500 (EST)
//	toUTCString         Tue, 02 Jan 2024 15:05:07 GMT
//	toISOString         2024-01-02T15:05:07.009Z (+275760-09-13T00:00:00.000Z)
//	toLocaleString      1/2/2024, 10:05:07 AM
//	toLocaleDateString  1/2/2024
//	toLocaleTimeString  10:05:07 AM
//
// Years outside 0..9999 print with as many digits as needed and a leading
// '-' when negative ("-0001", "-271821"), as V8's "%04d"/"%05d" do. The
// parenthesized zone name is the abbreviation from Go's tz database ("EST",
// "+1030", "LMT"); V8 prints ICU's long name ("Eastern Standard Time"),
// which needs CLDR data moejs does not carry. The toLocale* forms are V8's
// en-US output without Intl: the locales and options arguments are ignored
// and the year is the era year (1 - year for years before 1), as ICU's
// Gregorian calendar prints it.

var (
	dateDayNames   = [7]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	dateMonthNames = [12]string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
)

// appendPadded appends v >= 0 in decimal, zero-padded to width.
func appendPadded(b []byte, v int64, width int) []byte {
	var tmp [20]byte
	i := len(tmp)
	for v >= 10 || width > 1 {
		i--
		tmp[i] = byte('0' + v%10)
		v /= 10
		width--
	}
	i--
	tmp[i] = byte('0' + v)
	return append(b, tmp[i:]...)
}

// appendDateYear appends a year as V8's date strings do.
func appendDateYear(b []byte, y int64) []byte {
	if y < 0 {
		return appendPadded(append(b, '-'), -y, 4)
	}
	return appendPadded(b, y, 4)
}

// localDate breaks down the finite time value tv in the realm's time zone,
// returning the fields, the offset in ms and the zone abbreviation.
func (r *Realm) localDate(tv float64) (dateFields, int64, string) {
	t := int64(tv)
	off, name := r.zoneAt(t)
	return splitTime(t + off), off, name
}

func appendDatePart(b []byte, f dateFields) []byte {
	b = append(b, dateDayNames[f.weekday]...)
	b = append(b, ' ')
	b = append(b, dateMonthNames[f.month]...)
	b = append(b, ' ')
	b = appendPadded(b, int64(f.day), 2)
	b = append(b, ' ')
	return appendDateYear(b, f.year)
}

func appendClock(b []byte, f dateFields) []byte {
	b = appendPadded(b, int64(f.hour), 2)
	b = append(b, ':')
	b = appendPadded(b, int64(f.minute), 2)
	b = append(b, ':')
	return appendPadded(b, int64(f.second), 2)
}

func appendTimePart(b []byte, f dateFields, off int64, name string) []byte {
	b = appendClock(b, f)
	b = append(b, " GMT"...)
	// V8 truncates the offset to whole minutes first (LMT offsets have
	// seconds) and prints "+0000" for zero.
	mins := off / msPerMinute
	if mins < 0 {
		b = append(b, '-')
		mins = -mins
	} else {
		b = append(b, '+')
	}
	b = appendPadded(b, mins/60, 2)
	b = appendPadded(b, mins%60, 2)
	b = append(b, " ("...)
	b = append(b, name...)
	return append(b, ')')
}

// dateToString is ToDateString for a finite time value.
func (r *Realm) dateToString(tv float64) *String {
	f, off, name := r.localDate(tv)
	b := make([]byte, 0, 48+len(name))
	b = appendDatePart(b, f)
	b = append(b, ' ')
	b = appendTimePart(b, f, off, name)
	return FromGoString(string(b))
}

func (r *Realm) dateToDateString(tv float64) *String {
	f, _, _ := r.localDate(tv)
	return asciiString(string(appendDatePart(make([]byte, 0, 20), f)))
}

func (r *Realm) dateToTimeString(tv float64) *String {
	f, off, name := r.localDate(tv)
	return FromGoString(string(appendTimePart(make([]byte, 0, 24+len(name)), f, off, name)))
}

// dateToUTCString is the toUTCString form of a finite time value.
func dateToUTCString(tv float64) *String {
	f := splitTime(int64(tv))
	b := make([]byte, 0, 32)
	b = append(b, dateDayNames[f.weekday]...)
	b = append(b, ", "...)
	b = appendPadded(b, int64(f.day), 2)
	b = append(b, ' ')
	b = append(b, dateMonthNames[f.month]...)
	b = append(b, ' ')
	b = appendDateYear(b, f.year)
	b = append(b, ' ')
	b = appendClock(b, f)
	b = append(b, " GMT"...)
	return asciiString(string(b))
}

// formatISO renders a finite time value as YYYY-MM-DDTHH:mm:ss.sssZ, with the
// expanded ±YYYYYY year outside 0..9999.
func formatISO(tv float64) string {
	f := splitTime(int64(tv))
	b := make([]byte, 0, 27)
	switch {
	case f.year >= 0 && f.year <= 9999:
		b = appendPadded(b, f.year, 4)
	case f.year < 0:
		b = appendPadded(append(b, '-'), -f.year, 6)
	default:
		b = appendPadded(append(b, '+'), f.year, 6)
	}
	b = append(b, '-')
	b = appendPadded(b, int64(f.month)+1, 2)
	b = append(b, '-')
	b = appendPadded(b, int64(f.day), 2)
	b = append(b, 'T')
	b = appendClock(b, f)
	b = append(b, '.')
	b = appendPadded(b, int64(f.ms), 3)
	b = append(b, 'Z')
	return string(b)
}

func appendLocaleDate(b []byte, f dateFields) []byte {
	b = appendPadded(b, int64(f.month)+1, 1)
	b = append(b, '/')
	b = appendPadded(b, int64(f.day), 1)
	b = append(b, '/')
	y := f.year
	if y <= 0 {
		y = 1 - y
	}
	return appendPadded(b, y, 1)
}

func appendLocaleTime(b []byte, f dateFields) []byte {
	h := f.hour % 12
	if h == 0 {
		h = 12
	}
	b = appendPadded(b, int64(h), 1)
	b = append(b, ':')
	b = appendPadded(b, int64(f.minute), 2)
	b = append(b, ':')
	b = appendPadded(b, int64(f.second), 2)
	if f.hour < 12 {
		return append(b, " AM"...)
	}
	return append(b, " PM"...)
}

// dateToLocaleString formats a finite time value in the en-US forms; date
// and clock select the parts.
func (r *Realm) dateToLocaleString(tv float64, date, clock bool) *String {
	f, _, _ := r.localDate(tv)
	b := make([]byte, 0, 24)
	if date {
		b = appendLocaleDate(b, f)
	}
	if date && clock {
		b = append(b, ", "...)
	}
	if clock {
		b = appendLocaleTime(b, f)
	}
	return asciiString(string(b))
}
