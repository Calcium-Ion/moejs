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
can keep the whole argument alive. A `json.RawMessage` is copied when it
is converted, so the host can reuse its buffer for the next request. A Go
map or slice that the module stores before reading it still refers to the
host's values, `json.RawMessage` bytes inside it included, and those must
not change while the module keeps it.

### Memory

Until `ReleaseCallData`, a runtime keeps each request's input and output.
The input is the copy of the text that `ParseJSON` makes, or for `FromGo`
the host's own Go values, which it refers to rather than copies. The
exceptions are a `json.RawMessage`, whose text `FromGo` copies, and the
types it converts from their `json.Marshal` text. The live heap therefore
grows with the number of requests in flight times the size of one request,
on top of what the host itself keeps of each request. `ParseJSONString`,
and `FromGo` of the host's maps, slices and strings, add no copy of the
input's ASCII strings that have no escapes, such as base64 data. Strings
that are not ASCII are held as UTF-16 either way.

Under the default `GOGC=100`, a Go process's resident memory runs at
roughly twice its live heap, because the collector lets the heap grow by
as much as is live before it collects again. It runs higher with many
goroutines, and when long collection cycles overlap with contention for
the CPU.

To cap memory, set `GOMEMLIMIT` well above the expected peak live heap,
since the limit counts all of the Go runtime's memory and not the heap
alone, and keep `GOGC` at its default. Throughput is then unaffected until
the process nears the limit. A `GOMEMLIMIT` below the live heap makes the
collector run continuously, and throughput drops sharply. Lowering `GOGC`
trades throughput for memory across the whole process.

As an example, take an 8.1 MB JSON body with five base64 images, 32
requests in flight with `GOMAXPROCS=8`, and a host that keeps each body
until its request is done (Intel i5-13500H, Linux, go1.26.6, medians of 3
runs of 10 seconds). With `ParseJSON` the live heap was about 500 MB and
the resident memory about 1.1 GB. With `ParseJSONString` they were about
330 MB and 740 MB, and the process served 1.5 times as many requests per
second. Still with `ParseJSONString`, `GOMEMLIMIT=768MiB` left the requests
per second as they were, `GOMEMLIMIT=256MiB`, below the live heap, cut them
by 38%, and `GOGC=25` cut them by 9% and the resident memory to about
410 MB.

### Interrupts

`Interrupt(v)` stops running code, and any goroutine can call it. The pending
call returns an `*InterruptedError` that carries `v`. A script stops at the
next loop back-edge or call, and long-running builtins check for the
interrupt while they work.

An interrupt that arrives while nothing runs stops the next call, so the
host calls `ClearInterrupt` before it reuses the runtime.

A runtime that an event loop runs (`eventloop`, below) is interrupted
through `Loop.Interrupt`. `Runtime.Interrupt` alone stops the JavaScript
that is running, but does not wake a loop that sleeps until its next timer.

### Memory limit

`Options.MemoryLimit` caps the bytes one request's JavaScript may allocate
between resets. `ReleaseCallData` resets the count, and so does
`ResetAllocation`, for a host without a pool. A call that passes the limit
stops as an interrupt does: it returns an `*InterruptedError` whose value is
a `*MemoryLimitError` carrying the limit, the bytes counted and the
JavaScript stack at the allocation, and `errors.Is(err, moejs.ErrMemoryLimit)`
holds.

```go
rt := moejs.NewRuntime(moejs.Options{MemoryLimit: 64 << 20})
res, err := rt.Call(hook, arg)
if errors.Is(err, moejs.ErrMemoryLimit) {
	var mle *moejs.MemoryLimitError
	errors.As(err, &mle)
	log.Printf("plugin used %d bytes, limit %d\n%s", mle.Allocated, mle.Limit, mle.Stack)
}
rt.ClearInterrupt()
rt.ReleaseCallData() // a new budget for the next request
pool.Put(rt)
```

