package bytecode

import "fmt"

// PCBits bounds the pcs the engine stores to run a frame from later: a
// suspended generator's or async function's resume pc, the pc a frame
// reruns from, and a module body's first pc (Module.BodyPC). The engine
// packs the frame's environment depth above them.
const PCBits = 24

// VerifyRegisters checks what the engine's interpreter relies on to index
// a frame's registers without bounds checks. It walks fn.Code from pc 0,
// one instruction and its extra words at a time, and checks that
//
//   - every register an instruction names, and every run of registers it
//     reads or writes, lies below NumRegs: the operands A, B, C and the X
//     word's register where they are registers, a call's or a
//     construction's callee, this (or new.target) and arguments, the
//     argument array of the spread forms, UndefRange's run, iterator,
//     for-in and async-iterator records, the value and mode pair of a
//     suspension (RegisterSpans), and the registers its handlers write;
//   - the interpreter runs no other instructions than the ones the walk
//     checked: every jump lands on the start of one (Op.Jumps,
//     JumpTarget), and so do every handler and the body of a module
//     (Module.BodyPC). A jump onto an extra word would run it as an
//     instruction whose operands no one checked: the X word of an
//     immediate compare holds an integer of the source;
//   - the pcs the engine stores fit in PCBits: a module body's, and every
//     pc of a function that can suspend or rerun (Op.SavesPC);
//   - each handler's range lies inside the code, and it is a catch or a
//     finally row (the engine enters a row of another kind as a finally);
//   - NumRegs is at most MaxRegister+1, the registers of the window;
//   - the constant an op reads as a given kind is one (constOperand):
//     NewRegExp's a regular expression, NewPrivateName's and ThrowError's
//     a string. The engine reads it as that kind without looking;
//   - so is the register an op reads as a given kind (KindOperands):
//     every path to the op leaves one there (verifyKinds).
//
// A function with an opcode RegisterSpans has no layout for fails. A word
// that is no opcode names no register: the interpreter faults on it before
// it touches one. It does not look at fn's children.
//
// The engine runs it on every function it materializes (engine fillMeta),
// and the checked build of the interpreter (go test -tags moejsregcheck
// ./engine) compares every register access with RegisterSpans and every pc
// it dispatches with the walk's instruction starts.
func VerifyRegisters(fn *Function) error {
	n := int(fn.NumRegs)
	if n > MaxRegister+1 {
		// The interpreter's window has MaxRegister+1 registers, and the
		// computed ones (a handler's, a record's A+1) are checked
		// against it; the compiler never needs more.
		return fmt.Errorf("bytecode: %q: %d registers, more than the %d of a frame's window", fn.Name, n, MaxRegister+1)
	}
	code := fn.Code
	// starts marks the first word of every instruction the walk decodes; a
	// function below 4,096 words keeps it on the stack.
	var buf [64]uint64
	starts := buf[:]
	if k := len(code)>>6 + 1; k > len(buf) {
		starts = make([]uint64, k)
	}
	savesPC, kinds := false, false
	var tracked [4]uint64 // the registers of KindOperands, below NumRegs and so below 256
	for pc := 0; pc < len(code); {
		starts[pc>>6] |= 1 << (pc & 63)
		w := code[pc]
		op := DecodeOp(w)
		if int(op) >= OpCount {
			// The interpreter faults on it before it touches a register.
			pc++
			continue
		}
		extra := op.ExtraWords()
		if pc+extra >= len(code) {
			return fmt.Errorf("bytecode: %q pc %d: %s lacks its extra words", fn.Name, pc, op)
		}
		var x uint32
		if extra > 0 {
			x = code[pc+1]
		}
		var bad error
		ok := RegisterSpans(op, w, x, func(r, k int) {
			if bad == nil && r+k > n {
				bad = fmt.Errorf("bytecode: %q pc %d: %s names registers %d..%d of %d", fn.Name, pc, op, r, r+k-1, n)
			}
		})
		if !ok {
			return fmt.Errorf("bytecode: %q pc %d: no register layout for %s", fn.Name, pc, op)
		}
		if bad != nil {
			return bad
		}
		if want, ok := constOperand(op); ok {
			if bx := int(DecodeBx(w)); bx >= len(fn.Consts) || fn.Consts[bx].Kind != want {
				return fmt.Errorf("bytecode: %q pc %d: %s names constant %d, which is no %s", fn.Name, pc, op, bx, constKindNames[want])
			}
		}
		savesPC = savesPC || op.SavesPC()
		if readsKinds(op) {
			kinds = true
			KindOperands(op, w, func(r int, _ RegKind) { tracked[r>>6] |= 1 << (r & 63) })
		}
		pc += 1 + extra
	}
	// The jumps, once every start is known, and for verifyKinds the
	// first instruction of each block their targets and the instructions
	// after them begin.
	var leadBuf [64]uint64
	lead := leadBuf[:]
	if kinds && len(code)>>6+1 > len(leadBuf) {
		lead = make([]uint64, len(code)>>6+1)
	}
	for pc := 0; pc < len(code); {
		w := code[pc]
		op := DecodeOp(w)
		if int(op) >= OpCount {
			pc++
			continue
		}
		next := pc + 1 + op.ExtraWords()
		if op.Jumps() {
			t := JumpTarget(pc, w)
			if !isStart(starts, len(code), t) {
				return fmt.Errorf("bytecode: %q pc %d: %s jumps to %d, which starts no instruction", fn.Name, pc, op, t)
			}
			if kinds {
				lead[t>>6] |= 1 << (t & 63)
				markStart(lead, len(code), next)
			}
		} else if kinds && ends(op) {
			markStart(lead, len(code), next)
		}
		pc = next
	}
	for i, h := range fn.Handlers {
		if h.Kind != HandlerCatch && h.Kind != HandlerFinally {
			// The engine enters any other row as a finally, writing
			// R[Reg+1] too: no kind is left for it to read otherwise.
			return fmt.Errorf("bytecode: %q handler %d is of kind %d, neither a catch nor a finally", fn.Name, i, h.Kind)
		}
		k := handlerRegs(h.Kind)
		if int(h.Reg)+k > n {
			return fmt.Errorf("bytecode: %q handler %d writes registers %d..%d of %d", fn.Name, i, h.Reg, int(h.Reg)+k-1, n)
		}
		if !isStart(starts, len(code), int(h.Handler)) {
			return fmt.Errorf("bytecode: %q handler %d continues at %d, which starts no instruction", fn.Name, i, h.Handler)
		}
		if h.Start > h.End || int(h.End) > len(code) {
			return fmt.Errorf("bytecode: %q handler %d covers %d..%d of %d words", fn.Name, i, h.Start, h.End, len(code))
		}
	}
	if m := fn.Module; m != nil {
		if !isStart(starts, len(code), m.BodyPC) || m.BodyPC >= 1<<PCBits {
			return fmt.Errorf("bytecode: %q: the module body's pc %d starts no instruction below %d", fn.Name, m.BodyPC, 1<<PCBits)
		}
	}
	if savesPC && len(code) >= 1<<PCBits {
		return fmt.Errorf("bytecode: %q: %d words, which a function that suspends or reruns keeps below %d", fn.Name, len(code), 1<<PCBits)
	}
	if kinds {
		return verifyKinds(fn, starts, lead, &tracked)
	}
	return nil
}

