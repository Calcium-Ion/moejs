package engine

import (
	"sync"
	"unsafe"
)

// Atoms of Intl. Only the global name is static, because coldGlobalKeys
// needs it at package init; the property names are interned with the first
// Intl group of the process (intlNames), so a process that never names Intl
// carries none of them. They live apart from atoms.go so parallel additions
// to that table merge mechanically; staticAtom deduplicates names.
var AtomIntl = staticAtom("Intl")

// intlAtoms holds the property names and string values of Intl.
type intlAtoms struct {
	getCanonicalLocales, supportedLocalesOf, resolvedOptions *String
	localeMatcher, lookup, bestFit, locale                   *String

	// Intl.Locale.
	localeName, localeTag                                     *String // "Locale", "Intl.Locale"
	maximize, minimize, baseName                              *String
	language, script, region, variants                        *String
	calendar, caseFirst, collation, firstDayOfWeek, hourCycle *String
	numeric, numberingSystem                                  *String
	h11, h12, h23, h24, upper, lower, falseStr                *String

	// Intl.Segmenter.
	segmenterName, segmenterTag, segmentIteratorTag *String
	granularity, segment, containing, isWordLike    *String
	grapheme, word, sentence                        *String
}

// intlAtomNames are the strings of intlAtoms' fields, in field order.
var intlAtomNames = [...]string{
	"getCanonicalLocales", "supportedLocalesOf", "resolvedOptions",
	"localeMatcher", "lookup", "best fit", "locale",

	"Locale", "Intl.Locale",
	"maximize", "minimize", "baseName",
	"language", "script", "region", "variants",
	"calendar", "caseFirst", "collation", "firstDayOfWeek", "hourCycle",
	"numeric", "numberingSystem",
	"h11", "h12", "h23", "h24", "upper", "lower", "false",

	"Segmenter", "Intl.Segmenter", "Segmenter String Iterator",
	"granularity", "segment", "containing", "isWordLike",
	"grapheme", "word", "sentence",
}

// intlAtoms has a field for each of intlAtomNames.
var _ = [1]struct{}{}[unsafe.Sizeof(intlAtoms{})-unsafe.Sizeof([len(intlAtomNames)]*String{})]

// intlNames interns them once per process, as binaryNames does, filling
// the fields from the table.
var intlNames = sync.OnceValue(func() *intlAtoms {
	a := new(intlAtoms)
	fields := (*[len(intlAtomNames)]*String)(unsafe.Pointer(a))
	for i, name := range intlAtomNames {
		fields[i] = lateAtom(name)
	}
	return a
})
