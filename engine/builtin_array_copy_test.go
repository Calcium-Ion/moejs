package engine

import "testing"

func TestArrayCopyWithin(t *testing.T) {
	accTable(t, [][2]string{
		{`return [1, 2, 3, 4, 5].copyWithin(0, 3);`, `[4,5,3,4,5]`},
		{`return [1, 2, 3, 4, 5].copyWithin(1, 0);`, `[1,1,2,3,4]`},
		{`return [1, 2, 3, 4, 5].copyWithin(0, 1, 3);`, `[2,3,3,4,5]`},
		{`return [1, 2, 3, 4, 5].copyWithin(-2, -3, -1);`, `[1,2,3,3,4]`},
		{`return [1, 2, 3, 4, 5].copyWithin(0, 3, 1);`, `[1,2,3,4,5]`},
		{`return [1, 2, 3].copyWithin(5, 0);`, `[1,2,3]`},
		{`return [1, 2, 3].copyWithin(0, -Infinity, Infinity);`, `[1,2,3]`},
		{`return [1, 2, 3].copyWithin(1, undefined, undefined);`, `[1,1,2]`},
		{`var a = [1, , 3]; a.copyWithin(0, 1); return [0 in a, a[1], a.length];`, `[false,3,3]`},
		{`var a = [1, 2, 3]; return a.copyWithin(0, 1) === a;`, `true`},
		// Generic array-likes: an absent source deletes the target.
		{`var o = {length: 5, 3: 1}; Array.prototype.copyWithin.call(o, 0, 3); return [o[0], 1 in o, o[3]];`, `[1,false,1]`},
		{`var o = Array.prototype.copyWithin.call({length: 2, 0: "a", 1: "b"}, 1, 0); return [o[0], o[1]];`, `["a","a"]`},
		{`return typeof Array.prototype.copyWithin.call(1, 0, 0);`, `"object"`},
		// Overlapping backwards copy through the generic path.
		{`var o = {length: 4, 0: 0, 1: 1, 2: 2, 3: 3}; Array.prototype.copyWithin.call(o, 1, 0);
		  return [o[0], o[1], o[2], o[3]];`, `[0,0,1,2]`},
		// Array with an inherited index: generic path, spec order.
		{`var p = Object.create(Array.prototype); p[1] = "p"; var a = [0, , 2]; Object.setPrototypeOf(a, p);
		  a.copyWithin(0, 1); return [a[0], a[1], a.hasOwnProperty(1)];`, `["p",2,true]`},
		{`var a = Object.freeze([1, 2]); try { a.copyWithin(0, 1); } catch (e) { return e.name; }`, `"TypeError"`},
		// Coercions that shrink the array fall back to the generic path.
		{`var a = [1, 2, 3, 4]; a.copyWithin(0, {valueOf: function () { a.length = 2; return 2; }});
		  return [a.length, a];`, `[2,[null,null]]`},
		{`try { Array.prototype.copyWithin.call(null); } catch (e) { return e.name; }`, `"TypeError"`},
		{`var big = {length: Math.pow(2, 53) + 2, 9007199254740990: "x"};
		  Array.prototype.copyWithin.call(big, 9007199254740989, 9007199254740990);
		  return big[9007199254740989];`, `"x"`},
	})
}

func TestArrayToReversed(t *testing.T) {
	accTable(t, [][2]string{
		{`var a = [1, 2, 3]; var b = a.toReversed(); return [a, b, a === b];`, `[[1,2,3],[3,2,1],false]`},
		{`var b = [1, , 3].toReversed(); return [b, 1 in b];`, `[[3,null,1],true]`},
		{`return [].toReversed();`, `[]`},
		{`return Array.prototype.toReversed.call({length: 2, 0: "a", 1: "b"});`, `["b","a"]`},
		{`return Array.prototype.toReversed.call("abc");`, `["c","b","a"]`},
		{`var a = [0, , 2]; Object.setPrototypeOf(a, Object.create(Array.prototype, {1: {value: "p"}})); return a.toReversed();`, `[2,"p",0]`},
		{`try { Array.prototype.toReversed.call({length: Math.pow(2, 32)}); } catch (e) { return e.name; }`, `"RangeError"`},
		{`return Array.isArray(Array.prototype.toReversed.call({length: -1}));`, `true`},
		{`var o = {length: 3, 0: 0, 1: 1, get 2() { delete this[1]; return 2; }};
		  return Array.prototype.toReversed.call(o);`, `[2,null,0]`},
	})
}

