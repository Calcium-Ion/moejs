package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestInterpCompareJump covers the compare-and-branch ops in every position
// the compiler emits them (if, loops, ?:, && and || chains, !, switch
// cases): NaN and -0, the conversions of the generic path in source order,
// an exception from valueOf caught in the same frame, and the position a
// conversion's stack trace and a thrown TypeError report for the
// comparison.
func TestInterpCompareJump(t *testing.T) {
	src := `const vals = [0, -0, 1, -1, 1.5, NaN, Infinity, -Infinity, "1", "a", "b", "", null, undefined, true, false, 1n, 2n];
export function relational() {
  const out = [];
  for (const x of vals) for (const y of vals) {
    let s = "";
    if (x < y) s += "a"; if (x <= y) s += "b"; if (x > y) s += "c"; if (x >= y) s += "d";
    if (!(x < y)) s += "e"; if (!(x <= y)) s += "f"; if (!(x > y)) s += "g"; if (!(x >= y)) s += "h";
    s += x < y ? "i" : "j";
    if (x < y || x > y) s += "k"; if (x <= y && x >= y) s += "l";
    out.push(s);
  }
  return out.join(",");
}
export function strict() {
  const o = {}, s2 = ["a", "b"].join("");
  const ys = [0, -0, NaN, "ab", s2, o, {}, null, undefined, 1n, 1n];
  const out = [];
  for (const x of ys) for (const y of ys) out.push((x === y ? "T" : "F") + (x !== y ? "t" : "f") + (!(x === y) ? "n" : "y"));
  return out.join(",");
}
export function sw(v) {
  switch (v) { case 0: return "zero"; case "ab": return "ab"; case NaN: return "nan"; case 1n: return "big"; default: return "other"; }
}
export function loops(n) {
  let i = 0, s = 0;
  while (i < n) { s += i; i++; }
  do { s += 1000; i--; } while (i >= n - 2);
  for (let j = n; j > 0; j -= 3) s += j;
  for (let j = 0; j !== n; j++) s++;
  return s;
}
export function order() {
  const log = [];
  const a = { valueOf() { log.push("a"); return 1; } }, b = { valueOf() { log.push("b"); return 2; } };
  if (a < b) log.push("<"); if (a > b) log.push(">"); if (a <= b) log.push("<="); if (b >= a) log.push(">=");
  while (a > b) log.push("never");
  return log.join(" ");
}
export function caught() {
  const bad = { valueOf() { throw new RangeError("v"); } };
  let n = 0;
  for (let i = 0; i < 3; i++) {
    try { if (bad < i) n += 100; n++; } catch (e) { n += e.name.length; }
  }
  try { if (1n < Symbol()) n = -1; } catch (e) { n += e.message.length; }
  return n;
}
export function where() {
  const o = { valueOf() { throw new TypeError("from valueOf"); } };
  try { if (0 <
      o) return "lt"; } catch (e) { return e.stack; }
}
export function whereSymbol(o) {
  switch (o) { case 1: return "one"; }
  try { while (Symbol() < o) return "lt"; } catch (e) { return e.stack; }
}
`
	f := evalModule(t, src)
	assert.Equal(t, compareJumpWant, f.call("relational"))
	assert.Equal(t, "Tfy,Tfy,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Tfy,Tfy,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Tfy,Tfy,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Tfy,Tfy,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Tfy,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Tfy,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Tfy,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Tfy,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Tfy,Tfy,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Ftn,Tfy,Tfy", f.call("strict"))
	for _, c := range []struct {
		in   any
		want string
	}{{0, "zero"}, {"ab", "ab"}, {1, "other"}, {nil, "other"}} {
		assert.Equal(t, c.want, f.call("sw", c.in))
	}
	assert.Equal(t, int64(3077), f.call("loops", 10))
	assert.Equal(t, "a b < a b a b <= b a >= a b", f.call("order"))
	assert.Equal(t, int64(3*len("RangeError")+len("Cannot convert a Symbol value to a number")), f.call("caught"))

	// The comparison's position, as the unfused Lt reported it: in the
	// trace of a conversion that throws, and for its own TypeError.
	assert.Contains(t, f.call("where"), "\n    at where (t.js:"+lineCol(src, "0 <\n")+")")
	assert.Contains(t, f.call("whereSymbol", 2), "\n    at whereSymbol (t.js:"+lineCol(src, "Symbol() < o")+")")
}

