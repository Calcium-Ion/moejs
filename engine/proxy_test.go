package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// proxyLog prefixes a case body with logged(target): a proxy whose handler
// is itself a proxy, so every trap lookup is logged (with the key for the
// keyed traps) and then forwarded to Reflect.
const proxyLog = `const log = [];
const keyed = new Set(['get', 'set', 'has', 'deleteProperty', 'getOwnPropertyDescriptor', 'defineProperty']);
function logged(target) {
	return new Proxy(target, new Proxy({}, {get(_, trap) {
		return (...args) => { log.push(keyed.has(trap) ? trap + ' ' + String(args[1]) : trap); return Reflect[trap](...args); };
	}}));
}
`

func TestProxyTrapOrder(t *testing.T) {
	cases := []protoCase{
		{"get set has delete", `const p = logged({a: 1}); p.a; p.b = 2; 'a' in p; delete p.a; return log.join()`,
			"get a,set b,getOwnPropertyDescriptor b,defineProperty b,has a,deleteProperty a"},
		{"Object.keys", `const p = logged({a: 1, b: 2}); return Object.keys(p).join() + ';' + log.join()`,
			"a,b;ownKeys,getOwnPropertyDescriptor a,getOwnPropertyDescriptor b"},
		{"Object.values", `const p = logged({a: 1, b: 2}); return Object.values(p).join() + ';' + log.join()`,
			"1,2;ownKeys,getOwnPropertyDescriptor a,get a,getOwnPropertyDescriptor b,get b"},
		{"Object.entries", `const p = logged({a: 1}); return Object.entries(p).join() + ';' + log.join()`,
			"a,1;ownKeys,getOwnPropertyDescriptor a,get a"},
		{"Object.assign", `const p = logged({a: 1, b: 2}); const o = Object.assign({}, p); return o.a + o.b + ';' + log.join()`,
			"3;ownKeys,getOwnPropertyDescriptor a,get a,getOwnPropertyDescriptor b,get b"},
		{"Object.assign to a proxy", `const p = logged({}); Object.assign(p, {a: 1}); return log.join()`,
			"set a,getOwnPropertyDescriptor a,defineProperty a"},
		{"object spread", `const p = logged({a: 1, b: 2}); const {a, ...rest} = p; const o = {...p}; return [a, rest.b, o.a + o.b].join() + ';' + log.join()`,
			"1,2,3;get a,ownKeys,getOwnPropertyDescriptor b,get b,ownKeys,getOwnPropertyDescriptor a,get a,getOwnPropertyDescriptor b,get b"},
		{"JSON.stringify", `const p = logged({a: 1, b: 2}); return JSON.stringify(p) + ';' + log.join()`,
			`{"a":1,"b":2};get toJSON,ownKeys,getOwnPropertyDescriptor a,getOwnPropertyDescriptor b,get a,get b`},
		{"instanceof", `const p = logged({}); return (p instanceof Object) + ';' + log.join()`,
			"true;getPrototypeOf"},
		{"freeze", `const p = logged({a: 1}); Object.freeze(p); return log.join()`,
			"preventExtensions,ownKeys,getOwnPropertyDescriptor a,defineProperty a"},
		{"isFrozen", `const p = logged(Object.freeze({a: 1})); return Object.isFrozen(p) + ';' + log.join()`,
			"true;isExtensible,ownKeys,getOwnPropertyDescriptor a"},
		{"seal", `const p = logged({a: 1}); Object.seal(p); return Object.isSealed(p) + ';' + log.join()`,
			"true;preventExtensions,ownKeys,defineProperty a,isExtensible,ownKeys,getOwnPropertyDescriptor a"},
		{"prototype and extensibility", `const p = logged({}); Object.getPrototypeOf(p); Object.setPrototypeOf(p, null); Object.isExtensible(p); Object.preventExtensions(p); return log.join()`,
			"getPrototypeOf,setPrototypeOf,isExtensible,preventExtensions"},
		{"own property queries", `const p = logged({a: 1}); Object.getOwnPropertyNames(p); Object.getOwnPropertyDescriptors(p); Object.hasOwn(p, 'a'); ({}).propertyIsEnumerable.call(p, 'a'); Object.defineProperty(p, 'x', {value: 1}); return log.join()`,
			"ownKeys,ownKeys,getOwnPropertyDescriptor a,getOwnPropertyDescriptor a,getOwnPropertyDescriptor a,defineProperty x"},
		{"call and construct", `const f = logged(function (a) { return a + 1 }); const r = [f(1), typeof new f(2), f.call(null, 3)]; return r.join() + ';' + log.join()`,
			"2,object,4;apply,construct,get prototype,get call,apply"},
		{"array push", `const a = logged([1, 2]); a.push(3); return log.join()`,
			"get push,get length,set 2,getOwnPropertyDescriptor 2,defineProperty 2,set length,getOwnPropertyDescriptor length,defineProperty length"},
		{"array spread", `const a = logged([1, 2]); return [...a].join() + ';' + log.join()`,
			"1,2;get Symbol(Symbol.iterator),get length,get 0,get length,get 1,get length"},
		{"ToPrimitive", `const p = logged({}); return String(p) + ';' + log.join()`,
			"[object Object];get Symbol(Symbol.toPrimitive),get toString,get Symbol(Symbol.toStringTag)"},
		{"proxy as prototype", `const c = Object.create(logged({})); c.x; c.y = 1; 'z' in c; return Object.keys(c).join() + ';' + log.join()`,
			"y;get x,set y,has z"},
		{"no traps", `const p = logged([]); const f = logged(() => 1); return [typeof p, typeof f, Array.isArray(p), p === p, Object(p) === p].join() + ';' + log.length`,
			"object,function,true,true,true;0"},
		{"handler is looked up per operation", `const h = {}; const p = new Proxy({a: 1}, h); const r = [p.a]; h.get = () => 2; r.push(p.a); delete h.get; r.push(p.a); return r.join()`,
			"1,2,1"},
		{"receiver", `const t = {get self() { return this }}; const p = new Proxy(t, {}); const c = Object.create(p); return [p.self === p, c.self === c, Reflect.get(p, 'self', 1) === 1].join()`,
			"true,true,true"},
		{"trap arguments", `let got; const t = {}; const h = {set(...a) { got = a; return true }}; const p = new Proxy(t, h); p.k = 1; return [got.length, got[0] === t, got[1], got[2], got[3] === p, this === undefined].join()`,
			"4,true,k,1,true,true"},
		{"trap this is the handler", `let self; const h = {get() { self = this }}; new Proxy({}, h).x; return self === h`, "true"},
	}
	for i := range cases {
		cases[i].body = proxyLog + cases[i].body
	}
	runProtoCases(t, cases)
}

func TestProxyConstructor(t *testing.T) {
	runProtoCases(t, []protoCase{
		{"shape", `return [Proxy.length, Proxy.name, 'prototype' in Proxy, typeof Proxy, Proxy.revocable.length, Proxy.revocable.name, Object.getOwnPropertyNames(Proxy).sort().join('|')].join()`,
			"2,Proxy,false,function,2,revocable,length|name|revocable"},
		{"call without new", `return Proxy({}, {})`, "!TypeError: Constructor Proxy requires 'new'"},
		{"non-object target", `return new Proxy(1, {})`, "!TypeError: Cannot create proxy with a non-object as target or handler"},
		{"non-object handler", `return new Proxy({}, null)`, "!TypeError: Cannot create proxy with a non-object as target or handler"},
		{"revocable shape", `const r = Proxy.revocable({}, {}); return [Object.keys(r).join('|'), typeof r.revoke, r.revoke.length, r.revoke.name === '', 'prototype' in r.revoke, Object.getPrototypeOf(r) === Object.prototype].join()`,
			"proxy|revoke,function,0,true,false,true"},
		{"revoked target and handler", `const r = Proxy.revocable({}, {}); r.revoke(); const p = new Proxy(r.proxy, r.proxy); return typeof p`, "object"},
		{"prototype of a proxy", `return [Object.getPrototypeOf(new Proxy([], {})) === Array.prototype, Object.getPrototypeOf(new Proxy(Object.create(null), {}))].join()`, "true,"},
		{"extends Proxy", `class P extends Proxy {}`, "!TypeError: Class extends value"},
	})
}

