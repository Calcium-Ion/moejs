package regexpsyntax

import "slices"

// Character classes of the v flag (ECMA-262 22.2.1 ClassSetExpression):
// nested classes, union, intersection (&&) and subtraction (--) of operands,
// string literals (\q{...}) and properties of strings. A class evaluates to
// a classSet of single characters and strings (CompileToCharSet), and the
// atom using it lowers the strings to an alternation tried longest first
// (CompileAtom), which both engines run.
//
// With i, every operand is folded by simple case folding
// (MaybeSimpleCaseFolding) before the set operations, and a complement is
// taken among the folding's fixed points (AllCharacters), so `[^...]`,
// `\P{...}`, `\W` and set differences act on case-folded characters rather
// than on the raw ones as in u mode.

// classSet is a CharSet of the v flag: single code points (chars) and
// strings of any other length (strs, sorted and unique). The slices may
// alias the Unicode tables and are never modified in place.
type classSet struct {
	chars  Set
	strs   [][]rune
	mayStr bool // MayContainStrings, the syntactic test behind early errors
}

func (a *classSet) union(b *classSet) {
	a.chars = a.chars.Union(b.chars)
	a.mayStr = a.mayStr || b.mayStr
	if len(b.strs) == 0 {
		return
	}
	strs := make([][]rune, 0, len(a.strs)+len(b.strs))
	strs = append(strs, a.strs...)
	strs = append(strs, b.strs...)
	a.strs = sortStrings(strs)
}

func (a *classSet) intersect(b *classSet) {
	a.chars = a.chars.Intersect(b.chars)
	a.mayStr = a.mayStr && b.mayStr
	a.strs = filterStrings(a.strs, b.strs, true)
}

// subtract removes b from a; MayContainStrings stays that of a.
func (a *classSet) subtract(b *classSet) {
	a.chars = a.chars.Subtract(b.chars)
	a.strs = filterStrings(a.strs, b.strs, false)
}

// sortStrings sorts and deduplicates strs in place.
func sortStrings(strs [][]rune) [][]rune {
	slices.SortFunc(strs, slices.Compare[[]rune])
	return slices.CompactFunc(strs, slices.Equal[[]rune])
}

// filterStrings returns the members of a that are (keep) or are not (!keep)
// in b, as a new slice.
func filterStrings(a, b [][]rune, keep bool) [][]rune {
	if len(a) == 0 || len(b) == 0 && !keep {
		return a
	}
	var out [][]rune
	for _, s := range a {
		if _, ok := slices.BinarySearchFunc(b, s, slices.Compare[[]rune]); ok == keep {
			out = append(out, s)
		}
	}
	return out
}

var errClassSetOperation = &Error{"Invalid set operation in character class"}

// classSetClass parses a v-mode CharacterClass or NestedClass (pos at '[').
func (p *reParser) classSetClass() (*classSet, error) {
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer p.leave()
	p.pos++ // '['
	negate := false
	if c, ok := p.peek(); ok && c == '^' {
		negate = true
		p.pos++
	}
	cs, err := p.classSetExpression()
	if err != nil {
		return nil, err
	}
	if negate {
		if cs.mayStr {
			return nil, &Error{"Negated character class may contain strings"}
		}
		return &classSet{chars: p.classComplement(cs.chars)}, nil
	}
	return cs, nil
}

