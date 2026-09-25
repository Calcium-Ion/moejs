// Regenerates date_v8.json.gz from Node:
//
//	node engine/testdata/date_gen.js
//
// It runs date_probe.js in a fresh Node process per time zone (TZ set) and
// records Node's version; TestDateV8 checks every zone recorded.
"use strict";
const { execFileSync } = require("child_process");
const path = require("path");
const zones = ["UTC", "America/New_York", "Asia/Shanghai", "Australia/Lord_Howe",
  "Pacific/Apia", "Asia/Kolkata", "Europe/Dublin"];
if (process.argv[2] === "--run") {
  const src = require("fs").readFileSync(path.join(__dirname, "date_probe.js"), "utf8");
  process.stdout.write(JSON.stringify(new Function(src + "\nreturn probe();")()));
  return;
}
const out = { node: process.version, zones: {} };
for (const tz of zones) {
  const res = execFileSync(process.execPath, [__filename, "--run"], { env: { TZ: tz }, maxBuffer: 1 << 26 });
  out.zones[tz] = JSON.parse(res.toString());
}
const json = JSON.stringify(out).replace(/\],\[/g, "],\n[") + "\n";
require("fs").writeFileSync(path.join(__dirname, "date_v8.json.gz"), require("zlib").gzipSync(json, { level: 9 }));
