// Package eventloop runs a moejs Runtime on an event loop: the Node.js
// timers (setTimeout, setInterval, setImmediate and their clear functions)
// and host work whose result comes back from another goroutine.
//
// A bare Runtime has no timers, and the settle functions of
// Runtime.NewPromise may only be called from the goroutine that uses the
// runtime. A Loop owns its runtime while it runs, and is the one goroutine
// that uses it then:
//
//	rt := moejs.NewRuntime(moejs.Options{})
//	loop, err := eventloop.New(rt, eventloop.Options{})
//	err = loop.Run(func(rt *moejs.Runtime) error {
//		_, err := rt.RunScript(script) // may call setTimeout
//		return err
//	})
//
// Run returns once nothing keeps the loop alive: no timer or immediate that
// is ref'd, no Hold not yet done, no promise of NewPromise not yet settled
// and no task not yet run. Other goroutines hand work to the loop with
// RunOnLoop, and a host function whose result comes from another goroutine
// returns a promise of NewPromise. Between runs the runtime is the host's
// again.
//
// # Phases
//
// One iteration of the loop has three phases, as Node's timers, poll and
// check phases: the timers due when the phase starts, in order of expiry,
// then of scheduling; the tasks posted before the phase started (RunOnLoop,
// the done function of Hold, the settle function of NewPromise); the
// immediates set before the phase started. The promise jobs and
// queueMicrotask callbacks a callback or task queues run when it returns,
// before the next one. With nothing to run, the loop sleeps until the next
// timer is due or a task is posted.
//
// # Timers
//
// The timer functions follow Node.js:
//   - setTimeout and setInterval return a Timeout object with the methods
//     ref, unref, hasRef, refresh, close and Symbol.toPrimitive, which
//     returns the timer's numeric id; setImmediate returns an Immediate
//     with ref, unref and hasRef.
//   - The callback gets the extra arguments, and the Timeout or Immediate
//     as this. A callback that is not a function is a TypeError with the
//     code ERR_INVALID_ARG_TYPE; a string is not evaluated.
//   - The delay goes through ToNumber and is truncated to whole
//     milliseconds. NaN, a delay below 1 and one above 2147483647 are 1 ms,
//     so a timer set by a callback runs in a later iteration.
//   - An interval is rescheduled for its delay after the time its callback
//     started. refresh restarts a timer's delay from now, and runs a
//     timeout that already fired again; a cleared timer stays cleared.
//   - clearTimeout and clearInterval take a Timeout, or the id its
//     Symbol.toPrimitive returned, as a number or a string, once that was
//     read; clearImmediate takes an Immediate. They ignore anything else,
//     and work inside the timer's own callback.
//   - An unref'd timer or immediate does not keep Run alive, and runs only
//     while something else does.
//
// Differences from Node: the Timeout and Immediate objects have no own
// properties (JSON.stringify gives {}, and constructor.name is "Object"),
// their methods throw a TypeError when called on another object, a Proxy of
// one included, and a Timeout has no Symbol.dispose method. Timers expire
// in the order of their exact due times, where Node compares whole
// milliseconds. timers/promises, the AbortSignal options, util.promisify
// and Node's warning for a delay above 2147483647 are not provided.
//
// # Errors and interrupts
//
// A callback's exception stops the loop, as an uncaught exception ends a
// Node.js process, unless Options.OnError takes it. An interrupt always
// stops it, before it takes the next timer, task or immediate off its
// queues, which keep them. A loop is interrupted through Loop.Interrupt:
// Runtime.Interrupt alone stops the JavaScript running but does not wake a
// loop that sleeps. A rejection no handler catches is the host's business,
// through Runtime.SetPromiseRejectionTracker.
//
// # Memory limit
//
// Options.MemoryLimit of the runtime bounds each macrotask: the loop
// resets the runtime's allocation count (Runtime.ResetAllocation) before
// each timer or immediate callback and each task, so the promise jobs a
// callback queues count with it. A callback that passes the limit stops the
// loop with its *moejs.InterruptedError, as any interrupt does. The limit
// bounds what one macrotask allocates, not what the loop's callbacks keep
// alive between them.
package eventloop

