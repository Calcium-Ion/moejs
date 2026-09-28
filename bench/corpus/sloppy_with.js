// script
// with statements: object environments, @@unscopables and the references
// they intercept. Sobek's Array.prototype[@@unscopables] lacks "at", and a
// Proxy object environment sees no second has trap before a get or set
// (docs/NOTES.md, "Differential test differences").
var r = {};
function errName(f) {
  try { f(); return "no error"; } catch (e) { return e.name; }
}

var o = { a: 1, b: 2, f: function () { return this === o; } };
with (o) {
  r.read = [a, b, typeof c, f()];
  a = 10;
  var b = 20;
  c = 30;
  var d = 40;
}
r.write = [o.a, o.b, typeof b, c, d, "c" in o, "d" in o];

with ({ x: 1 }) with ({ y: 2 }) r.nested = x + y;
with ({ x: 1 }) with ({ x: 2 }) r.shadow = x;
with ({ x: 1 }) { let x = 2; r.letInside = x; }
r.missFallsThrough = (function () { var local = "local"; with ({}) return local; })();
r.paramShadowed = (function (p, q) { with (p) return [q, typeof p]; })({ q: "from object" }, "param");

// Symbol.unscopables hides names from the object environment.
var u = "outer u", v = "outer v";
var uo = { u: "inner u", v: "inner v", [Symbol.unscopables]: { u: true, v: false } };
with (uo) r.unscopables = [u, v];
var keys = "outer keys", values = "outer values", flat = "outer flat";
with ([]) r.arrayUnscopables = [keys, values, flat, typeof push, length];
var proto = Object.create({ [Symbol.unscopables]: { hidden: true } });
proto.hidden = "inner";
var hidden = "outer";
with (proto) r.inheritedUnscopables = hidden;

// Calls through the object environment pass the object as this.
var calls = { m() { return this === calls; }, t() { return this === calls; } };
with (calls) r.callThis = [m(), t``, (0, m)() === false];

// Compound, update, logical assignment, typeof and delete resolve through
// the object.
var counter = { n: 1, z: 0, gone: 1 };
with (counter) {
  n += 2;
  n++;
  z ||= 7;
  r.typeofs = [typeof n, typeof nothingHere];
  r.deleted = [delete gone, delete nothingHere];
}
r.counter = [counter.n, counter.z, "gone" in counter];

// Closures keep the object environment.
var live = { x: 1 };
var readX, readXArrow, writeX;
with (live) {
  readX = function () { return x; };
  readXArrow = () => x;
  writeX = function (v) { x = v; };
}
live.x = 5;
writeX(6);
r.closures = [readX(), readXArrow(), live.x];

// Primitives are boxed; null and undefined throw.
with ("abc") r.primitive = [length, charAt(1)];
with (5) r.number = toFixed(1);
r.nullObject = [errName(function () { with (null) {} }), errName(function () { with (undefined) {} })];

// A binding deleted after the reference was resolved: a sloppy assignment
// recreates it, a read of a missing name falls to the next environment.
var recreated = { p: 1 };
with (recreated) { p = (delete recreated.p, 5); }
r.recreated = recreated.p;

// A with body's completion value.
r.completion = [(function () { return typeof "unused"; })()];
with ({}) { r.emptyBody = "ran"; }

r;
