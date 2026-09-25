package engine

import (
	"strings"
	"testing"
)

func TestReflect(t *testing.T) {
	accTable(t, [][2]string{
		// Namespace shape.
		{`return [typeof Reflect, Object.getPrototypeOf(Reflect) === Object.prototype,
		  Object.getOwnPropertyDescriptor(globalThis, "Reflect").enumerable];`, `["object",true,false]`},
		{`try { Reflect(); } catch (e) { return e.name; }`, `"TypeError"`},
		{`try { new Reflect(); } catch (e) { return e.name; }`, `"TypeError"`},
		{`return Object.getOwnPropertyNames(Reflect).sort().map(function (k) { return k + ":" + Reflect[k].length; });`,
			`["apply:3","construct:2","defineProperty:3","deleteProperty:2","get:2","getOwnPropertyDescriptor:2","getPrototypeOf:1","has:2","isExtensible:1","ownKeys:1","preventExtensions:1","set:3","setPrototypeOf:2"]`},

		// apply.
		{`return Reflect.apply(Math.max, undefined, [1, 3, 2]);`, `3`},
		{`return Reflect.apply(function () { return this.v; }, {v: 7}, []);`, `7`},
		{`return Reflect.apply(function (a, b) { return a + b; }, null, {length: 2, 0: 1, 1: 2});`, `3`},
		{`try { Reflect.apply(1, null, []); } catch (e) { return e.name; }`, `"TypeError"`},
		{`try { Reflect.apply(function () {}, null); } catch (e) { return e.name; }`, `"TypeError"`},
		{`try { Reflect.apply(function () {}, null, 1); } catch (e) { return e.name; }`, `"TypeError"`},

		// construct.
		{`function F(a) { this.a = a; } var o = Reflect.construct(F, [5]);
		  return [o.a, o instanceof F];`, `[5,true]`},
		{`function F() { this.p = Object.getPrototypeOf(this); } function G() {}
		  var o = Reflect.construct(F, [], G); return [o.p === G.prototype, o instanceof G];`, `[true,true]`},
		{`return Reflect.construct(Date, [0], Object) instanceof Date;`, `false`},
		{`try { Reflect.construct(function () {}, [], Math.max); } catch (e) { return e.name; }`, `"TypeError"`},
		{`try { Reflect.construct(() => 1, []); } catch (e) { return e.name; }`, `"TypeError"`},
		{`try { Reflect.construct(function () {}); } catch (e) { return e.name; }`, `"TypeError"`},
		{`try { Reflect.construct(function () {}, [], undefined); } catch (e) { return e.name; }`, `"TypeError"`},
		{`var B = function (x) { this.x = x; }.bind(null, 1); return Reflect.construct(B, []).x;`, `1`},
		{`return Reflect.construct(Array, [3]).length;`, `3`},
		{`var e = Reflect.construct(Error, ["m"], TypeError); return [e.message, e instanceof TypeError];`, `["m",true]`},

		// defineProperty.
		{`var o = {}; return [Reflect.defineProperty(o, "x", {value: 1}), o.x,
		  Object.getOwnPropertyDescriptor(o, "x").writable];`, `[true,1,false]`},
		{`var o = Object.freeze({a: 1}); return [Reflect.defineProperty(o, "b", {value: 1}),
		  Reflect.defineProperty(o, "a", {value: 2}), Reflect.defineProperty(o, "a", {value: 1})];`, `[false,false,true]`},
		{`try { Reflect.defineProperty(1, "x", {}); } catch (e) { return e.name; }`, `"TypeError"`},
		{`try { Reflect.defineProperty({}, "x", 1); } catch (e) { return e.name; }`, `"TypeError"`},
		{`var a = []; return [Reflect.defineProperty(a, "length", {value: 2}), a.length];`, `[true,2]`},
		{`var a = [1, 2]; Object.defineProperty(a, 1, {configurable: false});
		  return [Reflect.defineProperty(a, "length", {value: 0}), a.length];`, `[false,2]`},

		// deleteProperty.
		{`var o = {x: 1}; return [Reflect.deleteProperty(o, "x"), "x" in o, Reflect.deleteProperty(o, "y")];`, `[true,false,true]`},
		{`var o = Object.freeze({x: 1}); return Reflect.deleteProperty(o, "x");`, `false`},
		{`try { Reflect.deleteProperty("s", "x"); } catch (e) { return e.name; }`, `"TypeError"`},

		// get with receiver.
		{`return Reflect.get({x: 1}, "x");`, `1`},
		{`var o = {get g() { return this.v; }, v: 1}; return [Reflect.get(o, "g"), Reflect.get(o, "g", {v: 2})];`, `[1,2]`},
		{`return Reflect.get([5, 6], 1);`, `6`},
		{`var k = {toString: function () { return "x"; }}; return Reflect.get({x: 3}, k);`, `3`},
		{`try { Reflect.get(undefined, "x"); } catch (e) { return e.name; }`, `"TypeError"`},

		// getOwnPropertyDescriptor.
		{`return Reflect.getOwnPropertyDescriptor({a: 1}, "a");`, `{"value":1,"writable":true,"enumerable":true,"configurable":true}`},
		{`return Reflect.getOwnPropertyDescriptor({}, "a") === undefined;`, `true`},
		{`try { Reflect.getOwnPropertyDescriptor("ab", 0); } catch (e) { return e.name; }`, `"TypeError"`},

		// getPrototypeOf / setPrototypeOf.
		{`return [Reflect.getPrototypeOf([]) === Array.prototype, Reflect.getPrototypeOf(Object.create(null))];`, `[true,null]`},
		{`try { Reflect.getPrototypeOf(1); } catch (e) { return e.name; }`, `"TypeError"`},
		{`var p = {}; var o = {}; return [Reflect.setPrototypeOf(o, p), Object.getPrototypeOf(o) === p,
		  Reflect.setPrototypeOf(o, null), Object.getPrototypeOf(o)];`, `[true,true,true,null]`},
		{`var a = {}; var b = Object.create(a); return Reflect.setPrototypeOf(a, b);`, `false`},
		{`var o = Object.preventExtensions({}); return [Reflect.setPrototypeOf(o, {}),
		  Reflect.setPrototypeOf(o, Object.prototype)];`, `[false,true]`},
		{`try { Reflect.setPrototypeOf({}, 1); } catch (e) { return e.name; }`, `"TypeError"`},
		{`try { Reflect.setPrototypeOf({}); } catch (e) { return e.name; }`, `"TypeError"`},
		{`try { Reflect.setPrototypeOf(1, {}); } catch (e) { return e.name; }`, `"TypeError"`},

		// has.
		{`var o = Object.create({p: 1}); o.q = 2; return [Reflect.has(o, "p"), Reflect.has(o, "q"), Reflect.has(o, "r")];`,
			`[true,true,false]`},
		{`return [Reflect.has([1], 0), Reflect.has([1], "length"), Reflect.has([1], 1)];`, `[true,true,false]`},
		{`try { Reflect.has("s", "length"); } catch (e) { return e.name; }`, `"TypeError"`},

		// isExtensible / preventExtensions.
		{`var o = {}; return [Reflect.isExtensible(o), Reflect.preventExtensions(o), Reflect.isExtensible(o)];`,
			`[true,true,false]`},
		{`return Reflect.isExtensible(Object.freeze({}));`, `false`},
		{`try { Reflect.isExtensible(1); } catch (e) { return e.name; }`, `"TypeError"`},
		{`try { Reflect.preventExtensions(1); } catch (e) { return e.name; }`, `"TypeError"`},

		// ownKeys: integer keys ascending, then strings, then symbols.
		{`var o = {b: 1, 2: 1, a: 1, 1: 1}; return Reflect.ownKeys(o);`, `["1","2","b","a"]`},
		{`return Reflect.ownKeys([7, 8]);`, `["0","1","length"]`},
		{`var o = {}; Object.defineProperty(o, "h", {value: 1}); return Reflect.ownKeys(o);`, `["h"]`},
		{`try { Reflect.ownKeys("ab"); } catch (e) { return e.name; }`, `"TypeError"`},

		// set with receiver.
		{`var o = {}; return [Reflect.set(o, "x", 1), o.x];`, `[true,1]`},
		{`var o = Object.freeze({x: 1}); return [Reflect.set(o, "x", 2), o.x, Reflect.set(o, "y", 1)];`, `[false,1,false]`},
		{`var t = {}; var r = {}; return [Reflect.set(t, "x", 1, r), "x" in t, r.x];`, `[true,false,1]`},
		{`var got; var o = {set s(v) { got = [this === r, v]; }}; var r = {}; Reflect.set(o, "s", 4, r); return got;`, `[true,4]`},
		{`var o = {get g() { return 1; }}; return Reflect.set(o, "g", 2);`, `false`},
		{`return Reflect.set({}, "x", 1, 1);`, `false`},
		{`var a = [1, 2, 3]; return [Reflect.set(a, "length", 1), a];`, `[true,[1]]`},
		{`try { Reflect.set(1, "x", 1); } catch (e) { return e.name; }`, `"TypeError"`},
	})
}

