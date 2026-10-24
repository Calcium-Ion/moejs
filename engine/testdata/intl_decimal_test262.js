// Prints test262's FormatNumericToString tables (intl402/NumberFormat/
// prototype/format/format-{rounding,fraction-digits,significant-digits,
// max-min-fraction-significant}*.js) as Go table entries.
const fs = require("fs"), path = require("path"), vm = require("vm");
const dir = process.argv[2];
const files = fs.readdirSync(dir).filter(f =>
  /^format-(rounding-|fraction-digits|significant-digits|max-min-fraction-significant)/.test(f)).sort();
const out = [];
for (const f of files) {
  const src = fs.readFileSync(path.join(dir, f), "utf8");
  const found = [];
  const ctx = { Intl, Object, JSON, testNumberFormat(locales, ns, options, data) { found.push([options, data]); } };
  vm.runInNewContext(src, ctx);
  out.push(`\t// ${f}`);
  for (const [options, data] of found) {
    const opts = Object.entries(options).filter(([k]) => k !== "useGrouping").map(([k, v]) => `${k}:${v}`).join(" ");
    const pairs = Object.entries(data).map(([k, v]) => `${JSON.stringify(k)}, ${JSON.stringify(v)}`).join(", ");
    out.push(`\t{${JSON.stringify(opts)}, []string{${pairs}}},`);
  }
}
console.log(out.join("\n"));
