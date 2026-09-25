package syntax

// ---- Expressions ------------------------------------------------------------

// parseExpression parses a comma-separated Expression. noIn disables the
// `in` operator (for-loop initialisers).
func (p *parser) parseExpression(noIn bool) Expr {
	start := p.start
	x := p.parseAssign(noIn)
	if p.tok != Comma {
		return x
	}
	seq := &SeqExpr{Exprs: []Expr{x}}
	for p.eat(Comma) {
		seq.Exprs = append(seq.Exprs, p.parseAssign(noIn))
	}
	seq.Span = Span{start, p.prevEnd}
	return seq
}

// parseAssign parses an AssignmentExpression, including arrow functions and
// yield/await forms.
func (p *parser) parseAssign(noIn bool) Expr {
	p.enter()
	defer p.leave()
	start := p.start
	switch p.tok {
	case KwYield:
		return p.parseYield(noIn)
	case Identifier:
		if p.isIdent("async") {
			if tok, nl, _ := p.peek(); tok == Identifier && !nl {
				// async x => body
				p.next()
				param := p.parseBindingIdent()
				if p.tok != Arrow || p.nlBefore {
					p.unexpected()
				}
				if param.Name == "await" {
					p.fail(param.Pos, "Unexpected reserved word")
				}
				return p.parseArrowBody(start, []Pattern{param}, nil, true, noIn)
			}
		}
		if tok, nl, _ := p.peek(); tok == Arrow && !nl {
			param := p.parseBindingIdent()
			return p.parseArrowBody(start, []Pattern{param}, nil, false, noIn)
		}
	}
	lhs := p.parseConditional(noIn)
	if p.tok.isAssignOp() {
		op := p.tok
		var target Pattern
		if op == Assign {
			target = p.toAssignTarget(lhs, "Invalid left-hand side in assignment")
		} else {
			target = p.toSimpleTarget(lhs, "Invalid left-hand side in assignment")
		}
		p.next()
		value := p.parseAssign(noIn)
		return &AssignExpr{Span{start, p.prevEnd}, op, p.isParenthesized(lhs), target, value}
	}
	if p.tok == Arrow && !p.nlBefore {
		// async (a, b) => body, parsed so far as a call to `async`.
		if call, ok := lhs.(*CallExpr); ok && call.Pos == start && isAsyncIdent(call.Callee) && p.asyncNL != start+1 {
			if pos, expr := firstAwait(call.Args); expr {
				p.fail(pos, "Illegal await-expression in formal parameters of async function")
			} else if pos >= 0 {
				p.fail(pos, "Unexpected reserved word")
			}
			params, rest := p.argsToParams(call.Args)
			return p.parseArrowBody(start, params, rest, true, noIn)
		}
	}
	return lhs
}

// firstAwait returns the position of the first await expression or `await`
// identifier in args, the parameters of an async arrow, or -1, and whether it
// is an expression. The parameters were parsed as call arguments, where await
// may be an identifier; in the arrow they are parsed with await reserved.
// Member names, non-computed keys and nested functions do not count, except
// the parameters of a nested arrow and the name, heritage and computed keys
// of a class, which inherit the reservation.
func firstAwait(args []Expr) (pos int, expr bool) {
	pos = -1
	var visit func(Node) bool
	visit = func(n Node) bool {
		if pos >= 0 {
			return false
		}
		switch n := n.(type) {
		case *Ident:
			if n.Name == "await" {
				pos = n.Pos
			}
		case *AwaitExpr:
			pos, expr = n.Pos, true
		case *Function:
			if n.IsArrow {
				for _, prm := range n.Params {
					Inspect(prm, visit)
				}
				if n.Rest != nil {
					Inspect(n.Rest, visit)
				}
			}
			return false
		case *Class:
			if n.Name != nil {
				visit(n.Name)
			}
			if n.Super != nil {
				Inspect(n.Super, visit)
			}
			for _, m := range n.Members {
				if m.Computed {
					Inspect(m.Key, visit)
				}
			}
			return false
		case *MemberExpr:
			Inspect(n.Object, visit)
			if n.Computed {
				Inspect(n.Prop, visit)
			}
			return false
		case *Property:
			if n.Computed || n.Kind == PropCoverInit {
				Inspect(n.Key, visit)
			}
			Inspect(n.Value, visit)
			return false
		case *PatternProp:
			if n.Computed {
				Inspect(n.Key, visit)
			}
			Inspect(n.Value, visit)
			return false
		}
		return true
	}
	for _, a := range args {
		Inspect(a, visit)
	}
	return pos, expr
}

// isAsyncIdent reports whether x is `async` spelled without escapes.
func isAsyncIdent(x Expr) bool {
	id, ok := x.(*Ident)
	return ok && id.Name == "async" && id.End-id.Pos == len("async")
}

func (p *parser) parseYield(noIn bool) Expr {
	start := p.start
	p.next()
	y := &YieldExpr{}
	if !p.nlBefore {
		if p.eat(Mul) {
			y.Delegate = true
			y.X = p.parseAssign(noIn)
		} else if p.startsExpression() {
			y.X = p.parseAssign(noIn)
		}
	}
	y.Span = Span{start, p.prevEnd}
	return y
}

