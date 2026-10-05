package engine

import (
	"math/bits"
	"sync/atomic"
)

// Anchored run patterns. A pattern that is a sequence of single characters
// and of greedy repeats of one, anchored at its start (a leading ^ without
// the m flag, or the y flag), with an optional trailing $ without m and any
// capture groups around parts of the sequence, is matched here on ASCII
// subjects: one table lookup per byte, where Go's regexp steps an NFA a
// rune at a time (about 20 ns a byte for a data URL's base64). That is the
// shape of the validation plugins run over every image of a request:
//
//	/^data:image\/[a-z0-9.+-]+;base64,[A-Za-z0-9+/]+={0,2}$/
//
// Each repeat takes as many characters as it can and never gives one back.
// That is the specification's result when no repeat of variable length can
// give back a character the rest could start with: its set is disjoint from
// the sets of the items after it, up to and including the first one that
// must match a character (an item that can match nothing may be skipped, so
// the next one could start there too). Then a shorter run leaves a character
// nothing after it can take, the backtracking would fail, and one pass is
// the whole match, in time linear in the subject. Patterns that do not meet
// the condition, and subjects that are not ASCII, take the other engines.
//
// The sets come from the parsed pattern, which applied the i, s, u and v
// flags (case closure, the dot, properties), so only their ASCII members
// are read.

// runStep is one item of an anchored run pattern: a literal (consecutive
// characters of one byte each, matched as a string), a run of min..max bytes
// of set (max < 0: unbounded), or, when mark >= 0, the capture slot mark
// recording the position.
type runStep struct {
	lit      string
	set      *[256]uint8 // 1 for a member byte
	min, max int
	mark     int
}

// runPattern is a compiled anchored run pattern.
type runPattern struct {
	steps []runStep
	begin bool // a leading ^: the match can only start at 0
	end   bool // a trailing $: the match must end at the subject's end
	slots int  // len of a match's index slice: 2 * (groups + 1)
}

// Bounds of a run pattern, so that a pattern built from request data costs
// in the process-wide regexp cache what its RE2 program costs: runMaxSlots
// capture slots a match records on the stack (seven groups; a pattern with
// more leaves its matches to the other engines), runMaxSteps steps and
// runMaxTables tables of its own (256 bytes each) for its sets of more than
// one byte. Plugin patterns have under 16 steps and 4 tables.
const (
	runMaxSlots  = 16
	runMaxSteps  = 64
	runMaxTables = 8
)

// runChunk is how many bytes a run scans between interrupt checks.
const runChunk = 1 << 14

// runByteTables are the tables of the single-byte sets, shared by every
// pattern and built on first use; the 129th is the set with no ASCII member,
// which no byte of an ASCII subject is in.
var runByteTables [129]atomic.Pointer[[256]uint8]

func runByteTable(b int) *[256]uint8 {
	if t := runByteTables[b].Load(); t != nil {
		return t
	}
	t := new([256]uint8)
	if b < 0x80 {
		t[b] = 1
	}
	if !runByteTables[b].CompareAndSwap(nil, t) {
		t = runByteTables[b].Load()
	}
	return t
}

// asciiBits is the set of the ASCII characters of a set.
type asciiBits [2]uint64

func asciiBitsOf(set runeSet) asciiBits {
	var cs asciiBits
	for i := 0; i+1 < len(set) && set[i] < 0x80; i += 2 {
		for c := set[i]; c <= min(set[i+1], 0x7F); c++ {
			cs[c>>6] |= 1 << (c & 63)
		}
	}
	return cs
}

// runBuilder walks a pattern into a run pattern.
type runBuilder struct {
	p      runPattern
	bits   []asciiBits // the characters each step can start with
	lit    []byte      // the literal being collected
	walked bool        // a step, a mark or ^ was walked: ^ may not follow
	ended  bool        // $ was walked: nothing may follow
}

