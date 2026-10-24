// Regenerates intl_decimal_node.json.gz, Node's (ICU's) Intl.NumberFormat
// output for random values and digit options, for TestIntlDecimalNode:
//
//	node engine/testdata/intl_decimal_node.js
//
// Each case is [kind, value, options, output]: kind "n" (a Number, value
// its Number::toString, "-0" for negative zero), "s" (a String) or "b" (a
// BigInt's decimal digits); options as "name:value" fields separated by
// spaces (useGrouping false and locale "en" are implied); output what
// format returns. The cases are drawn from a seeded generator, so a rerun
// with the same Node writes the same file.
"use strict";
const fs = require("fs");
const path = require("path");
const zlib = require("zlib");

let seed = 0x9e3779b9;
function rnd() { // mulberry32
  seed = (seed + 0x6d2b79f5) | 0;
  let t = seed;
  t = Math.imul(t ^ (t >>> 15), t | 1);
  t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
  return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
}
const int = (n) => Math.floor(rnd() * n);
const pick = (a) => a[int(a.length)];

const modes = ["ceil", "floor", "expand", "trunc", "halfCeil", "halfFloor", "halfExpand", "halfTrunc", "halfEven"];
const increments = [1, 2, 5, 10, 20, 25, 50, 100, 200, 250, 500, 1000, 2000, 2500, 5000];

function digits(n, alphabet = "0123456789999550") {
  let s = "";
  for (let i = 0; i < n; i++) s += alphabet[int(alphabet.length)];
  return s;
}

function numberCase() {
  let x;
  switch (int(6)) {
    case 0: x = (rnd() * 2 - 1) * 10 ** (int(50) - 25); break;
    case 1: x = Number((int(2) ? "-" : "") + digits(1 + int(6)) + "." + digits(int(6))); break;
    case 2: x = Number((int(2) ? "-" : "") + "0." + "0".repeat(int(12)) + digits(1 + int(8))); break;
    case 3: x = (int(2000001) - 1000000) / 10 ** int(8); break;
    case 4: x = pick([0, -0, 0.5, -0.5, 1.5, 2.5, 9.995, 999.5, 99.95, 1e21, 1e-7, 1.7976931348623157e308,
      5e-324, 2 ** 53, 2 ** 53 + 2, 123456789012345680000, 0.1 + 0.2, 1 / 3, 2 / 3]); break;
    default: x = new Float64Array(new BigUint64Array([BigInt(int(2 ** 32)) << 32n | BigInt(int(2 ** 32))]).buffer)[0];
  }
  return ["n", Object.is(x, -0) ? "-0" : String(x), x];
}

function stringCase() {
  let s;
  switch (int(4)) {
    case 0: s = (int(3) === 0 ? "-" : "") + digits(int(30)) + "." + digits(int(30)); break;
    case 1: s = (int(2) ? "-" : "") + digits(1 + int(25)) + (int(2) ? "." + digits(int(10)) : "") + "e" + (int(80) - 40); break;
    case 2: s = (int(2) ? "-" : "") + "0." + "0".repeat(int(40)) + digits(1 + int(25)); break;
    default: s = pick(["1e308", "-1e308", "1.797693134862315807937289714053e308", "1e400", "-1e400", "Infinity",
      "-Infinity", "NaN", "abc", "", " 12 ", "0x1F", "0b101", "0o17", "-0", "-0.0", ".5", "5.", "+.5e1",
      "1e-323", "0.1000000000000000000001", "123456789012345678901234567890", "9.995", "-9.995",
      "99999999999999999999.5", "0.00000000000000000000000000000123"]);
  }
  if (s.endsWith(".")) s += "0";
  return ["s", s, s];
}

function bigintCase() {
  const s = (int(3) === 0 ? "-" : "") + (1 + int(9)) + digits(int(60), "0123456789");
  return ["b", s, BigInt(s)];
}

function optionsCase() {
  const o = {};
  if (int(4)) o.roundingMode = pick(modes);
  switch (int(6)) {
    case 0: // fraction digits
      o.maximumFractionDigits = pick([0, 1, 2, 3, int(21), int(101)]);
      if (int(2)) o.minimumFractionDigits = int(o.maximumFractionDigits + 1);
      break;
    case 1: // significant digits
      o.maximumSignificantDigits = 1 + int(21);
      if (int(2)) o.minimumSignificantDigits = 1 + int(o.maximumSignificantDigits);
      break;
    case 2: // both, with a rounding priority
      o.roundingPriority = pick(["auto", "morePrecision", "lessPrecision"]);
      o.maximumFractionDigits = int(8);
      o.minimumFractionDigits = int(o.maximumFractionDigits + 1);
      o.maximumSignificantDigits = 1 + int(8);
      o.minimumSignificantDigits = 1 + int(o.maximumSignificantDigits);
      break;
    case 3: // an increment
      o.roundingIncrement = pick(increments.slice(1));
      o.maximumFractionDigits = o.minimumFractionDigits = int(6);
      break;
    case 4: // a priority with defaults
      o.roundingPriority = pick(["morePrecision", "lessPrecision"]);
      if (int(2)) o.maximumSignificantDigits = 1 + int(5); else o.minimumSignificantDigits = 1 + int(5);
      if (int(2)) o.maximumFractionDigits = int(5); else o.minimumFractionDigits = int(5);
      break;
    default: // the defaults
  }
  if (int(4) === 0) o.minimumIntegerDigits = 1 + int(21);
  if (int(4) === 0) o.trailingZeroDisplay = "stripIfInteger";
  return o;
}

