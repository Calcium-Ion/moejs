package syntax

// Span is the half-open byte range [Pos, End) of a node in the source. Every
// node embeds it; File.Position maps offsets to (line, col).
type Span struct {
	Pos, End int
}

// Range returns the byte range of the node.
func (s Span) Range() (pos, end int) { return s.Pos, s.End }

// Node is any AST node.
type Node interface {
	Range() (pos, end int)
}

// Expr is an expression node.
type Expr interface {
	Node
	exprNode()
}

// Stmt is a statement or declaration node.
type Stmt interface {
	Node
	stmtNode()
}

// Pattern is a binding or assignment target: *Ident, *MemberExpr (assignment
// targets only), *ObjectPattern, *ArrayPattern or *AssignPattern.
type Pattern interface {
	Node
	patternNode()
}

// Program is the shared body of Module and Script.
type Program struct {
	Span
	File  *File
	Body  []Stmt
	Scope *Scope // module or script scope; filled by the scope pass
}

// Module is a parsed ES module.
type Module struct {
	Program
	// Exports lists every local export entry in source order. Filled by the
	// scope pass; Binding is the module-scope binding the export reads.
	Exports []*ExportEntry
	// Requests lists the specifiers of the modules this module requests,
	// each once, in source order. Imports, Reexports and Stars refer to
	// them by index. All four are filled by the scope pass and are empty
	// for a module without import declarations or export ... from.
	Requests  []*ModuleRequest
	Imports   []*ImportEntry
	Reexports []*ReexportEntry
	Stars     []*StarExport
	// Async is set by the scope pass when the body awaits outside any
	// function (top-level await): the module evaluates asynchronously.
	// HasDirectEval is set by the scope pass when a direct eval call
	// appears in the module's code, at any depth.
	Async, HasDirectEval bool
}

// Script is a parsed script. Strict is set when its directive prologue
// holds a "use strict" directive; otherwise it is sloppy-mode code.
// HasDirectEval is set by the scope pass when a direct eval call appears
// in the script's code, at any depth.
type Script struct {
	Program
	Strict, HasDirectEval bool
}

// ExportEntry describes one exported name of a module.
type ExportEntry struct {
	Name    string   // exported name
	Local   string   // local binding name ("*default*" for an anonymous default export)
	Binding *Binding // module-scope binding
	Pos     int      // position of the export specifier or declaration
}

// ModuleRequest is a requested module specifier.
type ModuleRequest struct {
	Specifier string // WTF-8 value of the string literal
	Pos       int    // position of its first occurrence
}

// ImportEntry binds a local name to an export of a requested module.
type ImportEntry struct {
	Request   int      // index in Module.Requests
	Name      string   // imported export name (unset for a namespace import)
	Namespace bool     // import * as Local: the binding is the module namespace object
	Binding   *Binding // local binding (BindImport, or BindImportNS for a namespace import)
	Pos       int      // position of the import specifier
}

// ReexportEntry exports a name of a requested module without a local
// binding: export {x as y} from, export * as y from (All), and export {y}
// of an import binding (All for a namespace import).
type ReexportEntry struct {
	Name    string // exported name
	Request int    // index in Module.Requests
	Import  string // imported export name (unset when All)
	All     bool   // export * as Name from: the requested module's namespace object
	Pos     int
}

// StarExport is export * from: every name of the requested module except
// default.
type StarExport struct {
	Request int // index in Module.Requests
	Pos     int
}

// Export returns the binding exported under name, or nil.
func (m *Module) Export(name string) *Binding {
	for _, e := range m.Exports {
		if e.Name == name {
			return e.Binding
		}
	}
	return nil
}

// ---- Expressions -----------------------------------------------------------

// Ident is an identifier. In reference position the scope pass fills Binding
// (nil means global/unresolvable). In binding position (declarations,
// parameters, patterns) Binding is the binding it declares. Non-computed
// property keys and member names are also Idents but are never resolved.
type Ident struct {
	Span
	Name    string
	Binding *Binding
}

// ThisExpr is `this`. Binding is set in a derived class constructor (and
// the arrows nested in it), where `this` is the constructor's
// Function.ThisBinding; nil means the ordinary this value.
type ThisExpr struct {
	Span
	Binding *Binding
}