func TestProxyRevoked(t *testing.T) {
	const revoked = `const {proxy: p, revoke} = Proxy.revocable({a: 1}, {}); const {proxy: f, revoke: revokeF} = Proxy.revocable(function () {}, {}); revoke(); revokeF(); `
	cases := []protoCase{
		{"revoke twice", `revoke(); return [revoke(), typeof p, typeof f].join()`, ",object,function"},
		{"get", `return p.a`, "!TypeError: Cannot perform 'get' on a proxy that has been revoked"},
		{"set", `p.a = 1`, "!TypeError: Cannot perform 'set' on a proxy that has been revoked"},
		{"has", `return 'a' in p`, "!TypeError: Cannot perform 'has' on a proxy that has been revoked"},
		{"delete", `return delete p.a`, "!TypeError: Cannot perform 'deleteProperty' on a proxy that has been revoked"},
		{"ownKeys", `return Object.keys(p)`, "!TypeError: Cannot perform 'ownKeys' on a proxy that has been revoked"},
		{"getOwnPropertyDescriptor", `return Object.getOwnPropertyDescriptor(p, 'a')`, "!TypeError: Cannot perform 'getOwnPropertyDescriptor' on a proxy that has been revoked"},
		{"defineProperty", `return Object.defineProperty(p, 'a', {})`, "!TypeError: Cannot perform 'defineProperty' on a proxy that has been revoked"},
		{"getPrototypeOf", `return Object.getPrototypeOf(p)`, "!TypeError: Cannot perform 'getPrototypeOf' on a proxy that has been revoked"},
		{"setPrototypeOf", `return Object.setPrototypeOf(p, null)`, "!TypeError: Cannot perform 'setPrototypeOf' on a proxy that has been revoked"},
		{"isExtensible", `return Object.isExtensible(p)`, "!TypeError: Cannot perform 'isExtensible' on a proxy that has been revoked"},
		{"preventExtensions", `return Object.preventExtensions(p)`, "!TypeError: Cannot perform 'preventExtensions' on a proxy that has been revoked"},
		{"apply", `return f()`, "!TypeError: Cannot perform 'apply' on a proxy that has been revoked"},
		{"construct", `return new f()`, "!TypeError: Cannot perform 'construct' on a proxy that has been revoked"},
		{"IsArray", `return Array.isArray(p)`, "!TypeError: Cannot perform 'IsArray' on a proxy that has been revoked"},
		{"toString", `return Object.prototype.toString.call(p)`, "!TypeError: Cannot perform 'IsArray' on a proxy that has been revoked"},
		{"JSON", `return JSON.stringify(p)`, "!TypeError: Cannot perform 'get' on a proxy that has been revoked"},
		{"instanceof", `return p instanceof Object`, "!TypeError: Cannot perform 'getPrototypeOf' on a proxy that has been revoked"},
		{"on the prototype chain", `return Object.create(p).a`, "!TypeError: Cannot perform 'get' on a proxy that has been revoked"},
		{"Function.prototype.toString", `return Function.prototype.toString.call(f)`, "function () { [native code] }"},
		{"no trap runs before the check", `let n = 0; const r = Proxy.revocable({}, new Proxy({}, {get() { n++ }})); r.revoke(); try { r.proxy.x } catch {} return n`, "0"},
		{"revoked inside a trap", `let rv; const r = Proxy.revocable({a: 1}, {get(t, k) { rv(); return Reflect.get(t, k) }}); rv = r.revoke; return [r.proxy.a, typeof r.proxy].join()`, "1,object"},
		// GetPrototypeFromConstructor falls back to GetFunctionRealm(newTarget)
		// when newTarget.prototype is not an object.
		{"GetFunctionRealm", `const r = Proxy.revocable(function () {}, {get() { r.revoke() }}); return new r.proxy()`, "!TypeError: Cannot perform 'GetFunctionRealm' on a proxy that has been revoked"},
		{"GetFunctionRealm of a builtin", `const r = Proxy.revocable(function () {}, {get() { r.revoke() }}); return Reflect.construct(Array, [], r.proxy)`, "!TypeError: Cannot perform 'GetFunctionRealm' on a proxy that has been revoked"},
		// The get trap's invariant check reads the revoked target first.
		{"GetFunctionRealm through proxies", `const r = Proxy.revocable(function () {}, {}); const q = new Proxy(r.proxy, {get() { r.revoke() }}); return new q()`, "!TypeError: Cannot perform 'getOwnPropertyDescriptor' on a proxy that has been revoked"},
		{"GetFunctionRealm not revoked", `return Object.getPrototypeOf(Reflect.construct(Array, [], new Proxy(function () {}, {get() {}}))) === Array.prototype`, "true"},
	}
	for i := range cases {
		cases[i].body = revoked + cases[i].body
	}
	runProtoCases(t, cases)
}

