package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stringifyGo is JSON.stringify of v as a Go string.
func stringifyGo(t *testing.T, r *Realm, v Value) string {
	t.Helper()
	s, err := r.JSONStringify(v)
	require.NoError(t, err)
	require.NotNil(t, s)
	return s.GoString()
}

type fromGoSetting map[string]any
type fromGoHeaders map[string]string
type fromGoQuery map[string][]string
type fromGoList []any
type fromGoNames []string
type fromGoRows []map[string]any
type fromGoBytes []byte
type fromGoStatus string
type fromGoLevel int8
type fromGoRatio float32
type fromGoFlag bool
type fromGoCount uint64

// fromGoUpper is a named string with a MarshalJSON method, which wins over
// the retyping of its underlying type.
type fromGoUpper string

func (u fromGoUpper) MarshalJSON() ([]byte, error) {
	return json.Marshal(strings.ToUpper(string(u)))
}

type fromGoInner struct {
	Name string `json:"name"`
	Skip string `json:"-"`
	Opt  []int  `json:"opt,omitempty"`
}

type fromGoEmbedded struct {
	ID int64
}

type fromGoTask struct {
	fromGoEmbedded
	Status   fromGoStatus      `json:"status"`
	Progress float64           `json:"progress"`
	Data     json.RawMessage   `json:"data"`
	Inner    fromGoInner       `json:"inner"`
	Ptr      *fromGoInner      `json:"ptr"`
	Bytes    []byte            `json:"bytes"`
	At       time.Time         `json:"at"`
	IP       net.IP            `json:"ip"`
	Upper    fromGoUpper       `json:"upper"`
	Tags     map[string]int    `json:"tags"`
	Rows     []map[string]bool `json:"rows"`
	private  int
	HTML     string `json:"html"`
}

// TestFromGoNamedTypes: a named type whose underlying type FromGo converts
// is converted as that type, containers lazily: the result is the
// unnamed type's, and an untouched one exports its value of the unnamed
// type.
func TestFromGoNamedTypes(t *testing.T) {
	r := NewRealm()
	cases := []struct {
		named, base any
	}{
		{fromGoSetting{"b": 1.0, "a": "x", "n": map[string]any{"k": true}}, map[string]any{"b": 1.0, "a": "x", "n": map[string]any{"k": true}}},
		{fromGoHeaders{"X-B": "2", "X-A": "1"}, map[string]string{"X-B": "2", "X-A": "1"}},
		{fromGoQuery{"q": {"a", "b"}}, map[string][]string{"q": {"a", "b"}}},
		{fromGoList{"s", 1.5, nil}, []any{"s", 1.5, nil}},
		{fromGoNames{"x", "y"}, []string{"x", "y"}},
		{fromGoRows{{"k": "v"}, nil}, []map[string]any{{"k": "v"}, nil}},
		{fromGoStatus("queued"), "queued"},
		{fromGoLevel(-3), int8(-3)},
		{fromGoRatio(0.25), float32(0.25)},
		{fromGoFlag(true), true},
		{fromGoCount(1 << 60), uint64(1 << 60)},
		{fromGoSetting(nil), map[string]any(nil)},
		{fromGoSetting{}, map[string]any{}},
		{fromGoUpper("shout"), "SHOUT"},
	}
	for _, c := range cases {
		got, err := r.FromGo(c.named)
		require.NoError(t, err, "%T", c.named)
		want, err := r.FromGo(c.base)
		require.NoError(t, err)
		assert.Equal(t, stringifyGo(t, r, want), stringifyGo(t, r, got), "%T", c.named)
	}

	// A named map is a lazy node like the unnamed one: unmodified, it
	// exports its Go value, of the unnamed type.
	setting := fromGoSetting{"theme": "dark"}
	v := mustFromGo(t, r, setting)
	g := r.ToGo(v)
	assertSameGo(t, map[string]any(setting), g)

	// A []byte of a named type is an ArrayBuffer over its bytes.
	buf := fromGoBytes("ab")
	v = mustFromGo(t, r, buf)
	data, ok := v.AsObject().BufferData()
	require.True(t, ok)
	assert.Equal(t, []byte("ab"), data)
}

