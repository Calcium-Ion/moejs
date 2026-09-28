// script
// Mapped arguments objects of sloppy functions with simple parameter lists,
// and arguments.callee. Sobek mishandles an arrow function that uses the
// enclosing function's arguments, and keeps a mapped index enumerable when it
// is redefined with enumerable: false (docs/NOTES.md, "Differential test
// differences").
var r = {};
function errName(f) {
  try { f(); return "no error"; } catch (e) { return e.name; }
}
function desc(o, k) {
  var d = Object.getOwnPropertyDescriptor(o, k);
  if (!d) return "none";
  return "get" in d ? ["accessor", d.enumerable, d.configurable] : [typeof d.value, d.writable, d.enumerable, d.configurable];
}

function mapped(a, b) {
  var before = [a, b, arguments[0], arguments[1]];
  a = 10;
  arguments[1] = 20;
  return before.concat([a, b, arguments[0], arguments[1], arguments.length]);
}
r.mapped = [mapped(1, 2), mapped(1)];
r.extra = (function (a) { arguments[3] = "x"; return [arguments.length, a, arguments[3]]; })(1, 2);
r.closure = (function (a) { var g = function () { return a; }; arguments[0] = 9; return g(); })(1);
r.generator = (function* (a) { a = 2; yield arguments[0]; arguments[0] = 4; yield a; })(1);
r.generator = [r.generator.next().value, r.generator.next().value];
r.duplicate = [(function (a, a) { return [a, arguments[0], arguments[1]]; })(1, 2),
  (function (a, a) { a = 3; return [arguments[0], arguments[1]]; })(1, 2)];

// Redefining an index keeps or removes its mapping.
r.deleteUnmaps = (function (a) { delete arguments[0]; arguments[0] = 5; var x = a; a = 6; return [x, arguments[0]]; })(1);
r.defineValue = (function (a) { Object.defineProperty(arguments, "0", { value: 7 }); var x = a; a = 8; return [x, arguments[0]]; })(1);
r.defineReadonly = (function (a) {
  Object.defineProperty(arguments, "0", { value: 7, writable: false });
  a = 8;
  return [a, arguments[0], desc(arguments, "0")];
})(1);
r.defineAccessor = (function (a) {
  Object.defineProperty(arguments, "0", { get: function () { return "g"; }, configurable: true });
  a = 9;
  return [a, arguments[0], desc(arguments, "0")];
})(1);
r.freeze = (function (a) { Object.freeze(arguments); a = 2; return [a, arguments[0], Object.isFrozen(arguments)]; })(1);
r.readonlyWrite = (function (a) { Object.defineProperty(arguments, "0", { writable: false }); arguments[0] = 3; return [a, arguments[0]]; })(1);

// The object's own properties.
function shapes(a, b) {
  return {
    keys: Object.keys(arguments),
    names: Object.getOwnPropertyNames(arguments),
    tag: Object.prototype.toString.call(arguments),
    json: JSON.stringify(arguments),
    iterator: arguments[Symbol.iterator] === Array.prototype.values,
    spread: [...arguments],
    slice: Array.prototype.slice.call(arguments),
    length: desc(arguments, "length"),
    callee: desc(arguments, "callee"),
    index: desc(arguments, "0"),
    proto: Object.getPrototypeOf(arguments) === Object.prototype,
    isArray: Array.isArray(arguments),
  };
}
r.shapes = shapes(1, "two", null);
r.lengthWrite = (function (a) { arguments.length = 5; return [arguments.length, Array.prototype.slice.call(arguments).length]; })(1);

// arguments.callee is the function; strict code and functions with
// non-simple parameters get an unmapped object whose callee throws.
function callee() { return arguments.callee === callee; }
r.callee = [callee(), (function () { return typeof arguments.callee; })(),
  (function fact(n) { return n <= 1 ? 1 : n * arguments.callee(n - 1); })(5)];
r.strictUnmapped = (function (a) { "use strict"; a = 2; return [arguments[0], errName(function () { return arguments.callee; })]; })(1);
r.defaults = (function (a = 0) { a = 2; return [arguments[0], errName(() => arguments.callee)]; })(1);
r.rest = (function (a, ...rest) { a = 5; return [arguments[0], rest]; })(1, 2);
r.destructured = (function ({ x }) { return [x, arguments[0].x, errName(() => arguments.callee)]; })({ x: 3 });

// arguments is an ordinary binding in sloppy code.
r.assigned = (function () { arguments = 1; return arguments; })();
r.varArguments = (function (a) { var arguments; return typeof arguments; })(1);
r.functionArguments = (function (a) { function arguments() {} return typeof arguments; })(1);
r.paramArguments = (function (arguments) { return arguments; })("param");
r.apply = Math.max.apply(null, (function () { return arguments; })(1, 5, 3));

r;
