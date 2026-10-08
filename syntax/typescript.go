package syntax

import "strings"

// TypeScript (Options.TypeScript). The parser reads TypeScript and drops
// its type syntax as it goes, so the tree is the one the type-erased
// JavaScript gives: every position still refers to the TypeScript text and
// no source map is needed. The grammar and every disambiguation rule follow
// TypeScript's own parser (src/compiler/parser.ts); the function names in
// the comments below are its names.
//
// Types are only checked for well-formedness and skipped: no node is built
// for them. TypeScript that has run-time semantics (enums, namespaces with
// values, parameter properties, import = require, export =) fails with a
// SyntaxError naming the feature. Import specifiers whose bindings are only
// used as types are dropped by the scope pass (elideTypeImports), as tsc
// does.

// tsShared is the parser's TypeScript state (parser.tsx): the context a
// speculative parse restores (tsState), and memos of what failed ones
// ruled out and what the scope pass needs, which it keeps.
type tsShared struct {
	tsState
	// notArrow holds the start of each `(`, `<` or `async` where an arrow
	// function was tried and ruled out, so that a later parse of the same
	// text does not try again (TypeScript's notParenthesizedArrow).
	notArrow map[int]bool
	// badArgs holds each `<` whose type argument list does not parse, so
	// that a chain of comparisons (`a < b < c ...`) is not re-scanned from
	// every `<` (see tsTypeArgsInExpr).
	badArgs map[int]bool
	// parens lists the keys markParenthesized added, so that a failed
	// speculative parse removes its own.
	parens []Expr
	// casts holds the expressions an erased as, satisfies, !, <T> or type
	// arguments applied to (tsCast): no arrow function parameters
	// (tsNoCasts), and with as, satisfies or <T> no assignment targets.
	casts map[Expr]uint8
	// info goes to the module for the scope pass.
	info tsInfo
	// rewound counts the bytes of source failed speculative parses read
	// and rewound, which TestTypeScriptLinear bounds.
	rewound int
	// unwinding is set once a speculative parse rethrew what it may not
	// recover from, so that the ones around it let it pass: recovering
	// unwinds the stack, which would cost the depth of the nesting at each
	// of them.
	unwinding bool
}

// tsState is the TypeScript context of the parser, which a speculative
// parse saves and restores with the parser.
type tsState struct {
	noCond    bool  // a conditional type may not start here: the extends clause of one, the constraint of infer
	blockRet  bool  // the assignment expression about to be parsed is the true branch of a conditional (tsParen)
	bodiless  bool  // the function about to be parsed may have no body: a declaration or class method
	ambient   int   // ambient declarations (declare) and signatures enclosing the current position
	coverItem int   // 1 + the start of the item of tsParen's list being parsed
	args      []int // the `<` of each type argument list being parsed

	// top is 1 + the start of the module item being parsed, moved past its
	// export and declare; recTop is set while a declaration that starts
	// there is parsed, whose names tsRecord records for the module. ns
	// holds the names declared in each namespace body being parsed.
	top    int
	recTop bool
	ns     []map[string]uint8
}

// tsInfo is what the scope pass needs to erase imports and exports.
type tsInfo struct {
	// names maps a name the module declares with TypeScript syntax only to
	// how: tsTypeName (interface, type alias, type-only import, namespace
	// without values) and/or tsAmbientName (declare).
	names map[string]uint8
	// aliases are the names of `import x = A.B` declarations: erased when x
	// is not used as a value.
	aliases []*Ident

	// The scope pass's state: the binding of each alias (declareAliases),
	// and the type exports to drop (tsDropNode).
	aliasBindings map[*Binding]*Ident
	drop          map[Node]bool
}

const (
	tsTypeName    uint8 = 1 << iota // declared only as a type
	tsAmbientName                   // declared with declare: no run-time binding
)

// Kinds of erased casts (tsShared.casts).
const (
	tsCastLHS   uint8 = 1 << iota // x! or f<T>: still a left-hand side expression
	tsCastAs                      // x as T, x satisfies T or <T>x: none, unless parenthesised
	tsCastParen                   // as, satisfies or <T>, then parentheses around it
)

// Kinds of the declarations tsDeclaration parses.
const (
	tsNoDecl    = iota // no TypeScript declaration starts here
	tsTypeDecl         // erased; tsc does not count it as instantiating a namespace
	tsValueDecl        // erased, but tsc counts it as a value
	tsClassDecl        // an abstract class: kept
)

// tsUnsupported fails at pos for TypeScript with run-time semantics.
func (p *parser) tsUnsupported(pos int, feature string) {
	p.fail(pos, feature+" is not supported: only erasable TypeScript is (see TODO.md)")
}

// tsRecord records a name declared with TypeScript syntax: for the module
// when the declaration is one of the module's items, for the namespace
// when it is in a namespace body. A declaration in a block or a function is
// no module's, and no export can name it.
func (p *parser) tsRecord(name string, how uint8) {
	var names *map[string]uint8
	switch {
	case p.tsx.recTop:
		names = &p.tsx.info.names
	case len(p.tsx.ns) > 0:
		names = &p.tsx.ns[len(p.tsx.ns)-1]
	default:
		return
	}
	if *names == nil {
		*names = make(map[string]uint8)
	}
	(*names)[name] |= how
}

// tsAtTop reports whether a declaration starting at the current token is
// one of the module's items.
func (p *parser) tsAtTop() bool { return p.tsx.top == p.start+1 }

// ---- Speculation ------------------------------------------------------------

// tsTry runs f speculatively (TypeScript's tryParse). When f fails with a
// syntax error, the parser is rewound to where it was and tsTry reports
// false. A failure past MaxNestingDepth and a stop unwind as usual: a
// speculative parse nests as deep as the parse it competes with, so
// recovering would only repeat the work, possibly at every level. (A run
// of comparisons a < b < c ... longer than the bound fails for that
// reason: tried as type arguments, it nests as deep as it is long.)
func (p *parser) tsTry(f func()) (ok bool) {
	saved, st := *p, p.tsx.tsState
	lines, parens := len(p.file.lineStarts), len(p.tsx.parens)
	defer func() {
		if !ok && !p.tsx.unwinding {
			p.tsRecover(recover(), len(st.args))
			p.tsRewind(&saved, &st, lines, parens)
		}
	}()
	f()
	return true
}

// tsLook reports what f reports, scanning ahead from the current token
// (TypeScript's lookAhead), and rewinds the parser either way. A syntax
// error in f counts as false.
func (p *parser) tsLook(f func() bool) (yes bool) {
	saved, st := *p, p.tsx.tsState
	lines, parens := len(p.file.lineStarts), len(p.tsx.parens)
	defer func() {
		if p.tsx.unwinding {
			return
		}
		if !yes {
			p.tsRecover(recover(), len(st.args))
		}
		p.tsRewind(&saved, &st, lines, parens)
	}()
	return f()
}

// tsRecover rethrows r unless it is a syntax error a speculative parse may
// recover from. The type argument lists the parse opened (past base on
// args) and had not closed when it failed are lists that do not parse.
func (p *parser) tsRecover(r any, base int) {
	if r == nil {
		return
	}
	if _, ok := r.(bailout); !ok || strings.HasPrefix(p.err.Msg, errPrefix+"Nesting too deep") {
		p.tsx.unwinding = true
		panic(r)
	}
	if p.tsx.badArgs == nil {
		p.tsx.badArgs = make(map[int]bool)
	}
	for _, pos := range p.tsx.args[base:] {
		p.tsx.badArgs[pos] = true
	}
}

// tsRewind restores the parser and the TypeScript context saved before a
// speculative parse: the lexer, the parser's context, the error, the line
// table and the parenthesised expressions. The regexp checker taken
// meanwhile is kept for later literals, and the memos stay.
func (p *parser) tsRewind(saved *parser, st *tsState, lines, parens int) {
	p.tsx.rewound += p.end - saved.start
	for _, x := range p.tsx.parens[parens:] {
		delete(p.parens, x)
	}
	p.tsx.parens = p.tsx.parens[:parens]
	re, set := p.re, p.parens
	*p = *saved
	p.re, p.parens = re, set
	p.tsx.tsState = *st
	p.file.lineStarts = p.file.lineStarts[:lines]
}

// ---- Tokens -----------------------------------------------------------------

// tsIdentifier reports whether the current token is an identifier to
// TypeScript (isIdentifier): one that is not a reserved word, the strict
// mode reserved words included.
func (p *parser) tsIdentifier() bool {
	return p.tok == Identifier || p.tok >= KwYield && p.tok < keywordEnd
}

// tsIdentName consumes any IdentifierName.
func (p *parser) tsIdentName() {
	if p.tok != Identifier && p.tok != EscapedWord && !p.tok.IsKeyword() {
		p.unexpected()
	}
	p.next()
}

// tsBindingName consumes the name a type alias, an interface, a type
// parameter or a namespace declares, and returns it.
func (p *parser) tsBindingName() string {
	if p.tok != Identifier {
		p.unexpected()
	}
	name := p.val
	p.next()
	return name
}

// isIdentName reports whether tok spells an IdentifierName.
func isIdentName(tok Token) bool {
	return tok == Identifier || tok == EscapedWord || tok.IsKeyword()
}

// tsCanFollowModifier reports whether tok can follow a modifier
// (canFollowModifier).
func tsCanFollowModifier(tok Token) bool {
	switch tok {
	case LBrack, LBrace, Mul, Ellipsis, PrivateIdent, String, Number, BigInt:
		return true
	}
	return isIdentName(tok)
}

// tsModifierKind reports whether the current token is a modifier keyword
// (isModifierKind).
func (p *parser) tsModifierKind() bool {
	switch p.tok {
	case KwConst, KwDefault, KwExport, KwIn, KwPrivate, KwProtected, KwPublic, KwStatic:
		return true
	case Identifier:
		switch p.val {
		case "abstract", "accessor", "async", "declare", "out", "override", "readonly":
			return !p.escaped
		}
	}
	return false
}

