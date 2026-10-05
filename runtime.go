package moejs

import (
	"errors"
	"runtime/debug"
	"slices"
	"strconv"
	"time"
	"unsafe"

	"github.com/Calcium-Ion/moejs/engine"
)

// Options configures NewRuntime.
type Options struct {
	// MutableIntrinsics gives the runtime its own mutable copy of the
	// builtins (prototypes, constructors, Math, JSON). By default they are
	// built once per process, deeply frozen and shared by every runtime, and
	// writing to them throws a TypeError.
	MutableIntrinsics bool
	// TimeZone is the local time zone of Date; nil means time.Local.
	TimeZone *time.Location
	// Importer resolves the modules of import() and fills import.meta; nil
	// means import() rejects with a TypeError.
	Importer *Importer
	// MaxDynamicSource is the length in UTF-8 bytes of the longest source
	// text eval, the Function constructors and Realm.EvalScript compile:
	// longer text throws a RangeError. Zero
	// means engine.DefaultMaxDynamicSource (1 MiB), a negative value no
	// limit (engine.RealmOptions). Compile and CompileScript have no limit.
	MaxDynamicSource int
	// DisableDynamicCode makes eval of a string, the Function constructors
	// and Realm.EvalScript throw an EvalError instead of compiling anything,
	// for hosts whose code needs none (engine.RealmOptions). Compile and
	// CompileScript are not affected.
	DisableDynamicCode bool
}

// Runtime is one JavaScript global environment with at most one loaded
// module. It must be used from one goroutine at a time; only Interrupt and
// ClearInterrupt may be called concurrently.
//
// The objects its JavaScript creates are its own. Any object of another
// runtime can run that runtime's code when it is used, not only a function
// or generator but also through a method, an accessor, a proxy, a promise
// or a thenable, and that code reads the caches of the runtime running it:
// wrong results or an *InternalError. Only Go values (ToGo, FromGo) and
// JSON are safe to pass between runtimes. Call, SetGlobal, FromGo and the
// settlers of NewPromise return ErrForeign for a function or generator of
// another runtime (or a bound function or proxy of one), FromGo also for one
// inside the Go containers it converts (reading that member throws); they do
// not look inside other objects, and do not check a proxy of another runtime
// that is not callable, even inside a Go map: its traps run in the runtime
// that reads it.
type Runtime struct {
	realm *engine.Realm
	mod   *Module
	env   *engine.ModuleEnv
	// failed is what the module's top level threw or was interrupted with
	// before it ended or first awaited, which Call and Has return for its
	// hooks.
	failed error
	// argStack backs the argument slices Call hands to the engine, so the
	// caller's variadic slice does not escape and a call allocates nothing
	// for its arguments once the stack has grown.
	argStack []Value
}

// NewRuntime creates a runtime.
func NewRuntime(opts Options) *Runtime {
	rt := &Runtime{realm: engine.NewRealmWith(engine.RealmOptions{SharedIntrinsics: !opts.MutableIntrinsics, TimeZone: opts.TimeZone, MaxDynamicSource: opts.MaxDynamicSource, DisableDynamicCode: opts.DisableDynamicCode})}
	if opts.Importer != nil {
		rt.realm.SetImportHooks(opts.Importer.engineHooks())
	}
	return rt
}

// Realm returns the runtime's engine state, for host functions and tests
// that need the engine API directly.
func (rt *Runtime) Realm() *Realm { return rt.realm }

// SetGlobal assigns the global variable name. v is converted by FromGo, so a
// host installs a namespace of functions as a map[string]any with
// NativeFunc values. It writes the global object, whose property a script's
// global let, const or class declaration of the same name shadows
// (ECMA-262 9.1.1.4.1).
func (rt *Runtime) SetGlobal(name string, v any) (err error) {
	r := rt.realm
	defer rt.guard(&err, r.CallState(), len(rt.argStack))
	val, err := rt.FromGo(v)
	if err != nil {
		return err
	}
	r.HoldJobs()
	return r.ReleaseJobs(r.Global.SetProp(r, r.KeyFromGoString(name), val))
}