// constOperand reports the kind of the constant K[Bx] that op reads as that
// kind without looking.
func constOperand(op Op) (ConstKind, bool) {
	switch op {
	case NewRegExp:
		return ConstRegExp, true
	case NewPrivateName, ThrowError:
		return ConstString, true
	}
	return 0, false
}

var constKindNames = [...]string{ConstNumber: "number", ConstString: "string", ConstBigInt: "bigint",
	ConstRegExp: "regular expression", ConstTemplate: "template", ConstFunction: "function"}

// handlerRegs is the number of registers the engine writes when it enters
// a handler of kind k: the thrown value for a catch, and before it the
// completion kind for anything else (run).
func handlerRegs(k HandlerKind) int {
	if k == HandlerCatch {
		return 1
	}
	return 2
}

// markStart marks pc in a set of pcs of a function of n words, if pc is
// one of its words.
func markStart(set []uint64, n, pc int) {
	if pc < n {
		set[pc>>6] |= 1 << (pc & 63)
	}
}

// isStart reports whether pc is the start of an instruction of a function
// of n words, whose starts are marked in starts.
func isStart(starts []uint64, n, pc int) bool {
	return pc >= 0 && pc < n && starts[pc>>6]&(1<<(pc&63)) != 0
}

// RegisterSpans calls span(r, k) for each run of k registers from r that
// the instruction w (with its first extra word x) names, and reports
// whether op has a known layout. Operands that are not registers (counts,
// immediates, flags, constant and child indexes, environment slots) are
// left out; a count gives the length of the run it describes. A layout
// may name more than an instruction touches (New's this slot, IterClose's
// next method), never less: the engine's checked build fails on a
// register access a layout leaves out.
func RegisterSpans(op Op, w, x uint32, span func(r, k int)) bool {
	a, b, c := int(DecodeA(w)), int(DecodeB(w)), int(DecodeC(w))
	switch op {
	// No register.
	case PushEnv, PopEnv, CopyEnv, Jmp, RetUndef, ThrowConstAssign, ThrowError, CoerceThis:

	// A.
	case LoadConst, LoadInt, LoadUndef, LoadNull, LoadTrue, LoadFalse, LoadHole, LoadThis, LoadCallee,
		GetEnv, GetEnvChk, SetEnv, GetEnvW, GetEnvChkW, SetEnvW, CheckTDZ,
		GetGlobal, GetGlobalOrUndef, SetGlobal,
		NewObject, NewArray, ArrayHole, Closure, NewRegExp, GetTemplate,
		RequireObjectCoercible,
		JmpT, JmpF, JmpNullish, JmpNotNullish, JmpNotUndef,
		Ret, Throw,
		CtorEntry, LoadNewTarget, LoadHome, CheckSuper, NewPrivateName,
		GenFunc, GenStart, AsyncFunc, AsyncStart, AsyncGenStart,
		GetImport, GetImportW, ImportMeta,
		SetGlobalSloppy, ResolveGlobal, InitGlobal, SetGlobalVar, DelGlobal, JmpWith, MapArguments,
		JmpLtI, JmpLeI, JmpGtI, JmpGeI, JmpNLtI, JmpNLeI, JmpNGtI, JmpNGeI, JmpStrictEqI, JmpStrictNeI:
		span(a, 1)

	// A, B.
	case Move, GetProp, GetLen, SetProp, DelProp, DefineField, SetProto, CopyDataProps, ArrayPush, AppendSpread,
		AddImm, SubImm, Inc, Dec, Neg, Plus, Not, BitNot, ToNumeric, ToStr, Typeof, TypeofIs,
		SetHome, ToPropertyKey, GetProtoOf, DerivedResult, AddBrand,
		AsyncReturn, AsyncThrow, ImportCall,
		SetGlobalRef, SetPropSloppy, DelPropSloppy, ToObject, WithGet, WithSet:
		span(a, 1)
		span(b, 1)

	// A, B, C.
	case GetElem, SetElem, DelElem, In, InstanceOf, DefineElem, DefineMethod, DefineAccessor, CopyDataPropsEx,
		Add, Sub, Mul, Div, Mod, Exp, Concat, BitAnd, BitOr, BitXor, Shl, Shr, UShr,
		Eq, Ne, StrictEq, StrictNe, Lt, Le, Gt, Ge,
		DefineClassMethod, SetPrivateMethod, GetPrivate, SetPrivate, DefPrivate, InPrivate,
		SetElemSloppy, DelElemSloppy:
		span(a, 1)
		span(b, 1)
		span(c, 1)

	// A and the register in the X word.
	case JmpLt, JmpLe, JmpGt, JmpGe, JmpNLt, JmpNLe, JmpNGt, JmpNGe, JmpStrictEq, JmpStrictNe:
		span(a, 1)
		span(int(uint8(x)), 1)
	case GetElemRef:
		span(a, 1)
		span(b, 1)
		span(c, 1)
		span(int(uint8(x)), 1)

	// Runs.
	case UndefRange:
		span(a, b)
	case Call, New, SuperCall: // callee (or constructor), this (or new.target), B arguments
		span(a, 2+b)
	case CallSpread, NewSpread, SuperCallSpread: // callee, this, the argument array
		span(a, 3)
	case CallEval: // spread when C is not 0, as for directEval
		if c != 0 {
			span(a, 3)
		} else {
			span(a, 2+b)
		}
	case CreateClass: // the constructor and its prototype; R[B], the heritage, is read whatever C says
		span(a, 2)
		span(b, 1)
	case GetSuper: // the base and its receiver R[B+1]
		span(a, 1)
		span(b, 2)
		span(c, 1)
	case SetSuper, SetSuperSloppy: // the base and its receiver R[A+1]
		span(a, 2)
		span(b, 1)
		span(c, 1)

	// Iterator records: R[A], R[A+1], and the value in R[A+2] (R[A+3] for
	// for-in, whose record is three registers).
	case IterInit, GenIter, AsyncIterInit:
		span(a, 2)
		span(b, 1)
	case IterNext:
		span(a, 3)
	case IterValue, IterRest, IterThrow:
		span(a, 2)
		span(b, 1)
	case IterClose:
		span(a, 2)
	case ForInInit:
		span(a, 3)
		span(b, 1)
	case ForInNext:
		span(a, 4)
	case AsyncIterNext, AsyncIterResult, AsyncIterClose, AsyncIterClosed:
		span(a, 3)

	// Suspensions: the sent value in R[A] and the resumption mode in R[A+1].
	case Yield, Await, AsyncYield:
		span(a, 2)
		span(b, 1)
	case Delegate:
		span(a, 2)
		span(b, 2)
	case AsyncDelegate:
		span(a, 2)
		span(b, 3)

	default:
		return false
	}
	return true
}
