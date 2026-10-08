package engine

import (
	"encoding/json"
	"math"
	"math/big"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFromGoScalars(t *testing.T) {
	r := NewRealm()
	cases := []struct {
		in   any
		want Value
	}{
		{nil, Null()},
		{true, True()},
		{false, False()},
		{int(3), IntValue(3)},
		{int8(-3), IntValue(-3)},
		{int16(300), IntValue(300)},
		{int32(70000), IntValue(70000)},
		{int64(1 << 40), Int64Value(1 << 40)},
		{uint(5), IntValue(5)},
		{uint8(255), IntValue(255)},
		{uint16(65535), IntValue(65535)},
		{uint32(4294967295), NumberValue(4294967295)},
		{uint64(1 << 63), NumberValue(9223372036854775808)},
		{float32(1.5), NumberValue(1.5)},
		{2.25, NumberValue(2.25)},
		{json.Number("12"), IntValue(12)},
		{json.Number("1.5e3"), NumberValue(1500)},
		{json.Number("1e400"), NumberValue(math.Inf(1))},
		{IntValue(9), IntValue(9)},
	}
	for _, c := range cases {
		got, err := r.FromGo(c.in)
		require.NoError(t, err, "%v", c.in)
		assert.True(t, SameValue(c.want, got), "%v: got %v", c.in, got)
	}
	s, err := r.FromGo("héllo")
	require.NoError(t, err)
	assert.Equal(t, "héllo", s.AsString().GoString())
	_, err = r.FromGo(json.Number("abc"))
	assert.Error(t, err)
	_, err = r.FromGo(make(chan int))
	assert.ErrorContains(t, err, "unsupported Go type chan int")
	o := r.NewObject()
	ov, _ := r.FromGo(o)
	assert.Same(t, o, ov.AsObject())
	fv, err := r.FromGo(NativeFunc(func(*Realm, Value, []Value) (Value, error) { return IntValue(1), nil }))
	require.NoError(t, err)
	assert.True(t, IsCallable(fv))
	fv2, err := r.FromGo(func(*Realm, Value, []Value) (Value, error) { return IntValue(2), nil })
	require.NoError(t, err)
	res, _ := r.Call(fv2, Undefined(), nil)
	assert.Equal(t, IntValue(2), res)
}

func TestFromGoMapsAndSlices(t *testing.T) {
	r := NewRealm()
	v, err := r.FromGo(map[string]any{
		"zeta":  1,
		"alpha": "a",
		"200":   "idx",
		"3":     "three",
		"nested": map[string]any{
			"b": []any{1, "two", nil, true, map[string]any{"k": 1.5}},
			"a": []string{"x", "y"},
		},
		"rows": []map[string]any{{"id": 1}, {"id": 2}},
	})
	require.NoError(t, err)
	o := v.AsObject()
	assert.Equal(t, []string{"3", "200", "alpha", "nested", "rows", "zeta"}, keyNames(o.OwnEnumerableStringKeys()))
	nested, _ := o.GetProp(r, key(r, "nested"))
	assert.Equal(t, []string{"a", "b"}, keyNames(nested.AsObject().OwnEnumerableStringKeys()))
	b, _ := nested.AsObject().GetProp(r, key(r, "b"))
	require.True(t, IsArray(b))
	assert.Equal(t, uint32(5), b.AsObject().ArrayLength())
	assert.True(t, b.AsObject().IsDenseArray())
	e2, _ := b.AsObject().GetIndex(r, 2)
	assert.True(t, e2.IsNull())
	rows, _ := o.GetProp(r, key(r, "rows"))
	assert.Equal(t, uint32(2), rows.AsObject().ArrayLength())
	row1, _ := rows.AsObject().GetIndex(r, 1)
	id, _ := row1.AsObject().GetProp(r, key(r, "id"))
	assert.Equal(t, IntValue(2), id)
	idx, _ := o.GetIndex(r, 200)
	assert.Equal(t, "idx", idx.AsString().GoString())

	// nil map => null; empty map => {}.
	var nilMap map[string]any
	nv, _ := r.FromGo(nilMap)
	assert.True(t, nv.IsNull())
	ev, _ := r.FromGo(map[string]any{})
	assert.Empty(t, ev.AsObject().OwnPropertyKeys())
	// Large maps go to dictionary mode but keep sorted order.
	bigMap := map[string]any{}
	for i := range 70 {
		bigMap["k"+NumberToGoString(float64(1000+i))] = i
	}
	bv, err := r.FromGo(bigMap)
	require.NoError(t, err)
	assert.True(t, bv.AsObject().IsDictionaryMode())
	keys := keyNames(bv.AsObject().OwnEnumerableStringKeys())
	assert.Len(t, keys, 70)
	assert.Equal(t, "k1000", keys[0])
	assert.Equal(t, "k1069", keys[69])
}

func TestFromGoShapeCache(t *testing.T) {
	r := NewRealm()
	a, err := r.FromGo(map[string]any{"model": "x", "prompt": "p", "n": 1})
	require.NoError(t, err)
	b, err := r.FromGo(map[string]any{"n": 2, "prompt": "q", "model": "y"})
	require.NoError(t, err)
	assert.Same(t, a.AsObject().Shape(), b.AsObject().Shape())
	c, _ := r.FromGo(map[string]any{"model": "x", "prompt": "p"})
	assert.NotSame(t, a.AsObject().Shape(), c.AsObject().Shape())
	// Index keys do not participate in the named shape.
	d, _ := r.FromGo(map[string]any{"model": "x", "prompt": "p", "n": 1, "7": true})
	assert.Same(t, a.AsObject().Shape(), d.AsObject().Shape())
	assert.True(t, d.AsObject().HasOwnProperty(IndexKey(7)))
	// Built shapes are the same as those reached by ordinary assignment in
	// sorted order.
	m := r.NewObject()
	mustSet(t, r, m, "model", str("x"))
	mustSet(t, r, m, "n", IntValue(1))
	mustSet(t, r, m, "prompt", str("p"))
	assert.Same(t, a.AsObject().Shape(), m.Shape())
	// Values land in the right slots.
	mv, _ := a.AsObject().GetProp(r, key(r, "model"))
	assert.Equal(t, "x", mv.AsString().GoString())
	nv, _ := b.AsObject().GetProp(r, key(r, "n"))
	assert.Equal(t, IntValue(2), nv)
}

// TestFromGoDeepValues: the lazy conversion converts one level per touch, so
// a deeply nested Go value converts without a depth limit (eager conversion
// failed past 128 levels with ErrFromGoDepth) and walks to its leaf.
func TestFromGoDeepValues(t *testing.T) {
	r := NewRealm()
	var v any = "leaf"
	for range 20000 {
		v = []any{v}
	}
	cur := mustFromGo(t, r, v)
	for range 20000 {
		require.True(t, IsArray(cur))
		cur, _ = cur.AsObject().GetIndex(r, 0)
	}
	assert.Equal(t, "leaf", cur.AsString().GoString())
	v = "leaf"
	for range 130 {
		v = map[string]any{"c": v}
	}
	cur = mustFromGo(t, r, v)
	for range 130 {
		cur, _ = cur.AsObject().GetProp(r, key(r, "c"))
	}
	assert.Equal(t, "leaf", cur.AsString().GoString())
}

func TestToGoExportRules(t *testing.T) {
	r := NewRealm()
	b, _ := NewBigIntFromDecimal("5")
	cases := []struct {
		in   Value
		want any
	}{
		{Undefined(), nil},
		{Null(), nil},
		{True(), true},
		{IntValue(3), int64(3)},
		{NumberValue(math.Copysign(0, -1)), math.Copysign(0, -1)},
		{NumberValue(1.5), 1.5},
		{NumberValue(9007199254740993), int64(9007199254740992)},
		{NumberValue(1e19), 1e19},
		{NumberValue(-9223372036854775808), int64(math.MinInt64)},
		{NumberValue(math.Inf(1)), math.Inf(1)},
		{str("s"), "s"},
		{BigIntValue(b), big.NewInt(5)},
		{SymbolValue(SymIterator), SymIterator},
	}
	for _, c := range cases {
		got := r.ToGo(c.in)
		if f, ok := c.want.(float64); ok && math.IsInf(f, 0) {
			assert.Equal(t, f, got)
			continue
		}
		assert.Equal(t, c.want, got, "%v", c.in)
	}
	nan := r.ToGo(NaN())
	assert.True(t, math.IsNaN(nan.(float64)))
	// Functions export as the *Object itself.
	assert.Same(t, r.ArrayCtor, r.ToGo(ObjectValue(r.ArrayCtor)))
	// Wrappers export their primitive.
	w, _ := r.ToObject(str("wrapped"))
	assert.Equal(t, "wrapped", r.ToGo(ObjectValue(w)))
	n, _ := r.ToObject(NumberValue(2.5))
	assert.Equal(t, 2.5, r.ToGo(ObjectValue(n)))
	// Error objects export their enumerable own props only (none by default).
	assert.Equal(t, map[string]any{}, r.ToGo(ObjectValue(r.NewError(KindError, "x"))))
}

func TestToGoObjectsAndCycles(t *testing.T) {
	r := NewRealm()
	o := r.NewObject()
	mustSet(t, r, o, "b", IntValue(1))
	mustSet(t, r, o, "a", str("x"))
	mustSet(t, r, o, "0", True())
	o.DefineOwnDataFast(r, key(r, "hidden"), IntValue(9), attrHidden)
	arr := r.NewArray(IntValue(1), Undefined())
	arr.Push(r, ObjectValue(o))
	mustSet(t, r, arr, "5", str("five"))
	mustSet(t, r, o, "arr", ObjectValue(arr))
	got := r.ToGo(ObjectValue(o)).(map[string]any)
	require.Equal(t, 4, len(got))
	assert.Equal(t, true, got["0"])
	assert.Equal(t, int64(1), got["b"])
	assert.Equal(t, "x", got["a"])
	gotArr, ok := got["arr"].([]any)
	require.True(t, ok)
	require.Equal(t, 6, len(gotArr))
	assert.Equal(t, int64(1), gotArr[0])
	assert.Nil(t, gotArr[1])
	cyc, ok := gotArr[2].(map[string]any)
	require.True(t, ok, "cycle exports the already-built container")
	assert.Equal(t, "x", cyc["a"])
	assert.Nil(t, gotArr[3])
	assert.Nil(t, gotArr[4])
	assert.Equal(t, "five", gotArr[5])
	// Getter values are exported through Get.
	g := r.NewObject()
	g.DefineOwnAccessorFast(r, key(r, "v"), r.NewNativeFunction(AtomEmpty, 0,
		func(*Realm, Value, []Value) (Value, error) { return IntValue(7), nil }), nil, attrEnumerable)
	assert.Equal(t, map[string]any{"v": int64(7)}, r.ToGo(ObjectValue(g)))
	// Self-referencing array.
	self := r.NewArray()
	self.Push(r, ObjectValue(self))
	exported := r.ToGo(ObjectValue(self)).([]any)
	assert.Len(t, exported, 1)
	// The cycle points back to the same backing slice.
	inner := exported[0].([]any)
	assert.Equal(t, len(exported), len(inner))
}

func TestFromGoToGoRoundTrip(t *testing.T) {
	r := NewRealm()
	in := map[string]any{
		"model":  "video-1",
		"n":      int64(2),
		"ratio":  0.5,
		"flags":  []any{true, false, nil},
		"nested": map[string]any{"k": "v", "list": []any{int64(1), "2", 3.5}},
		"empty":  map[string]any{},
		"nulls":  nil,
	}
	v, err := r.FromGo(in)
	require.NoError(t, err)
	assertSameGo(t, in, r.ToGo(v))
	assert.Equal(t, in, walkToGo(r, v))
	// Reading does not modify: the materialized tree still exports in.
	assertSameGo(t, in, r.ToGo(v))
	// A write exports a snapshot of the written node; its unmodified
	// children still export their Go values.
	mustSet(t, r, v.AsObject(), "n", IntValue(3))
	got := r.ToGo(v).(map[string]any)
	assert.Equal(t, int64(3), got["n"])
	assertSameGo(t, in["nested"], got["nested"])
	// JSON-decoded input (float64 numbers) converts integral values to
	// numbers that export as int64 once a node is snapshotted.
	var decoded any
	require.NoError(t, json.Unmarshal([]byte(`{"a":1,"b":1.5,"c":[1,2.5,"x",null,{"d":true}]}`), &decoded))
	v, err = r.FromGo(decoded)
	require.NoError(t, err)
	assertSameGo(t, decoded, r.ToGo(v))
	assert.Equal(t, map[string]any{
		"a": int64(1), "b": 1.5,
		"c": []any{int64(1), 2.5, "x", nil, map[string]any{"d": true}},
	}, walkToGo(r, v))
	assertSameGo(t, decoded, r.ToGo(v))
}

func TestFromGoHostArgumentTypes(t *testing.T) {
	r := NewRealm()
	in := map[string]any{
		"params":         map[string]string{"z": "last", "a": "first", "7": "idx"},
		"query":          map[string][]string{"tag": {"x", "y"}, "empty": {}},
		"files":          []map[string]any{{"name": "a.png", "size": int64(3)}, {"name": "b.png", "size": int64(4)}},
		"requestHeaders": map[string]string{"Content-Type": "application/json"},
		"count":          int(2),
		"big":            int64(1 << 40),
		"ratio":          0.25,
		"ok":             true,
		"nothing":        nil,
	}
	v, err := r.FromGo(in)
	require.NoError(t, err)
	o := v.AsObject()
	params, _ := o.GetProp(r, key(r, "params"))
	assert.Equal(t, []string{"7", "a", "z"}, keyNames(params.AsObject().OwnEnumerableStringKeys()))
	pa, _ := params.AsObject().GetProp(r, key(r, "a"))
	assert.Equal(t, "first", pa.AsString().GoString())
	query, _ := o.GetProp(r, key(r, "query"))
	assert.Equal(t, []string{"empty", "tag"}, keyNames(query.AsObject().OwnEnumerableStringKeys()))
	tag, _ := query.AsObject().GetProp(r, key(r, "tag"))
	require.True(t, IsArray(tag))
	assert.Equal(t, uint32(2), tag.AsObject().ArrayLength())
	t1, _ := tag.AsObject().GetIndex(r, 1)
	assert.Equal(t, "y", t1.AsString().GoString())
	files, _ := o.GetProp(r, key(r, "files"))
	require.True(t, IsArray(files))
	f0, _ := files.AsObject().GetIndex(r, 0)
	f1, _ := files.AsObject().GetIndex(r, 1)
	assert.Same(t, f0.AsObject().Shape(), f1.AsObject().Shape(), "rows share a shape")
	// map[string]string objects share the shape of an equal-keyed map[string]any.
	hdr, _ := o.GetProp(r, key(r, "requestHeaders"))
	anyHdr, _ := r.FromGo(map[string]any{"Content-Type": "text/plain"})
	assert.Same(t, hdr.AsObject().Shape(), anyHdr.AsObject().Shape())
	// Reads leave the argument unmodified: it exports as itself.
	assertSameGo(t, in, r.ToGo(v))
	// The conversion's content, walked through [[Get]], is plain Go types.
	assert.Equal(t, map[string]any{
		"params":         map[string]any{"7": "idx", "a": "first", "z": "last"},
		"query":          map[string]any{"tag": []any{"x", "y"}, "empty": []any{}},
		"files":          []any{map[string]any{"name": "a.png", "size": int64(3)}, map[string]any{"name": "b.png", "size": int64(4)}},
		"requestHeaders": map[string]any{"Content-Type": "application/json"},
		"count":          int64(2),
		"big":            int64(1 << 40),
		"ratio":          0.25,
		"ok":             true,
		"nothing":        nil,
	}, walkToGo(r, v))
	// A written node exports as map[string]any; its siblings stay typed.
	mustSet(t, r, params.AsObject(), "a", str("changed"))
	out := r.ToGo(v).(map[string]any)
	assert.Equal(t, map[string]any{"7": "idx", "a": "changed", "z": "last"}, out["params"])
	assertSameGo(t, in["query"], out["query"])
	assertSameGo(t, in["files"], out["files"])
	assertSameGo(t, in["requestHeaders"], out["requestHeaders"])
	// nil typed maps become null.
	var nilStrMap map[string]string
	nv, _ := r.FromGo(nilStrMap)
	assert.True(t, nv.IsNull())
	var nilQuery map[string][]string
	nv, _ = r.FromGo(nilQuery)
	assert.True(t, nv.IsNull())
}

// TestFromGoPlanMirrorsBuild drives every branch of FromGo's conversion in
// one value (it began as the check that the eager conversion's two walks
// agreed): nested maps under index and named keys, typed maps, nil and empty
// containers, a dictionary-mode map and the pair scratch reused across
// materializations.
func TestFromGoPlanMirrorsBuild(t *testing.T) {
	r := NewRealm()
	big := map[string]any{}
	for i := range 70 {
		big["k"+NumberToGoString(float64(1000+i))] = i
	}
	var nilAny map[string]any
	var nilStr map[string]string
	var nilList map[string][]string
	in := map[string]any{
		"10":    "index key first",
		"z":     map[string]any{},
		"a":     map[string]string{"b": "", "a": "x", "3": "idx"},
		"lists": map[string][]string{"q": {"", "y"}, "p": nil, "2": {"two"}},
		"rows":  []map[string]any{{"id": 1, "m": map[string]any{"deep": []any{"s", nil, nilAny}}}, nil, {}},
		"mixed": []any{nilAny, nilStr, nilList, map[string]any{"c": []string{"", "e"}}, []string(nil), json.Number("7"), IntValue(5)},
		"big":   big,
		"n":     nil,
		"tail":  "end",
	}
	want := map[string]any{
		"10":    "index key first",
		"z":     map[string]any{},
		"a":     map[string]any{"3": "idx", "a": "x", "b": ""},
		"lists": map[string]any{"2": []any{"two"}, "p": []any{}, "q": []any{"", "y"}},
		"rows":  []any{map[string]any{"id": int64(1), "m": map[string]any{"deep": []any{"s", nil, nil}}}, nil, map[string]any{}},
		"mixed": []any{nil, nil, nil, map[string]any{"c": []any{"", "e"}}, []any{}, int64(7), int64(5)},
		"big":   walkToGo(r, mustFromGo(t, r, big)).(map[string]any),
		"n":     nil,
		"tail":  "end",
	}
	for range 3 { // the realm's scratch is reused between calls
		got := mustFromGo(t, r, in)
		assert.Equal(t, want, walkToGo(r, got))
		assertSameGo(t, in, r.ToGo(got))
		assert.Equal(t, []string{"10", "a", "big", "lists", "mixed", "n", "rows", "tail", "z"}, keyNames(got.AsObject().OwnEnumerableStringKeys()))
	}
	// A value that cannot be converted fails where it is read, not at the
	// call; the rest of the argument is usable and the scratch is reset.
	bad := mustFromGo(t, r, map[string]any{"a": map[string]any{"bad": complex(1, 2), "ok": 1}, "b": "x"})
	a, err := bad.AsObject().GetProp(r, key(r, "a"))
	require.NoError(t, err)
	okv, err := a.AsObject().GetProp(r, key(r, "ok"))
	require.NoError(t, err)
	assert.Equal(t, IntValue(1), okv)
	_, err = a.AsObject().GetProp(r, key(r, "bad"))
	assert.Error(t, err)
	_, err = r.ToGoStrict(bad)
	assert.Error(t, err, "the snapshot export reads the throwing property")
	assert.Equal(t, want, walkToGo(r, mustFromGo(t, r, in)))
}

func mustFromGo(t *testing.T, r *Realm, v any) Value {
	t.Helper()
	out, err := r.FromGo(v)
	require.NoError(t, err)
	return out
}

// walkToGo exports v by ToGo's rules but reads every container through
// [[Get]], materializing host nodes instead of returning the Go values they
// came from, so it checks what the conversion produced.
func walkToGo(r *Realm, v Value) any {
	if !v.IsObject() {
		return r.ToGo(v)
	}
	o := v.AsObject()
	switch o.Class() {
	case ClassArray:
		out := make([]any, o.ArrayLength())
		for i := range out {
			e, _ := o.GetIndex(r, uint32(i))
			out[i] = walkToGo(r, e)
		}
		return out
	case ClassObject:
		keys := o.OwnEnumerableStringKeys()
		out := make(map[string]any, len(keys))
		for _, k := range keys {
			e, _ := o.GetProp(r, k)
			out[k.GoString()] = walkToGo(r, e)
		}
		return out
	}
	return r.ToGo(v)
}

// assertSameGo asserts that got is the Go map or slice want itself, not an
// equal copy.
func assertSameGo(t *testing.T, want, got any) {
	t.Helper()
	require.NotNil(t, got)
	require.Equal(t, reflect.TypeOf(want), reflect.TypeOf(got))
	assert.Equal(t, reflect.ValueOf(want).Pointer(), reflect.ValueOf(got).Pointer(), "same %T", want)
	if reflect.TypeOf(want).Kind() == reflect.Slice {
		assert.Equal(t, reflect.ValueOf(want).Len(), reflect.ValueOf(got).Len())
	}
}

// TestFromGoSlabAllocations guards the arena: a nested JSON-shaped argument
// costs one slab per kind, not one allocation per node.
func TestFromGoSlabAllocations(t *testing.T) {
	r := NewRealm()
	arg := map[string]any{
		"model": "m", "n": 2.0, "stream": false,
		"headers": map[string]any{"A": "1", "B": "2", "C": "3"},
		"body": map[string]any{"input": []any{
			map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "héllo"}}},
			"plain",
		}},
	}
	mustFromGo(t, r, arg) // warm the shape cache and the scratch
	allocs := testing.AllocsPerRun(20, func() {
		if _, err := r.FromGo(arg); err != nil {
			t.Fatal(err)
		}
	})
	// objects, arrays, values and strings slabs plus the UTF-16 payload of "héllo".
	assert.LessOrEqual(t, allocs, 5.0)
}

