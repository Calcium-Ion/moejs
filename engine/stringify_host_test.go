package engine

import (
	"encoding/json"
	"fmt"
	"math"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// materializeDeep materializes every host node reachable from v.
func materializeDeep(v Value, depth int) {
	if !v.IsObject() || depth > 100 {
		return
	}
	o := v.AsObject()
	o.materializeHost()
	for _, k := range o.OwnPropertyKeys() {
		if c, ok := o.getOwnCell(k); ok && c.attrs&attrAccessor == 0 {
			materializeDeep(c.value, depth+1)
		}
	}
}

func isPlaceholder(v Value) bool {
	return v.IsObject() && v.AsObject().shape.key == hostSentinelKey
}

// hostJSON serializes a fresh conversion of g, untouched or fully
// materialized, with JSONStringify (the UTF-16 result as UTF-8) or
// AppendJSON; it also reports whether the root was left a placeholder.
func hostJSON(t *testing.T, r *Realm, g any, materialize, appendJSON bool) (out string, untouched bool) {
	t.Helper()
	v, err := r.FromGo(g)
	require.NoError(t, err)
	if materialize {
		materializeDeep(v, 0)
	}
	if appendJSON {
		b, ok, err := r.AppendJSON([]byte("<"), v)
		switch {
		case err != nil:
			out = "error: " + err.Error()
		case !ok:
			out = "<none>"
		default:
			out = string(b)
		}
	} else {
		s, err := r.JSONStringify(v)
		switch {
		case err != nil:
			out = "error: " + err.Error()
		case s == nil:
			out = "<none>"
		default:
			out = "<" + s.GoString()
		}
	}
	return out, isPlaceholder(v)
}

// checkHostJSON compares the direct walk with the materialized object for
// both entry points; direct says whether the walk must have handled g.
func checkHostJSON(t *testing.T, r *Realm, g any, direct bool) {
	t.Helper()
	for _, appendJSON := range []bool{false, true} {
		want, _ := hostJSON(t, r, g, true, appendJSON)
		got, untouched := hostJSON(t, r, g, false, appendJSON)
		require.Equal(t, want, got, "append %v: %#v", appendJSON, g)
		if direct {
			require.True(t, untouched, "append %v: not written from Go: %#v", appendJSON, g)
		}
	}
}

func TestHostJSONValues(t *testing.T) {
	r := NewRealm()
	long := strings.Repeat("QUJD+/==", interruptStride/2+3)
	direct := []any{
		map[string]any{"b": 1, "a": "x", "c": []any{true, false, nil, 1.5}, "": "empty", "A": "upper", "_": map[string]any{}},
		map[string]any{"s": "q\"b\\s/\n\t\x00\x1f\x7f", "k\"\\\n": "key escapes"},
		map[string]any{"zh": "中文", "emoji": "😀", "img": long, "mixed": long + "中" + long},
		[]any{map[string]any{"role": "user", "content": "hi"}, map[string]any{"role": "assistant", "content": "hello"}, []any{}, map[string]any{}, []string{}, []map[string]any{}},
		map[string]any{"nilmap": map[string]any(nil), "nilslice": []any(nil), "nilstrs": []string(nil), "nilmaps": []map[string]any(nil), "nilss": map[string]string(nil), "nilsl": map[string][]string(nil)},
		map[string]any{"ss": map[string]string{"b": "2", "a": "1", "toJSON": "not called"}, "sl": map[string][]string{"x": {"1", "2"}, "y": nil, "z": {}}},
		[]map[string]any{{"a": 1}, nil, {}},
		[]string{"a", "b\"", "中"},
		map[string]any{"i": int(-3), "i64": int64(1) << 60, "i32": int32(-7), "i16": int16(8), "i8": int8(-9), "u": uint(10), "u64": uint64(math.MaxUint64), "u32": uint32(11), "u16": uint16(12), "u8": uint8(13), "f32": float32(0.1), "neg0": math.Copysign(0, -1), "nan": math.NaN(), "inf": math.Inf(-1), "big": 1e21, "small": 1e-7},
		map[string]any{"01": "not an index", "4294967295": "not an index", "-1": "x", "1.5": "y"},
	}
	for _, g := range direct {
		checkHostJSON(t, r, g, true)
	}
	fallback := []any{
		map[string]any{"0": "index", "a": 1, "10": "ten", "2": "two"},
		map[string]any{"中": 1, "a": 2},
		map[string]any{"a\xff": 1, "a\xfe": 2, "b": 3}, // both keys decode to "a�"
		map[string]any{"toJSON": "a string", "a": 1},
		map[string]any{"toJSON": NativeFunc(func(r *Realm, _ Value, _ []Value) (Value, error) { return StringValue(FromGoString("called")), nil })},
		map[string]any{"f": NativeFunc(func(r *Realm, _ Value, _ []Value) (Value, error) { return Undefined(), nil }), "a": 1},
		map[string]any{"bytes": []byte("abc")},
		map[string]any{"n": json.Number("12.50"), "bad": json.Number("x")},
		map[string]any{"v": StringValue(FromGoString("value")), "o": ObjectValue(r.NewObject())},
		map[string]any{"bad": struct{}{}},
		map[string]any{"deep": []any{map[string]any{"bytes": []byte{1}}}},
		map[string]any{"bad": "a\xffb", "ok": "x"},
	}
	for _, g := range fallback {
		checkHostJSON(t, r, g, false)
	}
}

// TestHostJSONDepth nests Go containers past jsonScanDepth, where the walk
// gives up level by level, and past jsonMaxDepth, the RangeError.
func TestHostJSONDepth(t *testing.T) {
	r := NewRealm()
	for _, n := range []int{jsonScanDepth - 2, jsonScanDepth - 1, jsonScanDepth, jsonScanDepth + 1, 3 * jsonScanDepth} {
		var g any = "leaf"
		for i := range n {
			if i%2 == 0 {
				g = map[string]any{"k": g}
			} else {
				g = []any{g}
			}
		}
		checkHostJSON(t, r, g, n <= jsonScanDepth)
	}
	self := map[string]any{}
	self["self"] = self
	got, _ := hostJSON(t, r, self, false, true)
	require.Contains(t, got, "Maximum call stack size exceeded")
}

// TestHostJSONToJSON checks the walk against toJSON methods it must not
// skip: on Object.prototype, on Array.prototype, on a placeholder's new
// prototype, and returned placeholders.
func TestHostJSONToJSON(t *testing.T) {
	g := map[string]any{"a": []any{map[string]any{"b": 1}}, "c": "d"}
	for _, src := range []string{
		`Object.prototype.toJSON = function (k) { return "O" + k; }`,
		`Array.prototype.toJSON = function (k) { return "A" + k; }`,
		`Object.defineProperty(Object.prototype, "toJSON", { get() { return () => "getter"; } })`,
		`Object.setPrototypeOf(Array.prototype, { toJSON() { return "P"; } })`,
		`Object.setPrototypeOf(X, { toJSON() { return "proto"; } })`,
		`X.toJSON = () => "own"`,
	} {
		r := NewRealm()
		x, err := r.FromGo(g)
		require.NoError(t, err)
		require.NoError(t, r.Global.SetProp(r, r.KeyFromGoString("X"), x))
		runScriptsIn(t, r, src)
		got, err := r.JSONStringify(x)
		require.NoError(t, err)
		y, err := r.FromGo(g)
		require.NoError(t, err)
		materializeDeep(y, 0)
		if strings.Contains(src, "(X") || strings.Contains(src, "X.") {
			// The rule applies to X itself: compare with a materialized X under the same rule.
			r2 := NewRealm()
			y, err = r2.FromGo(g)
			require.NoError(t, err)
			materializeDeep(y, 0)
			require.NoError(t, r2.Global.SetProp(r2, r2.KeyFromGoString("X"), y))
			runScriptsIn(t, r2, src)
			want, err := r2.JSONStringify(y)
			require.NoError(t, err)
			require.Equal(t, want.GoString(), got.GoString(), src)
			continue
		}
		want, err := r.JSONStringify(y)
		require.NoError(t, err)
		require.Equal(t, want.GoString(), got.GoString(), src)
	}

	// A toJSON returning a placeholder: the returned value's own toJSON is
	// not called again, but those of the values inside it are.
	r := NewRealm()
	x, err := r.FromGo(map[string]any{"inner": map[string]any{"deep": map[string]any{"v": 1}}})
	require.NoError(t, err)
	require.NoError(t, r.Global.SetProp(r, r.KeyFromGoString("X"), x))
	got := runScriptsIn(t, r, `
		const inner = X.inner;
		Object.prototype.toJSON = function () { return this === inner ? "tagged" : this; };
		JSON.stringify({ w: X });`)
	require.Equal(t, `{"w":{"inner":"tagged"}}`, got)

	// A toJSON returning an untouched placeholder: the values inside it
	// still have theirs called.
	r = NewRealm()
	y, err := r.FromGo(map[string]any{"inner": map[string]any{"deep": map[string]any{"v": 1}}, "a": 1})
	require.NoError(t, err)
	require.NoError(t, r.Global.SetProp(r, r.KeyFromGoString("Y"), y))
	got = runScriptsIn(t, r, `
		const X = {};
		Object.prototype.toJSON = function (k) { return this === X ? Y : k === "deep" ? "T" : this; };
		JSON.stringify(X);`)
	require.Equal(t, `{"a":1,"inner":{"deep":"T"}}`, got)
}

// TestHostJSONOptions checks that a replacer, a property list and an
// indentation see the placeholder as the object.
func TestHostJSONOptions(t *testing.T) {
	r := NewRealm()
	g := map[string]any{"b": []any{1, map[string]any{"c": "d", "e": "f"}}, "a": "x"}
	for _, src := range []string{
		`JSON.stringify(X, (k, v) => typeof v === "string" ? v.toUpperCase() : v)`,
		`JSON.stringify(X, ["b", "c"])`,
		`JSON.stringify(X, null, 2)`,
		`JSON.stringify(X, null, "--")`,
		`JSON.stringify([X, X])`,
		`JSON.stringify({ x: X, y: X.b })`,
	} {
		x, err := r.FromGo(g)
		require.NoError(t, err)
		require.NoError(t, r.Global.SetProp(r, r.KeyFromGoString("X"), x))
		got := runScriptsIn(t, r, src)
		y, err := r.FromGo(g)
		require.NoError(t, err)
		materializeDeep(y, 0)
		require.NoError(t, r.Global.SetProp(r, r.KeyFromGoString("X"), y))
		want := runScriptsIn(t, r, src)
		require.Equal(t, want, got, src)
	}
}

// TestHostJSONInterrupt interrupts the walk of a large map.
func TestHostJSONInterrupt(t *testing.T) {
	r := NewRealm()
	list := make([]any, 3*interruptStride)
	for i := range list {
		list[i] = map[string]any{"i": i}
	}
	x, err := r.FromGo(map[string]any{"list": list})
	require.NoError(t, err)
	r.Interrupt("stop")
	_, _, err = r.AppendJSON(nil, x)
	var ie *InterruptedError
	require.ErrorAs(t, err, &ie)
	r.ClearInterrupt()
	out, ok, err := r.AppendJSON(nil, x)
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, strings.HasPrefix(string(out), `{"list":[{"i":0},{"i":1},`))
	require.True(t, isPlaceholder(x))
}

