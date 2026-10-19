package bytecode

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRegisterSpansCoverEveryOp checks that VerifyRegisters knows the
// register layout of every opcode, so a new op without one fails here
// rather than run unchecked.
func TestRegisterSpansCoverEveryOp(t *testing.T) {
	for op := Op(0); int(op) < OpCount; op++ {
		assert.True(t, RegisterSpans(op, EncodeABC(op, 1, 2, 1), 4, func(int, int) {}), "%s has no register layout", op)
	}
	assert.False(t, RegisterSpans(Op(OpCount), 0, 0, func(int, int) {}))
}

// TestJumps pins the ops whose sBx operand is a jump: a new op with an sBx
// that is a value, as LoadInt's, must be left out of Jumps, or the
// verifier rejects every function that uses it.
func TestJumps(t *testing.T) {
	want := map[Op]bool{Jmp: true, JmpT: true, JmpF: true, JmpNullish: true, JmpNotNullish: true, JmpNotUndef: true,
		IterNext: true, IterClose: true, ForInNext: true, AsyncIterResult: true, AsyncIterClose: true, JmpWith: true}
	for op := JmpLt; op <= JmpStrictNe; op++ {
		want[op], want[op.Immediate()] = true, true
	}
	for op := Op(0); int(op) < OpCount; op++ {
		assert.Equal(t, want[op], op.Jumps(), "%s", op)
	}
	assert.Equal(t, 10, JumpTarget(4, EncodeAsBx(Jmp, 0, 5)))
	assert.Equal(t, 0, JumpTarget(4, EncodeAsBx(JmpLtI, 0, -5)), "counted from the X word")
}

// TestVerifyRegisters checks the bounds of single registers, runs and the
// X word's register, and of handler registers.
func TestVerifyRegisters(t *testing.T) {
	fn := func(n int, code ...uint32) *Function { return &Function{Name: "f", NumRegs: uint16(n), Code: code} }
	ret := EncodeABC(RetUndef, 0, 0, 0)
	cases := []struct {
		name string
		n    int
		code []uint32
		ok   bool
	}{
		{"move in range", 6, []uint32{EncodeABC(Move, 5, 0, 0), EncodeABC(Ret, 5, 0, 0)}, true},
		{"move past", 5, []uint32{EncodeABC(Move, 5, 0, 0)}, false},
		{"source past", 5, []uint32{EncodeABC(Move, 0, 5, 0)}, false},
		{"add C past", 3, []uint32{EncodeABC(Add, 0, 1, 3)}, false},
		{"call block", 7, []uint32{EncodeABC(Call, 3, 2, 0)}, true},
		{"call block past", 6, []uint32{EncodeABC(Call, 3, 2, 0)}, false},
		{"plain call block past", 6, []uint32{EncodeABC(Call, 3, 2, CallUndefThis)}, false},
		{"spread call", 6, []uint32{EncodeABC(CallSpread, 3, 0, 0)}, true},
		{"spread call past", 5, []uint32{EncodeABC(CallSpread, 3, 0, 0)}, false},
		{"direct eval", 5, []uint32{EncodeABC(CallEval, 3, 0, 0), 0}, true},
		{"spread direct eval past", 5, []uint32{EncodeABC(CallEval, 3, 0, 1), 0}, false},
		{"spread direct eval, C 2, past", 5, []uint32{EncodeABC(CallEval, 3, 0, 2), 0}, false},
		{"spread direct eval, C 2", 6, []uint32{EncodeABC(CallEval, 3, 0, 2), 0}, true},
		{"undef range", 4, []uint32{EncodeABC(UndefRange, 1, 3, 0)}, true},
		{"undef range past", 4, []uint32{EncodeABC(UndefRange, 2, 3, 0)}, false},
		{"empty undef range", 1, []uint32{EncodeABC(UndefRange, 1, 0, 0)}, true},
		{"for-in record", 4, []uint32{EncodeABC(ForInInit, 0, 0, 0), EncodeAsBx(ForInNext, 0, 0), ret}, true},
		{"for-in record past", 3, []uint32{EncodeAsBx(ForInNext, 0, 0), ret}, false},
		{"compare X register", 3, []uint32{EncodeAsBx(JmpLt, 0, 1), 3, ret}, false},
		{"compare immediate", 1, []uint32{EncodeAsBx(JmpLtI, 0, 1), 300, ret}, true},
		{"element ref X", 4, []uint32{EncodeABC(GetElemRef, 0, 1, 2), 4}, false},
		{"missing extra word", 4, []uint32{EncodeAsBx(JmpLt, 0, 1)}, false},
		{"unknown op", 4, []uint32{uint32(OpCount)}, true},
		{"no registers", 0, []uint32{ret}, true},
	}
	for _, c := range cases {
		err := VerifyRegisters(fn(c.n, c.code...))
		assert.Equal(t, c.ok, err == nil, "%s: %v", c.name, err)
	}
	f := fn(3, ret)
	f.Handlers = []Handler{{Kind: HandlerFinally, Reg: 1}}
	assert.NoError(t, VerifyRegisters(f))
	f.Handlers = []Handler{{Kind: HandlerFinally, Reg: 2}}
	assert.Error(t, VerifyRegisters(f), "a finally handler writes two registers")
	f.Handlers = []Handler{{Kind: HandlerCatch, Reg: 3}}
	assert.Error(t, VerifyRegisters(f))
}

