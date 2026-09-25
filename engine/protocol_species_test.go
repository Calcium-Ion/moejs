package engine

import "testing"

// speciesSetup makes `a` an array whose species is C, a constructor that
// records its argument.
const speciesSetup = `function C(n) { this.n = n; } const a = [1, 2, 3]; a.constructor = {[Symbol.species]: C}; `

func TestArraySpecies(t *testing.T) {
	runProtoCases(t, []protoCase{
		{"getters", `const d = Object.getOwnPropertyDescriptor(Array, Symbol.species); return [Array[Symbol.species] === Array, RegExp[Symbol.species] === RegExp, d.get.name, d.set, d.enumerable].join()`, "true,true,get [Symbol.species],,false"},
		{"map", speciesSetup + `const r = a.map(x => x * 2); return [r instanceof C, r.n, r[0], r[2], Array.isArray(r), "length" in r].join()`, "true,3,2,6,false,false"},
		{"filter", speciesSetup + `const r = a.filter(x => x !== 2); return [r instanceof C, r.n, r[0], r[1], "length" in r].join()`, "true,0,1,3,false"},
		{"slice", speciesSetup + `const r = a.slice(1); return [r instanceof C, r.n, r[0], r[1], r.length].join()`, "true,2,2,3,2"},
		{"splice", speciesSetup + `const r = a.splice(0, 2, "x"); return [r instanceof C, r.n, r[0], r[1], r.length, a.join()].join()`, "true,2,1,2,2,x,3"},
		{"concat", speciesSetup + `const r = a.concat([4], 5); return [r instanceof C, r.n, r[0], r[3], r[4], r.length].join()`, "true,0,1,4,5,5"},
		{"flat", `function C(n) { this.n = n; } const a = [[1], [2, [3]]]; a.constructor = {[Symbol.species]: C}; const r = a.flat(); return [r instanceof C, r.n, r[0], r[1], Array.isArray(r[2]), "length" in r].join()`, "true,0,1,2,true,false"},
		{"flatMap", speciesSetup + `const r = a.flatMap(x => [x, x]); return [r instanceof C, r.n, r[0], r[5], "length" in r].join()`, "true,0,1,3,false"},
		{"null species", `const a = [1]; a.constructor = {[Symbol.species]: null}; return Array.isArray(a.map(x => x))`, "true"},
		{"undefined constructor", `const a = [1]; a.constructor = undefined; return Array.isArray(a.slice())`, "true"},
		{"primitive constructor", `const a = [1]; a.constructor = 1; return a.map(x => x)`, "!TypeError"},
		{"non-constructor species", `const a = [1]; a.constructor = {[Symbol.species]: () => 0}; return a.filter(x => x)`, "!TypeError"},
		{"species Array", `const a = [1]; a.constructor = {[Symbol.species]: Array}; return Array.isArray(a.concat(2))`, "true"},
		{"non-array receiver ignores species", `function C() {} const o = {length: 1, 0: 1, constructor: {[Symbol.species]: C}}; return Array.isArray(Array.prototype.map.call(o, x => x))`, "true"},
		{"species read once per call", `let reads = 0; const a = [1, 2]; a.constructor = {get [Symbol.species]() { reads++; return undefined; }}; a.map(x => x); a.concat(); return reads`, "2"},
		{"species created before callbacks", `const log = []; function C(n) { log.push("new " + n); } const a = [1, 2]; a.constructor = {[Symbol.species]: C}; a.map(x => log.push(x)); return log.join()`, "new 2,1,2"},
		{"frozen species result", `const target = Object.freeze([]); const a = [1]; a.constructor = {[Symbol.species]: function () { return target; }}; return a.map(x => x)`, "!TypeError"},
		{"prototype chain species", `function S(n) { this.len = n; } S[Symbol.species] = S; const P = Object.create(Array.prototype, {constructor: {value: S}}); const a = [1, 2]; Object.setPrototypeOf(a, P); const r = a.slice(); return [r instanceof S, r.len, r.length].join()`, "true,2,2"},
		{"inherits plain", `const a = [1, 2]; Object.setPrototypeOf(a, Object.create(Array.prototype)); return Array.isArray(a.map(x => x))`, "true"},
	})
	runMutableProtoCases(t, []protoCase{
		{"getter configurable", `return Object.getOwnPropertyDescriptor(Array, Symbol.species).configurable`, "true"},
		{"modified Array species", `const d = Object.getOwnPropertyDescriptor(Array, Symbol.species); function C(n) { this.n = n; } Object.defineProperty(Array, Symbol.species, {get() { return C; }, configurable: true}); const r = [1, 2].map(x => x); const s = [1].slice(); Object.defineProperty(Array, Symbol.species, d); return [r instanceof C, s instanceof C, Array.isArray([1].map(x => x))].join()`, "true,true,true"},
		{"modified Array.prototype.constructor", `function C(n) { this.n = n; } Array.prototype.constructor = {[Symbol.species]: C}; const r = [1, 2].filter(x => x); Array.prototype.constructor = Array; return [r instanceof C, Array.isArray([1].filter(x => x))].join()`, "true,true"},
		{"deleted Array.prototype.constructor", `delete Array.prototype.constructor; const r = [1].concat(2); Array.prototype.constructor = Array; return Array.isArray(r) + "," + r.length`, "true,2"},
	})
}