// startsExpression reports whether the current token can begin an
// AssignmentExpression (used after `yield`).
func (p *parser) startsExpression() bool {
	switch p.tok {
	case RParen, RBrack, RBrace, Comma, Semicolon, Colon, EOF, KwIn, Question:
		return false
	}
	return true
}

func (p *parser) parseConditional(noIn bool) Expr {
	start := p.start
	test := p.parseBinary(0, noIn)
	if !p.eat(Question) {
		return test
	}
	cons := p.parseAssign(false)
	p.expect(Colon)
	alt := p.parseAssign(noIn)
	return &CondExpr{Span{start, p.prevEnd}, test, cons, alt}
}

// parseBinary parses binary operators with precedence climbing.
func (p *parser) parseBinary(minPrec int, noIn bool) Expr {
	start := p.start
	left := p.parseOperand(minPrec, noIn)
	return p.parseBinaryRest(left, start, minPrec, noIn)
}

// parseOperand parses the operand of a binary operator whose right side
// binds tighter than minPrec: a unary expression, or `#x in y` where a
// RelationalExpression may start (the private name is only valid as the
// left operand of `in`).
func (p *parser) parseOperand(minPrec int, noIn bool) Expr {
	if p.tok != PrivateIdent {
		return p.parseUnary()
	}
	start := p.start
	pn := &PrivateName{Span: Span{p.start, p.end}, Name: p.val}
	if tok, _, _ := p.peek(); tok != KwIn || noIn || minPrec >= binaryPrec(KwIn) {
		p.unexpected()
	}
	p.next()
	p.next()
	rstart := p.start
	right := p.parseBinaryRest(p.parseUnary(), rstart, binaryPrec(KwIn), noIn)
	return &BinaryExpr{Span{start, p.prevEnd}, KwIn, pn, right}
}

func (p *parser) parseBinaryRest(left Expr, start, minPrec int, noIn bool) Expr {
	for {
		op := p.tok
		prec := binaryPrec(op)
		if prec == 0 || prec <= minPrec || op == KwIn && noIn {
			return left
		}
		if op == Exp {
			switch left.(type) {
			case *UnaryExpr, *AwaitExpr:
				if pos, _ := left.Range(); pos == start {
					p.fail(pos, "Unary operator used immediately before exponentiation expression. Parenthesis must be used to disambiguate operator precedence")
				}
			}
		}
		p.next()
		rstart := p.start
		nextMin := prec
		if op == Exp {
			nextMin = prec - 1 // right associative
		}
		right := p.parseBinaryRest(p.parseOperand(nextMin, noIn), rstart, nextMin, noIn)
		switch op {
		case LogAnd, LogOr, Nullish:
			p.checkNullishMix(op, left)
			p.checkNullishMix(op, right)
			left = &LogicalExpr{Span: Span{start, p.prevEnd}, Op: op, X: left, Y: right}
		default:
			left = &BinaryExpr{Span{start, p.prevEnd}, op, left, right}
		}
	}
}

// checkNullishMix rejects `a ?? b || c` and friends without parentheses.
func (p *parser) checkNullishMix(op Token, operand Expr) {
	l, ok := operand.(*LogicalExpr)
	if !ok || l.parenthesized {
		return
	}
	if op == Nullish && l.Op != Nullish || op != Nullish && l.Op == Nullish {
		p.fail(l.Pos, "Unexpected token '"+op.String()+"'")
	}
}

func (p *parser) parseUnary() Expr {
	p.enter()
	defer p.leave()
	start := p.start
	switch p.tok {
	case Not, BitNot, Plus, Minus, KwTypeof, KwVoid, KwDelete:
		op := p.tok
		p.next()
		x := p.parseUnary()
		if op == KwDelete {
			if _, ok := x.(*Ident); ok {
				p.fail(start, "Delete of an unqualified identifier in strict mode.")
			}
			if isPrivateMember(x) {
				p.fail(start, "Private fields can not be deleted")
			}
		}
		return &UnaryExpr{Span{start, p.prevEnd}, op, x}
	case Inc, Dec:
		op := p.tok
		p.next()
		x := p.parseUnary()
		target := p.toSimpleTarget(x, "Invalid left-hand side expression in prefix operation")
		return &UpdateExpr{Span{start, p.prevEnd}, op, true, target.(Expr)}
	case KwAwait:
		p.next()
		x := p.parseUnary()
		return &AwaitExpr{Span{start, p.prevEnd}, x}
	}
	x := p.parseLeftHandSide()
	if (p.tok == Inc || p.tok == Dec) && !p.nlBefore {
		op := p.tok
		target := p.toSimpleTarget(x, "Invalid left-hand side expression in postfix operation")
		p.next()
		return &UpdateExpr{Span{start, p.prevEnd}, op, false, target.(Expr)}
	}
	return x
}

// parseLeftHandSide parses new/call/member chains.
func (p *parser) parseLeftHandSide() Expr {
	start := p.start
	var x Expr
	if p.tok == KwNew {
		x = p.parseNew()
	} else {
		x = p.parsePrimary()
	}
	return p.parseCallTail(x, start, true)
}