// TestHostJSONLengthLimit checks the length limit on the walk of host
// values: with the limit lowered, a value of each shape whose text passes
// it fails with the RangeError, as the same text built in JavaScript does,
// and leaves dst as it was, a map[string]string whose closing brace is the
// byte past the limit included; one of exactly the limit is written. The
// walk stops at the first string past the limit, on both of quoteGo's
// paths (an invalid byte takes the second): 1,000 strings of 10,000 bytes,
// as a map[string]string, a []string or a map[string]any, allocate for the
// limit, not for 10 MB.
func TestHostJSONLengthLimit(t *testing.T) {
	lowerMaxStringLength(t, 1<<16)
	r := NewRealm()
	big := strings.Repeat("Q", 10000)
	strs := func(n int, s string) map[string]string {
		m := make(map[string]string, n)
		for i := range n {
			m[fmt.Sprintf("k%03d", i)] = s
		}
		return m
	}
	list := make([]string, 10)
	anyList := make([]any, 10)
	anyMap := map[string]any{}
	for i := range list {
		list[i], anyList[i], anyMap[fmt.Sprintf("k%d", i)] = big, big, big
	}
	for _, c := range []struct {
		name string
		v    any
	}{
		{"map[string]string", strs(10, big)},
		{"map[string]string nested", map[string]any{"h": strs(10, big)}},
		{"[]string", list},
		{"[]string nested", map[string]any{"l": list}},
		{"map[string][]string", map[string][]string{"a": list[:5], "b": list[5:]}},
		{"map[string]any", anyMap},
		{"[]any", anyList},
		{"the closing brace", map[string]string{"k": strings.Repeat("Q", 65529)}}, // {"k":"…"}: 65,537 bytes
	} {
		p, err := r.FromGo(c.v)
		require.NoError(t, err)
		out, ok, err := r.AppendJSON(append(make([]byte, 0, 1<<17), "pre:"...), p)
		require.EqualError(t, err, "RangeError: Invalid string length", c.name)
		require.False(t, ok, c.name)
		require.Equal(t, "pre:", string(out), c.name)
	}
	p, err := r.FromGo(map[string]string{"k": strings.Repeat("Q", 65528)})
	require.NoError(t, err)
	out, ok, err := r.AppendJSON(nil, p)
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, out, 1<<16)
	thousand, anyThousand := make([]string, 1000), make(map[string]any, 1000)
	for i := range thousand {
		thousand[i], anyThousand[fmt.Sprintf("k%03d", i)] = big, big
	}
	for _, c := range []struct {
		name string
		v    any
	}{
		{"map[string]string", strs(1000, big)},
		{"map[string]string, an invalid byte", strs(1000, big[1:]+"\xff")},
		{"[]string", thousand},
		{"map[string]any", anyThousand},
	} {
		p, err := r.FromGo(c.v)
		require.NoError(t, err)
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		_, _, err = r.AppendJSON(nil, p)
		runtime.ReadMemStats(&after)
		require.EqualError(t, err, "RangeError: Invalid string length", c.name)
		require.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(1<<20), c.name)
	}
}

