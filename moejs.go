// Package moejs embeds the moejs JavaScript engine in a Go host that runs
// ES modules as plugins: compile a module once, evaluate it in any number of
// runtimes, call its exported functions with Go data and read their results
// back as Go data or JSON bytes.
//
//	mod, err := moejs.Compile("plugin.js", source)
//	hook, err := mod.Hook("protocols", "openai", "decodeRequest")
//	rt := moejs.NewRuntime(moejs.Options{})
//	err = rt.SetGlobal("utils", map[string]any{"now": moejs.NativeFunc(now)})
//	err = rt.Load(mod)
//	arg, err := rt.ParseJSON(body)
//	res, err := rt.Call(hook, arg)
//	out, err := rt.AppendJSON(nil, res)
//
// Values are engine values (package engine) under their own names, so host
// functions and the host share one representation and nothing is wrapped on
// the way in or out. Errors are returned, never panicked: a JavaScript throw
// is an *Exception, an interrupt an *InterruptedError, bad source a
// *SyntaxError.
//
// The jobs JavaScript queues (promise reactions, queueMicrotask callbacks)
// run before the method that ran it returns, getters and proxy traps
// included (Load, Call, Has, Get, ToGo, AppendJSON, SetGlobal, the settlers
// of NewPromise): the first exception a callback throws is the method's
// error when it succeeded otherwise, and an interrupt drops the jobs left.
//
// A module that imports others is linked once with the modules it imports,
// which the host resolves (moejs never reads files or the network):
//
//	mod, err := moejs.Link(entry, func(referrer moejs.Referrer, specifier string) (*moejs.Module, error) {
//		return host.modules[specifier], nil // compiled once, by the host
//	})
//
// A runtime that loads the linked module evaluates each module of its graph
// once; its hooks and exports are the entry's, re-exported names included.
// import() and import.meta are the host's too, through the Importer of
// Options, which resolves with the same kind of resolver:
//
//	rt := moejs.NewRuntime(moejs.Options{Importer: &moejs.Importer{Resolve: resolve}})
//
// A module import() loads joins the runtime's modules: one the runtime
// evaluated before is not evaluated again.
//
// A Module is immutable and may be loaded by many runtimes concurrently. A
// Runtime is one global environment with one loaded module; it must be used
// from one goroutine at a time, except Interrupt and ClearInterrupt.
package moejs

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/Calcium-Ion/moejs/engine"
)

type (
	// Value is a JavaScript value.
	Value = engine.Value
	// Realm is the engine state a Runtime owns; host functions receive it.
	Realm = engine.Realm
	// Object is a JavaScript object.
	Object = engine.Object
	// NativeFunc is a host function. Returning an error throws: an
	// *Exception or *InterruptedError as is, any other error as an Error
	// whose message is err.Error() and which unwraps to err.
	NativeFunc = engine.NativeFunc
	// Exception is a thrown JavaScript value.
	Exception = engine.Exception
	// InterruptedError is returned when Interrupt stopped running code.
	InterruptedError = engine.InterruptedError
)

// Arg returns args[i], or undefined when there are fewer arguments.
var Arg = engine.Arg

// Value constructors, for host functions. None allocates except String.
var (
	Undefined = engine.Undefined
	Null      = engine.Null
	Bool      = engine.Bool
	Number    = engine.NumberValue
	Int       = engine.Int64Value
)

// String converts a Go string.
func String(s string) Value { return engine.StringValue(engine.FromGoString(s)) }

var (
	// ErrHookNotFound is returned when a hook's export or one of its members
	// is missing, undefined or null.
	ErrHookNotFound = errors.New("moejs: hook not found")
	// ErrNotCallable is returned when a hook names a value that is not a
	// function.
	ErrNotCallable = errors.New("moejs: hook is not a function")
	// ErrModulePending is Load's error for a module with top-level await
	// whose evaluation still awaits once no job is left.
	ErrModulePending = engine.ErrModulePending
)

// SyntaxError is a parse or early error in module source.
type SyntaxError struct {
	File    string
	Line    int // 1-based
	Column  int // 1-based, in code points
	Message string
}

// Error formats as "file:line:col: SyntaxError: message".
func (e *SyntaxError) Error() string {
	return e.File + ":" + strconv.Itoa(e.Line) + ":" + strconv.Itoa(e.Column) + ": SyntaxError: " + e.Message
}

// InternalError is a Go panic that escaped the engine: an engine bug or a
// host function that panicked. The runtime's call state is restored, but the
// host should drop the runtime.
type InternalError struct {
	Value any    // the recovered panic value
	Stack []byte // the goroutine stack at the panic
}

func (e *InternalError) Error() string {
	return fmt.Sprintf("moejs: internal error: %v", e.Value)
}

// Unwrap returns the panic value when it is an error.
func (e *InternalError) Unwrap() error {
	err, _ := e.Value.(error)
	return err
}
