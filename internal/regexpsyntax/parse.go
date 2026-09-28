// Package regexpsyntax parses ECMAScript regular expression patterns. It is
// shared by the regexp engine (package engine), which compiles the tree, and
// the syntax package, which reports invalid regexp literals as early errors.
// It depends on the standard library only.
package regexpsyntax

import (
	"strings"
	"unicode/utf16"
)

// Pattern parsing. Parse turns a JavaScript pattern into a
// Node tree that both execution paths of the engine consume: the RE2 emitter
// (engine/regexp_translate.go) and the backtracking compiler
// (engine/regexp_bt_compile.go).
// Character matching is resolved here: an OpChar node carries the exact set
// of characters it matches, with the i flag's Canonicalize closure and the
// class complement already applied, so neither path consults case tables
// while matching (backreferences excepted).
//
// Characters are UTF-16 code units without the u flag and code points with
// it; the sets of a non-u pattern stay within [0, 0xFFFF].

// Op is the kind of a Node.
type Op uint8

const (
	OpEmpty    Op = iota
	OpChar        // one character in set
	OpSeq         // subs in order
	OpAlt         // subs in priority order
	OpCapture     // group index around subs[0]
	OpRepeat      // subs[0] repeated min..max times (max -1: unbounded)
	OpLook        // lookahead or lookbehind (behind, negate) of subs[0]
	OpBackref     // backreference to the groups in refs
	OpBegin       // ^
	OpEnd         // $
	OpWordB       // \b
	OpNotWordB    // \B
)

// Node is one node of a parsed pattern.
type Node struct {
	Op        Op
	Greedy    bool // OpRepeat
	Negate    bool // OpLook
	Behind    bool // OpLook
	Multiline bool // OpBegin, OpEnd: the m flag
	// Icase is the i flag in effect for OpBackref (Canonicalize compare)
	// and OpWordB/OpNotWordB (u-mode word characters include U+017F and
	// U+212A).
	Icase    bool
	Index    int // OpCapture: group number
	Min, Max int // OpRepeat
	// CapLo and CapHi delimit the groups [CapLo, CapHi) inside a repeated or
	// lookaround body.
	CapLo, CapHi int
	Set          Set    // OpChar
	Refs         []int  // OpBackref: group numbers
	Name         string // OpBackref by name, until resolved
	Subs         []*Node
}

// CanBeEmpty reports whether n can match the empty string.
func (n *Node) CanBeEmpty() bool {
	switch n.Op {
	case OpChar:
		return false
	case OpSeq:
		for _, s := range n.Subs {
			if !s.CanBeEmpty() {
				return false
			}
		}
		return true
	case OpAlt:
		for _, s := range n.Subs {
			if s.CanBeEmpty() {
				return true
			}
		}
		return false
	case OpCapture:
		return n.Subs[0].CanBeEmpty()
	case OpRepeat:
		return n.Min == 0 || n.Subs[0].CanBeEmpty()
	}
	return true // empty, assertions, lookarounds, backreferences
}

// Walk calls fn on n and its descendants in pre-order.
func (n *Node) Walk(fn func(*Node)) {
	fn(n)
	for _, s := range n.Subs {
		s.Walk(fn)
	}
}

// reMaxNesting bounds the nesting of groups and lookarounds, which bounds
// the Go recursion of the parser, the emitter and the backtracking compiler.
// Go's RE2 parser has the same limit.
const reMaxNesting = 1000

// AST is a parsed pattern.
type AST struct {
	Root     *Node
	Names    []string // capture names by group number (index 0 unused); "" for unnamed
	HasNames bool
	DupNames bool // a name is shared by groups in different alternatives
}

// Flags are the flags that affect parsing.
type Flags struct {
	IgnoreCase, Multiline, DotAll, Unicode, UnicodeSets bool
}

// Error reports a malformed pattern; Msg is the V8 wording without the
// "Invalid regular expression: /.../: " prefix.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

// Parse parses a pattern (UTF-16 code units). Errors are *Error.
func Parse(pattern []uint16, flags Flags) (*AST, error) {
	p := &reParser{src: pattern, u: flags.Unicode, v: flags.UnicodeSets, icase: flags.IgnoreCase, multiline: flags.Multiline, dotAll: flags.DotAll}
	p.ngroups, p.hasNamed = p.prescanGroups()
	p.names = make([]string, 1, p.ngroups+1)
	return p.parse()
}

// A Checker validates patterns without building their tree, keeping its
// buffers from one pattern to the next. The zero value is ready to use.
type Checker struct{ p reParser }

