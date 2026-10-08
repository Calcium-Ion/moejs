package engine

import (
	"github.com/Calcium-Ion/moejs/bytecode"
	"unsafe"
)

// Async functions (ECMA-262 §27.7) suspend and resume the way generators do
// (generator.go): the frame's register window is copied out at an Await and
// back when the awaited promise settles, and no goroutine or Go stack is
// kept per call. A call runs the body from AsyncStart, which creates the
// call's state and the promise it returns, to an epilogue that resolves the
// promise (AsyncReturn) or, from a catch-all handler, rejects it
// (AsyncThrow); both return the promise, which the caller receives when the
// body completes without awaiting.
//
// Await resolves its operand to a promise and reacts to it with the state
// itself (a promiseReactor), so an await allocates the reaction record only:
// the frame is saved into the state and run returns the promise to the
// caller. The reaction job resumes the frame with the result in R[A] and
// the outcome in R[A+1] (undefined: fulfilled, true: rejected), and the
// code after Await throws a rejection where the await stands. A resumed
// frame runs with the call depth raised, as a call from the job would.

// asyncFunction is the state of an async function call: its frame and
// promise. It is the internal of a hidden object no program can reach (the
// body keeps it in a register).
type asyncFunction struct {
	obj     Object
	g       genFrame
	promise *Object
}

// asyncOp executes one async op (genOp's default case) of the frame whose
// registers start at base and returns the next pc. A *genFrame error
// suspends the frame.
func (r *Realm) asyncOp(fd *FunctionData, base int, w uint32, pc int) (int, error) {
	st := &r.interp
	top := base + int(fd.code.NumRegs)
	regs := st.stack[base:top:top]
	a, b := int(uint8(w>>8)), int(uint8(w>>16))
	switch bytecode.Op(w) {
	case bytecode.AsyncFunc:
		r.makeAsyncFunction(regs[a].AsObject())
	case bytecode.AsyncStart:
		af := &asyncFunction{promise: r.newPromise()}
		r.chargeObject(unsafe.Sizeof(asyncFunction{}))
		af.g = genFrame{fn: st.frames[st.nframes-1].fn, fd: fd, this: regs[a], state: genExecuting}
		o := initObject(&af.obj, ClassObject, r.nullProtoRoot)
		o.internal = af
		regs[a] = ObjectValue(o)
	case bytecode.AsyncGenStart:
		o, err := r.newAsyncGenerator(st.frames[st.nframes-1].fn, fd, regs[a])
		if err != nil {
			return pc, err
		}
		regs = st.stack[base:top:top]
		regs[a] = ObjectValue(o)
		g := &o.internal.(*asyncGenerator).g
		g.out = regs[a]
		return pc, g.save(regs, pc)
	case bytecode.Await:
		p, err := r.promiseResolve(regs[a])
		if err != nil {
			return pc, err
		}
		regs = st.stack[base:top:top]
		var g *genFrame
		var x promiseReactor
		switch s := regs[b].AsObject().internal.(type) {
		case *asyncFunction:
			g, x = &s.g, s
			g.out = ObjectValue(s.promise)
		case *asyncGenerator:
			g, x = &s.g, s // run returns undefined to the resumer
		}
		if g.regs == nil {
			g.regs = make([]Value, len(regs))
			r.charge(len(regs) * valueSize)
		}
		g.recv, g.state = uint8(a), genAwaiting
		r.promiseReact(p, x)
		return pc, g.save(regs, pc)
	case bytecode.AsyncReturn:
		af := regs[b].AsObject().internal.(*asyncFunction)
		err := r.resolvePromiseErr(af.promise, regs[a])
		regs = st.stack[base:top:top]
		regs[a] = ObjectValue(af.promise)
		return pc, err
	case bytecode.AsyncThrow:
		af := regs[b].AsObject().internal.(*asyncFunction)
		r.rejectPromise(af.promise, regs[a])
		regs = st.stack[base:top:top] // the rejection tracker may have run code
		regs[a] = ObjectValue(af.promise)
	case bytecode.AsyncYield:
		ag := regs[b].AsObject().internal.(*asyncGenerator)
		err := r.agCompleteStep(ag, regs[a], false, false)
		regs = st.stack[base:top:top]
		if err != nil {
			return pc, err
		}
		if len(ag.queue) > 0 {
			// The next request is already queued: continue with it.
			regs[a], regs[a+1] = ag.queue[0].value, ag.queue[0].mode
			return pc, nil
		}
		g := &ag.g
		g.recv, g.state = uint8(a), genSuspendedYield
		return pc, g.save(regs, pc)
	default:
		// The module and sloppy ops follow the async iteration ops: one
		// range test sends them on without asyncIterOp's hop.
		if op := bytecode.Op(w); op >= bytecode.ImportCall {
			if op >= bytecode.SetGlobalSloppy {
				return r.sloppyOp(fd, base, w, pc)
			}
			return r.moduleOp(fd, base, w, pc)
		}
		return r.asyncIterOp(fd, base, w, pc)
	}
	return pc, nil
}

