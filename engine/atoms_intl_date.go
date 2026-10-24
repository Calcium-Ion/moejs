package engine

import "sync"

// intlDateAtoms holds the property names and string values of
// Intl.DateTimeFormat, interned with the first DateTimeFormat of the process
// as intlNames are.
type intlDateAtoms struct {
	dateTimeFormatName, dateTimeFormatTag                       *String // "DateTimeFormat", "Intl.DateTimeFormat"
	format, formatToParts, formatRange, formatRangeToParts      *String
	timeZone, hour12, formatMatcher, dateStyle, timeStyle       *String
	weekday, era, year, month, day, dayPeriod                   *String
	hour, minute, second, fractionalSecondDigits, timeZoneName  *String
	narrow, long, short, twoDigit, numeric, full, medium, basic *String
	shortOffset, longOffset, shortGeneric, longGeneric          *String
	literal, fractionalSecond, typ, value, source               *String
	shared, startRange, endRange, gregory, getFormat            *String
}

var intlDateNames = sync.OnceValue(func() *intlDateAtoms {
	return &intlDateAtoms{
		dateTimeFormatName:     lateAtom("DateTimeFormat"),
		dateTimeFormatTag:      lateAtom("Intl.DateTimeFormat"),
		format:                 lateAtom("format"),
		formatToParts:          lateAtom("formatToParts"),
		formatRange:            lateAtom("formatRange"),
		formatRangeToParts:     lateAtom("formatRangeToParts"),
		timeZone:               lateAtom("timeZone"),
		hour12:                 lateAtom("hour12"),
		formatMatcher:          lateAtom("formatMatcher"),
		dateStyle:              lateAtom("dateStyle"),
		timeStyle:              lateAtom("timeStyle"),
		weekday:                lateAtom("weekday"),
		era:                    lateAtom("era"),
		year:                   lateAtom("year"),
		month:                  lateAtom("month"),
		day:                    lateAtom("day"),
		dayPeriod:              lateAtom("dayPeriod"),
		hour:                   lateAtom("hour"),
		minute:                 lateAtom("minute"),
		second:                 lateAtom("second"),
		fractionalSecondDigits: lateAtom("fractionalSecondDigits"),
		timeZoneName:           lateAtom("timeZoneName"),
		narrow:                 lateAtom("narrow"),
		long:                   lateAtom("long"),
		short:                  lateAtom("short"),
		twoDigit:               lateAtom("2-digit"),
		numeric:                lateAtom("numeric"),
		full:                   lateAtom("full"),
		medium:                 lateAtom("medium"),
		basic:                  lateAtom("basic"),
		shortOffset:            lateAtom("shortOffset"),
		longOffset:             lateAtom("longOffset"),
		shortGeneric:           lateAtom("shortGeneric"),
		longGeneric:            lateAtom("longGeneric"),
		literal:                lateAtom("literal"),
		fractionalSecond:       lateAtom("fractionalSecond"),
		typ:                    lateAtom("type"),
		value:                  lateAtom("value"),
		source:                 lateAtom("source"),
		shared:                 lateAtom("shared"),
		startRange:             lateAtom("startRange"),
		endRange:               lateAtom("endRange"),
		gregory:                lateAtom("gregory"),
		getFormat:              lateAtom("get format"),
	}
})
