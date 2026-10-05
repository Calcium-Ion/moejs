<div align="center">

<img src="docs/assets/moejs-logo.svg" width="160" alt="moejs">

# moejs

A JavaScript runtime in pure Go, for running JavaScript plugins inside Go programs.

<p align="center">
  <a href="./README.zh_CN.md">简体中文</a> |
  <strong>English</strong>
</p>

</div>

You compile each plugin once and give each request its own runtime. Plugin
functions take Go maps, slices or JSON directly, and you read the result back
as Go values, JSON, or your own struct.

moejs was written for the task plugins of
[new-api](https://github.com/QuantumNous/new-api), and its tests and
benchmarks run those plugins.

## Performance

The workload is new-api's 10 task plugins and 269 recorded calls. Times are
taken on the Go caller's side and include converting the arguments and the
result.

| | moejs | Sobek | QuickJS (quickjs-go) | V8 (v8go) |
|---|--:|--:|--:|--:|
| One plugin call | 6.9 µs | 14.3 µs | 104.5 µs | 56.6 µs ¹ |
| New runtime | 1.4 µs | 2.2 µs | 382 µs | 1,153 µs ¹ |
| Memory per runtime with the largest plugin loaded | 81 KiB | 264 KiB | 348 KiB ² | 1,544 KiB ² |

¹ V8's timings varied widely on the test machine. ² The engine's own heap.

Sobek is a pure-Go engine like moejs. QuickJS and V8 run through cgo. The
test machine, the full results and how to reproduce them are in
[docs/performance.md](docs/performance.md).

## Features

- moejs builds with `CGO_ENABLED=0`, cross-compiles without a C toolchain,
  and shows up in pprof and the race detector like any other Go code.
- moejs converts Go maps and slices as the plugin reads them, so a plugin
  pays only for the parts it uses.
- Runtimes can go back into a pool. After a request, `ReleaseCallData` makes
  the runtime let go of that request's arguments.
- moejs runs ES module graphs and dynamic `import()`. Every import goes to a
  Go function you provide, and a plugin can load only the modules that
  function returns.
- Classic scripts (strict or sloppy), `eval` and the `Function` constructor
  run too. A host can cap the length of dynamic code or turn it off.
- Plugins can use `async`/`await` and top-level `await`. Host functions can
  return a promise and settle it later from Go.
- Any goroutine can interrupt a running plugin, for timeouts and
  cancellation.
- A JavaScript throw, a syntax error, an interrupt and a panic in a host
  function come back as `*Exception`, `*SyntaxError`, `*InterruptedError`
  and `*InternalError`. `StackTrace` returns the V8-style stack of a thrown
  `Error`.
- Builtins are frozen and shared by every runtime. A plugin that writes to
  `Array.prototype` fails, with a `TypeError` in strict code. Each runtime
  has its own globals and time zone. For plugins that patch builtins, a host
  can give a runtime its own mutable copy.

## Quick start

```sh
go get github.com/Calcium-Ion/moejs
```

moejs needs Go 1.25 or later and depends only on the standard library (tests
use testify).

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
	// Compile once. Any number of runtimes can load the same Module.
	mod, err := moejs.Compile("plugin.js", source)
	if err != nil {
		panic(err)
	}
	build, err := mod.Hook("buildRequest")
	if err != nil {
		panic(err)
	}

	// Each request gets its own runtime: install the host functions, then load the module.
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
				// A Go error becomes a JavaScript Error with this message.
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

	// Go values in, a Go struct out.
	input, err = rt.FromGo(map[string]any{"prompt": "a dog", "n": 2})
	if err != nil {
		panic(err)
	}
	if res, err = rt.Call(build, input); err != nil {
		panic(err)
	}
	var req struct {
		Method string         `json:"method"`
		Body   map[string]any `json:"body"`
	}
	if err := rt.Unmarshal(res, &req); err != nil {
		panic(err)
	}
	fmt.Println(req.Method, req.Body) // POST map[n:2 prompt:a dog]

	// A JavaScript throw comes back as *moejs.Exception.
	_, err = rt.Call(build, moejs.Null())
	var exc *moejs.Exception
	fmt.Println(errors.As(err, &exc), exc.Name(), exc.Message())
	// true TypeError Cannot read properties of null (reading 'prompt')

	// Interrupt stops a hook that runs too long. Any goroutine can call it.
	spin, _ := mod.Hook("spin")
	timer := time.AfterFunc(50*time.Millisecond, func() { rt.Interrupt("timeout") })
	defer timer.Stop()
	_, err = rt.Call(spin)
	var interrupted *moejs.InterruptedError
	fmt.Println(errors.As(err, &interrupted), interrupted.Value) // true timeout
	rt.ClearInterrupt()

	// Before the runtime goes back to a pool, let go of this request's data.
	rt.ReleaseCallData()
}
```

A server usually compiles each plugin once and keeps a pool of runtimes for
it, one per concurrent request. The [guide](docs/guide.md) covers pools,
module graphs, value conversion, promises and errors, and the
[package documentation](https://pkg.go.dev/github.com/Calcium-Ion/moejs)
describes every function.

## JavaScript support

moejs runs ES modules and classic scripts, including sloppy mode and the
web-compatibility behaviour of Annex B. The language includes classes with
fields, private members and static blocks, destructuring, optional chaining,
generators, async functions and async iteration. The standard library has
`Proxy`, `Reflect`, `BigInt`, typed arrays, resizable `ArrayBuffer`s,
`WeakRef`, `structuredClone`, `TextEncoder`/`TextDecoder`, and recent
additions such as the `Set` methods, `Promise.try`, `Float16Array`,
`Array.fromAsync`, `Math.sumPrecise` and `Error.isError`. Regular
expressions support every flag and Unicode 17 properties, and `Date` takes
its time zones from Go.

On [test262](https://github.com/tc39/test262), 79,385 tests pass and 0
fail. The other 14,058 tests use features moejs does not implement and are
skipped. Results by directory are in
[bench/test262/RESULTS.md](bench/test262/RESULTS.md).

Not implemented: import attributes and JSON modules, `using` declarations
and `DisposableStack`, decorators, iterator helpers, `Intl`, `Temporal`,
`ShadowRealm`, `FinalizationRegistry`, timers and `JSON.rawJSON`.
[TODO.md](TODO.md) lists all of them, the known wrong results and the
limits.

## Status

moejs is in alpha, and the API may change between releases.

## Testing

```sh
# Test inputs are downloaded separately: new-api's plugins and a pinned
# test262 revision. Tests that need them skip until they are downloaded.
bench/testdata/plugins/fetch.sh
bench/test262/fetch.sh

