package engine

import (
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func jsonParseGo(t *testing.T, r *Realm, text string) Value {
	t.Helper()
	v, err := callMethodErr(r, ObjectValue(r.JSON), "parse", str(text))
	require.NoError(t, err, text)
	return v
}

func jsonStringifyGo(t *testing.T, r *Realm, v Value, extra ...Value) string {
	t.Helper()
	res, err := callMethodErr(r, ObjectValue(r.JSON), "stringify", append([]Value{v}, extra...)...)
	require.NoError(t, err)
	if res.IsUndefined() {
		return "<undefined>"
	}
	return res.AsString().GoString()
}

func TestJSONParseTable(t *testing.T) {
	r := NewRealm()
	cases := []struct {
		text string
		want any
	}{
		{`1`, int64(1)},
		{` -0 `, math.Copysign(0, -1)},
		{`0.1`, 0.1},
		{`1e21`, 1e21},
		{`-1.5E-3`, -0.0015},
		{`123456789012345678`, int64(123456789012345680)},
		{`9007199254740993`, int64(9007199254740992)},
		{`1e400`, math.Inf(1)},
		{`"str"`, "str"},
		{`"\u0041\n\t\"\\\/\b\f\r"`, "A\n\t\"\\/\b\f\r"},
		{`"\ud83d\ude00"`, "😀"},
		{`"\uD83D\uDE00"`, "😀"},
		{`"你好"`, "你好"},
		{`"emoji 😀 raw"`, "emoji 😀 raw"},
		{`true`, true},
		{`false`, false},
		{`null`, nil},
		{`[]`, []any{}},
		{`[1,"a",null,true,[2],{"b":3}]`, []any{int64(1), "a", nil, true, []any{int64(2)}, map[string]any{"b": int64(3)}}},
		{`{}`, map[string]any{}},
		{`{"a":1,"b":{"c":[1,2]}}`, map[string]any{"a": int64(1), "b": map[string]any{"c": []any{int64(1), int64(2)}}}},
		{`{"a":1,"a":2}`, map[string]any{"a": int64(2)}},
		{`{"__proto__":1}`, map[string]any{"__proto__": int64(1)}},
		{`{"1":"one","0":"zero","x":"y"}`, map[string]any{"0": "zero", "1": "one", "x": "y"}},
		{"\t\n\r {\"a\" : [ 1 , 2 ] } \n", map[string]any{"a": []any{int64(1), int64(2)}}},
	}
	for _, c := range cases {
		t.Run(c.text, func(t *testing.T) {
			got := r.ToGo(jsonParseGo(t, r, c.text))
			assert.Equal(t, c.want, got)
		})
	}
	// -0 stays -0 as a JS value.
	nz := jsonParseGo(t, r, "-0")
	assert.True(t, SameValue(nz, NumberValue(negativeZero)))
	// Lone surrogate escapes survive as code units.
	lone := jsonParseGo(t, r, `"\ud800x"`)
	assert.Equal(t, []uint16{0xD800, 'x'}, lone.AsString().UTF16())
	// Lone surrogates in UTF-16 input text survive too.
	loneText := FromUTF16([]uint16{'"', 0xDC00, '"'})
	v, err := callMethodErr(r, ObjectValue(r.JSON), "parse", StringValue(loneText))
	require.NoError(t, err)
	assert.Equal(t, []uint16{0xDC00}, v.AsString().UTF16())
	// Key order follows the text (index keys first, ascending).
	o := jsonParseGo(t, r, `{"z":1,"a":2,"10":3,"2":4}`).AsObject()
	assert.Equal(t, []string{"2", "10", "z", "a"}, keyNames(o.OwnEnumerableStringKeys()))
	// Arrays are dense.
	arr := jsonParseGo(t, r, `[1,2,3]`).AsObject()
	assert.True(t, arr.IsDenseArray())
	assert.Same(t, r.ArrayPrototype, arr.Proto())
	// Non-string input is coerced.
	n, err := callMethodErr(r, ObjectValue(r.JSON), "parse", IntValue(42))
	require.NoError(t, err)
	assert.Equal(t, IntValue(42), n)
}

func TestJSONParseErrors(t *testing.T) {
	r := NewRealm()
	cases := []struct{ text, msg string }{
		{``, "Unexpected end of JSON input"},
		{`{`, "Unexpected end of JSON input"},
		{`[1,`, "Unexpected end of JSON input"},
		{`{"a":1,}`, "Unexpected token } in JSON at position 7"},
		{`[1,]`, "Unexpected token ] in JSON at position 3"},
		{`01`, "Unexpected token 1 in JSON at position 1"},
		{`1.`, "Unexpected end of JSON input"},
		{`.5`, "Unexpected token . in JSON at position 0"},
		{`+1`, "Unexpected token + in JSON at position 0"},
		{`1e`, "Unexpected end of JSON input"},
		{`'a'`, "Unexpected token ' in JSON at position 0"},
		{`"a`, "Unterminated string in JSON at position 2"},
		{"\"a\nb\"", "Bad control character in string literal in JSON at position 2"},
		{`"\x"`, "Bad escaped character in JSON at position 2"},
		{`"\u12"`, "Unexpected end of JSON input"},
		{`"\u12g4"`, "Bad Unicode escape in JSON at position 5"},
		{`tru`, "Unexpected end of JSON input"},
		{`trux`, "Unexpected token x in JSON at position 3"},
		{`nul`, "Unexpected end of JSON input"},
		{`undefined`, "Unexpected token u in JSON at position 0"},
		{`NaN`, "Unexpected token N in JSON at position 0"},
		{`1 2`, "Unexpected token 2 in JSON at position 2"},
		{`{1:2}`, "Unexpected token 1 in JSON at position 1"},
		{`{"a" 1}`, "Unexpected token 1 in JSON at position 5"},
		{`[1 2]`, "Unexpected token 2 in JSON at position 3"},
		{`{"a":1 "b":2}`, `Unexpected token " in JSON at position 7`},
		{`é`, "Unexpected token é in JSON at position 0"},
		{strings.Repeat("[", 513), "JSON nesting too deep"},
		{strings.Repeat(`{"a":`, 513), "JSON nesting too deep"},
	}
	for _, c := range cases {
		t.Run(c.text[:min(len(c.text), 20)], func(t *testing.T) {
			_, err := callMethodErr(r, ObjectValue(r.JSON), "parse", str(c.text))
			assertErrorKind(t, err, KindSyntaxError, c.msg)
		})
	}
	// Depth 512 is fine.
	deep := strings.Repeat("[", 512) + strings.Repeat("]", 512)
	jsonParseGo(t, r, deep)
	// Interrupts are honoured.
	r.Interrupt("stop")
	_, err := callMethodErr(r, ObjectValue(r.JSON), "parse", str("["+strings.Repeat("1,", 5000)+"1]"))
	var ie *InterruptedError
	require.ErrorAs(t, err, &ie)
	r.ClearInterrupt()
}

func TestJSONParseSharesShapes(t *testing.T) {
	r := NewRealm()
	a := jsonParseGo(t, r, `{"model":"m","prompt":"p","size":1}`).AsObject()
	b := jsonParseGo(t, r, `{"model":"x","prompt":"y","size":2}`).AsObject()
	c := jsonParseGo(t, r, `{"prompt":"y","model":"x","size":2}`).AsObject()
	assert.Same(t, a.Shape(), b.Shape(), "same key order shares one *Shape")
	assert.NotSame(t, a.Shape(), c.Shape(), "different key order is a different shape")
	// Same shape as FromGo for the same (sorted) key set.
	g, err := r.FromGo(map[string]any{"model": "m", "prompt": "p", "size": 1})
	require.NoError(t, err)
	assert.Same(t, a.Shape(), g.AsObject().Shape(), "JSON.parse and FromGo share the realm shape tree")
	// Nested objects share too.
	n1 := jsonParseGo(t, r, `{"a":{"x":1,"y":2}}`).AsObject()
	n2 := jsonParseGo(t, r, `{"a":{"x":3,"y":4}}`).AsObject()
	ax, _ := n1.GetProp(r, key(r, "a"))
	bx, _ := n2.GetProp(r, key(r, "a"))
	assert.Same(t, ax.AsObject().Shape(), bx.AsObject().Shape())
	// Objects remain ordinary: extensible, writable, correct prototype.
	assert.Same(t, r.ObjectPrototype, a.Proto())
	require.NoError(t, a.SetProp(r, key(r, "extra"), IntValue(1)))
	assert.Equal(t, []string{"model", "prompt", "size", "extra"}, keyNames(a.OwnEnumerableStringKeys()))
	// Objects with more than 64 keys still work (dictionary mode).
	var sb strings.Builder
	sb.WriteString("{")
	for i := range 70 {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`"k` + NumberToGoString(float64(i)) + `":` + NumberToGoString(float64(i)))
	}
	sb.WriteString("}")
	big := jsonParseGo(t, r, sb.String()).AsObject()
	assert.Len(t, big.OwnEnumerableStringKeys(), 70)
	kv, _ := big.GetProp(r, key(r, "k69"))
	assert.Equal(t, IntValue(69), kv)
}

