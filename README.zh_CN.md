<div align="center">

<img src="docs/assets/moejs-logo.svg" width="160" alt="moejs">

# moejs

纯 Go 实现的 JavaScript 运行时，用来在 Go 程序里运行 JS 插件。

<p align="center">
  <strong>简体中文</strong> |
  <a href="./README.md">English</a>
</p>

</div>

moejs 支持 ES 模块、类、async/await、Proxy、BigInt 等现代 JavaScript 特性，在 test262 一致性测试集上运行的 79,385 个测试全部通过。

和同样是纯 Go 实现的 Sobek 跑同一批插件，moejs 的调用耗时约为它的一半，每个运行时的内存约为它的三分之一，服务可以给每个并发请求分一个运行时。

## 性能

测试负载是 new-api 的 10 个任务插件和 269 个录制下来的调用。计时在 Go 调用方进行，包括参数和结果的转换。

| | moejs | Sobek | QuickJS（quickjs-go） | V8（v8go） |
|---|--:|--:|--:|--:|
| 一次插件调用 | 6.9 µs | 14.3 µs | 104.5 µs | 56.6 µs ¹ |
| 新建运行时 | 1.4 µs | 2.2 µs | 382 µs | 1,153 µs ¹ |
| 加载最大的插件后，每个运行时的内存 | 81 KiB | 264 KiB | 348 KiB ² | 1,544 KiB ² |

¹ V8 的耗时在测试机上波动很大。² 引擎自己的堆。

Sobek 和 moejs 一样是纯 Go 引擎，QuickJS 和 V8 通过 cgo 调用。测试机器、完整结果和复现方法见
[docs/performance.zh_CN.md](docs/performance.zh_CN.md)。

## 功能

### 纯 Go

可以用 `CGO_ENABLED=0` 编译，交叉编译不需要 C 工具链。引擎就是普通的 Go 代码，pprof 和 race 检测器能看到它内部。

### 和 Go 之间传值

插件函数直接接收 Go 的 map、切片或 JSON。插件读到 map 的哪一层，moejs 才转换哪一层。返回值可以读成 Go 值或 JSON，也可以直接写进你自己的结构体，结果和 `json.Unmarshal` 相同。宿主函数就是普通的 Go 函数，也可以返回 promise，之后在 Go 里兑现。

### 插件能接触到什么

插件能用的是 JavaScript 标准库和你装进去的全局变量。每个 `import` 和 `import()` 都交给你提供的 Go 函数解析，`eval` 和 `new Function` 可以限制源码长度，也可以关掉。内建对象是冻结的，所有运行时共用一份，所以每个插件看到的 `Array.prototype` 都一样。每个运行时有自己的全局变量。

### 超时和错误

任意 goroutine 都可以中断正在运行的插件，死循环也能停下。JavaScript 异常、语法错误、中断和宿主函数里的 panic 各自以不同的 Go 错误类型返回，抛出的 `Error` 能取到 V8 格式的调用栈。宿主函数 panic 之后，运行时还能继续使用。

## 快速上手

```sh
go get github.com/Calcium-Ion/moejs
```

需要 Go 1.25 或更高版本。moejs 只依赖标准库（测试用到 testify）。

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

服务端照 `Plugin` 这样调用插件，每个请求的开销最小。从池里取一个运行时只是一次 channel 接收，新建运行时并加载最大的插件约需 71 µs。
运行时池、模块图、值的转换、Promise 和错误的细节见[使用指南](docs/guide.zh_CN.md)，每个函数的说明见[包文档](https://pkg.go.dev/github.com/Calcium-Ion/moejs)。

moejs 仓库里有一份按插件负载录制的 `default.pgo`。Go 只自动使用 main 包目录下的 profile，所以构建时要手动传入：

```sh
go build -pgo="$(go list -m -f '{{.Dir}}' github.com/Calcium-Ion/moejs)/default.pgo" .
```

## JavaScript 支持

moejs 运行 ES 模块和经典脚本，包括非严格模式和 Annex B 的网页兼容行为。语言特性有类的字段、私有成员和静态块，
解构、可选链、生成器、async 函数和异步迭代。标准库有 `Proxy`、`Reflect`、`BigInt`、类型化数组、可调整大小的
`ArrayBuffer`、`WeakRef`、`structuredClone`、`TextEncoder`/`TextDecoder`，以及 `Set` 的新方法、`Promise.try`、
`Float16Array`、`Array.fromAsync`、`Math.sumPrecise`、`Error.isError` 这些新近加入标准的 API。正则表达式支持所有标志和
Unicode 17 属性，`Date` 的时区数据来自 Go。

[test262](https://github.com/tc39/test262) 上 79,385 个测试通过，0 个失败。另外 14,058 个测试用到 moejs 没有实现的特性，
测试时跳过。按目录统计的结果见 [bench/test262/RESULTS.md](bench/test262/RESULTS.md)。

未实现的特性有：导入属性和 JSON 模块、`using` 声明和 `DisposableStack`、装饰器、迭代器辅助方法、`Intl`、`Temporal`、
`ShadowRealm`、`FinalizationRegistry`、定时器和 `JSON.rawJSON`。完整列表、已知的错误结果和各项限制见 [TODO.md](TODO.md)。

## 状态

moejs 目前是 alpha 版本，API 在版本之间可能会变。

## 测试

```sh
# 测试输入需要单独下载：new-api 的插件和固定版本的 test262。没下载时，依赖它们的测试会跳过。
bench/testdata/plugins/fetch.sh
bench/test262/fetch.sh

# 引擎的单元测试、审计测试和模糊测试语料。
go test ./...

# 和 Sobek 的差分测试、表达式语料、基准测试和 test262。
# bench/ 是单独的 Go module，Sobek 和 cgo 引擎只出现在它的依赖里。V8 和 QuickJS 基线需要 cgo。
cd bench && go test -timeout 30m ./...
```

## 致谢

moejs 的设计参考了下面这些项目，代码是独立编写的。

- [goja](https://github.com/dop251/goja) 和 [Sobek](https://github.com/grafana/sobek)：Go 互操作的约定和
  `Export` 规则。Sobek 也是差分测试的参照。
- [QuickJS](https://bellard.org/quickjs/)：16 字节的值布局、atom、形状转移和紧凑的内建表。
- [V8](https://v8.dev/)：hidden class、内联缓存、原型有效性检查（简化成一个计数器）、Ignition 寄存器解释器、
  `Date.parse` 和 `Error.prototype.stack` 的格式。
- [Lua 5.x](https://www.lua.org/)：定宽寄存器指令编码。
- [JavaScriptCore](https://webkit.org/) 和 [SpiderMonkey](https://spidermonkey.dev/)：NaN-boxing。
- [esbuild](https://github.com/evanw/esbuild)：用 Go 写快速 JavaScript 解析器的做法。
- [Hardened JavaScript / SES](https://github.com/endojs/endo/tree/master/packages/ses)：共享冻结内建对象所用的
  `lockdown()` 模型。
- [quickjs-go](https://github.com/buke/quickjs-go) 和 [v8go](https://github.com/rogchap/v8go)：基准测试里的 cgo 对照。
- [test262](https://github.com/tc39/test262)：一致性测试集。
- [new-api](https://github.com/QuantumNous/new-api)：插件宿主，它的 `pkg/jsplugin` 决定了 moejs 要支持哪些 API。

## 许可证

moejs 采用 [Apache License 2.0](LICENSE) 许可。

基准测试用到的 new-api 任务插件采用 AGPL-3.0 许可，需要用
[`bench/testdata/plugins/fetch.sh`](bench/testdata/plugins/README.md) 单独下载。
