export function run() {
  const proto = { greet() { return "hi " + this.name; }, kind: "proto" };
  const o = Object.create(proto);
  o.name = "obj";
  const merged = Object.assign({}, { a: 1, b: { c: 2 } }, { b: 3, d: 4 }, null, undefined, "xy");
  const frozen = Object.freeze({ x: 1, inner: { y: 2 } });
  let frozenErr = false;
  try { frozen.x = 2; } catch (e) { frozenErr = e instanceof TypeError; }
  frozen.inner.y = 3;
  const key = "dyn" + 1;
  const literal = { [key]: 1, ["b" + 2]: 2, 3: "three", 1: "one", "z": 0, method() { return this.z; }, ...{ spread: true, z: 5 } };
  const desc = {};
  Object.defineProperty(desc, "hidden", { value: 42, enumerable: false });
  Object.defineProperty(desc, "shown", { value: 43, enumerable: true, writable: false });
  let writeErr = false;
  try { desc.shown = 1; } catch (e) { writeErr = e instanceof TypeError; }
  const { a, b: { c = 9 } = {}, ...rest } = { a: 1, b: {}, x: 2, y: 3 };
  return {
    keys: Object.keys(literal), values: Object.values(literal).map(v => typeof v), entries: Object.entries({ b: 1, a: 2, 1: 3 }), fromEntries: Object.fromEntries([["a", 1], ["b", 2]]),
    protoChain: [o.greet(), o.kind, Object.getPrototypeOf(o) === proto, proto.isPrototypeOf(o), o.hasOwnProperty("kind"), Object.hasOwn(o, "name"), "kind" in o, Object.keys(o)],
    merged: merged, frozen: [frozenErr, Object.isFrozen(frozen), frozen.x, frozen.inner.y, Object.isFrozen(frozen.inner)],
    literal: [literal.dyn1, literal.b2, literal[3], literal.method(), literal.spread, literal.z],
    defined: [desc.hidden, Object.keys(desc), Object.getOwnPropertyNames(desc), JSON.stringify(desc), desc.propertyIsEnumerable("hidden"), writeErr],
    destructured: { a, c, rest }, deleted: (() => { const d = { p: 1, q: 2, r: 3 }; delete d.q; return [Object.keys(d), "q" in d, delete d.zzz]; })(),
    toStringTags: [Object.prototype.toString.call([]), Object.prototype.toString.call(null), Object.prototype.toString.call(undefined), Object.prototype.toString.call(1), Object.prototype.toString.call(""), Object.prototype.toString.call(true), Object.prototype.toString.call(function () {}), Object.prototype.toString.call(/x/), Object.prototype.toString.call(new Date(0)), Object.prototype.toString.call(new Error("x")), String({}), String([1, [2]]), `${{ toString() { return "custom"; } }}`],
    optional: [o?.name, o?.missing?.deep, o.missing?.(), null ?? "d", 0 ?? "d", "" || "d", 0 || null || "last", 1 && 2 && 3],
    functionProps: [(function named(a, b) {}).name, (function (a, b = 1, ...c) {}).length, ((a, b) => {}).length, (() => {}).name, (function () {}).name, ({ m() {} }).m.name, (function () {}).bind(null).name],
    thisBinding: [(function () { return this; })(), ({ f() { return this.v; }, v: 1 }).f(), typeof (function () { return this; }).call(5), (function () { return this.v; }).apply({ v: 8 }, []), (function () { return this.v; }).bind({ v: 9 })()],
    numericKeys: Object.keys({ 2: 1, 1: 1, "10": 1, "01": 1, "-1": 1, b: 1, a: 1, 4294967295: 1, 4294967294: 1 }),
    nullProto: Object.getOwnPropertyNames(Object.assign(Object.create(null), { z: 1 })),
    setProto: (() => { const p = { v: 1 }; const c = {}; Object.setPrototypeOf(c, p); return [c.v, Object.getPrototypeOf(c) === p, Object.getPrototypeOf(Object.prototype)]; })(),
    instance: [[] instanceof Array, [] instanceof Object, {} instanceof Array, new Error("x") instanceof Error, new TypeError("x") instanceof Error, Object(1) instanceof Number, "x" instanceof String, (function () {}) instanceof Function],
  };
}
