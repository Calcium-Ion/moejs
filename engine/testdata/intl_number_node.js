// Writes intl_number_node.json.gz, the outputs of Node's Intl.NumberFormat
// and Intl.PluralRules (Node 24.22, ICU 78.3, CLDR 48) that
// TestIntlNumberNode (intl_number_node_test.go) compares moejs with: many
// option combinations in the data locales, numbers of every kind (negative
// zero, huge, tiny, NaN, the infinities, BigInts, decimal strings, rounding
// edges) through format, formatToParts and formatRange(ToParts), and the
// plural categories of every CLDR language.
//
// Usage (the environment fixes Node's default locale and time zone):
//
//	LC_ALL=en_US.UTF-8 LANG=en_US.UTF-8 LANGUAGE= TZ=UTC node intl_number_node.js
"use strict";
const fs = require("fs");
const zlib = require("zlib");

const locales = ["en", "zh-Hans", "zh-Hant", "ja", "ko", "de", "fr", "es", "pt", "ru", "it"];
// Regional locales that run every config with the data locales: the
// issue's (zh-TW has zh-Hant's data, zh-HK its own) and those with data of
// their own that differ most from their language's.
const regional = ["zh-TW", "zh-HK", "en-GB", "en-IN", "de-CH", "fr-CA", "pt-PT", "es-MX"];
// Other locales, regional ones and xx, which resolves to the default
// locale.
const fallbacks = ["en-US", "ja-JP", "zh-CN", "zh-MO", "zh-Hant-HK", "zh-SG", "de-DE", "xx", "de-AT", "es-419",
  "en-001", "en-AU", "en-CA", "en-NZ", "en-IE", "en-ZA", "en-150", "en-NL", "fr-CH", "it-CH", "es-AR", "pt-AO", "ru-UA", "de-LU"];

// Values: "n:" a Number literal, "b:" a BigInt, "s:" a string.
const values = [
  "n:0", "n:-0", "n:1", "n:-1", "n:2", "n:5", "n:12", "n:0.5", "n:-0.5", "n:1.5", "n:2.5", "n:-2.5",
  "n:0.001", "n:0.0005", "n:1.005", "n:9.995", "n:99.95", "n:999.5", "n:1234.5678", "n:-1234.5678",
  "n:12345", "n:999999", "n:1000000", "n:1234567.891", "n:-98765432.1", "n:1e21", "n:1.7976931348623157e308",
  "n:5e-324", "n:1e-7", "n:123456789012345680000", "n:NaN", "n:Infinity", "n:-Infinity",
  "b:0", "b:123456789012345678901234567890", "b:-42",
  "s:1.23456789012345678901234567890", "s:-0", "s:1e-30", "s:0x1F", "s: 12 ", "s:Infinity", "s:abc", "s:", "s:-1234.5000",
];
const partValues = ["n:-1234.5678", "n:0", "n:-0", "n:NaN", "n:-Infinity", "n:1234567.891", "n:0.5", "b:-42", "s:1e-30"];
const rangePairs = [
  ["n:3", "n:5"], ["n:-5", "n:-3"], ["n:-3", "n:5"], ["n:5", "n:3"], ["n:3", "n:3"], ["n:3", "n:3.0001"],
  ["n:0", "n:-0"], ["n:1", "n:2"], ["n:0.5", "n:1"], ["n:1000", "n:5000"], ["n:999", "n:1001"],
  ["n:1234", "n:1200000"], ["n:-Infinity", "n:Infinity"], ["n:1", "n:Infinity"], ["b:1", "s:1.0"],
];