// tsModifier reports whether the current token is a modifier keyword
// followed, on the same line, by a token that can follow a modifier
// (nextTokenIsOnSameLineAndCanFollowModifier).
func (p *parser) tsModifier() bool {
	if !p.tsModifierKind() {
		return false
	}
	tok, nl, _ := p.peek()
	return !nl && tsCanFollowModifier(tok)
}

// tsCanSemicolon reports whether a statement may end here
// (canParseSemicolon).
func (p *parser) tsCanSemicolon() bool {
	return p.tok == Semicolon || p.tok == RBrace || p.tok == EOF || p.nlBefore
}

// tsLt consumes the `<` opening a type argument list, which may be the
// first half of `<<` (reScanLessThanToken).
func (p *parser) tsLt() {
	if p.tok == Shl {
		p.prevEnd = p.start + 1
		p.lexer.pos = p.start + 1
		p.lexer.next()
		return
	}
	p.expect(Lt)
}

// tsAtGt reports whether the current token starts with `>`.
func (p *parser) tsAtGt() bool {
	switch p.tok {
	case Gt, GtEq, Shr, UShr, ShrAssign, UShrAssign:
		return true
	}
	return false
}

// tsGt consumes the `>` closing a type parameter or argument list, which
// may be the first character of `>>`, `>=`, `>>>`, `>>=` or `>>>=`: in a
// type, TypeScript's scanner never merges `>` with what follows.
func (p *parser) tsGt() {
	if p.tok == Gt {
		p.next()
		return
	}
	if !p.tsAtGt() {
		p.unexpected()
	}
	p.prevEnd = p.start + 1
	p.lexer.pos = p.start + 1
	p.lexer.next()
}

// ---- Types ------------------------------------------------------------------

// tsType skips a Type (parseType).
func (p *parser) tsType() {
	p.enter()
	defer p.leave()
	if p.tsFunctionTypeAhead() {
		p.tsFunctionType()
		return
	}
	p.tsUnionType()
	if p.tsx.noCond || p.nlBefore || p.tok != KwExtends {
		return
	}
	// A conditional type; the type after extends may not be another one.
	p.next()
	p.tsx.noCond = true
	p.tsType()
	p.tsx.noCond = false
	p.expect(Question)
	p.tsType()
	p.expect(Colon)
	p.tsType()
}

// tsTypeAnnotation skips `: Type` when the current token is `:`.
func (p *parser) tsTypeAnnotation() bool {
	if p.tok != Colon {
		return false
	}
	p.next()
	p.tsType()
	return true
}

// tsReturnType skips a return type, which may be a type predicate `x is T`
// (parseTypeOrTypePredicate).
func (p *parser) tsReturnType() {
	if p.tsIdentifier() {
		if tok, nl, val := p.peek(); tok == Identifier && val == "is" && !nl {
			p.next()
			p.next()
		}
	}
	p.tsType()
}

// tsFunctionTypeAhead reports whether a function or constructor type
// starts here (isStartOfFunctionTypeOrConstructorType).
func (p *parser) tsFunctionTypeAhead() bool {
	switch p.tok {
	case Lt, KwNew:
		return true
	case LParen:
		return p.tsLook(p.tsFunctionTypeParams)
	case Identifier:
		if p.isIdent("abstract") {
			tok, _, _ := p.peek()
			return tok == KwNew
		}
	}
	return false
}

// tsFunctionTypeParams reports whether the `(` at the current token opens
// the parameters of a function type (isUnambiguouslyStartOfFunctionType).
func (p *parser) tsFunctionTypeParams() bool {
	p.next()
	if p.tok == RParen || p.tok == Ellipsis {
		return true
	}
	if !p.tsParamStart() {
		return false
	}
	switch p.tok {
	case Colon, Comma, Question, Assign:
		return true
	case RParen:
		p.next()
		return p.tok == Arrow
	}
	return false
}

// tsParamStart skips the modifiers and the name or binding pattern of a
// parameter, reporting whether there was one (skipParameterStart).
func (p *parser) tsParamStart() bool {
	for p.tsModifier() {
		p.next()
	}
	if p.tsIdentifier() || p.tok == KwThis {
		p.next()
		return true
	}
	if p.tok == LBrack || p.tok == LBrace {
		p.parseBindingTarget()
		return true
	}
	return false
}

// tsFunctionType skips a function or constructor type.
func (p *parser) tsFunctionType() {
	if p.isIdent("abstract") {
		p.next()
	}
	p.eat(KwNew)
	p.tsSignature(Arrow)
}

// tsSignature skips the type parameters, parameters and return type of a
// signature; ret is the token before the return type: `=>` for a function
// type (where it is required), `:` otherwise.
func (p *parser) tsSignature(ret Token) {
	if p.tok == Lt {
		p.tsTypeParams()
	}
	// The parameters of a signature bind nothing: they may be named eval
	// or arguments, as in ambient code.
	p.tsx.ambient++
	p.parseParams(&Function{IsStrict: true})
	p.tsx.ambient--
	if ret == Arrow {
		p.expect(Arrow)
		p.tsReturnType()
	} else if p.eat(Colon) {
		p.tsReturnType()
	}
}

func (p *parser) tsUnionType() {
	p.eat(BitOr)
	p.tsIntersectionType()
	for p.eat(BitOr) {
		p.tsIntersectionType()
	}
}

func (p *parser) tsIntersectionType() {
	p.eat(BitAnd)
	p.tsTypeOperator()
	for p.eat(BitAnd) {
		p.tsTypeOperator()
	}
}

// tsTypeOperator skips keyof T, unique T, readonly T, infer X or a postfix
// type (parseTypeOperatorOrHigher).
func (p *parser) tsTypeOperator() {
	if p.tok == Identifier && !p.escaped {
		switch p.val {
		case "keyof", "unique", "readonly":
			p.next()
			p.tsTypeOperator()
			return
		case "infer":
			p.next()
			p.tsBindingName()
			if p.tok == KwExtends {
				// The constraint of the inferred type, unless the extends
				// starts a conditional type around it
				// (tryParseConstraintOfInferType).
				noCond := p.tsx.noCond
				p.tsTry(func() {
					p.next()
					p.tsx.noCond = true
					p.tsType()
					p.tsx.noCond = noCond
					if !noCond && p.tok == Question {
						p.unexpected()
					}
				})
			}
			return
		}
	}
	p.tsPostfixType()
}

// tsPostfixType skips a type and the array and indexed access brackets
// after it (parsePostfixTypeOrHigher). Conditional types are allowed again
// inside it.
func (p *parser) tsPostfixType() {
	noCond := p.tsx.noCond
	p.tsx.noCond = false
	p.tsNonArrayType()
	for !p.nlBefore && p.tok == LBrack {
		p.next()
		if p.tok != RBrack {
			p.tsType()
		}
		p.expect(RBrack)
	}
	p.tsx.noCond = noCond
}

// tsNonArrayType skips a primary type (parseNonArrayType). The JSDoc types
// (`?T`, `T!`, `*`, `function(...)`) are not TypeScript and fail.
func (p *parser) tsNonArrayType() {
	switch p.tok {
	case KwVoid, KwNull, KwTrue, KwFalse, String, Number, BigInt:
		p.next()
		return
	case Template:
		p.tsTemplateType()
		return
	case Minus:
		if tok, _, _ := p.peek(); tok == Number || tok == BigInt {
			p.next()
			p.next()
			return
		}
	case KwThis:
		p.next()
		if p.isIdent("is") && !p.nlBefore {
			p.next()
			p.tsType()
		}
		return
	case KwTypeof:
		if tok, _, _ := p.peek(); tok == KwImport {
			p.next()
			p.tsImportType()
			return
		}
		p.next()
		p.tsEntityName()
		if !p.nlBefore && p.tok == Lt {
			p.tsTypeArgs()
		}
		return
	case LBrace:
		if p.tsLook(p.tsMappedTypeAhead) {
			p.tsMappedType()
		} else {
			p.tsTypeLiteral()
		}
		return
	case LBrack:
		p.tsTupleType()
		return
	case LParen:
		p.next()
		p.tsType()
		p.expect(RParen)
		return
	case KwImport:
		p.tsImportType()
		return
	case Identifier:
		if p.escaped {
			break
		}
		switch p.val {
		case "any", "unknown", "string", "number", "bigint", "symbol", "boolean", "undefined", "never", "object":
			// A keyword type, unless a dotted name starts with it.
			if tok, _, _ := p.peek(); tok != Dot {
				p.next()
				return
			}
		case "asserts":
			if tok, nl, _ := p.peek(); !nl && isIdentName(tok) {
				// asserts x, asserts x is T, asserts this
				p.next()
				p.tsIdentName()
				if p.isIdent("is") {
					p.next()
					p.tsType()
				}
				return
			}
		}
	}
	p.tsEntityName()
	if !p.nlBefore && (p.tok == Lt || p.tok == Shl) {
		p.tsTypeArgs()
	}
}

// tsEntityName skips a dotted name (parseEntityName): every part is an
// IdentifierName.
func (p *parser) tsEntityName() {
	p.tsIdentName()
	for p.eat(Dot) {
		p.tsIdentName()
	}
}

// tsTemplateType skips a template literal type.
func (p *parser) tsTemplateType() {
	for {
		if p.badEscape != 0 {
			p.failBadEscape()
		}
		tail := p.tail
		p.next()
		if tail {
			return
		}
		p.tsType()
		if p.tok != RBrace {
			p.unexpected()
		}
		p.rescanTemplateContinuation()
	}
}