// classSetExpression parses ClassContents up to and including the closing
// ']': a union of operands and ranges, or operands joined by one kind of
// operator (&& or --), which cannot be mixed.
func (p *reParser) classSetExpression() (*classSet, error) {
	if c, ok := p.peek(); ok && c == ']' {
		p.pos++
		return &classSet{}, nil
	}
	acc, isRange, err := p.classSetOperand(true)
	if err != nil {
		return nil, err
	}
	if !isRange && (p.hasPrefix("&&") || p.hasPrefix("--")) {
		op := "&&"
		if p.src[p.pos] == '-' {
			op = "--"
		}
		for {
			p.pos += 2
			if c, ok := p.peek(); ok && op == "&&" && c == '&' {
				return nil, errClassSetOperation
			}
			rhs, _, err := p.classSetOperand(false)
			if err != nil {
				return nil, err
			}
			if op == "&&" {
				acc.intersect(rhs)
			} else {
				acc.subtract(rhs)
			}
			c, ok := p.peek()
			switch {
			case !ok:
				return nil, &Error{"Unterminated character class"}
			case c == ']':
				p.pos++
				return acc, nil
			case !p.hasPrefix(op):
				return nil, errClassSetOperation
			}
		}
	}
	for {
		c, ok := p.peek()
		switch {
		case !ok:
			return nil, &Error{"Unterminated character class"}
		case c == ']':
			p.pos++
			return acc, nil
		case p.hasPrefix("&&") || p.hasPrefix("--"):
			return nil, errClassSetOperation
		}
		rhs, _, err := p.classSetOperand(true)
		if err != nil {
			return nil, err
		}
		acc.union(rhs)
	}
}

// classSetOperand parses a ClassSetOperand, or with allowRange also a
// ClassSetRange (isRange).
func (p *reParser) classSetOperand(allowRange bool) (cs *classSet, isRange bool, err error) {
	c, ok := p.peek()
	if !ok {
		return nil, false, &Error{"Unterminated character class"}
	}
	if c == '[' {
		cs, err := p.classSetClass()
		return cs, false, err
	}
	if c == '\\' {
		if e, ok := p.peekAt(1); ok {
			switch e {
			case 'q':
				if n, ok := p.peekAt(2); ok && n == '{' {
					p.pos += 3
					cs, err := p.classStringDisjunction()
					return cs, false, err
				}
			case 'd', 'D', 's', 'S', 'w', 'W', 'p', 'P':
				p.pos += 2
				cs, err := p.classSetEscape(e)
				return cs, false, err
			}
		}
	}
	lo, err := p.classSetCharacter()
	if err != nil {
		return nil, false, err
	}
	hi := lo
	if n, ok := p.peek(); allowRange && ok && n == '-' {
		if n2, ok := p.peekAt(1); !ok || n2 != '-' {
			p.pos++
			if hi, err = p.classSetCharacter(); err != nil {
				return nil, false, err
			}
			if hi < lo {
				return nil, false, &Error{"Range out of order in character class"}
			}
			isRange = true
		}
	}
	return p.foldClassSet(&classSet{chars: Set{lo, hi}}), isRange, nil
}

// classSetCharacter parses a ClassSetCharacter: a source character other
// than the class syntax characters and not starting a reserved double
// punctuator, a CharacterEscape, an escaped reserved punctuator or \b.
func (p *reParser) classSetCharacter() (rune, error) {
	c, ok := p.peek()
	if !ok {
		return 0, &Error{"Unterminated character class"}
	}
	if c == '\\' {
		p.pos++
		e, ok := p.peek()
		if !ok {
			return 0, &Error{`\ at end of pattern`}
		}
		switch e {
		case '&', '-', '!', '#', '%', ',', ':', ';', '<', '=', '>', '@', '`', '~':
			p.pos++
			return rune(e), nil
		}
		return p.characterEscape(true)
	}
	switch c {
	case '(', ')', '[', ']', '{', '}', '/', '-', '|':
		return 0, &Error{"Invalid character in character class"}
	case '&', '!', '#', '$', '%', '*', '+', ',', '.', ':', ';', '<', '=', '>', '?', '@', '^', '`', '~':
		if n, ok := p.peekAt(1); ok && n == c {
			return 0, errClassSetOperation
		}
	}
	return p.readPairRune(), nil
}