// compileRunPattern returns the run pattern of ast, or nil when ast is not
// one; flags.sticky (y) anchors a pattern without ^. A pattern that is not
// anchored is turned down before anything is allocated.
func compileRunPattern(ast *reAST, flags regexpFlags) *runPattern {
	if !flags.sticky {
		n := ast.Root
		for n.Op == reOpSeq && len(n.Subs) > 0 {
			n = n.Subs[0]
		}
		if n.Op != reOpBegin || n.Multiline {
			return nil
		}
	}
	b := &runBuilder{p: runPattern{slots: 2 * len(ast.Names)}}
	if !b.walk(ast.Root) {
		return nil
	}
	b.flush()
	p := &b.p
	if len(p.steps) > runMaxSteps {
		return nil
	}
	// No run of variable length may give back a character the rest could
	// start with.
	for i, st := range p.steps {
		if st.mark >= 0 || st.lit != "" || st.min == st.max {
			continue
		}
		for j := i + 1; j < len(p.steps); j++ {
			next := &p.steps[j]
			if next.mark >= 0 {
				continue
			}
			if b.bits[i][0]&b.bits[j][0]|b.bits[i][1]&b.bits[j][1] != 0 {
				return nil
			}
			if next.lit != "" || next.min > 0 {
				break
			}
		}
	}
	// The tables: a single byte's is shared, the others are the pattern's,
	// one per set.
	var own [runMaxTables]struct {
		cs    asciiBits
		table *[256]uint8
	}
	tables := 0
	for i := range p.steps {
		st := &p.steps[i]
		if st.mark >= 0 || st.lit != "" {
			continue
		}
		cs := b.bits[i]
		switch n := cs.count(); {
		case n == 0:
			st.set = runByteTable(0x80)
			continue
		case n == 1:
			st.set = runByteTable(cs.first())
			continue
		}
		k := 0
		for k < tables && own[k].cs != cs {
			k++
		}
		if k == tables {
			if tables == runMaxTables {
				return nil
			}
			t := new([256]uint8)
			for c := range 128 {
				t[c] = uint8(cs[c>>6] >> (c & 63) & 1)
			}
			own[k].cs, own[k].table = cs, t
			tables++
		}
		st.set = own[k].table
	}
	return p
}

// walk adds the items of n to the pattern and reports whether n is the shape
// of a run pattern.
func (b *runBuilder) walk(n *reNode) bool {
	if n.Op == reOpEmpty {
		return true
	}
	if b.ended || len(b.p.steps) > runMaxSteps {
		return false
	}
	switch n.Op {
	case reOpSeq:
		for _, s := range n.Subs {
			if !b.walk(s) {
				return false
			}
		}
		return true
	case reOpBegin:
		if n.Multiline || b.walked {
			return false
		}
		b.p.begin = true
	case reOpEnd:
		if n.Multiline {
			return false
		}
		b.p.end, b.ended = true, true
	case reOpCapture:
		b.mark(2 * n.Index)
		if !b.walk(n.Subs[0]) || b.ended {
			return false
		}
		b.mark(2*n.Index + 1)
	case reOpChar:
		cs := asciiBitsOf(n.Set)
		if cs.count() == 1 {
			b.lit = append(b.lit, byte(cs.first()))
		} else {
			b.step(runStep{min: 1, max: 1, mark: -1}, cs)
		}
	case reOpRepeat:
		if n.Subs[0].Op != reOpChar || !n.Greedy && n.Min != n.Max {
			return false
		}
		if n.Max != 0 {
			b.step(runStep{min: n.Min, max: n.Max, mark: -1}, asciiBitsOf(n.Subs[0].Set))
		}
	default:
		return false
	}
	b.walked = true
	return true
}

// flush ends the literal being collected.
func (b *runBuilder) flush() {
	if len(b.lit) > 0 {
		b.p.steps = append(b.p.steps, runStep{lit: string(b.lit), mark: -1})
		var cs asciiBits
		cs[b.lit[0]>>6] |= 1 << (b.lit[0] & 63)
		b.bits = append(b.bits, cs)
		b.lit = b.lit[:0]
	}
}

func (b *runBuilder) step(st runStep, cs asciiBits) {
	b.flush()
	b.p.steps = append(b.p.steps, st)
	b.bits = append(b.bits, cs)
}