// TestHostJSONScratchKept checks that the walk keeps the pair scratch its
// nested maps grew: writing messages whose content holds maps of maps costs
// the same allocations for 1,000 messages as for 10 (it cost one more a
// message when each map put back the scratch it had started with).
func TestHostJSONScratchKept(t *testing.T) {
	r := NewRealm()
	allocs := func(n int) float64 {
		msgs := make([]any, n)
		for i := range msgs {
			msgs[i] = map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": "hi"},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://x/y.png", "detail": "auto"}},
			}}
		}
		x, err := r.FromGo(map[string]any{"model": "m", "messages": msgs})
		require.NoError(t, err)
		var buf []byte
		return testing.AllocsPerRun(10, func() {
			buf, _, err = r.AppendJSON(buf[:0], x)
		})
	}
	require.Equal(t, allocs(10), allocs(1000))
	// What the scratch keeps is cleared, and one grown past
	// maxRetainedPairs is dropped.
	s := r.hostHeap().anyPairs
	require.NotZero(t, cap(s))
	require.Equal(t, make([]hostPair[any], cap(s)), s[:cap(s)])
	wide := make(map[string]any, 2*maxRetainedPairs)
	for i := range 2 * maxRetainedPairs {
		wide[fmt.Sprintf("k%04d", i)] = i
	}
	x, err := r.FromGo(wide)
	require.NoError(t, err)
	_, _, err = r.AppendJSON(nil, x)
	require.NoError(t, err)
	require.LessOrEqual(t, cap(r.hostHeap().anyPairs), maxRetainedPairs)
}

