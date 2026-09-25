package syntax

import (
	"strconv"
	"strings"
)

// Dump renders a node as an S-expression for golden tests and debugging.
// Statements are placed on their own lines and indented; expressions are
// printed inline.
func Dump(n Node) string {
	var d dumper
	d.node(n, 0)
	return strings.TrimRight(d.sb.String(), "\n")
}

type dumper struct {
	sb    strings.Builder
	level int // nesting level of the statement being printed
}

func (d *dumper) w(s string) { d.sb.WriteString(s) }

func (d *dumper) indent(level int) {
	for range level {
		d.w("  ")
	}
}

func (d *dumper) node(n Node, level int) {
	switch n := n.(type) {
	case *Module:
		d.w("(module")
		d.stmts(n.Body, level+1)
		d.w(")\n")
	case *Script:
		d.w("(script")
		d.stmts(n.Body, level+1)
		d.w(")\n")
	case Stmt:
		d.stmt(n, level)
	case Expr:
		d.expr(n)
	case Pattern:
		d.pattern(n)
	}
}

func (d *dumper) stmts(list []Stmt, level int) {
	for _, s := range list {
		d.w("\n")
		d.indent(level)
		d.stmt(s, level)
	}
}

func (d *dumper) block(b *BlockStmt, level int) {
	d.w("(block")
	d.stmts(b.Body, level+1)
	d.w(")")
}