A script cannot catch the hit. The engine allocates without failing, so the
only way to stop a script that allocates is an interrupt, and an interrupt
runs no `catch` or `finally` and drops the queued promise jobs. A script that
caught it could go on allocating. The builtins that know a result's size
before they allocate it throw the `RangeError` they throw for a size past
the engine's limits for a size past what is left of the budget instead, and
a script can catch that: `repeat`, `padStart` and `padEnd`, `new
ArrayBuffer`, typed array constructors, `btoa`, `escape`, `toBase64` and
`toHex`.

The count is of allocation, not of live memory: garbage counts until the
next reset, and nothing a request frees is taken off. It covers objects,
the storage their properties and elements grow into, strings, `Map` and
`Set` tables, buffers, `BigInt` results, closures' environments and code
compiled by `eval` and `Function`, each by about what it asks the Go
allocator for. A rope, the result of joining two
long strings, counts its node, and its units once it is read and flattened.
`FromGo` counts the wrappers it makes for the host's maps and slices but not
the host's Go values, which it refers to without copying; `ParseJSON`
counts its copy of the text. A few internal caches of bounded size are not
counted, nor is the interpreter's register stack, which the call depth
limit bounds and `ReleaseCallData` trims. Expect the count to run within a small factor of what the request
allocates, and set the limit with room.

Setup does not count against the first request: `Load` and `SetGlobal`
are setup, and each ends by resetting the count when it succeeds (for
`Load`, also when it returns `ErrModulePending`). A failed `Load` leaves
the count and the hit's error in `Stats` for the host to read. Called from
a host function, `SetGlobal` does not reset, which would give the running
script a new budget. A host that sets a global for each request sets it
before the request's first `Call`: between two hooks of one request, the
reset would let the request allocate the limit again. The limit bounds what
one request allocates, not what the module keeps from request to request; a
runtime that grows across requests is better discarded.

After a hit the runtime is reusable: `ClearInterrupt` and `ReleaseCallData`
leave it as a pooled runtime after any request. A host that would rather not
trust the module's state, which the hit may have left half-updated, drops
the runtime. Clearing the interrupt without a reset leaves the budget spent,
and the next allocation stops the runtime again.

The hit is the runtime's single pending interrupt, so a host's own
`Interrupt` (a timeout) from another goroutine can replace it, and the call
then returns the host's error. `Stats().MemoryLimitHits` counts the hit
either way, and `Stats().LastMemoryLimitError` keeps its error until the
next reset.

With no limit the runtime keeps no count, and each place that would charge
an allocation costs two loads and two branches.

Under `eventloop`, the loop gives each timer or immediate callback and each
task a new budget, so the limit applies to a macrotask and the promise jobs
it queues.

### Counters

`rt.Stats()` returns the runtime's counters, each read in constant time. A
host reads them on the goroutine that uses the runtime, between requests or
from a host function during one:

- `AllocatedBytes`, `Objects`, `Strings`, `Shapes`: what was counted since
  the limit was set, and `RequestAllocatedBytes` since the last reset.
  They count only while a limit is set; a limit of `math.MaxInt64` counts
  without limiting.
- `MemoryLimitHits` and `LastMemoryLimitError`, above.
- `Interrupts`: the calls of `Interrupt`, hits included.
- `ICEntries`: the inline cache entries of the functions that ran.
- `PendingJobs`: the queued promise jobs and microtasks.
- `RegisterStackBytes`: the size of the interpreter's register stack.

A pool can export them when it takes a runtime back:

```go
st := rt.Stats()
metrics.Observe("plugin_request_bytes", st.RequestAllocatedBytes)
rt.ReleaseCallData()
```

A host function can give them to the module:

```go
rt.SetGlobal("memoryUsage", moejs.NativeFunc(func(r *moejs.Realm, this moejs.Value, args []moejs.Value) (moejs.Value, error) {
	return moejs.Int(r.Stats().RequestAllocatedBytes), nil
}))
```