import (
	"errors"
	"sync"
	"time"

	"github.com/Calcium-Ion/moejs"
)

var (
	// ErrTerminated is Run's and Start's error once Terminate ended the
	// loop, and the value Terminate interrupts the runtime with.
	ErrTerminated = errors.New("eventloop: loop terminated")
	// ErrRunning is Run's and Start's error while the loop already runs,
	// started by Run or Start.
	ErrRunning = errors.New("eventloop: loop is already running")
)

// Options configures New.
type Options struct {
	// OnError receives the errors of the callbacks the loop runs: what a
	// timer or immediate callback throws, the first exception the jobs it
	// queued throw (a queueMicrotask callback's), a Go panic as an
	// *moejs.InternalError, and what the settle function of NewPromise
	// fails with. It runs on the loop, which keeps running afterwards.
	// When OnError is nil, such an error stops the loop: Run returns it,
	// or Stop for a loop started with Start, as an uncaught exception ends
	// a Node.js process. An interrupt always stops the loop and is never
	// passed to OnError.
	OnError func(err error)
}

// Loop is an event loop that owns one Runtime while it runs. Its methods
// may be called from any goroutine, except NewPromise, which a host
// function calls on the loop, and Stop and Terminate, which must not be
// called on the loop: they wait for it. On the loop means from Run's fn, a
// callback, a task, a host function JavaScript calls there, or OnError. A
// runtime has at most one Loop.
type Loop struct {
	rt      *moejs.Runtime
	onError func(error)
	epoch   time.Time
	// wake is signalled when a task is posted or a stop requested.
	wake chan struct{}

	mu sync.Mutex
	// cur is the run in progress, nil while the loop does not run.
	cur        *run
	background bool  // started by Start: alive while idle
	stop       bool  // Stop or StopNoWait asked the running loop to end
	terminated bool  // Terminate ended the loop for good
	lastErr    error // what ended the last run (Stop returns it)
	// err is the error that stops the running loop. Only the goroutine
	// running the loop reads and writes it, after enter reset it.
	err error

	timers     timerHeap
	ids        map[int64]*timer // timers whose id was read, while active
	immediates []*immediate     // set and not yet run, in order
	tasks      []func(*moejs.Runtime) error
	refs       int // active timers and pending immediates that are ref'd
	holds      int // Hold and NewPromise not yet done or settled
	seq        uint64
	lastID     int64

	// The prototypes of the Timeout and Immediate objects, created on
	// first use. Only the goroutine using the runtime touches them.
	timeoutProto, immediateProto *moejs.Object
}

// run is one run of the loop, by Run or Start.
type run struct {
	done chan struct{} // closed when the run ended
	err  error         // what ended it, set before done is closed
}

// New creates a loop for rt and installs setTimeout, clearTimeout,
// setInterval, clearInterval, setImmediate and clearImmediate as globals
// of rt, as the package documentation describes. The error is SetGlobal's.
func New(rt *moejs.Runtime, opts Options) (*Loop, error) {
	l := &Loop{rt: rt, onError: opts.OnError, epoch: time.Now(), wake: make(chan struct{}, 1)}
	if err := l.install(); err != nil {
		return nil, err
	}
	return l, nil
}

// Runtime returns the loop's runtime.
func (l *Loop) Runtime() *moejs.Runtime { return l.rt }

