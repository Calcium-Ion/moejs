// Runs the Intl.NumberFormat workloads of bench/testdata/intl_numberformat.mjs
// in Node (ICU) and prints, for each, ns/op and ns per call, as
// BenchmarkIntlNumberFormat reports them for moejs:
//
//	LC_ALL=en_US.UTF-8 LANG=en_US.UTF-8 LANGUAGE= node bench/scripts/intl_number_node.mjs
import * as w from "../testdata/intl_numberformat.mjs";

function time(fn, minMs) {
  for (let i = 0; i < 3; i++) fn();
  let n = 1;
  for (;;) {
    const t0 = process.hrtime.bigint();
    for (let i = 0; i < n; i++) fn();
    const ns = Number(process.hrtime.bigint() - t0);
    if (ns >= minMs * 1e6) return ns / n;
    n *= 2;
  }
}

for (const name of ["formatJPY", "formatUSD", "formatDecimal", "partsJPY", "partsUSD", "partsDecimal", "constructJPY", "constructUSD", "constructDecimal"]) {
  const ns = time(w[name], 1000);
  const line = [name.padEnd(16), `${ns.toFixed(0)} ns/op`];
  if (!name.startsWith("construct")) line.push(`${(ns / w.count).toFixed(1)} ns/call`);
  console.log(line.join("\t"));
}
console.log(`node ${process.version} icu ${process.versions.icu}`);