A host function that allocates for JavaScript on its own can count that
too, with `Realm.ChargeMemory(n)`, which returns the interrupt's error once
the limit is passed.

### Objects belong to one runtime

Objects that a runtime's JavaScript creates belong to that runtime. Using an
object of another runtime can run that runtime's code, so pass only Go values
and JSON between runtimes.

`Call`, `CallFunction`, `SetGlobal`, `FromGo` and the settle functions of
`NewPromise` return `ErrForeign` for a function or generator of another
runtime, or for a bound function or proxy of one. Inside the Go containers that `FromGo` converts,
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

### Function values

`CallFunction(fn, this, args...)` calls a function value the host got
earlier, such as a callback that a plugin passed to a host function. It
checks what `Call` checks. A value that is not a function gives
`ErrNotCallable`, a function of another runtime gives `ErrForeign`, and an
interrupt stops the call before a host function runs. The jobs the call
queues run before it returns. Called from inside a host function, it leaves
them to the end of the outermost call.

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

### TypeScript

`CompileTS(name, source)` compiles a TypeScript module. The parser drops the
type syntax as it reads it, as Node's type stripping does, and the module
runs as the JavaScript that is left. Nothing is type-checked.

The parser erases:

- type annotations, type parameters and type arguments, `as`, `satisfies`,
  `x!` and `<T>x`;
- `interface` and `type` declarations, everything after `declare`, function
  overload signatures, and namespaces that hold only types;
- `import type`, `export type` and specifiers marked `type`;
- a `this` parameter, which does not count in the function's `length`;
- the TypeScript modifiers of class members (`public`, `private`,
  `protected`, `readonly`, `override`, `abstract`, `declare`), index
  signatures, and methods without a body. A `declare` or `abstract` field
  produces nothing. A field with only a type stays a field, initialized to
  `undefined`, as tsc emits it for ES2022 and later.

TypeScript that has run-time semantics fails with a `*SyntaxError` that
names it: `enum` and `const enum` outside `declare`, a namespace with
values, constructor parameter properties (`constructor(private x: T)`),
`import x = require("m")`, `export import x = A.B`, an alias
`import x = A.B` used as a value, `export =`, `export as namespace`, and
`accessor` fields. Decorators are not
supported, as in JavaScript. TSX is not supported.

As tsc does, the compiler drops an import specifier whose binding no
expression reads, and an import declaration left without bindings.
`import "m"` stays. A reference in a type, `typeof x` included, does not
count as a read. `export { T }` and `export default T` are dropped when `T`
names only a type. So a module may import types and values from one
package by name, and the host's module for that package needs to export
only the values.

Errors and stack traces refer to the TypeScript text, so no source map is
needed. `Function.prototype.toString` returns the function's TypeScript
source, types included; Node returns it with the types blanked out.

A host that compiles TypeScript it does not trust should cap the length of
the source. Telling an arrow function from a parenthesised expression can
take time quadratic in the nesting: conditionals nested in the true
branches of each other as `a ? (b): T => a ? (b): T => …` take about
1.5 s at 2,000 levels and a few seconds just below the nesting limit, and
the cost adds up over every such expression of a source. `Runtime.Interrupt`
does not stop `CompileTS` or `Compile`, which the host calls itself.

A `Resolver` or an `Importer` picks the compiler per module, for example by
file extension. Modules from `CompileTS` and from `Compile` link with each
other.

