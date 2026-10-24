package test262

import (
	"os"
	"path/filepath"
	"strings"
)

// implemented is the set of test262 feature tags (features.txt in the
// checkout) that moejs implements. A test that lists any other feature, or
// includes a harness file that implies one (harness/features.yml), is
// skipped with that feature named rather than failed. Implementing a feature
// adds its tag here and regenerates baseline.txt and RESULTS.md.
var implemented = setOf(
	// Syntax.
	"arbitrary-module-namespace-names",
	"arrow-function",
	"async-functions",
	"async-iteration",
	"class",
	"class-fields-private",
	"class-fields-private-in",
	"class-fields-public",
	"class-methods-private",
	"class-static-block",
	"class-static-fields-private",
	"class-static-fields-public",
	"class-static-methods-private",
	"coalesce-expression",
	"computed-property-names",
	"const",
	"default-parameters",
	"destructuring-assignment",
	"destructuring-binding",
	"dynamic-import",
	"exponentiation",
	"export-star-as-namespace-from-module",
	"for-in-order",
	"for-of",
	"generators",
	"globalThis",
	"hashbang",
	"import.meta",
	"json-superset",
	"let",
	"logical-assignment-operators",
	"new.target",
	"numeric-separator-literal",
	"object-rest",
	"object-spread",
	"optional-catch-binding",
	"optional-chaining",
	"rest-parameters",
	"super",
	"template",
	"top-level-await",
	"u180e",
	"__proto__",

	// Regular expressions.
	"legacy-regexp",
	"regexp-dotall",
	"regexp-duplicate-named-groups",
	"regexp-lookbehind",
	"regexp-match-indices",
	"regexp-modifiers",
	"regexp-named-groups",
	"regexp-unicode-property-escapes",
	"regexp-v-flag",

	// Builtins.
	"AggregateError",
	"align-detached-buffer-semantics-with-web-reality",
	"Array.fromAsync",
	"ArrayBuffer",
	"arraybuffer-transfer",
	"Array.prototype.at",
	"Array.prototype.flat",
	"Array.prototype.flatMap",
	"Array.prototype.includes",
	"Array.prototype.values",
	"Atomics",
	"array-find-from-last",
	"array-grouping",
	"BigInt",
	"canonical-tz",
	"change-array-by-copy",
	"DataView",
	"DataView.prototype.getFloat32",
	"DataView.prototype.getFloat64",
	"DataView.prototype.getInt16",
	"DataView.prototype.getInt32",
	"DataView.prototype.getInt8",
	"DataView.prototype.getUint16",
	"DataView.prototype.getUint32",
	"DataView.prototype.setUint8",
	"error-cause",
	"Error.isError",
	"Float16Array",
	"Float32Array",
	"Float64Array",
	"Int16Array",
	"Int32Array",
	"Int8Array",
	"Intl-enumeration",
	"Intl.DateTimeFormat-datetimestyle",
	"Intl.DateTimeFormat-dayPeriod",
	"Intl.DateTimeFormat-extend-timezonename",
	"Intl.DateTimeFormat-formatRange",
	"Intl.DateTimeFormat-fractionalSecondDigits",
	"Intl.Locale",
	"Intl.Locale-info",
	"Intl.NumberFormat-unified",
	"Intl.NumberFormat-v3",
	"Intl.Segmenter",
	"Map",
	"Math.sumPrecise",
	"Object.fromEntries",
	"Object.hasOwn",
	"Object.is",
	"Promise",
	"Promise.allSettled",
	"Promise.any",
	"Promise.prototype.finally",
	"promise-try",
	"promise-with-resolvers",
	"Proxy",
	"proxy-missing-checks",
	"Reflect",
	"Reflect.construct",
	"Reflect.set",
	"Reflect.setPrototypeOf",
	"resizable-arraybuffer",
	"Set",
	"set-methods",
	"SharedArrayBuffer",
	"stable-array-sort",
	"stable-typedarray-sort",
	"string-trimming",
	"String.fromCodePoint",
	"String.prototype.at",
	"String.prototype.endsWith",
	"String.prototype.includes",
	"String.prototype.isWellFormed",
	"String.prototype.matchAll",
	"String.prototype.replaceAll",
	"String.prototype.toWellFormed",
	"String.prototype.trimEnd",
	"String.prototype.trimStart",
	"Symbol",
	"Symbol.asyncIterator",
	"Symbol.hasInstance",
	"Symbol.isConcatSpreadable",
	"Symbol.iterator",
	"Symbol.match",
	"Symbol.matchAll",
	"Symbol.prototype.description",
	"Symbol.replace",
	"Symbol.search",
	"Symbol.species",
	"Symbol.split",
	"Symbol.toPrimitive",
	"Symbol.toStringTag",
	"Symbol.unscopables",
	"symbols-as-weakmap-keys",
	"TypedArray",
	"TypedArray.prototype.at",
	"Uint16Array",
	"Uint32Array",
	"Uint8Array",
	"uint8array-base64",
	"Uint8ClampedArray",
	"WeakMap",
	"WeakRef",
	"WeakSet",
	"well-formed-json-stringify",
	"__getter__",
	"__setter__",
)