// TestVerifyHandlerKinds checks that a handler row is a catch or a
// finally: the engine enters a row of any other kind as a finally, writing
// R[Reg+1] too, which a check that sized it as a catch would let past the
// window (Reg = NumRegs-1) and the kind proof would let keep its facts.
func TestVerifyHandlerKinds(t *testing.T) {
	ret := EncodeABC(RetUndef, 0, 0, 0)
	for _, k := range []HandlerKind{2, 3, 255} {
		for _, reg := range []uint16{0, 2} {
			f := &Function{Name: "f", NumRegs: 3, Code: []uint32{ret}, Handlers: []Handler{{Kind: k, Reg: reg}}}
			assert.ErrorContains(t, VerifyRegisters(f), "neither a catch nor a finally", "kind %d, register %d", k, reg)
		}
	}
}

// TestVerifyWideFrames checks that NumRegs is at most MaxRegister+1, the
// window's registers, before anything indexes a set of registers: a frame
// past it fails with an error however its registers are named (a run past
// 255, a record at A = 255, a handler's register, a call block), with a
// kind operand or without.
func TestVerifyWideFrames(t *testing.T) {
	abc, abx := EncodeABC, EncodeABx
	obj, def, ret := abx(NewObject, 0, 0), []uint32{abc(DefineField, 0, 1, 0), 0}, abc(RetUndef, 0, 0, 0)
	cat := func(parts ...[]uint32) []uint32 { return slices.Concat(parts...) }
	cases := []struct {
		name     string
		code     []uint32
		handlers []Handler
	}{
		{"an UndefRange past 255", cat([]uint32{obj, abc(UndefRange, 250, 20, 0)}, def, []uint32{ret}), nil},
		{"the same without a kind operand", []uint32{obj, abc(UndefRange, 250, 20, 0), ret}, nil},
		{"a for-in record at 255", []uint32{abc(ForInInit, 255, 0, 0), EncodeAsBx(ForInNext, 255, 0), ret}, nil},
		{"a handler register past 255", cat([]uint32{obj}, def, []uint32{ret}), []Handler{{Start: 0, End: 1, Handler: 3, Kind: HandlerFinally, Reg: 280}}},
		{"a call block past 255", cat([]uint32{obj, abc(Call, 250, 20, 0)}, def, []uint32{ret}), nil},
	}
	for _, n := range []int{257, 300, 65535} {
		for _, c := range cases {
			err := VerifyRegisters(&Function{Name: "f", NumRegs: uint16(n), Code: c.code, Handlers: c.handlers})
			assert.ErrorContains(t, err, "more than the 256 of a frame's window", "%s, %d registers", c.name, n)
		}
	}
	assert.NoError(t, VerifyRegisters(&Function{Name: "f", NumRegs: 256, Code: cat([]uint32{obj, abc(UndefRange, 250, 6, 0)}, def, []uint32{ret})}))
}