func (p *parser) parseNew() Expr {
	p.enter()
	defer p.leave()
	start := p.start
	p.next()
	if p.eat(Dot) {
		if !p.isIdent("target") {
			p.unexpected()
		}
		if p.nonArrowDepth == 0 {
			p.fail(start, "new.target expression is not allowed here")
		}
		p.next()
		return &NewTarget{Span: Span{start, p.prevEnd}}
	}
	cstart := p.start
	var callee Expr
	if p.tok == KwNew {
		callee = p.parseNew()
	} else {
		if p.tok == KwImport {
			p.fail(p.start, "Cannot use new with import")
		}
		callee = p.parsePrimary()
	}
	callee = p.parseCallTail(callee, cstart, false)
	if p.tok == QuestionDot {
		p.fail(p.start, "Invalid optional chain from new expression")
	}
	var args []Expr
	if p.tok == LParen {
		args = p.parseArguments()
	}
	return &NewExpr{Span{start, p.prevEnd}, callee, args}
}

// parseCallTail parses `.x`, `[x]`, `(args)`, `?.` links and tagged
// templates after x. When allowCall is false (new-expression callee) the
// chain stops at `(`.
func (p *parser) parseCallTail(x Expr, start int, allowCall bool) Expr {
	inChain := false
	for {
		switch p.tok {
		case Dot:
			p.next()
			x = &MemberExpr{Span: Span{start, 0}, Object: x, Prop: p.parseMemberName()}
			x.(*MemberExpr).End = p.prevEnd
		case QuestionDot:
			if !allowCall {
				return x
			}
			inChain = true
			p.next()
			switch p.tok {
			case LParen:
				args := p.parseArguments()
				x = &CallExpr{Span{start, p.prevEnd}, x, args, true}
			case LBrack:
				p.next()
				idx := p.parseExpression(false)
				p.expect(RBrack)
				x = &MemberExpr{Span{start, p.prevEnd}, x, idx, true, true}
			case Template:
				p.fail(p.start, "Invalid tagged template on optional chain")
			default:
				x = &MemberExpr{Span: Span{start, 0}, Object: x, Prop: p.parseMemberName(), Optional: true}
				x.(*MemberExpr).End = p.prevEnd
			}
		case LBrack:
			p.next()
			idx := p.parseExpression(false)
			p.expect(RBrack)
			x = &MemberExpr{Span{start, p.prevEnd}, x, idx, true, false}
		case LParen:
			if !allowCall {
				return x
			}
			nl := p.nlBefore
			args := p.parseArguments()
			if nl && isAsyncIdent(x) {
				p.asyncNL = start + 1
			}
			x = &CallExpr{Span{start, p.prevEnd}, x, args, false}
		case Template:
			if inChain {
				p.fail(p.start, "Invalid tagged template on optional chain")
			}
			quasi := p.parseTemplate(true)
			x = &TaggedTemplate{Span{start, p.prevEnd}, x, quasi}
		default:
			if inChain {
				return &OptChain{Span{start, p.prevEnd}, x}
			}
			return x
		}
	}
}

// parseMemberName parses the name after `.` or `?.`: any IdentifierName or
// a private name.
func (p *parser) parseMemberName() Expr {
	if p.tok == PrivateIdent {
		pn := &PrivateName{Span: Span{p.start, p.end}, Name: p.val}
		p.next()
		return pn
	}
	return p.parseIdentifierName()
}

func (p *parser) parseArguments() []Expr {
	p.expect(LParen)
	var args []Expr
	for p.tok != RParen {
		if p.tok == Ellipsis {
			start := p.start
			p.next()
			x := p.parseAssign(false)
			args = append(args, &SpreadElem{Span{start, p.prevEnd}, x})
			if p.tok == Comma {
				p.spreadComma = p.start
			}
		} else {
			args = append(args, p.parseAssign(false))
		}
		if p.tok != RParen {
			p.expect(Comma)
		}
	}
	p.next()
	return args
}

func (p *parser) parsePrimary() Expr {
	start := p.start
	switch p.tok {
	case Identifier:
		if p.isIdent("async") {
			if tok, nl, _ := p.peek(); tok == KwFunction && !nl {
				p.next()
				return p.parseFunctionExpr(start, true)
			}
		}
		id := &Ident{Span: Span{start, p.end}, Name: p.val}
		p.next()
		return id
	case KwThis:
		p.next()
		return &ThisExpr{Span: Span{start, p.prevEnd}}
	case KwSuper:
		p.next()
		if p.tok != LParen && p.tok != Dot && p.tok != LBrack {
			p.fail(start, "'super' keyword unexpected here")
		}
		return &SuperExpr{Span{start, p.prevEnd}}
	case Number:
		n := &NumberLit{Span{start, p.end}, p.num}
		p.next()
		return n
	case BigInt:
		n := &BigIntLit{Span{start, p.end}, p.val}
		p.next()
		return n
	case String:
		s := &StringLit{Span{start, p.end}, p.val}
		p.next()
		return s
	case KwTrue, KwFalse:
		b := &BoolLit{Span{start, p.end}, p.tok == KwTrue}
		p.next()
		return b
	case KwNull:
		p.next()
		return &NullLit{Span{start, p.prevEnd}}
	case Div, DivAssign:
		p.rescanRegex()
		p.checkRegex(start)
		r := &RegexLit{Span{start, p.end}, p.val, p.raw}
		p.next()
		return r
	case Template:
		return p.parseTemplate(false)
	case LBrack:
		return p.parseArrayLit()
	case LBrace:
		return p.parseObjectLit()
	case LParen:
		return p.parseParenOrArrow()
	case KwFunction:
		return p.parseFunctionExpr(start, false)
	case KwClass:
		return p.parseClass(false)
	case KwImport:
		p.next()
		if p.eat(Dot) {
			if !p.isIdent("meta") {
				p.unexpected()
			}
			p.next()
			return &ImportMeta{Span{start, p.prevEnd}}
		}
		p.expect(LParen)
		src := p.parseAssign(false)
		p.eat(Comma)
		p.expect(RParen)
		return &ImportCall{Span{start, p.prevEnd}, src}
	}
	p.unexpected()
	return nil
}