func (d *dumper) stmt(s Stmt, level int) {
	saved := d.level
	d.level = level
	defer func() { d.level = saved }()
	switch s := s.(type) {
	case *VarDecl:
		d.w("(" + s.Kind.String())
		for _, decl := range s.Decls {
			d.w(" (")
			d.pattern(decl.Target)
			if decl.Init != nil {
				d.w(" ")
				d.expr(decl.Init)
			}
			d.w(")")
		}
		d.w(")")
	case *FuncDecl:
		d.function(s.Func)
	case *ClassDecl:
		d.class(s.Class)
	case *ExprStmt:
		d.expr(s.X)
	case *BlockStmt:
		d.block(s, level)
	case *EmptyStmt:
		d.w("(empty)")
	case *DebuggerStmt:
		d.w("(debugger)")
	case *IfStmt:
		d.w("(if ")
		d.expr(s.Cond)
		d.w(" ")
		d.stmt(s.Then, level+1)
		if s.Else != nil {
			d.w(" ")
			d.stmt(s.Else, level+1)
		}
		d.w(")")
	case *ForStmt:
		d.w("(for ")
		switch init := s.Init.(type) {
		case nil:
			d.w("_")
		case *VarDecl:
			d.stmt(init, level)
		case Expr:
			d.expr(init)
		}
		d.w(" ")
		d.optExpr(s.Cond)
		d.w(" ")
		d.optExpr(s.Update)
		d.w(" ")
		d.stmt(s.Body, level+1)
		d.w(")")
	case *ForInOfStmt:
		if s.Of {
			d.w("(for-of ")
		} else {
			d.w("(for-in ")
		}
		switch left := s.Left.(type) {
		case *VarDecl:
			d.stmt(left, level)
		case Pattern:
			d.pattern(left)
		}
		d.w(" ")
		d.expr(s.Right)
		d.w(" ")
		d.stmt(s.Body, level+1)
		d.w(")")
	case *WhileStmt:
		d.w("(while ")
		d.expr(s.Cond)
		d.w(" ")
		d.stmt(s.Body, level+1)
		d.w(")")
	case *DoWhileStmt:
		d.w("(do ")
		d.stmt(s.Body, level+1)
		d.w(" ")
		d.expr(s.Cond)
		d.w(")")
	case *ReturnStmt:
		d.w("(return")
		if s.Result != nil {
			d.w(" ")
			d.expr(s.Result)
		}
		d.w(")")
	case *ThrowStmt:
		d.w("(throw ")
		d.expr(s.X)
		d.w(")")
	case *BreakStmt:
		d.w("(break")
		if s.Label != nil {
			d.w(" " + s.Label.Name)
		}
		d.w(")")
	case *ContinueStmt:
		d.w("(continue")
		if s.Label != nil {
			d.w(" " + s.Label.Name)
		}
		d.w(")")
	case *LabeledStmt:
		d.w("(label " + s.Label.Name + " ")
		d.stmt(s.Body, level+1)
		d.w(")")
	case *SwitchStmt:
		d.w("(switch ")
		d.expr(s.Disc)
		for _, c := range s.Cases {
			d.w("\n")
			d.indent(level + 1)
			if c.Test == nil {
				d.w("(default")
			} else {
				d.w("(case ")
				d.expr(c.Test)
			}
			d.stmts(c.Body, level+2)
			d.w(")")
		}
		d.w(")")
	case *TryStmt:
		d.w("(try ")
		d.block(s.Block, level+1)
		if s.Handler != nil {
			d.w(" (catch")
			if s.Param != nil {
				d.w(" ")
				d.pattern(s.Param)
			}
			d.w(" ")
			d.block(s.Handler, level+1)
			d.w(")")
		}
		if s.Finalizer != nil {
			d.w(" (finally ")
			d.block(s.Finalizer, level+1)
			d.w(")")
		}
		d.w(")")
	case *ExportDecl:
		d.w("(export ")
		d.stmt(s.Decl, level)
		d.w(")")
	case *ExportNamed:
		d.w("(export {")
		for i, spec := range s.Specs {
			if i > 0 {
				d.w(", ")
			}
			d.w(spec.Local.Name)
			if spec.Exported.Name != spec.Local.Name {
				d.w(" as " + spec.Exported.Name)
			}
		}
		d.w("}")
		if s.Source != nil {
			d.w(" from " + strconv.Quote(s.Source.Value))
		}
		d.w(")")
	case *ExportDefault:
		d.w("(export-default ")
		switch decl := s.Decl.(type) {
		case Stmt:
			d.stmt(decl, level)
		case Expr:
			d.expr(decl)
		}
		d.w(")")
	case *ExportAll:
		d.w("(export *")
		if s.As != nil {
			d.w(" as " + s.As.Name)
		}
		d.w(" from " + strconv.Quote(s.Source.Value) + ")")
	case *ImportDecl:
		d.w("(import")
		if s.Default != nil {
			d.w(" " + s.Default.Name)
		}
		if s.Namespace != nil {
			d.w(" * as " + s.Namespace.Name)
		}
		if s.Specs != nil {
			d.w(" {")
			for i, spec := range s.Specs {
				if i > 0 {
					d.w(", ")
				}
				d.w(spec.Imported.Name)
				if spec.Local != spec.Imported {
					d.w(" as " + spec.Local.Name)
				}
			}
			d.w("}")
		}
		d.w(" from " + strconv.Quote(s.Source.Value) + ")")
	}
}

func (d *dumper) optExpr(e Expr) {
	if e == nil {
		d.w("_")
		return
	}
	d.expr(e)
}

func (d *dumper) exprs(list []Expr) {
	for _, e := range list {
		d.w(" ")
		if e == nil {
			d.w("<hole>")
			continue
		}
		d.expr(e)
	}
}

func (d *dumper) params(fn *Function) {
	d.w("(")
	for i, p := range fn.Params {
		if i > 0 {
			d.w(" ")
		}
		d.pattern(p)
	}
	if fn.Rest != nil {
		if len(fn.Params) > 0 {
			d.w(" ")
		}
		d.w("...")
		d.pattern(fn.Rest)
	}
	d.w(")")
}

func (d *dumper) function(fn *Function) {
	level := d.level
	d.w("(")
	if fn.IsAsync {
		d.w("async ")
	}
	switch {
	case fn.IsArrow:
		d.w("=>")
	case fn.Kind == FuncGetter:
		d.w("get")
	case fn.Kind == FuncSetter:
		d.w("set")
	case fn.Kind == FuncMethod || fn.Kind == FuncClassConstructor:
		d.w("method")
	default:
		d.w("function")
	}
	if fn.IsGenerator {
		d.w("*")
	}
	if fn.Name != nil {
		d.w(" " + fn.Name.Name)
	}
	d.w(" ")
	d.params(fn)
	if fn.ExprBody != nil {
		d.w(" ")
		d.expr(fn.ExprBody)
	} else {
		d.stmts(fn.Body.Body, level+1)
	}
	d.w(")")
}

