package syntax

// Visitor is called by Walk for each node; the returned visitor is used for
// the node's children, and a nil result skips them (as in go/ast).
type Visitor interface {
	Visit(n Node) Visitor
}

type inspector func(Node) bool

func (f inspector) Visit(n Node) Visitor {
	if f(n) {
		return f
	}
	return nil
}

// Inspect traverses the tree in depth-first source order, calling f for
// every node. If f returns false the node's children are skipped.
func Inspect(n Node, f func(Node) bool) { Walk(inspector(f), n) }

// Walk traverses the tree rooted at n in depth-first source order. Nil
// children (holes, absent else branches) are skipped; non-computed property
// keys and member names are visited as *Ident nodes.
func Walk(v Visitor, n Node) {
	if n == nil {
		return
	}
	if v = v.Visit(n); v == nil {
		return
	}
	switch n := n.(type) {
	case *Module:
		walkStmts(v, n.Body)
	case *Script:
		walkStmts(v, n.Body)
	case *Program:
		walkStmts(v, n.Body)

	case *Ident, *ThisExpr, *SuperExpr, *NewTarget, *ImportMeta, *NumberLit, *BigIntLit,
		*StringLit, *BoolLit, *NullLit, *RegexLit, *PrivateName, *EmptyStmt, *DebuggerStmt:
		// leaves
	case *ImportCall:
		Walk(v, n.Source)
	case *TemplateLit:
		walkExprs(v, n.Exprs)
	case *TaggedTemplate:
		Walk(v, n.Tag)
		Walk(v, n.Quasi)
	case *ArrayLit:
		walkExprs(v, n.Elems)
	case *ObjectLit:
		for _, p := range n.Props {
			Walk(v, p)
		}
	case *Property:
		if n.Key != nil {
			Walk(v, n.Key)
		}
		Walk(v, n.Value)
	case *Function:
		if n.Name != nil {
			Walk(v, n.Name)
		}
		for _, p := range n.Params {
			Walk(v, p)
		}
		if n.Rest != nil {
			Walk(v, n.Rest)
		}
		if n.Body != nil {
			Walk(v, n.Body)
		}
		if n.ExprBody != nil {
			Walk(v, n.ExprBody)
		}
	case *Class:
		if n.Name != nil {
			Walk(v, n.Name)
		}
		if n.Super != nil {
			Walk(v, n.Super)
		}
		for _, m := range n.Members {
			Walk(v, m)
		}
	case *ClassMember:
		if n.Key != nil {
			Walk(v, n.Key)
		}
		if n.Value != nil {
			Walk(v, n.Value)
		}
		if n.Body != nil {
			Walk(v, n.Body)
		}
	case *UnaryExpr:
		Walk(v, n.X)
	case *UpdateExpr:
		Walk(v, n.X)
	case *BinaryExpr:
		Walk(v, n.X)
		Walk(v, n.Y)
	case *LogicalExpr:
		Walk(v, n.X)
		Walk(v, n.Y)
	case *AssignExpr:
		Walk(v, n.Target)
		Walk(v, n.Value)
	case *CondExpr:
		Walk(v, n.Test)
		Walk(v, n.Cons)
		Walk(v, n.Alt)
	case *SeqExpr:
		walkExprs(v, n.Exprs)
	case *CallExpr:
		Walk(v, n.Callee)
		walkExprs(v, n.Args)
	case *NewExpr:
		Walk(v, n.Callee)
		walkExprs(v, n.Args)
	case *MemberExpr:
		Walk(v, n.Object)
		Walk(v, n.Prop)
	case *OptChain:
		Walk(v, n.X)
	case *SpreadElem:
		Walk(v, n.X)
	case *YieldExpr:
		if n.X != nil {
			Walk(v, n.X)
		}
	case *AwaitExpr:
		Walk(v, n.X)

	case *ObjectPattern:
		for _, p := range n.Props {
			Walk(v, p)
		}
		if n.Rest != nil {
			Walk(v, n.Rest)
		}
	case *PatternProp:
		Walk(v, n.Key)
		Walk(v, n.Value)
	case *ArrayPattern:
		for _, e := range n.Elems {
			if e != nil {
				Walk(v, e)
			}
		}
		if n.Rest != nil {
			Walk(v, n.Rest)
		}
	case *AssignPattern:
		Walk(v, n.Target)
		Walk(v, n.Default)

	case *VarDecl:
		for _, d := range n.Decls {
			Walk(v, d)
		}
	case *Declarator:
		Walk(v, n.Target)
		if n.Init != nil {
			Walk(v, n.Init)
		}
	case *FuncDecl:
		Walk(v, n.Func)
	case *ClassDecl:
		Walk(v, n.Class)
	case *ExprStmt:
		Walk(v, n.X)
	case *BlockStmt:
		walkStmts(v, n.Body)
	case *IfStmt:
		Walk(v, n.Cond)
		Walk(v, n.Then)
		if n.Else != nil {
			Walk(v, n.Else)
		}
	case *ForStmt:
		if n.Init != nil {
			Walk(v, n.Init)
		}
		if n.Cond != nil {
			Walk(v, n.Cond)
		}
		if n.Update != nil {
			Walk(v, n.Update)
		}
		Walk(v, n.Body)
	case *ForInOfStmt:
		Walk(v, n.Left)
		Walk(v, n.Right)
		Walk(v, n.Body)
	case *WhileStmt:
		Walk(v, n.Cond)
		Walk(v, n.Body)
	case *DoWhileStmt:
		Walk(v, n.Body)
		Walk(v, n.Cond)
	case *ReturnStmt:
		if n.Result != nil {
			Walk(v, n.Result)
		}
	case *ThrowStmt:
		Walk(v, n.X)
	case *BreakStmt:
		if n.Label != nil {
			Walk(v, n.Label)
		}
	case *ContinueStmt:
		if n.Label != nil {
			Walk(v, n.Label)
		}
	case *LabeledStmt:
		Walk(v, n.Label)
		Walk(v, n.Body)
	case *SwitchStmt:
		Walk(v, n.Disc)
		for _, c := range n.Cases {
			Walk(v, c)
		}
	case *SwitchCase:
		if n.Test != nil {
			Walk(v, n.Test)
		}
		walkStmts(v, n.Body)
	case *TryStmt:
		Walk(v, n.Block)
		if n.Param != nil {
			Walk(v, n.Param)
		}
		if n.Handler != nil {
			Walk(v, n.Handler)
		}
		if n.Finalizer != nil {
			Walk(v, n.Finalizer)
		}
	case *ExportDecl:
		Walk(v, n.Decl)
	case *ExportNamed:
		for _, s := range n.Specs {
			Walk(v, s)
		}
		if n.Source != nil {
			Walk(v, n.Source)
		}
	case *ExportSpec:
		Walk(v, n.Local)
		Walk(v, n.Exported)
	case *ExportDefault:
		Walk(v, n.Decl)
	case *ExportAll:
		if n.As != nil {
			Walk(v, n.As)
		}
		Walk(v, n.Source)
	case *ImportDecl:
		if n.Default != nil {
			Walk(v, n.Default)
		}
		if n.Namespace != nil {
			Walk(v, n.Namespace)
		}
		for _, s := range n.Specs {
			Walk(v, s)
		}
		Walk(v, n.Source)
	case *ImportSpec:
		Walk(v, n.Imported)
		Walk(v, n.Local)
	default:
		panic("syntax.Walk: unexpected node type")
	}
}

func walkStmts(v Visitor, list []Stmt) {
	for _, s := range list {
		Walk(v, s)
	}
}

func walkExprs(v Visitor, list []Expr) {
	for _, e := range list {
		if e != nil {
			Walk(v, e)
		}
	}
}
