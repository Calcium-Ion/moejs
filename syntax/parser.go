package syntax

import (
	"strconv"
	"unicode/utf8"
)

// MaxNestingDepth bounds the parser's recursion. Every nested statement,
// function, assignment expression, unary operator, `new`, class or binding
// pattern costs one unit, so a source-level construct costs one to three
// (`(` is an assignment expression and a unary operand). Real plugin code
// nests a few dozen levels; a 1 MB run of `(` used to overflow the Go stack,
// which is a fatal error that kills the process. The scope pass and the
// compiler recurse over the tree the parser produced, so the bound covers
// them as well.
const MaxNestingDepth = 10000

// parser is a recursive-descent parser with precedence climbing for binary
// operators. The lexer is embedded so that the current token is read from
// plain fields with no indirection.
type parser struct {
	lexer
	stop     func() error // Options.Stop
	isModule bool
	awaitKw  bool // await is a keyword: in modules and async functions
	yieldKw  bool // yield is a keyword in sloppy code: in generators
	prevEnd  int  // end offset of the previously consumed token
	depth    int  // recursion units in flight (MaxNestingDepth)

	// Statement context; saved and reset at every function boundary.
	funcDepth     int // functions of any kind enclosing the current position
	nonArrowDepth int // non-arrow functions enclosing the current position
	inLoop        int32
	inSwitch      int32
	labels        []label
	labelBase     int // labels below this index belong to enclosing functions
	pendingLabels int // labels attached to the statement about to be parsed

	re     *regexpChecker    // taken from regexpCache at the first regexp literal
	parens map[Expr]struct{} // parenthesised identifiers and assignments (markParenthesized)
	// asyncNL is 1 + the start of the last `async (...)` call whose ( follows
	// a line terminator: not an async arrow head.
	asyncNL int
	// spreadComma is the position of the last comma that followed a spread
	// call argument: a trailing one rules out an async arrow's rest.
	spreadComma int
}

type label struct {
	name string
	loop bool
}

// ParseModule parses src as an ES module (always strict mode code), runs
// the early-error and scope-resolution passes, and returns the annotated
// tree. The error, when non-nil, is a *Error, or the error Options.Stop
// returned.
func ParseModule(name, src string, opts Options) (*Module, error) {
	p := newParser(name, src, opts, true)
	m := &Module{}
	if err := p.run(func() { m.Program = p.parseProgram() }); err != nil {
		return nil, err
	}
	if err := resolveModule(m, opts); err != nil {
		return nil, err
	}
	return m, nil
}

// ParseScript parses src as a script: sloppy mode code unless its
// directive prologue holds a "use strict" directive (Script.Strict).
// Import and export declarations are rejected; HTML-like comments are
// recognised.
func ParseScript(name, src string, opts Options) (*Script, error) {
	p := newParser(name, src, opts, false)
	s := &Script{}
	if err := p.run(func() { s.Program = p.parseProgram(); s.Strict = p.strict }); err != nil {
		return nil, err
	}
	if err := resolveScript(s, opts); err != nil {
		return nil, err
	}
	return s, nil
}

func newParser(name, src string, opts Options, isModule bool) *parser {
	p := &parser{stop: opts.Stop, isModule: isModule, awaitKw: isModule}
	p.html, p.strict = !isModule, isModule
	p.lexer.init(newFile(name, src))
	return p
}

// run executes f, converting a bailout panic into the recorded error and a
// stopped one into Options.Stop's.
func (p *parser) run(f func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			switch r := r.(type) {
			case bailout:
				err = p.err
			case stopped:
				err = r.err
			default:
				panic(r)
			}
		}
		p.releaseRegexp()
	}()
	f()
	return nil
}

// enter counts one unit of recursion and fails the parse past
// MaxNestingDepth; leave undoes it.
func (p *parser) enter() {
	p.depth++
	if p.depth > MaxNestingDepth {
		p.fail(p.start, "Nesting too deep (the parser allows "+strconv.Itoa(MaxNestingDepth)+" levels)")
	}
}

func (p *parser) leave() { p.depth-- }

// ---- Token helpers ----------------------------------------------------------

func (p *parser) next() {
	p.prevEnd = p.end
	p.lexer.next()
	if reclassifiable[p.tok] {
		p.reclassify()
	}
}

// reclassifiable marks the tokens reclassify may turn into an Identifier.
var reclassifiable = func() (t [256]bool) {
	t[KwAwait], t[EscapedWord] = true, true
	for tok := KwYield; tok < keywordEnd; tok++ {
		t[tok] = true
	}
	return t
}()

// reclassify turns the current reserved-word token into an Identifier where
// the context does not reserve it: await outside modules and async
// functions, yield in sloppy code outside generators, and the strict-mode
// reserved words in sloppy code. `let` stays KwLet; the statement parser
// tells a sloppy-mode `let` identifier from a declaration. An escaped
// spelling (EscapedWord) becomes an Identifier under the same rule.
func (p *parser) reclassify() {
	tok := p.tok
	if tok == EscapedWord {
		tok = lookupKeyword(p.val)
	}
	switch tok {
	case KwAwait:
		if p.awaitKw {
			return
		}
	case KwYield:
		if p.yieldKw || p.strict {
			return
		}
	case KwLet:
		if p.tok == KwLet || p.strict {
			return
		}
	case KwImplements, KwInterface, KwPackage, KwPrivate, KwProtected, KwPublic, KwStatic:
		if p.strict {
			return
		}
	default:
		return
	}
	p.tok = Identifier
}

