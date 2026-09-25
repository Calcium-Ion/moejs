package moejs

import (
	"errors"
	"runtime/debug"
	"time"

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
}

// Runtime is one JavaScript global environment with at most one loaded
// module. It must be used from one goroutine at a time; only Interrupt and
// ClearInterrupt may be called concurrently.
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
	return &Runtime{realm: engine.NewRealmWith(engine.RealmOptions{SharedIntrinsics: !opts.MutableIntrinsics, TimeZone: opts.TimeZone})}
}

// Realm returns the runtime's engine state, for host functions and tests
// that need the engine API directly.
func (rt *Runtime) Realm() *Realm { return rt.realm }

// SetGlobal assigns the global variable name. v is converted by FromGo, so a
// host installs a namespace of functions as a map[string]any with
// NativeFunc values.
func (rt *Runtime) SetGlobal(name string, v any) (err error) {
	r := rt.realm
	defer rt.guard(&err, r.CallState(), len(rt.argStack))
	val, err := r.FromGo(v)
	if err != nil {
		return err
	}
	r.HoldJobs()
	return r.ReleaseJobs(r.Global.SetProp(r, r.KeyFromGoString(name), val))
}

// Function creates a host function with a name and a length, the values of
// its `name` and `length` properties.
func (rt *Runtime) Function(name string, length int, fn NativeFunc) Value {
	return engine.ObjectValue(rt.realm.NewNativeFunction(engine.FromGoString(name), length, fn))
}

// Load evaluates m's top level in this runtime. A runtime loads one module
// once, and a Load made while the top level runs is refused too. When the
// top level throws or is interrupted before it ends or first awaits, the
// error is returned and the module stays loaded: Export reads the bindings
// initialized before the failure (none when an interrupt stopped the top
// level before it started), and Call and Has return the error for the
// module's hooks, whose functions could find the others uninitialized. The
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
	r := rt.realm
	defer rt.guard(&err, r.CallState(), len(rt.argStack))
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

// Module returns the loaded module, or nil.
func (rt *Runtime) Module() *Module { return rt.mod }

// Export returns the current value of the loaded module's export name. ok is
// false when there is no such export or the binding is not initialized yet.
func (rt *Runtime) Export(name string) (v Value, ok bool) {
	if rt.env == nil {
		return engine.Undefined(), false
	}
	v, ok = rt.env.GetBindingValue(name)
	if !ok || v.IsHole() {
		return engine.Undefined(), false
	}
	return v, true
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
// failed return Load's error.
func (rt *Runtime) Call(h Hook, args ...Value) (res Value, err error) {
	r := rt.realm
	base := len(rt.argStack)
	defer rt.guard(&err, r.CallState(), base)
	fn, err := rt.lookup(h)
	if err != nil {
		return engine.Undefined(), err
	}
	// Bytecode checks for an interrupt when it is entered, natives do not.
	// The release drops the jobs a Go panic left queued (out of a call body
	// before any drain, or out of a job), as the return of an interrupted
	// call does.
	if err := r.CheckInterrupt(); err != nil {
		r.HoldJobs()
		return engine.Undefined(), r.ReleaseJobs(err)
	}
	rt.argStack = append(rt.argStack, args...)
	top := len(rt.argStack)
	res, err = r.CallObject(fn, engine.Undefined(), rt.argStack[base:top:top])
	clear(rt.argStack[base:top])
	rt.argStack = rt.argStack[:base]
	return res, err
}

// lookup walks h in the loaded module: the export, then each member as an
// own property of the value before it (an own getter runs; for a proxy the
// getOwnPropertyDescriptor and get traps). undefined, null, a primitive
// before the last member or a missing member is ErrHookNotFound. The walk
// ends as an outermost call does: the jobs its getters and traps queued run
// before it returns, and the first exception they throw is its error, also
// when the path leads nowhere.
func (rt *Runtime) lookup(h Hook) (*Object, error) {
	if h.mod != rt.mod {
		return nil, ErrHookNotFound
	}
	if rt.failed != nil {
		return nil, rt.failed
	}
	if rt.env == nil {
		return nil, ErrHookNotFound
	}
	v := rt.env.Slot(h.slot)
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
// ParseJSON.
func (rt *Runtime) FromGo(v any) (Value, error) { return rt.realm.FromGo(v) }

// ParseJSON is JSON.parse of b.
func (rt *Runtime) ParseJSON(b []byte) (Value, error) {
	return rt.realm.JSONParse(engine.FromGoString(string(b)))
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
// when it is callable. A getter or trap that throws, or a revoked proxy, is
// returned as the error.
func (rt *Runtime) ToGo(v Value) (out any, err error) {
	r := rt.realm
	defer rt.guard(&err, r.CallState(), len(rt.argStack))
	r.HoldJobs()
	out, err = r.ToGoStrict(v)
	return out, r.ReleaseJobs(err)
}

// AppendJSON appends JSON.stringify(v) to dst as UTF-8. A value with no
// JSON form (undefined, a function, a symbol) appends null. A proxy is
// serialized as JSON.stringify does it: its traps run.
func (rt *Runtime) AppendJSON(dst []byte, v Value) (out []byte, err error) {
	r := rt.realm
	defer rt.guard(&err, r.CallState(), len(rt.argStack))
	r.HoldJobs()
	s, err := r.JSONStringify(v)
	if err = r.ReleaseJobs(err); err != nil {
		return dst, err
	}
	if s == nil {
		return append(dst, "null"...), nil
	}
	if a, ok := s.ASCII(); ok {
		return append(dst, a...), nil
	}
	return append(dst, s.GoString()...), nil
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
// is reset to its value at entry and the panic becomes an *InternalError.
func (rt *Runtime) guard(err *error, saved engine.CallState, argBase int) {
	x := recover()
	if x == nil {
		return
	}
	rt.realm.RestoreCallState(saved)
	clear(rt.argStack[argBase:])
	rt.argStack = rt.argStack[:argBase]
	*err = &InternalError{Value: x, Stack: debug.Stack()}
}
