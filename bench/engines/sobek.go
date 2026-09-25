package engines

import (
	"errors"
	"fmt"
	"slices"

	"github.com/grafana/sobek"
	"github.com/grafana/sobek/parser"
)

// SobekEngine drives grafana/sobek exactly as pkg/jsplugin does: ParseModule
// + Link once, CyclicModuleRecordEvaluate per runtime, AssertFunction +
// ToValue + Export per call.
type SobekEngine struct{ host Host }

func NewSobekEngine() *SobekEngine { return &SobekEngine{host: DefaultHost} }

func (e *SobekEngine) Name() string { return "sobek" }

type sobekModule struct {
	name   string
	record *sobek.SourceTextModuleRecord
}

func (m *sobekModule) ModuleName() string { return m.name }

func noImports(any, string) (sobek.ModuleRecord, error) {
	return nil, errors.New("plugin imports are disabled")
}

func (e *SobekEngine) Compile(name, source string) (CompiledModule, error) {
	record, err := sobek.ParseModule(name, source, noImports, parser.WithDisableSourceMaps)
	if err != nil {
		return nil, err
	}
	if err := record.Link(); err != nil {
		return nil, err
	}
	return &sobekModule{name: name, record: record}, nil
}

type SobekRuntime struct {
	rt       *sobek.Runtime
	instance sobek.ModuleInstance
}

func (e *SobekEngine) NewRuntime() (Runtime, error) {
	rt := sobek.New()
	h := e.host
	utils := map[string]any{
		"hasCapability": h.HasCapability,
		"json": map[string]any{
			"clone": func(call sobek.FunctionCall) sobek.Value {
				if sobek.IsUndefined(call.Argument(0)) {
					panic(rt.NewTypeError("%s", errJSONCloneUndefined.Error()))
				}
				cloned, err := h.JSONClone(call.Argument(0).Export())
				if err != nil {
					panic(rt.NewTypeError("%s", err.Error()))
				}
				return sobekJSONValue(rt, cloned)
			},
		},
		"unixNow":         func() int64 { return h.UnixNow },
		"jwtSignHS256":    h.JWTSignHS256,
		"hmacSHA256":      h.HmacSHA256,
		"base64":          h.Base64,
		"base64URL":       h.Base64URL,
		"base64URLDecode": h.Base64URLDecode,
		"uuid":            func() string { return h.UUID },
		"volcSignV4":      h.VolcSignV4,
	}
	if err := rt.Set("utils", utils); err != nil {
		return nil, err
	}
	console := rt.NewObject()
	if err := console.Set("log", func(call sobek.FunctionCall) sobek.Value {
		for _, a := range call.Arguments {
			_ = a.String()
		}
		return sobek.Undefined()
	}); err != nil {
		return nil, err
	}
	if err := rt.Set("console", console); err != nil {
		return nil, err
	}
	return &SobekRuntime{rt: rt}, nil
}

// Sobek exposes the runtime for footprint measurements.
func (rt *SobekRuntime) Sobek() *sobek.Runtime { return rt.rt }

func (rt *SobekRuntime) Instantiate(module CompiledModule) (err error) {
	m, ok := module.(*sobekModule)
	if !ok {
		return fmt.Errorf("sobek: module compiled by another engine (%T)", module)
	}
	defer func() {
		if rec := recover(); rec != nil {
			err = sobekRecovered(rec)
		}
	}()
	p := rt.rt.CyclicModuleRecordEvaluate(m.record, noImports)
	if p.State() != sobek.PromiseStateFulfilled {
		return fmt.Errorf("evaluate plugin: %v", p.Result().Export())
	}
	rt.instance = rt.rt.GetModuleInstance(m.record)
	return nil
}

