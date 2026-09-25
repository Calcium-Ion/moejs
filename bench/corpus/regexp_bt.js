// Patterns RE2 cannot run exactly, which run on the backtracking VM:
// lookaround, backreferences and the string methods over them.
function errName(f) {
  try { f(); return "no error"; } catch (e) { return e.name; }
}
function execAll(re, s) {
  const out = [];
  re.lastIndex = 0;
  let m;
  while ((m = re.exec(s)) !== null) {
    out.push([m.index, re.lastIndex].concat(Array.from(m)));
    if (m[0] === "") re.lastIndex++;
  }
  return out;
}
function arr(m) {
  return m === null ? null : [m.index].concat(Array.from(m));
}
export function run() {
  return {
    lookahead: [
      arr(/\d+(?=px)/.exec("12em 34px")),
      arr(/(?!foo)\w+/.exec("foobar")),
      /^(?=.*\d)(?=.*[a-z])(?=.*[A-Z]).{8,}$/.test("abcDEF123"),
      /^(?=.*\d)(?=.*[a-z])(?=.*[A-Z]).{8,}$/.test("abcdef123"),
      "1234567".replace(/(\d)(?=(\d{3})+$)/g, "$1,"),
      arr(/(?=(\w+))\w/.exec("abc")),
      arr(/^(?!.*\.\.)[\w.]+@[\w.]+$/.exec("a.b@c.d")),
      /^(?!.*\.\.)[\w.]+@[\w.]+$/.test("a..b@c.d"),
      arr(/(?=a)*/.exec("a")),
      arr(/a(?!b)/.exec("abac")),
    ],
    lookbehind: [
      arr(/(?<=\$)\d+(\.\d+)?/.exec("cost $42.50")),
      arr(/(?<!\$)\b\d+/.exec("$5 7")),
      "a1b2 3".replace(/(?<=[a-z])\d/g, "#"),
      arr(/(?<=(\d)(\d))x/.exec("12x")),
      arr(/(?<=(\d+)(\d+))$/.exec("1053")),
      arr(/(?<=^|,)\s*(\w+)/.exec("x, yz")),
      execAll(/(?<=a)b/g, "abab cb ab"),
      arr(/(?<!a)b/.exec("abcb")),
      arr(/(?<=\bkey=)\w+/.exec("mykey=1 key=2")),
      "price: 10 USD, 20 EUR".match(/\d+(?= EUR)|(?<=: )\d+/g),
    ],
    backrefs: [
      arr(/(\w)\1/.exec("hello")),
      arr(/<(\w+)>.*?<\/\1>/.exec("<b>x</b><i>y</i>")),
      arr(/(a)|\1b/.exec("b")),
      arr(/\1(a)/.exec("aa")),
      arr(/(?<q>["']).*?\k<q>/.exec(`say "it's" 'yo'`)),
      arr(/(a)\1/i.exec("aA")),
      "aabbcdd".replace(/(.)\1/g, "$1"),
      arr(/^(\w+)\s+\1$/.exec("bye bye")),
      /^(\w+)\s+\1$/.test("bye byte"),
      arr(/(a*)b\1+/.exec("baaaac")),
      arr(/(?:(a)|b)\1/.exec("bb")),
      arr(/(?<n>x)\k<n>{2}/.exec("xxx")),
    ],
    captureReset: [
      arr(/(z)((a+)?(b+)?(c))*(?=$)/.exec("zaacbbbcac")),
      arr(/(a)?(?=b)b|\1c/.exec("c")),
    ],
    lazyGreedy: [
      arr(/(\w+?)(?=\d)/.exec("abc123")),
      arr(/(\w+)(?=\d)/.exec("abc123")),
      arr(/^(a|aa)+(?=b)/.exec("aaaaaaaaaab")),
      arr(/(.*?)(?=,|$)/.exec("k=v,x")),
      arr(/(a+)+(?=c)/.exec("aaac")),
      arr(/"((?:[^"\\]|\\.)*)"(?=\s*:)/.exec(`{"a\\"b" : 1}`)),
    ],
    methods: [
      "a1b2c3".split(/(?<=[a-z])\d/),
      "a,b;c".split(/(?<=\w)[,;](?=\w)/),
      "x1y22z".split(/(?<=\d)([a-z])/),
      "one two  three".split(/\s+(?=t)/),
      "2026-09-23".replace(/(?<y>\d+)-(?<m>\d+)-(?<d>\d+)(?=$)/, "$<d>.$<m>.$<y>"),
      "2026-09-23".replace(/(\d+)-(\d+)(?=-)/, "[$2|$1|$`|$'|$&]"),
      "aXbX".replace(/(?<=a)X/g, (m, i, s) => m.toLowerCase() + i + s.length),
      "abcabc".replace(/(b)(?=c)/g, (m, g1, i) => g1.toUpperCase() + i),
      "abc".replace(/b(?=c)|c$/g, "-"),
      "a.b..c".match(/(?<!\.)\.(?!\.)/g),
      "tel: 010-1234-5678".replace(/(?<=-)\d{4}(?=-)/, "****"),
      "__init__ __x".replace(/\b_+(\w+?)_+\b/g, "<$1>"),
    ],
    stickyGlobal: (() => {
      const y = /(?<=a)b/y;
      y.lastIndex = 1;
      const r1 = [y.test("abab"), y.lastIndex, y.test("abab"), y.lastIndex];
      y.lastIndex = 3;
      r1.push(y.test("abab"), y.lastIndex);
      const g = /(\w)\1/g;
      const r2 = [g.test("aabbc"), g.lastIndex, g.test("aabbc"), g.lastIndex, g.test("aabbc"), g.lastIndex];
      return [r1, r2, execAll(/(?=\w)/g, "ab c"), execAll(/\b(?=\w)/g, "ab c")];
    })(),
    unicode: [
      arr(/(?<=😀)x/u.exec("😀x")),
      arr(/(.)\1/u.exec("a😀😀")),
      /^(?=.)\u{1F600}$/u.test("😀"),
      arr(/(?<=.)./u.exec("😀😁")),
      /(?<=.)./.exec("😀😁")[0].charCodeAt(0),
      execAll(/(?=\p{Lu})/gu, "aBcD"),
      arr(/(?<!\p{L})\d+/u.exec("a1 22")),
      "😀a😀b".replace(/(?<=😀)[ab]/gu, "_"),
      "😀😀".split(/(?=😀)/u),
    ],
    flagsMix: [
      arr(/(?<=^)\w/gm.exec("a\nb")),
      "a\nb\nc".match(/(?<=^)\w/gm),
      "a\nb\nc".match(/\w(?=$)/gm),
      arr(/(?<=A)b/i.exec("ab")),
      arr(/(k)\1/i.exec("kK")),
      arr(/(?<=a.)c/s.exec("a\nc")),
      /(?<=a.)c/.test("a\nc"),
      arr(/^(?=[^]*z)./.exec("\nz")),
    ],
    errors: [
      errName(() => new RegExp("(?<=a")),
      errName(() => new RegExp("\\k<x>(?<y>.)")),
      new RegExp("(?=a)+").test("a"),
      new RegExp("\\2(a)").test("\x02a"),
      new RegExp("\\k<x>").test("k<x>"),
    ],
    source: [/(?<=a)\1(b)/.source, String(/(?!x)/gi), new RegExp("(?<n>a)\\k<n>", "g").flags],
  };
}