func (d *dumper) class(c *Class) {
	level := d.level
	d.w("(class")
	if c.Name != nil {
		d.w(" " + c.Name.Name)
	}
	if c.Super != nil {
		d.w(" (extends ")
		d.expr(c.Super)
		d.w(")")
	}
	for _, m := range c.Members {
		d.w("\n")
		d.indent(level + 1)
		d.level = level + 1
		if m.Kind == ClassMethod || m.Kind == ClassGetter || m.Kind == ClassSetter {
			if m.Static {
				d.w("(static ")
				d.function(m.Value.(*Function))
				d.w(")")
			} else {
				d.function(m.Value.(*Function))
			}
			continue
		}
		d.w("(")
		if m.Static {
			d.w("static ")
		}
		switch m.Kind {
		case ClassStaticBlock:
			d.w("block")
			d.stmts(m.Body.Body, level+2)
		case ClassField:
			d.w("field ")
			d.key(m.Key, m.Computed)
			if m.Value != nil {
				d.w(" ")
				d.expr(m.Value)
			}
		}
		d.w(")")
	}
	d.level = level
	d.w(")")
}

func (d *dumper) key(k Expr, computed bool) {
	if computed {
		d.w("[")
		d.expr(k)
		d.w("]")
		return
	}
	switch k := k.(type) {
	case *Ident:
		d.w(k.Name)
	case *PrivateName:
		d.w("#" + k.Name)
	default:
		d.expr(k)
	}
}

func (d *dumper) expr(e Expr) {
	switch e := e.(type) {
	case *Ident:
		d.w(e.Name)
	case *ThisExpr:
		d.w("this")
	case *SuperExpr:
		d.w("super")
	case *NewTarget:
		d.w("new.target")
	case *ImportMeta:
		d.w("import.meta")
	case *ImportCall:
		d.w("(import ")
		d.expr(e.Source)
		d.w(")")
	case *NumberLit:
		d.w(strconv.FormatFloat(e.Value, 'g', -1, 64))
	case *BigIntLit:
		d.w(e.Digits + "n")
	case *StringLit:
		d.w(strconv.Quote(e.Value))
	case *BoolLit:
		d.w(strconv.FormatBool(e.Value))
	case *NullLit:
		d.w("null")
	case *RegexLit:
		d.w("/" + e.Pattern + "/" + e.Flags)
	case *TemplateLit:
		d.w("(template")
		for i, q := range e.Quasis {
			if q.Cooked == InvalidCooked {
				d.w(" undefined")
			} else {
				d.w(" " + strconv.Quote(q.Cooked))
			}
			if i < len(e.Exprs) {
				d.w(" ")
				d.expr(e.Exprs[i])
			}
		}
		d.w(")")
	case *TaggedTemplate:
		d.w("(tagged ")
		d.expr(e.Tag)
		d.w(" ")
		d.expr(e.Quasi)
		d.w(")")
	case *ArrayLit:
		d.w("(array")
		d.exprs(e.Elems)
		d.w(")")
	case *ObjectLit:
		d.w("(object")
		for _, p := range e.Props {
			d.w(" ")
			d.property(p)
		}
		d.w(")")
	case *Function:
		d.function(e)
	case *Class:
		d.class(e)
	case *UnaryExpr:
		d.w("(" + e.Op.String() + " ")
		d.expr(e.X)
		d.w(")")
	case *UpdateExpr:
		if e.Prefix {
			d.w("(pre" + e.Op.String() + " ")
		} else {
			d.w("(post" + e.Op.String() + " ")
		}
		d.expr(e.X)
		d.w(")")
	case *BinaryExpr:
		d.w("(" + e.Op.String() + " ")
		d.expr(e.X)
		d.w(" ")
		d.expr(e.Y)
		d.w(")")
	case *LogicalExpr:
		d.w("(" + e.Op.String() + " ")
		d.expr(e.X)
		d.w(" ")
		d.expr(e.Y)
		d.w(")")
	case *AssignExpr:
		d.w("(" + e.Op.String() + " ")
		d.pattern(e.Target)
		d.w(" ")
		d.expr(e.Value)
		d.w(")")
	case *CondExpr:
		d.w("(? ")
		d.expr(e.Test)
		d.w(" ")
		d.expr(e.Cons)
		d.w(" ")
		d.expr(e.Alt)
		d.w(")")
	case *SeqExpr:
		d.w("(seq")
		d.exprs(e.Exprs)
		d.w(")")
	case *CallExpr:
		if e.Optional {
			d.w("(?call ")
		} else {
			d.w("(call ")
		}
		d.expr(e.Callee)
		d.exprs(e.Args)
		d.w(")")
	case *NewExpr:
		d.w("(new ")
		d.expr(e.Callee)
		d.exprs(e.Args)
		d.w(")")
	case *MemberExpr:
		switch {
		case e.Optional && e.Computed:
			d.w("(?[] ")
		case e.Optional:
			d.w("(?. ")
		case e.Computed:
			d.w("([] ")
		default:
			d.w("(. ")
		}
		d.expr(e.Object)
		d.w(" ")
		d.key(e.Prop, e.Computed)
		d.w(")")
	case *PrivateName:
		d.w("#" + e.Name)
	case *OptChain:
		d.w("(chain ")
		d.expr(e.X)
		d.w(")")
	case *SpreadElem:
		d.w("(... ")
		d.expr(e.X)
		d.w(")")
	case *YieldExpr:
		if e.Delegate {
			d.w("(yield*")
		} else {
			d.w("(yield")
		}
		if e.X != nil {
			d.w(" ")
			d.expr(e.X)
		}
		d.w(")")
	case *AwaitExpr:
		d.w("(await ")
		d.expr(e.X)
		d.w(")")
	}
}