// TestInternDoesNotPublishCallerString checks that the process-wide intern
// table keeps its own String header, so a header carved from a FromGo slab
// never pins the slab.
func TestInternDoesNotPublishCallerString(t *testing.T) {
	r := NewRealm()
	name := "intern_probe_" + NumberToGoString(float64(len(r.internCacheASCII)))
	s := asciiString(name)
	a := r.Intern(s)
	assert.NotSame(t, s, a)
	assert.True(t, a.IsInterned())
	assert.Same(t, a, r.InternGoString(name))
	u := FromGoString(name + "é")
	ua := r.Intern(u)
	assert.NotSame(t, u, ua)
	assert.Same(t, ua, r.Intern(FromGoString(name+"é")))
}

// TestToGoSharedReferencesAndWideGraphs covers the export scratch: a
// container referenced twice exports as one Go value (with the inline seen
// list and after it spills into the map), and a getter that mutates the
// holder during export falls back to [[Get]].
func TestToGoSharedReferencesAndWideGraphs(t *testing.T) {
	r := NewRealm()
	shared := r.NewObject()
	shared.DefineOwnDataFast(r, key(r, "x"), IntValue(1), attrDefault)
	root := r.NewObject()
	var items []Value
	for i := range 12 {
		o := r.NewObject()
		o.DefineOwnDataFast(r, key(r, "i"), IntValue(i), attrDefault)
		o.DefineOwnDataFast(r, key(r, "s"), ObjectValue(shared), attrDefault)
		items = append(items, ObjectValue(o))
	}
	root.DefineOwnDataFast(r, key(r, "list"), ObjectValue(r.NewArrayFromSlice(items)), attrDefault)
	root.DefineOwnDataFast(r, key(r, "first"), items[0], attrDefault)
	root.DefineOwnDataFast(r, key(r, "shared"), ObjectValue(shared), attrDefault)

	out, err := r.ToGoStrict(ObjectValue(root))
	require.NoError(t, err)
	m := out.(map[string]any)
	list := m["list"].([]any)
	require.Len(t, list, 12)
	sharedOut := m["shared"].(map[string]any)
	assert.Equal(t, int64(1), sharedOut["x"])
	for i, item := range list {
		im := item.(map[string]any)
		assert.Equal(t, int64(i), im["i"])
		assert.Equal(t, reflect.ValueOf(sharedOut).Pointer(), reflect.ValueOf(im["s"]).Pointer(), "item %d shares the exported map", i)
	}
	assert.Equal(t, reflect.ValueOf(list[0]).Pointer(), reflect.ValueOf(m["first"]).Pointer())

	// A getter that adds a property while the object is being exported: the
	// keys are the snapshot taken first, the values are read through [[Get]].
	holder := r.NewObject()
	holder.DefineOwnDataFast(r, key(r, "a"), IntValue(1), attrDefault)
	getter := r.NewNativeFunction(AtomEmpty, 0, func(r *Realm, this Value, _ []Value) (Value, error) {
		this.AsObject().DefineOwnDataFast(r, key(r, "late"), IntValue(3), attrDefault)
		return IntValue(2), nil
	})
	holder.DefineOwnAccessorFast(r, key(r, "b"), getter, nil, attrEnumerable|attrConfigurable)
	holder.DefineOwnDataFast(r, key(r, "c"), IntValue(4), attrDefault)
	out, err = r.ToGoStrict(ObjectValue(holder))
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"a": int64(1), "b": int64(2), "c": int64(4)}, out)
}