// SuperExpr is `super` in a SuperCall `super(args)` or a SuperProperty
// `super.x` / `super[x]` (the parser rejects any other use).
type SuperExpr struct{ Span }

// NewTarget is `new.target`. Binding is the enclosing non-arrow function's
// Function.NewTargetBinding.
type NewTarget struct {
	Span
	Binding *Binding
}

// ImportMeta is `import.meta`, only in module code.
type ImportMeta struct{ Span }

// ImportCall is a dynamic `import(x)`.
type ImportCall struct {
	Span
	Source Expr
}

// NumberLit is a numeric literal. Legacy octal forms are rejected by the
// lexer, so Value is always the exact ES mathematical value.
type NumberLit struct {
	Span
	Value float64
}

// BigIntLit is a BigInt literal; Digits is the value in decimal without a
// sign, leading zeros or separators, or in hex after "0x" for a 0x, 0o or 0b
// literal too large for 64 bits.
type BigIntLit struct {
	Span
	Digits string
}

// StringLit is a string literal. Value is the cooked text in WTF-8 (see
// wtf8.go): UTF-8, except that lone surrogates written as escapes are
// encoded as three-byte sequences so that DecodeWTF8 recovers the exact
// UTF-16 code units.
type StringLit struct {
	Span
	Value string
}

// BoolLit is `true` or `false`.
type BoolLit struct {
	Span
	Value bool
}

// NullLit is `null`.
type NullLit struct{ Span }

// RegexLit is a regular expression literal. Flags are validated for the set
// dgimsuvy without duplicates (u and v exclusive) and the pattern by
// internal/regexpsyntax.
type RegexLit struct {
	Span
	Pattern string
	Flags   string
}

// TemplateElement is one text chunk of a template literal.
type TemplateElement struct {
	Span
	Cooked string // WTF-8 cooked text, or InvalidCooked
	Raw    string // raw text with CR/CRLF normalised to LF
}

// InvalidCooked is the Cooked text of a tagged template chunk with an
// invalid escape, whose cooked value is undefined. The byte 0xFF never
// occurs in WTF-8.
const InvalidCooked = "\xff"

// TemplateLit is an untagged template literal. len(Quasis) == len(Exprs)+1.
type TemplateLit struct {
	Span
	Quasis []TemplateElement
	Exprs  []Expr
}

// TaggedTemplate is tag`...`.
type TaggedTemplate struct {
	Span
	Tag   Expr
	Quasi *TemplateLit
}

// ArrayLit is an array literal. A nil element is a hole; *SpreadElem
// elements are spreads.
type ArrayLit struct {
	Span
	Elems []Expr

	parenthesized bool // wrapped in parentheses: not convertible to a pattern
	restComma     bool // a comma follows the final spread: not convertible to a pattern
}

// PropKind classifies object literal properties.
type PropKind uint8

const (
	PropInit      PropKind = iota // key: value
	PropShorthand                 // { x }
	PropCoverInit                 // { x = 1 } cover grammar; only valid once converted to a pattern
	PropMethod                    // f() {}
	PropGetter                    // get f() {}
	PropSetter                    // set f(v) {}
	PropSpread                    // ...expr
)

// Property is one object literal entry.
type Property struct {
	Span
	Kind     PropKind
	Key      Expr // *Ident, *StringLit, *NumberLit, *BigIntLit, or any Expr when Computed; nil for PropSpread
	Computed bool
	Value    Expr // value, method *Function, spread operand, or cover default
}

// ObjectLit is an object literal.
type ObjectLit struct {
	Span
	Props []*Property

	parenthesized bool
	restComma     bool // a comma follows a spread: not convertible to a pattern
	// dupProto is the position of a second `__proto__: v` entry, an early
	// error unless the literal becomes an assignment pattern (scope pass).
	dupProto int
}

// FuncKind distinguishes the syntactic forms of a function.
type FuncKind uint8

const (
	FuncNormal FuncKind = iota
	FuncArrow
	FuncMethod
	FuncGetter
	FuncSetter
	FuncClassConstructor
	// FuncClassFields and FuncClassStatic are the synthetic methods the
	// resolver creates to run a class's instance field initializers and its
	// static fields and blocks (Class.Fields, Class.Static). They have no
	// source of their own and no Body.
	FuncClassFields
	FuncClassStatic
)