// failLegacy reports the legacy octal literal or escape of the current
// token, which strict code rejects.
func (p *parser) failLegacy() {
	pos, msg := p.legacyError()
	p.fail(pos, msg)
}

// peek returns the token after the current one without consuming anything.
// A legacy literal fails only once next consumes it.
func (p *parser) peek() (tok Token, nlBefore bool, val string) {
	saved := p.lexer
	p.lexer.strict = false
	p.lexer.next()
	p.lexer.strict = saved.strict
	if reclassifiable[p.tok] {
		p.reclassify()
	}
	tok, nlBefore, val = p.tok, p.nlBefore, p.val
	p.lexer = saved
	return tok, nlBefore, val
}

// reword reclassifies the current token after a switch of the strictness or
// of the await or yield keyword context, since it was scanned before the
// switch. Only the contextual reserved words change class; every function
// boundary calls it, so other tokens return at once.
func (p *parser) reword() {
	switch p.tok {
	case Identifier:
		if !contextualWord(p.val) {
			return
		}
	case EscapedWord, KwAwait:
	default:
		if p.tok < KwYield || p.tok >= keywordEnd {
			return
		}
	}
	if tok := lookupKeyword(p.val); tok == Identifier {
		return
	} else if p.escaped {
		p.tok = EscapedWord
	} else {
		p.tok = tok
	}
	p.reclassify()
}

// contextualWord reports whether s spells a reserved word that only some
// code reserves: await, yield and the strict mode reserved words.
func contextualWord(s string) bool {
	switch s {
	case "await", "yield", "let", "implements", "interface", "package", "private", "protected", "public", "static":
		return true
	}
	return false
}

// setAwaitKw switches whether await is a keyword, reclassifying the current
// token, which was scanned before the switch.
func (p *parser) setAwaitKw(on bool) {
	p.awaitKw = on
	p.reword()
}

// setYieldKw switches whether yield is a keyword in sloppy code.
func (p *parser) setYieldKw(on bool) {
	p.yieldKw = on
	p.reword()
}

// setStrict switches the parser to strict mode code, rejecting the current
// token if it was a legacy literal scanned before the switch.
func (p *parser) setStrict() {
	p.strict = true
	p.reword()
	if p.legacy != legacyNone {
		p.failLegacy()
	}
}

// isLetDecl reports whether the current `let` token starts a lexical
// declaration in sloppy code: when followed by a binding identifier or
// pattern. In a single-statement context a line break before a binding
// identifier makes the `let` an expression statement instead (sameLine).
func (p *parser) isLetDecl(sameLine bool) bool {
	tok, nl, _ := p.peek()
	switch tok {
	case LBrack:
		return true
	case LBrace, Identifier, KwLet, KwYield, KwAwait:
		return !sameLine || !nl
	}
	return false
}

func (p *parser) eat(tok Token) bool {
	if p.tok != tok {
		return false
	}
	p.next()
	return true
}

func (p *parser) expect(tok Token) {
	if p.tok != tok {
		p.unexpected()
	}
	p.next()
}

// isIdent reports whether the current token is the contextual keyword name
// (which is never a reserved word). An escaped spelling is an identifier
// and never acts as the keyword.
func (p *parser) isIdent(name string) bool {
	return p.tok == Identifier && p.val == name && !p.escaped
}

// unexpected reports the current token as unexpected, with V8-style wording.
// A template chunk with an invalid escape reports the escape instead.
func (p *parser) unexpected() {
	if p.tok == Template && p.badEscape != 0 {
		p.failBadEscape()
	}
	p.unexpectedAt(p.tok, p.start, p.text())
}

// failBadEscape reports the invalid escape of the current template chunk.
func (p *parser) failBadEscape() {
	p.fail(p.badEscape-1, p.templateEscapeError(p.badEscape))
}

// unexpectedAt reports token tok spelled text at pos as unexpected.
func (p *parser) unexpectedAt(tok Token, pos int, text string) {
	switch tok {
	case EOF:
		p.fail(pos, "Unexpected end of input")
	case Identifier:
		p.fail(pos, "Unexpected identifier '"+text+"'")
	case EscapedWord:
		p.fail(pos, "Keyword must not contain escaped characters")
	case Number, BigInt:
		p.fail(pos, "Unexpected number")
	case String:
		p.fail(pos, "Unexpected string")
	case Template:
		p.fail(pos, "Unexpected template string")
	case KwAwait:
		p.fail(pos, "Unexpected reserved word")
	case KwYield, KwLet, KwImplements, KwInterface, KwPackage, KwPrivate, KwProtected, KwPublic, KwStatic:
		p.fail(pos, "Unexpected strict mode reserved word")
	}
	p.fail(pos, "Unexpected token '"+text+"'")
}

// semicolon consumes a statement terminator or applies automatic semicolon
// insertion: a `;`, or a `}`/end of input/line break before the next token.
func (p *parser) semicolon() {
	if p.tok == Semicolon {
		p.next()
		return
	}
	if p.tok == RBrace || p.tok == EOF || p.nlBefore {
		return
	}
	p.unexpected()
}

