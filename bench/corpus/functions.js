function errName(f) {
  try { f(); return "no error"; } catch (e) { return e.name; }
}
function desc(o, k) {
  const d = Object.getOwnPropertyDescriptor(o, k);
  if (!d) return d;
  return "get" in d
    ? { get: typeof d.get, sameAccessor: d.get === d.set, enumerable: d.enumerable, configurable: d.configurable }
    : { value: d.value, writable: d.writable, enumerable: d.enumerable, configurable: d.configurable };
}

// Strict (unmapped) arguments objects.
function args() { return arguments; }
function unmapped(a, b) {
  a = 10;
  arguments[1] = 20;
  return [a, b, arguments[0], arguments[1], arguments.length];
}
function withDefaults(a, b = 2, ...rest) { return [arguments.length, a, b, rest, Array.from(arguments)]; }
function shapes() {
  const a = args(1, "two", null);
  const empty = args();
  return {
    length: [a.length, empty.length, args.apply(null, new Array(300)).length],
    type: [typeof a, Object.prototype.toString.call(a), Array.isArray(a), a instanceof Object, Object.getPrototypeOf(a) === Object.prototype],
    keys: [Object.keys(a), Object.getOwnPropertyNames(a), JSON.stringify(a), JSON.stringify(empty)],
    descs: [desc(a, "length"), desc(a, "callee"), desc(a, "0"), desc(a, "3")],
    callee: [errName(() => a.callee), errName(() => { a.callee = 1; }), "callee" in a],
    copies: [Array.prototype.slice.call(a, 1), Array.from(a), Array.prototype.join.call(a, "-"), Array.prototype.map.call(a, (x) => typeof x)],
    distinct: args() !== args(),
  };
}
function mutation() {
  const a = args(1, 2, 3);
  a.length = 5;
  a[4] = "four";
  delete a[0];
  a.extra = true;
  const b = args("x");
  b.length = 0;
  return { a: [a.length, a[0], a[4], 0 in a, Object.keys(a), JSON.stringify(a)], b: [b.length, b[0], Array.from(b)] };
}
function arrows() {
  const outer = function () { const f = () => arguments[0] + arguments.length; return f(99); };
  const nested = function () { return (function () { return arguments.length; })(1, 2, 3) + arguments.length; };
  const param = function (a) { return [a, arguments[0]]; };
  return [outer(5, 6), nested(1), param("p", "q")];
}

// Tagged templates.
function tag(strings, ...values) {
  return {
    strings: Array.from(strings), raw: Array.from(strings.raw), values,
    frozen: [Object.isFrozen(strings), Object.isFrozen(strings.raw), Array.isArray(strings), Array.isArray(strings.raw)],
    lengths: [strings.length, strings.raw.length, values.length],
    rawDesc: desc(strings, "raw"),
  };
}
function ident(s) { return s; }
function site() { return ident`same`; }
function siteWith(x) { return ident`same${x}`; }
function templates() {
  const log = [];
  const logged = (name, v) => { log.push(name); return v; };
  const obj = { name: "o", t(s, ...v) { return this.name + ":" + s.join("|") + ":" + v.join(","); } };
  const make = (p) => (s, ...v) => p + s.join("|") + v.join(",");
  function Ctor() { this.made = true; }
  function factory() { return Ctor; }
  function thisOf() { return this; }
  const seen = [];
  for (let i = 0; i < 3; i++) seen.push(ident`loop${i}`);
  const t1 = site(), t2 = site(), t3 = siteWith(1), t4 = siteWith(2);
  const x = 5;
  return {
    basic: tag`a${1}b${"two"}c`,
    empty: tag``,
    edges: tag`${x}${x}`,
    escapes: tag`line\nbreak \x41 \u0042 \u{43} \\ \``,
    invalid: tag`\unicode and \u{110000} and \xZ and \01`,
    invalidMid: tag`ok${1}\u{bad${2}fine`,
    continuation: tag`a\
b`,
    identity: [t1 === t2, t3 === t4, t1 === t3, t1 === ident`same`, seen[0] === seen[1], seen[1] === seen[2]],
    member: [obj.t`x${1}y`, obj["t"]`z`],
    call: make("p")`a${1}b${2}c`,
    ctor: new factory`x`().made,
    thisValue: thisOf`x` === undefined,
    order: [logged("tag", tag)`${logged("first", 1)}${logged("second", 2)}`.values, log],
    args: (function () { return tag`${arguments.length}${arguments[0]}`.values; })("A", "B"),
    notCallable: errName(() => { const o = {}; return o.missing`x`; }),
    untagged: [`plain ${x} ${`nested ${x + 1}`}`, `${{}}`, `${[1, 2]}`, `${null}-${undefined}`],
  };
}