// Run calls fn with the runtime on the calling goroutine and then runs the
// loop there until nothing keeps it alive, Stop or StopNoWait ends it, or
// an error or an interrupt stops it (Options.OnError, Interrupt). It
// returns fn's error at once when fn fails, the error that stopped the
// loop (an *moejs.InterruptedError for an interrupt), ErrTerminated once
// Terminate ended it, or nil. fn may be nil. fn runs on the loop: it must
// not call Stop or Terminate, which would wait for it.
//
// What is still pending when Run returns, an unref'd timer or immediate, or
// anything after a stop, an error or an interrupt, stays pending: the next
// Run or Start continues it, and JavaScript the host runs on the runtime
// between them (Call) can set more. The runtime is the host's again between
// them.
//
// A panic in fn or in a host task (the functions of RunOnLoop, of done and
// of settle, and OnError) is not recovered: it unwinds through Run, or
// ends the program for a loop started with Start. The loop is marked
// stopped first, and the tasks and immediates its phase had not run yet
// stay queued; the task that panicked is gone, and a promise whose settle
// function panicked stays pending.
func (l *Loop) Run(fn func(*moejs.Runtime) error) (err error) {
	if err := l.enter(false); err != nil {
		return err
	}
	defer func() { l.exit(err) }()
	if fn != nil {
		err = fn(l.rt)
	}
	if err == nil {
		err = l.loop()
	}
	l.mu.Lock()
	if l.terminated {
		err = ErrTerminated
	}
	l.mu.Unlock()
	return err
}

// Start runs the loop on a new goroutine until Stop, StopNoWait or
// Terminate ends it, or an error stops it (Options.OnError): idle, it
// waits for tasks (RunOnLoop) and timers. While it runs, the host uses the
// runtime only through RunOnLoop. It returns ErrRunning when the loop
// already runs and ErrTerminated once Terminate ended it.
func (l *Loop) Start() error {
	if err := l.enter(true); err != nil {
		return err
	}
	go func() {
		var err error
		defer func() { l.exit(err) }()
		err = l.loop()
	}()
	return nil
}

// Stop ends the running loop, started by Run or Start, after the callback
// or task it is running returns (or Run's fn), and waits until it has
// ended. Timers, immediates, holds and posted tasks stay pending for the
// next Run or Start. Stop returns what ended the run it waited for, or the
// last run when none was running, as Run returns it: nil, the error that
// stopped it (Options.OnError), the *moejs.InterruptedError of an
// interrupt or ErrTerminated; for a loop started with Start it is how the
// host learns of that error. It must not be called on the loop, where it
// would wait for itself; StopNoWait may be.
func (l *Loop) Stop() error {
	l.mu.Lock()
	r := l.cur
	if r == nil {
		defer l.mu.Unlock()
		return l.lastErr
	}
	l.stop = true
	l.mu.Unlock()
	l.signal()
	<-r.done
	return r.err
}

// StopNoWait is Stop without the wait: the loop ends after the callback or
// task it is running returns. It may be called on the loop.
func (l *Loop) StopNoWait() {
	l.mu.Lock()
	if l.cur != nil {
		l.stop = true
	}
	l.mu.Unlock()
	l.signal()
}

// Terminate ends the loop for good: it interrupts the runtime
// (Runtime.Interrupt with ErrTerminated), which stops the JavaScript the
// loop runs, drops every pending timer, immediate and task, and waits until
// a running loop has ended. Run and Start then return ErrTerminated,
// RunOnLoop returns false, the done function of Hold and the settle
// function of NewPromise do nothing, and setTimeout, setInterval and
// setImmediate throw. The interrupt is left pending also when no
// JavaScript was running, so the host calls Runtime.ClearInterrupt before
// it uses the runtime again; a new Loop may be created for it then. It
// must not be called on the loop, where it would wait for itself.
func (l *Loop) Terminate() {
	l.mu.Lock()
	if !l.terminated {
		l.terminated = true
		// Before the loop can see terminated, so that the interrupt never
		// lands after a ClearInterrupt the host makes once Run returned.
		l.rt.Interrupt(ErrTerminated)
		l.drop()
	}
	r := l.cur
	l.mu.Unlock()
	l.signal()
	if r != nil {
		<-r.done
	}
}

