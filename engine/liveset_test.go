package engine

// The visited set of a measurement keeps a region's first liveSparseMax
// nodes in a hash set and switches to a bitmap at the next, and its
// storage goes back to a process-wide pool.

import (
	"math/rand"
	"testing"
	"unsafe"
)

// farGlobal is a node far from the heap (a global), which the set keeps
// in its map of far regions.
var farGlobal [64]uint64

// liveSetPatterns is a sequence of addresses (with repeats) into buf.
func liveSetPatterns(buf []uint64) map[string][]unsafe.Pointer {
	at := func(k int) unsafe.Pointer { return unsafe.Pointer(&buf[k]) }
	region := 1 << 20 / 8 // words per region
	// The first word of the first whole region of buf.
	base := int((uintptr(1<<20) - uintptr(unsafe.Pointer(&buf[0]))&(1<<20-1)) / 8)
	ps := map[string][]unsafe.Pointer{}
	for _, n := range []int{1, 511, 512, 513, 514, 1024, 3000} {
		var seq []unsafe.Pointer
		for i := range n {
			seq = append(seq, at(base+i*3))
		}
		// Every one again, then the 513th region's first ones.
		for i := range n {
			seq = append(seq, at(base+i*3))
		}
		seq = append(seq, at(base+region), at(base+region+1), at(base))
		ps["dense region of "+string(rune('0'+n%10))+"/"+itoa(n)] = seq
	}
	r := rand.New(rand.NewSource(1))
	var random []unsafe.Pointer
	for range 200000 {
		random = append(random, at(r.Intn(len(buf))))
	}
	ps["random"] = random
	var down []unsafe.Pointer
	for k := len(buf) - 1; k >= 0; k -= 997 {
		down = append(down, at(k))
	}
	ps["downward"] = append(down, unsafe.Pointer(&farGlobal[0]), unsafe.Pointer(&farGlobal[1]), unsafe.Pointer(&farGlobal[0]))
	var boundary []unsafe.Pointer
	for reg := 0; reg < 6; reg++ {
		for i := range 512 + reg - 2 {
			boundary = append(boundary, at(base+reg*region+i*5))
		}
	}
	for i := range boundary {
		if i%7 == 0 {
			boundary = append(boundary, boundary[i])
		}
	}
	ps["regions at the boundary"] = boundary
	return ps
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for ; n > 0; n /= 10 {
		s = string(rune('0'+n%10)) + s
	}
	return s
}

// TestLiveSetDifferential: add and has answer as a map of the
// addresses does, at the switch from hash set to bitmap (512 and 513
// nodes in a region), across regions, below the first region reached, for
// a far address, with repeats, and again on storage a release emptied (as
// the pool hands it out to the next measurement).
func TestLiveSetDifferential(t *testing.T) {
	buf := make([]uint64, 12<<20/8)
	var s liveSet
	for round := range 3 {
		for name, seq := range liveSetPatterns(buf) {
			ref := map[unsafe.Pointer]bool{}
			for i, p := range seq {
				want := !ref[p]
				ref[p] = true
				if got := s.add(p); got != want {
					t.Fatalf("round %d %s: add #%d of %p: %v, want %v", round, name, i, p, got, want)
				}
			}
			for p := range ref {
				if !s.has(p) {
					t.Fatalf("round %d %s: has(%p) false after add", round, name, p)
				}
			}
			misses := 0
			for k := 1; k < len(buf); k += 4099 {
				p := unsafe.Pointer(&buf[k])
				if !ref[p] && s.has(p) {
					misses++
				}
			}
			if misses != 0 {
				t.Fatalf("round %d %s: has true for %d addresses never added", round, name, misses)
			}
			s.release()
			for p := range ref {
				if s.has(p) {
					t.Fatalf("round %d %s: has(%p) true after release", round, name, p)
				}
			}
		}
	}
	// The storage kept for the next regions is of the right sizes.
	for class, list := range s.slots {
		for _, x := range list {
			if len(x) != liveSlotsMin<<class {
				t.Fatalf("slots of class %d hold %d", class, len(x))
			}
			for _, v := range x {
				if v != 0 {
					t.Fatalf("kept slots not cleared")
				}
			}
		}
	}
	for _, c := range s.chunks {
		for _, w := range c {
			if w != 0 {
				t.Fatalf("kept chunk not cleared")
			}
		}
	}
}
