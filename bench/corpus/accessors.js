function errName(f) {
  try { f(); return "no error"; } catch (e) { return e.name; }
}
function desc(o, k) {
  const d = Object.getOwnPropertyDescriptor(o, k);
  if (!d) return d;
  return "get" in d
    ? { get: typeof d.get, set: typeof d.set, enumerable: d.enumerable, configurable: d.configurable }
    : { value: d.value, writable: d.writable, enumerable: d.enumerable, configurable: d.configurable };
}
const key = "dyn";
function literals() {
  let log = [];
  const o = {
    v: 1,
    get x() { return this.v * 10; },
    set x(n) { log.push(n); this.v = n; },
    get [key + "Get"]() { return "computed"; },
    get 7() { return "seven"; },
    get "a b"() { return "spaced"; },
    set only(v) { log.push("only:" + v); },
  };
  o.x = 4;
  o.only = 5;
  const pair = { get p() { return 1; }, set p(v) {} };
  const overridden = { get q() { return "getter"; }, q: "data" };
  const reget = { q: "data", get q() { return "getter"; } };
  return {
    x: o.x, log, dyn: o.dynGet, seven: o[7], spaced: o["a b"], only: o.only,
    keys: Object.keys(o), pair: desc(pair, "p"), overridden: desc(overridden, "q"), reget: [reget.q, desc(reget, "q")],
    names: [Object.getOwnPropertyDescriptor(o, "x").get.name, Object.getOwnPropertyDescriptor(o, "x").set.name, Object.getOwnPropertyDescriptor(o, "dynGet").get.name, Object.getOwnPropertyDescriptor(o, "7").get.name],
    lengths: [Object.getOwnPropertyDescriptor(o, "x").get.length, Object.getOwnPropertyDescriptor(o, "x").set.length],
  };
}
function receivers() {
  const proto = { get who() { return this.name; }, set who(v) { this.seen = v; } };
  const a = Object.create(proto), b = Object.create(proto);
  a.name = "a"; b.name = "b";
  const out = [];
  for (let i = 0; i < 6; i++) { const o = i % 2 ? b : a; o.who = i; out.push(o.who, o.seen); }
  const getterOnly = Object.create({ get ro() { return 1; } });
  return {
    out, ownWho: a.hasOwnProperty("who"), protoSeen: proto.seen,
    assignGetterOnly: errName(() => { getterOnly.ro = 2; }), stillRo: getterOnly.ro, ownRo: Object.keys(getterOnly),
  };
}
function enumeration() {
  let calls = 0;
  const src = { a: 1, get b() { calls++; return 2; }, c: 3 };
  Object.defineProperty(src, "hidden", { get() { calls++; return "h"; }, enumerable: false });
  const forIn = [];
  for (const k in src) forIn.push(k);
  const keys = Object.keys(src), values = Object.values(src), entries = Object.entries(src);
  const spread = { ...src };
  const setLog = [];
  const target = { set b(v) { setLog.push("set b " + v); } };
  const assigned = Object.assign(target, src);
  const json = JSON.stringify(src);
  const { a, b, hidden } = src;
  const [first] = [src.b];
  return {
    forIn, keys, values, entries, spread, spreadDesc: desc(spread, "b"), setLog, assignedB: desc(assigned, "b"), json, destructured: [a, b, hidden, first], calls,
    inOps: ["b" in src, "hidden" in src, src.hasOwnProperty("hidden"), src.propertyIsEnumerable("hidden"), delete src.b, "b" in src],
    jsonNested: JSON.stringify({ outer: { get inner() { return [1, { get deep() { return "d"; } }]; } } }),
    jsonToJSON: JSON.stringify({ get toJSON() { return function () { return "via toJSON"; }; } }),
    jsonGetterKeyDrop: JSON.stringify({ get a() { Object.defineProperty(this, "b", { enumerable: false }); return 1; }, b: 2 }),
  };
}
function descriptors() {
  const o = {};
  Object.defineProperty(o, "g", { get() { return "g"; }, configurable: true });
  const before = desc(o, "g");
  Object.defineProperty(o, "g", { set(v) { this.last = v; } });
  o.g = 9;
  const afterSet = [desc(o, "g"), o.g, o.last];
  Object.defineProperty(o, "g", { value: "data" });
  const toData = desc(o, "g");
  Object.defineProperties(o, { m: { get() { return "m"; }, enumerable: true }, n: { value: 1 } });
  const created = Object.create({ inherited: 1 }, { own: { get() { return this.inherited + 1; }, enumerable: true } });
  const errors = [
    errName(() => Object.defineProperty({}, "x", { get: 1 })),
    errName(() => Object.defineProperty({}, "x", { get() {}, value: 1 })),
    errName(() => Object.defineProperty({}, "x", { set() {}, writable: true })),
    errName(() => Object.defineProperty(o, "m", { get() { return "other"; } })),
    errName(() => Object.defineProperty(o, "m", { value: 1 })),
  ];
  const all = Object.getOwnPropertyDescriptors({ get a() { return 1; }, b: 2 });
  return {
    before, afterSet, toData, m: [o.m, desc(o, "m")], n: desc(o, "n"), created: [created.own, Object.keys(created)], errors,
    allKeys: Object.keys(all), allA: [typeof all.a.get, all.a.set, all.a.enumerable], allB: all.b,
  };
}
function integrity() {
  let setterRuns = 0;
  const f = Object.freeze({ v: 1, get x() { return this.v; }, set x(n) { setterRuns++; } });
  f.x = 5;
  const s = Object.seal({ get y() { return 2; } });
  const n = Object.preventExtensions({ get z() { return 3; } });
  return {
    frozen: [Object.isFrozen(f), f.x, setterRuns, desc(f, "x")],
    sealed: [Object.isSealed(s), Object.isFrozen(s), desc(s, "y"), errName(() => { delete s.y; })],
    nonExtensible: [Object.isExtensible(n), Object.isFrozen(n), errName(() => Object.defineProperty(n, "w", { get() {} }))],
    frozenGetterOnly: Object.isFrozen(Object.preventExtensions(Object.defineProperty({}, "k", { get() {} }))),
  };
}
function arrays() {
  const a = [1, 2, 3];
  let reads = 0;
  Object.defineProperty(a, 1, { get() { reads++; return "two"; }, set(v) { this.written = v; }, configurable: true, enumerable: true });
  a[1] = "ignored";
  const snapshot = [a.length, a[1], a.join("-"), a.indexOf("two"), a.slice(), a.map(x => x), JSON.stringify(a), Object.keys(a), a.written];
  a.push(4);
  const afterPush = [a.length, a.join()];
  const b = [0, , 2];
  Object.defineProperty(b, 1, { get() { return "hole"; }, configurable: true });
  const holes = [b.join(), b.filter(() => true), [...b], b.includes("hole")];
  const c = [3, 1, 2];
  Object.defineProperty(c, 0, { get() { return 3; }, set(v) { this.setTo = (this.setTo || []).concat([v]); }, configurable: true });
  c.reverse();
  const reversed = [c.setTo, c[2], c.length];
  const len = [1, 2];
  Object.defineProperty(len, "length", { writable: false });
  return {
    snapshot, reads: reads > 0, afterPush, holes, reversed,
    lengthFrozen: [errName(() => len.push(3)), len.length, errName(() => { len[5] = 1; }), len.length],
    arrayLikeGetter: Array.prototype.map.call({ length: 2, get 0() { return "g0"; }, 1: "d1" }, x => x),
  };
}
function builtinAccessors() {
  const p = { tag: "p" };
  const o = {};
  o.__proto__ = p;
  const d = Object.getOwnPropertyDescriptor(Object.prototype, "__proto__");
  const nullProto = {};
  nullProto.__proto__ = null;
  const ignored = {};
  ignored.__proto__ = 5;
  const literal = { __proto__: p };
  const computed = { ["__proto__"]: p };
  const fromJSON = JSON.parse('{"__proto__": {"tag": "json"}}');
  const fd = Object.getOwnPropertyDescriptor(Function.prototype, "caller");
  return {
    set: [o.tag, Object.getPrototypeOf(o) === p, o.__proto__ === p, o.hasOwnProperty("__proto__")],
    descriptor: [typeof d.get, typeof d.set, d.get.name, d.set.name, d.enumerable],
    primitives: [(1).__proto__ === Number.prototype, "s".__proto__ === String.prototype, [].__proto__ === Array.prototype],
    nullProto: [Object.getPrototypeOf(nullProto) === null, nullProto.__proto__, "__proto__" in nullProto],
    ignored: Object.getPrototypeOf(ignored) === Object.prototype,
    literal: [literal.tag, Object.keys(literal)],
    computed: [computed.tag, Object.keys(computed), Object.getPrototypeOf(computed) === Object.prototype],
    fromJSON: [fromJSON.tag, Object.keys(fromJSON)],
    assign: (() => { const t = Object.assign({}, computed); return [t.tag, Object.keys(t)]; })(),
    cycle: errName(() => { p.__proto__ = o; }),
    caller: [errName(() => (function () {}).caller), errName(() => { (() => 1).arguments = 1; }), fd.get === fd.set, fd.get.name, fd.enumerable],
  };
}
function caches() {
  const shapes = [
    { get v() { return 1; } },
    { a: 0, get v() { return 2; } },
    Object.create({ get v() { return 3; } }),
    { v: 4 },
  ];
  let sum = 0;
  for (let i = 0; i < 400; i++) sum += shapes[i % 4].v;
  const counter = { n: 0, set bump(k) { this.n += k; } };
  for (let i = 0; i < 100; i++) counter.bump = i;
  const flip = {};
  Object.defineProperty(flip, "x", { get() { return "g1"; }, configurable: true });
  const seen = [];
  for (let i = 0; i < 4; i++) {
    seen.push(flip.x);
    if (i === 1) Object.defineProperty(flip, "x", { get() { return "g2"; } });
    if (i === 2) Object.defineProperty(flip, "x", { value: "data" });
  }
  const proto = {};
  const child = Object.create(proto);
  const inherited = [];
  for (let i = 0; i < 4; i++) {
    inherited.push(child.late);
    if (i === 1) Object.defineProperty(proto, "late", { get() { return "late getter"; }, configurable: true });
  }
  return { sum, n: counter.n, seen, inherited };
}
function throwing() {
  const o = { get boom() { throw new RangeError("boom"); }, set boom(v) { throw new TypeError("set boom " + v); } };
  const out = [];
  try { o.boom; } catch (e) { out.push(e.name + ": " + e.message); }
  try { o.boom = 1; } catch (e) { out.push(e.name + ": " + e.message); }
  try { JSON.stringify(o); } catch (e) { out.push("json " + e.name); }
  try { ({ ...o }); } catch (e) { out.push("spread " + e.name); }
  try { Object.assign({}, o); } catch (e) { out.push("assign " + e.name); }
  try { throw { get message() { throw new Error("inner"); } }; } catch (e) { try { e.message; } catch (e2) { out.push("nested " + e2.message); } }
  return out;
}
export function run() {
  return {
    literals: literals(),
    receivers: receivers(),
    enumeration: enumeration(),
    descriptors: descriptors(),
    integrity: integrity(),
    arrays: arrays(),
    builtinAccessors: builtinAccessors(),
    caches: caches(),
    throwing: throwing(),
    // Export calls enumerable getters and skips non-enumerable ones.
    exported: Object.defineProperty({ plain: 1, get live() { return { nested: [1, 2], get deeper() { return "d"; } }; } }, "hidden", { get() { return "no"; } }),
  };
}
