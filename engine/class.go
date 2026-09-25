package engine

import "github.com/Calcium-Ion/moejs/bytecode"

// Classes (ECMA-262 §15.7) compile to ordinary bytecode plus the ops below,
// which sit after the interpreter's jump table and are dispatched from its
// default case through classOp: they only run while a class is defined and
// in class code, so ordinary calls and property accesses pay nothing.
//
// A class constructor is a bytecode function of kind KindClassCtor or
// KindDerivedCtor. Its [[Call]] enters the frame like any function and
// throws at its first op (CtorEntry). Its [[Construct]], like that of any
// function that reads new.target (bytecode.Function.NewTarget), goes
// through constructNT, which hands new.target to the callee in
// Realm.newTarget: the field is set only between constructNT and the
// callee's first op, which consumes it, so no frame carries it.
//
// A derived constructor runs with an uninitialized `this` (the hidden %this
// binding holds the hole); super(...) constructs the parent with the same
// new.target, binds %this and runs the field initializers. The compiler
// routes every return of a derived constructor through DerivedResult.

// Messages shared with the compiler's hidden %this binding.
const (
	errSuperNotCalled = "Must call super constructor in derived class before accessing 'this' or returning from derived constructor"
	errSuperTwice     = "Super constructor may only be called once"
)

// constructNT is [[Construct]] for class constructors and functions that
// read new.target (interpConstruct dispatches here).
func (r *Realm) constructNT(fn *Object, fd *FunctionData, args []Value, newTarget *Object) (Value, error) {
	var obj *Object
	this := Undefined()
	if fd.code.Kind != bytecode.KindDerivedCtor {
		o, err := r.OrdinaryCreateFromConstructor(newTarget, r.ObjectPrototype, ClassObject)
		if err != nil {
			return Undefined(), err
		}
		obj, this = o, ObjectValue(o)
	}
	r.newTarget = newTarget
	res, err := r.enterFrame(fn, fd, this, args)
	r.newTarget = nil
	if err != nil {
		return Undefined(), err
	}
	if obj == nil || res.IsObject() {
		return res, nil // DerivedResult made it an object
	}
	return this, nil
}

// classOp executes one class op of the frame whose registers start at base
// and returns the next pc.
func (r *Realm) classOp(fd *FunctionData, base int, w uint32, pc int) (int, error) {
	st := &r.interp
	code, meta := fd.code, fd.meta
	top := base + int(code.NumRegs)
	regs := st.stack[base:top:top]
	a, b, c := int(uint8(w>>8)), int(uint8(w>>16)), int(uint8(w>>24))
	switch bytecode.Op(w) {
	case bytecode.CtorEntry:
		nt := r.newTarget
		if nt == nil {
			return pc, r.TypeError("Class constructor %s cannot be invoked without 'new'", fd.name.GoString())
		}
		r.newTarget = nil
		regs[a] = ObjectValue(nt)
	case bytecode.LoadNewTarget:
		regs[a] = Undefined()
		if nt := r.newTarget; nt != nil {
			r.newTarget = nil
			regs[a] = ObjectValue(nt)
		}
	case bytecode.LoadHome:
		regs[a] = Undefined()
		if h := fd.HomeObject(); h != nil {
			regs[a] = ObjectValue(h)
		}
	case bytecode.SetHome:
		regs[a].AsObject().internal.(*FunctionData).SetHomeObject(regs[b].AsObject())
	case bytecode.CreateClass:
		f := regs[a].AsObject()
		if c&2 != 0 {
			key, err := r.ToPropertyKey(regs[a+1])
			if err != nil {
				return pc, err
			}
			name, err := r.keyFunctionName(key, "")
			if err != nil {
				return pc, err
			}
			r.setFunctionName(f, name)
		}
		return pc, r.createClass(f, regs[b], c&1 != 0, base+a+1)
	case bytecode.DefineClassMethod:
		return pc, r.defineClassMethod(regs[a].AsObject(), regs[b], regs[c].AsObject())
	case bytecode.ToPropertyKey:
		key, err := r.ToPropertyKey(regs[b])
		if err != nil {
			return pc, err
		}
		st.stack[base+a] = key.Value()
	case bytecode.GetProtoOf:
		p := Null()
		if v := regs[b]; v.IsObject() && v.AsObject().proto != nil {
			p = ObjectValue(v.AsObject().proto)
		}
		regs[a] = p
	case bytecode.GetSuper:
		v, err := r.getSuper(regs[b], regs[b+1], regs[c])
		if err != nil {
			return pc, err
		}
		st.stack[base+a] = v
	case bytecode.SetSuper:
		return pc, r.setSuper(regs[a], regs[a+1], regs[b], regs[c])
	case bytecode.SuperCall:
		args := regs[a+2 : a+2+b : a+2+b]
		res, err := r.superCall(regs[a], regs[a+1], args)
		if err != nil {
			return pc, err
		}
		st.stack[base+a] = res
	case bytecode.SuperCallSpread:
		argv, err := r.spreadArgs(top, regs[a+2])
		if err != nil {
			return pc, err
		}
		regs = st.stack[base:top:top]
		st.sp = top + len(argv) // keep the callee's window above the argument list
		res, err := r.superCall(regs[a], regs[a+1], argv)
		st.sp = top
		if err != nil {
			return pc, err
		}
		st.stack[base+a] = res
	case bytecode.CheckSuper:
		if !regs[a].IsHole() {
			return pc, r.ReferenceError(errSuperTwice)
		}
	case bytecode.DerivedResult:
		switch v := regs[a]; {
		case v.IsObject():
		case v.IsUndefined():
			if regs[b].IsHole() {
				return pc, r.ReferenceError(errSuperNotCalled)
			}
			regs[a] = regs[b]
		default:
			return pc, r.TypeError("Derived constructors may only return object or undefined")
		}
	case bytecode.NewPrivateName:
		regs[a] = privateValue(newPrivateName(meta.consts[w>>16].AsString()))
	case bytecode.SetPrivateMethod:
		flags := code.Code[pc]
		pc++
		definePrivateMethod(regs[a].asPrivate(), regs[b].AsObject(), regs[c], flags)
	case bytecode.GetPrivate:
		v, err := r.privateGet(regs[b], regs[c].asPrivate())
		if err != nil {
			return pc, err
		}
		st.stack[base+a] = v
	case bytecode.SetPrivate:
		return pc, r.privateSet(regs[a], regs[b].asPrivate(), regs[c])
	case bytecode.DefPrivate:
		return pc, r.privateDefine(regs[a], regs[b].asPrivate(), regs[c])
	case bytecode.AddBrand:
		return pc, r.privateAddBrand(regs[a], regs[b].asPrivate())
	case bytecode.InPrivate:
		ok, err := r.privateIn(regs[c], regs[b].asPrivate())
		if err != nil {
			return pc, err
		}
		regs[a] = Bool(ok)
	case bytecode.ThrowError:
		msg := meta.consts[w>>16].AsString().GoString()
		if uint8(a) == bytecode.ThrowReferenceError {
			return pc, r.ReferenceError("%s", msg)
		}
		return pc, r.TypeError("%s", msg)
	default:
		return r.genOp(fd, base, w, pc) // generator ops (generator.go)
	}
	return pc, nil
}

