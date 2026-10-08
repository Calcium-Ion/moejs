package engines

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	quickjs "github.com/buke/quickjs-go"
)

// QuickJSEngine drives QuickJS through buke/quickjs-go (cgo). Compile
// rewrites the ESM exports to globals. Arguments cross the boundary as JSON
// text (Go marshal -> JS parse) and results come back the same way, which is
// how its users move JSON data.
//
// It comes in two configurations. "quickjs-go" keeps the library defaults:
// every API call checks that it runs on the goroutine that created the
// runtime (v0.7.7 parses the goroutine ID out of runtime.Stack each time),
// and Instantiate parses the script source again in every context.
// "quickjs-go-tuned" is how a host that pools runtimes would run it: the
// owner check is off (WithOwnerGoroutineCheck(false); the pool already hands
// each runtime to one goroutine at a time, and with the check on no other
// goroutine could use a pooled runtime at all), Compile turns the script
// into QuickJS bytecode once, so Instantiate only loads that bytecode, and
// results are stringified with JSON.stringify and freed (Value.JSONStringify
// never frees its string in v0.7.7).
type QuickJSEngine struct {
	host  Host
	tuned bool
}

func NewQuickJSEngine() *QuickJSEngine { return &QuickJSEngine{host: DefaultHost} }

// NewQuickJSTunedEngine returns the "quickjs-go-tuned" configuration.
func NewQuickJSTunedEngine() *QuickJSEngine { return &QuickJSEngine{host: DefaultHost, tuned: true} }

func (e *QuickJSEngine) Name() string {
	if e.tuned {
		return "quickjs-go-tuned"
	}
	return "quickjs-go"
}

func (e *QuickJSEngine) runtimeOptions() []quickjs.Option {
	if e.tuned {
		return []quickjs.Option{quickjs.WithOwnerGoroutineCheck(false)}
	}
	return nil
}

// scriptModule is a module rewritten to a script for the cgo engines;
// bytecode is set when the tuned QuickJS engine compiled it.
type scriptModule struct {
	name     string
	script   string
	bytecode []byte
}

func (m *scriptModule) ModuleName() string { return m.name }

func (e *QuickJSEngine) Compile(name, source string) (CompiledModule, error) {
	script, err := ESMToGlobals(source)
	if err != nil {
		return nil, err
	}
	m := &scriptModule{name: name, script: script}
	if !e.tuned {
		return m, nil
	}
	rt := quickjs.NewRuntime(e.runtimeOptions()...)
	defer rt.Close()
	ctx := rt.NewContext()
	defer ctx.Close()
	m.bytecode, err = ctx.Compile(script, quickjs.EvalFileName(name), quickjs.EvalFlagStrict(true))
	if err != nil {
		return nil, wrapQuickJSError(err)
	}
	return m, nil
}

type QuickJSRuntime struct {
	rt            *quickjs.Runtime
	ctx           *quickjs.Context
	globals       *quickjs.Value
	jsonStringify *quickjs.Value   // JSON.stringify, tuned engine only
	keep          []*quickjs.Value // host function values kept alive for Close
}

func (e *QuickJSEngine) NewRuntime() (Runtime, error) {
	rt := quickjs.NewRuntime(e.runtimeOptions()...)
	ctx := rt.NewContext()
	q := &QuickJSRuntime{rt: rt, ctx: ctx, globals: ctx.Globals()}
	if e.tuned {
		jsonGlobal := q.globals.Get("JSON")
		q.jsonStringify = jsonGlobal.Get("stringify")
		jsonGlobal.Free()
	}
	h := e.host
	str := func(args []*quickjs.Value, i int) string {
		if i < len(args) {
			return args[i].String()
		}
		return ""
	}
	throw := func(c *quickjs.Context, err error) *quickjs.Value { return c.ThrowTypeError("%s", err.Error()) }
	utils := ctx.NewObject()
	set := func(obj *quickjs.Value, name string, fn func(*quickjs.Context, *quickjs.Value, []*quickjs.Value) *quickjs.Value) {
		obj.Set(name, ctx.NewFunction(fn))
	}
	set(utils, "hasCapability", func(c *quickjs.Context, _ *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		return c.NewBool(h.HasCapability(str(args, 0)))
	})
	jsonObj := ctx.NewObject()
	set(jsonObj, "clone", func(c *quickjs.Context, _ *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) == 0 || args[0].IsUndefined() {
			return throw(c, errJSONCloneUndefined)
		}
		var v any
		if err := json.Unmarshal([]byte(q.stringify(args[0])), &v); err != nil {
			return throw(c, err)
		}
		cloned, err := h.JSONClone(v)
		if err != nil {
			return throw(c, err)
		}
		encoded, _ := json.Marshal(cloned)
		return c.ParseJSON(string(encoded))
	})
	utils.Set("json", jsonObj)
	set(utils, "unixNow", func(c *quickjs.Context, _ *quickjs.Value, _ []*quickjs.Value) *quickjs.Value {
		return c.NewInt64(h.UnixNow)
	})
	set(utils, "jwtSignHS256", func(c *quickjs.Context, _ *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		var claims map[string]any
		if len(args) > 0 {
			_ = json.Unmarshal([]byte(q.stringify(args[0])), &claims)
		}
		token, err := h.JWTSignHS256(claims, str(args, 1))
		if err != nil {
			return throw(c, err)
		}
		return c.NewString(token)
	})
	set(utils, "hmacSHA256", func(c *quickjs.Context, _ *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		return c.NewString(h.HmacSHA256(str(args, 0), str(args, 1)))
	})
	set(utils, "base64", func(c *quickjs.Context, _ *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		return c.NewString(h.Base64(str(args, 0)))
	})
	set(utils, "base64URL", func(c *quickjs.Context, _ *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		return c.NewString(h.Base64URL(str(args, 0)))
	})
	set(utils, "base64URLDecode", func(c *quickjs.Context, _ *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		decoded, err := h.Base64URLDecode(str(args, 0))
		if err != nil {
			return throw(c, err)
		}
		return c.NewString(decoded)
	})
	set(utils, "uuid", func(c *quickjs.Context, _ *quickjs.Value, _ []*quickjs.Value) *quickjs.Value {
		return c.NewString(h.UUID)
	})
	set(utils, "volcSignV4", func(c *quickjs.Context, _ *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		var req map[string]any
		if len(args) > 0 {
			_ = json.Unmarshal([]byte(q.stringify(args[0])), &req)
		}
		signed, err := h.VolcSignV4(req)
		if err != nil {
			return throw(c, err)
		}
		encoded, _ := json.Marshal(signed)
		return c.ParseJSON(string(encoded))
	})
	q.globals.Set("utils", utils)
	console := ctx.NewObject()
	set(console, "log", func(c *quickjs.Context, _ *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		for _, a := range args {
			_ = a.String()
		}
		return c.NewUndefined()
	})
	q.globals.Set("console", console)
	return q, nil
}