// Function creates a host function with a name and a length, the values of
// its `name` and `length` properties; a negative length is 0, as no
// function's length is negative.
func (rt *Runtime) Function(name string, length int, fn NativeFunc) Value {
	return engine.ObjectValue(rt.realm.NewNativeFunction(engine.FromGoString(name), max(length, 0), fn))
}

// Load evaluates m's top level in this runtime. A runtime loads one module
// once, and a Load made while the top level runs is refused too. A module
// that imports others loads once Link linked it: Load then evaluates the
// modules of its graph first, each once, in the order of their imports; a
// failure of one of them is a failure of m's top level, which then does not
// run. In a runtime with an Importer, import() of m, or of a module of its
// graph, finds the instance Load evaluated. When the top level throws, is
// interrupted or panics (an *InternalError) before it ends or first
// awaits, the error is returned
// and the module stays loaded: Export reads
// the bindings initialized before the failure other than functions and
// module namespaces (none when an interrupt stopped the top level before it
// started), and Call and Has return the error for the module's hooks: a
// function could find the other bindings uninitialized. The
// jobs the top level queued run after it, when Export and the module's
// hooks find its bindings; while the top level itself runs they find none.
// A module with top-level await evaluates asynchronously: its top level
// resumes from those jobs, with the bindings found, and Load returns once
// none is left, with what the evaluation rejected with (an interrupt of
// those jobs wins), or ErrModulePending when it still awaits; an exported
// function called while it awaits throws a ReferenceError for a binding it
// has not initialized yet.
func (rt *Runtime) Load(m *Module) (err error) {
	if rt.mod != nil {
		return errors.New("moejs: runtime already loaded module " + rt.mod.name)
	}
	if m.graph != nil {
		return rt.loadGraph(m, m.graph.g)
	}
	if l := m.code.Module.Links; l != nil {
		if len(l.Requests) == 0 {
			// It uses import() or import.meta: its record is the realm's.
			g, err := m.selfGraph()
			if err != nil {
				return err
			}
			return rt.loadGraph(m, g)
		}
		return errors.New("moejs: module " + strconv.Quote(m.name) + " imports " + strconv.Quote(l.Requests[0].Specifier) + ": link it with moejs.Link and a resolver")
	}
	r := rt.realm
	if r.ImportHooks() != nil {
		// import() of m in r finds the instance Load evaluates.
		g, err := m.selfGraph()
		if err != nil {
			return err
		}
		return rt.loadGraph(m, g)
	}
	defer rt.guardLoad(&err, r.CallState(), len(rt.argStack))
	rt.mod = m
	r.HoldJobs()
	var p *engine.Object
	rt.env, p, err = r.EvaluateModuleAsync(m.code)
	if p != nil && err == nil {
		if state, _, _ := p.PromiseResult(); state == engine.PromiseRejected {
			err, p = r.ModuleEvaluationError(p, nil), nil
		}
	}
	rt.failed = err
	if err = r.ReleaseJobs(err); p != nil {
		err = r.ModuleEvaluationError(p, err)
	}
	return err
}

// loadGraph is Load of a module through its graph g: Link's, or the graph
// of the module alone for one that uses import() or import.meta or loads
// in a runtime with an Importer. The
// runtime evaluates the modules of g it has not evaluated, each at most
// once. The entry's hooks return what the evaluation failed with when it
// failed before the entry's top level ended or first awaited, which covers
// the failures of the modules it imports: its top level did not run then.
func (rt *Runtime) loadGraph(m *Module, g *engine.ModuleGraph) (err error) {
	r := rt.realm
	defer rt.guardGraph(&err, g, r.CallState(), len(rt.argStack))
	rt.mod = m
	r.HoldJobs()
	var p *engine.Object
	rt.env, p, err = r.EvaluateGraph(g)
	if le, ok := err.(*engine.LinkError); ok {
		// A module of g the runtime instantiated resolves a request to
		// another module in g.
		err = linkError(le)
	}
	if p != nil && err == nil {
		if state, _, _ := p.PromiseResult(); state == engine.PromiseRejected {
			err, p = r.ModuleEvaluationError(p, nil), nil
		}
	}
	rt.failed = err
	if err = r.ReleaseJobs(err); p != nil {
		err = r.ModuleEvaluationError(p, err)
		_, interrupted := err.(*InterruptedError)
		if state, _, _ := p.PromiseResult(); (state == engine.PromiseRejected || interrupted) && !r.GraphRan(g) {
			rt.failed = err
		}
	}
	return err
}

