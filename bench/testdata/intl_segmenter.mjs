// The Intl.Segmenter workloads of BenchmarkIntlSegmenter and of
// bench/scripts/intl_node.mjs, which runs the same module in Node: a text of
// 64 KiB (UTF-8) mixing Chinese, Japanese, Korean, Latin with combining
// marks, numbers, URLs and emoji sequences, segmented at each granularity,
// and the construction of a segmenter.

const base =
  "The quick brown fox jumps over the lazy dog — café, naïve, jalapeño, éä. " +
  "你好，世界！这是一个测试句子。" +
  "日本語のテキストも混ぜます。カタカナとひらがな。" +
  "한국어 문장도 있습니다. " +
  "Emoji: 👨‍👩‍👧‍👦 👍🏽 🇯🇵🇨🇳 ❤️ 1️⃣! " +
  "Pi is 3.14 and a thousand is 1,000; see https://example.com/a?q=1 for more. ";

function utf8Length(s) {
  let n = 0;
  for (const c of s) {
    const cp = c.codePointAt(0);
    n += cp < 0x80 ? 1 : cp < 0x800 ? 2 : cp < 0x10000 ? 3 : 4;
  }
  return n;
}

let t = "";
while (utf8Length(t) < 65536) t += base;
export const text = t;
export const bytes = utf8Length(t);
export function textBytes() { return bytes; }

const grapheme = new Intl.Segmenter("en", { granularity: "grapheme" });
const word = new Intl.Segmenter("en", { granularity: "word" });
const sentence = new Intl.Segmenter("en", { granularity: "sentence" });

function count(segmenter) {
  let n = 0;
  for (const s of segmenter.segment(text)) n += s.segment.length > 0 ? 1 : 0;
  return n;
}

export function graphemes() { return count(grapheme); }
export function words() { return count(word); }
export function sentences() { return count(sentence); }

// spread is the issue's pattern: [...segmenter.segment(s)].length.
export function spread() { return [...grapheme.segment(text)].length; }

// construct is what a module that segments does at load.
export function construct() {
  return new Intl.Segmenter("zh-Hans", { granularity: "grapheme" }) ? 1 : 0;
}
