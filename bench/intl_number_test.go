package bench

import (
	"os"
	"strings"
	"testing"
)

// BenchmarkIntlNumberFormat runs the workloads of
// testdata/intl_numberformat.mjs (bench/scripts/intl_number_node.mjs runs the
// same module in Node) on the engines that have Intl.NumberFormat: format
// and formatToParts of 1,000 numbers per op, reported also as ns per call,
// and the construction of a formatter.
func BenchmarkIntlNumberFormat(b *testing.B) {
	src, err := os.ReadFile("testdata/intl_numberformat.mjs")
	if err != nil {
		b.Fatal(err)
	}
	for _, e := range benchEngines() {
		mod, err := e.Compile("intl_numberformat.mjs", string(src))
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
		count, err := rt.Call("perOp", nil)
		if err != nil {
			b.Fatal(err)
		}
		for _, name := range []string{"formatJPY", "formatUSD", "formatDecimal", "partsJPY", "partsUSD", "partsDecimal", "constructJPY", "constructUSD", "constructDecimal"} {
			b.Run(name+"/"+e.Name(), func(b *testing.B) {
				if _, err := rt.Call(name, nil); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					if _, err := rt.Call(name, nil); err != nil {
						b.Fatal(err)
					}
				}
				if !strings.HasPrefix(name, "construct") {
					b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/number(b, count), "ns/call")
				}
			})
		}
		rt.Close()
	}
}
