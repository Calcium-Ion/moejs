// script
// Sloppy scripts: this coercion, implicit globals, silent failed assignments
// and deletes, and the global declarations of a script.
var r = {};
function errName(f) {
  try { f(); return "no error"; } catch (e) { return e.name; }
}
function desc(o, k) {
  var d = Object.getOwnPropertyDescriptor(o, k);
  return d ? [typeof d.value, d.writable, d.enumerable, d.configurable] : "none";
}

// this is coerced: undefined and null to the global object, primitives to
// wrappers; strict functions and class bodies keep it as passed.
function self() { return this; }
function selfType() { return typeof this; }
function strictSelf() { "use strict"; return this; }
class Klass { m() { return this; } }
r.thisTop = this === globalThis;
r.thisCoerced = [self() === globalThis, self.call(null) === globalThis, self.call(undefined) === globalThis,
  selfType.call(1), selfType.call("s"), selfType.call(true), selfType.call(Symbol("x")), selfType.call(1n),
  self.call(5) instanceof Number, self.call(5) !== self.call(5), self.call("ab").length];
r.thisStrict = [strictSelf() === undefined, strictSelf.call(1) === 1, typeof strictSelf.call("s"),
  new Klass().m.call(undefined) === undefined];
r.thisArrow = (function () { return (() => this)(); }).call(2) instanceof Number;
r.thisCallbacks = [[1].map(function () { return this === globalThis; })[0], [1].map(function () { return typeof this; }, 5)[0]];
r.thisBound = [self.bind(null)() === globalThis, typeof self.bind(3)(), Reflect.apply(self, undefined, []) === globalThis];
r.thisGetter = typeof Object.getOwnPropertyDescriptor({ get g() { return this; } }, "g").get.call(7);
r.thisGenerator = (function* () { yield this; }).call(undefined).next().value === globalThis;

// An unresolvable assignment creates a configurable global property; var and
// function declarations create non-configurable ones.
implicitOne = 1;
(function () { implicitTwo = 2; })();
var declared = 1;
function declaredFn() {}
r.implicit = [implicitOne, globalThis.implicitTwo, desc(globalThis, "implicitOne")];
r.declared = [desc(globalThis, "declared"), desc(globalThis, "declaredFn")];
r.hoisted = [typeof laterFn, typeof laterVar, "laterVar" in globalThis];
function laterFn() {}
var laterVar = 1;
function twice() { return 1; }
function twice() { return 2; }
r.lastFunctionWins = twice();

// let, const and class are global bindings but not properties.
let lex = 1;
const constLex = 2;
r.lexical = ["lex" in globalThis, "constLex" in globalThis, "Klass" in globalThis, typeof Klass, lex + constLex];
r.constAssign = errName(function () { constLex = 3; });
r.tdz = errName(function () { return laterLet; });
let laterLet = 1;
globalThis.shadowed = "property";
let shadowedLex = typeof shadowed;
r.propertyVisible = shadowedLex;

// Failed assignments are silent.
var frozen = Object.freeze({ p: 1 });
frozen.p = 2; frozen.q = 3; frozen["r"] = 4;
var frozenArr = Object.freeze([1, 2]);
frozenArr[0] = 9; frozenArr[5] = 1; frozenArr.length = 0;
var getterOnly = { get g() { return 1; } };
getterOnly.g = 2;
var nonExt = Object.preventExtensions({});
nonExt.x = 1;
var str = "abc";
str.prop = 1; str[0] = "z"; str.length = 0;
var num = 5;
num.prop = 1;
var readonly = Object.defineProperty({}, "ro", { value: 1 });
readonly.ro = 2;
var inherited = Object.create(frozen);
inherited.p = 5;
function named() {}
named.name = "x";
named.length = 9;
undefined = 1;
Infinity = 2;
r.silent = [frozen.p, frozen.q, frozen.r, frozenArr.join(), getterOnly.g, "x" in nonExt, str.prop, str, str.length,
  num.prop, readonly.ro, inherited.p, Object.keys(inherited), named.name, named.length, typeof undefined, String(Infinity)];
r.silentCompound = (function () { var o = Object.freeze({ n: 1 }); o.n += 5; o.n++; o.n ||= 0; o.n &&= 7; return o.n; })();
r.silentSuper = (function () {
  var o = { m() { super.x = 1; Object.freeze(this); super.y = 2; super["z"] = 3; return Object.keys(this); } };
  return o.m();
})();
r.nullBase = [errName(function () { var n = null; n.p = 1; }), errName(function () { var u; u[0] = 1; }),
  errName(function () { var n = null; n.p += 1; })];
r.setterThrows = errName(function () { ({ set s(v) { throw new RangeError("s"); } }).s = 1; });

// delete reports false instead of throwing.
var deletableVar = 1;
implicitDel = 1;
r.deletes = [delete deletableVar, delete implicitDel, typeof implicitDel, delete notDefinedAnywhere, delete frozen.p,
  delete Object.prototype, delete [].length, delete lex, delete str[0], delete str.length, delete Math.PI,
  delete frozenArr[0], (function (a) { var l; return [delete l, delete a, delete arguments]; })(1)];

// Names reserved only in strict code are identifiers.
var yield = 1, static = 2, implements = 3, package = 4, protected = 5, interface = 6, private = 7, public = 8;
var let = 9;
r.futureReserved = [yield, static, implements, package, protected, interface, private, public, let];

// Function declarations in sloppy scripts.
if (true) function ifDecl() { return "if"; } else function elseDecl() { return "else"; }
r.ifDecl = [ifDecl(), typeof elseDecl];
r.duplicateParams = (function (a, b, a) { return [a, b]; })(1, 2, 3);
r.completion = "done";

r;
