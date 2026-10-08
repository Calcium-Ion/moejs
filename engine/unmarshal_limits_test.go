package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// decodeLimitExprs are values the measure counts and values it leaves to
// the text: strings with every escape, numbers at the writers' edges,
// undefined members and elements, host values of every type.
var decodeLimitExprs = []string{
	`null`, `undefined`, `true`, `false`, `0`, `-0`, `1.5`, `1e21`, `1e-7`, `5e-324`, `2**53+1`, `2**63`, `-(2**63)`, `NaN`, `-Infinity`, `123456789012345680000`,
	`""`, `"plain"`, `"<a & b>"`, `"q\"b\\s/"`, `"\b\f\n\r\t\u0000\u001f\u007f"`, `"é😀"`, `"  "`, `"\ud800"`, `"x\udc00\ud800y"`, `"a".repeat(70000)`, `"<".repeat(5000)`,
	`[]`, `{}`, `[1, "a", null, undefined, true, [], {}]`, `{a: 1, b: undefined, c: [undefined], d: {e: "f"}}`, `{"": 1, "é": 2, "\ud800": 3, "<k>": 4}`,
	`[Symbol()]`, `{s: Symbol(), n: 1}`, `[[[[[[1]]]]]]`, `{z: 1, a: 2, m: {y: 3, b: 4}}`,
	`Object.assign(Object.create(null), {i: 1})`, `new (class { constructor() { this.i = 1 } })()`,
	`HOST`, `({h: HOST, n: 1})`, `[HOST.messages, HOST.hdr, HOST.tags, HOST.query]`, `(() => { const h = HOST; h.messages; return h })()`, `(() => { const h = HOST; h.n = 3; return h })()`,
	`RAW`, `({state: RAW})`, `(() => { RAW.z; return RAW })()`,
	`{toJSON() { return 1 }}`, `{get g() { return 1 }}`, `new Date(0)`, `new Map([["a", 1]])`, `[1, , 3]`, `1n`, `new Proxy({}, {})`, `() => 1`, `{f() {}}`,
}

func decodeLimitHost() map[string]any {
	return map[string]any{
		"model": "m<x>", "n": 2, "f": 1.5, "neg0": math.Copysign(0, -1), "big": int64(1<<53 + 1), "u8": uint8(3), "f32": float32(0.1), "t": true, "nil": nil,
		"messages": []any{map[string]any{"role": "user", "content": "hi é"}, "s", 1e21, nil, []any{}, map[string]any{}},
		"tags":     []string{"a", "b<"}, "hdr": map[string]string{"k": "v\x01"}, "query": map[string][]string{"q": {"1"}, "e": nil},
		"nilslice": []any(nil), "nilstrs": []string(nil), "nilmap": map[string]any(nil), "maps": []map[string]any{{"k": "v"}, nil},
		"bad": "a\xffb", "num": json.Number("1.50"),
	}
}

// TestDecodeMeasureExact: where MeasureDecode counts a value, its bytes and
// values are those of the text the decode's round trip writes, AppendJSON's
// or json.Marshal's of ToGo's value, and MeasureDecodeGo counts the latter
// on ToGo's value too.
func TestDecodeMeasureExact(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("export function v(i, HOST, RAW) { return [\n")
	for _, e := range decodeLimitExprs {
		sb.WriteString("() => (" + e + "),\n")
	}
	sb.WriteString("][i](); }\n")
	for _, realm := range hostLazyRealms {
		f := evalModuleWith(t, sb.String(), realm.opts)
		fn, ok := f.env.GetBindingValue("v")
		require.True(t, ok)
		measured := map[bool]int{}
		for i, e := range decodeLimitExprs {
			for _, togo := range []bool{false, true} {
				host := mustFromGo(t, f.r, decodeLimitHost())
				raw := mustFromGo(t, f.r, json.RawMessage(` {"z": [1, 2.50, 1E2, "< >"], "a": null} `))
				v, err := f.r.Call(fn, Undefined(), []Value{IntValue(i), host, raw})
				require.NoError(t, err, e)
				m := newDecodeMeasure(f.r, DecodeOptions{}, togo)
				if !m.js(v, 0) {
					require.NoError(t, m.err, e)
					continue
				}
				measured[togo]++
				var text []byte
				if togo {
					g, err := f.r.ToGoStrict(v)
					require.NoError(t, err, e)
					gm := newDecodeMeasure(nil, DecodeOptions{}, true)
					require.True(t, gm.goJSON(g, 0), e)
					text, err = json.Marshal(g)
					if err != nil {
						continue // NaN: json.Marshal's error, the decode's
					}
					assert.Equal(t, int64(len(text)), gm.bytes, "MeasureDecodeGo bytes of %s", e)
					assert.Equal(t, m.nodes, gm.nodes, "MeasureDecodeGo values of %s", e)
				} else {
					text, ok, err = f.r.AppendJSON(nil, v)
					require.NoError(t, err, e)
					if !ok {
						text = []byte("null")
					}
				}
				var tree any
				require.NoError(t, json.Unmarshal(text, &tree))
				assert.Equal(t, int64(len(text)), m.bytes, "togo=%v bytes of %s: %s", togo, e, text)
				assert.Equal(t, countJSONValues(tree), m.nodes, "togo=%v values of %s: %s", togo, e, text)
				_, n := jsonTextCount(string(text), false)
				assert.Equal(t, m.nodes, n, "jsonTextCount of %s", text)

				// The limits are exact.
				for _, c := range []struct {
					opts DecodeOptions
					over bool
				}{
					{DecodeOptions{MaxBytes: len(text)}, false},
					{DecodeOptions{MaxBytes: len(text) - 1}, len(text) > 1},
					{DecodeOptions{MaxNodes: int(m.nodes)}, false},
					{DecodeOptions{MaxNodes: int(m.nodes) - 1}, m.nodes > 1},
				} {
					ok, err := measureDecode(f.r, v, c.opts, togo)
					if c.over {
						assert.ErrorIs(t, err, ErrTooLarge, "%s %+v", e, c.opts)
						assert.False(t, ok)
					} else {
						assert.NoError(t, err, "%s %+v", e, c.opts)
						assert.True(t, ok)
					}
				}
			}
		}
		t.Logf("%s: %d of %d values counted for AppendJSON, %d for json.Marshal", realm.name, measured[false], len(decodeLimitExprs), measured[true])
		assert.Greater(t, measured[false], 40)
		assert.Greater(t, measured[true], 40)
	}
}

