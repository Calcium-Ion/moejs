package engine

import (
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ropeRead is one read of a rope that needs its units, checked against the
// string's Go form; r is the reading goroutine's own realm.
type ropeRead struct {
	name string
	read func(r *Realm, s *String, want string) bool
}

var ropeReads = []ropeRead{
	{"At", func(r *Realm, s *String, want string) bool {
		u := FromGoString(want)
		return s.At(0) == u.At(0) && s.At(s.Len()/2) == u.At(s.Len()/2) && s.At(s.Len()-1) == u.At(s.Len()-1)
	}},
	{"GoString", func(r *Realm, s *String, want string) bool { return s.GoString() == want }},
	{"Equals", func(r *Realm, s *String, want string) bool {
		w := FromGoString(want)
		return s.Equals(w) && w.Equals(s) && s.EqualsGoString(want) && s.Compare(w) == 0
	}},
	{"Hash", func(r *Realm, s *String, want string) bool { return s.Hash() == FromGoString(want).Hash() }},
	{"Intern", func(r *Realm, s *String, want string) bool {
		return r.Intern(s) == r.Intern(FromGoString(want)) && r.KeyFromString(s) == r.KeyFromGoString(want)
	}},
	{"Concat", func(r *Realm, s *String, want string) bool {
		x, err := r.Concat(s, FromGoString("!"))
		y, err2 := r.Concat(FromGoString("<"+strings.Repeat("-", 100)), s)
		return err == nil && err2 == nil && x.GoString() == want+"!" && y.GoString() == "<"+strings.Repeat("-", 100)+want
	}},
	{"Substring", func(r *Realm, s *String, want string) bool {
		w := FromGoString(want)
		return s.Substring(1, s.Len()-1).Equals(w.Substring(1, w.Len()-1)) && s.IndexOf(w.Substring(w.Len()-3, w.Len()), w.Len()-3) == w.Len()-3
	}},
	{"Units", func(r *Realm, s *String, want string) bool {
		a, ok := s.ASCII()
		return s.IsASCII() == isASCII(want) && ok == isASCII(want) && (!ok || a == want) &&
			FromUTF16(s.UTF16()).GoString() == want && s.IsWellFormed()
	}},
	{"JSON", func(r *Realm, s *String, want string) bool {
		b, ok, err := r.AppendJSON(nil, StringValue(s))
		w, _, _ := r.AppendJSON(nil, StringValue(FromGoString(want)))
		return err == nil && ok && string(b) == string(w)
	}},
}

func isRopeForTest(s *String) bool { return s.kind == strRope }

// ropeCase builds fresh ropes for one round: the strings the goroutines read
// and their Go forms.
type ropeCase struct {
	name  string
	build func() ([]*String, []string)
}

func ropeCases() []ropeCase {
	pair := func(a, b string) func() ([]*String, []string) {
		return func() ([]*String, []string) {
			return []*String{concat(FromGoString(a), FromGoString(b))}, []string{a + b}
		}
	}
	return []ropeCase{
		{"ascii short", pair(strings.Repeat("a", 30), strings.Repeat("b", 40))},
		{"ascii long", pair(strings.Repeat("x", 1500), strings.Repeat("y", 2600))},
		{"utf16 short", pair(strings.Repeat("é", 30), strings.Repeat("c", 40))},
		{"utf16 long", pair(strings.Repeat("p", 1500), strings.Repeat("q😀", 900))},
		// A DAG of ropes: each goroutine reads one node, so flattening an
		// outer one walks inner ones others flatten at the same time.
		{"nested", func() ([]*String, []string) {
			a, b, c, d := strings.Repeat("A", 50), strings.Repeat("ü", 60), strings.Repeat("C", 30), strings.Repeat("D", appendPieceMax)
			r1 := concat(FromGoString(a), FromGoString(b)) // prepend
			r2 := concat(r1, FromGoString(c))              // append to a rope
			r3 := concat(FromGoString(d[:20]), r2)         // prepend to a rope
			r4 := concat(r3, r1)                           // r1 twice
			r5 := concat(FromGoString(d), FromGoString(d)) // join of big ASCII pieces
			r6 := concat(r5, r5)
			return []*String{r1, r2, r3, r4, r5, r6}, []string{a + b, a + b + c, d[:20] + a + b + c, d[:20] + a + b + c + a + b, d + d, d + d + d + d}
		}},
	}
}

// TestRopeConcurrentFlatten reads the same ropes from several goroutines at
// once, each with its own realm, as strings a host keeps may be: every read
// that needs a rope's units flattens it, the first one to finish publishes
// its copy, and every goroutine sees the whole string. The race detector
// checks the publication; without it, a reader could follow a child another
// goroutine had dropped, or see the new kind before the units.
func TestRopeConcurrentFlatten(t *testing.T) {
	const goroutines = 6
	rounds := 150
	if testing.Short() {
		rounds = 30
	}
	realms := make([]*Realm, goroutines)
	for i := range realms {
		realms[i] = NewRealm()
	}
	for _, c := range ropeCases() {
		t.Run(c.name, func(t *testing.T) {
			for round := range rounds {
				strs, wants := c.build()
				for _, s := range strs {
					require.True(t, isRopeForTest(s), "%s: a rope", c.name)
				}
				start := make(chan struct{})
				var wg sync.WaitGroup
				for g := range goroutines {
					wg.Add(1)
					go func() {
						defer wg.Done()
						<-start
						i := (g + round) % len(strs)
						op := ropeReads[(g+round)%len(ropeReads)]
						assert.True(t, op.read(realms[g], strs[i], wants[i]), "%s round %d: %s of string %d", c.name, round, op.name, i)
					}()
				}
				close(start)
				wg.Wait()
				for i, s := range strs {
					assert.Equal(t, wants[i], s.GoString())
				}
			}
		})
	}
}

// TestRopeReadNoAlloc: reading a flattened rope allocates nothing. Each
// read takes a view of the published contents in storage on its own stack
// (flat), which a read that lets the view reach a method with a rope branch
// would move to the heap.
func TestRopeReadNoAlloc(t *testing.T) {
	r := NewRealm()
	for _, tail := range []string{"b", "é"} {
		want := strings.Repeat("a", 30) + strings.Repeat(tail, 100)
		s := concat(FromGoString(strings.Repeat("a", 30)), FromGoString(strings.Repeat(tail, 100)))
		require.True(t, isRopeForTest(s))
		w, sub := FromGoString(want), FromGoString(strings.Repeat(tail, 3))
		s.flatten()
		r.KeyFromString(s) // the realm caches the atom
		for name, read := range map[string]func(){
			"At":             func() { s.At(70) },
			"IsASCII":        func() { s.IsASCII() },
			"ASCII":          func() { s.ASCII() },
			"UTF16":          func() { s.UTF16() },
			"Equals":         func() { s.Equals(w); w.Equals(s) },
			"EqualsGoString": func() { s.EqualsGoString(want) },
			"Compare":        func() { s.Compare(w) },
			"Hash":           func() { s.Hash() },
			"IndexOf":        func() { s.IndexOf(sub, 0) },
			"lastIndexOf":    func() { lastIndexOfString(s, sub, s.Len()) },
			"Substring":      func() { s.Substring(0, s.Len()) },
			"IsWellFormed":   func() { s.IsWellFormed() },
			"KeyFromString":  func() { r.KeyFromString(s) },
			"collHash":       func() { collHash(StringValue(s)) },
		} {
			switch {
			case name == "UTF16" && tail == "b":
				continue // an ASCII string's units are a copy
			case (name == "EqualsGoString" || name == "KeyFromString") && tail == "é":
				continue // a flat UTF-16 string allocates its UTF-8 form, its key
			}
			assert.Equal(t, 0.0, testing.AllocsPerRun(50, read), "%s %q", name, tail)
		}
		assert.Equal(t, want, s.GoString())
	}
}