// TestReflectMutableAttributes checks the method attributes in a mutable
// realm; the shared template freezes every intrinsic (lockdown).
func TestReflectMutableAttributes(t *testing.T) {
	got := accEval(t, false, `var d = Object.getOwnPropertyDescriptor(Reflect, "apply");
	  var g = Object.getOwnPropertyDescriptor(globalThis, "Reflect");
	  return [d.writable, d.enumerable, d.configurable, g.writable, g.enumerable, g.configurable];`)
	if got != `[true,false,true,true,false,true]` {
		t.Fatalf("got %s", got)
	}
}

// TestReflectOwnKeysSymbols checks that symbol keys come last. There is no
// Symbol global yet, so the object is built from Go.
func TestReflectOwnKeysSymbols(t *testing.T) {
	r := NewRealm()
	o := r.NewObject()
	for _, k := range []PropertyKey{StringKey(AtomLength), SymbolKey(SymIterator), IndexKey(3), StringKey(AtomName), IndexKey(1)} {
		_, err := o.CreateDataProperty(r, k, IntValue(1))
		if err != nil {
			t.Fatal(err)
		}
	}
	v, err := reflectOwnKeys(r, Undefined(), []Value{ObjectValue(o)})
	if err != nil {
		t.Fatal(err)
	}
	a := v.AsObject()
	var got []string
	for i := range uint32(5) {
		e, _ := a.Get(r, IndexKey(i), v)
		if e.IsSymbol() {
			got = append(got, "@@"+e.AsSymbol().descriptiveString())
		} else {
			got = append(got, e.AsString().GoString())
		}
	}
	want := []string{"1", "3", "length", "name", "@@Symbol(Symbol.iterator)"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}
}