# Unit, audit and fuzz-corpus tests of the engine.
go test ./...

# Differential tests against Sobek, the expression corpus, the benchmarks and
# test262. bench/ is a separate Go module, so Sobek and the cgo engines are
# dependencies of bench/ only. Its V8 and QuickJS baselines need cgo.
cd bench && go test -timeout 30m ./...
```

## Acknowledgements

moejs takes design ideas from these projects. Its code was written
independently.

- [goja](https://github.com/dop251/goja) and [Sobek](https://github.com/grafana/sobek):
  the Go interop conventions and the `Export` rules. Sobek is also the
  reference for the differential tests.
- [QuickJS](https://bellard.org/quickjs/): the 16-byte value layout, atoms,
  shape transitions and compact builtin tables.
- [V8](https://v8.dev/): hidden classes, inline caches, prototype validity
  checks (reduced to one counter), the Ignition register interpreter,
  `Date.parse` and the `Error.prototype.stack` format.
- [Lua 5.x](https://www.lua.org/): the fixed-width register instruction
  encoding.
- [JavaScriptCore](https://webkit.org/) and [SpiderMonkey](https://spidermonkey.dev/):
  NaN-boxing.
- [esbuild](https://github.com/evanw/esbuild): how to write a fast
  JavaScript parser in Go.
- [Hardened JavaScript / SES](https://github.com/endojs/endo/tree/master/packages/ses):
  the `lockdown()` model behind the shared frozen builtins.
- [quickjs-go](https://github.com/buke/quickjs-go) and [v8go](https://github.com/rogchap/v8go):
  the cgo baselines of the benchmarks.
- [test262](https://github.com/tc39/test262): the conformance suite.
- [new-api](https://github.com/QuantumNous/new-api): the plugin host. Its
  `pkg/jsplugin` decides which APIs moejs has to support.

## License

moejs is licensed under the [Apache License 2.0](LICENSE).

The benchmarks run new-api's task plugins, which are licensed under AGPL-3.0
and downloaded separately by
[`bench/testdata/plugins/fetch.sh`](bench/testdata/plugins/README.md).