func TestArrayToSorted(t *testing.T) {
	accTable(t, [][2]string{
		{`var a = [3, 1, 2]; var b = a.toSorted(); return [a, b];`, `[[3,1,2],[1,2,3]]`},
		{`return [3, 1, 2].toSorted(function (x, y) { return y - x; });`, `[3,2,1]`},
		{`return [10, 9, 1].toSorted();`, `[1,10,9]`},
		{`var b = [undefined, 2, , 1].toSorted(); return [b, b.length, 3 in b];`, `[[1,2,null,null],4,true]`},
		{`return Array.prototype.toSorted.call({length: 3, 0: "c", 1: "a", 2: "b"});`, `["a","b","c"]`},
		{`try { [].toSorted(null); } catch (e) { return e.name; }`, `"TypeError"`},
		{`try { Array.prototype.toSorted.call(null, 1); } catch (e) { return e.message.indexOf("comparison") >= 0; }`, `true`},
		{`try { Array.prototype.toSorted.call({length: Math.pow(2, 32)}); } catch (e) { return e.name; }`, `"RangeError"`},
		{`var a = [2, 1]; a.toSorted(function (x, y) { a.push(9); return x - y; }); return a;`, `[2,1,9]`},
		{`try { [2, 1].toSorted(function () { throw new EvalError("c"); }); } catch (e) { return e.name; }`, `"EvalError"`},
	})
}

func TestArrayToSpliced(t *testing.T) {
	accTable(t, [][2]string{
		{`var a = [1, 2, 3, 4]; var b = a.toSpliced(1, 2); return [a, b];`, `[[1,2,3,4],[1,4]]`},
		{`return [1, 2, 3].toSpliced();`, `[1,2,3]`},
		{`return [1, 2, 3].toSpliced(1);`, `[1]`},
		{`return [1, 2, 3].toSpliced(1, undefined);`, `[1,2,3]`},
		{`return [1, 2, 3].toSpliced(-1, 0, "x", "y");`, `[1,2,"x","y",3]`},
		{`return [1, 2, 3].toSpliced(1, 1, "x");`, `[1,"x",3]`},
		{`return [1, 2, 3].toSpliced(0, Infinity, "z");`, `["z"]`},
		{`return [1, 2, 3].toSpliced(5, -2, "z");`, `[1,2,3,"z"]`},
		{`var b = [1, , 3].toSpliced(0, 0); return [b, 1 in b];`, `[[1,null,3],true]`},
		{`return Array.prototype.toSpliced.call({length: 3, 0: "a", 2: "c"}, 1, 0, "b");`, `["a","b",null,"c"]`},
		{`try { Array.prototype.toSpliced.call({length: Math.pow(2, 53) - 1}, 0, 0, 1); } catch (e) { return e.name; }`, `"TypeError"`},
		{`try { Array.prototype.toSpliced.call({length: Math.pow(2, 32)}, 0, 0); } catch (e) { return e.name; }`, `"RangeError"`},
		{`return Array.prototype.toSpliced.call({length: Math.pow(2, 32)}, 0, Math.pow(2, 32) - 1);`, `[null]`},
		{`var a = [1, 2, 3]; var b = a.toSpliced({valueOf: function () { a.length = 1; return 0; }}, 1);
		  return b;`, `[null,null]`},
	})
}

