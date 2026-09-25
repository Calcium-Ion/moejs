package compiler

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Calcium-Ion/moejs/bytecode"
	"github.com/Calcium-Ion/moejs/syntax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func compileModule(t *testing.T, src string) *bytecode.Function {
	t.Helper()
	m, err := syntax.ParseModule("t.js", src, syntax.Options{})
	require.NoError(t, err)
	fn, err := CompileModule(m)
	require.NoError(t, err)
	return fn
}

// child returns the child template named name.
func child(t *testing.T, fn *bytecode.Function, name string) *bytecode.Function {
	t.Helper()
	for _, c := range fn.Children {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no child %q", name)
	return nil
}

func ops(fn *bytecode.Function) []bytecode.Op {
	var out []bytecode.Op
	for pc := 0; pc < len(fn.Code); {
		op := bytecode.DecodeOp(fn.Code[pc])
		out = append(out, op)
		pc += 1 + op.ExtraWords()
	}
	return out
}

func TestDisassembleGolden(t *testing.T) {
	fn := compileModule(t, `export function clamp(v, lo, hi) {
  const n = +v;
  if (typeof v !== "number") return lo;
  if (n < lo) return lo;
  return n > hi ? hi : n;
}`)
	want := `function clamp (normal) params=3 regs=6 env=0 ics=0
  0000 UndefRange r3 1
  0001 Plus r3 r0
  0002 TypeofIs r4 r0 "number"
  0003 Not r4 r4
  0004 JmpF r4 -> 0006
  0005 Ret r1
  0006 Lt r4 r3 r1
  0007 JmpF r4 -> 0009
  0008 Ret r1
  0009 Gt r5 r3 r2
  0010 JmpF r5 -> 0013
  0011 Move r4 r2
  0012 Jmp -> 0014
  0013 Move r4 r3
  0014 Ret r4
  0015 RetUndef
`
	assert.Equal(t, want, bytecode.Disassemble(child(t, fn, "clamp")))
}

func TestDisassembleModuleAndHandlers(t *testing.T) {
	fn := compileModule(t, `export let n = 0;
export function guard(f) { try { return f(); } catch (e) { n++; return e.message; } }`)
	got := bytecode.Disassemble(fn)
	assert.True(t, strings.HasPrefix(got, "function <anonymous> (module) params=0 regs=1 env=2 ics=0\n"), got)
	assert.Contains(t, got, "  F0:\n  function guard (normal) params=1 regs=")
	assert.Contains(t, got, `K0 = "message" (key)`)
	assert.Contains(t, got, "GetProp r")
	assert.Contains(t, got, " catch r")
	assert.Len(t, fn.Exports, 2)
	assert.NotEqual(t, fn.Exports["n"], fn.Exports["guard"], "each export has its own module Env slot")
	guard := child(t, fn, "guard")
	require.Len(t, guard.Handlers, 1)
	assert.Equal(t, bytecode.HandlerCatch, guard.Handlers[0].Kind)
	assert.Equal(t, uint32(0), guard.Handlers[0].Start)
	assert.Less(t, guard.Handlers[0].End, guard.Handlers[0].Handler)
}

// TestIterCloseRows checks the handler rows around the iterator close of a
// return leaving a for-of loop: the rows of a try statement inside the loop
// skip it, and the loop's own row covers it whole (the record is closed
// before its return method runs, so the row only rethrows).
func TestIterCloseRows(t *testing.T) {
	fn := compileModule(t, `export function plain(a) { for (const x of a) if (x) return x; }
export function nested(a) { for (const x of a) { try { if (x) return x; } catch (e) {} } }`)
	covering := func(f *bytecode.Function) []bytecode.Handler {
		pc := slices.IndexFunc(f.Code, func(w uint32) bool { return w == bytecode.EncodeAsBx(bytecode.IterClose, uint8(w>>8), 0) })
		require.GreaterOrEqual(t, pc, 0, "no IterClose")
		var rows []bytecode.Handler
		for _, h := range f.Handlers {
			if h.Start <= uint32(pc) && uint32(pc) < h.End {
				rows = append(rows, h)
			}
		}
		return rows
	}
	plain := child(t, fn, "plain")
	assert.Len(t, plain.Handlers, 1, "the loop's row is not split")
	assert.Len(t, covering(plain), 1)
	nested := child(t, fn, "nested")
	assert.Len(t, nested.Handlers, 3, "the try's row is split around the close, the loop's is not")
	assert.Len(t, covering(nested), 1, "only the loop's row covers the close")
}

func TestConstantFolding(t *testing.T) {
	fn := compileModule(t, `
export const a = 1 + 2 * 3;
export const b = "x" + "y" + "z";
export const c = !0;
export const d = -(2 ** 3);
export const e = typeof "s";
export const f = 1 < 2 ? "yes" : "no";
export const g = null ?? undefined ?? 5;
export const h = (1, 2) | 4;
export const i = 1 / 0;
`)
	names := map[float64]bool{}
	strs := map[string]bool{}
	for _, k := range fn.Consts {
		switch k.Kind {
		case bytecode.ConstNumber:
			names[k.Num] = true
		case bytecode.ConstString:
			strs[k.Str] = true
		}
	}
	assert.True(t, strs["xyz"], "string concatenation folded")
	assert.False(t, strs["x"], "no intermediate string constants")
	assert.True(t, strs["yes"])
	assert.False(t, strs["no"], "dead conditional branch dropped")
	assert.True(t, strs["string"], "typeof folded")
	for _, op := range ops(fn) {
		assert.NotContains(t, []bytecode.Op{bytecode.Add, bytecode.Mul, bytecode.Exp, bytecode.Not, bytecode.Typeof, bytecode.BitOr, bytecode.Div, bytecode.JmpT, bytecode.JmpF, bytecode.JmpNotNullish}, op, "operator %s should have been folded", op)
	}
	// Small integers load as immediates, others through the constant pool.
	seenLoadInt := false
	for _, op := range ops(fn) {
		if op == bytecode.LoadInt {
			seenLoadInt = true
		}
	}
	assert.True(t, seenLoadInt)
	assert.True(t, names[1/zero()], "Infinity is a pool constant")
}

func zero() float64 { return 0 }

// TestFoldStringChains covers the concatenations built across the links of
// one chain, through logical operators that do and do not short-circuit.
func TestFoldStringChains(t *testing.T) {
	const no = "\x00" // the chain does not fold
	for _, tt := range []struct{ src, want string }{
		{`"a" + "b" + "c" + "d"`, "abcd"},
		{`("a" + "b" || 0) + "c"`, "abc"},
		{`("a" + "b" ?? 0) + "c" + "d"`, "abcd"},
		{`("" + "" || "x") + "y"`, "xy"},
		{`("a" + "b" && "q") + "r"`, "qr"},
		{`("a" + "b" && "") + "c"`, "c"},
		{`(("a" + "b" && "p" + "q") || 0) + "r"`, "pqr"},
		{`(typeof 1 + "!" || 0) + "?"`, "number!?"},
		{`"" + "" + ""`, ""},
		{`"a" + "b" + 1`, no},
		{`("a" + "b" || 0) + x`, no},
		{`("a" + "b" - 1) + "c"`, no},
	} {
		fn := compileModule(t, "export const x = 1; export const y = "+tt.src+";")
		strs := []string{""} // the empty string loads without a constant
		for _, k := range fn.Consts {
			if k.Kind == bytecode.ConstString {
				strs = append(strs, k.Str)
			}
		}
		adds := slices.ContainsFunc(ops(fn), func(op bytecode.Op) bool { return op == bytecode.Add || op == bytecode.AddImm })
		if tt.want == no {
			assert.True(t, adds, "%s does not fold", tt.src)
			continue
		}
		assert.False(t, adds, "%s folds", tt.src)
		assert.Contains(t, strs, tt.want, "%s", tt.src)
	}
}

func TestDeadCodeAfterReturn(t *testing.T) {
	fn := compileModule(t, `export function f(x) { return x; x = 2; f(); throw 1; }
export function g(x) { if (x) { return 1; g(); } return 2; }
export function h() { throw new Error("e"); return 3; }`)
	assert.Equal(t, []bytecode.Op{bytecode.Ret, bytecode.RetUndef}, ops(child(t, fn, "f")))
	assert.NotContains(t, ops(child(t, fn, "g")), bytecode.Call)
	hops := ops(child(t, fn, "h"))
	assert.Equal(t, bytecode.Throw, hops[len(hops)-2])
	assert.Equal(t, bytecode.RetUndef, hops[len(hops)-1])
}

func TestRegisterAndEnvAllocation(t *testing.T) {
	fn := compileModule(t, `
export function plain(a, b) { let c = a + b; const d = c * 2; return d; }
export function captured(a) { let n = a; return () => n++; }
export function block() { let out; { let x = 1; out = () => x; } return out; }
export function loopEnv() { const fs = []; for (let i = 0; i < 2; i++) fs[i] = () => i; return fs; }
`)
	plain := child(t, fn, "plain")
	assert.Empty(t, plain.CaptureLayout)
	assert.NotContains(t, ops(plain), bytecode.PushEnv)
	assert.NotContains(t, ops(plain), bytecode.GetEnv)
	assert.NotContains(t, ops(plain), bytecode.CheckTDZ)

	cap := child(t, fn, "captured")
	assert.Equal(t, bytecode.CaptureLayout{bytecode.NoRegister}, cap.CaptureLayout, "only n is boxed, not the parameter")
	assert.Contains(t, ops(cap), bytecode.SetEnv)
	inner := cap.Children[0]
	assert.Equal(t, bytecode.KindArrow, inner.Kind)
	assert.Contains(t, ops(inner), bytecode.GetEnv)

	blk := child(t, fn, "block")
	assert.Empty(t, blk.CaptureLayout, "block scope environments are pushed, not part of the function env")
	assert.Contains(t, ops(blk), bytecode.PushEnv)
	assert.Contains(t, ops(blk), bytecode.PopEnv)

	loop := child(t, fn, "loopEnv")
	assert.Contains(t, ops(loop), bytecode.CopyEnv)
	assert.False(t, loop.HasArguments)
}

// TestArgumentsRegister checks that a function referencing `arguments` (in
// its body or a nested arrow) gets HasArguments and a register after its
// parameters and rest array, boxed into its environment when an arrow reads
// it; arrows never get their own.
func TestArgumentsRegister(t *testing.T) {
	fn := compileModule(t, `
export function direct(a, b) { return arguments[0]; }
export function rest(a, ...r) { return arguments.length; }
export function viaArrow(a) { return () => arguments; }
`)
	direct := child(t, fn, "direct")
	assert.True(t, direct.HasArguments)
	assert.Empty(t, direct.CaptureLayout)
	assert.GreaterOrEqual(t, int(direct.NumRegs), 3)

	rest := child(t, fn, "rest")
	assert.True(t, rest.HasArguments)
	assert.True(t, rest.HasRest)
	assert.GreaterOrEqual(t, int(rest.NumRegs), 3)

	via := child(t, fn, "viaArrow")
	assert.True(t, via.HasArguments)
	assert.Equal(t, bytecode.CaptureLayout{1}, via.CaptureLayout, "the arguments register (after a) is boxed")
	arrow := via.Children[0]
	assert.False(t, arrow.HasArguments)
	assert.Contains(t, ops(arrow), bytecode.GetEnv)
}

// TestTaggedTemplateCompile checks the tagged-template lowering: one
// ConstTemplate and one inline-cache slot per site, the tag's object as
// this, and the substitutions as the arguments after the template object.
func TestTaggedTemplateCompile(t *testing.T) {
	fn := compileModule(t, "export function f(o, x) { return o.t`a${x}\\01`; }\nexport function g(x) { return `a${x}`; }")
	f := child(t, fn, "f")
	assert.Equal(t, []bytecode.Op{bytecode.Move, bytecode.GetProp, bytecode.GetTemplate, bytecode.Move, bytecode.Call, bytecode.Ret, bytecode.RetUndef}, ops(f))
	var tmpl *bytecode.Const
	for i := range f.Consts {
		if f.Consts[i].Kind == bytecode.ConstTemplate {
			require.Nil(t, tmpl, "one template constant")
			tmpl = &f.Consts[i]
		}
	}
	require.NotNil(t, tmpl)
	assert.Equal(t, []string{"a", bytecode.UndefinedCooked}, tmpl.Cooked)
	assert.Equal(t, []string{"a", `\01`}, tmpl.Raw)
	assert.Equal(t, uint32(2), f.ICCount, "GetProp and GetTemplate")
	g := child(t, fn, "g")
	assert.NotContains(t, ops(g), bytecode.GetTemplate)
	assert.Zero(t, g.ICCount, "untagged templates use no cache slot")
}

func TestModuleExportsAndTopLevel(t *testing.T) {
	fn := compileModule(t, `
let a = 1;
export { a as b, a as c };
export default 42;
export function f() {}
`)
	assert.Equal(t, bytecode.KindModule, fn.Kind)
	assert.Equal(t, 3, len(fn.CaptureLayout), "a, *default*, f")
	assert.Equal(t, fn.Exports["b"], fn.Exports["c"])
	_, hasDefault := fn.Exports["default"]
	assert.True(t, hasDefault)
	assert.NotNil(t, fn.Source)
	assert.Equal(t, "t.js", fn.Source.Name)
	assert.Equal(t, "function f() {}", child(t, fn, "f").SourceText())
}

func TestModuleTopLevelAwait(t *testing.T) {
	fn := compileModule(t, `export const x = await 1;`)
	assert.True(t, fn.Async, "a module with top-level await is an async body")
	o := ops(fn)
	assert.Contains(t, o, bytecode.AsyncStart)
	assert.Contains(t, o, bytecode.Await)
	assert.Contains(t, o, bytecode.AsyncReturn)
	fn = compileModule(t, `export const x = 1;`)
	assert.False(t, fn.Async)
	o = ops(fn)
	assert.NotContains(t, o, bytecode.AsyncStart)
	assert.Equal(t, bytecode.RetUndef, o[len(o)-1])
}

func TestCompileScript(t *testing.T) {
	s, err := syntax.ParseScript("s.js", `var x = 1; x + 1;`, syntax.Options{})
	require.NoError(t, err)
	fn, err := CompileScript(s)
	require.NoError(t, err)
	assert.Equal(t, bytecode.KindScript, fn.Kind)
	o := ops(fn)
	assert.Equal(t, bytecode.Ret, o[len(o)-1], "scripts return their completion value")
}

func TestCompileUnsupported(t *testing.T) {
	tests := []struct{ src, feature string }{
		{`import x from "y";`, "import"},
		{`export * from "y";`, "export ... from"},
		{`const p = import("x");`, "dynamic import()"},
		{`function f() { eval("1"); }`, "direct eval"},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			m, err := syntax.ParseModule("t.js", tt.src, syntax.Options{AllowUnsupported: true})
			require.NoError(t, err)
			_, err = CompileModule(m)
			require.Error(t, err)
			var ce *Error
			require.ErrorAs(t, err, &ce)
			assert.Equal(t, "SyntaxError: "+tt.feature+" is not supported yet (see TODO.md)", ce.Msg)
			assert.Contains(t, err.Error(), "t.js:1:")
		})
	}
}