func TestJSONParseReviver(t *testing.T) {
	r := NewRealm()
	var visits []string
	reviver := ObjectValue(r.NewNativeFunction(AtomEmpty, 2, func(r *Realm, this Value, args []Value) (Value, error) {
		k := args[0].AsString().GoString()
		visits = append(visits, k)
		v := args[1]
		if v.IsNumber() {
			return NumberValue(v.AsNumber() * 10), nil
		}
		if k == "drop" {
			return Undefined(), nil
		}
		// holder is `this`
		if !this.IsObject() {
			return Undefined(), r.TypeError("holder is not an object")
		}
		return v, nil
	}))
	v, err := callMethodErr(r, ObjectValue(r.JSON), "parse", str(`{"a":1,"b":[2,{"c":3}],"drop":"x","s":"t"}`), reviver)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"a": int64(10), "b": []any{int64(20), map[string]any{"c": int64(30)}}, "s": "t"}, r.ToGo(v))
	assert.Equal(t, []string{"a", "0", "c", "1", "b", "drop", "s", ""}, visits)
	// The root holder is {"": value}; returning undefined at the root yields undefined.
	drop := ObjectValue(r.NewNativeFunction(AtomEmpty, 2, func(*Realm, Value, []Value) (Value, error) { return Undefined(), nil }))
	v, err = callMethodErr(r, ObjectValue(r.JSON), "parse", str(`[1]`), drop)
	require.NoError(t, err)
	assert.True(t, v.IsUndefined())
	// Errors in the reviver propagate.
	thrower := ObjectValue(r.NewNativeFunction(AtomEmpty, 2, func(r *Realm, this Value, args []Value) (Value, error) {
		return Undefined(), r.RangeError("nope")
	}))
	_, err = callMethodErr(r, ObjectValue(r.JSON), "parse", str(`1`), thrower)
	assertErrorKind(t, err, KindRangeError, "nope")
}