// QuickJS exposes the runtime for footprint measurements.
func (q *QuickJSRuntime) QuickJS() *quickjs.Runtime { return q.rt }

func (q *QuickJSRuntime) Instantiate(module CompiledModule) error {
	m, ok := module.(*scriptModule)
	if !ok {
		return fmt.Errorf("quickjs: module compiled by another engine (%T)", module)
	}
	var res *quickjs.Value
	if m.bytecode != nil {
		res = q.ctx.EvalBytecode(m.bytecode)
	} else {
		res = q.ctx.Eval(m.script, quickjs.EvalFileName(m.name), quickjs.EvalFlagStrict(true))
	}
	defer res.Free()
	if res.IsException() {
		return wrapQuickJSError(q.ctx.Exception())
	}
	return nil
}

func (q *QuickJSRuntime) resolve(export string, path []string) (*quickjs.Value, error) {
	v := q.globals.Get(export)
	if v.IsUndefined() || v.IsNull() {
		v.Free()
		return nil, ErrNotFound
	}
	for _, member := range path {
		next := v.Get(member)
		v.Free()
		if next.IsUndefined() || next.IsNull() {
			next.Free()
			return nil, ErrNotFound
		}
		v = next
	}
	if !v.IsFunction() {
		v.Free()
		return nil, ErrNotFound
	}
	return v, nil
}

func (q *QuickJSRuntime) Call(export string, path []string, args ...any) (any, error) {
	fn, err := q.resolve(export, path)
	if err != nil {
		return nil, err
	}
	defer fn.Free()
	jsArgs := make([]*quickjs.Value, len(args))
	for i, a := range args {
		encoded, err := json.Marshal(a)
		if err != nil {
			return nil, err
		}
		jsArgs[i] = q.ctx.ParseJSON(string(encoded))
		defer jsArgs[i].Free()
	}
	res := fn.Execute(q.ctx.NewUndefined(), jsArgs...)
	defer res.Free()
	if res.IsException() {
		return nil, wrapQuickJSError(q.ctx.Exception())
	}
	if res.IsUndefined() {
		return nil, nil
	}
	var out any
	if err := json.Unmarshal([]byte(q.stringify(res)), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// stringify returns JSON.stringify(v). quickjs-go v0.7.7's
// Value.JSONStringify never frees the string JS_JSONStringify returns, so the
// default engine leaks one string per call, as code using that method does;
// the tuned engine calls JSON.stringify and frees the result.
func (q *QuickJSRuntime) stringify(v *quickjs.Value) string {
	if q.jsonStringify == nil {
		return v.JSONStringify()
	}
	s := q.jsonStringify.Execute(q.ctx.NewUndefined(), v)
	defer s.Free()
	return s.String()
}

func (q *QuickJSRuntime) Close() {
	if q.jsonStringify != nil {
		q.jsonStringify.Free()
	}
	q.globals.Free()
	q.ctx.Close()
	q.rt.Close()
}

func wrapQuickJSError(err error) error {
	var qe *quickjs.Error
	if errors.As(err, &qe) {
		msg := qe.Message
		if qe.Name == "" && qe.Message == "" {
			msg = strings.TrimSpace(qe.Cause)
		}
		return &HookError{Name: qe.Name, Message: msg}
	}
	if err == nil {
		return &HookError{Message: "exception"}
	}
	return &HookError{Message: err.Error()}
}