// TestExportNotModuleBinding covers an export entry whose binding the module
// does not declare, which the scope pass never produces.
func TestExportNotModuleBinding(t *testing.T) {
	m, err := syntax.ParseModule("t.js", "const a = 1;\nexport { a as b };", syntax.Options{})
	require.NoError(t, err)
	m.Exports[0].Binding = &syntax.Binding{Name: "a"}
	_, err = CompileModule(m)
	var ce *Error
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, "t.js:2:10: SyntaxError: export 'b' is not a module binding", err.Error())
}

func TestCompileLimits(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("export function f() {\n")
	for i := range 300 {
		sb.WriteString("let v")
		sb.WriteString(strings.Repeat("x", i%5))
		sb.WriteString(itoa(i))
		sb.WriteString(" = ")
		sb.WriteString(itoa(i))
		sb.WriteString(";\n")
	}
	sb.WriteString("return v0;\n}")
	m, err := syntax.ParseModule("t.js", sb.String(), syntax.Options{})
	require.NoError(t, err)
	_, err = CompileModule(m)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "more than 256 registers")
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func TestLineTable(t *testing.T) {
	fn := compileModule(t, "export function f(a) {\n  const b = a + 1;\n  return b.x;\n}")
	f := child(t, fn, "f")
	require.NotEmpty(t, f.LineTable)
	// The property read on line 3 maps back to line 3.
	for pc := 0; pc < len(f.Code); {
		op := bytecode.DecodeOp(f.Code[pc])
		if op == bytecode.GetProp {
			line, col := f.Position(uint32(pc))
			assert.Equal(t, 3, line)
			assert.Equal(t, 10, col)
		}
		pc += 1 + op.ExtraWords()
	}
	assert.Equal(t, uint32(0), f.LineTable[0].PC)
	assert.Equal(t, int32(1), f.LineTable[0].Line)
}