// Interrupt interrupts the runtime (Runtime.Interrupt with v) and wakes the
// loop, which Runtime.Interrupt alone leaves asleep until its next timer
// or task. The JavaScript running stops, and the loop stops before it takes
// another timer, task or immediate off its queues, which keep them: Run
// returns the *moejs.InterruptedError, and Stop for a loop started with
// Start. As after Runtime.Interrupt, the interrupt stays pending, so the
// host calls Runtime.ClearInterrupt before it runs the loop or uses the
// runtime again. Interrupt may be called from any goroutine, the loop
// included.
func (l *Loop) Interrupt(v any) {
	l.rt.Interrupt(v)
	l.signal()
}

// RunOnLoop posts fn to the loop, which calls it with the runtime in its
// next tasks phase, and reports true. It may be called from any goroutine,
// the loop included. When the loop does not run, fn waits for the next Run
// or Start; a posted task keeps Run alive until it ran. RunOnLoop returns
// false once Terminate ended the loop, and fn is dropped.
func (l *Loop) RunOnLoop(fn func(*moejs.Runtime)) bool {
	if fn == nil {
		l.mu.Lock()
		defer l.mu.Unlock()
		return !l.terminated
	}
	return l.post(func(rt *moejs.Runtime) error {
		fn(rt)
		return nil
	})
}

// Hold keeps the loop alive for host work in progress, until done is
// called. done may be called from any goroutine: it posts fn to the loop as
// RunOnLoop does (fn may be nil) and releases the hold. The first call
// counts and later calls do nothing; after Terminate, done drops fn. Hold
// may be called from any goroutine, but only a hold taken on the loop (in a
// host function or a callback) keeps a running Run from returning: one
// taken on another goroutine races with a Run that has nothing else left,
// and almost always loses it, so that it only keeps the next Run alive.
func (l *Loop) Hold() (done func(fn func(*moejs.Runtime))) {
	release := l.hold()
	return func(fn func(*moejs.Runtime)) {
		if fn == nil {
			release(nil)
			return
		}
		release(func(rt *moejs.Runtime) error {
			fn(rt)
			return nil
		})
	}
}

// NewPromise creates a pending promise for a host function to return, and
// keeps the loop alive until settle is called. It is called on the loop.
// settle may be called from any goroutine: the first call posts f to the
// loop, which settles the promise with what f returns there, and later
// calls do nothing. A nil error fulfills it with the value, or adopts the
// value when it is a thenable; any other error rejects it with the value
// JavaScript would catch from a host function that returned the error
// (Runtime.ThrownValue). An *moejs.InterruptedError leaves it pending and
// stops the loop. A nil f fulfills it with undefined. The jobs settling
// queues run before the loop goes on; what they throw, and what settling
// fails with (moejs.ErrForeign), goes to Options.OnError.
func (l *Loop) NewPromise() (p moejs.Value, settle func(f func(*moejs.Runtime) (moejs.Value, error))) {
	p, resolve, reject := l.rt.NewPromise()
	release := l.hold()
	return p, func(f func(*moejs.Runtime) (moejs.Value, error)) {
		release(func(rt *moejs.Runtime) error {
			v := moejs.Undefined()
			if f != nil {
				var err error
				if v, err = f(rt); err != nil {
					reason, ok := rt.ThrownValue(err)
					if !ok {
						return err
					}
					return reject(reason)
				}
			}
			return resolve(v)
		})
	}
}

// hold takes a hold and returns the function that releases it once,
// posting task when it is not nil.
func (l *Loop) hold() func(task func(*moejs.Runtime) error) {
	l.mu.Lock()
	l.holds++
	l.mu.Unlock()
	released := false
	return func(task func(*moejs.Runtime) error) {
		l.mu.Lock()
		if released {
			l.mu.Unlock()
			return
		}
		released = true
		l.holds--
		if task != nil && !l.terminated {
			l.tasks = append(l.tasks, task)
		}
		l.mu.Unlock()
		l.signal()
	}
}

