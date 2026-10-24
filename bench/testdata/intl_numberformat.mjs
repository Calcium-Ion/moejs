// The Intl.NumberFormat workloads of BenchmarkIntlNumberFormat and of
// bench/scripts/intl_number_node.mjs, which runs the same module in Node:
// format and formatToParts of 1,000 numbers with a currency formatter (ja-JP
// JPY, the issue's, and en-US USD) and a decimal one, and the construction
// of each formatter.

const values = [];
for (let i = 0; i < 1000; i++) values.push(((i * 7919) % 100003) / 7 + i);
export const count = values.length;
export function perOp() { return count; }

const jpy = new Intl.NumberFormat("ja-JP", { style: "currency", currency: "JPY" });
const usd = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD" });
const dec = new Intl.NumberFormat("en-US");

function format(nf) {
  let n = 0;
  for (let i = 0; i < values.length; i++) n += nf.format(values[i]).length;
  return n;
}

function parts(nf) {
  let n = 0;
  for (let i = 0; i < values.length; i++) n += nf.formatToParts(values[i]).length;
  return n;
}

export function formatJPY() { return format(jpy); }
export function formatUSD() { return format(usd); }
export function formatDecimal() { return format(dec); }
export function partsJPY() { return parts(jpy); }
export function partsUSD() { return parts(usd); }
export function partsDecimal() { return parts(dec); }

// The constructions build one formatter per op.
export function constructJPY() { return new Intl.NumberFormat("ja-JP", { style: "currency", currency: "JPY" }) ? 1 : 0; }
export function constructUSD() { return new Intl.NumberFormat("en-US", { style: "currency", currency: "USD" }) ? 1 : 0; }
export function constructDecimal() { return new Intl.NumberFormat("en-US") ? 1 : 0; }
