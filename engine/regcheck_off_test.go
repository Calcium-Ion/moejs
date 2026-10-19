//go:build !moejsregcheck

package engine

import "testing"

// skipInRegcheck skips a test of the fast build's allocations in the
// checked build, whose run allocates its register checks (regcheck.go).
func skipInRegcheck(t *testing.T) { t.Helper() }

// regcheckBuild reports whether the tests run in the checked build.
const regcheckBuild = false
