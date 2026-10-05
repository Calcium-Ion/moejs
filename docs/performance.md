# moejs performance

How moejs compares with Sobek, QuickJS and V8 on the workload it was built
for, and how to reproduce the numbers.

[简体中文](performance.zh_CN.md)

## Setup

All numbers are medians of 5 runs on 2026-10-01: 13th Gen Intel Core
i5-13500H (6 performance + 6 efficiency cores, hyperthreaded to 16 logical
CPUs), Linux, go1.26.6 linux/amd64. Serial rows are pinned to 8 threads
(`taskset -c 0-7 GOMAXPROCS=8`). The machine was not idle, so treat
differences under about 10% as noise. The baselines are Sobek
`v0.0.0-20260708062710` (pure Go), quickjs-go `v0.7.7` (QuickJS, cgo) and
v8go `v0.9.0` (V8, cgo).

The workload is new-api's 10 task plugins (6,358 lines) and 269 hook calls
recorded on Sobek, 47 of which throw. Every measurement is taken from the Go
caller's side and includes converting the arguments and the result. The cgo
engines exchange JSON text, and a host that uses them pays for it too. Every iteration checks
the result.

## Hook calls

All 269 cases in a loop, one goroutine, mean per call:

| Engine | Time | Bytes | Allocations |
|---|--:|--:|--:|
| **moejs** | **6.86 µs** | 4.7 KB | 30 |
| Sobek | 14.32 µs | 11.1 KB | 163 |
| v8go | 56.64 µs ¹ | 5.3 KB | 104 |
| quickjs-go | 104.50 µs | 7.6 KB | 118 |

¹ v8go's timings varied widely on this machine. Read them as an order of
magnitude.

A few of moejs's 30 allocations are the benchmark adapter's own: boxing the
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
in it. Memory is the Go heap retained per live runtime:

| | moejs | Sobek | quickjs-go | v8go |
|---|--:|--:|--:|--:|
| New runtime | 1.39 µs / 27 allocs | 2.20 µs / 47 | 381.7 µs / 135 | 1152.7 µs ¹ / 54 |
| + largest plugin (alibaba) | 71.0 µs / 479 | 321.8 µs / 4,499 | 3,230 µs ² | 2,493 µs ¹ ² |
| + smallest plugin (sora) | 7.0 µs / 81 | 37.6 µs / 672 | 1,070 µs ² | 1,349 µs ¹ ² |
| Retained, alibaba, 512 runtimes | 80.5 KiB | 263.6 KiB | 347.9 KiB ³ | 1,544 KiB ³ |
| Retained, sora, 64 runtimes | 11.8 KiB | 49.2 KiB | | |

¹ v8go, see the note above. ² Includes compiling the script, which the cgo
engines do per context. ³ The engine's own heap (QuickJS `malloc_size`, V8
used heap size).

Compiling the largest plugin takes 2.2 ms in moejs and 2.6 ms in Sobek, once
per process. With `Options{MutableIntrinsics: true}` a new runtime costs
35.9 µs and a live alibaba runtime retains 229.4 KiB.

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
