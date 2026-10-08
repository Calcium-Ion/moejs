package engine

import "github.com/Calcium-Ion/moejs/bytecode"

// coldOp executes one op that the interpreter loop leaves to its default
// case, and returns the next pc. These are ops that are rare in loops (TDZ
// and import reads, wide environment slots, object-literal shapes beyond
// plain fields, spread calls, for-in setup) or that call a slow helper on
// every execution anyway (delete, in, instanceof), so the extra call costs
// little next to their work. Keeping them out of (*Realm).run keeps it
// below the compiler's big-function size (DESIGN §6). Ops it does not
// handle go on to classOp.
//
// Everything here may re-enter the interpreter: a result is written through
// st.stack after any call, never through a register slice taken before it.
func (r *Realm) coldOp(fd *FunctionData, base int, env *Env, w uint32, pc int) (int, error) {
	st := &r.interp
	code, meta := fd.code, fd.meta
	insns := code.Code
	top := base + int(code.NumRegs)
	regs := st.stack[base:top:top]
	a, b, c := int(uint8(w>>8)), int(uint8(w>>16)), int(uint8(w>>24))
	switch bytecode.Op(w) {
	// --- closure environments and imports ---
	case bytecode.GetEnvChk:
		v := env.up(uint8(b)).slots[c]
		if v.IsHole() {
			return pc + 1, r.tdzError(meta, insns[pc])
		}
		regs[a] = v
		return pc + 1, nil
	case bytecode.GetEnvW:
		regs[a] = env.up(uint8(b)).slots[insns[pc]]
		return pc + 1, nil
	case bytecode.GetEnvChkW:
		v := env.up(uint8(b)).slots[insns[pc]]
		if v.IsHole() {
			return pc + 2, r.tdzError(meta, insns[pc+1])
		}
		regs[a] = v
		return pc + 2, nil
	case bytecode.SetEnvW:
		env.up(uint8(b)).slots[insns[pc]] = regs[a]
		return pc + 1, nil
	case bytecode.GetImport:
		v := *env.up(uint8(b)).slots[c].importTarget()
		if v.IsHole() {
			return pc + 1, r.tdzError(meta, insns[pc])
		}
		regs[a] = v
		return pc + 1, nil
	case bytecode.GetImportW:
		v := *env.up(uint8(b)).slots[insns[pc]].importTarget()
		if v.IsHole() {
			return pc + 2, r.tdzError(meta, insns[pc+1])
		}
		regs[a] = v
		return pc + 2, nil
	case bytecode.CheckTDZ:
		if regs[a].IsHole() {
			return pc + 1, r.tdzError(meta, insns[pc])
		}
		return pc + 1, nil

	// --- properties ---
	case bytecode.DelProp:
		res, err := r.deleteSlow(regs[b], meta.keys[insns[pc]].Value())
		if err == nil {
			st.stack[base+a] = Bool(res)
		}
		return pc + 1, err
	case bytecode.DelElem:
		res, err := r.deleteSlow(regs[b], regs[c])
		if err == nil {
			st.stack[base+a] = Bool(res)
		}
		return pc, err
	case bytecode.In:
		res, err := r.HasPropertyIn(regs[b], regs[c])
		if err == nil {
			st.stack[base+a] = Bool(res)
		}
		return pc, err
	case bytecode.InstanceOf:
		res, err := r.InstanceOf(regs[b], regs[c])
		if err == nil {
			st.stack[base+a] = Bool(res)
		}
		return pc, err
	case bytecode.GetElemRef:
		// Past run's dense element read.
		k, v, err := r.getElemRef(regs[b], regs[uint8(insns[pc])])
		if err != nil {
			return pc + 1, err
		}
		st.stack[base+c] = k
		st.stack[base+a] = v
		return pc + 1, nil

	// --- literals ---
	case bytecode.DefineElem:
		return pc, r.defineElemSlow(regs[a].AsObject(), regs[b], regs[c])
	case bytecode.DefineMethod:
		return pc, r.defineMethodSlow(regs[a].AsObject(), regs[b], regs[c])
	case bytecode.DefineAccessor:
		return pc + 1, r.defineAccessor(regs[a].AsObject(), regs[b], regs[c].AsObject(), insns[pc])
	case bytecode.SetProto:
		o := regs[a].AsObject()
		if v := regs[b]; v.IsObject() {
			o.SetPrototypeOf(r, v.AsObject())
		} else if v.IsNull() {
			o.SetPrototypeOf(r, nil)
		}
		return pc, nil
	case bytecode.CopyDataProps:
		return pc, r.copyDataProps(regs[a].AsObject(), regs[b], Undefined())
	case bytecode.CopyDataPropsEx:
		return pc, r.copyDataProps(regs[a].AsObject(), regs[b], regs[c])
	case bytecode.ArrayHole:
		arr := regs[a].AsObject()
		arr.elements = r.growWithHoles(arr.elements, len(arr.elements)+1)
		arr.internal.(*ArrayData).length = uint32(len(arr.elements))
		return pc, nil
	case bytecode.AppendSpread:
		return pc, r.appendSpread(regs[a].AsObject(), regs[b])
	case bytecode.NewRegExp:
		// RegExpCreate with %RegExp.prototype% (the constructor's
		// prototype property is immutable, so this equals constructing
		// through %RegExp%).
		bx := w >> 16
		rx, err := r.NewRegExp(meta.consts[bx].AsString(), meta.names[bx])
		if err == nil {
			st.stack[base+a] = ObjectValue(rx)
		}
		return pc, err
	case bytecode.GetTemplate:
		x := insns[pc]
		regs[a] = ObjectValue(r.templateObject(&code.Consts[uint16(x)], &r.ic[fd.icBase+x>>16]))
		return pc + 1, nil

	// --- calls ---
	case bytecode.CallSpread, bytecode.NewSpread:
		argv, err := r.spreadArgs(top, regs[a+2])
		if err != nil {
			return pc, err
		}
		regs = st.stack[base:top:top]
		var res Value
		st.sp = top + len(argv) // keep the callee's window above the argument list
		if bytecode.Op(w) == bytecode.CallSpread {
			res, err = r.callValue(regs[a], regs[a+1], argv)
		} else {
			res, err = r.constructValue(regs[a], argv)
		}
		st.sp = top
		if err == nil {
			st.stack[base+a] = res
		}
		return pc, err
	case bytecode.ThrowConstAssign:
		return pc + 1, r.TypeError("Assignment to constant variable.")
	case bytecode.RequireObjectCoercible:
		if v := regs[a]; v.IsNullish() {
			return pc, r.TypeError("Cannot destructure '%s' as it is %s.", v.String(), v.String())
		}
		return pc, nil

	// --- iteration ---
	case bytecode.ForInInit:
		keys, obj, err := r.forInInit(regs[b])
		if err == nil {
			regs = st.stack[base:top:top]
			regs[a] = keys
			regs[a+1] = IntValue(0)
			regs[a+2] = obj
		}
		return pc, err
	}
	return r.classOp(fd, base, w, pc) // class ops (class.go)
}
