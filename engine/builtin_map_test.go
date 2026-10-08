package engine

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// refList is the spec's [[MapData]]: an append-only list whose deleted
// entries become empty, walked by index.
type refList struct {
	keys  []int
	alive []bool
}

func (l *refList) find(k int) int {
	for i, key := range l.keys {
		if l.alive[i] && key == k {
			return i
		}
	}
	return -1
}

// TestCollectionTableAgainstList drives the table and the spec's list with
// the same random operations, stepping several cursors between them: every
// cursor must yield exactly what an index walk of the list yields, across
// rebuilds (growth, shrinking) and clear.
func TestCollectionTableAgainstList(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for round := 0; round < 200; round++ {
		var c collection
		var ref refList
		type pair struct {
			cur  collCursor
			i    int // index into the reference list
			done bool
		}
		var curs []*pair
		keyRange := 4 + rng.IntN(200)
		for op := 0; op < 600; op++ {
			k := rng.IntN(keyRange)
			switch x := rng.IntN(100); {
			case x < 45:
				c.set(nil, IntValue(k), IntValue(op))
				if ref.find(k) < 0 {
					ref.keys = append(ref.keys, k)
					ref.alive = append(ref.alive, true)
				}
			case x < 85:
				got := c.delete(IntValue(k))
				i := ref.find(k)
				require.Equal(t, i >= 0, got)
				if i >= 0 {
					ref.alive[i] = false
				}
			case x < 87:
				c.clear()
				// clear empties every slot; later additions append, and a
				// cursor continues from the next slot (the spec's list keeps
				// its length).
				for i := range ref.alive {
					ref.alive[i] = false
				}
			case x < 92:
				curs = append(curs, &pair{cur: c.cursor()})
			default:
				for _, p := range curs {
					key, _, ok := p.cur.next()
					for p.i < len(ref.keys) && !ref.alive[p.i] {
						p.i++
					}
					if p.done || p.i >= len(ref.keys) {
						require.False(t, ok, "round %d op %d: cursor should be done", round, op)
						p.done = true
						continue
					}
					require.True(t, ok, "round %d op %d", round, op)
					require.Equal(t, float64(ref.keys[p.i]), key.AsNumber(), "round %d op %d", round, op)
					p.i++
				}
			}
			live := 0
			for _, a := range ref.alive {
				if a {
					live++
				}
			}
			require.Equal(t, live, c.size())
		}
		// The table stays proportional to the live entries.
		if c.t != nil {
			assert.LessOrEqual(t, cap(c.t.entries), max(32, 8*collCapFor(c.size())))
		}
	}
}

func TestCollectionKeys(t *testing.T) {
	var c collection
	s1 := StringValue(FromGoString("ab"))
	rope, err := NewRealm().Concat(asciiString("a"), asciiString("b"))
	require.NoError(t, err)
	s2 := StringValue(rope)
	u1 := StringValue(FromGoString("é"))
	u2 := StringValue(FromGoString("é"))
	c.set(nil, s1, IntValue(1))
	c.set(nil, u1, IntValue(2))
	c.set(nil, NumberValue(math.Copysign(0, -1)), IntValue(3))
	c.set(nil, NumberValue(math.NaN()), IntValue(4))
	c.set(nil, IntValue(1), IntValue(5))
	for _, tc := range []struct {
		k    Value
		want float64
	}{{s2, 1}, {u2, 2}, {IntValue(0), 3}, {NumberValue(math.Copysign(0, -1)), 3}, {NumberValue(math.NaN()), 4}, {NumberValue(1.0), 5}} {
		v, ok := c.get(tc.k)
		require.True(t, ok)
		assert.Equal(t, tc.want, v.AsNumber())
	}
	_, ok := c.get(StringValue(FromGoString("1")))
	assert.False(t, ok)
	cur := c.cursor()
	k, _, _ := cur.next()
	k, _, _ = cur.next()
	k, _, _ = cur.next()
	assert.Equal(t, negZeroBits != k.bits, true, "-0 is stored as +0")
}

