package engines

import "testing"

// The adapter's allocations count in the HookSuite row: a call that
// succeeds must not pay for the error mapping.
func TestWrapMoejsErrorNilAllocatesNothing(t *testing.T) {
	if n := testing.AllocsPerRun(100, func() {
		if wrapMoejsError(nil) != nil {
			t.Fatal("wrapMoejsError(nil) != nil")
		}
	}); n != 0 {
		t.Fatalf("wrapMoejsError(nil) allocates %v times", n)
	}
}
