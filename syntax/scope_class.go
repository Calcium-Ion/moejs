package syntax

import (
	"math"
)

// Names of the hidden bindings (BindHidden). They cannot be written as
// identifiers, so they never collide with user bindings.
const (
	hiddenThis      = "%this"
	hiddenCallee    = "%callee"
	hiddenNewTarget = "%newtarget"
	hiddenHome      = "%home"
	hiddenFields    = "%fields"
	hiddenBrand     = "%brand"
	hiddenKey       = "%key"
	hiddenArgs      = "%args"
	// hiddenEvalVars holds the object that receives the var and function
	// declarations of the direct evals of a sloppy function (see
	// resolver.directEval).
	hiddenEvalVars = "%evalvars"
)

// class resolves a class declaration or expression.
//
// The class scope (ScopeClass) belongs to the enclosing function and holds
// the inner name binding, the private names and the hidden bindings that
// carry the brand, the field initializer closure and computed field keys
// from class definition to the initializers. Instance field initializers
// are resolved inside the synthetic Class.Fields method and static fields
// and blocks inside Class.Static, so `this`, super properties, new.target,
// `arguments` and `await` see the right function; a static block is an
// arrow function nested in Class.Static, which gives it its own var scope.
func (r *resolver) class(c *Class) {
	s := r.newScope(ScopeClass, c)
	c.Scope = s
	r.push(s)
	if c.Name != nil {
		c.NameBinding = &Binding{Name: c.Name.Name, Kind: BindConst, Pos: c.Name.Pos}
		s.add(c.NameBinding)
	}
	needBrand := false
	for _, m := range c.Members {
		pn, ok := m.Key.(*PrivateName)
		if !ok {
			continue
		}
		b := s.Lookup("#" + pn.Name)
		if b == nil {
			b = &Binding{Name: "#" + pn.Name, Kind: BindPrivate, Pos: pn.Pos}
			s.add(b)
		}
		pn.Binding = b
		switch {
		case m.Kind == ClassField:
			b.Captured = true // defined by the Fields or Static method
		case !m.Static:
			needBrand = true
		}
	}
	if needBrand {
		c.BrandBinding = &Binding{Name: hiddenBrand, Kind: BindHidden, Pos: c.Pos, Captured: true}
		s.add(c.BrandBinding)
	}

	// The heritage sees the outer private environment, and the inner name
	// binding in its TDZ.
	if c.Super != nil {
		s.heritage = true
		r.classDefinition(c, c.Super)
		s.heritage = false
	}

	ctor := c.Ctor
	if ctor == nil {
		ctor = &Function{Span: c.Span, Kind: FuncClassConstructor, IsStrict: true, DefaultCtor: true, Body: &BlockStmt{Span: c.Span}}
		if c.Super != nil {
			// constructor(...args) { super(...args); }, forwarding the rest
			// array without iterating it.
			ctor.Rest = &Ident{Span: Span{c.Pos, c.Pos}, Name: hiddenArgs}
			ctor.HasRest = true
		}
		c.Ctor = ctor
	}
	ctor.Derived = c.Super != nil

	var fields, static *fnFrame
	if needBrand {
		fields = r.syntheticFrame(c, &c.Fields, FuncClassFields)
	}
	for _, m := range c.Members {
		if m.Computed {
			r.classDefinition(c, m.Key)
			if m.Kind == ClassField {
				m.KeyBinding = &Binding{Name: hiddenKey, Kind: BindHidden, Pos: m.Pos, Captured: true}
				s.add(m.KeyBinding)
			}
		}
		switch m.Kind {
		case ClassMethod, ClassGetter, ClassSetter:
			fn := m.Value.(*Function)
			r.function(fn, nil, false)
		case ClassField:
			var frame *fnFrame
			if m.Static {
				if static == nil {
					static = r.syntheticFrame(c, &c.Static, FuncClassStatic)
				}
				frame = static
			} else {
				if fields == nil {
					fields = r.syntheticFrame(c, &c.Fields, FuncClassFields)
				}
				frame = fields
			}
			if m.Value != nil {
				r.inFrame(frame, func() { r.expr(m.Value) })
			}
		case ClassStaticBlock:
			if static == nil {
				static = r.syntheticFrame(c, &c.Static, FuncClassStatic)
			}
			m.Block = &Function{Span: m.Span, Kind: FuncArrow, IsArrow: true, IsStrict: true, Body: m.Body}
			r.inFrame(static, func() { r.function(m.Block, nil, false) })
		}
	}
	if ctor.DefaultCtor {
		r.function(ctor, nil, false)
		if ctor.Derived {
			hiddenOf(ctor, &ctor.ThisBinding, hiddenThis, true)
			hiddenOf(ctor, &ctor.CalleeBinding, hiddenCallee, false)
			hiddenOf(ctor, &ctor.NewTargetBinding, hiddenNewTarget, false)
		}
	}
	if fields != nil {
		c.FieldsBinding = &Binding{Name: hiddenFields, Kind: BindHidden, Pos: c.Pos, Captured: true}
		s.add(c.FieldsBinding)
	}
	r.pop()
}

// classDefinition resolves an expression evaluated while the class is being
// defined (the heritage or a computed key), where the inner name binding is
// still in its TDZ: raising its initEnd past every position makes each
// reference from here, including from nested functions, check the TDZ.
// References from the class elements themselves cannot run before the
// binding is initialized and need no check.
func (r *resolver) classDefinition(c *Class, e Expr) {
	if c.NameBinding != nil {
		c.NameBinding.initEnd = math.MaxInt
	}
	r.expr(e)
	if c.NameBinding != nil {
		c.NameBinding.initEnd = 0
	}
}