// ---- Program and statements -------------------------------------------------

func (p *parser) parseProgram() Program {
	p.next()
	prog := Program{File: p.file}
	prog.Pos = 0
	if !p.isModule {
		prog.Body = p.parseDirectives(nil)
	}
	for p.tok != EOF {
		poll(p.stop, len(prog.Body))
		prog.Body = append(prog.Body, p.parseModuleItem())
	}
	prog.End = len(p.src)
	return prog
}

// parseDirectives parses the directive prologue of a script or a function
// body (fn): the leading statements that are a bare string literal. A "use
// strict" directive makes the code strict mode code and applies the strict
// rules retroactively to what was scanned before it: the function's name
// and parameters and the prologue's earlier strings. A directive is matched
// on its source text, so escapes or parentheses make it an ordinary string.
func (p *parser) parseDirectives(fn *Function) (body []Stmt) {
	legacyPos, legacyMsg := -1, ""
	for p.tok == String {
		start, end := p.start, p.end
		if p.legacy != legacyNone && legacyPos < 0 {
			legacyPos, legacyMsg = p.legacyError()
		}
		poll(p.stop, len(body))
		s := p.parseStatement()
		body = append(body, s)
		es, ok := s.(*ExprStmt)
		if !ok {
			return body
		}
		if str, ok := es.X.(*StringLit); !ok || str.Pos != start || str.End != end {
			return body
		}
		if p.src[start+1:end-1] != "use strict" {
			continue
		}
		if fn != nil && !fn.HasSimpleParams {
			p.fail(start, "Illegal 'use strict' directive in function with non-simple parameter list")
		}
		if p.strict {
			continue
		}
		if legacyPos >= 0 {
			p.fail(legacyPos, legacyMsg)
		}
		p.setStrict()
		if fn != nil {
			fn.IsStrict = true
			p.checkStrictParams(fn)
		}
	}
	return body
}

// checkStrictParams applies the strict mode rules to the name and the
// (simple) parameters of a function found to be strict by its own "use
// strict" directive, which were parsed as sloppy mode code.
func (p *parser) checkStrictParams(fn *Function) {
	if fn.Name != nil && fn.Kind == FuncNormal {
		p.checkStrictName(fn.Name)
	}
	for i, param := range fn.Params {
		id := param.(*Ident)
		p.checkStrictName(id)
		for _, prev := range fn.Params[:i] {
			if prev.(*Ident).Name == id.Name {
				p.fail(id.Pos, "Duplicate parameter name not allowed in this context")
			}
		}
	}
}

// checkStrictName rejects a binding name that strict mode code reserves.
func (p *parser) checkStrictName(id *Ident) {
	switch id.Name {
	case "eval", "arguments":
		p.fail(id.Pos, "Unexpected eval or arguments in strict mode")
	case "implements", "interface", "let", "package", "private", "protected", "public", "static", "yield":
		p.fail(id.Pos, "Unexpected strict mode reserved word")
	}
}

// parseModuleItem parses a top-level item, where import/export are allowed.
func (p *parser) parseModuleItem() Stmt {
	switch p.tok {
	case KwImport:
		if tok, _, _ := p.peek(); tok != LParen && tok != Dot {
			if !p.isModule {
				p.fail(p.start, "Cannot use import statement outside a module")
			}
			return p.parseImport()
		}
	case KwExport:
		if !p.isModule {
			p.unexpected()
		}
		return p.parseExport()
	}
	return p.parseStatementListItem()
}

// parseStatementListItem parses a statement or declaration.
func (p *parser) parseStatementListItem() Stmt {
	switch p.tok {
	case KwFunction:
		return p.parseFunctionDecl(p.start, false, true)
	case KwClass:
		return p.parseClassDecl(true)
	case KwConst:
		return p.parseVarDeclStmt(DeclConst)
	case KwLet:
		if p.strict || p.isLetDecl(false) {
			return p.parseVarDeclStmt(DeclLet)
		}
	case KwImport:
		if tok, _, _ := p.peek(); tok != LParen && tok != Dot {
			p.fail(p.start, "'import' and 'export' may only appear at the top level")
		}
	case KwExport:
		p.fail(p.start, "'import' and 'export' may only appear at the top level")
	case Identifier:
		if p.isIdent("async") {
			if tok, nl, _ := p.peek(); tok == KwFunction && !nl {
				start := p.start
				p.next()
				return p.parseFunctionDecl(start, true, true)
			}
		}
	}
	return p.parseStatement()
}

