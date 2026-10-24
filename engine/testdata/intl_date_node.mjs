// Writes intl_date_node.json.gz, the outputs of Node 24 (ICU 78.3, CLDR
// 48) for the cases of intl_date_cases.js, which TestIntlDateTimeFormatNode
// compares with moejs's. Run from engine/testdata with the environment's
// locale and time zone pinned:
//
//	LC_ALL=en_US.UTF-8 LANG=en_US.UTF-8 LANGUAGE= TZ=UTC node intl_date_node.mjs
import { createHash } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";
import { runInThisContext } from "node:vm";
import { gzipSync } from "node:zlib";

const out = runInThisContext(readFileSync("intl_date_cases.js", "utf8"));
// The outputs only, with a digest of the case names, which the test makes
// itself.
const names = createHash("sha256").update(out.map((c) => c[0]).join("\n")).digest("hex");
const json = JSON.stringify({ node: process.versions.node, icu: process.versions.icu, names, outputs: out.map((c) => c[1]) });
writeFileSync("intl_date_node.json.gz", gzipSync(json, { level: 9 }));
console.log(out.length, "cases");