// TestHostJSONGiveUpAfterGrowth checks a map that gives up below one whose
// earlier member grew the scratch: n's member a (40 keys) moves the scratch
// to a larger array, b gives up on its "toJSON" key and leaves the scratch
// at length 0, and n, whose entries start past the scratch's start, keeps
// its own array. The value is written as JSONStringify writes it once
// materialized, and the scratch is empty afterwards, with the scratch cold
// and warm.
func TestHostJSONGiveUpAfterGrowth(t *testing.T) {
	r := NewRealm()
	big := make(map[string]any, 40)
	for i := range 40 {
		big[fmt.Sprintf("k%02d", i)] = float64(i)
	}
	g := map[string]any{"n": map[string]any{"a": big, "b": map[string]any{"toJSON": "x", "q": 1.5}}, "z": "end"}
	for range 2 {
		p, err := r.FromGo(g)
		require.NoError(t, err)
		out, ok, err := r.AppendJSON(nil, p)
		require.NoError(t, err)
		require.True(t, ok)
		want, err := r.JSONStringify(p)
		require.NoError(t, err)
		require.Equal(t, want.GoString(), string(out))
		require.Contains(t, string(out), `"toJSON":"x"`)
		require.Empty(t, r.hostHeap().anyPairs)
	}
}

// TestHostJSONPartlyRead serializes a node JavaScript materialized whose
// children it did not touch, and children it modified.
func TestHostJSONPartlyRead(t *testing.T) {
	g := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "a"}, map[string]any{"role": "assistant", "content": "b"}}, "n": 2}
	for _, src := range []string{
		`X.messages.length`,
		`X.messages[1].content = "changed"`,
		`X.messages.push({ role: "tool" })`,
		`X.extra = { z: [1] }`,
		`delete X.n`,
	} {
		r := NewRealm()
		x, err := r.FromGo(g)
		require.NoError(t, err)
		require.NoError(t, r.Global.SetProp(r, r.KeyFromGoString("X"), x))
		runScriptsIn(t, r, src)
		got := runScriptsIn(t, r, `JSON.stringify(X)`)
		r2 := NewRealm()
		y, err := r2.FromGo(g)
		require.NoError(t, err)
		materializeDeep(y, 0)
		require.NoError(t, r2.Global.SetProp(r2, r2.KeyFromGoString("X"), y))
		runScriptsIn(t, r2, src)
		require.Equal(t, runScriptsIn(t, r2, `JSON.stringify(X)`), got, src)
	}
}

