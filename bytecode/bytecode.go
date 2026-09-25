// Package bytecode defines the moejs instruction set and the compiled-function
// template shared between the compiler and the engine's interpreter.
//
// See doc.go for the ISA reference and opcodes.go for the encoding. The
// types in this file are the data model: nothing in a Function is mutated
// after compilation, so one *Function is shared by every realm (and every
// goroutine) that instantiates the same module. Per-realm state (inline
// caches, materialized constants) lives in engine side tables keyed by
// *Function.
package bytecode

import "sort"

// Kind classifies a compiled function template.
type Kind uint8

const (
	// KindNormal is an ordinary function declaration or expression.
	KindNormal Kind = iota
	// KindArrow is an arrow function (lexical this, not constructible).
	KindArrow
	// KindMethod is a concise object-literal method (not constructible).
	KindMethod
	// KindModule is the module top-level body.
	KindModule
	// KindScript is a strict-mode script top-level body (RunString).
	KindScript
	// KindClassCtor is a base class constructor: [[Construct]] creates
	// `this` from new.target and [[Call]] throws (CtorEntry). The class
	// kinds come last so IsClassCtor is one comparison.
	KindClassCtor
	// KindDerivedCtor is the constructor of a class with an extends clause:
	// `this` stays uninitialized until super() returns.
	KindDerivedCtor
)

// IsClassCtor reports whether k is a class constructor kind.
func (k Kind) IsClassCtor() bool { return k >= KindClassCtor }

// String returns the kind name used by disassembly and error messages.
func (k Kind) String() string {
	switch k {
	case KindNormal:
		return "normal"
	case KindArrow:
		return "arrow"
	case KindMethod:
		return "method"
	case KindModule:
		return "module"
	case KindScript:
		return "script"
	case KindClassCtor:
		return "class constructor"
	case KindDerivedCtor:
		return "derived constructor"
	}
	return "unknown"
}

// ConstKind tags a constant-pool entry.
type ConstKind uint8

const (
	// ConstNumber is a float64 literal (Num).
	ConstNumber ConstKind = iota
	// ConstString is a string literal (Str, IsASCII, Units).
	ConstString
	// ConstBigInt is a BigInt literal stored as text (Str): decimal, or hex
	// after "0x" (see syntax.BigIntLit).
	ConstBigInt
	// ConstRegExp is a regular-expression literal (Str = pattern, Flags).
	ConstRegExp
	// ConstTemplate is a tagged template's strings (Cooked, Raw); untagged
	// templates are lowered to Concat.
	ConstTemplate
	// ConstFunction is an index into Function.Children (Index). Reserved;
	// Closure indexes Children directly.
	ConstFunction
)

// Const is a constant-pool entry. Only the fields relevant to Kind are set.
type Const struct {
	Kind    ConstKind
	Num     float64  // ConstNumber
	Str     string   // ConstString (WTF-8), ConstBigInt (decimal or 0x hex), ConstRegExp (pattern)
	IsASCII bool     // ConstString: every byte of Str is < 0x80
	Units   []uint16 // ConstString: UTF-16 code units when !IsASCII (nil otherwise)
	Key     bool     // ConstString: used as a property name (the engine interns it)
	Flags   string   // ConstRegExp flags
	Cooked  []string // ConstTemplate cooked strings (WTF-8, or UndefinedCooked)
	Raw     []string // ConstTemplate raw strings (UTF-8)
	Index   int      // ConstFunction: index into Function.Children
}

// UndefinedCooked is the Const.Cooked entry of a tagged template chunk with
// an invalid escape, whose cooked value is undefined. The byte 0xFF never
// occurs in WTF-8.
const UndefinedCooked = "\xff"

// HandlerKind distinguishes catch and finally rows of the handler table.
type HandlerKind uint8

const (
	// HandlerCatch receives the thrown value in R[Reg].
	HandlerCatch HandlerKind = iota
	// HandlerFinally receives the completion kind (1 = throw) in R[Reg] and
	// the thrown value in R[Reg+1].
	HandlerFinally
)

// Handler is one exception-table row: [Start, End) is the protected pc range,
// Handler is the pc to resume at and StackDepth the closure-environment
// depth (number of PushEnv levels relative to function entry) to unwind to.
// Rows are ordered innermost first; the interpreter takes the first row whose
// range covers the faulting pc.
type Handler struct {
	Start      uint32
	End        uint32
	Handler    uint32
	StackDepth uint16
	Kind       HandlerKind
	Reg        uint16 // register receiving the exception (see HandlerKind)
}

