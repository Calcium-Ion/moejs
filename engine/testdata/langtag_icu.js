// Writes langtag_icu.json.gz, the ICU reference of langtag_test.go: run with
// Node 24.22 (ICU 78.3, CLDR 48.0) from engine/testdata, with the common/
// directory of cldr-common-48.zip (internal/gen/cldr downloads it):
//
//	node langtag_icu.js path/to/cldr/common
//
// For every alias type, subdivision alias and keyword value alias of CLDR
// 48, every likely-subtags key and a curated list of tags it records
// Intl.getCanonicalLocales(tag)[0] (null for a RangeError) and, for a valid
// tag, new Intl.Locale(tag).maximize() and .minimize(); for the curated tags
// also Intl.Segmenter.supportedLocalesOf(tag). Each entry is a string
// "tag canonical maximized minimized" or, for the curated tags, "tag
// canonical maximized minimized supported" (the supported locales joined by
// ','); "!" stands for a RangeError, "=" for the canonical form.
"use strict";
const fs = require("fs"), path = require("path"), zlib = require("zlib");

const common = process.argv[2];
if (!common) throw new Error("usage: node langtag_icu.js path/to/cldr/common");
const read = p => fs.readFileSync(path.join(common, p), "utf8");
const attrs = (xml, elem) => [...xml.matchAll(new RegExp(`<${elem}\\s([^>]*)>`, "g"))].map(m =>
  Object.fromEntries([...m[1].matchAll(/(\w+)="([^"]*)"/g)].map(a => [a[1], a[2]])));
const bcp = s => s.replace(/_/g, "-");

const inputs = new Set();
const meta = read("supplemental/supplementalMetadata.xml");
for (const a of attrs(meta, "languageAlias")) inputs.add(bcp(a.type));
for (const kind of ["scriptAlias", "territoryAlias", "variantAlias"])
  for (const a of attrs(meta, kind)) inputs.add("und-" + a.type);
for (const a of attrs(meta, "territoryAlias")) {
  // The region exception picks the replacement by the likely region of the
  // language: try the languages of the replacements.
  if (!a.replacement.includes(" ")) continue;
  for (const lang of ["en", "ru", "hy", "sr", "sr-Latn", "az", "ar", "cs", "sk", "nl", "fr", "de"])
    inputs.add(lang + "-" + a.type);
}
for (const a of attrs(meta, "subdivisionAlias")) {
  inputs.add("und-u-rg-" + a.type);
  inputs.add("und-u-sd-" + a.type);
}
for (const a of attrs(read("supplemental/likelySubtags.xml"), "likelySubtag")) {
  inputs.add(bcp(a.from));
  inputs.add(bcp(a.to));
}
for (const f of fs.readdirSync(path.join(common, "bcp47")).sort()) {
  const xml = read("bcp47/" + f);
  for (const m of xml.matchAll(/<key\s([^>]*)>([\s\S]*?)<\/key>/g)) {
    const key = /name="([^"]*)"/.exec(m[1])[1];
    const ext = /extension="t"/.test(m[1]) ? "t" : "u";
    for (const t of attrs(m[2], "type")) {
      for (const v of [t.name, ...(t.alias || "").split(" ")]) {
        if (!/^[0-9a-z]{3,8}(-[0-9a-z]{3,8})*$/i.test(v)) continue;
        inputs.add(`und-${ext}-${key}-${v}`);
      }
    }
  }
}

