package engine

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// classCases are class, super and new.target programs: each body runs in
// its own function; want is the JSON of its result or "throws " and the
// error.
var classCases = []struct{ name, body, want string }{
	{"basic", `class A { constructor(x) { this.x = x; } get y() { return this.x + 1; } m() { return this.y * 2; } static s() { return "s"; } }
		const a = new A(3); return [a.x, a.y, a.m(), A.s(), typeof A, A.name, A.length, a.constructor === A];`, `[3,4,8,"s","function","A",1,true]`},
	{"call without new", `class A {} return A();`, `throws TypeError: Class constructor A cannot be invoked without 'new'`},
	{"method not a constructor", `class A { m() {} } new (new A().m)();`, `throws TypeError: (intermediate value).m is not a constructor`},
	{"non-enumerable members", `class A { m() {} get g() { return 1; } static s() {} }
		return [Object.keys(A.prototype), Object.getOwnPropertyNames(A.prototype), Object.keys(A), Object.getOwnPropertyNames(A).sort()];`,
		`[[],["constructor","m","g"],[],["length","name","prototype","s"]]`},
	{"prototype attributes", `class A {} const d = Object.getOwnPropertyDescriptor(A, "prototype"); return [d.writable, d.enumerable, d.configurable];`, `[false,false,false]`},
	{"fields in order", `let i = 0; class A { a = ++i; b = this.a + 10; ["c" + i] = i; static s = ++i; #p = 5; getP() { return this.#p; } }
		const x = new A(); return [x.a, x.b, x.c0, A.s, x.getP(), Object.keys(x)];`, `[2,12,2,1,5,["a","b","c0"]]`},
	{"computed key order", `const log = []; class A { [(log.push(1), "a")]() {} static [(log.push(2), "b")] = log.push(4); [(log.push(3), "c")] = 0; } return log;`, `[1,2,3,4]`},
	{"field arrow this", `class A { v = 3; f = () => this.v; } const g = new A().f; return g();`, `3`},
	{"static this", `class A { static a = 1; static b = this.a + 1; static c = () => this.b; } return [A.b, A.c()];`, `[2,2]`},
	{"static blocks", `const log = []; class A { static x = 1; static { log.push(this.x, this === A); } static y = 2; static { const f = () => this; log.push(f() === A); } } return [log, A.y];`, `[[1,true,true],2]`},
	{"derived", `class A { constructor(v) { this.v = v; } who() { return "A" + this.v; } static sw() { return "SA"; } }
		class B extends A { constructor() { super(7); this.w = 1; } who() { return "B" + super.who(); } static sw() { return "SB" + super.sw(); } }
		const b = new B(); return [b.v, b.w, b.who(), B.sw(), b instanceof A, Object.getPrototypeOf(B) === A];`, `[7,1,"BA7","SBSA",true,true]`},
	{"derived fields", `class A { a = 1; } class B extends A { b = this.a + 1; constructor() { super(); this.c = this.b + 1; } } return new B();`, `{"a":1,"b":2,"c":3}`},
	{"default derived constructor", `class A { constructor(...a) { this.a = a; } } class B extends A {} return [new B(1, 2, 3).a, B.length];`, `[[1,2,3],0]`},
	{"super spread", `class A { constructor(...a) { this.a = a; } } class B extends A { constructor(...x) { super(0, ...x, 9); } } return new B(1, 2).a;`, `[0,1,2,9]`},
	{"this before super", `class A {} class B extends A { constructor() { this.x = 1; super(); } } new B();`,
		`throws ReferenceError: Must call super constructor in derived class before accessing 'this' or returning from derived constructor`},
	{"super twice", `class A {} class B extends A { constructor() { super(); super(); } } new B();`, `throws ReferenceError: Super constructor may only be called once`},
	{"no super", `class A {} class B extends A { constructor() {} } new B();`,
		`throws ReferenceError: Must call super constructor in derived class before accessing 'this' or returning from derived constructor`},
	{"return object without super", `class A {} class B extends A { constructor() { return { z: 1 }; } } return new B();`, `{"z":1}`},
	{"return undefined", `class A {} class B extends A { constructor() { super(); return undefined; } } return new B() instanceof B;`, `true`},
	{"return primitive", `class A {} class B extends A { constructor() { super(); return 1; } } new B();`, `throws TypeError: Derived constructors may only return object or undefined`},
	{"return check not catchable", `class A {} class B extends A { constructor() { super(); try { return 1; } catch (e) { return { caught: true }; } } } new B();`,
		`throws TypeError: Derived constructors may only return object or undefined`},
	{"return through finally", `class A {} class B extends A { constructor() { super(); try { return 1; } finally { this.x = 1; } } } new B();`,
		`throws TypeError: Derived constructors may only return object or undefined`},
	{"finally overrides return", `class A {} class B extends A { constructor() { super(); try { return 1; } finally { return { f: 2 }; } } } return new B();`, `{"f":2}`},
	{"arrow this before and after super", `class A {} class B extends A { constructor() { const f = () => this; try { f(); } catch (e) { super(); return { e: e.name, ok: f() === this }; } } } return new B();`,
		`{"e":"ReferenceError","ok":true}`},
	{"super call in arrow", `class A { constructor() { this.a = 1; } } class B extends A { constructor() { const f = () => super(); f(); this.b = 2; } } return new B();`, `{"a":1,"b":2}`},
	{"new.target", `function F() { return new.target === F; } class A { constructor() { this.nt = new.target.name; } } class B extends A {}
		return [F(), new F() instanceof F, new A().nt, new B().nt];`, `[false,true,"A","B"]`},
	{"new.target in arrow", `function F() { const g = () => new.target; return g() === undefined; } function G() { this.t = (() => new.target)() === G; } return [F(), new G().t];`, `[true,true]`},
	{"new.target in super chain", `class A { constructor() { this.t = new.target === B; } } class B extends A { constructor() { super(); } } return new B().t;`, `true`},
	{"private members", `class A { #x = 1; #m() { return this.#x + 1; } get #g() { return this.#x * 10; } set #g(v) { this.#x = v; }
		run() { this.#g = 4; return [this.#m(), this.#g, #x in this, #m in {}]; } static #sm() { return "sm"; } static callSm() { return A.#sm(); } }
		return [new A().run(), A.callSm()];`, `[[5,40,true,false],"sm"]`},
	{"private brand check", `class A { #m() {} static t(o) { return o.#m; } } return A.t({});`,
		`throws TypeError: Cannot read private member #m from an object whose class did not declare it`},
	{"private names per evaluation", `const cs = []; for (let i = 0; i < 2; i++) cs.push(class { #x = i; static get(o) { return o.#x; } });
		const a = new cs[0](); let err; try { cs[1].get(a); } catch (e) { err = e.name; } return [cs[0].get(a), err];`, `[0,"TypeError"]`},
	{"private static field", `class A { static #c = 0; static inc() { return ++A.#c; } } A.inc(); return A.inc();`, `2`},
	{"private static not inherited", `class A { static #c = 0; static get(o) { return o.#c; } } class B extends A {} return B.get(B);`,
		`throws TypeError: Cannot read private member #c from an object whose class did not declare it`},
	{"private in primitive", `class A { #x; static t(o) { return #x in o; } } return A.t(1);`, `throws TypeError: Cannot use 'in' operator to search for '#x' in 1`},
	{"private field twice", `class Base { constructor(o) { return o; } } class A extends Base { #x = 1; constructor(o) { super(o); } } const o = {}; new A(o); new A(o);`,
		`throws TypeError: Cannot initialize #x twice on the same object`},
	{"private getter only", `class A { get #s() { return 1; } m() { this.#s = 2; } } new A().m();`, `throws TypeError: '#s' was defined without a setter`},
	{"private setter only", `class A { set #s(v) {} m() { return this.#s; } } new A().m();`, `throws TypeError: '#s' was defined without a getter`},
	{"private method not writable", `class A { #m() {} w() { this.#m = 2; } } new A().w();`, `throws TypeError: Private method '#m' is not writable`},
	{"private invisible", `class A { #x = 1; } const a = new A(); return [Object.keys(a).length, Object.getOwnPropertyNames(a).length, JSON.stringify(a)];`, `[0,0,"{}"]`},
	{"private update and destructuring", `class A { #x = 1; m() { this.#x++; ++this.#x; this.#x += 10; this.#x ||= 0; [this.#x] = [this.#x + 1]; return this.#x; } } return new A().m();`, `14`},
	{"super property set", `class A {} A.prototype.x = 1; class B extends A { m() { super.y = 2; return [this.y, Object.hasOwn(this, "y"), super.x]; } } return new B().m();`, `[2,true,1]`},
	{"super compound", `const p = { n: 1 }; const o = { __proto__: p, m() { super.n += 5; super.n++; return [this.n, p.n]; } }; return o.m();`, `[2,1]`},
	{"super destructuring", `class A { m() { [super.x] = [5]; ({ a: super.y } = { a: 6 }); return [this.x, this.y]; } } return new A().m();`, `[5,6]`},
	{"super in arrow", `class A { m() { return "am"; } } class B extends A { m() { const f = () => super.m(); return f(); } } return new B().m();`, `"am"`},
	{"super in object getter", `const p = { get v() { return "pv:" + this.tag; } }; const o = { __proto__: p, tag: "o", get v() { return "ov/" + super.v; } }; return o.v;`, `"ov/pv:o"`},
	{"super in static block", `class A { static f() { return "af"; } } class B extends A { static { this.r = super.f(); } } return B.r;`, `"af"`},
	{"super in field initializers", `class A { m() { return "am:" + this.tag; } static s() { return "as"; } } class B extends A { tag = "b"; x = super.m(); y = () => super.m(); static z = super.s(); } const b = new B(); return [b.x, b.y(), B.z];`, `["am:b","am:b","as"]`},
	{"super chain", `class A { m() { return "A"; } } class B extends A {} class C extends B { m() { return "C" + super.m(); } } return new C().m();`, `"CA"`},
	{"delete super", `class A { m() { delete super.x; } } new A().m();`, `throws ReferenceError: Unsupported reference to 'super'`},
	{"names", `class A { ["x" + 1]() {} get g() {} set g(v) {} static get sg() {} static #p() {} static pn() { return A.#p.name; } }
		const d = Object.getOwnPropertyDescriptor(A.prototype, "g"), sd = Object.getOwnPropertyDescriptor(A, "sg"); const C = class {}; const o = { k: class {} };
		return [A.prototype.x1.name, d.get.name, d.set.name, sd.get.name, A.pn(), C.name, o.k.name, (class { static name = "custom"; }).name, (class {}).name];`,
		`["x1","get g","set g","get sg","#p","C","k","custom",""]`},
	{"computed key names", `const s = Symbol("t"), a = Symbol(), k = "kk"; class A { [s]() {} static [a]() {} [k]() {} } const o = { [a]() {} };
		return [A.prototype[s].name, A[a].name, A.prototype.kk.name, o[a].name];`, `["[t]","","kk",""]`},
	{"field function names", `const k = "kk"; class A { [k] = class {}; #q = class {}; f = function () {}; ar = () => {}; getQ() { return this.#q.name; } }
		const a = new A(); return [a.kk.name, a.getQ(), a.f.name, a.ar.name];`, `["kk","#q","f","ar"]`},
	{"lengths", `class A { constructor(a, b, c = 1) {} m(x, y) {} } return [A.length, A.prototype.m.length];`, `[2,2]`},
	{"inner name immutable", `class A { static m() { A = 1; } } A.m();`, `throws TypeError: Assignment to constant variable.`},
	{"outer name mutable", `class A {} A = 5; return A;`, `5`},
	{"class expression name", `const C = class Named { who() { return Named.name; } }; return [C.name, new C().who(), typeof Named];`, `["Named","Named","undefined"]`},
	{"heritage TDZ", `class A extends A {}`, `throws ReferenceError: Cannot access 'A' before initialization`},
	{"toString", `class A { m() {} static  async() {} get  [1 + 1]() {} } const C = class Foo extends Object { static x = 1; };
		return [String(A), String(A.prototype.m), String(A.async), String(Object.getOwnPropertyDescriptor(A.prototype, "2").get), String(C)];`,
		`["class A { m() {} static  async() {} get  [1 + 1]() {} }","m() {}","async() {}","get  [1 + 1]() {}","class Foo extends Object { static x = 1; }"]`},
	{"extends null", `class A extends null {} return [Object.getPrototypeOf(A.prototype), Object.getPrototypeOf(A) === Function.prototype];`, `[null,true]`},
	{"extends null construct", `class A extends null {} new A();`, `throws TypeError: Super constructor null is not a constructor`},
	{"extends null return", `class A extends null { constructor() { return Object.create(A.prototype); } } return new A() instanceof A;`, `true`},
	{"extends non-constructor", `class A extends 1 {}`, `throws TypeError: Class extends value 1 is not a constructor or null`},
	{"extends null prototype", `function F() {} F.prototype = null; class B extends F {} return Object.getPrototypeOf(B.prototype);`, `null`},
	{"extends bad prototype", `function F() {} F.prototype = 3; class B extends F {}`, `throws TypeError: Class extends value does not have valid prototype property 3`},
	{"extends call", `function mk() { return class { base() { return 1; } }; } class B extends mk() {} return new B().base();`, `1`},
	{"static prototype key", `const k = "prototype"; class A { static [k]() {} }`, `throws TypeError: Cannot redefine property: prototype`},
	{"class in default parameter", `function f(C = class { static v = 3; }) { return C.v; } return f();`, `3`},
	// Builtin subclassing (GetPrototypeFromConstructor).
	{"extends Array", `class A extends Array { sum() { return this.reduce((s, x) => s + x, 0); } } const a = new A(1, 2, 3); a.push(4); a[9] = 0;
		return [a instanceof A, Array.isArray(a), Object.getPrototypeOf(a) === A.prototype, a.length, a.sum(), new A(3).length, JSON.stringify(a.slice(0, 2))];`,
		`[true,true,true,10,10,3,"[1,2]"]`},
	{"extends Array length", `class A extends Array {} new A(-1);`, `throws RangeError: Invalid array length`},
	{"extends Error", `class E extends TypeError { constructor(m) { super(m); this.name = "E"; } } const e = new E("boom");
		return [e instanceof E, e instanceof TypeError, e instanceof Error, e.message, String(e), Object.prototype.hasOwnProperty.call(e, "message")];`,
		`[true,true,true,"boom","E: boom",true]`},
	{"extends Error default", `class E extends Error {} const e = new E("m", { cause: 1 }); return [e instanceof E, e.name, e.message, e.cause, String(e)];`, `[true,"Error","m",1,"Error: m"]`},
	{"extends RegExp", `class R extends RegExp { exec(s) { this.calls = (this.calls || 0) + 1; return super.exec(s); } } const r = new R("a+", "g");
		return [r instanceof R, r instanceof RegExp, r.source, r.flags, r.exec("caab")[0], r.lastIndex, r.calls];`,
		`[true,true,"a+","g","aa",3,1]`},
	{"extends RegExp order", `const log = []; class R extends RegExp {} const p = { toString() { log.push("pattern"); return "x"; } };
		const r = new R(p); return [log, r.source, Object.getPrototypeOf(r) === R.prototype];`, `[["pattern"],"x",true]`},
	{"extends Date", `class D extends Date { year() { return this.getTime() === 0 ? "epoch" : "other"; } } const d = new D(0);
		return [d instanceof D, d instanceof Date, d.year(), d.toISOString()];`, `[true,true,"epoch","1970-01-01T00:00:00.000Z"]`},
	{"extends Object", `class O extends Object { constructor() { super(); this.x = 1; } } const o = new O(); return [o instanceof O, Object.getPrototypeOf(O.prototype) === Object.prototype, o.x];`, `[true,true,1]`},
	{"extends primitives", `class B extends Boolean {} class N extends Number { twice() { return this * 2; } } class S extends String { first() { return this[0]; } }
		const b = new B(false), n = new N(21), s = new S("hi");
		return [b instanceof B, b.valueOf(), n.twice(), n + 1, s.length, s.first(), s instanceof S, String(s)];`,
		`[true,false,42,22,2,"h",true,"hi"]`},
	{"extends Function", `class F extends Function {} new F();`, `throws TypeError: new Function is not supported yet (see TODO.md)`},
	{"extends Array default ctor", `class A extends Array { constructor(...a) { super(...a); this.tag = "t"; } } const a = new A(5, 6); return [a.length, a.tag, a[1]];`, `[2,"t",6]`},
}

