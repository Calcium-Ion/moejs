package moejs

import (
	"errors"
	"slices"
	"strings"

	"github.com/Calcium-Ion/moejs/bytecode"
	"github.com/Calcium-Ion/moejs/compiler"
	"github.com/Calcium-Ion/moejs/engine"
	"github.com/Calcium-Ion/moejs/syntax"
)

// Module is a compiled ES module. It is immutable: any number of runtimes
// may load it, concurrently.
type Module struct {
	name    string
	code    *bytecode.Function
	exports []string // sorted
}

// Compile parses and compiles an ES module. Bad source, including features
// the engine does not support yet, is a *SyntaxError. Modules cannot import.
func Compile(name, source string) (*Module, error) {
	mod, err := syntax.ParseModule(name, source, syntax.Options{})
	if err != nil {
		return nil, syntaxError(err)
	}
	code, err := compiler.CompileModule(mod)
	if err != nil {
		return nil, syntaxError(err)
	}
	exports := make([]string, 0, len(code.Exports))
	for n := range code.Exports {
		exports = append(exports, n)
	}
	slices.Sort(exports)
	return &Module{name: name, code: code, exports: exports}, nil
}

func syntaxError(err error) error {
	var se *syntax.Error
	if errors.As(err, &se) {
		return &SyntaxError{File: se.Name, Line: se.Line, Column: se.Col, Message: se.Message()}
	}
	var ce *compiler.Error
	if errors.As(err, &ce) {
		return &SyntaxError{File: ce.Name, Line: ce.Line, Column: ce.Col, Message: strings.TrimPrefix(ce.Msg, "SyntaxError: ")}
	}
	return err
}

// Name returns the name given to Compile.
func (m *Module) Name() string { return m.name }

// Exports returns the module's export names, sorted.
func (m *Module) Exports() []string { return slices.Clone(m.exports) }

// Hook names an exported function, or a function below an exported object
// reached through own properties (members "openai", "decodeRequest" of
// export "protocols"). The export slot and the property keys are resolved
// here, once; the bindings stay live, so each Call reads the export and
// walks the members in the runtime at hand. An export the module does not
// declare is ErrHookNotFound.
func (m *Module) Hook(export string, members ...string) (Hook, error) {
	slot, ok := m.code.Exports[export]
	if !ok {
		return Hook{}, ErrHookNotFound
	}
	h := Hook{mod: m, slot: slot, name: export}
	if len(members) != 0 {
		h.keys = make([]engine.PropertyKey, len(members))
		for i, s := range members {
			h.keys[i] = engine.InternKey(s)
		}
		h.name = export + "." + strings.Join(members, ".")
	}
	return h, nil
}

// Hook is a resolved path to a function in a module (Module.Hook). The zero
// Hook names nothing.
type Hook struct {
	mod  *Module
	slot int
	keys []engine.PropertyKey
	name string
}

// Name returns the path joined with dots ("protocols.openai.decodeRequest").
func (h Hook) Name() string { return h.name }
