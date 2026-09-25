export function run() {
  const s = "  Hello, Wörld! 日本語テキスト  ";
  const t = s.trim();
  return {
    len: s.length, trimmed: t, upper: t.toUpperCase(), lower: t.toLowerCase(),
    parts: t.split(/[,!\s]+/), joined: ["a", "b", "c"].join("-"),
    idx: [t.indexOf("W"), t.lastIndexOf("o"), t.indexOf("zzz")],
    slices: [t.slice(0, 5), t.slice(-6), t.substring(7, 12), t.substr(1, 3), t.at(-1), t.charAt(0), t.charCodeAt(8), t.codePointAt(15)],
    pad: ["7".padStart(3, "0"), "ab".padEnd(5, ".-"), "x".repeat(4)],
    includes: [t.includes("Wörld"), t.startsWith("Hello"), t.endsWith("ト"), "abc".localeCompare("abd")],
    replaced: [t.replace("l", "L"), t.replaceAll("l", "L"), t.replace(/o/g, "0"), "a-b-c".replace(/-(\w)/g, (m, c) => c.toUpperCase()), "x1y22".replace(/\d+/g, "$&$&"), "John Smith".replace(/(?<first>\w+)\s(?<last>\w+)/, "$<last>, $<first>")],
    template: `${t.length} chars, ${1 + 2} sum, ${[1, 2].map(x => x * 2)}`,
    concat: "a".concat("b", 1, null, undefined), fromCode: String.fromCharCode(72, 105, 0x1F600 & 0xFFFF),
    surrogate: ["😀".length, "😀".codePointAt(0), [..."a😀b"].length, "😀".charCodeAt(0), "😀".charCodeAt(1)],
    number: [String(1.5), String(-0), String(1e21), String(0.000001), String(1e-7), (255).toString(16), (0.1 + 0.2).toString(), (1234.5678).toFixed(2), (1234.5678).toPrecision(6), (1234.5678).toExponential(2), String(NaN), String(Infinity)],
    coerce: [1 + "2", "3" * "4", "5" - 2, "x" + null + undefined + true + 1.5 + [1, 2] + {}],
    trimVariants: ["\t a \n".trimStart(), "\t a \n".trimEnd(), " ﻿ a".trim()],
  };
}
