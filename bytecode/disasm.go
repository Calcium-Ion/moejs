package bytecode

import (
	"strconv"
	"strings"
)

// Disassemble renders fn (and, indented, its children) in a stable text
// form for tests and debugging.
func Disassemble(fn *Function) string {
	var sb strings.Builder
	disassemble(&sb, fn, "")
	return sb.String()
}

func disassemble(sb *strings.Builder, fn *Function, indent string) {
	name := fn.Name
	if name == "" {
		name = "<anonymous>"
	}
	sb.WriteString(indent)
	sb.WriteString("function ")
	sb.WriteString(name)
	sb.WriteString(" (")
	sb.WriteString(fn.Kind.String())
	sb.WriteString(") params=")
	sb.WriteString(strconv.Itoa(int(fn.NumParams)))
	if fn.HasRest {
		sb.WriteString("+rest")
	}
	sb.WriteString(" regs=")
	sb.WriteString(strconv.Itoa(int(fn.NumRegs)))
	sb.WriteString(" env=")
	sb.WriteString(strconv.Itoa(len(fn.CaptureLayout)))
	sb.WriteString(" ics=")
	sb.WriteString(strconv.FormatUint(uint64(fn.ICCount), 10))
	sb.WriteString("\n")

	for i, k := range fn.Consts {
		sb.WriteString(indent)
		sb.WriteString("  K")
		sb.WriteString(strconv.Itoa(i))
		sb.WriteString(" = ")
		sb.WriteString(constString(k))
		sb.WriteString("\n")
	}

	code := fn.Code
	for pc := 0; pc < len(code); {
		w := code[pc]
		op := DecodeOp(w)
		sb.WriteString(indent)
		sb.WriteString("  ")
		writePC(sb, pc)
		sb.WriteString(" ")
		sb.WriteString(op.String())
		info := opTable[op]
		switch info.fmt {
		case FmtA:
			writeReg(sb, DecodeA(w))
		case FmtAB:
			writeReg(sb, DecodeA(w))
			if op == GetEnvW || op == SetEnvW || op == GetEnvChkW || op == GetImportW {
				sb.WriteString(" env")
				sb.WriteString(strconv.Itoa(int(DecodeB(w))))
				break
			}
			if op == UndefRange || op == Call || op == New {
				sb.WriteString(" ")
				sb.WriteString(strconv.Itoa(int(DecodeB(w))))
				break
			}
			writeReg(sb, DecodeB(w))
		case FmtABC:
			writeReg(sb, DecodeA(w))
			if op == GetEnv || op == SetEnv || op == GetEnvChk || op == GetImport {
				sb.WriteString(" env")
				sb.WriteString(strconv.Itoa(int(DecodeB(w))))
				sb.WriteString("[")
				sb.WriteString(strconv.Itoa(int(DecodeC(w))))
				sb.WriteString("]")
				break
			}
			writeReg(sb, DecodeB(w))
			if op == AddImm || op == SubImm {
				sb.WriteString(" ")
				sb.WriteString(strconv.Itoa(int(int8(DecodeC(w)))))
			} else if op == TypeofIs {
				sb.WriteString(" \"")
				sb.WriteString(TypeNames[DecodeC(w)])
				sb.WriteString("\"")
			} else {
				writeReg(sb, DecodeC(w))
			}
		case FmtABx:
			writeReg(sb, DecodeA(w))
			sb.WriteString(" ")
			bx := int(DecodeBx(w))
			switch op {
			case LoadConst, NewRegExp, NewPrivateName, ThrowError:
				sb.WriteString("K")
				sb.WriteString(strconv.Itoa(bx))
			case Closure:
				sb.WriteString("F")
				sb.WriteString(strconv.Itoa(bx))
			default:
				sb.WriteString(strconv.Itoa(bx))
			}
		case FmtAsBx:
			writeReg(sb, DecodeA(w))
			if op == LoadInt {
				sb.WriteString(" ")
				sb.WriteString(strconv.Itoa(int(DecodeSBx(w))))
				break
			}
			sb.WriteString(" -> ")
			writePC(sb, pc+1+int(DecodeSBx(w)))
		case FmtSBx:
			sb.WriteString(" -> ")
			writePC(sb, pc+1+int(DecodeSBx(w)))
		}
		pc++
		for i := 0; i < int(info.extra) && pc < len(code); i++ {
			x := code[pc]
			pc++
			sb.WriteString(" ")
			writeExtra(sb, op, i, x)
		}
		sb.WriteString("\n")
	}

	for _, h := range fn.Handlers {
		sb.WriteString(indent)
		sb.WriteString("  handler [")
		writePC(sb, int(h.Start))
		sb.WriteString(", ")
		writePC(sb, int(h.End))
		sb.WriteString(") -> ")
		writePC(sb, int(h.Handler))
		if h.Kind == HandlerFinally {
			sb.WriteString(" finally")
		} else {
			sb.WriteString(" catch")
		}
		sb.WriteString(" r")
		sb.WriteString(strconv.Itoa(int(h.Reg)))
		sb.WriteString(" env=")
		sb.WriteString(strconv.Itoa(int(h.StackDepth)))
		sb.WriteString("\n")
	}

	for i, c := range fn.Children {
		sb.WriteString(indent)
		sb.WriteString("  F")
		sb.WriteString(strconv.Itoa(i))
		sb.WriteString(":\n")
		disassemble(sb, c, indent+"  ")
	}
}

