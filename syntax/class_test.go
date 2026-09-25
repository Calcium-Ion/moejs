package syntax

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClassEarlyErrors is the class early-error table: each source must
// fail at exactly the given line:col with the given (real, not
// "unsupported") SyntaxError.
func TestClassEarlyErrors(t *testing.T) {
	tests := []struct {
		src       string
		line, col int
		msg       string
	}{
		// Constructors.
		{"class A { constructor() {} constructor() {} }", 1, 28, "A class may only have one constructor"},
		{"class A { constructor() {} 'constructor'() {} }", 1, 28, "A class may only have one constructor"},
		{"class A { get constructor() {} }", 1, 15, "Class constructor may not be an accessor"},
		{"class A { set constructor(v) {} }", 1, 15, "Class constructor may not be an accessor"},
		{"class A { *constructor() {} }", 1, 12, "Class constructor may not be a generator"},
		{"class A { async constructor() {} }", 1, 17, "Class constructor may not be an async method"},
		{"class A { constructor = 1; }", 1, 11, "Classes may not have a field named 'constructor'"},
		{"class A { 'constructor'; }", 1, 11, "Classes may not have a field named 'constructor'"},
		{"class A { #constructor() {} }", 1, 11, "Classes may not have a private field named '#constructor'"},
		{"class A { #constructor; }", 1, 11, "Classes may not have a private field named '#constructor'"},
		{"class A { static prototype() {} }", 1, 18, "Classes may not have a static property named 'prototype'"},
		{"class A { static 'prototype' = 1; }", 1, 18, "Classes may not have a static property named 'prototype'"},
		// Private name declarations.
		{"class A { #x; #x; }", 1, 15, "Identifier '#x' has already been declared"},
		{"class A { #x; #x() {} }", 1, 15, "Identifier '#x' has already been declared"},
		{"class A { get #x() {} get #x() {} }", 1, 27, "Identifier '#x' has already been declared"},
		{"class A { get #x() {} set #x(v) {} set #x(v) {} }", 1, 40, "Identifier '#x' has already been declared"},
		{"class A { get #x() {} static set #x(v) {} }", 1, 34, "Identifier '#x' has already been declared"},
		{"class A { #x; get #x() {} }", 1, 19, "Identifier '#x' has already been declared"},
		// Private name references.
		{"class A { m() { this.#y; } }", 1, 22, "Private field '#y' must be declared in an enclosing class"},
		{"class A { #x; m() { class B { n() { this.#y; } } } }", 1, 42, "Private field '#y' must be declared in an enclosing class"},
		{"class A { m() { class B { #y; } this.#y; } }", 1, 38, "Private field '#y' must be declared in an enclosing class"},
		{"class A extends (o.#x, B) { #x; }", 1, 20, "Private field '#x' must be declared in an enclosing class"},
		{"class A { #y; m() { class B extends (o.#x, C) { #x; } } }", 1, 40, "Private field '#x' must be declared in an enclosing class"},
		{"this.#x;", 1, 6, "Private field '#x' must be declared in an enclosing class"},
		{"#x in o;", 1, 1, "Private field '#x' must be declared in an enclosing class"},
		{"class A { #x; m() { delete this.#x; } }", 1, 21, "Private fields can not be deleted"},
		{"class A { #x; m() { delete this?.#x; } }", 1, 21, "Private fields can not be deleted"},
		{"class A { #x; m() { return #x; } }", 1, 28, "Unexpected token '#x'"},
		{"class A { #x; m() { return 1 + #x in o; } }", 1, 32, "Unexpected token '#x'"},
		{"class A { #x; m(o) { return a < #x in o; } }", 1, 33, "Unexpected token '#x'"},
		{"class A { set\n x() {} }", 2, 3, "Setter must have exactly one formal parameter."},
		{"x = { #x: 1 };", 1, 7, "Unexpected token '#x'"},
		{"class A { #x; m() { return super.#x; } }", 1, 34, "Unexpected private field"},
		// super.
		{"class A { constructor() { super(); } }", 1, 27, "'super' keyword unexpected here"},
		{"class A extends B { m() { super(); } }", 1, 27, "'super' keyword unexpected here"},
		{"class A extends B { x = super(); }", 1, 25, "'super' keyword unexpected here"},
		{"class A extends B { x = () => super(); }", 1, 31, "'super' keyword unexpected here"},
		{"class A extends B { static { super(); } }", 1, 30, "'super' keyword unexpected here"},
		{"class A extends B { constructor() { function f() { super(); } } }", 1, 52, "'super' keyword unexpected here"},
		{"class A extends B { constructor() { new super(); } }", 1, 41, "'super' keyword unexpected here"},
		{"function f() { super.x; }", 1, 16, "'super' keyword unexpected here"},
		{"function f() { super(); }", 1, 16, "'super' keyword unexpected here"},
		{"x = { f: function() { super.x; } };", 1, 23, "'super' keyword unexpected here"},
		{"x = { f: () => super.x };", 1, 16, "'super' keyword unexpected here"},
		{"super.x;", 1, 1, "'super' keyword unexpected here"},
		{"class A { m() { super; } }", 1, 17, "'super' keyword unexpected here"},
		// arguments, await and return in initializers.
		{"class A { x = arguments; }", 1, 15, "'arguments' is not allowed in class field initializer or static initialization block"},
		{"class A { x = () => arguments; }", 1, 21, "'arguments' is not allowed in class field initializer or static initialization block"},
		{"class A { static x = arguments; }", 1, 22, "'arguments' is not allowed in class field initializer or static initialization block"},
		{"class A { static { arguments; } }", 1, 20, "'arguments' is not allowed in class field initializer or static initialization block"},
		{"class A { static { () => arguments; } }", 1, 26, "'arguments' is not allowed in class field initializer or static initialization block"},
		{"class A { static { await 0; } }", 1, 20, "await is not allowed in class field initializers or static initialization blocks"},
		{"class A { static { () => await 0; } }", 1, 26, "await is not allowed in class field initializers or static initialization blocks"},
		{"class A { x = await 0; }", 1, 15, "await is not allowed in class field initializers or static initialization blocks"},
		{"class A { static { return; } }", 1, 20, "Illegal return statement"},
		{"class A { static { let a; var a; } }", 1, 31, "Identifier 'a' has already been declared"},
		// Element syntax.
		{"class A { x = 1 y = 2 }", 1, 17, "Unexpected identifier 'y'"},
		{"class A { get x = 1 }", 1, 17, "Unexpected token '='"},
		{"class A { *x = 1 }", 1, 14, "Unexpected token '='"},
		{"class A extends B, C {}", 1, 18, "Unexpected token ','"},
		{"class A extends () => {} {}", 1, 20, "Unexpected token '=>'"},
		{"class A extends (a) => a {}", 1, 21, "Unexpected token '=>'"},
		{"class A { st\\u0061tic m() {} }", 1, 23, "Unexpected identifier 'm'"},
		{"class A { g\\u0065t x() {} }", 1, 20, "Unexpected identifier 'x'"},
		{"class A { set *a(x) {} }", 1, 15, "Unexpected token '*'"},
		{"class {}", 1, 7, "Unexpected token '{'"},
		{"class A {} class A {}", 1, 18, "Identifier 'A' has already been declared"},
		{"let A; class A {}", 1, 14, "Identifier 'A' has already been declared"},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			_, err := ParseModule("t.js", tt.src, Options{})
			require.Error(t, err)
			var se *Error
			require.ErrorAs(t, err, &se)
			assert.Equal(t, "SyntaxError: "+tt.msg, se.Msg)
			assert.Equal(t, [2]int{tt.line, tt.col}, [2]int{se.Line, se.Col}, "position")
		})
	}
}

