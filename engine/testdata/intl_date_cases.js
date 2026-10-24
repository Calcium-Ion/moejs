// The cases of the differential test of Intl.DateTimeFormat against Node
// (TestIntlDateTimeFormatNode, intl_date_node_test.go): locales, option
// sets and time values whose format, formatToParts, resolvedOptions and
// ranges the test compares with Node 24's (intl_date_node.json.gz, written
// by intl_date_node.mjs). It runs in both engines and evaluates to the list
// of [case, output] pairs.
(function () {
  "use strict";
  const out = [];
  const locales = [
    "en", "zh-Hans", "zh-Hant", "ja", "ko", "de", "fr", "es", "pt", "ru", "it",
    "en-US", "zh-CN", "zh-TW", "ja-JP", "pt-BR",
    // Regional locales with data of their own, inherited by CLDR's parent
    // locales (index 16 on); the issue's zh-HK first.
    "zh-HK", "zh-MO", "zh-Hant-HK", "zh-SG", "de-AT", "en-GB", "fr-CA", "es-MX",
    "en-001", "en-AU", "en-CA", "en-IN", "en-NZ", "en-IE", "en-ZA", "en-150", "de-CH", "fr-CH",
    "es-419", "es-AR", "pt-PT", "pt-AO",
  ];
  const T = (s) => Date.parse(s);
  const dates = [
    T("2026-10-22T00:05:07.089Z"), T("2026-10-22T09:00:00Z"), T("2026-10-22T12:00:00Z"),
    T("2026-10-22T13:45:30.5Z"), T("2026-10-22T21:00:00Z"), T("2026-10-22T23:59:59.999Z"),
    T("2026-03-08T07:30:00Z"), T("2026-11-01T05:59:59Z"), T("1970-01-01T00:00:00Z"),
    T("1900-01-01T00:00:00Z"), T("1582-10-10T12:00:00Z"), T("0000-01-01T00:00:00Z"),
    T("-000500-03-01T04:00:00Z"), 8.64e15, -8.64e15,
  ];
  const nls = ["narrow", "short", "long"];
  const sets = [
    {}, { dateStyle: "full" }, { dateStyle: "long" }, { dateStyle: "medium" }, { dateStyle: "short" },
    { timeStyle: "full" }, { timeStyle: "long" }, { timeStyle: "medium" }, { timeStyle: "short" },
    { dateStyle: "full", timeStyle: "full" }, { dateStyle: "long", timeStyle: "long" },
    { dateStyle: "medium", timeStyle: "medium" }, { dateStyle: "short", timeStyle: "short" },
    { dateStyle: "full", timeStyle: "short" }, { dateStyle: "short", timeStyle: "full" },
    { dateStyle: "medium", timeStyle: "short", hour12: true }, { dateStyle: "medium", timeStyle: "short", hour12: false },
    { timeStyle: "medium", hourCycle: "h11" }, { timeStyle: "medium", hourCycle: "h12" },
    { timeStyle: "medium", hourCycle: "h23" }, { timeStyle: "medium", hourCycle: "h24" },
    { year: "numeric" }, { year: "2-digit" }, { month: "numeric" }, { month: "2-digit" },
    { month: "narrow" }, { month: "short" }, { month: "long" }, { day: "numeric" }, { day: "2-digit" },
    { weekday: "narrow" }, { weekday: "short" }, { weekday: "long" },
    { era: "narrow" }, { era: "short" }, { era: "long" },
    { year: "numeric", era: "short" }, { year: "numeric", month: "long", era: "long" },
    { year: "numeric", month: "short", day: "numeric", era: "narrow" },
    { year: "numeric", month: "numeric", day: "numeric" }, { year: "2-digit", month: "2-digit", day: "2-digit" },
    { year: "numeric", month: "2-digit", day: "2-digit" }, { year: "numeric", month: "long", day: "numeric" },
    { year: "numeric", month: "short", day: "numeric" }, { year: "numeric", month: "narrow", day: "numeric" },
    { weekday: "long", year: "numeric", month: "long", day: "numeric" },
    { weekday: "short", year: "numeric", month: "short", day: "numeric" },
    { weekday: "short", month: "numeric", day: "numeric" }, { weekday: "long", day: "numeric" },
    { month: "short", day: "numeric" }, { month: "long", day: "numeric" }, { year: "numeric", month: "short" },
    { year: "numeric", month: "long" }, { year: "numeric", month: "numeric" }, { month: "long", weekday: "long" },
    { year: "numeric", weekday: "short" }, { era: "long", weekday: "long" },
    { hour: "numeric" }, { hour: "2-digit" }, { hour: "numeric", minute: "numeric" }, { hour: "2-digit", minute: "2-digit" },
    { hour: "numeric", minute: "2-digit", second: "2-digit" }, { hour: "numeric", hour12: true }, { hour: "numeric", hour12: false },
    { hour: "numeric", minute: "2-digit", hourCycle: "h11" }, { hour: "numeric", minute: "2-digit", hourCycle: "h12" },
    { hour: "2-digit", minute: "2-digit", hourCycle: "h23" }, { hour: "numeric", minute: "2-digit", hourCycle: "h24" },
    { hour: "numeric", hourCycle: "h23", hour12: true },
    { hour: "numeric", dayPeriod: "narrow" }, { hour: "numeric", dayPeriod: "short" }, { hour: "numeric", dayPeriod: "long" },
    { hour: "numeric", minute: "numeric", dayPeriod: "short", hour12: true }, { dayPeriod: "long" }, { dayPeriod: "narrow", hour12: true },
    { minute: "numeric" }, { minute: "2-digit" }, { second: "numeric" }, { second: "2-digit" }, { minute: "2-digit", second: "2-digit" },
    { hour: "numeric", second: "numeric" }, { hour: "numeric", minute: "numeric", second: "numeric", fractionalSecondDigits: 1 },
    { minute: "2-digit", second: "2-digit", fractionalSecondDigits: 2 }, { second: "numeric", fractionalSecondDigits: 3 },
    { fractionalSecondDigits: 3 }, { hour: "numeric", fractionalSecondDigits: 2 },
    { weekday: "short", hour: "numeric", minute: "numeric" }, { day: "numeric", hour: "numeric" },
    { year: "numeric", month: "short", day: "numeric", hour: "numeric", minute: "numeric" },
    { year: "numeric", month: "long", day: "numeric", hour: "numeric", minute: "2-digit", second: "2-digit" },
    { weekday: "long", year: "numeric", month: "long", day: "numeric", hour: "2-digit", minute: "2-digit", hourCycle: "h23" },
    { month: "numeric", day: "numeric", hour: "numeric", minute: "numeric", hour12: false },
    { year: "2-digit", month: "narrow", weekday: "narrow", day: "2-digit", hour: "2-digit", minute: "numeric", second: "numeric" },
    { era: "short", hour: "numeric" }, { era: "long", year: "numeric", hour: "numeric", minute: "numeric" },
    { hour: "numeric", timeZoneName: "short" }, { hour: "numeric", minute: "numeric", timeZoneName: "long" },
    { hour: "numeric", timeZoneName: "shortOffset" }, { hour: "numeric", timeZoneName: "longOffset" },
    { hour: "numeric", timeZoneName: "shortGeneric" }, { hour: "numeric", timeZoneName: "longGeneric" },
    { hour: "numeric", timeZoneName: "shortGeneric", hourCycle: "h23" },
    { year: "numeric", month: "numeric", day: "numeric", timeZoneName: "short" }, { timeZoneName: "short" },
    { numberingSystem: "arab", dateStyle: "medium", timeStyle: "medium" }, { numberingSystem: "hanidec", year: "numeric", month: "long", day: "numeric" },
    { numberingSystem: "thai", hour: "2-digit", minute: "2-digit", second: "2-digit", fractionalSecondDigits: 3 },
    { numberingSystem: "fullwide" }, { calendar: "gregory", dateStyle: "long" },
  ];
  const zones = ["UTC", "UTC", "Asia/Tokyo", "America/New_York", "Europe/Berlin", "Asia/Kolkata", "+05:30"];
  const fmtParts = (parts) => parts.map((p) => p.type + "=" + p.value + (p.source ? "/" + p.source[0] : "")).join("|");
  const run = (desc, fn) => {
    let v;
    try {
      v = fn();
    } catch (e) {
      v = "ERR:" + e.constructor.name;
    }
    out.push([desc, v]);
  };
  for (let li = 0; li < locales.length; li++) {
    const loc = locales[li];
    for (let si = 0; si < sets.length; si++) {
      if (li >= 17 && si % 3 != 0) continue;
      const opts = Object.assign({ timeZone: zones[(li + si) % zones.length] }, sets[si]);
      const desc = loc + " " + JSON.stringify(opts);
      let f;
      run(desc + " new", () => {
        f = new Intl.DateTimeFormat(loc, opts);
        return JSON.stringify(f.resolvedOptions());
      });
      if (!f) continue;
      for (let di = 0; di < dates.length; di++) {
        // Every option set gets a few of the dates, all of them for the
        // locales with data.
        if (li >= 11 && (di + si) % 4 != 0) continue;
        run(desc + " " + dates[di], () => f.format(dates[di]));
        if ((di + si) % 4 == 0) run(desc + " parts " + dates[di], () => fmtParts(f.formatToParts(dates[di])));
      }
      if (li < 3 ? si % 2 == 0 : si % 4 == 0) {
        for (const [a, b] of [
          [dates[0], dates[0] + 3], [dates[0], dates[0] + 1500], [dates[0], dates[0] + 60000],
          [dates[0], dates[0] + 13 * 3600000], [dates[0], dates[0] + 86400000], [dates[0], dates[0] + 40 * 86400000],
          [dates[0], dates[0] + 400 * 86400000], [dates[12], dates[0]], [dates[3], dates[1]],
        ]) {
          run(desc + " range " + a + " " + b, () => f.formatRange(a, b));
          if (si % 4 == 0) run(desc + " rangeParts " + a + " " + b, () => fmtParts(f.formatRangeToParts(a, b)));
        }
      }
    }
  }
  // The locale's own keywords and errors.
  for (const [loc, opts] of [
    ["en-u-hc-h23", { hour: "numeric" }], ["en-u-hc-h23", { timeStyle: "short" }], ["ja-u-hc-h11", { timeStyle: "medium" }],
    ["en-u-hc-h11", { hour: "numeric", hour12: true }], ["en-u-hc-h24", { hour: "numeric", hourCycle: "h12" }],
    ["de-u-hc-h12", { dateStyle: "short", timeStyle: "short" }], ["zh-u-nu-hanidec", { dateStyle: "long" }],
    ["en-u-nu-arab-hc-h23", { hour: "2-digit", minute: "2-digit" }], ["en-u-ca-gregory", {}], ["fr-u-ca-gregory-hc-h12", { timeStyle: "short" }],
    ["en", { dateStyle: "short", year: "numeric" }], ["en", { hour: "bogus" }], ["en", { fractionalSecondDigits: 4 }],
    ["en", { timeZone: "Mars/Olympus" }], ["en", { calendar: "x" }], ["en", { numberingSystem: "latn-x" }],
  ]) {
    const desc = loc + " " + JSON.stringify(opts);
    run(desc + " new", () => {
      const f = new Intl.DateTimeFormat(loc, Object.assign({ timeZone: "UTC" }, opts));
      return JSON.stringify(f.resolvedOptions()) + " " + f.format(dates[3]) + " " + fmtParts(f.formatToParts(dates[0]));
    });
  }
  return out;
})();