// Module returns the loaded module, or nil.
func (rt *Runtime) Module() *Module { return rt.mod }

// Export returns the current value of the loaded module's export name, a
// name it re-exports included. ok is false when there is no such export or
// the binding is not initialized yet, and, once the module's top level
// failed (Load), for a function or a module namespace object, whose code
// could find the module's other bindings uninitialized without the
// ReferenceError.
func (rt *Runtime) Export(name string) (v Value, ok bool) {
	if rt.env == nil {
		return engine.Undefined(), false
	}
	v, ok = rt.env.GetBindingValue(name)
	if !ok && rt.mod.graph != nil {
		v, ok = rt.reexport(name)
	}
	if !ok || v.IsHole() {
		return engine.Undefined(), false
	}
	if rt.failed != nil && v.IsObject() && (engine.IsCallable(v) || v.AsObject().IsModuleNamespace()) {
		return engine.Undefined(), false
	}
	return v, true
}

// reexport reads the export name of the loaded graph's entry that is not
// one of its own bindings.
func (rt *Runtime) reexport(name string) (Value, bool) {
	l := rt.mod.graph
	i, found := slices.BinarySearch(l.exports, name)
	if !found {
		return engine.Undefined(), false
	}
	return rt.realm.GraphBinding(l.g, l.bindings[i])
}

// Has reports whether h names a function in this runtime now. A getter on
// the path that throws is returned as the error, and so is Load's for the
// hooks of a module whose top level failed.
func (rt *Runtime) Has(h Hook) (ok bool, err error) {
	r := rt.realm
	defer rt.guard(&err, r.CallState(), len(rt.argStack))
	_, err = rt.lookup(h)
	switch err {
	case nil:
		return true, nil
	case ErrHookNotFound, ErrNotCallable:
		return false, nil
	}
	return false, err
}

// Call invokes the function h names with this = undefined. A path that
// does not lead to a value is ErrHookNotFound, one that leads to a value
// that is not a function ErrNotCallable; a throw is an *Exception, an
// interrupt an *InterruptedError. The hooks of a module whose top level
// failed return Load's error. An argument that is a function or generator
// of another runtime is ErrForeign.
func (rt *Runtime) Call(h Hook, args ...Value) (res Value, err error) {
	r := rt.realm
	base := len(rt.argStack)
	defer rt.guard(&err, r.CallState(), base)
	fn, err := rt.lookup(h)
	if err != nil {
		return engine.Undefined(), err
	}
	// Bytecode checks for an interrupt when it is entered, natives do not.
	// The release drops any jobs left queued, as the return of an
	// interrupted call does.
	if err := r.CheckInterrupt(); err != nil {
		r.HoldJobs()
		return engine.Undefined(), r.ReleaseJobs(err)
	}
	for _, a := range args {
		if a.IsObject() && a.AsObject().HoldsCode() && r.IsForeign(a) {
			return engine.Undefined(), ErrForeign
		}
	}
	rt.argStack = append(rt.argStack, args...)
	top := len(rt.argStack)
	res, err = r.CallObject(fn, engine.Undefined(), rt.argStack[base:top:top])
	clear(rt.argStack[base:top])
	rt.argStack = rt.argStack[:base]
	return res, err
}

// ReleaseCallData drops the runtime's references to the host values of
// finished calls, so a pooled runtime does not keep the last request alive.
// A host that pools runtimes calls it after the request's last use of the
// runtime (Call, ToGo, Get, AppendJSON: each can run JavaScript), before it
// returns the runtime to its pool; Call does not, so that a request running
// several hooks converts its data as one, and hosts that do not pool pay
// nothing. Values already returned stay valid: their nodes keep their own
// storage, so ToGo and Get of them work after it. Between calls only; a
// no-op inside one (from a host function).
//
// What JavaScript keeps stays alive with what it references: a string of a
// FromGo argument the module stores keeps every string converted in its
// period (up to 512), and an object it stores keeps its chunk and, through
// the root placeholder in it, the whole argument; a substring of an ASCII
// string shares its bytes, so storing a slice of a large one keeps all of
// it. The names the runtime keeps for its caches (property names, RegExp
// patterns) are its own copies, whether they come from map keys, a parsed
// JSON text or a slice of a string.
func (rt *Runtime) ReleaseCallData() { rt.realm.ReleaseCallData() }