// TestFromGoIndexKeysNestedMaps is an audit reproducer: the pre-walk
// recorded nested maps in sorted key order while the build converted the
// named keys first and the canonical-index keys last, so a map holding a
// nested map under both an index-like key and a named key had the two
// swapped ({"0": {x:1}, "size": {w:2}} built {"0": {w:2}, "size": {x:1}}).
// Both walks now share one order (orderPairs).
func TestFromGoIndexKeysNestedMaps(t *testing.T) {
	r := NewRealm()
	roundTrip := func(in any) any {
		t.Helper()
		v, err := r.FromGo(in)
		require.NoError(t, err)
		assertSameGo(t, in, r.ToGo(v))
		return walkToGo(r, v)
	}
	in := map[string]any{
		"0":    map[string]any{"x": 1},
		"size": map[string]any{"w": 2},
	}
	want := map[string]any{"0": map[string]any{"x": int64(1)}, "size": map[string]any{"w": int64(2)}}
	require.Equal(t, want, roundTrip(in))

	in2 := map[string]any{
		"1":    map[string]string{"a": "A"},
		"name": map[string]string{"b": "B", "c": "C"},
	}
	want2 := map[string]any{"1": map[string]any{"a": "A"}, "name": map[string]any{"b": "B", "c": "C"}}
	require.Equal(t, want2, roundTrip(in2))

	in3 := map[string]any{
		"2":     []any{map[string]any{"p": 1}},
		"items": []any{map[string]any{"q": 2}},
	}
	want3 := map[string]any{"2": []any{map[string]any{"p": int64(1)}}, "items": []any{map[string]any{"q": int64(2)}}}
	require.Equal(t, want3, roundTrip(in3))

	// Two nesting levels of mixed index-like and named keys, arrays of maps
	// inside maps, and string/list maps whose own keys mix both kinds.
	in4 := map[string]any{
		"0":     map[string]any{"1": map[string]any{"deep": "a"}, "z": map[string]any{"deep": "b"}, "5": "five"},
		"10":    []any{map[string]any{"0": []any{1, map[string]any{"i": "j"}}, "k": "v"}, "s"},
		"alpha": map[string]string{"2": "two", "name": "n", "00": "not an index"},
		"beta":  map[string][]string{"3": {"x"}, "tags": {"y", "z"}},
		"2":     []map[string]any{{"1": map[string]any{"q": 1}, "p": map[string]any{"q": 2}}},
		"gamma": map[string]any{"7": map[string]any{"n": 7}, "6": map[string]any{"n": 6}, "b": map[string]any{"n": "b"}, "a": map[string]any{"n": "a"}},
	}
	want4 := map[string]any{
		"0":     map[string]any{"1": map[string]any{"deep": "a"}, "z": map[string]any{"deep": "b"}, "5": "five"},
		"10":    []any{map[string]any{"0": []any{int64(1), map[string]any{"i": "j"}}, "k": "v"}, "s"},
		"alpha": map[string]any{"2": "two", "name": "n", "00": "not an index"},
		"beta":  map[string]any{"3": []any{"x"}, "tags": []any{"y", "z"}},
		"2":     []any{map[string]any{"1": map[string]any{"q": int64(1)}, "p": map[string]any{"q": int64(2)}}},
		"gamma": map[string]any{"7": map[string]any{"n": int64(7)}, "6": map[string]any{"n": int64(6)}, "b": map[string]any{"n": "b"}, "a": map[string]any{"n": "a"}},
	}
	require.Equal(t, want4, roundTrip(in4))

	// Enumeration order is unchanged: indices ascending, then named keys sorted.
	v, err := r.FromGo(in4["gamma"])
	require.NoError(t, err)
	var keys []string
	for _, k := range v.AsObject().OwnEnumerableStringKeys() {
		keys = append(keys, k.GoString())
	}
	require.Equal(t, []string{"6", "7", "a", "b"}, keys)
}