func TestProxyInvariants(t *testing.T) {
	const nonExt = `const ne = Object.preventExtensions({a: 1}); `
	runProtoCases(t, []protoCase{
		// getPrototypeOf
		{"getPrototypeOf non-object", `return Object.getPrototypeOf(new Proxy({}, {getPrototypeOf() { return 1 }}))`,
			"!TypeError: 'getPrototypeOf' on proxy: trap returned neither object nor null"},
		{"getPrototypeOf non-extensible", nonExt + `return Object.getPrototypeOf(new Proxy(ne, {getPrototypeOf() { return null }}))`,
			"!TypeError: 'getPrototypeOf' on proxy: proxy target is non-extensible but the trap did not return its actual prototype"},
		{"getPrototypeOf allowed", nonExt + `return [Object.getPrototypeOf(new Proxy(ne, {getPrototypeOf() { return Object.prototype }})) === Object.prototype, Object.getPrototypeOf(new Proxy({}, {getPrototypeOf() { return Array.prototype }})) === Array.prototype].join()`,
			"true,true"},
		// setPrototypeOf
		{"setPrototypeOf non-extensible", nonExt + `return Reflect.setPrototypeOf(new Proxy(ne, {setPrototypeOf() { return true }}), null)`,
			"!TypeError: 'setPrototypeOf' on proxy: trap returned truish for setting a new prototype on the non-extensible proxy target"},
		{"setPrototypeOf falsish", `return [Reflect.setPrototypeOf(new Proxy({}, {setPrototypeOf() { return 0 }}), null), Object.setPrototypeOf(new Proxy({}, {setPrototypeOf() { return 0 }}), null)].join()`,
			"!TypeError: 'setPrototypeOf' on proxy: trap returned falsish"},
		{"__proto__ setter falsish", `new Proxy({}, {setPrototypeOf() { return false }}).__proto__ = null`,
			"!TypeError: 'setPrototypeOf' on proxy: trap returned falsish"},
		{"setPrototypeOf same", nonExt + `return Reflect.setPrototypeOf(new Proxy(ne, {setPrototypeOf() { return true }}), Object.prototype)`, "true"},
		// isExtensible
		{"isExtensible mismatch", `return Object.isExtensible(new Proxy({}, {isExtensible() { return false }}))`,
			"!TypeError: 'isExtensible' on proxy: trap result does not reflect extensibility of proxy target (which is 'true')"},
		// preventExtensions
		{"preventExtensions truish", `return Reflect.preventExtensions(new Proxy({}, {preventExtensions() { return true }}))`,
			"!TypeError: 'preventExtensions' on proxy: trap returned truish but the proxy target is extensible"},
		{"preventExtensions falsish", `return [Reflect.preventExtensions(new Proxy({}, {preventExtensions() { return false }})), Object.preventExtensions(new Proxy({}, {preventExtensions() { return false }}))].join()`,
			"!TypeError: 'preventExtensions' on proxy: trap returned falsish"},
		{"freeze falsish", `return Object.freeze(new Proxy({}, {preventExtensions() { return false }}))`,
			"!TypeError: 'preventExtensions' on proxy: trap returned falsish"},
		// getOwnPropertyDescriptor
		{"gOPD non-object", `return Object.getOwnPropertyDescriptor(new Proxy({}, {getOwnPropertyDescriptor() { return 1 }}), 'x')`,
			"!TypeError: 'getOwnPropertyDescriptor' on proxy: trap returned neither object nor undefined for property 'x'"},
		{"gOPD hides non-configurable", `return Object.getOwnPropertyDescriptor(new Proxy(Object.defineProperty({}, 'x', {value: 1}), {getOwnPropertyDescriptor() {}}), 'x')`,
			"!TypeError: 'getOwnPropertyDescriptor' on proxy: trap returned undefined for property 'x' which is non-configurable in the proxy target"},
		{"gOPD hides on non-extensible", nonExt + `return Object.getOwnPropertyDescriptor(new Proxy(ne, {getOwnPropertyDescriptor() {}}), 'a')`,
			"!TypeError: 'getOwnPropertyDescriptor' on proxy: trap returned undefined for property 'a' which exists in the non-extensible proxy target"},
		{"gOPD incompatible", `return Object.getOwnPropertyDescriptor(new Proxy(Object.defineProperty({}, 'x', {value: 1}), {getOwnPropertyDescriptor() { return {value: 2, configurable: false} }}), 'x')`,
			"!TypeError: 'getOwnPropertyDescriptor' on proxy: trap returned descriptor for property 'x' that is incompatible with the existing property in the proxy target"},
		{"gOPD new on non-extensible", nonExt + `return Object.getOwnPropertyDescriptor(new Proxy(ne, {getOwnPropertyDescriptor() { return {value: 1, configurable: true} }}), 'b')`,
			"!TypeError: 'getOwnPropertyDescriptor' on proxy: trap returned descriptor for property 'b' that is incompatible with the existing property in the proxy target"},
		{"gOPD non-configurable for configurable", `return Object.getOwnPropertyDescriptor(new Proxy({x: 1}, {getOwnPropertyDescriptor() { return {value: 1, configurable: false} }}), 'x')`,
			"!TypeError: 'getOwnPropertyDescriptor' on proxy: trap reported non-configurability for property 'x' which is either non-existent or configurable in the proxy target"},
		{"gOPD non-configurable writable", `return Object.getOwnPropertyDescriptor(new Proxy(Object.defineProperty({}, 'x', {value: 1, writable: true}), {getOwnPropertyDescriptor() { return {value: 1, writable: false, configurable: false} }}), 'x')`,
			"!TypeError: 'getOwnPropertyDescriptor' on proxy: trap reported non-configurable and writable for property 'x' which is non-configurable, non-writable in the proxy target"},
		{"gOPD completes the descriptor", `const d = Object.getOwnPropertyDescriptor(new Proxy({}, {getOwnPropertyDescriptor() { return {get: undefined, enumerable: 1, configurable: true} }}), 'x'); return [Object.keys(d).join('|'), d.enumerable, d.set].join()`,
			"get|set|enumerable|configurable,true,"},
		{"gOPD accessor object", `return Object.getOwnPropertyDescriptor(new Proxy({}, {getOwnPropertyDescriptor() { return {get: 1, configurable: true} }}), 'x')`,
			"!TypeError"},
		// defineProperty
		{"defineProperty falsish", `Object.defineProperty(new Proxy({}, {defineProperty() { return false }}), 'x', {value: 1})`,
			"!TypeError: 'defineProperty' on proxy: trap returned falsish for property 'x'"},
		{"defineProperty falsish Reflect", `return Reflect.defineProperty(new Proxy({}, {defineProperty() { return false }}), 'x', {value: 1})`, "false"},
		{"defineProperty on non-extensible", nonExt + `return Reflect.defineProperty(new Proxy(ne, {defineProperty() { return true }}), 'b', {value: 1})`,
			"!TypeError: 'defineProperty' on proxy: trap returned truish for adding property 'b' to the non-extensible proxy target"},
		{"defineProperty non-configurable absent", `return Reflect.defineProperty(new Proxy({}, {defineProperty() { return true }}), 'x', {value: 1, configurable: false})`,
			"!TypeError: 'defineProperty' on proxy: trap returned truish for defining non-configurable property 'x' which is either non-existent or configurable in the proxy target"},
		{"defineProperty non-configurable configurable", `return Reflect.defineProperty(new Proxy({x: 1}, {defineProperty() { return true }}), 'x', {configurable: false})`,
			"!TypeError: 'defineProperty' on proxy: trap returned truish for defining non-configurable property 'x' which is either non-existent or configurable in the proxy target"},
		{"defineProperty incompatible", `return Reflect.defineProperty(new Proxy(Object.defineProperty({}, 'x', {value: 1}), {defineProperty() { return true }}), 'x', {value: 2})`,
			"!TypeError: 'defineProperty' on proxy: trap returned truish for adding property 'x' that is incompatible with the existing property in the proxy target"},
		{"defineProperty non-writable", `return Reflect.defineProperty(new Proxy(Object.defineProperty({}, 'x', {value: 1, writable: true}), {defineProperty() { return true }}), 'x', {writable: false, configurable: false})`,
			"!TypeError: 'defineProperty' on proxy: trap returned truish for defining non-configurable property 'x' which cannot be non-writable, unless there exists a corresponding non-configurable, non-writable own property of the target object"},
		{"defineProperty descriptor object", `let got; Reflect.defineProperty(new Proxy({}, {defineProperty(t, k, d) { got = d; return true }}), 'x', {value: 1, extra: 2}); return [Object.keys(got).join('|'), Object.getPrototypeOf(got) === Object.prototype].join()`,
			"value,true"},
		// has
		{"has hides non-configurable", `return 'x' in new Proxy(Object.defineProperty({}, 'x', {value: 1}), {has() { return false }})`,
			"!TypeError: 'has' on proxy: trap returned falsish for property 'x' which exists in the proxy target as non-configurable"},
		{"has hides on non-extensible", nonExt + `return 'a' in new Proxy(ne, {has() { return false }})`,
			"!TypeError: 'has' on proxy: trap returned falsish for property 'a' but the proxy target is not extensible"},
		{"has with", `return Reflect.has(new Proxy({}, {has(t, k) { return k === 'y' }}), 'y')`, "true"},
		// get
		{"get non-writable", `return new Proxy(Object.defineProperty({}, 'x', {value: 1}), {get() { return 2 }}).x`,
			"!TypeError: 'get' on proxy: property 'x' is a read-only and non-configurable data property on the proxy target but the proxy did not return its actual value"},
		{"get non-writable same", `return new Proxy(Object.defineProperty({}, 'x', {value: NaN}), {get() { return NaN }}).x`, "NaN"},
		{"get accessor without getter", `return new Proxy(Object.defineProperty({}, 'x', {set(v) {}}), {get() { return 2 }}).x`,
			"!TypeError: 'get' on proxy: property 'x' is a non-configurable accessor property on the proxy target and does not have a getter function, but the trap did not return 'undefined'"},
		// set
		{"set falsish strict", `new Proxy({}, {set() { return false }}).x = 1`,
			"!TypeError: 'set' on proxy: trap returned falsish for property 'x'"},
		{"set falsish Reflect", `return Reflect.set(new Proxy({}, {set() { return false }}), 'x', 1)`, "false"},
		{"set non-writable", `return Reflect.set(new Proxy(Object.defineProperty({}, 'x', {value: 1}), {set() { return true }}), 'x', 2)`,
			"!TypeError: 'set' on proxy: trap returned truish for property 'x' which exists in the proxy target as a non-configurable and non-writable data property with a different value"},
		{"set accessor without setter", `return Reflect.set(new Proxy(Object.defineProperty({}, 'x', {get() {}}), {set() { return true }}), 'x', 2)`,
			"!TypeError: 'set' on proxy: trap returned truish for property 'x' which exists in the proxy target as a non-configurable and non-writable accessor property without a setter"},
		// deleteProperty
		{"delete falsish strict", `delete new Proxy({}, {deleteProperty() { return false }}).x`,
			"!TypeError: 'deleteProperty' on proxy: trap returned falsish for property 'x'"},
		{"delete non-configurable", `return Reflect.deleteProperty(new Proxy(Object.defineProperty({}, 'x', {value: 1}), {deleteProperty() { return true }}), 'x')`,
			"!TypeError: 'deleteProperty' on proxy: trap returned truish for property 'x' which is non-configurable in the proxy target"},
		{"delete on non-extensible", nonExt + `return Reflect.deleteProperty(new Proxy(ne, {deleteProperty() { return true }}), 'a')`,
			"!TypeError: 'deleteProperty' on proxy: trap returned truish for property 'a' but the proxy target is non-extensible"},
		// ownKeys
		{"ownKeys duplicates", `return Reflect.ownKeys(new Proxy({}, {ownKeys() { return ['a', 'a'] }}))`,
			"!TypeError: 'ownKeys' on proxy: trap returned duplicate entries"},
		{"ownKeys invalid entry", `return Reflect.ownKeys(new Proxy({}, {ownKeys() { return [1] }}))`,
			"!TypeError: 1 is not a valid property name"},
		{"ownKeys non-object", `return Reflect.ownKeys(new Proxy({}, {ownKeys() { return 'ab' }}))`,
			"!TypeError: CreateListFromArrayLike called on non-object"},
		{"ownKeys missing non-configurable", `return Reflect.ownKeys(new Proxy(Object.defineProperty({}, 'x', {value: 1}), {ownKeys() { return [] }}))`,
			"!TypeError: 'ownKeys' on proxy: trap result did not include 'x'"},
		{"ownKeys missing on non-extensible", nonExt + `return Reflect.ownKeys(new Proxy(ne, {ownKeys() { return [] }}))`,
			"!TypeError: 'ownKeys' on proxy: trap result did not include 'a'"},
		{"ownKeys extra on non-extensible", nonExt + `return Reflect.ownKeys(new Proxy(ne, {ownKeys() { return ['a', 'b'] }}))`,
			"!TypeError: 'ownKeys' on proxy: trap returned extra keys but proxy target is non-extensible"},
		{"ownKeys order and symbols", `const s = Symbol('s'); const k = Reflect.ownKeys(new Proxy({}, {ownKeys() { return ['b', s, '1', 'a'] }})); return [k.length, k[1] === s, k.filter(x => typeof x === 'string').join('|')].join()`,
			"4,true,b|1|a"},
		{"ownKeys array-like", `return Object.getOwnPropertyNames(new Proxy({}, {ownKeys() { return {length: 2, 0: 'a', 1: 'b'} }})).join()`, "a,b"},
		{"ownKeys getter order", `const log = []; const res = new Proxy(['a'], {get(t, k) { log.push(String(k)); return Reflect.get(t, k) }}); Reflect.ownKeys(new Proxy({}, {ownKeys() { return res }})); return log.join()`,
			"length,0"},
		// apply and construct
		{"apply", `const f = new Proxy(function () {}, {apply(t, self, args) { return [self, args.length, Array.isArray(args)].join() }}); return f.call('s', 1, 2)`, "s,2,true"},
		{"construct non-object", `return new (new Proxy(function () {}, {construct() { return 1 }}))`,
			"!TypeError: 'construct' on proxy: trap returned non-object ('1')"},
		{"construct newTarget", `const P = new Proxy(function () {}, {construct(t, args, nt) { return {same: nt === P, n: args.length} }}); const r = new P(1, 2); class C extends P {}; return [r.same, r.n, Reflect.construct(P, [], Array).same].join()`,
			"true,2,false"},
		{"construct not a constructor", `return new (new Proxy(() => 1, {construct() { return {} }}))`, "!TypeError"},
		{"construct of a proxy of a class", `class A { constructor(x) { this.x = x } } const P = new Proxy(A, {}); const a = new P(3); return [a.x, a instanceof A, Object.getPrototypeOf(a) === A.prototype].join()`,
			"3,true,true"},
		{"extends a proxy", `class A { constructor(x) { this.x = x } } class B extends new Proxy(A, {}) { constructor() { super(5) } } const b = new B(); return [b.x, b instanceof B, b instanceof A].join()`,
			"5,true,true"},
		{"call a proxy of a class", `return new Proxy(class {}, {})()`, "!TypeError"},
		{"trap not callable", `return new Proxy({}, {get: 1}).x`, "!TypeError"},
		{"trap null forwards", `return new Proxy({x: 1}, {get: null, has: undefined}).x`, "1"},
		{"trap throws", `try { new Proxy({}, {get() { throw new RangeError('boom') }}).x } catch (e) { return e.name + ': ' + e.message }`, "RangeError: boom"},
	})
}

