// The Intl.DateTimeFormat workloads of BenchmarkIntlDateTimeFormat and of
// bench/scripts/intl_dtf_node.mjs, which runs the same module in Node: each
// call formats 1,000 time values (one a minute from 2026-10-22T00:00Z) with
// a constructed formatter, or constructs 100 formatters.

const tokyo = new Intl.DateTimeFormat("en-US", {
  timeZone: "Asia/Tokyo", year: "numeric", month: "2-digit",
  day: "2-digit", hour: "2-digit", hourCycle: "h23",
});
const styles = new Intl.DateTimeFormat("en-US", { dateStyle: "medium", timeStyle: "short", timeZone: "UTC" });
const zh = new Intl.DateTimeFormat("zh-Hans", { dateStyle: "full", timeStyle: "medium", timeZone: "Asia/Shanghai" });
const t0 = Date.UTC(2026, 9, 22);
const N = 1000;

function formatAll(f) {
  let n = 0;
  for (let i = 0; i < N; i++) n += f.format(t0 + i * 60000).length;
  return n;
}

function partsAll(f) {
  let n = 0;
  for (let i = 0; i < N; i++) n += f.formatToParts(t0 + i * 60000).length;
  return n;
}

export function calls() { return N; }
export function tokyoFormat() { return formatAll(tokyo); }
export function tokyoParts() { return partsAll(tokyo); }
export function stylesFormat() { return formatAll(styles); }
export function stylesParts() { return partsAll(styles); }
export function zhFormat() { return formatAll(zh); }
export function range() {
  let n = 0;
  for (let i = 0; i < N; i++) n += styles.formatRange(t0, t0 + i * 3600000).length;
  return n;
}
export function construct() {
  let n = 0;
  for (let i = 0; i < 100; i++) {
    n += new Intl.DateTimeFormat("en-US", {
      timeZone: "Asia/Tokyo", year: "numeric", month: "2-digit",
      day: "2-digit", hour: "2-digit", hourCycle: "h23",
    }).resolvedOptions().locale.length;
  }
  return n;
}
export function constructStyles() {
  let n = 0;
  for (let i = 0; i < 100; i++) n += new Intl.DateTimeFormat("de", { dateStyle: "long", timeStyle: "short" }).resolvedOptions().locale.length;
  return n;
}