// Fixed cases first: carries, ties and the rounding priorities' magnitudes.
const fixed = [
  ["n", 9.995, { minimumFractionDigits: 2, maximumFractionDigits: 2 }],
  ["n", 999.5, { maximumSignificantDigits: 3 }],
  ["n", 9.96, { roundingPriority: "morePrecision", maximumSignificantDigits: 2, minimumFractionDigits: 1, maximumFractionDigits: 1 }],
  ["n", 9.96, { roundingPriority: "lessPrecision", maximumSignificantDigits: 2, minimumFractionDigits: 1, maximumFractionDigits: 1 }],
  ["n", 99.5, { roundingPriority: "morePrecision", maximumSignificantDigits: 2, minimumFractionDigits: 1, maximumFractionDigits: 1 }],
  ["n", 99.95, { roundingPriority: "lessPrecision", maximumSignificantDigits: 3, minimumFractionDigits: 2, maximumFractionDigits: 2 }],
  ["n", 0.0995, { roundingPriority: "morePrecision", maximumSignificantDigits: 2, minimumFractionDigits: 3, maximumFractionDigits: 3 }],
  ["n", 0.0995, { roundingPriority: "lessPrecision", maximumSignificantDigits: 2, minimumFractionDigits: 3, maximumFractionDigits: 3 }],
  ["n", 1234.5, { maximumFractionDigits: 0, roundingMode: "halfEven" }],
  ["n", 1235.5, { maximumFractionDigits: 0, roundingMode: "halfEven" }],
  ["n", -0.0001, { maximumFractionDigits: 2 }],
  ["n", -0, { minimumSignificantDigits: 3 }],
  ["n", 1e21, { maximumFractionDigits: 3 }],
  ["n", 0.000123456, { maximumSignificantDigits: 3 }],
  ["n", 9.9e-11, { roundingIncrement: 2000, maximumFractionDigits: 15, minimumFractionDigits: 15, roundingMode: "halfEven" }],
  ["n", 1.375, { roundingIncrement: 25, maximumFractionDigits: 2, minimumFractionDigits: 2, roundingMode: "halfEven" }],
  ["n", 1.125, { roundingIncrement: 25, maximumFractionDigits: 2, minimumFractionDigits: 2, roundingMode: "halfEven" }],
  ["s", "99999.875", { roundingIncrement: 25, maximumFractionDigits: 2, minimumFractionDigits: 2 }],
  ["s", "123456789012345678901234567890", { maximumSignificantDigits: 21 }],
  ["s", "0.1000000000000000000001", { maximumFractionDigits: 22 }],
  ["s", "1e-400", { maximumSignificantDigits: 3 }],
  ["s", "-1e-400", { maximumFractionDigits: 3 }],
  ["s", "1e-323", { maximumSignificantDigits: 3 }],
  ["s", "2.4703282292062327e-324", { maximumSignificantDigits: 3 }],
  ["b", "123456789012345678901234567890", { maximumFractionDigits: 2 }],
];
const cases = [];
const seen = new Set();
while (cases.length < 6000) {
  let kind, text, value, o;
  if (cases.length < fixed.length) {
    [kind, value, o] = fixed[cases.length];
    text = kind === "n" && Object.is(value, -0) ? "-0" : String(value);
    if (kind === "b") value = BigInt(value);
  } else {
    const r = int(10);
    [kind, text, value] = r < 5 ? numberCase() : r < 8 ? stringCase() : bigintCase();
    o = optionsCase();
  }
  let out;
  try {
    out = new Intl.NumberFormat("en", { ...o, useGrouping: false }).format(value);
  } catch (e) {
    continue; // options the constructor rejects
  }
  const opts = Object.entries(o).map(([k, v]) => `${k}:${v}`).join(" ");
  const key = kind + text + opts;
  if (seen.has(key)) continue;
  seen.add(key);
  cases.push([kind, text, opts, out]);
}
const json = "[\n" + cases.map((c) => JSON.stringify(c)).join(",\n") + "\n]\n";
fs.writeFileSync(path.join(__dirname, "intl_decimal_node.json.gz"), zlib.gzipSync(json, { level: 9 }));
console.log(`${cases.length} cases, Node ${process.version}, ICU ${process.versions.icu}, CLDR ${process.versions.cldr}`);
