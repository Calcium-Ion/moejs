package engine

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRegExpTest262Semantics runs RegExp semantics ported from tc39/test262 at
// the pinned revision (bench/test262/REVISION; BSD-3-Clause, Ecma
// International), so the features this engine implements are held without
// the test262 checkout. testdata/regexp_test262.json has, per test file: the
// __executed expression of the S15.10.2.* pattern tests; every match and
// non-match string of the generated unicodeSets tests (want "[]": the
// strings the pattern gets wrong); the first argument of each
// assert.compareArray/sameValue in the lookBehind, named-groups,
// match-indices and top-level tests that evaluates on its own; and the
// regexp-modifiers tests whole, with assertion messages replaced by line
// numbers. Expected values are node's (V8 passes these tests), rendered by
// auditShowJS.
func TestRegExpTest262Semantics(t *testing.T) {
	b, err := os.ReadFile("testdata/regexp_test262.json")
	require.NoError(t, err)
	var cases []struct{ File, JS, Want string }
	require.NoError(t, json.Unmarshal(b, &cases))
	require.NotEmpty(t, cases)
	for _, c := range cases {
		t.Run(c.File, func(t *testing.T) {
			assert.Equal(t, c.Want, auditEvalJS(t, c.JS), "%s: %s", c.File, c.JS)
		})
	}
}