func TestTypeofIsFusion(t *testing.T) {
	fn := compileModule(t, `export function f(x) { return [typeof x === "string", "object" == typeof x, typeof x !== "function", typeof x === "nope", typeof x === y]; }`)
	f := child(t, fn, "f")
	count := 0
	for _, op := range ops(f) {
		if op == bytecode.TypeofIs {
			count++
		}
	}
	assert.Equal(t, 3, count, "three fusable comparisons; unknown type names and non-literals stay generic")
	assert.Contains(t, ops(f), bytecode.Typeof)
}

func TestNamedEvaluation(t *testing.T) {
	fn := compileModule(t, `
export const arrow = () => 1;
export const fexpr = function () {};
export const named = function real() {};
export default () => 2;
const o = { m: () => 3, meth() {}, ["comp" + "uted"]: () => 4 };
`)
	names := map[string]bool{}
	for _, c := range fn.Children {
		names[c.Name] = true
	}
	assert.True(t, names["arrow"])
	assert.True(t, names["fexpr"])
	assert.True(t, names["real"])
	assert.False(t, names["named"])
	assert.True(t, names["default"])
	assert.True(t, names["m"])
	assert.True(t, names["meth"])
	assert.True(t, names[""], "computed-key arrow stays anonymous")
}

func TestAccessorLiteral(t *testing.T) {
	fn := compileModule(t, `export function f(k) { return { get a() { return 1; }, set [k](v) {}, set 2(v) {} }; }`)
	want := `function f (normal) params=1 regs=4 env=0 ics=0
  K0 = "a"
  K1 = "2"
  0000 NewObject r1 3
  0001 LoadConst r2 K0
  0002 Closure r3 F0
  0003 DefineAccessor r1 r2 r3 2
  0005 Move r2 r0
  0006 Closure r3 F1
  0007 DefineAccessor r1 r2 r3 7
  0009 LoadConst r2 K1
  0010 Closure r3 F2
  0011 DefineAccessor r1 r2 r3 3
  0013 Ret r1
  0014 RetUndef
`
	f := child(t, fn, "f")
	dis := bytecode.Disassemble(f)
	assert.Equal(t, want, dis[:strings.Index(dis, "  F0:")])
	require.Len(t, f.Children, 3)
	for i, name := range []string{"get a", "", "set 2"} {
		assert.Equal(t, name, f.Children[i].Name)
		assert.Equal(t, bytecode.KindMethod, f.Children[i].Kind)
	}
	assert.Equal(t, 0, int(f.Children[0].Length))
	assert.Equal(t, 1, int(f.Children[1].Length))
}

