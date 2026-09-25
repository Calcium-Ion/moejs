package engine

// Backtracking matcher, compilation half. Patterns
// that RE2 cannot run with JavaScript semantics (reExact) are compiled from
// the reNode tree of internal/regexpsyntax into a flat instruction program that
// the VM of regexp_bt.go runs over the subject's UTF-16 code units.
//
// An instruction program rather than the spec's tree of continuation
// closures: the VM keeps its backtrack state in one explicit slice, so
// neither the subject length nor the pattern nesting grows the Go stack; the
// slice can be bounded (a RangeError instead of exhausting memory) and
// pooled; the program is immutable, so it joins the process-wide pattern
// cache next to the RE2 programs; and the single dispatch loop is where the
// interrupt is polled. Counted repeats run as counters in registers, so
// `a{1000000}` is one instruction and `(?:ab){100000}` a loop, never an
// unrolled copy of the body.

// btOp is a VM instruction.
type btOp uint8

const (
	btMatch     btOp = iota
	btUnit           // the code unit a (a character in both modes: never a surrogate with u)
	btChar           // one character of set a
	btStar           // greedy run of min b, max c (-1: unbounded) characters of set a
	btStarLazy       // lazy run of min b, max c characters of set a
	btSplit          // continue at a, backtrack to b
	btJmp            // continue at a
	btSave           // capture slot a := pos
	btBegin          // ^ (flag: multiline)
	btEnd            // $ (flag: multiline)
	btWordB          // \b (flag: U+017F and U+212A are word characters)
	btNotWordB       // \B (flag as btWordB)
	btBackref        // backreference to the groups refs[a:a+b] (flag: ignore case)
	btLookStart      // lookaround in register a, continuing at b after it (flag: negative)
	btLookEnd        // end of the lookaround in register a (flag: negative)
	btLoopInit       // counter register a := 0
	btLoop           // counter a, min b, max c (-1: unbounded), exit d (flag: greedy)
	btIterStart      // register a := pos (a -1: none); slots [b, c) := undefined
	btIterEnd        // counter a += 1 and continue at loop d; fails an empty iteration past min c (start register b, -1: no check)
)

// btInst is one instruction. back is set inside a lookbehind, where
// characters are read right to left.
type btInst struct {
	op         btOp
	back       bool
	flag       bool
	a, b, c, d int32
}

// btSet is a character set with an ASCII bitmap in front of the ranges.
type btSet struct {
	ascii  [2]uint64
	high   bool // the set has characters >= 0x80
	ranges runeSet
}

func newBTSet(set runeSet) btSet {
	s := btSet{ranges: set}
	for i := 0; i < len(set); i += 2 {
		for c := set[i]; c <= set[i+1] && c < 0x80; c++ {
			s.ascii[c>>6] |= 1 << (c & 63)
		}
		if set[i+1] >= 0x80 {
			s.high = true
		}
	}
	return s
}

func (s *btSet) has(c rune) bool {
	if c < 0x80 {
		return s.ascii[c>>6]&(1<<(c&63)) != 0
	}
	return s.high && s.ranges.Has(c)
}

// btProg is a compiled pattern.
type btProg struct {
	insts   []btInst
	sets    []btSet
	refs    []int32 // backreference group lists
	nslots  int     // 2 * (groups + 1)
	nregs   int
	unicode bool
	// anchored is set when the pattern starts with a ^ without the m flag,
	// so a search tries position 0 only.
	anchored bool
	// first is the set index of the characters a match can start with, or
	// -1 when unknown (a match can be empty or start with a backreference).
	first int32
}

// compileBT compiles a parsed pattern. The recursion is bounded by the
// parser's nesting limit.
func compileBT(ast *reAST, flags regexpFlags) *btProg {
	c := &btCompiler{p: &btProg{nslots: 2 * len(ast.Names), unicode: flags.unicode, first: -1}}
	c.node(ast.Root, false)
	c.emit(btInst{op: btMatch})
	root := ast.Root
	if root.Op == reOpSeq {
		root = root.Subs[0]
	}
	c.p.anchored = root.Op == reOpBegin && !root.Multiline
	if set, nullable, ok := btFirst(ast.Root); ok && !nullable {
		c.p.first = c.set(set)
	}
	return c.p
}

type btCompiler struct {
	p *btProg
}

