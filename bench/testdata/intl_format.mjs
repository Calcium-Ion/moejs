// The Intl formatting workloads of BenchmarkIntlFormat and
// bench/scripts/intl_node.mjs: format and formatToParts of a currency
// NumberFormat (GitHub issue #4's ja-JP yen), a decimal one and a
// DateTimeFormat with a time zone (the issue's Asia/Tokyo parts) and with
// date and time styles, the toLocaleString of a number and of a Date without
// arguments, and the construction of each formatter. Each function formats
// N values (constructs once) and returns N.
const N = 100;
const numbers = Array.from({ length: N }, (_, i) => 1250 + i * 37.25);
const times = Array.from({ length: N }, (_, i) => Date.UTC(2026, 9, 22) + i * 27e6);
const dates = times.map(t => new Date(t));

const yen = new Intl.NumberFormat("ja-JP", { style: "currency", currency: "JPY" });
const dollars = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD" });
const decimal = new Intl.NumberFormat("de-DE", { maximumFractionDigits: 2 });
const tokyo = new Intl.DateTimeFormat("en-US", {
  timeZone: "Asia/Tokyo", year: "numeric", month: "2-digit",
  day: "2-digit", hour: "2-digit", hourCycle: "h23",
});
const styled = new Intl.DateTimeFormat("ja-JP", { dateStyle: "medium", timeStyle: "short", timeZone: "Asia/Tokyo" });

let sink = 0;
function each(list, fn) {
  for (const v of list) sink += fn(v).length;
  return list.length;
}

export const yenFormat = () => each(numbers, v => yen.format(v));
export const yenParts = () => each(numbers, v => yen.formatToParts(v));
export const dollarFormat = () => each(numbers, v => dollars.format(v));
export const decimalFormat = () => each(numbers, v => decimal.format(v));
export const decimalParts = () => each(numbers, v => decimal.formatToParts(v));
export const tokyoFormat = () => each(times, t => tokyo.format(t));
export const tokyoParts = () => each(times, t => tokyo.formatToParts(t));
export const styledFormat = () => each(times, t => styled.format(t));
export const numberToLocale = () => each(numbers, v => v.toLocaleString());
export const dateToLocale = () => each(dates, d => d.toLocaleString());
export function constructNumber() {
  sink += new Intl.NumberFormat("ja-JP", { style: "currency", currency: "JPY" }).resolvedOptions().minimumFractionDigits;
  return 1;
}
export function constructDate() {
  sink += new Intl.DateTimeFormat("en-US", { timeZone: "Asia/Tokyo", year: "numeric", month: "2-digit", day: "2-digit" }).resolvedOptions().timeZone.length;
  return 1;
}
export const workloads = ["yenFormat", "yenParts", "dollarFormat", "decimalFormat", "decimalParts", "tokyoFormat", "tokyoParts", "styledFormat", "numberToLocale", "dateToLocale", "constructNumber", "constructDate"];