// parseTemplate parses a template literal; only a tagged one may contain
// invalid escapes.
func (p *parser) parseTemplate(tagged bool) *TemplateLit {
	start := p.start
	t := &TemplateLit{}
	for {
		if p.badEscape != 0 && !tagged {
			p.failBadEscape()
		}
		cooked := p.val
		if p.badEscape != 0 {
			cooked = InvalidCooked
		}
		t.Quasis = append(t.Quasis, TemplateElement{Span{p.start, p.end}, cooked, p.raw})
		if p.tail {
			p.next()
			break
		}
		p.next()
		t.Exprs = append(t.Exprs, p.parseExpression(false))
		if p.tok != RBrace {
			p.unexpected()
		}
		p.rescanTemplateContinuation()
	}
	t.Span = Span{start, p.prevEnd}
	return t
}

func (p *parser) parseArrayLit() Expr {
	start := p.start
	p.next()
	arr := &ArrayLit{}
	for p.tok != RBrack {
		if p.eat(Comma) {
			arr.Elems = append(arr.Elems, nil)
			continue
		}
		var elem Expr
		if p.tok == Ellipsis {
			sstart := p.start
			p.next()
			x := p.parseAssign(false)
			elem = &SpreadElem{Span{sstart, p.prevEnd}, x}
		} else {
			elem = p.parseAssign(false)
		}
		arr.Elems = append(arr.Elems, elem)
		if p.tok == RBrack {
			break
		}
		p.expect(Comma)
		if _, ok := elem.(*SpreadElem); ok {
			arr.restComma = true
		}
	}
	p.next()
	arr.Span = Span{start, p.prevEnd}
	return arr
}

// parsePropertyKey parses a PropertyName: identifier name, string, number,
// bigint or [computed].
func (p *parser) parsePropertyKey() (key Expr, computed bool) {
	start := p.start
	switch p.tok {
	case String:
		key = &StringLit{Span{start, p.end}, p.val}
	case Number:
		key = &NumberLit{Span{start, p.end}, p.num}
	case BigInt:
		key = &BigIntLit{Span{start, p.end}, p.val}
	case LBrack:
		p.next()
		key = p.parseAssign(false)
		p.expect(RBrack)
		return key, true
	default:
		return p.parseIdentifierName(), false
	}
	p.next()
	return key, false
}

func (p *parser) parseObjectLit() Expr {
	start := p.start
	p.next()
	obj := &ObjectLit{}
	sawProto := false
	for p.tok != RBrace {
		prop := &Property{Span: Span{p.start, 0}}
		if p.eat(Ellipsis) {
			prop.Kind = PropSpread
			prop.Value = p.parseAssign(false)
		} else {
			p.parsePropertyBody(prop)
			if prop.Kind == PropInit && !prop.Computed && isProtoKey(prop.Key) {
				if sawProto && obj.dupProto == 0 {
					obj.dupProto = prop.Pos
				}
				sawProto = true
			}
		}
		prop.End = p.prevEnd
		obj.Props = append(obj.Props, prop)
		if p.tok != RBrace {
			p.expect(Comma)
			if prop.Kind == PropSpread {
				obj.restComma = true
			}
		}
	}
	p.next()
	obj.Span = Span{start, p.prevEnd}
	return obj
}

// parsePropertyBody parses one non-spread object literal entry into prop.
func (p *parser) parsePropertyBody(prop *Property) {
	kind := FuncMethod
	isAsync, isGen := false, false
	if p.eat(Mul) {
		isGen = true
	} else if p.tok == Identifier && !p.escaped && (p.val == "async" || p.val == "get" || p.val == "set") {
		if tok, nl, _ := p.peek(); tok != Comma && tok != Colon && tok != LParen && tok != RBrace && tok != Assign && !(p.val == "async" && nl) {
			switch p.val {
			case "async":
				isAsync = true
			case "get":
				kind = FuncGetter
			case "set":
				kind = FuncSetter
			}
			p.next()
			if isAsync && p.eat(Mul) {
				isGen = true
			}
		}
	}
	keyTok := p.tok
	prop.Key, prop.Computed = p.parsePropertyKey()
	if p.tok == LParen {
		fn := p.parseMethod(prop.Pos, prop.Key, prop.Computed, kind, isAsync, isGen)
		prop.Value = fn
		switch kind {
		case FuncGetter:
			prop.Kind = PropGetter
		case FuncSetter:
			prop.Kind = PropSetter
		default:
			prop.Kind = PropMethod
		}
		return
	}
	if isAsync || isGen || kind != FuncMethod {
		p.unexpected()
	}
	if p.eat(Colon) {
		prop.Kind = PropInit
		prop.Value = p.parseAssign(false)
		return
	}
	// Shorthand property or cover-initialized name.
	if keyTok != Identifier {
		_, keyEnd := prop.Key.Range()
		p.unexpectedAt(keyTok, prop.Pos, p.src[prop.Pos:keyEnd])
	}
	key := prop.Key.(*Ident)
	if p.eat(Assign) {
		prop.Kind = PropCoverInit
		prop.Value = p.parseAssign(false)
		return
	}
	prop.Kind = PropShorthand
	prop.Value = &Ident{Span: key.Span, Name: key.Name}
}

