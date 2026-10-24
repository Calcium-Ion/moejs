// Regenerates intl_tz_icu.json.gz, Node's (ICU's) time zone data for
// TestIntlTZICU (intl_tz_icu_test.go):
//
//	LC_ALL=en_US.UTF-8 LANG=en_US.UTF-8 LANGUAGE= TZ=UTC node engine/testdata/intl_tz_icu.js
//
// For every identifier of intl_tz_tables.go it records the identifier Node
// reports in resolvedOptions().timeZone (r); for every primary one the
// offsets in seconds (o: i at -8.64e15, s at 1800-01-01, t the transitions
// from 1800 to 2050, found by weekly samples and bisection to the second, f
// at far instants as [time value / 1000, offset]), and for each
// data locale (n) the six timeZoneName styles at the instants of nameTimes
// (en, zh-Hans and ja at each, the others at every other one), taken from
// formatToParts' timeZoneName part, for the primary zones and some offset
// zones. It also records
// Intl.supportedValuesOf("timeZone") (s) and Intl.Locale's getTimeZones for
// every two-letter region and 001 (g).
"use strict";
const fs = require("fs");
const path = require("path");
const zlib = require("zlib");

const tables = fs.readFileSync(path.join(__dirname, "..", "intl_tz_tables.go"), "utf8");
const constOf = (name) => {
  const m = tables.match(new RegExp("const " + name + " = ((?:\"(?:[^\"\\\\]|\\\\.)*\"(?:\\s*\\+\\s*)?)+)"));
  if (!m) throw new Error("no " + name);
  return [...m[1].matchAll(/"((?:[^"\\]|\\.)*)"/g)].map((x) => x[1]).join("");
};
const ids = constOf("intlTZNames").split(";").filter((s) => s !== "");
// The primary identifiers: intlTZLinks' first uint16 of each identifier.
const links = constOf("intlTZLinks").replace(/\\x([0-9a-f]{2})/g, (_, h) => String.fromCharCode(parseInt(h, 16)))
  .replace(/\\(["\\])/g, "$1");
const primary = ids.filter((id, i) => (links.charCodeAt(4 * i) << 8 | links.charCodeAt(4 * i + 1)) === i);
const locales = ["en", "zh-Hans", "zh-Hant", "ja", "ko", "de", "fr", "es", "pt", "ru", "it"];
const styles = ["short", "long", "shortOffset", "longOffset", "shortGeneric", "longGeneric"];
const nameTimes = [
  "1800-01-01", "1950-01-15", "1950-07-15", "1969-12-31T12:00", "1975-01-15", "1975-07-15", "1990-01-15",
  "1990-07-15", "2000-01-15", "2000-07-15", "2010-01-15", "2010-07-15", "2018-01-15", "2018-07-15",
  "2026-01-15", "2026-07-15", "2026-10-22", "2027-01-15", "2027-07-15", "2045-01-15", "2045-07-15",
  "+010000-01-15", "+275760-07-15",
].map((s) => Date.parse(s.includes("T") ? s + "Z" : s + "T00:00Z"));
const farTimes = ["2100-01-15", "2100-07-15", "2500-01-15", "2500-07-15", "+010000-01-15", "+010000-07-15",
  "+100000-01-15", "+100000-07-15", "+275760-07-15"].map((s) => Date.parse(s + "T00:00Z")).concat([8.64e15]);

const offsetFormats = new Map();
const offsetOf = (zone, t) => {
  let f = offsetFormats.get(zone);
  if (!f) {
    f = new Intl.DateTimeFormat("en", { timeZone: zone, timeZoneName: "longOffset" });
    offsetFormats.set(zone, f);
  }
  const s = f.formatToParts(t).find((p) => p.type === "timeZoneName").value;
  if (s === "GMT") return 0;
  const m = s.match(/^GMT([+-])(\d\d):(\d\d)(?::(\d\d))?$/);
  if (!m) throw new Error(zone + " " + t + ": " + s);
  const v = (+m[2]) * 3600 + (+m[3]) * 60 + (+(m[4] || 0));
  return m[1] === "-" ? -v : v;
};

const out = { node: process.version, icu: process.versions.icu, tz: process.versions.tz, cldr: process.versions.cldr };
out.r = ids.map((id) => {
  try {
    return new Intl.DateTimeFormat("en", { timeZone: id }).resolvedOptions().timeZone;
  } catch (e) {
    return "!" + e.message;
  }
});
const week = 7 * 86400e3;
const start = Date.parse("1800-01-01T00:00Z"), end = Date.parse("2050-01-01T00:00Z");
out.o = {};
for (const zone of primary) {
  const tr = [];
  let prev = offsetOf(zone, start), t0 = start;
  for (let t = start + week; t <= end; t += week) {
    const o = offsetOf(zone, t);
    if (o !== prev) {
      // Bisect (t0, t] to the second.
      let lo = t0 / 1000, hi = t / 1000;
      while (hi - lo > 1) {
        const mid = Math.floor((lo + hi) / 2);
        if (offsetOf(zone, mid * 1000) === prev) lo = mid; else hi = mid;
      }
      tr.push([hi, offsetOf(zone, hi * 1000)]);
      prev = o;
    }
    t0 = t;
  }
  // The transitions as "seconds since the previous one (1800-01-01 for the
  // first):new offset" in base 36, ','-separated.
  let last = start / 1000;
  const t = tr.map(([sec, off]) => {
    const v = (sec - last).toString(36) + ":" + off.toString(36);
    last = sec;
    return v;
  }).join(",");
  out.o[zone] = {
    i: offsetOf(zone, -8.64e15), s: offsetOf(zone, start), t,
    f: farTimes.map((t) => [t / 1000, offsetOf(zone, t)]),
  };
}
// en, zh-Hans and ja at every instant, the other locales at every other one.
const timesOf = (loc) => ["en", "zh-Hans", "ja"].includes(loc) ? nameTimes : nameTimes.filter((t, i) => i % 2 === 1);
out.n = {};
for (const loc of locales) {
  // The names as indexes (base 36) into the locale's distinct names u, per
  // zone styles × the locale's instants t in order, ','-separated, a run of
  // n equal ones as index*n.
  const u = [], index = new Map(), z = {};
  const zones = primary.concat(["+05:30", "-03:00", "+00:00", "+13:45", "-09:30"]);
  for (const zone of zones) {
    const list = [];
    for (const timeZoneName of styles) {
      const f = new Intl.DateTimeFormat(loc, { timeZone: zone, timeZoneName });
      for (const t of timesOf(loc)) {
        const v = f.formatToParts(t).find((p) => p.type === "timeZoneName").value;
        if (!index.has(v)) {
          index.set(v, u.length);
          u.push(v);
        }
        list.push(index.get(v).toString(36));
      }
    }
    const runs = [];
    for (let i = 0; i < list.length;) {
      let j = i;
      while (j < list.length && list[j] === list[i]) j++;
      runs.push(j - i > 1 ? list[i] + "*" + (j - i) : list[i]);
      i = j;
    }
    z[zone] = runs.join(",");
  }
  out.n[loc] = { u, z, t: timesOf(loc) };
}
out.s = Intl.supportedValuesOf("timeZone");
out.g = {};
const A = "ABCDEFGHIJKLMNOPQRSTUVWXYZ";
for (const a of A) for (const b of A) {
  try {
    const l = new Intl.Locale("und-" + a + b);
    if (l.region !== a + b) continue; // a deprecated region, canonicalized
    const z = l.getTimeZones();
    if (z && z.length) out.g[a + b] = z;
  } catch (e) { /* not a region */ }
}
out.g["001"] = new Intl.Locale("und-001").getTimeZones();
fs.writeFileSync(path.join(__dirname, "intl_tz_icu.json.gz"), zlib.gzipSync(JSON.stringify(out), { level: 9 }));
