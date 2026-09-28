// script
// The Function, GeneratorFunction and AsyncFunction constructors
// (CreateDynamicFunction): parameters and body, the global scope of the
// function, toString, and the SyntaxErrors of a parameter list or body that
// does not stay what it is. Sobek has no async generators, binds the name
// anonymous in the function and accepts a parameter list that ends inside
// a comment the body closes, so those are left to the unit tests
// (docs/NOTES.md, "Differential test differences").
var r = {};
function errName(f) {
  try { f(); return "no error"; } catch (e) { return e.name; }
}
var GeneratorFunction = Object.getPrototypeOf(function* () {}).constructor;
var AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;

var add = new Function("a", "b", "return a + b");
r.call = [add(2, 3), Function("a, b", "return a * b")(2, 3), Function("...rest", "return rest.length")(1, 2, 3)];
r.shape = [add.name, add.length, typeof add.prototype, Object.getPrototypeOf(add) === Function.prototype];
r.toString = [String(add), String(Function()), String(Function("a = 1", "b", ""))];
r.scope = (function () { var local = 1; return new Function("return typeof local")(); })();
r.sloppy = [new Function("return this")() === globalThis, new Function("'use strict'; return this")()];
r.defaults = Function("a = 2", "{ b } = { b: 3 }", "return a * b")();

var gen = new GeneratorFunction("a", "yield a; yield a * 2");
r.generator = [Array.from(gen(4)), String(gen), Object.getPrototypeOf(gen) === GeneratorFunction.prototype, typeof gen.prototype];
var asyncFn = AsyncFunction("a", "return await a + 1");
r.async = [asyncFn(1) instanceof Promise, String(asyncFn), "prototype" in asyncFn];

// Subclasses construct instances of themselves.
class MyFunction extends Function {}
var mine = new MyFunction("return 7");
r.subclass = [mine(), mine instanceof MyFunction];

r.errors = [
  errName(function () { Function("}", ""); }),
  errName(function () { Function("", "}{"); }),
  errName(function () { Function("", "}; function x() {"); }),
  errName(function () { Function("a", "'use strict'; var a; with (a) {}"); }),
  errName(function () { Function("a, a", "'use strict';"); }),
  errName(function () { GeneratorFunction("yield", ""); }),
  errName(function () { AsyncFunction("", "await"); }),
  errName(function () { AsyncFunction("a = await 1", ""); }),
  errName(function () { Function("", "super.x"); }),
  errName(function () { Function("", "new.target"); }),
];
r.argumentsToString = Function({ toString() { return "p"; } }, { toString() { return "return p"; } })(9);

r;