// nestedSource builds a module nested n levels deep in the given shape.
func nestedSource(kind string, n int) string {
	rep := strings.Repeat
	switch kind {
	case "paren":
		return "const x = " + rep("(", n) + "1" + rep(")", n) + ";"
	case "array":
		return "const x = " + rep("[", n) + rep("]", n) + ";"
	case "object":
		return "const x = " + rep("{a:", n) + "1" + rep("}", n) + ";"
	case "block":
		return rep("{", n) + rep("}", n)
	case "if":
		return rep("if(1)", n) + ";"
	case "not":
		return "const x = " + rep("!", n) + "1;"
	case "func":
		return rep("function f(){", n) + rep("}", n)
	case "arrow":
		return "const x = " + rep("()=>", n) + "1;"
	case "call":
		return "const f = x => x; const x = " + rep("f(", n) + "1" + rep(")", n) + ";"
	case "ternary":
		return "const c = 1; const x = " + rep("c?", n) + "1" + rep(":0", n) + ";"
	case "template":
		return "const x = " + rep("`${", n) + "1" + rep("}`", n) + ";"
	case "new":
		return "const f = x => x; const x = " + rep("new ", n) + "f;"
	case "pattern":
		return "const " + rep("[", n) + "a" + rep("]", n) + " = [];"
	}
	panic("unknown kind " + kind)
}

