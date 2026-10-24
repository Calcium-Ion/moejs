// Regenerates seg_icu.json.gz, Node's (ICU's) Intl.Segmenter segmentation
// of a corpus, for TestSegICU:
//
//	node engine/testdata/seg_icu.js
//
// The corpus is every string of the three UAX #29 break tests (the trimmed
// copies go generate writes here), the lines and the whole of
// seg_sample.txt, and the strings below. For each it records the ends of the
// grapheme (g), word (w) and sentence (t) segments of the "en" segmenters
// in UTF-16 code units, and the word segments' isWordLike flags (l, "1" or
// "0" per segment).
"use strict";
const fs = require("fs");
const path = require("path");
const zlib = require("zlib");

const corpus = [];
for (const file of ["grapheme_break_test.txt.gz", "word_break_test.txt.gz", "sentence_break_test.txt.gz"]) {
  const text = zlib.gunzipSync(fs.readFileSync(path.join(__dirname, file))).toString();
  for (const line of text.split("\n")) {
    if (line === "" || line.startsWith("#")) continue;
    const cps = line.split(/\s+/).filter((f) => f !== "÷" && f !== "×" && f !== "");
    corpus.push(String.fromCodePoint(...cps.map((h) => parseInt(h, 16))));
  }
}
const sample = fs.readFileSync(path.join(__dirname, "seg_sample.txt"), "utf8");
corpus.push(sample, ...sample.split("\n").filter((l) => l !== ""));
corpus.push(
  // Prose, numbers, contractions, URLs and quotes.
  "The quick brown fox can't jump over 3.14 lazy dogs, can it? Yes! It costs $1,000.50 (approx.) today.",
  "In the U.S.A. we'd say \"hello\" to Mr. Smith. He said, 'It's fine.' She replied: \"No.\" Then... nothing.",
  "Visit https://example.com/path?q=1&x=2#frag or mail john.doe@example.org now.",
  "Version 2.0.1-beta_3 released on 2024-05-06T12:00Z. I.B.M. and A.T.&T. merged, etc. i.e. next.",
  "3.14 1,000 1,000.5 1.2.3 10% -5 +7 1e10 0x1F ½ ² ① 3a A3 a.b 1.a a.1 1'2 a'b a''b a..b 1,,2",
  "don't won't y'all rock'n'roll 'quoted' o'clock dogs' ‘single’ “double” «guillemets» „low“",
  "Really?! Sure!? (Hello.) World. \"Stop.\" he said. etc. and more. Mr. jones. 1. Item\n2. Item",
  "Is it?\nYes.\r\nNo. Para. Line.\u0085Next.\vTab.\fFeed.",
  "a. B a. B a.  b a.)  b a.\" B ?! ¿Qué? ¡Hola! 。と！ 「はい。」 Ｈｅｌｌｏ．Ｗｏｒｌｄ",
  // Emoji: ZWJ families, flags, skin tones, keycaps.
  "👨‍👩‍👧‍👦 family 🏳️‍🌈 flag 🇯🇵🇺🇸🇫🇷 flags 👍🏽👍🏿 tones 1️⃣ #️⃣ *️⃣ keycaps ❤️‍🔥 🧑🏽‍💻 😀😃😄 a👍b x‍😀",
  "🇯🇵🇺🇸🇫 🏴󠁧󠁢󠁳󠁣󠁴󠁿 🫱🏻‍🫲🏿 ©️ ®️ ™ ℹ️ ↔️ ✌️ ☺ 🀄 🃏",
  // Hangul, Japanese, Chinese.
  "안녕하세요. 저는 학생입니다! 한국어 텍스트 분할 테스트. 가각 갂 각",
  "日本語のテキストを分割します。カタカナとひらがな、漢字が混在しています！ラーメン、東京タワー333m。",
  "中文分词测试：你好，世界！这是一个句子。另一个句子？繁體中文。Go语言和JavaScript。",
  // Arabic, Hebrew with quotes.
  "مرحبا بالعالم. هذه جملة عربية؟ نعم! ١٢٣ ٤٫٥ ١٬٠٠٠",
  "צה\"ל וארה\"ב, ג'ירפה ו־שלום. שלום עולם. מה שלומך? א'ב א\"ב א׳ב",
  // Devanagari and Bengali conjuncts, Tamil, Thai, Lao, Khmer, Myanmar.
  "नमस्ते दुनिया। क्षत्रिय और ज्ञान। क्‍ष क्‌ष स्त्री द्विवेदी",
  "বাংলা ভাষা। ক্ষমা। ক্ষ্ম স্ত্রী தமிழ் மொழி. ಕನ್ನಡ ಭಾಷೆ.",
  "สวัสดีครับ ภาษาไทยไม่มีช่องว่างระหว่างคำ ๑๒๓ ພາສາລາວ ភាសាខ្មែរ မြန်မာဘာသာ",
  // ICU status and boundary probes.
  "_", "__", "a_", "_a", "1_", "_1", "__a", "a‍\u{1F600}", "\u{1F600}", "  ", "　", "­",
  "́", "́a", "á", ".", "x.y", "e.g.", "can't", "א\"ב", "א'", "가", "กับ", "ๆ", "ฯ", "一二",
  "あい", "アイ", "ア_", "々", "〇", "\u{20000}", "⺀", "٠١", "௰", "\u{1D7CE}", "​", "﻿",
  "a﻿b", "๐๑", "၀", "០", "༠", "ＡＢ", "ｱ", "０", "ʼ", "ˈ", "〱", "゛", "ー", "・", "\u{1B000}",
  "\u{16FE3}", "가́", "가각́", "ᄀ", "ᄀ́", "가a", "a가", "a‍ℹ", "ℹ",
  "a‍\u{1F600}b", "1'", "a'", "_́", "́_", "1_́", "a׳", "⁠a", "a⁠",
  "กั", "xก", "กx", "々a", "a々", "゙", "ア゙", "\u{1F600}́", "\r\n", "\n", " ", "a b",
  "a  b", "a　b", "á́", " ́ ", "‍\u{1F600}", "\n‍\u{1F600}", "\u{1F1E6}́\u{1F1E6}",
  "a’b", "a․b", "1٬2", "a·b", "1﹒2", "a:b", "1:2", "a;b", "1;2",
  // Lone surrogates.
  "a\uD800́b", "\uD800́", "\uDC00‍", "a\uDBFF", "\uD83D", "x\uDE00y", "\uDC00\uD800",
  "😀\uDE00", "A.\uD800 b", "a\uD800'b",
);

const seg = (granularity) => new Intl.Segmenter("en", { granularity });
const [graphemes, words, sentences] = [seg("grapheme"), seg("word"), seg("sentence")];
const ends = (segs) => segs.map((s) => s.index + s.segment.length);
const cases = [];
for (const s of corpus) {
  const w = [...words.segment(s)];
  cases.push({
    s,
    g: ends([...graphemes.segment(s)]),
    w: ends(w),
    l: w.map((x) => (x.isWordLike ? "1" : "0")).join(""),
    t: ends([...sentences.segment(s)]),
  });
}
const out = { node: process.version, icu: process.versions.icu, unicode: process.versions.unicode, cases };
const json = JSON.stringify(out).replace(/\},\{/g, "},\n{") + "\n";
fs.writeFileSync(path.join(__dirname, "seg_icu.json.gz"), zlib.gzipSync(json, { level: 9 }));