// lookup walks h in the loaded module: the export, then each member as an
// own property of the value before it (an own getter runs; for a proxy the
// getOwnPropertyDescriptor and get traps). undefined, null, a primitive
// before the last member or a missing member is ErrHookNotFound. The walk
// ends as an outermost call does: the jobs its getters and traps queued run
// before it returns, and the first exception they throw is its error, also
// when the path leads nowhere.
func (rt *Runtime) lookup(h Hook) (*Object, error) {
	if h.mod != rt.mod {
		return rt.lookupOwn(h)
	}
	if rt.failed != nil {
		return nil, rt.failed
	}
	if rt.env == nil {
		return nil, ErrHookNotFound
	}
	var v Value
	if h.slot >= 0 {
		v = rt.env.Slot(h.slot)
	} else {
		l := rt.mod.graph
		v, _ = rt.realm.GraphBinding(l.g, l.bindings[-1-h.slot])
	}
	for i, k := range h.keys {
		if !v.IsObject() {
			return nil, ErrHookNotFound
		}
		next, ok := v.AsObject().GetOwnDataValue(k)
		if !ok {
			var err error
			if v, err = rt.members(v.AsObject(), h.keys[i:]); err != nil {
				return nil, err
			}
			break
		}
		v = next
	}
	switch {
	case v.IsUndefined(), v.IsNull(), v.IsHole():
		return nil, ErrHookNotFound
	case !engine.IsCallable(v):
		return nil, ErrNotCallable
	}
	return v.AsObject(), nil
}

// lookupOwn is lookup for a hook of another module than the loaded one,
// which names nothing unless it is of an export the loaded module declares
// in a Module of the same code: the one Link linked, or another Link of it.
func (rt *Runtime) lookupOwn(h Hook) (*Object, error) {
	if h.slot < 0 || h.mod == nil || rt.mod == nil || h.mod.code != rt.mod.code {
		return nil, ErrHookNotFound
	}
	h.mod = rt.mod
	return rt.lookup(h)
}

// members walks keys from o for lookup, which reads own data properties
// directly (they run no code) and hands over at the first member that is not
// one of o: a getter, a proxy, whose traps run, or a missing member. It
// holds the jobs as an outermost call does. A path that leads nowhere is
// undefined.
func (rt *Runtime) members(o *Object, keys []engine.PropertyKey) (Value, error) {
	r := rt.realm
	r.HoldJobs()
	v := engine.ObjectValue(o)
	var err error
	for _, k := range keys {
		if !v.IsObject() {
			v = engine.Undefined()
			break
		}
		o := v.AsObject()
		var has bool
		if has, err = r.HasOwn(o, k); err != nil || !has {
			v = engine.Undefined()
			break
		}
		if v, err = o.GetProp(r, k); err != nil {
			break
		}
	}
	return v, r.ReleaseJobs(err)
}

// Interrupt stops running code: the pending Call (or Load, ToGo, ...)
// returns an *InterruptedError carrying v. It may be called from any
// goroutine. An interrupt that arrives while nothing runs stops the next
// Call, whatever the function it names, or Load, and any other method once
// it runs JavaScript, so a host calls ClearInterrupt before reusing the
// runtime.
func (rt *Runtime) Interrupt(v any) { rt.realm.Interrupt(v) }

// ClearInterrupt drops a pending interrupt.
func (rt *Runtime) ClearInterrupt() { rt.realm.ClearInterrupt() }

