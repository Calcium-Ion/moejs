package regexpsyntax

import "slices"

// MaxCodePoint is the largest Unicode code point.
const MaxCodePoint = 0x10FFFF

// Set is a set of characters (code units or code points) as sorted,
// non-overlapping, non-adjacent inclusive ranges [lo0, hi0, lo1, hi1, ...].
// Every constructor below returns a normalized set; the set operations of the
// regexp parser (classes, v-flag difference and intersection, case closure)
// work on this form.
type Set []rune

// Normalize sorts the ranges and merges overlapping or adjacent ones.
func (s Set) Normalize() Set {
	if len(s) <= 2 {
		return s
	}
	type rng struct{ lo, hi rune }
	rs := make([]rng, len(s)/2)
	for i := range rs {
		rs[i] = rng{s[2*i], s[2*i+1]}
	}
	slices.SortFunc(rs, func(a, b rng) int { return int(a.lo - b.lo) })
	out := s[:0]
	for _, r := range rs {
		if n := len(out); n > 0 && r.lo <= out[n-1]+1 {
			out[n-1] = max(out[n-1], r.hi)
			continue
		}
		out = append(out, r.lo, r.hi)
	}
	return out
}

// Union returns s | t.
func (s Set) Union(t Set) Set {
	if len(t) == 0 {
		return s
	}
	if len(s) == 0 {
		return t
	}
	out := make(Set, 0, len(s)+len(t))
	out = append(out, s...)
	out = append(out, t...)
	return out.Normalize()
}

// Complement returns the characters in [0, MaxCodePoint] not in s.
func (s Set) Complement() Set {
	return s.ComplementIn(MaxCodePoint)
}

// ComplementIn returns the characters in [0, top] not in s.
func (s Set) ComplementIn(top rune) Set {
	out := make(Set, 0, len(s)+2)
	next := rune(0)
	for i := 0; i < len(s); i += 2 {
		if s[i] > top {
			break
		}
		if s[i] > next {
			out = append(out, next, s[i]-1)
		}
		next = s[i+1] + 1
	}
	if next <= top {
		out = append(out, next, top)
	}
	return out
}

// Intersect returns s & t.
func (s Set) Intersect(t Set) Set {
	var out Set
	i, j := 0, 0
	for i < len(s) && j < len(t) {
		lo := max(s[i], t[j])
		hi := min(s[i+1], t[j+1])
		if lo <= hi {
			out = append(out, lo, hi)
		}
		if s[i+1] < t[j+1] {
			i += 2
		} else {
			j += 2
		}
	}
	return out
}

// Subtract returns s minus t.
func (s Set) Subtract(t Set) Set {
	if len(t) == 0 || len(s) == 0 {
		return s
	}
	return s.Intersect(t.Complement())
}

// Has reports whether c is in s.
func (s Set) Has(c rune) bool {
	lo, hi := 0, len(s)/2
	for lo < hi {
		m := (lo + hi) >> 1
		switch {
		case c < s[2*m]:
			hi = m
		case c > s[2*m+1]:
			lo = m + 1
		default:
			return true
		}
	}
	return false
}

// Single reports whether s is exactly one character.
func (s Set) Single() (rune, bool) {
	if len(s) == 2 && s[0] == s[1] {
		return s[0], true
	}
	return 0, false
}

// Sets of the class escapes (ECMA-262 CharacterClassEscape).
var (
	digitSet = Set{'0', '9'}
	wordSet  = Set{'0', '9', 'A', 'Z', '_', '_', 'a', 'z'}
	// wordFoldSet is WordCharacters with u (or v) and i: the characters whose
	// Canonicalize is in wordSet, adding U+017F and U+212A.
	wordFoldSet = Set{'0', '9', 'A', 'Z', '_', '_', 'a', 'z', 0x17F, 0x17F, 0x212A, 0x212A}
	spaceSet    = Set{'\t', '\r', ' ', ' ', 0xA0, 0xA0, 0x1680, 0x1680, 0x2000, 0x200A,
		0x2028, 0x2029, 0x202F, 0x202F, 0x205F, 0x205F, 0x3000, 0x3000, 0xFEFF, 0xFEFF}
	// lineTerminatorSet is the complement of `.` without the s flag.
	lineTerminatorSet = Set{'\n', '\n', '\r', '\r', 0x2028, 0x2029}
)