// LineEntry maps the first pc of a run of instructions to a source position.
type LineEntry struct {
	PC   uint32
	Line int32
	Col  int32
}

// SourceInfo locates a function template inside its source file.
// Function.prototype.toString slices Src[Start:End].
type SourceInfo struct {
	Name  string // file name used in stack traces
	Src   string // full module/script source text
	Start int    // byte offset of the function's first character
	End   int    // byte offset one past the function's last character
}

// NoRegister marks a CaptureLayout slot that is not initialized from a
// parameter register.
const NoRegister uint16 = 0xFFFF

// CaptureLayout describes the function's own closure environment: its
// length is the number of Env slots created at function entry (zero means
// the function creates no Env and runs in its closure's environment), and
// entry i is the parameter register whose value is boxed into slot i at
// entry, or NoRegister for slots initialized to undefined.
type CaptureLayout []uint16

// Function is an immutable compiled-function template. A single *Function is
// shared by every realm that instantiates the same module, so nothing in it
// may be mutated after compilation; per-realm state (inline caches) lives in
// engine.Realm side tables keyed by *Function.
type Function struct {
	Name      string // "" for anonymous functions
	Length    int    // formal parameter count exposed as .length
	Strict    bool   // always true (strict mode only)
	Kind      Kind
	NumRegs   uint16 // register window size
	NumParams uint16 // declared parameters (excluding rest)
	HasRest   bool   // the rest array is collected into R[NumParams] at entry
	// HasArguments makes the entry create an unmapped arguments object in
	// the register after the parameters (and the rest array).
	HasArguments bool
	// NewTarget is set when the function reads new.target (LoadNewTarget):
	// [[Construct]] then passes it through the realm. Class constructors
	// always receive it.
	NewTarget bool
	// Generator marks a generator function (of kind KindMethod: not
	// constructible); its body starts with GenStart.
	Generator bool
	// Async marks an async function, async generator function or module
	// with top-level await (see compiler/async.go).
	Async bool
	// ICCount is the number of inline-cache slots this function needs. Their
	// indices are assigned per realm on the function's first call there
	// (engine.Realm.AllocIC); the template is never mutated.
	ICCount uint32

	Code     []uint32
	Consts   []Const
	Children []*Function
	Handlers []Handler

	CaptureLayout CaptureLayout
	LineTable     []LineEntry
	Source        *SourceInfo

	// Exports maps export names to module Env slots (KindModule only). The
	// module Env is the function's own Env, so every exported binding is a
	// CaptureLayout slot and GetBindingValue is one slice index.
	Exports map[string]int

	// Globals lists the names a script declares at top level (KindScript
	// only; nil when it declares none) for the checks of
	// GlobalDeclarationInstantiation before the body runs.
	Globals *GlobalNames
}

// GlobalNames are the top-level declarations of a script, each list in
// source order.
type GlobalNames struct {
	Lexical  []string // let, const and class names
	Function []string // function declaration names
	Var      []string // var names that are not also function names
}

// Position returns the source line and column of the instruction at pc, or
// (0, 0) when the function has no line table.
func (f *Function) Position(pc uint32) (line, col int) {
	lt := f.LineTable
	if len(lt) == 0 {
		return 0, 0
	}
	i := sort.Search(len(lt), func(i int) bool { return lt[i].PC > pc })
	if i == 0 {
		return int(lt[0].Line), int(lt[0].Col)
	}
	return int(lt[i-1].Line), int(lt[i-1].Col)
}

// DescribeCallee, when non-nil, returns the source form of the callee of the
// Call, CallSpread, New or NewSpread instruction at pc of f ("obj.foo",
// "f(...)"), or "" when it cannot tell. Package compiler installs it; the
// engine calls it only to word the TypeError of a call or construction of a
// value that is not callable, so no call site carries any extra data.
var DescribeCallee func(f *Function, pc uint32) string

// SourceText returns the function's source slice for Function.prototype.toString,
// or "" when unknown.
func (f *Function) SourceText() string {
	s := f.Source
	if s == nil || s.Start < 0 || s.End > len(s.Src) || s.Start > s.End {
		return ""
	}
	return s.Src[s.Start:s.End]
}
