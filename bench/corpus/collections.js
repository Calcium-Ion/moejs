// Map, Set, WeakMap, WeakSet: insertion order, SameValueZero keys, deletion
// during iteration, iterator objects and the shapes plugins use them for.
export function run() {
  const m = new Map([["a", 1], ["b", 2]]);
  const k = {id: 1};
  m.set(k, "obj").set(NaN, "nan").set(0, "zero");
  const mapOps = [
    m.size, m.get("a"), m.get(k), m.get(NaN), m.get(-0), m.has("b"), m.has("z"), m.delete("b"), m.delete("b"), m.size,
    [...m.keys()].map(x => typeof x === "object" ? "obj" : String(x)).join(),
    m.set("a", 9) === m, [...m.values()].join(), m.get(Object.keys({x: 1})[0]),
  ];
  const e = [];
  m.forEach(function (v, key, map) { e.push([typeof key === "object" ? "o" : String(key), v, map === m, this.t]); }, {t: "this"});

  const live = new Map([[1, "a"], [2, "b"], [3, "c"]]);
  const seen = [];
  for (const [key] of live) {
    seen.push(key);
    if (key === 1) { live.delete(2); live.set(4, "d"); }
    if (key === 3) live.clear();
  }
  const cleared = [live.size, [...live].length];

  const s = new Set([3, 1, 3, "3", NaN, NaN, 0, -0]);
  const setOps = [s.size, [...s].map(String).join(), s.has(-0), s.has("1"), s.add(1) === s, s.size, s.delete(3), [...s.entries()].map(p => p[0] === p[1]).join()];
  const dedup = [...new Set("mississippi")].join("");
  const union = [...new Set([...[1, 2, 3], ...[2, 3, 4]])];
  const inter = [1, 2, 3, 4].filter(x => new Set([2, 4, 6]).has(x));

  const mi = new Map([[1, 2]])[Symbol.iterator]();
  const si = new Set([1]).values();
  const iters = [
    Object.prototype.toString.call(mi), Object.prototype.toString.call(si), JSON.stringify(mi.next()), JSON.stringify(mi.next()),
    Map.prototype[Symbol.iterator] === Map.prototype.entries, Set.prototype.keys === Set.prototype.values,
    Set.prototype[Symbol.iterator] === Set.prototype.values, Object.prototype.toString.call(new Map()), Map.name, Set.length,
  ];

  const wm = new WeakMap();
  const ws = new WeakSet();
  const w1 = {}, w2 = function () {};
  wm.set(w1, "one").set(w2, "two");
  ws.add(w1);
  const weak = [
    wm.get(w1), wm.get(w2), wm.has({}), wm.delete(w1), wm.has(w1), ws.has(w1), ws.has(w2), ws.delete(w1), ws.has(w1),
    (() => { try { wm.set(1, 1); return "no"; } catch (err) { return err.constructor.name; } })(),
    (() => { try { ws.add("s"); return "no"; } catch (err) { return err.constructor.name; } })(),
    wm.get(1), wm.has("x"), ws.has(null), Object.prototype.toString.call(wm),
  ];

  const errors = [
    (() => { try { Map(); return "no"; } catch (err) { return err.constructor.name; } })(),
    (() => { try { new Map([1]); return "no"; } catch (err) { return err.constructor.name; } })(),
    (() => { try { Map.prototype.get.call({}, 1); return "no"; } catch (err) { return err.constructor.name; } })(),
    (() => { try { new Set(1); return "no"; } catch (err) { return err.constructor.name; } })(),
  ];

  // Plugin-shaped uses: counting, grouping headers, memoising by object.
  const counts = new Map();
  for (const w of "the cat and the hat and the bat".split(" ")) counts.set(w, (counts.get(w) || 0) + 1);
  const top = [...counts].sort((x, y) => y[1] - x[1] || (x[0] < y[0] ? -1 : 1)).slice(0, 3);
  const headers = new Map(Object.entries({"Content-Type": "a", "X-Id": "b"}).map(([h, v]) => [h.toLowerCase(), v]));
  const cache = new WeakMap();
  const memo = obj => { if (!cache.has(obj)) cache.set(obj, Object.keys(obj).length); return cache.get(obj); };
  const cfg = {a: 1, b: 2};
  const fromMap = Object.fromEntries(new Map([["x", 1], ["y", [2]]]));

  return {mapOps, forEach: e, seen, cleared, setOps, dedup, union, inter, iters, weak, errors,
    plugin: [top, [...headers.keys()], headers.get("x-id"), memo(cfg), memo(cfg), JSON.stringify(fromMap)]};
}
