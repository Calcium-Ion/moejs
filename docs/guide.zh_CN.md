# moejs 使用指南

每个函数的说明见[包文档](https://pkg.go.dev/github.com/Calcium-Ion/moejs)，完整示例见 [README](../README.zh_CN.md)。

[English](guide.md)

## 运行时与运行时池

### 模块和运行时

`Module` 编译后不可修改，任意多个运行时可以同时加载同一个 `Module`。宿主一般每个插件编译一次，再为它维护一个运行时池。

同一时刻只有一个 goroutine 使用一个 `Runtime`。其他 goroutine 只能调用 `Interrupt` 和 `ClearInterrupt`。

### 把运行时还回池里

请求用完运行时后，宿主先调用 `rt.ReleaseCallData()`，再把运行时还回池里，空闲的运行时随即释放这个请求的参数。`Call`、`ToGo`、`Get`
和 `AppendJSON` 都可能运行 JavaScript，所以要等这几个函数的最后一次调用结束后再释放。

`Call` 把这一步留给宿主。这样一个请求运行多个钩子时，多个钩子共用一次数据转换。不用运行时池的宿主可以跳过这一步。

已经返回的值仍然有效。模块存下的数据会连同它引用的内容一起保持存活，所以模块从某个参数里留下一个对象，可能让整个参数一直存活。`json.RawMessage`
在转换时被复制，所以宿主可以把它的缓冲区留给下一个请求复用。模块在读取之前就存下的 Go map 或切片仍然指向宿主的值，其中的 `json.RawMessage`
字节也算在内，模块保留它的期间，这些值不能改动。

### 内存

在 `ReleaseCallData` 之前，运行时会保留每个请求的输入和输出。输入是 `ParseJSON` 复制的那份文本，对 `FromGo` 来说则是宿主自己的 Go 值，
运行时只引用它们，不复制。例外是 `json.RawMessage`，`FromGo` 会复制它的文本，以及 `FromGo` 通过 `json.Marshal` 文本转换的类型。所以活跃堆
随同时处理的请求数乘以单个请求的大小增长，宿主自己为每个请求保留的数据还要另算。`ParseJSONString`，以及对宿主的 map、切片和字符串调用
`FromGo`，都不会复制输入里不含转义的 ASCII 字符串，比如 base64 数据。非 ASCII 的字符串无论哪种方式都以 UTF-16 保存。

在默认的 `GOGC=100` 下，Go 进程的常驻内存大约是活跃堆的两倍，因为回收器要等堆在活跃部分之外再增长同样多，才开始下一次回收。goroutine
很多，或者回收周期很长、又和其他任务争抢 CPU 时，常驻内存还会更高。

要限制内存，就把 `GOMEMLIMIT` 设在预计的活跃堆峰值之上并留出充足余量，因为这个上限计入的是 Go 运行时的全部内存，不只是堆，`GOGC`
保持默认值。这样在进程接近上限之前，吞吐量不受影响。`GOMEMLIMIT` 低于活跃堆时，回收器会一直运行，吞吐量大幅下降。调低 `GOGC` 则是在
整个进程范围内用吞吐量换内存。

举个例子：8.1 MB 的 JSON 请求体，带五张 base64 图片，同时处理 32 个请求，`GOMAXPROCS=8`，宿主在请求结束前一直保留请求体（Intel i5-13500H，
Linux，go1.26.6，每次运行 10 秒，取 3 次的中位数）。用 `ParseJSON` 时，活跃堆约 500 MB，常驻内存约 1.1 GB。用 `ParseJSONString`
时分别约 330 MB 和 740 MB，每秒处理的请求数是前者的 1.5 倍。同样用 `ParseJSONString`，`GOMEMLIMIT=768MiB` 时每秒请求数不变，
`GOMEMLIMIT=256MiB` 低于活跃堆，每秒请求数下降 38%，`GOGC=25` 让每秒请求数下降 9%，常驻内存降到约 410 MB。

### 中断

任意 goroutine 都可以调用 `Interrupt(v)` 停止正在运行的代码，未完成的调用返回带有 `v` 的 `*InterruptedError`。脚本在下一个循环回边或函数调用处停下，
耗时长的内建函数在执行过程中检查中断。

没有代码运行时到达的中断会停止下一次调用，所以宿主复用运行时之前要调用 `ClearInterrupt`。

由事件循环（下文的 `eventloop`）运行的运行时要通过 `Loop.Interrupt` 中断。单独调用 `Runtime.Interrupt` 能停止正在运行的
JavaScript，但不会唤醒正在睡眠、等待下一个定时器的循环。

### 内存上限

`Options.MemoryLimit` 限制一次请求的 JavaScript 在两次重置之间最多分配多少字节。`ReleaseCallData` 会重置计数，没有运行时池的宿主可以调用
`ResetAllocation`。超过上限的调用像被中断一样停下：返回 `*InterruptedError`，其值是 `*MemoryLimitError`，带有上限、已计的字节数和分配处的
JavaScript 调用栈，并且 `errors.Is(err, moejs.ErrMemoryLimit)` 成立。

```go
rt := moejs.NewRuntime(moejs.Options{MemoryLimit: 64 << 20})
res, err := rt.Call(hook, arg)
if errors.Is(err, moejs.ErrMemoryLimit) {
	var mle *moejs.MemoryLimitError
	errors.As(err, &mle)
	log.Printf("plugin used %d bytes, limit %d\n%s", mle.Allocated, mle.Limit, mle.Stack)
}
rt.ClearInterrupt()
rt.ReleaseCallData() // 下一个请求用新的额度
pool.Put(rt)
```

脚本捕获不到这次超限。引擎的分配不会失败，所以停下一个不断分配的脚本只能靠中断；中断不运行任何 `catch` 和 `finally`，并丢弃排队的
promise 任务，否则脚本捕获之后可以继续分配。分配前就知道结果大小的内建函数，在大小超过剩余额度时改为抛出它们在超过引擎限制时抛出的
`RangeError`，脚本可以捕获：`repeat`、`padStart` 和 `padEnd`、`new ArrayBuffer`、类型化数组构造函数、`btoa`、`escape`、`toBase64` 和 `toHex`。

计的是分配量，不是存活内存：垃圾在下次重置前一直计入，请求释放的内存也不会扣除。计入的有对象、对象属性和元素增长出来的存储、字符串、
`Map` 和 `Set` 的表、缓冲区、`BigInt` 结果、闭包的环境，以及 `eval` 和 `Function` 编译出的代码，各按向 Go 分配器申请的大小大致计算。
rope（两个长字符串拼接的结果）先计它的节点，读取并展平时再计它的内容。`FromGo` 计入它为宿主的 map 和切片建立的包装，但不计宿主的 Go
值本身，因为它直接引用而不复制；`ParseJSON` 计入它复制的文本。几个大小有界的内部缓存不计入，解释器的寄存器栈也不计入：调用深度限制约束它的大小，`ReleaseCallData` 会收缩它。计数与请求实际分配的量相差在一个不大的倍数内，
设置上限时要留出余量。

准备工作不计入第一次请求：`Load` 和 `SetGlobal` 属于准备工作，成功时都会在结束前重置计数（`Load` 返回 `ErrModulePending` 时也会）。
失败的 `Load` 保留计数和 `Stats` 里的超限错误，供宿主读取。在宿主函数里调用的 `SetGlobal` 不重置，否则正在运行的脚本会得到新的额度。
每个请求都设置全局变量的宿主，要在请求的第一次 `Call` 之前设置：在同一请求的两个钩子之间重置，会让这个请求再分配一次上限的量。
上限约束的是一次请求分配多少，而不是模块在请求之间保留多少；在请求之间持续增长的运行时最好丢弃。

超限之后运行时仍可复用：`ClearInterrupt` 加 `ReleaseCallData` 之后，它和其他请求结束后的池中运行时一样。不信任模块状态（超限可能让它只更新了一半）的宿主可以丢弃这个运行时。
只清除中断而不重置，额度仍然用完，下一次分配会再次停下运行时。

超限产生的是运行时唯一的待处理中断，所以宿主从另一个 goroutine 调用的 `Interrupt`（比如超时）可能替换它，调用随后返回宿主的错误。
无论如何 `Stats().MemoryLimitHits` 都会计入这次超限，`Stats().LastMemoryLimitError` 保留它的错误直到下次重置。

没有设置上限时运行时不计数，每个本该计数的分配点只多两次读取和两次分支。

在 `eventloop` 下，循环在每个定时器或 immediate 回调、每个任务之前重置额度，所以上限作用于一个宏任务及它排队的 promise 任务。

### 计数器

`rt.Stats()` 返回运行时的计数器，每项都是常数时间读取。宿主在使用该运行时的 goroutine 上读取，可以在请求之间，也可以在请求中的宿主函数里：

- `AllocatedBytes`、`Objects`、`Strings`、`Shapes`：设置上限以来的计数，`RequestAllocatedBytes` 是上次重置以来的。只在设置了上限时计数；
  上限设为 `math.MaxInt64` 可以只计数不限制。
- `MemoryLimitHits` 和 `LastMemoryLimitError`，见上文。
- `Interrupts`：`Interrupt` 的调用次数，包括超限。
- `ICEntries`：运行过的函数的内联缓存条目数。
- `PendingJobs`：排队的 promise 任务和微任务。
- `RegisterStackBytes`：解释器寄存器栈的大小。

运行时池可以在收回运行时时导出它们：

```go
st := rt.Stats()
metrics.Observe("plugin_request_bytes", st.RequestAllocatedBytes)
rt.ReleaseCallData()
```

宿主函数可以把它们交给模块：

```go
rt.SetGlobal("memoryUsage", moejs.NativeFunc(func(r *moejs.Realm, this moejs.Value, args []moejs.Value) (moejs.Value, error) {
	return moejs.Int(r.Stats().RequestAllocatedBytes), nil
}))
```

自己为 JavaScript 分配内存的宿主函数也可以计入，调用 `Realm.ChargeMemory(n)`，超过上限后它返回中断的错误。

### 对象属于一个运行时

运行时里的 JavaScript 创建的对象属于这个运行时。使用另一个运行时的对象可能运行那个运行时的代码，所以运行时之间只传 Go 值和 JSON。

`Call`、`CallFunction`、`SetGlobal`、`FromGo` 和 `NewPromise` 的 settle 函数遇到属于另一个运行时的函数、生成器、绑定函数或代理时，
返回 `ErrForeign`。
在 `FromGo` 转换的 Go 容器里读到这样的函数，会抛出消息与 `ErrForeign` 相同的 `TypeError`。

检查覆盖传入的值和 Go 容器的成员，跳过 JavaScript 对象的属性，也跳过另一个运行时里不可调用的代理，即使这个代理放在 Go map 里。
读取这种代理时，它的陷阱在读取它的运行时里运行。

## 代码

### 钩子

`Module.Hook(export, members...)` 一次性解析一个导出函数，或者导出对象下面的函数，例如 `mod.Hook("protocols", "openai", "decodeRequest")`。
绑定是活的：每次 `Call` 都在当前运行时里读取这个导出，再按自有属性逐级找到成员。

路径中途找不到时返回 `ErrHookNotFound`，路径终点的值不是函数时返回 `ErrNotCallable`。这两种情况下 `Has` 都返回 false。

### 函数值

`CallFunction(fn, this, args...)` 调用宿主之前拿到的函数值，比如插件传给宿主函数的回调。它和 `Call` 做同样的检查：
不是函数的值返回 `ErrNotCallable`，属于另一个运行时的函数返回 `ErrForeign`，宿主函数运行之前先检查中断。
调用排入的任务在它返回之前运行；在宿主函数里调用时，这些任务留到最外层调用结束时运行。

### 模块图

导入了其他模块的模块，由宿主先链接，再交给运行时加载。`moejs.Link(entry, resolve)` 向宿主的 `Resolver` 查询每个说明符对应的模块，
同一模块里的同一说明符查询一次，然后一次性链接整个模块图。模块图里的每个模块都来自解析器。

`Link` 返回要加载的 `Module`。它和其他模块一样不可修改，可以共享。它的 `Hook`、`Export` 和 `Exports` 指向入口模块的导出，包括再导出的名称。

解析器收到的 `referrer` 是发起导入的 `*Module`，类型是 `Referrer`。`Referrer` 是密封接口，表示请求模块的代码，取值是 `*Module` 或 `*Script`。

每个运行时实例化并求值链接好的模块图，每个模块求值一次。多个模块图共用的模块，只要解析器每次返回同一个 `*Module`，就只编译一次。

解析失败返回 `*ResolveError`。导入没有解析到唯一的绑定时返回 `*SyntaxError`。两种错误都带有导入方模块里的位置。`Load`
遇到有导入但没有链接过的模块时返回错误。没有导入的模块可以直接交给 `Load`。

```go
entry, err := moejs.Compile("plugin.js", source)
mod, err := moejs.Link(entry, func(referrer moejs.Referrer, specifier string) (*moejs.Module, error) {
	return host.module(specifier) // 每个进程只编译一次
})
err = rt.Load(mod) // 每个运行时
```

### 动态导入

`Options.Importer` 是宿主为 `import()` 和 `import.meta` 提供的实现。它的 `Resolve` 是一个 `Resolver`，收到的 `referrer` 是发起导入的
`*Module`，或者 `RunScript` 运行的 `*Script`。可选的 `Meta` 填写模块的 `import.meta`。`import.meta` 是首次使用时创建的对象，原型为 null。

`import(specifier)` 立即调用 `Resolve` 并返回一个 promise。promise 兑现为模块的命名空间对象，解析、链接或求值失败时以对应的错误拒绝。
没有设置 `Importer` 时，它以 `TypeError` 拒绝。

一个运行时对同一个模块只求值一次，静态导入和动态导入都算在内。`import()` 加载的模块图，每个 `Importer` 链接一次。任意多个运行时可以共用一个
`Importer`，每个运行时只做实例化和求值。任务队列和中断的行为与 `Load`、`Call` 相同。`import()` 和 `import.meta` 的开销只落在用到它们的代码上。

```go
imp := &moejs.Importer{Resolve: func(referrer moejs.Referrer, specifier string) (*moejs.Module, error) {
	return host.module(specifier)
}}
rt := moejs.NewRuntime(moejs.Options{Importer: imp}) // 所有运行时共用一个 imp
```

### TypeScript

`CompileTS(name, source)` 编译一个 TypeScript 模块。解析器边读边丢掉类型语法，做法和 Node 的类型剥离一样，模块按剩下的 JavaScript 运行。
不做类型检查。

解析器删掉的内容：

- 类型注解、类型参数和类型实参，`as`、`satisfies`、`x!` 和 `<T>x`；
- `interface` 和 `type` 声明，`declare` 之后的一切，函数重载签名，以及只含类型的 namespace；
- `import type`、`export type` 和标了 `type` 的说明符；
- `this` 参数，它不计入函数的 `length`；
- 类成员的 TypeScript 修饰符（`public`、`private`、`protected`、`readonly`、`override`、`abstract`、`declare`）、索引签名和没有函数体的方法。
  `declare` 或 `abstract` 字段什么也不产生。只有类型的字段仍是字段，初始值为 `undefined`，和 tsc 面向 ES2022 及以后的输出一致。

有运行时语义的 TypeScript 返回 `*SyntaxError`，错误信息会写明是哪种语法：`declare` 之外的 `enum` 和 `const enum`、带值的 namespace、
构造函数参数属性（`constructor(private x: T)`）、`import x = require("m")`、`export import x = A.B`、当作值使用的别名
`import x = A.B`、`export =`、`export as namespace` 以及 `accessor` 字段。装饰器和 JavaScript 里一样不支持。也不支持 TSX。

和 tsc 一样，编译器会删掉没有任何表达式读取的导入说明符，以及删完后没有绑定的导入声明。`import "m"` 保留。类型里的引用（包括
`typeof x`）不算读取。`T` 只是类型时，`export { T }` 和 `export default T` 也会被删掉。所以模块可以按名字从同一个包导入类型和值，
宿主为这个包提供的模块只需要导出值。

错误和栈追踪指向 TypeScript 源码，不需要 source map。`Function.prototype.toString` 返回函数的 TypeScript 源码，包括类型；
Node 返回的是类型被替换成空白的文本。

宿主编译不可信的 TypeScript 时应当限制源码长度。区分箭头函数和带括号的表达式所需的时间，可能随嵌套深度平方增长：像
`a ? (b): T => a ? (b): T => …` 这样在真分支里层层嵌套的条件表达式，2,000 层约需 1.5 秒，接近嵌套上限时需几秒，
而且一份源码里每个这样的表达式都会累加。`Runtime.Interrupt` 不能中止宿主自己调用的 `CompileTS` 或 `Compile`。

`Resolver` 或 `Importer` 可以按模块选择编译函数，比如按文件扩展名。`CompileTS` 和 `Compile` 编译的模块可以互相链接。

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

### 脚本

`CompileScript(name, source)` 把经典脚本编译成不可修改的 `*Script`。脚本以 `"use strict"` 指令开头时是严格模式，其余是非严格模式。
`Runtime.RunScript` 在运行时的全局环境里运行它，返回它的完成值。

脚本里的 `var` 和函数声明成为全局对象的属性。`let`、`const` 和 `class` 声明成为全局绑定，之后运行的脚本和已加载的模块都能看到。
声明和已有的全局绑定冲突时，脚本在运行任何代码之前抛出错误。

`SetGlobal` 写入全局对象。脚本声明 `let x` 之后，这个全局词法绑定会遮蔽 `SetGlobal("x", …)` 设置的值（ECMA-262 9.1.1.4.1）。

### Eval

导入 `moejs` 包就会安装编译器，`eval`、`Function` 系列构造函数和 `Realm.EvalScript(name, source)` 都用它编译。`NativeFunc` 可以对收到的
`*Realm` 调用 `EvalScript`，从字符串运行经典脚本，作用和 test262 的 `$262.evalScript` 一样。

直接 `eval` 能看到调用方的绑定，包括模块的导入。被求值的代码和它创建的函数，在栈追踪里以发起求值的脚本或模块为来源。这些代码里的 `import()`
也把这个脚本或模块作为 `referrer` 传给 `Resolver`。如果代码来自间接 `eval` 或 `Function` 系列构造函数，而发起调用的脚本或模块既没有用
`import()`、`import.meta`，也没有直接 `eval`，`referrer` 为 nil（见 [TODO.md](../TODO.md)）。

`eval` 和 `with` 的开销只落在用到它们的代码上。

### 动态代码的限制

`Options.MaxDynamicSource` 限制 `eval`、`Function` 系列构造函数和 `Realm.EvalScript` 能编译的源码长度。默认上限是 1 MiB（按 UTF-8 计），
设为负数表示不限制。超过上限的源码在解析之前抛出 `RangeError`。编译的时间和内存都随源码长度线性增长，这个上限同时限制了两者。
编译进行中也可以被中断。

`Options.DisableDynamicCode` 为一个运行时关闭动态代码，这时 `eval`、`Function` 系列构造函数和 `EvalScript` 都抛出 `EvalError`。

这两个选项作用于运行时里的动态代码。宿主自己调用的 `Compile` 和 `CompileScript` 照常编译。

## 值

### 传入 Go 值

`FromGo` 能转换这些 Go 类型：`nil`、布尔、数字、字符串、`json.Number`、`*big.Int`（转成 BigInt）、`Value`、`NativeFunc`，以及 JSON 形态的容器
`map[string]any`、`[]any`、`map[string]string`、`[]string`、`map[string][]string` 和 `[]map[string]any`。

`FromGo` 惰性转换容器：JavaScript 第一次读取时才转换，每次转换一层，所以钩子只为读到的那部分参数付出开销。JavaScript 写入时改的是 JavaScript
对象，Go 值保持不变。在 JavaScript 还可能读取的期间，宿主必须保持这个值不变。

由 Go map 转换来的对象按排序后的顺序枚举键。

`[]byte` 转换成共享同一段字节的 `ArrayBuffer`，所以 JavaScript 的写入会改到 Go 切片。

底层类型在上面列表中的命名类型按底层类型转换：`type Settings map[string]any` 转成惰性转换的 map，`type Status string` 转成字符串。

`encoding/json` 能写出的其他类型，比如结构体、指针、`map[string]int` 和 `[]map[string]string`，从它们的 `json.Marshal` 文本转换，
由引擎解析一次。结构体标签和 `MarshalJSON` 方法的效果和 `json.Marshal` 相同，`[]byte` 字段变成 base64 字符串。结果是一个快照：在 `FromGo`
运行时生成；放在 Go map 或切片里的值，在 JavaScript 第一次读取所在容器时生成。之后对结构体的修改看不到。

通道、`NativeFunc` 以外的函数、复数，以及 `json.Marshal` 拒绝的值会返回错误。`error`，以及 `json.Marshal` 一个字段都写不出的结构体（比如
`sync.Mutex` 或 `context.Context`）也会返回错误，除非类型有 `MarshalJSON` 或 `MarshalText` 方法：`json.Marshal` 会把它们写成 `{}`，或者写成
说明不了什么的文本。错误请传 `err.Error()`。完全没有字段的结构体是 `{}`。在容器里时，读取这样的成员会抛出 `TypeError`。

`SetGlobal(name, v)` 用同样的方式转换 `v`，所以宿主可以用 `NativeFunc` 组成的 `map[string]any` 安装一组函数。`Function(name, length, fn)`
包装一个 `NativeFunc`，并设置 JavaScript 看到的 `name` 和 `length`。

### 传入 JSON

`ParseJSON(b)` 对这些字节执行 `JSON.parse`，适合宿主以 JSON 形式保存的参数，比如存下的任务数据或请求体。它会先把字节复制成字符串，所以它返回后
宿主就可以复用这些字节。

`ParseJSONString(s)` 解析一个字符串，字符串是有效的 UTF-8 时不做这次复制，结果里的字符串可能和 `s` 共用内存。宿主以 `[]byte` 持有
文本时，可以传入 `unsafe.String(unsafe.SliceData(b), len(b))`，前提是在 `ReleaseCallData` 之前，以及宿主或模块还保留着由这些字节得出的
值的期间，从不修改这些字节。没有任何检查保证这一点，由宿主负责。

`FromGo` 也接受 `json.RawMessage`，可以单独传入，也可以作为 `map[string]any` 或 `[]any` 的成员，JavaScript 拿到的是对其文本执行
`JSON.parse` 的结果。转换用的是文本的副本：单独传入时在 `FromGo` 运行时复制，作为成员时在 JavaScript 第一次读取所在容器时复制。之后宿主
可以复用这些字节。对象或数组的文本在转换时检查，在 JavaScript 第一次读取这个值时解析，所以从不读取它的钩子不用付解析的开销。其他文本立即
解析。无效的文本是 `SyntaxError`：顶层的值由 `FromGo` 返回这个错误，容器里的值在读取成员时抛出。nil 的 `json.RawMessage` 是 `null`。
JavaScript 没有读取过的 `json.RawMessage` 由 `ToGo` 返回为一个带有这段文本的新 `json.RawMessage`。

`json.RawMessage` 的类型化容器，比如 `[]json.RawMessage`、`map[string]json.RawMessage`、`*json.RawMessage` 或结构体字段，和其他类型一样
经过 `json.Marshal`，并立即解析。

钩子一定会读取的文本用 `ParseJSON` 或 `ParseJSONString` 更省：它们只解析一次，`ParseJSON` 也只复制一次。`json.RawMessage` 先复制并检查
文本，钩子读取时才解析，所以适合钩子不一定读取的文本。

### 读出 Go 值

`ToGo` 的转换规则：

- 整数转成 `int64`，其他数字转成 `float64`
- 数组转成 `[]any`
- 对象转成由自有可枚举属性组成的 `map[string]any`
- `Date` 转成 `time.Time`
- BigInt 转成 `*big.Int`
- `ArrayBuffer`、类型化数组和 `DataView` 转成 `[]byte`，内容是它持有或指向的字节的副本

`FromGo` 转换的参数只要没被 JavaScript 修改过，就原样返回最初的 Go 值。

`Get(v, key)` 读取一个属性，会执行 getter。`Export(name)` 读取已加载模块某个导出的当前值。

### 读出 JSON

`AppendJSON` 执行 `JSON.stringify`，把文本追加到字节切片。它的输出和对 `ToGo` 的结果做 `json.Marshal` 有几处不同：值为 `undefined`
的成员会省略，NaN 和 ±Infinity 写成 `null`，键按插入顺序输出，并且会调用 `toJSON`。

`AppendJSON` 按上次输出的长度再加八分之一预留空间，写进切片的剩余容量。宿主把上次的输出传回去（`buf, err = rt.AppendJSON(buf[:0], v)`）时，
有两种情况会分配内存：切片剩余空间不够这个大小，或者值里有可能运行代码的部分。可能运行代码的部分包括 `toJSON` 方法（`Date` 的也算）、getter、
代理、BigInt，以及原型不是 `Object.prototype` 或 `Array.prototype` 的对象或数组，比如类实例、`Map` 和 `Object.create(null)`。遇到这些值时，
输出先写进一个随写入增长的独立缓冲区，最后再追加到切片。

输出最长 2^30−24 字节。

### 写入宿主自己的类型

`Unmarshal(v, target)` 把值写进一个 Go 值，结果和对 `AppendJSON` 的文本做 `json.Unmarshal` 相同。普通对象、数组和没有被修改过的参数直接写进
Go 值，跳过文本和字符串复制。其他值，比如带 `toJSON` 的值、getter，或者自己实现反序列化的目标类型，会经过文本。

`AppendJSON` 会失败的情况下（BigInt、循环引用、抛出异常的 `toJSON`、无法写出的 Go 值、超过长度上限的文本），`target` 的内容保持不变。
`json.Unmarshal` 返回错误时，`target` 里是出错之前已经写入的部分。

`ToGoInto(v, target)` 的结果和错误，与依次执行 `ToGo`、`json.Marshal` 和 `json.Unmarshal` 写进 `target` 相同。已经在用这三步的宿主可以
直接换成它，行为不变。和 `Unmarshal` 一样，普通对象、数组和没有被修改过的参数直接写进 Go 值，不生成 `ToGo` 的 Go map，也不生成文本。getter、
代理、`Date`、`Map`、类型化数组、BigInt、函数，以及自己实现反序列化的目标类型，会改走这三步。`ToGo` 或 `json.Marshal` 会失败的情况下
（抛出异常的 getter、NaN 或 ±Infinity、循环引用），`target` 的内容保持不变。

`ToGo` 和 `AppendJSON` 结果不同的地方，`ToGoInto` 和 `ToGo` 一致：值为 `undefined` 的成员保留为 `null`，-0 保持 -0，NaN 和 ±Infinity
返回错误，不调用 `toJSON`。和 `ToGo` 一样，只有 getter 运行时才会被中断。

目标里的 `json.RawMessage` 收到值中对应部分的 JSON 文本，目标的其余部分仍然直接写入。它可以是字段、指针、map 的值、切片元素或目标本身。
写入的字节和往返时得到的相同：`Unmarshal` 写入 `AppendJSON` 的文本；`ToGoInto` 写入对 `ToGo` 结果做 `json.Marshal` 的文本，键已排序，
`<`、`>` 和 `&` 已转义。和 `json.Unmarshal` 一样，文本放得下时写进 `json.RawMessage` 自己的数组。对于 `ToGoInto`，钩子未读取就返回的
`json.RawMessage` 参数不会被解析：目标收到的是 `json.Marshal` 压缩后的文本。其他自己实现反序列化的类型仍然让整个值走往返。

### 限制结果的大小

`UnmarshalWith`、`ToGoIntoWith` 和 `ToGoWith` 接受 `DecodeOptions`，限制结果的 JSON 文本。`MaxBytes` 限制文本长度，`MaxNodes` 限制其中值的
个数：对象、数组、字符串、数字、布尔值和 null，键不计入。这里的文本是往返时写出的文本：`UnmarshalWith` 用 `AppendJSON` 的文本，另外两个用
对 `ToGo` 结果做 `json.Marshal` 的文本。计数是精确的：文本为 `n` 字节的结果能通过 `MaxBytes: n`，通不过 `MaxBytes: n-1`。字段为零表示
不限制。

超过限制的结果返回一个包装了 `ErrTooLarge` 的错误，`target` 的内容保持不变。不需要运行 JavaScript 就能读取的值，即普通对象、数组和来自
`FromGo` 的参数，在解码任何内容之前计数，不写出文本。钩子未读取就返回的 `json.RawMessage` 参数是 `UnmarshalWith` 的唯一例外：
`AppendJSON` 写出的是它解析成的值的文本，所以要先解析才能计数，除非对它的一次快速扫描已经说明文本超过限制。`ToGoIntoWith` 和 `ToGoWith`
不解析就能计数它的文本。需要运行 JavaScript 才能读取的值，比如 `toJSON`、getter 或代理，在往返写出文本之后计数（`ToGoWith` 则在 `ToGo`
得出结果之后计数），但仍在解码任何内容之前。循环引用和超过 10,000 层的嵌套超过所有限制。

不设限制时，这几个函数就是 `Unmarshal`、`ToGoInto` 和 `ToGo`，开销相同。

### 请求之后还要保留的字符串

`ToGo`、`Unmarshal` 和 `ToGoInto` 从 `ParseJSON` 或 `ParseJSONString` 生成的值或 `json.RawMessage` 参数里返回的字符串，可能和解析的文本
（对 `ParseJSONString` 来说是宿主的字符串，对 `json.RawMessage` 来说是运行时复制的那份文本）共用内存，并让整段文本一直存活。对字符串值
调用 `String()` 得到的字符串也是这样。宿主要长期保留的字符串，先用 `strings.Clone` 复制一份。

### 嵌套深度

数组和对象嵌套超过 10,000 层时，`ParseJSON`、`ParseJSONString`、`ToGo` 和 `AppendJSON` 返回 `RangeError`。

## Promise 与错误

### Promise

`NewPromise` 给宿主函数一个可以返回的 promise，同时给出稍后兑现或拒绝它的 Go 函数。`PromiseResult` 读取 promise 的状态和结果。
`SetPromiseRejectionTracker` 报告没有被处理函数接住的拒绝，和 Sobek 的同名接口一样。

`ThrownValue(err)` 返回宿主函数返回 `err` 时抛出的值。宿主用它拒绝 promise，JavaScript 就拿到同样的 `Error`；
这个值作为 `*Exception` 回到宿主时可以解包出 `err`。

settle 函数只能在使用运行时的那个 goroutine 上调用。要从其他 goroutine 兑现 promise，用下面的 `eventloop` 包。

### 错误

JavaScript 抛出的异常以 `*Exception` 返回。`Name()` 和 `Message()` 读取抛出值的 `name` 和 `message` 数据属性，抛出的是原始值时就用这个值本身。
这两个方法都不执行 JavaScript。

宿主函数返回 Go error 时，JavaScript 里会抛出一个 `Error`，消息是这个错误的文本。对应的 `*Exception` 可以解包出原来的 Go error。

中断以 `*InterruptedError` 返回。中断的值是 error 时，它可以解包出这个值，所以调用 `Interrupt(context.Cause(ctx))` 的宿主可以用
`errors.Is` 和 `errors.As` 找到 `context.DeadlineExceeded` 或自己的原因。有语法错误的模块或脚本返回带位置的 `*SyntaxError`。调用过程中的 Go panic（比如宿主函数 panic）以
`*InternalError` 返回，之后运行时仍然可以使用。

在宿主函数里调用的 `Call` 或 `CallFunction` 会把其下发生的 panic 的 `*InternalError` 返回给这个宿主函数。宿主函数把它作为错误返回时，
JavaScript 看到的是一个抛出的 `Error`，和其他 Go error 一样可以被捕获。

`Runtime.StackTrace(exc)` 返回被抛出的 `Error` 的 V8 格式 `stack`，同样不执行 JavaScript。

## 事件循环和定时器

### 运行循环

单独的 `Runtime` 没有定时器。`eventloop` 包提供定时器，以及一个在运行期间独占运行时的事件循环：

```go
loop, err := eventloop.New(rt, eventloop.Options{})
err = loop.Run(func(rt *moejs.Runtime) error {
	_, err := rt.RunScript(script) // 脚本里可以调用 setTimeout
	return err
})
```

`New` 安装全局函数 `setTimeout`、`clearTimeout`、`setInterval`、`clearInterval`、`setImmediate` 和 `clearImmediate`。
`Run` 先调用传入的函数，然后在同一个 goroutine 上运行定时器、immediate 和投递的任务。没有东西让循环继续时 `Run` 返回：
没有 ref 状态的定时器或 immediate，没有未完成的 `Hold`，没有未兑现的 `NewPromise`，也没有未运行的任务。

模块的顶层 `await` 在等定时器时，`Load` 返回 `ErrModulePending`。循环之后运行这个定时器，模块随之执行完，
所以传给 `Run` 的函数把这个错误当作成功。

`Start` 则在新的 goroutine 上运行循环。这样的循环空闲时也不退出，直到 `Stop` 或 `Terminate`。

### 阶段

每轮循环有三个阶段，对应 Node 的 timers、poll 和 check 阶段：

1. 阶段开始时已到期的定时器，按到期时间排序，时间相同的按设置顺序。
2. 阶段开始前投递的任务：`RunOnLoop`、`Hold` 的 `done` 函数和 `NewPromise` 的 settle 函数。
3. 阶段开始前设置的 immediate。

一个阶段新加入的工作留到之后的轮次。延迟至少 1 毫秒，所以定时器回调里设置的定时器在之后的轮次运行。
回调排入的 promise 任务和 `queueMicrotask` 回调在它返回时运行，先于下一个回调。没有工作时，循环睡眠到下一个定时器到期或有任务到来。

### 与 Node 的兼容

需要定时器的代码是为 Node.js 写的，所以定时器遵循 Node.js：

- `setTimeout` 和 `setInterval` 返回 `Timeout` 对象，带有 `ref`、`unref`、`hasRef`、`refresh`、`close` 和返回数字 id 的
  `[Symbol.toPrimitive]`。`setImmediate` 返回 `Immediate` 对象，带有 `ref`、`unref` 和 `hasRef`。
- 回调收到额外的参数，`this` 是 `Timeout` 或 `Immediate` 对象。回调不是函数时抛出 `code` 为 `ERR_INVALID_ARG_TYPE` 的
  `TypeError`。字符串不会被当作代码执行。
- 延迟经过 `ToNumber` 转换，再截断为整毫秒。NaN、小于 1 和大于 2147483647 的延迟都按 1 毫秒处理。
- interval 在回调开始的时刻加上延迟重新排期。`refresh` 从现在起重新计算定时器的延迟，已经触发过的 timeout 会再运行一次。
- `clearTimeout` 和 `clearInterval` 接受 `Timeout` 对象，或者读取过之后的 id（数字或字符串）。`clearImmediate` 接受
  `Immediate` 对象。三者都忽略其他值，在定时器自己的回调里调用也有效。
- unref 状态的定时器或 immediate 不会让 `Run` 继续，只在有别的东西让循环继续时运行。

有些细节和 Node 不同。`Timeout` 和 `Immediate` 对象没有自有属性：`JSON.stringify` 得到 `{}`，`constructor.name` 是
`"Object"`。它们的方法在别的对象上调用时抛出 `TypeError`，包括这种对象的 `Proxy`；`Timeout` 没有 `Symbol.dispose` 方法。
定时器按精确的到期时间排序，Node 则比较整毫秒。不提供 `timers/promises`、`AbortSignal` 选项和 `util.promisify`。

### 异步宿主函数

循环运行期间只有它的 goroutine 使用运行时。其他 goroutine 用三种方式把工作交给它：

- `RunOnLoop(fn)` 投递 `fn`。循环终止后返回 false。
- `Hold()` 让循环一直运行到它的 `done(fn)` 被调用，`done` 会投递 `fn`。
- 宿主函数在循环上调用 `NewPromise()`，得到一个 promise 和一个 `settle` 函数。`settle(f)` 在循环上运行 `f`：
  返回值兑现 promise，Go error 则以宿主函数的同一个错误会抛出的 `Error`（`ThrownValue`）拒绝它。

`done` 和 `settle` 可以在任意 goroutine 上调用。第一次调用生效，之后的调用不做任何事。`Hold` 要在循环上调用，
也就是在宿主函数或回调里。在别的 goroutine 上调用的 `Hold` 要和一个已经没有别的事可做的 `Run` 赛跑，几乎总是输掉，
结果只能让下一次 `Run` 继续运行。

```go
rt.SetGlobal("fetchJSON", moejs.NativeFunc(func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
	url := moejs.Arg(args, 0).String()
	p, settle := loop.NewPromise()
	go func() {
		body, err := download(url) // 阻塞的工作在循环之外运行
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

### 错误

下面这些错误会停止循环，并由 `Run` 返回：回调抛出的异常、回调之后的任务抛出的第一个异常（比如 `queueMicrotask` 回调里的）、
宿主函数里的 Go panic，以及 `settle` 的失败。Node.js 进程遇到未捕获的异常也会结束。设置了 `Options.OnError` 时，
循环把这些错误交给它，然后继续运行。`Run` 自己的函数返回的错误让 `Run` 立即返回。

中断总是停止循环，而且在循环从队列取出下一个定时器、任务或 immediate 之前停止，队列里的东西都保留下来。`Run` 返回
`*InterruptedError`。`Loop.Interrupt(v)` 中断运行时并唤醒循环；单独调用 `Runtime.Interrupt` 时，睡眠中的循环要等到下一个定时器才醒。
中断会保留，所以宿主再次运行循环之前要调用 `ClearInterrupt`。

宿主任务（`RunOnLoop`、`done` 和 `settle` 的函数，以及 `OnError`）里的 panic 不会被恢复。它从 `Run` 向外展开；
`Start` 启动的循环则会让程序退出。当前阶段还没运行的任务和 immediate 留在队列里，等下一次 `Run`。发生 panic 的任务本身丢失，
settle 函数 panic 的 promise 永远不会兑现。

没有处理函数接住的拒绝由宿主通过 `SetPromiseRejectionTracker` 处理。

### 停止和复用

`Stop` 在当前回调结束后停止循环，并等到循环结束。它像 `Run` 一样返回结束这次运行的原因：`nil`、停止循环的错误、中断的
`*InterruptedError` 或 `ErrTerminated`，宿主由此知道 `Start` 启动的循环因为什么错误停止。`StopNoWait` 不等待，所以循环上的回调可以调用它。
`Stop` 和 `Terminate` 不能在循环上调用（传给 `Run` 的函数也算在循环上），否则会等待自己。

`Run` 返回或循环停止后，运行时重新归宿主使用。还没运行的东西保持原样，比如 unref 状态的定时器，或者停止、错误和中断留下的一切；
下一次 `Run` 或 `Start` 会接着运行。宿主在两次之间运行的 JavaScript（比如通过 `Call`）也可以设置新的定时器。

`Terminate` 永久结束循环。它用 `ErrTerminated` 中断运行时，从而停止正在运行的 JavaScript，丢弃所有待运行的定时器、immediate
和任务，并等到循环结束。之后 `Run` 返回 `ErrTerminated`，`RunOnLoop` 返回 false，定时器函数会抛出异常。即使当时没有 JavaScript
在运行，中断也会保留，所以宿主再次使用运行时之前要调用 `ClearInterrupt`。再调用一次 `New` 可以给运行时一个新的循环。

## 隔离

### 共享冻结的内建对象

默认情况下，所有运行时共用一套深度冻结的内建对象，所以每个插件看到的内建对象都一样。这是 Hardened JavaScript（SES `lockdown()`）的模型。
写入 `Array.prototype` 在严格模式代码里抛出 `TypeError`，在非严格模式代码里和写入其他冻结对象一样静默失败。

内建对象的全局绑定也被锁定。`Promise = MyPromise` 在非严格模式下被忽略，在严格模式下抛出错误，`delete globalThis.Array` 返回 false。
插件想用自己的 `Promise` 时，在自己的模块里声明同名绑定。

写入共享内建对象的操作在任何 setter 运行之前就失败，旧式的 `RegExp.input = v` 和 `RegExp.$_ = v` 也一样。`RegExp.$1` 等旧式静态属性读取的是
本运行时自己最近一次的匹配。

`Options{MutableIntrinsics: true}` 给每个运行时单独建一份可修改的内建对象，适合插件会修改内建对象的宿主。这样创建运行时的成本约为默认的 25 倍。

### 按运行时设置时区

`Options.TimeZone` 设置 `Date` 使用的本地时区，nil 表示 `time.Local`。

## 实现

### 16 字节的值

`Value` 占 16 字节，由一个 `unsafe.Pointer` 和一个 `uint64` 组成。数字、布尔、`undefined` 和 `null` 不需要分配内存。指针字里始终是有效指针或 nil，
Go 的垃圾回收器可以安全地扫描它。

### 形状和内联缓存

对象共用一棵不可修改的形状转移树，按属性插入顺序组织。属性访问指令带有内联缓存槽，原型链是否仍然有效由一个计数器判断。由 Go map
转换来的对象按键集合共用形状，所以插件代码读取 `ctx.xxx` 时跨调用保持单态。

### 寄存器字节码虚拟机

虚拟机执行定宽 32 位寄存器指令，每个运行时有一段连续的寄存器栈。函数调用不分配内存。异常通过处理表展开。

### 字符串

字符串有三种形式：ASCII（直接使用 Go string，零拷贝）、UTF-16 和 rope。rope 在需要时扁平化。内建属性名是静态 atom。

### 锁

属性访问、函数调用、字符串操作、宿主值转换和正则匹配都无锁运行。不同 goroutine 上的运行时只共用垃圾回收器。

### 正则表达式

moejs 能精确翻译的正则表达式交给 Go 的 `regexp`（RE2，线性时间）执行，其他的由纯 Go 回溯引擎执行。回溯引擎的栈有上限，每 4,096 步检查一次中断。

### 有界缓存

即使每个请求都带来以数据为键的对象（ID、用户输入、宿主 map 的键），池中运行时的缓存也保持在固定上限之内。每个 intern 缓存最多保存 4,096 个名称。
宿主形状缓存最多保存 1,024 个键集合，合计 8,192 个键。缓存满了以后，运行时清空它，再按实际使用重新填充。一个形状强引用前 8 个转移，
其余的转移用弱引用，所以以数据为键的对象被回收时，它们的形状也一起被回收。
