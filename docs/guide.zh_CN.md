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

已经返回的值仍然有效。模块存下的数据会连同它引用的内容一起保持存活，所以模块从某个参数里留下一个对象，可能让整个参数一直存活。

### 中断

任意 goroutine 都可以调用 `Interrupt(v)` 停止正在运行的代码，未完成的调用返回带有 `v` 的 `*InterruptedError`。脚本在下一个循环回边或函数调用处停下，
耗时长的内建函数在执行过程中检查中断。

没有代码运行时到达的中断会停止下一次调用，所以宿主复用运行时之前要调用 `ClearInterrupt`。

### 对象属于一个运行时

运行时里的 JavaScript 创建的对象属于这个运行时。使用另一个运行时的对象可能运行那个运行时的代码，所以运行时之间只传 Go 值和 JSON。

`Call`、`SetGlobal`、`FromGo` 和 `NewPromise` 的 settle 函数遇到属于另一个运行时的函数、生成器、绑定函数或代理时，返回 `ErrForeign`。
在 `FromGo` 转换的 Go 容器里读到这样的函数，会抛出消息与 `ErrForeign` 相同的 `TypeError`。

检查覆盖传入的值和 Go 容器的成员，跳过 JavaScript 对象的属性，也跳过另一个运行时里不可调用的代理，即使这个代理放在 Go map 里。
读取这种代理时，它的陷阱在读取它的运行时里运行。

## 代码

### 钩子

`Module.Hook(export, members...)` 一次性解析一个导出函数，或者导出对象下面的函数，例如 `mod.Hook("protocols", "openai", "decodeRequest")`。
绑定是活的：每次 `Call` 都在当前运行时里读取这个导出，再按自有属性逐级找到成员。

路径中途找不到时返回 `ErrHookNotFound`，路径终点的值不是函数时返回 `ErrNotCallable`。这两种情况下 `Has` 都返回 false。

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

结构体和其他类型会返回错误。先把它们序列化成 JSON，再交给 `ParseJSON`。

`SetGlobal(name, v)` 用同样的方式转换 `v`，所以宿主可以用 `NativeFunc` 组成的 `map[string]any` 安装一组函数。`Function(name, length, fn)`
包装一个 `NativeFunc`，并设置 JavaScript 看到的 `name` 和 `length`。

### 传入 JSON

`ParseJSON(b)` 对这些字节执行 `JSON.parse`，适合宿主以 JSON 形式保存的参数，比如存下的任务数据或请求体。它会先把字节复制成字符串。

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

### 请求之后还要保留的字符串

`ToGo`、`Unmarshal` 和 `ToGoInto` 从 `ParseJSON` 生成的值里返回的字符串，可能和解析的文本共用内存，并让整段文本一直存活。宿主要长期保留的字符串，先用
`strings.Clone` 复制一份。

### 嵌套深度

数组和对象嵌套超过 10,000 层时，`ParseJSON`、`ToGo` 和 `AppendJSON` 返回 `RangeError`。

## Promise 与错误

### Promise

`NewPromise` 给宿主函数一个可以返回的 promise，同时给出稍后兑现或拒绝它的 Go 函数。`PromiseResult` 读取 promise 的状态和结果。
`SetPromiseRejectionTracker` 报告没有被处理函数接住的拒绝，和 Sobek 的同名接口一样。

### 错误

JavaScript 抛出的异常以 `*Exception` 返回。`Name()` 和 `Message()` 读取抛出值的 `name` 和 `message` 数据属性，抛出的是原始值时就用这个值本身。
这两个方法都不执行 JavaScript。

宿主函数返回 Go error 时，JavaScript 里会抛出一个 `Error`，消息是这个错误的文本。对应的 `*Exception` 可以解包出原来的 Go error。

中断以 `*InterruptedError` 返回。有语法错误的模块或脚本返回带位置的 `*SyntaxError`。调用过程中的 Go panic（比如宿主函数 panic）以
`*InternalError` 返回，之后运行时仍然可以使用。

`Runtime.StackTrace(exc)` 返回被抛出的 `Error` 的 V8 格式 `stack`，同样不执行 JavaScript。

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