// Check returns the error Parse would return for pattern, or nil. It skips
// only the work that cannot fail: the tree and the character sets of plain
// atoms and non-v classes.
func (c *Checker) Check(pattern []uint16, flags Flags) error {
	p := &c.p
	*p = reParser{src: pattern, u: flags.Unicode, v: flags.UnicodeSets, icase: flags.IgnoreCase, multiline: flags.Multiline, dotAll: flags.DotAll,
		check: true, names: p.names[:0], alts: p.alts[:0], namedRefs: p.namedRefs[:0], classBuf: p.classBuf[:0]}
	p.ngroups, p.hasNamed = p.prescanGroups()
	p.names = append(p.names, "")
	_, err := p.parse()
	p.src = nil
	return err
}

// parse parses the prescanned pattern.
func (p *reParser) parse() (*AST, error) {
	root, err := p.disjunction()
	if err != nil {
		return nil, err
	}
	if p.pos < len(p.src) {
		return nil, &Error{"Unmatched ')'"}
	}
	for len(p.names) <= p.ngroups {
		p.names = append(p.names, "")
	}
	for _, ref := range p.namedRefs {
		for i, name := range p.names[1:] {
			if name == ref.Name {
				ref.Refs = append(ref.Refs, i+1)
			}
		}
		if len(ref.Refs) == 0 {
			return nil, &Error{"Invalid named capture referenced"}
		}
	}
	if p.check {
		return nil, nil
	}
	return &AST{Root: root, Names: p.names, HasNames: p.hasNamed, DupNames: p.dupNames}, nil
}

// reParser is a recursive-descent parser over the pattern grammar of
// ECMA-262 22.2.1 with the Annex B leniencies in non-u mode.
type reParser struct {
	src []uint16
	pos int
	u   bool // characters are code points
	v   bool // the v flag's class syntax (classset.go)
	// The flags in effect at pos.
	icase, multiline, dotAll bool
	ngroups                  int  // capturing groups in the pattern (prescan)
	hasNamed                 bool // the pattern has a named group
	names                    []string
	depth                    int
	namedRefs                []*Node
	// alts is the path of alternatives from the root to pos, one entry per
	// enclosing disjunction; namedAlts holds the path of each named group,
	// for the duplicate-name early error.
	alts      []reAltPos
	namedAlts []reNamedGroup
	disjs     int // disjunctions so far, their ids
	dupNames  bool
	// check is set for a Checker: node returns the scratch node, Subs and
	// the sets of plain atoms and non-v classes stay empty, and the class
	// buffer is reused.
	check    bool
	scratch  Node
	classBuf Set
}

// node returns a new node holding n, or the scratch node in check mode.
func (p *reParser) node(n Node) *Node {
	if p.check {
		return &p.scratch
	}
	m := new(Node)
	*m = n
	return m
}

// one returns the Subs of a node with the single child n.
func (p *reParser) one(n *Node) []*Node {
	if p.check {
		return nil
	}
	return []*Node{n}
}

// reAltPos is alternative alt of disjunction disj.
type reAltPos struct{ disj, alt int }

type reNamedGroup struct {
	name string
	alts []reAltPos
}

// mightBothParticipate is MightBothParticipate (22.2.1.1) for two groups
// with alternative paths a and b: false when some disjunction holds them in
// different alternatives.
func mightBothParticipate(a, b []reAltPos) bool {
	for i := 0; i < len(a) && i < len(b) && a[i].disj == b[i].disj; i++ {
		if a[i].alt != b[i].alt {
			return false
		}
	}
	return true
}

// top is the largest character of the pattern's alphabet.
func (p *reParser) top() rune {
	if p.u {
		return MaxCodePoint
	}
	return 0xFFFF
}

func (p *reParser) peek() (uint16, bool) {
	if p.pos < len(p.src) {
		return p.src[p.pos], true
	}
	return 0, false
}

func (p *reParser) peekAt(off int) (uint16, bool) {
	if p.pos+off < len(p.src) {
		return p.src[p.pos+off], true
	}
	return 0, false
}

func (p *reParser) hasPrefix(s string) bool {
	if p.pos+len(s) > len(p.src) {
		return false
	}
	for i := range len(s) {
		if p.src[p.pos+i] != uint16(s[i]) {
			return false
		}
	}
	return true
}

// enter and leave bracket a nested group.
func (p *reParser) enter() error {
	p.depth++
	if p.depth > reMaxNesting {
		return &Error{"Regular expression too deeply nested"}
	}
	return nil
}

func (p *reParser) leave() { p.depth-- }

// prescanGroups counts capturing groups and detects named groups, which the
// spec needs before parsing (backreference validity, \k handling).
func (p *reParser) prescanGroups() (int, bool) {
	n, named := 0, false
	inClass := 0 // nesting depth; classes only nest with v
	for i := 0; i < len(p.src); i++ {
		switch p.src[i] {
		case '\\':
			i++
		case '[':
			if inClass == 0 || p.v {
				inClass++
			}
		case ']':
			inClass = max(inClass-1, 0)
		case '(':
			if inClass > 0 {
				continue
			}
			if i+1 < len(p.src) && p.src[i+1] == '?' {
				if i+2 < len(p.src) && p.src[i+2] == '<' && i+3 < len(p.src) && p.src[i+3] != '=' && p.src[i+3] != '!' {
					n++
					named = true
				}
				continue
			}
			n++
		}
	}
	return n, named
}

