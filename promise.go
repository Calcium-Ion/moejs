package moejs

import "github.com/Calcium-Ion/moejs/engine"

type (
	// PromiseState is the state of a promise.
	PromiseState = engine.PromiseState
	// PromiseRejectionOperation is what a rejection tracker is told (see
	// SetPromiseRejectionTracker).
	PromiseRejectionOperation = engine.PromiseRejectionOperation
)

// Promise states.
const (
	PromisePending   = engine.PromisePending
	PromiseFulfilled = engine.PromiseFulfilled
	PromiseRejected  = engine.PromiseRejected
)

// Rejection tracker operations.
const (
	// PromiseRejectionReject: the promise was rejected with no handler.
	PromiseRejectionReject = engine.PromiseRejectionReject
	// PromiseRejectionHandle: the first handler was added to the promise,
	// rejected earlier with none.
	PromiseRejectionHandle = engine.PromiseRejectionHandle
)

// NewPromise creates a pending promise, for a host function to return, and
// the functions that settle it. resolve does what the promise's resolve
// function does in JavaScript: it adopts a thenable and fulfills with any
// other value; reject rejects. The first call of either decides and later
// calls do nothing. Both may be called after the host function returned,
// from the goroutine that uses the runtime: called outside a Call, they run
// the jobs they queue before returning, as the end of a Call does, and
// return what a Call would for those (an *InterruptedError, or the first
// exception a queueMicrotask callback threw).
func (rt *Runtime) NewPromise() (p Value, resolve, reject func(v Value) error) {
	o, res, rej := rt.realm.NewPromiseWithResolvers()
	return engine.ObjectValue(o), rt.settler(res), rt.settler(rej)
}

// settler calls the resolving function fn with the value it is given.
func (rt *Runtime) settler(fn *Object) func(v Value) error {
	return func(v Value) (err error) {
		r := rt.realm
		base := len(rt.argStack)
		defer rt.guard(&err, r.CallState(), base)
		rt.argStack = append(rt.argStack, v)
		top := len(rt.argStack)
		_, err = r.CallObject(fn, engine.Undefined(), rt.argStack[base:top:top])
		clear(rt.argStack[base:top])
		rt.argStack = rt.argStack[:base]
		return err
	}
}

// PromiseResult reports the state of the promise p and its result: the
// fulfillment value or the rejection reason, undefined while pending. ok is
// false when p is not a promise. No user code runs, so a promise a Call
// returned is read as the jobs that Call ran left it.
func PromiseResult(p Value) (state PromiseState, result Value, ok bool) {
	if !p.IsObject() {
		return PromisePending, engine.Undefined(), false
	}
	return p.AsObject().PromiseResult()
}

// SetPromiseRejectionTracker registers f to be called when a promise is
// rejected with no handler (PromiseRejectionReject) and when a promise so
// rejected gets its first handler (PromiseRejectionHandle), ECMAScript's
// HostPromiseRejectionTracker. A promise told Reject and not Handle when a
// Call returns has an unhandled rejection. f runs synchronously, in the
// reject or then that caused the operation. nil removes the tracker.
func (rt *Runtime) SetPromiseRejectionTracker(f func(p Value, op PromiseRejectionOperation)) {
	if f == nil {
		rt.realm.SetPromiseRejectionTracker(nil)
		return
	}
	rt.realm.SetPromiseRejectionTracker(func(p *Object, op PromiseRejectionOperation) {
		f(engine.ObjectValue(p), op)
	})
}
