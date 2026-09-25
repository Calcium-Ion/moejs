package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStringConcatLengthLimit is an audit reproducer: `s = s + s`
// builds ropes without copying, so the length limit must be enforced on the
// length arithmetic. Past it, `+` is RangeError: Invalid string length; it
// used to wrap the int32 length at 2^31 and panic in flatten on the first
// read (makeslice: cap out of range).
func TestStringConcatLengthLimit(t *testing.T) {
	f := evalModule(t, `
export function grow(n) { let s = "x".repeat(1 << 20); for (let i = 0; i < n; i++) s = s + s; return s.length; }
export function read(n) { let s = "x".repeat(1 << 20); for (let i = 0; i < n; i++) s = s + s; return s.charCodeAt(0); }
`)
	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("Go panic escaped from the interpreter: %v", p)
		}
	}()
	// 2^29 code units is the last legal doubling.
	require.Equal(t, int64(1<<29), f.call("grow", 9))
	// 2^30 code units already exceeds MaxStringLength: expected RangeError.
	res, err := f.callErr("grow", 10)
	require.Error(t, err, "grow(10) returned %v; expected RangeError: Invalid string length", res)
	require.Equal(t, "RangeError: Invalid string length", errMessage(t, err))
	// 2^31 code units: the length used to read as -2147483648 and the first
	// flatten panicked.
	res, err = f.callErr("read", 11)
	require.Error(t, err, "read(11) returned %v; expected RangeError", res)
	require.Equal(t, "RangeError: Invalid string length", errMessage(t, err))
}

// lowerMaxStringLength lowers the string length limit for the rest of the
// test so every producer can be driven past it with small inputs.
func lowerMaxStringLength(t *testing.T, n int) {
	t.Helper()
	old := maxStringLength
	maxStringLength = n
	t.Cleanup(func() { maxStringLength = old })
}

// TestStringLengthLimitProducers drives every builtin whose output can
// outgrow its inputs past the (lowered) limit: `+`, template literals,
// concat, repeat, padStart/padEnd, join, string and regexp replace on each
// of its paths, JSON.stringify, case conversion with expanding mappings,
// URI encoding and the bound-function name. Each must raise RangeError:
// Invalid string length, and a result of exactly the limit must succeed.
func TestStringLengthLimitProducers(t *testing.T) {
	lowerMaxStringLength(t, 1000)
	f := evalModule(t, `
const x600 = "x".repeat(600), y600 = "y".repeat(600);
export function over(which) {
  switch (which) {
  case "add": return x600 + y600;
  case "add-mixed": return x600 + 12345 + y600;
  case "template": return `+"`${x600}-${y600}`"+`;
  case "concat": return x600.concat(y600);
  case "repeat": return "a".repeat(1001);
  case "padStart": return "a".padStart(1001);
  case "padEnd": return "a".padEnd(1001, "xy");
  case "join": return [x600, y600].join("");
  case "join-sep": return ["a", "b", "c"].join(x600);
  case "replace": return x600.replace("x", "y".repeat(500));
  case "replaceAll": return x600.replaceAll("x", "yy");
  case "replace-fn": return x600.replaceAll("x", () => "yy");
  case "regexp-fast": return x600.replace(/x/g, "yy");
  case "regexp-template": return x600.replace(/(x)/g, "$1$1");
  case "regexp-fn": return x600.replace(/x/g, () => "yy");
  case "regexp-class": return " ".repeat(600).replace(/\s/g, "ab");
  case "regexp-utf16": return "é".repeat(600).replace(/é/g, "éé");
  case "json-string": return JSON.stringify(x600 + "x".repeat(399));
  case "json-array": return JSON.stringify([x600, y600]);
  case "json-object": return JSON.stringify({a: x600, b: y600});
  case "json-indent": { let o = 1; for (let i = 0; i < 60; i++) o = [o]; return JSON.stringify(o, null, 10); }
  case "upper": return "ß".repeat(600).toUpperCase();
  case "lower": return "İ".repeat(600).toLowerCase();
  case "encodeURIComponent": return encodeURIComponent("%".repeat(400));
  case "encodeURI-utf16": return encodeURI("é".repeat(200));
  case "bind": { const g = function () {}; Object.defineProperty(g, "name", {value: "n".repeat(998)}); return g.bind(null); }
  }
  throw new Error("unknown case " + which);
}
export function exact() {
  const g = function () {};
  Object.defineProperty(g, "name", {value: "n".repeat(994)});
  return [
    ("x".repeat(500) + "y".repeat(500)).length,
    `+"`${x600}${\"y\".repeat(400)}`"+`.length,
    "a".repeat(1000).length,
    "a".padStart(1000).length,
    ["x".repeat(500), "y".repeat(500)].join("").length,
    x600.replaceAll("x", "y").length,
    "x".repeat(500).replace(/x/g, "yy").length,
    JSON.stringify("x".repeat(998)).length,
    "ß".repeat(500).toUpperCase().length,
    encodeURIComponent("%".repeat(333)).length,
    g.bind(null).name.length,
  ];
}`)
	for _, which := range []string{
		"add", "add-mixed", "template", "concat", "repeat", "padStart", "padEnd", "join", "join-sep",
		"replace", "replaceAll", "replace-fn", "regexp-fast", "regexp-template", "regexp-fn", "regexp-class", "regexp-utf16",
		"json-string", "json-array", "json-object", "json-indent",
		"upper", "lower", "encodeURIComponent", "encodeURI-utf16", "bind",
	} {
		res, err := f.callErr("over", which)
		require.Error(t, err, "%s returned %v", which, res)
		assert.Equal(t, "RangeError: Invalid string length", errMessage(t, err), which)
	}
	assert.Equal(t, []any{int64(1000), int64(1000), int64(1000), int64(1000), int64(1000), int64(600), int64(1000), int64(1000), int64(1000), int64(999), int64(1000)}, f.call("exact"))
}

// TestConcatLimitAllocatesNothing checks that the limit is enforced on the
// computed length before any allocation: the failing Concat allocates only
// the RangeError.
func TestConcatLimitAllocatesNothing(t *testing.T) {
	lowerMaxStringLength(t, 100)
	r := NewRealm()
	a := FromGoString(string(make([]byte, 60)))
	ok, err := r.Concat(a, a) // 120 > 100
	require.Nil(t, ok)
	require.Equal(t, "RangeError: Invalid string length", errMessage(t, err))
	// A legal rope: no allocation beyond the rope node itself.
	b := FromGoString(string(make([]byte, 40)))
	assert.Equal(t, 1.0, testing.AllocsPerRun(20, func() {
		if _, err := r.Concat(a, b); err != nil {
			panic(err)
		}
	}))
}