// Function is any function: declaration body, expression, arrow or method.
// FuncDecl wraps it in statement position. Span covers the whole source text
// from the first token (`function`, `async`, the method name or the arrow
// parameters) to the end of the body, for Function.prototype.toString.
type Function struct {
	Span
	Name        *Ident // nil when anonymous
	Params      []Pattern
	Rest        Pattern    // rest parameter target or nil
	Body        *BlockStmt // nil for a concise arrow body
	ExprBody    Expr       // concise arrow body
	Kind        FuncKind
	IsArrow     bool
	IsAsync     bool
	IsGenerator bool
	IsStrict    bool // strict mode code: a class member, inside strict code, or with its own "use strict"

	// Scope annotations.
	Scope           *Scope // parameters, var declarations and body-level lexical declarations (Body.Scope when kept apart)
	SelfBinding     *Binding
	UsesThis        bool // `this` (or super/new.target) appears in the body or in a nested arrow
	UsesArguments   bool // `arguments` appears in the body or in a nested arrow
	HasDirectEval   bool // a direct eval call appears in the function or a function nested in it
	ParamCount      int  // formal parameters excluding rest
	Length          int  // ES `length`: formals before the first default or rest
	HasSimpleParams bool // every formal is a plain identifier without default; no rest
	HasRest         bool

	// Class, super and new.target annotations. The hidden bindings live in
	// the function scope (registers or captured slots like any binding) and
	// are nil when unused.
	Derived          bool     // constructor of a class with an extends clause
	DefaultCtor      bool     // constructor synthesized for a class without one
	ThisBinding      *Binding // derived constructor: `this`, in TDZ until super() returns
	CalleeBinding    *Binding // derived constructor: the active function, read by super()
	NewTargetBinding *Binding // new.target, used here or in a nested arrow
	HomeBinding      *Binding // [[HomeObject]], for super property access here or in a nested arrow
}

// Class is a class declaration or expression.
type Class struct {
	Span
	Name    *Ident
	Super   Expr
	Members []*ClassMember

	// Scope annotations. Scope (kind ScopeClass) holds the inner name
	// binding, one binding per private name (named "#x") and the hidden
	// bindings of the class machinery; the heritage, the computed keys and
	// every member function are resolved inside it.
	Scope         *Scope
	NameBinding   *Binding  // inner immutable class-name binding (named classes)
	Ctor          *Function // the constructor; synthesized (DefaultCtor) when the body has none
	Fields        *Function // FuncClassFields: instance fields and the private-method brand (nil when none)
	Static        *Function // FuncClassStatic: static fields and blocks in order (nil when none)
	FieldsBinding *Binding  // hidden binding holding the Fields closure, read by the constructor
	BrandBinding  *Binding  // hidden binding holding the brand of the instance private methods (nil when none)
}

// ClassMemberKind classifies class body entries.
type ClassMemberKind uint8

const (
	ClassMethod ClassMemberKind = iota
	ClassGetter
	ClassSetter
	ClassField
	ClassStaticBlock
)

// ClassMember is one entry of a class body.
type ClassMember struct {
	Span
	Kind     ClassMemberKind
	Static   bool
	Key      Expr // *Ident, *StringLit, *NumberLit, *BigIntLit, *PrivateName, or computed Expr
	Computed bool
	Value    Expr       // *Function for methods, initializer for fields (may be nil)
	Body     *BlockStmt // static block body

	// Scope annotations. KeyBinding is the hidden binding that carries a
	// computed field key from class definition to the field initializer.
	// Block is a static block's body as an arrow function nested in
	// Class.Static, which gives it its own var scope and the static
	// initializer's this, home object and new.target.
	KeyBinding *Binding
	Block      *Function
}

// UnaryExpr is a prefix operator: ! ~ - + typeof void delete.
type UnaryExpr struct {
	Span
	Op Token
	X  Expr
}

// UpdateExpr is ++ or -- in prefix or postfix form. X is *Ident,
// *MemberExpr or a sloppy *CallExpr (which throws after the call).
type UpdateExpr struct {
	Span
	Op     Token // Inc or Dec
	Prefix bool
	X      Expr
}

