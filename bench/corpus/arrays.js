export function run() {
  const a = [5, 3, 8, 1, 9, 2];
  const holes = [1, , 3];
  const copy = a.slice();
  copy.sort();
  const nums = a.slice().sort((x, y) => x - y);
  const words = ["banana", "Apple", "cherry", "apple"];
  return {
    map: a.map((x, i) => x * i), filter: a.filter(x => x % 2), reduce: a.reduce((s, x) => s + x, 0), reduceRight: ["a", "b", "c"].reduceRight((s, x) => s + x),
    find: [a.find(x => x > 4), a.findIndex(x => x > 4), a.findLast(x => x > 4), a.findLastIndex(x => x > 4), a.indexOf(8), a.lastIndexOf(9), a.includes(2), a.includes(NaN), [NaN].includes(NaN), [NaN].indexOf(NaN)],
    some: [a.some(x => x > 8), a.every(x => x > 0)], sorted: [copy, nums, words.slice().sort(), [10, 9, 1].sort(), [3, 1, 2].sort((x, y) => y - x)],
    splice: (() => { const b = a.slice(); const removed = b.splice(1, 2, "x", "y", "z"); return [b, removed]; })(),
    stack: (() => { const b = [1]; b.push(2, 3); const p = b.pop(); b.unshift(0); const s = b.shift(); return [b, p, s, b.length]; })(),
    concat: [1].concat([2, [3]], 4, [[5]]), flat: [[1, [2, [3]]], 4].flat(2), flatMap: [1, 2].flatMap(x => [x, x * 10]),
    fill: new Array(3).fill(7), from: [Array.from("héy"), Array.from({ length: 3 }, (_, i) => i * i), Array.from([1, 2], x => x + 1), Array.of(1, 2)],
    holes: [holes.length, 1 in holes, holes.map(x => x * 2), holes.join("-"), holes.indexOf(undefined), holes.includes(undefined), JSON.stringify(holes), Object.keys(holes)],
    spread: [...a, ...[10]], destructure: (([x, , y = 4, ...rest]) => ({ x, y, rest }))(a), reverse: a.slice().reverse(), at: [a.at(-1), a.at(0), a.at(99)],
    entries: [...a.entries()].slice(0, 2), keys: [...a.keys()], values: [...a.values()].length, forOf: (() => { let s = 0; for (const x of a) s += x; return s; })(),
    join: [[1, null, undefined, 2].join(), [[1, 2], [3]].join(";"), String([1, [2, 3]]), [].join(), [null].toString()],
    length: (() => { const b = [1, 2, 3]; b.length = 1; b[4] = 5; return [b.length, b, b[3]]; })(),
    isArray: [Array.isArray([]), Array.isArray({ length: 0 }), Array.isArray("x")], typeofs: [typeof [], typeof null, typeof undefined, typeof (() => 1), typeof 1n, typeof JSON, typeof Math.max],
    forEach: (() => { const out = []; ["a", "b"].forEach((v, i, arr) => out.push(v + i + arr.length)); return out; })(),
    stableSort: [{ k: 1, v: "a" }, { k: 0, v: "b" }, { k: 1, v: "c" }, { k: 0, v: "d" }].sort((p, q) => p.k - q.k).map(o => o.v).join(""),
    nested: [[1, 2], [3, 4]].map(([p, q]) => p + q), objectKeysArr: Object.keys(["x", "y"]), inOp: [0 in [1], 5 in [1], "length" in []],
  };
}