// tsImportType skips import("m").T<A>, whose import attributes are an
// object type.
func (p *parser) tsImportType() {
	p.expect(KwImport)
	p.expect(LParen)
	p.tsType()
	if p.eat(Comma) && p.tok != RParen {
		p.tsType()
		p.eat(Comma)
	}
	p.expect(RParen)
	if p.eat(Dot) {
		p.tsEntityName()
	}
	if !p.nlBefore && (p.tok == Lt || p.tok == Shl) {
		p.tsTypeArgs()
	}
}

// tsTypeParams skips a type parameter list (parseTypeParameters) and
// reports whether it was a single name, <T>, which is a type too.
func (p *parser) tsTypeParams() (simple bool) {
	p.expect(Lt)
	simple = true
	for {
		// const T, in T, out T (and the modifiers TypeScript rejects later)
		for p.tsModifier() {
			p.next()
			simple = false
		}
		p.tsBindingName()
		if p.eat(KwExtends) {
			if !p.tsStartsType() && p.tsStartsExpression() {
				p.unexpected()
			}
			p.tsType()
			simple = false
		}
		if p.eat(Assign) {
			p.tsType()
			simple = false
		}
		if !p.eat(Comma) {
			break
		}
		simple = false
		if p.tsAtGt() {
			break
		}
	}
	p.tsGt()
	return simple
}

// tsTypeArgs skips a type argument list in a type. Its `<` is on args
// while it is parsed, for tsRecover.
func (p *parser) tsTypeArgs() {
	p.tsx.args = append(p.tsx.args, p.start)
	p.tsLt()
	for {
		p.tsType()
		if !p.eat(Comma) || p.tsAtGt() {
			break
		}
	}
	p.tsGt()
	p.tsx.args = p.tsx.args[:len(p.tsx.args)-1]
}

// tsTypeArgsInExpr reads type arguments after an expression, as in f<T>(x)
// or the instantiation expression f<T>, and reports whether it did
// (parseTypeArgumentsInExpression): they must close with a lone `>`, and
// the token after them must be one that can follow them
// (canFollowTypeArgumentsInExpression); otherwise the `<` is an operator
// and the parser is left where it was.
//
// A failed list is not tried again from the same `<`. Since the lists
// still open when a speculative parse fails are recorded too (tsRecover),
// a run of comparisons a < b < c < ... parses in linear time: the first
// attempt reads the rest of the run as nested type arguments and fails,
// and every later `<` of the run is already known.
func (p *parser) tsTypeArgsInExpr() bool {
	start := p.start
	if p.tsx.badArgs[start] {
		return false
	}
	return p.tsTry(func() {
		p.tsx.args = append(p.tsx.args, start)
		p.tsLt()
		for {
			p.tsType()
			if !p.eat(Comma) || p.tsAtGt() {
				break
			}
		}
		if p.tok != Gt {
			p.unexpected()
		}
		p.tsx.args = p.tsx.args[:len(p.tsx.args)-1]
		p.next()
		if !p.tsCanFollowTypeArgs() {
			p.unexpected()
		}
	})
}

// tsCanFollowTypeArgs reports whether the current token can follow type
// arguments in an expression (canFollowTypeArgumentsInExpression). A token
// that starts with `>` is the `>` TypeScript's scanner returns alone.
func (p *parser) tsCanFollowTypeArgs() bool {
	switch p.tok {
	case LParen, Template:
		return true
	case Lt, Plus, Minus:
		return false
	}
	if p.tsAtGt() {
		return false
	}
	return p.nlBefore || p.tsBinaryOperator() || !p.tsStartsExpression()
}

// tsBinaryOperator reports whether the current token is a binary operator,
// as and satisfies included (isBinaryOperator).
func (p *parser) tsBinaryOperator() bool {
	return binaryPrec(p.tok) > 0 || p.tok == Identifier && !p.escaped && (p.val == "as" || p.val == "satisfies")
}

// tsStartsExpression reports whether the current token can start an
// expression (isStartOfExpression).
func (p *parser) tsStartsExpression() bool {
	switch p.tok {
	case KwThis, KwSuper, KwNull, KwTrue, KwFalse, Number, BigInt, String, Template,
		LParen, LBrack, LBrace, KwFunction, KwClass, KwNew, Div, DivAssign, Identifier,
		Plus, Minus, BitNot, Not, KwDelete, KwTypeof, KwVoid, Inc, Dec, Lt, KwAwait, KwYield, PrivateIdent:
		return true
	case KwImport:
		tok, _, _ := p.peek()
		return tok == LParen || tok == Lt || tok == Dot
	}
	return p.tsBinaryOperator() || p.tsIdentifier()
}

// tsStartsType reports whether the current token can start a type
// (isStartOfType).
func (p *parser) tsStartsType() bool {
	switch p.tok {
	case LBrace, LBrack, Lt, BitOr, BitAnd, KwNew, String, Number, BigInt, KwTrue, KwFalse,
		KwNull, KwThis, KwTypeof, KwVoid, KwImport, Template, Ellipsis, KwFunction:
		return true
	case Minus:
		tok, _, _ := p.peek()
		return tok == Number || tok == BigInt
	case LParen:
		return p.tsLook(func() bool {
			p.next()
			return p.tok == RParen || p.tok == Ellipsis || p.tsIdentifier() || p.tok == LBrack ||
				p.tok == LBrace || p.tsModifierKind() || p.tsStartsType()
		})
	}
	return p.tsIdentifier()
}

// tsTypeLiteral skips an object type.
func (p *parser) tsTypeLiteral() {
	p.expect(LBrace)
	p.tsTypeMembers()
	p.next()
}

// tsTypeMembers skips the members of an object type, up to its `}`.
func (p *parser) tsTypeMembers() {
	for p.tok != RBrace {
		if p.tok == EOF {
			p.unexpected()
		}
		p.tsTypeMember()
	}
}

// tsTypeMember skips one member of an object type or interface
// (parseTypeMember), with the `,` or `;` after it.
func (p *parser) tsTypeMember() {
	switch tok, _, _ := p.peek(); {
	case p.tok == LParen || p.tok == Lt:
		p.tsSignature(Colon) // call signature
	case p.tok == KwNew && (tok == LParen || tok == Lt):
		p.next()
		p.tsSignature(Colon) // construct signature
	default:
		for p.tsModifier() {
			p.next()
		}
		switch {
		case p.tsAccessorAhead():
			p.next()
			p.tsPropertyName()
			p.tsSignature(Colon)
		case p.tok == LBrack && p.tsLook(p.tsIndexSignatureAhead):
			p.tsIndexSignature()
		default:
			p.tsPropertyName()
			p.eat(Question)
			if p.tok == LParen || p.tok == Lt {
				p.tsSignature(Colon) // method signature
			} else {
				p.tsTypeAnnotation()
			}
		}
	}
	if !p.eat(Comma) {
		p.semicolon()
	}
}

// tsAccessorAhead reports whether the current token is the get or set of
// an accessor (parseContextualModifier).
func (p *parser) tsAccessorAhead() bool {
	if !p.isIdent("get") && !p.isIdent("set") {
		return false
	}
	switch tok, _, _ := p.peek(); tok {
	case LBrack, PrivateIdent, String, Number, BigInt:
		return true
	default:
		return isIdentName(tok)
	}
}

// tsPropertyName skips a property name (parsePropertyName).
func (p *parser) tsPropertyName() {
	switch p.tok {
	case String, Number, BigInt, PrivateIdent:
		p.next()
	case LBrack:
		p.next()
		p.parseExpression(false)
		p.expect(RBrack)
	default:
		p.tsIdentName()
	}
}

// tsIndexSignatureAhead reports whether the `[` at the current token opens
// an index signature (isUnambiguouslyIndexSignature).
func (p *parser) tsIndexSignatureAhead() bool {
	p.next()
	if p.tok == Ellipsis || p.tok == RBrack {
		return true
	}
	if p.tsModifierKind() {
		p.next()
		if p.tsIdentifier() {
			return true
		}
	} else if !p.tsIdentifier() {
		return false
	} else {
		p.next()
	}
	if p.tok == Colon || p.tok == Comma {
		return true
	}
	if p.tok != Question {
		return false
	}
	p.next()
	return p.tok == Colon || p.tok == Comma || p.tok == RBrack
}

// tsIndexSignature skips `[key: K]: T`.
func (p *parser) tsIndexSignature() {
	p.expect(LBrack)
	for p.tok != RBrack {
		for p.tsModifier() {
			p.next()
		}
		p.eat(Ellipsis)
		p.parseBindingTarget()
		p.eat(Question)
		p.tsTypeAnnotation()
		if !p.eat(Comma) {
			break
		}
	}
	p.expect(RBrack)
	p.tsTypeAnnotation()
}

// tsMappedTypeAhead reports whether the `{` at the current token opens a
// mapped type (isStartOfMappedType).
func (p *parser) tsMappedTypeAhead() bool {
	p.next()
	if p.tok == Plus || p.tok == Minus {
		p.next()
		return p.isIdent("readonly")
	}
	if p.isIdent("readonly") {
		p.next()
	}
	if p.tok != LBrack {
		return false
	}
	p.next()
	if !p.tsIdentifier() {
		return false
	}
	p.next()
	return p.tok == KwIn
}

// tsMappedType skips { readonly [K in T as N]?: V }.
func (p *parser) tsMappedType() {
	p.expect(LBrace)
	if p.tok == Plus || p.tok == Minus {
		p.next()
		if !p.isIdent("readonly") {
			p.unexpected()
		}
		p.next()
	} else if p.isIdent("readonly") {
		p.next()
	}
	p.expect(LBrack)
	p.tsIdentName()
	p.expect(KwIn)
	p.tsType()
	if p.isIdent("as") {
		p.next()
		p.tsType()
	}
	p.expect(RBrack)
	if p.tok == Plus || p.tok == Minus {
		p.next()
		p.expect(Question)
	} else {
		p.eat(Question)
	}
	p.tsTypeAnnotation()
	p.semicolon()
	p.tsTypeMembers()
	p.next()
}