func TestProxyDeepChain(t *testing.T) {
	runProtoCases(t, []protoCase{
		{"forwarding chain", `let p = {x: 1}; for (let i = 0; i < 100000; i++) p = new Proxy(p, {});
const out = [];
for (const f of [() => p.x, () => { p.y = 1 }, () => 'x' in p, () => delete p.x, () => Object.keys(p), () => Object.getPrototypeOf(p),
	() => Object.isExtensible(p), () => Object.getOwnPropertyDescriptor(p, 'x'), () => Object.defineProperty(p, 'z', {value: 1}),
	() => Object.preventExtensions(p), () => Object.setPrototypeOf(p, null), () => JSON.stringify(p), () => Object.create(p).x, () => Array.isArray(p)]) {
	try { f(); out.push('ok') } catch (e) { out.push(e.name) }
}
return out.join()`, "RangeError,RangeError,RangeError,RangeError,RangeError,RangeError,RangeError,RangeError,RangeError,RangeError,RangeError,RangeError,RangeError,ok"},
		{"callable chain", `let f = function () { return 1 }; for (let i = 0; i < 100000; i++) f = new Proxy(f, {});
const out = [typeof f];
for (const g of [() => f(), () => new f(), () => f.call(null)]) { try { g(); out.push('ok') } catch (e) { out.push(e.name) } }
return out.join()`, "function,RangeError,RangeError,RangeError"},
		{"short chain", `let p = {x: 1}; let f = () => 2; for (let i = 0; i < 300; i++) { p = new Proxy(p, {}); f = new Proxy(f, {}) } return [p.x, f(), Object.keys(p).join()].join()`, "1,2,x"},
		{"recursive trap", `const p = new Proxy({}, {get(t, k, r) { return r[k] }}); return p.x`, "!RangeError: Maximum call stack size exceeded"},
	})
}

