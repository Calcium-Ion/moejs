// Runs the Intl.DateTimeFormat workloads of
// bench/testdata/intl_datetimeformat.mjs in Node (ICU) and prints, for each,
// ns/op and ns per call, as BenchmarkIntlDateTimeFormat reports them for
// moejs:
//
//	LC_ALL=en_US.UTF-8 TZ=UTC node bench/scripts/intl_dtf_node.mjs
import * as w from "../testdata/intl_datetimeformat.mjs";

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

for (const name of ["tokyoFormat", "tokyoParts", "stylesFormat", "stylesParts", "zhFormat", "range", "construct", "constructStyles"]) {
  const ns = time(w[name], 1000);
  const calls = name.startsWith("construct") ? 100 : w.calls();
  console.log([name.padEnd(16), `${ns.toFixed(0)} ns/op`, `${(ns / calls).toFixed(1)} ns/call`].join("\t"));
}
console.log(`node ${process.version} icu ${process.versions.icu}`);
