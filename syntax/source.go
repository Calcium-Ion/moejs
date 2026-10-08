package syntax

import (
	"sort"
	"strconv"
)

// Options controls parsing.
type Options struct {
	// AllowUnsupported keeps the parser from rejecting syntax the engine
	// does not implement yet, so supporting a feature lifts its restriction
	// rather than touching the parser. No feature is deferred this way at
	// present (the proposals TestUnsupportedModuleSyntax lists fail
	// whatever the options); the option stays for the next one.
	AllowUnsupported bool
	// EarlyExports counts every exported function of a module as called
	// before the module's body runs, so the lexical bindings they read are
	// checked. A module that imports others always is; the linker asks for
	// this variant of an import-free module only when an import cycle can
	// call into it before its body ran (engine.LinkOptions.EarlyExports).
	EarlyExports bool
	// TypeScript makes ParseModule read TypeScript, whose type syntax it
	// erases: the tree is the one the type-erased JavaScript gives, with
	// every position in the TypeScript text. Import specifiers used only as
	// types are dropped, and TypeScript with run-time semantics (enums,
	// namespaces with values, parameter properties, import = require,
	// export =) is a syntax error. ParseScript rejects it: TypeScript code
	// is a module here.
	TypeScript bool
	// Stop, when set, is called before every stopEvery-th statement of each
	// statement list the parser and the resolver go through, the first
	// included: an error it returns ends the parse, which returns that error
	// rather than a *Error. The engine passes one that reports an interrupt,
	// so that one can stop a long compile of code from a string
	// (engine.Compiler); a host compile has none and pays nothing for it.
	Stop func() error
}

// stopEvery is the number of statements of a list between two calls of
// Options.Stop: a power of two, as the index of the statement is tested.
const stopEvery = 1024

// stopped is the panic that ends a parse or a resolution Options.Stop
// stopped, carrying its error.
type stopped struct{ err error }

// poll calls stop before the statement at index i of a list, every
// stopEvery statements, unwinding with its error.
func poll(stop func() error, i int) {
	if stop != nil && i&(stopEvery-1) == 0 {
		stopNow(stop)
	}
}

//go:noinline
func stopNow(stop func() error) {
	if err := stop(); err != nil {
		panic(stopped{err})
	}
}

// Error is a syntax error with a position. Msg always starts with
// "SyntaxError: " so that Error() reads like a compiler diagnostic and
// Message() yields the text a JS SyntaxError object should carry.
type Error struct {
	Name string // file name given to ParseModule/ParseScript
	Pos  int    // byte offset into the source
	Line int    // 1-based line
	Col  int    // 1-based column in code points
	Msg  string
}

const errPrefix = "SyntaxError: "

// Error formats as name:line:col: msg.
func (e *Error) Error() string {
	return e.Name + ":" + strconv.Itoa(e.Line) + ":" + strconv.Itoa(e.Col) + ": " + e.Msg
}

// Message returns Msg without the "SyntaxError: " prefix.
func (e *Error) Message() string {
	if len(e.Msg) >= len(errPrefix) && e.Msg[:len(errPrefix)] == errPrefix {
		return e.Msg[len(errPrefix):]
	}
	return e.Msg
}

// File is a parsed source text with a line-start table for mapping byte
// offsets to (line, col).
type File struct {
	Name string
	Src  string

	lineStarts []int // byte offset of the first character of each line; lineStarts[0] == 0
	// runeCounts[k] is the number of code points in Src[:k*runeStride]; nil
	// for an all-ASCII source, where a column is a byte offset. It makes
	// Position independent of the column, so compiling a minified line (one
	// lookup per emitted instruction) stays linear.
	runeCounts []int32
}

// runeStride is the granularity of File.runeCounts: a column lookup counts
// at most this many bytes.
const runeStride = 64

// newFile creates a File whose line table is filled in by the lexer as it
// scans, so the table is exact for whatever was tokenised (all of it on a
// successful parse).
func newFile(name, src string) *File {
	f := &File{Name: name, Src: src, lineStarts: make([]int, 1, 1+len(src)/32)}
	if !IsASCII(src) {
		f.runeCounts = make([]int32, len(src)/runeStride+1)
		n := 0
		for k := 1; k < len(f.runeCounts); k++ {
			n += runeStarts(src[(k-1)*runeStride : k*runeStride])
			f.runeCounts[k] = int32(n)
		}
	}
	return f
}

// runeStarts counts the code points beginning in s (every byte that is not
// a UTF-8 continuation byte), which for valid UTF-8 is utf8.RuneCountInString
// and, unlike it, splits consistently at arbitrary byte boundaries.
func runeStarts(s string) int {
	n := 0
	for i := range len(s) {
		if s[i]&0xC0 != 0x80 {
			n++
		}
	}
	return n
}

// runesBefore returns the number of code points in Src[:pos].
func (f *File) runesBefore(pos int) int {
	if f.runeCounts == nil {
		return pos
	}
	k := pos / runeStride
	return int(f.runeCounts[k]) + runeStarts(f.Src[k*runeStride:pos])
}

// addLine records that a new line begins at byte offset pos. Rescans and
// lookahead may revisit a line terminator; duplicates are ignored.
func (f *File) addLine(pos int) {
	if pos > f.lineStarts[len(f.lineStarts)-1] {
		f.lineStarts = append(f.lineStarts, pos)
	}
}

// Line returns the 1-based line containing byte offset pos.
func (f *File) Line(pos int) int {
	// sort.Search finds the first line start > pos; the line is its index.
	return sort.SearchInts(f.lineStarts, pos+1)
}

// Position returns the 1-based line and the 1-based column (counted in code
// points) of byte offset pos, in O(log lines + runeStride).
func (f *File) Position(pos int) (line, col int) {
	pos = min(max(pos, 0), len(f.Src))
	line = f.Line(pos)
	start := f.lineStarts[line-1]
	return line, f.runesBefore(pos) - f.runesBefore(start) + 1
}

// LineCount returns the number of lines seen by the lexer.
func (f *File) LineCount() int { return len(f.lineStarts) }

// errorAt builds an Error for offset pos.
func (f *File) errorAt(pos int, msg string) *Error {
	line, col := f.Position(pos)
	return &Error{Name: f.Name, Pos: pos, Line: line, Col: col, Msg: errPrefix + msg}
}
