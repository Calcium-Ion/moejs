package engine

import (
	"strconv"
	"testing"
)

// BenchmarkInternDistinct50k interns 50,000 names that the process-wide
// table has never seen (one fresh realm per iteration, names unique per
// iteration), so every intern is a table insert.
func BenchmarkInternDistinct50k(b *testing.B) {
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		r := NewRealmWith(RealmOptions{SharedIntrinsics: true})
		prefix := "bench_intern_" + strconv.Itoa(i) + "_"
		for j := range 50_000 {
			r.InternGoString(prefix + strconv.Itoa(j))
		}
	}
}
