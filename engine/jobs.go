package engine

// The job queue (ES2025 §9.5 HostEnqueuePromiseJob, HTML's microtask
// queue): one FIFO per realm of promise reaction jobs, promise resolve
// thenable jobs and queueMicrotask callbacks. A job is a small struct, not a
// closure, so enqueueing one costs a queue slot.
//
// The queue drains when the outermost call into the realm returns
// (CallObject, Construct, and so RunScript and EvaluateModule), or when a
// host's HoldJobs is released:
//   - the drain runs every job queued so far and every job those queue, in
//     order, checking the interrupt flag before each;
//   - an interrupt drops the jobs still queued and becomes the call's error,
//     so the realm is reusable after ClearInterrupt with no stale jobs; the
//     modules whose asynchronous evaluation the dropped jobs, or the job
//     the interrupt stopped, would continue fail with it (module_eval.go);
//   - a reaction job catches what its handler throws (it rejects the derived
//     promise), so only a queueMicrotask callback, or the resolve or reject
//     function of a capability a species constructor made, can throw out of
//     a job. The outermost call returns the first such exception as its
//     error if the call itself succeeded, and the remaining jobs still run: a
//     host never loses an error silently;
//   - the call's own exception does not stop the drain.
//
// Jobs call their callbacks through CallObject, which brings the call depth
// back to zero and so reaches the drain again; draining guards against that.
// The outermost return also ends a job for WeakRef (ClearKeptObjects), which
// is why the [[KeptAlive]] list lives here; while draining, the drain ends
// each job itself, so a return inside a job keeps what it derefed alive.

// job is one queued job:
//   - reaction set: a PromiseReactionJob for arg, running the rejection
//     handler when rejected;
//   - promise set: a PromiseResolveThenableJob resolving promise with the
//     thenable arg through its then method fn;
//   - otherwise a queueMicrotask callback fn.
type job struct {
	reaction *promiseReaction
	promise  *Object
	fn       *Object
	arg      Value
	rejected bool
}

// jobState is the realm's job queue and the [[KeptAlive]] list of the
// current job (builtin_weak.go).
type jobState struct {
	kept     []Value
	keptGen  uint64
	queue    []job
	head     int // queue[head:] is still to run
	draining bool
	// tracker receives the HostPromiseRejectionTracker operations.
	tracker func(p *Object, op PromiseRejectionOperation)
}

// jobState returns the realm's job state, creating it on first use.
func (r *Realm) jobState() *jobState {
	lz := r.lazyState()
	if lz.jobs == nil {
		lz.jobs = &jobState{}
	}
	return lz.jobs
}

// pending reports whether the end of a job has work: jobs to run or kept
// objects to release.
func (j *jobState) pending() bool {
	return len(j.queue) != 0 || len(j.kept) != 0
}

// enqueue appends a job (HostEnqueuePromiseJob).
func (r *Realm) enqueue(jb job) {
	j := r.jobState()
	j.queue = append(j.queue, jb)
	r.jobsPending = true
}

// endJob runs at the return of the outermost call when r.jobsPending
// (CallObject, Construct, ReleaseJobs): unless a drain is already running
// further up, it ends the job for WeakRef and drains the queue. err is the
// call's error, and endJob returns the call's error (see the file comment).
// It leaves no job queued and no object kept, and so clears r.jobsPending
// (a Go panic out of the call before it runs leaves the call's jobs queued,
// and one out of a job the jobs after it, for DropJobs).
//
//go:noinline
func (r *Realm) endJob(err error) error {
	j := r.lazy.jobs
	if j.draining {
		return err
	}
	r.jobsPending = false
	j.clearKept()
	if len(j.queue) == 0 {
		return err
	}
	if _, ok := err.(*InterruptedError); ok {
		r.dropJobs(nil, err)
		return err
	}
	j.draining = true
	defer func() { j.draining, r.jobsPending = false, j.pending() }()
	for j.head < len(j.queue) {
		if ierr := r.CheckInterrupt(); ierr != nil {
			r.dropJobs(nil, ierr)
			return ierr
		}
		jb := j.queue[j.head]
		j.queue[j.head] = job{}
		j.head++
		if j.head == len(j.queue) {
			j.queue, j.head = j.queue[:0], 0
		} else if j.head >= 1024 && 2*j.head >= len(j.queue) {
			n := copy(j.queue, j.queue[j.head:])
			clear(j.queue[n:])
			j.queue, j.head = j.queue[:n], 0
		}
		jerr := r.runJob(&jb)
		j.clearKept()
		if jerr == nil {
			continue
		}
		if _, ok := jerr.(*InterruptedError); ok {
			r.dropJobs(jb.reaction, jerr)
			return jerr
		}
		if err == nil {
			err = jerr
		}
	}
	if cap(j.queue) > 1024 {
		j.queue = nil
	}
	return err
}

