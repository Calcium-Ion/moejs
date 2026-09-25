package engines

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	v8 "rogchap.com/v8go"
)

// V8Engine drives V8 through rogchap.com/v8go (cgo). Isolates cannot share
// compiled scripts, so the plugin is compiled per context in Instantiate.
// Values cross the boundary as JSON text.
type V8Engine struct{ host Host }

func NewV8Engine() *V8Engine { return &V8Engine{host: DefaultHost} }

func (e *V8Engine) Name() string { return "v8go" }

func (e *V8Engine) Compile(name, source string) (CompiledModule, error) {
	script, err := ESMToGlobals(source)
	if err != nil {
		return nil, err
	}
	return &scriptModule{name: name, script: script}, nil
}

type V8Runtime struct {
	iso *v8.Isolate
	ctx *v8.Context
}

func (e *V8Engine) NewRuntime() (Runtime, error) {
	iso := v8.NewIsolate()
	h := e.host
	str := func(info *v8.FunctionCallbackInfo, i int) string {
		args := info.Args()
		if i < len(args) {
			return args[i].String()
		}
		return ""
	}
	mustValue := func(iso *v8.Isolate, v any) *v8.Value {
		val, err := v8.NewValue(iso, v)
		if err != nil {
			panic(err)
		}
		return val
	}
	throw := func(iso *v8.Isolate, err error) *v8.Value {
		return iso.ThrowException(mustValue(iso, err.Error()))
	}
	jsonArg := func(info *v8.FunctionCallbackInfo, i int) any {
		args := info.Args()
		if i >= len(args) {
			return nil
		}
		s, err := v8.JSONStringify(info.Context(), args[i])
		if err != nil {
			return nil
		}
		var v any
		_ = json.Unmarshal([]byte(s), &v)
		return v
	}
	global := v8.NewObjectTemplate(iso)
	utils := v8.NewObjectTemplate(iso)
	fn := func(cb func(info *v8.FunctionCallbackInfo) *v8.Value) *v8.FunctionTemplate {
		return v8.NewFunctionTemplate(iso, cb)
	}
	must := func(err error) {
		if err != nil {
			panic(err)
		}
	}
	must(utils.Set("hasCapability", fn(func(info *v8.FunctionCallbackInfo) *v8.Value {
		return mustValue(iso, h.HasCapability(str(info, 0)))
	})))
	jsonTmpl := v8.NewObjectTemplate(iso)
	must(jsonTmpl.Set("clone", fn(func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		if len(args) == 0 || args[0].IsUndefined() {
			return throw(iso, errJSONCloneUndefined)
		}
		cloned, err := h.JSONClone(jsonArg(info, 0))
		if err != nil {
			return throw(iso, err)
		}
		encoded, _ := json.Marshal(cloned)
		val, err := v8.JSONParse(info.Context(), string(encoded))
		if err != nil {
			return throw(iso, err)
		}
		return val
	})))
	must(utils.Set("json", jsonTmpl))
	must(utils.Set("unixNow", fn(func(info *v8.FunctionCallbackInfo) *v8.Value {
		return mustValue(iso, float64(h.UnixNow))
	})))
	must(utils.Set("jwtSignHS256", fn(func(info *v8.FunctionCallbackInfo) *v8.Value {
		claims, _ := jsonArg(info, 0).(map[string]any)
		token, err := h.JWTSignHS256(claims, str(info, 1))
		if err != nil {
			return throw(iso, err)
		}
		return mustValue(iso, token)
	})))
	must(utils.Set("hmacSHA256", fn(func(info *v8.FunctionCallbackInfo) *v8.Value {
		return mustValue(iso, h.HmacSHA256(str(info, 0), str(info, 1)))
	})))
	must(utils.Set("base64", fn(func(info *v8.FunctionCallbackInfo) *v8.Value {
		return mustValue(iso, h.Base64(str(info, 0)))
	})))
	must(utils.Set("base64URL", fn(func(info *v8.FunctionCallbackInfo) *v8.Value {
		return mustValue(iso, h.Base64URL(str(info, 0)))
	})))
	must(utils.Set("base64URLDecode", fn(func(info *v8.FunctionCallbackInfo) *v8.Value {
		decoded, err := h.Base64URLDecode(str(info, 0))
		if err != nil {
			return throw(iso, err)
		}
		return mustValue(iso, decoded)
	})))
	must(utils.Set("uuid", fn(func(info *v8.FunctionCallbackInfo) *v8.Value {
		return mustValue(iso, h.UUID)
	})))
	must(utils.Set("volcSignV4", fn(func(info *v8.FunctionCallbackInfo) *v8.Value {
		req, _ := jsonArg(info, 0).(map[string]any)
		signed, err := h.VolcSignV4(req)
		if err != nil {
			return throw(iso, err)
		}
		encoded, _ := json.Marshal(signed)
		val, err := v8.JSONParse(info.Context(), string(encoded))
		if err != nil {
			return throw(iso, err)
		}
		return val
	})))
	must(global.Set("utils", utils))
	console := v8.NewObjectTemplate(iso)
	must(console.Set("log", fn(func(info *v8.FunctionCallbackInfo) *v8.Value {
		for _, a := range info.Args() {
			_ = a.String()
		}
		return v8.Undefined(iso)
	})))
	must(global.Set("console", console))
	ctx := v8.NewContext(iso, global)
	return &V8Runtime{iso: iso, ctx: ctx}, nil
}

