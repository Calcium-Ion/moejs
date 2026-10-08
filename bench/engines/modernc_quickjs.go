package engines

import (
	"errors"
	"fmt"
	"strings"

	quickjs "modernc.org/quickjs"
)

// ModerncQuickJSEngine drives QuickJS through modernc.org/quickjs, a pure-Go
// (CGO_ENABLED=0) transpilation of the C QuickJS library via the modernc
// toolchain. Like the cgo quickjs-go adapter, contexts cannot share compiled
// code, so Compile rewrites ESM exports to globals and Instantiate evaluates
// the script per VM. Complex values cross the boundary through the engine's
// built-in JSON-based marshalling (Go marshal ↔ JS value), which is
// functionally equivalent to the explicit JSON round-trip in quickjs-go.
type ModerncQuickJSEngine struct{ host Host }

func NewModerncQuickJSEngine() *ModerncQuickJSEngine {
	return &ModerncQuickJSEngine{host: DefaultHost}
}

func (e *ModerncQuickJSEngine) Name() string { return "modernc-quickjs" }

func (e *ModerncQuickJSEngine) Compile(name, source string) (CompiledModule, error) {
	script, err := ESMToGlobals(source)
	if err != nil {
		return nil, err
	}
	return &scriptModule{name: name, script: script}, nil
}

type ModerncQuickJSRuntime struct {
	vm *quickjs.VM
}

func (e *ModerncQuickJSEngine) NewRuntime() (Runtime, error) {
	vm, err := quickjs.NewVM()
	if err != nil {
		return nil, err
	}
	h := e.host
	str := func(args []any, i int) string {
		if i < len(args) {
			if s, ok := args[i].(string); ok {
				return s
			}
		}
		return ""
	}
	objArg := func(args []any, i int) map[string]any {
		if i >= len(args) {
			return nil
		}
		switch a := args[i].(type) {
		case *quickjs.Object:
			var v map[string]any
			_ = a.Into(&v)
			return v
		case map[string]any:
			return a
		}
		return nil
	}

	_ = vm.RegisterHostFunc("__hasCapability", func(args []any) (any, error) {
		return h.HasCapability(str(args, 0)), nil
	})
	_ = vm.RegisterHostFunc("__jsonClone", func(args []any) (any, error) {
		if len(args) == 0 {
			return nil, errJSONCloneUndefined
		}
		if _, ok := args[0].(quickjs.Undefined); ok {
			return nil, errJSONCloneUndefined
		}
		var v any
		switch a := args[0].(type) {
		case *quickjs.Object:
			if err := a.Into(&v); err != nil {
				return nil, err
			}
		default:
			v = a
		}
		return h.JSONClone(v)
	})
	_ = vm.RegisterHostFunc("__unixNow", func(args []any) (any, error) {
		return h.UnixNow, nil
	})
	_ = vm.RegisterHostFunc("__jwtSignHS256", func(args []any) (any, error) {
		return h.JWTSignHS256(objArg(args, 0), str(args, 1))
	})
	_ = vm.RegisterHostFunc("__hmacSHA256", func(args []any) (any, error) {
		return h.HmacSHA256(str(args, 0), str(args, 1)), nil
	})
	_ = vm.RegisterHostFunc("__base64", func(args []any) (any, error) {
		return h.Base64(str(args, 0)), nil
	})
	_ = vm.RegisterHostFunc("__base64URL", func(args []any) (any, error) {
		return h.Base64URL(str(args, 0)), nil
	})
	_ = vm.RegisterHostFunc("__base64URLDecode", func(args []any) (any, error) {
		return h.Base64URLDecode(str(args, 0))
	})
	_ = vm.RegisterHostFunc("__uuid", func(args []any) (any, error) {
		return h.UUID, nil
	})
	_ = vm.RegisterHostFunc("__volcSignV4", func(args []any) (any, error) {
		signed, err := h.VolcSignV4(objArg(args, 0))
		if err != nil {
			return nil, err
		}
		return signed, nil
	})
	_ = vm.RegisterHostFunc("__consoleLog", func(args []any) (any, error) {
		return nil, nil
	})

	if _, err = vm.Eval(`
		globalThis.utils = {
			hasCapability: __hasCapability,
			json: { clone: __jsonClone },
			unixNow: __unixNow,
			jwtSignHS256: __jwtSignHS256,
			hmacSHA256: __hmacSHA256,
			base64: __base64,
			base64URL: __base64URL,
			base64URLDecode: __base64URLDecode,
			uuid: __uuid,
			volcSignV4: __volcSignV4,
		};
		globalThis.console = { log: __consoleLog };
	`, quickjs.EvalGlobal); err != nil {
		vm.Close()
		return nil, err
	}
	return &ModerncQuickJSRuntime{vm: vm}, nil
}

func (r *ModerncQuickJSRuntime) VM() *quickjs.VM { return r.vm }

func (r *ModerncQuickJSRuntime) Instantiate(module CompiledModule) error {
	m, ok := module.(*scriptModule)
	if !ok {
		return fmt.Errorf("modernc-quickjs: module compiled by another engine (%T)", module)
	}
	if _, err := r.vm.Eval(m.script, quickjs.EvalGlobal); err != nil {
		return wrapModerncError(err)
	}
	return nil
}

func (r *ModerncQuickJSRuntime) resolveModernc(export string, path []string) (quickjs.Value, error) {
	global := r.vm.GlobalObject()

	atom, err := r.vm.NewAtom(export)
	if err != nil {
		global.Free()
		return quickjs.UndefinedValue, ErrNotFound
	}
	v, err := global.GetPropertyValue(atom)
	global.Free()
	if err != nil {
		return quickjs.UndefinedValue, ErrNotFound
	}
	if v.IsUndefined() {
		v.Free()
		return quickjs.UndefinedValue, ErrNotFound
	}

	for _, member := range path {
		a, err := r.vm.NewAtom(member)
		if err != nil {
			v.Free()
			return quickjs.UndefinedValue, ErrNotFound
		}
		next, err := v.GetPropertyValue(a)
		v.Free()
		if err != nil {
			return quickjs.UndefinedValue, ErrNotFound
		}
		if next.IsUndefined() {
			next.Free()
			return quickjs.UndefinedValue, ErrNotFound
		}
		v = next
	}
	return v, nil
}

func (r *ModerncQuickJSRuntime) Call(export string, path []string, args ...any) (any, error) {
	fn, err := r.resolveModernc(export, path)
	if err != nil {
		return nil, err
	}
	defer fn.Free()
	result, err := fn.Call(quickjs.UndefinedValue, args...)
	if err != nil {
		if strings.Contains(err.Error(), "non-function") || strings.Contains(err.Error(), "not a function") {
			return nil, ErrNotFound
		}
		return nil, wrapModerncError(err)
	}
	return exportModerncResult(result), nil
}

func (r *ModerncQuickJSRuntime) Close() {
	r.vm.Close()
}

func exportModerncResult(v any) any {
	switch r := v.(type) {
	case *quickjs.Object:
		var out any
		_ = r.Into(&out)
		return out
	case quickjs.Undefined:
		return nil
	default:
		return v
	}
}

func wrapModerncError(err error) error {
	var qe *quickjs.Error
	if errors.As(err, &qe) {
		return &HookError{Name: qe.Name, Message: qe.Message}
	}
	if err == nil {
		return &HookError{Message: "exception"}
	}
	return &HookError{Message: err.Error()}
}
