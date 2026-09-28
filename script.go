package moejs

import (
	"github.com/Calcium-Ion/moejs/bytecode"
	"github.com/Calcium-Ion/moejs/compiler"
	"github.com/Calcium-Ion/moejs/engine"
	"github.com/Calcium-Ion/moejs/syntax"
)

// The engine compiles the source text of eval, the Function constructors
// and $262-style script evaluation through the compiler this package
// installs.
func init() { engine.SetCompiler(compiler.Hook{}) }

// Script is a compiled classic script. It is immutable: any number of
// runtimes may run it, concurrently, and a runtime may run it more than
// once.
type Script struct {
	name string
	code *bytecode.Function
}

// CompileScript parses and compiles a classic script. A script is sloppy
// mode code unless it starts with a "use strict" directive. Bad source,
// including features the engine does not support yet, is a *SyntaxError.
func CompileScript(name, source string) (*Script, error) {
	s, err := syntax.ParseScript(name, source, syntax.Options{})
	if err != nil {
		return nil, syntaxError(err)
	}
	code, err := compiler.CompileScript(s)
	if err != nil {
		return nil, syntaxError(err)
	}
	return &Script{name: name, code: code}, nil
}

// Name returns the name given to CompileScript.
func (s *Script) Name() string { return s.name }

func (*Script) referrer() {}

// RunScript runs s in the runtime's global environment and returns its
// completion value. Its var and function declarations become properties
// of the global object; its let, const and class declarations are global
// bindings that later scripts and the loaded module (before or after Load)
// see too. A declaration that conflicts with an existing global binding
// throws before anything runs. The jobs the script queued run before
// RunScript returns. A throw is an *Exception, an interrupt an
// *InterruptedError.
func (rt *Runtime) RunScript(s *Script) (res Value, err error) {
	r := rt.realm
	defer func() {
		if err != nil {
			res = engine.Undefined()
		}
	}()
	defer rt.guard(&err, r.CallState(), len(rt.argStack))
	r.HoldJobs()
	if err = r.CheckInterrupt(); err == nil {
		if s.code.ScriptOrModule {
			// The referrer of its import() calls.
			r.SetHostDefined(s.code, s)
		}
		res, err = r.RunScript(s.code)
	}
	return res, r.ReleaseJobs(err)
}