// syntheticFrame creates the Fields or Static method of c (kind
// FuncClassFields or FuncClassStatic) with its function scope, as a child
// of the class scope.
func (r *resolver) syntheticFrame(c *Class, slot **Function, kind FuncKind) *fnFrame {
	fn := &Function{Span: c.Span, Kind: kind, IsStrict: true}
	*slot = fn
	s := r.newScope(ScopeFunction, fn)
	s.Func = fn
	fn.Scope = s
	return &fnFrame{fn: fn}
}

// inFrame resolves f inside a synthetic method frame. The frame is entered
// once per initializer, interleaved with the other class elements.
func (r *resolver) inFrame(frame *fnFrame, f func()) {
	r.frames = append(r.frames, frame)
	prev := r.scope
	r.scope = frame.fn.Scope
	f()
	r.scope = prev
	r.frames = r.frames[:len(r.frames)-1]
}

// hiddenOf returns the hidden binding in *slot of fn, declaring it in the
// function scope on first use.
func hiddenOf(fn *Function, slot **Binding, name string, tdz bool) *Binding {
	b := *slot
	if b == nil {
		b = &Binding{Name: name, Kind: BindHidden, Pos: fn.Pos, NeedsTDZ: tdz}
		fn.Scope.add(b)
		*slot = b
	}
	return b
}

// hidden is hiddenOf for the function of frame i, marking the binding
// captured when the reference is in a nested arrow function.
func (r *resolver) hidden(i int, slot **Binding, name string, tdz bool) *Binding {
	b := hiddenOf(r.frames[i].fn, slot, name, tdz)
	if i != len(r.frames)-1 {
		b.Captured = true
	}
	return b
}

// nonArrowFrame returns the index of the innermost non-arrow function
// frame, which supplies this, new.target, super and arguments (0 at the top
// level).
func (r *resolver) nonArrowFrame() int {
	i := len(r.frames) - 1
	for i > 0 && r.frames[i].fn.IsArrow {
		i--
	}
	return i
}

// derivedThis returns the `this` binding when the reference is in a derived
// class constructor (or an arrow nested in one), and nil otherwise.
func (r *resolver) derivedThis() *Binding {
	i := r.nonArrowFrame()
	if i == 0 {
		return nil
	}
	fn := r.frames[i].fn
	if fn.Kind != FuncClassConstructor || !fn.Derived {
		return nil
	}
	return r.hidden(i, &fn.ThisBinding, hiddenThis, true)
}

func (r *resolver) newTarget() *Binding {
	i := r.nonArrowFrame()
	if i == 0 {
		return nil // rejected by the parser
	}
	fn := r.frames[i].fn
	return r.hidden(i, &fn.NewTargetBinding, hiddenNewTarget, false)
}

// superCall resolves super(...), valid only in a derived constructor or an
// arrow nested in one. It reads the active function (for its prototype,
// the super constructor) and new.target, and initializes `this`.
func (r *resolver) superCall(e *SuperExpr) {
	i := r.nonArrowFrame()
	if i == 0 || r.frames[i].fn.Kind != FuncClassConstructor || !r.frames[i].fn.Derived {
		r.fail(e.Pos, "'super' keyword unexpected here")
	}
	fn := r.frames[i].fn
	r.markThis()
	r.hidden(i, &fn.ThisBinding, hiddenThis, true)
	r.hidden(i, &fn.CalleeBinding, hiddenCallee, false)
	r.hidden(i, &fn.NewTargetBinding, hiddenNewTarget, false)
}

// superProperty resolves super.x / super[x], valid in methods (class or
// object literal), accessors, constructors, field initializers and static
// blocks, and in arrows nested in them.
func (r *resolver) superProperty(e *SuperExpr) {
	i := r.nonArrowFrame()
	if i == 0 {
		r.fail(e.Pos, "'super' keyword unexpected here")
	}
	fn := r.frames[i].fn
	switch fn.Kind {
	case FuncMethod, FuncGetter, FuncSetter, FuncClassConstructor, FuncClassFields, FuncClassStatic:
	default:
		r.fail(e.Pos, "'super' keyword unexpected here")
	}
	r.markThis()
	r.derivedThis()
	r.hidden(i, &fn.HomeBinding, hiddenHome, false)
}

// privateName resolves a reference to a private name in the enclosing
// classes; an undeclared name is an early error.
func (r *resolver) privateName(pn *PrivateName) {
	name := "#" + pn.Name
	for s := r.scope; s != nil; s = s.Parent {
		if s.Kind != ScopeClass || s.heritage {
			continue
		}
		if b := s.Lookup(name); b != nil {
			pn.Binding = b
			if r.ownerFrame(b) != len(r.frames)-1 {
				b.Captured = true
			}
			return
		}
	}
	r.fail(pn.Pos, "Private field '"+name+"' must be declared in an enclosing class")
}

// inClassInitializer reports whether the innermost non-arrow function is a
// field initializer or static block.
func (r *resolver) inClassInitializer() bool {
	i := r.nonArrowFrame()
	if i == 0 {
		return false
	}
	k := r.frames[i].fn.Kind
	return k == FuncClassFields || k == FuncClassStatic
}

func (r *resolver) checkClassArguments(id *Ident) {
	if r.inClassInitializer() {
		r.fail(id.Pos, "'arguments' is not allowed in class field initializer or static initialization block")
	}
}

// checkClassAwait rejects await in a field initializer or static block
// (outside any nested async function).
func (r *resolver) checkClassAwait(e *AwaitExpr) {
	for i := len(r.frames) - 1; i > 0; i-- {
		if r.frames[i].fn.IsAsync {
			return
		}
		if !r.frames[i].fn.IsArrow {
			break
		}
	}
	if r.inClassInitializer() {
		r.fail(e.Pos, "await is not allowed in class field initializers or static initialization blocks")
	}
}