// TestCompileAtNestingLimit compiles every nesting shape at the deepest
// level the parser accepts (syntax.MaxNestingDepth bounds its recursion):
// the scope pass and the compiler recurse over the same tree and must
// either compile it or fail with a positioned error, never overflow the Go
// stack (a fatal error the host cannot recover from).
func TestCompileAtNestingLimit(t *testing.T) {
	for _, kind := range []string{"paren", "array", "object", "block", "if", "not", "func", "arrow", "call", "ternary", "template", "new", "pattern"} {
		t.Run(kind, func(t *testing.T) {
			lo, hi := 1, syntax.MaxNestingDepth+1
			for hi-lo > 1 {
				mid := (lo + hi) / 2
				if _, err := syntax.ParseModule("deep.js", nestedSource(kind, mid), syntax.Options{}); err == nil {
					lo = mid
				} else {
					hi = mid
				}
			}
			m, err := syntax.ParseModule("deep.js", nestedSource(kind, lo), syntax.Options{})
			require.NoError(t, err)
			_, err = CompileModule(m)
			if err != nil {
				var ce *Error
				require.ErrorAs(t, err, &ce, "%s at depth %d: %v", kind, lo, err)
				t.Logf("%s at depth %d: %v", kind, lo, err)
				return
			}
			t.Logf("%s at depth %d: compiled", kind, lo)
		})
	}
}

