// Date probe shared by TestDateV8 and date_gen.js: probe() returns
// [key, value] pairs that date_gen.js records from Node (V8) once per time
// zone into date_v8.json.gz, and TestDateV8 compares with moejs in the same
// zone.
// toString and toTimeString drop the parenthesized zone name, which V8 takes
// from ICU ("Eastern Standard Time") and moejs from Go's tz database ("EST").
function probe() {
  var out = [];
  function enc(v) {
    return typeof v === "number" && v !== v ? "NaN" : v;
  }
  function add(key, f) {
    var v;
    try {
      v = enc(f());
    } catch (e) {
      v = "throws " + e.name;
    }
    out.push([key, v]);
  }
  function noZone(s) {
    return s.replace(/ \([^)]*\)$/, "");
  }

  var parses = [
    "Jan 2", "2", "2024", "1/2/3", "13/1/2024", "2024/13/1", "Jan 2 2024 10 PM",
    "Jan 2 2024 10.30", "foo2024", "foo 2024", "Jan 2 2024 foo", "Ja 2 2024",
    "2024-01-02T10:00+05", "2024-01-02T1:00", "T10:00", "20240102",
    "Jan 2 2024 -0800 10:00", "Jan 2 2024 10:00 +123456", "Jan 32 2024",
    "Jan 2 2024 25:00", "Jan 2 2024 13:00 PM", "2024-02-30", "2024-02-31T10:00",
    "2024-01-02T24:00:01", "-000000-01-01", "2024-1-2", "2024-01-02 10:00",
    "Jan 2 49", "Jan 2 50", "Jan 2 2024 GMT-8:30", "Jan 2 2024 10:00 +596523:00",
    "Jan 2 2024 10:00 +1193047:00", "Jan 2 2024 10:00 +0099", "Jan 2 2024 10.30.5",
    "Jan 2 2024 10:30.5", "Jan 2 2024 10:30:15.5", "Jan 2 2024 10:30:15.12345",
    "1 2 3 4", "Tue Jan 02 2024 10:05:07 GMT-0500 (Eastern Standard Time)",
    "Tue Jan 02 2024 10:05:07 GMT-0500", "Tue, 02 Jan 2024 15:05:07 GMT",
    "2024-01-02T15:05:07.009Z", "+275760-09-13T00:00:00.000Z",
    "+275760-09-13T00:00:00.001Z", "-271821-04-20T00:00:00.000Z",
    "-271821-04-19T23:59:59.999Z", "-271821-04-20T00:00:00.000+01:00",
    "-271821-04-20T00:00:00", "+275760-09-13T00:00:00",
    "Mon, 23 Sep 2024 10:20:30 GMT", "Mon, 23 Sep 2024 10:20:30 +0200",
    "23 Sep 2024 10:20:30 EST", "23 Sep 2024 10:20:30 pdt", "Sep 23 2024",
    "September 23, 2024", "23 September 2024 10:20", "2024/09/23",
    "2024/09/23 10:20:30", "9/23/2024", "9/23/2024 10:20 PM", "9/23/2024 12:00 AM",
    "9/23/2024 12:00 PM", "9/23/2024 0:30 am", "9/23/2024 13:30 am",
    "Monday, September 23, 2024 10:20:30 AM", "Mon Sep 23 2024 10:20:30 GMT+0530",
    "2024-09-23T10:20:30", "2024-09-23T10:20", "2024-09-23T10:20:30.1",
    "2024-09-23T10:20:30.123456789Z", "2024-09-23T10:20:30,5Z",
    "2024-09-23t10:20:30z", "2024-09-23T10:20:30+0200", "2024-09-23T10:20:30+02",
    "2024-09-23T10:20:30+24:00", "2024-09-23T10:20:30+23:59",
    "2024-09-23 10:20:30Z", "2024-09-23 10:20:30 +02:00", "2024-09-23T10:20:30 Z",
    "  2024-09-23  ", "2024-09-23 ", "(comment) Sep 23 2024",
    "Sep 23 2024 (Pacific Standard Time)", "Sep 23 (x (nested)) 2024",
    "Sep 23 2024 (unclosed", "Sep 23 2024)", "12:00", "Sep 23 2024 12:00:00 UTC+0100",
    "Sep 23 2024 12:00:00 UT", "Sep 23 2024 12:00:00 Z", "Sep 23 2024 12:00:00 z",
    "Sep 23 2024 12::", "Sep 23 2024 12::30", "Sep 23 2024 24:00", "Sep 23 2024 24:00:01",
    "Sep 23 2024 10:20:30 GMT+05:30", "Sep 23 2024 10:20:30 GMT+5",
    "Sep 23 2024 10:20:30 GMT+530", "Sep 23 2024 10:20:30 GMT-12:00",
    "Sep 23 2024 10:20:30 +24:00", "Sep 23 2024 10:20:30 +2400",
    "Sep 23 2024 10:20:30 +12345", "Sep 23 2024 10:20:30 GMT+", "Sep 23 2024 10:20:30 -",
    "0", "00", "000", "0000", "99", "100", "1000", "10000", "275760-09-13",
    "Sep 13 275760", "Sep 14 275760", "Apr 20 -271821", "Apr 20 271821",
    "Jan", "jan 2 2024", "JAN 2 2024", "Janxyz 2 2024", "Sat, 01-Jan-2000 08:00:00 GMT",
    "2000-01-01T00:00:00.000-00:00", "+002024-01-01", "-002024-01-01T00:00:00Z",
    "002024-01-01", "2024-01-01T00:00:00.Z", "2024-01-01T00:00:00.", "2024-01",
    "2024-00", "2024-01-00", "2024-01-32", "2024-1", "Jan 2 2024 10:00",
    "Jan 2 2024​10:00", "Jan 2 2024", "２０２４",
    "Jan 2 2024 10:00 é", "Jan 2 2024\u0000 10:00", "2024-01-02\u0000", "",
    " ", "-", "+", ":", "Z", "T", "2024 Jan 2", "2 2024 Jan", "Jan 2024 2", "2024 2 Jan",
    "Feb 29 2023", "Feb 29 2024", "Apr 31 2024", "1/32/2024", "12/31/1969 23:59:59.999 GMT",
    "1/1/1970 00:00:00 GMT-0000", "Jan 1 1970 00:00:00 gmt+1", "Jan 1 1970 est",
    "Jan 1 1970 10:00 EST+1", "Jan 1 1970 10:00 UTC-5:30", "Jan 2 2024 10:00:00:00",
    "Jan 2 2024 10 : 00", "Jan 2 2024 10:00 10:00", "Jan 2 2024 10:00 am pm",
    "1970-01-01T00:00:00.000+00:60", "1970-01-01T00:00:60Z", "1970-01-01T00:60Z",
    "999999999999-01-01", "Jan 1 999999999999", "Jan 1 123456789", "Jan 1 1234567890"
  ];
  for (var i = 0; i < parses.length; i++) {
    (function (s) {
      add("parse " + JSON.stringify(s), function () { return Date.parse(s); });
    })(parses[i]);
  }

  var tvs = [
    0, -1, 1e12, 1704189907009, -1e14, 8.64e15, -8.64e15, 8.64e15 - 1, -8.64e15 + 1,
    -62198755200000, -62135596800000, -62135596800001, -2208988800000,
    -4260211200000, -5000000000000, 1710053999999, 1710054000000, 1730613599999,
    1730613600000, 1730617200000, 1712415599999, 1712415600000, 1712417400000,
    1728142199999, 1728142200000, 1325152799999, 1325152800000, 1301752800000,
    1316872800000, 673200000000, 684000000000, -1325491557000, -1325491556000,
    -2840140800000, -2524521600000, 2e14, 4102444800000, 7e15, -3e15, 1e15 + 123
  ];
  var formats = ["toString", "toDateString", "toTimeString", "toUTCString",
    "toISOString", "toJSON", "toLocaleString", "toLocaleDateString",
    "toLocaleTimeString", "toGMTString"];
  var getters = ["getFullYear", "getMonth", "getDate", "getDay", "getHours",
    "getMinutes", "getSeconds", "getMilliseconds", "getUTCFullYear", "getUTCMonth",
    "getUTCDate", "getUTCDay", "getUTCHours", "getUTCMinutes", "getUTCSeconds",
    "getUTCMilliseconds", "getTimezoneOffset", "getYear", "getTime", "valueOf"];
  tvs.push(NaN);
  for (var j = 0; j < tvs.length; j++) {
    (function (tv) {
      var d = new Date(tv);
      add(tv + " formats", function () {
        return formats.map(function (m) {
          try {
            var s = d[m]();
            return m === "toString" || m === "toTimeString" ? noZone(s) : s;
          } catch (e) {
            return "throws " + e.name;
          }
        }).concat([noZone(String(d)), noZone(d + "")]).join(" | ");
      });
      add(tv + " getters", function () {
        return getters.map(function (m) { return String(d[m]()); }).concat([String(+d)]).join(",");
      });
    })(tvs[j]);
  }

  var ctors = [
    [2024, 0, 2, 10, 5, 7, 9], [2024, 2, 10, 2, 30], [2024, 2, 10, 3, 0],
    [2024, 10, 3, 1, 30], [2024, 10, 3, 0, 59, 59, 999], [2024, 10, 3, 2, 0],
    [2024, 3, 7, 1, 45], [2024, 3, 7, 2, 0], [2024, 9, 6, 2, 15], [2024, 9, 6, 2, 30],
    [2011, 11, 30, 12], [2011, 11, 29, 23, 59, 59], [2011, 11, 31], [2011, 3, 3, 3, 30],
    [1991, 3, 14, 2, 30], [1830, 0, 1], [1, 0, 1], [99, 0], [100, 0], [-1, 0],
    [0, 0], [275760, 8, 13], [275760, 8, 12, 23, 59, 59, 999], [275760, 8, 13, 0, 0, 0, 1],
    [-271821, 3, 20], [-271821, 3, 19, 23, 59, 59, 999], [-271821, 3, 20, 12],
    [2024, 24], [2024, -1], [2024, 0, 0], [2024, 0, 1, -1], [2024, 0, 1, 0, 0, 0, -1],
    [1e8, 0], [NaN, 0], [2024, NaN], [2024, 1.9, 2.9], [2024, 0, 1, 0, 0, 0, 0.9],
    [Infinity, 0], [-0.5, 0], [2024, 0, 1, 24], [2024, 0, 1e9], [2024, 1e10],
    [-1e8, 0], [2024, 0, 1, 0, 0, 0, 8.64e15], [2024, 0, 1, 0, 0, 0, -8.64e15],
    [1e16, 0], [2024, -1e16], ["2024", "1", "2"], [null, false, true], [2024]
  ];
  for (var k = 0; k < ctors.length; k++) {
    (function (a) {
      var key = JSON.stringify(a.map(function (x) { return String(x); }));
      add("new " + key, function () {
        var d = a.length === 1 ? new Date(a[0], undefined) : new Date(a[0], a[1], a[2] === undefined ? 1 : a[2],
          a[3] || 0, a[4] || 0, a[5] || 0, a[6] || 0);
        return d.getTime();
      });
      add("UTC " + key, function () { return Date.UTC.apply(null, a); });
    })(ctors[k]);
  }

  var bases = [1706695200000, 1710050400000, 1730611800000, NaN, 8.64e15];
  var sets = [
    ["setMonth", 1], ["setMonth", 1, 15], ["setMonth"], ["setMonth", NaN], ["setDate", 0],
    ["setDate", 31], ["setDate", 1e9], ["setFullYear", 2023], ["setFullYear", 2023, 5],
    ["setFullYear", 2023, 5, 6], ["setFullYear"], ["setFullYear", 99], ["setHours", 2],
    ["setHours", 25], ["setHours", 1, 30], ["setHours", 1, 2, 3, 4], ["setMinutes", -1],
    ["setMinutes", 30, 30], ["setSeconds", 3600], ["setMilliseconds", 1000],
    ["setMilliseconds", -1], ["setUTCHours", 0], ["setUTCHours", 2, 30],
    ["setUTCDate", 1], ["setUTCMonth", 11, 31], ["setUTCFullYear", 1970],
    ["setUTCFullYear", 1970, 0, 1], ["setUTCMinutes", 1, 2, 3], ["setUTCSeconds", 1, 2],
    ["setUTCMilliseconds", 5], ["setYear", 99], ["setYear", 2000], ["setYear", -1],
    ["setYear", NaN], ["setYear"], ["setTime", 5], ["setTime", 8.64e15 + 1],
    ["setTime"], ["setTime", "12"], ["setHours", 1.9]
  ];
  bases.forEach(function (b) {
    sets.forEach(function (s) {
      add(b + " " + JSON.stringify(s.map(String)), function () {
        var d = new Date(b);
        var r = d[s[0]].apply(d, s.slice(1));
        return [enc(r), enc(d.getTime())].join(" ");
      });
    });
  });

  // Coercion order and hints.
  add("order", function () {
    var log = [];
    function v(n, x) { return { valueOf: function () { log.push(n); return x; } }; }
    new Date(v("y", 2024), v("m", 0), v("d", 1), v("h", 0), v("min", 0), v("s", 0), v("ms", 0));
    var d = new Date(NaN);
    d.setHours(v("h", 1), v("min", 2), v("s", 3), v("ms", 4));
    d.setUTCMonth(v("M", 1), v("D", 2));
    Date.UTC(v("uy", 2024), v("um", 0));
    return log.join(",");
  });
  add("setter this check first", function () {
    var log = [];
    try {
      Date.prototype.setHours.call({}, { valueOf: function () { log.push("x"); return 1; } });
    } catch (e) { log.push(e.name); }
    return log.join(",");
  });
  add("toPrimitive", function () {
    var d = new Date(0);
    var p = Date.prototype[Symbol.toPrimitive];
    return [typeof p, p.name, p.length, typeof p.call(d, "number"),
      typeof p.call(d, "default"), typeof p.call(d, "string")].join(",");
  });
  add("toPrimitive bad hint", function () { return Date.prototype[Symbol.toPrimitive].call(new Date(0), "x"); });
  add("toPrimitive no hint", function () { return Date.prototype[Symbol.toPrimitive].call(new Date(0)); });
  add("toPrimitive non-object", function () { return Date.prototype[Symbol.toPrimitive].call(1, "number"); });
  add("toPrimitive generic", function () {
    return Date.prototype[Symbol.toPrimitive].call({ valueOf: function () { return 7; }, toString: function () { return "s"; } }, "default");
  });
  add("gmt is utc", function () { return Date.prototype.toGMTString === Date.prototype.toUTCString; });
  add("Date() type", function () { return typeof Date(); });
  add("Date(0) ignores args", function () { return Date(0) === Date(); });
  add("lengths", function () {
    return [Date.length, Date.UTC.length, Date.parse.length, Date.now.length,
      Date.prototype.setFullYear.length, Date.prototype.setMonth.length,
      Date.prototype.setDate.length, Date.prototype.setHours.length,
      Date.prototype.setMinutes.length, Date.prototype.setSeconds.length,
      Date.prototype.setMilliseconds.length, Date.prototype.setUTCHours.length,
      Date.prototype.setYear.length, Date.prototype.toJSON.length].join(",");
  });
  add("names", function () {
    return Object.getOwnPropertyNames(Date.prototype).sort().join(",");
  });
  add("ctor names", function () {
    return Object.getOwnPropertyNames(Date).sort().join(",");
  });
  add("new Date(date)", function () { var d = new Date(5); d.valueOf = function () { return 9; }; return new Date(d).getTime(); });
  add("new Date(string obj)", function () { return new Date(new String("2024-01-01")).getTime(); });
  add("new Date(toPrimitive)", function () {
    var o = {}; o[Symbol.toPrimitive] = function (h) { return h === "default" ? "1970-01-01T00:00:01Z" : 0; };
    return new Date(o).getTime();
  });
  add("date + 1", function () { return typeof (new Date(0) + 1); });
  add("date - 1", function () { return new Date(5) - 1; });
  add("date == str", function () { var d = new Date(0); return d == d.toString(); });
  add("date < date", function () { return new Date(1) < new Date(2); });
  add("JSON", function () { return JSON.stringify({ d: new Date(0), n: new Date(NaN) }); });
  add("toJSON generic", function () {
    return Date.prototype.toJSON.call({ valueOf: function () { return 1; }, toISOString: function () { return "x"; } });
  });
  add("toJSON infinite", function () { return Date.prototype.toJSON.call({ valueOf: function () { return Infinity; } }); });
  add("toJSON no toISOString", function () { return Date.prototype.toJSON.call({}); });
  return out;
}