// classStringDisjunction parses the body of \q{...} (pos after the '{').
func (p *reParser) classStringDisjunction() (*classSet, error) {
	cs := &classSet{}
	var str []rune
	for {
		c, ok := p.peek()
		if !ok {
			return nil, &Error{"Invalid escape"}
		}
		if c != '|' && c != '}' {
			r, err := p.classSetCharacter()
			if err != nil {
				return nil, err
			}
			str = append(str, r)
			continue
		}
		p.pos++
		if len(str) == 1 {
			cs.chars = cs.chars.Union(Set{str[0], str[0]})
		} else {
			cs.strs = append(cs.strs, str)
			cs.mayStr = true
		}
		str = nil
		if c == '}' {
			break
		}
	}
	cs.strs = sortStrings(cs.strs)
	return p.foldClassSet(cs), nil
}

// classSetEscape evaluates the CharacterClassEscape c (\d \D \s \S \w \W,
// \p{...} and \P{...}; pos after c).
func (p *reParser) classSetEscape(c uint16) (*classSet, error) {
	switch c {
	case 'd':
		return &classSet{chars: digitSet}, nil
	case 'D':
		return &classSet{chars: p.classComplement(digitSet)}, nil
	case 's':
		return &classSet{chars: spaceSet}, nil
	case 'S':
		return &classSet{chars: p.classComplement(spaceSet)}, nil
	case 'w', 'W':
		cs := p.foldClassSet(&classSet{chars: p.wordChars()})
		if c == 'W' {
			cs.chars = p.classComplement(cs.chars)
		}
		return cs, nil
	}
	set, strs, err := p.unicodePropertyValue()
	if err != nil {
		return nil, err
	}
	cs := &classSet{chars: set}
	if strs != nil {
		if c == 'P' {
			return nil, &Error{"Invalid property name"}
		}
		cs = &classSet{chars: strs.chars, strs: strs.seqs, mayStr: true}
	}
	cs = p.foldClassSet(cs)
	if c == 'P' {
		cs.chars = p.classComplement(cs.chars)
	}
	return cs, nil
}

// foldClassSet is MaybeSimpleCaseFolding: with i, every character of cs
// (alone or in a string) is replaced by its simple case folding.
func (p *reParser) foldClassSet(cs *classSet) *classSet {
	if !p.icase {
		return cs
	}
	m := simpleFolding()
	cs.chars = m.simpleFoldSet(cs.chars)
	if len(cs.strs) != 0 {
		strs := make([][]rune, len(cs.strs))
		for i, s := range cs.strs {
			t := make([]rune, len(s))
			for j, c := range s {
				t[j] = m.Canonicalize(c)
			}
			strs[i] = t
		}
		cs.strs = sortStrings(strs)
	}
	return cs
}

// classComplement is CharacterComplement: the code points not in set,
// among the simple case folding's fixed points with i.
func (p *reParser) classComplement(set Set) Set {
	if p.icase {
		return set.Union(simpleFolding().keySet).Complement()
	}
	return set.Complement()
}

// classSetNode lowers a v-mode class to its matcher (CompileAtom): the
// strings longest first, then the single characters, then the empty string.
// Each character matches its case closure under i, like any character atom.
func (p *reParser) classSetNode(cs *classSet) *Node {
	if p.check {
		return &p.scratch
	}
	chars := &Node{Op: OpChar, Set: cs.chars}
	if p.icase {
		chars.Set = caseClosure(cs.chars, true)
	}
	if len(cs.strs) == 0 {
		return chars
	}
	strs := slices.Clone(cs.strs)
	slices.SortStableFunc(strs, func(a, b []rune) int { return len(b) - len(a) })
	alt := &Node{Op: OpAlt}
	empty := false
	for _, s := range strs {
		if len(s) == 0 {
			empty = true
			continue
		}
		seq := &Node{Op: OpSeq, Subs: make([]*Node, len(s))}
		for i, c := range s {
			seq.Subs[i] = p.charNode(Set{c, c})
		}
		alt.Subs = append(alt.Subs, seq)
	}
	if len(cs.chars) != 0 {
		alt.Subs = append(alt.Subs, chars)
	}
	if empty {
		alt.Subs = append(alt.Subs, &Node{Op: OpEmpty})
	}
	if len(alt.Subs) == 1 {
		return alt.Subs[0]
	}
	return alt
}