// outOfScope maps the tests that need what moejs's Intl leaves out on
// purpose, or that check what it keeps as V8 has it, to the reason; they are
// skipped as "out of scope: <reason>".
var outOfScope = map[string]string{
	// Intl.DateTimeFormat has the Gregorian calendar only: getCalendars is
	// ["gregory"] for every region, so these find no region pair whose
	// calendars differ, and the others format or resolve other calendars.
	"intl402/Locale/prototype/getCalendars/likely-subtags-region.js":                                       gregoryOnly,
	"intl402/Locale/prototype/getCalendars/region-override.js":                                             gregoryOnly,
	"intl402/Locale/prototype/getCalendars/region-priority.js":                                             gregoryOnly,
	"intl402/Locale/prototype/getCalendars/subdivision-region.js":                                          gregoryOnly,
	"intl402/DateTimeFormat/canonicalize-calendar.js":                                                      gregoryOnly,
	"intl402/DateTimeFormat/prototype/format/related-year-zh.js":                                           gregoryOnly,
	"intl402/DateTimeFormat/prototype/formatRangeToParts/chinese-calendar-dates.js":                        gregoryOnly,
	"intl402/DateTimeFormat/prototype/formatRangeToParts/dangi-calendar-dates.js":                          gregoryOnly,
	"intl402/DateTimeFormat/prototype/formatRangeToParts/pattern-on-calendar.js":                           gregoryOnly,
	"intl402/DateTimeFormat/prototype/formatToParts/chinese-calendar-dates.js":                             gregoryOnly,
	"intl402/DateTimeFormat/prototype/formatToParts/pattern-on-calendar.js":                                gregoryOnly,
	"intl402/DateTimeFormat/prototype/formatToParts/related-year-zh.js":                                    gregoryOnly,
	"intl402/DateTimeFormat/prototype/formatToParts/related-year.js":                                       gregoryOnly,
	"intl402/DateTimeFormat/prototype/resolvedOptions/resolved-calendar-unicode-extensions-and-options.js": gregoryOnly,
	// moejs has no Intl.Collator.
	"intl402/DateTimeFormat/this-value-ignored.js":                      noCollator,
	"intl402/Intl/supportedValuesOf/collations-accepted-by-Collator.js": noCollator,
	"intl402/NumberFormat/this-value-ignored.js":                        noCollator,
	"intl402/PluralRules/undefined-newtarget-throws.js":                 noCollator,
	// format writes a space where formatToParts has U+202F (before AM/PM
	// in en), as V8 does; Node 24 fails this test too.
	"intl402/DateTimeFormat/prototype/format/timedatestyle-en.js": "format's space for U+202F, as in V8",
}

const (
	gregoryOnly = "Gregorian calendar only"
	noCollator  = "no Intl.Collator"
)

// nonGoal reports whether a feature is outside the project's goals: the
// Atomics.wait family, which needs a blocking or multi-agent host,
// FinalizationRegistry, and the normative-optional parts of ECMA-402. Such
// tests are skipped as "non-goal: <feature>".
func nonGoal(feature string) bool {
	switch feature {
	case "Atomics.waitAsync", "FinalizationRegistry", "intl-normative-optional":
		return true
	}
	return false
}

func setOf(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// loadImplied reads harness/features.yml, which maps a harness include to
// the features every test that includes it needs. Keys are stored with the
// .js suffix, as the tests' includes spell them.
func loadImplied(root string) (map[string][]string, error) {
	b, err := os.ReadFile(filepath.Join(root, "harness", "features.yml"))
	if err != nil {
		return nil, err
	}
	implied := make(map[string][]string)
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		items, _, err := parseList([]string{line}, 0, strings.TrimSpace(value))
		if err != nil {
			return nil, err
		}
		implied[strings.TrimSuffix(strings.TrimSpace(key), ".js")+".js"] = items
	}
	return implied, nil
}
