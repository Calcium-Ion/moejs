# moejs guide

The [package documentation](https://pkg.go.dev/github.com/Calcium-Ion/moejs)
describes every function, and the [README](../README.md) has a complete
example.

[简体中文](guide.zh_CN.md)

## Runtimes and pools

### Modules and runtimes

A `Module` is immutable, and any number of runtimes can load it at the same
time. A host compiles each plugin once and keeps a pool of runtimes for it.

One goroutine at a time uses a `Runtime`. Other goroutines can call only
`Interrupt` and `ClearInterrupt`.

### Returning a runtime to its pool

When a request is done with a runtime, the host calls `rt.ReleaseCallData()`
and then returns the runtime to its pool. The idle runtime then lets go of
the request's arguments. A request is done after its last `Call`, `ToGo`,
`Get` or `AppendJSON`, because each of them can run JavaScript.

`Call` leaves this step to the host. A request that runs several hooks then
converts its data once, and a host without a pool skips the step.

Values already returned stay valid. Data the module stored stays alive along
with everything it references, so an object the module kept from an argument
can keep the whole argument alive.

### Interrupts

`Interrupt(v)` stops running code, and any goroutine can call it. The pending
call returns an `*InterruptedError` that carries `v`. A script stops at the
next loop back-edge or call, and long-running builtins check for the
interrupt while they work.

An interrupt that arrives while nothing runs stops the next call, so the
host calls `ClearInterrupt` before it reuses the runtime.

### Objects belong to one runtime

Objects that a runtime's JavaScript creates belong to that runtime. Using an
object of another runtime can run that runtime's code, so pass only Go values
and JSON between runtimes.

`Call`, `SetGlobal`, `FromGo` and the settle functions of `NewPromise` return
`ErrForeign` for a function or generator of another runtime, or for a bound
function or proxy of one. Inside the Go containers that `FromGo` converts,
reading such a function throws a `TypeError` with `ErrForeign`'s message.

The check covers the value passed and the members of Go containers. It skips
the properties of JavaScript objects, and it skips a proxy of another runtime
that cannot be called, even inside a Go map. Reading that proxy runs its
traps in the runtime that reads it.

## Code

### Hooks

`Module.Hook(export, members...)` resolves an exported function, or a
function below an exported object, once:
`mod.Hook("protocols", "openai", "decodeRequest")`. The binding stays live.
Each `Call` reads the export in the current runtime and walks the members as
own properties.

A path that leads nowhere gives `ErrHookNotFound`, and a path that ends at a
value other than a function gives `ErrNotCallable`. `Has` returns false for
both.

### Module graphs

The host links a module that imports other modules before runtimes load it.
`moejs.Link(entry, resolve)` asks the host's `Resolver` for the module each
specifier names, once per module and specifier, and links the whole graph
once. Every module in the graph comes from the resolver.

`Link` returns the `Module` to load. Like any module, it is immutable and can
be shared. Its `Hook`, `Export` and `Exports` refer to the entry module's
exports, including re-exported names.

The resolver's `referrer` is the importing `*Module`, typed as `Referrer`.
`Referrer` is a sealed interface for the code that requests a module, and
its value is a `*Module` or a `*Script`.

Each runtime instantiates and evaluates the linked graph, each module once.
A module that several graphs share is compiled once if the resolver returns
the same `*Module` for it.

A failed resolution is a `*ResolveError`. An import that does not resolve to
exactly one binding is a `*SyntaxError`. Both carry the position in the
importing module. `Load` returns an error for a module that has imports and
was not linked. A runtime loads a module without imports directly, with no
`Link` step.

```go
entry, err := moejs.Compile("plugin.js", source)
mod, err := moejs.Link(entry, func(referrer moejs.Referrer, specifier string) (*moejs.Module, error) {
	return host.module(specifier) // compiled once per process
})
err = rt.Load(mod) // per runtime
```

### Dynamic import

`Options.Importer` is the host's side of `import()` and `import.meta`. Its
`Resolve` is a `Resolver`, and the `referrer` it receives is the importing
`*Module` or the `*Script` that `RunScript` ran. Its optional `Meta` fills a
module's `import.meta`, which is a null-prototype object created on first
use.

`import(specifier)` calls `Resolve` right away and returns a promise. The
promise resolves to the module's namespace, or rejects with the error from
resolving, linking or evaluating. Without an `Importer`, it rejects with a
`TypeError`.

A runtime evaluates each module once, whether it was imported statically or
dynamically. The graph that `import()` loads is linked once per `Importer`.
Any number of runtimes can share one `Importer`, and each of them only
instantiates and evaluates the graph. Jobs and interrupts behave as they do
for `Load` and `Call`. Only code that uses `import()` or `import.meta` pays
for them.

```go
imp := &moejs.Importer{Resolve: func(referrer moejs.Referrer, specifier string) (*moejs.Module, error) {
	return host.module(specifier)
}}
rt := moejs.NewRuntime(moejs.Options{Importer: imp}) // one imp for every runtime
```

### Scripts

`CompileScript(name, source)` compiles a classic script into an immutable
`*Script`. The script is sloppy unless it starts with a `"use strict"`
directive. `Runtime.RunScript` runs it in the runtime's global environment
and returns its completion value.

The script's `var` and function declarations become properties of the global
object. Its `let`, `const` and `class` declarations become global bindings
that later scripts and loaded modules can see. A declaration that conflicts
with an existing global binding throws before any code runs.

`SetGlobal` writes to the global object. After a script declares `let x`,
the global lexical binding `x` shadows a value set with `SetGlobal("x", …)`
(ECMA-262 9.1.1.4.1).

### Eval

Importing the `moejs` package installs the compiler that `eval`, the
`Function` constructors and `Realm.EvalScript(name, source)` use. A native
function can call `EvalScript` on the `*Realm` it receives to run a classic
script from a string, like test262's `$262.evalScript`.

A direct `eval` sees the caller's bindings, including a module's imports.
Stack traces show the evaluating script or module as the source of the
evaluated code and of the functions it creates. `import()` in that code also
passes the evaluating script or module to the `Resolver` as `referrer`. The
`referrer` is nil for code from an indirect `eval` or a `Function`
constructor when the calling script or module uses none of `import()`,
`import.meta` and direct `eval` (see [TODO.md](../TODO.md)).

Only code that uses `eval` or `with` pays for them.

### Limits on dynamic code

`Options.MaxDynamicSource` caps the length of the source text that `eval`,
the `Function` constructors and `Realm.EvalScript` compile. The default is
1 MiB of UTF-8, and a negative value removes the cap. Longer text throws a
`RangeError` before it is parsed. Compile time and memory grow linearly with
the text, so the cap bounds both. An interrupt can stop a compile in
progress.

`Options.DisableDynamicCode` turns dynamic code off for a runtime. `eval`,
the `Function` constructors and `EvalScript` then throw an `EvalError`.

`Compile` and `CompileScript`, which the host calls itself, ignore both
options.

## Values

### Go values in

`FromGo` converts these Go types: `nil`, booleans, numbers, strings,
`json.Number`, `*big.Int` (to a BigInt), `Value`, `NativeFunc`, and the
JSON-shaped containers `map[string]any`, `[]any`, `map[string]string`,
`[]string`, `map[string][]string` and `[]map[string]any`.

`FromGo` converts containers lazily, one level at a time, when JavaScript
first reads them, so a hook pays only for the parts of its arguments it
reads. A JavaScript write changes the JavaScript object and leaves the Go
value as it is. The host must leave the value unchanged while JavaScript can
still read it.

Objects converted from Go maps enumerate their keys in sorted order.

A `[]byte` becomes an `ArrayBuffer` over the same bytes, so JavaScript writes
to it change the Go slice.

Structs and other types return an error. Marshal them to JSON and pass the
bytes to `ParseJSON`.

`SetGlobal(name, v)` converts `v` the same way, so a host can install a
namespace of functions as a `map[string]any` of `NativeFunc` values.
`Function(name, length, fn)` wraps a `NativeFunc` and sets the `name` and
`length` that JavaScript sees.

### JSON in

`ParseJSON(b)` runs `JSON.parse` on the bytes. Use it for arguments the host
holds as JSON, such as stored task data or a request body. It copies the
bytes into a string first.

### Go values out

`ToGo` converts:

- integral numbers to `int64`, and other numbers to `float64`
- arrays to `[]any`
- objects to a `map[string]any` of their own enumerable properties
- a `Date` to `time.Time`
- a BigInt to `*big.Int`
- an `ArrayBuffer`, typed array or `DataView` to a `[]byte` copy of the
  bytes it holds or views

An argument that `FromGo` converted comes back as the original Go value as
long as JavaScript has not modified it.

`Get(v, key)` reads one property and runs getters. `Export(name)` reads the
current value of an export of the loaded module.

### JSON out

`AppendJSON` runs `JSON.stringify` and appends the text to a byte slice. Its
output differs from `json.Marshal` of `ToGo`'s result: it omits members
whose value is `undefined`, writes NaN and ±Infinity as `null`, keeps
insertion order and calls `toJSON`.

`AppendJSON` reserves the last output's length plus an eighth and writes
into the slice's spare capacity. A host that passes its previous output back
(`buf, err = rt.AppendJSON(buf[:0], v)`) allocates in two cases: the slice
has less room than that, or the value contains something that can run code.
Those are a `toJSON` method (including `Date`'s), a getter, a proxy, a
BigInt, and an object or array whose prototype is something other than
`Object.prototype` or `Array.prototype`, such as a class instance, a `Map`
or `Object.create(null)`. For those values the output goes to a separate
buffer that grows as it is written, and is appended to the slice at the end.

The output is limited to 2^30−24 bytes.

### Into the host's types

`Unmarshal(v, target)` stores a value into a Go value with the same result
as `json.Unmarshal` of `AppendJSON`'s text. Plain objects, arrays and
arguments returned unmodified go in directly, without producing text or
copying strings. Other values, such as a `toJSON`, a getter or a target type
that unmarshals itself, go through the text.

When `AppendJSON` would fail (a BigInt, a cycle, a throwing `toJSON`, a Go
value it cannot write, text past the length limit), `target` keeps its old
contents. When `json.Unmarshal` returns an error, `target` holds what it
wrote before the error.

### Strings kept after the request

Strings that `ToGo` and `Unmarshal` return from a value `ParseJSON` produced
can share memory with the parsed text and keep it alive. Call
`strings.Clone` on the strings the host keeps.

### Nesting

`ParseJSON`, `ToGo` and `AppendJSON` return a `RangeError` when arrays and
objects nest deeper than 10,000 levels.

## Promises and errors

### Promises

`NewPromise` gives a host function a promise to return, along with Go
functions that settle it later. `PromiseResult` reads a promise's state and
result. `SetPromiseRejectionTracker` reports rejections that no handler
caught, like Sobek's tracker.

### Errors

A JavaScript throw comes back as an `*Exception`. `Name()` and `Message()`
read the `name` and `message` data properties of the thrown value. For a
thrown primitive they use the value itself. Neither runs JavaScript.

When a host function returns a Go error, JavaScript sees a thrown `Error`
whose message is the error's text. The resulting `*Exception` unwraps to the
original Go error.

An interrupt is an `*InterruptedError`. A malformed module or script is a
`*SyntaxError` with its position. A Go panic during a call, for example in a
host function, is an `*InternalError`, and the runtime can still be used
afterwards.

`Runtime.StackTrace(exc)` returns the V8-format `stack` of a thrown `Error`,
also without running JavaScript.

## Isolation

### Shared frozen builtins

By default all runtimes share one deeply frozen set of builtins, so every
plugin sees the same builtins. This is the Hardened JavaScript model (SES
`lockdown()`). Writing to
`Array.prototype` throws a `TypeError` in strict code and fails silently in
sloppy code, as it does for any frozen object.

The global bindings of the builtins are locked too. `Promise = MyPromise` is
ignored in sloppy code and throws in strict code, and
`delete globalThis.Array` returns false. A plugin that wants its own
`Promise` declares a binding with that name in its module.

A write to a shared builtin fails before any setter runs. This also applies
to the legacy `RegExp.input = v` and `RegExp.$_ = v`. `RegExp.$1` and the
other legacy static properties read the runtime's own last match.

`Options{MutableIntrinsics: true}` gives each runtime its own mutable copy of
the builtins, for hosts whose plugins patch them. Creating such a runtime
costs about 25 times as much.

### Time zone per runtime

`Options.TimeZone` sets the local time zone of `Date`. nil means
`time.Local`.

## Implementation

### 16-byte values

A `Value` is 16 bytes: an `unsafe.Pointer` and a `uint64`. Numbers,
booleans, `undefined` and `null` need no allocation. The pointer word always
holds a valid pointer or nil, so Go's garbage collector can scan it safely.

### Shapes and inline caches

Objects share an immutable tree of shape transitions, ordered by property
insertion. Property access instructions carry inline cache slots, and one
counter tells whether prototype chains are still valid. Objects converted
from Go maps share a shape per key set, so plugin code that reads `ctx.xxx`
stays monomorphic across calls.

### Register bytecode VM

The VM runs fixed-width 32-bit register instructions on one contiguous
register stack per runtime. Calls allocate nothing. Exceptions unwind
through handler tables.

### Strings

A string is ASCII (a Go string, used without copying), UTF-16, or a rope
that is flattened when needed. Builtin property names are static atoms.

### Locks

Property access, calls, strings, host conversion and regular expression
matching run without locks. Runtimes on different goroutines share only the
garbage collector.

### Regular expressions

A regular expression runs on Go's `regexp` (RE2, linear time) when moejs can
translate it exactly, and on a pure-Go backtracking engine otherwise. The
backtracking engine has a bounded stack and checks for an interrupt every
4,096 steps.

### Bounded caches

A pooled runtime's caches stay within fixed limits, even when every request
brings objects keyed by data (ids, user values, host map keys). Each intern
cache holds at most 4,096 names. The host shape cache holds at most 1,024
key sets with 8,192 keys in total. When a cache is full, the runtime empties
it, and it refills with what is in use. A shape holds its first 8 transitions
strongly and the rest weakly, so the garbage collector frees the shapes of
data-keyed objects along with the objects.
