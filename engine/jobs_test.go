package engine

import (
	"errors"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// jobsLib is the prelude of the job fixtures: cases log into log.
const jobsLib = `
export const log = [];
export function done() { return log.join("|"); }
`

// newJobsFixture evaluates jobsLib+src with two globals: stop interrupts the
// realm and frames returns the depth of the Go stack.
func newJobsFixture(t *testing.T, src string) *moduleFixture {
	t.Helper()
	f := evalModule(t, jobsLib+src)
	r := f.r
	require.NoError(t, r.Global.SetProp(r, r.KeyFromGoString("stop"), ObjectValue(r.NewNativeFunction(AtomEmpty, 0,
		func(r *Realm, _ Value, _ []Value) (Value, error) {
			r.Interrupt("stop")
			return Undefined(), nil
		}))))
	require.NoError(t, r.Global.SetProp(r, r.KeyFromGoString("frames"), ObjectValue(r.NewNativeFunction(AtomEmpty, 0,
		func(r *Realm, _ Value, _ []Value) (Value, error) {
			return IntValue(runtime.Callers(0, make([]uintptr, 1<<16))), nil
		}))))
	return f
}

// queued returns the number of jobs still queued.
func queued(r *Realm) int {
	if r.lazy == nil || r.lazy.jobs == nil {
		return 0
	}
	j := r.lazy.jobs
	return len(j.queue) - j.head
}

func TestJobsDrainAtOutermostReturn(t *testing.T) {
	f := newJobsFixture(t, `
export function f() {
	queueMicrotask(() => log.push("m"));
	g();
	[0].forEach(() => queueMicrotask(() => log.push("m2")));
	log.push("f:" + log.length);
}
function g() { Promise.resolve().then(() => log.push("p")); log.push("g"); }
export class K { constructor() { queueMicrotask(() => log.push("k")); log.push("ctor"); } }
`)
	f.call("f")
	assert.Equal(t, "g|f:1|m|p|m2", f.call("done"))
	assert.Zero(t, queued(f.r))

	// Construct drains too.
	k, ok := f.env.GetBindingValue("K")
	require.True(t, ok)
	_, err := f.r.Construct(k, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "g|f:1|m|p|m2|ctor|k", f.call("done"))
}

func TestJobsNestedJobsRunInOrder(t *testing.T) {
	f := newJobsFixture(t, `
export function f() {
	queueMicrotask(() => { log.push("a"); queueMicrotask(() => log.push("c")); log.push("a-end"); });
	queueMicrotask(() => log.push("b"));
}
`)
	f.call("f")
	assert.Equal(t, "a|a-end|b|c", f.call("done"))
}

func TestJobsInterruptDropsTheQueue(t *testing.T) {
	f := newJobsFixture(t, `
export function f() {
	queueMicrotask(() => { log.push("m1"); stop(); });
	queueMicrotask(() => log.push("m2"));
	Promise.resolve().then(() => log.push("p"));
}
export function spin() {
	queueMicrotask(() => log.push("never"));
	stop();
	for (;;) {}
}
export function g() { queueMicrotask(() => log.push("g")); }
`)
	r := f.r
	_, err := f.callErr("f")
	var ie *InterruptedError
	require.True(t, errors.As(err, &ie), "got %v", err)
	assert.Equal(t, "stop", ie.Value)
	assert.Zero(t, queued(r), "an interrupt drops the queue")
	r.ClearInterrupt()
	f.call("g")
	assert.Equal(t, "m1|g", f.call("done"), "no stale jobs after ClearInterrupt")

	// An interrupted call drops the jobs it queued.
	_, err = f.callErr("spin")
	require.True(t, errors.As(err, &ie), "got %v", err)
	assert.Zero(t, queued(r))
	r.ClearInterrupt()
	f.call("g")
	assert.Equal(t, "m1|g|g", f.call("done"))
	assert.Zero(t, r.callDepth)
	assert.Zero(t, r.interp.sp)
}

func TestJobsInterruptBeforeDrain(t *testing.T) {
	// A pending interrupt stops the drain before its first job even when
	// the call itself returned normally.
	f := newJobsFixture(t, `export function f() { queueMicrotask(() => log.push("m")); stop(); }`)
	_, err := f.callErr("f")
	var ie *InterruptedError
	require.True(t, errors.As(err, &ie), "got %v", err)
	f.r.ClearInterrupt()
	assert.Equal(t, "", f.call("done"))
}

func TestJobsExceptionBecomesTheCallsError(t *testing.T) {
	f := newJobsFixture(t, `
export function f() {
	queueMicrotask(() => { throw new RangeError("first"); });
	queueMicrotask(() => { throw new TypeError("second"); });
	queueMicrotask(() => log.push("still runs"));
	return 1;
}
export function own() {
	queueMicrotask(() => { throw new RangeError("job"); });
	queueMicrotask(() => log.push("after own"));
	throw new Error("own");
}
`)
	_, err := f.callErr("f")
	var exc *Exception
	require.True(t, asException(err, &exc), "got %v", err)
	assert.Equal(t, "RangeError: first", errorDisplayString(exc.Value))
	assert.Equal(t, "still runs", f.call("done"))

	_, err = f.callErr("own")
	require.True(t, asException(err, &exc), "got %v", err)
	assert.Equal(t, "Error: own", errorDisplayString(exc.Value), "the call's own error wins")
	assert.Equal(t, "still runs|after own", f.call("done"))
}

func TestJobsSubclassCapabilityThrows(t *testing.T) {
	// The resolve function of a subclass's capability runs in the reaction
	// job, so what it throws escapes the job.
	f := newJobsFixture(t, `
export function f() {
	function C(ex) { ex(() => { throw new Error("cap"); }, () => {}); }
	const p = Promise.resolve(1);
	p.constructor = {[Symbol.species]: C};
	p.then(() => 2);
	queueMicrotask(() => log.push("next"));
}
`)
	_, err := f.callErr("f")
	var exc *Exception
	require.True(t, asException(err, &exc), "got %v", err)
	assert.Equal(t, "Error: cap", errorDisplayString(exc.Value))
	assert.Equal(t, "next", f.call("done"))
}

func TestJobsLongChainsDoNotRecurse(t *testing.T) {
	f := newJobsFixture(t, `
export function chain(n) {
	let p = Promise.resolve(0);
	for (let i = 0; i < n; i++) p = p.then(v => v + 1);
	p.then(v => log.push(v));
}
export function adopt(n) {
	// Each promise resolves with the next one: adoption is a job, not a call.
	let p = Promise.resolve("end");
	for (let i = 0; i < n; i++) { const q = p; p = new Promise(res => res(q)); }
	p.then(v => log.push(v));
}
export function ticks(n) {
	let first = 0, k = 0;
	function tick() {
		const d = frames();
		if (k === 0) first = d;
		if (d !== first) { log.push("depth " + first + " -> " + d); return; }
		if (++k < n) queueMicrotask(tick); else log.push("ticks " + k);
	}
	queueMicrotask(tick);
}
export function fanout(n) {
	// The queue grows past the compaction threshold while it drains.
	const out = [];
	for (let i = 0; i < n; i++) queueMicrotask(() => { out.push(i); queueMicrotask(() => out.push(n + i)); });
	queueMicrotask(() => queueMicrotask(() => queueMicrotask(() => {
		let ok = out.length === 2 * n;
		for (let i = 0; ok && i < out.length; i++) ok = out[i] === i;
		log.push("fanout " + ok);
	})));
}
`)
	f.call("chain", 100000)
	f.call("adopt", 50000)
	f.call("ticks", 20000)
	f.call("fanout", 3000)
	assert.Equal(t, "100000|end|ticks 20000|fanout true", f.call("done"))
	assert.Zero(t, queued(f.r))
}

func TestJobsKeepDerefedTargetsUntilTheJobEnds(t *testing.T) {
	// The handler's return and the then getter run in one job: the target
	// the handler derefed stays kept until the drain ends that job.
	f := newJobsFixture(t, `
const w = new WeakRef({});
export function f() {
	Promise.resolve().then(() => { w.deref(); return { get then() { log.push(kept()); } }; });
}
`)
	r := f.r
	require.NoError(t, r.Global.SetProp(r, r.KeyFromGoString("kept"), ObjectValue(r.NewNativeFunction(AtomEmpty, 0,
		func(r *Realm, _ Value, _ []Value) (Value, error) {
			return IntValue(len(r.lazy.jobs.kept)), nil
		}))))
	f.call("f")
	assert.Equal(t, "1", f.call("done"))
	assert.Empty(t, r.lazy.jobs.kept, "cleared when the job ended")
}

func TestJobsInterruptReleasesALargeQueue(t *testing.T) {
	// Each interrupt exit of the drain frees the backing array of a queue
	// grown past 1024 jobs, as a drain that runs to the end does.
	f := newJobsFixture(t, `
function fill() { for (let i = 0; i < 5000; i++) queueMicrotask(() => {}); }
export function before() { queueMicrotask(() => stop()); fill(); }
export function inJob() { queueMicrotask(() => { stop(); for (;;) {} }); fill(); }
export function own() { fill(); stop(); for (;;) {} }
`)
	r := f.r
	for _, name := range []string{"before", "inJob", "own"} {
		_, err := f.callErr(name)
		var ie *InterruptedError
		require.True(t, errors.As(err, &ie), "%s: got %v", name, err)
		assert.Nil(t, r.lazy.jobs.queue, name)
		r.ClearInterrupt()
	}
}

func TestJobsHold(t *testing.T) {
	f := newJobsFixture(t, `
export function f() { queueMicrotask(() => log.push("m")); log.push("f"); }
export function g() { queueMicrotask(() => stop()); queueMicrotask(() => log.push("dropped")); }
`)
	r := f.r
	r.HoldJobs()
	f.call("f")
	r.HoldJobs()
	f.call("f")
	require.NoError(t, r.ReleaseJobs(nil))
	assert.Equal(t, 2, queued(r), "an inner release does not drain")
	own := errors.New("own")
	require.Same(t, own, r.ReleaseJobs(own), "the held call's error wins")
	assert.Equal(t, "f|f|m|m", f.call("done"))
	assert.Zero(t, r.callDepth)

	r.HoldJobs()
	f.call("g")
	var ie *InterruptedError
	require.True(t, errors.As(r.ReleaseJobs(nil), &ie), "the drain's interrupt")
	r.ClearInterrupt()
	assert.Zero(t, queued(r))
	assert.Equal(t, "f|f|m|m", f.call("done"))
}
