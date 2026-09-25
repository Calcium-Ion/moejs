package engines

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	quickjs "github.com/buke/quickjs-go"
)

// QuickJSEngine drives QuickJS through buke/quickjs-go (cgo). QuickJS
// contexts cannot share compiled code, so Compile only rewrites the ESM
// exports to globals and Instantiate evaluates the script per context.
// Arguments cross the boundary as JSON text (Go marshal -> JS parse) and
// results come back the same way, which is how its users move JSON data.
type QuickJSEngine struct{ host Host }

func NewQuickJSEngine() *QuickJSEngine { return &QuickJSEngine{host: DefaultHost} }

func (e *QuickJSEngine) Name() string { return "quickjs-go" }

type scriptModule struct {
	name   string
	script string
}

func (m *scriptModule) ModuleName() string { return m.name }

func (e *QuickJSEngine) Compile(name, source string) (CompiledModule, error) {
	script, err := ESMToGlobals(source)
	if err != nil {
		return nil, err
	}
	return &scriptModule{name: name, script: script}, nil
}

type QuickJSRuntime struct {
	rt      *quickjs.Runtime
	ctx     *quickjs.Context
	globals *quickjs.Value
	keep    []*quickjs.Value // host function values kept alive for Close
}

func (e *QuickJSEngine) NewRuntime() (Runtime, error) {
	rt := quickjs.NewRuntime()
	ctx := rt.NewContext()
	q := &QuickJSRuntime{rt: rt, ctx: ctx, globals: ctx.Globals()}
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
		if err := json.Unmarshal([]byte(args[0].JSONStringify()), &v); err != nil {
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
			_ = json.Unmarshal([]byte(args[0].JSONStringify()), &claims)
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
			_ = json.Unmarshal([]byte(args[0].JSONStringify()), &req)
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
	res := q.ctx.Eval(m.script, quickjs.EvalFileName(m.name), quickjs.EvalFlagStrict(true))
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
	if err := json.Unmarshal([]byte(res.JSONStringify()), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (q *QuickJSRuntime) Close() {
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
