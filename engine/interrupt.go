package engine

// interruptPayload boxes the value passed to Interrupt so it can be published
// atomically alongside the flag. n counts the calls of Interrupt so far
// (Stats.Interrupts); ClearInterrupt leaves a cleared payload that keeps
// the count and drops the value.
type interruptPayload struct {
	v       any
	n       int64
	cleared bool
}

// The interrupt flag (Realm.interruptFlag) is flagInterrupt while an
// interrupt is pending, and flagMeasure while a measurement of the heap
// waits for the interpreter: a charge a builtin made past the memory limit
// could not measure (remeasure), so the interpreter measures where no
// builtin runs: once a native it called returns (measureAfterCall) or at a
// frame's entry (pendingInterrupt). A loop's back edge checks only for an
// interrupt: a loop that calls nothing makes no charge that waits. An
// interrupt replaces a pending measurement; natives see only interrupts
// (CheckInterrupt).
// flagInterrupt is the sign bit, so that a loop's back edge tests it as
// the whole flag was tested before measurements waited on it (TESTL; JS),
// with code of the same size and so the same placement.
const (
	flagInterrupt = -1 << 31
	flagMeasure   = 2
)

// Interrupt requests that the running program stop. It is safe to call from
// any goroutine; the interpreter observes the flag at loop back-edges and
// function entry, natives through CheckInterrupt.
func (r *Realm) Interrupt(v any) {
	p := &interruptPayload{v: v, n: 1}
	for {
		old := r.interruptValue.Load()
		if old != nil {
			p.n = old.n + 1
		}
		if r.interruptValue.CompareAndSwap(old, p) {
			break
		}
	}
	r.interruptFlag.Store(flagInterrupt)
}

// ClearInterrupt resets the interrupt flag and drops the payload's value.
func (r *Realm) ClearInterrupt() {
	r.interruptFlag.Store(0)
	for {
		old := r.interruptValue.Load()
		if old == nil || old.cleared {
			return
		}
		if r.interruptValue.CompareAndSwap(old, &interruptPayload{n: old.n, cleared: true}) {
			return
		}
	}
}

// Interrupted reports whether an interrupt is pending (one atomic load).
func (r *Realm) Interrupted() bool { return r.interruptFlag.Load()&flagInterrupt != 0 }

// CheckInterrupt returns an *InterruptedError when an interrupt is pending.
// It builds the error itself rather than calling interruptError, which
// would cost the per-step checks of natives (interruptEvery,
// btMachine.tick) their inlining.
func (r *Realm) CheckInterrupt() error {
	if r.interruptFlag.Load()&flagInterrupt == 0 {
		return nil
	}
	var v any
	if p := r.interruptValue.Load(); p != nil {
		v = p.v
	}
	return &InterruptedError{Value: v}
}

// interruptError returns the *InterruptedError of an interrupt its caller
// saw pending. It does not load the flag again: a ClearInterrupt racing
// with the caller must not turn the interrupt it saw into a nil error, and
// so into an undefined result.
//
//go:noinline
func (r *Realm) interruptError() error {
	var v any
	if p := r.interruptValue.Load(); p != nil {
		v = p.v
	}
	return &InterruptedError{Value: v}
}

// pendingInterrupt is the slow path of the interpreter's check of the flag
// at a frame's entry (a loop's back edge checks only flagInterrupt and
// returns interruptError): a pending measurement is
// made there (measurePending) and the check passes, unless an interrupt
// came meanwhile, or was what set the flag, which is the error. Its caller
// saw the flag set.
//
//go:noinline
func (r *Realm) pendingInterrupt() error {
	if r.interruptFlag.CompareAndSwap(flagMeasure, 0) {
		r.measurePending()
		if r.interruptFlag.Load()&flagInterrupt == 0 {
			return nil
		}
	}
	return r.interruptError()
}

// interrupted reports whether an interrupt is pending, a pending
// measurement aside.
func (r *Realm) interrupted() bool { return r.interruptFlag.Load()&flagInterrupt != 0 }
