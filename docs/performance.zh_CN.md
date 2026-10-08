# moejs 性能

moejs 在其目标负载上与 Sobek、QuickJS、V8 的对比，以及如何复现这些数字。

[English](performance.md)

## 测试环境

以下数字均为 2026-10-08 测得的 3 次运行中位数：Intel Xeon E5-2650 v3
（10 核 × 2 路，超线程到 40 个逻辑 CPU），Linux，go1.26.0 linux/amd64。
测量时机器上还有别的负载，10% 以内的差异按噪声看。
对比基线为 Sobek `v0.0.0-20260708062710`（纯 Go）、modernc.org/quickjs `v0.25.0`
（QuickJS 经 modernc 工具链转译为纯 Go，`CGO_ENABLED=0`）与 quickjs-go `v0.7.7`
（QuickJS，cgo）。V8（v8go `v0.9.0`）因测试机内存不足未纳入本次测量。

负载是 new-api 的 10 个任务插件（6,358 行）与在 Sobek 上录制的 269 个钩子调用，其中 47 个会抛错。
所有测量都在 Go 调用方进行，包含参数与结果的转换。cgo 引擎以 JSON 文本传入传出，宿主用它们时也要付这部分成本。每次迭代都校验结果。

## 钩子调用

全部 269 个用例循环，单 goroutine，每次调用的均值：

| 引擎 | 耗时 | 分配字节 | 分配次数 |
|---|--:|--:|--:|
| **moejs** | **21.4 µs** | 4.8 KB | 29 |
| Sobek | 45.8 µs | 11.3 KB | 163 |
| modernc-quickjs | 111.6 µs | 5.4 KB | 100 |
| quickjs-go | 316 µs | 7.8 KB | 118 |

moejs 的 29 次分配中有几次属于基准适配器本身：把参数和结果装箱成测试框架的接口类型。
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

新建运行时并注入 new-api 的宿主全局，再在其中求值插件模块。内存是每个存活运行时的占用：

| | moejs | Sobek | modernc-quickjs | quickjs-go |
|---|--:|--:|--:|--:|
| 新建运行时 | 4.27 µs / 27 次分配 | 6.17 µs / 47 | 556 µs / 87 | 1,126 µs / 135 |
| + 最大插件（alibaba） | 247 µs / 478 | 931 µs / 4,499 | 10,666 µs ¹ | 7,530 µs ¹ |
| + 最小插件（sora） | 22.2 µs / 80 | 111 µs / 672 | 2,944 µs ¹ | 2,694 µs ¹ |
| 保留内存，alibaba，512 个运行时 | 75.7 KiB | 264.2 KiB | 456 KiB ² | 347.9 KiB ³ |
| 保留内存，sora，64 个运行时 | 11.9 KiB | 51.8 KiB | | |

¹ 含脚本编译，这些引擎按上下文编译。² RSS 增量；modernc.org/quickjs 通过 modernc
C-to-Go 分配器分配内存，不计入 Go 堆统计。³ 引擎自身的堆（QuickJS `malloc_size`）。

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