// TestBufferHostConversion checks the []byte conversions of the native API:
// FromGo wraps the bytes in an ArrayBuffer without copying them and ToGo
// copies the bytes of a buffer or of the part of its buffer a view views.
func TestBufferHostConversion(t *testing.T) {
	const src = `
export function write(b) { new Uint8Array(b)[0] = 9; return [b instanceof ArrayBuffer, b.byteLength, b.resizable].join() }
export function grow(b) { const t = b.transfer(4); return [b.detached, new Uint8Array(t).join()].join() }
export function views() {
	const b = new ArrayBuffer(8, {maxByteLength: 16}); new Uint8Array(b).set([1, 2, 3, 4, 5, 6, 7, 8]);
	const d = new ArrayBuffer(2), dv = new Uint8Array(d); d.transfer();
	return [b, new Uint16Array(b, 2, 2), new DataView(b, 1, 3), new Int8Array(b, 5), new Uint32Array(b),
		new SharedArrayBuffer(2), d, dv, new Uint8Array(0), new Uint16Array(new ArrayBuffer(7, {maxByteLength: 8}))];
}
export function twice() { const b = new ArrayBuffer(1); return [b, b, new Uint8Array(b)] }
export function read(v) { return new Uint8Array(v.buffer ?? v).join() }
export function join(v) { return v.join() }
export function elems() { return [new Int16Array([1, -2]), new Uint8ClampedArray([300]), new Float16Array([1]), new BigUint64Array([2n ** 64n - 1n]), new Float64Array(new ArrayBuffer(16), 8)] }
`
	for _, shared := range []bool{false, true} {
		f := evalModuleWith(t, src, RealmOptions{SharedIntrinsics: shared})
		// FromGo: the buffer is the host's bytes, clipped to their length.
		backing := []byte{1, 2, 3, 0xaa}
		host := backing[:3]
		assert.Equal(t, "true,3,false", f.call("write", host))
		assert.Equal(t, byte(9), host[0], "no copy")
		assert.Equal(t, "true,9,2,3,0", f.call("grow", host))
		assert.Equal(t, byte(0xaa), backing[3], "nothing written past len")
		assert.Equal(t, "true,0,false", f.call("write", []byte(nil)), "nil is an empty buffer")
		nested, err := f.r.FromGo(map[string]any{"b": []byte{7}})
		require.NoError(t, err)
		b, err := nested.AsObject().Get(f.r, key(f.r, "b"), nested)
		require.NoError(t, err)
		assert.Equal(t, ClassArrayBuffer, b.AsObject().Class(), "a nested []byte too")

		// ToGo: copies, empty when detached or out of bounds.
		got := f.call("views").([]any)
		assert.Equal(t, []any{
			[]byte{1, 2, 3, 4, 5, 6, 7, 8}, []byte{3, 4, 5, 6}, []byte{2, 3, 4}, []byte{6, 7, 8}, []byte{1, 2, 3, 4, 5, 6, 7, 8},
			[]byte{0, 0}, []byte(nil), []byte(nil), []byte{}, []byte{0, 0, 0, 0, 0, 0},
		}, got)
		fn, _ := f.env.GetBindingValue("views")
		arr, err := f.r.Call(fn, Undefined(), nil)
		require.NoError(t, err)
		view, err := arr.AsObject().GetIndex(f.r, 1)
		require.NoError(t, err)
		out := f.r.ToGo(view).([]byte)
		out[0] = 0
		assert.Equal(t, "1,2,3,4,5,6,7,8", f.call("read", view), "a copy")
		pair := f.call("twice").([]any)
		require.Len(t, pair, 3)
		assert.Same(t, &pair[0].([]byte)[0], &pair[1].([]byte)[0], "one buffer exports once")
		assert.NotSame(t, &pair[0].([]byte)[0], &pair[2].([]byte)[0])

		// BufferData and TypedArrayElements alias the buffer.
		data, ok := view.AsObject().BufferData()
		require.True(t, ok)
		assert.Equal(t, []byte{3, 4, 5, 6}, data)
		assert.Equal(t, len(data), cap(data))
		data[0] = 30
		assert.Equal(t, "1,2,30,4,5,6,7,8", f.call("read", view))
		_, ok = f.r.ObjectPrototype.BufferData()
		assert.False(t, ok)
		fn, _ = f.env.GetBindingValue("elems")
		arr, err = f.r.Call(fn, Undefined(), nil)
		require.NoError(t, err)
		var elems []any
		for i := range uint32(5) {
			v, err := arr.AsObject().GetIndex(f.r, i)
			require.NoError(t, err)
			e, ok := v.AsObject().TypedArrayElements()
			require.True(t, ok)
			elems = append(elems, e)
		}
		assert.Equal(t, []any{[]int16{1, -2}, []uint8{255}, []uint16{0x3c00}, []uint64{math.MaxUint64}, []float64{0}}, elems)
		elems[0].([]int16)[1] = 5
		v, _ := arr.AsObject().GetIndex(f.r, 0)
		assert.Equal(t, "1,5", f.call("join", v))
		_, ok = f.r.ObjectPrototype.TypedArrayElements()
		assert.False(t, ok)

		// Detach.
		ab, err := f.r.NewArrayBuffer([]byte{1})
		require.NoError(t, err)
		assert.False(t, ab.IsDetachedBuffer())
		require.NoError(t, f.r.DetachArrayBuffer(ObjectValue(ab)))
		assert.True(t, ab.IsDetachedBuffer())
		assert.Nil(t, f.r.ToGo(ObjectValue(ab)).([]byte))
		assert.False(t, f.r.ObjectPrototype.IsDetachedBuffer())
	}
}

