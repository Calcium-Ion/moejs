package engine

// The host side of promises: creating one with its resolving functions,
// reading its state, and the rejection tracker
// (HostPromiseRejectionTracker). Package moejs builds its promise API on
// these.

// PromiseRejectionOperation is the operation HostPromiseRejectionTracker
// reports.
type PromiseRejectionOperation uint8

const (
	// PromiseRejectionReject: the promise was rejected with no handler.
	PromiseRejectionReject PromiseRejectionOperation = iota
	// PromiseRejectionHandle: the first handler was added to the promise,
	// rejected earlier with none.
	PromiseRejectionHandle
)

// NewPromiseWithResolvers creates a pending promise and its resolve and
// reject functions, as Promise.withResolvers does. Calling them through
// CallObject settles the promise and, from the outermost call, runs the jobs
// that queues.
func (r *Realm) NewPromiseWithResolvers() (p, resolve, reject *Object) {
	p = r.newPromise()
	_, resolve, reject = r.createResolvingFunctions(p)
	return p, resolve, reject
}

// PromiseResult returns the state of the promise o and its result: the
// fulfillment value or the rejection reason, undefined while pending. ok is
// false when o is not a promise.
func (o *Object) PromiseResult() (state PromiseState, result Value, ok bool) {
	if o.class != ClassPromise {
		return PromisePending, Undefined(), false
	}
	d := o.internal.(*promiseData)
	return d.state, d.result, true
}

// SetPromiseRejectionTracker registers f to be called when a promise is
// rejected with no handler (PromiseRejectionReject) and when a promise so
// rejected gets its first handler (PromiseRejectionHandle), synchronously,
// as HostPromiseRejectionTracker. A promise told Reject and not Handle when
// the outermost call returns has an unhandled rejection. nil removes the
// tracker.
func (r *Realm) SetPromiseRejectionTracker(f func(p *Object, op PromiseRejectionOperation)) {
	if f == nil && (r.lazy == nil || r.lazy.jobs == nil) {
		return
	}
	r.jobState().tracker = f
}
