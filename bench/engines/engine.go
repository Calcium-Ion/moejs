// Package engines drives the new-api task plugins through four JavaScript
// engines behind one interface so that the benchmark and differential-test
// harness in the parent package can treat them uniformly.
//
// Every adapter mirrors how new-api's pkg/jsplugin uses its engine: a runtime
// is created with the `utils` and `console` host globals, the plugin module is
// compiled once (where the engine allows it) and instantiated per runtime, and
// a hook is called with host-shaped JSON values as arguments and its result
// exported back to plain Go values. The cgo engines receive and return JSON
// text, which is how their users move JSON-shaped data in practice.
package engines

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Engine creates runtimes and compiles plugin modules.
type Engine interface {
	Name() string
	// Compile parses the plugin source once. Engines that cannot share
	// compiled code across contexts return a source-carrying module and pay
	// the compile cost in Runtime.Instantiate (reported honestly as such).
	Compile(name, source string) (CompiledModule, error)
	NewRuntime() (Runtime, error)
}

// Runtime is one JavaScript context with the host globals installed. It is
// single-goroutine, exactly like the engines it wraps.
type Runtime interface {
	Instantiate(module CompiledModule) error
	// Call invokes the export named export, or the function found by walking
	// path below it (protocols.openai_responses.decodeRequest is export
	// "protocols" with path ["openai_responses", "decodeRequest"]). Arguments
	// are JSON-shaped Go values (map[string]any, []any, string, float64, bool,
	// nil); the result is the exported Go value in the same shape. The
	// pure-Go adapters export a returned promise as {state, value} (see
	// settledPromise).
	Call(export string, path []string, args ...any) (any, error)
	Close()
}

// CompiledModule is an engine-specific compiled (or merely parsed) plugin.
type CompiledModule interface {
	ModuleName() string
}

// Phased is implemented by adapters that can split a hook call into its
// three phases so the cost of host conversion can be attributed separately
// from interpretation: FromGo(args) -> Call -> ToGo(result).
type Phased interface {
	// Prepare converts Go arguments into engine values.
	Prepare(args ...any) (PreparedArgs, error)
	// CallPrepared runs the hook on already converted arguments and returns
	// the raw engine result without exporting it.
	CallPrepared(export string, path []string, args PreparedArgs) (RawResult, error)
	// Export converts a raw engine result to a Go value.
	Export(raw RawResult) any
}

// PreparedArgs and RawResult are opaque engine values.
type (
	PreparedArgs any
	RawResult    any
)

// settledPromise is how Call exports a promise the hook returned: state is
// "pending", "fulfilled" or "rejected" and value the exported result, nil
// while pending. Sobek and moejs drain the job queue when the outermost call
// returns, so the promise is as settled as it will get.
func settledPromise(state string, value any) map[string]any {
	return map[string]any{"state": state, "value": value}
}

// HookError is a JavaScript exception thrown by a plugin hook, normalized
// across engines so the differential tests can compare messages.
type HookError struct {
	Name    string // constructor name when known ("Error", "TypeError", ...)
	Message string // the `message` property, or the string form of the value
}

func (e *HookError) Error() string {
	if e.Name == "" {
		return e.Message
	}
	return e.Name + ": " + e.Message
}

// AsHookError extracts a *HookError from err.
func AsHookError(err error) (*HookError, bool) {
	var he *HookError
	ok := errors.As(err, &he)
	return he, ok
}

// ErrNotFound is returned when the export or a member of the path does not
// exist or is not callable, mirroring the host's "plugin hook %q not found".
var ErrNotFound = errors.New("plugin hook not found")

// HookName joins an export and a member path the way new-api names hooks.
func HookName(export string, path []string) string {
	if len(path) == 0 {
		return export
	}
	return export + "." + strings.Join(path, ".")
}

// exportPattern matches the ESM export forms the plugin corpus uses.
var exportPattern = regexp.MustCompile(`(?m)^export\s+(function\s+([A-Za-z_$][\w$]*)|(?:const|let|var)\s+([A-Za-z_$][\w$]*))`)

// ESMToGlobals rewrites `export function f` and `export const x` at the top
// level into plain declarations followed by `globalThis.<name> = <name>`
// assignments, so engines that run scripts rather than modules (quickjs-go,
// v8go) can execute the plugin unchanged otherwise. The plugin corpus has no
// `export { ... }` lists and no `export default`.
func ESMToGlobals(source string) (string, error) {
	var names []string
	rewritten := exportPattern.ReplaceAllStringFunc(source, func(m string) string {
		sub := exportPattern.FindStringSubmatch(m)
		name := sub[2]
		if name == "" {
			name = sub[3]
		}
		names = append(names, name)
		return strings.TrimPrefix(m, "export ")
	})
	if strings.Contains(rewritten, "\nexport ") || strings.HasPrefix(rewritten, "export ") {
		return "", fmt.Errorf("engines: unsupported export form in module")
	}
	var b strings.Builder
	b.Grow(len(rewritten) + 32*len(names))
	b.WriteString(rewritten)
	b.WriteString("\n")
	for _, n := range names {
		b.WriteString("globalThis.")
		b.WriteString(n)
		b.WriteString(" = ")
		b.WriteString(n)
		b.WriteString(";\n")
	}
	return b.String(), nil
}
