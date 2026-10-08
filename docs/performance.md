# moejs performance

How moejs compares with Sobek, QuickJS and V8 on the workload it was built
for, and how to reproduce the numbers.

[简体中文](performance.zh_CN.md)

## Setup

All numbers are medians of 3 runs on 2026-10-08: Intel Xeon E5-2650 v3
(10 cores × 2 sockets, hyperthreaded to 40 logical CPUs), Linux, go1.26.0
linux/amd64. The machine was not idle, so treat differences under about 10%
as noise. The baselines are Sobek `v0.0.0-20260708062710` (pure Go),
modernc.org/quickjs `v0.25.0` (QuickJS transpiled to pure Go via the modernc
toolchain, `CGO_ENABLED=0`), and quickjs-go `v0.7.7` (QuickJS, cgo). V8
(v8go `v0.9.0`) was not tested in this run due to memory constraints on the
test machine.

The workload is new-api's 10 task plugins (6,358 lines) and 269 hook calls
recorded on Sobek, 47 of which throw. Every measurement is taken from the Go
caller's side and includes converting the arguments and the result. The cgo
engines exchange JSON text, and a host that uses them pays for it too. Every iteration checks
the result.

## Hook calls

All 269 cases in a loop, one goroutine, mean per call:

| Engine | Time | Bytes | Allocations |
|---|--:|--:|--:|
| **moejs** | **21.4 µs** | 4.8 KB | 29 |
| Sobek | 45.8 µs | 11.3 KB | 163 |
| modernc-quickjs | 111.6 µs | 5.4 KB | 100 |
| quickjs-go | 316 µs | 7.8 KB | 118 |

A few of moejs's 29 allocations are the benchmark adapter's own: boxing the
arguments and the result into the harness's interface types.
`Runtime.Call` itself allocates nothing. The rest come from the plugins'
objects and the conversions.

## Host path

`BenchmarkHostFlow`: the same 269 calls with everything new-api's host does
around them, mean per call. With Sobek's API the host deep-copies every
argument, because Sobek wraps Go maps live, and decodes a result through
`Export`, `json.Marshal` and `json.Unmarshal` into its struct. With moejs it
passes its maps as they are and unmarshals the bytes of `AppendJSON`. The
second row starts from each argument's JSON bytes, as the host holds stored
task data (`json.Unmarshal` + `ToValue` for Sobek, `ParseJSON` for moejs):

| Arguments | moejs | Sobek |
|---|--:|--:|
| Go values | 12.92 µs / 5.1 KB / 37 allocs | 22.80 µs / 13.8 KB / 206 |
| JSON bytes | 17.45 µs / 8.4 KB / 79 | 36.30 µs / 15.0 KB / 252 |

A 10 KiB JSON-shaped host round trip (`identity(v) { return v }`, no
property reads) is 177 ns / 6 allocs in moejs against Sobek's 308 ns / 10
allocs, although Sobek's live-proxy `ToValue` pays nothing until JavaScript
reads a field.

## Runtimes

A new runtime with new-api's host globals, then evaluating a plugin module
in it. Memory is the cost retained per live runtime:

| | moejs | Sobek | modernc-quickjs | quickjs-go |
|---|--:|--:|--:|--:|
| New runtime | 4.27 µs / 27 allocs | 6.17 µs / 47 | 556 µs / 87 | 1,126 µs / 135 |
| + largest plugin (alibaba) | 247 µs / 478 | 931 µs / 4,499 | 10,666 µs ¹ | 7,530 µs ¹ |
| + smallest plugin (sora) | 22.2 µs / 80 | 111 µs / 672 | 2,944 µs ¹ | 2,694 µs ¹ |
| Retained, alibaba, 512 runtimes | 75.7 KiB | 264.2 KiB | 456 KiB ² | 347.9 KiB ³ |
| Retained, sora, 64 runtimes | 11.9 KiB | 51.8 KiB | | |

¹ Includes compiling the script, which these engines do per context.
² RSS delta; modernc.org/quickjs allocates through the modernc C-to-Go
allocator, which is invisible to Go's heap stats. ³ The engine's own heap
(QuickJS `malloc_size`).

## Micro-benchmarks

moejs vs Sobek, 100 iterations per operation:

| Case | moejs | Sobek | Speed-up | Allocations (moejs / Sobek) |
|---|--:|--:|--:|--:|
| Property read, monomorphic | 6.4 µs | 18.9 µs | 2.9x | 9 / 96 |
| Property read, polymorphic | 6.6 µs | 13.3 µs | 2.0x | 8 / 11 |
| Function call | 6.4 µs | 12.0 µs | 1.9x | 9 / 89 |
| Closure | 17.6 µs | 58.9 µs | 3.3x | 211 / 1,094 |
| Array push + for-of | 7.3 µs | 64.7 µs | 8.9x | 15 / 719 |
| String concatenation | 20.7 µs | 34.5 µs | 1.7x | 399 / 712 |
| String methods | 181 µs | 791 µs | 4.4x | 909 / 11,811 |
| `JSON.parse` | 7.3 ms | 43.3 ms | 5.9x | 66k / 807k |
| `JSON.stringify` | 4.7 ms | 14.5 ms | 3.1x | 1,415 / 237k |
| `Object.keys` + `Object.assign` | 268 µs | 777 µs | 2.9x | 609 / 17,302 |
| RegExp test + replace | 193 µs | 552 µs | 2.9x | 2,210 / 9,503 |
| `new Error` + throw + catch | 30.2 µs | 72.8 µs | 2.4x | 300 / 1,393 |

## Reproducing

The benchmarks live in `bench/` and run new-api's plugins, which
`bench/testdata/plugins/fetch.sh` downloads at a pinned commit:

```sh
bench/testdata/plugins/fetch.sh
cd bench
go test -run xxx -bench 'Benchmark(HookSuite|HostFlow|NewRuntime|Instantiate|Compile|Micro)$' -benchmem -count 5 .
go test -run TestFootprint -v .
```

`bench/scripts/run_all.sh` runs the full set, including the parallel
throughput and per-phase benchmarks.

## PGO

`default.pgo` is recorded from the whole hook workload (`go run ./cmd/pgo`
in `bench/` regenerates it). Go applies a `default.pgo` automatically only
in the main package's directory, so an embedding program passes
`-pgo=<path to moejs>/default.pgo` or copies the profile into its own main
package.