// TestFromGoStructs: other types encoding/json writes are converted from
// json.Marshal's text, parsed once by the engine: the result is JSON.parse
// of that text, a snapshot taken by FromGo.
func TestFromGoStructs(t *testing.T) {
	r := NewRealm()
	task := &fromGoTask{
		fromGoEmbedded: fromGoEmbedded{ID: 7},
		Status:         "running",
		Progress:       0.5,
		Data:           json.RawMessage(` {"k": [1, 2.50, "x"]} `),
		Inner:          fromGoInner{Name: "in", Skip: "never", Opt: nil},
		Bytes:          []byte{0, 1, 2},
		At:             time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC),
		IP:             net.IPv4(10, 0, 0, 1),
		Upper:          "up",
		Tags:           map[string]int{"b": 2, "a": 1, "10": 10},
		Rows:           []map[string]bool{{"ok": true}, nil},
		private:        3,
		HTML:           "<a&b> ",
	}
	for _, v := range []any{
		task,
		*task,
		map[string]int{"z": 1, "a": 2},
		[]map[string]string{{"role": "user"}, {}},
		[]int{3, 1, 2},
		[2]string{"a", "b"},
		map[int]string{2: "two", 10: "ten"},
		(*fromGoTask)(nil),
		struct{}{},
		uintptr(5),
	} {
		got, err := r.FromGo(v)
		require.NoError(t, err, "%T", v)
		text, err := json.Marshal(v)
		require.NoError(t, err)
		want, err := r.JSONParseGoString(string(text))
		require.NoError(t, err)
		assert.Equal(t, stringifyGo(t, r, want), stringifyGo(t, r, got), "%T", v)
	}

	// Field order, tags, omitempty, "-", the Marshal methods and the
	// embedded struct's promoted field are encoding/json's.
	got := mustFromGo(t, r, task)
	assert.Equal(t, `{"ID":7,"status":"running","progress":0.5,"data":{"k":[1,2.5,"x"]},"inner":{"name":"in"},"ptr":null,`+
		`"bytes":"AAEC","at":"2026-10-06T12:00:00Z","ip":"10.0.0.1","upper":"UP","tags":{"10":10,"a":1,"b":2},`+
		`"rows":[{"ok":true},null],"html":"<a&b> "}`, stringifyGo(t, r, got))

	// A snapshot: changing the struct afterwards changes nothing.
	task.Status = "done"
	status, err := got.AsObject().GetProp(r, key(r, "status"))
	require.NoError(t, err)
	assert.Equal(t, "running", status.AsString().GoString())

	// Inside a container the value is converted when the container is
	// materialized, and an error is thrown where the member is read.
	host := mustFromGo(t, r, map[string]any{
		"task":    fromGoTask{Status: "s"},
		"setting": fromGoSetting{"a": 1.0},
		"bad":     struct{ C chan int }{},
		"nan":     struct{ F float64 }{math.NaN()},
		"list":    []any{fromGoNames{"n"}, fromGoInner{Name: "i"}},
	})
	o := host.AsObject()
	task2, err := o.GetProp(r, key(r, "task"))
	require.NoError(t, err)
	assert.Contains(t, stringifyGo(t, r, task2), `"status":"s"`)
	setting, err := o.GetProp(r, key(r, "setting"))
	require.NoError(t, err)
	assert.Equal(t, `{"a":1}`, stringifyGo(t, r, setting))
	_, err = o.GetProp(r, key(r, "bad"))
	assert.ErrorContains(t, err, "json: unsupported type: chan int")
	_, err = o.GetProp(r, key(r, "nan"))
	assert.ErrorContains(t, err, "json: unsupported value: NaN")
	list, err := o.GetProp(r, key(r, "list"))
	require.NoError(t, err)
	assert.Equal(t, `[["n"],{"name":"i"}]`, stringifyGo(t, r, list))

	// At the top level the error is FromGo's.
	_, err = r.FromGo(struct{ C chan int }{})
	assert.ErrorContains(t, err, "engine: FromGo: struct { C chan int }: json: unsupported type: chan int")
	var unsupported *json.UnsupportedTypeError
	assert.True(t, errors.As(err, &unsupported))
	for _, v := range []any{make(chan int), func() {}, complex64(1), complex(1, 2)} {
		_, err = r.FromGo(v)
		assert.ErrorContains(t, err, "engine: FromGo: unsupported Go type "+reflect.TypeOf(v).String())
	}
}

