// script
// Annex B in sloppy scripts, where Sobek implements it: function declarations
// in if statements and blocks, the block functions B.3.2 does not hoist, a
// for-in var redeclaring a catch parameter, legacy octal literals and
// escapes, and legacy built-ins. Sobek implements neither B.3.2 hoisting, nor
// B.3.4's var redeclaration, nor for-in initializers, call assignment
// targets, labelled function declarations, 08/09 literals, "\400",
// HTML-like comments, the String HTML methods, Date getYear, setYear and
// toGMTString, or __defineGetter__ and its kin (docs/NOTES.md,
// "Differential test differences").
var r = {};

// B.3.3: function declarations in if statements.
if (true) function ifFn() { return "if"; }
r.ifFn = ifFn();
r.ifElse = (function () { if (true) function then() { return 2; } else function otherwise() {} return [then(), typeof otherwise]; })();

// Block functions: visible in the block; B.3.2 does not make a var of one
// whose var would be an early error or would replace a parameter.
r.inBlock = (function () { { function g() { return 1; } var x = g(); } return x; })();
r.blockedByLet = (function () { let f = 1; { function f() {} } return typeof f; })();
r.blockedByParam = (function (f) { { function f() {} } return typeof f; })(1);
r.blockedByInnerLet = (function () { { let h = 1; { function h() {} } } return typeof h; })();
r.blockScoped = (function () { "use strict"; { function s() {} } return typeof s; })();

// B.3.4: a for-in var may redeclare a simple catch parameter.
r.catchForIn = (function () { var seen; try { throw 1; } catch (e) { for (var e in { key: 1 }) seen = e; } return [typeof e, seen]; })();

// Legacy octal literals and escapes (ES2025 12.9.3, 12.9.4), sloppy code only.
r.octal = [010, 0777, 00, "\101", "\0", "\1234", "\08", "\8", "\9", "\7", "\377"];

// B.2: legacy built-ins.
r.builtins = [escape("a b+äć/@*_-."), unescape("%u0107%41%zz%"), "abcdef".substr(-4, 2), "abc".substr(1),
  "  t ".trimLeft() === "  t ".trimStart(), String.prototype.trimRight === String.prototype.trimEnd];
r.proto = [Object.getPrototypeOf({ __proto__: Array.prototype }) === Array.prototype,
  Object.getPrototypeOf({ "__proto__": null }) === null, Object.keys({ ["__proto__"]: 1 })];

r;