// exportDepth counts the containers along the first element or entry of an
// exported value.
func exportDepth(v any) int {
	d := 0
	for {
		switch x := v.(type) {
		case []any:
			d++
			if len(x) == 0 {
				return d
			}
			v = x[0]
		case map[string]any:
			d++
			if len(x) == 0 {
				return d
			}
			for _, e := range x {
				v = e
				break
			}
		default:
			return d
		}
	}
}

// TestToGoDepthLimit checks MaxToGoDepth: values nested to the limit export
// in full, one container more is a RangeError from ToGoStrict and cut to nil
// by ToGo, width does not count, and an array nested millions deep ends in
// the RangeError instead of overflowing the Go stack (3e6 levels crashed the
// process when toGo recursed without a limit).
func TestToGoDepthLimit(t *testing.T) {
	f := evalModule(t, `
export function arrays(n) { let a = []; for (let i = 0; i < n; i++) a = [a]; return a; }
export function objects(n) { let a = {}; for (let i = 0; i < n; i++) a = { a }; return a; }
export function mixed(n) { let a = []; for (let i = 0; i < n; i++) a = [{ a }]; return a; }
export function wide(n) { const o = {}; for (let i = 0; i < n; i++) o["k" + i] = [i]; return o; }
`)
	value := func(name string, n int) Value {
		t.Helper()
		fn, ok := f.env.GetBindingValue(name)
		require.True(t, ok)
		v, err := f.r.Call(fn, Undefined(), []Value{IntValue(n)})
		require.NoError(t, err)
		return v
	}
	limit := MaxToGoDepth
	for _, name := range []string{"arrays", "objects"} {
		out, err := f.r.ToGoStrict(value(name, limit-1))
		require.NoError(t, err, name)
		assert.Equal(t, limit, exportDepth(out), name)
	}

	deep := value("mixed", limit)
	_, err := f.r.ToGoStrict(deep)
	var exc *Exception
	require.ErrorAs(t, err, &exc)
	assert.Equal(t, "RangeError: Maximum export depth exceeded (10000 nested containers)", errMessage(t, err))
	assert.Equal(t, limit, exportDepth(f.r.ToGo(deep)), "the lenient export cuts the deeper containers to nil")

	out, err := f.r.ToGoStrict(value("wide", limit+5))
	require.NoError(t, err)
	assert.Len(t, out, limit+5)

	for _, n := range []int{1_000_000, 3_000_000} {
		v := value("arrays", n)
		_, err := f.r.ToGoStrict(v)
		require.ErrorAs(t, err, &exc, "depth %d", n)
		assert.Equal(t, limit, exportDepth(f.r.ToGo(v)), "depth %d", n)
	}
}

