package engine

import "strconv"

// JavaScript regular expressions run on Go's regexp package (RE2) when the
// translation is exact. parseRegExp (internal/regexpsyntax) turns
// the pattern into a reNode tree whose character nodes already hold the
// exact set they match (case closure, class complement and Unicode
// properties resolved from the Unicode tables of internal/regexpsyntax); the
// emitter below writes that tree as an RE2 pattern with explicit ranges, so
// RE2 never applies its own case folding or Unicode tables.
//
// UTF-16 semantics on a UTF-8 engine: the subject is matched as a sequence
// of "characters" that are code units (non-u mode) or code points (u mode).
// Surrogate code units that must stay individual characters (every one in
// non-u mode, lone ones in u mode) are encoded in the working copy as the
// private-use runes surrogateBase+k, and the emitter maps the surrogates of
// every set to the same runes, so `.`, negated classes and quantifiers count
// exactly as JavaScript does.
//
// Where RE2 and JavaScript differ on a subject, that subject runs on the
// backtracking VM instead (compiledRegExp.useBT):
//   - `m` mode `^`/`$` recognise only \n as a line terminator (RE2 rule),
//     JavaScript also recognises \r, U+2028 and U+2029;
//   - with u and i, `\b` and `\B` use ASCII word characters (RE2 rule);
//     JavaScript adds U+017F and U+212A;
//   - a subject containing real private-use characters U+F0000..U+F07FF is
//     indistinguishable from surrogate code units in the working copy.
// Patterns RE2 cannot run exactly at all (reExact) never reach the emitter.

// surrogateBase is the private-use rune standing for surrogate code unit
// 0xD800 in the matcher's working copy (0xD800+k <-> surrogateBase+k).
const surrogateBase = 0xF0000

// isSurrogateUnit reports whether r is a UTF-16 surrogate code unit value.
func isSurrogateUnit(r rune) bool { return r >= 0xD800 && r < 0xE000 }

// surrogateRune maps a surrogate code unit to its working-copy rune.
func surrogateRune(c rune) rune { return surrogateBase + (c - 0xD800) }

// regexpFlags is the parsed flag set of a RegExp object. The v flag sets
// both unicodeSets and unicode: unicode means "characters are code points"
// everywhere inside the engine, and only the unicode getter and the flags
// text tell u from v.
type regexpFlags struct {
	global, ignoreCase, multiline, dotAll, unicode, sticky, hasIndices, unicodeSets bool
}

// parseRegExpFlags parses "dgimsuvy" flags; ok is false on unknown or
// repeated flags and on u together with v.
func parseRegExpFlags(f string) (regexpFlags, bool) {
	var fl regexpFlags
	for i := range len(f) {
		var p *bool
		switch f[i] {
		case 'd':
			p = &fl.hasIndices
		case 'g':
			p = &fl.global
		case 'i':
			p = &fl.ignoreCase
		case 'm':
			p = &fl.multiline
		case 's':
			p = &fl.dotAll
		case 'u':
			p = &fl.unicode
		case 'v':
			p = &fl.unicodeSets
		case 'y':
			p = &fl.sticky
		default:
			return fl, false
		}
		if *p {
			return fl, false
		}
		*p = true
	}
	if fl.unicodeSets {
		if fl.unicode {
			return fl, false
		}
		fl.unicode = true
	}
	return fl, true
}

// String renders the flags in the canonical "dgimsuvy" order of the spec's
// RegExp.prototype.flags getter.
func (f regexpFlags) String() string {
	var b [8]byte
	n := 0
	for _, c := range [...]struct {
		on bool
		ch byte
	}{{f.hasIndices, 'd'}, {f.global, 'g'}, {f.ignoreCase, 'i'}, {f.multiline, 'm'}, {f.dotAll, 's'}, {f.unicode && !f.unicodeSets, 'u'}, {f.unicodeSets, 'v'}, {f.sticky, 'y'}} {
		if c.on {
			b[n] = c.ch
			n++
		}
	}
	return string(b[:n])
}