func TestMapSemantics(t *testing.T) {
	runProtoCases(t, []protoCase{
		{"basic", `const m = new Map([[1, "a"], ["1", "b"]]); m.set(NaN, "n").set(-0, "z"); return [m.size, m.get(1), m.get("1"), m.get(NaN), m.get(0), m.has(+0), m.delete(1), m.has(1), m.size].join()`, "4,a,b,n,z,true,true,false,3"},
		{"order", `const m = new Map(); m.set("b", 1).set("a", 2).set("b", 3); m.delete("a"); m.set("a", 4); return [...m].join(";")`, "b,3;a,4"},
		{"-0 key", `const m = new Map([[-0, 1]]); return String(1 / [...m.keys()][0])`, "Infinity"},
		{"object keys", `const a = {}, b = {}; const m = new Map([[a, 1], [b, 2]]); return [m.get(a), m.get(b), m.get({})].join()`, "1,2,"},
		{"live iteration", `const m = new Map([[1, 1], [2, 2], [3, 3]]); const out = []; for (const [k] of m) { out.push(k); if (k === 1) { m.delete(2); m.set(4, 4); } } return out.join()`, "1,3,4"},
		{"forEach live", `const m = new Map([[1, "a"]]); const out = []; m.forEach(function (v, k, map) { out.push(k + v + (map === m) + (this === out)); if (k < 3) m.set(k + 1, "b"); }, out); return out.join()`, "1atruetrue,2btruetrue,3btruetrue"},
		{"clear during iteration", `const m = new Map([[1, 1], [2, 2]]); const it = m.keys(); it.next(); m.clear(); m.set(5, 5); return JSON.stringify([it.next(), it.next()])`, `[{"value":5,"done":false},{"done":true}]`},
		{"done stays done", `const m = new Map(); const it = m.entries(); it.next(); m.set(1, 1); return it.next().done`, "true"},
		{"mass delete during iteration", `const m = new Map(); for (let i = 0; i < 1000; i++) m.set(i, i); const it = m.keys(); const out = [it.next().value]; for (let i = 0; i < 995; i++) m.delete(i); out.push(...it); return out.join()`, "0,995,996,997,998,999"},
		{"entries value", `return JSON.stringify(new Map([["a", 1]]).entries().next())`, `{"value":["a",1],"done":false}`},
		{"iterator protos", `const it = new Map().keys(); const MIP = Object.getPrototypeOf(it); return [Object.prototype.toString.call(it), Object.getPrototypeOf(MIP) === Object.getPrototypeOf(Object.getPrototypeOf([].values())), MIP.next.length, Object.hasOwn(MIP, Symbol.iterator)].join()`, "[object Map Iterator],true,0,false"},
		{"@@iterator is entries", `return Map.prototype[Symbol.iterator] === Map.prototype.entries && Map.prototype.entries.name`, "entries"},
		{"size getter", `const d = Object.getOwnPropertyDescriptor(Map.prototype, "size"); return [typeof d.get, d.get.name, d.set, d.enumerable].join()`, "function,get size,,false"},
		{"lengths", `return [Map.length, Map.prototype.set.length, Map.prototype.get.length, Map.prototype.forEach.length, Map.groupBy.length].join()`, "0,2,1,1,2"},
		{"toStringTag", `return Object.prototype.toString.call(new Map())`, "[object Map]"},
		{"call throws", `return Map()`, "!TypeError"},
		{"incompatible receiver", `return Map.prototype.get.call(new Set(), 1)`, "!TypeError"},
		{"size receiver", `return Object.getOwnPropertyDescriptor(Map.prototype, "size").get.call({})`, "!TypeError"},
		{"species", `return Map[Symbol.species] === Map`, "true"},
		{"inherited methods", `const P = Object.create(Map.prototype); const m = new Map([[1, 2]]); Object.setPrototypeOf(m, P); return [m instanceof Map, m.get(1), m.size].join()`, "true,2,1"},
		{"bad entry", `return new Map([1])`, "!TypeError"},
		{"bad entry closes", `const log = []; const it = {[Symbol.iterator]() { return {next() { return {value: 1, done: false}; }, return() { log.push("r"); return {}; }}; }}; try { new Map(it); } catch (e) { log.push(e.name); } return log.join()`, "r,TypeError"},
		{"groupBy", `const m = Map.groupBy([1, 2, 3, 4, 5], (v, i) => v % 2 ? "odd" : i); return [...m].map(([k, v]) => k + ":" + v.join("")).join()`, "odd:135,1:2,3:4"},
		{"groupBy -0", `const m = Map.groupBy([1], () => -0); return String(1 / [...m.keys()][0])`, "Infinity"},
		{"groupBy callback throws closes", `const log = []; const it = {[Symbol.iterator]() { return {next() { return {value: 1, done: false}; }, return() { log.push("r"); return {}; }}; }}; try { Map.groupBy(it, () => { throw new Error("cb"); }); } catch (e) { log.push(e.message); } return log.join()`, "r,cb"},
		{"groupBy not callable", `return Map.groupBy([], 1)`, "!TypeError"},
		{"groupBy nullish", `return Map.groupBy(null, () => 1)`, "!TypeError"},
		{"groupBy string", `return [...Map.groupBy("aab", c => c)].map(([k, v]) => k + v.length).join()`, "a2,b1"},
		{"spread and Array.from", `const m = new Map([[1, 2]]); return [[...m.values()].join(), Array.from(m.keys()).join(), [...m][0].join()].join("|")`, "2|1|1,2"},
		{"many entries", `const m = new Map(); for (let i = 0; i < 10000; i++) m.set("k" + i, i); for (let i = 0; i < 10000; i += 2) m.delete("k" + i); let s = 0; for (const [, v] of m) s += v; return [m.size, s, m.get("k9999"), m.get("k0")].join()`, "5000,25000000,9999,"},
		{"bigint keys", `const m = new Map([[10n, "a"], [1180591620717411303424n, "b"]]); return [m.get(10n), m.get(0xan), m.get(10), m.get(1180591620717411303424n), m.get(1180591620717411303424)].join()`, "a,a,,b,"},
		{"symbol keys", `const s = Symbol(); const m = new Map([[s, 1]]); return [m.get(s), m.get(Symbol())].join()`, "1,"},
	})
	runMutableProtoCases(t, []protoCase{
		{"modified iterator next", `const MIP = Object.getPrototypeOf(new Map().keys()); const orig = MIP.next; MIP.next = function () { return {done: true}; }; const r = [...new Map([[1, 1]])].length; MIP.next = orig; return r + "," + [...new Map([[1, 1]])].length`, "0,1"},
		{"modified set adder", `const orig = Map.prototype.set; const log = []; Map.prototype.set = function (k, v) { log.push(k + "=" + v); return orig.call(this, k, v * 10); }; const m = new Map([["a", 1], ["b", 2]]); Map.prototype.set = orig; return log.join() + "|" + m.get("b")`, "a=1,b=2|20"},
		{"adder not callable", `const orig = Map.prototype.set; Map.prototype.set = 1; try { new Map([]); } finally { Map.prototype.set = orig; }`, "!TypeError"},
		{"null iterable skips adder", `const orig = Map.prototype.set; Map.prototype.set = 1; try { return new Map(null).size + new Map(undefined).size; } finally { Map.prototype.set = orig; }`, "0"},
		{"adder read once", `const orig = Map.prototype.set; let reads = 0; Object.defineProperty(Map.prototype, "set", {get() { reads++; return orig; }, configurable: true}); new Map([[1, 1], [2, 2]]); Object.defineProperty(Map.prototype, "set", {value: orig, writable: true, configurable: true}); return reads`, "1"},
		{"attrs", `const d = Object.getOwnPropertyDescriptor(Map.prototype, "get"); const t = Object.getOwnPropertyDescriptor(Map.prototype, Symbol.toStringTag); const s = Object.getOwnPropertyDescriptor(Map, Symbol.species); return [d.writable, d.enumerable, d.configurable, t.value, t.writable, t.configurable, typeof s.get, s.set, s.configurable, s.get.name].join()`,
			"true,false,true,Map,false,true,function,,true,get [Symbol.species]"},
	})
}

