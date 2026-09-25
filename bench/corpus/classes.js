function errName(f) {
  try { f(); return "no error"; } catch (e) { return e.name; }
}
function desc(o, k) {
  const d = Object.getOwnPropertyDescriptor(o, k);
  if (!d) return d;
  return "get" in d
    ? { get: typeof d.get, set: typeof d.set, enumerable: d.enumerable, configurable: d.configurable }
    : { value: typeof d.value === "function" ? "fn:" + d.value.name : d.value, writable: d.writable, enumerable: d.enumerable, configurable: d.configurable };
}
const suffix = "Key";
function basics() {
  class Point {
    constructor(x, y) { this.x = x; this.y = y; }
    norm() { return Math.sqrt(this.x * this.x + this.y * this.y); }
    get sum() { return this.x + this.y; }
    set sum(v) { this.x = v - this.y; }
    static origin() { return new Point(0, 0); }
    static get kind() { return "point"; }
    ["computed" + suffix]() { return "computed"; }
    static [suffix.toLowerCase()] = "static computed";
    7() { return "seven"; }
    "a b"() { return "spaced"; }
  }
  const p = new Point(3, 4);
  p.sum = 10;
  const Anon = class {};
  const Named = class Inner { who() { return Inner.name; } };
  return {
    norm: new Point(3, 4).norm(), sum: p.sum, x: p.x, origin: Point.origin().norm(), kind: Point.kind,
    computed: p.computedKey(), staticComputed: Point.key, seven: p[7](), spaced: p["a b"](),
    typeof: typeof Point, protoKeys: Object.keys(Point.prototype), ownNames: Object.getOwnPropertyNames(Point.prototype),
    staticNames: Object.getOwnPropertyNames(Point).sort(), instanceKeys: Object.keys(p),
    ctor: desc(Point.prototype, "constructor"), method: desc(Point.prototype, "norm"), accessor: desc(Point.prototype, "sum"),
    prototype: desc(Point, "prototype"), staticMethod: desc(Point, "origin"),
    names: [Point.name, Anon.name, Named.name, new Named().who(), Point.prototype.norm.name, Point.origin.name,
      Object.getOwnPropertyDescriptor(Point.prototype, "sum").get.name, Object.getOwnPropertyDescriptor(Point.prototype, "sum").set.name,
      Point.prototype.computedKey.name, Point.prototype[7].name],
    lengths: [Point.length, Point.prototype.norm.length, class { constructor(a, b = 1, ...c) {} }.length],
    callWithoutNew: errName(() => Point(1, 2)), newMethod: errName(() => new p.norm()),
    instanceOf: [p instanceof Point, Object.getPrototypeOf(p) === Point.prototype],
  };
}
function fields() {
  const log = [];
  let n = 0;
  class Counter {
    a = (log.push("a"), ++n);
    b = this.a * 10;
    arrow = () => this.a;
    [(log.push("key"), "c" + suffix)] = (log.push("c"), "c");
    static s = (log.push("static"), "s");
    static self = this;
    empty;
    constructor() { log.push("ctor:" + this.b); }
  }
  log.push("defined");
  const c1 = new Counter(), c2 = new Counter();
  const detached = c1.arrow;
  class Shadow { x = 1; get x() { return "getter"; } }
  return {
    log, a: [c1.a, c2.a], b: c2.b, arrow: detached(), cKey: c1.cKey, keys: Object.keys(c1),
    empty: [("empty" in c1), c1.empty === undefined], staticS: Counter.s, staticSelf: Counter.self === Counter,
    fieldDesc: desc(c1, "a"), shadow: new Shadow().x, json: JSON.stringify(new Counter()),
  };
}
function privates() {
  class Account {
    #balance = 0;
    static #count = 0;
    #log = [];
    constructor(start) { this.#balance = start; Account.#count++; }
    #record(op) { this.#log.push(op); return this; }
    get #doubled() { return this.#balance * 2; }
    set #amount(v) { this.#balance = v; this.#record("set:" + v); }
    deposit(v) { this.#balance += v; return this.#record("deposit:" + v); }
    reset() { this.#amount = 0; return this.#doubled; }
    get balance() { return this.#balance; }
    get doubled() { return this.#doubled; }
    history() { return this.#log.slice(); }
    static count() { return Account.#count; }
    static #secret() { return "static private"; }
    static reveal() { return Account.#secret(); }
    static isAccount(o) { return #balance in o; }
    static peek(o) { return o.#balance; }
    static callRecord(o) { return o.#record("x"); }
    static setAmount(o) { o.#amount = 1; }
    methodName() { return this.#record.name; }
    increment() { return this.#balance++; }
    compound() { this.#balance **= 2; this.#balance ||= 5; return this.#balance; }
    destructure() { [this.#balance] = [42]; ({ v: this.#balance } = { v: this.#balance + 1 }); return this.#balance; }
    optional(o) { return o?.#balance; }
  }
  const a = new Account(10).deposit(5), b = new Account(1);
  const hist = a.history();
  const before = a.balance, doubled = a.doubled, reset = a.reset();
  return {
    before, doubled, reset, after: a.balance, hist, histAfter: a.history(), count: Account.count(), reveal: Account.reveal(),
    isAccount: [Account.isAccount(a), Account.isAccount({}), errName(() => Account.isAccount(1))],
    brand: [errName(() => Account.peek({})), errName(() => Account.callRecord({})), errName(() => Account.setAmount({})), errName(() => Account.peek(null))],
    methodName: a.methodName(), increment: [b.increment(), b.balance], compound: b.compound(), destructure: b.destructure(),
    optional: [b.optional(b), b.optional(null)],
    invisible: [Object.keys(a), Object.getOwnPropertyNames(a), JSON.stringify(a), Object.getOwnPropertyNames(Account).sort()],
    hasOwn: ["#balance" in a, a.hasOwnProperty("#balance")],
  };
}
function privateCollisions() {
  const make = () => class { #x = "inner"; static get(o) { return o.#x; } };
  const A = make(), B = make();
  class Outer {
    #x = "outer";
    run() {
      class Nested { #x = "nested"; read(o) { return o.#x; } }
      const n = new Nested();
      return [n.read(n), errName(() => n.read(this)), this.#x];
    }
    static readInner(o) { return o.#x; }
  }
  class Returner { constructor(o) { return o; } }
  class Stamp extends Returner { #tag = "stamped"; static tag(o) { return o.#tag; } static has(o) { return #tag in o; } }
  const plain = {};
  new Stamp(plain);
  return {
    sameSource: [A.get(new A()), errName(() => A.get(new B()))],
    nested: new Outer().run(), foreign: errName(() => Outer.readInner(new A())),
    stamped: [Stamp.tag(plain), Stamp.has(plain), Object.keys(plain), JSON.stringify(plain), errName(() => new Stamp(plain))],
  };
}
function staticBlocks() {
  const log = [];
  let leaked;
  class Config {
    static defaults = { retries: 3 };
    static #secret = "s3";
    static {
      log.push("block1:" + this.defaults.retries);
      var local = "var in block";
      this.computed = this.defaults.retries * 2;
      leaked = () => Config.#secret;
      log.push(local);
    }
    static later = (log.push("field after block"), 1);
    static { log.push("block2:" + this.later); }
  }
  return { log, computed: Config.computed, leaked: leaked(), localLeak: typeof local };
}
function inheritance() {
  class Animal {
    constructor(name) { this.name = name; }
    speak() { return this.name + " makes a sound"; }
    get kind() { return "animal"; }
    static create(name) { return new this(name); }
    describe() { return this.constructor.name + ":" + this.kind; }
  }
  class Dog extends Animal {
    constructor(name, breed) { super(name); this.breed = breed; }
    speak() { return super.speak() + " (woof)"; }
    get kind() { return "dog/" + super.kind; }
  }
  class Puppy extends Dog {
    speak() { return super.speak().toUpperCase(); }
  }
  class Quiet extends Animal {}
  const d = new Dog("Rex", "lab"), p = new Puppy("Bit", "pug"), q = Quiet.create("Mo");
  return {
    speak: [d.speak(), p.speak(), q.speak()], kind: [d.kind, p.kind], describe: [d.describe(), p.describe(), q.describe()],
    breed: p.breed, staticInherited: Puppy.create("Pip") instanceof Puppy, chain: [p instanceof Dog, p instanceof Animal, Object.getPrototypeOf(Puppy) === Dog],
    quietLength: Quiet.length, keys: Object.keys(p),
  };
}
function superProperty() {
  const log = [];
  class Base {
    get v() { return "base getter:" + this.tag; }
    set v(x) { log.push("base setter " + x); this.stored = x; }
    m(k) { return "base " + k; }
    static sm() { return "base static " + this.name; }
  }
  class Derived extends Base {
    tag = "derived";
    readV() { return super.v; }
    writeV(x) { super.v = x; return this.stored; }
    writePlain() { super.plain = 1; return [this.plain, Base.prototype.plain]; }
    computed(k) { return super[k]("computed"); }
    arrow() { return (() => super.m("from arrow"))(); }
    static sm() { return super.sm() + "!"; }
  }
  const d = new Derived();
  const proto = { greet() { return "proto hello " + this.who; } };
  const obj = { who: "obj", greet() { return super.greet() + "!"; }, get g() { return super.greet(); } };
  Object.setPrototypeOf(obj, proto);
  const borrowed = { who: "borrowed", greet: obj.greet };
  return {
    readV: d.readV(), writeV: d.writeV(7), writePlain: d.writePlain(), log, computed: d.computed("m"), arrow: d.arrow(),
    static: Derived.sm(), literal: [obj.greet(), obj.g, borrowed.greet()],
  };
}
function derivedConstructors() {
  class Base { constructor() { this.base = true; } }
  class TDZ extends Base { constructor() { this.x = 1; super(); } }
  class Twice extends Base { constructor() { super(); super(); } }
  class NoSuper extends Base { constructor() {} }
  class ReturnsObject extends Base { constructor() { return { replaced: true }; } }
  class ReturnsPrimitive extends Base { constructor() { super(); return 1; } }
  class ReturnsUndefined extends Base { constructor() { super(); return undefined; } }
  class ArrowSuper extends Base { constructor() { const init = () => super(); init(); this.after = true; } }
  class Conditional extends Base { constructor(flag) { if (flag) { super(); } else { return { skipped: true }; } } }
  let count = 0;
  class Forward { constructor(...args) { this.args = args; count++; } }
  class Default extends Forward {}
  class TryFinally extends Base { constructor() { try { super(); return; } finally { this.finallyRan = true; } } }
  class BaseReturnsOther { constructor() { return { other: true }; } }
  class Inherit extends BaseReturnsOther { field = "set on returned object"; }
  class ThisInArrowBeforeSuper extends Base { constructor() { const f = () => this; errName(f); super(); this.ok = f() === this; } }
  const sparse = new Default(...[1, , 3]);
  return {
    tdz: errName(() => new TDZ()), twice: errName(() => new Twice()), noSuper: errName(() => new NoSuper()),
    returnsObject: new ReturnsObject(), returnsPrimitive: errName(() => new ReturnsPrimitive()), returnsUndefined: new ReturnsUndefined(),
    arrowSuper: new ArrowSuper(), conditional: [new Conditional(true), new Conditional(false)],
    forward: new Default(1, "two", [3]).args, sparse: [sparse.args.length, 1 in sparse.args], count,
    tryFinally: new TryFinally(), inherit: new Inherit(), arrowThis: new ThisInArrowBeforeSuper().ok,
  };
}
function heritage() {
  function OldStyle(v) { this.v = v; }
  OldStyle.prototype.get = function () { return this.v; };
  class FromFunction extends OldStyle { twice() { return this.get() * 2; } }
  const mixin = (B) => class extends B { mixed() { return "mixed:" + this.get(); } };
  class Mixed extends mixin(OldStyle) {}
  class NullBase extends null { static s() { return "static ok"; } }
  let evaluated = 0;
  const pick = () => { evaluated++; return OldStyle; };
  class Computed extends pick() {}
  function NullProto() {}
  NullProto.prototype = null;
  class FromNullProto extends NullProto {}
  return {
    fromFunction: new FromFunction(4).twice(), mixed: new Mixed(3).mixed(),
    nullBase: [NullBase.s(), Object.getPrototypeOf(NullBase.prototype), Object.getPrototypeOf(NullBase) === Function.prototype, errName(() => new NullBase())],
    computed: [new Computed(1).get(), evaluated], nullProto: Object.getPrototypeOf(FromNullProto.prototype),
    notCtor: [errName(() => { class X extends 1 {} }), errName(() => { const f = () => {}; class X extends f {} }), errName(() => { class X extends Math.max {} })],
    badProto: errName(() => { function F() {} F.prototype = 3; class X extends F {} }),
  };
}
function newTarget() {
  function Plain() { return new.target === undefined ? "call" : "new:" + new.target.name; }
  function ViaArrow() { const f = () => new.target; this.nt = f() === ViaArrow; }
  class Base { constructor() { this.created = new.target.name; } static make() { return new this(); } }
  class Child extends Base {}
  class Abstract { constructor() { if (new.target === Abstract) throw new TypeError("abstract"); } }
  class Concrete extends Abstract {}
  class FieldNT { x = new.target; static y = new.target; }
  return {
    plain: [Plain(), new Plain().constructor === Plain], viaArrow: new ViaArrow().nt,
    created: [new Base().created, new Child().created, Child.make().created],
    abstract: [errName(() => new Abstract()), new Concrete() instanceof Abstract],
    field: [new FieldNT().x, FieldNT.y],
  };
}
function bindings() {
  class Inner { static rename() { Inner = null; } static self() { return Inner; } }
  const renameErr = errName(() => Inner.rename());
  let Outer = class Outer { self() { return Outer; } };
  const saved = Outer;
  const instance = new Outer();
  Outer = "reassigned";
  let tdzErr = errName(() => { class A extends A {} });
  let computedTdz = errName(() => { class B { [B.name]() {} } });
  return { renameErr, selfIntact: Inner.self() === Inner, outer: [typeof Outer, instance.self() === saved], tdzErr, computedTdz };
}
function sourceText() {
  class Shape { area() { return 0; } }
  const expr = class Named extends Shape { static s() {} };
  const methods = { m(a) { return a; } };
  return [String(Shape), String(expr), String(Shape.prototype.area), String(expr.s), String(methods.m),
    String(Object.getOwnPropertyDescriptor(class { get g() { return 1; } }.prototype, "g").get)];
}
function builtins() {
  class Stack extends Array {
    peek() { return this[this.length - 1]; }
  }
  const s = new Stack();
  s.push(1, 2, 3);
  const s2 = new Stack(3);
  class HttpError extends Error {
    constructor(status, msg) { super(msg); this.status = status; this.name = "HttpError"; }
  }
  class Validation extends TypeError {}
  const e = new HttpError(404, "not found"), v = new Validation("bad");
  class Pattern extends RegExp { first(s) { return this.exec(s)[0]; } }
  const re = new Pattern("a+", "g");
  class Day extends Date { iso() { return this.toISOString().slice(0, 10); } }
  class Obj extends Object { hi() { return "hi"; } }
  class Bool extends Boolean {}
  class Num extends Number { double() { return this * 2; } }
  class Str extends String { shout() { return this.toUpperCase() + "!"; } }
  return {
    stack: [s.peek(), s.length, Array.isArray(s), s instanceof Stack, s2.length, JSON.stringify(s), errName(() => new Stack(-1))],
    error: [e.message, e.status, e.name, String(e), e instanceof Error, e instanceof HttpError, Object.prototype.toString.call(e)],
    validation: [v.name, v.message, v instanceof TypeError, v instanceof Validation, String(v)],
    regexp: [re.first("caab"), re.lastIndex, re.source, re.flags, re instanceof RegExp],
    date: [new Day(86400000).iso(), new Day(0) instanceof Date], object: [new Obj().hi(), new Obj(1).hi()],
    wrappers: [new Bool(false).valueOf(), new Num(21).double(), new Str("hey").shout(), new Str("abc").length],
  };
}
function pluginStyle(arg) {
  class Headers {
    #map = {};
    constructor(init) { for (const k of Object.keys(init)) this.set(k, init[k]); }
    set(k, v) { this.#map[k.toLowerCase()] = String(v); return this; }
    get(k) { return this.#map[k.toLowerCase()]; }
    has(k) { return k.toLowerCase() in this.#map; }
    toJSON() { return Object.assign({}, this.#map); }
  }
  class RequestBuilder {
    static #instances = 0;
    #headers;
    #body = {};
    constructor(model) { this.model = model; this.#headers = new Headers({ Accept: "application/json" }); RequestBuilder.#instances++; }
    header(k, v) { this.#headers.set(k, v); return this; }
    field(k, v) { if (v !== undefined) this.#body[k] = v; return this; }
    build() { return { model: this.model, headers: this.#headers.toJSON(), body: { ...this.#body } }; }
    static get instances() { return RequestBuilder.#instances; }
  }
  class VendorBuilder extends RequestBuilder {
    build() {
      const req = super.build();
      req.body.vendor = true;
      return req;
    }
  }
  const body = arg.body.value;
  const out = [];
  for (let i = 0; i < 3; i++) {
    const b = new VendorBuilder(body.model).header("Authorization", arg.headers.Authorization).field("size", body.size).field("n", body.n + i).field("missing", undefined);
    out.push(b.build());
  }
  return { out, instances: RequestBuilder.instances, hasAccept: new Headers({ ACCEPT: 1 }).has("accept") };
}
export function run(arg) {
  return {
    basics: basics(),
    fields: fields(),
    privates: privates(),
    privateCollisions: privateCollisions(),
    staticBlocks: staticBlocks(),
    inheritance: inheritance(),
    superProperty: superProperty(),
    derivedConstructors: derivedConstructors(),
    heritage: heritage(),
    newTarget: newTarget(),
    bindings: bindings(),
    sourceText: sourceText(),
    builtins: builtins(),
    pluginStyle: pluginStyle(arg),
  };
}
