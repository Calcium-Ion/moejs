package syntax

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModuleRequests(t *testing.T) {
	m := parseModule(t, `import "./a.js"; export * from "./b.js"; import x, * as ns from "./a.js"; export { y } from "./c.js"; import { z } from "./b.js";`)
	var specs []string
	for _, r := range m.Requests {
		specs = append(specs, r.Specifier)
	}
	// Each specifier once, in order of first appearance.
	assert.Equal(t, []string{"./a.js", "./b.js", "./c.js"}, specs)
	assert.Equal(t, 7, m.Requests[0].Pos)

	m = parseModule(t, "export const a = 1;")
	assert.Empty(t, m.Requests)
	assert.Empty(t, m.Imports)
	assert.Empty(t, m.Reexports)
	assert.Empty(t, m.Stars)
}

func TestModuleImports(t *testing.T) {
	m := parseModule(t, `import d, { a, b as c, "s t" as u, default as e } from "m"; import * as ns from "n"; d; ns;`)
	type entry struct {
		Request int
		Name    string
		NS      bool
		Local   string
		Kind    BindKind
	}
	var got []entry
	for _, ie := range m.Imports {
		got = append(got, entry{ie.Request, ie.Name, ie.Namespace, ie.Binding.Name, ie.Binding.Kind})
		assert.Same(t, m.Scope, ie.Binding.Scope)
		assert.False(t, ie.Binding.NeedsTDZ)
	}
	assert.Equal(t, []entry{
		{0, "default", false, "d", BindImport},
		{0, "a", false, "a", BindImport},
		{0, "b", false, "c", BindImport},
		{0, "s t", false, "u", BindImport},
		{0, "default", false, "e", BindImport},
		{1, "", true, "ns", BindImportNS},
	}, got)
	body := m.Body[2].(*ExprStmt).X.(*Ident)
	assert.Same(t, m.Scope.Lookup("d"), body.Binding)
}

func TestModuleExportsFrom(t *testing.T) {
	m := parseModule(t, `export { a, b as c, "x y" as "z" } from "m"; export * as ns from "n"; export * from "m"; import { i as j } from "o"; export { j, j as k }; import * as all from "p"; export { all };`)
	type reexport struct {
		Name, Import string
		Request      int
		All          bool
	}
	var got []reexport
	for _, e := range m.Reexports {
		got = append(got, reexport{e.Name, e.Import, e.Request, e.All})
	}
	assert.Equal(t, []reexport{
		{"a", "a", 0, false},
		{"c", "b", 0, false},
		{"z", "x y", 0, false},
		{"ns", "", 1, true},
		{"j", "i", 2, false},
		{"k", "i", 2, false},
		{"all", "", 3, true}, // a namespace import re-exported, as export * as
	}, got)
	require.Len(t, m.Stars, 1)
	assert.Equal(t, 0, m.Stars[0].Request)
	assert.Empty(t, m.Exports)
	assert.Nil(t, m.Export("j"))
	assert.Nil(t, m.Export("all"))
}

func TestModuleEarlyErrors(t *testing.T) {
	tests := []struct {
		name, src string
		line, col int
		msg       string
	}{
		{"duplicate import", `import { a } from "m"; import { b as a } from "n";`, 1, 38, "Identifier 'a' has already been declared"},
		{"import and let", `import a from "m"; let a;`, 1, 24, "Identifier 'a' has already been declared"},
		{"import and nested var", `{ var a; } import a from "m";`, 1, 19, "Identifier 'a' has already been declared"},
		{"import and function", `function a() {} import * as a from "m";`, 1, 29, "Identifier 'a' has already been declared"},
		{"duplicate reexport", `export { a } from "m"; export { b as a } from "n";`, 1, 33, "Duplicate export of 'a'"},
		{"reexport and local", `export let a; export * as a from "m";`, 1, 27, "Duplicate export of 'a'"},
		{"import reexported twice", `import { x } from "m"; export { x, x };`, 1, 36, "Duplicate export of 'x'"},
		{"undefined local export", `export { x }; import { y } from "m";`, 1, 10, "Export 'x' is not defined"},
		{"lone surrogate import", `import { "\uD800" as a } from "m";`, 1, 10, "Invalid module export name: contains unpaired surrogate"},
		{"lone surrogate export", `export * as "\uDC00" from "m";`, 1, 13, "Invalid module export name: contains unpaired surrogate"},
		{"string local without from", `export { "a" };`, 1, 10, "Unexpected token 'a'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseModule("t.js", tt.src, Options{})
			var se *Error
			require.ErrorAs(t, err, &se)
			assert.Equal(t, "SyntaxError: "+tt.msg, se.Msg)
			assert.Equal(t, [2]int{tt.line, tt.col}, [2]int{se.Line, se.Col}, "position")
		})
	}
	// A surrogate pair is a well-formed name.
	_, err := ParseModule("t.js", `export * as "😀" from "m";`, Options{})
	assert.NoError(t, err)
}

// TestModuleCycleTDZ: a module that requests others may have its exports
// called by a module of its import cycle before its body runs, so what an
// exported function reads before its initialiser needs a TDZ check.
func TestModuleCycleTDZ(t *testing.T) {
	tests := []struct {
		src  string
		want bool
	}{
		{`import "a"; export function f() { m; } let m = 1;`, true},
		{`import "a"; function f() { m; } let m = 1; f();`, false},
		{`import "a"; export function f() { g(); } function g() { m; } let m = 1;`, true},
		{`export * from "a"; function f() { m; } export { f as g }; let m = 1;`, true},
		{`export function f() { m; } let m = 1;`, false},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			m := parseModule(t, tt.src)
			assert.Equal(t, tt.want, m.Scope.Lookup("m").NeedsTDZ)
		})
	}
}
