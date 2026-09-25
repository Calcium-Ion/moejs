package engine

import (
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

// fuzzSurrogateEscape matches a \u escape of a surrogate code unit, which
// encoding/json decodes to U+FFFD while JSON.parse keeps the code unit.
var fuzzSurrogateEscape = regexp.MustCompile(`\\u[dD][89abAB]`)

// fuzzJSONNormalize maps ToGo's int64 numbers to float64 so an exported
// JSON.parse result compares with json.Unmarshal's.
func fuzzJSONNormalize(v any) any {
	switch x := v.(type) {
	case int64:
		return float64(x)
	case []any:
		for i, e := range x {
			x[i] = fuzzJSONNormalize(e)
		}
	case map[string]any:
		for k, e := range x {
			x[k] = fuzzJSONNormalize(e)
		}
	}
	return v
}

// FuzzJSON checks JSON.parse against encoding/json: the same texts are
// valid (inputs nesting deeper than JSON.parse's 512 levels are skipped),
// a valid text parses to the same value when encoding/json can represent
// it (valid UTF-8, no surrogate escapes, finite numbers), and
// stringify(parse(text)) is a fixed point of parse-then-stringify.
func FuzzJSON(f *testing.F) {
	for _, s := range []string{
		`{"a":[1,-0,2.5e-3,1E+2,true,false,null],"b":{"":"\u00e9\n\"\\\/"}}`,
		`[]`, `{}`, ` "x" `, `0`, `-0.0e0`, `1e999`, `12345678901234567890`,
		`{"__proto__":1,"a":1,"a":2,"1":3,"0":4}`,
		`"\ud800"`, `"\udc00\ud800x"`, `"\uD83D\uDE00"`, "\"\xff\"", "\"\t\"",
		`[1,]`, `{"a":1,}`, `01`, `1.`, `.5`, `+1`, `-`, `"\x41"`, `'a'`, `[1 2]`,
		"\ufeff1", "1\n\r\t ", `{"a" 1}`, `nul`, `[`, `"`, `"\u12"`, `tRue`,
		strings.Repeat("[", 400) + strings.Repeat("]", 400),
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, text string) {
		if strings.Count(text, "[")+strings.Count(text, "{") > 500 {
			return
		}
		r := NewRealm()
		v, err := r.JSONParse(FromGoString(text))
		if err != nil {
			if _, ok := err.(*Exception); !ok {
				t.Fatalf("parse error is %T, want a SyntaxError exception: %v", err, err)
			}
		}
		if valid := json.Valid([]byte(text)); valid != (err == nil) {
			t.Fatalf("encoding/json valid=%v, JSON.parse error=%v", valid, err)
		}
		if err != nil {
			return
		}
		if utf8.ValidString(text) && !fuzzSurrogateEscape.MatchString(text) {
			var want any
			if json.Unmarshal([]byte(text), &want) == nil {
				if got := fuzzJSONNormalize(r.ToGo(v)); !reflect.DeepEqual(got, want) {
					t.Fatalf("JSON.parse = %#v, encoding/json = %#v", got, want)
				}
			}
		}
		s1, err := r.JSONStringify(v)
		if err != nil || s1 == nil {
			t.Fatalf("stringify of a parsed value: %v, %v", s1, err)
		}
		v2, err := r.JSONParse(s1)
		if err != nil {
			t.Fatalf("stringify output %q does not parse: %v", s1, err)
		}
		s2, err := r.JSONStringify(v2)
		if err != nil || s2 == nil || s2.GoString() != s1.GoString() {
			t.Fatalf("round trip changed %q into %v (%v)", s1, s2, err)
		}
	})
}