// BinaryExpr is an arithmetic, bitwise, comparison, `in` or `instanceof`
// expression.
type BinaryExpr struct {
	Span
	Op   Token
	X, Y Expr
}

// LogicalExpr is && || ??; Y is evaluated conditionally.
type LogicalExpr struct {
	Span
	Op   Token
	X, Y Expr

	parenthesized bool
}

// AssignExpr is `=` or a compound assignment. Target is *Ident, *MemberExpr,
// a sloppy *CallExpr (not for logical assignment; it throws after the call)
// or (for `=` only) a destructuring pattern.
type AssignExpr struct {
	Span
	Op          Token
	ParenTarget bool // Target is a parenthesised identifier: not an IdentifierRef, so the value is not named after it
	Target      Pattern
	Value       Expr
}

// CondExpr is Test ? Cons : Alt.
type CondExpr struct {
	Span
	Test, Cons, Alt Expr
}

// SeqExpr is the comma operator.
type SeqExpr struct {
	Span
	Exprs []Expr
}

// CallExpr is a call. Optional marks `f?.()`.
type CallExpr struct {
	Span
	Callee   Expr
	Args     []Expr // may contain *SpreadElem
	Optional bool
}

// NewExpr is `new Callee(Args)`.
type NewExpr struct {
	Span
	Callee Expr
	Args   []Expr
}

// MemberExpr is property access. Prop is an *Ident (never resolved) or a
// *PrivateName when !Computed. Optional marks `a?.b`.
type MemberExpr struct {
	Span
	Object   Expr
	Prop     Expr
	Computed bool
	Optional bool
}

// PrivateName is `#x` in a member, `#x in o` or class element position.
// Binding is the class-scope binding of the private name (named "#x").
type PrivateName struct {
	Span
	Name    string // without '#'
	Binding *Binding
}

// OptChain wraps the whole of an optional chain `a?.b.c()`; when any
// Optional link short-circuits, the value of the OptChain is undefined.
type OptChain struct {
	Span
	X Expr // *MemberExpr or *CallExpr
}

// SpreadElem is `...X` in an array literal or argument list.
type SpreadElem struct {
	Span
	X Expr
}

// YieldExpr is `yield X` or `yield* X`.
type YieldExpr struct {
	Span
	X        Expr // may be nil
	Delegate bool
}

// AwaitExpr is `await X`.
type AwaitExpr struct {
	Span
	X Expr
}

// ---- Patterns ---------------------------------------------------------------

// PatternProp is one entry of an object pattern.
type PatternProp struct {
	Span
	Key      Expr // as Property.Key
	Computed bool
	Value    Pattern
}

// ObjectPattern is { a, b: c, ...rest }.
type ObjectPattern struct {
	Span
	Props []*PatternProp
	Rest  Pattern // *Ident or (assignment only) *MemberExpr, or nil
}

// ArrayPattern is [a, , b = 1, ...rest]. A nil element is an elision.
type ArrayPattern struct {
	Span
	Elems []Pattern
	Rest  Pattern
}

// AssignPattern is Target = Default inside a pattern or parameter list.
type AssignPattern struct {
	Span
	Target  Pattern
	Default Expr
}

// ---- Statements -------------------------------------------------------------

// DeclKind is var, let or const.
type DeclKind uint8

const (
	DeclVar DeclKind = iota
	DeclLet
	DeclConst
)

func (k DeclKind) String() string {
	switch k {
	case DeclVar:
		return "var"
	case DeclLet:
		return "let"
	}
	return "const"
}

// Declarator is one `Target = Init` of a variable declaration.
type Declarator struct {
	Span
	Target Pattern
	Init   Expr // may be nil
}

// VarDecl is a var/let/const declaration.
type VarDecl struct {
	Span
	Kind  DeclKind
	Decls []*Declarator
}

// FuncDecl is a function declaration in statement position.
type FuncDecl struct {
	Span
	Func *Function
}

// ClassDecl is a class declaration (unsupported).
type ClassDecl struct {
	Span
	Class *Class
}

// ExprStmt is an expression statement.
type ExprStmt struct {
	Span
	X Expr
}

