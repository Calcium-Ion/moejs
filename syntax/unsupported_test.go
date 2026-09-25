package syntax

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUnsupportedFeatures asserts the exact diagnostic for every deferred
// language feature, and that Options.AllowUnsupported lifts the rejection
// without touching the parser.
func TestUnsupportedFeatures(t *testing.T) {
	tests := []struct {
		name      string
		src       string
		line, col int
		msg       string
	}{
		{"import", "import x from \"y\";", 1, 1, "SyntaxError: import is not supported yet (see TODO.md)"},
		{"import-named", "import { a } from \"y\";", 1, 1, "SyntaxError: import is not supported yet (see TODO.md)"},
		{"import-side-effect", "import \"y\";", 1, 1, "SyntaxError: import is not supported yet (see TODO.md)"},
		{"dynamic-import", "import(\"y\");", 1, 1, "SyntaxError: dynamic import() is not supported yet (see TODO.md)"},
		{"import-meta", "import.meta;", 1, 1, "SyntaxError: import.meta is not supported yet (see TODO.md)"},
		{"export-all", "export * from \"y\";", 1, 1, "SyntaxError: export ... from is not supported yet (see TODO.md)"},
		{"export-from", "export { x } from \"y\";", 1, 1, "SyntaxError: export ... from is not supported yet (see TODO.md)"},
		{"direct-eval", "eval(\"x\");", 1, 1, "SyntaxError: direct eval is not supported yet (see TODO.md)"},
		{"direct-eval-in-function", "function f() { return eval(\"x\"); }", 1, 23, "SyntaxError: direct eval is not supported yet (see TODO.md)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseModule("t.js", tt.src, Options{})
			require.Error(t, err)
			var se *Error
			require.ErrorAs(t, err, &se)
			assert.Equal(t, tt.msg, se.Msg)
			assert.Equal(t, [2]int{tt.line, tt.col}, [2]int{se.Line, se.Col}, "position")

			_, err = ParseModule("t.js", tt.src, Options{AllowUnsupported: true})
			assert.NoError(t, err, "AllowUnsupported must accept the syntax")
		})
	}
}

// TestUnsupportedAnnotations checks the function flags that drive the
// arguments/eval rejections when the restriction is lifted.
func TestUnsupportedAnnotations(t *testing.T) {
	m, err := ParseModule("t.js", "function f() { const g = () => arguments; return eval(\"x\"); } const h = () => arguments; eval; x.eval();", Options{AllowUnsupported: true})
	require.NoError(t, err)
	f := m.Body[0].(*FuncDecl).Func
	g := f.Body.Body[0].(*VarDecl).Decls[0].Init.(*Function)
	assert.True(t, f.UsesArguments)
	assert.True(t, g.UsesArguments)
	assert.True(t, f.HasDirectEval)
	assert.False(t, g.HasDirectEval)

	// A module-level arrow has no arguments object to use; the reference
	// is an ordinary (unresolved) global.
	h := m.Body[1].(*VarDecl).Decls[0].Init.(*Function)
	assert.False(t, h.UsesArguments)
	assert.Nil(t, h.ExprBody.(*Ident).Binding)

	// Only a call to the unresolved name `eval` is direct eval.
	m, err = ParseModule("t.js", "function f() { eval; return x.eval(1) + eval.length; }", Options{})
	require.NoError(t, err)
	assert.False(t, m.Body[0].(*FuncDecl).Func.HasDirectEval)
}

func TestUnsupportedOrder(t *testing.T) {
	// The first unsupported construct in source order is reported even when
	// a later one nests inside.
	_, err := ParseModule("t.js", "eval(import(\"y\"));", Options{})
	require.Error(t, err)
	assert.Equal(t, "t.js:1:1: SyntaxError: direct eval is not supported yet (see TODO.md)", err.Error())

	_, err = ParseModule("t.js", "function f() { import.meta; eval(\"x\"); }", Options{})
	require.Error(t, err)
	assert.Equal(t, "t.js:1:16: SyntaxError: import.meta is not supported yet (see TODO.md)", err.Error())
}