func TestSetSemantics(t *testing.T) {
	runProtoCases(t, []protoCase{
		{"basic", `const s = new Set([1, 1, "1", NaN, NaN, -0, 0]); return [s.size, s.has(1), s.has(NaN), s.has(-0), s.add(2) === s, s.delete("1"), s.size].join()`, "4,true,true,true,true,true,4"},
		{"order", `const s = new Set("hello"); return [...s].join("")`, "helo"},
		{"keys is values", `return [Set.prototype.keys === Set.prototype.values, Set.prototype[Symbol.iterator] === Set.prototype.values, Set.prototype.values.name].join()`, "true,true,values"},
		{"entries", `return JSON.stringify([...new Set(["a"]).entries()])`, `[["a","a"]]`},
		{"forEach", `const out = []; new Set(["a", "b"]).forEach((v, k, s) => out.push(v + k + s.size)); return out.join()`, "aa2,bb2"},
		{"live", `const s = new Set([1, 2, 3]); const out = []; for (const v of s) { out.push(v); if (v === 1) { s.delete(2); s.add(4); } } return out.join()`, "1,3,4"},
		{"iterator proto", `const it = new Set().values(); return [Object.prototype.toString.call(it), Object.getPrototypeOf(it) !== Object.getPrototypeOf(new Map().keys())].join()`, "[object Set Iterator],true"},
		{"call throws", `return Set()`, "!TypeError"},
		{"receiver", `return Set.prototype.has.call(new Map(), 1)`, "!TypeError"},
		{"lengths", `return [Set.length, Set.prototype.add.length, Set.prototype.union.length, Set.prototype.isSubsetOf.length].join()`, "0,1,1,1"},
		{"union", `return [...new Set([1, 2]).union(new Set([2, 3]))].join()`, "1,2,3"},
		{"intersection small this", `return [...new Set([3, 1, 2]).intersection(new Set([1, 2, 3, 4]))].join()`, "3,1,2"},
		{"intersection small other", `return [...new Set([1, 2, 3, 4]).intersection(new Set([3, 1]))].join()`, "3,1"},
		{"difference", `return [...new Set([1, 2, 3]).difference(new Set([2])), "|", ...new Set([1, 2]).difference(new Set([2, 3, 4]))].join()`, "1,3,|,1"},
		{"symmetricDifference", `return [...new Set([1, 2]).symmetricDifference(new Set([2, 3]))].join()`, "1,3"},
		{"isSubsetOf", `return [new Set([1]).isSubsetOf(new Set([1, 2])), new Set([1, 3]).isSubsetOf(new Set([1, 2])), new Set([1, 2, 3]).isSubsetOf(new Set([1]))].join()`, "true,false,false"},
		{"isSupersetOf", `return [new Set([1, 2]).isSupersetOf(new Set([1])), new Set([1]).isSupersetOf(new Set([2])), new Set([1]).isSupersetOf(new Set([1, 2]))].join()`, "true,false,false"},
		{"isDisjointFrom", `return [new Set([1]).isDisjointFrom(new Set([2])), new Set([1, 2]).isDisjointFrom(new Set([2, 3, 4])), new Set([1, 2, 3]).isDisjointFrom(new Set([3]))].join()`, "true,false,false"},
		{"set-like", `const like = {size: 2, has(v) { return v === 1 || v === 9; }, keys() { return [9, 1][Symbol.iterator](); }}; const s = new Set([1, 2, 3]); return [[...s.union(like)].join(""), [...s.intersection(like)].join(""), [...s.difference(like)].join(""), s.isSupersetOf(like), [...s.symmetricDifference(like)].join("")].join()`, "1239,1,23,false,239"},
		{"set-like Map", `return [...new Set([1, 2]).union(new Map([[3, "x"]]))].join()`, "1,2,3"},
		{"set-like size NaN", `return new Set().union({size: undefined, has() {}, keys() {}})`, "!TypeError"},
		{"set-like size negative", `return new Set().union({size: -1, has() {}, keys() {}})`, "!RangeError"},
		{"set-like has missing", `return new Set().union({size: 1, keys() {}})`, "!TypeError"},
		{"set-like keys missing", `return new Set().union({size: 1, has() {}})`, "!TypeError"},
		{"set-like not object", `return new Set().union([1])`, "!TypeError"},
		{"set-like order", `const log = []; const like = {get size() { log.push("size"); return {valueOf() { log.push("valueOf"); return 1; }}; }, get has() { log.push("has"); return () => true; }, get keys() { log.push("keys"); return () => [][Symbol.iterator](); }}; new Set([1]).isSubsetOf(like); return log.join()`, "size,valueOf,has,keys"},
		{"size infinity", `return new Set([1]).isSubsetOf({size: Infinity, has: () => true, keys() { throw new Error("no"); }})`, "true"},
		{"superset closes", `const log = []; const like = {size: 1, has: () => false, keys() { return {next() { return {value: 5, done: false}; }, return() { log.push("closed"); return {}; }}; }}; return new Set([1]).isSupersetOf(like) + log.join()`, "falseclosed"},
		{"disjoint closes", `const log = []; const like = {size: 0, has: () => false, keys() { return {next() { return {value: 1, done: false}; }, return() { log.push("closed"); return {}; }}; }}; return new Set([1]).isDisjointFrom(like) + log.join()`, "falseclosed"},
		{"keys -0 canonical", `const like = {size: 1, has: () => false, keys: () => [-0].values()}; return String(1 / [...new Set().union(like)][0])`, "Infinity"},
		{"result is plain Set", `const s = new Set([1]); Object.setPrototypeOf(s, Object.create(Set.prototype)); const r = s.union(new Set()); return [Object.getPrototypeOf(r) === Set.prototype, r.size].join()`, "true,1"},
		{"has mutates this", `const s = new Set([1, 2, 3]); const like = {size: 10, has(v) { if (v === 1) { s.delete(2); s.add(4); } return true; }, keys() {}}; return [...s.intersection(like)].join()`, "1,3,4"},
		{"keys non-object", `return new Set([1]).union({size: 1, has() {}, keys() { return 1; }})`, "!TypeError"},
		{"keys next not callable", `return new Set([1]).union({size: 1, has() {}, keys() { return {next: 1}; }})`, "!TypeError"},
	})
	runMutableProtoCases(t, []protoCase{
		{"custom add", `const orig = Set.prototype.add; const log = []; Set.prototype.add = function (v) { log.push(v); return orig.call(this, v); }; new Set([1, 2]); Set.prototype.add = orig; return log.join()`, "1,2"},
		{"add throws closes", `const orig = Set.prototype.add; const log = []; Set.prototype.add = function (v) { throw new Error("add"); }; const it = {[Symbol.iterator]() { return {next() { return {value: 1, done: false}; }, return() { log.push("r"); return {}; }}; }}; try { new Set(it); } catch (e) { log.push(e.message); } Set.prototype.add = orig; return log.join()`, "r,add"},
		{"modified iterator next", `const SIP = Object.getPrototypeOf(new Set().values()); const orig = SIP.next; SIP.next = function () { return {done: true}; }; const r = [...new Set([1])].length; SIP.next = orig; return r + "," + [...new Set([1])].length`, "0,1"},
	})
}

