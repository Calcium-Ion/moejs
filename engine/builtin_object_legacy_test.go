package engine

import "testing"

func TestObjectIs(t *testing.T) {
	accTable(t, [][2]string{
		{`return [Object.is(NaN, NaN), Object.is(0, -0), Object.is(-0, -0), Object.is(1, 1.0), Object.is("a", "a")];`,
			`[true,false,true,true,true]`},
		{`var o = {}; return [Object.is(o, o), Object.is(o, {}), Object.is(null, undefined), Object.is()];`,
			`[true,false,false,true]`},
		{`return [Object.is(1, "1"), Object.is(true, 1), Object.is(1n, 1n), Object.is(0 / 0, NaN)];`,
			`[false,false,true,true]`},
		{`try { new Object.is(1, 1); } catch (e) { return e.name; }`, `"TypeError"`},
	})
}

func TestObjectLegacyAccessors(t *testing.T) {
	accTable(t, [][2]string{
		{`var o = {}; o.__defineGetter__("x", function () { return this === o; });
		  var d = Object.getOwnPropertyDescriptor(o, "x");
		  return [o.x, d.enumerable, d.configurable, d.set];`, `[true,true,true,null]`},
		{`var o = {}, got; o.__defineSetter__("x", function (v) { got = v; }); o.x = 5;
		  var d = Object.getOwnPropertyDescriptor(o, "x"); return [got, d.get, d.enumerable];`, `[5,null,true]`},
		// Defining the other half keeps the first.
		{`var o = {}; o.__defineGetter__("x", function () { return 1; }); o.__defineSetter__("x", function () {});
		  var d = Object.getOwnPropertyDescriptor(o, "x"); return [typeof d.get, typeof d.set, o.x];`, `["function","function",1]`},
		{`try { ({}).__defineGetter__("x", 1); } catch (e) { return e.name; }`, `"TypeError"`},
		{`try { ({}).__defineSetter__("x"); } catch (e) { return e.name; }`, `"TypeError"`},
		// The callable check runs before the key coercion.
		{`var n = 0; var k = {toString: function () { n++; return "k"; }};
		  try { ({}).__defineGetter__(k, null); } catch (e) { return [e.name, n]; }`, `["TypeError",0]`},
		{`var o = Object.preventExtensions({}); try { o.__defineGetter__("x", function () {}); } catch (e) { return e.name; }`, `"TypeError"`},
		{`var o = {}; Object.defineProperty(o, "x", {value: 1}); try { o.__defineGetter__("x", function () {}); } catch (e) { return e.name; }`, `"TypeError"`},
		{`try { Object.prototype.__defineGetter__.call(undefined, "x", function () {}); } catch (e) { return e.name; }`, `"TypeError"`},
		{`var o = {}; o.__defineGetter__(1, function () { return "one"; }); return [o[1], Object.keys(o)];`, `["one",["1"]]`},

		// Lookup walks the chain to the first own property.
		{`var g = function () {}; var p = {}; p.__defineGetter__("x", g); var o = Object.create(p);
		  return [o.__lookupGetter__("x") === g, o.__lookupSetter__("x")];`, `[true,null]`},
		{`var p = {}; p.__defineGetter__("x", function () {}); var o = Object.create(p); o.x0 = 1;
		  Object.defineProperty(o, "x", {value: 1}); return o.__lookupGetter__("x");`, `undefined`},
		{`return [({}).__lookupGetter__("nope"), Object.create(null).__lookupGetter__ === undefined];`, `[null,true]`},
		{`var s = function (v) {}; var o = {set y(v) {}}; o.__defineSetter__("z", s); return [o.__lookupSetter__("z") === s, typeof o.__lookupSetter__("y")];`,
			`[true,"function"]`},
		{`return typeof ({}).__lookupGetter__("__proto__");`, `"function"`},
		{`return [].__lookupGetter__("length");`, `undefined`},
		{`try { Object.prototype.__lookupGetter__.call(null, "x"); } catch (e) { return e.name; }`, `"TypeError"`},
		{`var n = 0; var k = {toString: function () { n++; return "x"; }}; ({}).__lookupSetter__(k); return n;`, `1`},
		{`return typeof "ab".__lookupGetter__("length");`, `"undefined"`},
	})
}