// tsTupleType skips a tuple type, whose elements may be named, optional
// or rest elements.
func (p *parser) tsTupleType() {
	p.expect(LBrack)
	for p.tok != RBrack {
		if p.tsLook(p.tsTupleNameAhead) {
			p.eat(Ellipsis)
			p.tsIdentName()
			p.eat(Question)
			p.expect(Colon)
		}
		if p.eat(Ellipsis) {
			p.tsType()
		} else {
			p.tsType()
			p.eat(Question)
		}
		if !p.eat(Comma) {
			break
		}
	}
	p.expect(RBrack)
}

// tsTupleNameAhead reports whether a tuple element starts with a name
// (isTupleElementName).
func (p *parser) tsTupleNameAhead() bool {
	p.eat(Ellipsis)
	if !isIdentName(p.tok) {
		return false
	}
	p.next()
	if p.tok == Question {
		p.next()
	}
	return p.tok == Colon
}

// ---- Expressions ------------------------------------------------------------

// tsAssign is parseAssign for TypeScript: it also reads arrow functions
// with type parameters, typed parameters and return types.
func (p *parser) tsAssign(noIn bool) Expr {
	blocked := p.tsx.blockRet
	p.tsx.blockRet = false
	start := p.start
	var lhs Expr
	switch p.tok {
	case KwYield:
		return p.parseYield(noIn)
	case LParen:
		// The nesting unit is the one parseUnary takes on the way to
		// parsePrimary, as in parseAssign.
		p.enter()
		h := p.tsParen(start, false, false, noIn, blocked)
		p.leave()
		if h.x == nil {
			return p.tsHeadArrow(h, start, false, noIn, blocked)
		}
		lhs = p.parseConditionalFrom(h.x, start, noIn)
	case Lt:
		// <T>(x) => x, unless a type assertion <T>x
		if tok, _, _ := p.peek(); tok == Identifier || tok == KwConst || tok >= KwYield && tok < keywordEnd {
			arrow, operand, at := p.tsGenericArrow(start, false, noIn, blocked)
			if arrow != nil {
				return arrow
			}
			if operand != nil {
				// The rest of the assertion's operand, a unary expression.
				x := p.parseCallTail(operand, at, true)
				if (p.tok == Inc || p.tok == Dec) && !p.nlBefore {
					x = p.parsePostfix(x, at)
				}
				p.tsCast(x, tsCastAs)
				lhs = p.parseConditionalFrom(x, at, noIn)
				break
			}
		}
		lhs = p.parseConditional(noIn)
	case Identifier:
		if p.isIdent("async") {
			switch tok, nl, val := p.peek(); {
			case nl:
			case tok == Identifier && (val != "as" && val != "satisfies" || p.tsLook(p.tsAsyncArrowAhead)):
				// async x => body (but async as T is an assertion)
				p.next()
				param := p.parseBindingIdent()
				if p.tok != Arrow || p.nlBefore {
					p.unexpected()
				}
				if param.Name == "await" {
					p.fail(param.Pos, "Unexpected reserved word")
				}
				return p.tsArrowBody(start, []Pattern{param}, nil, true, noIn, blocked)
			case tok == LParen:
				p.enter()
				p.next()
				h := p.tsParen(start, true, false, noIn, blocked)
				p.leave()
				if h.x == nil {
					return p.tsHeadArrow(h, start, true, noIn, blocked)
				}
				lhs = p.parseConditionalFrom(h.x, start, noIn)
			case tok == Lt:
				if x, _, _ := p.tsGenericArrow(start, true, noIn, blocked); x != nil {
					return x
				}
			}
		}
		if lhs != nil {
			break
		}
		if tok, nl, _ := p.peek(); tok == Arrow && !nl {
			param := p.parseBindingIdent()
			return p.tsArrowBody(start, []Pattern{param}, nil, false, noIn, blocked)
		}
		lhs = p.parseConditional(noIn)
	default:
		lhs = p.parseConditional(noIn)
	}
	if p.tok.isAssignOp() {
		op := p.tok
		p.tsNoCastTarget(lhs)
		var target Pattern
		switch op {
		case Assign:
			target = p.toAssignTarget(lhs, "Invalid left-hand side in assignment")
		case LogAndAssign, LogOrAssign, NullishAssign:
			if _, call := lhs.(*CallExpr); call {
				pos, _ := lhs.Range()
				p.fail(pos, "Invalid left-hand side in assignment")
			}
			fallthrough
		default:
			target = p.toSimpleTarget(lhs, "Invalid left-hand side in assignment")
		}
		p.next()
		value := p.parseAssign(noIn)
		return &AssignExpr{Span{start, p.prevEnd}, op, p.isParenthesized(lhs), target, value}
	}
	if p.tok == Arrow && !p.nlBefore {
		// async\n(a) => b: a call to async, which is not an arrow head.
		if call, ok := lhs.(*CallExpr); ok && call.Pos == start && isAsyncIdent(call.Callee) && p.asyncNL == start+1 {
			p.unexpected()
		}
	}
	return lhs
}

// tsAsyncArrowAhead reports whether `async x =>` starts here, with x on
// the same line as async and => on the same line as x.
func (p *parser) tsAsyncArrowAhead() bool {
	p.next()
	p.next()
	return p.tok == Arrow && !p.nlBefore
}

// tsHead is what tsParen read: an arrow function's parameters (the
// current token is then its =>), the arrow function itself when deciding
// needed its body, or else the parenthesised expression or async call.
type tsHead struct {
	params []Pattern
	rest   Pattern
	fn     Expr
	x      Expr
}

// tsParen parses the `(...)` at the current token where an arrow function
// may start at start (isParenthesizedArrowFunctionExpression,
// parseParenthesizedArrowFunctionExpression). Like parseParenOrArrow it
// reads the list as expressions, or after async as a call's arguments, and
// converts them to parameters when an arrow follows; the items may also
// carry what only a parameter can: `?`, a type, and a default after them.
//
// Where TypeScript's lookahead is sure of an arrow function (an empty
// list, a rest parameter, or a first item that is a name with `?` or a
// type), and where an item has a type or `?`, a return type may follow
// `)`. Elsewhere `:` after `)` may as well be the colon of a conditional
// expression or of a case clause: it starts a return type only when `=>`
// follows the type, and in the true branch of a conditional (blocked) only
// when the arrow function's body is followed by the conditional's colon
// too, so it is read with its body. A list after type parameters
// (generic) is never sure.
func (p *parser) tsParen(start int, async, generic, noIn, blocked bool) (h tsHead) {
	call := async && !generic
	cover := p.tsx.coverItem
	lparen := p.start
	p.next()
	var items []Expr
	var rest Pattern
	trailing := false
	typed, typedTok := -1, Illegal // the first `?` or type, which only a parameter has
	sure := !generic && p.tok == RParen
	note := func() {
		if typed < 0 {
			typed, typedTok = p.start, p.tok
		}
	}
	for p.tok != RParen {
		if p.tok == Ellipsis {
			spos := p.start
			sure = sure || !generic && len(items) == 0
			p.next()
			if call {
				x := p.parseAssign(false)
				items = append(items, &SpreadElem{Span{spos, p.prevEnd}, x})
				if p.tok == Colon {
					note()
					p.tsTypeAnnotation()
				}
				if p.tok == Comma {
					p.spreadComma = p.start
				}
			} else {
				rest = p.parseBindingTarget()
				if p.tok == Colon {
					note()
					p.tsTypeAnnotation()
				}
				if p.tok != RParen {
					p.fail(p.start, "Rest parameter must be last formal parameter")
				}
				break
			}
		} else {
			ipos := p.start
			if len(items) == 0 && (p.tok == KwThis || p.tsModifierKind()) {
				if tok, _, val := p.peek(); p.tok == KwThis && tok == Colon {
					p.fail(ipos, "An arrow function cannot have a 'this' parameter.")
				} else if p.tok != KwThis && !p.isIdent("async") && tok == Identifier && val != "as" {
					p.tsUnsupported(ipos, "A parameter property")
				}
			}
			p.tsx.coverItem = ipos + 1
			item := p.parseAssign(false)
			p.tsx.coverItem = cover
			param := false
			if p.tok == Question { // left by parseConditional (tsOptionalMark)
				note()
				p.next()
				param = true
			}
			if p.tok == Colon {
				note()
				p.tsTypeAnnotation()
				param = true
			}
			if param {
				if id, ok := item.(*Ident); ok && len(items) == 0 && !generic && id.Pos == ipos {
					sure = true
				}
				if p.tok == Assign {
					p.next()
					def := p.parseAssign(false)
					item = &AssignExpr{Span{ipos, p.prevEnd}, Assign, false, p.toAssignTarget(item, "Invalid destructuring assignment target"), def}
				}
			}
			items = append(items, item)
		}
		if !p.eat(Comma) {
			break
		}
		if p.tok == RParen {
			trailing = true
		}
	}
	rparen := p.start
	p.expect(RParen)
	switch {
	case p.tok == Arrow && !p.nlBefore:
	case p.tok == Colon && (sure || typed >= 0):
		p.next()
		p.tsReturnType()
		if p.tok != Arrow || p.nlBefore {
			p.unexpected()
		}
	case p.tok == Colon:
		// The items are converted in the speculative parse too: when they
		// are no parameters, the `:` is someone else's. A list that is not
		// an arrow function's is not tried again: in the true branch, the
		// arrow function's body is read too, and nested conditionals would
		// otherwise read each other's bodies again and again.
		if p.tsx.notArrow[lparen] {
			return p.tsParenExpr(start, lparen, rparen, items, rest, trailing, call, typed, typedTok)
		}
		if !p.tsTry(func() {
			p.next()
			p.tsReturnType()
			if p.tok != Arrow || p.nlBefore {
				p.unexpected()
			}
			h.params, h.rest = p.tsParams(items, rest, async, call)
			if blocked {
				h.fn = p.tsArrowBody(start, h.params, h.rest, async, noIn, true)
				if p.tok != Colon {
					p.unexpected()
				}
			}
		}) {
			p.tsNotArrow(lparen)
			return p.tsParenExpr(start, lparen, rparen, items, rest, trailing, call, typed, typedTok)
		}
		return h
	default:
		return p.tsParenExpr(start, lparen, rparen, items, rest, trailing, call, typed, typedTok)
	}
	h.params, h.rest = p.tsParams(items, rest, async, call)
	return h
}

