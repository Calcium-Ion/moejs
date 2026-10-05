# moejs 性能

moejs 在其目标负载上与 Sobek、QuickJS、V8 的对比，以及如何复现这些数字。

[English](performance.md)

## 测试环境

以下数字均为 2026-10-01 测得的 5 次运行中位数：13th Gen Intel Core i5-13500H
（6 个性能核 + 6 个能效核，超线程到 16 个逻辑 CPU），Linux，go1.26.6 linux/amd64。
串行各行固定在 8 线程上（`taskset -c 0-7 GOMAXPROCS=8`）。测量时机器上还有别的负载，
10% 以内的差异按噪声看。
对比基线为 Sobek `v0.0.0-20260708062710`（纯 Go）、quickjs-go `v0.7.7`（QuickJS，cgo）与 v8go `v0.9.0`（V8，cgo）。

负载是 new-api 的 10 个任务插件（6,358 行）与在 Sobek 上录制的 269 个钩子调用，其中 47 个会抛错。
所有测量都在 Go 调用方进行，包含参数与结果的转换。cgo 引擎以 JSON 文本传入传出，宿主用它们时也要付这部分成本。每次迭代都校验结果。

## 钩子调用

全部 269 个用例循环，单 goroutine，每次调用的均值：

| 引擎 | 耗时 | 分配字节 | 分配次数 |
|---|--:|--:|--:|
| **moejs** | **6.86 µs** | 4.7 KB | 30 |
| Sobek | 14.32 µs | 11.1 KB | 163 |
| v8go | 56.64 µs ¹ | 5.3 KB | 104 |
| quickjs-go | 104.50 µs | 7.6 KB | 118 |

¹ v8go 的耗时在这台机器上波动很大，只能看量级。

moejs 的 30 次分配中有几次属于基准适配器本身：把参数和结果装箱成测试框架的接口类型。
`Runtime.Call` 本身不分配，其余分配来自插件创建的对象和值的转换。

## 宿主路径

`BenchmarkHostFlow`：同样的 269 个调用，加上 new-api 宿主在调用前后做的全部工作，每次调用的均值。
使用 Sobek 的 API 时，宿主要深拷贝每个参数（Sobek 以活引用包装 Go map），结果要经 `Export`、`json.Marshal`、
`json.Unmarshal` 解码到结构体。使用 moejs 时，宿主直接传入 map，再对 `AppendJSON` 的字节做 Unmarshal。
第二行从每个参数的 JSON 字节开始，对应宿主保存的任务数据（Sobek 用 `json.Unmarshal` + `ToValue`，moejs 用 `ParseJSON`）：

| 参数 | moejs | Sobek |
|---|--:|--:|
| Go 值 | 12.92 µs / 5.1 KB / 37 次分配 | 22.80 µs / 13.8 KB / 206 |
| JSON 字节 | 17.45 µs / 8.4 KB / 79 | 36.30 µs / 15.0 KB / 252 |

一次 10 KiB、JSON 形态的宿主往返（`identity(v) { return v }`，不读取任何属性），
moejs 为 177 ns / 6 次分配，Sobek 为 308 ns / 10 次分配，尽管 Sobek 的活代理 `ToValue`
在 JavaScript 读取字段之前不付任何代价。

## 运行时

新建运行时并注入 new-api 的宿主全局，再在其中求值插件模块。内存是每个存活运行时保留的 Go 堆：

| | moejs | Sobek | quickjs-go | v8go |
|---|--:|--:|--:|--:|
| 新建运行时 | 1.39 µs / 27 次分配 | 2.20 µs / 47 | 381.7 µs / 135 | 1152.7 µs ¹ / 54 |
| + 最大插件（alibaba） | 71.0 µs / 479 | 321.8 µs / 4,499 | 3,230 µs ² | 2,493 µs ¹ ² |
| + 最小插件（sora） | 7.0 µs / 81 | 37.6 µs / 672 | 1,070 µs ² | 1,349 µs ¹ ² |
| 保留内存，alibaba，512 个运行时 | 80.5 KiB | 263.6 KiB | 347.9 KiB ³ | 1,544 KiB ³ |
| 保留内存，sora，64 个运行时 | 11.8 KiB | 49.2 KiB | | |

¹ v8go，见上方说明。² 含脚本编译，cgo 引擎按上下文编译。
³ 引擎自身的堆（QuickJS `malloc_size`、V8 已用堆大小）。

编译最大的插件 moejs 需 2.2 ms、Sobek 需 2.6 ms，每个进程只需一次。使用 `Options{MutableIntrinsics: true}`
时，新建运行时需 35.9 µs，存活的 alibaba 运行时保留 229.4 KiB。

## 微基准

moejs 对比 Sobek，每次操作 100 次内循环：

| 用例 | moejs | Sobek | 加速比 | 分配次数（moejs / Sobek） |
|---|--:|--:|--:|--:|
| 属性读取，单态 | 6.4 µs | 18.9 µs | 2.9x | 9 / 96 |
| 属性读取，多态 | 6.6 µs | 13.3 µs | 2.0x | 8 / 11 |
| 函数调用 | 6.4 µs | 12.0 µs | 1.9x | 9 / 89 |
| 闭包 | 17.6 µs | 58.9 µs | 3.3x | 211 / 1,094 |
| 数组 push + for-of | 7.3 µs | 64.7 µs | 8.9x | 15 / 719 |
| 字符串拼接 | 20.7 µs | 34.5 µs | 1.7x | 399 / 712 |
| 字符串方法 | 181 µs | 791 µs | 4.4x | 909 / 11,811 |
| `JSON.parse` | 7.3 ms | 43.3 ms | 5.9x | 66k / 807k |
| `JSON.stringify` | 4.7 ms | 14.5 ms | 3.1x | 1,415 / 237k |
| `Object.keys` + `Object.assign` | 268 µs | 777 µs | 2.9x | 609 / 17,302 |
| RegExp test + replace | 193 µs | 552 µs | 2.9x | 2,210 / 9,503 |
| `new Error` + throw + catch | 30.2 µs | 72.8 µs | 2.4x | 300 / 1,393 |

## 复现

基准测试位于 `bench/`，运行的是 new-api 的插件，由 `bench/testdata/plugins/fetch.sh` 按固定提交下载：

```sh
bench/testdata/plugins/fetch.sh
cd bench
go test -run xxx -bench 'Benchmark(HookSuite|HostFlow|NewRuntime|Instantiate|Compile|Micro)$' -benchmem -count 5 .
go test -run TestFootprint -v .
```

`bench/scripts/run_all.sh` 运行全部测量，包括并发吞吐与分阶段基准。

## PGO

`default.pgo` 以整套钩子负载录制（在 `bench/` 中运行 `go run ./cmd/pgo` 重新生成）。Go 只会自动使用 main 包目录下的
`default.pgo`，因此嵌入方需要传入 `-pgo=<moejs 路径>/default.pgo`，或把该文件复制到自己的 main 包目录。
