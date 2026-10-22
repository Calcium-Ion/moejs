package engine

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Exceptional exits must account for reads just as normal returns do. In
// particular, an exception must not let each caller decode another string
// in finally while unwinding without ever observing the memory limit.
func TestLazyExceptionalExitHonorsMemoryLimit(t *testing.T) {
	const src = `
export function one(xs) { return xs[0].length; }
export function thrown(xs) { throw xs[0].length; }
export function constructed(xs) {
  function C() { throw xs[0].length; }
  new C();
}
export function unwind(xs, i) {
  if (i + 1 < xs.length) {
    try { unwind(xs, i + 1); }
    finally { let n = xs[i].length; }
  } else {
    throw xs[i].length;
  }
}
`
	one := `"` + strings.Repeat("中", 16384) + `"`
	input := "[" + strings.Repeat(one+",", 31) + one + "]"
	for _, name := range []string{"thrown", "constructed", "unwind"} {
		for _, budget := range []struct {
			name  string
			limit int64
			hit   bool
		}{
			{"limited", 64 << 10, true},
			{"within_budget", 16 << 20, false},
			{"unlimited", 0, false},
		} {
			t.Run(name+"/"+budget.name, func(t *testing.T) {
				r := NewRealmWith(RealmOptions{MemoryLimit: budget.limit})
				env := evalModuleOn(t, r, src)
				r.ResetAllocation()
				v, err := r.JSONParseGoString(input)
				require.NoError(t, err)
				require.NoError(t, r.CheckInterrupt())
				// The host keeps the strings, so their decoding is retained.
				remove := r.AddLiveRoots(func(add func(Value)) { add(v) })
				fn, ok := env.GetBindingValue(name)
				require.True(t, ok)
				_, err = r.Call(fn, Undefined(), []Value{v, IntValue(0)})
				if !budget.hit {
					var ex *Exception
					require.ErrorAs(t, err, &ex)
					require.Equal(t, float64(16384), ex.Value.AsNumber())
					require.Zero(t, r.Stats().MemoryLimitHits)
					remove()
					return
				}
				require.ErrorIs(t, err, ErrMemoryLimit)
				require.Equal(t, int64(1), r.Stats().MemoryLimitHits)
				require.ErrorIs(t, r.CheckInterrupt(), ErrMemoryLimit)

				// A host can reset and reuse the realm after this hit too.
				remove()
				r.ClearInterrupt()
				require.NoError(t, r.RebaseAllocation(nil))
				r.ResetAllocation()
				v, err = r.JSONParseGoString(`["中"]`)
				require.NoError(t, err)
				fn, ok = env.GetBindingValue("one")
				require.True(t, ok)
				got, err := r.Call(fn, Undefined(), []Value{v})
				require.NoError(t, err)
				require.Equal(t, float64(1), got.AsNumber())
			})
		}
	}
}

// A new deferred-memory check must not replace errors when the read fits
// the budget or an interruption was already observed by the native call.
func TestLazyCallPreservesExistingErrors(t *testing.T) {
	for _, name := range []string{"no_read", "within_budget", "pending_host_interrupt", "cleared_host_interrupt", "already_charged"} {
		t.Run(name, func(t *testing.T) {
			r := NewRealmWith(RealmOptions{MemoryLimit: 64 << 10})
			size := 16384
			if name == "within_budget" {
				size = 16
			}
			s := r.hostString(strings.Repeat("中", size))
			hostErr := errors.New("host interruption")
			var want error = &Exception{Value: IntValue(7)}
			fn := r.NewNativeFunction(AtomEmpty, 0, func(r *Realm, _ Value, _ []Value) (Value, error) {
				if name != "no_read" {
					_ = s.Len()
				}
				switch name {
				case "pending_host_interrupt":
					r.Interrupt(hostErr)
				case "cleared_host_interrupt":
					r.Interrupt(hostErr)
					want = r.CheckInterrupt()
					r.ClearInterrupt()
				case "already_charged":
					r.charge(0)
				}
				return Undefined(), want
			})
			_, got := r.CallObject(fn, Undefined(), nil)
			if name == "already_charged" {
				// The outermost return reports a hit made in the call (endRunSees).
				require.ErrorIs(t, got, ErrMemoryLimit)
			} else {
				require.Same(t, want, got)
			}
			switch name {
			case "pending_host_interrupt":
				require.ErrorIs(t, r.CheckInterrupt(), hostErr)
				require.Zero(t, r.Stats().MemoryLimitHits)
			case "already_charged":
				require.ErrorIs(t, r.CheckInterrupt(), ErrMemoryLimit)
				require.Equal(t, int64(1), r.Stats().MemoryLimitHits)
			default:
				require.NoError(t, r.CheckInterrupt())
				require.Zero(t, r.Stats().MemoryLimitHits)
			}
		})
	}
}