// disjunction parses Alternative ('|' Alternative)* up to ')' or the end.
func (p *reParser) disjunction() (*Node, error) {
	var alts []*Node
	p.alts = append(p.alts, reAltPos{disj: p.disjs})
	p.disjs++
	for {
		alt, err := p.alternative()
		if err != nil {
			return nil, err
		}
		if !p.check {
			alts = append(alts, alt)
		}
		c, ok := p.peek()
		if !ok || c == ')' {
			break
		}
		p.pos++ // '|'
		p.alts[len(p.alts)-1].alt++
	}
	p.alts = p.alts[:len(p.alts)-1]
	if len(alts) == 1 {
		return alts[0], nil
	}
	return p.node(Node{Op: OpAlt, Subs: alts}), nil
}

// alternative parses Term*.
func (p *reParser) alternative() (*Node, error) {
	var terms []*Node
	for {
		c, ok := p.peek()
		if !ok || c == '|' || c == ')' {
			break
		}
		n, err := p.term()
		if err != nil {
			return nil, err
		}
		if !p.check {
			terms = append(terms, n)
		}
	}
	switch len(terms) {
	case 0:
		return p.node(Node{Op: OpEmpty}), nil
	case 1:
		return terms[0], nil
	}
	return p.node(Node{Op: OpSeq, Subs: terms}), nil
}

// term parses one Assertion or Atom Quantifier?.
func (p *reParser) term() (*Node, error) {
	c := p.src[p.pos]
	switch c {
	case '^':
		p.pos++
		return p.node(Node{Op: OpBegin, Multiline: p.multiline}), nil
	case '$':
		p.pos++
		return p.node(Node{Op: OpEnd, Multiline: p.multiline}), nil
	case '\\':
		if n, ok := p.peekAt(1); ok && (n == 'b' || n == 'B') {
			p.pos += 2
			op := OpWordB
			if n == 'B' {
				op = OpNotWordB
			}
			return p.node(Node{Op: op, Icase: p.icase}), nil
		}
	case '(':
		if p.hasPrefix("(?=") || p.hasPrefix("(?!") || p.hasPrefix("(?<=") || p.hasPrefix("(?<!") {
			capLo := p.capCount()
			behind := p.hasPrefix("(?<")
			look, err := p.lookaround()
			if err != nil {
				return nil, err
			}
			look.CapLo, look.CapHi = capLo, p.capCount()
			// Annex B (QuantifiableAssertion): a lookahead may be quantified
			// without the u flag.
			if !behind && !p.u {
				return p.quantifier(look, capLo)
			}
			if p.atQuantifier() {
				return nil, &Error{"Nothing to repeat"}
			}
			return look, nil
		}
	case '*', '+', '?':
		return nil, &Error{"Nothing to repeat"}
	case '{':
		if p.u {
			return nil, &Error{"Lone quantifier brackets"}
		}
		if _, _, n, ok := p.scanBraceQuantifier(p.pos); ok {
			p.pos = n
			return nil, &Error{"Nothing to repeat"}
		}
	case '}', ']':
		if p.u {
			return nil, &Error{"Lone quantifier brackets"}
		}
	}
	capLo := p.capCount()
	atom, err := p.atom()
	if err != nil {
		return nil, err
	}
	return p.quantifier(atom, capLo)
}

// capCount is the group number the next capturing group gets.
func (p *reParser) capCount() int { return len(p.names) }

// atQuantifier reports whether a quantifier starts at pos.
func (p *reParser) atQuantifier() bool {
	c, ok := p.peek()
	return ok && (c == '*' || c == '+' || c == '?' || c == '{' && p.isBraceQuantifierAt(p.pos))
}

// quantifier parses an optional quantifier for atom; capLo is the first
// group number inside atom.
func (p *reParser) quantifier(atom *Node, capLo int) (*Node, error) {
	c, ok := p.peek()
	if !ok {
		return atom, nil
	}
	var minRep, maxRep int
	switch c {
	case '*':
		p.pos++
		minRep, maxRep = 0, -1
	case '+':
		p.pos++
		minRep, maxRep = 1, -1
	case '?':
		p.pos++
		minRep, maxRep = 0, 1
	case '{':
		lo, hi, next, ok := p.scanBraceQuantifier(p.pos)
		if !ok {
			if p.u {
				return nil, &Error{"Incomplete quantifier"}
			}
			return atom, nil // literal '{' handled by the next term
		}
		if hi != -1 && hi < lo {
			return nil, &Error{"numbers out of order in {} quantifier"}
		}
		p.pos = next
		minRep, maxRep = lo, hi
	default:
		return atom, nil
	}
	greedy := true
	if n, ok := p.peek(); ok && n == '?' {
		p.pos++
		greedy = false
	}
	if p.atQuantifier() {
		return nil, &Error{"Nothing to repeat"}
	}
	return p.node(Node{Op: OpRepeat, Min: minRep, Max: maxRep, Greedy: greedy, CapLo: capLo, CapHi: p.capCount(), Subs: p.one(atom)}), nil
}