// countJSONValues counts the values of a decoded JSON text.
func countJSONValues(v any) int64 {
	switch x := v.(type) {
	case map[string]any:
		n := int64(1)
		for _, e := range x {
			n += countJSONValues(e)
		}
		return n
	case []any:
		n := int64(1)
		for _, e := range x {
			n += countJSONValues(e)
		}
		return n
	}
	return 1
}

// TestDecodeMeasureDeep: a cycle, and nesting past 10,000 levels, pass every
// limit.
func TestDecodeMeasureDeep(t *testing.T) {
	r := NewRealm()
	for _, src := range []string{`(() => { const o = {}; o.self = o; return o })()`, `(() => { const a = []; a.push(a); return a })()`, `(() => { let a = []; for (let i = 0; i < 10000; i++) a = [a]; return a })()`} {
		v := evalValue(t, r, src)
		for _, togo := range []bool{false, true} {
			ok, err := measureDecode(r, v, DecodeOptions{MaxBytes: 1 << 30}, togo)
			assert.False(t, ok)
			assert.ErrorIs(t, err, ErrTooLarge, src)
			assert.ErrorContains(t, err, "nested deeper than 10000")
		}
	}
	v := evalValue(t, r, `(() => { let a = []; for (let i = 1; i < 10000; i++) a = [a]; return a })()`)
	ok, err := r.MeasureUnmarshal(v, DecodeOptions{MaxBytes: 1 << 30})
	assert.True(t, ok)
	assert.NoError(t, err)
}

// TestDecodeMeasureInterrupt: counting AppendJSON's text observes an
// interrupt every 4096 values, as AppendJSON does; counting ToGo's does not.
func TestDecodeMeasureInterrupt(t *testing.T) {
	r := NewRealm()
	v := evalValue(t, r, `Array.from({length: 10000}, (_, i) => i)`)
	r.Interrupt("stop")
	defer r.ClearInterrupt()
	_, err := r.MeasureUnmarshal(v, DecodeOptions{MaxBytes: 1 << 30})
	var ie *InterruptedError
	assert.True(t, errors.As(err, &ie))
	ok, err := r.MeasureToGo(v, DecodeOptions{MaxBytes: 1 << 30})
	assert.True(t, ok)
	assert.NoError(t, err)
}