// post queues a task, reporting false once the loop is terminated.
func (l *Loop) post(task func(*moejs.Runtime) error) bool {
	l.mu.Lock()
	if l.terminated {
		l.mu.Unlock()
		return false
	}
	l.tasks = append(l.tasks, task)
	l.mu.Unlock()
	l.signal()
	return true
}

// signal wakes a sleeping loop.
func (l *Loop) signal() {
	select {
	case l.wake <- struct{}{}:
	default:
	}
}

// enter marks the loop running, started by Start when background.
func (l *Loop) enter(background bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	switch {
	case l.terminated:
		return ErrTerminated
	case l.cur != nil:
		return ErrRunning
	}
	l.cur = &run{done: make(chan struct{})}
	l.background, l.stop, l.lastErr, l.err = background, false, nil, nil
	return nil
}

// exit marks the loop not running, after a run that ended with err.
func (l *Loop) exit(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.terminated {
		// What the run had in hand when Terminate dropped the rest.
		l.drop()
		err = ErrTerminated
	}
	l.cur.err = err
	close(l.cur.done)
	l.cur, l.background, l.stop, l.lastErr = nil, false, false, err
}

// halted reports whether the running loop must end now. A pending
// interrupt becomes the error that stops it here, before the caller takes
// anything off a queue, so that what the interrupt stops stays pending; it
// is the run's result also when a stop was asked for too. l.mu is held.
func (l *Loop) halted() bool {
	if l.err == nil {
		l.err = l.rt.Realm().CheckInterrupt()
	}
	return l.err != nil || l.stop || l.terminated
}

// drop discards every pending timer, immediate and task. l.mu is held.
func (l *Loop) drop() {
	for _, t := range l.timers {
		t.index, t.active = -1, false
	}
	for _, im := range l.immediates {
		im.pending, im.ref = false, false
	}
	clear(l.timers)
	l.timers = l.timers[:0]
	clear(l.ids)
	clear(l.immediates)
	l.immediates = nil
	clear(l.tasks)
	l.tasks = nil
	l.refs = 0
}

// now is the loop's clock: the monotonic time since New.
func (l *Loop) now() int64 { return int64(time.Since(l.epoch)) }

// loop runs iterations until the loop must end, and returns the error that
// stopped it.
func (l *Loop) loop() error {
	sleep := time.NewTimer(time.Hour)
	sleep.Stop()
	defer sleep.Stop()
	for l.wait(sleep) && !l.runTimers() && !l.runTasks() && !l.runImmediates() {
	}
	return l.err
}

// fail handles the error of a callback or task on the loop and reports
// whether it stops the loop.
func (l *Loop) fail(err error) bool {
	if err == nil {
		return false
	}
	// Only an interrupt itself, as the engine sees one: a host function's
	// error that wraps one is an Error JavaScript can catch.
	if _, ok := err.(*moejs.InterruptedError); ok || l.onError == nil {
		l.err = err
		return true
	}
	l.onError(err)
	return false
}

// runTimers is the timers phase: it runs the timers due when it starts, in
// order of expiry, then of scheduling, and reports whether the loop must
// end. A timer set or refreshed meanwhile is due at least 1 ms after the
// phase started, so it waits for a later iteration.
func (l *Loop) runTimers() bool {
	now := l.now()
	for {
		l.mu.Lock()
		if l.halted() {
			l.mu.Unlock()
			return true
		}
		if len(l.timers) == 0 || l.timers[0].when > now {
			l.mu.Unlock()
			return false
		}
		t := l.timers.pop()
		fn, this, args := t.fn, t.this, t.args
		// Node reschedules an interval from the time its callback starts.
		start := l.now()
		l.mu.Unlock()
		err := l.call(fn, this, args)
		l.mu.Lock()
		switch {
		case !t.active || l.terminated:
			// Cleared by its callback, or dropped by Terminate.
		case t.repeat:
			if t.index >= 0 {
				// Refreshed by its callback: the interval wins, as in Node.
				l.timers.remove(t)
			}
			t.when = start + t.delay
			l.push(t)
		case t.index < 0:
			// A timeout its callback did not refresh. As in Node, it was
			// active, and clearTimeout found it by its id, until now.
			l.deactivate(t)
		}
		l.mu.Unlock()
		if l.fail(err) {
			return true
		}
	}
}