func (p *parser) parseStatement() Stmt {
	p.enter()
	defer p.leave()
	pending := p.pendingLabels
	p.pendingLabels = 0
	start := p.start
	switch p.tok {
	case LBrace:
		return p.parseBlock()
	case Semicolon:
		p.next()
		return &EmptyStmt{Span{start, p.prevEnd}}
	case KwVar:
		return p.parseVarDeclStmt(DeclVar)
	case KwIf:
		return p.parseIf()
	case KwFor, KwWhile, KwDo:
		for i := len(p.labels) - pending; i < len(p.labels); i++ {
			p.labels[i].loop = true
		}
		switch p.tok {
		case KwFor:
			return p.parseFor()
		case KwWhile:
			return p.parseWhile()
		}
		return p.parseDoWhile()
	case KwReturn:
		return p.parseReturn()
	case KwThrow:
		return p.parseThrow()
	case KwBreak:
		return p.parseBreakContinue(true)
	case KwContinue:
		return p.parseBreakContinue(false)
	case KwSwitch:
		return p.parseSwitch()
	case KwTry:
		return p.parseTry()
	case KwDebugger:
		p.next()
		p.semicolon()
		return &DebuggerStmt{Span{start, p.prevEnd}}
	case KwWith:
		return p.parseWith()
	case KwFunction:
		p.failFunctionPlacement(start)
	case KwClass:
		p.unexpected()
	case KwLet:
		// In sloppy code `let` is an identifier where a declaration cannot
		// be, but an expression statement never starts with `let [`.
		if !p.strict {
			tok, _, _ := p.peek()
			if tok == Colon {
				p.pendingLabels = pending
				return p.parseLabeled()
			}
			if !p.isLetDecl(true) {
				break
			}
		}
		p.fail(start, "Lexical declaration cannot appear in a single-statement context")
	case KwConst:
		p.fail(start, "Lexical declaration cannot appear in a single-statement context")
	case Identifier:
		if p.isIdent("async") {
			if tok, nl, _ := p.peek(); tok == KwFunction && !nl {
				p.fail(start, "Async functions can only be declared at the top level or inside a block.")
			}
		}
		if tok, _, _ := p.peek(); tok == Colon {
			p.pendingLabels = pending
			return p.parseLabeled()
		}
	}
	x := p.parseExpression(false)
	p.semicolon()
	return &ExprStmt{Span{start, p.prevEnd}, x}
}

// failFunctionPlacement rejects a function declaration where only a
// statement may appear.
func (p *parser) failFunctionPlacement(pos int) {
	if p.strict {
		p.fail(pos, "In strict mode code, functions can only be declared at top level or inside a block.")
	}
	p.fail(pos, "In non-strict mode code, functions can only be declared at top level, inside a block, or as the body of an if statement.")
}

// checkLabelledFunction rejects a labelled function declaration as the body
// of an if, iteration or with statement (Annex B.3.1 allows one only where
// a declaration may appear).
func (p *parser) checkLabelledFunction(body Stmt) {
	for {
		l, ok := body.(*LabeledStmt)
		if !ok {
			return
		}
		if fd, ok := l.Body.(*FuncDecl); ok {
			p.failFunctionPlacement(fd.Pos)
		}
		body = l.Body
	}
}

func (p *parser) parseBlock() *BlockStmt {
	start := p.start
	p.expect(LBrace)
	var body []Stmt
	for p.tok != RBrace {
		if p.tok == EOF {
			p.unexpected()
		}
		poll(p.stop, len(body))
		body = append(body, p.parseStatementListItem())
	}
	p.next()
	return &BlockStmt{Span: Span{start, p.prevEnd}, Body: body}
}

func (p *parser) parseVarDeclStmt(kind DeclKind) Stmt {
	decl := p.parseVarDecl(kind, false, false)
	p.semicolon()
	decl.End = p.prevEnd
	return decl
}

// parseVarDecl parses `var|let|const` declarators. In a for-loop head the
// initializer requirements are checked by the caller once the loop form is
// known.
func (p *parser) parseVarDecl(kind DeclKind, noIn, forHead bool) *VarDecl {
	decl := &VarDecl{Span: Span{p.start, 0}, Kind: kind}
	p.next()
	for {
		d := &Declarator{Span: Span{p.start, 0}}
		d.Target = p.parseBindingTarget()
		if kind != DeclVar && !p.strict {
			p.checkLexicalLet(d.Target)
		}
		if p.eat(Assign) {
			d.Init = p.parseAssign(noIn)
		} else if !forHead {
			p.checkDeclaratorInit(kind, d)
		}
		d.End = p.prevEnd
		decl.Decls = append(decl.Decls, d)
		if !p.eat(Comma) {
			break
		}
	}
	decl.End = p.prevEnd
	return decl
}

// checkLexicalLet rejects `let` as a name bound by a let or const
// declaration, which sloppy code otherwise allows as an identifier.
func (p *parser) checkLexicalLet(target Pattern) {
	if id, ok := target.(*Ident); ok && id.Name != "let" {
		return
	}
	for _, id := range boundNames(target, nil) {
		if id.Name == "let" {
			p.fail(id.Pos, "let is disallowed as a lexically bound name")
		}
	}
}

func (p *parser) checkDeclaratorInit(kind DeclKind, d *Declarator) {
	if _, ok := d.Target.(*Ident); !ok {
		p.fail(d.Pos, "Missing initializer in destructuring declaration")
	}
	if kind == DeclConst {
		p.fail(d.Pos, "Missing initializer in const declaration")
	}
}

func (p *parser) parseIf() Stmt {
	start := p.start
	p.next()
	p.expect(LParen)
	cond := p.parseExpression(false)
	p.expect(RParen)
	then := p.parseIfClause()
	var els Stmt
	if p.eat(KwElse) {
		els = p.parseIfClause()
	}
	return &IfStmt{Span{start, p.prevEnd}, cond, then, els}
}