// TestVerifyJumps checks every jump op against the instruction starts: a
// jump onto an instruction is accepted, one onto the X word of an
// instruction, past the end or before the first word is not.
func TestVerifyJumps(t *testing.T) {
	ret := EncodeABC(RetUndef, 0, 0, 0)
	// pc 0 is a two-word instruction, pc 2 makes ForInNext's record, pc 3
	// is the jump (with its extra words), then two returns.
	head := []uint32{EncodeAsBx(JmpLtI, 0, 1), 7, EncodeABC(ForInInit, 0, 0, 0)}
	for op := Op(0); int(op) < OpCount; op++ {
		if !op.Jumps() {
			continue
		}
		at, extra := len(head), op.ExtraWords()
		end := at + 1 + extra + 2 // the length of the code
		for _, c := range []struct {
			name   string
			target int
			ok     bool
		}{
			{"onto a return", at + 1 + extra, true},
			{"onto the last return", end - 1, true},
			{"back onto the first instruction", 0, true},
			{"onto the jump itself", at, true},
			{"onto an X word", 1, false},
			{"past the end", end, false},
			{"before the first word", -1, false},
		} {
			code := append(append([]uint32{}, head...), EncodeAsBx(op, 0, int16(c.target-(at+1))))
			for range extra {
				code = append(code, 0)
			}
			code = append(code, ret, ret)
			err := VerifyRegisters(&Function{Name: "f", NumRegs: 8, Code: code})
			assert.Equal(t, c.ok, err == nil, "%s %s: %v", op, c.name, err)
		}
	}
	if extra := JmpLtI.ExtraWords(); extra != 1 {
		t.Fatalf("JmpLtI has %d extra words", extra)
	}

	// The audit's function: a jump onto the X word of an immediate
	// compare, which decodes as Move R[200], R[0].
	x := EncodeABC(Move, 200, 0, 0)
	jump := &Function{Name: "jump", NumRegs: 1, Code: []uint32{EncodeAsBx(Jmp, 0, 1), EncodeAsBx(JmpLtI, 0, 1), x, ret}}
	assert.ErrorContains(t, VerifyRegisters(jump), "Jmp jumps to 2, which starts no instruction")
	jump.Code[0] = EncodeAsBx(Jmp, 0, 2)
	assert.NoError(t, VerifyRegisters(jump))
}

// TestVerifyHandlers checks a handler's pc against the instruction starts
// and its range against the code.
func TestVerifyHandlers(t *testing.T) {
	ret := EncodeABC(RetUndef, 0, 0, 0)
	x := EncodeABC(Move, 200, 0, 0)
	code := []uint32{EncodeABC(Throw, 0, 0, 0), EncodeAsBx(JmpLtI, 0, 1), x, ret}
	for _, c := range []struct {
		name string
		h    Handler
		ok   bool
	}{
		{"a catch", Handler{Start: 0, End: 1, Handler: 3, Kind: HandlerCatch}, true},
		{"onto the X word", Handler{Start: 0, End: 1, Handler: 2, Kind: HandlerCatch}, false},
		{"past the end", Handler{Start: 0, End: 1, Handler: 4, Kind: HandlerCatch}, false},
		{"the whole code", Handler{Start: 0, End: 4, Handler: 3, Kind: HandlerCatch}, true},
		{"a range past the end", Handler{Start: 0, End: 5, Handler: 3, Kind: HandlerCatch}, false},
		{"a range that ends before it starts", Handler{Start: 2, End: 1, Handler: 3, Kind: HandlerCatch}, false},
	} {
		err := VerifyRegisters(&Function{Name: "f", NumRegs: 1, Code: code, Handlers: []Handler{c.h}})
		assert.Equal(t, c.ok, err == nil, "%s: %v", c.name, err)
	}
}