// createClass completes ClassDefinitionEvaluation's object setup for the
// constructor F: the prototype object (stored in the register at protoAt),
// the heritage links, F.prototype and prototype.constructor.
func (r *Realm) createClass(f *Object, heritage Value, derived bool, protoAt int) error {
	protoParent := r.ObjectPrototype
	if derived {
		switch {
		case heritage.IsNull():
			protoParent = nil
		case !IsConstructor(heritage):
			return r.TypeError("Class extends value %s is not a constructor or null", r.DisplayString(heritage))
		default:
			sup := heritage.AsObject()
			pp, err := sup.GetProp(r, StringKey(AtomPrototype))
			if err != nil {
				return err
			}
			switch {
			case pp.IsObject():
				protoParent = pp.AsObject()
			case pp.IsNull():
				protoParent = nil
			default:
				return r.TypeError("Class extends value does not have valid prototype property %s", r.DisplayString(pp))
			}
			f.SetPrototypeOf(r, sup)
		}
	}
	proto := r.NewObjectWithProto(protoParent)
	proto.addNamed(r, StringKey(AtomConstructor), propCell{value: ObjectValue(f), attrs: attrHidden})
	f.addNamed(r, StringKey(AtomPrototype), propCell{value: ObjectValue(proto), attrs: 0})
	f.internal.(*FunctionData).SetHomeObject(proto)
	r.interp.stack[protoAt] = ObjectValue(proto)
	return nil
}

// defineClassMethod defines a class method: non-enumerable, with o as its
// [[HomeObject]] and, when the template had no name (a computed key), named
// after the key.
func (r *Realm) defineClassMethod(o *Object, k Value, fn *Object) error {
	key, err := r.ToPropertyKey(k)
	if err != nil {
		return err
	}
	fd := fn.internal.(*FunctionData)
	fd.SetHomeObject(o)
	if fd.name == nil || fd.name.Len() == 0 {
		name, err := r.keyFunctionName(key, "")
		if err != nil {
			return err
		}
		r.setFunctionName(fn, name)
	}
	return o.DefinePropertyOrThrow(r, key, DataDescriptor(ObjectValue(fn), attrHidden))
}

// getSuper reads super[k]: base is the home object's prototype and receiver
// the `this` value (GetValue of a super reference).
func (r *Realm) getSuper(base, receiver, k Value) (Value, error) {
	if !base.IsObject() {
		return Undefined(), r.TypeError("Cannot read properties of %s (reading '%s')", base.String(), k.String())
	}
	key, err := r.ToPropertyKey(k)
	if err != nil {
		return Undefined(), err
	}
	return base.AsObject().Get(r, key, receiver)
}

// setSuper assigns super[k] = v (PutValue of a super reference, strict).
func (r *Realm) setSuper(base, receiver, k, v Value) error {
	if !base.IsObject() {
		return r.TypeError("Cannot set properties of %s (setting '%s')", base.String(), k.String())
	}
	key, err := r.ToPropertyKey(k)
	if err != nil {
		return err
	}
	ok, err := base.AsObject().Set(r, key, v, receiver)
	if err != nil {
		return err
	}
	if !ok {
		return r.TypeError("Cannot assign to read only property '%s' of object", key.GoString())
	}
	return nil
}

// superCall constructs the super constructor fn with new.target nt.
func (r *Realm) superCall(fn, nt Value, args []Value) (Value, error) {
	if !IsConstructor(fn) {
		if fn.IsObject() && fn.AsObject() == r.FunctionPrototype {
			fn = Null() // the parent of `class extends null`
		}
		return Undefined(), r.TypeError("Super constructor %s is not a constructor", r.DisplayString(fn))
	}
	return r.Construct(fn, args, nt.AsObject())
}