// parseIfClause parses the body of an if or else clause. Annex B.3.3 lets
// sloppy code put a plain function declaration there, as if in a block of
// its own.
func (p *parser) parseIfClause() Stmt {
	if p.tok != KwFunction || p.strict {
		body := p.parseStatement()
		p.checkLabelledFunction(body)
		return body
	}
	start := p.start
	if tok, _, _ := p.peek(); tok == Mul {
		p.fail(start, "Generators can only be declared at the top level or inside a block.")
	}
	decl := p.parseFunctionDecl(start, false, true)
	return &BlockStmt{Span: decl.Span, Body: []Stmt{decl}}
}

func (p *parser) parseLoopBody() Stmt {
	p.inLoop++
	body := p.parseStatement()
	p.inLoop--
	p.checkLabelledFunction(body)
	return body
}

// parseWith parses `with (object) body`, which strict code does not allow.
func (p *parser) parseWith() Stmt {
	start := p.start
	if p.strict {
		p.fail(start, "Strict mode code may not include a with statement")
	}
	p.next()
	p.expect(LParen)
	obj := p.parseExpression(false)
	p.expect(RParen)
	body := p.parseStatement()
	p.checkLabelledFunction(body)
	return &WithStmt{Span: Span{start, p.prevEnd}, Object: obj, Body: body}
}

func (p *parser) parseWhile() Stmt {
	start := p.start
	p.next()
	p.expect(LParen)
	cond := p.parseExpression(false)
	p.expect(RParen)
	body := p.parseLoopBody()
	return &WhileStmt{Span{start, p.prevEnd}, cond, body}
}

func (p *parser) parseDoWhile() Stmt {
	start := p.start
	p.next()
	body := p.parseLoopBody()
	p.expect(KwWhile)
	p.expect(LParen)
	cond := p.parseExpression(false)
	p.expect(RParen)
	p.eat(Semicolon) // the semicolon after do-while is always optional
	return &DoWhileStmt{Span{start, p.prevEnd}, body, cond}
}

func (p *parser) parseFor() Stmt {
	start := p.start
	p.next()
	isAwait := p.eat(KwAwait)
	p.expect(LParen)

	var init Node
	tok := p.tok
	if tok == KwLet && !p.strict && !p.isLetDecl(false) {
		tok = Identifier // `let` as an identifier: for (let in o), for (let.x;;)
	}
	switch tok {
	case Semicolon:
		// no initializer
	case KwVar, KwLet, KwConst:
		kind := DeclVar
		switch p.tok {
		case KwLet:
			kind = DeclLet
		case KwConst:
			kind = DeclConst
		}
		decl := p.parseVarDecl(kind, true, true)
		if p.tok == KwIn || p.isIdent("of") {
			if len(decl.Decls) != 1 {
				p.fail(decl.Pos, "Invalid left-hand side in for-"+p.val+" loop: Must have a single binding.")
			}
			if decl.Decls[0].Init != nil && !p.forInInit(decl) {
				p.fail(decl.Pos, "for-"+p.val+" loop variable declaration may not have an initializer.")
			}
			return p.parseForInOf(start, decl, isAwait)
		}
		for _, d := range decl.Decls {
			if d.Init == nil {
				p.checkDeclaratorInit(kind, d)
			}
		}
		init = decl
	default:
		var x Expr
		if tok, _, val := p.peek(); isAwait && p.isIdent("async") && tok == Identifier && val == "of" {
			x = p.parseLeftHandSide() // for await (async of y): not an async arrow
		} else {
			x = p.parseExpression(true)
		}
		if p.tok == KwIn || p.isIdent("of") {
			if tok == KwLet && p.tok != KwIn {
				pos, _ := x.Range()
				p.fail(pos, "The left-hand side of a for-of loop may not be 'let'.")
			}
			return p.parseForInOf(start, p.toAssignTarget(x, "Invalid left-hand side in for-"+p.val+" loop"), isAwait)
		}
		init = x
	}
	if isAwait {
		p.fail(start, "for await is only valid with of")
	}
	p.expect(Semicolon)
	var cond, update Expr
	if p.tok != Semicolon {
		cond = p.parseExpression(false)
	}
	p.expect(Semicolon)
	if p.tok != RParen {
		update = p.parseExpression(false)
	}
	p.expect(RParen)
	body := p.parseLoopBody()
	return &ForStmt{Span: Span{start, p.prevEnd}, Init: init, Cond: cond, Update: update, Body: body}
}

// forInInit reports whether the single declarator of decl may keep its
// initializer in a for-in head: Annex B (Initializers in ForIn Statement
// Heads) allows `for (var x = init in o)` in sloppy code, for a var with a
// plain name.
func (p *parser) forInInit(decl *VarDecl) bool {
	_, name := decl.Decls[0].Target.(*Ident)
	return name && decl.Kind == DeclVar && p.tok == KwIn && !p.strict
}

func (p *parser) parseForInOf(start int, left Node, isAwait bool) Stmt {
	of := p.tok != KwIn
	if isAwait && !of {
		p.fail(start, "for await is only valid with of")
	}
	p.next()
	var right Expr
	if of {
		right = p.parseAssign(false)
	} else {
		right = p.parseExpression(false)
	}
	p.expect(RParen)
	body := p.parseLoopBody()
	return &ForInOfStmt{Span: Span{start, p.prevEnd}, Of: of, Await: isAwait, Left: left, Right: right, Body: body}
}