// TestFromGoNaNPayloadCanonicalized checks that every NaN a host passes in
// is canonicalized, at the top level and inside maps and slices: the engine
// reserves the NaN payloads 0x7FFA... for undefined, null, the booleans and
// the hole, so a host NaN with such a payload must still read as NaN. A
// float32 NaN can widen into the reserved prefix too.
func TestFromGoNaNPayloadCanonicalized(t *testing.T) {
	f := evalModule(t, `
const is = (v) => [typeof v, Number.isNaN(v), v === undefined || v === null || v === true || v === false];
export function check(v, o) { return [is(v), is(o.n), is(o.list[0]), is(o.deep.m.n)]; }
`)
	nan := []any{"number", true, false}
	want := []any{nan, nan, nan, nan}
	var values []any
	for _, bits := range []uint64{
		0x7FFA000000000000, // false
		0x7FFA000000000001, // true
		0x7FFA000000000002, // undefined
		0x7FFA000000000003, // null
		0x7FFA000000000004, // hole
		0x7FFA000000000005,
		0x7FFAFFFFFFFFFFFF,
		0xFFFA000000000002,
		0x7FF0000000000001, // signalling
	} {
		values = append(values, math.Float64frombits(bits))
	}
	for _, bits := range []uint32{0x7FD00000, 0x7FD00001, 0x7FDFFFFF, 0xFFD00000} {
		values = append(values, math.Float32frombits(bits))
	}
	for _, x := range values {
		o := map[string]any{"n": x, "list": []any{x}, "deep": map[string]any{"m": map[string]any{"n": x}}}
		assert.Equal(t, want, f.call("check", x, o), "%v", x)
		v, err := f.r.FromGo(x)
		require.NoError(t, err)
		assert.True(t, v.IsNumber())
		got, ok := f.r.ToGo(v).(float64)
		require.True(t, ok)
		assert.True(t, math.IsNaN(got))
	}
}