// regexpUnsupportedError reports a pattern feature that is not implemented.
// The emitter returns it for constructs RE2 cannot express, which
// compilePattern never hands it (reExact sends them to the backtracking VM).
type regexpUnsupportedError struct{ feature string }

func (e *regexpUnsupportedError) Error() string {
	return "regular expression feature " + e.feature + " is not supported yet (see TODO.md)"
}

// translatedRegExp is the output of translateRegExp.
type translatedRegExp struct {
	body     string   // RE2 pattern
	names    []string // capture names by group index (index 0 unused); "" for unnamed
	hasNames bool
	dupNames bool // a name is shared by groups in different alternatives
	minLen0  bool // the pattern can match the empty string
	// leftContext is set when the pattern looks at the text before the match
	// start (^, \b, \B; RE2 has no lookbehind), so a search from a non-zero
	// position needs the context variant instead of a slice (matchAt).
	leftContext bool
}

// translateRegExp converts a JavaScript pattern (as UTF-16 code units) to RE2
// syntax. Errors are *regexpUnsupportedError or *regexpInvalidError.
func translateRegExp(pattern []uint16, flags regexpFlags) (*translatedRegExp, error) {
	ast, err := parseRegExp(pattern, flags)
	if err != nil {
		return nil, err
	}
	return emitRE2(ast, flags, len(pattern))
}

// emitRE2 writes a parsed pattern as RE2 syntax; sizeHint is the pattern
// length.
func emitRE2(ast *reAST, flags regexpFlags, sizeHint int) (*translatedRegExp, error) {
	e := &re2Emitter{u: flags.unicode, out: make([]byte, 0, 2*sizeHint+16)}
	if err := e.node(ast.Root, true); err != nil {
		return nil, err
	}
	tr := &translatedRegExp{body: string(e.out), names: ast.Names, hasNames: ast.HasNames, dupNames: ast.DupNames, minLen0: ast.Root.CanBeEmpty()}
	ast.Root.Walk(func(n *reNode) {
		switch n.Op {
		case reOpBegin, reOpWordB, reOpNotWordB:
			tr.leftContext = true
		}
	})
	return tr, nil
}

// reMaxRE2Repeat is RE2's largest repeat count.
const reMaxRE2Repeat = 1000

// re2Emitter appends the RE2 form of a reNode tree to out.
type re2Emitter struct {
	u   bool
	out []byte
}

// node emits n. bare is set where an alternation needs no group around it
// (the whole pattern and a capture body).
func (e *re2Emitter) node(n *reNode, bare bool) error {
	switch n.Op {
	case reOpEmpty:
	case reOpChar:
		e.out = appendRE2Set(e.out, n.Set, e.u)
	case reOpSeq:
		for _, s := range n.Subs {
			if err := e.node(s, false); err != nil {
				return err
			}
		}
	case reOpAlt:
		if !bare {
			e.out = append(e.out, "(?:"...)
		}
		for i, s := range n.Subs {
			if i > 0 {
				e.out = append(e.out, '|')
			}
			if err := e.node(s, true); err != nil {
				return err
			}
		}
		if !bare {
			e.out = append(e.out, ')')
		}
	case reOpCapture:
		e.out = append(e.out, '(')
		if err := e.node(n.Subs[0], true); err != nil {
			return err
		}
		e.out = append(e.out, ')')
	case reOpRepeat:
		return e.repeat(n)
	case reOpBegin:
		if n.Multiline {
			e.out = append(e.out, "(?m:^)"...)
		} else {
			e.out = append(e.out, `\A`...)
		}
	case reOpEnd:
		if n.Multiline {
			e.out = append(e.out, "(?m:$)"...)
		} else {
			e.out = append(e.out, `\z`...)
		}
	case reOpWordB:
		e.out = append(e.out, `\b`...)
	case reOpNotWordB:
		e.out = append(e.out, `\B`...)
	case reOpLook:
		if n.Behind {
			return &regexpUnsupportedError{"lookbehind"}
		}
		return &regexpUnsupportedError{"lookahead"}
	case reOpBackref:
		if n.Name != "" {
			return &regexpUnsupportedError{"named backreferences"}
		}
		return &regexpUnsupportedError{"backreferences"}
	}
	return nil
}