func TestClasses(t *testing.T) {
	var src strings.Builder
	src.WriteString("export const out = {};\n")
	for _, c := range classCases {
		src.WriteString("try { out[" + strconv.Quote(c.name) + "] = JSON.stringify((() => {\n" + c.body + "\n})()); } catch (e) { out[" + strconv.Quote(c.name) + "] = \"throws \" + String(e); }\n")
	}
	f := evalModule(t, src.String())
	out := f.export("out").(map[string]any)
	for _, c := range classCases {
		assert.Equal(t, c.want, out[c.name], c.name)
	}
}

// TestGetPrototypeFromConstructor checks when builtin constructors read
// newTarget.prototype: never for the builtin itself, and before (Array,
// Error, RegExp) or after (Date) coercing their arguments, as the spec
// orders it.
func TestGetPrototypeFromConstructor(t *testing.T) {
	r := NewRealm()
	fail := func(msg string) *Object {
		return r.NewNativeFunction(AtomEmpty, 0, func(r *Realm, this Value, args []Value) (Value, error) {
			return Undefined(), r.Throw(StringValue(r.InternGoString(msg)))
		})
	}
	newTarget := r.NewNativeFunction(AtomEmpty, 0, nil)
	require.NoError(t, newTarget.DefinePropertyOrThrow(r, StringKey(AtomPrototype), AccessorDescriptor(fail("prototype"), nil, attrConfigurable)))
	arg := r.NewObject()
	require.NoError(t, arg.DefinePropertyOrThrow(r, StringKey(AtomToString), DataDescriptor(ObjectValue(fail("argument")), attrConfigurable)))

	for _, tt := range []struct {
		ctor *Object
		args []Value
		want string
	}{
		{r.ArrayCtor, []Value{IntValue(-1)}, "prototype"},
		{r.errorCtors[KindTypeError], []Value{ObjectValue(arg)}, "prototype"},
		{r.RegExpCtor, []Value{ObjectValue(arg)}, "prototype"},
		{r.DateCtor, []Value{ObjectValue(arg)}, "argument"},
	} {
		_, err := r.Construct(ObjectValue(tt.ctor), tt.args, newTarget)
		assert.EqualError(t, err, tt.want, r.DisplayString(ObjectValue(tt.ctor)))
	}

	proto, err := r.GetPrototypeFromConstructor(r.ArrayCtor, r.ArrayCtor, r.ArrayPrototype)
	require.NoError(t, err)
	assert.Same(t, r.ArrayPrototype, proto)
	plain := r.NewNativeFunction(AtomEmpty, 0, nil)
	require.NoError(t, plain.DefinePropertyOrThrow(r, StringKey(AtomPrototype), DataDescriptor(IntValue(1), attrConfigurable)))
	proto, err = r.GetPrototypeFromConstructor(plain, r.ArrayCtor, r.ArrayPrototype)
	require.NoError(t, err)
	assert.Same(t, r.ArrayPrototype, proto, "a non-object prototype falls back to the intrinsic")
}
