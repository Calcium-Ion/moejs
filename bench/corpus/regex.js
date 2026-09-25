export function run() {
  const re = /(\d{4})-(\d{2})-(\d{2})/;
  const m = re.exec("Date: 2026-09-23, next 2027-01-05");
  const g = /(\d{4})-(\d{2})/g;
  const all = [];
  let x;
  while ((x = g.exec("2026-09 2027-01 2028-12")) !== null) all.push([x[0], x.index, g.lastIndex]);
  const named = /(?<y>\d{4})-(?<m>\d{2})/.exec("on 1999-12");
  return {
    exec: [m[0], m[1], m[2], m[3], m.index, m.input.length], groups: named.groups, all: all,
    test: [/^https?:\/\//i.test("HTTPS://x"), /\bfoo\b/.test("a foo b"), /\bfoo\b/.test("afoob"), /^\s*$/.test("  \t\n"), /a.c/.test("a\nc"), /a.c/s.test("a\nc"), /^b/m.test("a\nb"), /\u{1F600}/u.test("😀"), /^.$/u.test("😀"), /^.$/.test("😀")],
    match: ["a1b22c333".match(/\d+/g), "abc".match(/x/g), "abc".match(/b/).index, "aXbXc".split(/x/i), "a1b2c3".split(/(\d)/), "abc".split(""), "a,b,,c".split(",", 3)],
    replace: ["2026-09-23".replace(re, "$3/$2/$1"), "aaa".replace(/a/g, (m, i) => i), "tab\tsep".replace(/\s/g, "_"), "café naïve".replace(/[^\x00-\x7F]/g, "?"), "x".replace(/x/, "$$"), "abc".replace(/(b)/, "[$1$2]")],
    props: [re.source, re.flags, g.global, /a/gimsuy.flags, String(/a\/b/g), new RegExp("a+", "g").source, new RegExp(/x/i).flags],
    classes: [/[\w-]+/.exec("foo-bar baz")[0], /[^a-z]+/i.exec("abcDEF123")[0], /\D+/.exec("12ab34")[0], /[\s\S]+?x/.exec("a\nbx")[0], /[一-龥]+/.exec("mixed 中文 text")[0]],
    sticky: (() => { const r = /a/y; r.lastIndex = 1; return [r.test("ba"), r.lastIndex, r.test("ba"), r.lastIndex]; })(),
    lastIndexReset: (() => { const r = /x/g; r.test("x"); r.test("y"); return r.lastIndex; })(),
    escapes: [/\//.test("/"), /\./.test("a"), /\d\.\d/.test("1.5"), /[.]/.test("x"), /\//.source, /\n/.test("\n"), /\x41/.test("A"), /B/.test("B")],
    quantifiers: [/a{2,3}/.exec("aaaa")[0], /a{2,3}?/.exec("aaaa")[0], /(ab)+/.exec("ababab")[0], /(a|b)*c/.exec("abac")[0], /(?:x)(y)?/.exec("x")[1]],
    error: (() => { try { new RegExp("("); return "no"; } catch (e) { return e instanceof SyntaxError; } })(),
  };
}
