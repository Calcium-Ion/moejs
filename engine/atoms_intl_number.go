package engine

import "sync"

// Atoms of Intl.NumberFormat and Intl.PluralRules, interned with the first
// use of either (intlNumberNames), as intlNames does for the rest of Intl.
type intlNumberAtoms struct {
	numberFormatName, numberFormatTag, pluralRulesName, pluralRulesTag *String

	format, formatGetter, formatToParts, formatRange, formatRangeToParts *String
	selectName, selectRange                                              *String

	// Option names.
	style, currency, currencyDisplay, currencySign, unit, unitDisplay  *String
	notation, compactDisplay, useGrouping, signDisplay                 *String
	minimumIntegerDigits, minimumFractionDigits, maximumFractionDigits *String
	minimumSignificantDigits, maximumSignificantDigits                 *String
	roundingIncrement, roundingMode, roundingPriority                  *String
	trailingZeroDisplay, typeName, pluralCategories                    *String

	// Option values.
	decimal, percent, code, symbol, narrowSymbol, name       *String
	standard, accounting, short, narrow, long                *String
	scientific, engineering, compact                         *String
	min2, auto, always, trueStr, never, exceptZero, negative *String
	ceil, floor, expand, trunc                               *String
	halfCeil, halfFloor, halfExpand, halfTrunc, halfEven     *String
	morePrecision, lessPrecision, stripIfInteger             *String
	cardinal, ordinal                                        *String
	zero, one, two, few, many, other                         *String

	// formatToParts.
	value, source, startRange, endRange, shared            *String
	literal, integer, group, fraction, plusSign, minusSign *String
	percentSign, exponentSeparator, exponentMinusSign      *String
	exponentInteger, approximatelySign, nan, infinity      *String
}

var intlNumberNames = sync.OnceValue(func() *intlNumberAtoms {
	return &intlNumberAtoms{
		numberFormatName: lateAtom("NumberFormat"),
		numberFormatTag:  lateAtom("Intl.NumberFormat"),
		pluralRulesName:  lateAtom("PluralRules"),
		pluralRulesTag:   lateAtom("Intl.PluralRules"),

		format:             lateAtom("format"),
		formatGetter:       lateAtom("get format"),
		formatToParts:      lateAtom("formatToParts"),
		formatRange:        lateAtom("formatRange"),
		formatRangeToParts: lateAtom("formatRangeToParts"),
		selectName:         lateAtom("select"),
		selectRange:        lateAtom("selectRange"),

		style:                    lateAtom("style"),
		currency:                 lateAtom("currency"),
		currencyDisplay:          lateAtom("currencyDisplay"),
		currencySign:             lateAtom("currencySign"),
		unit:                     lateAtom("unit"),
		unitDisplay:              lateAtom("unitDisplay"),
		notation:                 lateAtom("notation"),
		compactDisplay:           lateAtom("compactDisplay"),
		useGrouping:              lateAtom("useGrouping"),
		signDisplay:              lateAtom("signDisplay"),
		minimumIntegerDigits:     lateAtom("minimumIntegerDigits"),
		minimumFractionDigits:    lateAtom("minimumFractionDigits"),
		maximumFractionDigits:    lateAtom("maximumFractionDigits"),
		minimumSignificantDigits: lateAtom("minimumSignificantDigits"),
		maximumSignificantDigits: lateAtom("maximumSignificantDigits"),
		roundingIncrement:        lateAtom("roundingIncrement"),
		roundingMode:             lateAtom("roundingMode"),
		roundingPriority:         lateAtom("roundingPriority"),
		trailingZeroDisplay:      lateAtom("trailingZeroDisplay"),
		typeName:                 lateAtom("type"),
		pluralCategories:         lateAtom("pluralCategories"),

		decimal:        lateAtom("decimal"),
		percent:        lateAtom("percent"),
		code:           lateAtom("code"),
		symbol:         lateAtom("symbol"),
		narrowSymbol:   lateAtom("narrowSymbol"),
		name:           lateAtom("name"),
		standard:       lateAtom("standard"),
		accounting:     lateAtom("accounting"),
		short:          lateAtom("short"),
		narrow:         lateAtom("narrow"),
		long:           lateAtom("long"),
		scientific:     lateAtom("scientific"),
		engineering:    lateAtom("engineering"),
		compact:        lateAtom("compact"),
		min2:           lateAtom("min2"),
		auto:           lateAtom("auto"),
		always:         lateAtom("always"),
		trueStr:        lateAtom("true"),
		never:          lateAtom("never"),
		exceptZero:     lateAtom("exceptZero"),
		negative:       lateAtom("negative"),
		ceil:           lateAtom("ceil"),
		floor:          lateAtom("floor"),
		expand:         lateAtom("expand"),
		trunc:          lateAtom("trunc"),
		halfCeil:       lateAtom("halfCeil"),
		halfFloor:      lateAtom("halfFloor"),
		halfExpand:     lateAtom("halfExpand"),
		halfTrunc:      lateAtom("halfTrunc"),
		halfEven:       lateAtom("halfEven"),
		morePrecision:  lateAtom("morePrecision"),
		lessPrecision:  lateAtom("lessPrecision"),
		stripIfInteger: lateAtom("stripIfInteger"),
		cardinal:       lateAtom("cardinal"),
		ordinal:        lateAtom("ordinal"),
		zero:           lateAtom("zero"),
		one:            lateAtom("one"),
		two:            lateAtom("two"),
		few:            lateAtom("few"),
		many:           lateAtom("many"),
		other:          lateAtom("other"),

		value:             lateAtom("value"),
		source:            lateAtom("source"),
		startRange:        lateAtom("startRange"),
		endRange:          lateAtom("endRange"),
		shared:            lateAtom("shared"),
		literal:           lateAtom("literal"),
		integer:           lateAtom("integer"),
		group:             lateAtom("group"),
		fraction:          lateAtom("fraction"),
		plusSign:          lateAtom("plusSign"),
		minusSign:         lateAtom("minusSign"),
		percentSign:       lateAtom("percentSign"),
		exponentSeparator: lateAtom("exponentSeparator"),
		exponentMinusSign: lateAtom("exponentMinusSign"),
		exponentInteger:   lateAtom("exponentInteger"),
		approximatelySign: lateAtom("approximatelySign"),
		nan:               lateAtom("nan"),
		infinity:          lateAtom("infinity"),
	}
})