func (p *reParser) isBraceQuantifierAt(pos int) bool {
	_, _, _, ok := p.scanBraceQuantifier(pos)
	return ok
}

// reMaxRepeat caps the counts of a {n,m} quantifier: larger counts behave
// the same on any subject a string can hold.
const reMaxRepeat = 1<<31 - 1

// scanBraceQuantifier parses {n}, {n,} or {n,m} at pos. hi is -1 for open
// ranges; values saturate at reMaxRepeat.
func (p *reParser) scanBraceQuantifier(pos int) (lo, hi, next int, ok bool) {
	i := pos + 1
	readInt := func() (int, bool) {
		start := i
		v := 0
		for i < len(p.src) && p.src[i] >= '0' && p.src[i] <= '9' {
			v = min(v*10+int(p.src[i]-'0'), reMaxRepeat)
			i++
		}
		return v, i > start
	}
	lo, okLo := readInt()
	if !okLo {
		return 0, 0, 0, false
	}
	hi = lo
	if i < len(p.src) && p.src[i] == ',' {
		i++
		if v, okHi := readInt(); okHi {
			hi = v
		} else {
			hi = -1
		}
	}
	if i >= len(p.src) || p.src[i] != '}' {
		return 0, 0, 0, false
	}
	return lo, hi, i + 1, true
}

// atom parses one Atom.
func (p *reParser) atom() (*Node, error) {
	c := p.src[p.pos]
	switch c {
	case '.':
		p.pos++
		if p.check {
			return &p.scratch, nil
		}
		// Line terminators have no case mappings: no closure needed.
		set := Set{0, p.top()}
		if !p.dotAll {
			set = set.Subtract(lineTerminatorSet)
		}
		return &Node{Op: OpChar, Set: set}, nil
	case '(':
		return p.group()
	case ')':
		return nil, &Error{"Unmatched ')'"}
	case '[':
		if p.v {
			cs, err := p.classSetClass()
			if err != nil {
				return nil, err
			}
			return p.classSetNode(cs), nil
		}
		return p.characterClass()
	case '\\':
		return p.atomEscape()
	}
	return p.runeNode(p.readSourceRune()), nil
}

// charNode returns the node matching the characters of set under the
// current flags (the i flag matches its Canonicalize closure).
func (p *reParser) charNode(set Set) *Node {
	if p.check {
		return &p.scratch
	}
	if p.icase {
		set = caseClosure(set, p.u)
	}
	return &Node{Op: OpChar, Set: set}
}

// runeNode is charNode for the single character r.
func (p *reParser) runeNode(r rune) *Node {
	if p.check {
		return &p.scratch
	}
	return p.charNode(Set{r, r})
}

// caseClosure returns the characters that match set under the i flag: every
// c whose Canonicalize equals that of a member. ASCII-only sets, the common
// case, are closed without loading the case tables: without u the only
// ASCII equivalences are the letter pairs (Canonicalize never maps non-ASCII
// to ASCII), with u simple case folding adds U+017F to s and U+212A to k.
func caseClosure(set Set, unicodeMode bool) Set {
	if len(set) == 0 || set[len(set)-1] >= 0x80 {
		if len(set) == 0 {
			return set
		}
		return Canonicalizer(unicodeMode).closure(set)
	}
	var add Set
	for i := 0; i < len(set); i += 2 {
		lo, hi := set[i], set[i+1]
		if l, h := max(lo, 'a'), min(hi, 'z'); l <= h {
			add = append(add, l-'a'+'A', h-'a'+'A')
		}
		if l, h := max(lo, 'A'), min(hi, 'Z'); l <= h {
			add = append(add, l-'A'+'a', h-'A'+'a')
		}
		if unicodeMode {
			if lo <= 's' && hi >= 's' || lo <= 'S' && hi >= 'S' {
				add = append(add, 0x17F, 0x17F)
			}
			if lo <= 'k' && hi >= 'k' || lo <= 'K' && hi >= 'K' {
				add = append(add, 0x212A, 0x212A)
			}
		}
	}
	return set.Union(add)
}

// readSourceRune consumes one pattern character, combining surrogate pairs
// in u mode (in non-u mode each code unit is a character).
func (p *reParser) readSourceRune() rune {
	if p.u {
		return p.readPairRune()
	}
	c := p.src[p.pos]
	p.pos++
	return rune(c)
}

