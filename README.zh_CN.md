<div align="center">

<img src="docs/assets/moejs-logo.svg" width="160" alt="moejs">

# moejs

**纯 Go 实现的 JavaScript 运行时，为高速运行大量小型插件沙箱而生。**

<p align="center">
  <strong>简体中文</strong> |
  <a href="./README.md">English</a>
</p>

</div>

moejs 是用 Go 编写的 ECMAScript 引擎，不依赖 cgo，也不含汇编。它最初为运行
[new-api](https://github.com/QuantumNous/new-api) 的 JavaScript 任务插件而开发，宿主 API 也按这项工作设计：
插件模块只编译一次，加载到许多小型运行时中，以 JSON 形态的 Go 值或 JSON 字节调用钩子函数，再以 Go 值或 JSON 字节读回结果。

在这类负载上，一次钩子调用前后的完整宿主路径（传入参数、调用、把结果解码到宿主的结构体）约 6 µs，比使用 [Sobek](https://github.com/grafana/sobek) 快 2.5 倍，分配次数只有其六分之一；
创建一个运行时约 1 µs，每个存活运行时保留的内存不到 Sobek 的三分之一（见[性能](#性能)）。

- **纯 Go**：Go 能编译到的平台都能用；无需 C 工具链，没有 cgo 调用开销，Go 调度器与 race 检测器能看到全部代码。
- **不做多余转换的宿主 API**：钩子只解析一次，参数是 Go 值的惰性视图或直接从 JSON 字节解析，
  结果以 Go 值或 JSON 字节返回，错误以返回值给出。
- **运行时创建廉价**：内建对象每个进程只构建一次，冻结后由所有运行时共享，新建运行时只需一个全局对象和一段寄存器栈。
- **可安全中断**：失控脚本在下一个循环回边或函数调用处停止，长耗时内建在执行中检查中断标志，中断后运行时仍可复用。
- **明确报错**：未支持的特性在编译期或运行期以指明该特性的错误报出，绝不静默错误执行。

## 状态

moejs 支持经典脚本（严格或非严格模式）与 ES 模块，模块既可单独运行，也可组成相互导入的模块图。它在 [test262](https://github.com/tc39/test262) 一致性测试集上的结果（总体与按目录）见
[`bench/test262/RESULTS.md`](bench/test262/RESULTS.md)，该文件由 test262 运行器重新生成。

已支持的部分特性：

- **语言**：`let`/`const`、箭头函数、类（字段、私有名称与私有方法、`#x in o`、静态块、`super`、`new.target`、
  内建对象子类化）、解构、展开与剩余参数、默认参数、模板字符串与带标签模板、可选链、`??`、getter 与 setter、
  `Symbol` 与迭代器协议（`for-of`、展开、解构）、生成器、async 函数与 `await`、async 生成器与 `for await`、模块间的 `import` 与 `export`（活绑定、循环依赖、模块命名空间对象、`export * as`、字符串导出名）、
  跨模块图的顶层 `await`、模块与脚本中的动态 `import()`、`import.meta`、带标签语句、异常、直接与间接 `eval`，以及
  `Function`、`GeneratorFunction`、`AsyncFunction` 与 `AsyncGeneratorFunction` 构造函数。
- **非严格模式**（不以 `"use strict"` 开头的脚本）：`this` 转换、隐式全局变量、静默失败的赋值与删除、`with`（含
  `Symbol.unscopables`）、映射的 `arguments` 对象与 `arguments.callee`，以及脚本的 Annex B 语法与语义：块级函数、
  带标签的函数声明、catch 参数重复声明、`for (var x = init in o)`、以函数调用为赋值目标、旧式八进制字面量与转义、
  HTML 风格注释。
- **内建对象**：`Object`、`Function`、`Array`（含 ES2023 方法与 `Array.fromAsync`）、`String`（含基于 Unicode 17 的 `normalize`）、
  `Number`、`Boolean`、`Symbol`、`BigInt`、`Math`（含 `sumPrecise`）、`JSON`、`Proxy`、`Reflect`、`Map` 与 `Set`（含 ES2025 集合方法）、
  `WeakMap`、`WeakSet`、`WeakRef`、`ArrayBuffer`（可调整大小，含 `transfer`）、`SharedArrayBuffer`、`DataView`（含 `getFloat16`/`setFloat16`）、类型化数组（含 `Float16Array`）、`Atomics`、`Error` 系列（含 `AggregateError`、`Error.isError` 与 V8 格式的 `stack`）、
  `structuredClone`、`TextEncoder`/`TextDecoder`（UTF-8）、`atob`/`btoa`、URI 函数、`Object.groupBy`/`Map.groupBy`、`Promise`（含 `Promise.try`）、
  `queueMicrotask`、`globalThis`。
- **Date**：ES2024 的全部方法与 Annex B 方法、移植自 V8 的 `Date.parse`，时区来自 Go 的 tz 数据库，可按运行时设置。
- **RegExp**：全部标志（`dgimsuvy`）、先行与后行断言、反向引用、命名组与重复命名组、模式修饰符，以及 Unicode 17 的全部
  `\p{…}` 属性。

尚未支持：`Intl`、迭代器辅助方法、`FinalizationRegistry`、定时器、导入属性与 JSON 模块。
完整列表与已知的错误结果见 [`TODO.md`](TODO.md)。

## 安装

```sh
go get github.com/Calcium-Ion/moejs
```

需要 Go 1.25 或更高版本；除标准库外没有其他依赖（testify 仅用于测试）。

## 使用

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

`Module` 不可变，可由任意多个运行时并发加载，因此宿主可以每个插件只编译一次，再为每个插件维护一个运行时池。
同一时刻一个 `Runtime` 只能由一个 goroutine 使用，只有 `Interrupt` 与 `ClearInterrupt` 可以在其他 goroutine 调用。

## 宿主 API

- **钩子。** `Module.Hook(export, members...)` 一次性解析一个导出函数，或导出对象下的函数
  （`mod.Hook("protocols", "openai", "decodeRequest")`）。绑定保持活性：每次 `Call` 都在当前运行时中读取导出，
  并按自有属性逐级查找成员。路径不通为 `ErrHookNotFound`，指向的值不是函数为 `ErrNotCallable`；`Has` 对两者都返回 false。
- **模块图。** 导入其他模块的模块须先链接，再交给运行时加载。`moejs.Link(entry, resolve)` 向宿主的 `Resolver`
  查询每个说明符所指的模块，每个模块的每个说明符只查询一次（moejs 从不读取文件或网络），一次性链接整个模块图，
  返回供加载的 `Module`：它与其他模块一样不可变、可共享，其 `Hook`、`Export` 与 `Exports` 针对入口模块的导出，
  包括再导出的名称。解析器收到的 `referrer` 是发起导入的 `*Module`，类型为 `Referrer`：即请求模块的代码的密封接口，
  取值为 `*Module` 或 `*Script`。每个运行时只需实例化并求值该模块图，其中每个模块只求值一次；多个模块图共享的模块，
  只要解析器返回同一个 `*Module`，就只编译一次。解析失败为 `*ResolveError`，未能解析为唯一绑定的导入为
  `*SyntaxError`，两者都带有导入方模块中的位置；`Load` 拒绝未经链接且含导入的模块。不导入任何模块的模块无需
  `Link`，也不为模块图付出任何开销。

  ```go
  entry, err := moejs.Compile("plugin.js", source)
  mod, err := moejs.Link(entry, func(referrer moejs.Referrer, specifier string) (*moejs.Module, error) {
  	return host.module(specifier) // 每个进程只编译一次
  })
  err = rt.Load(mod) // 每个运行时
  ```
- **动态导入。** `Options.Importer` 是宿主一侧的 `import()` 与 `import.meta`：其 `Resolve` 是一个 `Resolver`，
  `referrer` 为发起导入的 `*Module`，或 `RunScript` 运行的 `*Script`；可选的 `Meta` 填充模块的 `import.meta`，
  它是首次使用时创建的原型为 null 的对象。`import(specifier)` 立即调用 `Resolve`，返回一个 promise：它以模块的命名空间
  兑现，或以解析、链接或求值失败的原因拒绝；未设置 `Importer` 时以 `TypeError` 拒绝。模块即身份：无论静态导入还是
  动态导入，一个运行时对同一模块只求值一次；`import()` 加载的模块图每个 `Importer` 只链接一次，一个 `Importer`
  可由任意多个运行时共享，每个运行时只需实例化并求值。任务队列与中断的行为与 `Load`、`Call` 相同。不使用
  `import()` 或 `import.meta` 的代码，编译结果与运行方式都与之前完全相同。

  ```go
  imp := &moejs.Importer{Resolve: func(referrer moejs.Referrer, specifier string) (*moejs.Module, error) {
  	return host.module(specifier)
  }}
  rt := moejs.NewRuntime(moejs.Options{Importer: imp}) // 所有运行时共用一个 imp
  ```
- **传入值。** `FromGo` 转换 `nil`、布尔、数字、字符串、`json.Number`、`Value`、`NativeFunc` 以及 JSON 形态的容器
  （`map[string]any`、`[]any`、`map[string]string`、`[]string` 等）。容器惰性转换，JavaScript 首次读取时逐层展开，
  钩子只为实际读取的参数部分付费。JavaScript 的写入不会回写到 Go 值；在 JavaScript 仍可能读取期间，宿主不得修改该值。
  来自 Go map 的对象键按字典序枚举。`[]byte` 转为共享同一段字节的 `ArrayBuffer`，JavaScript 的写入会回写到它。
  结构体等其他类型会报错：先序列化，再用 `ParseJSON`。`Function(name, length, fn)` 把 `NativeFunc`
  包装为带有 JavaScript 可见的 `name` 与 `length` 的函数。
- **读出值。** `ToGo` 把整数导出为 `int64`、其他数字为 `float64`，数组为 `[]any`，对象为由其自有可枚举属性组成的
  `map[string]any`，BigInt 为 `*big.Int`（`FromGo` 也接受它），`ArrayBuffer`、类型化数组与 `DataView`
  为其持有或所视字节的副本 `[]byte`。`AppendJSON` 即写入字节切片的 `JSON.stringify`；与对 `ToGo` 结果做 `json.Marshal` 不同，
  它省略值为 `undefined` 的成员，把 NaN 与 ±Infinity 写成 `null`，保留插入顺序，并调用 `toJSON`。
  `Get(v, key)` 读取单个属性（会执行 getter），`Export(name)` 读取已加载模块某个导出的当前值。
- **脚本。** `CompileScript(name, source)` 把经典脚本编译为不可变的 `*Script`，除非以 `"use strict"` 指令开头，
  否则为非严格模式；`Runtime.RunScript` 在运行时的全局环境中运行它并返回其完成值。脚本的 `var` 与函数声明成为全局对象的属性，
  `let`、`const` 与 `class` 声明成为全局绑定，之后的脚本和已加载的模块都能看到。与已有全局绑定冲突的声明会在任何代码运行前抛出。
  `SetGlobal` 写入的是全局对象，因此脚本声明 `let x` 之后，`SetGlobal("x", …)` 会被该全局词法绑定遮蔽（ECMA-262 9.1.1.4.1）。
- **Eval。** 导入 `moejs` 即安装 `eval`、`Function` 系列构造函数与 `Realm.EvalScript(name, source)` 背后的编译器；
  原生函数可对其收到的 `*Realm` 调用 `EvalScript`，从字符串运行经典脚本（即 test262 的 `$262.evalScript`）。直接 `eval`
  能看到调用者的绑定，包括模块的导入；被求值的代码及其创建的函数在栈追踪中以发起求值的脚本或模块为来源，其中的
  `import()` 也以它作为 `Resolver` 的 `referrer`（若发起间接 `eval` 或调用构造函数的脚本或模块本身既不使用 `import()`、
  `import.meta` 也不含直接 `eval`，则为 nil，见 TODO.md）。
  既不使用 `eval` 也不使用 `with` 的代码编译结果与以前完全相同，没有任何额外开销。
- **动态代码的限制。** `Options.MaxDynamicSource` 限制 `eval`、`Function` 系列构造函数与 `Realm.EvalScript`
  编译的源码长度：默认 1 MiB（UTF-8），为负数时不限制。超长的源码在解析前就抛出 `RangeError`。编译的时间与内存
  都与源码长度成线性关系，因此这一上限同时限制了两者，编译过程中也能被中断。`Options.DisableDynamicCode`
  为运行时关闭动态代码：上述入口都抛出 `EvalError`，不做任何编译。两者都不影响 `Compile` 与 `CompileScript`。
- **Promise。** `NewPromise` 为宿主函数创建一个可返回的 promise，以及稍后将其敲定的 Go 函数。`PromiseResult`
  读取 promise 的状态与结果；`SetPromiseRejectionTracker` 与 Sobek 的同名接口一样，报告没有处理函数的 rejection。
- **错误。** 抛出的值为 `*Exception`。`Name()` 与 `Message()` 读取抛出值的 `name` 与 `message` 数据属性
  （抛出原始值时即该值本身），从不执行 JavaScript。宿主函数返回 Go error 时，向 JavaScript 抛出以该错误文本为
  message 的 `Error`，对应的 `*Exception` 可解包出原 Go error。中断为 `*InterruptedError`，错误的模块或脚本为带位置的
  `*SyntaxError`，调用中的 Go panic（例如宿主函数 panic）为 `*InternalError`；运行时仍可继续使用。
  `Runtime.StackTrace(exc)` 返回被抛出的 `Error` 的 V8 格式 `stack`，同样不执行 JavaScript。
- **默认共享冻结的内建对象。** 所有运行时共享同一套深度冻结的内建对象，即 Hardened JavaScript（SES `lockdown()`）
  模型：写入 `Array.prototype` 在严格代码中抛出 `TypeError`，在非严格代码中与写入任何冻结对象一样静默失败，插件之间因此无法互相污染原型。需要修改内建对象的宿主可用
  `Options{MutableIntrinsics: true}` 为每个运行时构建可变副本，创建成本约为前者的 25 倍。
- **按运行时设置时区。** `Options.TimeZone` 设置 `Date` 的本地时区（nil 表示 `time.Local`）。

与 Sobek 一样，moejs 用 Go 的 `strconv` 转换十进制文本：小数点或指数前有效数字超过 800 位的十进制数，
或指数达到 100000 以上、由一长串零抵消的十进制数，可能转换为错误的值（`Number("1" + "0".repeat(900) + "e-900")`
得到 `1e-101` 而非 1），`Number`、`parseFloat`、`JSON.parse`、`ParseJSON` 与源码字面量均受影响。

## 设计要点

- **16 字节的值。** `Value` 由一个 `unsafe.Pointer` 和一个 `uint64` 组成。数字、布尔、`undefined` 与 `null` 从不分配；
  指针字始终是真实指针或 nil，因此对 Go 垃圾回收器是安全的。
- **形状与内联缓存。** 对象按属性插入顺序共享不可变的形状转移树，属性访问指令携带内联缓存槽位，原型链有效性由一个计数器校验。
  由 Go map 转换来的对象按键集合共享形状，插件代码读取 `ctx.xxx` 时跨调用保持单态。
- **寄存器式字节码虚拟机。** 定宽 32 位指令，每个运行时一段连续的寄存器栈，调用不分配，异常经处理表展开而不使用 Go 的
  `panic`/`recover`。
- **字符串** 有 ASCII（零拷贝 Go string）、UTF-16 与 rope 三种形态，rope 按需扁平化；内建属性名是静态 atom。
- **热路径无锁。** 属性访问、调用、字符串、宿主转换与正则匹配都不取锁，不同 goroutine 上的运行时只在垃圾回收器中相遇。
- **正则表达式** 在能精确翻译时交给 Go 的 `regexp`（RE2，线性时间）执行，否则由纯 Go 回溯引擎执行，回溯栈有界，
  每 4096 步检查一次中断。

## 性能

以下数字均为 2026-09-24 测得的 5 次运行中位数：Apple M5 Pro（6 个超级核 + 12 个性能核，64 GiB），
go1.26.6 darwin/arm64，GOMAXPROCS=18。测量时机器并不空闲（负载 4–7），10% 以内的差异应视为噪声。
对比基线为 Sobek `v0.0.0-20260708062710`（纯 Go）、quickjs-go `v0.7.7`（QuickJS，cgo）与 v8go `v0.9.0`（V8，cgo）。

负载是 new-api 的 10 个任务插件（6,358 行）与在 Sobek 上录制的 269 个钩子调用，其中 47 个会抛错。
所有测量都从 Go 调用方视角进行，包含参数与结果的转换；cgo 引擎以 JSON 文本进出，这就是它们的真实成本。每次迭代都校验结果。

**钩子调用**（全部 269 个用例循环，单 goroutine，每次调用的均值）：

| 引擎 | 耗时 | 分配字节 | 分配次数 |
|---|--:|--:|--:|
| **moejs** | **3.91 µs** | 4.9 KB | 30 |
| Sobek | 8.64 µs | 11.4 KB | 163 |
| v8go | 14.37 µs | 5.4 KB | 104 |
| quickjs-go | 60.41 µs | 7.7 KB | 118 |

**宿主路径**（`BenchmarkHostFlow`：同样的 269 个调用，加上 new-api 宿主在调用前后做的全部工作，每次调用的均值）。
使用 Sobek 的 API 时，宿主要深拷贝每个参数（Sobek 以活引用包装 Go map），结果要经 `Export`、`json.Marshal`、
`json.Unmarshal` 解码到结构体；使用 moejs 时，宿主直接传入 map，再对 `AppendJSON` 的字节做 Unmarshal。
第二行从每个参数的 JSON 字节开始，对应宿主保存的任务数据（Sobek 用 `json.Unmarshal` + `ToValue`，moejs 用 `ParseJSON`）：

| 参数 | moejs | Sobek |
|---|--:|--:|
| Go 值 | 6.09 µs / 6.3 KB / 44 次分配 | 15.32 µs / 17.1 KB / 248 |
| JSON 字节 | 8.03 µs / 10.3 KB / 96 | 19.53 µs / 18.6 KB / 304 |

**运行时**（新建运行时并注入 new-api 的宿主全局，再在其中求值插件模块；内存为每个存活运行时保留的 Go 堆）：

| | moejs | Sobek | quickjs-go | v8go |
|---|--:|--:|--:|--:|
| 新建运行时 | 0.90 µs / 27 次分配 | 1.59 µs / 47 | 210 µs / 135 | 609 µs / 54 |
| + 最大插件（alibaba） | 51 µs / 479 | 197 µs / 4,499 | 1,717 µs ¹ | 1,309 µs ¹ |
| + 最小插件（sora） | 5.0 µs / 81 | 24.1 µs / 672 | 556 µs ¹ | 722 µs ¹ |
| 保留内存，alibaba，512 个运行时 | 79 KiB | 258 KiB | 339 KiB ² | 785 KiB ² |
| 保留内存，sora，64 个运行时 | 11.5 KiB | 48 KiB | | |

¹ 含脚本编译，cgo 引擎按上下文编译。
² 引擎自身的堆（QuickJS `malloc_size`、V8 已用堆大小）。

编译最大的插件 moejs 需 1.5 ms、Sobek 需 1.6 ms，每个进程只需一次。使用 `Options{MutableIntrinsics: true}`
时，新建运行时需 22.6 µs，存活的 alibaba 运行时保留 231 KiB。

**微基准**（moejs 对比 Sobek，每次操作 100 次内循环）：

| 用例 | moejs | Sobek | 加速比 | 分配次数（moejs / Sobek） |
|---|--:|--:|--:|--:|
| 属性读取，单态 | 4.2 µs | 10.6 µs | 2.5x | 9 / 96 |
| 属性读取，多态 | 4.4 µs | 7.5 µs | 1.7x | 8 / 11 |
| 函数调用 | 4.1 µs | 7.0 µs | 1.7x | 9 / 89 |
| 闭包 | 13.3 µs | 38.0 µs | 2.9x | 211 / 1,094 |
| 数组 push + for-of | 5.4 µs | 43.2 µs | 8.1x | 15 / 719 |
| 字符串拼接 | 14.0 µs | 22.3 µs | 1.6x | 399 / 712 |
| 字符串方法 | 135 µs | 478 µs | 3.5x | 909 / 11,810 |
| `JSON.parse` | 4.2 ms | 27.6 ms | 6.6x | 66k / 807k |
| `JSON.stringify` | 3.5 ms | 10.4 ms | 3.0x | 1,415 / 237k |
| `Object.keys` + `Object.assign` | 141 µs | 492 µs | 3.5x | 609 / 17,302 |
| RegExp test + replace | 123 µs | 309 µs | 2.5x | 2,210 / 9,503 |
| `new Error` + throw + catch | 19.1 µs | 45.1 µs | 2.4x | 300 / 1,393 |

基准测试位于 `bench/`，运行的是 new-api 的插件，由 `bench/testdata/plugins/fetch.sh` 按固定提交下载。复现命令：

```sh
bench/testdata/plugins/fetch.sh
cd bench
go test -run xxx -bench 'Benchmark(HookSuite|HostFlow|NewRuntime|Instantiate|Compile|Micro)$' -benchmem -count 5 .
go test -run TestFootprint -v .
```

`bench/scripts/run_all.sh` 运行全部测量，包括并发吞吐与分阶段基准。

**PGO。** `default.pgo` 以整套钩子负载录制（在 `bench/` 中运行 `go run ./cmd/pgo` 重新生成）。Go 只会自动使用 main 包目录下的
`default.pgo`，因此嵌入方需要传入 `-pgo=<moejs 路径>/default.pgo`，或把该文件复制到自己的 main 包目录。

## 测试

```sh
# 不在仓库中的测试输入：new-api 的插件与固定修订版的 test262。未下载时，依赖它们的测试会跳过。
bench/testdata/plugins/fetch.sh
bench/test262/fetch.sh

# 引擎的单元、审计与模糊测试语料。
go test ./...

# 与 Sobek 的差分测试、表达式语料、基准测试与 test262。
# bench/ 是独立 module，避免 Sobek 与 cgo 引擎成为 moejs 的依赖；其中 V8 与 QuickJS 基线需要 cgo。
cd bench && go test -timeout 30m ./...
```

test262 的运行规则见 [`bench/test262/README.md`](bench/test262/README.md)，按目录统计的结果见
[`bench/test262/RESULTS.md`](bench/test262/RESULTS.md)。

## 致谢

moejs 在设计上参考了以下项目，未复制其代码。

- [goja](https://github.com/dop251/goja) 与 [Sobek](https://github.com/grafana/sobek)：Go 互操作约定与
  `Export` 规则；Sobek 也是差分测试的参照。
- [QuickJS](https://bellard.org/quickjs/)：16 字节值布局、atom、形状转移与紧凑的内建表。
- [V8](https://v8.dev/)：hidden class、内联缓存、原型有效性校验（简化为一个计数器）、Ignition 寄存器式解释器、
  `Date.parse` 与 `Error.prototype.stack` 格式。
- [Lua 5.x](https://www.lua.org/)：定宽寄存器指令编码。
- [JavaScriptCore](https://webkit.org/) 与 [SpiderMonkey](https://spidermonkey.dev/)：NaN-boxing。
- [esbuild](https://github.com/evanw/esbuild)：用 Go 编写高性能 JavaScript 解析器的工程实践。
- [Hardened JavaScript / SES](https://github.com/endojs/endo/tree/master/packages/ses)：共享冻结内建所依据的
  `lockdown()` 模型。
- [quickjs-go](https://github.com/buke/quickjs-go) 与 [v8go](https://github.com/rogchap/v8go)：基准测试中的 cgo 基线。
- [test262](https://github.com/tc39/test262)：一致性测试集。
- [new-api](https://github.com/QuantumNous/new-api)：插件宿主，其 `pkg/jsplugin` 定义了 moejs 需要支持的 API。

## 许可证

moejs 以 [Apache License 2.0](LICENSE) 授权。

基准测试运行的 new-api 任务插件以 AGPL-3.0 授权，不包含在本仓库中，由
[`bench/testdata/plugins/fetch.sh`](bench/testdata/plugins/README.md) 下载。
