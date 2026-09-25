package engine

import (
	"errors"
	"runtime"
	"sync"
	"weak"

	"github.com/Calcium-Ion/moejs/bytecode"
)

// Interpreter entry points and per-realm interpreter state.
//
// Frames are windows into one contiguous per-realm register stack; a call
// allocates nothing when the callee's registers fit. The stack grows by
// doubling and copying, so no Go pointer into it may be held across a call:
// the run loop re-slices its register window after every operation that can
// re-enter the interpreter.

// interpState is the interpreter's per-realm mutable state (embedded in Realm).
type interpState struct {
	stack   []Value // register stack; frames are windows [base, base+NumRegs)
	sp      int     // first free register
	frames  []frameInfo
	nframes int
}

// frameInfo is the compact per-frame record used for stack capture: the
// running function, the pc of its current instruction and its register
// window base (interp_stack.go reads the caller's call instruction there).
type frameInfo struct {
	fn   *Object
	pc   uint32
	base uint32
}

const (
	initialStackSize = 64 // 1 KiB of registers; doubles on demand
	initialFrames    = 16
)

// funcMeta holds the process-wide materialized form of one compiled
// function: constants as engine values and property keys, and the metas of
// its children (parallel to Function.Children). It is built once per
// bytecode.Function and shared by every realm (strings are immutable and
// pre-hashed; keys are process-wide atoms). It holds no strong reference to
// the template so that the cache below cannot keep dead templates alive.
type funcMeta struct {
	consts   []Value       // ConstNumber/ConstString/ConstBigInt materialized, ConstRegExp: the pattern; zero for other kinds
	keys     []PropertyKey // ConstString with Key set: interned property key
	names    []*String     // ConstString: the string (for messages and TDZ errors); ConstRegExp: the flags
	children []*funcMeta
	name     *String   // interned function name ("" -> AtomEmpty)
	root     *funcMeta // root of the tree (the root itself)
	index    uint32    // pre-order index in the tree: this function's IC base in Realm.icTrees
	nfuncs   uint32    // root only: functions in the tree
	totalICs uint32    // root only: inline-cache slots of the whole tree
}

// funcMetas caches materialized function trees by root template. Keys are
// weak so a template that is no longer referenced (a RunString script, a
// recompiled plugin) is collected; a cleanup drops its entry.
var funcMetas sync.Map // weak.Pointer[bytecode.Function] -> *funcMeta

// metaFor returns the materialized tree for root, building it on first use.
func (r *Realm) metaFor(root *bytecode.Function) *funcMeta {
	k := weak.Make(root)
	if m, ok := funcMetas.Load(k); ok {
		return m.(*funcMeta)
	}
	n := uint32(0)
	m := r.materialize(root, nil, &n)
	m.nfuncs = n
	actual, loaded := funcMetas.LoadOrStore(k, m)
	if !loaded {
		runtime.AddCleanup(root, func(k weak.Pointer[bytecode.Function]) { funcMetas.Delete(k) }, k)
	}
	return actual.(*funcMeta)
}

// materialize builds the funcMeta of code and its children in the tree of
// root (nil: code is the root), numbering the templates pre-order through
// next.
func (r *Realm) materialize(code *bytecode.Function, root *funcMeta, next *uint32) *funcMeta {
	m := &funcMeta{}
	if root == nil {
		root = m
	}
	m.root, m.index = root, *next
	*next++
	root.totalICs += code.ICCount
	m.name = AtomEmpty
	if code.Name != "" {
		m.name = r.InternGoString(code.Name)
		prepareSharedString(m.name)
	}
	if n := len(code.Consts); n > 0 {
		m.consts = make([]Value, n)
		m.keys = make([]PropertyKey, n)
		m.names = make([]*String, n)
	}
	for i, k := range code.Consts {
		switch k.Kind {
		case bytecode.ConstNumber:
			m.consts[i] = NumberValue(k.Num)
		case bytecode.ConstString:
			var s *String
			if k.IsASCII {
				s = asciiString(k.Str)
			} else {
				s = FromUTF16(k.Units)
			}
			if k.Key {
				s = r.Intern(s)
				m.keys[i] = StringKey(s)
			}
			prepareSharedString(s)
			m.names[i] = s
			m.consts[i] = StringValue(s)
		case bytecode.ConstBigInt:
			b, ok := newBigIntConst(k.Str)
			if !ok {
				b = NewBigIntFromInt64(0)
			}
			m.consts[i] = BigIntValue(b)
		case bytecode.ConstRegExp:
			// Pattern and flags are materialized once per template so a
			// regular expression literal costs only its RegExp object.
			pattern, flags := FromGoString(k.Str), FromGoString(k.Flags)
			prepareSharedString(pattern)
			prepareSharedString(flags)
			m.consts[i] = StringValue(pattern)
			m.names[i] = flags
		}
	}
	if m.keys != nil {
		addCodeKeys(m.keys)
	}
	if len(code.Children) > 0 {
		m.children = make([]*funcMeta, len(code.Children))
		for i, c := range code.Children {
			m.children[i] = r.materialize(c, root, next)
		}
	}
	return m
}