// FuzzDecodeQuotedLen: the string lengths the measure computes are those
// of the writers' texts.
func FuzzDecodeQuotedLen(f *testing.F) {
	for _, s := range []string{"", "plain", "<a&b>", "q\"\\/", "\b\f\n\r\t\x00\x1f\x7f", "é😀", "  ", "a\xffb\xc3", "\xed\xa0\x80"} {
		f.Add(s)
	}
	r := NewRealm()
	f.Fuzz(func(t *testing.T, s string) {
		b, err := json.Marshal(s)
		require.NoError(t, err)
		assert.Equal(t, int64(len(b)), goQuotedLen(s), "goQuotedLen(%q)", s)
		js := FromGoString(s)
		text, _, err := r.AppendJSON(nil, StringValue(js))
		require.NoError(t, err)
		assert.Equal(t, int64(len(text)), jsQuotedLenGo(s), "jsQuotedLenGo(%q)", s)
		assert.Equal(t, int64(len(text)), jsQuotedLen(js), "jsQuotedLen(%q)", s)
		b, err = json.Marshal(js.GoString())
		require.NoError(t, err)
		assert.Equal(t, int64(len(b)), goQuotedLenJS(js), "goQuotedLenJS(%q)", s)

		// The bytes as UTF-16 code units, lone surrogates among them.
		u := make([]uint16, 0, len(s)/2)
		for i := 0; i+1 < len(s); i += 2 {
			u = append(u, uint16(s[i])|uint16(s[i+1])<<8)
		}
		js = FromUTF16(u)
		text, _, err = r.AppendJSON(nil, StringValue(js))
		require.NoError(t, err)
		assert.Equal(t, int64(len(text)), jsQuotedLen(js), "jsQuotedLen(%x)", u)
		b, err = json.Marshal(js.GoString())
		require.NoError(t, err)
		assert.Equal(t, int64(len(b)), goQuotedLenJS(js), "goQuotedLenJS(%x)", u)
	})
}

func TestDecodeNumberLen(t *testing.T) {
	for _, f := range []float64{0, math.Copysign(0, -1), 1, -1, 1.5, 0.1, 1e21, 1e20, 1e-6, 1e-7, 5e-324, math.MaxFloat64, 1 << 53, 1<<53 + 2, 1 << 63, -(1 << 63), 1 << 64, 123456789012345680000, math.Inf(1), math.NaN()} {
		var buf [32]byte
		want := int64(len(AppendNumber(buf[:0], f)))
		if f != f || math.IsInf(f, 0) {
			want = 4
		}
		assert.Equal(t, want, jsNumberLen(f), "%v", f)
		if f == f && !math.IsInf(f, 0) {
			r := NewRealm()
			b, err := json.Marshal(r.ToGo(NumberValue(f)))
			require.NoError(t, err)
			assert.Equal(t, int64(len(b)), togoNumberLen(f), "%v", f)
			b, err = json.Marshal(float32(f))
			if err == nil {
				assert.Equal(t, int64(len(b)), goFloatLen(float64(float32(f)), 32), "float32 %v", f)
			}
		}
	}
}

// TestDecodeTextCount: jsonTextCount counts the values of a text, and gives
// json.Marshal's length of it as a json.RawMessage.
func TestDecodeTextCount(t *testing.T) {
	for _, s := range []string{
		`null`, ` 1 `, `"a"`, `[]`, `{}`, `{"a":1,"b":[true,false,null,"x"],"c":{"d":-1.5e3}}`,
		" {\n \"a\" : [ 1 , \"<&>\\u0041\\\"\" , { } ] ,\t\"b\":\"  \"}\r\n", `["\\", "a\"b", ":", ","]`,
	} {
		var tree any
		require.NoError(t, json.Unmarshal([]byte(s), &tree))
		b, n := jsonTextCount(s, false)
		assert.Equal(t, int64(len(s)), b)
		assert.Equal(t, countJSONValues(tree), n, "%s", s)
		want, err := json.Marshal(json.RawMessage(s))
		require.NoError(t, err)
		b, n = jsonTextCount(s, true)
		assert.Equal(t, int64(len(want)), b, "compact %s: %s", s, want)
		assert.Equal(t, countJSONValues(tree), n)
	}
}

// checkRawLowerBound asserts that rawLowerBound of the checked text s is at
// most AppendJSON's text of the value s parses to, in bytes and in values.
func checkRawLowerBound(t *testing.T, r *Realm, s string) (lbBytes, lbNodes, bytes, nodes int64) {
	t.Helper()
	i := jsonSkipSpace(s, 0)
	if i >= len(s) || s[i] != '{' && s[i] != '[' {
		return
	}
	if _, ok := jsonCheck(s, i); !ok {
		return
	}
	v, err := r.JSONParseGoString(s)
	require.NoError(t, err)
	text, _, err := r.AppendJSON(nil, v)
	require.NoError(t, err)
	_, nodes = jsonTextCount(string(text), false)
	bytes = int64(len(text))
	lbBytes, lbNodes = rawLowerBound(s)
	require.LessOrEqual(t, lbBytes, bytes, "bytes of %q: %s", s, text)
	require.LessOrEqual(t, lbNodes, nodes, "values of %q: %s", s, text)
	return lbBytes, lbNodes, bytes, nodes
}