// TestFromGoTypedNoInterrupt: FromGo observes no interrupt, also while it
// parses a struct's text.
func TestFromGoTypedNoInterrupt(t *testing.T) {
	r := NewRealm()
	big := make([]int, 10000)
	r.Interrupt("stop")
	v, err := r.FromGo(big)
	require.NoError(t, err)
	assert.Equal(t, uint32(10000), v.AsObject().ArrayLength())
	r.ClearInterrupt()
}

type fromGoCodeErr string

func (e fromGoCodeErr) Error() string { return string(e) }

type fromGoErrJSON struct{ Code int }

func (e *fromGoErrJSON) Error() string { return "code" }

func (e *fromGoErrJSON) MarshalJSON() ([]byte, error) { return []byte(`{"code":1}`), nil }

type fromGoHidden struct {
	n    int
	name string
}

type fromGoEmbedHidden struct {
	fromGoHidden
	m int
}

type fromGoEmbedPromoted struct {
	fromGoInner // unexported embedded type of exported fields: promoted
	z           int
}

type fromGoTaggedHidden struct {
	fromGoHidden `json:"inner"` // a named field for encoding/json: {"inner":{}}
}

type fromGoOnlyDash struct {
	A int `json:"-"`
}

// TestFromGoRejected: values json.Marshal writes as a text that says nothing
// of them stay an error, at the top level and inside a container: an error
// without a Marshal method, and a struct with fields json.Marshal writes
// none of. A struct with no fields is {}; one with a Marshal method, or
// with a field promoted from an embedded struct, is converted.
func TestFromGoRejected(t *testing.T) {
	r := NewRealm()
	var mu sync.Mutex
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, c := range []struct {
		v    any
		want string // "" for an error
	}{
		{errors.New("boom"), ""},
		{fmt.Errorf("wrapped: %w", io.EOF), ""},
		{fromGoCodeErr("named"), ""},
		{&fromGoErrJSON{Code: 1}, `{"code":1}`},
		{fromGoHidden{n: 1}, ""},
		{&fromGoHidden{}, ""},
		{fromGoEmbedHidden{}, ""},
		{&mu, ""},
		{context.Background(), ""},
		{fromGoOnlyDash{A: 1}, ""},
		{struct{}{}, `{}`},
		{fromGoTaggedHidden{}, `{"inner":{}}`},
		{fromGoEmbedPromoted{fromGoInner: fromGoInner{Name: "p"}}, `{"name":"p"}`},
		{ctx, `{"Context":{}}`}, // its exported embedded Context: json.Marshal's text
	} {
		got, err := r.FromGo(c.v)
		if c.want == "" {
			assert.ErrorContains(t, err, "engine: FromGo: unsupported Go type", "%T", c.v)
			host := mustFromGo(t, r, map[string]any{"v": c.v})
			_, err = host.AsObject().GetProp(r, key(r, "v"))
			assert.ErrorContains(t, err, "unsupported Go type", "%T in a map", c.v)
			continue
		}
		require.NoError(t, err, "%T", c.v)
		assert.Equal(t, c.want, stringifyGo(t, r, got), "%T", c.v)
	}
}