// TestFoldChainLinear pins the linear compile time of long left-nested
// chains: compiling a chain folds every level's left operand on the way
// down, and before foldChain remembered the unfoldable spine this was
// quadratic (a 100,000-term `a+a+...` took 55 s, 20,000 terms of `&&` 2 s).
// One chain of 16n terms must cost about as much as sixteen chains of n
// terms (deep recursion and stack growth allow up to 6x); a quadratic
// compiler is 16x slower. The minimum of three trials keeps the ratio robust
// on a loaded machine and under -race.
func TestFoldChainLinear(t *testing.T) {
	chain := func(prefix, term, op, suffix string) func(n int) string {
		return func(n int) string {
			return "const a = 1; " + prefix + strings.TrimSuffix(strings.Repeat(term+op, n), op) + suffix
		}
	}
	for _, c := range []struct {
		name string
		n    int
		src  func(n int) string
	}{
		{"plus", 2000, chain("export const x = ", "a", "+", ";")},
		{"plus-const", 2000, chain("export const x = a+", "1", "+", ";")},
		{"concat", 2000, chain("export const x = ", `"abcdefgh"`, "+", ";")},
		{"concat-or", 300, func(n int) string {
			return "export const x = " + strings.Repeat("(", n) + `""` + strings.Repeat(` + "abcdefgh" || 0)`, n) + ";"
		}},
		{"lt", 2000, chain("export const x = ", "a", "<", ";")},
		{"and", 2000, chain("export const x = ", "a", "&&", ";")},
		{"or", 2000, chain("export const x = ", "a", "||", ";")},
		{"nullish", 2000, chain("export const x = ", "a", "??", ";")},
		{"mixed", 2000, chain("export const x = ", "a*a+a", "&&", ";")},
		{"if-and", 500, chain("if (", "a", "&&", ") {}")},
	} {
		t.Run(c.name, func(t *testing.T) {
			parse := func(n int) *syntax.Module {
				m, err := syntax.ParseModule("chain.js", c.src(n), syntax.Options{})
				require.NoError(t, err)
				return m
			}
			compile := func(ms ...*syntax.Module) time.Duration {
				start := time.Now()
				for _, m := range ms {
					_, err := CompileModule(m)
					require.NoError(t, err)
				}
				return time.Since(start)
			}
			const k = 16
			one, many := time.Duration(1<<62), time.Duration(1<<62)
			for range 3 {
				small := make([]*syntax.Module, k)
				for i := range small {
					small[i] = parse(c.n)
				}
				many = min(many, compile(small...))
				one = min(one, compile(parse(k*c.n)))
			}
			t.Logf("%s: 1 x %d terms %v, %d x %d terms %v", c.name, k*c.n, one, k, c.n, many)
			assert.Less(t, one, 6*many+2*time.Millisecond, "compile time grows quadratically with the chain length")
		})
	}
}
