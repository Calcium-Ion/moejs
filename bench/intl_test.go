package bench

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
)

// BenchmarkIntlSegmenter runs the workloads of testdata/intl_segmenter.mjs
// (bench/scripts/intl_node.mjs runs the same module in Node) on the engines
// that have Intl.Segmenter: each but construct segments 64 KiB of mixed CJK,
// Latin and emoji text, reported as MB/s of UTF-8 and ns per segment, and
// construct creates one segmenter.
func BenchmarkIntlSegmenter(b *testing.B) {
	src, err := os.ReadFile("testdata/intl_segmenter.mjs")
	if err != nil {
		b.Fatal(err)
	}
	for _, e := range benchEngines() {
		mod, err := e.Compile("intl_segmenter.mjs", string(src))
		if err != nil {
			b.Fatal(err)
		}
		rt, err := e.NewRuntime()
		if err != nil {
			b.Fatal(err)
		}
		if err := rt.Instantiate(mod); err != nil {
			rt.Close() // no Intl.Segmenter
			continue
		}
		size, err := rt.Call("textBytes", nil)
		if err != nil {
			b.Fatal(err)
		}
		for _, name := range []string{"graphemes", "words", "sentences", "spread", "construct"} {
			b.Run(name+"/"+e.Name(), func(b *testing.B) {
				first, err := rt.Call(name, nil)
				if err != nil {
					b.Fatal(err)
				}
				text := name != "construct"
				if text {
					b.SetBytes(int64(number(b, size)))
				}
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					if _, err := rt.Call(name, nil); err != nil {
						b.Fatal(err)
					}
				}
				if text {
					b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/number(b, first), "ns/segment")
				}
			})
		}
		rt.Close()
	}
}

// BenchmarkIntlDateTimeFormat runs the workloads of
// testdata/intl_datetimeformat.mjs (bench/scripts/intl_dtf_node.mjs runs the
// same module in Node) on the engines that have Intl.DateTimeFormat: format
// and formatToParts of 1,000 time values with a formatter of components in
// Asia/Tokyo (the issue's), of a dateStyle/timeStyle one and of a Chinese
// full date, formatRange, and 100 constructions; reported per call.
func BenchmarkIntlDateTimeFormat(b *testing.B) {
	src, err := os.ReadFile("testdata/intl_datetimeformat.mjs")
	if err != nil {
		b.Fatal(err)
	}
	for _, e := range benchEngines() {
		mod, err := e.Compile("intl_datetimeformat.mjs", string(src))
		if err != nil {
			b.Fatal(err)
		}
		rt, err := e.NewRuntime()
		if err != nil {
			b.Fatal(err)
		}
		if err := rt.Instantiate(mod); err != nil {
			rt.Close() // no Intl.DateTimeFormat
			continue
		}
		for _, name := range []string{"tokyoFormat", "tokyoParts", "stylesFormat", "stylesParts", "zhFormat", "range", "construct", "constructStyles"} {
			b.Run(name+"/"+e.Name(), func(b *testing.B) {
				if _, err := rt.Call(name, nil); err != nil {
					b.Fatal(err)
				}
				calls := 1000
				if strings.HasPrefix(name, "construct") {
					calls = 100
				}
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					if _, err := rt.Call(name, nil); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(calls), "ns/call")
			})
		}
		rt.Close()
	}
}

// number reads a number the engines export as int64 or float64.
func number(tb testing.TB, v any) float64 {
	f, err := strconv.ParseFloat(fmt.Sprint(v), 64)
	if err != nil {
		tb.Fatal(err)
	}
	return f
}

// BenchmarkIntlFormat runs the workloads of testdata/intl_format.mjs
// (bench/scripts/intl_node.mjs -format runs the same module in Node) on the
// engines that have Intl.NumberFormat and Intl.DateTimeFormat, reported as
// ns/op and ns per formatted value. The Date workloads run in UTC.
func BenchmarkIntlFormat(b *testing.B) {
	src, err := os.ReadFile("testdata/intl_format.mjs")
	if err != nil {
		b.Fatal(err)
	}
	for _, e := range benchEngines() {
		mod, err := e.Compile("intl_format.mjs", string(src))
		if err != nil {
			b.Fatal(err)
		}
		rt, err := e.NewRuntime()
		if err != nil {
			b.Fatal(err)
		}
		if err := rt.Instantiate(mod); err != nil {
			rt.Close() // no Intl.NumberFormat
			continue
		}
		for _, name := range []string{"yenFormat", "yenParts", "dollarFormat", "decimalFormat", "decimalParts", "tokyoFormat", "tokyoParts", "styledFormat", "numberToLocale", "dateToLocale", "constructNumber", "constructDate"} {
			b.Run(name+"/"+e.Name(), func(b *testing.B) {
				first, err := rt.Call(name, nil)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					if _, err := rt.Call(name, nil); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/number(b, first), "ns/value")
			})
		}
		rt.Close()
	}
}