// readPairRune consumes one pattern character, combining a surrogate pair.
func (p *reParser) readPairRune() rune {
	c := p.src[p.pos]
	p.pos++
	if c >= 0xD800 && c < 0xDC00 && p.pos < len(p.src) && p.src[p.pos] >= 0xDC00 && p.src[p.pos] < 0xE000 {
		r := utf16.DecodeRune(rune(c), rune(p.src[p.pos]))
		p.pos++
		return r
	}
	return rune(c)
}

// group parses "(" ... ")" in its capturing, named and non-capturing forms.
func (p *reParser) group() (*Node, error) {
	p.pos++ // '('
	index := -1
	switch {
	case p.hasPrefix("?:"):
		p.pos += 2
	case p.hasPrefix("?<"):
		p.pos += 2
		name, ok := p.groupName()
		if !ok {
			return nil, &Error{"Invalid capture group name"}
		}
		// A name may repeat only in alternatives that cannot both
		// participate: (?<a>x)|(?<a>y).
		for _, g := range p.namedAlts {
			if g.name == name {
				if mightBothParticipate(g.alts, p.alts) {
					return nil, &Error{"Duplicate capture group name"}
				}
				p.dupNames = true
			}
		}
		p.namedAlts = append(p.namedAlts, reNamedGroup{name, append([]reAltPos(nil), p.alts...)})
		index = len(p.names)
		p.names = append(p.names, name)
	case p.hasPrefix("?"):
		return p.modifiedGroup()
	default:
		index = len(p.names)
		p.names = append(p.names, "")
	}
	body, err := p.groupBody()
	if err != nil {
		return nil, err
	}
	if index < 0 {
		return body, nil
	}
	return p.node(Node{Op: OpCapture, Index: index, Subs: p.one(body)}), nil
}

// modifiedGroup parses the rest of a modified group, `(?ims-ims:...)`
// (22.2.1.1): the flags added and removed apply to the group's body only.
// p.pos is at the '?'.
func (p *reParser) modifiedGroup() (*Node, error) {
	var seen [2]string // the flags before and after '-'
	i := p.pos + 1
	side := 0
	for ; i < len(p.src); i++ {
		c := p.src[i]
		if c == '-' {
			if side == 1 {
				return nil, &Error{"Multiple dashes in flag group"}
			}
			side = 1
			continue
		}
		if c != 'i' && c != 'm' && c != 's' {
			break
		}
		if strings.IndexByte(seen[0], byte(c)) >= 0 || strings.IndexByte(seen[1], byte(c)) >= 0 {
			return nil, &Error{"Repeated flag in flag group"}
		}
		seen[side] += string(rune(c))
	}
	if i >= len(p.src) || p.src[i] != ':' {
		return nil, &Error{"Invalid group"}
	}
	if side == 1 && seen[0] == "" && seen[1] == "" {
		return nil, &Error{"Invalid flag group"}
	}
	p.pos = i + 1
	icase, multiline, dotAll := p.icase, p.multiline, p.dotAll
	for side, fl := range seen {
		for _, c := range fl {
			switch c {
			case 'i':
				p.icase = side == 0
			case 'm':
				p.multiline = side == 0
			case 's':
				p.dotAll = side == 0
			}
		}
	}
	body, err := p.groupBody()
	p.icase, p.multiline, p.dotAll = icase, multiline, dotAll
	return body, err
}

// groupBody parses the disjunction of a group and its closing ')'.
func (p *reParser) groupBody() (*Node, error) {
	if err := p.enter(); err != nil {
		return nil, err
	}
	body, err := p.disjunction()
	if err != nil {
		return nil, err
	}
	p.leave()
	if c, ok := p.peek(); !ok || c != ')' {
		return nil, &Error{"Unterminated group"}
	}
	p.pos++
	return body, nil
}

// lookaround parses (?=...), (?!...), (?<=...) or (?<!...).
func (p *reParser) lookaround() (*Node, error) {
	behind := p.src[p.pos+2] == '<'
	var negate bool
	if behind {
		negate = p.src[p.pos+3] == '!'
		p.pos += 4
	} else {
		negate = p.src[p.pos+2] == '!'
		p.pos += 3
	}
	body, err := p.groupBody()
	if err != nil {
		return nil, err
	}
	return p.node(Node{Op: OpLook, Behind: behind, Negate: negate, Subs: p.one(body)}), nil
}

// groupName parses a RegExpIdentifierName up to and including '>'. Escapes
// are always u-mode escapes and surrogate pairs always combine (ECMA-262
// 22.2.1 GroupName).
func (p *reParser) groupName() (string, bool) {
	var name []rune
	for {
		c, ok := p.peek()
		if !ok {
			return "", false
		}
		if c == '>' {
			p.pos++
			break
		}
		var r rune
		if c == '\\' {
			p.pos++
			if n, ok := p.peek(); !ok || n != 'u' {
				return "", false
			}
			p.pos++
			if r, ok = p.unicodeEscapeBody(true, true); !ok {
				return "", false
			}
		} else {
			r = p.readPairRune()
		}
		if !isRegExpIdentifierChar(r, len(name) == 0) {
			return "", false
		}
		name = append(name, r)
	}
	return string(name), len(name) > 0
}