// promiseSettled resumes the awaiting call with the awaited promise's
// result. The body settles its own promise, so an error here is an
// interrupt.
func (af *asyncFunction) promiseSettled(r *Realm, v Value, rejected bool) error {
	_, err := r.resumeAwait(&af.g, v, rejected)
	if af.g.state == genExecuting {
		af.g.finish()
	}
	return err
}

// resumeAwait resumes the frame g suspended at an Await from the reaction
// job of the awaited promise, counting the resumption as a call.
func (r *Realm) resumeAwait(g *genFrame, v Value, rejected bool) (Value, error) {
	mode := Undefined()
	if rejected {
		mode = True()
	}
	r.callDepth++
	res, err := r.resumeFrame(g, v, mode)
	r.callDepth--
	return res, err
}

// makeAsyncFunction completes the fresh closure o of an async function: its
// [[Prototype]] is %AsyncFunction.prototype%. An async generator function's
// is %AsyncGeneratorFunction.prototype%, and its own `prototype` (writable,
// not enumerable or configurable) an ordinary object inheriting from
// %AsyncGeneratorPrototype%.
func (r *Realm) makeAsyncFunction(o *Object) {
	ai := r.asyncIntr()
	if o.internal.(*FunctionData).code.Generator {
		o.proto = ai.AsyncGeneratorFunctionPrototype
		o.shape = r.rootShapeFor(o.proto).
			addProperty(r, StringKey(AtomLength), attrConfigurable).
			addProperty(r, StringKey(AtomName), attrConfigurable).
			addProperty(r, StringKey(AtomPrototype), attrWritable)
		o.slots = append(o.slots, ObjectValue(r.NewObjectWithProto(ai.AsyncGeneratorPrototype)))
		return
	}
	o.proto = ai.AsyncFunctionPrototype
	o.shape = r.rootShapeFor(o.proto).
		addProperty(r, StringKey(AtomLength), attrConfigurable).
		addProperty(r, StringKey(AtomName), attrConfigurable)
}

// --- intrinsics ----------------------------------------------------------------

// asyncIntr returns the realm's async intrinsics, building them on first
// use: into a mutable realm's own, once per process for the shared realms
// (sharedAsyncIntrinsics).
func (r *Realm) asyncIntr() *asyncIntrinsics {
	ai := r.generatorIntrinsics()
	if ai.AsyncFunctionPrototype == nil {
		if r.sharedIntrinsics {
			r.Intrinsics = sharedAsyncIntrinsics()
			return r.async
		}
		installAsyncIntrinsics(r, ai)
	}
	return ai
}