func TestProxySeeThrough(t *testing.T) {
	runProtoCases(t, []protoCase{
		{"typeof", `return [typeof new Proxy({}, {}), typeof new Proxy(function () {}, {}), typeof new Proxy(class {}, {}), typeof new Proxy(new Proxy(() => 1, {}), {})].join()`,
			"object,function,function,function"},
		{"Array.isArray", `return [Array.isArray(new Proxy([], {})), Array.isArray(new Proxy(new Proxy([], {}), {})), Array.isArray(new Proxy({}, {})), Array.isArray(new Proxy({length: 0}, {getPrototypeOf() { return Array.prototype }}))].join()`,
			"true,true,false,false"},
		{"Object.prototype.toString", `const ts = x => Object.prototype.toString.call(x); return [ts(new Proxy([], {})), ts(new Proxy(function () {}, {})), ts(new Proxy({}, {})), ts(new Proxy(new Date(0), {})), ts(new Proxy(new Error(), {})), ts(new Proxy({}, {get(t, k) { return k === Symbol.toStringTag ? 'X' : undefined }}))].join()`,
			"[object Array],[object Function],[object Object],[object Object],[object Object],[object X]"},
		{"for-in", `const p = new Proxy({a: 1, b: 2}, {ownKeys() { return ['b', 'a', 'c'] }}); const out = []; for (const k in p) out.push(k); return out.join()`, "b,a"},
		{"for-in enumerability", `const p = new Proxy({a: 1, b: 2}, {getOwnPropertyDescriptor(t, k) { const d = Reflect.getOwnPropertyDescriptor(t, k); if (k === 'a') d.enumerable = false; return d }}); const out = []; for (const k in p) out.push(k); return out.join()`, "b"},
		{"for-in prototype", `const c = Object.create(new Proxy({a: 1}, {})); c.b = 2; const out = []; for (const k in c) out.push(k); return out.join()`, "b,a"},
		{"for-in proxy prototype chain", `const p = new Proxy({a: 1}, {getPrototypeOf() { return {z: 1} }}); const out = []; for (const k in p) out.push(k); return out.join()`, "a,z"},
		{"instanceof", `class A {} const p = new Proxy({}, {getPrototypeOf() { return A.prototype }}); return [p instanceof A, new Proxy(new A(), {}) instanceof A, A.prototype.isPrototypeOf(p)].join()`,
			"true,true,true"},
		{"instanceof callable proxy", `function F() {} const P = new Proxy(F, {}); return [new F() instanceof P, Function.prototype[Symbol.hasInstance].call(P, new F())].join()`, "true,true"},
		{"Function.prototype.toString", `return [Function.prototype.toString.call(new Proxy(function f() {}, {})), Function.prototype.toString.call(new Proxy(class A {}, {}))].join('|')`,
			"function () { [native code] }|function () { [native code] }"},
		{"Function.prototype.toString non-callable", `return Function.prototype.toString.call(new Proxy({}, {}))`, "!TypeError"},
		{"bind", `const p = new Proxy(function f(a, b) { return a + b }, {}); const b = p.bind(null, 1); return [b(2), b.name, b.length, new (new Proxy(function () { this.x = 1 }, {}).bind())().x].join()`,
			"3,bound f,1,1"},
		{"call and apply", `const p = new Proxy(function (a) { return this.v + a }, {}); return [p.call({v: 1}, 2), p.apply({v: 3}, [4]), Reflect.apply(p, {v: 5}, [6])].join()`, "3,7,11"},
		{"hasOwnProperty", `const p = new Proxy({a: 1}, {}); return [p.hasOwnProperty('a'), Object.prototype.hasOwnProperty.call(p, 'b'), Object.hasOwn(p, 'a'), ({}).propertyIsEnumerable.call(p, 'a')].join()`,
			"true,false,true,true"},
		{"__proto__", `const d = Object.getOwnPropertyDescriptor(Object.prototype, '__proto__'); const p = new Proxy([], {}); const r = [d.get.call(p) === Array.prototype]; p.__proto__ = null; return r.concat(Object.getPrototypeOf(p), p.__proto__).join()`, "true,,"},
		{"__lookupGetter__", `const p = new Proxy({get x() { return 1 }}, {}); return [typeof p.__lookupGetter__('x'), typeof Object.create(p).__lookupGetter__('x')].join()`, "function,function"},
		{"isPrototypeOf", `const a = {}; const p = new Proxy(Object.create(a), {}); return [a.isPrototypeOf(p), Object.prototype.isPrototypeOf(p), Array.prototype.isPrototypeOf(p)].join()`, "true,true,false"},
		{"Object.freeze", `const p = new Proxy({a: 1}, {}); Object.freeze(p); return [Object.isFrozen(p), Object.isSealed(p), Object.isExtensible(p), Object.getOwnPropertyDescriptor(p, 'a').writable, Reflect.set(p, 'a', 2)].join()`,
			"true,true,false,false,false"},
		{"Object.seal", `const p = new Proxy([1], {}); Object.seal(p); p[0] = 2; return [Object.isSealed(p), Object.isFrozen(p), p[0], Reflect.deleteProperty(p, 0)].join()`, "true,false,2,false"},
		{"isFrozen empty", `return [Object.isFrozen(new Proxy(Object.preventExtensions({}), {})), Object.isFrozen(new Proxy({}, {}))].join()`, "true,false"},
		{"Object.getOwnPropertyDescriptors", `const d = Object.getOwnPropertyDescriptors(new Proxy({a: 1}, {ownKeys() { return ['a', 'b'] }})); return Object.keys(d).join()`, "a"},
		{"Object.defineProperties", `const p = new Proxy({}, {}); Object.defineProperties(p, new Proxy({a: {value: 1, enumerable: true}}, {})); return Object.keys(p).join()`, "a"},
		{"Object.fromEntries", `return JSON.stringify(Object.fromEntries(new Proxy([['a', 1]], {})))`, `{"a":1}`},
		{"Object.groupBy", `return JSON.stringify(Object.groupBy(new Proxy([1, 2, 3], {}), x => x % 2 ? 'odd' : 'even'))`, `{"odd":[1,3],"even":[2]}`},
		{"Error cause", `const e = new Error('m', new Proxy({cause: 1}, {has(t, k) { return k === 'cause' }})); return e.cause`, "1"},
		{"Error.isError", `return [Error.isError(new Proxy(new Error(), {})), structuredClone.length].join()`, "false,1"},
		{"structuredClone", `return structuredClone(new Proxy({}, {}))`, "!TypeError: DataCloneError"},
		{"equality", `const t = {}; const p = new Proxy(t, {}); return [p === t, p == t, new Map([[p, 1]]).get(p), new Set([p, t]).size, Object.is(p, p)].join()`, "false,false,1,2,true"},
		{"template and concatenation", `const p = new Proxy({toString() { return 'x' }}, {}); return [` + "`${p}`" + `, p + '', [p].join()].join()`, "x,x,x"},
		{"with a Symbol key", `const s = Symbol('k'); const p = new Proxy({[s]: 1}, {}); return [p[s], s in p, Object.getOwnPropertySymbols(p).length].join()`, "1,true,1"},
		{"optional call", `const p = new Proxy({f() { return 1 }}, {}); return [p.f?.(), p.g?.()].join()`, "1,"},
		{"getter receiver on the chain", `const o = Object.create(new Proxy({get me() { return this }}, {})); return o.me === o`, "true"},
		{"setter receiver on the chain", `let got; const o = Object.create(new Proxy({set me(v) { got = this }}, {})); o.me = 1; return [got === o, Object.hasOwn(o, 'me')].join()`, "true,false"},
		{"array target set", `const p = new Proxy([], {}); p[5] = 1; p.length = 2; p.push(7); return [p.length, p.join()].join('|')`, "3|,,7"},
	})
}

