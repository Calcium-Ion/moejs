<div align="center">

<img src="docs/assets/moejs-logo.svg" width="160" alt="moejs">

# moejs

A JavaScript runtime in pure Go, for running JavaScript plugins inside Go programs.

<p align="center">
  <a href="./README.zh_CN.md">简体中文</a> |
  <strong>English</strong>
</p>

</div>

moejs supports modern JavaScript, including ES modules, classes,
async/await, Proxy and BigInt, and passes all 79,385 test262 tests it runs.

On the same plugins as Sobek, another pure-Go engine, a moejs call takes
about half the time and a runtime uses about a third of the memory, so a
server can give every concurrent request its own runtime.

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

### Pure Go

moejs builds with `CGO_ENABLED=0` and cross-compiles without a C toolchain.
The engine is ordinary Go code, so pprof and the race detector see inside it.

### Passing values

Plugin functions take Go maps and slices or JSON. moejs converts a map one
level at a time, as far as the plugin reads it. Results come back as Go
values or JSON, or go straight into your own struct with the same result as
`json.Unmarshal`. Host functions are plain Go functions, and one can return a
promise and settle it later from Go.

### What a plugin can reach

A plugin sees the JavaScript standard library and the globals you install.
Every `import` and `import()` goes through a Go function you provide, and
`eval` and `new Function` can be capped in length or turned off. Builtins are
frozen and shared by every runtime, so every plugin sees the same
`Array.prototype`. Each runtime has its own globals.

### Timeouts and errors

Any goroutine can interrupt a running plugin, even one stuck in an endless
loop. A JavaScript throw, a syntax error, an interrupt and a panic in a host
function each come back as their own Go error type, and a thrown `Error` has
a V8-style stack trace. A runtime stays usable after a host function panics.

## Quick start

```sh
go get github.com/Calcium-Ion/moejs
```

moejs needs Go 1.25 or later and depends only on the standard library (tests
use testify).

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/Calcium-Ion/moejs"
)

const source = `
export function buildRequest(task) {
  return {
    method: "POST",
    url: "https://api.example.com/v1/tasks",
    headers: { authorization: "Bearer " + utils.env("API_KEY") },
    body: { prompt: task.prompt.trim(), n: task.n ?? 1 },
  };
}
export function spin() { for (;;) {} }
`

// Request is what buildRequest returns.
type Request struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Body    struct {
		Prompt string `json:"prompt"`
		N      int    `json:"n"`
	} `json:"body"`
}

// Plugin is a compiled plugin and a pool of runtimes that have loaded it.
type Plugin struct {
	mod  *moejs.Module
	idle chan *moejs.Runtime
}

func NewPlugin(name, source string, size int) (*Plugin, error) {
	// Compile once. Every runtime in the pool loads the same Module.
	mod, err := moejs.Compile(name, source)
	if err != nil {
		return nil, err
	}
	return &Plugin{mod: mod, idle: make(chan *moejs.Runtime, size)}, nil
}

// Call runs hook on a runtime from the pool and decodes the result into out.
// Any number of goroutines can call it at once.
func (p *Plugin) Call(ctx context.Context, hook moejs.Hook, args map[string]any, out any) error {
	rt, err := p.get()
	if err != nil {
		return err
	}
	defer p.put(rt)

	// Interrupt stops the hook when ctx ends. Any goroutine can call it.
	interrupted := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		rt.Interrupt(context.Cause(ctx))
		close(interrupted)
	})
	defer func() {
		if !stop() {
			<-interrupted // Interrupt must return before rt goes back to the pool.
		}
	}()

	// FromGo converts the map as the plugin reads it, one level at a time.
	arg, err := rt.FromGo(args)
	if err != nil {
		return err
	}
	res, err := rt.Call(hook, arg)
	if err != nil {
		return err
	}
	// Unmarshal writes the result straight into out, with no JSON text in between.
	return rt.Unmarshal(res, out)
}

// get takes an idle runtime or makes a new one: host functions first, then the module.
func (p *Plugin) get() (*moejs.Runtime, error) {
	select {
	case rt := <-p.idle:
		return rt, nil
	default:
	}
	rt := moejs.NewRuntime(moejs.Options{})
	if err := rt.SetGlobal("utils", map[string]any{"env": moejs.NativeFunc(env)}); err != nil {
		return nil, err
	}
	if err := rt.Load(p.mod); err != nil {
		return nil, err
	}
	return rt, nil
}

// put resets the runtime and returns it to the pool.
func (p *Plugin) put(rt *moejs.Runtime) {
	rt.ClearInterrupt()
	rt.ReleaseCallData() // The idle runtime lets go of this call's arguments.
	select {
	case p.idle <- rt:
	default: // The pool is full.
	}
}

var secrets = map[string]string{"API_KEY": "test-key"}

// env is the host function behind utils.env.
func env(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
	name, err := r.ToString(moejs.Arg(args, 0))
	if err != nil {
		return moejs.Undefined(), err
	}
	v, ok := secrets[name.GoString()]
	if !ok {
		// A Go error becomes a JavaScript Error with this message.
		return moejs.Undefined(), fmt.Errorf("%s is not set", name.GoString())
	}
	return moejs.String(v), nil
}

func main() {
	p, err := NewPlugin("plugin.js", source, runtime.GOMAXPROCS(0))
	if err != nil {
		panic(err)
	}
	// Look hooks up once and reuse them on every call.
	build, err := p.mod.Hook("buildRequest")
	if err != nil {
		panic(err)
	}
	spin, err := p.mod.Hook("spin")
	if err != nil {
		panic(err)
	}

	// Concurrent calls each get their own runtime.
	reqs := make([]Request, 3)
	var wg sync.WaitGroup
	for i := range reqs {
		wg.Go(func() {
			task := map[string]any{"prompt": fmt.Sprintf(" cat %d ", i), "n": i + 1}
			if err := p.Call(context.Background(), build, task, &reqs[i]); err != nil {
				panic(err)
			}
		})
	}
	wg.Wait()
	for _, req := range reqs {
		fmt.Println(req.Method, req.URL, req.Headers["authorization"], req.Body.Prompt, req.Body.N)
	}
	// POST https://api.example.com/v1/tasks Bearer test-key cat 0 1
	// POST https://api.example.com/v1/tasks Bearer test-key cat 1 2
	// POST https://api.example.com/v1/tasks Bearer test-key cat 2 3

	// A JavaScript throw comes back as *moejs.Exception.
	err = p.Call(context.Background(), build, map[string]any{}, &Request{})
	var exc *moejs.Exception
	fmt.Println(errors.As(err, &exc), exc.Name(), exc.Message())
	// true TypeError Cannot read properties of undefined (reading 'trim')

	// A hook still running when ctx ends stops with *moejs.InterruptedError.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err = p.Call(ctx, spin, nil, nil)
	var interrupted *moejs.InterruptedError
	fmt.Println(errors.As(err, &interrupted), interrupted.Value)
	// true context deadline exceeded
}
```

A server calling plugins the way `Plugin` does pays the least per request.
Taking a runtime from the pool is one channel receive, while a new runtime
with the largest plugin loaded takes about 71 µs. The [guide](docs/guide.md)
covers pools, module graphs, value conversion, promises and errors, and the
[package documentation](https://pkg.go.dev/github.com/Calcium-Ion/moejs)
describes every function.

moejs ships a `default.pgo` profile recorded from the plugin workload. Go
applies a profile automatically only from the main package's directory, so
pass it when you build:

```sh
go build -pgo="$(go list -m -f '{{.Dir}}' github.com/Calcium-Ion/moejs)/default.pgo" .
```

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