// sharedAsyncIntrinsics returns the latest shared intrinsics, building the
// async ones into them first if no shared realm of the process has. The
// template leaves them out, as it does the late globals (buildLateGroup),
// so a process that runs no async function or async iteration carries none
// of their objects, which every collection would mark. No group installer
// needs them: its builder would build them into the template's copy.
func sharedAsyncIntrinsics() *Intrinsics {
	if intr := sharedIntr.Load(); intr.async.AsyncFunctionPrototype != nil {
		return intr
	}
	lateMu.Lock()
	defer lateMu.Unlock()
	if intr := sharedIntr.Load(); intr.async.AsyncFunctionPrototype != nil {
		return intr
	}
	b := newGroupBuilder()
	ai := *b.async
	b.async = &ai
	installAsyncIntrinsics(b, &ai)
	b.freezeIntrinsics()
	addSharedRoots(b.rootShapes)
	sharedIntr.Store(b.Intrinsics)
	return b.Intrinsics
}

// installAsyncIntrinsics builds the intrinsics of async functions, async
// generators and async iteration.
func installAsyncIntrinsics(r *Realm, ai *asyncIntrinsics) {
	// %AsyncFunction.prototype%: constructor, @@toStringTag.
	afp := r.newIntrinsic(ClassObject, r.FunctionPrototype, 2)
	af := r.newConstructor(AtomAsyncFunction, 1, asyncFunctionCall, asyncFunctionConstruct, afp)
	af.SetPrototypeOf(r, r.FunctionCtor)
	r.installOrReplace(afp, StringKey(AtomConstructor), propCell{value: ObjectValue(af), attrs: attrConfigurable})
	r.installToStringTag(afp, AtomAsyncFunction)

	// %AsyncIteratorPrototype%: @@asyncIterator.
	aip := r.newIntrinsic(ClassObject, r.ObjectPrototype, 1)
	r.installSymbolMethod(aip, SymAsyncIterator, atomAsyncIteratorFn, iteratorProtoIterator, 0, attrHidden)

	// %AsyncGeneratorFunction.prototype%: constructor, prototype,
	// @@toStringTag; %AsyncGeneratorPrototype%: constructor, next, return,
	// throw, @@toStringTag. The links are read-only, as the generator ones.
	agfp := r.newIntrinsic(ClassObject, r.FunctionPrototype, 3)
	agp := r.newIntrinsic(ClassObject, aip, 5)
	agf := r.newConstructor(AtomAsyncGeneratorFunction, 1, asyncGeneratorFunctionCall, asyncGeneratorFunctionConstruct, agfp)
	agf.SetPrototypeOf(r, r.FunctionCtor)
	r.installOrReplace(agfp, StringKey(AtomConstructor), propCell{value: ObjectValue(agf), attrs: attrConfigurable})
	r.installOrReplace(agfp, StringKey(AtomPrototype), propCell{value: ObjectValue(agp), attrs: attrConfigurable})
	r.installToStringTag(agfp, AtomAsyncGeneratorFunction)
	r.installOrReplace(agp, StringKey(AtomConstructor), propCell{value: ObjectValue(agfp), attrs: attrConfigurable})
	r.installBuiltins(agp, []builtinDef{
		{AtomNext, asyncGeneratorNext, 1},
		{AtomReturn, asyncGeneratorReturn, 1},
		{AtomThrow, asyncGeneratorThrow, 1},
	})
	r.installToStringTag(agp, AtomAsyncGenerator)

	// %AsyncFromSyncIteratorPrototype%: next, return, throw.
	afsp := r.newIntrinsic(ClassObject, aip, 3)
	r.installBuiltins(afsp, []builtinDef{
		{AtomNext, asyncFromSyncNext, 1},
		{AtomReturn, asyncFromSyncReturn, 1},
		{AtomThrow, asyncFromSyncThrow, 1},
	})
	next, _, _ := afsp.shape.Lookup(nextKey)

	ai.AsyncFunction, ai.AsyncFunctionPrototype = af, afp
	ai.AsyncGeneratorFunction, ai.AsyncGeneratorFunctionPrototype, ai.AsyncGeneratorPrototype = agf, agfp, agp
	ai.AsyncIteratorPrototype, ai.AsyncFromSyncIteratorPrototype = aip, afsp
	ai.asyncFromSyncNext = afsp.slots[next].AsObject()
}