func TestProxyArrays(t *testing.T) {
	runProtoCases(t, []protoCase{
		{"concat", `return [].concat(new Proxy([1, 2], {}), 3, new Proxy({length: 1, 0: 'x'}, {})).length`, "4"},
		{"concat spreadable", `return [].concat(new Proxy({length: 1, 0: 'x', [Symbol.isConcatSpreadable]: true}, {}), new Proxy([1, 2], {})).join()`, "x,1,2"},
		{"flat", `return [new Proxy([1, [2, new Proxy([3], {})]], {})].flat(Infinity).join()`, "1,2,3"},
		{"flat length trap", `return [new Proxy([1, 2, 3], {get(t, k) { return k === 'length' ? 2 : Reflect.get(t, k) }})].flat().join()`, "1,2"},
		{"map", `const r = Array.prototype.map.call(new Proxy([1, 2], {}), x => x * 2); return [Array.isArray(r), r.join()].join()`, "true,2,4"},
		{"species of a proxy", `class C extends Array {} const p = new Proxy([1, 2], {get(t, k, r) { return k === 'constructor' ? C : Reflect.get(t, k, r) }}); return [Array.prototype.map.call(p, x => x) instanceof C, Array.prototype.filter.call(p, x => x > 1) instanceof C, Array.prototype.slice.call(p) instanceof C].join()`,
			"true,true,true"},
		{"species of a non-array proxy", `const p = new Proxy({length: 1, 0: 1, constructor: 1}, {}); return Array.isArray(Array.prototype.map.call(p, x => x))`, "true"},
		{"species creates a proxy", `const a = [1, 2]; a.constructor = {[Symbol.species]: function (n) { return new Proxy(new Array(n), {}) }}; const r = a.map(x => x + 1); return [Array.isArray(r), r.join()].join()`, "true,2,3"},
		{"sort", `const p = new Proxy([3, 1, 2], {}); Array.prototype.sort.call(p); return Array.prototype.join.call(p)`, "1,2,3"},
		{"reverse", `const p = new Proxy([1, 2, 3], {}); Array.prototype.reverse.call(p); return Array.prototype.join.call(p)`, "3,2,1"},
		{"splice", `const p = new Proxy([1, 2, 3, 4], {}); const r = p.splice(1, 2, 'x'); return [r.join(), p.join()].join('|')`, "2,3|1,x,4"},
		{"shift unshift pop", `const p = new Proxy([1, 2, 3], {}); p.unshift(0); p.shift(); p.pop(); return p.join()`, "1,2"},
		{"copyWithin fill", `const p = new Proxy([1, 2, 3, 4], {}); p.copyWithin(0, 2); return p.fill(0, 3).join()`, "3,4,3,0"},
		{"holes", `const p = new Proxy([1, , 3], {has(t, k) { return k !== '0' && Reflect.has(t, k) }}); const out = []; p.forEach((x, i) => out.push(i)); return [out.join(), p.indexOf(1), p.includes(1)].join('|')`, "2|-1|true"},
		{"Array.from", `return Array.from(new Proxy([1, 2], {})).join() + Array.from(new Proxy({length: 2, 0: 'a'}, {})).join()`, "1,2a,"},
		{"iteration", `const out = []; for (const x of new Proxy([1, 2], {})) out.push(x); return out.join()`, "1,2"},
		{"toSorted with", `const p = new Proxy([2, 1], {}); return [p.toSorted().join(), p.with(0, 9).join(), p.toReversed().join(), p.at(-1)].join('|')`, "1,2|9,1|1,2|1"},
		// A proxy is not its target: joining the target reaches the proxy,
		// whose join reaches the target again only through itself.
		{"join cycle", `const a = []; const p = new Proxy(a, {}); a.push(1, p); return [p.join(), a.join()].join('|')`, "1,|1,1,"},
		{"join cycle plain", `const a = [1]; a.push(a); return a.join()`, "1,"},
		{"length", `return new Proxy([1, 2, 3], {}).length`, "3"},
	})
}

// protoProxyArray prefixes a case body with run(f): f gets a = [0, , 2]
// whose prototype is a proxy of Array.prototype that supplies index 1
// through its has and get traps and logs the numeric keys set through it.
// run returns f's result (or "a" when it is a), a's own elements and the
// trap log, read before the result is rendered.
const protoProxyArray = `function run(f) {
	const L = [];
	const a = [0, , 2];
	Object.setPrototypeOf(a, new Proxy(Array.prototype, {
		get(t, k, r) { if (k === '1') { L.push('get:1'); return 'X' } return Reflect.get(t, k, r) },
		has(t, k) { if (k === '1') { L.push('has:1'); return true } return Reflect.has(t, k) },
		set(t, k, v, r) { if (typeof k === 'string' && /^\d+$/.test(k)) L.push('set:' + k); return Reflect.set(t, k, v, r) },
	}));
	const res = f(a);
	const log = L.join();
	const own = Object.getOwnPropertyNames(a).filter(k => k !== 'length').map(k => k + '=' + String(a[k])).join(' ');
	return [res === a ? 'a' : JSON.stringify(res), own, log].join('|');
}
`

// TestProxyArrayPrototype checks that the array builtins whose fast path
// takes the prototype chain to be free of indexed properties see a proxy on
// it: its traps run where the spec's HasProperty, Get and Set reach it.
func TestProxyArrayPrototype(t *testing.T) {
	cases := []protoCase{
		{"slice", `return run(a => a.slice())`, `[0,"X",2]|0=0 2=2|has:1,get:1`},
		{"toReversed", `return run(a => a.toReversed())`, `[2,"X",0]|0=0 2=2|get:1`},
		{"toSorted", `return run(a => a.toSorted())`, `[0,2,"X"]|0=0 2=2|get:1`},
		{"with", `return run(a => a.with(0, 9))`, `[9,"X",2]|0=0 2=2|get:1`},
		{"toSpliced", `return run(a => a.toSpliced(0, 0))`, `[0,"X",2]|0=0 2=2|get:1`},
		{"copyWithin", `return run(a => a.copyWithin(0, 1))`, `a|0=X 1=2 2=2|has:1,get:1,set:1`},
		{"splice", `return run(a => a.splice(0, 2))`, `[0,"X"]|0=2|has:1,get:1`},
		{"shift", `return run(a => a.shift())`, `0|0=X 1=2|has:1,get:1,set:1`},
		{"unshift", `return run(a => a.unshift(7))`, `4|0=7 1=0 2=X 3=2|set:3,has:1,get:1,set:1`},
		{"fill", `return run(a => a.fill(5, 1, 2))`, `a|0=0 1=5 2=2|set:1`},
		{"push", `return run(a => a.push(7))`, `4|0=0 2=2 3=7|set:3`},
		{"sort", `return run(a => a.sort())`, `a|0=0 1=2 2=X|has:1,get:1,set:1`},
		{"pop", `return run(a => a.pop())`, `2|0=0|`},
		{"reverse", `return run(a => (a.push(3), a.reverse()))`, `a|0=3 1=2 2=X 3=0|set:3,has:1,get:1,set:1`},
	}
	for i := range cases {
		cases[i].body = protoProxyArray + cases[i].body
	}
	runProtoCases(t, cases)
}

func TestProxyJSON(t *testing.T) {
	runProtoCases(t, []protoCase{
		{"stringify array", `return JSON.stringify(new Proxy([1, {a: 2}], {}))`, `[1,{"a":2}]`},
		{"stringify keys", `return JSON.stringify({x: new Proxy({a: 1, b: 2}, {ownKeys() { return ['b', 'a'] }})})`, `{"x":{"b":2,"a":1}}`},
		{"stringify function", `return [JSON.stringify(new Proxy(function () {}, {})), JSON.stringify([new Proxy(function () {}, {})]), JSON.stringify({f: new Proxy(() => 1, {})})].join('|')`,
			"|[null]|{}"},
		{"stringify toJSON", `return JSON.stringify(new Proxy({}, {get(t, k) { return k === 'toJSON' ? () => 'j' : undefined }}))`, `"j"`},
		{"stringify array length trap", `return JSON.stringify(new Proxy([1, 2, 3], {get(t, k) { return k === 'length' ? 2 : Reflect.get(t, k) }}))`, "[1,2]"},
		{"stringify cycle", `const a = {}; a.self = new Proxy(a, {}); return JSON.stringify(a)`, "!TypeError"},
		{"stringify proxy cycle", `const a = []; const p = new Proxy(a, {}); a.push(p); return JSON.stringify(p)`, "!TypeError"},
		{"stringify gap", `return JSON.stringify(new Proxy({a: [1]}, {}), null, 1)`, "{\n \"a\": [\n  1\n ]\n}"},
		{"replacer array proxy", `return JSON.stringify({a: 1, b: 2}, new Proxy(['b'], {}))`, `{"b":2}`},
		{"replacer function proxy", `return JSON.stringify({a: 1}, new Proxy((k, v) => typeof v === 'number' ? v + 1 : v, {}))`, `{"a":2}`},
		{"parse reviver", `const log = []; const r = JSON.parse('{"a":1,"b":{"c":2}}', function (k, v) {
	if (k === 'a') this.b = new Proxy({c: 2, d: 3}, {
		ownKeys(t) { log.push('ownKeys'); return Reflect.ownKeys(t) },
		defineProperty(t, k, d) { log.push('define ' + k); return Reflect.defineProperty(t, k, d) },
		deleteProperty(t, k) { log.push('delete ' + k); return Reflect.deleteProperty(t, k) },
	});
	return k === 'd' ? undefined : v;
}); const before = log.join(); return [JSON.stringify(r), before].join('|')`, `{"a":1,"b":{"c":2}}|ownKeys,define c,delete d`},
		{"parse reviver array proxy", `const r = JSON.parse('[1,[2]]', function (k, v) { if (k === '0' && Array.isArray(this) && this.length === 2 && this[0] === 1) this[1] = new Proxy([5, 6], {}); return typeof v === 'number' ? v * 10 : v }); return JSON.stringify(r)`, "[10,[50,60]]"},
		// A reviver that grows the structure as it is walked.
		{"parse reviver growing", `return JSON.parse('[1,[2]]', function (k, v) { if (k === '0') this[1] = new Proxy([5, 6], {}); return v })`, "!RangeError: Maximum call stack size exceeded"},
		{"parse reviver growing plain", `return JSON.parse('[1,[2]]', function (k, v) { if (k === '0') this[1] = [5, 6]; return v })`, "!RangeError: Maximum call stack size exceeded"},
		{"parse reviver deepest", `const s = '['.repeat(512) + ']'.repeat(512); let n = 0; JSON.parse(s, function (k, v) { n++; return v }); return n + (() => { try { JSON.parse('[' + s + ']') } catch (e) { return e.name } })()`, "512SyntaxError"},
	})
}

