// Proxy and Reflect: the traps each common operation runs and their order
// (a handler that is itself a proxy logs every trap lookup), forwarding to
// the target, revocation, the invariant TypeErrors and the Reflect methods.
// Errors compare by name. Left out where Sobek deviates from the spec:
// Array.prototype.map, concat and the other HasProperty-guarded array
// methods skip that step on a proxy (Sobek runs no has trap and reads the
// indices its target lacks), so the array-like proxy below has a has trap
// and concat shows only its result.
const t = (f) => { try { return f(); } catch (e) { return e.name; } };
const keyed = new Set(["get", "set", "has", "deleteProperty", "getOwnPropertyDescriptor", "defineProperty"]);
function traced(log) {
  return (target) => new Proxy(target, new Proxy({}, {
    get(_, trap) {
      return (...args) => { log.push(keyed.has(trap) ? trap + " " + String(args[1]) : trap); return Reflect[trap](...args); };
    },
  }));
}
// order runs f with a fresh logged() and returns its result and the log.
function order(f) {
  const log = [];
  const res = t(() => f(traced(log)));
  return [String(res), log.join()];
}
export function run() {
  const r = {};
  r.basic = order((logged) => { const p = logged({ a: 1 }); p.a; p.b = 2; "a" in p; delete p.a; return [p.b, "a" in p]; });
  r.keys = order((logged) => Object.keys(logged({ a: 1, b: 2 })));
  r.values = order((logged) => Object.values(logged({ a: 1, b: 2 })));
  r.entries = order((logged) => Object.entries(logged({ a: 1 })));
  r.names = order((logged) => Object.getOwnPropertyNames(logged({ a: 1, [Symbol("s")]: 2 })));
  r.descriptors = order((logged) => JSON.stringify(Object.getOwnPropertyDescriptors(logged({ a: 1 }))));
  r.assignFrom = order((logged) => { const o = Object.assign({}, logged({ a: 1, b: 2 })); return o.a + o.b; });
  r.assignTo = order((logged) => { const p = logged({}); Object.assign(p, { a: 1 }); return p.a; });
  r.spread = order((logged) => { const p = logged({ a: 1, b: 2 }); const { a, ...rest } = p; const o = { ...p }; return [a, rest.b, o.a + o.b]; });
  r.stringify = order((logged) => JSON.stringify(logged({ a: 1, b: [2] })));
  r.instanceofOp = order((logged) => logged({}) instanceof Object);
  r.freeze = order((logged) => { const p = logged({ a: 1 }); Object.freeze(p); return Object.isFrozen(p); });
  r.seal = order((logged) => { const p = logged({ a: 1 }); Object.seal(p); return Object.isSealed(p); });
  r.protoExt = order((logged) => { const p = logged({}); Object.getPrototypeOf(p); Object.setPrototypeOf(p, null); Object.isExtensible(p); Object.preventExtensions(p); return Object.isExtensible(p); });
  r.hasOwn = order((logged) => { const p = logged({ a: 1 }); return [Object.hasOwn(p, "a"), p.hasOwnProperty("b"), Object.prototype.propertyIsEnumerable.call(p, "a")]; });
  r.defineProperty = order((logged) => { const p = logged({}); Object.defineProperty(p, "x", { value: 1 }); return Object.getOwnPropertyDescriptor(p, "x").writable; });
  r.callConstruct = order((logged) => { const f = logged(function (a) { return a + 1; }); return [f(1), typeof new f(2), f.call(null, 3), f.apply(null, [4])]; });
  r.arrayPush = order((logged) => { const a = logged([1, 2]); a.push(3); return a.length; });
  r.arrayIterate = order((logged) => [...logged([1, 2])]);
  r.isArray = order((logged) => [Array.isArray(logged([])), Array.isArray(logged({})), Array.isArray(logged(logged([])))]);
  r.concat = [].concat(new Proxy([1, 2], {}), new Proxy({ length: 1, 0: "x", [Symbol.isConcatSpreadable]: true }, {})).join();
  r.toPrimitive = order((logged) => String(logged({ toString() { return "s"; } })));
  r.asPrototype = order((logged) => { const c = Object.create(logged({ x: 1 })); c.y = 2; return [c.x, "z" in c, Object.keys(c)]; });
  r.getter = order((logged) => { const target = { get self() { return this; } }; const p = logged(target); return p.self === p; });

  // A handler's traps are looked up on every operation.
  const h = {};
  const hp = new Proxy({ a: 1 }, h);
  const handlerLive = [hp.a];
  h.get = () => 2;
  handlerLive.push(hp.a);
  delete h.get;
  handlerLive.push(hp.a);
  r.handlerLive = handlerLive;

  // Trap arguments and this.
  let got, self;
  const target = {};
  const handler = { set(...a) { got = a; self = this; return true; } };
  const tp = new Proxy(target, handler);
  tp.k = 1;
  r.trapArgs = [got.length, got[0] === target, got[1], got[2], got[3] === tp, self === handler, "k" in target];

  // Virtual objects.
  const virt = new Proxy({}, {
    get: (_, k) => (typeof k === "string" ? k.toUpperCase() : undefined),
    has: (_, k) => k.startsWith("v"),
    ownKeys: () => ["v1", "v2"],
    getOwnPropertyDescriptor: (_, k) => ({ value: k, enumerable: true, configurable: true }),
  });
  r.virtual = [virt.abc, "vx" in virt, "x" in virt, Object.keys(virt), JSON.stringify(virt), Object.entries(virt).join("|")];
  const counts = new Proxy([], { get: (tg, k) => (k === "length" ? 3 : k in tg ? tg[k] : Number(k) * 10), has: (tg, k) => k in tg || /^\d+$/.test(k) });
  r.arrayLike = [Array.from(counts), Array.prototype.map.call(counts, (x) => x + 1), Array.prototype.join.call(counts, "-")];

  // Revocation.
  const rv = Proxy.revocable({ a: 1 }, {});
  const rf = Proxy.revocable(function () {}, {});
  r.beforeRevoke = [rv.proxy.a, typeof rf.proxy, Object.keys(rv).join(), rv.revoke.length];
  rv.revoke();
  rf.revoke();
  rv.revoke();
  const p = rv.proxy;
  const f = rf.proxy;
  r.revoked = [() => p.a, () => { p.a = 1; }, () => "a" in p, () => delete p.a, () => Object.keys(p), () => Object.getOwnPropertyDescriptor(p, "a"), () => Object.defineProperty(p, "a", {}), () => Object.getPrototypeOf(p), () => Object.setPrototypeOf(p, null), () => Object.isExtensible(p), () => Object.preventExtensions(p), () => f(), () => new f(), () => Array.isArray(p), () => JSON.stringify(p), () => p instanceof Object, () => Object.create(p).a, () => typeof p, () => typeof f, () => new Proxy(p, {}) !== p].map(t);

  // Invariants.
  const frozen = Object.freeze({ a: 1 });
  const sealed = Object.preventExtensions({ b: 2 });
  r.invariants = [
    () => new Proxy(frozen, { get: () => 2 }).a,
    () => new Proxy(frozen, { get: () => 1 }).a,
    () => new Proxy(frozen, { has: () => false }).a,
    () => "a" in new Proxy(frozen, { has: () => false }),
    () => "b" in new Proxy(sealed, { has: () => false }),
    () => { "use strict"; new Proxy(frozen, { set: () => true }).a = 2; },
    () => { "use strict"; new Proxy({}, { set: () => false }).a = 2; },
    () => Reflect.set(new Proxy({}, { set: () => false }), "a", 1),
    () => delete new Proxy(frozen, { deleteProperty: () => true }).a,
    () => Reflect.deleteProperty(new Proxy({}, { deleteProperty: () => false }), "a"),
    () => Object.keys(new Proxy(frozen, { ownKeys: () => [] })),
    () => Object.keys(new Proxy(sealed, { ownKeys: () => ["b", "c"] })),
    () => Object.keys(new Proxy({}, { ownKeys: () => ["a", "a"] })),
    () => Object.keys(new Proxy({}, { ownKeys: () => [1] })),
    () => Object.keys(new Proxy({}, { ownKeys: () => "ab" })),
    () => Object.getOwnPropertyDescriptor(new Proxy(frozen, { getOwnPropertyDescriptor: () => undefined }), "a"),
    () => Object.getOwnPropertyDescriptor(new Proxy({}, { getOwnPropertyDescriptor: () => ({ value: 1, configurable: false }) }), "a"),
    () => Object.getOwnPropertyDescriptor(new Proxy({}, { getOwnPropertyDescriptor: () => 1 }), "a"),
    () => Object.defineProperty(new Proxy(sealed, { defineProperty: () => true }), "c", { value: 1 }),
    () => Object.defineProperty(new Proxy({}, { defineProperty: () => false }), "c", { value: 1 }),
    () => Reflect.defineProperty(new Proxy({}, { defineProperty: () => false }), "c", { value: 1 }),
    () => Object.getPrototypeOf(new Proxy(sealed, { getPrototypeOf: () => null })),
    () => Object.getPrototypeOf(new Proxy({}, { getPrototypeOf: () => 1 })),
    () => Object.getPrototypeOf(new Proxy({}, { getPrototypeOf: () => Array.prototype })) === Array.prototype,
    () => Object.setPrototypeOf(new Proxy({}, { setPrototypeOf: () => false }), null),
    () => Reflect.setPrototypeOf(new Proxy({}, { setPrototypeOf: () => false }), null),
    () => Object.isExtensible(new Proxy({}, { isExtensible: () => false })),
    () => Object.preventExtensions(new Proxy({}, { preventExtensions: () => true })),
    () => Object.preventExtensions(new Proxy({}, { preventExtensions: () => false })),
    () => Reflect.preventExtensions(new Proxy({}, { preventExtensions: () => false })),
    () => new (new Proxy(function () {}, { construct: () => 1 }))(),
    () => new (new Proxy(() => {}, {}))(),
    () => new Proxy({}, { apply: () => 1 })(),
    () => new Proxy({}, { get: 1 }).a,
    () => new Proxy({}, { get: null }).a,
    () => new Proxy({}, { get: undefined, has: () => true }).a,
  ].map(t);

  // The constructor.
  r.ctor = [() => Proxy.length, () => Proxy.name, () => "prototype" in Proxy, () => typeof Proxy, () => Proxy({}, {}), () => new Proxy(1, {}), () => new Proxy({}, null), () => typeof new Proxy(class {}, {}), () => Object.getPrototypeOf(new Proxy([], {})) === Array.prototype, () => Object.prototype.toString.call(new Proxy([], {})), () => Object.prototype.toString.call(new Proxy(new Date(0), {})), () => Object.prototype.toString.call(new Proxy(function () {}, {}))].map(t);

  // Reflect.
  const obj = { a: 1, get g() { return this; } };
  const recv = { b: 2 };
  class Base { constructor(x) { this.x = x; this.nt = new.target === Base; } }
  class Other {}
  r.reflect = [
    () => Reflect.get(obj, "a"),
    () => Reflect.get(obj, "g", recv) === recv,
    () => Reflect.set(recv, "c", 3) && recv.c,
    () => Reflect.set(Object.freeze({ a: 1 }), "a", 2),
    () => Reflect.has(obj, "a"),
    () => Reflect.has(obj, "toString"),
    () => Reflect.ownKeys({ b: 1, a: 2, 1: 3, [Symbol.iterator]: 4 }).map(String).join(),
    () => Reflect.deleteProperty(Object.freeze({ a: 1 }), "a"),
    () => Reflect.defineProperty({}, "a", { value: 1 }),
    () => Reflect.defineProperty(Object.freeze({}), "a", { value: 1 }),
    () => JSON.stringify(Reflect.getOwnPropertyDescriptor(obj, "a")),
    () => Reflect.getPrototypeOf([]) === Array.prototype,
    () => Reflect.setPrototypeOf({}, null),
    () => Reflect.setPrototypeOf(Object.preventExtensions({}), {}),
    () => Reflect.isExtensible(Object.seal({})),
    () => Reflect.preventExtensions({}),
    () => Reflect.apply(Math.max, null, [1, 3, 2]),
    () => Reflect.apply(function () { return this; }, "s", []) === "s",
    () => Reflect.construct(Base, [5]).x,
    () => Reflect.construct(Base, [5]).nt,
    () => Object.getPrototypeOf(Reflect.construct(Base, [5], Other)) === Other.prototype,
    () => Reflect.construct(Base, [5], Other).nt,
    () => Reflect.construct(Array, [3], Other).length,
    () => Reflect.construct(Date, [0]).getTime(),
    () => Reflect.get(1, "a"),
    () => Reflect.ownKeys("s"),
    () => Reflect.apply(1, null, []),
    () => Reflect.apply(Math.max, null, 1),
    () => Reflect.construct(() => {}, []),
    () => Reflect.construct(Base, [], () => {}),
    () => Reflect.defineProperty({}, "a", 1),
    () => Reflect.setPrototypeOf({}, 1),
    () => Reflect[Symbol.toStringTag],
    () => typeof Reflect,
    () => Object.keys(Reflect).length,
    () => Object.getOwnPropertyNames(Reflect).sort().join(),
  ].map(t);

  // Proxies of proxies and of builtins.
  const inner = new Proxy({ a: 1 }, { get: (tg, k) => (k === "a" ? 10 : tg[k]) });
  const outer = new Proxy(inner, { get: (tg, k) => (k === "a" ? tg.a + 1 : tg[k]) });
  const m = new Proxy(new Map([[1, 2]]), {});
  const dp = new Proxy(new Date(0), {});
  r.nested = [outer.a, outer.b, Object.keys(outer), t(() => m.get(1)), t(() => Map.prototype.get.call(m, 1)), t(() => dp.getTime()), t(() => [1, 2, 3].map(new Proxy((x) => x * 2, {})).join())];
  const arr = new Proxy([3, 1, 2], {});
  arr.sort();
  r.arrayMethods = [arr.join(), arr.length, arr.slice(1).join(), arr.indexOf(2), arr.includes(3), arr.reverse().join(), JSON.stringify(arr), [...arr.entries()].join("|"), arr.at(-1), Array.from(arr).length];
  r.exported = { plainOfProxy: { ...new Proxy({ x: 1, y: [1, 2] }, {}) }, list: Array.from(new Proxy([1, "two"], {})) };
  // The reviver walk takes a proxy array's length from its get trap.
  r.reviverTrapLength = (() => { const seen = []; const p = new Proxy([], { get: (tg, k) => (k === "length" ? 3 : typeof k === "string" ? "e" + k : undefined) }); const out = JSON.parse("[1,2]", function (k, v) { if (k === "0") this[1] = p; seen.push(k); return v; }); return [seen.join(), JSON.stringify(out)]; })();
  // An array whose prototype is a proxy that has index 1: the array
  // methods read and write through it (results and own elements only, as
  // Sobek skips the has traps).
  r.protoProxyArray = (() => {
    const own = (a) => Object.getOwnPropertyNames(a).filter((k) => k !== "length").map((k) => k + "=" + String(a[k])).join(" ");
    const ops = {
      slice: (a) => a.slice(), toReversed: (a) => a.toReversed(), toSorted: (a) => a.toSorted(), with: (a) => a.with(0, 9),
      toSpliced: (a) => a.toSpliced(0, 0), copyWithin: (a) => a.copyWithin(0, 1), splice: (a) => a.splice(0, 2), shift: (a) => a.shift(),
      unshift: (a) => a.unshift(7), fill: (a) => a.fill(5, 1, 2), push: (a) => a.push(7), sort: (a) => a.sort(), pop: (a) => a.pop(),
      reverse: (a) => (a.push(3), a.reverse()),
    };
    const out = {};
    for (const name of Object.keys(ops)) {
      const a = [0, , 2];
      Object.setPrototypeOf(a, new Proxy(Array.prototype, {
        get: (tg, k, rc) => (k === "1" ? "X" : Reflect.get(tg, k, rc)),
        has: (tg, k) => k === "1" || Reflect.has(tg, k),
      }));
      const res = t(() => ops[name](a));
      out[name] = (res === a ? "a" : JSON.stringify(res)) + "|" + own(a);
    }
    return out;
  })();
  return r;
}
