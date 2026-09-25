package engine

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLazyFunctionPrototype checks that an ordinary function's `prototype`
// object is created on first access and is otherwise indistinguishable from
// an eager one: key order, attributes, MakeConstructor's `constructor` link,
// `new`, instanceof, assignment, defineProperty, dictionary mode, and the
// inline-cache paths through a function on a prototype chain.
func TestLazyFunctionPrototype(t *testing.T) {
	_, r, fn := callAllocs(t, `
		function F(x) { this.x = x; }
		const arrow = () => 1;
		const o = { m() { return 2; } };
		export function f(which) {
			switch (which) {
			case "keys": return Object.getOwnPropertyNames(F).join(",") + "|" + Object.getOwnPropertyNames(arrow).join(",") + "|" + Object.getOwnPropertyNames(o.m).join(",");
			case "F": return F;
			case "new": { const i = new F(3); return [i.x, i instanceof F, Object.getPrototypeOf(i) === F.prototype, F.prototype === F.prototype]; }
			case "assign": { function G() {} G.prototype = { tag: "g" }; return [new G().tag, Object.keys(G).length, Object.assign({}, G).prototype === undefined, "prototype" in G, G.hasOwnProperty("prototype")]; }
			case "define": { function H() {} Object.defineProperty(H, "prototype", { value: 5 }); return [H.prototype, Object.getPrototypeOf(new H()) === Object.prototype]; }
			case "chain": { function K() {} const child = Object.create(K); const seen = []; for (let i = 0; i < 3; i++) seen.push(typeof child.prototype); return seen.join(",") + "|" + (child.prototype === K.prototype); }
			case "dict": { function D() {} D.extra = 1; delete D.name; return [typeof D.prototype, D.prototype.constructor === D, Object.getOwnPropertyNames(D).join(",")]; }
			case "json": return JSON.stringify({ f: F, n: 1 });
			case "arrow": return [arrow.prototype, o.m.prototype];
			}
		}`, str("keys"))
	call := func(which string) string {
		v, err := r.Call(fn, Undefined(), []Value{str(which)})
		require.NoError(t, err, which)
		return jsonOf(t, r, v)
	}
	assert.Equal(t, `"length,name,prototype|length,name|length,name"`, call("keys"))
	fv, err := r.Call(fn, Undefined(), []Value{str("F")})
	require.NoError(t, err)
	fobj := fv.AsObject()
	// The descriptor is read through GetOwnProperty (which materializes).
	d, ok := fobj.GetOwnProperty(prototypeKey)
	require.True(t, ok)
	assert.True(t, d.Writable())
	assert.False(t, d.Enumerable())
	assert.False(t, d.Configurable())
	require.True(t, d.Value.IsObject())
	proto := d.Value.AsObject()
	cv, ok := proto.GetOwnDataValue(StringKey(AtomConstructor))
	require.True(t, ok)
	assert.Same(t, fobj, cv.AsObject())
	assert.Equal(t, []PropertyKey{StringKey(AtomConstructor)}, proto.OwnPropertyKeys())
	assert.Same(t, r.ObjectPrototype, proto.Proto())
	// Reading twice yields the same object.
	again, _ := fobj.GetProp(r, prototypeKey)
	assert.Same(t, proto, again.AsObject())
	assert.Equal(t, `[3,true,true,true]`, call("new"))
	assert.Equal(t, `["g",0,true,true,true]`, call("assign"))
	assert.Equal(t, `[5,true]`, call("define")) // a non-object prototype falls back to %Object.prototype%
	assert.Equal(t, `"object,object,object|true"`, call("chain"))
	assert.Equal(t, `["object",true,"length,prototype,extra"]`, call("dict"))
	assert.Equal(t, `"{\"n\":1}"`, call("json"))
	assert.Equal(t, `[null,null]`, call("arrow"))
}

// TestLazyFunctionPrototypeAllocation checks that creating a function
// expression no longer allocates its prototype object.
func TestLazyFunctionPrototypeAllocation(t *testing.T) {
	allocs, _, _ := callAllocs(t, `export function f() { return function inner() {}; }`)
	assert.Equal(t, 1.0, allocs, "the function object only")
}

// holeLeakModule is an audit reproducer: an ordinary object with the
// same keys and attributes as a function (Function.prototype root, then
// length, name, prototype) fills the inline cache of a property-read site;
// the next read on a real function must not take the cached slot path to the
// unmaterialized `prototype` slot and hand the internal hole marker to
// JavaScript.
const holeLeakModule = `
const o = Object.create(Function.prototype);
Object.defineProperty(o, "length", {value: 0, configurable: true});
Object.defineProperty(o, "name", {value: "", configurable: true});
Object.defineProperty(o, "prototype", {value: 1, writable: true});
function target() {}
function read(x) { return x.prototype; }
export function leak() { read(o); read(o); return read(target); }
export function describe() {
  const p = leak();
  return [typeof p, p === undefined, p === null, JSON.stringify({a: p})];
}
export function stringify() { return String(leak()); }
`

