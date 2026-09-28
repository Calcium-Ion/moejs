package syntax

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUnsupportedModuleSyntax checks that the import forms of proposals
// moejs does not implement fail naming them, whatever the options, and that
// the imports they resemble parse.
func TestUnsupportedModuleSyntax(t *testing.T) {
	tests := []struct {
		src string
		msg string // "" parses
	}{
		{`import x from "m" with { type: "json" };`, `t.js:1:19: SyntaxError: import attribute syntax is not supported yet (see TODO.md)`},
		{`import "m" with {};`, `t.js:1:12: SyntaxError: import attribute syntax is not supported yet (see TODO.md)`},
		{`export { x } from "m" with { type: "json" };`, `t.js:1:23: SyntaxError: import attribute syntax is not supported yet (see TODO.md)`},
		{`export * from "m" with {};`, `t.js:1:19: SyntaxError: import attribute syntax is not supported yet (see TODO.md)`},
		{`x = import("m", { with: { type: "json" } });`, `t.js:1:17: SyntaxError: import attribute syntax is not supported yet (see TODO.md)`},
		{`x = import("m", {},);`, `t.js:1:17: SyntaxError: import attribute syntax is not supported yet (see TODO.md)`},
		{`x = import("m",);`, ""},
		{`x = import.meta;`, ""},
		{`import defer * as ns from "m";`, `t.js:1:1: SyntaxError: import defer is not supported yet (see TODO.md)`},
		{`import source s from "m";`, `t.js:1:1: SyntaxError: import source is not supported yet (see TODO.md)`},
		{`import source from from "m";`, `t.js:1:1: SyntaxError: import source is not supported yet (see TODO.md)`},
		{`x = import.source("m");`, `t.js:1:5: SyntaxError: import source is not supported yet (see TODO.md)`},
		{`x = import.defer("m");`, `t.js:1:5: SyntaxError: import defer is not supported yet (see TODO.md)`},
		{`import defer from "m";`, ""},
		{`import defer, * as ns from "m";`, ""},
		{`import source from "m";`, ""},
		{`import source, { x } from "m";`, ""},
		{`import { defer, source } from "m";`, ""},
	}
	for _, tt := range tests {
		for _, opts := range []Options{{}, {AllowUnsupported: true}} {
			_, err := ParseModule("t.js", tt.src, opts)
			if tt.msg == "" {
				assert.NoError(t, err, tt.src)
				continue
			}
			assert.EqualError(t, err, tt.msg, tt.src)
		}
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

	// Only a call of the identifier eval, not optional, is direct eval,
	// whatever the name resolves to.
	m, err = ParseModule("t.js", "function f() { eval; return x.eval(1) + eval.length + eval?.(1) + (0, eval)(1); }", Options{})
	require.NoError(t, err)
	assert.False(t, m.Body[0].(*FuncDecl).Func.HasDirectEval)
	s, err := ParseScript("t.js", "function g(eval) { return (eval)(1); }", Options{})
	require.NoError(t, err)
	assert.True(t, s.Body[0].(*FuncDecl).Func.HasDirectEval)
}