// tsParenExpr returns what tsParen read when no arrow function follows.
func (p *parser) tsParenExpr(start, lparen, rparen int, items []Expr, rest Pattern, trailing, call bool, typed int, typedTok Token) tsHead {
	if typed >= 0 {
		p.unexpectedAt(typedTok, typed, typedTok.String())
	}
	if call {
		callee := &Ident{Span: Span{start, start + len("async")}, Name: "async"}
		return tsHead{x: &CallExpr{Span{start, p.prevEnd}, callee, items, false}}
	}
	if rest != nil || trailing || len(items) == 0 {
		p.fail(rparen, "Unexpected token ')'")
	}
	if len(items) == 1 {
		p.markParenthesized(items[0])
		p.tsParenCast(items[0])
		return tsHead{x: items[0]}
	}
	return tsHead{x: &SeqExpr{Span{lparen, p.prevEnd}, items}}
}

// tsParams converts the items of tsParen to parameters, as an async
// arrow function's when async (whose list is a call's arguments when
// call).
func (p *parser) tsParams(items []Expr, rest Pattern, async, call bool) ([]Pattern, Pattern) {
	if async {
		if pos, expr := firstAwait(items); expr {
			p.fail(pos, "Illegal await-expression in formal parameters of async function")
		} else if pos >= 0 {
			p.fail(pos, "Unexpected reserved word")
		}
	}
	if p.tsx.casts != nil {
		for _, item := range items {
			p.tsNoCasts(item)
		}
	}
	if call {
		return p.argsToParams(items)
	}
	params := make([]Pattern, len(items))
	for i, item := range items {
		params[i] = p.toParam(item)
	}
	return params, rest
}

// tsHeadArrow parses the body of the arrow function whose head tsParen
// read.
func (p *parser) tsHeadArrow(h tsHead, start int, async, noIn, blocked bool) Expr {
	if h.fn != nil {
		return h.fn
	}
	return p.tsArrowBody(start, h.params, h.rest, async, noIn, blocked)
}

// tsArrowBody parses `=> body`. A concise body is blocked as its arrow
// function is (see tsParen).
func (p *parser) tsArrowBody(start int, params []Pattern, rest Pattern, async, noIn, blocked bool) Expr {
	if blocked && p.tok == Arrow {
		if tok, _, _ := p.peek(); tok != LBrace {
			p.tsx.blockRet = true
		}
	}
	return p.parseArrowBody(start, params, rest, async, noIn)
}

// tsGenericArrow parses the arrow function with type parameters at the
// current `<`, or after async (the current token) when async. When there
// is none, <T>(x) is a type assertion and async < T a comparison: it
// returns nil and leaves the parser where it was, except after <T>(x),
// where it returns the parenthesised expression x, the start of the
// assertion's operand, which starts at the `(` at. Only the head of the
// arrow function is read speculatively, and a `<` that starts none is not
// tried again; with the reuse of x, nested assertions <T>(<T>(...)) parse
// in linear time.
func (p *parser) tsGenericArrow(start int, async, noIn, blocked bool) (arrow, operand Expr, at int) {
	if p.tsx.notArrow[start] {
		return nil, nil, 0
	}
	var h tsHead
	if !p.tsTry(func() {
		if async {
			p.next()
		}
		simple := p.tsTypeParams()
		if p.tok != LParen {
			p.unexpected()
		}
		at = p.start
		h = p.tsParen(start, async, true, noIn, blocked)
		if h.x != nil && (async || !simple) {
			p.unexpected()
		}
	}) {
		p.tsNotArrow(start)
		return nil, nil, 0
	}
	if h.x != nil {
		return nil, h.x, at
	}
	return p.tsHeadArrow(h, start, async, noIn, blocked), nil, 0
}

// tsNotArrow records that no arrow function starts at pos.
func (p *parser) tsNotArrow(pos int) {
	if p.tsx.notArrow == nil {
		p.tsx.notArrow = make(map[int]bool)
	}
	p.tsx.notArrow[pos] = true
}

// tsOptionalMark reports whether the `?` after the expression that starts
// at start marks an optional parameter: the expression is an item of the
// list tsParen is reading, and `:`, `,`, `=` or `)` follows the `?`.
func (p *parser) tsOptionalMark(start int) bool {
	if p.tsx.coverItem != start+1 {
		return false
	}
	switch tok, _, _ := p.peek(); tok {
	case Colon, Comma, Assign, RParen:
		return true
	}
	return false
}

// tsAs skips `as T` or `satisfies T` after the operand x, whose operator
// binds looser than a relational one (minPrec), the keyword on the
// operand's line, and reports whether it did.
func (p *parser) tsAs(x Expr, minPrec int) bool {
	if p.tok != Identifier || p.escaped || p.nlBefore || minPrec >= binaryPrec(Lt) || p.val != "as" && p.val != "satisfies" {
		return false
	}
	p.next()
	p.tsType()
	p.tsCast(x, tsCastAs)
	return true
}

// tsTypeAssertion parses <T>x (parseTypeAssertion), returning x.
func (p *parser) tsTypeAssertion() Expr {
	p.next()
	p.tsType()
	p.tsGt()
	x := p.parseUnary()
	p.tsCast(x, tsCastAs)
	return x
}

// tsMember skips the TypeScript link of a member or call chain after x at
// the current token, a non-null assertion x! or the type arguments of
// f<T>(x) or of an instantiation expression f<T>, and reports whether it
// did.
func (p *parser) tsMember(x Expr) bool {
	switch p.tok {
	case Not:
		if p.nlBefore {
			return false
		}
		p.next()
	case Lt, Shl:
		if !p.tsTypeArgsInExpr() {
			return false
		}
		// f<T>.g and f<T>?.g, but not f<T>?.() or f<T>?.[0]
		if tok, _, _ := p.peek(); p.tok == Dot || p.tok == QuestionDot && tok != LParen && tok != LBrack && tok != Lt && tok != Shl {
			p.fail(p.start, "An instantiation expression cannot be followed by a property access.")
		}
	default:
		return false
	}
	p.tsCast(x, tsCastLHS)
	return true
}

// tsCast records that an erased cast of kind applied to x.
func (p *parser) tsCast(x Expr, kind uint8) {
	if p.tsx.casts == nil {
		p.tsx.casts = make(map[Expr]uint8)
	}
	p.tsx.casts[x] |= kind
}

// tsParenCast records that x, which parentheses enclose, is a left-hand
// side expression again: (a as T) = b, but not a as T = b. Only tsParen's
// parentheses need it: the others enclose an operand, which no assignment
// can have as its target.
func (p *parser) tsParenCast(x Expr) {
	if c := p.tsx.casts[x]; c&tsCastAs != 0 {
		p.tsx.casts[x] = c&^tsCastAs | tsCastParen
	}
}

// tsNoCastTarget fails when x, the left operand of an assignment, is an
// erased as, satisfies or <T>, which is no left-hand side expression
// (TypeScript then reads the assignment operator as the start of another
// statement).
func (p *parser) tsNoCastTarget(x Expr) {
	if p.tsx.casts[x]&tsCastAs != 0 {
		pos, _ := x.Range()
		p.fail(pos, "Invalid left-hand side in assignment")
	}
}

// tsNoCasts fails when an erased as, satisfies, ! or <T> applied to a
// binding target in x, an item of a list about to become an arrow
// function's parameters: TypeScript reads (a!) => a, (a as T) => a and
// ({ a: b! }) => b as no parameter list. A default value may hold them.
func (p *parser) tsNoCasts(x Node) {
	if e, ok := x.(Expr); ok && p.tsx.casts[e] != 0 {
		pos, _ := x.Range()
		p.fail(pos, "Invalid destructuring assignment target")
	}
	switch x := x.(type) {
	case *AssignExpr:
		p.tsNoCasts(x.Target)
	case *SpreadElem:
		p.tsNoCasts(x.X)
	case *ObjectLit:
		for _, prop := range x.Props {
			if prop.Kind == PropInit || prop.Kind == PropSpread {
				p.tsNoCasts(prop.Value)
			}
		}
	case *ArrayLit:
		for _, e := range x.Elems {
			if e != nil {
				p.tsNoCasts(e)
			}
		}
	case *ObjectPattern:
		for _, prop := range x.Props {
			p.tsNoCasts(prop.Value)
		}
		if x.Rest != nil {
			p.tsNoCasts(x.Rest)
		}
	case *ArrayPattern:
		for _, e := range x.Elems {
			if e != nil {
				p.tsNoCasts(e)
			}
		}
		if x.Rest != nil {
			p.tsNoCasts(x.Rest)
		}
	case *AssignPattern:
		p.tsNoCasts(x.Target)
	}
}

// ---- Declarations -----------------------------------------------------------