func init() {
	runFunction = interpRun
	constructFunction = interpConstruct
	defaultStackCapture = captureStack
	defaultStackCaptureInto = captureStackInto
	defaultStackFormat = formatStack
}

// defaultStackCapture / defaultStackCaptureInto / defaultStackFormat are
// installed on every new realm by the realm constructors (realm.go).
var (
	defaultStackCapture     func(r *Realm) []StackFrame
	defaultStackCaptureInto func(r *Realm, buf []StackFrame, skip *Object, limit int) []StackFrame
	defaultStackFormat      func(r *Realm, frames []StackFrame) string
)

// ErrModulePending is EvaluateModule's error for a module with top-level
// await whose evaluation is still pending once the job queue has drained:
// it awaits a promise nothing settles.
var ErrModulePending = errors.New("engine: module evaluation is still pending after the job queue drained")

// EvaluateModule instantiates and runs a compiled module in r and returns
// its environment, whose slots back the live export bindings, or nil when
// the evaluation failed before the module's body started (an interrupt
// pending on entry), so no binding was initialized. The compiled
// template is shared: only per-realm state (function objects, the module
// Env, inline caches) is created here. A module with top-level await
// evaluates asynchronously; the job queue drains before an outermost
// EvaluateModule returns, and its error is then ModuleEvaluationError's.
func (r *Realm) EvaluateModule(code *bytecode.Function) (*ModuleEnv, error) {
	menv, p, err := r.EvaluateModuleAsync(code)
	if p == nil {
		return menv, err
	}
	return menv, r.ModuleEvaluationError(p, err)
}

// ModuleEvaluationError returns the error of the evaluation of a module
// with top-level await, whose promise EvaluateModuleAsync returned, once
// the jobs ran: the rejection reason as an *Exception, else err, the jobs'
// error (see ReleaseJobs), else ErrModulePending while p is pending. The
// rejection wins over err as a call's own exception does over its jobs',
// and an interrupt in err over the rejection, as over a call's exception.
func (r *Realm) ModuleEvaluationError(p *Object, err error) error {
	_, interrupted := err.(*InterruptedError)
	switch state, v, _ := p.PromiseResult(); {
	case state == PromiseRejected && !interrupted:
		return r.Throw(v)
	case err != nil:
		return err
	case state == PromisePending:
		return ErrModulePending
	}
	return nil
}

// EvaluateModuleAsync is EvaluateModule returning the promise of the
// evaluation of a module with top-level await (code.Async), as the job
// queue's drain left it, instead of its outcome. The promise is nil for any
// other module, which evaluates synchronously, and when an interrupt stops
// the evaluation before it starts, which leaves no environment either.
func (r *Realm) EvaluateModuleAsync(code *bytecode.Function) (*ModuleEnv, *Object, error) {
	if code.Kind != bytecode.KindModule {
		return nil, nil, errors.New("engine: EvaluateModule requires a module template")
	}
	fn, env := r.instantiateTopLevel(code)
	res, err := r.CallObject(fn, Undefined(), nil)
	if err != nil && fn.internal.(*FunctionData).icBase == icUnbound {
		// The inline caches are still unbound: enterFrame did not run the
		// body.
		return nil, nil, err
	}
	menv := &ModuleEnv{Env: env, exports: code.Exports}
	if code.Async && res.IsObject() {
		return menv, res.AsObject(), err
	}
	return menv, nil, err
}

// RunScript runs a compiled strict-mode script and returns its completion
// value. Top-level bindings live in the script's own environment (mirroring
// them onto the global object is TODO).
func (r *Realm) RunScript(code *bytecode.Function) (Value, error) {
	if code.Kind != bytecode.KindScript {
		return Undefined(), errors.New("engine: RunScript requires a script template")
	}
	if err := r.CheckGlobalDeclarations(code); err != nil {
		return Undefined(), err
	}
	fn, _ := r.instantiateTopLevel(code)
	return r.CallObject(fn, ObjectValue(r.Global), nil)
}