// FromGo converts a Go value: nil, bool, the integer and float kinds,
// string, json.Number, *big.Int (a bigint), Value, NativeFunc, and the
// JSON-shaped containers map[string]any, map[string]string,
// map[string][]string, []any, []string and []map[string]any. Containers convert lazily, one level when first
// touched, so a large argument the hook reads little of costs little; the
// Go value must not change while the result is in use, and JavaScript
// writes never reach it. Maps enumerate their keys sorted. A []byte becomes
// an ArrayBuffer over the same bytes, not a copy: JavaScript writes reach
// them, and the host must not modify them while JavaScript may read them.
// Other types (structs, named map types) are an error: marshal them and use
// ParseJSON. A Value or *Object that is a function or generator of another
// runtime is ErrForeign; inside a container, reading its member throws a
// TypeError with ErrForeign's text.
func (rt *Runtime) FromGo(v any) (Value, error) {
	return rt.realm.FromGo(v)
}

// ParseJSON is JSON.parse of b. Nesting deeper than 10,000 arrays and
// objects (engine.MaxToGoDepth) is a RangeError.
func (rt *Runtime) ParseJSON(b []byte) (Value, error) {
	return rt.realm.JSONParseGoString(string(b))
}

// Get reads property key of v, running a getter and walking the prototype
// chain; undefined and null have no properties and read as undefined.
func (rt *Runtime) Get(v Value, key string) (res Value, err error) {
	if v.IsUndefined() || v.IsNull() {
		return engine.Undefined(), nil
	}
	r := rt.realm
	defer rt.guard(&err, r.CallState(), len(rt.argStack))
	r.HoldJobs()
	res, err = r.GetV(v, r.KeyFromGoString(key))
	return res, r.ReleaseJobs(err)
}

// ToGo exports v: undefined and null become nil, booleans bool, strings
// string, integral numbers in int64 range int64 (except -0), other numbers
// float64, arrays []any, other objects map[string]any of their own
// enumerable string-keyed properties, Date time.Time, bigint *big.Int, and
// functions and symbols the engine value itself (*Object, *engine.Symbol).
// An ArrayBuffer or SharedArrayBuffer exports a copy of its bytes as a
// []byte, and a typed array or DataView a copy of the bytes it views (so a
// Uint16Array of 2 elements gives 4 bytes, little-endian); a detached
// buffer and a view out of its buffer's bounds give a nil []byte, and a
// zero-length buffer or view a non-nil empty one.
// A proxy exports through its traps (see engine.Realm.ToGo): []any when its
// target is an array, the map of its enumerable keys otherwise, the *Object
// when it is callable. An array or object FromGo converted from a non-empty
// Go map or slice exports as that Go value while JavaScript has not
// modified it (engine.Object.HostValue); an empty one converted to a plain
// array or object, and a nil map to null. A getter or trap that throws, a
// revoked proxy, or nesting deeper than 10,000 containers
// (engine.MaxToGoDepth) is returned as the error. The Go string of an ASCII
// string is the one v holds: for a value ParseJSON produced it may share the
// parsed text and keep it alive; strings.Clone what is kept.
func (rt *Runtime) ToGo(v Value) (out any, err error) {
	r := rt.realm
	defer rt.guard(&err, r.CallState(), len(rt.argStack))
	r.HoldJobs()
	out, err = r.ToGoStrict(v)
	return out, r.ReleaseJobs(err)
}