```go
resolve := func(referrer moejs.Referrer, specifier string) (*moejs.Module, error) {
	src, err := host.read(specifier)
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(specifier, ".ts") {
		return moejs.CompileTS(specifier, src)
	}
	return moejs.Compile(specifier, src)
}
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

A named type whose underlying type is in that list converts as that type:
`type Settings map[string]any` becomes a lazy map, and `type Status string`
a string.

Other types that `encoding/json` writes, such as structs, pointers,
`map[string]int` and `[]map[string]string`, convert from their
`json.Marshal` text, which the engine parses once. Struct tags and
`MarshalJSON` methods apply as they do in `json.Marshal`, and a `[]byte`
field becomes a base64 string. The result is a snapshot taken when `FromGo`
runs, or, for a value inside a Go map or slice, when JavaScript first reads
that container. Later changes to the struct are not seen.

Channels, functions other than `NativeFunc`, complex numbers and values that
`json.Marshal` rejects return an error. So do an `error` and a struct none
of whose fields `json.Marshal` writes, such as a `sync.Mutex` or a
`context.Context`, unless the type has a `MarshalJSON` or `MarshalText`
method: `json.Marshal` would write them as `{}` or as a text that says
nothing about them. Pass `err.Error()` for an error. A struct with no fields
at all is `{}`. Inside a container, reading such a member throws a
`TypeError`.

`SetGlobal(name, v)` converts `v` the same way, so a host can install a
namespace of functions as a `map[string]any` of `NativeFunc` values.
`Function(name, length, fn)` wraps a `NativeFunc` and sets the `name` and
`length` that JavaScript sees.

### JSON in

`ParseJSON(b)` runs `JSON.parse` on the bytes. Use it for arguments the host
holds as JSON, such as stored task data or a request body. It copies the
bytes into a string first, so the host can reuse them once it returns.

`ParseJSONString(s)` parses a string without that copy when it is valid
UTF-8, and strings in the result can share the memory of `s`. A host that
holds the text as a `[]byte` can pass
`unsafe.String(unsafe.SliceData(b), len(b))` if it never modifies the bytes
before `ReleaseCallData`, nor while it or the module keeps values derived
from them. Nothing checks this; the host is responsible for it.

`FromGo` also takes a `json.RawMessage`, alone or as a member of a
`map[string]any` or `[]any`, and JavaScript gets `JSON.parse` of its text.
It is converted from a copy of the text, made when `FromGo` runs or, for a
member, when JavaScript first reads its container. After that the host may
reuse the bytes. The text of an object or an array is checked when it is
converted and parsed when JavaScript first reads the value, so a hook that
never reads it never pays for the parse. Other text is parsed at once.
Invalid text is a `SyntaxError`: `FromGo` returns it for a top-level value,
and reading the member throws it inside a container. A nil
`json.RawMessage` is `null`. A `json.RawMessage` that JavaScript has not
read comes back from `ToGo` as a new `json.RawMessage` with the text.

Typed containers of `json.RawMessage`, such as `[]json.RawMessage`,
`map[string]json.RawMessage`, a `*json.RawMessage` or a struct field, go
through `json.Marshal` like any other type, and are parsed at once.

`ParseJSON` and `ParseJSONString` are the cheaper choice for a text the hook
always reads: they parse the text once, and `ParseJSON` copies it once. A
`json.RawMessage` copies and checks the text, and parses it only when the
hook reads it, so it pays off for a text the hook may not read.

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

`ToGoInto(v, target)` gives the result and the error of `ToGo`, then
`json.Marshal`, then `json.Unmarshal` into `target`. A host that already
runs those three steps can switch to it and keep the same behavior. As with
`Unmarshal`, plain objects, arrays and unmodified arguments go in directly,
without building `ToGo`'s Go map or writing text. A getter, a proxy, a
`Date`, a `Map`, a typed array, a BigInt, a function or a target type that
unmarshals itself makes it run the three steps. When `ToGo` or
`json.Marshal` would fail (a throwing getter, NaN or ±Infinity, a cycle),
`target` keeps its old contents.

Where `ToGo` and `AppendJSON` disagree, `ToGoInto` follows `ToGo`. A member
whose value is `undefined` stays, as `null`. A -0 stays -0. NaN and
±Infinity are an error. `toJSON` is not called. As with `ToGo`, an
interrupt stops it only while a getter runs.

A `json.RawMessage` in the target receives the JSON text of its part of the
value, while the rest of the target is still filled directly. It can be a
field, a pointer, a map value, a slice element or the target itself. The
bytes are the ones the round trip gives it: `AppendJSON`'s text for
`Unmarshal`, and for `ToGoInto` the `json.Marshal` text of `ToGo`'s result,
with keys sorted and `<`, `>` and `&` escaped. Like `json.Unmarshal`, it
writes into the `json.RawMessage`'s own array when the text fits. For
`ToGoInto`, a `json.RawMessage` argument that the hook returns unread is not
parsed: the target receives its text as `json.Marshal` compacts it. Any
other type that unmarshals itself still sends the whole value through the
round trip.

### Bounding a result

`UnmarshalWith`, `ToGoIntoWith` and `ToGoWith` take `DecodeOptions` that
limit the JSON text of the result. `MaxBytes` limits its length, and
`MaxNodes` the number of values in it: objects, arrays, strings, numbers,
booleans and null, but not keys. The text is the one the round trip writes,
which is `AppendJSON`'s for `UnmarshalWith` and the `json.Marshal` text of
`ToGo`'s result for the other two. The counts are exact: a result whose text
has `n` bytes passes `MaxBytes: n` and fails `MaxBytes: n-1`. A zero field
sets no limit.

A result past a limit returns an error that wraps `ErrTooLarge`, and the
target keeps its old contents. The value is counted before anything is
decoded, without writing the text, when it can be read without running
JavaScript: plain objects, arrays and arguments from `FromGo`. A
`json.RawMessage` argument the hook returns unread is the one exception for
`UnmarshalWith`: `AppendJSON` writes the text of the value it parses to, so
it is parsed to be counted, unless a quick scan of it already shows a text
past the limit. `ToGoIntoWith` and `ToGoWith` count its text without a
parse. A value that needs JavaScript to be read, such as a `toJSON`, a
getter or a proxy, is counted on the text once the round trip has written
it, or for `ToGoWith` on the result of `ToGo`, but still before anything is
decoded. A cycle, and nesting deeper than 10,000 levels, passes every
limit.

With no limit set, these functions are `Unmarshal`, `ToGoInto` and `ToGo`,
at the same cost.

### Strings kept after the request

Strings that `ToGo`, `Unmarshal` and `ToGoInto` return from a value
`ParseJSON` or `ParseJSONString` produced, or from a `json.RawMessage`
argument, can share memory with the parsed text (for `ParseJSONString`, the
host's string; for a `json.RawMessage`, the runtime's copy of it) and keep
it alive. So can `String()` of a string value. Call `strings.Clone` on the
strings the host keeps.

### Nesting

`ParseJSON`, `ParseJSONString`, `ToGo` and `AppendJSON` return a
`RangeError` when arrays and objects nest deeper than 10,000 levels.

## Promises and errors

### Promises

`NewPromise` gives a host function a promise to return, along with Go
functions that settle it later. `PromiseResult` reads a promise's state and
result. `SetPromiseRejectionTracker` reports rejections that no handler
caught, like Sobek's tracker.

`ThrownValue(err)` returns the value that a host function returning `err`
throws. A host rejects a promise with it to give JavaScript the same
`Error`, which unwraps to `err` when it comes back as an `*Exception`.

The settle functions run only on the goroutine that uses the runtime. The
`eventloop` package (below) settles promises from other goroutines.

### Errors

A JavaScript throw comes back as an `*Exception`. `Name()` and `Message()`
read the `name` and `message` data properties of the thrown value. For a
thrown primitive they use the value itself. Neither runs JavaScript.

When a host function returns a Go error, JavaScript sees a thrown `Error`
whose message is the error's text. The resulting `*Exception` unwraps to the
original Go error.

An interrupt is an `*InterruptedError`. It unwraps to the interrupt's value
when that value is an error, so a host that calls
`Interrupt(context.Cause(ctx))` finds `context.DeadlineExceeded`, or its own
cause, with `errors.Is` and `errors.As`. A malformed module or script is a
`*SyntaxError` with its position. A Go panic during a call, for example in a
host function, is an `*InternalError`, and the runtime can still be used
afterwards.

A `Call` or `CallFunction` made inside a host function returns the
`*InternalError` of a panic below it to that host function. If the host
function returns the error, JavaScript sees it thrown as an `Error` that it
can catch, like any other Go error.

`Runtime.StackTrace(exc)` returns the V8-format `stack` of a thrown `Error`,
also without running JavaScript.

## Event loop and timers

### Running a loop

A bare `Runtime` has no timers. The `eventloop` package adds them, along
with an event loop that owns the runtime while it runs:

```go
loop, err := eventloop.New(rt, eventloop.Options{})
err = loop.Run(func(rt *moejs.Runtime) error {
	_, err := rt.RunScript(script) // the script may call setTimeout
	return err
})
```

`New` installs `setTimeout`, `clearTimeout`, `setInterval`,
`clearInterval`, `setImmediate` and `clearImmediate` as globals. `Run`
calls the function, then runs timers, immediates and posted tasks on the
same goroutine. It returns when nothing keeps the loop alive: no ref'd
timer or immediate, no `Hold` that is not done, no promise of `NewPromise`
that is not settled and no task that has not run.

A module whose top-level `await` waits for a timer makes `Load` return
`ErrModulePending`. The loop runs the timer later and the module finishes
then, so the function passed to `Run` treats that error as success.

`Start` runs the loop on a new goroutine instead. That loop stays alive
while idle, until `Stop` or `Terminate`.

### Phases

Each iteration has three phases, like Node's timers, poll and check phases:

1. The timers due when the phase starts, in order of expiry, then of
   scheduling.
2. The tasks posted before the phase starts: `RunOnLoop`, the `done`
   function of `Hold` and the settle function of `NewPromise`.
3. The immediates set before the phase starts.

Work that a phase adds waits for a later iteration. A delay is at least
1 ms, so a timer that a timer callback sets runs in a later iteration. The
promise jobs and `queueMicrotask` callbacks that a callback queues run when
it returns, before the next callback. With nothing to run, the loop sleeps
until the next timer is due or a task arrives.

### Node compatibility

The timers follow Node.js, because the code that needs them is written for
Node:

- `setTimeout` and `setInterval` return a `Timeout` object with `ref`,
  `unref`, `hasRef`, `refresh`, `close` and `[Symbol.toPrimitive]`, which
  returns a numeric id. `setImmediate` returns an `Immediate` with `ref`,
  `unref` and `hasRef`.
- A callback gets the extra arguments, and the `Timeout` or `Immediate` as
  `this`. A callback that is not a function throws a `TypeError` with the
  code `ERR_INVALID_ARG_TYPE`. A string is not evaluated.
- The delay goes through `ToNumber` and is truncated to whole milliseconds.
  NaN, a delay below 1 and a delay above 2147483647 become 1 ms.
- An interval is rescheduled for its delay after the time its callback
  started. `refresh` restarts a timer's delay from now, and runs a timeout
  that already fired again.
- `clearTimeout` and `clearInterval` take a `Timeout`, or its id as a
  number or a string once the id has been read. `clearImmediate` takes an
  `Immediate`. All three ignore anything else, and work inside the timer's
  own callback.
- An unref'd timer or immediate does not keep `Run` alive. It runs only
  while something else does.

Some details differ from Node. The `Timeout` and `Immediate` objects have
no own properties: `JSON.stringify` gives `{}`, and `constructor.name` is
`"Object"`. Their methods throw a `TypeError` when called on another object,
including a `Proxy` of one, and a `Timeout` has no `Symbol.dispose` method.
Timers expire in the order of their exact due times, where Node compares
whole milliseconds. `timers/promises`, the `AbortSignal` options and
`util.promisify` are not provided.

### Asynchronous host functions

While the loop runs, only its goroutine uses the runtime. Other goroutines
hand it work in three ways:

- `RunOnLoop(fn)` posts `fn`. It returns false once the loop is
  terminated.
- `Hold()` keeps the loop alive until its `done(fn)` is called, which posts
  `fn`.
- `NewPromise()`, which a host function calls on the loop, returns a
  promise and a `settle` function. `settle(f)` runs `f` on the loop. A
  value fulfills the promise, and a Go error rejects it with the `Error` a
  host function's error throws (`ThrownValue`).

`done` and `settle` can be called from any goroutine. The first call counts,
and later calls do nothing. Take a hold on the loop, in a host function or a
callback. A hold taken on another goroutine races with a `Run` that has
nothing else left, and almost always loses: it then keeps only the next
`Run` alive.

```go
rt.SetGlobal("fetchJSON", moejs.NativeFunc(func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
	url := moejs.Arg(args, 0).String()
	p, settle := loop.NewPromise()
	go func() {
		body, err := download(url) // blocking work runs off the loop
		settle(func(rt *moejs.Runtime) (moejs.Value, error) {
			if err != nil {
				return moejs.Undefined(), err
			}
			return rt.ParseJSON(body)
		})
	}()
	return p, nil
}))
```

### Errors

These errors stop the loop, and `Run` returns them: a callback's exception,
the first exception of the jobs after it (from a `queueMicrotask` callback,
for example), a Go panic in a host function, and a failure of `settle`. An
uncaught exception ends a Node.js process too. With `Options.OnError` set,
the loop passes these errors to it and keeps running. An error from `Run`'s
own function ends `Run` at once.

An interrupt always stops the loop, before it takes the next timer, task or
immediate off its queues, which keep them. `Run` returns the
`*InterruptedError`. `Loop.Interrupt(v)` interrupts the runtime and wakes
the loop; `Runtime.Interrupt` alone leaves a sleeping loop asleep until its
next timer. The interrupt stays pending, so the host calls `ClearInterrupt`
before it runs the loop again.

A panic in a host task (the functions of `RunOnLoop`, `done` and `settle`,
or `OnError`) is not recovered. It unwinds through `Run`, and ends the
program for a loop started with `Start`. The tasks and immediates of the
phase that had not run stay queued for the next `Run`. The task that
panicked is gone, and a promise whose settle function panicked stays
pending.

A rejection that no handler catches is the host's business, through
`SetPromiseRejectionTracker`.

### Stopping and reuse

`Stop` ends the loop after the callback it is running and waits for the loop
to end. It returns what ended that run, as `Run` would: `nil`, the error
that stopped it, the `*InterruptedError` of an interrupt or
`ErrTerminated`. That is how a host learns of an error that stopped a loop
started with `Start`. `StopNoWait` does not wait, so a callback on the loop
can call it. `Stop` and `Terminate` must not be called on the loop, which
includes the function passed to `Run`, because they would wait for
themselves.

When `Run` returns or the loop stops, the runtime is the host's again. What
is still pending stays pending, such as an unref'd timer, or everything a
stop, an error or an interrupt left. The next `Run` or `Start` continues it. JavaScript
that the host runs in between, with `Call` for example, can set more timers.

`Terminate` ends the loop for good. It interrupts the runtime with
`ErrTerminated`, which stops the running JavaScript, drops every pending
timer, immediate and task, and waits for the loop to end. Afterwards `Run`
returns `ErrTerminated`, `RunOnLoop` returns false and the timer functions
throw. The interrupt stays pending even when no JavaScript was running, so
the host calls `ClearInterrupt` before it uses the runtime again. Another
`New` gives the runtime a fresh loop.

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