func isProtoKey(key Expr) bool {
	switch k := key.(type) {
	case *Ident:
		return k.Name == "__proto__"
	case *StringLit:
		return k.Value == "__proto__"
	}
	return false
}

// parseParenOrArrow parses `( ... )`: either a parenthesised expression or
// an arrow function parameter list, decided by the token after `)`.
func (p *parser) parseParenOrArrow() Expr {
	start := p.start
	heritage := p.heritage
	p.heritage = false
	p.next()
	if p.tok == RParen {
		rparen := p.start
		p.next()
		if p.tok != Arrow || p.nlBefore {
			p.fail(rparen, "Unexpected token ')'")
		}
		if heritage {
			p.unexpected()
		}
		return p.parseArrowBody(start, nil, nil, false, false)
	}
	var items []Expr
	var rest Pattern
	trailingComma := false
	for {
		if p.tok == Ellipsis {
			p.next()
			rest = p.parseBindingTarget()
			if p.tok != RParen {
				p.fail(p.start, "Rest parameter must be last formal parameter")
			}
			break
		}
		items = append(items, p.parseAssign(false))
		if !p.eat(Comma) {
			break
		}
		if p.tok == RParen {
			trailingComma = true
			break
		}
	}
	rparen := p.start
	p.expect(RParen)
	if p.tok == Arrow && !p.nlBefore {
		if heritage {
			p.unexpected()
		}
		params := make([]Pattern, len(items))
		for i, item := range items {
			params[i] = p.toParam(item)
		}
		return p.parseArrowBody(start, params, rest, false, false)
	}
	if rest != nil || trailingComma {
		p.fail(rparen, "Unexpected token ')'")
	}
	if len(items) == 1 {
		p.markParenthesized(items[0])
		return items[0]
	}
	seq := &SeqExpr{Span{start, p.prevEnd}, items}
	return seq
}

// markParenthesized records that x was wrapped in parentheses, which
// disables its use as a destructuring pattern and settles ??/|| mixing.
// Identifiers and assignments stay valid assignment targets, so they are kept
// in a side set rather than growing their nodes.
func (p *parser) markParenthesized(x Expr) {
	switch x := x.(type) {
	case *ObjectLit:
		x.parenthesized = true
	case *ArrayLit:
		x.parenthesized = true
	case *LogicalExpr:
		x.parenthesized = true
	case *Ident, *AssignExpr:
		if p.parens == nil {
			p.parens = make(map[Expr]struct{})
		}
		p.parens[x] = struct{}{}
	}
}

// isParenthesized reports whether markParenthesized saw x.
func (p *parser) isParenthesized(x Expr) bool {
	_, ok := p.parens[x]
	return ok
}

// ---- Assignment targets and cover grammar -----------------------------------

// toSimpleTarget checks that x is an identifier or member expression.
func (p *parser) toSimpleTarget(x Expr, msg string) Pattern {
	switch t := x.(type) {
	case *Ident:
		p.checkBindingName(t)
		return t
	case *MemberExpr:
		return t
	}
	pos, _ := x.Range()
	p.fail(pos, msg)
	return nil
}

// toAssignTarget converts the left operand of `=` (or a for-in/of head) into
// an assignment target, applying the destructuring cover grammar.
func (p *parser) toAssignTarget(x Expr, msg string) Pattern {
	switch t := x.(type) {
	case *Ident, *MemberExpr:
		return p.toSimpleTarget(x, msg)
	case *ObjectLit:
		if !t.parenthesized {
			return p.objectToPattern(t, false)
		}
	case *ArrayLit:
		if !t.parenthesized {
			return p.arrayToPattern(t, false)
		}
	}
	pos, _ := x.Range()
	p.fail(pos, msg)
	return nil
}

// toParam converts an arrow parameter parsed as an expression into a
// binding pattern.
func (p *parser) toParam(x Expr) Pattern {
	if p.isParenthesized(x) {
		pos, _ := x.Range()
		p.fail(pos, "Invalid destructuring assignment target")
	}
	switch t := x.(type) {
	case *Ident:
		p.checkBindingName(t)
		return t
	case *AssignExpr:
		if t.Op == Assign {
			return &AssignPattern{t.Span, p.toBindingPattern(t.Target), t.Value}
		}
	case *ObjectLit:
		if !t.parenthesized {
			return p.objectToPattern(t, true)
		}
	case *ArrayLit:
		if !t.parenthesized {
			return p.arrayToPattern(t, true)
		}
	}
	pos, _ := x.Range()
	p.fail(pos, "Malformed arrow function parameter list")
	return nil
}

// argsToParams converts the arguments of the `async(...)` cover call into
// arrow parameters.
func (p *parser) argsToParams(args []Expr) (params []Pattern, rest Pattern) {
	for i, a := range args {
		if s, ok := a.(*SpreadElem); ok {
			if i != len(args)-1 || p.spreadComma >= s.End {
				p.fail(s.Pos, "Rest parameter must be last formal parameter")
			}
			rest = p.toBindingPattern(p.toParam(s.X))
			if _, ok := rest.(*AssignPattern); ok {
				p.fail(s.Pos, "Rest parameter may not have a default initializer")
			}
			continue
		}
		params = append(params, p.toParam(a))
	}
	return params, rest
}