func (c *btCompiler) emit(in btInst) int {
	c.p.insts = append(c.p.insts, in)
	return len(c.p.insts) - 1
}

func (c *btCompiler) pc() int32 { return int32(len(c.p.insts)) }

func (c *btCompiler) set(set runeSet) int32 {
	c.p.sets = append(c.p.sets, newBTSet(set))
	return int32(len(c.p.sets) - 1)
}

func (c *btCompiler) reg() int32 {
	c.p.nregs++
	return int32(c.p.nregs - 1)
}

// node compiles n; back reads right to left (lookbehind bodies).
func (c *btCompiler) node(n *reNode, back bool) {
	switch n.Op {
	case reOpEmpty:
	case reOpChar:
		c.char(n.Set, back)
	case reOpSeq:
		if back {
			for i := len(n.Subs) - 1; i >= 0; i-- {
				c.node(n.Subs[i], back)
			}
		} else {
			for _, s := range n.Subs {
				c.node(s, back)
			}
		}
	case reOpAlt:
		var jumps []int
		for i, s := range n.Subs {
			if i == len(n.Subs)-1 {
				c.node(s, back)
				break
			}
			split := c.emit(btInst{op: btSplit, a: c.pc() + 1})
			c.node(s, back)
			jumps = append(jumps, c.emit(btInst{op: btJmp}))
			c.p.insts[split].b = c.pc()
		}
		for _, j := range jumps {
			c.p.insts[j].a = c.pc()
		}
	case reOpCapture:
		// Right to left the end of the group is reached first.
		open, close := int32(2*n.Index), int32(2*n.Index+1)
		if back {
			open, close = close, open
		}
		c.emit(btInst{op: btSave, a: open})
		c.node(n.Subs[0], back)
		c.emit(btInst{op: btSave, a: close})
	case reOpRepeat:
		c.repeat(n, back)
	case reOpLook:
		reg := c.reg()
		start := c.emit(btInst{op: btLookStart, a: reg, flag: n.Negate})
		c.node(n.Subs[0], n.Behind)
		c.emit(btInst{op: btLookEnd, a: reg, flag: n.Negate})
		c.p.insts[start].b = c.pc()
	case reOpBackref:
		off := int32(len(c.p.refs))
		for _, g := range n.Refs {
			c.p.refs = append(c.p.refs, int32(g))
		}
		c.emit(btInst{op: btBackref, back: back, flag: n.Icase, a: off, b: int32(len(n.Refs))})
	case reOpBegin:
		c.emit(btInst{op: btBegin, flag: n.Multiline})
	case reOpEnd:
		c.emit(btInst{op: btEnd, flag: n.Multiline})
	case reOpWordB:
		c.emit(btInst{op: btWordB, flag: n.Icase && c.p.unicode})
	case reOpNotWordB:
		c.emit(btInst{op: btNotWordB, flag: n.Icase && c.p.unicode})
	}
}

// char compiles one character of set.
func (c *btCompiler) char(set runeSet, back bool) {
	if r, ok := set.Single(); ok && r <= 0xFFFF && (!c.p.unicode || !isSurrogateUnit(r)) {
		c.emit(btInst{op: btUnit, back: back, a: r})
		return
	}
	c.emit(btInst{op: btChar, back: back, a: c.set(set)})
}

// repeat compiles a quantified atom: a run instruction for a single
// character, a split for an optional atom that cannot match empty, and a
// counter loop otherwise (RepeatMatcher: captures of the body reset at each
// iteration, and an iteration past min that matches empty fails).
func (c *btCompiler) repeat(n *reNode, back bool) {
	body := n.Subs[0]
	switch {
	case n.Max == 0:
		return
	case n.Min == 1 && n.Max == 1:
		c.node(body, back)
		return
	case body.Op == reOpChar:
		op := btStar
		if !n.Greedy && n.Min != n.Max {
			op = btStarLazy
		}
		c.emit(btInst{op: op, back: back, a: c.set(body.Set), b: int32(n.Min), c: int32(n.Max)})
		return
	}
	empty := body.CanBeEmpty()
	if n.Min == 0 && n.Max == 1 && !empty {
		split := c.emit(btInst{op: btSplit})
		c.node(body, back)
		if n.Greedy {
			c.p.insts[split].a, c.p.insts[split].b = int32(split+1), c.pc()
		} else {
			c.p.insts[split].a, c.p.insts[split].b = c.pc(), int32(split+1)
		}
		return
	}
	cnt := c.reg()
	start := int32(-1)
	if empty && n.Min != n.Max {
		start = c.reg()
	}
	c.emit(btInst{op: btLoopInit, a: cnt})
	loop := c.emit(btInst{op: btLoop, a: cnt, b: int32(n.Min), c: int32(n.Max), flag: n.Greedy})
	if start >= 0 || n.CapLo < n.CapHi {
		c.emit(btInst{op: btIterStart, a: start, b: int32(2 * n.CapLo), c: int32(2 * n.CapHi)})
	}
	c.node(body, back)
	c.emit(btInst{op: btIterEnd, a: cnt, b: start, c: int32(n.Min), d: int32(loop)})
	c.p.insts[loop].d = c.pc()
}

