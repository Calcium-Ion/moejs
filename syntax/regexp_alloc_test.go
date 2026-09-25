package syntax

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRegexpCheckAllocs checks that the early check of regexp literals
// reuses the cached checker: once warm, a parse with regexps allocates what
// the same parse with strings does, garbage collections included.
func TestRegexpCheckAllocs(t *testing.T) {
	allocs := func(src string) float64 {
		parse := func() {
			_, err := ParseScript("t.js", src, Options{})
			require.NoError(t, err)
		}
		parse()
		return testing.AllocsPerRun(50, func() {
			parse()
			runtime.GC()
		})
	}
	assert.Equal(t, allocs("x = 'a'; y = 'b';"), allocs(`x = /a+[b-c]\d{2,}/; y = /^data:([a-z/+-]+);base64,(.*)$/iu;`))
}