// toBindingPattern re-validates an assignment pattern for use as a binding
// pattern (arrow parameters): member expressions are not allowed.
func (p *parser) toBindingPattern(pat Pattern) Pattern {
	switch t := pat.(type) {
	case *Ident:
		if p.isParenthesized(t) {
			p.fail(t.Pos, "Invalid destructuring assignment target")
		}
		p.checkBindingName(t)
	case *MemberExpr:
		p.fail(t.Pos, "Invalid destructuring assignment target")
	case *AssignPattern:
		p.toBindingPattern(t.Target)
	case *ObjectPattern:
		for _, prop := range t.Props {
			p.toBindingPattern(prop.Value)
		}
		if t.Rest != nil {
			p.toBindingPattern(t.Rest)
		}
	case *ArrayPattern:
		for _, e := range t.Elems {
			if e != nil {
				p.toBindingPattern(e)
			}
		}
		if t.Rest != nil {
			p.toBindingPattern(t.Rest)
		}
	}
	return pat
}

// elemToPattern converts a destructuring element expression to a pattern.
func (p *parser) elemToPattern(x Expr, binding bool) Pattern {
	switch t := x.(type) {
	case *AssignExpr:
		if t.Op != Assign || p.isParenthesized(t) {
			break
		}
		target := t.Target
		if binding {
			target = p.toBindingPattern(target)
		}
		return &AssignPattern{t.Span, target, t.Value}
	case *Ident:
		if binding && p.isParenthesized(t) {
			break
		}
		p.checkBindingName(t)
		return t
	case *MemberExpr:
		if !binding {
			return t
		}
	case *ObjectLit:
		if !t.parenthesized {
			return p.objectToPattern(t, binding)
		}
	case *ArrayLit:
		if !t.parenthesized {
			return p.arrayToPattern(t, binding)
		}
	}
	pos, _ := x.Range()
	p.fail(pos, "Invalid destructuring assignment target")
	return nil
}

func (p *parser) objectToPattern(obj *ObjectLit, binding bool) *ObjectPattern {
	pat := &ObjectPattern{Span: obj.Span}
	for i, prop := range obj.Props {
		switch prop.Kind {
		case PropSpread:
			if i != len(obj.Props)-1 || obj.restComma {
				p.fail(prop.Pos, "Rest element must be last element")
			}
			rest := p.elemToPattern(prop.Value, binding)
			switch rest.(type) {
			case *Ident, *MemberExpr:
			default:
				p.fail(prop.Pos, "`...` must be followed by an assignable reference in assignment contexts")
			}
			pat.Rest = rest
		case PropInit:
			pat.Props = append(pat.Props, &PatternProp{prop.Span, prop.Key, prop.Computed, p.elemToPattern(prop.Value, binding)})
		case PropShorthand:
			id := prop.Value.(*Ident)
			p.checkBindingName(id)
			pat.Props = append(pat.Props, &PatternProp{prop.Span, prop.Key, false, id})
		case PropCoverInit:
			key := prop.Key.(*Ident)
			id := &Ident{Span: key.Span, Name: key.Name}
			p.checkBindingName(id)
			def := &AssignPattern{prop.Span, id, prop.Value}
			pat.Props = append(pat.Props, &PatternProp{prop.Span, prop.Key, false, def})
		default:
			p.fail(prop.Pos, "Invalid destructuring assignment target")
		}
	}
	return pat
}

func (p *parser) arrayToPattern(arr *ArrayLit, binding bool) *ArrayPattern {
	pat := &ArrayPattern{Span: arr.Span}
	for i, elem := range arr.Elems {
		if elem == nil {
			pat.Elems = append(pat.Elems, nil)
			continue
		}
		if s, ok := elem.(*SpreadElem); ok {
			if i != len(arr.Elems)-1 || arr.restComma {
				p.fail(s.Pos, "Rest element must be last element")
			}
			rest := p.elemToPattern(s.X, binding)
			if _, ok := rest.(*AssignPattern); ok {
				p.fail(s.Pos, "Rest element may not have a default initializer")
			}
			pat.Rest = rest
			continue
		}
		pat.Elems = append(pat.Elems, p.elemToPattern(elem, binding))
	}
	return pat
}

// ---- Functions --------------------------------------------------------------

// funcContext is the statement context saved across a function boundary.
type funcContext struct {
	inLoop, inSwitch, labelBase, pendingLabels int
	awaitKw                                    bool
}

func (p *parser) enterFunction(fn *Function) funcContext {
	p.enter()
	saved := funcContext{p.inLoop, p.inSwitch, p.labelBase, p.pendingLabels, p.awaitKw}
	p.setAwaitKw(p.isModule || fn.IsAsync)
	p.inLoop, p.inSwitch, p.pendingLabels = 0, 0, 0
	p.labelBase = len(p.labels)
	p.funcDepth++
	if !fn.IsArrow {
		p.nonArrowDepth++
	}
	return saved
}