// compareJumpWant is what V8 (node) returns for TestInterpCompareJump's
// relational; the other expectations there are V8's too.
const compareJumpWant = "bdegjl,bdegjl,abghik,cdefjk,abghik,efghj,abghik,cdefjk,abghik,efghj,efghj,bdegjl,bdegjl,efghj,abghik,bdegjl,abghik,abghik,bdegjl,bdegjl,abghik,cdefjk,abghik,efghj,abghik,cdefjk,abghik,efghj,efghj,bdegjl,bdegjl,efghj,abghik,bdegjl,abghik,abghik,cdefjk,cdefjk,bdegjl,cdefjk,abghik,efghj,abghik,cdefjk,bdegjl,efghj,efghj,cdefjk,cdefjk,efghj,bdegjl,cdefjk,bdegjl,abghik,abghik,abghik,abghik,bdegjl,abghik,efghj,abghik,cdefjk,abghik,efghj,efghj,abghik,abghik,efghj,abghik,abghik,abghik,abghik,cdefjk,cdefjk,cdefjk,cdefjk,bdegjl,efghj,abghik,cdefjk,cdefjk,efghj,efghj,cdefjk,cdefjk,efghj,cdefjk,cdefjk,cdefjk,abghik,efghj,efghj,efghj,efghj,efghj,efghj,efghj,efghj,efghj,efghj,efghj,efghj,efghj,efghj,efghj,efghj,efghj,efghj,cdefjk,cdefjk,cdefjk,cdefjk,cdefjk,efghj,bdegjl,cdefjk,cdefjk,efghj,efghj,cdefjk,cdefjk,efghj,cdefjk,cdefjk,cdefjk,cdefjk,abghik,abghik,abghik,abghik,abghik,efghj,abghik,bdegjl,abghik,efghj,efghj,abghik,abghik,efghj,abghik,abghik,abghik,abghik,cdefjk,cdefjk,bdegjl,cdefjk,abghik,efghj,abghik,cdefjk,bdegjl,abghik,abghik,cdefjk,cdefjk,efghj,bdegjl,cdefjk,bdegjl,abghik,efghj,efghj,efghj,efghj,efghj,efghj,efghj,efghj,cdefjk,bdegjl,abghik,cdefjk,efghj,efghj,efghj,efghj,efghj,efghj,efghj,efghj,efghj,efghj,efghj,efghj,efghj,efghj,cdefjk,cdefjk,bdegjl,cdefjk,efghj,efghj,efghj,efghj,efghj,efghj,bdegjl,bdegjl,abghik,cdefjk,abghik,efghj,abghik,cdefjk,abghik,abghik,abghik,bdegjl,bdegjl,efghj,abghik,bdegjl,abghik,abghik,bdegjl,bdegjl,abghik,cdefjk,abghik,efghj,abghik,cdefjk,abghik,efghj,efghj,bdegjl,bdegjl,efghj,abghik,bdegjl,abghik,abghik,efghj,efghj,efghj,efghj,efghj,efghj,efghj,efghj,efghj,efghj,efghj,efghj,efghj,efghj,efghj,efghj,efghj,efghj,cdefjk,cdefjk,bdegjl,cdefjk,abghik,efghj,abghik,cdefjk,bdegjl,efghj,efghj,cdefjk,cdefjk,efghj,bdegjl,cdefjk,bdegjl,abghik,bdegjl,bdegjl,abghik,cdefjk,abghik,efghj,abghik,cdefjk,abghik,efghj,efghj,bdegjl,bdegjl,efghj,abghik,bdegjl,abghik,abghik,cdefjk,cdefjk,bdegjl,cdefjk,abghik,efghj,abghik,cdefjk,bdegjl,efghj,efghj,cdefjk,cdefjk,efghj,bdegjl,cdefjk,bdegjl,abghik,cdefjk,cdefjk,cdefjk,cdefjk,cdefjk,efghj,abghik,cdefjk,cdefjk,efghj,efghj,cdefjk,cdefjk,efghj,cdefjk,cdefjk,cdefjk,bdegjl"
