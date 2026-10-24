package syntax

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// reuseShapes nest the speculative parses that fail only after reading
// nested ones: in the true branch of a conditional, an arrow function with
// a return type that no colon follows; after async, type parameters that
// start no arrow function; and the default value of a parameter of a
// function type, which the lookahead for the type reads.
var reuseShapes = []struct{ name, open, mid, close string }{
	{"conditional return type", "x = a ? (b): T => ", "c", ""},
	{"conditional return type, parenthesized", "x = a ? (b): T => (", "c", ")"},
	{"generic conditional return type", "x = a ? <T>(b): T => ", "c", ""},
	{"async comparison", "x = async < b > (", "c", ")"},
	{"async generic default", "x = async <T>([a = ", "0", "])"},
	{"function type default", "let x: ([a = (c: ", "T", ") => 0]) => void"},
	{"function type default, object", "let x: ({a = (c: ", "T", ") => 0}) => void"},
}

// splitShape splits the start of a shape into the statement's head and
// the part that nests.
func splitShape(open string) (head, nest string) {
	for _, h := range []string{"x = ", "let x: "} {
		if strings.HasPrefix(open, h) {
			return h, open[len(h):]
		}
	}
	return "", open
}

// TestTypeScriptReuse: an expression a speculative parse read, reused by
// the parse that reads its text again (tsReused), gives the tree and the
// errors reading it again gives. Every source parses the same with no
// reuse, with the default reuse, and with every expression reused however
// short, whose copies keep the marks of parentheses and casts.
func TestTypeScriptReuse(t *testing.T) {
	sources := append([]string{}, fuzzSeeds...)
	for _, p := range tsPairs {
		sources = append(sources, p.ts)
	}
	// Each shape nested a few levels around expressions whose reading
	// depends on marks: parentheses, casts, targets and arrow heads.
	for _, sh := range reuseShapes {
		for _, mid := range []string{sh.mid, "(c)", "({c})", "[c] = d", "(c) = d", "({c}) = d", "((c)) => 0", "(c as T) = d",
			"c as T = d", "async\n(c)", "<T>(c)", "(c): U => d", "f<T>(c)", "(c) => (d): U => e", "a ? (b) : c"} {
			for _, n := range []int{1, 2, 3} {
				prefix, open := splitShape(sh.open)
				sources = append(sources, prefix+strings.Repeat(open, n)+mid+strings.Repeat(sh.close, n)+";")
			}
		}
	}
	defer func(n int) { tsReuseMin = n }(tsReuseMin)
	parse := func(src string, min int) string {
		tsReuseMin = min
		m, err := ParseModule("t.ts", src, tsOpts)
		if err != nil {
			return "error: " + err.Error()
		}
		return Dump(m)
	}
	reused := 0
	for _, src := range sources {
		want := parse(src, 1<<30)
		assert.Equal(t, want, parse(src, 64), src)
		assert.Equal(t, want, parse(src, 1), src)
		if !strings.HasPrefix(want, "error") {
			reused++
		}
	}
	assert.Greater(t, reused, len(sources)/3, "most sources parse")
}

// TestTypeScriptReuseLinear: the shapes above parse in time linear in
// their depth, which the allocations, counted exactly, show: four times
// as deep allocates about four times as much, where reading the nesting
// again at every level allocated sixteen times as much, and a lookahead
// that read it again at every level doubled the work at each.
func TestTypeScriptReuseLinear(t *testing.T) {
	for _, sh := range reuseShapes {
		allocs := func(n int) float64 {
			prefix, open := splitShape(sh.open)
			src := prefix + strings.Repeat(open, n) + sh.mid + strings.Repeat(sh.close, n) + ";"
			return testing.AllocsPerRun(3, func() {
				if _, err := ParseModule("t.ts", src, tsOpts); err != nil {
					t.Fatal(sh.name, err)
				}
			})
		}
		small, large := allocs(250), allocs(1000)
		assert.Less(t, large/small, 5.0, "%s: %v allocations at 250 levels, %v at 1000", sh.name, small, large)
	}
}