func TestConcatSpreadable(t *testing.T) {
	runProtoCases(t, []protoCase{
		{"spreadable object", `const o = {length: 2, 0: "a", 1: "b", [Symbol.isConcatSpreadable]: true}; return JSON.stringify([0].concat(o))`, `[0,"a","b"]`},
		{"spreadable holes", `const o = {length: 3, 1: "b", [Symbol.isConcatSpreadable]: true}; const r = [].concat(o); return [r.length, 0 in r, 1 in r].join()`, "3,false,true"},
		{"array not spreadable", `const a = [1, 2]; a[Symbol.isConcatSpreadable] = false; const r = [0].concat(a); return [r.length, r[1] === a].join()`, "2,true"},
		{"undefined falls back to IsArray", `const a = [1]; a[Symbol.isConcatSpreadable] = undefined; return [0].concat(a).length`, "2"},
		{"truthy coercion", `const o = {length: 1, 0: "x", [Symbol.isConcatSpreadable]: 1}; return [].concat(o).join()`, "x"},
		{"receiver spreadable", `const o = {length: 1, 0: "x", [Symbol.isConcatSpreadable]: true}; return Array.prototype.concat.call(o, 1).join()`, "x,1"},
		{"receiver not spreadable", `const r = Array.prototype.concat.call({a: 1}, 1); return [r.length, typeof r[0]].join()`, "2,object"},
		{"primitive receiver", `const r = Array.prototype.concat.call(1, 2); return [r.length, typeof r[0]].join()`, "2,object"},
		{"getter order", `const log = []; const mk = (name, v) => ({get [Symbol.isConcatSpreadable]() { log.push(name); return v; }, length: 1, get 0() { log.push(name + "0"); return name; }}); [].concat(mk("a", true), mk("b", false), mk("c", true)); return log.join()`, "a,a0,b,c,c0"},
		{"getter throws", `return [].concat({get [Symbol.isConcatSpreadable]() { throw new RangeError("x"); }})`, "!RangeError"},
		{"string length", `const o = {length: "2", 0: 1, 1: 2, [Symbol.isConcatSpreadable]: true}; return [].concat(o).length`, "2"},
		{"inherited", `const P = {[Symbol.isConcatSpreadable]: true}; const o = Object.create(P); o.length = 1; o[0] = "p"; return [].concat(o).join()`, "p"},
	})
	runMutableProtoCases(t, []protoCase{
		{"on Array.prototype", `Array.prototype[Symbol.isConcatSpreadable] = false; const r = [1].concat([2]); delete Array.prototype[Symbol.isConcatSpreadable]; return [r.length, Array.isArray(r[0]), Array.isArray(r[1])].join()`, "2,true,true"},
		{"on Object.prototype", `Object.prototype[Symbol.isConcatSpreadable] = true; const r = [].concat({length: 1, 0: "o"}); delete Object.prototype[Symbol.isConcatSpreadable]; return r.join()`, "o"},
		{"guards re-armed", `const a = [1].concat([2], {}).length; Array.prototype[Symbol.isConcatSpreadable] = false; const b = [1].concat([2]).length; delete Array.prototype[Symbol.isConcatSpreadable]; const c = [1].concat([2]).length; Object.prototype[Symbol.isConcatSpreadable] = true; const d = [].concat({length: 2}).length; delete Object.prototype[Symbol.isConcatSpreadable]; return [a, b, c, d, [].concat({length: 2}).length].join()`, "3,2,2,2,1"},
		{"Array.prototype's prototype", `const saved = Object.getPrototypeOf(Array.prototype); [].concat([1]); Object.setPrototypeOf(Array.prototype, {[Symbol.isConcatSpreadable]: false, __proto__: saved}); const r = [1].concat([2, 3]); Object.setPrototypeOf(Array.prototype, saved); return [r.length, [1].concat([2, 3]).length].join()`, "2,3"},
	})
}