// BlockStmt is `{ ... }`. Scope is nil unless the block declares lexical
// bindings (let/const/class/function).
type BlockStmt struct {
	Span
	Body  []Stmt
	Scope *Scope
}

// EmptyStmt is `;`.
type EmptyStmt struct{ Span }

// DebuggerStmt is `debugger;` (a no-op).
type DebuggerStmt struct{ Span }

// IfStmt is if/else.
type IfStmt struct {
	Span
	Cond Expr
	Then Stmt
	Else Stmt // may be nil
}

// ForStmt is the classic for loop. Init is nil, *VarDecl or an Expr. Scope
// is nil unless Init is a let/const declaration.
type ForStmt struct {
	Span
	Init   Node
	Cond   Expr
	Update Expr
	Body   Stmt
	Scope  *Scope
}

// ForInOfStmt is for-in (Of == false) or for-of. Left is a *VarDecl with a
// single declarator, or a Pattern. The declarator has no initializer, except
// a sloppy `for (var x = init in o)` (Annex B). Scope is nil unless Left is
// a let/const declaration.
type ForInOfStmt struct {
	Span
	Of    bool
	Await bool // for await
	Left  Node
	Right Expr
	Body  Stmt
	Scope *Scope
}

// WhileStmt is while.
type WhileStmt struct {
	Span
	Cond Expr
	Body Stmt
}

// DoWhileStmt is do/while.
type DoWhileStmt struct {
	Span
	Body Stmt
	Cond Expr
}

// ReturnStmt is return.
type ReturnStmt struct {
	Span
	Result Expr // may be nil
}

// ThrowStmt is throw.
type ThrowStmt struct {
	Span
	X Expr
}

// BreakStmt is break, optionally labelled.
type BreakStmt struct {
	Span
	Label *Ident
}

// ContinueStmt is continue, optionally labelled.
type ContinueStmt struct {
	Span
	Label *Ident
}

// WithStmt is `with (Object) Body` (sloppy code only). Scope holds the
// hidden binding of the object environment the body's names consult.
type WithStmt struct {
	Span
	Object Expr
	Body   Stmt
	Scope  *Scope
}

// LabeledStmt is `Label: Body`.
type LabeledStmt struct {
	Span
	Label *Ident
	Body  Stmt
}

// SwitchCase is one case (Test == nil for default).
type SwitchCase struct {
	Span
	Test Expr
	Body []Stmt
}

// SwitchStmt is switch. Scope is nil unless the case block declares lexical
// bindings; such bindings always need TDZ checks.
type SwitchStmt struct {
	Span
	Disc  Expr
	Cases []*SwitchCase
	Scope *Scope
}

// TryStmt is try/catch/finally. Param is nil for `catch {` or when there is
// no catch clause; CatchScope holds the catch parameter bindings.
type TryStmt struct {
	Span
	Block      *BlockStmt
	Param      Pattern
	Handler    *BlockStmt
	Finalizer  *BlockStmt
	CatchScope *Scope
}

// ExportDecl is `export <declaration>`.
type ExportDecl struct {
	Span
	Decl Stmt // *VarDecl, *FuncDecl or *ClassDecl
}

// ExportSpec is `Local as Exported` (Exported may be spelled as a string).
type ExportSpec struct {
	Span
	Local    *Ident
	Exported *Ident
}

// ExportNamed is `export { ... }` optionally `from "..."`.
type ExportNamed struct {
	Span
	Specs  []*ExportSpec
	Source *StringLit
}

// ExportDefault is `export default ...`. Decl is a *FuncDecl or *ClassDecl
// for named/anonymous declarations, or an Expr.
type ExportDefault struct {
	Span
	Decl Node
}

// ExportAll is `export * [as x] from "..."`.
type ExportAll struct {
	Span
	As     *Ident
	Source *StringLit
}

// ImportSpec is `Imported as Local`.
type ImportSpec struct {
	Span
	Imported *Ident
	Local    *Ident
}

// ImportDecl is an import declaration.
type ImportDecl struct {
	Span
	Default   *Ident
	Namespace *Ident
	Specs     []*ImportSpec
	Source    *StringLit
}

// ---- Marker methods ---------------------------------------------------------

