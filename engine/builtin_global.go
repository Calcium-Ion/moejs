package engine

import (
	"math"
	"strconv"
)

// installGlobal defines the global function properties parseInt, parseFloat,
// isNaN and isFinite. The value globals (globalThis, undefined, NaN,
// Infinity) are created by initGlobal. installNumber reuses the
// parseInt/parseFloat function objects created here, so installGlobal must
// run first.
func installGlobal(r *Realm) {
	// Shared realms bind intrinsics read-only (lockdown model, see
	// initGlobal); mutable realms keep the spec's writable, configurable
	// bindings.
	attrs := attrHidden
	if r.buildingShared {
		attrs = attrFrozen
	}
	g := r.Global
	g.ReserveSlots(r, len(globalFunctions))
	for _, d := range globalFunctions {
		fn := r.NewNativeFunction(d.name, d.length, d.fn)
		g.DefineOwnDataFast(r, StringKey(d.name), ObjectValue(fn), attrs)
	}
}

var globalFunctions = []builtinDef{
	{AtomParseInt, globalParseInt, 2},
	{AtomParseFloat, globalParseFloat, 1},
	{AtomIsNaN, globalIsNaN, 1},
	{AtomIsFinite, globalIsFinite, 1},
}

// globalParseInt implements parseInt(string, radix) (ES2023 19.2.5). The
// string is coerced before the radix, as the spec orders.
func globalParseInt(r *Realm, this Value, args []Value) (Value, error) {
	s, err := r.ToString(Arg(args, 0))
	if err != nil {
		return Undefined(), err
	}
	radix, err := r.ToInt32(Arg(args, 1))
	if err != nil {
		return Undefined(), err
	}
	return NumberValue(parseIntString(s, int(radix))), nil
}

// parseIntString runs the parseInt algorithm on already coerced arguments.
func parseIntString(s *String, radix int) float64 {
	n := s.Len()
	i := 0
	for i < n && isJSWhitespace(s.At(i)) {
		i++
	}
	neg := false
	if i < n {
		switch s.At(i) {
		case '-':
			neg = true
			i++
		case '+':
			i++
		}
	}
	stripPrefix := true
	if radix != 0 {
		if radix < 2 || radix > 36 {
			return math.NaN()
		}
		if radix != 16 {
			stripPrefix = false
		}
	} else {
		radix = 10
	}
	if stripPrefix && i+1 < n && s.At(i) == '0' && s.At(i+1)|0x20 == 'x' {
		i += 2
		radix = 16
	}
	start := i
	for i < n {
		c := s.At(i)
		if c >= 0x80 {
			break
		}
		if d := digitValue(byte(c)); d < 0 || d >= radix {
			break
		}
		i++
	}
	if i == start {
		return math.NaN()
	}
	v := radixDigitsToFloat(s, start, i, radix)
	if neg {
		return -v // -0 when the digits are all zero, as the spec requires
	}
	return v
}

// radixDigitsToFloat converts the digits s[start:end] (all valid for radix)
// to the nearest number: exactly through uint64 while the value fits in 53
// bits, otherwise through radixToFloat.
func radixDigitsToFloat(s *String, start, end, radix int) float64 {
	const maxExact = 1<<53 - 1
	var v uint64
	i := start
	for ; i < end; i++ {
		d := uint64(digitValue(byte(s.At(i))))
		if v > (maxExact-d)/uint64(radix) {
			break
		}
		v = v*uint64(radix) + d
	}
	if i == end {
		return float64(v)
	}
	for s.At(start) == '0' { // a nonzero digit follows: v overflowed
		start++
	}
	if end-start > maxFloatDigits {
		return posInf
	}
	digits := make([]byte, end-start)
	for j := range digits {
		digits[j] = byte(s.At(start + j))
	}
	return radixToFloat(bytesToString(digits), radix)
}

// globalParseFloat implements parseFloat(string) (ES2023 19.2.4).
func globalParseFloat(r *Realm, this Value, args []Value) (Value, error) {
	s, err := r.ToString(Arg(args, 0))
	if err != nil {
		return Undefined(), err
	}
	return NumberValue(parseFloatString(s)), nil
}

// parseFloatString returns the value of the longest prefix of s (after
// leading whitespace) that is a StrDecimalLiteral, or NaN when there is none.
func parseFloatString(s *String) float64 {
	n := s.Len()
	i := 0
	for i < n && isJSWhitespace(s.At(i)) {
		i++
	}
	start := i
	if i < n && (s.At(i) == '+' || s.At(i) == '-') {
		i++
	}
	if hasASCIIPrefixAt(s, i, "Infinity") {
		if s.At(start) == '-' {
			return negInf
		}
		return posInf
	}
	digits := 0
	for i < n && isASCIIDigit(s.At(i)) {
		i++
		digits++
	}
	if i < n && s.At(i) == '.' {
		j := i + 1
		frac := 0
		for j < n && isASCIIDigit(s.At(j)) {
			j++
			frac++
		}
		if digits+frac > 0 {
			i = j
			digits += frac
		}
	}
	if digits == 0 {
		return math.NaN()
	}
	if i < n && s.At(i)|0x20 == 'e' {
		j := i + 1
		if j < n && (s.At(j) == '+' || s.At(j) == '-') {
			j++
		}
		k := j
		for k < n && isASCIIDigit(s.At(k)) {
			k++
		}
		if k > j {
			i = k
		}
	}
	var lit string
	if a, ok := s.ASCII(); ok {
		lit = a[start:i]
	} else {
		lit, _ = s.Substring(start, i).ASCII() // the scanned prefix is ASCII by construction
	}
	f, _ := strconv.ParseFloat(lit, 64) // ErrRange yields ±Inf or 0, which is right
	return f
}

func isASCIIDigit(c uint16) bool { return c >= '0' && c <= '9' }

// hasASCIIPrefixAt reports whether s[i:] starts with the ASCII string p.
func hasASCIIPrefixAt(s *String, i int, p string) bool {
	if i+len(p) > s.Len() {
		return false
	}
	for j := range len(p) {
		if s.At(i+j) != uint16(p[j]) {
			return false
		}
	}
	return true
}

// globalIsNaN implements isNaN(number): ToNumber, then the NaN test.
func globalIsNaN(r *Realm, this Value, args []Value) (Value, error) {
	f, err := r.ToNumber(Arg(args, 0))
	if err != nil {
		return Undefined(), err
	}
	return Bool(f != f), nil
}

// globalIsFinite implements isFinite(number): ToNumber, then the finite test.
func globalIsFinite(r *Realm, this Value, args []Value) (Value, error) {
	f, err := r.ToNumber(Arg(args, 0))
	if err != nil {
		return Undefined(), err
	}
	return Bool(!math.IsNaN(f) && !math.IsInf(f, 0)), nil
}