// CheckGlobalDeclarations makes the checks of GlobalDeclarationInstantiation
// that can fail before a script runs: a lexical declaration may not shadow a
// non-configurable property of the global object, such as undefined; a
// function declaration may replace a global property only when it is
// configurable or a writable, enumerable data property
// (CanDeclareGlobalFunction); and a function or var name the global object
// does not have needs it extensible (CanDeclareGlobalVar).
func (r *Realm) CheckGlobalDeclarations(code *bytecode.Function) error {
	g := code.Globals
	if g == nil {
		return nil
	}
	o := r.Global
	for _, name := range g.Lexical {
		if d, ok := o.GetOwnProperty(r.KeyFromGoString(name)); ok && !d.Configurable() {
			return r.SyntaxError("Identifier '%s' has already been declared", name)
		}
	}
	for _, name := range g.Function {
		d, ok := o.GetOwnProperty(r.KeyFromGoString(name))
		switch {
		case !ok && o.IsExtensible(), ok && d.Configurable():
		case ok && !d.IsAccessorDescriptor() && d.Writable() && d.Enumerable():
		default:
			return r.TypeError("Cannot redefine global function '%s'", name)
		}
	}
	for _, name := range g.Var {
		if !o.IsExtensible() && !o.HasOwnProperty(r.KeyFromGoString(name)) {
			return r.TypeError("Cannot define global variable '%s', global object is not extensible", name)
		}
	}
	return nil
}

// instantiateTopLevel creates the function object and environment of a
// module or script template.
func (r *Realm) instantiateTopLevel(code *bytecode.Function) (*Object, *Env) {
	meta := r.metaFor(code)
	env := NewEnv(nil, len(code.CaptureLayout))
	fn, fd := r.newClosure(code, meta, env, Undefined())
	return fn, fd.env
}

// protoObject co-allocates the `prototype` object of an ordinary function
// with its single `constructor` slot.
type protoObject struct {
	obj   Object
	slots [1]Value
}

// newClosure creates a function object over code/meta closing over env; this
// is the lexical this for arrows. A KindNormal function gets its `prototype`
// property in the shape (so [[OwnPropertyKeys]] order and attributes are
// spec-exact) but the prototype object itself is created on first access:
// the slot holds the hole marker until materializePrototype replaces it
// (getOwnCell), which most plugin functions never trigger. The property
// carries attrLazy, which keeps the function's shape distinct from that of
// any ordinary object with the same keys (Object.create(Function.prototype)
// plus length, name and prototype) and keeps every slot read of it off the
// inline-cache fast path, so the marker can only be read through getOwnCell.
func (r *Realm) newClosure(code *bytecode.Function, meta *funcMeta, env *Env, this Value) (*Object, *FunctionData) {
	o, fd := r.newFunctionObjectCap(meta.name, code.Length, FuncBytecode, 3)
	fd.code = code
	fd.env = env
	fd.meta = meta
	fd.icBase = icUnbound
	fd.thisValue = Undefined()
	switch code.Kind {
	case bytecode.KindArrow:
		fd.thisValue = this
	case bytecode.KindNormal:
		o.shape = o.shape.addProperty(r, StringKey(AtomPrototype), attrWritable|attrLazy)
		o.slots = append(o.slots, Hole())
	}
	return o, fd
}

// materializePrototype creates the `prototype` object of an ordinary
// function whose slot still holds the lazy marker: an ordinary object with
// the function as its non-enumerable `constructor`, as MakeConstructor
// requires. Native functions never carry the marker (constructors get their
// prototype eagerly), so fd.realm is the creating realm.
func (o *Object) materializePrototype(slot *Value) {
	r := o.internal.(*FunctionData).realm
	po := &protoObject{}
	p := &po.obj
	p.shape = r.plainRoot.addProperty(r, StringKey(AtomConstructor), attrHidden)
	p.proto = r.ObjectPrototype
	p.class = ClassObject
	p.flags = flagExtensible
	po.slots[0] = ObjectValue(o)
	p.slots = po.slots[:1:1]
	*slot = ObjectValue(p)
}

// icUnbound is the icBase of a closure whose inline caches are not bound
// yet. enterFrame binds them on the first call, reserving them on the
// template's first call in the realm, so the functions of a program that
// never run cost no IC storage; closures of one template share them.
const icUnbound = ^uint32(0)

// interpRun implements [[Call]] for bytecode functions (runFunction).
func interpRun(r *Realm, fn *Object, fd *FunctionData, this Value, args []Value) (Value, error) {
	return r.enterFrame(fn, fd, this, args)
}