func (*Ident) exprNode()          {}
func (*ThisExpr) exprNode()       {}
func (*SuperExpr) exprNode()      {}
func (*NewTarget) exprNode()      {}
func (*ImportMeta) exprNode()     {}
func (*ImportCall) exprNode()     {}
func (*NumberLit) exprNode()      {}
func (*BigIntLit) exprNode()      {}
func (*StringLit) exprNode()      {}
func (*BoolLit) exprNode()        {}
func (*NullLit) exprNode()        {}
func (*RegexLit) exprNode()       {}
func (*TemplateLit) exprNode()    {}
func (*TaggedTemplate) exprNode() {}
func (*ArrayLit) exprNode()       {}
func (*ObjectLit) exprNode()      {}
func (*Function) exprNode()       {}
func (*Class) exprNode()          {}
func (*UnaryExpr) exprNode()      {}
func (*UpdateExpr) exprNode()     {}
func (*BinaryExpr) exprNode()     {}
func (*LogicalExpr) exprNode()    {}
func (*AssignExpr) exprNode()     {}
func (*CondExpr) exprNode()       {}
func (*SeqExpr) exprNode()        {}
func (*CallExpr) exprNode()       {}
func (*NewExpr) exprNode()        {}
func (*MemberExpr) exprNode()     {}
func (*PrivateName) exprNode()    {}
func (*OptChain) exprNode()       {}
func (*SpreadElem) exprNode()     {}
func (*YieldExpr) exprNode()      {}
func (*AwaitExpr) exprNode()      {}

func (*Ident) patternNode()         {}
func (*MemberExpr) patternNode()    {}
func (*CallExpr) patternNode()      {} // a sloppy assignment target that throws (Annex B)
func (*ObjectPattern) patternNode() {}
func (*ArrayPattern) patternNode()  {}
func (*AssignPattern) patternNode() {}

func (*VarDecl) stmtNode()       {}
func (*FuncDecl) stmtNode()      {}
func (*ClassDecl) stmtNode()     {}
func (*ExprStmt) stmtNode()      {}
func (*BlockStmt) stmtNode()     {}
func (*EmptyStmt) stmtNode()     {}
func (*DebuggerStmt) stmtNode()  {}
func (*IfStmt) stmtNode()        {}
func (*ForStmt) stmtNode()       {}
func (*ForInOfStmt) stmtNode()   {}
func (*WhileStmt) stmtNode()     {}
func (*DoWhileStmt) stmtNode()   {}
func (*ReturnStmt) stmtNode()    {}
func (*ThrowStmt) stmtNode()     {}
func (*BreakStmt) stmtNode()     {}
func (*ContinueStmt) stmtNode()  {}
func (*LabeledStmt) stmtNode()   {}
func (*WithStmt) stmtNode()      {}
func (*SwitchStmt) stmtNode()    {}
func (*TryStmt) stmtNode()       {}
func (*ExportDecl) stmtNode()    {}
func (*ExportNamed) stmtNode()   {}
func (*ExportDefault) stmtNode() {}
func (*ExportAll) stmtNode()     {}
func (*ImportDecl) stmtNode()    {}

// ---- Pattern helpers --------------------------------------------------------

// boundNames appends the identifiers bound by a pattern in source order.
func boundNames(pat Pattern, out []*Ident) []*Ident {
	switch pat := pat.(type) {
	case *Ident:
		return append(out, pat)
	case *AssignPattern:
		return boundNames(pat.Target, out)
	case *ObjectPattern:
		for _, prop := range pat.Props {
			out = boundNames(prop.Value, out)
		}
		if pat.Rest != nil {
			out = boundNames(pat.Rest, out)
		}
	case *ArrayPattern:
		for _, e := range pat.Elems {
			if e != nil {
				out = boundNames(e, out)
			}
		}
		if pat.Rest != nil {
			out = boundNames(pat.Rest, out)
		}
	}
	return out
}

// BoundNames returns the identifiers bound by a pattern in source order.
func BoundNames(pat Pattern) []*Ident { return boundNames(pat, nil) }

// isSimpleParam reports whether a parameter is a plain identifier.
func isSimpleParam(pat Pattern) bool {
	_, ok := pat.(*Ident)
	return ok
}
