package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestInterpColdOps runs every op that the loop leaves to coldOp: results,
// errors with their positions, a catch in the same frame, and results
// written after a call that grew the register stack.
func TestInterpColdOps(t *testing.T) {
	// 300 module bindings of each kind: slots past 255 take the wide env
	// ops, and pre reads v299 before its declaration runs.
	var wide strings.Builder
	wide.WriteString("const pre = () => v299;\nexport const early = (() => { try { return pre(); } catch (e) { return e.message; } })();\n")
	for i := range 300 {
		fmt.Fprintf(&wide, "let v%d = %d;\nvar x%d = %d;\n", i, i, i, i)
	}
	wide.WriteString("export function wide() { v299 = 1000; x299 += 1; return pre() + v0 + x299; }\n")

	src := `function deep(n) { return n === 0 ? 0 : 1 + deep(n - 1); }
const grow = { has(t, k) { return deep(400) === 400; }, deleteProperty(t, k) { return deep(400) === 400; } };
export function values() {
  const o = { a: 1, b: 2 }, k = "b", p = { inherited: 1 };
  const arr = [1, , 3];
  const lit = { [k]: 5, [k + "m"]() { return 6; }, get g() { return 7; }, __proto__: p, ...{ s: 8 } };
  const { a, ...rest } = o;
  const spread = [...arr, ...[4]];
  function sum(...xs) { return xs.join("+"); }
  class C { constructor(...xs) { this.n = xs.length; } }
  const tag = (s) => s.raw.join("|");
  const keys = [];
  for (const key in o) keys.push(key);
  o[k] += 10;
  return [
    delete o.a, delete o[k], o.a, "b" in o, "inherited" in lit, o instanceof Object, [] instanceof C,
    lit.b, lit.bm(), lit.g, lit.inherited, lit.s, JSON.stringify(rest), arr.length, 1 in arr,
    spread.join(), sum(...spread), new C(...spread).n, /a+/g.flags, tag` + "`x${1}y`" + `, keys.join(), a,
  ].join(";");
}
export function grown() {
  const p = new Proxy({}, grow);
  return ["x" in p, delete p.x, String(new Proxy({}, grow) instanceof Object)].join();
}
export function tdz() {
  const read = () => x;
  try { read(); } catch (e) { return e.name + ": " + e.message; }
  let x = 1;
}
export function tdzRegister() {
  try { y; } catch (e) { return e.message; }
  let y = 1;
}
export function constAssign() { const c = 1; try { c = 2; } catch (e) { return e.message; } }
export function coercible(v) { try { const {} = v; return "ok"; } catch (e) { return e.message; } }
export function badIn() { try { return "x" in 1; } catch (e) { return e.message; } }
export function badInstanceOf() { try { return 1 instanceof 2; } catch (e) { return e.message; } }
export function badSpread() { try { Math.max(...1); } catch (e) { return e.message; } }
export function badNewSpread() { try { new Math.max(...[1]); } catch (e) { return e.message; } }
export function badRef(o) { try { o.x[1] += 1; } catch (e) { return e.message; } }
export function frozen() { "use strict"; const f = Object.freeze({ a: 1 }); try { delete f.a; } catch (e) { return e.name; } }
export function stack() {
  try { const z = 1; z = 2; } catch (e) { return e.stack; }
}
` + wide.String()
	f := evalModule(t, src)
	assert.Equal(t, "true;true;;false;true;true;false;5;6;7;1;8;{\"b\":2};3;false;1,,3,4;1++3+4;4;g;x|y;a,b;1", f.call("values"))
	assert.Equal(t, "true,true,true", f.call("grown"))
	assert.Equal(t, "ReferenceError: Cannot access 'x' before initialization", f.call("tdz"))
	assert.Equal(t, "Cannot access 'y' before initialization", f.call("tdzRegister"))
	assert.Equal(t, "Assignment to constant variable.", f.call("constAssign"))
	assert.Equal(t, "ok", f.call("coercible", 1))
	assert.Equal(t, "Cannot destructure 'null' as it is null.", f.call("coercible", nil))
	assert.Equal(t, "Cannot use 'in' operator to search for 'x' in 1", f.call("badIn"))
	assert.Equal(t, "Right-hand side of 'instanceof' is not an object", f.call("badInstanceOf"))
	assert.Contains(t, f.call("badSpread"), "is not iterable")
	assert.Contains(t, f.call("badNewSpread"), "is not a constructor")
	assert.Equal(t, "Cannot read properties of undefined (reading '1')", f.call("badRef", map[string]any{}))
	assert.Equal(t, "TypeError", f.call("frozen"))
	assert.Equal(t, "TypeError: Assignment to constant variable.\n    at stack (t.js:"+lineCol(src, "z = 2")+")", f.call("stack"))
	assert.Equal(t, int64(1300), f.call("wide"))
	assert.Equal(t, "Cannot access 'v299' before initialization", f.export("early"))
	st := &f.r.interp
	assert.Zero(t, st.sp, "register stack released")
	assert.Zero(t, st.nframes, "frames released")
}