// tsDeclaration parses the TypeScript declaration that starts at the
// current token, if one does (isStartOfDeclaration for the TypeScript
// forms): type, interface, namespace, module, global, declare, abstract
// class, enum and const enum. It returns the kind of declaration and, for
// an abstract class, the class; every other kind is erased.
func (p *parser) tsDeclaration() (Stmt, int) {
	switch p.tok {
	case KwInterface, KwEnum, KwConst, Identifier:
	default:
		return nil, tsNoDecl
	}
	recTop := p.tsx.recTop
	p.tsx.recTop = p.tsAtTop()
	defer func() { p.tsx.recTop = recTop }()
	start := p.start
	tok, nl, _ := p.peek()
	switch p.tok {
	case KwInterface:
		if nl || tok != Identifier {
			return nil, tsNoDecl
		}
		p.tsInterface()
		return nil, tsTypeDecl
	case KwEnum:
		if p.tsx.ambient == 0 {
			p.tsUnsupported(start, "A TypeScript enum")
		}
		p.tsRecord(p.tsEnum(), tsAmbientName)
		return nil, tsValueDecl
	case KwConst:
		if tok != KwEnum {
			return nil, tsNoDecl
		}
		if p.tsx.ambient == 0 {
			p.tsUnsupported(start, "A TypeScript const enum")
		}
		p.next()
		p.tsRecord(p.tsEnum(), tsAmbientName)
		return nil, tsValueDecl
	case Identifier:
		if p.escaped || nl {
			return nil, tsNoDecl
		}
		switch p.val {
		case "type":
			if tok != Identifier {
				return nil, tsNoDecl
			}
			p.tsTypeAlias()
			return nil, tsTypeDecl
		case "namespace", "module":
			if tok != Identifier && tok != String {
				return nil, tsNoDecl
			}
			return nil, p.tsNamespace()
		case "global":
			if tok != LBrace {
				return nil, tsNoDecl
			}
			if p.tsx.ambient == 0 {
				p.fail(start, "Augmentations for the global scope can only be directly nested in external modules or ambient module declarations.")
			}
			p.next()
			p.tsModuleBody()
			return nil, tsValueDecl
		case "declare":
			if !p.tsDeclareAhead(tok) {
				return nil, tsNoDecl
			}
			return nil, p.tsDeclare()
		case "abstract":
			if tok != KwClass {
				return nil, tsNoDecl
			}
			p.next()
			c := p.parseClassDecl(true)
			if p.tsx.ambient > 0 {
				p.tsRecord(c.Class.Name.Name, tsAmbientName)
				return nil, tsValueDecl
			}
			return c, tsClassDecl
		}
	}
	return nil, tsNoDecl
}

// tsSingleDeclaration parses the TypeScript declaration at start, where
// only a statement may appear (if (a) type T = 1), and returns the empty
// statement tsc erases it to, or nil when no declaration starts there. An
// abstract class fails there, as a class does.
func (p *parser) tsSingleDeclaration(start int) Stmt {
	if tok, nl, _ := p.peek(); p.isIdent("abstract") && tok == KwClass && !nl {
		p.next()
		p.unexpected()
	}
	if _, kind := p.tsDeclaration(); kind == tsNoDecl {
		return nil
	}
	return &EmptyStmt{Span{start, p.prevEnd}}
}

// tsDeclareAhead reports whether the declare at the current token, which
// tok follows on the same line, starts an ambient declaration.
func (p *parser) tsDeclareAhead(tok Token) bool {
	switch tok {
	case KwVar, KwLet, KwConst, KwFunction, KwClass, KwEnum, KwInterface:
		return true
	case Identifier:
		return p.tsLook(func() bool {
			p.next()
			if p.escaped {
				return false
			}
			switch p.val {
			case "type", "namespace", "module", "global", "abstract", "async":
				return true
			}
			return false
		})
	}
	return false
}

// tsDeclare parses an ambient declaration (declare ...), which is erased,
// and returns its kind, as tsc counts it when it decides whether a
// namespace is instantiated (getModuleInstanceState).
func (p *parser) tsDeclare() int {
	top := p.tsAtTop()
	p.next()
	if top {
		p.tsx.top = p.start + 1
	}
	p.tsx.ambient++
	defer func() { p.tsx.ambient-- }()
	switch p.tok {
	case KwVar, KwLet, KwConst:
		if tok, _, _ := p.peek(); p.tok == KwConst && tok == KwEnum {
			break
		}
		p.tsAmbientVar()
		return tsValueDecl
	case KwFunction:
		p.tsAmbientFunction(p.start, false)
		return tsValueDecl
	case KwClass:
		p.tsRecord(p.parseClassDecl(true).Class.Name.Name, tsAmbientName)
		return tsValueDecl
	case Identifier:
		if p.isIdent("async") {
			start := p.start
			p.next()
			p.tsAmbientFunction(start, true)
			return tsValueDecl
		}
	}
	_, kind := p.tsDeclaration()
	if kind == tsNoDecl {
		p.unexpected()
	}
	return kind
}

// tsAmbientVar parses the declarators of declare var, let or const, whose
// initializers are optional.
func (p *parser) tsAmbientVar() {
	p.next()
	for {
		target := p.parseBindingTarget()
		for _, id := range boundNames(target, nil) {
			p.tsRecord(id.Name, tsAmbientName)
		}
		p.tsDeclaratorType(target)
		if p.eat(Assign) {
			p.parseAssign(false)
		}
		if !p.eat(Comma) {
			break
		}
	}
	p.semicolon()
}

// tsAmbientFunction parses declare function f(): T.
func (p *parser) tsAmbientFunction(start int, async bool) {
	p.tsx.bodiless = true
	fd := p.parseFunctionDecl(start, async, true)
	p.tsRecord(fd.Func.Name.Name, tsAmbientName)
}

// tsFunctionDecl parses a function declaration, which may be an overload
// signature: nil then.
func (p *parser) tsFunctionDecl(start int, async, requireName bool) Stmt {
	p.tsx.bodiless = true
	fd := p.parseFunctionDecl(start, async, requireName)
	if fd.Func.Body == nil {
		return nil
	}
	return fd
}

// tsFunctionTail parses a function's return type after its parameters and
// reports whether the function has no body: an overload signature or an
// ambient function, where bodiless allows one.
func (p *parser) tsFunctionTail(bodiless bool) bool {
	if p.eat(Colon) {
		p.tsReturnType()
	}
	if !bodiless || p.tok == LBrace || !p.tsCanSemicolon() {
		return false
	}
	p.semicolon()
	return true
}

// tsInterface parses interface I<T> extends A, B<T> { ... }.
func (p *parser) tsInterface() {
	p.next()
	name := p.tsBindingName()
	if p.tok == Lt {
		p.tsTypeParams()
	}
	if p.eat(KwExtends) {
		for {
			p.tsHeritage()
			if !p.eat(Comma) {
				break
			}
		}
	}
	p.tsTypeLiteral()
	p.tsRecord(name, tsTypeName)
}

// tsHeritage skips a type in an extends clause of an interface or in an
// implements clause: a dotted name with type arguments, whose `<` is never
// the half of a `<<` (tryParseTypeArguments).
func (p *parser) tsHeritage() {
	p.tsEntityName()
	if p.tok == Lt {
		p.tsTypeArgs()
	}
}

// tsTypeAlias parses type T<P> = Type.
func (p *parser) tsTypeAlias() {
	p.next()
	name := p.tsBindingName()
	if p.tok == Lt {
		p.tsTypeParams()
	}
	p.expect(Assign)
	p.tsType()
	p.semicolon()
	p.tsRecord(name, tsTypeName)
}

// tsEnum parses the enum declaration of ambient code and returns its name.
func (p *parser) tsEnum() string {
	p.expect(KwEnum)
	name := p.tsBindingName()
	p.expect(LBrace)
	for p.tok != RBrace {
		p.tsPropertyName()
		if p.eat(Assign) {
			p.parseAssign(false)
		}
		if !p.eat(Comma) {
			break
		}
	}
	p.expect(RBrace)
	return name
}

// tsNamespace parses namespace N.M { ... } or module N { ... }, and in
// ambient code module "m" { ... }, and returns its kind. Outside ambient
// code it is erased when tsc counts it as not instantiated: when it holds
// only types, other such namespaces and import aliases; otherwise it fails,
// since tsc emits an object for it.
func (p *parser) tsNamespace() int {
	start := p.start
	p.next()
	if p.tok == String {
		if p.tsx.ambient == 0 {
			p.fail(start, "Only ambient modules can use quoted names.")
		}
		p.next()
		if p.tok == LBrace {
			p.tsModuleBody()
		} else {
			p.semicolon()
		}
		return tsValueDecl
	}
	name := p.tsBindingName()
	for p.eat(Dot) {
		p.tsBindingName()
	}
	if p.tsModuleBody() {
		if p.tsx.ambient == 0 {
			p.tsUnsupported(start, "A TypeScript namespace with values")
		}
		p.tsRecord(name, tsAmbientName)
		return tsValueDecl
	}
	if p.tsx.ambient > 0 {
		p.tsRecord(name, tsAmbientName)
	} else {
		p.tsRecord(name, tsTypeName)
	}
	return tsTypeDecl
}

// tsModuleBody parses the block of a namespace or ambient module and
// reports whether one of its statements instantiates it to tsc. Outside
// ambient code, it stops at the first such statement, which fails.
func (p *parser) tsModuleBody() bool {
	p.enter()
	recTop, ns := p.tsx.recTop, p.tsx.ns
	p.tsx.recTop, p.tsx.ns = false, append(ns, nil)
	defer func() {
		p.tsx.recTop, p.tsx.ns = recTop, ns
		p.leave()
	}()
	p.expect(LBrace)
	inst := false
	for p.tok != RBrace {
		if p.tok == EOF {
			p.unexpected()
		}
		if p.tsModuleItem() {
			if p.tsx.ambient == 0 {
				return true
			}
			inst = true
		}
	}
	p.next()
	return inst
}