func (p *parser) parseReturn() Stmt {
	start := p.start
	if p.funcDepth == 0 {
		p.fail(start, "Illegal return statement")
	}
	p.next()
	var result Expr
	if p.tok != Semicolon && p.tok != RBrace && p.tok != EOF && !p.nlBefore {
		result = p.parseExpression(false)
	}
	p.semicolon()
	return &ReturnStmt{Span{start, p.prevEnd}, result}
}

func (p *parser) parseThrow() Stmt {
	start := p.start
	p.next()
	if p.nlBefore {
		p.fail(p.start, "Illegal newline after throw")
	}
	x := p.parseExpression(false)
	p.semicolon()
	return &ThrowStmt{Span{start, p.prevEnd}, x}
}

func (p *parser) parseBreakContinue(isBreak bool) Stmt {
	start := p.start
	p.next()
	var lbl *Ident
	if (p.tok == Identifier || p.tok == KwLet && !p.strict) && !p.nlBefore {
		lbl = &Ident{Span: Span{p.start, p.end}, Name: p.val}
		p.next()
		found := false
		for i := len(p.labels) - 1; i >= p.labelBase; i-- {
			if p.labels[i].name != lbl.Name {
				continue
			}
			found = true
			if !isBreak && !p.labels[i].loop {
				p.fail(lbl.Pos, "Illegal continue statement: '"+lbl.Name+"' does not denote an iteration statement")
			}
			break
		}
		if !found {
			p.fail(lbl.Pos, "Undefined label '"+lbl.Name+"'")
		}
	} else if isBreak {
		if p.inLoop == 0 && p.inSwitch == 0 {
			p.fail(start, "Illegal break statement")
		}
	} else if p.inLoop == 0 {
		p.fail(start, "Illegal continue statement: no surrounding iteration statement")
	}
	p.semicolon()
	if isBreak {
		return &BreakStmt{Span{start, p.prevEnd}, lbl}
	}
	return &ContinueStmt{Span{start, p.prevEnd}, lbl}
}

func (p *parser) parseLabeled() Stmt {
	start := p.start
	name := p.val
	for i := p.labelBase; i < len(p.labels); i++ {
		if p.labels[i].name == name {
			p.fail(start, "Label '"+name+"' has already been declared")
		}
	}
	lbl := &Ident{Span: Span{start, p.end}, Name: name}
	p.next()
	p.expect(Colon)
	if p.tok == KwFunction {
		// Annex B.3.1: sloppy code may label a plain function declaration.
		fnStart := p.start
		if p.strict {
			p.failFunctionPlacement(fnStart)
		}
		if tok, _, _ := p.peek(); tok == Mul {
			p.fail(fnStart, "Generators can only be declared at the top level or inside a block.")
		}
		p.pendingLabels = 0
		decl := p.parseFunctionDecl(fnStart, false, true)
		return &LabeledStmt{Span{start, p.prevEnd}, lbl, decl}
	}
	p.labels = append(p.labels, label{name: name})
	p.pendingLabels++
	body := p.parseStatement()
	p.labels = p.labels[:len(p.labels)-1]
	return &LabeledStmt{Span{start, p.prevEnd}, lbl, body}
}

func (p *parser) parseSwitch() Stmt {
	start := p.start
	p.next()
	p.expect(LParen)
	disc := p.parseExpression(false)
	p.expect(RParen)
	p.expect(LBrace)
	sw := &SwitchStmt{Disc: disc}
	p.inSwitch++
	hasDefault := false
	for p.tok != RBrace {
		c := &SwitchCase{Span: Span{p.start, 0}}
		if p.eat(KwDefault) {
			if hasDefault {
				p.fail(c.Pos, "More than one default clause in switch statement")
			}
			hasDefault = true
		} else {
			p.expect(KwCase)
			c.Test = p.parseExpression(false)
		}
		p.expect(Colon)
		for p.tok != KwCase && p.tok != KwDefault && p.tok != RBrace {
			if p.tok == EOF {
				p.unexpected()
			}
			poll(p.stop, len(c.Body))
			c.Body = append(c.Body, p.parseStatementListItem())
		}
		c.End = p.prevEnd
		sw.Cases = append(sw.Cases, c)
	}
	p.inSwitch--
	p.next()
	sw.Span = Span{start, p.prevEnd}
	return sw
}

func (p *parser) parseTry() Stmt {
	start := p.start
	p.next()
	t := &TryStmt{Block: p.parseBlock()}
	if p.eat(KwCatch) {
		if p.eat(LParen) {
			t.Param = p.parseBindingTarget()
			p.expect(RParen)
		}
		t.Handler = p.parseBlock()
	}
	if p.eat(KwFinally) {
		t.Finalizer = p.parseBlock()
	}
	if t.Handler == nil && t.Finalizer == nil {
		p.fail(p.start, "Missing catch or finally after try")
	}
	t.Span = Span{start, p.prevEnd}
	return t
}

// ---- Modules ----------------------------------------------------------------

func (p *parser) parseModuleSource() *StringLit {
	if p.tok != String {
		p.unexpected()
	}
	s := &StringLit{Span{p.start, p.end}, p.val}
	p.next()
	if p.tok == KwWith {
		p.unsupported(p.start, "import attribute syntax")
	}
	return s
}

