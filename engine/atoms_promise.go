package engine

// Static atoms for Promise and queueMicrotask (builtin_promise.go, jobs.go).
var (
	AtomPromise        = staticAtom("Promise")
	AtomQueueMicrotask = staticAtom("queueMicrotask")
	AtomAll            = staticAtom("all")
	AtomAllSettled     = staticAtom("allSettled")
	AtomAny            = staticAtom("any")
	AtomRace           = staticAtom("race")
	AtomReject         = staticAtom("reject")
	AtomResolve        = staticAtom("resolve")
	AtomTry            = staticAtom("try")
	AtomWithResolvers  = staticAtom("withResolvers")
	AtomCatch          = staticAtom("catch")
	AtomFinally        = staticAtom("finally")
	atomPromiseLower   = staticAtom("promise")
	atomStatus         = staticAtom("status")
	atomReason         = staticAtom("reason")
	atomFulfilled      = staticAtom("fulfilled")
	atomRejected       = staticAtom("rejected")
)