const configs = [
  {}, {useGrouping: false}, {useGrouping: "always"}, {useGrouping: "min2"}, {useGrouping: true},
  {minimumIntegerDigits: 3}, {minimumFractionDigits: 2}, {maximumFractionDigits: 0}, {maximumFractionDigits: 20},
  {minimumSignificantDigits: 3}, {maximumSignificantDigits: 2}, {minimumSignificantDigits: 2, maximumSignificantDigits: 4},
  {maximumFractionDigits: 2, maximumSignificantDigits: 3, roundingPriority: "morePrecision"},
  {maximumFractionDigits: 2, maximumSignificantDigits: 3, roundingPriority: "lessPrecision"},
  {roundingIncrement: 5, maximumFractionDigits: 2, minimumFractionDigits: 2},
  {roundingIncrement: 25, maximumFractionDigits: 2, minimumFractionDigits: 2},
  {trailingZeroDisplay: "stripIfInteger", minimumFractionDigits: 2},
  ...["ceil", "floor", "expand", "trunc", "halfCeil", "halfFloor", "halfExpand", "halfTrunc", "halfEven"].map(roundingMode => ({roundingMode, maximumFractionDigits: 0})),
  ...["never", "always", "exceptZero", "negative"].map(signDisplay => ({signDisplay})),
  {style: "percent"}, {style: "percent", minimumFractionDigits: 1}, {style: "percent", signDisplay: "exceptZero"},
  ...["USD", "EUR", "JPY", "CNY", "KRW", "GBP", "CHF", "BHD", "TWD", "RUB", "BRL", "XXX", "ABC", "ITL"].map(currency => ({style: "currency", currency})),
  ...["code", "narrowSymbol", "name"].flatMap(currencyDisplay => ["USD", "EUR", "JPY", "RUB", "CNY"].map(currency => ({style: "currency", currency, currencyDisplay}))),
  {style: "currency", currency: "USD", currencySign: "accounting"}, {style: "currency", currency: "EUR", currencySign: "accounting"},
  ...["always", "exceptZero", "never", "negative"].map(signDisplay => ({style: "currency", currency: "USD", currencySign: "accounting", signDisplay})),
  {style: "currency", currency: "EUR", currencySign: "accounting", currencyDisplay: "name"},
  {style: "currency", currency: "USD", notation: "compact"}, {style: "currency", currency: "EUR", notation: "compact", compactDisplay: "long"},
  {style: "currency", currency: "JPY", notation: "compact"}, {style: "currency", currency: "CHF", notation: "compact", currencyDisplay: "code"},
  {style: "currency", currency: "USD", notation: "compact", currencyDisplay: "name"}, {style: "currency", currency: "EUR", notation: "scientific"},
  {style: "currency", currency: "USD", minimumFractionDigits: 0}, {style: "currency", currency: "JPY", maximumSignificantDigits: 2},
  ...["meter", "kilometer-per-hour", "kilobyte-per-second", "percent", "celsius", "liter", "mile-per-gallon", "byte", "day", "hour", "gram-per-liter", "fluid-ounce", "mile-scandinavian", "second-per-meter"]
    .flatMap(unit => ["short", "narrow", "long"].map(unitDisplay => ({style: "unit", unit, unitDisplay}))),
  {notation: "scientific"}, {notation: "engineering"}, {notation: "scientific", maximumFractionDigits: 1}, {notation: "engineering", minimumSignificantDigits: 4},
  {notation: "compact"}, {notation: "compact", compactDisplay: "long"}, {notation: "compact", maximumSignificantDigits: 4},
  {notation: "compact", minimumFractionDigits: 1}, {notation: "compact", useGrouping: "always"}, {notation: "compact", signDisplay: "always"},
  {style: "percent", notation: "compact"}, {style: "unit", unit: "kilometer", unitDisplay: "long", notation: "compact"},
  {style: "unit", unit: "byte", notation: "compact", compactDisplay: "long"}, {style: "percent", notation: "scientific"},
  {numberingSystem: "arab"}, {numberingSystem: "arabext"}, {numberingSystem: "deva"}, {numberingSystem: "hanidec"}, {numberingSystem: "fullwide"},
  {numberingSystem: "thai", style: "currency", currency: "EUR"}, {numberingSystem: "beng", style: "percent"}, {numberingSystem: "arab", notation: "compact"},
  {numberingSystem: "arab", style: "currency", currency: "USD"}, {numberingSystem: "arab", notation: "scientific"}, {numberingSystem: "mathbold", signDisplay: "always"},
  {numberingSystem: "native"}, {numberingSystem: "latn"}, {numberingSystem: "abc"},
];
// Locales with a -u-nu- extension.
const nuLocales = ["en-u-nu-arab", "fr-u-nu-arabext", "de-u-nu-deva", "zh-u-nu-hanidec", "ja-u-nu-fullwide", "ru-u-nu-latn", "ko-u-nu-thai", "es-u-nu-traditio", "it-u-nu-bali"];
const rangeConfigs = [
  {}, {style: "currency", currency: "USD"}, {style: "currency", currency: "EUR"}, {style: "currency", currency: "JPY"},
  {style: "currency", currency: "EUR", currencyDisplay: "code"}, {style: "currency", currency: "USD", currencyDisplay: "name"},
  {style: "percent"}, {style: "unit", unit: "meter", unitDisplay: "long"}, {style: "unit", unit: "kilometer-per-hour"},
  {style: "unit", unit: "celsius", unitDisplay: "narrow"}, {notation: "compact"}, {notation: "compact", compactDisplay: "long"},
  {notation: "scientific"}, {maximumFractionDigits: 0}, {signDisplay: "always"}, {style: "currency", currency: "USD", currencySign: "accounting"},
  {style: "currency", currency: "EUR", notation: "compact"}, {numberingSystem: "arab", style: "currency", currency: "USD"},
];