// unsupported fails at pos for a feature moejs does not implement.
func (p *parser) unsupported(pos int, feature string) {
	p.fail(pos, feature+" is not supported yet (see TODO.md)")
}

// parseModuleExportName parses an IdentifierName or a string literal used
// as an import/export name.
func (p *parser) parseModuleExportName() *Ident {
	if p.tok == String {
		// WTF-8 is valid UTF-8 exactly when it holds no lone surrogate.
		if !utf8.ValidString(p.val) {
			p.fail(p.start, "Invalid module export name: contains unpaired surrogate")
		}
		id := &Ident{Span: Span{p.start, p.end}, Name: p.val}
		p.next()
		return id
	}
	return p.parseIdentifierName()
}

// parseIdentifierName accepts any IdentifierName, including reserved words.
func (p *parser) parseIdentifierName() *Ident {
	if p.tok != Identifier && p.tok != EscapedWord && !p.tok.IsKeyword() {
		p.unexpected()
	}
	id := &Ident{Span: Span{p.start, p.end}, Name: p.val}
	if p.tok != Identifier && p.tok != EscapedWord {
		id.Name = p.text()
	}
	p.next()
	return id
}

func (p *parser) parseImport() Stmt {
	start := p.start
	p.next()
	decl := &ImportDecl{}
	if p.tok == String {
		decl.Source = p.parseModuleSource()
		p.semicolon()
		decl.Span = Span{start, p.prevEnd}
		return decl
	}
	if p.tok == Identifier {
		// import defer * as ns, import source x (but import source from).
		if tok, _, val := p.peek(); p.isIdent("defer") && tok == Mul {
			p.unsupported(start, "import defer")
		} else if p.isIdent("source") && tok == Identifier && val != "from" {
			p.unsupported(start, "import source")
		}
		decl.Default = p.parseBindingIdent()
		if !p.eat(Comma) {
			return p.finishImport(start, decl)
		}
	}
	switch p.tok {
	case Mul:
		p.next()
		if !p.isIdent("as") {
			p.unexpected()
		}
		p.next()
		decl.Namespace = p.parseBindingIdent()
	case LBrace:
		p.next()
		for p.tok != RBrace {
			spec := &ImportSpec{Span: Span{p.start, 0}}
			importedTok := p.tok
			spec.Imported = p.parseModuleExportName()
			if p.isIdent("as") {
				p.next()
				spec.Local = p.parseBindingIdent()
			} else {
				if importedTok == EscapedWord {
					p.unexpectedAt(EscapedWord, spec.Pos, "")
				}
				if importedTok != Identifier {
					p.fail(spec.Pos, "Unexpected token '"+spec.Imported.Name+"'")
				}
				p.checkBindingName(spec.Imported)
				spec.Local = spec.Imported
			}
			spec.End = p.prevEnd
			decl.Specs = append(decl.Specs, spec)
			if p.tok != RBrace {
				p.expect(Comma)
			}
		}
		p.next()
	default:
		p.unexpected()
	}
	return p.finishImport(start, decl)
}

func (p *parser) finishImport(start int, decl *ImportDecl) Stmt {
	if !p.isIdent("from") {
		p.unexpected()
	}
	p.next()
	if p.isIdent("from") && decl.Default != nil && decl.Default.Name == "source" && decl.Specs == nil && decl.Namespace == nil {
		p.unsupported(start, "import source") // import source from from "m"
	}
	decl.Source = p.parseModuleSource()
	p.semicolon()
	decl.Span = Span{start, p.prevEnd}
	return decl
}

func (p *parser) parseExport() Stmt {
	start := p.start
	p.next()
	switch p.tok {
	case KwDefault:
		return p.parseExportDefault(start)
	case Mul:
		p.next()
		all := &ExportAll{}
		if p.isIdent("as") {
			p.next()
			all.As = p.parseModuleExportName()
		}
		if !p.isIdent("from") {
			p.unexpected()
		}
		p.next()
		all.Source = p.parseModuleSource()
		p.semicolon()
		all.Span = Span{start, p.prevEnd}
		return all
	case LBrace:
		p.next()
		named := &ExportNamed{}
		localToks := make([]Token, 0, 8)
		for p.tok != RBrace {
			spec := &ExportSpec{Span: Span{p.start, 0}}
			localToks = append(localToks, p.tok)
			spec.Local = p.parseModuleExportName()
			spec.Exported = spec.Local
			if p.isIdent("as") {
				p.next()
				spec.Exported = p.parseModuleExportName()
			}
			spec.End = p.prevEnd
			named.Specs = append(named.Specs, spec)
			if p.tok != RBrace {
				p.expect(Comma)
			}
		}
		p.next()
		if p.isIdent("from") {
			p.next()
			named.Source = p.parseModuleSource()
		} else {
			for i, spec := range named.Specs {
				if localToks[i] == EscapedWord {
					p.unexpectedAt(EscapedWord, spec.Local.Pos, "")
				}
				if localToks[i] != Identifier {
					p.fail(spec.Local.Pos, "Unexpected token '"+spec.Local.Name+"'")
				}
			}
		}
		p.semicolon()
		named.Span = Span{start, p.prevEnd}
		return named
	case KwVar, KwLet, KwConst:
		kind := DeclVar
		switch p.tok {
		case KwLet:
			kind = DeclLet
		case KwConst:
			kind = DeclConst
		}
		decl := p.parseVarDeclStmt(kind)
		return &ExportDecl{Span{start, p.prevEnd}, decl}
	case KwFunction:
		decl := p.parseFunctionDecl(p.start, false, true)
		return &ExportDecl{Span{start, p.prevEnd}, decl}
	case KwClass:
		decl := p.parseClassDecl(true)
		return &ExportDecl{Span{start, p.prevEnd}, decl}
	case Identifier:
		if p.isIdent("async") {
			if tok, nl, _ := p.peek(); tok == KwFunction && !nl {
				fstart := p.start
				p.next()
				decl := p.parseFunctionDecl(fstart, true, true)
				return &ExportDecl{Span{start, p.prevEnd}, decl}
			}
		}
	}
	p.unexpected()
	return nil
}