// TestVerifyModuleBody checks a module's body pc against the instruction
// starts.
func TestVerifyModuleBody(t *testing.T) {
	ret := EncodeABC(RetUndef, 0, 0, 0)
	code := []uint32{EncodeABC(LoadUndef, 0, 0, 0), EncodeAsBx(JmpLtI, 0, 1), 7, ret}
	for _, c := range []struct {
		body int
		ok   bool
	}{{0, true}, {1, true}, {3, true}, {2, false}, {4, false}, {-1, false}} {
		fn := &Function{Name: "m", Kind: KindModule, NumRegs: 1, Code: code, Module: &Module{BodyPC: c.body}}
		err := VerifyRegisters(fn)
		assert.Equal(t, c.ok, err == nil, "body pc %d: %v", c.body, err)
	}
}

// TestVerifyResumablePCs checks that a function that can suspend or rerun
// stays below 1<<PCBits words: the engine packs a saved pc with the
// environment depth above them. The other words are no opcode, which the
// walk skips.
func TestVerifyResumablePCs(t *testing.T) {
	if testing.Short() {
		t.Skip("verifies functions of 16 M words")
	}
	saves := []Op{GenStart, Yield, AsyncGenStart, Await, AsyncYield, CoerceThis, MapArguments, CallEval}
	for op := Op(0); int(op) < OpCount; op++ {
		assert.Equal(t, slices.Contains(saves, op), op.SavesPC(), "%s", op)
	}
	code := make([]uint32, 1<<PCBits)
	for i := range code {
		code[i] = uint32(OpCount)
	}
	for _, op := range append(saves, Move) {
		code[0], code[1] = EncodeABC(op, 0, 1, 0), uint32(OpCount)
		if op.ExtraWords() > 0 {
			code[1] = 0
		}
		long := &Function{Name: "g", NumRegs: 3, Code: code}
		if op.SavesPC() {
			assert.ErrorContains(t, VerifyRegisters(long), "suspends or reruns", "%s", op)
		} else {
			assert.NoError(t, VerifyRegisters(long), "%s never resumes", op)
		}
		long.Code = code[:len(code)-1]
		assert.NoError(t, VerifyRegisters(long), "%s", op)
	}
}

// TestVerifyConstOperands checks the constants the engine reads as a
// given kind without looking: NewRegExp's must be a regular expression,
// NewPrivateName's and ThrowError's a string, and inside the pool.
func TestVerifyConstOperands(t *testing.T) {
	consts := []Const{{Kind: ConstString, Str: "s"}, {Kind: ConstRegExp, Str: "a", Flags: "g"}, {Kind: ConstNumber, Num: 1}}
	ret := EncodeABC(RetUndef, 0, 0, 0)
	for _, c := range []struct {
		op Op
		bx uint16
		ok bool
	}{
		{NewRegExp, 1, true}, {NewRegExp, 0, false}, {NewRegExp, 2, false}, {NewRegExp, 3, false},
		{NewPrivateName, 0, true}, {NewPrivateName, 1, false}, {NewPrivateName, 2, false}, {NewPrivateName, 9, false},
		{ThrowError, 0, true}, {ThrowError, 2, false}, {ThrowError, 3, false},
		{LoadConst, 2, true}, // any kind
	} {
		fn := &Function{Name: "f", NumRegs: 1, Consts: consts, Code: []uint32{EncodeABx(c.op, 0, c.bx), ret}}
		err := VerifyRegisters(fn)
		assert.Equal(t, c.ok, err == nil, "%s K%d: %v", c.op, c.bx, err)
	}
}