// isRegExpIdentifierChar reports whether r may start (start set) or continue
// a group name: ID_Start, $ and _, or ID_Continue, $, ZWNJ and ZWJ.
func isRegExpIdentifierChar(r rune, start bool) bool {
	if r < 0x80 {
		return r == '$' || r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || !start && r >= '0' && r <= '9'
	}
	if start {
		return IsIDStart(r)
	}
	return r == 0x200C || r == 0x200D || IsIDContinue(r)
}

// classEscapeSet returns the set of \d \D \s \S \w \W (ok false for any
// other c). With u and i the word characters include U+017F and U+212A,
// whose Canonicalize is an ASCII word character (WordCharacters).
func (p *reParser) classEscapeSet(c uint16) (Set, bool) {
	switch c {
	case 'd':
		return digitSet, true
	case 'D':
		return digitSet.ComplementIn(p.top()), true
	case 's':
		return spaceSet, true
	case 'S':
		return spaceSet.ComplementIn(p.top()), true
	case 'w':
		return p.wordChars(), true
	case 'W':
		return p.wordChars().ComplementIn(p.top()), true
	}
	return nil, false
}

func (p *reParser) wordChars() Set {
	if p.u && p.icase {
		return wordFoldSet
	}
	return wordSet
}

// atomEscape parses an escape outside a character class (the leading '\' is
// at pos).
func (p *reParser) atomEscape() (*Node, error) {
	p.pos++ // '\'
	c, ok := p.peek()
	if !ok {
		return nil, &Error{`\ at end of pattern`}
	}
	if p.v {
		// With i the v flag folds and complements class escapes differently
		// (and \p may name a property of strings).
		switch c {
		case 'd', 'D', 's', 'S', 'w', 'W', 'p', 'P':
			p.pos++
			cs, err := p.classSetEscape(c)
			if err != nil {
				return nil, err
			}
			return p.classSetNode(cs), nil
		}
	}
	if set, ok := p.classEscapeSet(c); ok {
		p.pos++
		return p.charNode(set), nil
	}
	switch c {
	case 'p', 'P':
		if p.u {
			p.pos++
			set, err := p.unicodeProperty(c == 'P')
			if err != nil {
				return nil, err
			}
			return p.charNode(set), nil
		}
	case 'k':
		if p.u || p.hasNamed {
			p.pos++
			if n, ok := p.peek(); !ok || n != '<' {
				return nil, &Error{"Invalid named reference"}
			}
			p.pos++
			name, ok := p.groupName()
			if !ok {
				return nil, &Error{"Invalid named reference"}
			}
			ref := &Node{Op: OpBackref, Name: name, Icase: p.icase}
			p.namedRefs = append(p.namedRefs, ref)
			return ref, nil
		}
	case '1', '2', '3', '4', '5', '6', '7', '8', '9':
		n, next := p.scanDecimal(p.pos)
		if n <= p.ngroups {
			p.pos = next
			if p.check {
				return &p.scratch, nil
			}
			return &Node{Op: OpBackref, Refs: []int{n}, Icase: p.icase}, nil
		}
		if p.u {
			return nil, &Error{"Invalid escape"}
		}
		if c >= '8' {
			p.pos++
			return p.runeNode(rune(c)), nil
		}
		return p.runeNode(p.legacyOctal()), nil
	}
	r, err := p.characterEscape(false)
	if err != nil {
		return nil, err
	}
	return p.runeNode(r), nil
}

// scanDecimal reads a decimal integer at pos (saturating) and returns it with
// the index after the digits.
func (p *reParser) scanDecimal(pos int) (int, int) {
	v := 0
	for pos < len(p.src) && p.src[pos] >= '0' && p.src[pos] <= '9' {
		v = min(v*10+int(p.src[pos]-'0'), reMaxRepeat)
		pos++
	}
	return v, pos
}

// legacyOctal parses an Annex B octal escape (up to three digits, value <= 255).
func (p *reParser) legacyOctal() rune {
	v := 0
	n := 0
	for n < 3 && p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '7' {
		nv := v*8 + int(p.src[p.pos]-'0')
		if nv > 255 {
			break
		}
		v = nv
		p.pos++
		n++
	}
	return rune(v)
}

