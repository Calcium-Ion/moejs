package engine

// interruptPayload boxes the value passed to Interrupt so it can be published
// atomically alongside the flag.
type interruptPayload struct {
	v any
}

// Interrupt requests that the running program stop. It is safe to call from
// any goroutine; the interpreter observes the flag at loop back-edges and
// function entry, natives through CheckInterrupt.
func (r *Realm) Interrupt(v any) {
	r.interruptValue.Store(&interruptPayload{v: v})
	r.interruptFlag.Store(1)
}

// ClearInterrupt resets the interrupt flag and payload.
func (r *Realm) ClearInterrupt() {
	r.interruptFlag.Store(0)
	r.interruptValue.Store(nil)
}

// Interrupted reports whether an interrupt is pending (one atomic load).
func (r *Realm) Interrupted() bool { return r.interruptFlag.Load() != 0 }

// CheckInterrupt returns an *InterruptedError when an interrupt is pending.
// It builds the error itself rather than calling interruptError, which
// would cost the per-step checks of natives (interruptEvery,
// btMachine.tick) their inlining.
func (r *Realm) CheckInterrupt() error {
	if r.interruptFlag.Load() == 0 {
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
