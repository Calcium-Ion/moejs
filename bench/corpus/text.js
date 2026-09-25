// String.prototype.normalize, Function.prototype.toString and Error stacks,
// compared with Sobek. Stacks are compared structurally: the error name and,
// per frame, the bare function name and line (moejs formats V8's
// "    at Type.name (file:line:col)", "new F", "get g" and natives as
// "JSON.parse (<anonymous>)"; Sobek "\tat name (file:line:col(pc))" and
// "parse (native)"; the columns differ). Left out where Sobek differs from
// V8 and moejs follows V8: a bound function's toString is
// "function () { [native code] }" (Sobek "function bound f() ..."), and
// normalize keeps lone surrogates (Sobek turns them into U+FFFD).
function inner(x) { return new Error("in " + x); }
function outer(x) { return inner(x + 1); }
const obj = {
  method() { return outer(1); },
  get getter() { return new Error("g"); },
  set setter(v) { this.e = new Error("s"); },
};
function Ctor() { this.e = new Error("c"); }
const arrow = () => new Error("a");
function thrower() { null.x; }
function catchIt(f) { try { f(); } catch (e) { return e; } }
function recurse(n) { return n === 0 ? new Error("deep") : recurse(n - 1); }

const frameRe = /^\s*at (?:(.+?) \()?(.+?)\)?$/;
function shape(e) {
  const frames = e.stack.split("\n").slice(1).filter((l) => l.trim() !== "").map((l) => {
    const m = frameRe.exec(l);
    const name = (m[1] || "").replace(/^(new|get|set) /, "").replace(/^.*\./, "");
    const loc = m[2];
    return loc === "native" || loc === "<anonymous>" ? name + " native" : name + " " + loc.split(":")[1];
  });
  return [e.name, e instanceof Error, typeof e.stack].concat(frames);
}

export function run() {
  const r = {};
  obj.setter = 1;
  r.stacks = [
    inner(0), outer(0), obj.method(), obj.getter, obj.e, new Ctor().e, arrow(), recurse(5),
    catchIt(thrower),
    catchIt(() => JSON.parse("{")),
    catchIt(() => [1].map(function cb() { throw new TypeError("t"); })),
    catchIt(() => { undefinedName; }),
    catchIt(function named() { throw new RangeError("r"); }),
    (function () { return new SyntaxError("anon"); })(),
    catchIt(() => [3, 1, 2].sort(function cmp() { throw new Error("sort"); })),
    catchIt(() => JSON.stringify({ toJSON() { throw new Error("tj"); } })),
  ].map(shape);
  r.messages = [inner(0), new Ctor().e, catchIt(() => { throw new EvalError("ev"); })].map((e) => [String(e), e.message, e.stack.split("\n")[0]]);
  const fns = [
    inner, outer, obj.method,
    Object.getOwnPropertyDescriptor(obj, "getter").get,
    Object.getOwnPropertyDescriptor(obj, "setter").set,
    Ctor, arrow, thrower, catchIt, shape,
    function () {},
    function named(a, b = 2, ...c) { /* comment */ return a; },
    async => async,
    (a, { b, c: [d] }) => { return a + b + d; },
    x=>x*2,
    function	tabs	(	)	{	},
    { m() {} }.m,
    { ["a" + "b"]() {} }.ab,
    { 1() {} }[1],
    { "s k"() {} }["s k"],
    { f: function () { return "ƒ 日本"; } }.f,
    { nested() { return () => `t${1}`; } }.nested(),
    Math.max, Array.prototype.push, Function.prototype, JSON.parse, Object, String.prototype.normalize,
  ];
  r.fns = fns.map((f) => { try { return Function.prototype.toString.call(f); } catch (e) { return e.name; } });
  r.fnStrings = [String(inner), inner + "", `${arrow}`, [arrow].join()];
  r.fnErrors = [{}, 1, "f", null].map((v) => { try { return Function.prototype.toString.call(v); } catch (e) { return e.name; } });

  const cps = (s) => [...s].map((c) => c.codePointAt(0).toString(16)).join(" ");
  const samples = [
    "Å", "Å", "Å", "ẛ̣", "Ḍ̇", "Ḍ̇", "Ḍ̇",
    "ﬁ", "ȩ́", "ḉ", "한국어", "각", "한ᆯ",
    "①２Ｋ", "Ω", "ǆ", "ﾊﾟ", "😀́", "", "abc", "̈́", "ཱི", "½", "⁵₀",
    "Ǆǅǆ", "́A", "Á́̂", "क़ऱ़", "ֱָֹ֑",
    "é".repeat(5), "ﷺ", "㍿", "ϓϔ", "ᾀᾈ",
  ];
  r.norm = samples.map((s) => [s.normalize(), s.normalize("NFC"), s.normalize("NFD"), s.normalize("NFKC"), s.normalize("NFKD")].map(cps));
  r.normLength = samples.map((s) => [s.length, s.normalize("NFD").length, s.normalize("NFKC").length]);
  r.normIdempotent = samples.every((s) => ["NFC", "NFD", "NFKC", "NFKD"].every((f) => s.normalize(f).normalize(f) === s.normalize(f)));
  r.normErr = ["nfc", "", "NFX", null, "NFC ", 1].map((f) => { try { return cps("x".normalize(f)); } catch (e) { return e.name; } });
  r.normThis = [String.prototype.normalize.call(123), String.prototype.normalize.call(true, "NFD"), (() => { try { return String.prototype.normalize.call(null); } catch (e) { return e.name; } })(), (() => { try { return String.prototype.normalize.call(undefined); } catch (e) { return e.name; } })(), "x".normalize(undefined), String.prototype.normalize.length, String.prototype.normalize.name, String.prototype.normalize.call({ toString() { return "Å"; } }, { toString() { return "NFKD"; } }).length];
  r.normCompare = ["Å" === "Å", "Å".normalize("NFD") === "Å", "Å".normalize() === "Å", ["Å", "Å"].map((s) => s.normalize()).filter((s, i, a) => a.indexOf(s) === i).length];
  return r;
}
