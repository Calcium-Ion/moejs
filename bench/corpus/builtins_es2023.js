function errName(f) {
  try { f(); return "no error"; } catch (e) { return e.name; }
}
function desc(o, k) {
  const d = Object.getOwnPropertyDescriptor(o, k);
  if (!d) return d;
  return "get" in d
    ? { get: typeof d.get, set: typeof d.set, enumerable: d.enumerable, configurable: d.configurable }
    : { value: show(d.value), writable: d.writable, enumerable: d.enumerable, configurable: d.configurable };
}
function enumerable(o, k) {
  const d = Object.getOwnPropertyDescriptor(o, k);
  return [typeof d.value, d.enumerable];
}
// show makes a value exportable: functions by name, objects by type,
// non-finite numbers as strings.
function show(v) {
  if (typeof v === "function") return "function " + v.name;
  if (typeof v === "object" && v !== null) return Array.isArray(v) ? v : "object";
  return typeof v === "number" && !isFinite(v) ? String(v) : v;
}
function reflect() {
  const proto = { inherited: 1, get viaGetter() { return this.tag; }, set viaSetter(v) { this.stored = v; } };
  const o = Object.create(proto);
  o.own = 2;
  o[1] = "one";
  o[0] = "zero";
  o.b = "b";
  const receiver = { tag: "receiver" };
  function Point(x, y) { this.x = x; this.y = y; }
  function Other() {}
  Other.prototype.kind = "other";
  const built = Reflect.construct(Point, [1, 2]);
  const withTarget = Reflect.construct(Point, [3, 4], Other);
  const frozen = Object.freeze({ k: 1 });
  const sealed = Object.seal({ k: 1 });
  const ne = { k: 1 };
  const preventResult = Reflect.preventExtensions(ne);
  const target = {};
  const setOnReceiver = {};
  return {
    apply: [Reflect.apply(Math.max, null, [1, 5, 3]), Reflect.apply(function () { return this; }, "s", []) instanceof String, Reflect.apply(String.prototype.slice, "abcdef", [1, 3])],
    applyArrayLike: Reflect.apply(Array.prototype.join, { length: 2, 0: "a", 1: "b" }, ["+"]),
    construct: [built.x, built.y, built instanceof Point, withTarget.x, withTarget.kind, withTarget instanceof Other, Object.getPrototypeOf(withTarget) === Other.prototype],
    constructBuiltins: [Reflect.construct(Array, [3]).length, Reflect.construct(Date, [0]).getTime(), Reflect.construct(Error, ["m"]).message, Reflect.construct(Boolean, [0]).valueOf()],
    define: [Reflect.defineProperty(target, "x", { value: 1 }), Reflect.defineProperty(target, "x", { value: 2 }), Reflect.defineProperty(frozen, "k", { value: 2 }), Reflect.defineProperty(ne, "n", { value: 1 }), desc(target, "x")],
    deleteProperty: [Reflect.deleteProperty(o, "own"), "own" in o, Reflect.deleteProperty(sealed, "k"), Reflect.deleteProperty(o, "missing"), Reflect.deleteProperty([1, 2], "length")],
    get: [Reflect.get(o, "inherited"), Reflect.get(o, "viaGetter", receiver), Reflect.get([7, 8], 1), Reflect.get(o, "nope")],
    set: [Reflect.set(o, "viaSetter", 5, setOnReceiver), setOnReceiver.stored, o.stored, Reflect.set(frozen, "k", 9), frozen.k, Reflect.set(o, "fresh", 1, setOnReceiver), setOnReceiver.fresh, o.fresh],
    setArrayLength: (() => { const a = [1, 2, 3]; return [Reflect.set(a, "length", 1), a]; })(),
    gopd: [enumerable(Reflect, "apply"), Reflect.getOwnPropertyDescriptor(o, "inherited"), desc(o, "b")],
    proto: [Reflect.getPrototypeOf(o) === proto, Reflect.setPrototypeOf(o, null), Reflect.getPrototypeOf(o), Reflect.setPrototypeOf(ne, {}), Reflect.setPrototypeOf(ne, Object.prototype)],
    has: [Reflect.has({ a: 1 }, "a"), Reflect.has(Object.create({ p: 1 }), "p"), Reflect.has([1], 0), Reflect.has([1], "1"), Reflect.has(Math, "PI")],
    extensible: [preventResult, Reflect.isExtensible({}), Reflect.isExtensible(frozen), Reflect.isExtensible(ne), Reflect.preventExtensions(Object.freeze([]))],
    ownKeys: [Reflect.ownKeys(o), Reflect.ownKeys([1, 2]), Reflect.ownKeys(function f(a, b) {}).sort()],
    errors: [
      errName(() => Reflect.apply(1, null, [])), errName(() => Reflect.apply(Math.max, null)), errName(() => Reflect.construct(() => 1, [])),
      errName(() => Reflect.construct(Point, [], Math.max)), errName(() => Reflect.get(1, "a")), errName(() => Reflect.set("s", "a", 1)),
      errName(() => Reflect.defineProperty({}, "a", 1)), errName(() => Reflect.ownKeys(null)), errName(() => Reflect.setPrototypeOf({}, 1)),
      errName(() => Reflect.has(undefined, "a")), errName(() => new Reflect.get({}, "a")), errName(() => Reflect()),
    ],
    shape: [typeof Reflect, Object.keys(Reflect), Object.getOwnPropertyNames(Reflect).sort(), Reflect.apply.length, Reflect.construct.length, Reflect.defineProperty.length, Reflect.set.length, Reflect.get.length],
  };
}
function arrays() {
  const a = [1, 2, 3, 4, 5];
  const like = { length: 5, 0: "a", 1: "b", 2: "c", 3: "d", 4: "e" };
  const sparse = [1, , 3, , 5];
  const cmpLog = [];
  const toSorted = [3, 1, undefined, 2, , 10].toSorted((x, y) => { cmpLog.push(typeof x); return x - y; });
  return {
    copyWithin: [a.slice().copyWithin(0, 3), a.slice().copyWithin(1, 0, 2), a.slice().copyWithin(-2, 0), a.slice().copyWithin(0, -3, -1), a.slice().copyWithin(2, 1),
      a.slice().copyWithin(0, 1, NaN), a.slice().copyWithin("1", "3"), sparse.slice().copyWithin(0, 1), Object.keys(sparse.slice().copyWithin(0, 1))],
    copyWithinGeneric: [Array.prototype.copyWithin.call(Object.assign({}, like), 0, 3), Array.prototype.copyWithin.call({ length: 3, 2: "x" }, 0, 2)],
    toReversed: [a.toReversed(), a, sparse.toReversed(), Object.keys(sparse.toReversed()), Array.prototype.toReversed.call(like), Array.prototype.toReversed.call({ length: 0 })],
    toSorted: [toSorted, toSorted.length, 4 in toSorted, cmpLog.indexOf("undefined"), ["b", "a", "c"].toSorted(), [5, 1, 10].toSorted(), Array.prototype.toSorted.call({ length: 2, 0: "z", 1: "y" })],
    toSpliced: [a.toSpliced(1, 2), a.toSpliced(1, 0, "x", "y"), a.toSpliced(-2), a.toSpliced(), a.toSpliced(undefined), a.toSpliced(2, undefined, "u"), a.toSpliced(1, Infinity), sparse.toSpliced(0, 1), a,
      Array.prototype.toSpliced.call(like, 1, 3, "!")],
    with: [a.with(0, "z"), a.with(-1, "last"), sparse.with(1, "h"), a, Array.prototype.with.call(like, 2, "C"), errName(() => a.with(5, 0)), errName(() => a.with(-6, 0)), a.with("1", "s"), a.with(1.9, "t")],
    at: [a.at(0), a.at(-1), a.at(-5), a.at(-6), a.at(5), a.at("2"), a.at(1.7), a.at(-0.5), a.at(NaN), a.at(), Array.prototype.at.call(like, -2), Array.prototype.at.call("str", 1)],
    flat: [[1, [2, [3, [4]]]].flat(), [1, [2, [3, [4]]]].flat(Infinity), [1, [2, [3]]].flat(0), [1, [2, [3]]].flat(-1), [1, [2, [3]]].flat("2"), [1, [2]].flat(NaN), [1, , [2, , 3]].flat(),
      [[]].flat(), Array.prototype.flat.call({ length: 2, 0: [1], 1: 2 })],
    flatMap: [[1, 2, 3].flatMap(x => [x, [x * 2]]), ["a b", "c"].flatMap(s => s.split(" ")), [1, 2].flatMap(function (x) { return this.k + x; }, { k: 10 }), [1, , 3].flatMap(x => x), errName(() => [1].flatMap())],
    findLast: [a.findLast(x => x % 2 === 0), a.findLastIndex(x => x > 10), [, undefined].findLast(x => x === undefined), Array.prototype.findLast.call(like, (v, i) => i < 2), sparse.findLastIndex(x => x === undefined)],
    findLastOrder: (() => { const seen = []; [1, 2, 3].findLast((v, i, arr) => { seen.push(i + ":" + v + ":" + arr.length); return false; }); return seen; })(),
    lengthLimits: [
      Array.prototype.at.call({ length: 2 ** 53 + 10, [2 ** 53 - 2]: "top" }, -1),
      Array.prototype.findLastIndex.call({ length: 2 ** 53 - 1, [2 ** 53 - 2]: "top" }, x => x === "top"),
      Array.prototype.includes.call({ length: 2 ** 53 - 1, [2 ** 53 - 2]: "top" }, "top", 2 ** 53 - 3),
      Array.prototype.lastIndexOf.call({ length: 2 ** 53 - 1, [2 ** 53 - 2]: "top" }, "top"),
      errName(() => Array.prototype.toSpliced.call({ length: 2 ** 53 - 1 }, 0, 0, 1)), errName(() => Array.prototype.with.call({ length: 2 ** 32 }, 0, 1)),
      errName(() => Array.prototype.toReversed.call({ length: 2 ** 32 })), errName(() => [].toSorted(1)),
      (() => { const o = { length: 2 ** 53 - 1 }; return [errName(() => Array.prototype.push.call(o, 1)), o.length]; })(),
    ],
    toLocaleString: [[1, "a", null, undefined, [2, 3]].toLocaleString(), [{ toLocaleString() { return "L"; } }, { toLocaleString: () => 7 }].toLocaleString(), errName(() => [{ toLocaleString: 1 }].toLocaleString())],
    generic: [Array.prototype.map.call("abc", c => c.toUpperCase()), Array.prototype.filter.call(like, (v, i) => i % 2), Array.prototype.join.call({ length: 3, 1: "x" }, "-"), Array.prototype.indexOf.call({ length: 3, 2: NaN, 1: 1 }, 1),
      Array.prototype.reverse.call({ length: 3, 0: "a", 2: "c" }), Array.prototype.fill.call({ length: 2 }, 0), Array.prototype.includes.call({ length: 1, 0: NaN }, NaN)],
    unscopableShape: [Array.prototype.copyWithin.length, Array.prototype.toSpliced.length, Array.prototype.with.length, Array.prototype.toSorted.length, Array.prototype.flat.length, Array.prototype.flatMap.length, Array.prototype.at.length],
  };
}
function objects() {
  const o = { b: 1, a: 2, 10: "ten", 2: "two", [-1]: "neg", "01": "lead", 4294967294: "maxIndex", 4294967295: "beyond" };
  Object.defineProperty(o, "hidden", { value: "h", enumerable: false });
  const proto = { inheritedKey: 1 };
  const child = Object.create(proto);
  child.own = 1;
  const arr = ["x", "y"];
  arr.extra = true;
  const fn = function named(a, b) {};
  return {
    is: [Object.is(NaN, NaN), Object.is(0, -0), Object.is(-0, -0), Object.is("a", "a"), Object.is({}, {}), Object.is(null, undefined), Object.is(), Object.is(1 / 0, Infinity)],
    hasOwn: [Object.hasOwn(o, "b"), Object.hasOwn(child, "inheritedKey"), Object.hasOwn(arr, 0), Object.hasOwn(arr, "length"), Object.hasOwn("str", 1), Object.hasOwn("str", "length"), errName(() => Object.hasOwn(null, "a"))],
    names: [Object.getOwnPropertyNames(o), Object.getOwnPropertyNames(arr), Object.getOwnPropertyNames("ab"), Object.getOwnPropertyNames(fn).sort(), Object.getOwnPropertyNames(child)],
    order: [Object.keys(o), Object.values(o), Object.entries({ z: 1, 1: 2, y: 3, 0: 4 }), Object.entries("hi"), Object.values([3, , 4]), JSON.stringify(o)],
    orderAfterDelete: (() => { const p = { a: 1, b: 2, c: 3 }; delete p.a; p.a = 4; p[5] = 5; p[1] = 1; return [Object.keys(p), Object.entries(p)]; })(),
    liveEntries: (() => { const log = []; const p = { a: 1, get b() { delete this.c; log.push("b"); return 2; }, c: 3 }; return [Object.entries(p), log]; })(),
    propertyIsEnumerable: [o.propertyIsEnumerable("b"), o.propertyIsEnumerable("hidden"), o.propertyIsEnumerable("missing"), child.propertyIsEnumerable("inheritedKey"), arr.propertyIsEnumerable("length"), arr.propertyIsEnumerable(0),
      Object.prototype.propertyIsEnumerable.call("abc", 0), Object.prototype.propertyIsEnumerable.call("abc", "length"), errName(() => Object.prototype.propertyIsEnumerable.call(undefined, "x"))],
    isPrototypeOf: [proto.isPrototypeOf(child), Object.prototype.isPrototypeOf(child), child.isPrototypeOf(proto), Object.prototype.isPrototypeOf.call(proto, 1), Array.prototype.isPrototypeOf([]),
      Function.prototype.isPrototypeOf(fn), Object.prototype.isPrototypeOf.call(null, 1), errName(() => Object.prototype.isPrototypeOf.call(null, {}))],
    toLocaleString: [({}).toLocaleString(), [1, 2].toLocaleString(), ({ toString() { return "custom"; } }).toLocaleString(), Object.prototype.toLocaleString.call(1), errName(() => Object.prototype.toLocaleString.call(null))],
    toStringTags: [Object.prototype.toString.call([]), Object.prototype.toString.call(null), Object.prototype.toString.call(undefined), Object.prototype.toString.call(fn), Object.prototype.toString.call(new Error("x")),
      Object.prototype.toString.call(/x/), Object.prototype.toString.call(new Date(0)), Object.prototype.toString.call("s"), Object.prototype.toString.call(1), Object.prototype.toString.call(true)],
    // Writable and configurable differ: intrinsics are frozen in moejs's shared realms.
    descriptors: [enumerable(Object, "is"), enumerable(Object, "hasOwn"), enumerable(Object.prototype, "propertyIsEnumerable"), enumerable(Object.prototype, "toLocaleString")],
  };
}
function strings() {
  return {
    fromCodePoint: [String.fromCodePoint(65, 0x1f600, 0x10ffff).length, String.fromCodePoint(), String.fromCodePoint("66", 67.0), errName(() => String.fromCodePoint(0x110000)), errName(() => String.fromCodePoint(1.5)),
      errName(() => String.fromCodePoint(-1)), errName(() => String.fromCodePoint(NaN)), String.fromCodePoint.length],
    raw: [String.raw({ raw: ["a", "b", "c"] }, 1, 2, 3), String.raw({ raw: "xyz" }, "-", "+"), String.raw({ raw: { length: 0 } }), String.raw({ raw: ["only"] }), errName(() => String.raw({})), String.raw.length],
    at: ["héllo".at(1), "abc".at(-1), "abc".at(3), "😀".at(0).length],
    pad: ["5".padStart(3, "0"), "abc".padEnd(6, "12"), "abc".padStart(2), "x".padEnd(4)],
    locale: ["İ".toLocaleLowerCase().length, "ß".toLocaleUpperCase(), "ΑΣ".toLocaleLowerCase(), "abc".toLocaleUpperCase("tr")],
    localeCompare: ["a".localeCompare("b"), "b".localeCompare("a"), "a".localeCompare("a"), "a".localeCompare("B"), "A".localeCompare("a"), "résumé".localeCompare("resume"), "a".localeCompare("á"),
      ["b", "a", "C", "á", "A", "c"].sort((x, y) => x.localeCompare(y)), "".localeCompare("a"), "10".localeCompare("9")],
    substr: ["abcdef".substr(1, 3), "abcdef".substr(-3), "abcdef".substr(2), "abcdef".substr(1, -1), "abcdef".substr(NaN, 2), "abc".substr(0, Infinity)],
    trim: [" x ".trimLeft(), " x ".trimRight(), String.prototype.trimLeft === String.prototype.trimStart, String.prototype.trimRight.name],
    codePoints: ["😀a".codePointAt(0), "😀a".codePointAt(1), "a".codePointAt(5), "\ud800".codePointAt(0)],
    repeat: ["ab".repeat(3), "".repeat(5), errName(() => "a".repeat(-1)), errName(() => "a".repeat(Infinity)), "x".repeat(0)],
  };
}
function numbers() {
  return {
    radix: [(255).toString(16), (-255).toString(2), (0.5).toString(2), (0.1).toString(3), (3.75).toString(16), (123.456).toString(36), (1e21).toString(16), (2 ** 53).toString(2).length, (Number.MAX_SAFE_INTEGER).toString(36),
      (0.1).toString(16), (-0).toString(7), NaN.toString(2), Infinity.toString(36), errName(() => (1).toString(1)), errName(() => (1).toString(37)), (10).toString(undefined), (35).toString("36")],
    fixed: [(1.005).toFixed(2), (1e21).toFixed(2), (0.000001).toFixed(7), (-1.5).toFixed(0), (123.456).toFixed(10), errName(() => (1).toFixed(101))],
    precision: [(123.456).toPrecision(4), (0.00001).toPrecision(1), (1e21).toPrecision(3), (123).toPrecision(), errName(() => (1).toPrecision(0))],
    exponential: [(123456).toExponential(2), (0).toExponential(), (1.5).toExponential(0), (-1.5e-7).toExponential(3)],
    statics: [Number.isInteger(5.0), Number.isInteger(5.5), Number.isSafeInteger(2 ** 53), Number.isSafeInteger(2 ** 53 - 1), Number.parseFloat === parseFloat, Number.parseInt === parseInt, Number.EPSILON > 0],
    math: [Math.trunc(-4.7), Math.sign(-3), Math.cbrt(27), Math.hypot(3, 4), Math.clz32(1), Math.fround(5.5), Math.log2(8), Math.expm1(0), Math.imul(3, 4), String(Math.max()), String(Math.min()), Math.round(-2.5), Math.round(2.5)],
  };
}
function globals() {
  return {
    escape: [escape("abc123@*_+-./"), escape("ä öĀ\ud83d"), escape("!#$%&'()"), unescape("%41%u0042%zz%u12%"), unescape(escape("héllo 世界")), escape.length, unescape.length],
    uri: [encodeURIComponent("a b&c/é"), encodeURI("http://x/a b?q=é#f"), decodeURIComponent("%E4%B8%96"), decodeURI("%23%3F"), errName(() => decodeURIComponent("%")), errName(() => encodeURI("\ud800"))],
    parse: [parseInt("0x1F"), parseInt("  12px"), parseInt("z", 36), parseInt("08"), parseInt("-0"), parseFloat("3.14abc"), parseFloat(".5e1"), String(parseFloat("-Infinityx")), isNaN("abc"), isFinite("12")],
    // The bindings of intrinsics are frozen in moejs's shared realms (the lockdown model),
    // so only the immutable ones are compared.
    globalThis: [typeof globalThis, globalThis.globalThis === globalThis, desc(globalThis, "NaN"), desc(globalThis, "Infinity"), desc(globalThis, "undefined")],
  };
}
function errorsAndFunctions() {
  const e = new Error("msg");
  function f(a, b, c) { return [this === undefined ? "undef" : typeof this, a, b, c]; }
  const bound = f.bind("t", 1);
  const boundTwice = bound.bind(null, 2);
  function Ctor(x) { this.x = x; }
  const BoundCtor = Ctor.bind(null, 5);
  const instance = new BoundCtor();
  const lengthy = function () {};
  Object.defineProperty(lengthy, "length", { value: -5 });
  const hugeLen = function () {};
  Object.defineProperty(hugeLen, "length", { value: Infinity });
  const fracLen = function () {};
  Object.defineProperty(fracLen, "length", { value: 2.7 });
  const noName = function () {};
  Object.defineProperty(noName, "name", { value: 42 });
  return {
    errorToString: [e.toString(), Error.prototype.toString.call({ name: "N", message: "" }), Error.prototype.toString.call({ message: "only" }), Error.prototype.toString.call({ name: "", message: "m" }),
      Error.prototype.toString.call({ name: undefined, message: undefined }), Error.prototype.toString.call({ name: 1, message: 2 }), errName(() => Error.prototype.toString.call(1)), String(new TypeError()),
      new RangeError("r", { cause: 1 }).cause, "cause" in new Error("x", {}), Error("called").message, Error.prototype.message === "", Error.prototype.name],
    nativeErrors: [TypeError.prototype.name, Object.getPrototypeOf(TypeError) === Error, Object.getPrototypeOf(RangeError.prototype) === Error.prototype, new SyntaxError("s") instanceof Error, URIError.length, EvalError("x").message],
    apply: [f.apply("x", [1, 2, 3]), f.apply(undefined, { length: 2, 0: "a", 1: "b" }), f.apply(null), f.apply(null, undefined), errName(() => f.apply(null, 1)), errName(() => Function.prototype.apply.call(1))],
    call: [f.call(1, "a"), f.call(), errName(() => Function.prototype.call.call({}))],
    bind: [bound(2, 3), boundTwice(3), bound.name, boundTwice.name, bound.length, boundTwice.length, f.bind().length, f.bind(null, 1, 2, 3, 4).length, lengthy.bind().length, String(hugeLen.bind().length), String(hugeLen.bind(null, 1).length),
      fracLen.bind().length, noName.bind().name, instance.x, instance instanceof Ctor, instance instanceof BoundCtor, "prototype" in BoundCtor, errName(() => Function.prototype.bind.call({})), Math.max.bind(null, 1).name],
    boundNew: (() => { function T() { return this instanceof T; } const B = T.bind(null); return [new B() instanceof T, B()]; })(),
    fnProto: [Function.prototype(), Function.prototype.length, Function.prototype.name, typeof Function.prototype, Object.getPrototypeOf(Function.prototype) === Object.prototype],
  };
}
export function run() {
  return {
    reflect: reflect(),
    arrays: arrays(),
    objects: objects(),
    strings: strings(),
    numbers: numbers(),
    globals: globals(),
    errorsAndFunctions: errorsAndFunctions(),
  };
}