// TestClassNoErrors lists class forms that must parse and resolve.
func TestClassNoErrors(t *testing.T) {
	for _, src := range []string{
		"class A {}",
		"class A extends null {}",
		"class A extends B.C {}",
		"class A extends f() {}",
		"class A extends (B, C) {}",
		"class A extends (() => {}) {}",
		"class A extends f((a) => a) {}",
		"class A { bre\\u0061k() {} st\\u0061tic() {} static \\u0069f = 1; }",
		"const A = class {};",
		"const A = class B extends A { m() { return B; } };",
		"export class A {}",
		"export default class {}",
		"export default class A extends B {}",
		"class A { 'constructor'() {} }",
		"class A { static constructor() {} }",
		"class A { ['constructor']() {} ['constructor'] = 1; }",
		"class A { static ['prototype']() {} }",
		"class A { prototype() {} prototype = 1; }",
		"class A { async() {} get() {} set() {} static() {} }",
		"class A { get; set; static; async; }",
		"class A { static async = 1; get = 2; async\n x() {} }",
		"class A { 'x' = 1; 1 = 2; [k] = 3; #y = 4; static #z; 2n = 5; }",
		"class A { x\n y }",
		"class A { set\n *a(x) {} static get\n *b() {} }",
		"class A { ; ; m() {} ; }",
		"class A { #x; m(o) { return #x in o; } }",
		"class A { #x; m(o) { return #x in o && #x in o || !(#x in o); } }",
		"class A { #x; m() { class B extends (o.#x, C) { #x; } } }",
		"class A { get #x() {} set #x(v) {} }",
		"class A { static set #x(v) {} static get #x() {} }",
		"class A { #x; m() { return this.#x + this?.#x + this.#x.y; } }",
		"class A { #x; m() { class B { n(o) { return o.#x; } } } }",
		"class A { #x; m() { class B { #x; n(o) { return o.#x; } } } }",
		"class A { #x; m() { return { o() { return this.#x; } }; } }",
		"class A { #x; m() { this.#x = 1; this.#x += 1; this.#x++; [this.#x] = [1]; } }",
		"class A { m() { this.#x; } #x; }",
		"class A extends B { constructor() { super(); super.x; super['y'](); () => super(); } }",
		"class A extends B { constructor() { () => () => this; super(); } }",
		"class A extends B { m() { return super.m(); } static s() { return super.s(); } }",
		"class A extends B { x = super.x; static y = super.y; static { super.z; } }",
		"class A { get x() { return super.x; } set x(v) { super.x = v; } }",
		"class A { static { var x; let y; } static { var x; } }",
		"class A { static x = this; static { this.y = 1; } }",
		"class A { m() { return new.target; } x = new.target; static { new.target; } }",
		"class A { x = function() { return new.target; }; y = () => new.target; }",
		"x = { m() { return super.x; }, get y() { return super.y; }, set y(v) { super.y = v; } };",
		"x = { m() { return () => super.x; } };",
		"function f() { return new.target; }",
		"function f() { return () => new.target; }",
		"class C { static m() { class D extends C { #p; } } }",
		"class A { static { class B { static { this; } } } }",
		"class A { x = () => this; static y = () => this; }",
		"class A { [this.x] = 1; }",
		"async function f() { class A { [await 0]() {} } }",
	} {
		_, err := ParseModule("t.js", src, Options{AllowUnsupported: true})
		assert.NoError(t, err, src)
	}
}