func (b *runBuilder) mark(slot int) {
	b.flush()
	b.p.steps = append(b.p.steps, runStep{mark: slot})
	b.bits = append(b.bits, asciiBits{})
	b.walked = true
}

func (cs asciiBits) count() int { return bits.OnesCount64(cs[0]) + bits.OnesCount64(cs[1]) }

func (cs asciiBits) first() int {
	if cs[0] != 0 {
		return bits.TrailingZeros64(cs[0])
	}
	return 64 + bits.TrailingZeros64(cs[1])
}

// match matches sub from pos (anchored there when sticky; a pattern with ^
// can only match at 0) and returns the match's capture slots, nil for no
// match; test is match without the slots. handled is false for a subject
// that is not ASCII, a pattern with more than seven groups and a search
// that is not sticky with a pattern without ^: the caller runs the other
// engines.
func (p *runPattern) match(r *Realm, sub *reSubject, pos int, sticky bool) (m []int, handled bool, err error) {
	if !sub.ascii || p.slots > runMaxSlots {
		return nil, false, nil
	}
	if !p.anchoredAt(pos, sticky) {
		return nil, p.begin, nil
	}
	// The marks go to the stack first: a failed match allocates nothing.
	var buf [runMaxSlots]int
	marks := buf[:p.slots]
	for i := 2; i < len(marks); i++ {
		marks[i] = -1
	}
	end, err := p.run(r, sub.text, pos, marks)
	if end < 0 || err != nil {
		return nil, true, err
	}
	m = make([]int, p.slots)
	copy(m, marks)
	m[0], m[1] = pos, end
	return m, true, nil
}

func (p *runPattern) test(r *Realm, text []byte) (bool, error) {
	end, err := p.run(r, text, 0, nil)
	return end >= 0, err
}

// anchoredAt reports whether a match can start at pos: at 0 with ^, at pos
// for a sticky search.
func (p *runPattern) anchoredAt(pos int, sticky bool) bool {
	if p.begin {
		return pos == 0
	}
	return sticky
}

// run runs the steps from pos and returns the match's end, or -1; it records
// the capture marks in m when m is not nil.
func (p *runPattern) run(r *Realm, text []byte, pos int, m []int) (int, error) {
	for i := range p.steps {
		st := &p.steps[i]
		if st.mark >= 0 {
			if m != nil {
				m[st.mark] = pos
			}
			continue
		}
		if st.lit != "" {
			if len(text)-pos < len(st.lit) || string(text[pos:pos+len(st.lit)]) != st.lit {
				return -1, nil
			}
			pos += len(st.lit)
			continue
		}
		n := 0
		if st.max == 1 { // a character, maybe optional: no scan
			if pos < len(text) {
				n = int(st.set[text[pos]])
			}
		} else {
			limit := len(text) - pos
			if st.max >= 0 && st.max < limit {
				limit = st.max
			}
			if limit > 0 {
				var err error
				if n, err = scanRun(r, st.set, text[pos:pos+limit]); err != nil {
					return -1, err
				}
			}
		}
		if n < st.min {
			return -1, nil
		}
		pos += n
	}
	if p.end && pos != len(text) {
		return -1, nil
	}
	return pos, nil
}

// scanRun returns the length of the prefix of b whose bytes are all in set,
// testing eight at a time, and checks the interrupt every runChunk bytes.
func scanRun(r *Realm, set *[256]uint8, b []byte) (int, error) {
	n := 0
	for {
		c := b[:min(len(b), runChunk)]
		k := 0
		for ; k+8 <= len(c); k += 8 {
			w := c[k : k+8 : k+8]
			if set[w[0]]&set[w[1]]&set[w[2]]&set[w[3]]&set[w[4]]&set[w[5]]&set[w[6]]&set[w[7]] == 0 {
				break
			}
		}
		for ; k < len(c) && set[c[k]] != 0; k++ {
		}
		n += k
		if k < len(c) || len(c) == len(b) {
			return n, nil
		}
		b = b[len(c):]
		if err := r.CheckInterrupt(); err != nil {
			return n, err
		}
	}
}
