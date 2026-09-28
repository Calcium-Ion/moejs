// script
// Direct and indirect eval: the caller's scope, where sloppy eval code
// declares its vars and functions, strict eval code, this, arguments,
// new.target and super inside eval code, and eval's early errors. Sobek
// declares no var for a block function of eval code (Annex B.3.2.3), so
// none is used here (docs/NOTES.md, "Differential test differences").
var r = {};
function errName(f) {
  try { f(); return "no error"; } catch (e) { return e.name; }
}

r.completion = [eval("1; 2 + 3"), eval("if (true) 'then'; else 'else';"), eval(""), eval(42), typeof eval({})];
r.scope = (function (a) { let l = 2; const c = 3; return eval("a * l + c"); })(4);

// Sloppy eval code declares its vars and functions in the caller.
r.sloppyVars = (function () {
  eval("var v = 1; function fn() { return 'fn'; }");
  var before = [v, fn()];
  eval("var v = 2");
  return before.concat(v, delete v, typeof v, delete fn, typeof fn);
})();
r.shared = (function () { eval("var s1 = 1"); eval("var s2 = s1 + 1"); return s2; })();
r.closure = (function () { eval("var n = 0"); var inc = () => ++n; inc(); inc(); return [n, inc()]; })();
r.nested = (function () { var x = 1; return eval("eval('x + 1')"); })();
r.implicitGlobal = (function () { eval("implicitFromEval = 5"); return typeof implicitFromEval; })();
r.letStays = (function () { eval("let l = 1; const k = 2; class K {}"); return [typeof l, typeof k, typeof K]; })();

// Strict eval code, or eval in strict code, gets a variable environment of
// its own.
r.strict = [
  (function () { "use strict"; eval("var sv = 1"); return typeof sv; })(),
  (function () { eval("'use strict'; var sv = 1"); return typeof sv; })(),
  (function () { "use strict"; return eval("var sv = 1; sv"); })(),
];

// Global eval code.
eval("var globalVar = 'gv'; function globalFn() { return 'gf'; }");
r.global = [globalVar, globalFn(), Object.getOwnPropertyDescriptor(globalThis, "globalVar").configurable];
r.globalDelete = [delete globalThis.globalVar, typeof globalVar];

// Indirect eval runs as global code.
var where = "global";
r.indirect = (function () {
  var where = "local";
  var e = eval;
  return [(0, eval)("where"), e("where"), eval("where"), globalThis.eval("typeof e")];
})();
r.indirectDeclares = (function () { (0, eval)("var fromIndirect = 1"); return typeof fromIndirect; })() + "," + typeof fromIndirect;
r.indirectStrict = (function () { "use strict"; return (0, eval)("this === globalThis"); })();

// this, arguments, new.target and super.
var obj = { m() { return eval("this === obj"); } };
r.thisValue = [obj.m(), (() => eval("this === globalThis"))()];
r.args = (function (a, b) { return eval("arguments.length + ':' + arguments[1]"); })(1, "two", 3);
r.mappedArgs = (function (a) { eval("arguments[0] = 'changed'"); return a; })("orig");
function NT() { this.nt = eval("new.target === NT"); }
r.newTarget = [new NT().nt, eval("typeof function () { return eval('new.target'); }")];
class Base { m() { return "base"; } }
class Derived extends Base {
  constructor() { eval("super()"); this.field = eval("super.m()"); }
  m() { return eval("super.m() + '!'"); }
}
r.superUse = [new Derived().field, new Derived().m()];
class Fields { a = 1; b = eval("this.a + 1"); static s = eval("this.name"); }
r.fields = [new Fields().b, Fields.s];

// Parameter expressions.
r.params = (function (a = eval("var pv = 'param'"), b = pv) { return b; })();

// with statements.
var wo = { p: 1 };
with (wo) { r.withRead = eval("p + 1"); eval("var p = 5"); }
r.withWrite = [wo.p, typeof p];

// A local binding named eval is called like any function.
r.localEval = (function (eval) { return eval("x"); })(function (s) { return s + "!"; });

// Errors.
r.errors = [
  errName(function () { eval("a b"); }),
  errName(function () { eval("throw new RangeError('x')"); }),
  errName(function () { eval("new.target"); }),
  errName(function () { eval("super.x"); }),
  errName(function () { class A { m() { eval("super()"); } } new A().m(); }),
  errName(function () { class G { x = eval("arguments"); } new G(); }),
  errName(function () { let taken; eval("var taken"); }),
  errName(function () { eval("undefinedFunction()"); }),
];
r.errorMessage = (function () { try { eval("throw new TypeError('boom')"); } catch (e) { return e.message; } })();

// The same string evaluated repeatedly, in a loop.
var sum = 0;
for (var i = 0; i < 50; i++) sum += eval("i * 2");
r.loop = sum;

r;