// runTasks is the tasks phase: it runs the tasks posted before it started
// and reports whether the loop must end.
func (l *Loop) runTasks() bool {
	l.mu.Lock()
	batch := l.tasks
	l.tasks = nil
	l.mu.Unlock()
	i := 0
	// What a stop, an error or a panic of a task leaves of the batch goes
	// back in front of the queue; the panic goes on.
	defer func() {
		if i < len(batch) {
			l.mu.Lock()
			l.requeueTasks(batch[i:])
			l.mu.Unlock()
		}
	}()
	for i < len(batch) {
		l.mu.Lock()
		halt := l.halted()
		l.mu.Unlock()
		if halt {
			return true
		}
		task := batch[i]
		batch[i] = nil
		i++
		l.rt.ResetAllocation()
		if l.fail(task(l.rt)) {
			return true
		}
	}
	return false
}

// requeueTasks puts the tasks a run left unrun back in front of the
// queue. l.mu is held.
func (l *Loop) requeueTasks(rest []func(*moejs.Runtime) error) {
	if len(rest) != 0 && !l.terminated {
		l.tasks = append(rest[:len(rest):len(rest)], l.tasks...)
	}
}

// runImmediates is the check phase: it runs the immediates set before it
// started, in order, and reports whether the loop must end.
func (l *Loop) runImmediates() bool {
	l.mu.Lock()
	batch := l.immediates
	l.immediates = nil
	l.mu.Unlock()
	i := 0
	// As in runTasks: OnError may panic.
	defer func() {
		if i < len(batch) {
			l.mu.Lock()
			l.requeueImmediates(batch[i:])
			l.mu.Unlock()
		}
	}()
	for i < len(batch) {
		im := batch[i]
		l.mu.Lock()
		if l.halted() {
			l.mu.Unlock()
			return true
		}
		i++
		if !im.pending {
			l.mu.Unlock()
			continue
		}
		fn, this, args := im.fn, im.this, im.args
		l.settleImmediate(im)
		l.mu.Unlock()
		if l.fail(l.call(fn, this, args)) {
			return true
		}
	}
	return false
}

// call calls a callback, with a new budget of the runtime's memory limit.
func (l *Loop) call(fn, this moejs.Value, args []moejs.Value) error {
	l.rt.ResetAllocation()
	_, err := l.rt.CallFunction(fn, this, args...)
	return err
}

// requeueImmediates puts the immediates a run left unrun back in front of
// the queue. l.mu is held.
func (l *Loop) requeueImmediates(rest []*immediate) {
	if len(rest) != 0 && !l.terminated {
		l.immediates = append(rest[:len(rest):len(rest)], l.immediates...)
	}
}

// wait starts an iteration. It reports false when the loop must end: it
// was asked to, an error stopped it, or nothing keeps a loop started by Run
// alive, so that an unref'd timer or immediate does not run then.
// Otherwise it returns at once when tasks or immediates are pending, else
// when the next timer is due or a task is posted.
func (l *Loop) wait(sleep *time.Timer) bool {
	l.mu.Lock()
	if l.halted() || !(l.background || l.refs > 0 || l.holds > 0 || len(l.tasks) > 0) {
		l.mu.Unlock()
		return false
	}
	if len(l.tasks) > 0 || len(l.immediates) > 0 {
		l.mu.Unlock()
		return true
	}
	d := time.Duration(-1)
	if len(l.timers) > 0 {
		d = max(time.Duration(l.timers[0].when-l.now()), 0)
	}
	l.mu.Unlock()
	switch {
	case d == 0:
	case d < 0:
		<-l.wake
	default:
		sleep.Reset(d)
		select {
		case <-l.wake:
			sleep.Stop()
		case <-sleep.C:
		}
	}
	return true
}
