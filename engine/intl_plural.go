package engine

import "strings"

// The operands and rules of CLDR's plural categories (UTS #35 part 3
// §5.1) for Intl.PluralRules and the plural forms of Intl.NumberFormat.
// A rule set is parsed once into relations over the operands, which come
// from the formatted digits. The rules only take operands modulo powers
// of ten and compare them with small values, so each operand keeps its
// value modulo 10^18 and whether it reaches 10^18: exact for any length.

// intlPluralCategory is a CLDR plural category, in CLDR's order.
type intlPluralCategory uint8

const (
	intlPluralZero intlPluralCategory = iota
	intlPluralOne
	intlPluralTwo
	intlPluralFew
	intlPluralMany
	intlPluralOther
)

// intlPluralNames are the categories' names, in CLDR's order.
var intlPluralNames = [...]string{"zero", "one", "two", "few", "many", "other"}

// intlPluralMod is the modulus the operands i, f and t keep; every
// modulus of a rule divides it.
const intlPluralMod uint64 = 1e18

// intlPluralOperands are the operands of a number for the plural rules
// (UTS #35 part 3 §5.1.1: n, i, v, w, f, t, c and e), from its formatted
// string and its compact or scientific exponent.
type intlPluralOperands struct {
	// i, f and t modulo intlPluralMod. n is i when t is zero, and not an
	// integer otherwise.
	i, f, t uint64
	// v and w count the visible fraction digits with and without their
	// trailing zeros.
	v, w int
	// e is the exponent (c and e).
	e int
	// large has intlPluralLargeI, F and T for those at least intlPluralMod.
	large uint8
}

const (
	intlPluralLargeI uint8 = 1 << iota
	intlPluralLargeF
	intlPluralLargeT
)

// intlOperands returns the operands of a formatted string (as
// formatNumericToString appends it) with the exponent of compact or
// scientific notation (0 for none).
func intlOperands(formatted []byte, exponent int) intlPluralOperands {
	// The digits without the '.', numbered from 0, the point after the
	// integer digits moved by the exponent.
	ni := len(formatted)
	for j, c := range formatted {
		if c == '.' {
			ni = j
			break
		}
	}
	nd := max(len(formatted)-1, ni)
	point := ni + exponent
	end := max(nd, point) // the fraction's end
	tend := end           // and without its trailing zeros
	for tend > point && intlPluralDigit(formatted, ni, tend-1) == '0' {
		tend--
	}
	o := intlPluralOperands{v: end - point, w: tend - point, e: exponent}
	var large bool
	if o.i, large = intlPluralDigits(formatted, ni, 0, point); large {
		o.large |= intlPluralLargeI
	}
	if o.f, large = intlPluralDigits(formatted, ni, point, end); large {
		o.large |= intlPluralLargeF
	}
	if o.t, large = intlPluralDigits(formatted, ni, point, tend); large {
		o.large |= intlPluralLargeT
	}
	return o
}

// intlPluralDigit returns the j-th digit of formatted without its '.' at
// ni, '0' outside them.
func intlPluralDigit(formatted []byte, ni, j int) byte {
	switch {
	case j < 0:
	case j < ni:
		return formatted[j]
	case j+1 < len(formatted):
		return formatted[j+1]
	}
	return '0'
}

// intlPluralDigits returns the integer of the digits a to b (exclusive) of
// formatted without its '.' at ni modulo intlPluralMod, and whether it
// reaches intlPluralMod.
func intlPluralDigits(formatted []byte, ni, a, b int) (x uint64, large bool) {
	for j := a; j < b; j++ {
		x = x*10 + uint64(intlPluralDigit(formatted, ni, j)-'0')
		if x >= intlPluralMod {
			x %= intlPluralMod
			large = true
		}
	}
	return
}

// intlPluralRuleSet is a compiled set of CLDR plural rules of one locale
// and type (cardinal or ordinal).
type intlPluralRuleSet struct {
	rules  []intlPluralRule
	rels   []intlPluralRel
	ranges []uint64 // inclusive bounds, two per range
	cats   []intlPluralCategory
}

// intlPluralRule is a category's condition, rels[start:end]: relations
// joined by "and" and "or", "and" binding tighter.
type intlPluralRule struct {
	cat        intlPluralCategory
	start, end uint16
}

// intlPluralRel is a relation: operand [% mod] (= | !=) range list.
type intlPluralRel struct {
	mod    uint64 // 0 for none
	lo, hi uint16 // the ranges, ranges[2lo:2hi]
	op     byte   // 'n', 'i', 'v', 'w', 'f', 't', 'c' or 'e'
	neg    bool   // !=
	and    bool   // joined to the next relation by "and" (else "or", or last)
}

