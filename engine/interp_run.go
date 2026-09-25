package engine

import (
	"math"

	"github.com/Calcium-Ion/moejs/bytecode"
)

// errThrown is the in-frame throw marker: the thrown value travels in the
// run loop's local instead of a heap-allocated *Exception, which is created
// only when the exception leaves the frame.
type thrownMarker struct{}

func (thrownMarker) Error() string { return "engine: thrown" }

var errThrown error = thrownMarker{}

// run executes the frame fi of fd whose registers start at base. It returns
// the function's return value or the error that escaped it. No panic/recover
// is involved on the throw path: natives return errors and the loop unwinds
// through the handler table.
func (r *Realm) run(fi int, fd *FunctionData, base int, env *Env, this Value, callee *Object) (Value, error) {
	st := &r.interp
	code := fd.code
	meta := fd.meta
	icBase := fd.icBase
	insns := code.Code
	consts := meta.consts
	top := base + int(code.NumRegs)
	regs := st.stack[base:top:top]
	fr := &st.frames[fi]
	// A call starts at 0, a resumed generator at its saved pc with the
	// environment depth in the high byte (generator.go).
	pc := int(fr.pc)
	envDepth := pc >> genDepthShift
	pc &= 1<<genDepthShift - 1
	var (
		err    error
		thrown Value
	)
	for {
		w := insns[pc]
		ipc := pc
		fr.pc = uint32(ipc)
		pc++
		a := int(uint8(w >> 8))
		switch bytecode.Op(w) {
		// --- loads and moves ---
		case bytecode.LoadConst:
			regs[a] = consts[w>>16]
		case bytecode.LoadInt:
			regs[a] = IntValue(int(int16(w >> 16)))
		case bytecode.LoadUndef:
			regs[a] = Undefined()
		case bytecode.LoadNull:
			regs[a] = Null()
		case bytecode.LoadTrue:
			regs[a] = True()
		case bytecode.LoadFalse:
			regs[a] = False()
		case bytecode.LoadHole:
			regs[a] = Hole()
		case bytecode.LoadThis:
			regs[a] = this
		case bytecode.LoadCallee:
			regs[a] = ObjectValue(callee)
		case bytecode.Move:
			regs[a] = regs[uint8(w>>16)]
		case bytecode.UndefRange:
			u := Undefined()
			for i := range int(uint8(w >> 16)) {
				regs[a+i] = u
			}

		// --- closure environments ---
		case bytecode.GetEnv:
			e := env
			for d := uint8(w >> 16); d > 0; d-- {
				e = e.parent
			}
			regs[a] = e.slots[uint8(w>>24)]
		case bytecode.GetEnvChk:
			e := env
			for d := uint8(w >> 16); d > 0; d-- {
				e = e.parent
			}
			v := e.slots[uint8(w>>24)]
			x := insns[pc]
			pc++
			if v.IsHole() {
				err = r.tdzError(meta, x)
				break
			}
			regs[a] = v
		case bytecode.SetEnv:
			e := env
			for d := uint8(w >> 16); d > 0; d-- {
				e = e.parent
			}
			e.slots[uint8(w>>24)] = regs[a]
		case bytecode.GetEnvW:
			e := env
			for d := uint8(w >> 16); d > 0; d-- {
				e = e.parent
			}
			regs[a] = e.slots[insns[pc]]
			pc++
		case bytecode.GetEnvChkW:
			e := env
			for d := uint8(w >> 16); d > 0; d-- {
				e = e.parent
			}
			v := e.slots[insns[pc]]
			x := insns[pc+1]
			pc += 2
			if v.IsHole() {
				err = r.tdzError(meta, x)
				break
			}
			regs[a] = v
		case bytecode.SetEnvW:
			e := env
			for d := uint8(w >> 16); d > 0; d-- {
				e = e.parent
			}
			e.slots[insns[pc]] = regs[a]
			pc++
		case bytecode.PushEnv:
			env = newEnvSized(env, int(w>>16))
			envDepth++
		case bytecode.PopEnv:
			env = env.parent
			envDepth--
		case bytecode.CopyEnv:
			ne := newEnvSized(env.parent, len(env.slots))
			copy(ne.slots, env.slots)
			env = ne
		case bytecode.CheckTDZ:
			x := insns[pc]
			pc++
			if regs[a].IsHole() {
				err = r.tdzError(meta, x)
			}

		// --- globals ---
		case bytecode.GetGlobal, bytecode.GetGlobalOrUndef:
			x := insns[pc]
			pc++
			g := r.Global
			e := &r.ic[icBase+x>>16]
			if g.shape == e.Shape && r.protoEpoch == e.Epoch {
				if icCensus {
					censusHit(code, bytecode.Op(w), x, ipc, g.shape)
				}
				regs[a] = e.Holder(g).slots[e.Slot()]
				break
			}
			if icCensus {
				censusMiss(code, bytecode.Op(w), x, ipc, e, g.shape)
			}
			var v Value
			v, err = r.getGlobalSlow(meta.keys[uint16(x)], e, bytecode.Op(w) == bytecode.GetGlobalOrUndef)
			regs, fr = st.stack[base:top:top], &st.frames[fi]
			if err == nil {
				regs[a] = v
			}
		case bytecode.SetGlobal:
			x := insns[pc]
			pc++
			g := r.Global
			e := &r.ic[icBase+x>>16]
			if g.shape == e.Shape && r.protoEpoch == e.Epoch {
				if icCensus {
					censusHit(code, bytecode.Op(w), x, ipc, g.shape)
				}
				g.slots[e.Slot()] = regs[a]
				break
			}
			if icCensus {
				censusMiss(code, bytecode.Op(w), x, ipc, e, g.shape)
			}
			err = r.setGlobalSlow(meta.keys[uint16(x)], regs[a], e)
			regs, fr = st.stack[base:top:top], &st.frames[fi]

		// --- properties ---
		case bytecode.GetProp:
			x := insns[pc]
			pc++
			v := regs[uint8(w>>16)]
			if v.IsObject() {
				o := v.AsObject()
				e := &r.ic[icBase+x>>16]
				if o.shape == e.Shape && r.protoEpoch == e.Epoch {
					if icCensus {
						censusHit(code, bytecode.Op(w), x, ipc, o.shape)
					}
					regs[a] = e.Holder(o).slots[e.Slot()]
					break
				}
				if icCensus {
					censusMiss(code, bytecode.Op(w), x, ipc, e, o.shape)
				}
				var res Value
				res, err = r.getNamedSlow(o, v, meta.keys[uint16(x)], e)
				regs, fr = st.stack[base:top:top], &st.frames[fi]
				if err == nil {
					regs[a] = res
				}
				break
			}
			if v.IsString() {
				// Methods of string primitives resolve on String.prototype;
				// the entry caches that lookup keyed by the prototype's shape.
				sp := r.StringPrototype
				e := &r.ic[icBase+x>>16]
				if sp.shape == e.Shape && r.protoEpoch == e.Epoch {
					if icCensus {
						censusHit(code, bytecode.Op(w), x, ipc, sp.shape)
					}
					regs[a] = e.Holder(sp).slots[e.Slot()]
					break
				}
				if icCensus {
					censusMiss(code, bytecode.Op(w), x, ipc, e, sp.shape)
				}
				var res Value
				res, err = r.getStringProp(v, meta.keys[uint16(x)], e)
				regs, fr = st.stack[base:top:top], &st.frames[fi]
				if err == nil {
					regs[a] = res
				}
				break
			}
			var res Value
			res, err = r.getPrimitiveProp(v, meta.keys[uint16(x)])
			regs, fr = st.stack[base:top:top], &st.frames[fi]
			if err == nil {
				regs[a] = res
			}
		case bytecode.GetLen:
			x := insns[pc]
			pc++
			v := regs[uint8(w>>16)]
			if v.IsObject() {
				o := v.AsObject()
				if o.class == ClassArray {
					regs[a] = Uint32Value(o.internal.(*ArrayData).length)
					break
				}
				e := &r.ic[icBase+x>>16]
				if o.shape == e.Shape && r.protoEpoch == e.Epoch {
					if icCensus {
						censusHit(code, bytecode.Op(w), x, ipc, o.shape)
					}
					regs[a] = e.Holder(o).slots[e.Slot()]
					break
				}
				if icCensus {
					censusMiss(code, bytecode.Op(w), x, ipc, e, o.shape)
				}
				var res Value
				res, err = r.getNamedSlow(o, v, lengthKey, e)
				regs, fr = st.stack[base:top:top], &st.frames[fi]
				if err == nil {
					regs[a] = res
				}
				break
			}
			if v.IsString() {
				regs[a] = IntValue(v.AsString().Len())
				break
			}
			var res Value
			res, err = r.getPrimitiveProp(v, lengthKey)
			regs, fr = st.stack[base:top:top], &st.frames[fi]
			if err == nil {
				regs[a] = res
			}
		case bytecode.SetProp:
			x := insns[pc]
			pc++
			t := regs[a]
			v := regs[uint8(w>>16)]
			if t.IsObject() {
				o := t.AsObject()
				e := &r.ic[icBase+x>>16]
				if o.shape == e.Shape && r.protoEpoch == e.Epoch {
					if icCensus {
						censusHit(code, bytecode.Op(w), x, ipc, o.shape)
					}
					o.slots[e.Slot()] = v
					break
				}
				if icCensus {
					censusMiss(code, bytecode.Op(w), x, ipc, e, o.shape)
				}
				err = r.setNamedSlow(o, meta.keys[uint16(x)], v, e)
			} else {
				err = r.SetV(t, meta.keys[uint16(x)], v)
			}
			regs, fr = st.stack[base:top:top], &st.frames[fi]
		case bytecode.GetElem:
			o := regs[uint8(w>>16)]
			k := regs[uint8(w>>24)]
			if o.IsObject() && k.IsNumber() {
				obj := o.AsObject()
				f := k.AsNumber()
				if i := int(f); float64(i) == f && i >= 0 && i < len(obj.elements) && obj.class != ClassString {
					if v := obj.elements[i]; !v.IsHole() {
						regs[a] = v
						break
					}
				}
			}
			var res Value
			res, err = r.getElemSlow(o, k)
			regs, fr = st.stack[base:top:top], &st.frames[fi]
			if err == nil {
				regs[a] = res
			}
		case bytecode.SetElem:
			o := regs[a]
			k := regs[uint8(w>>16)]
			v := regs[uint8(w>>24)]
			if o.IsObject() && k.IsNumber() {
				obj := o.AsObject()
				f := k.AsNumber()
				if i := int(f); float64(i) == f && i >= 0 && i < len(obj.elements) && obj.class == ClassArray && obj.flags&(flagFrozen|flagShared) == 0 {
					if !obj.elements[i].IsHole() {
						obj.elements[i] = v
						break
					}
				}
			}
			err = r.setElemSlow(o, k, v)
			regs, fr = st.stack[base:top:top], &st.frames[fi]
		case bytecode.DelProp:
			x := insns[pc]
			pc++
			var res bool
			res, err = r.deleteSlow(regs[uint8(w>>16)], meta.keys[x].Value())
			regs, fr = st.stack[base:top:top], &st.frames[fi]
			if err == nil {
				regs[a] = Bool(res)
			}
		case bytecode.DelElem:
			var res bool
			res, err = r.deleteSlow(regs[uint8(w>>16)], regs[uint8(w>>24)])
			regs, fr = st.stack[base:top:top], &st.frames[fi]
			if err == nil {
				regs[a] = Bool(res)
			}
		case bytecode.In:
			var res bool
			res, err = r.HasPropertyIn(regs[uint8(w>>16)], regs[uint8(w>>24)])
			regs, fr = st.stack[base:top:top], &st.frames[fi]
			if err == nil {
				regs[a] = Bool(res)
			}
		case bytecode.InstanceOf:
			var res bool
			res, err = r.InstanceOf(regs[uint8(w>>16)], regs[uint8(w>>24)])
			regs, fr = st.stack[base:top:top], &st.frames[fi]
			if err == nil {
				regs[a] = Bool(res)
			}

		// --- literals ---
		case bytecode.NewObject:
			regs[a] = ObjectValue(r.NewObjectCap(int(w >> 16)))
		case bytecode.NewArray:
			regs[a] = ObjectValue(r.NewArrayCap(int(w >> 16)))
		case bytecode.DefineField:
			x := insns[pc]
			pc++
			o := regs[a].AsObject()
			v := regs[uint8(w>>16)]
			e := &r.ic[icBase+x>>16]
			if after := e.Shape; after != nil && after.parent == o.shape && o.flags&(flagIsPrototype|flagExtensible|flagDict) == flagExtensible {
				if icCensus {
					censusHit(code, bytecode.Op(w), x, ipc, o.shape)
				}
				o.shape = after
				o.slots = append(o.slots, v)
				break
			}
			if icCensus {
				censusMiss(code, bytecode.Op(w), x, ipc, e, o.shape)
			}
			err = r.defineFieldSlow(o, meta.keys[uint16(x)], v, e)
			regs, fr = st.stack[base:top:top], &st.frames[fi]
		case bytecode.DefineElem:
			err = r.defineElemSlow(regs[a].AsObject(), regs[uint8(w>>16)], regs[uint8(w>>24)])
			regs, fr = st.stack[base:top:top], &st.frames[fi]
		case bytecode.DefineMethod:
			err = r.defineMethodSlow(regs[a].AsObject(), regs[uint8(w>>16)], regs[uint8(w>>24)])
			regs, fr = st.stack[base:top:top], &st.frames[fi]
		case bytecode.DefineAccessor:
			x := insns[pc]
			pc++
			err = r.defineAccessor(regs[a].AsObject(), regs[uint8(w>>16)], regs[uint8(w>>24)].AsObject(), x)
			regs, fr = st.stack[base:top:top], &st.frames[fi]
		case bytecode.SetProto:
			o := regs[a].AsObject()
			v := regs[uint8(w>>16)]
			if v.IsObject() {
				o.SetPrototypeOf(r, v.AsObject())
			} else if v.IsNull() {
				o.SetPrototypeOf(r, nil)
			}
		case bytecode.CopyDataProps:
			err = r.copyDataProps(regs[a].AsObject(), regs[uint8(w>>16)], Undefined())
			regs, fr = st.stack[base:top:top], &st.frames[fi]
		case bytecode.CopyDataPropsEx:
			err = r.copyDataProps(regs[a].AsObject(), regs[uint8(w>>16)], regs[uint8(w>>24)])
			regs, fr = st.stack[base:top:top], &st.frames[fi]
		case bytecode.ArrayPush:
			arr := regs[a].AsObject()
			arr.Push(r, regs[uint8(w>>16)])
		case bytecode.ArrayHole:
			arr := regs[a].AsObject()
			arr.elements = growWithHoles(arr.elements, len(arr.elements)+1)
			arr.internal.(*ArrayData).length = uint32(len(arr.elements))
		case bytecode.AppendSpread:
			err = r.appendSpread(regs[a].AsObject(), regs[uint8(w>>16)])
			regs, fr = st.stack[base:top:top], &st.frames[fi]
		case bytecode.Closure:
			o, _ := r.newClosure(code.Children[w>>16], meta.children[w>>16], env, this)
			regs[a] = ObjectValue(o)
		case bytecode.NewRegExp:
			// RegExpCreate with %RegExp.prototype% (the constructor's
			// prototype property is immutable, so this equals constructing
			// through %RegExp%).
			var rx *Object
			rx, err = r.NewRegExp(consts[w>>16].AsString(), meta.names[w>>16])
			regs, fr = st.stack[base:top:top], &st.frames[fi]
			if err == nil {
				regs[a] = ObjectValue(rx)
			}
		case bytecode.GetTemplate:
			x := insns[pc]
			pc++
			regs[a] = ObjectValue(r.templateObject(&code.Consts[uint16(x)], &r.ic[icBase+x>>16]))

		// --- arithmetic ---
		case bytecode.Add:
			x, y := regs[uint8(w>>16)], regs[uint8(w>>24)]
			if x.IsNumber() && y.IsNumber() {
				regs[a] = NumberValue(x.AsNumber() + y.AsNumber())
				break
			}
			if x.IsString() && y.IsString() {
				var res Value
				if res, err = r.concatValues(x.AsString(), y.AsString()); err == nil {
					regs[a] = res
				}
				break
			}
			var res Value
			res, err = r.Add(x, y)
			regs, fr = st.stack[base:top:top], &st.frames[fi]
			if err == nil {
				regs[a] = res
			}
		case bytecode.Sub, bytecode.Mul, bytecode.Div, bytecode.Mod, bytecode.Exp:
			x, y := regs[uint8(w>>16)], regs[uint8(w>>24)]
			if x.IsNumber() && y.IsNumber() {
				regs[a] = NumberValue(numericOp(bytecode.Op(w), x.AsNumber(), y.AsNumber()))
				break
			}
			var res Value
			res, err = r.arithSlow(bytecode.Op(w), x, y)
			regs, fr = st.stack[base:top:top], &st.frames[fi]
			if err == nil {
				regs[a] = res
			}
		case bytecode.AddImm:
			x := regs[uint8(w>>16)]
			imm := float64(int8(w >> 24))
			if x.IsNumber() {
				regs[a] = NumberValue(x.AsNumber() + imm)
				break
			}
			var res Value
			res, err = r.Add(x, NumberValue(imm))
			regs, fr = st.stack[base:top:top], &st.frames[fi]
			if err == nil {
				regs[a] = res
			}
		case bytecode.SubImm:
			x := regs[uint8(w>>16)]
			imm := float64(int8(w >> 24))
			if x.IsNumber() {
				regs[a] = NumberValue(x.AsNumber() - imm)
				break
			}
			var res Value
			res, err = r.arithSlow(bytecode.Sub, x, NumberValue(imm))
			regs, fr = st.stack[base:top:top], &st.frames[fi]
			if err == nil {
				regs[a] = res
			}
		case bytecode.Inc, bytecode.Dec:
			x := regs[uint8(w>>16)]
			d := 1.0
			if bytecode.Op(w) == bytecode.Dec {
				d = -1
			}
			if x.IsNumber() {
				regs[a] = NumberValue(x.AsNumber() + d)
				break
			}
			var res Value
			res, err = r.incSlow(x, d)
			regs, fr = st.stack[base:top:top], &st.frames[fi]
			if err == nil {
				regs[a] = res
			}
		case bytecode.Neg:
			x := regs[uint8(w>>16)]
			if x.IsNumber() {
				regs[a] = NumberValue(-x.AsNumber())
				break
			}
			var res Value
			res, err = r.negSlow(x)
			regs, fr = st.stack[base:top:top], &st.frames[fi]
			if err == nil {
				regs[a] = res
			}
		case bytecode.Plus:
			x := regs[uint8(w>>16)]
			if x.IsNumber() {
				regs[a] = x
				break
			}
			var f float64
			f, err = r.ToNumber(x)
			regs, fr = st.stack[base:top:top], &st.frames[fi]
			if err == nil {
				regs[a] = NumberValue(f)
			}
		case bytecode.Not:
			regs[a] = Bool(!ToBoolean(regs[uint8(w>>16)]))
		case bytecode.BitNot:
			x := regs[uint8(w>>16)]
			if x.IsNumber() {
				regs[a] = IntValue(int(^ToInt32Float(x.AsNumber())))
				break
			}
			var res Value
			res, err = r.bitNotSlow(x)
			regs, fr = st.stack[base:top:top], &st.frames[fi]
			if err == nil {
				regs[a] = res
			}
		case bytecode.ToNumeric:
			x := regs[uint8(w>>16)]
			if x.IsNumber() {
				regs[a] = x
				break
			}
			var res Value
			res, err = r.ToNumeric(x)
			regs, fr = st.stack[base:top:top], &st.frames[fi]
			if err == nil {
				regs[a] = res
			}
		case bytecode.ToStr:
			x := regs[uint8(w>>16)]
			if x.IsString() {
				regs[a] = x
				break
			}
			var s *String
			s, err = r.ToString(x)
			regs, fr = st.stack[base:top:top], &st.frames[fi]
			if err == nil {
				regs[a] = StringValue(s)
			}
		case bytecode.Concat:
			x, y := regs[uint8(w>>16)], regs[uint8(w>>24)]
			if x.IsString() && y.IsString() {
				var res Value
				if res, err = r.concatValues(x.AsString(), y.AsString()); err == nil {
					regs[a] = res
				}
				break
			}
			var res Value
			res, err = r.Add(x, y)
			regs, fr = st.stack[base:top:top], &st.frames[fi]
			if err == nil {
				regs[a] = res
			}
		case bytecode.Typeof:
			regs[a] = StringValue(TypeOf(regs[uint8(w>>16)]))
		case bytecode.TypeofIs:
			regs[a] = Bool(typeIndex(regs[uint8(w>>16)]) == uint8(w>>24))
		case bytecode.BitAnd, bytecode.BitOr, bytecode.BitXor, bytecode.Shl, bytecode.Shr, bytecode.UShr:
			x, y := regs[uint8(w>>16)], regs[uint8(w>>24)]
			if x.IsNumber() && y.IsNumber() {
				regs[a] = bitwiseOp(bytecode.Op(w), x.AsNumber(), y.AsNumber())
				break
			}
			var res Value
			res, err = r.bitwiseSlow(bytecode.Op(w), x, y)
			regs, fr = st.stack[base:top:top], &st.frames[fi]
			if err == nil {
				regs[a] = res
			}

		// --- comparison ---
		case bytecode.StrictEq:
			regs[a] = Bool(StrictEquals(regs[uint8(w>>16)], regs[uint8(w>>24)]))
		case bytecode.StrictNe:
			regs[a] = Bool(!StrictEquals(regs[uint8(w>>16)], regs[uint8(w>>24)]))
		case bytecode.Eq, bytecode.Ne:
			x, y := regs[uint8(w>>16)], regs[uint8(w>>24)]
			var res bool
			if x.Type() == y.Type() {
				res = StrictEquals(x, y)
			} else {
				res, err = r.LooseEquals(x, y)
				regs, fr = st.stack[base:top:top], &st.frames[fi]
				if err != nil {
					break
				}
			}
			regs[a] = Bool(res == (bytecode.Op(w) == bytecode.Eq))
		case bytecode.Lt, bytecode.Le, bytecode.Gt, bytecode.Ge:
			x, y := regs[uint8(w>>16)], regs[uint8(w>>24)]
			if x.IsNumber() && y.IsNumber() {
				fx, fy := x.AsNumber(), y.AsNumber()
				var res bool
				switch bytecode.Op(w) {
				case bytecode.Lt:
					res = fx < fy
				case bytecode.Le:
					res = fx <= fy
				case bytecode.Gt:
					res = fx > fy
				default:
					res = fx >= fy
				}
				regs[a] = Bool(res)
				break
			}
			var res bool
			res, err = r.compareSlow(bytecode.Op(w), x, y)
			regs, fr = st.stack[base:top:top], &st.frames[fi]
			if err == nil {
				regs[a] = Bool(res)
			}

		// --- control flow ---
		case bytecode.Jmp:
			off := int(int16(w >> 16))
			if off < 0 && r.interruptFlag.Load() != 0 {
				return Undefined(), r.interruptError()
			}
			pc += off
		case bytecode.JmpT:
			if ToBoolean(regs[a]) {
				pc += int(int16(w >> 16))
			}
		case bytecode.JmpF:
			if !ToBoolean(regs[a]) {
				pc += int(int16(w >> 16))
			}
		case bytecode.JmpNullish:
			if regs[a].IsNullish() {
				pc += int(int16(w >> 16))
			}
		case bytecode.JmpNotNullish:
			if !regs[a].IsNullish() {
				pc += int(int16(w >> 16))
			}
		case bytecode.JmpNotUndef:
			if !regs[a].IsUndefined() {
				pc += int(int16(w >> 16))
			}

		// --- calls ---
		case bytecode.Call:
			argc := int(uint8(w >> 16))
			var res Value
			res, err = r.callValue(regs[a], regs[a+1], st.stack[base+a+2:base+a+2+argc:base+a+2+argc])
			regs, fr = st.stack[base:top:top], &st.frames[fi]
			if err == nil {
				regs[a] = res
			}
		case bytecode.CallSpread:
			var argv []Value
			argv, err = r.spreadArgs(top, regs[a+2])
			regs, fr = st.stack[base:top:top], &st.frames[fi]
			if err != nil {
				break
			}
			var res Value
			st.sp = top + len(argv) // keep the callee's window above the argument list
			res, err = r.callValue(regs[a], regs[a+1], argv)
			st.sp = top
			regs, fr = st.stack[base:top:top], &st.frames[fi]
			if err == nil {
				regs[a] = res
			}
		case bytecode.New:
			argc := int(uint8(w >> 16))
			var res Value
			res, err = r.constructValue(regs[a], st.stack[base+a+2:base+a+2+argc:base+a+2+argc])
			regs, fr = st.stack[base:top:top], &st.frames[fi]
			if err == nil {
				regs[a] = res
			}
		case bytecode.NewSpread:
			var argv []Value
			argv, err = r.spreadArgs(top, regs[a+2])
			regs, fr = st.stack[base:top:top], &st.frames[fi]
			if err != nil {
				break
			}
			var res Value
			st.sp = top + len(argv)
			res, err = r.constructValue(regs[a], argv)
			st.sp = top
			regs, fr = st.stack[base:top:top], &st.frames[fi]
			if err == nil {
				regs[a] = res
			}
		case bytecode.Ret:
			return regs[a], nil
		case bytecode.RetUndef:
			return Undefined(), nil
		case bytecode.Throw:
			thrown = regs[a]
			err = errThrown
		case bytecode.ThrowConstAssign:
			pc++
			err = r.TypeError("Assignment to constant variable.")
		case bytecode.RequireObjectCoercible:
			if v := regs[a]; v.IsNullish() {
				err = r.TypeError("Cannot destructure '%s' as it is %s.", v.String(), v.String())
			}

		// --- iteration ---
		// Only the index fast paths run inline; iterOp takes the rest.
		case bytecode.IterInit:
			if v := regs[uint8(w>>16)]; r.fastIterable(v) {
				regs[a] = v
				regs[a+1] = IntValue(0)
				break
			}
			pc, err = r.iterOp(fd, base, w, pc)
			regs, fr = st.stack[base:top:top], &st.frames[fi]
		case bytecode.IterNext:
			if it, pos := regs[a], regs[a+1]; it.IsObject() && pos.IsNumber() { // array fast mode
				o := it.AsObject()
				if i := int(pos.AsNumber()); i < len(o.elements) && i < int(o.internal.(*ArrayData).length) {
					if v := o.elements[i]; !v.IsHole() {
						regs[a+1] = IntValue(i + 1)
						regs[a+2] = v
						break
					}
				}
			}
			pc, err = r.iterOp(fd, base, w, pc)
			regs, fr = st.stack[base:top:top], &st.frames[fi]
		case bytecode.IterValue, bytecode.IterRest, bytecode.IterClose:
			pc, err = r.iterOp(fd, base, w, pc)
			regs, fr = st.stack[base:top:top], &st.frames[fi]
		case bytecode.IterThrow:
			thrown = regs[uint8(w>>16)]
			pc, err = r.iterOp(fd, base, w, pc)
			regs, fr = st.stack[base:top:top], &st.frames[fi]
			if err == nil {
				err = errThrown
			}
		case bytecode.ForInInit:
			var keys, obj Value
			keys, obj, err = r.forInInit(regs[uint8(w>>16)])
			regs, fr = st.stack[base:top:top], &st.frames[fi]
			if err == nil {
				regs[a] = keys
				regs[a+1] = IntValue(0)
				regs[a+2] = obj
			}
		case bytecode.ForInNext:
			k, ok := forInNext(&regs[a], &regs[a+1], regs[a+2])
			if !ok {
				pc += int(int16(w >> 16))
				break
			}
			regs[a+3] = k
		default:
			pc, err = r.classOp(fd, base, w, pc) // class ops, off the jump table (class.go)
			regs, fr = st.stack[base:top:top], &st.frames[fi]
		}

		if err == nil {
			continue
		}
		// --- unwinding ---
		if err != errThrown {
			switch e := err.(type) {
			case *InterruptedError:
				return Undefined(), err
			case *Exception:
				thrown = e.Value
			case *genFrame:
				return e.suspend(env, envDepth), nil // a generator suspends
			default:
				thrown = r.hostErrorValue(err)
			}
		}
		h := findHandler(code.Handlers, uint32(ipc))
		if h == nil {
			if err == errThrown {
				return Undefined(), &Exception{Value: thrown}
			}
			if _, ok := err.(*Exception); ok {
				return Undefined(), err
			}
			return Undefined(), &Exception{Value: thrown}
		}
		for envDepth > int(h.StackDepth) {
			env = env.parent
			envDepth--
		}
		regs, fr = st.stack[base:top:top], &st.frames[fi]
		if h.Kind == bytecode.HandlerCatch {
			regs[h.Reg] = thrown
		} else {
			regs[h.Reg] = IntValue(1)
			regs[h.Reg+1] = thrown
		}
		pc = int(h.Handler)
		err = nil
	}
}