func (p *parser) parseExportDefault(start int) Stmt {
	p.next()
	var decl Node
	switch p.tok {
	case KwFunction:
		decl = p.parseFunctionDecl(p.start, false, false)
	case KwClass:
		decl = p.parseClassDecl(false)
	case Identifier:
		if p.isIdent("async") {
			if tok, nl, _ := p.peek(); tok == KwFunction && !nl {
				fstart := p.start
				p.next()
				decl = p.parseFunctionDecl(fstart, true, false)
				break
			}
		}
		fallthrough
	default:
		decl = p.parseAssign(false)
		p.semicolon()
	}
	return &ExportDefault{Span{start, p.prevEnd}, decl}
}

// ---- Bindings ---------------------------------------------------------------

// parseBindingIdent parses a BindingIdentifier and applies the strict-mode
// restrictions on eval/arguments and reserved words. Sloppy code may bind
// `let` except in a lexical declaration (checkLexicalLet).
func (p *parser) parseBindingIdent() *Ident {
	if p.tok == KwLet && p.strict {
		p.fail(p.start, "let is disallowed as a lexically bound name")
	}
	if p.tok != Identifier && p.tok != KwLet {
		p.unexpected()
	}
	id := &Ident{Span: Span{p.start, p.end}, Name: p.val}
	p.checkBindingName(id)
	p.next()
	return id
}

func (p *parser) checkBindingName(id *Ident) {
	if p.strict && (id.Name == "eval" || id.Name == "arguments") {
		p.fail(id.Pos, "Unexpected eval or arguments in strict mode")
	}
}

// parseBindingTarget parses a BindingIdentifier or a binding pattern.
func (p *parser) parseBindingTarget() Pattern {
	p.enter()
	defer p.leave()
	switch p.tok {
	case LBrace:
		return p.parseObjectBindingPattern()
	case LBrack:
		return p.parseArrayBindingPattern()
	}
	return p.parseBindingIdent()
}

// parseBindingElement parses a binding target with an optional default.
func (p *parser) parseBindingElement() Pattern {
	start := p.start
	target := p.parseBindingTarget()
	if !p.eat(Assign) {
		return target
	}
	def := p.parseAssign(false)
	return &AssignPattern{Span{start, p.prevEnd}, target, def}
}

func (p *parser) parseObjectBindingPattern() Pattern {
	start := p.start
	p.next()
	pat := &ObjectPattern{}
	for p.tok != RBrace {
		if p.eat(Ellipsis) {
			pat.Rest = p.parseBindingIdent()
			if p.tok != RBrace {
				p.fail(p.start, "Rest element must be last element")
			}
			break
		}
		prop := &PatternProp{Span: Span{p.start, 0}}
		keyTok := p.tok
		prop.Key, prop.Computed = p.parsePropertyKey()
		if p.eat(Colon) {
			prop.Value = p.parseBindingElement()
		} else {
			if keyTok != Identifier && (keyTok != KwLet || p.strict) {
				p.unexpectedAt(keyTok, prop.Pos, p.src[prop.Pos:p.prevEnd])
			}
			key := prop.Key.(*Ident)
			p.checkBindingName(key)
			id := &Ident{Span: key.Span, Name: key.Name}
			prop.Value = id
			if p.eat(Assign) {
				def := p.parseAssign(false)
				prop.Value = &AssignPattern{Span{id.Pos, p.prevEnd}, id, def}
			}
		}
		prop.End = p.prevEnd
		pat.Props = append(pat.Props, prop)
		if p.tok != RBrace {
			p.expect(Comma)
		}
	}
	p.next()
	pat.Span = Span{start, p.prevEnd}
	return pat
}

func (p *parser) parseArrayBindingPattern() Pattern {
	start := p.start
	p.next()
	pat := &ArrayPattern{}
	for p.tok != RBrack {
		if p.eat(Comma) {
			pat.Elems = append(pat.Elems, nil)
			continue
		}
		if p.eat(Ellipsis) {
			pat.Rest = p.parseBindingTarget()
			if p.tok != RBrack {
				p.fail(p.start, "Rest element must be last element")
			}
			break
		}
		pat.Elems = append(pat.Elems, p.parseBindingElement())
		if p.tok != RBrack {
			p.expect(Comma)
		}
	}
	p.next()
	pat.Span = Span{start, p.prevEnd}
	return pat
}