func (e *re2Emitter) repeat(n *reNode) error {
	if n.Min > reMaxRE2Repeat || n.Max > reMaxRE2Repeat {
		return &regexpUnsupportedError{"repeat count above 1000"}
	}
	// A character or a capture is one RE2 atom; anything else is grouped
	// (RE2 rejects a repeated repeat such as "a**").
	body := n.Subs[0]
	atom := body.Op == reOpChar || body.Op == reOpCapture
	if !atom {
		e.out = append(e.out, "(?:"...)
	}
	if err := e.node(body, true); err != nil {
		return err
	}
	if !atom {
		e.out = append(e.out, ')')
	}
	switch {
	case n.Min == 0 && n.Max == -1:
		e.out = append(e.out, '*')
	case n.Min == 1 && n.Max == -1:
		e.out = append(e.out, '+')
	case n.Min == 0 && n.Max == 1:
		e.out = append(e.out, '?')
	default:
		e.out = append(e.out, '{')
		e.out = strconv.AppendInt(e.out, int64(n.Min), 10)
		if n.Max != n.Min {
			e.out = append(e.out, ',')
			if n.Max != -1 {
				e.out = strconv.AppendInt(e.out, int64(n.Max), 10)
			}
		}
		e.out = append(e.out, '}')
	}
	if !n.Greedy {
		e.out = append(e.out, '?')
	}
	return nil
}

var (
	surrogateSet = runeSet{0xD800, 0xDFFF}
	// surrogateRuneSet is the working-copy image of surrogateSet.
	surrogateRuneSet = runeSet{surrogateBase, surrogateBase + 0x7FF}
	// re2DontCare lists the runes a working copy never contains: the
	// surrogates themselves (always remapped) and, without u, every code
	// point outside the BMP except the remapped surrogates.
	re2DontCareUnicode = surrogateSet
	re2DontCareUnits   = runeSet{0xD800, 0xDFFF, 0x10000, surrogateBase - 1, surrogateBase + 0x800, maxCodePoint}
)

// appendRE2Set appends a character matching exactly the characters of set
// (code units without u, code points with u) in the working copy: the
// surrogates of set become their working-copy runes, and the shorter of the
// class and its complement is written, ignoring the runes a working copy
// never contains.
func appendRE2Set(b []byte, set runeSet, unicodeMode bool) []byte {
	m := set
	if len(set.Intersect(surrogateSet)) != 0 || len(set.Intersect(surrogateRuneSet)) != 0 {
		var shifted runeSet
		for _, r := range set.Intersect(surrogateSet) {
			shifted = append(shifted, surrogateRune(r))
		}
		m = set.Subtract(surrogateSet).Subtract(surrogateRuneSet).Union(shifted)
	}
	if len(m) == 0 {
		return append(b, `[^\x00-\x{10ffff}]`...)
	}
	if r, ok := m.Single(); ok {
		return appendClassRune(b, r)
	}
	dc := re2DontCareUnits
	if unicodeMode {
		dc = re2DontCareUnicode
	}
	neg := m.Union(dc).Complement()
	if len(neg) == 0 {
		return append(b, `[\x00-\x{10ffff}]`...)
	}
	b = append(b, '[')
	ranges := m
	if len(neg) < len(m) {
		b = append(b, '^')
		ranges = neg
	}
	for i := 0; i < len(ranges); i += 2 {
		lo, hi := ranges[i], ranges[i+1]
		b = appendClassRune(b, lo)
		if hi != lo {
			if hi > lo+1 {
				b = append(b, '-')
			}
			b = appendClassRune(b, hi)
		}
	}
	return append(b, ']')
}

// appendClassRune appends one working-copy rune in a form RE2 never
// misreads, inside or outside a class.
func appendClassRune(b []byte, r rune) []byte {
	if r < 0x80 && (r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '_') {
		return append(b, byte(r))
	}
	b = append(b, `\x{`...)
	b = strconv.AppendInt(b, int64(r), 16)
	return append(b, '}')
}