func TestLazyPrototypeHoleLeaksThroughIC(t *testing.T) {
	f := evalModule(t, holeLeakModule)
	require.Equal(t, []any{"object", false, false, `{"a":{}}`}, f.call("describe"))
}

// String() of a leaked marker used to be a fatal Go stack overflow, so the
// conversion runs in a child process; the child must exit cleanly with the
// spec result "[object Object]".
func TestLazyPrototypeHoleToString(t *testing.T) {
	if os.Getenv("MOEJS_HOLE_CHILD") == "1" {
		f := evalModule(t, holeLeakModule)
		require.Equal(t, "[object Object]", f.call("stringify"))
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestLazyPrototypeHoleToString$", "-test.v")
	cmd.Env = append(os.Environ(), "MOEJS_HOLE_CHILD=1")
	out, err := cmd.CombinedOutput()
	if len(out) > 1500 {
		out = out[:1500]
	}
	require.NoError(t, err, "child process failed:\n%s", out)
}

// TestLazyPrototypeReadPaths drives every read path that can observe the
// `prototype` slot of a function whose prototype was never touched: inline
// caches filled on same-shaped ordinary objects in either order, key and
// value enumeration (Object.keys/values/entries/assign, spread, for-in,
// JSON), computed access by own-property name, `in`/hasOwnProperty, reads
// through a prototype chain, frozen and re-prototyped functions, dictionary
// mode, store inline caches, defineProperty, new, instanceof and string
// conversion. isHole is the JavaScript-visible signature of the marker.
func TestLazyPrototypeReadPaths(t *testing.T) {
	f := evalModule(t, `
const isHole = v => typeof v === "undefined" && v !== undefined;
function fakeFunction() {
  const o = Object.create(Function.prototype);
  Object.defineProperty(o, "length", {value: 0, configurable: true});
  Object.defineProperty(o, "name", {value: "", configurable: true});
  Object.defineProperty(o, "prototype", {value: 1, writable: true});
  return o;
}
export function scan(which) {
  switch (which) {
  case "ic": {
    const o = fakeFunction();
    function target() {}
    function site(x) { return x.prototype; }
    site(o); site(o);
    const p = site(target);
    return [isHole(p), typeof p, p.constructor === target, site(o), site(target) === p];
  }
  case "ic-reverse": {
    const o = fakeFunction();
    function target() {}
    function other() {}
    function site(x) { return x.prototype; }
    const first = site(target);
    site(o); site(o);
    const p = site(other);
    return [isHole(first), isHole(p), p.constructor === other, first.constructor === target];
  }
  case "enumerate": {
    function F() {}
    const holes = [];
    for (const v of Object.values(F)) holes.push(isHole(v));
    for (const [, v] of Object.entries(F)) holes.push(isHole(v));
    for (const k in F) holes.push(isHole(F[k]));
    const assigned = Object.assign({}, F), spread = {...F};
    for (const k of Object.keys(assigned)) holes.push(isHole(assigned[k]));
    for (const k of Object.keys(spread)) holes.push(isHole(spread[k]));
    return [Object.keys(F).length, Object.values(F).length, Object.entries(F).length,
      Object.keys(assigned).length, Object.keys(spread).length, JSON.stringify(F), JSON.stringify({f: F, n: 1}),
      holes.some(Boolean)];
  }
  case "names": {
    function F() {}
    const names = Object.getOwnPropertyNames(F);
    return [names.join(","), names.some(n => isHole(F[n])), typeof F["prototype"], F["prototype"].constructor === F];
  }
  case "has": { function F() {} return ["prototype" in F, F.hasOwnProperty("prototype"), isHole(F.prototype)]; }
  case "chain": {
    function K() {}
    const c = Object.create(K);
    const seen = [];
    for (let i = 0; i < 3; i++) seen.push(isHole(c.prototype));
    return [seen.some(Boolean), c.prototype === K.prototype];
  }
  case "frozen": { function F() {} Object.freeze(F); return [isHole(F.prototype), typeof F.prototype, Object.isFrozen(F)]; }
  case "reproto": { function F() {} Object.setPrototypeOf(F, null); return [isHole(F.prototype), F.prototype.constructor === F]; }
  case "dict": { function F() {} F.a = 1; delete F.length; return [isHole(F.prototype), F.prototype.constructor === F]; }
  case "store-ic": {
    function set(f, v) { f.prototype = v; }
    function A() {} function B() {} function C() {}
    const pa = {};
    set(A, pa); set(B, pa);
    return [A.prototype === pa, B.prototype === pa, isHole(C.prototype), C.prototype.constructor === C];
  }
  case "define": {
    function F() {}
    Object.defineProperty(F, "prototype", {writable: false});
    return [isHole(F.prototype), typeof F.prototype, Object.getOwnPropertyNames(F).join(",")];
  }
  case "new": { function F() {} const i = new F(); return [Object.getPrototypeOf(i) === F.prototype, i instanceof F]; }
  case "instanceof": { function F() {} return [({}) instanceof F, Object.create(F.prototype) instanceof F]; }
  case "string": { function F() {} return String(F.prototype) + "|" + (F.prototype + "") + "|" + typeof F.prototype; }
  }
}`)
	want := map[string]string{
		"ic":         `[false,"object",true,1,true]`,
		"ic-reverse": `[false,false,true,true]`,
		"enumerate":  `[0,0,0,0,0,null,"{\"n\":1}",false]`,
		"names":      `["length,name,prototype",false,"object",true]`,
		"has":        `[true,true,false]`,
		"chain":      `[false,true]`,
		"frozen":     `[false,"object",true]`,
		"reproto":    `[false,true]`,
		"dict":       `[false,true]`,
		"store-ic":   `[true,true,false,true]`,
		"define":     `[false,"object","length,name,prototype"]`,
		"new":        `[true,true]`,
		"instanceof": `[false,true]`,
		"string":     `"[object Object]|[object Object]|object"`,
	}
	scan, _ := f.env.GetBindingValue("scan")
	for which, w := range want {
		v, err := f.r.Call(scan, Undefined(), []Value{str(which)})
		require.NoError(t, err, which)
		assert.Equal(t, w, jsonOf(t, f.r, v), which)
	}
}

// TestLazyPrototypeShapeIsDistinct checks the mechanism behind the fix: the
// lazy slot carries attrLazy, so a function's shape is never the shape of a
// same-keyed ordinary object, the inline cache refuses to cache the slot,
// the marker stays in the slot until the first read materializes it, and
// the attribute never reaches a descriptor.
func TestLazyPrototypeShapeIsDistinct(t *testing.T) {
	f := evalModule(t, `
export function F() {}
export const fake = (() => {
  const o = Object.create(Function.prototype);
  Object.defineProperty(o, "length", {value: 0, configurable: true});
  Object.defineProperty(o, "name", {value: "", configurable: true});
  Object.defineProperty(o, "prototype", {value: 1, writable: true});
  return o;
})();`)
	fv, _ := f.env.GetBindingValue("F")
	ov, _ := f.env.GetBindingValue("fake")
	fn, fake := fv.AsObject(), ov.AsObject()
	require.Equal(t, fn.OwnPropertyKeys(), fake.OwnPropertyKeys())
	assert.NotSame(t, fn.Shape(), fake.Shape())
	_, attrs, ok := fn.Shape().Lookup(prototypeKey)
	require.True(t, ok)
	assert.NotZero(t, attrs&attrLazy)
	_, attrs, ok = fake.Shape().Lookup(prototypeKey)
	require.True(t, ok)
	assert.Zero(t, attrs&attrLazy)

	var e ICEntry
	f.r.fillGetIC(&e, fn, prototypeKey)
	assert.Nil(t, e.Shape, "the inline cache must not cache a lazy slot")
	f.r.fillGetIC(&e, fake, prototypeKey)
	assert.Same(t, fake.Shape(), e.Shape, "an ordinary data slot is cacheable")
	f.r.fillGetIC(&e, fn, lengthKey)
	assert.Same(t, fn.Shape(), e.Shape, "the function's other slots are cacheable")

	assert.True(t, fn.Slot(2).IsHole(), "untouched: the slot holds the marker")
	d, ok := fn.GetOwnProperty(prototypeKey)
	require.True(t, ok)
	assert.Zero(t, d.Attrs()&attrLazy, "attrLazy never reaches a descriptor")
	assert.True(t, d.Writable())
	assert.False(t, fn.Slot(2).IsHole(), "the first read materialized the prototype")
	v, ok := fn.GetOwnDataValue(prototypeKey)
	require.True(t, ok)
	assert.Same(t, d.Value.AsObject(), v.AsObject())
	assert.Same(t, fn, f.r.ToGo(fv), "functions export as themselves")
}

// TestHoleConversionsAreInternalErrors is the belt-and-braces guard: a hole
// that does reach a conversion is an internal error, never a recursion.
func TestHoleConversionsAreInternalErrors(t *testing.T) {
	r := NewRealm()
	_, err := r.ToString(Hole())
	assert.ErrorIs(t, err, errHole)
	_, err = r.ToNumber(Hole())
	assert.ErrorIs(t, err, errHole)
	_, err = r.ToNumeric(Hole())
	assert.ErrorIs(t, err, errHole)
	_, err = r.ToPropertyKey(Hole())
	assert.ErrorIs(t, err, errHole)
	_, err = r.Add(Hole(), str("x"))
	assert.ErrorIs(t, err, errHole)
	assert.Equal(t, "<hole>", r.DisplayString(Hole()))
}