func TestArrayWith(t *testing.T) {
	accTable(t, [][2]string{
		{`var a = [1, 2, 3]; var b = a.with(1, "x"); return [a, b];`, `[[1,2,3],[1,"x",3]]`},
		{`return [1, 2, 3].with(-1, "x");`, `[1,2,"x"]`},
		{`return [1, 2, 3].with(1.7, "x");`, `[1,"x",3]`},
		{`try { [1, 2, 3].with(3, "x"); } catch (e) { return e.name; }`, `"RangeError"`},
		{`try { [1, 2, 3].with(-4, "x"); } catch (e) { return e.name; }`, `"RangeError"`},
		{`try { [].with(0, "x"); } catch (e) { return e.name; }`, `"RangeError"`},
		{`try { [1].with(-Infinity); } catch (e) { return e.name; }`, `"RangeError"`},
		{`var b = [1, , 3].with(0, 0); return [b, 1 in b];`, `[[0,null,3],true]`},
		{`return Array.prototype.with.call({length: 2, 0: "a", 1: "b"}, 0, "z");`, `["z","b"]`},
		{`return Array.prototype.with.call("ab", 1, "z");`, `["a","z"]`},
		{`try { Array.prototype.with.call({length: Math.pow(2, 32)}, 0); } catch (e) { return e.name; }`, `"RangeError"`},
		{`var reads = []; var o = {length: 3, get 0() { reads.push(0); }, get 1() { reads.push(1); }, get 2() { reads.push(2); }};
		  Array.prototype.with.call(o, 1, 0); return reads;`, `[0,2]`},
		{`var a = [1, 2, 3]; var b = a.with({valueOf: function () { a.length = 1; return 0; }}, 9); return b;`, `[9,null,null]`},
	})
}

func TestArrayToLocaleString(t *testing.T) {
	accTable(t, [][2]string{
		{`return [1, "a", null, undefined, true].toLocaleString();`, `"1,a,,,true"`},
		{`var n = 0; var o = {toLocaleString: function () { n++; return "o"; }};
		  return [[undefined, o, null, o].toLocaleString(), n];`, `[",o,,o",2]`},
		{`var got; var o = {toLocaleString: function (...rest) { got = rest.length; return ""; }};
		  [o].toLocaleString("en", {}); return got;`, `0`},
		{`return Array.prototype.toLocaleString.call({length: 2, 0: "a", 1: "b"});`, `"a,b"`},
		{`try { [{toLocaleString: 1}].toLocaleString(); } catch (e) { return e.name; }`, `"TypeError"`},
		{`return [{toLocaleString: function () { return {toString: function () { return "t"; }}; }}].toLocaleString();`, `"t"`},
		{`try { [{toLocaleString: function () { return Object.create(null); }}].toLocaleString(); } catch (e) { return e.name; }`, `"TypeError"`},
		{`try { Array.prototype.toLocaleString.call(undefined); } catch (e) { return e.name; }`, `"TypeError"`},
	})
}

// TestArrayToLocaleStringPrimitiveThis checks that a primitive element is
// the receiver itself; it patches Number.prototype, so it needs a mutable
// realm.
func TestArrayToLocaleStringPrimitiveThis(t *testing.T) {
	got := accEval(t, false, `Number.prototype.toLocaleString = function () { "use strict"; return typeof this; };
	  return [1, 2].toLocaleString();`)
	if got != `"number,number"` {
		t.Fatalf("got %s", got)
	}
}

func TestArrayReverseProbeOrder(t *testing.T) {
	accTable(t, [][2]string{
		// The lower getter shrinks the array before upper is probed.
		{`var a = ["first", "second"];
		  Object.defineProperty(a, 0, {get: function () { a.length = 0; return "first"; }});
		  a.reverse(); return [0 in a, 1 in a, a[1]];`, `[false,true,"first"]`},
	})
}

func TestArraySetLengthReadsDescriptorAfterCoercion(t *testing.T) {
	accTable(t, [][2]string{
		{`var a = [1, 2]; var calls = 0;
		  var len = {valueOf: function () { if (++calls === 2) Object.defineProperty(a, "length", {writable: false}); return a.length; }};
		  var ok = Reflect.defineProperty(a, "length", {value: len, writable: true});
		  return [ok, calls, Object.getOwnPropertyDescriptor(a, "length").writable];`, `[false,2,false]`},
	})
}