// characterEscape parses the escape after '\' for the escapes shared by
// atoms and classes (pos is at the escaped character). inClass enables the
// class-only forms (\b backspace, \- and \cX with digits/underscore).
func (p *reParser) characterEscape(inClass bool) (rune, error) {
	c := p.src[p.pos]
	p.pos++
	switch c {
	case 't':
		return '\t', nil
	case 'n':
		return '\n', nil
	case 'v':
		return 0x0B, nil
	case 'f':
		return 0x0C, nil
	case 'r':
		return '\r', nil
	case 'b':
		if inClass {
			return 0x08, nil
		}
	case '-':
		if inClass || !p.u {
			return '-', nil
		}
		return 0, &Error{"Invalid escape"}
	case '0':
		if n, ok := p.peek(); ok && n >= '0' && n <= '9' {
			if p.u {
				return 0, &Error{"Invalid decimal escape"}
			}
			p.pos--
			return p.legacyOctal(), nil
		}
		return 0, nil
	case 'c':
		if n, ok := p.peek(); ok {
			if n >= 'a' && n <= 'z' || n >= 'A' && n <= 'Z' {
				p.pos++
				return rune(n % 32), nil
			}
			if inClass && !p.u && (n >= '0' && n <= '9' || n == '_') {
				p.pos++
				return rune(n % 32), nil
			}
		}
		if p.u {
			return 0, &Error{"Invalid unicode escape"}
		}
		p.pos-- // "\c" matches a literal backslash followed by 'c'
		return '\\', nil
	case 'x':
		if hi, ok := p.hexDigits(2); ok {
			return hi, nil
		}
		if p.u {
			return 0, &Error{"Invalid escape"}
		}
		return 'x', nil
	case 'u':
		if r, ok := p.unicodeEscapeBody(p.u, p.u); ok {
			return r, nil
		}
		if p.u {
			return 0, &Error{"Invalid Unicode escape"}
		}
		return 'u', nil
	}
	if p.u {
		switch c {
		case '^', '$', '\\', '.', '*', '+', '?', '(', ')', '[', ']', '{', '}', '|', '/':
			return rune(c), nil
		}
		return 0, &Error{"Invalid escape"}
	}
	if c >= '1' && c <= '9' {
		// Inside a class in non-u mode a decimal escape is a legacy octal
		// escape (or the literal digit for 8 and 9).
		if c >= '8' {
			return rune(c), nil
		}
		p.pos--
		return p.legacyOctal(), nil
	}
	return rune(c), nil
}

// hexDigits reads exactly n hex digits.
func (p *reParser) hexDigits(n int) (rune, bool) {
	if p.pos+n > len(p.src) {
		return 0, false
	}
	var v rune
	for i := range n {
		d := digitValueUnit(p.src[p.pos+i])
		if d < 0 || d >= 16 {
			return 0, false
		}
		v = v*16 + rune(d)
	}
	p.pos += n
	return v, true
}

// digitValueUnit is the value of c as a base-36 digit, or -1.
func digitValueUnit(c uint16) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'z':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'Z':
		return int(c-'A') + 10
	}
	return -1
}

// unicodeEscapeBody parses the part after "\u": XXXX (combining a following
// \uXXXX low surrogate when pairs is set) or, when braces is set, {X...}.
func (p *reParser) unicodeEscapeBody(braces, pairs bool) (rune, bool) {
	if braces && p.pos < len(p.src) && p.src[p.pos] == '{' {
		i := p.pos + 1
		var v rune
		n := 0
		for i < len(p.src) && p.src[i] != '}' {
			d := digitValueUnit(p.src[i])
			if d < 0 || d >= 16 {
				return 0, false
			}
			v = v*16 + rune(d)
			if v > MaxCodePoint {
				return 0, false
			}
			i++
			n++
		}
		if n == 0 || i >= len(p.src) {
			return 0, false
		}
		p.pos = i + 1
		return v, true
	}
	hi, ok := p.hexDigits(4)
	if !ok {
		return 0, false
	}
	if pairs && hi >= 0xD800 && hi < 0xDC00 && p.pos+6 <= len(p.src) && p.src[p.pos] == '\\' && p.src[p.pos+1] == 'u' {
		save := p.pos
		p.pos += 2
		if lo, ok := p.hexDigits(4); ok && lo >= 0xDC00 && lo < 0xE000 {
			return utf16.DecodeRune(hi, lo), true
		}
		p.pos = save
	}
	return hi, true
}

