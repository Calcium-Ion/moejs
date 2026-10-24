// Runs the Intl formatting workloads of bench/testdata/intl_format.mjs in
// Node (ICU) and prints ns/op and ns per formatted value for each, as
// BenchmarkIntlFormat reports them for moejs. Run it in UTC with the en-US
// locale, as the Go benchmark runs:
//
//	TZ=UTC LC_ALL=en_US.UTF-8 LANG=en_US.UTF-8 node bench/scripts/intl_format_node.mjs
import * as w from "../testdata/intl_format.mjs";

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

for (const name of w.workloads) {
  const count = w[name]();
  const ns = time(w[name], 1000);
  console.log([name.padEnd(16), `${ns.toFixed(0)} ns/op`, `${(ns / count).toFixed(1)} ns/value`].join("\t"));
}
console.log(`node ${process.version} icu ${process.versions.icu} tz ${process.versions.tz}`);