// parseIntlPluralRules compiles rules written as CLDR's plurals.xml has
// them without the samples, "category:condition;" each, "other" implicit:
// "one:i = 1 and v = 0;many:e = 0 and i != 0 and i % 1000000 = 0 and v = 0 or e != 0..5;".
// It panics on a malformed rule: the rules are generated data.
func parseIntlPluralRules(src string) *intlPluralRuleSet {
	s := &intlPluralRuleSet{}
	var seen [intlPluralOther + 1]bool
	for _, rule := range strings.Split(src, ";") {
		name, cond, ok := strings.Cut(rule, ":")
		if !ok {
			if strings.TrimSpace(rule) == "" {
				continue
			}
			panic("intl: plural rule without a category: " + rule)
		}
		cat := intlPluralCategory(0)
		for cat < intlPluralOther && intlPluralNames[cat] != strings.TrimSpace(name) {
			cat++
		}
		if cat == intlPluralOther {
			if strings.TrimSpace(name) != "other" {
				panic("intl: unknown plural category: " + name)
			}
			continue // other is what no rule matches
		}
		start := len(s.rels)
		p := intlPluralParser{src: cond}
		p.condition(s)
		s.rules = append(s.rules, intlPluralRule{cat, uint16(start), uint16(len(s.rels))})
		seen[cat] = true
	}
	seen[intlPluralOther] = true
	for c, ok := range seen {
		if ok {
			s.cats = append(s.cats, intlPluralCategory(c))
		}
	}
	return s
}

// category returns the category of a number's operands.
func (s *intlPluralRuleSet) category(ops *intlPluralOperands) intlPluralCategory {
	for _, r := range s.rules {
		ok := true
		for j := r.start; j < r.end; j++ {
			rel := &s.rels[j]
			ok = ok && s.holds(rel, ops)
			if !rel.and {
				if ok {
					return r.cat
				}
				ok = true
			}
		}
	}
	return intlPluralOther
}

// categories returns the categories the rules use, other included, in
// CLDR's order.
func (s *intlPluralRuleSet) categories() []intlPluralCategory { return s.cats }

// holds evaluates a relation.
func (s *intlPluralRuleSet) holds(rel *intlPluralRel, o *intlPluralOperands) bool {
	var x uint64
	large := false
	switch rel.op {
	case 'n':
		if o.w != 0 {
			// Not an integer: in no range, modulo anything.
			return rel.neg
		}
		x, large = o.i, o.large&intlPluralLargeI != 0
	case 'i':
		x, large = o.i, o.large&intlPluralLargeI != 0
	case 'f':
		x, large = o.f, o.large&intlPluralLargeF != 0
	case 't':
		x, large = o.t, o.large&intlPluralLargeT != 0
	case 'v':
		x = uint64(o.v)
	case 'w':
		x = uint64(o.w)
	default: // 'c', 'e'
		if o.e < 0 {
			return rel.neg
		}
		x = uint64(o.e)
	}
	if rel.mod != 0 {
		x %= rel.mod // exact: mod divides intlPluralMod
	} else if large {
		return rel.neg // beyond every value
	}
	r := s.ranges[2*int(rel.lo) : 2*int(rel.hi)]
	for j := 0; j < len(r); j += 2 {
		if r[j] <= x && x <= r[j+1] {
			return !rel.neg
		}
	}
	return rel.neg
}

// intlPluralParser reads a condition of UTS #35 part 3 §5.1's syntax as
// CLDR writes it: relations "operand [% value] (= | !=) range_list"
// joined by "and" and "or", a range list being values and ranges a..b
// separated by commas.
type intlPluralParser struct {
	src string
	pos int
}

func (p *intlPluralParser) fail() {
	panic("intl: bad plural rule: " + p.src)
}

func (p *intlPluralParser) space() {
	for p.pos < len(p.src) && p.src[p.pos] == ' ' {
		p.pos++
	}
}

// word reads a run of letters.
func (p *intlPluralParser) word() string {
	p.space()
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos]-'a' < 26 {
		p.pos++
	}
	return p.src[start:p.pos]
}

// accept skips tok if it comes next.
func (p *intlPluralParser) accept(tok string) bool {
	p.space()
	if strings.HasPrefix(p.src[p.pos:], tok) {
		p.pos += len(tok)
		return true
	}
	return false
}

// value reads a value below intlPluralMod.
func (p *intlPluralParser) value() uint64 {
	p.space()
	start := p.pos
	var v uint64
	for ; p.pos < len(p.src) && intlIsDigit(p.src[p.pos]); p.pos++ {
		v = v*10 + uint64(p.src[p.pos]-'0')
		if v >= intlPluralMod {
			p.fail()
		}
	}
	if p.pos == start {
		p.fail()
	}
	return v
}

func (p *intlPluralParser) condition(s *intlPluralRuleSet) {
	for {
		var rel intlPluralRel
		op := p.word()
		if len(op) != 1 || !strings.Contains("niwvftce", op) {
			p.fail()
		}
		rel.op = op[0]
		if p.accept("%") {
			if rel.mod = p.value(); rel.mod == 0 || intlPluralMod%rel.mod != 0 {
				p.fail()
			}
		}
		switch {
		case p.accept("!="):
			rel.neg = true
		case p.accept("="):
		default:
			p.fail()
		}
		rel.lo = uint16(len(s.ranges) / 2)
		for {
			lo := p.value()
			hi := lo
			if p.accept("..") {
				if hi = p.value(); hi < lo {
					p.fail()
				}
			}
			s.ranges = append(s.ranges, lo, hi)
			if !p.accept(",") {
				break
			}
		}
		rel.hi = uint16(len(s.ranges) / 2)
		switch p.word() {
		case "and":
			rel.and = true
			s.rels = append(s.rels, rel)
		case "or":
			s.rels = append(s.rels, rel)
		case "":
			s.rels = append(s.rels, rel)
			if p.space(); p.pos != len(p.src) {
				p.fail()
			}
			return
		default:
			p.fail()
		}
	}
}