const curated = [
  // Case, order and structure.
  "en", "EN", "en-US", "en-us", "EN-us", "zh-Hans-CN", "zh-hans-cn", "ZH-HANT-TW", "ja-JP-u-ca-japanese",
  "es-419", "de-CH-1996", "de-1996-fonipa", "sl-rozaj-biske-1994", "sl-ROZAJ-Biske-1994", "en-scouse-fonipa",
  "enochian", "abcdefgh", "en-Latn-US-basiceng-boont", "und", "und-US", "und-Latn", "und-Hant", "und-Zzzz",
  "und-ZZ", "und-Zzzz-ZZ", "en-Zzzz", "en-ZZ", "zz", "zzz", "xyz-Latn", "qaa", "qaa-Qaaa-QM", "en-Qaaa", "en-AA",
  "en-XA", "en-001", "en-150", "es-419-u-nu-latn", "aa-a-foo-x-a-foo-bar", "de-gregory-u-ca-gregory",
  // Extensions.
  "en-u-ca-gregory-nu-latn", "en-u-nu-latn-ca-gregory", "en-u-attr2-attr1-ca-gregory", "en-u-attr-attr",
  "en-u-ca-gregory-ca-buddhist", "en-u-kn-true", "en-u-kn-yes", "en-u-kn-false", "en-u-kn-no", "en-u-kn",
  "en-u-ca-true", "en-u-co-true", "en-u-ks-primary", "en-u-ks-level1", "en-u-ms-imperial", "en-u-tz-eire",
  "en-u-tz-zulu", "en-u-tz-utc", "en-u-tz-cnckg", "en-u-ca-islamicc", "en-u-ca-ethiopic-amete-alem",
  "en-u-rg-gbzzzz", "en-u-rg-fi01", "en-u-sd-fi01", "en-u-rg-no23", "en-u-sd-usca", "en-u-0c", "en-u-c0",
  "en-u-va-posix", "en-US-u-va-posix", "en-u-fw-mon", "en-u-hc-h23", "en-u-dx-thai-hani",
  "en-a-bar-u-baz-x-u-foo", "en-z-zz-a-aa-0-zz", "en-b-bb-a-aa", "en-1-abc", "en-x-private", "en-x-a-b-c",
  "x-private", "en-x", "en-u", "en-a", "en-t", "en-u-", "en-a-b",
  // Transformed content.
  "en-t-iw", "en-t-en", "en-t-en-latn-ca-emodeng", "en-t-m0-names", "und-Latn-t-und-hani-m0-names",
  "DE-T-M0-DIN-K0-QWERTZ", "en-t-k0-qwertz-m0-din", "en-t-m0-din-k0-qwertz", "en-t-k0-abc-k0-def",
  "en-t-en-u-ca-gregory", "en-t-sh", "en-t-cmn-hans", "en-t-zh-hant-tw-h0-hybrid", "en-t-d0-ascii",
  "en-t-i0-handwrit", "en-t-s0-accents-publish", "en-t-iw-x-private", "en-t-m0-true", "en-t-root",
  "en-t-en-emodeng-emodeng", "en-t-ar-aao",
  // Aliases.
  "sh", "sh-Cyrl", "cnr", "cnr-BA", "hbs", "hbs-Cyrl", "mo", "iw", "in", "ji", "jw", "tl", "no", "nb", "no-bok",
  "no-nyn", "art-lojban", "cel-gaulish", "zh-guoyu", "zh-hakka", "zh-xiang", "zh-min", "zh-min-nan",
  "hy-arevela", "hy-arevmda", "ja-latn-hepburn-heploc", "ja-Latn-heploc", "und-heploc", "sv-aaland",
  "sv-FI-aaland", "el-polytoni", "sgn-GR", "sgn-BR", "sgn-US", "cmn", "cmn-TW", "yue-Hant", "zh-yue",
  "ru-SU", "hy-SU", "en-SU", "und-SU", "und-Armn-SU", "sr-CS", "sr-Latn-CS", "az-NT", "en-NT", "cs-CS",
  "sk-CS", "de-DD", "en-BU", "en-YU", "en-AN", "nl-AN", "en-FQ", "en-PC", "en-200", "sk-200", "aar-x-private",
  "heb-x-private", "ces", "posix", "hi-direct", "zh-pinyin", "es-ES-preeuro", "uz-UZ-cyrillic", "de-u-kf",
  // Invalid forms.
  "", "-", "en-", "-en", "en--US", "en_US", "root", "i-klingon", "en-GB-oed", "i-default", "x-foo", "zh-cmn-TW",
  "en-US-US", "en-Latn-Latn", "de-1996-1996", "de-tester-Tester", "en-u-ca-u-nu", "pt-u-ca-gregory-u-nu-latn",
  "en-abcdefghi", "en-a-abcdefghi", "en-x-abcdefghi", "a", "abcdefghi", "en-ß", "én", "12", "Latn-US",
  "en-u-c", "en-u-ca-abcdefghi", "en-t-0x", "en-t-en-0", "en-t-en-x0-",
  // Locale negotiation of Intl.Segmenter (every CLDR locale is available).
  "en-GB", "en-AU", "en-ZZ", "en-001", "de-AT", "de-DE-u-co-phonebk", "fr-CA", "pt-BR", "pt-PT", "zh-TW",
  "zh-HK", "zh-Hant", "zh-Hant-HK", "zh-Hans", "zh-Hans-SG", "zh-CN", "zh-SG", "sr-Latn", "sr-Latn-RS",
  "sr-RS", "sr-ME", "ca-ES-valencia", "es-ES", "es-MX", "es-419", "ar-EG", "ar-001", "hi-Latn", "ks-Arab",
  "ks-Deva-IN", "kok-Latn", "yue-Hans", "ff-Adlm-BF", "zxx", "und", "tlh", "ife", "ife-TG", "mul", "en-150",
  "en-US-posix", "nn", "nb-NO", "no-NO", "fil", "tl-PH", "he-IL", "iw-IL", "id-ID", "in-ID", "jbo",
  "en-a-foo", "en-x-foo", "ja-JP-u-ca-japanese-x-foo", "th-TH-u-nu-thai", "en-Shaw", "sat-Olck-IN",
];

const entries = [];
const run = (tag, seg) => {
  let canonical = null, max = null, min = null;
  try {
    canonical = Intl.getCanonicalLocales(tag)[0];
    max = new Intl.Locale(tag).maximize().toString();
    min = new Intl.Locale(tag).minimize().toString();
  } catch (e) {
    if (!(e instanceof RangeError)) throw e;
  }
  const same = s => s === canonical ? "=" : s;
  const entry = canonical === null ? [tag, "!", "!", "!"] : [tag, canonical, same(max), same(min)];
  if (seg) {
    let supported = "!";
    try { supported = Intl.Segmenter.supportedLocalesOf(tag).join(","); } catch (e) {}
    entry.push(supported);
  }
  entries.push(entry.join(" "));
};
for (const tag of [...inputs].sort()) if (!curated.includes(tag)) run(tag, false);
for (const tag of curated) run(tag, true);

const json = JSON.stringify({ node: process.version, icu: process.versions.icu, cldr: process.versions.cldr, entries });
fs.writeFileSync(path.join(__dirname, "langtag_icu.json.gz"), zlib.gzipSync(json, { level: 9 }));
console.log(`${entries.length} tags, ${json.length} bytes of JSON`);
