package engine

import "sync"

// intlInfoAtoms holds the names of the Intl.Locale info methods and of
// Intl.supportedValuesOf, interned with the first Intl group as intlNames.
type intlInfoAtoms struct {
	getCalendars, getCollations, getHourCycles, getNumberingSystems *String
	getTimeZones, getTextInfo, getWeekInfo, supportedValuesOf       *String
	direction, rtl, ltr, firstDay, weekend                          *String
}

var intlInfoNames = sync.OnceValue(func() *intlInfoAtoms {
	return &intlInfoAtoms{
		getCalendars:        lateAtom("getCalendars"),
		getCollations:       lateAtom("getCollations"),
		getHourCycles:       lateAtom("getHourCycles"),
		getNumberingSystems: lateAtom("getNumberingSystems"),
		getTimeZones:        lateAtom("getTimeZones"),
		getTextInfo:         lateAtom("getTextInfo"),
		getWeekInfo:         lateAtom("getWeekInfo"),
		supportedValuesOf:   lateAtom("supportedValuesOf"),
		direction:           lateAtom("direction"),
		rtl:                 lateAtom("rtl"),
		ltr:                 lateAtom("ltr"),
		firstDay:            lateAtom("firstDay"),
		weekend:             lateAtom("weekend"),
	}
})