// findHandler returns the innermost handler row covering pc, or nil.
func findHandler(handlers []bytecode.Handler, pc uint32) *bytecode.Handler {
	for i := range handlers {
		h := &handlers[i]
		if pc >= h.Start && pc < h.End {
			return h
		}
	}
	return nil
}

// typeIndex maps a value to its bytecode.TypeNames index (typeof category).
func typeIndex(v Value) uint8 {
	switch v.Type() {
	case TypeUndefined:
		return bytecode.TypeUndefined
	case TypeNull:
		return bytecode.TypeObject
	case TypeBoolean:
		return bytecode.TypeBoolean
	case TypeNumber:
		return bytecode.TypeNumber
	case TypeString:
		return bytecode.TypeString
	case TypeSymbol:
		return bytecode.TypeSymbol
	case TypeBigInt:
		return bytecode.TypeBigInt
	case TypeObject:
		if v.AsObject().IsCallable() {
			return bytecode.TypeFunction
		}
		return bytecode.TypeObject
	}
	return bytecode.TypeUndefined
}

// numericOp applies a binary arithmetic operator to two numbers.
func numericOp(op bytecode.Op, x, y float64) float64 {
	switch op {
	case bytecode.Sub:
		return x - y
	case bytecode.Mul:
		return x * y
	case bytecode.Div:
		return x / y
	case bytecode.Mod:
		return math.Mod(x, y)
	case bytecode.Exp:
		return jsPow(x, y)
	}
	return x + y
}

// jsPow implements Number::exponentiate, which differs from math.Pow for
// |base| == 1 with an infinite exponent (NaN in JavaScript).
func jsPow(x, y float64) float64 {
	if y != y {
		return math.NaN()
	}
	if y == 0 {
		return 1
	}
	if math.IsInf(y, 0) && (x == 1 || x == -1) {
		return math.NaN()
	}
	return math.Pow(x, y)
}

// bitwiseOp applies a bitwise or shift operator to two numbers.
func bitwiseOp(op bytecode.Op, x, y float64) Value {
	a, b := ToInt32Float(x), ToInt32Float(y)
	switch op {
	case bytecode.BitAnd:
		return IntValue(int(a & b))
	case bytecode.BitOr:
		return IntValue(int(a | b))
	case bytecode.BitXor:
		return IntValue(int(a ^ b))
	case bytecode.Shl:
		return IntValue(int(a << (uint32(b) & 31)))
	case bytecode.Shr:
		return IntValue(int(a >> (uint32(b) & 31)))
	}
	return NumberValue(float64(uint32(a) >> (uint32(b) & 31)))
}