// TestJSONStringifyToJSONLookups checks the toJSON lookups resolve skips for
// plain objects and arrays: an own toJSON, one a toJSON adds to a prototype
// halfway through (the epoch), on a class prototype, a dictionary-mode
// object, a Date, an object with a null prototype.
func TestJSONStringifyToJSONLookups(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`JSON.stringify({ a: { toJSON() { return 1; } }, b: [{ toJSON: () => 2 }] })`, `{"a":1,"b":[2]}`},
		{`JSON.stringify({ a: { toJSON() { Object.prototype.toJSON = function () { return "P"; }; return 1; } }, b: { c: 1 }, d: [{ e: 2 }] })`, `{"a":1,"b":"P","d":"P"}`},
		{`JSON.stringify([{ toJSON() { Array.prototype.toJSON = () => "A"; return 1; } }, [1], { x: [2] }])`, `[1,"A",{"x":"A"}]`},
		{`class C { toJSON() { return "C"; } } JSON.stringify({ c: new C(), d: [new C()] })`, `{"c":"C","d":["C"]}`},
		{`const o = {}; for (let i = 0; i < 100; i++) o["k" + i] = i; o.toJSON = () => "dict"; JSON.stringify({ o })`, `{"o":"dict"}`},
		{`JSON.stringify({ d: new Date(0) })`, `{"d":"1970-01-01T00:00:00.000Z"}`},
		{`const n = Object.create(null); n.toJSON = () => "null proto"; JSON.stringify([n])`, `["null proto"]`},
		{`Object.defineProperty(Object.prototype, "toJSON", { get() { return () => "G"; }, configurable: true }); JSON.stringify({ a: [1] })`, `"G"`},
		{`JSON.stringify({ a: 1 }); Object.prototype.toJSON = () => "later"; JSON.stringify({ a: 1 })`, `"later"`},
	} {
		require.Equal(t, c.want, runScripts(t, c.src), c.src)
	}
}