// btFirst returns the characters a forward match of n can start with, and
// whether n can match without consuming one; ok is false when unknown.
// Assertions and lookarounds consume nothing and are skipped.
func btFirst(n *reNode) (set runeSet, nullable, ok bool) {
	switch n.Op {
	case reOpChar:
		return n.Set, false, true
	case reOpSeq:
		for _, s := range n.Subs {
			ss, null, ok := btFirst(s)
			if !ok {
				return nil, false, false
			}
			set = set.Union(ss)
			if !null {
				return set, false, true
			}
		}
		return set, true, true
	case reOpAlt:
		for _, s := range n.Subs {
			ss, null, ok := btFirst(s)
			if !ok {
				return nil, false, false
			}
			set = set.Union(ss)
			nullable = nullable || null
		}
		return set, nullable, true
	case reOpCapture:
		return btFirst(n.Subs[0])
	case reOpRepeat:
		if n.Max == 0 {
			return nil, true, true
		}
		set, nullable, ok = btFirst(n.Subs[0])
		return set, nullable || n.Min == 0, ok
	case reOpBackref:
		return nil, false, false
	}
	return nil, true, true // empty, assertions, lookarounds
}

// --- engine selection -----------------------------------------------------------

// reExact reports whether RE2 runs the translated pattern with JavaScript's
// semantics, so the pattern may use RE2 (and the single-class fast path).
// Besides the constructs RE2 lacks (lookarounds, backreferences, counts
// above 1000), two RepeatMatcher rules have no RE2 equivalent:
//
//   - an iteration past min that matches the empty string fails in
//     JavaScript, while RE2 accepts it: a repeat whose body can match empty
//     is inexact unless its count is fixed (min == max, where no iteration
//     runs past min);
//   - every iteration starts with the body's captures undefined, while RE2
//     keeps a group's value from an earlier iteration: a repeat that can
//     iterate more than once is inexact when some capture in its body may
//     be skipped by an iteration (it sits under an alternation, an optional
//     repeat or a lookaround within the body). A capture every iteration
//     sets, as in `(?:x(\d))+` or `(a|b)*`, ends with the last iteration's
//     value on both engines.
//
// The subject-dependent differences (m-mode line terminators, `\b` with u
// and i, private-use characters standing for surrogates) are checked per
// subject by compiledRegExp.useBT.
func reExact(root *reNode) bool {
	exact := true
	root.Walk(func(n *reNode) {
		switch n.Op {
		case reOpLook, reOpBackref:
			exact = false
		case reOpRepeat:
			if n.Min > reMaxRE2Repeat || n.Max > reMaxRE2Repeat {
				exact = false
			}
			if n.Max != n.Min && n.Subs[0].CanBeEmpty() {
				exact = false
			}
			if (n.Max == -1 || n.Max > 1) && hasConditionalCapture(n.Subs[0], false) {
				exact = false
			}
		}
	})
	return exact
}

// hasConditionalCapture reports whether a match of n may skip one of the
// captures in n; cond is set below a construct that may be skipped.
func hasConditionalCapture(n *reNode, cond bool) bool {
	switch n.Op {
	case reOpCapture:
		if cond {
			return true
		}
	case reOpAlt:
		cond = cond || len(n.Subs) > 1
	case reOpRepeat:
		cond = cond || n.Min == 0
	case reOpLook:
		cond = true
	}
	for _, s := range n.Subs {
		if hasConditionalCapture(s, cond) {
			return true
		}
	}
	return false
}