func (p *parser) leaveFunction(fn *Function, saved funcContext) {
	p.leave()
	p.funcDepth--
	if !fn.IsArrow {
		p.nonArrowDepth--
	}
	p.inLoop, p.inSwitch, p.labelBase, p.pendingLabels = saved.inLoop, saved.inSwitch, saved.labelBase, saved.pendingLabels
	p.setAwaitKw(saved.awaitKw)
}

// parseFunctionDecl parses `function name(params) { body }` at start (which
// may be the position of a preceding `async`).
func (p *parser) parseFunctionDecl(start int, isAsync, requireName bool) *FuncDecl {
	fn := &Function{IsAsync: isAsync, IsStrict: true}
	p.expect(KwFunction)
	fn.IsGenerator = p.eat(Mul)
	if p.tok == Identifier || p.tok == KwLet {
		fn.Name = p.parseBindingIdent()
	} else if requireName {
		p.unexpected()
	}
	p.parseFunctionRest(fn, start)
	return &FuncDecl{fn.Span, fn}
}

func (p *parser) parseFunctionExpr(start int, isAsync bool) *Function {
	fn := &Function{IsAsync: isAsync, IsStrict: true}
	p.expect(KwFunction)
	fn.IsGenerator = p.eat(Mul)
	// The name is bound inside the function: await is reserved in it only
	// as in the body (a static block's await is not).
	outer := p.awaitKw
	p.setAwaitKw(p.isModule || isAsync)
	if p.tok == Identifier || p.tok == KwLet {
		fn.Name = p.parseBindingIdent()
	}
	p.setAwaitKw(outer)
	p.parseFunctionRest(fn, start)
	return fn
}

// parseMethod parses the parameter list and body of an object or class
// method whose key has just been parsed. A computed key names the method
// at run time only.
func (p *parser) parseMethod(start int, key Expr, computed bool, kind FuncKind, isAsync, isGen bool) *Function {
	fn := &Function{Kind: kind, IsAsync: isAsync, IsGenerator: isGen, IsStrict: true}
	if id, ok := key.(*Ident); ok && !computed {
		fn.Name = &Ident{Span: id.Span, Name: id.Name}
	}
	p.parseFunctionRest(fn, start)
	return fn
}

// parseFunctionRest parses `(params) { body }` into fn.
func (p *parser) parseFunctionRest(fn *Function, start int) {
	saved := p.enterFunction(fn)
	lparen := p.start
	p.parseParams(fn)
	switch {
	case fn.Kind == FuncGetter && (len(fn.Params) != 0 || fn.Rest != nil):
		p.fail(lparen, "Getter must not have any formal parameters.")
	case fn.Kind == FuncSetter && fn.Rest != nil:
		pos, _ := fn.Rest.Range()
		p.fail(pos, "Setter function argument must not be a rest parameter")
	case fn.Kind == FuncSetter && len(fn.Params) != 1:
		p.fail(lparen, "Setter must have exactly one formal parameter.")
	}
	fn.Body = p.parseBlock()
	p.checkUseStrict(fn)
	p.leaveFunction(fn, saved)
	fn.Span = Span{start, p.prevEnd}
}

// checkUseStrict rejects a "use strict" directive in the body of a function
// whose parameter list is not simple. The directive prologue is the leading
// run of statements that are a bare string literal; a directive is matched on
// its source text, so escapes or parentheses make it an ordinary string.
func (p *parser) checkUseStrict(fn *Function) {
	if fn.HasSimpleParams {
		return
	}
	for _, s := range fn.Body.Body {
		es, ok := s.(*ExprStmt)
		if !ok {
			return
		}
		str, ok := es.X.(*StringLit)
		if !ok || str.Pos != es.Pos {
			return
		}
		if p.src[str.Pos+1:str.End-1] == "use strict" {
			p.fail(str.Pos, "Illegal 'use strict' directive in function with non-simple parameter list")
		}
	}
}

func (p *parser) parseParams(fn *Function) {
	p.expect(LParen)
	for p.tok != RParen {
		if p.eat(Ellipsis) {
			fn.Rest = p.parseBindingTarget()
			if p.tok == Assign {
				p.fail(p.start, "Rest parameter may not have a default initializer")
			}
			if p.tok != RParen {
				p.fail(p.start, "Rest parameter must be last formal parameter")
			}
			break
		}
		fn.Params = append(fn.Params, p.parseBindingElement())
		if p.tok != RParen {
			p.expect(Comma)
		}
	}
	p.next()
	p.finishParams(fn)
}

// finishParams computes the parameter summary fields and rejects duplicate
// parameter names.
func (p *parser) finishParams(fn *Function) {
	fn.ParamCount = len(fn.Params)
	fn.HasRest = fn.Rest != nil
	fn.HasSimpleParams = !fn.HasRest
	fn.Length = len(fn.Params)
	for i, param := range fn.Params {
		if !isSimpleParam(param) {
			fn.HasSimpleParams = false
			if fn.Length == len(fn.Params) {
				if _, ok := param.(*AssignPattern); ok {
					fn.Length = i
				}
			}
		}
	}
	names := make([]*Ident, 0, len(fn.Params)+1)
	for _, param := range fn.Params {
		names = boundNames(param, names)
	}
	if fn.Rest != nil {
		names = boundNames(fn.Rest, names)
	}
	for i, id := range names {
		for _, prev := range names[:i] {
			if prev.Name == id.Name {
				p.fail(id.Pos, "Duplicate parameter name not allowed in this context")
			}
		}
	}
}

