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
// frame's entry (pendingInterrupt). flagFlat says deferred storage, a
// flattened rope's or a decoded lazy string's, waits to be folded into the
// account (addFlat, foldFlatSlow); any goroutine may set it. A loop's back
// edge checks for an interrupt and a fold, not a measurement: a loop that
// calls nothing makes no charge that waits. An interrupt replaces a pending
// measurement; natives see only interrupts (CheckInterrupt).
// flagInterrupt is the sign bit, so that a loop's back edge tests it as
// the whole flag was tested before measurements waited on it (TESTL; JS),
// with code of the same size and so the same placement.
const (
	flagInterrupt = -1 << 31
	flagMeasure   = 2
	flagFlat      = 4 // deferred rope or lazy-string storage needs folding
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
	for {
		flags := r.interruptFlag.Load()
		if r.interruptFlag.CompareAndSwap(flags, flagInterrupt|flags&flagFlat) {
			break
		}
	}
}

// ClearInterrupt resets the interrupt flag and drops the payload's value.
func (r *Realm) ClearInterrupt() {
	r.interruptFlag.And(flagFlat)
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

// frameInterrupt is the check of the flag at a frame's entry, taking
// pendingInterrupt only when the flag is set.
func (r *Realm) frameInterrupt() error {
	if flags := r.interruptFlag.Load(); flags != 0 {
		return r.pendingInterrupt(flags)
	}
	return nil
}

// pendingInterrupt is the slow path of the interpreter's check of the flag
// at a frame's entry (a loop's back edge checks only flagInterrupt and
// returns interruptError): a pending measurement is
// made there (measurePending) and the check passes, unless an interrupt
// came meanwhile, or was what set the flag, which is the error. Its caller
// saw the flag set.
//
//go:noinline
func (r *Realm) pendingInterrupt(flags int32) error {
	if flags&flagInterrupt != 0 {
		return r.interruptError()
	}
	if flags&flagFlat != 0 && r.foldFlatSlow(r.mem()) {
		return r.interruptError()
	}
	if r.takeMeasure() {
		r.measurePending()
	}
	if r.interrupted() {
		return r.interruptError()
	}
	return nil
}

// loopInterrupt is the slow path of a loop's back edge, which run inlines
// as one load of the flag, taken for an interrupt or a fold (flagFlat),
// never for a measurement. run is too large for the Go compiler to inline
// a helper holding that check, so each back edge spells it out. It keeps
// the interrupt flags held even when ClearInterrupt races with it.
//
//go:noinline
func (r *Realm) loopInterrupt(flags int32) error {
	if flags&flagInterrupt != 0 || r.foldFlatSlow(r.mem()) {
		return r.interruptError()
	}
	return nil
}

// measureRequested reports a measurement waiting in flags, the
// interrupt flag's value, with no interrupt pending; a fold may wait too.
func measureRequested(flags int32) bool {
	return flags&(flagInterrupt|flagMeasure) == flagMeasure
}

// takeMeasure claims a waiting measurement (measureRequested), clearing
// flagMeasure alone: a fold notification set meanwhile stays for its own
// check.
func (r *Realm) takeMeasure() bool {
	for {
		flags := r.interruptFlag.Load()
		if !measureRequested(flags) {
			return false
		}
		if r.interruptFlag.CompareAndSwap(flags, flags&^flagMeasure) {
			return true
		}
	}
}

// requestMeasure preserves a concurrent decode notification and never
// replaces a host interrupt. Both kinds of pending work can share the flag.
func (r *Realm) requestMeasure() {
	for {
		flags := r.interruptFlag.Load()
		if flags&flagInterrupt != 0 || r.interruptFlag.CompareAndSwap(flags, flags|flagMeasure) {
			return
		}
	}
}

// interrupted reports whether an interrupt is pending, a pending
// measurement aside.
func (r *Realm) interrupted() bool { return r.interruptFlag.Load()&flagInterrupt != 0 }
