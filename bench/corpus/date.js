// zone: America/New_York
// Date in a fixed zone with a DST rule and pre-1883 LMT, compared with Sobek.
// Left out where Sobek differs from V8 and moejs follows V8 (engine
// TestDateV8 checks these against Node): a local time in a DST gap resolves
// with the offset before the transition (2:30 on 2026-03-08 is 3:30 EDT,
// Sobek 1:30 EST); toISOString uses 6-digit years outside 0..9999
// ("-000001-...", "+010000-...", Sobek 4/5 digits); Date.parse accepts a
// lowercase "t"/"z" in ISO strings and rejects a bare 13-digit number
// ("1700000000000", Sobek year 1700000000000 clipped); getTimezoneOffset is
// whole minutes under an LMT offset with seconds (Sobek 296.0333...);
// toGMTString is the same function as toUTCString; getYear/setYear exist.
const num = (x) => (typeof x === "number" && x !== x ? "NaN" : x);
const all = (d) => [d.getTime(), d.getFullYear(), d.getMonth(), d.getDate(), d.getDay(), d.getHours(), d.getMinutes(), d.getSeconds(), d.getMilliseconds(), Math.round(d.getTimezoneOffset()), d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate(), d.getUTCDay(), d.getUTCHours(), d.getUTCMinutes(), d.getUTCSeconds(), d.getUTCMilliseconds()].map(num);
const strs = (d) => { const out = []; for (const m of ["toString", "toDateString", "toTimeString", "toISOString", "toUTCString", "toJSON"]) { try { out.push(String(d[m]())); } catch (e) { out.push(e.name); } } return out; };
export function run() {
  const r = {};
  r.ctorLocal = [new Date(2026, 8, 23, 10, 20, 30, 456), new Date(2026, 0), new Date(99, 11, 31), new Date(0, 0), new Date(100, 0), new Date(-1, 0), new Date(2026, 13, 40, 25, 61, 61, 1001), new Date(2026, -1), new Date(275760, 8, 13), new Date(-271821, 3, 20)].map(all);
  r.dstGap = [new Date(2026, 2, 8, 1, 59, 59, 999), new Date(2026, 2, 8, 3, 0)].map(all);
  r.dstOverlap = [new Date(2026, 10, 1, 1, 30), new Date(2026, 10, 1, 0, 59), new Date(2026, 10, 1, 2, 0)].map(all);
  r.dstInstants = [Date.UTC(2026, 2, 8, 6, 59, 59, 999), Date.UTC(2026, 2, 8, 7), Date.UTC(2026, 10, 1, 5, 59, 59, 999), Date.UTC(2026, 10, 1, 6)].map((t) => all(new Date(t)).concat(strs(new Date(t))));
  r.historic = [new Date(1883, 10, 18, 12), new Date(1900, 0, 1), new Date(1945, 7, 14, 12), new Date(1970, 0, 1), new Date(1974, 0, 6, 12), new Date(2007, 2, 11, 12), new Date(2038, 0, 19, 3, 14, 8), new Date(2100, 6, 4)].map((d) => all(d).concat(strs(d)));
  r.utc = [Date.UTC(2026), Date.UTC(2026, 8), Date.UTC(2026, 8, 23, 10, 20, 30, 456), Date.UTC(99, 0), Date.UTC(100, 0), Date.UTC(-1, 0), Date.UTC(2026, 0, 1, 0, 0, 0, 0.9), Date.UTC(NaN), Date.UTC(275760, 8, 13), Date.UTC(275760, 8, 13, 0, 0, 0, 1), Date.UTC(1970, 0, 1, 0, 0, 0, -0.5)].map(num);
  r.args = [new Date(null), new Date(true), new Date(""), new Date(undefined), new Date(NaN), new Date(8.64e15), new Date(8.64e15 + 1), new Date(-8.64e15), new Date(1.5), new Date(-1.5), new Date(new Date(123)), new Date({ valueOf() { return 42; } }), new Date({ toString() { return "2026-01-02"; }, valueOf: undefined })].map((d) => num(d.getTime()));
  r.strings = [new Date(1700000000123), new Date(0), new Date(-1), new Date(8.64e15), new Date(-8.64e15), new Date(NaN), new Date(-2208988800000), new Date(1e12)].map(strs);
  r.farYears = [new Date(-62198755200000), new Date(-62198755200001), new Date(253402300800000)].map((d) => [d.toString(), d.toDateString(), d.toTimeString(), d.toUTCString(), d.getFullYear(), d.getUTCFullYear()]);
  r.parseIso = ["2026-09-23", "2026-09", "2026", "2026-09-23T10:20", "2026-09-23T10:20:30", "2026-09-23T10:20:30.4", "2026-09-23T10:20:30.456", "2026-09-23T10:20:30.456789", "2026-09-23T10:20:30Z", "2026-09-23T10:20:30+05:30", "2026-09-23T10:20:30-0800", "+002026-09-23T00:00:00Z", "-000001-01-01T00:00:00Z", "-000000-01-01T00:00:00Z", "2026-09-23T24:00", "2026-09-23T24:00:01", "2026-02-30", "2026-13-01", "2026-09-23 10:20:30", "2026-3-8", "2026-11-01T01:30:00", "275760-09-13T00:00:00Z", "+275760-09-13T00:00:00.001Z"].map((s) => num(Date.parse(s)));
  r.parseLegacy = ["Sep 23 2026", "September 23, 2026 10:20:30", "23 Sep 2026 10:20:30 GMT", "Wed, 23 Sep 2026 10:20:30 GMT", "Wed Sep 23 2026 10:20:30 GMT-0400 (Eastern Daylight Time)", "Wed Sep 23 2026 10:20:30 GMT+0200", "2026/09/23", "2026/09/23 10:20", "09/23/2026", "9/23/2026 10:20 PM", "Sep 23 2026 10:20:30 UTC", "Sep 23 2026 10:20:30 EST", "Sep 23 2026 10:20:30 PDT", "Thu, 01 Jan 1970 00:00:00 GMT", "Jan 1 1970 00:00:00 GMT+0100", "not a date", "", "2026-09-23T10:20:30.456Z junk", "12:00", "Sep 2026"].map((s) => num(Date.parse(s)));
  const d0 = new Date(1700000000123);
  r.roundTrip = [Date.parse(d0.toString()), Date.parse(d0.toUTCString()), Date.parse(d0.toISOString()), Date.parse(new Date(2026, 2, 8, 3, 30).toString()), Date.parse(new Date(2026, 10, 1, 1, 30).toString())].map(num);
  const set = (f) => { const d = new Date(2026, 0, 31, 12, 0, 0, 0); const ret = f(d); return [num(ret), num(d.getTime())]; };
  r.setters = [
    set((d) => d.setMonth(1)), set((d) => d.setMonth(1, 15)), set((d) => d.setDate(0)), set((d) => d.setDate(400)), set((d) => d.setFullYear(2024, 1, 29)), set((d) => d.setFullYear(2025, 1, 29)), set((d) => d.setHours(25)), set((d) => d.setHours(-1, 30, 30, 500)), set((d) => d.setMinutes(90)), set((d) => d.setSeconds(-1)), set((d) => d.setMilliseconds(1e6)), set((d) => d.setTime(5)), set((d) => d.setTime(NaN)), set((d) => d.setTime("12")), set((d) => d.setUTCHours(3)), set((d) => d.setUTCDate(1)), set((d) => d.setUTCMonth(11, 31)), set((d) => d.setUTCFullYear(2000)), set((d) => d.setUTCMinutes(1, 2, 3)), set((d) => d.setUTCSeconds(59, 999)), set((d) => d.setUTCMilliseconds(-1)), set((d) => d.setHours(NaN)), set((d) => d.setMonth()), set((d) => d.setHours(1, 30) && d.setMonth(10, 1)), set((d) => d.setFullYear(8.64e15)),
  ];
  const bad = new Date(NaN);
  r.setOnNaN = [bad.setFullYear(2026), new Date(NaN).setMonth(1), new Date(NaN).setUTCFullYear(2026, 5), new Date(NaN).setHours(1), new Date(NaN).setTime(7)].map(num);
  r.coercion = [typeof (d0 + 1), (d0 + 1).slice(-1), d0 - 0, +d0, d0 * 1, d0 > new Date(0), d0 == d0.toString(), d0 == d0.getTime(), `${d0}` === d0.toString(), JSON.stringify({ d: d0, n: new Date(NaN) }), JSON.stringify([new Date(-1)]), String(d0) === d0.toString(), [d0].join() === d0.toString()];
  r.toJSONGeneric = [Date.prototype.toJSON.call({ toISOString() { return "iso!"; } }), Date.prototype.toJSON.call({ valueOf() { return NaN; }, toISOString() { return "x"; } }), (() => { try { return Date.prototype.toJSON.call({}); } catch (e) { return e.name; } })()];
  r.brand = ["toString", "getTime", "valueOf", "toISOString", "getFullYear", "setTime", "toUTCString", "toDateString"].map((m) => { try { return String(Date.prototype[m].call({})); } catch (e) { return e.name; } });
  r.protoIsDate = (() => { try { return Date.prototype.getTime(); } catch (e) { return e.name; } })();
  r.meta = [Date.length, Date.name, typeof Date.now(), Date.now() > 1.7e12, typeof Date(), Date(2020, 1) === Date(), Object.prototype.toString.call(d0), Date.prototype.constructor === Date, Object.getPrototypeOf(d0) === Date.prototype, d0 instanceof Date, Date.UTC.length, Date.parse.length, Date.prototype.setHours.length, Date.prototype.setUTCFullYear.length, Object.keys(d0).length, Object.prototype.toString.call(Date.prototype)];
  r.locale = [typeof d0.toLocaleString(), typeof d0.toLocaleDateString(), typeof d0.toLocaleTimeString()];
  r.sortDates = [new Date(3), new Date(1), new Date(2)].sort((a, b) => a - b).map((d) => d.getTime());
  r.subclassLike = (() => { function MyDate() { const d = new Date(0); Object.setPrototypeOf(d, MyDate.prototype); return d; } MyDate.prototype = Object.create(Date.prototype); const m = new MyDate(); return [m.getTime(), m instanceof Date, m.toISOString()]; })();
  r.exported = d0;
  r.exportedNested = { when: new Date(0), list: [new Date(Date.UTC(2026, 6, 1, 12))] };
  return r;
}