// writeExtra renders ExtraArg word i of op.
func writeExtra(sb *strings.Builder, op Op, i int, x uint32) {
	switch op {
	case GetGlobal, GetGlobalOrUndef, SetGlobal, GetProp, SetProp, DefineField, GetTemplate, SetGlobalSloppy, SetPropSloppy:
		sb.WriteString("K")
		sb.WriteString(strconv.Itoa(int(ExtraLo(x))))
		sb.WriteString(" ic")
		sb.WriteString(strconv.Itoa(int(ExtraHi(x))))
	case GetLen:
		sb.WriteString("ic")
		sb.WriteString(strconv.Itoa(int(ExtraHi(x))))
	case InitGlobal, WithGet, WithSet:
		sb.WriteString("K")
		sb.WriteString(strconv.Itoa(int(ExtraLo(x))))
		if ExtraHi(x) != 0 {
			sb.WriteString(" +")
			sb.WriteString(strconv.Itoa(int(ExtraHi(x))))
		}
	case GetEnvW, SetEnvW, DefineAccessor, SetPrivateMethod:
		sb.WriteString(strconv.Itoa(int(x)))
	case CallEval:
		sb.WriteString("E")
		sb.WriteString(strconv.Itoa(int(x)))
	case GetEnvChkW, GetImportW:
		if i == 0 {
			sb.WriteString(strconv.Itoa(int(x)))
		} else {
			sb.WriteString("K")
			sb.WriteString(strconv.Itoa(int(x)))
		}
	default: // name constants
		sb.WriteString("K")
		sb.WriteString(strconv.Itoa(int(x)))
	}
}

func writePC(sb *strings.Builder, pc int) {
	s := strconv.Itoa(pc)
	for i := len(s); i < 4; i++ {
		sb.WriteByte('0')
	}
	sb.WriteString(s)
}

func writeReg(sb *strings.Builder, r uint8) {
	sb.WriteString(" r")
	sb.WriteString(strconv.Itoa(int(r)))
}

func constString(k Const) string {
	switch k.Kind {
	case ConstNumber:
		return strconv.FormatFloat(k.Num, 'g', -1, 64)
	case ConstString:
		s := strconv.Quote(k.Str)
		if k.Key {
			return s + " (key)"
		}
		return s
	case ConstBigInt:
		return k.Str + "n"
	case ConstRegExp:
		return "/" + k.Str + "/" + k.Flags
	case ConstTemplate:
		return "template(" + strconv.Itoa(len(k.Cooked)) + ")"
	case ConstFunction:
		return "F" + strconv.Itoa(k.Index)
	}
	return "?"
}