// TestClassSetFieldBeforeGenerator: `get` or `set` followed by `*` is a
// field name, which ASI ends before a generator method on the next line.
func TestClassSetFieldBeforeGenerator(t *testing.T) {
	m, err := ParseModule("t.js", "class A { set\n *a(x) {} }", Options{AllowUnsupported: true})
	require.NoError(t, err)
	c := findClass(t, m)
	require.Len(t, c.Members, 2)
	assert.Equal(t, ClassField, c.Members[0].Kind)
	assert.Equal(t, "set", c.Members[0].Key.(*Ident).Name)
	assert.Equal(t, ClassMethod, c.Members[1].Kind)
	assert.True(t, memberFunc(c, 1).IsGenerator)
}

func findClass(t *testing.T, m *Module) *Class {
	t.Helper()
	var c *Class
	Inspect(m, func(n Node) bool {
		if cl, ok := n.(*Class); ok && c == nil {
			c = cl
		}
		return true
	})
	require.NotNil(t, c)
	return c
}

func memberFunc(c *Class, i int) *Function { return c.Members[i].Value.(*Function) }

// TestClassScope checks the class scope and the hidden bindings the
// compiler relies on.
func TestClassScope(t *testing.T) {
	t.Run("scope", func(t *testing.T) {
		m := parseModule(t, "class A { #x; #m() {} static #s; m() { return A; } }")
		c := findClass(t, m)
		require.NotNil(t, c.Scope)
		assert.Equal(t, ScopeClass, c.Scope.Kind)
		assert.Equal(t, "class", c.Scope.Kind.String())
		assert.Same(t, c, c.Scope.Node)
		// The inner name is immutable, captured by the method and, since no
		// element can run before it is initialized, free of TDZ checks.
		inner := c.NameBinding
		require.NotNil(t, inner)
		assert.Equal(t, BindConst, inner.Kind)
		assert.True(t, inner.Captured)
		assert.False(t, inner.NeedsTDZ)
		// The declaration's name is the outer, mutable binding.
		outer := c.Name.Binding
		assert.Equal(t, BindClass, outer.Kind)
		assert.Same(t, m.Scope, outer.Scope)
		// One binding per private name; fields are defined by the Fields
		// and Static methods, so they are captured.
		x := c.Scope.Lookup("#x")
		require.NotNil(t, x)
		assert.Equal(t, BindPrivate, x.Kind)
		assert.Equal(t, "private", x.Kind.String())
		assert.True(t, x.Captured)
		assert.Same(t, x, c.Members[0].Key.(*PrivateName).Binding)
		assert.False(t, c.Scope.Lookup("#m").Captured)
		assert.True(t, c.Scope.Lookup("#s").Captured)
		// Instance private methods need a brand, added by Fields.
		require.NotNil(t, c.BrandBinding)
		assert.Equal(t, BindHidden, c.BrandBinding.Kind)
		assert.Equal(t, "hidden", c.BrandBinding.Kind.String())
		require.NotNil(t, c.Fields)
		assert.Equal(t, FuncClassFields, c.Fields.Kind)
		require.NotNil(t, c.FieldsBinding)
		assert.True(t, c.FieldsBinding.Captured)
		require.NotNil(t, c.Static)
		assert.Equal(t, FuncClassStatic, c.Static.Kind)
		// The default constructor is synthesized.
		require.NotNil(t, c.Ctor)
		assert.True(t, c.Ctor.DefaultCtor)
		assert.False(t, c.Ctor.Derived)
		assert.Equal(t, c.Span, c.Ctor.Span)
		assert.Nil(t, c.Ctor.ThisBinding)
	})

	t.Run("tdz", func(t *testing.T) {
		for src, want := range map[string]bool{
			"class A { m() { return A; } static x = A; static { A; } y = A; }": false,
			"class A extends A {}":                         true,
			"class A extends (() => A) {}":                 true,
			"class A { [A]() {} }":                         true,
			"class A { [(() => A)()] = 1; }":               true,
			"x = class A { static [A.name]() {} };":        true,
			"x = class A { m() { class B extends A {} } }": false,
		} {
			c := findClass(t, parseModule(t, src))
			assert.Equal(t, want, c.NameBinding.NeedsTDZ, src)
		}
		// The outer binding keeps the ordinary rules.
		m := parseModule(t, "new A(); class A {}")
		assert.True(t, m.Body[1].(*ClassDecl).Class.Name.Binding.NeedsTDZ)
	})

	t.Run("no-fields", func(t *testing.T) {
		c := findClass(t, parseModule(t, "class A { m() {} static s() {} get x() {} static #p() {} }"))
		assert.Nil(t, c.Fields)
		assert.Nil(t, c.FieldsBinding)
		assert.Nil(t, c.Static)
		assert.Nil(t, c.BrandBinding)
	})

	t.Run("fields", func(t *testing.T) {
		c := findClass(t, parseModule(t, "class A { [k] = this; static [j] = 1; static { var v; } #p = () => this; }"))
		require.NotNil(t, c.Fields)
		require.NotNil(t, c.Static)
		assert.True(t, c.Fields.UsesThis)
		k := c.Members[0].KeyBinding
		require.NotNil(t, k)
		assert.True(t, k.Captured)
		assert.Same(t, c.Scope, k.Scope)
		assert.NotNil(t, c.Members[1].KeyBinding)
		block := c.Members[2].Block
		require.NotNil(t, block)
		assert.True(t, block.IsArrow)
		assert.Same(t, c.Static.Scope, block.Scope.Parent)
		assert.NotNil(t, block.Scope.Lookup("v"))
		arrow := c.Members[3].Value.(*Function)
		assert.Same(t, c.Fields.Scope, arrow.Scope.Parent)
	})

	t.Run("derived", func(t *testing.T) {
		c := findClass(t, parseModule(t, "class A extends B { constructor() { const f = () => this; super(); this.x = 1; } }"))
		ctor := c.Ctor
		assert.False(t, ctor.DefaultCtor)
		assert.True(t, ctor.Derived)
		require.NotNil(t, ctor.ThisBinding)
		assert.True(t, ctor.ThisBinding.NeedsTDZ)
		assert.True(t, ctor.ThisBinding.Captured) // read by the arrow
		assert.Same(t, ctor.Scope, ctor.ThisBinding.Scope)
		require.NotNil(t, ctor.CalleeBinding)
		assert.False(t, ctor.CalleeBinding.Captured)
		require.NotNil(t, ctor.NewTargetBinding)
		var thisRefs int
		Inspect(ctor.Body, func(n Node) bool {
			if te, ok := n.(*ThisExpr); ok {
				assert.Same(t, ctor.ThisBinding, te.Binding)
				thisRefs++
			}
			return true
		})
		assert.Equal(t, 2, thisRefs)
	})

	t.Run("derived-default", func(t *testing.T) {
		c := findClass(t, parseModule(t, "class A extends B {}"))
		ctor := c.Ctor
		assert.True(t, ctor.DefaultCtor)
		assert.True(t, ctor.Derived)
		assert.True(t, ctor.HasRest)
		assert.NotNil(t, ctor.Rest.(*Ident).Binding)
		assert.NotNil(t, ctor.ThisBinding)
		assert.NotNil(t, ctor.CalleeBinding)
		assert.NotNil(t, ctor.NewTargetBinding)
	})

	t.Run("super-in-arrow", func(t *testing.T) {
		c := findClass(t, parseModule(t, "class A extends B { constructor() { (() => super())(); } m() { return () => super.m(); } }"))
		assert.True(t, c.Ctor.ThisBinding.Captured)
		assert.True(t, c.Ctor.CalleeBinding.Captured)
		assert.True(t, c.Ctor.NewTargetBinding.Captured)
		m := memberFunc(c, 1)
		require.NotNil(t, m.HomeBinding)
		assert.True(t, m.HomeBinding.Captured)
		assert.Nil(t, m.ThisBinding)
		assert.Nil(t, c.Ctor.HomeBinding)
	})

	t.Run("base-this", func(t *testing.T) {
		c := findClass(t, parseModule(t, "class A { constructor() { this.x = super.y; } }"))
		assert.Nil(t, c.Ctor.ThisBinding)
		assert.NotNil(t, c.Ctor.HomeBinding)
		assert.False(t, c.Ctor.HomeBinding.Captured)
	})

	t.Run("object-method-home", func(t *testing.T) {
		m := parseModule(t, "x = { m() { return super.x; }, n() {} };")
		obj := m.Body[0].(*ExprStmt).X.(*AssignExpr).Value.(*ObjectLit)
		assert.NotNil(t, obj.Props[0].Value.(*Function).HomeBinding)
		assert.Nil(t, obj.Props[1].Value.(*Function).HomeBinding)
	})

	t.Run("new-target", func(t *testing.T) {
		m := parseModule(t, "function f() { return () => new.target; } function g() { return new.target; }")
		f := m.Body[0].(*FuncDecl).Func
		require.NotNil(t, f.NewTargetBinding)
		assert.True(t, f.NewTargetBinding.Captured)
		g := m.Body[1].(*FuncDecl).Func
		require.NotNil(t, g.NewTargetBinding)
		assert.False(t, g.NewTargetBinding.Captured)
		var nt *NewTarget
		Inspect(g.Body, func(n Node) bool {
			if x, ok := n.(*NewTarget); ok {
				nt = x
			}
			return true
		})
		assert.Same(t, g.NewTargetBinding, nt.Binding)
	})
}