func (d *dumper) property(p *Property) {
	switch p.Kind {
	case PropSpread:
		d.w("(... ")
		d.expr(p.Value)
		d.w(")")
	case PropShorthand:
		d.key(p.Key, false)
	case PropCoverInit:
		d.w("(cover ")
		d.key(p.Key, false)
		d.w(" ")
		d.expr(p.Value)
		d.w(")")
	case PropMethod, PropGetter, PropSetter:
		fn := p.Value.(*Function)
		d.w("(")
		switch p.Kind {
		case PropGetter:
			d.w("get ")
		case PropSetter:
			d.w("set ")
		default:
			d.w("method ")
		}
		d.key(p.Key, p.Computed)
		d.w(" ")
		d.params(fn)
		d.stmts(fn.Body.Body, d.level+1)
		d.w(")")
	default:
		d.w("(")
		d.key(p.Key, p.Computed)
		d.w(" ")
		d.expr(p.Value)
		d.w(")")
	}
}

func (d *dumper) pattern(p Pattern) {
	switch p := p.(type) {
	case *Ident:
		d.w(p.Name)
	case *MemberExpr:
		d.expr(p)
	case *AssignPattern:
		d.w("(= ")
		d.pattern(p.Target)
		d.w(" ")
		d.expr(p.Default)
		d.w(")")
	case *ObjectPattern:
		d.w("(opat")
		for _, prop := range p.Props {
			d.w(" (")
			d.key(prop.Key, prop.Computed)
			d.w(" ")
			d.pattern(prop.Value)
			d.w(")")
		}
		if p.Rest != nil {
			d.w(" (... ")
			d.pattern(p.Rest)
			d.w(")")
		}
		d.w(")")
	case *ArrayPattern:
		d.w("(apat")
		for _, e := range p.Elems {
			d.w(" ")
			if e == nil {
				d.w("<hole>")
				continue
			}
			d.pattern(e)
		}
		if p.Rest != nil {
			d.w(" (... ")
			d.pattern(p.Rest)
			d.w(")")
		}
		d.w(")")
	}
}