// TestCollectionNewTarget covers GetPrototypeFromConstructor(newTarget)
// (class syntax is not available yet).
func TestCollectionNewTarget(t *testing.T) {
	for _, tc := range []struct {
		ctor  NativeCtor
		class Class
	}{{mapConstruct, ClassMap}, {setConstruct, ClassSet}} {
		r := NewRealm()
		proto := r.NewObject()
		nt := r.NewNativeFunction(AtomEmpty, 0, nil)
		require.NoError(t, nt.SetProp(r, StringKey(AtomPrototype), ObjectValue(proto)))
		v, err := tc.ctor(r, nil, nt)
		require.NoError(t, err)
		assert.Equal(t, proto, v.AsObject().proto)
		assert.Equal(t, tc.class, v.AsObject().class)
		require.NoError(t, nt.SetProp(r, StringKey(AtomPrototype), IntValue(1)))
		v, err = tc.ctor(r, nil, nt)
		require.NoError(t, err)
		assert.NotEqual(t, proto, v.AsObject().proto)
	}
}

func TestCollectionExport(t *testing.T) {
	f := evalModule(t, `
export const m = new Map([["a", 1], [2, {x: [1]}]]);
export const s = new Set(["a", 1]);
export const c = new Map(); c.set("self", c);
export const shrunk = new Map([[1, {get g() { shrunk.delete(2); return 0; }}], [2, 2]]);
`)
	assert.Equal(t, [][2]any{{"a", int64(1)}, {int64(2), map[string]any{"x": []any{int64(1)}}}}, f.export("m"))
	assert.Equal(t, []any{"a", int64(1)}, f.export("s"))
	c := f.export("c").([][2]any)
	require.Len(t, c, 1)
	assert.Equal(t, "self", c[0][0])
	assert.Equal(t, [][2]any{{int64(1), map[string]any{"g": int64(0)}}, {nil, nil}}, f.export("shrunk"), "entries removed during the export leave zero pairs")
}