// Isolate exposes the isolate for footprint measurements.
func (v *V8Runtime) Isolate() *v8.Isolate { return v.iso }

func (v *V8Runtime) Instantiate(module CompiledModule) error {
	m, ok := module.(*scriptModule)
	if !ok {
		return fmt.Errorf("v8go: module compiled by another engine (%T)", module)
	}
	if _, err := v.ctx.RunScript(m.script, m.name); err != nil {
		return wrapV8Error(err)
	}
	return nil
}

// resolve walks export.path on the global object. Every v8go value is a
// persistent handle that lives until the context closes unless released, so
// intermediate handles are released here and the returned function is
// released by the caller: a pooled isolate that never releases handles grows
// its heap with every call until V8 aborts the process.
func (v *V8Runtime) resolve(export string, path []string) (*v8.Function, error) {
	global := v.ctx.Global()
	defer global.Release()
	val, err := global.Get(export)
	if err != nil {
		return nil, err
	}
	if val.IsUndefined() || val.IsNull() {
		val.Release()
		return nil, ErrNotFound
	}
	for _, member := range path {
		if !val.IsObject() {
			val.Release()
			return nil, ErrNotFound
		}
		next, err := val.Object().Get(member)
		val.Release()
		if err != nil {
			return nil, err
		}
		if next.IsUndefined() || next.IsNull() {
			next.Release()
			return nil, ErrNotFound
		}
		val = next
	}
	fn, err := val.AsFunction()
	if err != nil {
		val.Release()
		return nil, ErrNotFound
	}
	return fn, nil
}

func (v *V8Runtime) Call(export string, path []string, args ...any) (any, error) {
	fn, err := v.resolve(export, path)
	if err != nil {
		return nil, err
	}
	defer fn.Release()
	jsArgs := make([]v8.Valuer, 0, len(args))
	defer func() {
		for _, a := range jsArgs {
			a.(*v8.Value).Release()
		}
	}()
	for _, a := range args {
		encoded, err := json.Marshal(a)
		if err != nil {
			return nil, err
		}
		val, err := v8.JSONParse(v.ctx, string(encoded))
		if err != nil {
			return nil, err
		}
		jsArgs = append(jsArgs, val)
	}
	res, err := fn.Call(v8.Undefined(v.iso), jsArgs...)
	if err != nil {
		return nil, wrapV8Error(err)
	}
	defer res.Release()
	if res.IsUndefined() {
		return nil, nil
	}
	s, err := v8.JSONStringify(v.ctx, res)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (v *V8Runtime) Close() {
	v.ctx.Close()
	v.iso.Dispose()
}

// wrapV8Error extracts the JS message from v8go's "Name: message" rendering.
func wrapV8Error(err error) error {
	var je *v8.JSError
	if !errors.As(err, &je) {
		return err
	}
	he := &HookError{Message: je.Message}
	if name, msg, ok := strings.Cut(je.Message, ": "); ok && !strings.ContainsAny(name, " \t") {
		he.Name, he.Message = name, msg
	}
	return he
}
