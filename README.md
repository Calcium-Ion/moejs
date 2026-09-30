<div align="center">

<img src="docs/assets/moejs-logo.svg" width="160" alt="moejs">

# moejs

**A pure-Go JavaScript runtime built for running many small plugin sandboxes fast.**

<p align="center">
  <a href="./README.zh_CN.md">简体中文</a> |
  <strong>English</strong>
</p>

</div>

moejs is an ECMAScript engine written in Go, with no cgo and no assembly. It
was built to run the JavaScript task plugins of
[new-api](https://github.com/QuantumNous/new-api), and its host API is shaped
by that job: compile a plugin module once, load it into many small runtimes,
call its hook functions with JSON-shaped Go values or JSON bytes, and read
the results back as Go values or JSON bytes.

On that workload the whole host path around a hook call (arguments in, the
call, the result decoded into the host's struct) takes about 6 µs,
2.5 times faster than with [Sobek](https://github.com/grafana/sobek) and
with a sixth of its allocations, a runtime is created in about 1 µs, and a
live runtime retains less than a third of Sobek's memory (see
[Performance](#performance)).

- **Pure Go.** Cross-compiles anywhere Go does; no C toolchain, no cgo call
  overhead, and the Go scheduler and race detector see everything.
- **A host API without needless conversions.** Hooks are resolved once,
  arguments are lazy views of Go values or parsed straight from JSON bytes,
  results come back as Go values or as JSON bytes, and errors are returned
  as values.
- **Cheap runtimes.** Intrinsics are built once per process, frozen and shared
  by every runtime, so creating one costs a global object and a register
  stack.
- **Safe to interrupt.** A runaway script stops at the next loop back-edge or
  call, long-running builtins check the flag as they work, and the runtime is
  reusable afterwards.
- **Fails loudly.** An unsupported feature is a compile-time or runtime error
  that names it, never a silent misbehaviour.

## Status

moejs runs classic scripts, strict or sloppy, and ES modules, alone or as
graphs of modules that import each other. Its results on the
[test262](https://github.com/tc39/test262) conformance suite, overall and by
directory, are in [`bench/test262/RESULTS.md`](bench/test262/RESULTS.md),
which the test262 runner regenerates.

Supported, among others:

- **Language:** `let`/`const`, arrow functions, classes (fields, private
  names and methods, `#x in o`, static blocks, `super`, `new.target`,
  subclassing builtins), destructuring, spread and rest, default parameters,
  template and tagged template literals, optional chaining, `??`, getters and
  setters, `Symbol` and the iterator protocol (`for-of`, spread,
  destructuring), generators, async functions and `await`, async
  generators and `for await`, `import` and `export` between modules (live
  bindings, cycles, namespace objects, `export * as`, string export names),
  top-level `await` across a module graph, dynamic `import()` in modules and
  scripts, `import.meta`, labelled statements, exceptions, direct and
  indirect `eval`, and the `Function`, `GeneratorFunction`, `AsyncFunction`
  and `AsyncGeneratorFunction` constructors.
- **Sloppy mode** (scripts without `"use strict"`): `this` coercion,
  implicit globals, silently failing assignments and deletes, `with`
  (including `Symbol.unscopables`), the mapped `arguments` object and
  `arguments.callee`, and the Annex B syntax and semantics of scripts:
  block-level functions, labelled function declarations, catch parameter
  redeclaration, `for (var x = init in o)`, calls as assignment targets,
  legacy octal literals and escapes, and HTML-like comments.
- **Builtins:** `Object`, `Function`, `Array` (including the ES2023 methods
  and `Array.fromAsync`), `String` (including `normalize` on Unicode 17),
  `Number`, `Boolean`,
  `Symbol`, `BigInt`, `Math` (including `sumPrecise`), `JSON`, `Proxy`, `Reflect`,
  `Map` and `Set` (including the ES2025 set methods), `WeakMap`, `WeakSet`,
  `WeakRef`, `ArrayBuffer` (resizable, with `transfer`), `SharedArrayBuffer`,
  `DataView` (including `getFloat16`/`setFloat16`), the typed arrays
  (including `Float16Array`), `Atomics`, the `Error` family with
  `AggregateError`, `Error.isError` and V8-format `stack`, `structuredClone`,
  `TextEncoder`/`TextDecoder` (UTF-8), `atob`/`btoa`, the URI functions, `Object.groupBy`/`Map.groupBy`, `Promise`
  (including `Promise.try`), `queueMicrotask`, `globalThis`.
- **Date:** all of ES2024 plus the Annex B methods, V8's `Date.parse`, and
  time zones from Go's tz database, settable per runtime.
- **RegExp:** every flag (`dgimsuvy`), lookahead and lookbehind,
  backreferences, named and duplicate named groups, pattern modifiers and
  all Unicode 17 `\p{…}` properties.

Not supported yet: `Intl`, iterator helpers, `FinalizationRegistry`,
timers, import attributes and JSON modules. [`TODO.md`](TODO.md) has the
full list and the known wrong results.

## Install

```sh
go get github.com/Calcium-Ion/moejs
```

The module requires Go 1.25 or later and has no dependencies outside the
standard library (testify is used by tests only).

## Usage

```go
package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/Calcium-Ion/moejs"
)

const source = `
export function buildRequest(input) {
  return {
    method: "POST",
    url: "https://api.example.com/v1/tasks",
    headers: { authorization: "Bearer " + utils.env("API_KEY") },
    body: { prompt: input.prompt.trim(), n: input.n ?? 1 },
  };
}
export function spin() { for (;;) {} }
`

func main() {
	// Compile once per process; a Module is immutable and shared.
	mod, err := moejs.Compile("plugin.js", source)
	if err != nil {
		panic(err)
	}
	build, err := mod.Hook("buildRequest")
	if err != nil {
		panic(err)
	}

	// One runtime per sandbox: host functions, then the module.
	env := map[string]string{"API_KEY": "test-key"}
	rt := moejs.NewRuntime(moejs.Options{})
	err = rt.SetGlobal("utils", map[string]any{
		"env": moejs.NativeFunc(func(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			name, err := r.ToString(moejs.Arg(args, 0))
			if err != nil {
				return moejs.Undefined(), err
			}
			v, ok := env[name.GoString()]
			if !ok {
				// A Go error throws an Error with this message.
				return moejs.Undefined(), fmt.Errorf("%s is not set", name.GoString())
			}
			return moejs.String(v), nil
		}),
	})
	if err != nil {
		panic(err)
	}
	if err := rt.Load(mod); err != nil {
		panic(err)
	}

	// JSON in, JSON out.
	input, err := rt.ParseJSON([]byte(`{"prompt": " a cat "}`))
	if err != nil {
		panic(err)
	}
	res, err := rt.Call(build, input)
	if err != nil {
		panic(err)
	}
	out, err := rt.AppendJSON(nil, res)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(out))
	// {"method":"POST","url":"https://api.example.com/v1/tasks","headers":{"authorization":"Bearer test-key"},"body":{"prompt":"a cat","n":1}}

	// Go values in, Go values out.
	input, err = rt.FromGo(map[string]any{"prompt": "a dog", "n": 2})
	if err != nil {
		panic(err)
	}
	if res, err = rt.Call(build, input); err != nil {
		panic(err)
	}
	req, err := rt.ToGo(res)
	if err != nil {
		panic(err)
	}
	fmt.Println(req.(map[string]any)["body"]) // map[n:2 prompt:a dog]

	// A throw is an *moejs.Exception.
	_, err = rt.Call(build, moejs.Null())
	var exc *moejs.Exception
	fmt.Println(errors.As(err, &exc), exc.Name(), exc.Message())
	// true TypeError Cannot read properties of null (reading 'prompt')

	// Interrupts stop a runaway hook from another goroutine.
	spin, _ := mod.Hook("spin")
	timer := time.AfterFunc(50*time.Millisecond, func() { rt.Interrupt("timeout") })
	defer timer.Stop()
	_, err = rt.Call(spin)
	var interrupted *moejs.InterruptedError
	fmt.Println(errors.As(err, &interrupted), interrupted.Value) // true timeout
	rt.ClearInterrupt()
}
```

A `Module` is immutable and can be loaded by any number of runtimes
concurrently, so a host compiles each plugin once and keeps a pool of
runtimes per plugin. A `Runtime` belongs to one goroutine at a time; only
`Interrupt` and `ClearInterrupt` may be called from others. Before it
returns a runtime to the pool, the host calls `rt.ReleaseCallData()`, so
the idle runtime does not keep the last request's arguments alive; what the
module stored of them stays valid.

## Host API

- **Hooks.** `Module.Hook(export, members...)` resolves an exported function,
  or one below an exported object (`mod.Hook("protocols", "openai",
  "decodeRequest")`), once. Bindings stay live: each `Call` reads the export
  and walks the members, as own properties, in the runtime at hand. A path
  that leads nowhere is `ErrHookNotFound`, one that leads to a value that is
  not a function `ErrNotCallable`; `Has` reports either as false.
- **Module graphs.** A module that imports others is linked before runtimes
  load it. `moejs.Link(entry, resolve)` asks the host's `Resolver` for the
  module each specifier names, once per module and specifier (moejs never
  reads files or the network), links the graph once and returns the
  `Module` to load: immutable and shared like any other, with `Hook`,
  `Export` and `Exports` addressing the entry's exports, re-exported names
  included. The resolver's `referrer` is the importing `*Module`, as a
  `Referrer`: the sealed interface of the code that requests a module, a
  `*Module` or a `*Script`. Each runtime only instantiates and evaluates the graph, each
  module of it once, and a module several graphs share is compiled once
  when the resolver returns the same `*Module`. A failed resolution is a
  `*ResolveError`, and an import that does not resolve to one binding a
  `*SyntaxError`, both at the importing module's position; `Load` refuses a
  module that imports and was not linked. A module that imports nothing
  needs no `Link` and pays nothing for graphs.

  ```go
  entry, err := moejs.Compile("plugin.js", source)
  mod, err := moejs.Link(entry, func(referrer moejs.Referrer, specifier string) (*moejs.Module, error) {
  	return host.module(specifier) // compiled once per process
  })
  err = rt.Load(mod) // per runtime
  ```
- **Dynamic import.** `Options.Importer` is the host's side of `import()`
  and `import.meta`: its `Resolve` is a `Resolver`, whose `referrer` is the
  importing `*Module` or the `*Script` that `RunScript` ran, and its
  optional `Meta` fills a module's `import.meta`, a null-prototype object
  made on first use. `import(specifier)` asks `Resolve` at once and returns
  a promise that settles with the module's namespace, or rejects with what
  the resolution, the linking or the evaluation failed with; without an
  `Importer` it rejects with a `TypeError`. The module is the identity: a
  runtime evaluates a module once, whether it was imported statically or
  dynamically, and the graph of a module `import()` loads is linked once
  per `Importer`, which any number of runtimes may share, so each runtime
  only instantiates and evaluates it. Jobs and interrupts behave as for
  `Load` and `Call`. Code without `import()` or `import.meta` compiles and
  runs exactly as before.

  ```go
  imp := &moejs.Importer{Resolve: func(referrer moejs.Referrer, specifier string) (*moejs.Module, error) {
  	return host.module(specifier)
  }}
  rt := moejs.NewRuntime(moejs.Options{Importer: imp}) // one imp for every runtime
  ```
- **Values in.** `FromGo` converts `nil`, booleans, numbers, strings,
  `json.Number`, `Value`, `NativeFunc` and the JSON-shaped containers
  (`map[string]any`, `[]any`, `map[string]string`, `[]string`, ...) lazily,
  one level at a time when JavaScript first reads it, so a hook pays only for
  the parts of its arguments it touches. JavaScript writes do not reach the
  Go value, and the host must not modify a value while JavaScript may still
  read it. Keys of objects from Go maps enumerate in sorted order. A
  `[]byte` becomes an `ArrayBuffer` over the same bytes, which JavaScript
  writes do reach. Structs and other types are an error: marshal them and
  use `ParseJSON`. `Function(name, length, fn)` wraps a `NativeFunc` with
  the `name` and `length` JavaScript sees.
- **Values out.** `ToGo` exports integral numbers as `int64` and other
  numbers as `float64`, arrays as `[]any`, objects as `map[string]any` of
  their own enumerable properties, bigints as `*big.Int`, which `FromGo`
  also accepts, and an `ArrayBuffer`, typed array or `DataView` as a copy of
  the bytes it holds or views, a `[]byte`. `AppendJSON` is `JSON.stringify` written to a byte slice;
  unlike `json.Marshal` of `ToGo`'s result, it omits `undefined` members,
  writes NaN and ±Infinity as `null`, keeps insertion order and calls
  `toJSON`. `Get(v, key)` reads one property, running getters, and
  `Export(name)` the current value of an export of the loaded module.
- **Scripts.** `CompileScript(name, source)` compiles a classic script into
  an immutable `*Script`, sloppy unless it starts with a `"use strict"`
  directive; `Runtime.RunScript` runs it in the runtime's global environment
  and returns its completion value. Its `var` and function declarations
  become properties of the global object, and its `let`, `const` and
  `class` declarations global bindings that later scripts and the loaded
  modules see. A declaration that conflicts with an existing global binding
  throws before anything runs. `SetGlobal` writes the global object, so
  after a script's `let x` the global lexical binding shadows a
  `SetGlobal("x", …)` (ECMA-262 9.1.1.4.1).
- **Eval.** Importing `moejs` installs the compiler behind `eval`, the
  `Function` constructors and `Realm.EvalScript(name, source)`, which a
  native function can call on the `*Realm` it receives to run a classic
  script from a string (test262's `$262.evalScript`). A direct `eval` sees
  the caller's bindings, including a module's imports; the evaluated code
  and the functions it creates report the evaluating script or module as
  their source in stack traces, and their `import()` passes it as the
  `Resolver`'s `referrer` (nil for the code of an indirect `eval` or a
  constructor called from a script or module that uses neither `import()`,
  `import.meta` nor a direct `eval`; see TODO.md). Code that uses neither
  `eval` nor `with` compiles exactly as before and pays nothing.
- **Limits on dynamic code.** `Options.MaxDynamicSource` caps the source
  text that `eval`, the `Function` constructors and `Realm.EvalScript`
  compile: 1 MiB of UTF-8 by default, no limit when negative. Longer text
  throws a `RangeError` before it is parsed. Compiling takes time and memory
  linear in the length of the text, so the cap bounds both, and an interrupt
  stops a compile in progress. `Options.DisableDynamicCode` turns dynamic
  code off for a runtime: all of them throw an `EvalError` instead of
  compiling. Neither applies to `Compile` and `CompileScript`.
- **Promises.** `NewPromise` gives a host function a promise to return and
  Go functions that settle it later. `PromiseResult` reads a promise's state
  and result, and `SetPromiseRejectionTracker` reports rejections no handler
  caught, as Sobek's tracker does.
- **Errors.** A throw is an `*Exception`. `Name()` and `Message()` read the
  thrown value's `name` and `message` data properties (the thrown value
  itself for a primitive) and never run JavaScript. A host function that
  returns a Go error throws an `Error` with the error's text as its message,
  and the `*Exception` unwraps to the Go error. An interrupt is an
  `*InterruptedError`, a bad module or script a `*SyntaxError` with its
  position, and a Go panic inside a call (a panicking host function) an
  `*InternalError`; the runtime stays usable. `Runtime.StackTrace(exc)`
  returns a thrown `Error`'s V8-format `stack`, also without running
  JavaScript.
- **Shared, frozen intrinsics by default.** Runtimes share one deeply frozen
  set of builtins, the Hardened JavaScript (SES `lockdown()`) model: writing
  to `Array.prototype` throws a `TypeError` in strict code and, as for any
  frozen object, fails silently in sloppy code, so plugins cannot pollute
  each other's prototypes.
  `Options{MutableIntrinsics: true}` builds a mutable copy
  per runtime for embeddings that patch builtins, at about 25 times
  the creation cost.
- **Time zone per runtime.** `Options.TimeZone` sets the local zone of `Date`
  (nil means `time.Local`).

Like Sobek, moejs converts decimal text with Go's `strconv`: a decimal
number with more than 800 significant digits before its point or exponent,
or with an exponent of 100000 or more that a long run of zeros offsets, can
convert to the wrong value (`Number("1" + "0".repeat(900) + "e-900")` is
`1e-101`, not 1), in `Number`, `parseFloat`, `JSON.parse`, `ParseJSON` and
source literals.

## Design

- **16-byte values.** A `Value` is an `unsafe.Pointer` and a `uint64`.
  Numbers, booleans, `undefined` and `null` never allocate, and the pointer
  word always holds a real pointer or nil, so the value is safe for Go's
  garbage collector.
- **Shapes and inline caches.** Objects share an immutable shape transition
  tree by property insertion order, property access instructions carry
  inline cache slots, and one epoch counter validates prototype chains.
  Objects converted from Go maps share shapes by key set, so plugin code
  reading `ctx.xxx` stays monomorphic across calls.
- **Register bytecode VM.** Fixed-width 32-bit instructions, one contiguous
  register stack per runtime, calls without allocation, and exceptions
  unwound through handler tables instead of Go `panic`/`recover`.
- **Strings** are ASCII (a zero-copy Go string), UTF-16 or a rope that
  flattens on demand; builtin property names are static atoms.
- **No locks on the hot path.** Property access, calls, strings, host
  conversion and regular expression matching take no locks, so runtimes on
  different goroutines only meet in the garbage collector.
- **Regular expressions** run on Go's `regexp` (RE2, linear time) when the
  translation is exact, and on a pure-Go backtracking engine otherwise, with
  a bounded stack and an interrupt check every 4096 steps.

## Performance

All numbers are medians of 5 runs on 2026-09-24: Apple M5 Pro (6 super + 12
performance cores, 64 GiB), go1.26.6 darwin/arm64, GOMAXPROCS=18. The machine
was not idle (load average 4–7), so treat differences under about 10% as
noise. The baselines are Sobek `v0.0.0-20260708062710` (pure Go), quickjs-go
`v0.7.7` (QuickJS, cgo) and v8go `v0.9.0` (V8, cgo).

The workload is new-api's 10 task plugins (6,358 lines) and 269 hook calls
recorded on Sobek, 47 of which throw. Every measurement is taken from the Go
caller's side and includes converting the arguments and the result; the cgo
engines exchange JSON text, which is their real cost. Every iteration checks
the result.

**Hook calls** (all 269 cases in a loop, one goroutine, mean per call):

| Engine | Time | Bytes | Allocations |
|---|--:|--:|--:|
| **moejs** | **3.91 µs** | 4.9 KB | 30 |
| Sobek | 8.64 µs | 11.4 KB | 163 |
| v8go | 14.37 µs | 5.4 KB | 104 |
| quickjs-go | 60.41 µs | 7.7 KB | 118 |

**Host path** (`BenchmarkHostFlow`: the same 269 calls with everything
new-api's host does around them, mean per call). With Sobek's API the host
deep-copies every argument, because Sobek wraps Go maps live, and decodes a
result through `Export`, `json.Marshal` and `json.Unmarshal` into its struct.
With moejs it passes its maps as they are and unmarshals the bytes of
`AppendJSON`. The second row starts from each argument's JSON bytes, as
the host holds stored task data (`json.Unmarshal` + `ToValue` for Sobek,
`ParseJSON` for moejs):

| Arguments | moejs | Sobek |
|---|--:|--:|
| Go values | 6.09 µs / 6.3 KB / 44 allocs | 15.32 µs / 17.1 KB / 248 |
| JSON bytes | 8.03 µs / 10.3 KB / 96 | 19.53 µs / 18.6 KB / 304 |

**Runtimes** (a new runtime with new-api's host globals, then evaluating a
plugin module in it; memory is the Go heap retained per live runtime):

| | moejs | Sobek | quickjs-go | v8go |
|---|--:|--:|--:|--:|
| New runtime | 0.90 µs / 27 allocs | 1.59 µs / 47 | 210 µs / 135 | 609 µs / 54 |
| + largest plugin (alibaba) | 51 µs / 479 | 197 µs / 4,499 | 1,717 µs ¹ | 1,309 µs ¹ |
| + smallest plugin (sora) | 5.0 µs / 81 | 24.1 µs / 672 | 556 µs ¹ | 722 µs ¹ |
| Retained, alibaba, 512 runtimes | 79 KiB | 258 KiB | 339 KiB ² | 785 KiB ² |
| Retained, sora, 64 runtimes | 11.5 KiB | 48 KiB | | |

¹ Includes compiling the script, which the cgo engines do per context.
² The engine's own heap (QuickJS `malloc_size`, V8 used heap size).

Compiling the largest plugin takes 1.5 ms in moejs and 1.6 ms in Sobek, once
per process. With `Options{MutableIntrinsics: true}` a new runtime costs
22.6 µs and a live alibaba runtime retains 231 KiB.

**Micro-benchmarks** (moejs vs Sobek, 100 iterations per operation):

| Case | moejs | Sobek | Speed-up | Allocations (moejs / Sobek) |
|---|--:|--:|--:|--:|
| Property read, monomorphic | 4.2 µs | 10.6 µs | 2.5x | 9 / 96 |
| Property read, polymorphic | 4.4 µs | 7.5 µs | 1.7x | 8 / 11 |
| Function call | 4.1 µs | 7.0 µs | 1.7x | 9 / 89 |
| Closure | 13.3 µs | 38.0 µs | 2.9x | 211 / 1,094 |
| Array push + for-of | 5.4 µs | 43.2 µs | 8.1x | 15 / 719 |
| String concatenation | 14.0 µs | 22.3 µs | 1.6x | 399 / 712 |
| String methods | 135 µs | 478 µs | 3.5x | 909 / 11,810 |
| `JSON.parse` | 4.2 ms | 27.6 ms | 6.6x | 66k / 807k |
| `JSON.stringify` | 3.5 ms | 10.4 ms | 3.0x | 1,415 / 237k |
| `Object.keys` + `Object.assign` | 141 µs | 492 µs | 3.5x | 609 / 17,302 |
| RegExp test + replace | 123 µs | 309 µs | 2.5x | 2,210 / 9,503 |
| `new Error` + throw + catch | 19.1 µs | 45.1 µs | 2.4x | 300 / 1,393 |

The benchmarks live in `bench/` and run new-api's plugins, which
`bench/testdata/plugins/fetch.sh` downloads at a pinned commit. To reproduce
them:

```sh
bench/testdata/plugins/fetch.sh
cd bench
go test -run xxx -bench 'Benchmark(HookSuite|HostFlow|NewRuntime|Instantiate|Compile|Micro)$' -benchmem -count 5 .
go test -run TestFootprint -v .
```

`bench/scripts/run_all.sh` runs the full set, including the parallel
throughput and per-phase benchmarks.

**PGO.** `default.pgo` is recorded from the whole hook workload
(`go run ./cmd/pgo` in `bench/` regenerates it). Go applies a `default.pgo`
automatically only in the main package's directory, so an embedding program passes
`-pgo=<path to moejs>/default.pgo` or copies the profile into its own main
package.

## Testing

```sh
# Test inputs that are not part of the repository: new-api's plugins and the
# pinned test262 revision. Tests that need them skip until they are fetched.
bench/testdata/plugins/fetch.sh
bench/test262/fetch.sh

# Unit, audit and fuzz-corpus tests of the engine.
go test ./...

# Differential tests against Sobek, the expression corpus, the benchmarks and
# test262. bench/ is a separate module so that Sobek and the cgo engines never
# become dependencies of moejs; its V8 and QuickJS baselines need cgo.
cd bench && go test -timeout 30m ./...
```

[`bench/test262/README.md`](bench/test262/README.md) describes the test262
rules, and [`bench/test262/RESULTS.md`](bench/test262/RESULTS.md) has the
results per directory.

## Acknowledgements

moejs learned from these projects; no code was copied from them.

- [goja](https://github.com/dop251/goja) and [Sobek](https://github.com/grafana/sobek):
  the Go interop conventions and the `Export` rules; Sobek is the reference
  of the differential tests.
- [QuickJS](https://bellard.org/quickjs/): the 16-byte value layout, atoms,
  shape transitions and compact builtin tables.
- [V8](https://v8.dev/): hidden classes, inline caches, prototype validity
  (reduced to one counter), the Ignition register interpreter, `Date.parse`
  and the `Error.prototype.stack` format.
- [Lua 5.x](https://www.lua.org/): the fixed-width register instruction
  encoding.
- [JavaScriptCore](https://webkit.org/) and [SpiderMonkey](https://spidermonkey.dev/):
  NaN-boxing.
- [esbuild](https://github.com/evanw/esbuild): the engineering of a fast
  JavaScript parser in Go.
- [Hardened JavaScript / SES](https://github.com/endojs/endo/tree/master/packages/ses):
  the `lockdown()` model behind shared frozen intrinsics.
- [quickjs-go](https://github.com/buke/quickjs-go) and [v8go](https://github.com/rogchap/v8go):
  the cgo baselines of the benchmarks.
- [test262](https://github.com/tc39/test262): the conformance suite.
- [new-api](https://github.com/QuantumNous/new-api): the plugin host whose
  `pkg/jsplugin` defines the API moejs has to support.

## License

moejs is licensed under the [Apache License 2.0](LICENSE).

The benchmarks run new-api's task plugins, which are licensed under AGPL-3.0
and are not included in this repository;
[`bench/testdata/plugins/fetch.sh`](bench/testdata/plugins/README.md)
downloads them.