// TestProxyJSONReviverLength walks a reviver over a proxy array whose get
// trap reports a length of 2^53-1. The walk must not size anything from that
// length and must stop at an interrupt even when no JavaScript runs per
// element (the trap and the reviver are natives).
func TestProxyJSONReviverLength(t *testing.T) {
	f := evalModule(t, `export function mk(get) { return new Proxy([], {get}) }
export function walk(reviver) { return JSON.parse('[1, 2]', reviver) }`)
	r := f.r
	mk, _ := f.env.GetBindingValue("mk")
	walk, _ := f.env.GetBindingValue("walk")
	get := nativeFn(r, func(_ Value, args []Value) (Value, error) {
		if r.ToGo(Arg(args, 1)) == "length" {
			return NumberValue(1<<53 - 1), nil
		}
		return Undefined(), nil
	})
	p, err := r.Call(mk, Undefined(), []Value{get})
	require.NoError(t, err)
	const stopAt = 50000
	calls := 0
	reviver := nativeFn(r, func(this Value, args []Value) (Value, error) {
		calls++
		if calls == 1 { // "0" of the parsed [1, 2]: its "1" becomes p
			if err := this.AsObject().SetProp(r, IndexKey(1), p); err != nil {
				return Undefined(), err
			}
		}
		if calls == stopAt {
			r.Interrupt("stop")
		}
		return Arg(args, 1), nil
	})
	_, err = r.Call(walk, Undefined(), []Value{reviver})
	var ie *InterruptedError
	require.ErrorAs(t, err, &ie)
	assert.Less(t, calls, stopAt+interruptStride+1)
	r.ClearInterrupt()
}

// TestProxyJSONReplacerLength checks that a proxy replacer array whose
// length is 2^53-1 and whose get trap is native, so that no JavaScript
// runs, still stops at an interrupt.
func TestProxyJSONReplacerLength(t *testing.T) {
	f := evalModule(t, `export function mk(get) { return new Proxy([], {get}) }
export function str(replacer) { return JSON.stringify({a: 1}, replacer) }`)
	r := f.r
	mk, _ := f.env.GetBindingValue("mk")
	str, _ := f.env.GetBindingValue("str")
	const stopAt = 50000
	calls := 0
	get := nativeFn(r, func(_ Value, args []Value) (Value, error) {
		if r.ToGo(Arg(args, 1)) == "length" {
			return NumberValue(1<<53 - 1), nil
		}
		switch calls++; {
		case calls == stopAt:
			r.Interrupt("stop")
		case calls > stopAt+interruptStride:
			return Undefined(), r.TypeError("not interrupted")
		}
		return NumberValue(float64(calls)), nil // a new key each time
	})
	p, err := r.Call(mk, Undefined(), []Value{get})
	require.NoError(t, err)
	_, err = r.Call(str, Undefined(), []Value{p})
	var ie *InterruptedError
	require.ErrorAs(t, err, &ie)
	r.ClearInterrupt()
}

func TestJSONReplacerListDuplicates(t *testing.T) {
	runProtoCases(t, []protoCase{
		{"long list", `const keys = []; const o = {};
for (let i = 0; i < 80; i++) { keys.push('k' + (i % 50)); o['k' + i] = i; }
keys.push(3, '3', new String('k1'), new Number(3), 'k60'); o[3] = 'three';
const want = '{' + Array.from({length: 50}, (_, i) => '"k' + i + '":' + i).join() + ',"3":"three","k60":60}';
return JSON.stringify(o, keys) === want`, "true"},
		{"short list", `return JSON.stringify({a: 1, b: 2, 1: 3}, ['b', 'a', 'b', 1, '1'])`, `{"b":2,"a":1,"1":3}`},
	})
}

// TestObjectLookup checks Object.Lookup: found is false only when the
// chain, a get trap-less proxy's target included, lacks the property, and
// no has trap runs.
func TestObjectLookup(t *testing.T) {
	f := evalModule(t, `export const log = [];
const noHas = { has(t, k) { log.push('has ' + String(k)); return false } };
export const plain = Object.create({ up: 1, get acc() { return this === plain } });
export const dyn = new Proxy({}, { ...noHas, get(t, k) { log.push('get ' + String(k)); return k === 'x' ? 1 : undefined } });
export const bare = Object.create(new Proxy(Object.create({ up: 2 }), noHas));
const rv = Proxy.revocable({}, {}); rv.revoke();
export const revoked = rv.proxy;`)
	r := f.r
	for _, c := range []struct {
		obj, key string
		want     any
		found    bool
	}{
		{"plain", "up", int64(1), true}, {"plain", "acc", true, true}, {"plain", "missing", nil, false},
		{"dyn", "x", int64(1), true}, {"dyn", "y", nil, true},
		{"bare", "up", int64(2), true}, {"bare", "missing", nil, false},
	} {
		o, _ := f.env.GetBindingValue(c.obj)
		v, found, err := o.AsObject().Lookup(r, r.KeyFromGoString(c.key))
		require.NoError(t, err, c.obj+"."+c.key)
		assert.Equal(t, c.found, found, c.obj+"."+c.key)
		assert.Equal(t, c.want, r.ToGo(v), c.obj+"."+c.key)
	}
	logv, _ := f.env.GetBindingValue("log")
	assert.Equal(t, []any{"get x", "get y"}, r.ToGo(logv))
	o, _ := f.env.GetBindingValue("revoked")
	_, _, err := o.AsObject().Lookup(r, r.KeyFromGoString("x"))
	assert.ErrorContains(t, err, "revoked")
}

