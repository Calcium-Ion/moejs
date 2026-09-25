// Array.prototype.join, toString and toLocaleString on cyclic structures: an
// object already being joined (by join or toLocaleString) joins as "",
// whether the nested join is reached through an element's toString, a
// getter, the separator, the length of an array-like or a proxy, and a join
// that throws partway leaves no mark behind. Errors compare by name.
const t = (f) => { try { return f(); } catch (e) { return e.name; } };
export function run() {
  const r = {};
  r.self = (() => { const a = [1]; a.push(a); return [a.join(), a.join("-"), String(a), a + "", `${a}`, a.toLocaleString()]; })();
  r.onlySelf = (() => { const a = []; a[0] = a; a[1] = a; return a.join(); })();
  r.nested = (() => { const a = [1], b = [2, a]; a.push(b); return [a.join(), b.join("-"), a.toLocaleString(), String(b)]; })();
  r.separatorInside = (() => { const a = [1, 2]; a.push([3, a]); return a.join("-"); })();
  r.sharedNotCyclic = (() => { const b = [1, [2]]; return [b, b, [b]].join(); })();
  r.deepNotCyclic = (() => { let a = [1]; for (let i = 0; i < 50; i++) a = [a, i]; return a.join().length; })();
  r.crossToLocale = (() => { const a = [1, { toString() { return a.toLocaleString(); } }]; return a.join(); })();
  r.crossJoin = (() => { const a = [1, { toLocaleString() { return "<" + a.join() + ">"; } }]; return a.toLocaleString(); })();
  r.otherArray = (() => { const a = [1], b = [a]; a.push({ toString() { return b.join() + a.join(); } }); return [b.join(), a.join()]; })();
  r.getter = (() => { const a = [1, 2]; Object.defineProperty(a, 1, { get() { return "<" + a.join("+") + ">"; } }); return a.join(); })();
  r.inheritedGetter = (() => { const a = [1, , 3]; Object.setPrototypeOf(a, Object.create(Array.prototype, { 1: { get() { return "<" + this.join("+") + ">"; } } })); return a.join(); })();
  r.separator = (() => { const a = [1, 2]; return [[1].join({ toString() { return a.join(); } }), a.join({ toString() { return a.join(); } })]; })();
  r.arrayLike = (() => { const o = { length: 2, 0: 1, join: Array.prototype.join, toString() { return this.join(); } }; o[1] = o; return o.join(); })();
  r.arrayLikeLength = (() => { const o = { get length() { return "<" + Array.prototype.join.call(o) + ">"; } }; return Array.prototype.join.call(o); })();
  r.string = Array.prototype.join.call("ab", Array.prototype.join.call("cd"));
  r.proxy = (() => { const a = [1]; const p = new Proxy(a, {}); a.push(p); return [a.join(), p.join(), String(p), a.toLocaleString(), p.toLocaleString()]; })();
  r.proxyGetter = (() => { const a = [1, 2]; const p = new Proxy(a, { get(tg, k, rc) { return k === "1" ? "<" + p.join("+") + ">" : Reflect.get(tg, k, rc); } }); return p.join(); })();
  r.throwThenJoin = (() => { let n = 0; const a = [1, { toString() { if (n++ === 0) throw new Error("x"); return "y"; } }]; return [t(() => a.join()), a.join()]; })();
  r.throwInside = (() => { let n = 0; const b = [2, { toString() { if (n++ === 0) throw new Error("x"); return "y"; } }]; const a = [1, b]; b.push(a); return [t(() => a.join()), a.join(), b.join()]; })();
  r.throwToLocale = (() => { let n = 0; const a = [1, { toLocaleString() { if (n++ === 0) throw new Error("x"); return "y"; } }]; return [t(() => a.toLocaleString()), a.toLocaleString(), a.join()]; })();
  r.throwLength = (() => { let n = 0; const o = { get length() { if (n++ === 0) throw new Error("x"); return 1; }, 0: "z" }; return [t(() => Array.prototype.join.call(o)), Array.prototype.join.call(o)]; })();
  r.throwSeparator = (() => { const a = [1, 2]; return [t(() => a.join({ toString() { throw new Error("x"); } })), a.join("-")]; })();
  r.symbolElement = (() => { const a = [1, Symbol()]; const e = t(() => a.join()); a[1] = a; return [e, a.join()]; })();
  r.joinReplaced = (() => { const a = [1]; a.push(a); a.join = function () { return "J"; }; return [String(a), String([a, a]), Array.prototype.join.call(a)]; })();
  return r;
}