// interpConstruct implements [[Construct]] for bytecode functions.
func interpConstruct(r *Realm, fn *Object, fd *FunctionData, args []Value, newTarget *Object) (Value, error) {
	if fd.code.NewTarget {
		return r.constructNT(fn, fd, args, newTarget)
	}
	obj, err := r.OrdinaryCreateFromConstructor(newTarget, r.ObjectPrototype, ClassObject)
	if err != nil {
		return Undefined(), err
	}
	res, err := r.enterFrame(fn, fd, ObjectValue(obj), args)
	if err != nil {
		return Undefined(), err
	}
	if res.IsObject() {
		return res, nil
	}
	return ObjectValue(obj), nil
}

// enterFrame reserves a register window, binds arguments and the closure
// environment, and runs the function. It allocates only when the stack or
// frame table must grow, when the function has captured bindings (its Env)
// or a rest parameter (its array).
func (r *Realm) enterFrame(fn *Object, fd *FunctionData, this Value, args []Value) (Value, error) {
	if r.interruptFlag.Load() != 0 {
		return Undefined(), r.interruptError()
	}
	code := fd.code
	if fd.icBase == icUnbound {
		fd.icBase = r.icBaseFor(fd.meta, code.ICCount)
	}
	st := &r.interp
	base := st.sp
	top := base + int(code.NumRegs)
	if top > len(st.stack) {
		r.growStack(top)
	}
	regs := st.stack[base:top:top]
	np := int(code.NumParams)
	n := min(np, len(args))
	copy(regs, args[:n])
	u := Undefined()
	for i := n; i < np; i++ {
		regs[i] = u
	}
	if code.HasRest {
		var rest []Value
		if len(args) > np {
			rest = make([]Value, len(args)-np)
			copy(rest, args[np:])
		}
		regs[np] = ObjectValue(r.NewArrayFromSlice(rest))
	}
	if code.HasArguments {
		i := np
		if code.HasRest {
			i++
		}
		regs[i] = ObjectValue(r.newArguments(args))
	}
	env := fd.env
	if layout := code.CaptureLayout; len(layout) > 0 && code.Kind != bytecode.KindModule && code.Kind != bytecode.KindScript {
		env = newEnvSized(fd.env, len(layout))
		for i, reg := range layout {
			if reg != bytecode.NoRegister {
				env.slots[i] = regs[reg]
			}
		}
	}
	if code.Kind == bytecode.KindArrow {
		this = fd.thisValue
	}
	fi := st.nframes
	if fi == len(st.frames) {
		nf := make([]frameInfo, max(initialFrames, 2*len(st.frames)))
		copy(nf, st.frames)
		st.frames = nf
	}
	st.frames[fi] = frameInfo{fn: fn, base: uint32(base)}
	st.nframes = fi + 1
	st.sp = top
	res, err := r.run(fi, fd, base, env, this, fn)
	st.sp = base
	st.nframes = fi
	return res, err
}

// Small environments are co-allocated with their slot storage so that a
// closure scope costs one allocation.
type env1 struct {
	e   Env
	buf [1]Value
}

type env2 struct {
	e   Env
	buf [2]Value
}

type env4 struct {
	e   Env
	buf [4]Value
}

// newEnvSized is NewEnv with inline storage for up to four slots.
func newEnvSized(parent *Env, n int) *Env {
	var e *Env
	switch {
	case n <= 1:
		x := &env1{}
		x.e.slots = x.buf[:n:n]
		e = &x.e
	case n == 2:
		x := &env2{}
		x.e.slots = x.buf[:n:n]
		e = &x.e
	case n <= 4:
		x := &env4{}
		x.e.slots = x.buf[:n:n]
		e = &x.e
	default:
		return NewEnv(parent, n)
	}
	e.parent = parent
	u := Undefined()
	for i := range e.slots {
		e.slots[i] = u
	}
	return e
}

// pushArgs reserves n registers above the live frames for the callback
// arguments of a builtin that calls back into JavaScript repeatedly (map,
// filter, sort, ...): a Go array would escape to the heap through the
// indirect call. The callee's frame starts above the window, so the window
// is copied out before it can be reused; a stale slice after the stack has
// grown is still valid (the old storage stays alive while referenced).
// popArgs releases the window.
func (r *Realm) pushArgs(n int) []Value {
	st := &r.interp
	if st.sp+n > len(st.stack) {
		r.growStack(st.sp + n)
	}
	args := st.stack[st.sp : st.sp+n : st.sp+n]
	st.sp += n
	return args
}

func (r *Realm) popArgs(n int) { r.interp.sp -= n }

// growStack enlarges the register stack to hold at least top registers.
func (r *Realm) growStack(top int) {
	st := &r.interp
	n := max(initialStackSize, 2*len(st.stack))
	for n < top {
		n *= 2
	}
	ns := make([]Value, n)
	copy(ns, st.stack[:st.sp])
	st.stack = ns
}
