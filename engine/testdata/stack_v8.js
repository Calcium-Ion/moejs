const o = { get g() { return new Error("g"); }, set s(v) { this.e = new Error("s"); }, m() { return new Error("m"); } };
export function getter() { return o.g.stack; }
export function setter() { o.s = 1; return o.e.stack; }
export function method() { return o.m().stack; }
export function forEachArrow() { let e; [1].forEach(() => { e = new Error("c"); }); return e.stack; }
export function forEachNamed() { let e; [1].forEach(function cb() { e = new Error("c"); }); return e.stack; }
export function forEachThisArg() { let e; [1].forEach(function cb() { e = new Error("c"); }, {}); return e.stack; }
export function assigned() { const obj = {}; obj.prop = function () { return new Error("p"); }; return obj.prop().stack; }
export function nested() { const obj = { x: { y: function () { return new Error("p"); } } }; return obj.x.y().stack; }
function inner() { return new Error("i"); }
function outer() { return inner(); }
export function two() { return outer().stack; }
export function bound() { const b = function bf() { return new Error("b"); }.bind({}); return b().stack; }
export function boundNull() { const b = function bf() { return new Error("b"); }.bind(null); return b().stack; }
export function callObj() { return function cf() { return new Error("b"); }.call({ a: 1 }).stack; }
export function callArr() { return function cf() { return new Error("b"); }.call([]).stack; }
export function callFn() { return function cf() { return new Error("b"); }.call(function Q() {}).stack; }
export function callAnonFn() { return function cf() { return new Error("b"); }.call(function () {}).stack; }
export function applyObj() { return function af() { return new Error("b"); }.apply({}, []).stack; }
export function callNum() { return function nr() { return new Error("u"); }.call(5).stack; }
export function callStr() { return function sr() { return new Error("u"); }.call("s").stack; }
export function callBool() { return function br() { return new Error("u"); }.call(true).stack; }
export function callUndef() { return function () { return new Error("u"); }.call(undefined).stack; }
function F() { this.e = new Error("F"); }
export function newF() { return new F().e.stack; }
export function newAnon() { return new (function () { this.e = new Error("x"); })().e.stack; }
export function protoMethod() { const proto = { pm() { return new Error("pm"); } }; return Object.create(proto).pm().stack; }
const al = { q() { return new Error("q"); } };
export function dupNames() { al.r = al.q; return al.r().stack; }
export function aliased() { const al2 = {}; al2.w = al.q; return al2.w().stack; }
export function reviver() { let e; JSON.parse("[1]", function rv() { e = e || new Error("rv"); }); return e.stack; }
export function errorCall() { return (function ec() { return Error("x"); })().stack; }
export function errorDotCall() { return (function ed() { return Error.call(undefined, "x"); })().stack; }
export function nullProto() { const n = Object.create(null); n.f = function f() { return new Error("np"); }; return n.f().stack; }
export function jsonParse() { try { JSON.parse("{"); } catch (e) { return e.stack; } }
export function objectKeys() { try { Object.keys(null); } catch (e) { return e.stack; } }
export function nullRead() { try { null.x; } catch (e) { return e.stack; } }
export function renamed() { function r() { return new Error("r"); } Object.defineProperty(r, "name", { value: "zz" }); return r().stack; }
export function globalFn() { globalThis.gf = function gf() { return new Error("g"); }; return globalThis.gf().stack; }
export function stringConv() { let e; String({ toString() { e = new Error("ts"); return ""; } }); return e.stack; }
export function arrayFrom() { let e; Array.from([1], function fromcb() { e = new Error("fr"); }); return e.stack; }
export function mathMethod() { const m = Object.create(Math); m.f = function f() { return new Error("mm"); }; return m.f().stack; }
export function jsonMethod() { const j = Object.create(JSON); j.f = function f() { return new Error("jm"); }; return j.f().stack; }
export function fnReceiverNamed() { function Holder() {} Holder.make = function make() { return new Error("h"); }; return Holder.make().stack; }
export function arrayIter() { const it = [1].values(); it.f = function f() { return new Error("ai"); }; return it.f().stack; }
export function customCtor() { function Point() {} Point.prototype.at = function at() { return new Error("pt"); }; return new Point().at().stack; }
export function changedCtor() { function P() {} P.prototype.constructor = function Other() {}; P.prototype.m = function m() { return new Error("c"); }; return new P().m().stack; }
export function nonIdentName() { const x = { "a-b": function () { return new Error("nb"); } }; return x["a-b"]().stack; }
export function asMethodSuffix() { const x = { f: function g() { return new Error("s"); } }; return x.f().stack; }
class P1 { constructor() { this.e = new Error("p"); } }
export function classSuper() { class C1 extends P1 { constructor() { super(); } } return new C1().e.stack; }
export function classSuperSpread() { class C2 extends P1 { constructor(...a) { super(...a); } } return new C2(1).e.stack; }
export function classDefaultDerived() { class C3 extends P1 {} return new C3().e.stack; }
export function classStatic() { class Q1 { static s() { return new Error("s"); } } return Q1.s().stack; }
export function classInherited() { class Q2 { m() { return new Error("m"); } } class R2 extends Q2 {} return new R2().m().stack; }
export function classMethod() { class Q3 { m() { return new Error("m"); } } return new Q3().m().stack; }
export function classGetter() { class G1 { get g() { return new Error("g"); } } return new G1().g.stack; }
export function classStaticGetter() { class G2 { static get g() { return new Error("g"); } } return G2.g.stack; }
export function classPrivate() { class P2 { #p() { return new Error("pp"); } m() { return this.#p(); } } return new P2().m().stack; }
export function classErrorSub() { class E1 extends Error {} function mk() { return new E1("x"); } return mk().stack; }
export function classErrorSubCtor() { class E2 extends Error { constructor() { super("y"); } } function mk() { return new E2(); } return mk().stack; }
export function classNoNew() { try { P1(); } catch (e) { return e.stack; } }
export function classInit() { class I1 { e = new Error("i"); } return new I1().e.stack; }
export function classInitDerived() { class I2 extends P1 { f = new Error("i2"); } return new I2().f.stack; }
export function classStaticInit() { class S1 { static e = new Error("si"); } return S1.e.stack; }
export function classStaticBlock() { class S2 { static { S2.e = new Error("sb"); } } return S2.e.stack; }
export function classCtorThrow() { class T1 { constructor() { this.e = new Error("t"); } } return new T1().e.stack; }
export function classExprAnon() { const K = class { m() { return new Error("k"); } }; return new K().m().stack; }
export function classNewTargetFn() { class N1 { constructor() { this.e = new Error("n"); } } return Reflect.construct(N1, []).e.stack; }
export function mapClass() { class M { constructor() { throw new Error("mc"); } } try { [1].map(M); } catch (e) { return e.stack; } }
export function mapClassNoNew() { class M2 {} try { [1].map(M2); } catch (e) { return e.stack; } }
export function superNative() { class SN extends Array { constructor() { super(); this.e = new Error("sn"); } } return new SN().e.stack; }
export function reflApply() { return Reflect.apply(function ra() { return new Error("ra"); }, {}, []).stack; }
export function reflApplyNull() { return Reflect.apply(function ra() { return new Error("ra"); }, undefined, []).stack; }
export function reflApplyBad() { try { Reflect.apply(1, null, []); } catch (e) { return e.stack.replace(e.message, "bad"); } }
export function reflConstruct() { function RC() { this.e = new Error("rc"); } return Reflect.construct(RC, []).e.stack; }
export function reflConstructNT() { function RC() { this.e = new Error("rc"); } function NT() {} return Reflect.construct(RC, [], NT).e.stack; }
export function reflConstructNative() { class X extends Error {} function mk() { return Reflect.construct(Error, ["n"], X); } return mk().stack; }
export function reflConstructBad() { try { Reflect.construct(() => 1, []); } catch (e) { return e.stack.replace(e.message, "bad"); } }
export function reflConstructArgs() { function RC2() { this.e = new Error("rc"); } try { return Reflect.construct(RC2, 1).e.stack; } catch (e) { return e.stack; } }