// characterClass parses "[...]" (pos at '[').
func (p *reParser) characterClass() (*Node, error) {
	p.pos++ // '['
	negate := false
	if c, ok := p.peek(); ok && c == '^' {
		negate = true
		p.pos++
	}
	var set Set
	if p.check {
		set = p.classBuf[:0]
	}
	for {
		c, ok := p.peek()
		if !ok {
			return nil, &Error{"Unterminated character class"}
		}
		if c == ']' {
			p.pos++
			break
		}
		lo, loSet, isSet, err := p.classAtom()
		if err != nil {
			return nil, err
		}
		if isSet {
			set = append(set, loSet...)
			// A set followed by '-' and another atom is an error in u mode;
			// in non-u mode (Annex B) it is the union of the set, '-' and the
			// atom, which cannot start a range: [\s-0-A] has no '1'.
			if n, ok := p.peek(); ok && n == '-' {
				if n2, ok2 := p.peekAt(1); ok2 && n2 != ']' {
					if p.u {
						return nil, &Error{"Invalid character class"}
					}
					p.pos++
					hi, hiSet, hiIsSet, err := p.classAtom()
					if err != nil {
						return nil, err
					}
					set = append(set, '-', '-')
					if hiIsSet {
						set = append(set, hiSet...)
					} else {
						set = append(set, hi, hi)
					}
				}
			}
			continue
		}
		if n, ok := p.peek(); ok && n == '-' {
			if n2, ok2 := p.peekAt(1); ok2 && n2 != ']' {
				p.pos++
				hi, hiSet, hiIsSet, err := p.classAtom()
				if err != nil {
					return nil, err
				}
				if hiIsSet {
					if p.u {
						return nil, &Error{"Invalid character class"}
					}
					set = append(set, lo, lo, '-', '-')
					set = append(set, hiSet...)
					continue
				}
				if hi < lo {
					return nil, &Error{"Range out of order in character class"}
				}
				set = append(set, lo, hi)
				continue
			}
		}
		set = append(set, lo, lo)
	}
	if p.check {
		p.classBuf = set
		return &p.scratch, nil
	}
	set = set.Normalize()
	// Without the v flag a class matches Canonicalize-equal characters of
	// its members, and a negated class the rest (CharacterSetMatcher).
	if p.icase {
		set = caseClosure(set, p.u)
	}
	if negate {
		set = set.ComplementIn(p.top())
	}
	return &Node{Op: OpChar, Set: set}, nil
}

// classAtom parses one ClassAtom: a character r, or a class escape's set
// (isSet).
func (p *reParser) classAtom() (r rune, set Set, isSet bool, err error) {
	c := p.src[p.pos]
	if c != '\\' {
		return p.readSourceRune(), nil, false, nil
	}
	p.pos++
	e, ok := p.peek()
	if !ok {
		return 0, nil, false, &Error{`\ at end of pattern`}
	}
	if set, ok := p.classEscapeSet(e); ok {
		p.pos++
		return 0, set, true, nil
	}
	if (e == 'p' || e == 'P') && p.u {
		p.pos++
		set, err := p.unicodeProperty(e == 'P')
		return 0, set, true, err
	}
	if e == 'B' && !p.u {
		p.pos++
		return 'B', nil, false, nil
	}
	if e == 'k' && !p.u {
		// Annex B's IdentityEscape excludes k in a pattern with a group
		// name (SourceCharacterIdentityEscape[+NamedCaptureGroups]).
		if p.hasNamed {
			return 0, nil, false, &Error{"Invalid escape"}
		}
		p.pos++
		return 'k', nil, false, nil
	}
	r, err = p.characterEscape(true)
	return r, nil, false, err
}

// unicodeProperty parses "{Name}" or "{Name=Value}" after \p or \P in u
// mode (unicodePropertyValue) and complements the set for \P.
func (p *reParser) unicodeProperty(negate bool) (Set, error) {
	set, _, err := p.unicodePropertyValue()
	if err != nil {
		return nil, err
	}
	if negate {
		set = set.Complement()
	}
	return set, nil
}

// unicodePropertyValue parses "{Name}" or "{Name=Value}" after \p or \P: a
// General_Category value, a binary property of ECMA-262 table 67, or
// General_Category, Script or Script_Extensions with a value (any alias).
// With v a lone name may also be a property of strings (strs).
func (p *reParser) unicodePropertyValue() (set Set, strs *stringProperty, err error) {
	if c, ok := p.peek(); !ok || c != '{' {
		return nil, nil, &Error{"Invalid property name"}
	}
	p.pos++
	var sb strings.Builder
	for {
		c, ok := p.peek()
		if !ok || !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '=' || c == '}') {
			return nil, nil, &Error{"Invalid property name"}
		}
		p.pos++
		if c == '}' {
			break
		}
		sb.WriteByte(byte(c))
	}
	name, value, hasValue := strings.Cut(sb.String(), "=")
	var ok bool
	if hasValue {
		switch name {
		case "General_Category", "gc":
			set, ok = generalCategorySet(value)
		case "Script", "sc":
			set, ok = scriptSet(value, false)
		case "Script_Extensions", "scx":
			set, ok = scriptSet(value, true)
		}
	} else if set, ok = generalCategorySet(name); !ok {
		if set, ok = binaryPropertySet(name); !ok && p.v {
			strs, ok = stringPropertySet(name)
		}
	}
	if !ok {
		return nil, nil, &Error{"Invalid property name"}
	}
	return set, strs, nil
}
