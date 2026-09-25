package syntax

import "strconv"

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
	opts     Options
	isModule bool
	awaitKw  bool // await is a keyword: in modules and async functions
	heritage bool // the next parenthesized expression starts a class heritage: not an arrow
	prevEnd  int  // end offset of the previously consumed token
	depth    int  // recursion units in flight (MaxNestingDepth)

	// Statement context; saved and reset at every function boundary.
	funcDepth     int // functions of any kind enclosing the current position
	nonArrowDepth int // non-arrow functions enclosing the current position
	inLoop        int
	inSwitch      int
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

// ParseModule parses src as a strict-mode ES module, runs the early-error and
// scope-resolution passes, and returns the annotated tree. The error, when
// non-nil, is always a *Error.
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

// ParseScript parses src as a strict-mode script (the runtime's RunString
// input). Import and export declarations are rejected.
func ParseScript(name, src string, opts Options) (*Script, error) {
	p := newParser(name, src, opts, false)
	s := &Script{}
	if err := p.run(func() { s.Program = p.parseProgram() }); err != nil {
		return nil, err
	}
	if err := resolveScript(s, opts); err != nil {
		return nil, err
	}
	return s, nil
}

func newParser(name, src string, opts Options, isModule bool) *parser {
	p := &parser{opts: opts, isModule: isModule, awaitKw: isModule}
	p.lexer.init(newFile(name, src))
	return p
}

// run executes f, converting a bailout panic into the recorded error.
func (p *parser) run(f func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(bailout); !ok {
				panic(r)
			}
			err = p.err
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
	if (p.tok == KwAwait || p.tok == EscapedWord && p.val == "await") && !p.awaitKw {
		p.tok = Identifier
	}
}

// peek returns the token after the current one without consuming anything.
func (p *parser) peek() (tok Token, nlBefore bool, val string) {
	saved := p.lexer
	p.lexer.next()
	tok, nlBefore, val = p.tok, p.nlBefore, p.val
	p.lexer = saved
	if (tok == KwAwait || tok == EscapedWord && val == "await") && !p.awaitKw {
		tok = Identifier
	}
	return tok, nlBefore, val
}

// setAwaitKw switches whether await is a keyword, reclassifying the current
// token, which was scanned before the switch.
func (p *parser) setAwaitKw(on bool) {
	p.awaitKw = on
	switch {
	case on && p.tok == Identifier && p.val == "await":
		p.tok = KwAwait
		if p.escaped {
			p.tok = EscapedWord
		}
	case !on && (p.tok == KwAwait || p.tok == EscapedWord && p.val == "await"):
		p.tok = Identifier
	}
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
	case KwLet, KwYield, KwImplements, KwInterface, KwPackage, KwPrivate, KwProtected, KwPublic, KwStatic:
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
	for p.tok != EOF {
		prog.Body = append(prog.Body, p.parseModuleItem())
	}
	prog.End = len(p.src)
	return prog
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
		return p.parseVarDeclStmt(DeclLet)
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
		p.fail(start, "Strict mode code may not include a with statement")
	case KwFunction:
		p.fail(start, "In strict mode code, functions can only be declared at top level or inside a block.")
	case KwClass:
		p.unexpected()
	case KwLet, KwConst:
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

func (p *parser) parseBlock() *BlockStmt {
	start := p.start
	p.expect(LBrace)
	var body []Stmt
	for p.tok != RBrace {
		if p.tok == EOF {
			p.unexpected()
		}
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
	then := p.parseStatement()
	var els Stmt
	if p.eat(KwElse) {
		els = p.parseStatement()
	}
	return &IfStmt{Span{start, p.prevEnd}, cond, then, els}
}

func (p *parser) parseLoopBody() Stmt {
	p.inLoop++
	body := p.parseStatement()
	p.inLoop--
	return body
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
	switch p.tok {
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
			if decl.Decls[0].Init != nil {
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
	if p.tok == Identifier && !p.nlBefore {
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
		p.fail(p.start, "In strict mode code, functions can only be declared at top level or inside a block.")
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
	return s
}

// parseModuleExportName parses an IdentifierName or a string literal used
// as an import/export name.
func (p *parser) parseModuleExportName() *Ident {
	if p.tok == String {
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
// restrictions on eval/arguments and reserved words.
func (p *parser) parseBindingIdent() *Ident {
	if p.tok == KwLet {
		p.fail(p.start, "let is disallowed as a lexically bound name")
	}
	if p.tok != Identifier {
		p.unexpected()
	}
	id := &Ident{Span: Span{p.start, p.end}, Name: p.val}
	p.checkBindingName(id)
	p.next()
	return id
}

func (p *parser) checkBindingName(id *Ident) {
	if id.Name == "eval" || id.Name == "arguments" {
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
			if keyTok != Identifier {
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