func (rt *SobekRuntime) resolve(export string, path []string) (sobek.Callable, error) {
	if rt.instance == nil {
		return nil, errors.New("sobek: module not instantiated")
	}
	value := rt.instance.GetBindingValue(export)
	if value == nil || sobek.IsUndefined(value) || sobek.IsNull(value) {
		return nil, ErrNotFound
	}
	for _, member := range path {
		object := value.ToObject(rt.rt)
		if !slices.Contains(object.GetOwnPropertyNames(), member) {
			return nil, ErrNotFound
		}
		value = object.Get(member)
		if value == nil || sobek.IsUndefined(value) || sobek.IsNull(value) {
			return nil, ErrNotFound
		}
	}
	fn, ok := sobek.AssertFunction(value)
	if !ok {
		return nil, ErrNotFound
	}
	return fn, nil
}

func (rt *SobekRuntime) Call(export string, path []string, args ...any) (any, error) {
	prepared, err := rt.Prepare(args...)
	if err != nil {
		return nil, err
	}
	raw, err := rt.CallPrepared(export, path, prepared)
	if err != nil {
		return nil, err
	}
	out := rt.Export(raw)
	if p, ok := out.(*sobek.Promise); ok {
		return sobekPromise(p), nil
	}
	return out, nil
}

// sobekPromise exports p as {state, value} (see settledPromise).
func sobekPromise(p *sobek.Promise) map[string]any {
	switch p.State() {
	case sobek.PromiseStateFulfilled:
		return settledPromise("fulfilled", p.Result().Export())
	case sobek.PromiseStateRejected:
		return settledPromise("rejected", p.Result().Export())
	}
	return settledPromise("pending", nil)
}

func (rt *SobekRuntime) Prepare(args ...any) (PreparedArgs, error) {
	vals := make([]sobek.Value, len(args))
	for i, a := range args {
		vals[i] = rt.rt.ToValue(a)
	}
	return vals, nil
}

func (rt *SobekRuntime) CallPrepared(export string, path []string, args PreparedArgs) (raw RawResult, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			raw, err = nil, sobekRecovered(rec)
		}
	}()
	fn, err := rt.resolve(export, path)
	if err != nil {
		return nil, err
	}
	out, err := fn(sobek.Undefined(), args.([]sobek.Value)...)
	if err != nil {
		return nil, wrapSobekError(err)
	}
	return out, nil
}

func (rt *SobekRuntime) Export(raw RawResult) any { return raw.(sobek.Value).Export() }

func (rt *SobekRuntime) Close() {}

func sobekRecovered(rec any) error {
	switch v := rec.(type) {
	case *sobek.Exception:
		return wrapSobekError(v)
	case *sobek.InterruptedError:
		return v
	case error:
		return v
	}
	return fmt.Errorf("sobek panic: %v", rec)
}

func wrapSobekError(err error) error {
	var exc *sobek.Exception
	if !errors.As(err, &exc) {
		return err
	}
	he := &HookError{}
	val := exc.Value()
	if val == nil || sobek.IsUndefined(val) || sobek.IsNull(val) {
		return he
	}
	if obj, ok := val.(*sobek.Object); ok {
		if name := obj.Get("name"); name != nil && !sobek.IsUndefined(name) {
			he.Name = name.String()
		}
		if msg := obj.Get("message"); msg != nil && !sobek.IsUndefined(msg) && !sobek.IsNull(msg) {
			he.Message = msg.String()
			return he
		}
	}
	if s, ok := val.Export().(string); ok {
		he.Message = s
		return he
	}
	he.Message = val.String()
	return he
}

// sobekJSONValue builds genuine JavaScript objects and arrays from a
// JSON-shaped Go value, as new-api's json.clone does through its jsValue
// builder. rt.ToValue would wrap []any and map[string]any as live Go proxies,
// whose Array.prototype.push / index semantics differ from real arrays.
func sobekJSONValue(rt *sobek.Runtime, v any) sobek.Value {
	switch t := v.(type) {
	case map[string]any:
		obj := rt.NewObject()
		for k, item := range t {
			_ = obj.Set(k, sobekJSONValue(rt, item))
		}
		return obj
	case []any:
		items := make([]any, len(t))
		for i, item := range t {
			items[i] = sobekJSONValue(rt, item)
		}
		return rt.NewArray(items...)
	}
	return rt.ToValue(v)
}
