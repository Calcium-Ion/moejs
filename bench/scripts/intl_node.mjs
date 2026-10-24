// Runs the Intl.Segmenter workloads of bench/testdata/intl_segmenter.mjs in
// Node (ICU) and prints, for each, ns/op, MB/s of UTF-8 text and ns per
// segment, as BenchmarkIntlSegmenter reports them for moejs:
//
//	node bench/scripts/intl_node.mjs
import * as w from "../testdata/intl_segmenter.mjs";

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

for (const name of ["graphemes", "words", "sentences", "spread", "construct"]) {
  const segs = w[name]();
  const ns = time(w[name], 1000);
  const line = [name.padEnd(10), `${ns.toFixed(0)} ns/op`];
  if (name !== "construct") line.push(`${(w.textBytes() / ns * 1e3).toFixed(1)} MB/s`, `${(ns / segs).toFixed(1)} ns/segment`, `${segs} segments`);
  console.log(line.join("\t"));
}
console.log(`node ${process.version} icu ${process.versions.icu} text ${w.textBytes()} bytes`);