// TestJSONParseReviverGrowing: a reviver that adds a container per level
// makes the walk endless; it ends in a RangeError instead of overflowing the
// Go stack, while the deepest structure the parse accepts is still revived.
func TestJSONParseReviverGrowing(t *testing.T) {
	runProtoCases(t, []protoCase{
		{"growing", `return JSON.parse('[1,[2]]', function (k, v) { if (k === '0') this[1] = [5, 6]; return v })`, "!RangeError: Maximum call stack size exceeded"},
		{"growing object", `return JSON.parse('{"a":1,"b":{}}', function (k, v) { if (k === 'a') this.b = {a: 1, b: {}}; return v })`, "!RangeError: Maximum call stack size exceeded"},
		{"deepest", `const s = '['.repeat(512) + ']'.repeat(512); let n = 0; JSON.parse(s, function (k, v) { n++; return v }); return n + (() => { try { JSON.parse('[' + s + ']') } catch (e) { return e.name } })()`, "512SyntaxError"},
	})
}

func TestJSONStringifyTable(t *testing.T) {
	r := NewRealm()
	mk := func(src string) Value { return jsonParseGo(t, r, src) }
	fn := ObjectValue(r.NewNativeFunction(AtomEmpty, 0, func(*Realm, Value, []Value) (Value, error) { return Undefined(), nil }))
	withFn := r.NewObject()
	require.NoError(t, withFn.SetProp(r, key(r, "f"), fn))
	require.NoError(t, withFn.SetProp(r, key(r, "u"), Undefined()))
	require.NoError(t, withFn.SetProp(r, key(r, "sym"), SymbolValue(SymIterator)))
	require.NoError(t, withFn.SetProp(r, key(r, "n"), Null()))
	arrWithFn := r.NewArray(fn, Undefined(), SymbolValue(SymIterator), NaN(), NumberValue(math.Inf(-1)))
	nested := r.NewObject()
	require.NoError(t, nested.SetProp(r, key(r, "holes"), ObjectValue(r.NewArrayLen(2))))
	numObj, _ := r.ToObject(IntValue(5))
	strObj, _ := r.ToObject(str("s"))
	boolObj, _ := r.ToObject(True())
	dateObj, err := r.Construct(ObjectValue(r.DateCtor), []Value{IntValue(0)}, nil)
	require.NoError(t, err)
	cases := []struct {
		name string
		v    Value
		want string
	}{
		{"number", IntValue(1), "1"},
		{"negative zero", NumberValue(negativeZero), "0"},
		{"float", NumberValue(0.1), "0.1"},
		{"1e21", NumberValue(1e21), "1e+21"},
		{"large int", NumberValue(123456789012345680), "123456789012345680"},
		{"NaN", NaN(), "null"},
		{"Infinity", NumberValue(math.Inf(1)), "null"},
		{"string", str("a\"b\\c\n\t\u0001"), `"a\"b\\c\n\t\u0001"`},
		{"string unicode", str("你好😀"), `"你好😀"`},
		{"lone surrogate", utf16Str('a', 0xD800, 'b', 0xDFFF), `"a\ud800b\udfff"`},
		{"2028 unescaped", str("\u2028"), "\"\u2028\""},
		{"true", True(), "true"},
		{"null", Null(), "null"},
		{"undefined", Undefined(), "<undefined>"},
		{"function", fn, "<undefined>"},
		{"symbol", SymbolValue(SymIterator), "<undefined>"},
		{"empty object", ObjectValue(r.NewObject()), "{}"},
		{"empty array", ObjectValue(r.NewArrayLen(0)), "[]"},
		{"object", mk(`{"a":1,"b":"x","c":[true,null],"d":{"e":{}}}`), `{"a":1,"b":"x","c":[true,null],"d":{"e":{}}}`},
		{"omits undefined function symbol", ObjectValue(withFn), `{"n":null}`},
		{"array nulls", ObjectValue(arrWithFn), `[null,null,null,null,null]`},
		{"array holes", ObjectValue(nested), `{"holes":[null,null]}`},
		{"number wrapper", ObjectValue(numObj), "5"},
		{"string wrapper", ObjectValue(strObj), `"s"`},
		{"boolean wrapper", ObjectValue(boolObj), "true"},
		{"date toJSON", dateObj, `"1970-01-01T00:00:00.000Z"`},
		{"index keys first", mk(`{"b":1,"2":2,"a":3,"1":4}`), `{"1":4,"2":2,"b":1,"a":3}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, jsonStringifyGo(t, r, c.v))
		})
	}
}

func TestJSONStringifyIndentAndReplacer(t *testing.T) {
	r := NewRealm()
	v := jsonParseGo(t, r, `{"a":1,"b":[1,{"c":2}],"d":{},"e":[]}`)
	assert.Equal(t, "{\n  \"a\": 1,\n  \"b\": [\n    1,\n    {\n      \"c\": 2\n    }\n  ],\n  \"d\": {},\n  \"e\": []\n}", jsonStringifyGo(t, r, v, Undefined(), IntValue(2)))
	assert.Equal(t, "{\n\t\"a\": 1\n}", jsonStringifyGo(t, r, jsonParseGo(t, r, `{"a":1}`), Undefined(), str("\t")))
	// Indent clamps to 10 characters / 10 spaces; 0 or "" means none.
	assert.Equal(t, "[\n          1\n]", jsonStringifyGo(t, r, jsonParseGo(t, r, `[1]`), Undefined(), IntValue(20)))
	assert.Equal(t, "[\n01234567891\n]", jsonStringifyGo(t, r, jsonParseGo(t, r, `[1]`), Undefined(), str("0123456789abc")))
	assert.Equal(t, "[1]", jsonStringifyGo(t, r, jsonParseGo(t, r, `[1]`), Undefined(), IntValue(0)))
	assert.Equal(t, "[1]", jsonStringifyGo(t, r, jsonParseGo(t, r, `[1]`), Undefined(), str("")))
	assert.Equal(t, "[\n x1\n]", jsonStringifyGo(t, r, jsonParseGo(t, r, `[1]`), Undefined(), str(" x")))
	numObj, _ := r.ToObject(IntValue(1))
	assert.Equal(t, "[\n 1\n]", jsonStringifyGo(t, r, jsonParseGo(t, r, `[1]`), Undefined(), ObjectValue(numObj)))
	// Replacer array selects and orders keys (duplicates and non-strings dropped; numbers allowed), applies to nested objects.
	list := r.NewArray(str("b"), str("a"), str("b"), IntValue(1), True(), str("zz"))
	assert.Equal(t, `{"b":[1,{}],"a":1}`, jsonStringifyGo(t, r, v, ObjectValue(list)))
	withIdx := jsonParseGo(t, r, `{"1":"one","a":{"1":"x","b":2}}`)
	assert.Equal(t, `{"a":{"1":"x"},"1":"one"}`, jsonStringifyGo(t, r, withIdx, ObjectValue(r.NewArray(str("a"), IntValue(1)))))
	// Replacer function sees (key, value) with the holder as this and can rewrite/omit.
	repl := ObjectValue(r.NewNativeFunction(AtomEmpty, 2, func(r *Realm, this Value, args []Value) (Value, error) {
		k := args[0].AsString().GoString()
		if k == "a" {
			return str("A"), nil
		}
		if k == "d" {
			return Undefined(), nil
		}
		if args[1].IsNumber() && k != "" {
			return NumberValue(args[1].AsNumber() + 100), nil
		}
		return args[1], nil
	}))
	assert.Equal(t, `{"a":"A","b":[101,{"c":102}],"e":[]}`, jsonStringifyGo(t, r, v, repl))
	// Root call: key "" and holder {"": value}.
	var rootThis Value
	rootRepl := ObjectValue(r.NewNativeFunction(AtomEmpty, 2, func(r *Realm, this Value, args []Value) (Value, error) {
		if args[0].AsString().Len() == 0 {
			rootThis = this
		}
		return args[1], nil
	}))
	jsonStringifyGo(t, r, IntValue(1), rootRepl)
	require.True(t, rootThis.IsObject())
	rv, _ := rootThis.AsObject().GetProp(r, StringKey(AtomEmpty))
	assert.Equal(t, IntValue(1), rv)
}

func TestJSONStringifyToJSONAndErrors(t *testing.T) {
	r := NewRealm()
	o := r.NewObject()
	toJSON := ObjectValue(r.NewNativeFunction(AtomToJSON, 1, func(r *Realm, this Value, args []Value) (Value, error) {
		return str("key=" + args[0].AsString().GoString()), nil
	}))
	require.NoError(t, o.SetProp(r, StringKey(AtomToJSON), toJSON))
	holder := r.NewObject()
	require.NoError(t, holder.SetProp(r, key(r, "x"), ObjectValue(o)))
	assert.Equal(t, `{"x":"key=x"}`, jsonStringifyGo(t, r, ObjectValue(holder)))
	assert.Equal(t, `"key="`, jsonStringifyGo(t, r, ObjectValue(o)))
	arr := r.NewArray(ObjectValue(o))
	assert.Equal(t, `["key=0"]`, jsonStringifyGo(t, r, ObjectValue(arr)))
	// Cycle detection.
	cyc := r.NewObject()
	require.NoError(t, cyc.SetProp(r, key(r, "self"), ObjectValue(cyc)))
	_, err := callMethodErr(r, ObjectValue(r.JSON), "stringify", ObjectValue(cyc))
	assertErrorKind(t, err, KindTypeError, "Converting circular structure to JSON")
	cycArr := r.NewArrayLen(0)
	require.True(t, cycArr.Push(r, ObjectValue(cycArr)))
	_, err = callMethodErr(r, ObjectValue(r.JSON), "stringify", ObjectValue(cycArr))
	assertErrorKind(t, err, KindTypeError, "Converting circular structure to JSON")
	// Shared (non-cyclic) references are fine.
	leaf := r.NewObject()
	twice := r.NewArray(ObjectValue(leaf), ObjectValue(leaf))
	assert.Equal(t, `[{},{}]`, jsonStringifyGo(t, r, ObjectValue(twice)))
	// BigInt throws.
	b, _ := NewBigIntFromDecimal("1")
	_, err = callMethodErr(r, ObjectValue(r.JSON), "stringify", BigIntValue(b))
	assertErrorKind(t, err, KindTypeError, "Do not know how to serialize a BigInt")
	// Getters run and can throw.
	thrower := r.NewNativeFunction(AtomEmpty, 0, func(r *Realm, this Value, args []Value) (Value, error) {
		return Undefined(), r.TypeError("getter boom")
	})
	g := r.NewObject()
	g.DefineOwnAccessorFast(r, key(r, "g"), thrower, nil, attrEnumerable|attrConfigurable)
	_, err = callMethodErr(r, ObjectValue(r.JSON), "stringify", ObjectValue(g))
	assertErrorKind(t, err, KindTypeError, "getter boom")
	// Deep nesting is bounded.
	deep := jsonParseGo(t, r, strings.Repeat("[", 512)+strings.Repeat("]", 512))
	assert.Equal(t, strings.Repeat("[", 512)+strings.Repeat("]", 512), jsonStringifyGo(t, r, deep))
	// Interrupt.
	bigArr := jsonParseGo(t, r, "["+strings.Repeat("1,", 5000)+"1]")
	r.Interrupt("stop")
	_, err = callMethodErr(r, ObjectValue(r.JSON), "stringify", bigArr)
	var ie *InterruptedError
	require.ErrorAs(t, err, &ie)
	r.ClearInterrupt()
}

// TestJSONStringifySkipsCountWork checks that a property JSON.stringify
// visits and does not write counts toward the interrupt check like one it
// writes: a key of the property list the object lacks, an undefined,
// function or symbol value, a non-enumerable or symbol key, the last also
// where the key walk drops it (a dictionary-mode object, a proxy). Each
// input writes a few dozen values and skips thousands, so it must stop at
// the pending interrupt.
func TestJSONStringifySkipsCountWork(t *testing.T) {
	f := evalModule(t, `
const keys = Array.from({length: 1000}, (_, i) => "k" + i);
const many = {};
for (const k of keys) many[k] = undefined;
const values = {a: undefined, b() {}, c: Symbol()};
const hidden = {[Symbol()]: 1};
for (const k of ["d", "e", "f", "g"]) Object.defineProperty(hidden, k, {value: 1});
const dictHidden = {}, dictSymbols = {};
for (const k of keys) Object.defineProperty(dictHidden, k, {value: 1});
for (const k of keys) dictSymbols[Symbol(k)] = 1;
export const cases = {
  list: [new Array(10).fill({}), keys],
  "list of a proxy": [new Array(10).fill(new Proxy({}, {})), new Proxy(keys, {})],
  "many undefined values": [new Array(10).fill(many)],
  "skipped values": [new Array(1500).fill(values)],
  "skipped keys": [new Array(1200).fill(hidden)],
  "non-enumerable keys of a dictionary": [new Array(10).fill(dictHidden)],
  "symbol keys of a dictionary": [new Array(10).fill(dictSymbols)],
  "non-enumerable keys through a proxy": [new Array(10).fill(new Proxy(dictHidden, {}))],
};
`)
	cases, ok := f.env.GetBindingValue("cases")
	require.True(t, ok)
	r := f.r
	for _, name := range []string{"list", "list of a proxy", "many undefined values", "skipped values", "skipped keys",
		"non-enumerable keys of a dictionary", "symbol keys of a dictionary", "non-enumerable keys through a proxy"} {
		c, err := cases.AsObject().GetProp(r, key(r, name))
		require.NoError(t, err)
		args, err := r.CreateListFromArrayLike(c)
		require.NoError(t, err)
		_, err = callMethodErr(r, ObjectValue(r.JSON), "stringify", args...)
		require.NoError(t, err, name)
		r.Interrupt("stop")
		_, err = callMethodErr(r, ObjectValue(r.JSON), "stringify", args...)
		var ie *InterruptedError
		assert.ErrorAs(t, err, &ie, name)
		r.ClearInterrupt()
	}
}

// TestJSONStringifyChecksLengthAsItWrites checks that JSON.stringify stops
// at the string length limit as it writes, not at the end: a hole costs no
// memory, so 2**28 of them wrote about 1.3 GB before the final check, and
// references to one object repeat its output. With the limit at 1000 each
// input passes it within a few hundred values, so the RangeError has to come
// before the 4096th unit of work (a value written or a key walked) checks
// the pending interrupt: the object's 3000 keys leave about 1100 values.
func TestJSONStringifyChecksLengthAsItWrites(t *testing.T) {
	lowerMaxStringLength(t, 1000)
	f := evalModule(t, `
const holes = []; holes.length = 2 ** 28;
const empties = {};
for (let i = 0; i < 3000; i++) empties["k" + i] = [];
export const cases = {
  holes: [holes],
  "holes indented": [[holes], null, 10],
  "undefined elements": [new Array(5000).fill(undefined)],
  "function elements": [new Array(5000).fill(() => {})],
  "empty arrays": [new Array(5000).fill([])],
  "empty objects": [new Array(5000).fill({})],
  "object of empty arrays": [empties],
};
export {holes};
`)
	cases, ok := f.env.GetBindingValue("cases")
	require.True(t, ok)
	r := f.r
	r.Interrupt("stop")
	defer r.ClearInterrupt()
	for _, name := range []string{"holes", "holes indented", "undefined elements", "function elements", "empty arrays", "empty objects", "object of empty arrays"} {
		c, err := cases.AsObject().GetProp(r, key(r, name))
		require.NoError(t, err)
		args, err := r.CreateListFromArrayLike(c)
		require.NoError(t, err)
		_, err = callMethodErr(r, ObjectValue(r.JSON), "stringify", args...)
		assertInvalidLength(t, err, name)
	}
	holes, ok := f.env.GetBindingValue("holes")
	require.True(t, ok)
	_, err := r.JSONStringify(holes)
	assertInvalidLength(t, err, "JSONStringify")
}

func assertInvalidLength(t *testing.T, err error, name string) {
	t.Helper()
	var exc *Exception
	if assert.ErrorAs(t, err, &exc, name) {
		assert.Equal(t, "RangeError: Invalid string length", errorDisplayString(exc.Value), name)
	}
}

func TestJSONRoundTrip(t *testing.T) {
	r := NewRealm()
	for _, text := range []string{
		`{"model":"video-gen-1","prompt":"a cat surfing on a rainbow","size":"1280x720","duration":8,"seed":42,"metadata":{"user":"u_1","tags":["a","b","c"],"flags":{"hd":true,"audio":false}}}`,
		`[1,2.5,-3,1e-7,1e+21,"x",null,true,false,{"a":[[]]}]`,
		`"\u0000\u001f\"\\"`,
		`{"你好":"世界","emoji":"😀","lone":"\ud800"}`,
		`0.1`, `-0.5`, `123456789`, `1.7976931348623157e+308`, `5e-324`,
	} {
		v := jsonParseGo(t, r, text)
		assert.Equal(t, text, jsonStringifyGo(t, r, v), "round trip of %s", text)
	}
	// Go-callable entry points.
	v, err := r.JSONParse(FromGoString(`{"a":[1,2]}`))
	require.NoError(t, err)
	s, err := r.JSONStringify(v)
	require.NoError(t, err)
	assert.Equal(t, `{"a":[1,2]}`, s.GoString())
	s, err = r.JSONStringify(Undefined())
	require.NoError(t, err)
	assert.Nil(t, s)
	_, err = r.JSONParse(FromGoString(`{`))
	assertErrorKind(t, err, KindSyntaxError, "Unexpected end of JSON input")
}