func TestProxyICAndGlobals(t *testing.T) {
	runProtoCases(t, []protoCase{
		{"prototype chain loop", `const proto = new Proxy({}, {get(t, k, r) { return k === 'x' ? 42 : Reflect.get(t, k, r) }});
const o = Object.create(proto); const plain = Object.create({x: 1});
function f(o) { return o.x }
let s = 0;
for (let i = 0; i < 50; i++) s += f(o) + f(plain);
o.x = 5;
for (let i = 0; i < 50; i++) s += f(o) + f(plain);
return [s, Object.hasOwn(o, 'x')].join()`, "2450,true"},
		{"changing trap", `let v = 1; const o = Object.create(new Proxy({}, {get(t, k) { return k === 'x' ? v++ : undefined }})); let s = 0; for (let i = 0; i < 10; i++) s += o.x; return s`, "55"},
		{"shapes across prototypes", `const a = Object.create(new Proxy({}, {get() { return 'p' }})); const b = Object.create({x: 'o'}); const c = {x: 'c'}; const out = []; for (let i = 0; i < 20; i++) for (const o of [a, b, c]) out.push(o.x); return out.slice(-3).join() + out.length`, "p,o,c60"},
		{"method on a proxy prototype", `class A { m() { return 1 } } const o = Object.create(new Proxy(A.prototype, {})); let s = 0; for (let i = 0; i < 20; i++) s += o.m(); return s`, "20"},
		{"set through a proxy prototype", `const log = []; const o = Object.create(new Proxy({}, {set(t, k, v, r) { log.push(k); return Reflect.set(t, k, v, r) }})); for (let i = 0; i < 5; i++) o['k' + (i % 2)] = i; return [log.join(), o.k0, o.k1].join('|')`, "k0,k1|4|3"},
		{"proxy as the global prototype", `Object.setPrototypeOf(globalThis, new Proxy(Object.getPrototypeOf(globalThis), {has(t, k) { return k === 'magic' || Reflect.has(t, k) }, get(t, k, r) { return k === 'magic' ? 7 : Reflect.get(t, k, r) }}));
const r = [magic, typeof nope, typeof magic, typeof Object];
magic = 8;
return r.concat(magic, Object.hasOwn(globalThis, 'magic')).join()`, "7,undefined,number,function,8,true"},
		{"global has trap throws", `Object.setPrototypeOf(globalThis, new Proxy({}, {has() { throw new Error('h') }})); return missing`, "!Error: h"},
	})
}

// globalProxyTraps puts a proxy on the global object's prototype that logs
// the has, get and set traps for zzq and zzr; has answers for zzr only
// while alive() is true.
const globalProxyTraps = `const L = []; let alive = () => true;
Object.setPrototypeOf(globalThis, new Proxy(Object.getPrototypeOf(globalThis), {
	has(t, k) { if (k === 'zzq' || k === 'zzr') { L.push('has'); return k === 'zzq' || alive() } return Reflect.has(t, k) },
	get(t, k, r) { if (k === 'zzq' || k === 'zzr') { L.push('get'); return 1 } return Reflect.get(t, k, r) },
	set(t, k, v, r) { if (k === 'zzq' || k === 'zzr') { L.push('set:' + v); return true } return Reflect.set(t, k, v, r) },
}));
const take = () => L.splice(0).join();
`

// TestProxyGlobalBindingHas checks that an identifier resolved through a
// proxy on the global object's prototype is looked up twice, by the
// reference and by GetBindingValue or SetMutableBinding, and that a
// binding gone at the second look is a ReferenceError.
func TestProxyGlobalBindingHas(t *testing.T) {
	runProtoCases(t, []protoCase{
		{"sequence", globalProxyTraps + `const out = [];
zzq; out.push(take());
zzq = 5; out.push(take());
typeof zzq; out.push(take());
zzq += 1; out.push(take());
for (let i = 0; i < 3; i++) zzq; out.push(take());
return out.join('|')`, "has,has,get|has,has,set:5|has,has,get|has,has,get,has,has,set:2|has,has,get,has,has,get,has,has,get"},
		{"read of a vanishing binding", globalProxyTraps + `let n = 0; alive = () => ++n === 1; try { return zzr } catch (e) { return String(e) + '|' + take() }`, "ReferenceError: zzr is not defined|has,has"},
		{"write of a vanishing binding", globalProxyTraps + `let n = 0; alive = () => ++n === 1; try { zzr = 2 } catch (e) { return String(e) + '|' + take() }`, "ReferenceError: zzr is not defined|has,has"},
		{"typeof a vanishing binding", globalProxyTraps + `let n = 0; alive = () => ++n === 1; try { return typeof zzr } catch (e) { return String(e) + '|' + take() }`, "ReferenceError: zzr is not defined|has,has"},
		{"unresolvable", globalProxyTraps + `alive = () => false; const t = typeof zzr; try { zzr } catch (e) { return t + '|' + String(e) + '|' + take() }`, "undefined|ReferenceError: zzr is not defined|has,has"},
		{"own global property", globalProxyTraps + `Object.defineProperty(globalThis, 'zzq', {value: 3, writable: true}); const v = zzq; zzq = 4; return v + '|' + zzq + '|' + take()`, "3|4|"},
	})
}

func TestProxyToGo(t *testing.T) {
	f := evalModule(t, `
export const obj = new Proxy({a: 1, b: [1, 2]}, {get(t, k) { return k === 'a' ? 'trap' : Reflect.get(t, k) }});
export const arr = new Proxy([1, 2, 3], {get(t, k) { return k === 'length' ? 2 : Reflect.get(t, k) }});
export const fn = new Proxy(function () {}, {});
export const revoked = (() => { const r = Proxy.revocable({}, {}); r.revoke(); return r.proxy })();
export const throwing = new Proxy({a: 1}, {get() { throw new Error('g') }});
export const throwingKeys = new Proxy({}, {ownKeys() { throw new Error('k') }});
export const selfArr = (() => { const a = []; const p = new Proxy(a, {}); a.push(p); return p })();
export const selfObj = (() => { const o = {}; const p = new Proxy(o, {}); o.self = p; return p })();
export const inArr = (() => { const p = new Proxy([1], {}); return [p, p] })();
export function thrower() { globalThis.n = 0; throw new Proxy(new TypeError('m'), new Proxy({}, {get() { globalThis.n++ }})) }
`)
	r := f.r
	get := func(name string) Value {
		v, ok := f.env.GetBindingValue(name)
		require.True(t, ok, name)
		return v
	}
	obj, arr, fn, revoked := get("obj"), get("arr"), get("fn"), get("revoked")
	assert.Equal(t, map[string]any{"a": "trap", "b": []any{int64(1), int64(2)}}, r.ToGo(obj))
	assert.Equal(t, []any{int64(1), int64(2)}, r.ToGo(arr))
	assert.Equal(t, fn.AsObject(), r.ToGo(fn))

	assert.Nil(t, r.ToGo(revoked))
	_, err := r.ToGoStrict(revoked)
	assert.ErrorContains(t, err, "revoked")
	assert.Equal(t, map[string]any{"a": nil}, r.ToGo(get("throwing")))
	_, err = r.ToGoStrict(get("throwing"))
	assert.ErrorContains(t, err, "g")
	_, err = r.ToGoStrict(get("throwingKeys"))
	assert.ErrorContains(t, err, "k")

	// A proxy array that contains itself nests to the depth limit; a proxy
	// object is registered before its properties are read.
	_, err = r.ToGoStrict(get("selfArr"))
	assert.Error(t, err)
	m, err := r.ToGoStrict(get("selfObj"))
	require.NoError(t, err)
	assert.Equal(t, m, m.(map[string]any)["self"])
	assert.Equal(t, []any{[]any{int64(1)}, []any{int64(1)}}, r.ToGo(get("inArr")))

	s, err := r.JSONStringify(obj)
	require.NoError(t, err)
	assert.Equal(t, `{"a":"trap","b":[1,2]}`, s.GoString())

	ok, err := r.IsArray(arr)
	assert.True(t, ok)
	assert.NoError(t, err)
	_, err = r.IsArray(revoked)
	assert.Error(t, err)
	keys, err := r.EnumerableOwnKeys(obj.AsObject())
	require.NoError(t, err)
	assert.Len(t, keys, 2)
	all, err := r.OwnKeys(arr.AsObject())
	require.NoError(t, err)
	assert.Len(t, all, 4) // 0, 1, 2, length
	proto, err := r.PrototypeOf(arr.AsObject())
	require.NoError(t, err)
	assert.Equal(t, r.ArrayPrototype, proto)
	has, err := r.HasPropertyIn(StringValue(FromGoString("a")), obj)
	assert.True(t, has)
	assert.NoError(t, err)

	// Exception.Name and Message read no trap of a thrown proxy.
	_, err = r.Call(get("thrower"), Undefined(), nil)
	var exc *Exception
	require.True(t, asException(err, &exc))
	assert.Equal(t, "", exc.Name())
	assert.Equal(t, "", exc.Message())
	_ = exc.Error()
	n, _ := r.Global.GetOwnDataValue(r.KeyFromGoString("n"))
	assert.Equal(t, int64(0), r.ToGo(n))
}