// Escaped and Unicode identifiers.
const \u0061lpha = "alpha";
const caf\u00e9 = "café", π = 3.14, 変数 = "hen", ᾩ = "greek";
function \u{66}n(\u0061rg) { return arg + 1; }
function identifiers() {
  const o = { \u0062eta: 2, \u{63}ount: 3, "\u0064": 4, if: "kw", \u0069n: "in" };
  o.\u0065lse = "else";
  const { beta: b, \u0063ount: c } = o;
  return {
    values: [alpha, \u0061lpha, café, caf\u{e9}, π, 変数, ᾩ, fn(1), \u0066n(2)],
    keys: Object.keys(o),
    props: [o.beta, o.\u0062eta, o.count, o.d, o.if, o.\u0069f, o.in, o.else, b, c],
    keywordProps: [{ new: 1, class: 2, function: 3 }.class, { default: 1 }.\u0064efault],
  };
}

// Class methods, accessors and constructors have their own arguments and
// parameter scopes too.
class ArgsBase {
  constructor(a, b = 1) { this.n = arguments.length; this.first = arguments[0]; this.b = b; }
  static s(x = 2) { var x; return [x, arguments.length]; }
  t() { return String.raw`a${arguments.length}\n`; }
  get g() { return arguments.length; }
  set g(v) { this.gv = arguments.length; }
}
class ArgsDerived extends ArgsBase {
  constructor(p = 3) { var p; super(...arguments); this.p = p; this.args = Array.from(arguments); }
  static s() { return super.s(...arguments); }
}
class ArgsDefault extends ArgsBase {}
class ArgsSelf extends ArgsBase {
  constructor(a = () => this) { super(10); this.self = a() === this; }
}
function ArgsFn(a = 0) { var a; this.nt = new.target === ArgsFn; this.len = arguments.length; }
function classes() {
  const a = new ArgsBase(1, 2, 3), b = new ArgsDerived(4, 5), c = new ArgsDefault(6), d = new ArgsSelf();
  a.g = 1;
  const o = { m(x = this.valueOf) { var x; return [typeof x, arguments.length]; }, ["k" + 1](a) { return arguments.length; } };
  return {
    base: [a.n, a.first, a.b, ArgsBase.s(), ArgsBase.s(undefined, 1), a.t(), a.g, a.gv],
    derived: [b.n, b.first, b.p, b.args, ArgsDerived.s(8, 9)],
    implicit: [c.n, c.first, c.b],
    self: [d.n, d.first, d.self],
    fn: [new ArgsFn().nt, new ArgsFn(1).len, ArgsFn.call({}, 2) === undefined],
    methods: [o.m(), o.m(1, 2), o.k1(1, 2, 3)],
  };
}

export function run() {
  return {
    unmapped: [unmapped(1, 2), unmapped(1), unmapped()],
    defaults: [withDefaults(1), withDefaults(1, undefined, 3, 4)],
    shapes: shapes(),
    mutation: mutation(),
    arrows: arrows(),
    templates: templates(),
    identifiers: identifiers(),
    classes: classes(),
  };
}