// AppendJSON appends JSON.stringify(v) to dst as UTF-8. A value with no
// JSON form (undefined, a function, a symbol) appends null. A proxy is
// serialized as JSON.stringify does it: its traps run. Nesting deeper than
// 10,000 arrays and objects (engine.MaxToGoDepth) is a RangeError, and so is
// an output of more than 2^30-24 bytes: the string length limit counted in
// bytes of UTF-8, JSON.stringify's limit for ASCII text and reached first
// for other text.
//
// The output is written into dst's spare capacity, after reserving the last
// output's length plus an eighth: a host that passes its previous output
// back (buf, err = rt.AppendJSON(buf[:0], v)) allocates only when buf has
// less room than that, and after one very large output a call with a small
// dst allocates that much again. Before any JavaScript runs (a toJSON, a
// Date's included, a getter, a proxy's trap) the output moves to a buffer
// of its own, which holds twice what is written so far and grows as the
// rest is written, and is appended to dst when AppendJSON returns, so a
// host function that JavaScript calls meanwhile may append to dst or pass
// it to another AppendJSON; what it appended to dst's spare capacity is
// overwritten then, as append(dst, text...) would overwrite it. The same
// holds for a job (a promise reaction, queueMicrotask) that code queued,
// when AppendJSON is called outside any Call: the job runs before the
// output is appended to dst. Inside a host function the jobs run when the
// outermost Call returns, after AppendJSON has returned, and one that
// appends to the same dst overwrites what AppendJSON appended, as any later
// append would: a host function should use a buffer of its own.
func (rt *Runtime) AppendJSON(dst []byte, v Value) (out []byte, err error) {
	r := rt.realm
	defer rt.guard(&err, r.CallState(), len(rt.argStack))
	r.HoldJobs()
	// What a panic returns (guard), the output being suspect: a host
	// function's inside the call, or a job's at the release.
	out = dst
	res, ok, err := r.AppendJSON(dst, v)
	// The jobs a toJSON or getter queued run at the release, after the
	// output landed in dst's spare capacity; a host function they call may
	// use dst (above), so when any are pending the output first moves out.
	// A value that runs no code queues none and is not copied.
	if ok && err == nil && r.JobsPending() && len(dst) < len(res) && unsafe.SliceData(res) == unsafe.SliceData(dst) {
		text := append([]byte(nil), res[len(dst):]...)
		if err = r.ReleaseJobs(nil); err != nil {
			return dst, err
		}
		return append(dst, text...), nil
	}
	if err = r.ReleaseJobs(err); err != nil {
		return dst, err
	}
	if !ok {
		return append(dst, "null"...), nil
	}
	return res, nil
}

// StackTrace returns the `stack` of an Error thrown in this runtime:
// "Name: message" and one "    at ..." line per frame. It is "" when the
// thrown value is not an Error or its stack was replaced by a non-string.
// No user code runs.
func (rt *Runtime) StackTrace(exc *Exception) string {
	if exc == nil {
		return ""
	}
	return rt.realm.StackTrace(exc.Value)
}

// guard is deferred by every method that can run JavaScript. A Go panic
// that unwound through the engine (an engine bug or a panicking host
// function) skipped the interpreter's frame exits, so the call bookkeeping
// is reset to its value at entry and the panic becomes an *InternalError;
// at the outermost call the jobs it left queued are dropped, so that they
// do not run in the next call.
func (rt *Runtime) guard(err *error, saved engine.CallState, argBase int) {
	x := recover()
	if x == nil {
		return
	}
	rt.recovered(err, x, saved, argBase)
}

// recovered restores the runtime after the panic x and sets *err to it.
func (rt *Runtime) recovered(err *error, x any, saved engine.CallState, argBase int) {
	rt.realm.RestoreCallState(saved)
	clear(rt.argStack[argBase:])
	rt.argStack = rt.argStack[:argBase]
	*err = &InternalError{Value: x, Stack: debug.Stack()}
	rt.realm.DropJobs(*err)
}

// guardLoad is guard for Load: a panic that stopped the evaluation before
// the entry's top level ran to its end or first await fails the entry's
// hooks, as a throw does.
func (rt *Runtime) guardLoad(err *error, saved engine.CallState, argBase int) {
	if x := recover(); x != nil {
		rt.loadPanicked(err, x, nil, saved, argBase)
	}
}

// guardGraph is guardLoad for loadGraph of g.
func (rt *Runtime) guardGraph(err *error, g *engine.ModuleGraph, saved engine.CallState, argBase int) {
	if x := recover(); x != nil {
		rt.loadPanicked(err, x, g, saved, argBase)
	}
}

// loadPanicked handles the panic x of a Load, through the graph g if not
// nil.
func (rt *Runtime) loadPanicked(err *error, x any, g *engine.ModuleGraph, saved engine.CallState, argBase int) {
	rt.recovered(err, x, saved, argBase)
	ran := rt.env != nil && rt.failed == nil
	if g != nil {
		ran = rt.realm.GraphRan(g)
	}
	if !ran {
		rt.failed = *err
	}
}