// tsModuleItem parses a statement of a namespace or ambient module body
// and reports whether it instantiates the namespace to tsc. Outside ambient
// code it parses only what does not.
func (p *parser) tsModuleItem() bool {
	ambient := p.tsx.ambient > 0
	exported := false
	if p.tok == KwExport {
		start := p.start
		tok, _, val := p.peek()
		switch {
		case tok == KwImport:
			p.next()
			p.next()
			p.tsImportEquals(start, p.parseBindingIdent(), false, true)
			return true
		case tok == LBrace && !ambient:
			p.next()
			return p.tsLocalExports()
		case ambient && (tok == Assign || tok == LBrace || tok == Mul || tok == KwDefault || tok == Identifier && val == "as"):
			p.parseExport()
			return true
		}
		p.next()
		exported = true
	}
	if p.tok == KwImport {
		if ambient {
			// Any import: an ambient module may import from others, and
			// tsImport tells import x = A.B from import x from "m".
			p.parseImport()
			return exported
		}
		start := p.start
		p.next()
		typeOnly := false
		if tok, _, _ := p.peek(); p.isIdent("type") && tok == Identifier {
			typeOnly = true
			p.next()
		}
		p.tsImportEquals(start, p.parseBindingIdent(), typeOnly, false)
		return exported && !typeOnly
	}
	if _, kind := p.tsDeclaration(); kind != tsNoDecl {
		return kind != tsTypeDecl
	}
	if !ambient {
		return true
	}
	switch p.tok {
	case KwVar, KwLet, KwConst:
		p.tsAmbientVar()
	case KwFunction:
		p.tsAmbientFunction(p.start, false)
	default:
		p.parseStatementListItem()
	}
	return true
}

// tsLocalExports parses export { ... } in a namespace body, which does not
// instantiate it when it names types only.
func (p *parser) tsLocalExports() bool {
	inst := false
	p.expect(LBrace)
	for p.tok != RBrace {
		name, _, isType := p.tsSpecifier(false)
		if !isType && !p.tsTypeNamed(name.Name) {
			inst = true
		}
		if p.tok != RBrace {
			p.expect(Comma)
		}
	}
	p.next()
	p.semicolon()
	return inst
}

// tsTypeNamed reports whether a namespace body being parsed or the module
// declared name as a type before.
func (p *parser) tsTypeNamed(name string) bool {
	for _, names := range p.tsx.ns {
		if names[name]&tsTypeName != 0 {
			return true
		}
	}
	return p.tsx.info.names[name]&tsTypeName != 0
}

// ---- Imports and exports ----------------------------------------------------

// tsImport is parseImport for TypeScript, after the import keyword at
// start: type-only imports and type-only specifiers are erased (their names
// recorded as types), as is an import that binds nothing as a value, and
// import x = A.B is an alias that the scope pass erases unless x is used as
// a value.
func (p *parser) tsImport(start int) Stmt {
	recTop := p.tsx.recTop
	p.tsx.recTop = p.tsx.top == start+1
	defer func() { p.tsx.recTop = recTop }()
	decl := &ImportDecl{}
	if p.tok == String {
		decl.Source = p.parseModuleSource()
		p.semicolon()
		decl.Span = Span{start, p.prevEnd}
		return decl
	}
	typeOnly := false
	if p.isIdent("type") {
		switch tok, _, val := p.peek(); {
		case tok == Mul || tok == LBrace || tok == Identifier && val != "from":
			typeOnly = true
		case tok == Identifier:
			// import type from from "m", import type from = require("m")
			typeOnly = p.tsLook(func() bool {
				p.next()
				p.next()
				return p.isIdent("from") || p.tok == Assign
			})
		}
		if typeOnly {
			p.next()
		}
	}
	if p.tok == Identifier {
		if tok, _, val := p.peek(); p.isIdent("defer") && tok == Mul && !typeOnly {
			p.unsupported(start, "import defer")
		} else if p.isIdent("source") && tok == Identifier && val != "from" && !typeOnly {
			p.unsupported(start, "import source")
		}
		def := p.parseBindingIdent()
		if p.tok != Comma && !p.isIdent("from") {
			p.tsImportEquals(start, def, typeOnly, false)
			return nil
		}
		if typeOnly {
			p.tsRecord(def.Name, tsTypeName)
		} else {
			decl.Default = def
		}
		if !p.eat(Comma) {
			return p.tsFinishImport(start, decl)
		}
	}
	switch p.tok {
	case Mul:
		p.next()
		if !p.isIdent("as") {
			p.unexpected()
		}
		p.next()
		ns := p.parseBindingIdent()
		if typeOnly {
			p.tsRecord(ns.Name, tsTypeName)
		} else {
			decl.Namespace = ns
		}
	case LBrace:
		p.next()
		for p.tok != RBrace {
			spos := p.start
			imported, local, isType := p.tsSpecifier(true)
			if typeOnly || isType {
				p.tsRecord(local.Name, tsTypeName)
			} else {
				decl.Specs = append(decl.Specs, &ImportSpec{Span{spos, p.prevEnd}, imported, local})
			}
			if p.tok != RBrace {
				p.expect(Comma)
			}
		}
		p.next()
	default:
		p.unexpected()
	}
	return p.tsFinishImport(start, decl)
}

// tsFinishImport parses the from clause of an import declaration and
// returns the declaration, or nil when it binds nothing as a value: tsc
// drops import {} from "m" too, unlike import "m".
func (p *parser) tsFinishImport(start int, decl *ImportDecl) Stmt {
	p.finishImport(start, decl)
	if decl.Default == nil && decl.Namespace == nil && len(decl.Specs) == 0 {
		return nil
	}
	return decl
}

// tsImportEquals parses `= require("m")` or `= A.B` after import x (export
// import x when exported) at start. Only the type-only forms and the alias
// of a namespace are erasable; the scope pass erases an alias that is not
// used as a value.
func (p *parser) tsImportEquals(start int, name *Ident, typeOnly, exported bool) {
	p.expect(Assign)
	ambient := p.tsx.ambient > 0
	if tok, _, _ := p.peek(); p.isIdent("require") && tok == LParen {
		p.next()
		p.next()
		if p.tok != String {
			p.unexpected()
		}
		p.next()
		p.expect(RParen)
		p.semicolon()
		if !typeOnly && !ambient {
			p.tsUnsupported(start, "import = require")
		}
	} else {
		p.tsEntityName()
		p.semicolon()
		if exported && !ambient {
			p.tsUnsupported(start, "export import")
		}
	}
	// An alias in a namespace body binds nothing in the module, and tsc
	// counts it as a value there whatever it names.
	switch {
	case !p.tsx.recTop:
	case typeOnly:
		p.tsRecord(name.Name, tsTypeName)
	case !ambient && !exported:
		p.tsx.info.aliases = append(p.tsx.info.aliases, name)
	}
}

// tsSpecifier parses an import specifier (local is the binding then) or
// an export specifier (parseImportOrExportSpecifier), which may be marked
// type-only: `type x`, `type x as y`, but `type`, `type as x` and `type
// as as` name type.
func (p *parser) tsSpecifier(isImport bool) (name, local *Ident, isType bool) {
	nameTok := p.tok
	name = p.parseModuleExportName()
	canAs := true
	if nameTok == Identifier && name.Name == "type" && name.End-name.Pos == len("type") {
		switch {
		case p.isIdent("as"):
			firstAs := p.parseIdentifierName()
			switch {
			case p.isIdent("as"):
				secondAs := p.parseIdentifierName()
				if isIdentName(p.tok) || p.tok == String {
					isType, name, canAs = true, firstAs, false // type as as x
					local = p.tsSpecifierLocal(isImport)
				} else {
					local, canAs = secondAs, false // type as as
				}
			case isIdentName(p.tok) || p.tok == String:
				canAs = false // type as x
				local = p.tsSpecifierLocal(isImport)
			default:
				isType, name, local = true, firstAs, firstAs // type as
			}
		case isIdentName(p.tok) || p.tok == String:
			isType = true // type x
			nameTok = p.tok
			name = p.parseModuleExportName()
		}
	}
	if canAs && p.isIdent("as") {
		p.next()
		local = p.tsSpecifierLocal(isImport)
	}
	if local == nil {
		local = name
		if isImport {
			if nameTok == EscapedWord {
				p.unexpectedAt(EscapedWord, name.Pos, "")
			}
			if nameTok != Identifier {
				p.fail(name.Pos, "Unexpected token '"+name.Name+"'")
			}
			p.checkBindingName(name)
		}
	} else if isImport {
		p.checkBindingName(local)
	}
	return name, local, isType
}

// tsSpecifierLocal parses the name after as in a specifier.
func (p *parser) tsSpecifierLocal(isImport bool) *Ident {
	if isImport {
		return p.parseBindingIdent()
	}
	return p.parseModuleExportName()
}