// dropJobs drops the queued jobs for the interrupt err, which stopped the
// job of reaction when it is not nil. The modules in asynchronous
// evaluation that the jobs would have continued fail with err
// (stopModules): nothing is left to run them.
func (r *Realm) dropJobs(reaction *promiseReaction, err error) {
	j := r.lazy.jobs
	if mm := r.lazy.modules; mm != nil && mm.asyncOrder != 0 {
		r.stopModules(reaction, j.queue[j.head:], err)
	}
	j.drop()
}

// DropJobs discards the queued jobs after a Go panic out of a call, which
// skipped the drain at the call's end (or the rest of it, for a panic out
// of a job): a host that recovers the panic at its outermost boundary calls
// it after RestoreCallState, so that the call's jobs do not run at the end
// of the next one. The modules in asynchronous evaluation those jobs would
// have continued fail with err. A no-op inside a call, where the jobs are
// the outermost call's. It also clears what a JSON.parse the panic stopped
// left on the realm's kept stack (jsonStack).
func (r *Realm) DropJobs(err error) {
	if r.callDepth != 0 || r.lazy == nil {
		return
	}
	if s := r.lazy.json; s != nil {
		s.keep(s.vals[:cap(s.vals)], s.keys[:cap(s.keys)])
	}
	if r.lazy.jobs == nil {
		return
	}
	r.jobsPending = false
	r.lazy.jobs.clearKept()
	r.dropJobs(nil, err)
}

// drop discards the queued jobs, releasing a large queue as the end of a
// drain does.
func (j *jobState) drop() {
	if cap(j.queue) > 1024 {
		j.queue = nil
	} else {
		clear(j.queue)
		j.queue = j.queue[:0]
	}
	j.head = 0
}

// HoldJobs defers the drain for a host that has bookkeeping to finish after
// a call and before the call's jobs run, as package moejs's Load recording
// the module's environment does: it counts one frame, so the calls until
// ReleaseJobs return without running jobs, as calls inside a call do. Take
// any CallState snapshot before HoldJobs.
func (r *Realm) HoldJobs() { r.callDepth++ }

// ReleaseJobs releases the frame of HoldJobs and, when that was the
// outermost one, ends the job as the return of CallObject does. err is the
// error of what ran held; ReleaseJobs returns it, or what the drain
// returns in its place (see the file comment).
func (r *Realm) ReleaseJobs(err error) error {
	if r.callDepth--; r.callDepth == 0 && r.jobsPending {
		return r.endJob(err)
	}
	return err
}

// runJob runs one job.
func (r *Realm) runJob(jb *job) error {
	switch {
	case jb.reaction != nil:
		return r.promiseReactionJob(jb.reaction, jb.arg, jb.rejected)
	case jb.promise != nil:
		return r.promiseResolveThenableJob(jb.promise, jb.arg, jb.fn)
	}
	_, err := r.CallObject(jb.fn, Undefined(), nil)
	return err
}

// clearKept implements ClearKeptObjects.
func (j *jobState) clearKept() {
	if len(j.kept) == 0 {
		return
	}
	clear(j.kept)
	j.kept = j.kept[:0]
	if cap(j.kept) > 64 {
		j.kept = nil
	}
	j.keptGen++
}

func init() { lateGlobal(StringKey(AtomQueueMicrotask), installQueueMicrotask) }

func installQueueMicrotask(r *Realm) {
	r.bindGlobal(AtomQueueMicrotask, ObjectValue(r.NewNativeFunction(AtomQueueMicrotask, 1, globalQueueMicrotask)))
}

// globalQueueMicrotask implements queueMicrotask(callback) (HTML).
func globalQueueMicrotask(r *Realm, this Value, args []Value) (Value, error) {
	cb := Arg(args, 0)
	if !IsCallable(cb) {
		return Undefined(), r.TypeError("queueMicrotask: %s is not a function", r.DisplayString(cb))
	}
	r.enqueue(job{fn: cb.AsObject()})
	return Undefined(), nil
}