// parseArrowBody parses `=> body` for an arrow whose parameters are known.
func (p *parser) parseArrowBody(start int, params []Pattern, rest Pattern, isAsync, noIn bool) Expr {
	p.expect(Arrow)
	fn := &Function{Span: Span{start, 0}, Params: params, Rest: rest, Kind: FuncArrow, IsArrow: true, IsAsync: isAsync, IsStrict: true}
	p.finishParams(fn)
	saved := p.enterFunction(fn)
	if p.tok == LBrace {
		fn.Body = p.parseBlock()
		p.checkUseStrict(fn)
	} else {
		fn.ExprBody = p.parseAssign(noIn)
	}
	p.leaveFunction(fn, saved)
	fn.End = p.prevEnd
	return fn
}

// ---- Classes ------------------------------------------------------------------

// isPrivateMember reports whether x is `a.#b` or an optional chain ending
// in one (the operand of a rejected `delete`).
func isPrivateMember(x Expr) bool {
	if oc, ok := x.(*OptChain); ok {
		x = oc.X
	}
	m, ok := x.(*MemberExpr)
	if !ok {
		return false
	}
	_, ok = m.Prop.(*PrivateName)
	return ok
}

func (p *parser) parseClassDecl(requireName bool) *ClassDecl {
	c := p.parseClass(requireName)
	return &ClassDecl{c.Span, c}
}

// privateDecl records how a private name was declared in a class body, for
// the duplicate check (a getter/setter pair of the same placement is the
// only allowed repetition).
type privateDecl struct {
	kind   ClassMemberKind
	static bool
	paired bool
}

// parseClass parses a class declaration or expression. The early errors
// that need only the element list (constructor, `prototype` and private
// name declarations) are applied here; the rest are scope rules
// (resolver.class).
func (p *parser) parseClass(requireName bool) *Class {
	p.enter()
	defer p.leave()
	start := p.start
	p.expect(KwClass)
	c := &Class{}
	if p.tok == Identifier || p.tok == KwLet {
		c.Name = p.parseBindingIdent()
	} else if requireName {
		p.unexpected()
	}
	if p.eat(KwExtends) {
		p.heritage = p.tok == LParen // an arrow function is not a LeftHandSideExpression
		c.Super = p.parseLeftHandSide()
	}
	p.expect(LBrace)
	var privs map[string]*privateDecl
	for p.tok != RBrace {
		if p.eat(Semicolon) {
			continue
		}
		m := p.parseClassMember()
		if fn, ok := m.Value.(*Function); ok && fn.Kind == FuncClassConstructor {
			if c.Ctor != nil {
				p.fail(m.Pos, "A class may only have one constructor")
			}
			c.Ctor = fn
		}
		if pn, ok := m.Key.(*PrivateName); ok {
			if privs == nil {
				privs = make(map[string]*privateDecl)
			}
			prev := privs[pn.Name]
			switch {
			case prev == nil:
				privs[pn.Name] = &privateDecl{kind: m.Kind, static: m.Static}
			case !prev.paired && prev.static == m.Static &&
				(prev.kind == ClassGetter && m.Kind == ClassSetter || prev.kind == ClassSetter && m.Kind == ClassGetter):
				prev.paired = true
			default:
				p.fail(pn.Pos, "Identifier '#"+pn.Name+"' has already been declared")
			}
		}
		c.Members = append(c.Members, m)
	}
	p.next()
	c.Span = Span{start, p.prevEnd}
	return c
}

// staticPropName returns the name of a non-computed identifier or string
// key (the forms the `constructor` and `prototype` early errors match).
func staticPropName(key Expr) string {
	switch k := key.(type) {
	case *Ident:
		return k.Name
	case *StringLit:
		return k.Value
	}
	return ""
}

func (p *parser) parseClassMember() *ClassMember {
	m := &ClassMember{Span: Span{p.start, 0}}
	if p.tok == KwStatic {
		if tok, _, _ := p.peek(); tok != LParen && tok != Assign && tok != Semicolon && tok != RBrace {
			m.Static = true
			p.next()
			if p.tok == LBrace {
				m.Kind = ClassStaticBlock
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
		}
	}
	start := p.start // a method's source text starts after `static`
	kind := FuncMethod
	isAsync, isGen := false, false
	if p.eat(Mul) {
		isGen = true
	} else if p.tok == Identifier && !p.escaped && (p.val == "async" || p.val == "get" || p.val == "set") {
		if tok, nl, _ := p.peek(); tok != LParen && tok != Assign && tok != Semicolon && tok != RBrace && !(p.val == "async" && nl) && (tok != Mul || p.val == "async") {
			switch p.val {
			case "async":
				isAsync = true
			case "get":
				kind = FuncGetter
			case "set":
				kind = FuncSetter
			}
			p.next()
			if isAsync && p.eat(Mul) {
				isGen = true
			}
		}
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
	if p.tok == LParen {
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
		m.Value = p.parseMethod(start, m.Key, m.Computed, kind, isAsync, isGen)
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
	m.Kind = ClassField
	if p.eat(Assign) {
		fn := &Function{IsStrict: true}
		saved := p.enterFunction(fn)
		m.Value = p.parseAssign(false)
		p.leaveFunction(fn, saved)
	}
	p.semicolon()
	m.End = p.prevEnd
	return m
}