// tsExport parses the TypeScript forms of an export declaration after the
// export keyword at start and reports whether it did. Erased forms give a
// nil statement.
func (p *parser) tsExport(start int) (Stmt, bool) {
	if p.tok == Div || p.tok == DivAssign {
		return nil, false
	}
	ambient := p.tsx.ambient > 0
	recTop := p.tsx.recTop
	p.tsx.recTop = p.tsx.top == start+1
	defer func() { p.tsx.recTop = recTop }()
	if p.tsx.recTop {
		p.tsx.top = p.start + 1
	}
	tok, nl, val := p.peek()
	switch p.tok {
	case Assign:
		if !ambient {
			p.tsUnsupported(start, "export =")
		}
		p.next()
		p.parseAssign(false)
		p.semicolon()
		return nil, true
	case KwImport:
		p.next()
		p.tsImportEquals(start, p.parseBindingIdent(), false, true)
		return nil, true
	case KwDefault:
		switch {
		case tok == KwInterface:
			p.next()
			p.tsInterface()
			return nil, true
		case tok == Identifier && val == "abstract" && p.tsLook(func() bool {
			p.next()
			tok, nl, _ := p.peek()
			return tok == KwClass && !nl
		}):
			p.next()
			p.next()
			c := p.parseClassDecl(false)
			return &ExportDefault{Span{start, p.prevEnd}, c}, true
		case tok == KwFunction:
			p.next()
			if fd := p.tsFunctionDecl(p.start, false, false); fd != nil {
				return &ExportDefault{Span{start, p.prevEnd}, fd}, true
			}
			return nil, true
		case tok == Identifier && val == "async" && p.tsLook(func() bool {
			p.next()
			tok, nl, _ := p.peek()
			return tok == KwFunction && !nl
		}):
			p.next()
			fstart := p.start
			p.next()
			if fd := p.tsFunctionDecl(fstart, true, false); fd != nil {
				return &ExportDefault{Span{start, p.prevEnd}, fd}, true
			}
			return nil, true
		}
		return nil, false
	case LBrace:
		return p.tsExportClause(start), true
	case KwFunction:
		if fd := p.tsFunctionDecl(p.start, false, true); fd != nil {
			return &ExportDecl{Span{start, p.prevEnd}, fd}, true
		}
		return nil, true
	case KwVar, KwLet, KwConst:
		if ambient && tok != KwEnum {
			p.tsAmbientVar()
			return nil, true
		}
	case Identifier:
		switch {
		case p.isIdent("type") && (tok == LBrace || tok == Mul):
			// export type { ... } [from "m"], export type * [as ns] from "m"
			p.next()
			if p.eat(Mul) {
				if p.isIdent("as") {
					p.next()
					p.parseModuleExportName()
				}
				if !p.isIdent("from") {
					p.unexpected()
				}
				p.next()
				p.parseModuleSource()
				p.semicolon()
			} else {
				p.tsExportClause(start)
			}
			return nil, true
		case p.isIdent("as") && tok == Identifier && val == "namespace":
			if !ambient {
				p.tsUnsupported(start, "export as namespace")
			}
			p.next()
			p.next()
			p.tsIdentName()
			p.semicolon()
			return nil, true
		case p.isIdent("async") && tok == KwFunction && !nl:
			fstart := p.start
			p.next()
			if fd := p.tsFunctionDecl(fstart, true, true); fd != nil {
				return &ExportDecl{Span{start, p.prevEnd}, fd}, true
			}
			return nil, true
		}
	}
	if s, kind := p.tsDeclaration(); kind != tsNoDecl {
		if s != nil {
			return &ExportDecl{Span{start, p.prevEnd}, s}, true
		}
		return nil, true
	}
	return nil, false
}

// tsExportClause parses export { ... } [from "m"] at its `{`, dropping the
// type-only specifiers, and the declaration when no specifier is left: tsc
// drops export {} from "m" too, unlike export * from "m". A local export
// of a name that turns out to be a type is dropped by the scope pass.
func (p *parser) tsExportClause(start int) Stmt {
	p.next()
	named := &ExportNamed{}
	var localToks []Token
	for p.tok != RBrace {
		spos, tok := p.start, p.tok
		local, exported, isType := p.tsSpecifier(false)
		if !isType {
			named.Specs = append(named.Specs, &ExportSpec{Span{spos, p.prevEnd}, local, exported})
			localToks = append(localToks, tok)
		}
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
	if len(named.Specs) == 0 {
		return nil
	}
	return named
}

// ---- Functions and classes --------------------------------------------------

// tsParam is parseBindingElement for a parameter: a name or pattern, `?`,
// a type and a default. A parameter property (constructor(private x))
// fails.
func (p *parser) tsParam() Pattern {
	start := p.start
	switch p.tok {
	case KwPublic, KwPrivate, KwProtected:
		p.tsParamProperty()
	case Identifier:
		if p.isIdent("readonly") || p.isIdent("override") {
			p.tsParamProperty()
		}
	}
	target := p.parseBindingTarget()
	p.eat(Question)
	p.tsTypeAnnotation()
	if !p.eat(Assign) {
		return target
	}
	def := p.parseAssign(false)
	return &AssignPattern{Span{start, p.prevEnd}, target, def}
}

// tsParamProperty fails when the current token is a modifier that makes
// the parameter a parameter property.
func (p *parser) tsParamProperty() {
	if p.tsModifier() {
		p.tsUnsupported(p.start, "A parameter property")
	}
}

// tsThisParam skips a leading `this` parameter, which is not counted in
// the function's length.
func (p *parser) tsThisParam() {
	if p.tok != KwThis {
		return
	}
	p.next()
	p.tsTypeAnnotation()
	if p.tok != RParen {
		p.expect(Comma)
	}
}

// tsClassHeritage skips the type parameters after a class's name.
func (p *parser) tsClassTypeParams() {
	if p.tok == Lt {
		p.tsTypeParams()
	}
}

// tsClassHeritage skips the type arguments of a class's extends clause
// and its implements clause.
func (p *parser) tsClassHeritage() {
	if p.tok == Lt {
		p.tsTypeArgs()
	}
	if p.eat(KwImplements) {
		for {
			p.tsHeritage()
			if !p.eat(Comma) {
				break
			}
		}
	}
}

// tsClassMember is parseClassMember for TypeScript (parseClassElement):
// modifiers, index signatures, optional and definite members, types, and
// signatures without a body. It returns nil for a member that erases to
// nothing: an index signature, a method without a body (an overload or an
// abstract method), and a declare or abstract field. A field with only a
// type is kept, initialised to undefined, as tsc and esbuild do for
// ECMAScript 2022 and later.
func (p *parser) tsClassMember() *ClassMember {
	m := &ClassMember{Span: Span{p.start, 0}}
	isAsync, declare, abstract := false, false, false
	start := 0
modifiers:
	for {
		tok, nl, _ := p.peek()
		switch {
		case p.tok == KwStatic:
			if tok == LBrace && !m.Static {
				return p.tsStaticBlock(m)
			}
			if m.Static || !tsCanFollowModifier(tok) {
				break modifiers
			}
			m.Static = true
		case nl || !tsCanFollowModifier(tok):
			break modifiers
		case p.tok == KwPublic || p.tok == KwPrivate || p.tok == KwProtected:
		case p.tok == Identifier && !p.escaped:
			switch p.val {
			case "readonly", "override":
			case "declare":
				declare = true
			case "abstract":
				abstract = true
			case "accessor":
				p.tsUnsupported(p.start, "An accessor field")
			case "async":
				isAsync, start = true, p.start
			default:
				break modifiers
			}
		default:
			break modifiers
		}
		p.next()
	}
	if !isAsync {
		start = p.start // a method's source text starts after its modifiers
	}
	kind := FuncMethod
	isGen := false
	if p.eat(Mul) {
		isGen = true
	} else if p.tsAccessorAhead() {
		kind = FuncGetter
		if p.val == "set" {
			kind = FuncSetter
		}
		p.next()
	}
	if p.tok == LBrack && !isGen && kind == FuncMethod && p.tsLook(p.tsIndexSignatureAhead) {
		p.tsIndexSignature()
		if !p.eat(Comma) {
			p.semicolon()
		}
		return nil
	}
	keyPos := p.start
	if p.tok == PrivateIdent {
		pn := &PrivateName{Span: Span{p.start, p.end}, Name: p.val}
		if pn.Name == "constructor" {
			p.fail(keyPos, "Classes may not have a private field named '#constructor'")
		}
		m.Key = pn
		p.next()
	} else {
		m.Key, m.Computed = p.parsePropertyKey()
	}
	name := ""
	if !m.Computed {
		name = staticPropName(m.Key)
	}
	if m.Static && name == "prototype" {
		p.fail(keyPos, "Classes may not have a static property named 'prototype'")
	}
	optional := p.eat(Question)
	if isGen || p.tok == LParen || p.tok == Lt {
		if !m.Static && name == "constructor" {
			switch {
			case kind == FuncGetter || kind == FuncSetter:
				p.fail(keyPos, "Class constructor may not be an accessor")
			case isAsync:
				p.fail(keyPos, "Class constructor may not be an async method")
			case isGen:
				p.fail(keyPos, "Class constructor may not be a generator")
			}
			kind = FuncClassConstructor
		}
		p.tsx.bodiless = true
		fn := p.parseMethod(start, m.Key, m.Computed, kind, isAsync, isGen)
		if fn.Body == nil {
			return nil
		}
		if abstract {
			p.fail(keyPos, "An abstract method cannot have an implementation.")
		}
		if declare {
			p.fail(m.Pos, "'declare' modifier cannot appear on a method with a body.")
		}
		m.Value = fn
		switch kind {
		case FuncGetter:
			m.Kind = ClassGetter
		case FuncSetter:
			m.Kind = ClassSetter
		default:
			m.Kind = ClassMethod
		}
		m.End = p.prevEnd
		return m
	}
	if isAsync || isGen || kind != FuncMethod {
		p.unexpected()
	}
	if name == "constructor" {
		p.fail(keyPos, "Classes may not have a field named 'constructor'")
	}
	if !optional && !p.nlBefore && p.tok == Not {
		p.next() // definite: x!: T
	}
	p.tsTypeAnnotation()
	m.Kind = ClassField
	if p.eat(Assign) {
		fn := &Function{IsStrict: true}
		saved := p.enterFunction(fn)
		m.Value = p.parseAssign(false)
		p.leaveFunction(fn, saved)
	}
	p.semicolon()
	m.End = p.prevEnd
	if declare || abstract {
		return nil
	}
	return m
}

// tsStaticBlock parses static { ... } at its static.
func (p *parser) tsStaticBlock(m *ClassMember) *ClassMember {
	m.Static = true
	m.Kind = ClassStaticBlock
	p.next()
	fn := &Function{IsStrict: true}
	saved := p.enterFunction(fn)
	p.setAwaitKw(true) // reserved in a static block, as in a module
	depth := p.funcDepth
	p.funcDepth = 0 // `return` is not allowed in a static block
	m.Body = p.parseBlock()
	p.funcDepth = depth
	p.leaveFunction(fn, saved)
	m.End = p.prevEnd
	return m
}

// tsDeclaratorType skips the definite assignment mark and the type of a
// variable declarator: let x!: T.
func (p *parser) tsDeclaratorType(target Pattern) {
	if _, ok := target.(*Ident); ok && p.tok == Not && !p.nlBefore {
		p.next()
	}
	p.tsTypeAnnotation()
}