// TestRawLowerBound: the bound never passes the exact count, is exact for
// text that AppendJSON writes back as it is, and keeps only the members that
// stay of an object with repeated keys.
func TestRawLowerBound(t *testing.T) {
	r := NewRealm()
	for _, s := range rawJSONCorpus() {
		checkRawLowerBound(t, r, s)
	}
	for _, c := range []struct {
		s     string
		exact bool
	}{
		{`{"a":"xx","b":[1,true,null,false],"c":{"d":"e"}}`, true},
		{`[{"model":"m","image":"QUJD"},"s",[[]]]`, true},
		{` { "a" : 1 } `, true},
		{`{"a":"QUJDRA==","a":1}`, false}, // the first member is replaced
		{`{"a":1,"a":"QUJDRA=="}`, false}, // only the last member counts
		{`["A\n", 1.50, 1e2, -0]`, false}, // escapes and numbers shrink
	} {
		lb, ln, b, n := checkRawLowerBound(t, r, c.s)
		if c.exact {
			assert.Equal(t, b, lb, "%s", c.s)
			assert.Equal(t, n, ln, "%s", c.s)
		}
	}
	// A wide object, keys compared through a map.
	var sb strings.Builder
	sb.WriteString(`{`)
	for i := range 100 {
		fmt.Fprintf(&sb, `"k%d":%d,`, i%90, i)
	}
	sb.WriteString(`"last":0}`)
	checkRawLowerBound(t, r, sb.String())
}

// FuzzRawLowerBound: rawLowerBound never passes AppendJSON's text of the
// parsed value.
func FuzzRawLowerBound(f *testing.F) {
	for _, s := range rawJSONCorpus() {
		if len(s) < 4096 {
			f.Add(s)
		}
	}
	f.Add(`{"a":1,"a":{"b":[1,2]},"c":"A","\ud800":"x"}`)
	r := NewRealm()
	f.Fuzz(func(t *testing.T, s string) {
		checkRawLowerBound(t, r, s)
	})
}

// TestDecodeMeasureRawFailsFast: a RawMessage passed through whose lower
// bound passes the limit fails without being parsed; one within it is
// parsed and counted exactly.
func TestDecodeMeasureRawFailsFast(t *testing.T) {
	r := NewRealm()
	text := `{"image":"` + strings.Repeat("QUJD", 1<<14) + `"}`
	v := mustFromGo(t, r, json.RawMessage(text))
	ok, err := r.MeasureUnmarshal(v, DecodeOptions{MaxBytes: 1 << 10})
	assert.False(t, ok)
	assert.ErrorIs(t, err, ErrTooLarge)
	assert.Equal(t, hostSentinelKey, v.AsObject().shape.key, "not parsed")
	ok, err = r.MeasureUnmarshal(v, DecodeOptions{MaxBytes: len(text)})
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.NotEqual(t, hostSentinelKey, v.AsObject().shape.key, "parsed to be counted")
}

// measureDecode is MeasureToGo when togo is set, else MeasureUnmarshal.
func measureDecode(r *Realm, v Value, opts DecodeOptions, togo bool) (bool, error) {
	if togo {
		return r.MeasureToGo(v, opts)
	}
	return r.MeasureUnmarshal(v, opts)
}

// TestCanDecodeDirect: the plain check the bounded decodes ask before the
// round trip. A Go map holding a json.RawMessage is plain although the
// measure does not count it; a toJSON, a getter or a proxy is not.
func TestCanDecodeDirect(t *testing.T) {
	r := NewRealm()
	host := mustFromGo(t, r, map[string]any{"state": json.RawMessage(`{"a": [1, 2]}`), "n": 1.0})
	ok, err := r.MeasureUnmarshal(host, DecodeOptions{MaxBytes: 1 << 20})
	require.NoError(t, err)
	assert.False(t, ok, "the measure gives no verdict on a RawMessage in a Go map")
	assert.True(t, r.CanUnmarshalDirect(host))
	assert.True(t, r.CanToGoIntoDirect(host))
	for _, src := range []string{`({toJSON() { return 1 }})`, `({get g() { return 1 }})`, `new Proxy({}, {})`} {
		v := evalValue(t, r, src)
		assert.False(t, r.CanUnmarshalDirect(v), src)
	}
	// The walk stores such a value in a json.RawMessage: what the bounded
	// decode now does instead of json.Unmarshal of the text it checked.
	res := r.NewObject()
	res.SetProp(r, key(r, "state"), host)
	type result struct{ State json.RawMessage }
	var a, b result
	complete, err := r.Unmarshal(ObjectValue(res), &a)
	require.NoError(t, err)
	require.True(t, complete)
	text, _, err := r.AppendJSON(nil, ObjectValue(res))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(text, &b))
	assert.Equal(t, b, a)
}
