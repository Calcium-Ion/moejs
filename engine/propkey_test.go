package engine

import "testing"

// TestIndexKeyStringsRoundTrip checks that the key strings Object.keys and
// for-in produce for index properties look the index properties up again
// (they were once interned, which made KeyFromString treat them as names).
func TestIndexKeyStringsRoundTrip(t *testing.T) {
	runProtoCases(t, []protoCase{
		{"Object.keys", `const g = {}; g[1] = 2; g.x = 3; return Object.keys(g).map(k => g[k]).join()`, "2,3"},
		{"for-in", `const g = {7: "a"}; const out = []; for (const k in g) out.push(g[k], k in g, Object.hasOwn(g, k)); return out.join()`, "a,true,true"},
		{"array names", `const a = [1, , 3]; return Object.getOwnPropertyNames(a).map(k => a[k]).join()`, "1,3,3"},
		{"entries", `const g = {0: "z"}; return Object.entries(g).map(([k, v]) => g[k] === v).join()`, "true"},
	})
}
