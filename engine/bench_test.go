package engine

import (
	"encoding/json"
	"strings"
	"testing"
)

func BenchmarkNewRealm(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		NewRealm()
	}
}

func BenchmarkGetOwnProperty(b *testing.B) {
	r := NewRealm()
	o := r.NewObject()
	for _, n := range []string{"model", "prompt", "size", "duration", "seed"} {
		o.DefineOwnDataFast(r, key(r, n), IntValue(1), attrDefault)
	}
	k := key(r, "duration")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		v, err := o.Get(r, k, ObjectValue(o))
		if err != nil || !v.IsNumber() {
			b.Fatal("bad get")
		}
	}
}

func BenchmarkShapeLookupHit(b *testing.B) {
	r := NewRealm()
	o := r.NewObject()
	for _, n := range []string{"model", "prompt", "size", "duration", "seed"} {
		o.DefineOwnDataFast(r, key(r, n), IntValue(1), attrDefault)
	}
	k := key(r, "seed")
	shape := o.Shape()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, _, ok := shape.Lookup(k); !ok {
			b.Fatal("miss")
		}
	}
}

// sampleJSON10KiB builds a ~10 KiB JSON document shaped like a plugin
// request context.
func sampleJSON10KiB() any {
	var sb strings.Builder
	sb.WriteString(`{"model":"video-gen-1","prompt":"a cat surfing on a rainbow","size":"1280x720","duration":8,"seed":42,"metadata":{"user":"u_1","tags":["a","b","c"],"flags":{"hd":true,"audio":false}},"images":[`)
	for i := range 40 {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"url":"https://example.com/images/`)
		sb.WriteString(strings.Repeat("x", 40))
		sb.WriteString(`.png","width":1024,"height":768,"weight":0.75,"caption":"reference image number `)
		sb.WriteString(strings.Repeat("y", 60))
		sb.WriteString(`","nested":{"a":1,"b":[1,2,3],"c":null}}`)
	}
	sb.WriteString(`]}`)
	if sb.Len() < 10*1024 || sb.Len() > 12*1024 {
		panic("sample JSON size drifted")
	}
	var v any
	if err := json.Unmarshal([]byte(sb.String()), &v); err != nil {
		panic(err)
	}
	return v
}

func BenchmarkFromGo10KiBJSON(b *testing.B) {
	r := NewRealm()
	doc := sampleJSON10KiB()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := r.FromGo(doc); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkToGo10KiBJSON(b *testing.B) {
	r := NewRealm()
	v, err := r.FromGo(sampleJSON10KiB())
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if r.ToGo(v) == nil {
			b.Fatal("nil export")
		}
	}
}

func BenchmarkStringConcatRope(b *testing.B) {
	piece := FromGoString("data: {\"delta\":\"hello\"}\n")
	b.ReportAllocs()
	for b.Loop() {
		s := emptyString
		for range 200 {
			s = concat(s, piece)
		}
		if s.GoString() == "" {
			b.Fatal("empty")
		}
	}
}

func BenchmarkFromGoString(b *testing.B) {
	src := strings.Repeat("abcdefgh", 64)
	b.ReportAllocs()
	for b.Loop() {
		if FromGoString(src).Len() != 512 {
			b.Fatal("len")
		}
	}
}