function decode(v) {
  const k = v.slice(0, 2), s = v.slice(2);
  if (k === "n:") return Number(s);
  if (k === "b:") return BigInt(s);
  return s;
}

function run(fn) {
  try {
    return fn();
  } catch (e) {
    return "!" + e.constructor.name;
  }
}

function parts(ps) {
  return ps.map(p => p.source ? [p.type, p.value, p.source] : [p.type, p.value]);
}

const out = {node: process.version, icu: process.versions.icu, cldr: process.versions.cldr, locales, regional, fallbacks, nuLocales, values, partValues, rangePairs, configs, rangeConfigs};

// format: [config][locale]: the resolved options, the formats of values
// (of shortValues for the configs past fullConfigs) and the parts of
// partValues (of the first two past fullConfigs). The data and regional
// locales run every config, the fallback and -u-nu- locales the first
// extraConfigs only.
const fullConfigs = 66, extraConfigs = 12;
const shortValues = ["n:0", "n:-0", "n:1", "n:2", "n:5", "n:-1234.5678", "n:1234567.891", "n:0.5", "n:1.5", "n:999.5", "n:NaN", "n:-Infinity", "b:-42", "s:1e-30"];
out.fullConfigs = fullConfigs;
out.extraConfigs = extraConfigs;
out.shortValues = shortValues;
const mainLocales = [...locales, ...regional];
const allLocales = [...mainLocales, ...fallbacks, ...nuLocales];
out.allLocales = allLocales;
out.mainLocales = mainLocales.length;
out.format = configs.map((c, ci) => allLocales.slice(0, ci < extraConfigs ? allLocales.length : mainLocales.length).map(l => {
  const nf = run(() => new Intl.NumberFormat(l, c));
  if (typeof nf === "string") return nf;
  const vs = ci < fullConfigs ? values : shortValues;
  const ps = ci < fullConfigs ? partValues : partValues.slice(0, 2);
  return {
    resolved: run(() => JSON.stringify(nf.resolvedOptions())),
    format: vs.map(v => run(() => nf.format(decode(v)))),
    parts: ps.map(v => run(() => parts(nf.formatToParts(decode(v))))),
  };
}));
out.range = rangeConfigs.map(c => [...mainLocales, ...nuLocales.slice(0, 2)].map(l => {
  const nf = new Intl.NumberFormat(l, c);
  return rangePairs.map(([x, y]) => run(() => [nf.formatRange(decode(x), decode(y)), parts(nf.formatRangeToParts(decode(x), decode(y)))]));
}));

// PluralRules: every language of plurals.xml and ordinals.xml (passed on
// the command line as a file of tags, one per line, by the Go test's
// generator note; here read from intl_number_plural_locales.txt).
const pluralLocales = fs.readFileSync(__dirname + "/intl_number_plural_locales.txt", "utf8").split("\n").filter(Boolean);
const pluralValues = ["n:0", "n:1", "n:2", "n:3", "n:4", "n:5", "n:6", "n:7", "n:8", "n:10", "n:11", "n:12", "n:13", "n:19", "n:20", "n:21", "n:22", "n:23",
  "n:100", "n:101", "n:102", "n:111", "n:1000", "n:1000000", "n:0.5", "n:1.5", "n:2.1", "n:0.1", "n:1.25", "s:1.0", "s:2.00", "s:11.10", "n:-1", "n:NaN", "b:1000000"];
const pluralRanges = [["n:0", "n:1"], ["n:1", "n:2"], ["n:1", "n:1.5"], ["n:2", "n:5"], ["n:0", "n:0.5"], ["n:1", "n:100"], ["n:2", "n:3"], ["n:5", "n:21"], ["n:1", "n:1"], ["n:0.5", "n:1"]];
out.pluralLocales = pluralLocales;
out.pluralValues = pluralValues;
out.pluralRanges = pluralRanges;
out.plural = pluralLocales.map(l => ["cardinal", "ordinal"].map(type => {
  const pr = new Intl.PluralRules(l, {type});
  return {
    locale: pr.resolvedOptions().locale,
    categories: pr.resolvedOptions().pluralCategories.join(),
    select: pluralValues.map(v => run(() => pr.select(decode(v)))),
    range: pluralRanges.map(([x, y]) => run(() => pr.selectRange(decode(x), decode(y)))),
  };
}));
out.pluralOptions = [{minimumFractionDigits: 2}, {maximumSignificantDigits: 1}, {roundingMode: "floor", maximumFractionDigits: 0}].map(o =>
  ["en", "fr", "ru", "ar", "pl", "cy"].map(l => pluralValues.map(v => run(() => new Intl.PluralRules(l, o).select(decode(v))))));

fs.writeFileSync(__dirname + "/intl_number_node.json.gz", zlib.gzipSync(JSON.stringify(out), {level: 9}));
