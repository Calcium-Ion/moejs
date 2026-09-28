package syntax

import (
	"math/big"
	"math/rand/v2"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lexAll tokenises src and returns the token kinds and their source text.
func lexAll(t *testing.T, src string) (toks []Token, texts []string) {
	t.Helper()
	l := &lexer{}
	l.init(newFile("t.js", src))
	for {
		l.next()
		toks = append(toks, l.tok)
		texts = append(texts, l.text())
		if l.tok == EOF {
			return toks, texts
		}
	}
}

// lexOne scans the first token of src.
func lexOne(t *testing.T, src string) *lexer {
	t.Helper()
	l := &lexer{}
	l.init(newFile("t.js", src))
	l.next()
	return l
}

// lexError tokenises src and returns the first lexer error message, or "".
func lexError(src string) (msg string) {
	l := &lexer{strict: true} // rejecting the legacy literals
	l.init(newFile("t.js", src))
	defer func() {
		if r := recover(); r != nil {
			msg = l.err.Message()
		}
	}()
	for {
		l.next()
		if l.tok == EOF {
			return ""
		}
	}
}

func TestLexPunctuators(t *testing.T) {
	src := "{ } ( ) [ ] . ... ; , < > <= >= == != === !== + - * / % ** ++ -- << >> >>> & | ^ ! ~ && || ?? ? ?. : = += -= *= /= %= **= <<= >>= >>>= &= |= ^= &&= ||= ??= =>"
	toks, _ := lexAll(t, src)
	want := []Token{LBrace, RBrace, LParen, RParen, LBrack, RBrack, Dot, Ellipsis, Semicolon, Comma, Lt, Gt, LtEq, GtEq, Eq, NotEq, StrictEq, StrictNeq,
		Plus, Minus, Mul, Div, Rem, Exp, Inc, Dec, Shl, Shr, UShr, BitAnd, BitOr, BitXor, Not, BitNot, LogAnd, LogOr, Nullish, Question, QuestionDot, Colon,
		Assign, AddAssign, SubAssign, MulAssign, DivAssign, RemAssign, ExpAssign, ShlAssign, ShrAssign, UShrAssign, AndAssign, OrAssign, XorAssign,
		LogAndAssign, LogOrAssign, NullishAssign, Arrow, EOF}
	assert.Equal(t, want, toks)
}

func TestLexConditionalWithFraction(t *testing.T) {
	toks, _ := lexAll(t, "a?.5:b")
	assert.Equal(t, []Token{Identifier, Question, Number, Colon, Identifier, EOF}, toks)
}

func TestLexKeywords(t *testing.T) {
	for tok := keywordStart + 1; tok < keywordEnd; tok++ {
		assert.Equal(t, tok, lookupKeyword(tokenNames[tok]), tokenNames[tok])
		assert.True(t, tok.IsKeyword())
	}
	toks, _ := lexAll(t, "if else let await yield static of async get set")
	assert.Equal(t, []Token{KwIf, KwElse, KwLet, KwAwait, KwYield, KwStatic, Identifier, Identifier, Identifier, Identifier, EOF}, toks)
	assert.False(t, Identifier.IsKeyword())
}

func TestLexNumbers(t *testing.T) {
	tests := []struct {
		src  string
		want float64
	}{
		{"0", 0}, {"42", 42}, {"3.14", 3.14}, {".5", 0.5}, {"5.", 5}, {"1e3", 1000}, {"1E-3", 0.001}, {"2.5e+2", 250},
		{"0x1F", 31}, {"0X1f", 31}, {"0b101", 5}, {"0B11", 3}, {"0o17", 15}, {"0O7", 7},
		{"1_000_000", 1e6}, {"0xFF_FF", 65535}, {"1_0.5_5", 10.55}, {"1e1_0", 1e10},
		{"0xFFFFFFFFFFFFFFFFF", 2.9514790517935283e+20}, {"9007199254740993", 9007199254740992},
		{"1e400", inf()}, {"0.0000001", 1e-7},
	}
	for _, tt := range tests {
		l := lexOne(t, tt.src)
		assert.Equal(t, Number, l.tok, tt.src)
		assert.Equal(t, tt.want, l.num, tt.src)
		assert.Equal(t, len(tt.src), l.end, tt.src)
	}
}

func inf() float64 { return 1 / zero }

var zero float64

func TestLexBigInt(t *testing.T) {
	tests := map[string]string{"1n": "1", "0n": "0", "0x10n": "16", "0b11n": "3", "0o7n": "7", "1_000n": "1000", "0xFFFFFFFFFFFFFFFFn": "18446744073709551615", "0xFFFFFFFFFFFFFFFFFn": "0xfffffffffffffffff", "0b1_" + strings.Repeat("0", 64) + "n": "0x10000000000000000"}
	for src, want := range tests {
		l := lexOne(t, src)
		assert.Equal(t, BigInt, l.tok, src)
		assert.Equal(t, want, l.val, src)
	}
}

// lexAlloc scans the first token of src and returns the bytes scanning it
// allocated.
func lexAlloc(t *testing.T, src string) (*lexer, uint64) {
	t.Helper()
	l := &lexer{}
	l.init(newFile("t.js", src))
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	l.next()
	runtime.ReadMemStats(&after)
	return l, after.TotalAlloc - before.TotalAlloc
}

// A 0x/0o/0b literal is lexed in linear time: a Number past 2^1024 is
// Infinity without converting its digits, and a BigInt is packed from its
// digits rather than multiplied up one digit at a time.
func TestLexRadixLongLiterals(t *testing.T) {
	radixes := []struct {
		prefix string
		base   int
		top    string
	}{{"0x", 16, "f"}, {"0o", 8, "7"}, {"0b", 2, "1"}}
	for _, r := range radixes {
		for _, src := range []string{
			r.prefix + strings.Repeat(r.top, 400000),
			r.prefix + strings.Repeat(r.top+"_", 200000) + r.top,
			r.prefix + strings.Repeat("0", 400000) + strings.Repeat(r.top, 1100),
		} {
			l, alloc := lexAlloc(t, src)
			assert.Equal(t, Number, l.tok, src[:8])
			assert.Equal(t, inf(), l.num, src[:8])
			assert.Less(t, alloc, uint64(16<<10), src[:8])
		}
		l, alloc := lexAlloc(t, r.prefix+strings.Repeat("0", 400000)+"1")
		assert.Equal(t, 1.0, l.num, r.prefix)
		assert.Less(t, alloc, uint64(16<<10), r.prefix)

		// The largest BigInt, 2^(2^20)-1, costs about its hex text, which
		// it keeps instead of decimal (whose conversions are superlinear).
		want := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), maxBigIntBits), big.NewInt(1))
		wantText := "0x" + want.Text(16)
		l, alloc = lexAlloc(t, r.prefix+strings.Repeat("0", 1000)+want.Text(r.base)+"n")
		assert.Equal(t, BigInt, l.tok, r.prefix)
		assert.True(t, l.val == wantText, r.prefix)
		assert.Less(t, alloc, uint64(4*len(wantText)), r.prefix)
	}

	rng := rand.New(rand.NewPCG(1, 2))
	for range 2000 {
		r := radixes[rng.IntN(len(radixes))]
		n := 1 + rng.IntN(400)
		if rng.IntN(4) == 0 {
			n = 1 + rng.IntN(4000)
		}
		var digits, lit strings.Builder
		lit.WriteString(r.prefix)
		lit.WriteString(strings.Repeat("0", rng.IntN(3)*10))
		for i := range n {
			d := "0123456789abcdef"[rng.IntN(r.base)]
			switch {
			case i < 3 && rng.IntN(2) == 0:
				d = r.top[0]
			case i < 3 && rng.IntN(2) == 0:
				d = '0'
			}
			if i > 0 && rng.IntN(8) == 0 {
				lit.WriteByte('_')
			}
			digits.WriteByte(d)
			lit.WriteByte(d)
		}
		want, ok := new(big.Int).SetString(digits.String(), r.base)
		require.True(t, ok)
		wantNum, _ := new(big.Float).SetInt(want).Float64()
		l := lexOne(t, lit.String())
		assert.Equal(t, Number, l.tok, lit.String())
		assert.Equal(t, wantNum, l.num, lit.String())
		l = lexOne(t, lit.String()+"n")
		assert.Equal(t, BigInt, l.tok, lit.String())
		wantText := want.String()
		if !want.IsUint64() {
			wantText = "0x" + want.Text(16)
		}
		assert.Equal(t, wantText, l.val, lit.String())
	}
	// Around the largest finite double and the halfway point above it.
	two1024 := new(big.Int).Lsh(big.NewInt(1), 1024)
	half := new(big.Int).Sub(two1024, new(big.Int).Lsh(big.NewInt(1), 970))
	for _, v := range []*big.Int{half, new(big.Int).Sub(half, big.NewInt(1)), two1024, new(big.Int).Sub(two1024, big.NewInt(1))} {
		want, _ := new(big.Float).SetInt(v).Float64()
		for _, r := range radixes {
			assert.Equal(t, want, lexOne(t, r.prefix+v.Text(r.base)).num, r.prefix+v.Text(16))
		}
	}
}

// A BigInt literal is held to the size limit every BigInt operation keeps
// to (SpiderMonkey's 2^20 bits, which rejects larger literals too).
func TestLexBigIntSizeLimit(t *testing.T) {
	one := big.NewInt(1)
	limit := new(big.Int).Lsh(one, maxBigIntBits)
	largest := new(big.Int).Sub(limit, one)
	assert.Len(t, limit.String(), maxBigIntDigits)
	for _, v := range []*big.Int{largest, new(big.Int).Exp(big.NewInt(10), big.NewInt(maxBigIntDigits-1), nil)} {
		for _, base := range []int{10, 16, 8, 2} {
			prefix := map[int]string{10: "", 16: "0x", 8: "0o", 2: "0b"}[base]
			l := lexOne(t, prefix+v.Text(base)+"n")
			assert.Equal(t, BigInt, l.tok, "%s %d", prefix, v.BitLen())
			want := v.String()
			if base != 10 {
				want = "0x" + v.Text(16)
			}
			assert.True(t, l.val == want, "%s %d", prefix, v.BitLen())
		}
	}
	const msg = "Maximum BigInt size exceeded"
	for _, src := range []string{
		limit.String() + "n",
		strings.Repeat("9", maxBigIntDigits) + "n",
		"1" + strings.Repeat("0", maxBigIntDigits) + "n",
		"1" + strings.Repeat("0", 400000) + "n",
		"1_" + strings.Repeat("0", maxBigIntDigits-1) + "0n",
		"0x" + limit.Text(16) + "n",
		"0o" + limit.Text(8) + "n",
		"0b" + limit.Text(2) + "n",
		"0b1_" + strings.Repeat("0", maxBigIntBits) + "n",
		"0x" + strings.Repeat("f", 400000) + "n",
	} {
		assert.Equal(t, msg, lexError(src), src[:8])
	}
	// The same digits are a Number.
	assert.Equal(t, "", lexError("0x"+limit.Text(16)))
	assert.Equal(t, "", lexError(limit.String()))
}

func TestLexNumberErrors(t *testing.T) {
	tests := map[string]string{
		"010":      "Octal literals are not allowed in strict mode.",
		"08":       "Decimals with leading zeros are not allowed in strict mode.",
		"1__0":     "Only one underscore is allowed as numeric separator",
		"1_":       "Numeric separators are not allowed at the end of numeric literals",
		"0_1":      "Numeric separator can not be used after leading 0.",
		"1._5":     "Numeric separators are not allowed here.",
		"0x_1":     "Numeric separators are not allowed here.",
		"1.5n":     "Invalid BigInt literal",
		"3in x":    "Identifier directly after number",
		"0xg":      "Invalid or unexpected token",
		"0b12":     "Invalid or unexpected token",
		"1e":       "Invalid or unexpected token",
		"1e+":      "Invalid or unexpected token",
		"1\\u0061": "Identifier directly after number",
	}
	for src, want := range tests {
		assert.Equal(t, want, lexError(src), src)
	}
}

func TestLexStrings(t *testing.T) {
	tests := []struct{ src, want string }{
		{`"abc"`, "abc"},
		{`'it\'s'`, "it's"},
		{`"\n\t\r\b\f\v\0"`, "\n\t\r\b\f\v\x00"},
		{`"\x41B\u{43}\u{1F600}"`, "ABC😀"},
		{"\"a\\\nb\"", "ab"},
		{"\"a\\\r\nb\"", "ab"},
		{"\"a\\ b\"", "ab"},
		{`"\q\'\"\\"`, `q'"\`},
		{`" "`, " "},
		{"\" \"", " "},
		{`"é中"`, "é中"},
		{`"\0a"`, "\x00a"},
		{`""`, ""},
	}
	for _, tt := range tests {
		l := lexOne(t, tt.src)
		assert.Equal(t, String, l.tok, tt.src)
		assert.Equal(t, tt.want, l.val, tt.src)
	}
}

func TestLexLoneSurrogates(t *testing.T) {
	l := lexOne(t, `"\uD83D"`)
	assert.Equal(t, []uint16{0xD83D}, DecodeWTF8(l.val))
	assert.False(t, IsASCII(l.val))

	l = lexOne(t, `"😀"`)
	assert.Equal(t, "😀", l.val)
	assert.Equal(t, []uint16{0xD83D, 0xDE00}, DecodeWTF8(l.val))

	l = lexOne(t, `"\uDE00\uD83D"`)
	assert.Equal(t, []uint16{0xDE00, 0xD83D}, DecodeWTF8(l.val))

	l = lexOne(t, `"a\uD800b\u{10FFFF}"`)
	assert.Equal(t, []uint16{'a', 0xD800, 'b', 0xDBFF, 0xDFFF}, DecodeWTF8(l.val))

	assert.Equal(t, []uint16{'h', 'i'}, DecodeWTF8("hi"))
	assert.True(t, IsASCII("hi"))
	assert.Equal(t, []uint16{0xE9, 0x4E2D}, DecodeWTF8("é中"))
	// Other invalid UTF-8 reads as U+FFFD, a byte at a time.
	assert.Equal(t, []uint16{0xFFFD, 0xFFFD, 'a', 0xFFFD, 0xFFFD}, DecodeWTF8("\xed\xa0a\xed\xa0"))
	assert.Equal(t, []uint16{0xFFFD, 'x'}, DecodeWTF8("\xffx"))
}

// TestLexLegacyLiterals covers the legacy octal and leading-zero numbers
// and the octal and \8/\9 escapes of sloppy code: their values, and the
// kind and position strict code reports.
func TestLexLegacyLiterals(t *testing.T) {
	numbers := []struct {
		src  string
		want float64
		kind uint8
	}{
		{"010", 8, legacyOctal},
		{"0777", 511, legacyOctal},
		{"00", 0, legacyOctal},
		{"08", 8, legacyDecimal},
		{"09.5", 9.5, legacyDecimal},
		{"019e1", 190, legacyDecimal},
		{"07777777777777777777777", 0o7777777777777777777777, legacyOctal},
		{"0", 0, legacyNone},
		{"0.5", 0.5, legacyNone},
	}
	for _, tt := range numbers {
		l := lexOne(t, tt.src)
		require.Equal(t, Number, l.tok, tt.src)
		assert.Equal(t, tt.want, l.num, tt.src)
		assert.Equal(t, tt.kind, l.legacy, tt.src)
	}
	strs := []struct {
		src  string
		want string
		kind uint8
		pos  int
	}{
		{`"\101"`, "A", legacyEscape, 1},
		{`"\0"`, "\x00", legacyNone, 0},
		{`"\08"`, "\x008", legacyEscape, 1},
		{`"\00"`, "\x00", legacyEscape, 1},
		{`"\377"`, "\u00ff", legacyEscape, 1},
		{`"\400"`, " 0", legacyEscape, 1},
		{`"ab\7"`, "ab\a", legacyEscape, 3},
		{`"\8"`, "8", legacyEscape89, 1},
		{`"x\9\1"`, "x9\x01", legacyEscape89, 2},
	}
	for _, tt := range strs {
		l := lexOne(t, tt.src)
		require.Equal(t, String, l.tok, tt.src)
		assert.Equal(t, tt.want, l.val, tt.src)
		assert.Equal(t, tt.kind, l.legacy, tt.src)
		if tt.kind != legacyNone {
			pos, _ := l.legacyError()
			assert.Equal(t, tt.pos, pos, tt.src)
		}
	}
	for src, want := range map[string]string{
		"08n":  "Invalid BigInt literal",
		"010n": "Invalid BigInt literal",
		"08_1": "Numeric separator can not be used after leading 0.",
		"01_1": "Numeric separator can not be used after leading 0.",
		"08in": "Identifier directly after number",
	} {
		l := &lexer{}
		l.init(newFile("t.js", src))
		msg := func() (msg string) {
			defer func() {
				if recover() != nil {
					msg = l.err.Message()
				}
			}()
			l.next()
			return ""
		}()
		assert.Equal(t, want, msg, src)
	}
}

func TestLexStringErrors(t *testing.T) {
	tests := map[string]string{
		`"abc`:         "Unterminated string constant",
		"\"a\nb\"":     "Unterminated string constant",
		`"\01"`:        "Octal escape sequences are not allowed in strict mode.",
		`"\1"`:         "Octal escape sequences are not allowed in strict mode.",
		`"\8"`:         "\\8 and \\9 are not allowed in strict mode.",
		`"\x4"`:        "Invalid hexadecimal escape sequence",
		`"\u12"`:       "Invalid Unicode escape sequence",
		`"\u{}"`:       "Invalid Unicode escape sequence",
		`"\u{110000}"`: "Undefined Unicode code-point",
		`"\u{41"`:      "Invalid Unicode escape sequence",
		"'\\":          "Unterminated string constant",
	}
	for src, want := range tests {
		assert.Equal(t, want, lexError(src), src)
	}
}

func TestLexTemplate(t *testing.T) {
	l := lexOne(t, "`a${b}c`")
	require.Equal(t, Template, l.tok)
	assert.Equal(t, "a", l.val)
	assert.Equal(t, "a", l.raw)
	assert.False(t, l.tail)
	l.next()
	assert.Equal(t, Identifier, l.tok)
	l.next()
	require.Equal(t, RBrace, l.tok)
	l.rescanTemplateContinuation()
	assert.Equal(t, Template, l.tok)
	assert.Equal(t, "c", l.val)
	assert.True(t, l.tail)
	l.next()
	assert.Equal(t, EOF, l.tok)

	l = lexOne(t, "`\\n\\u{41}\\x42`")
	assert.Equal(t, "\nAB", l.val)
	assert.Equal(t, `\n\u{41}\x42`, l.raw)
	assert.True(t, l.tail)

	l = lexOne(t, "`a\r\nb\rc\nd`")
	assert.Equal(t, "a\nb\nc\nd", l.val)
	assert.Equal(t, "a\nb\nc\nd", l.raw)
	assert.Equal(t, 4, l.file.LineCount())

	l = lexOne(t, "`x\\\r\ny`")
	assert.Equal(t, "xy", l.val)
	assert.Equal(t, "x\\\ny", l.raw)

	// Invalid escapes are recorded rather than reported: tagged templates
	// accept them with an undefined cooked value, and the parser reports
	// them for untagged ones.
	for _, c := range []struct{ src, raw, msg string }{
		{"`a\\01`", `a\01`, "Octal escape sequences are not allowed in template strings."},
		{"`\\1`", `\1`, "Octal escape sequences are not allowed in template strings."},
		{"`\\9`", `\9`, "\\8 and \\9 are not allowed in template strings."},
		{"`\\xg`", `\xg`, "Invalid hexadecimal escape sequence"},
		{"`\\x4`", `\x4`, "Invalid hexadecimal escape sequence"},
		{"`\\u00g`", `\u00g`, "Invalid Unicode escape sequence"},
		{"`\\u{`", `\u{`, "Invalid Unicode escape sequence"},
		{"`\\u{110000}`", `\u{110000}`, "Undefined Unicode code-point"},
		{"`\\uD800\\u`", `\uD800\u`, "Invalid Unicode escape sequence"},
		{"`\\n\\xz\\u0041`", `\n\xz\u0041`, "Invalid hexadecimal escape sequence"},
	} {
		l = lexOne(t, c.src)
		require.Equal(t, Template, l.tok, c.src)
		require.NotZero(t, l.badEscape, c.src)
		assert.Equal(t, c.raw, l.raw, c.src)
		assert.Equal(t, "", l.val, c.src)
		assert.Equal(t, c.msg, l.templateEscapeError(l.badEscape), c.src)
	}
	l = lexOne(t, "`\\01${x}\\0`")
	assert.Equal(t, 2, l.badEscape)
	l.next()
	l.next()
	l.rescanTemplateContinuation()
	assert.Zero(t, l.badEscape, "reset per chunk")
	assert.Equal(t, "\x00", l.val)
	assert.Equal(t, "Unterminated template literal", lexError("`abc"))
}

func TestLexIdentifiers(t *testing.T) {
	toks, texts := lexAll(t, "abc $x _y a1 ünïcödé 𠮷野家 a‍b")
	assert.Equal(t, []Token{Identifier, Identifier, Identifier, Identifier, Identifier, Identifier, Identifier, EOF}, toks)
	assert.Equal(t, []string{"abc", "$x", "_y", "a1", "ünïcödé", "𠮷野家", "a‍b", ""}, texts)

	l := lexOne(t, "\\u"+"0061bc = 1")
	assert.Equal(t, Identifier, l.tok)
	assert.Equal(t, "abc", l.val)
	assert.True(t, l.escaped)
	assert.Equal(t, 8, l.end)

	l = lexOne(t, `a\u{62}c`)
	assert.Equal(t, "abc", l.val)

	l = lexOne(t, "\\u"+"0069f (x) {}")
	assert.Equal(t, EscapedWord, l.tok, "the parser accepts it only as an IdentifierName")
	assert.Equal(t, "if", l.val)
	assert.True(t, l.escaped)

	assert.Equal(t, "Invalid Unicode escape sequence", lexError(`a\x`))
	assert.Equal(t, "Invalid or unexpected token", lexError("\\u"+"0031x"))
	assert.Equal(t, "Invalid or unexpected token", lexError("@"))
	assert.Equal(t, "Invalid or unexpected token", lexError("a # b"))
	assert.Equal(t, "Invalid or unexpected token", lexError("¿"))

	// ID_Start and ID_Continue follow the regexp tables' Unicode version
	// (17.0), not Go's: U+1C89 (16.0) and U+088F (17.0) start an identifier,
	// U+1ACF (17.0) continues one, and U+2E2F VERTICAL TILDE (Lm, but
	// Pattern_Syntax) does neither.
	toks, texts = lexAll(t, "\u1c89 \u088f a\u1acf")
	assert.Equal(t, []Token{Identifier, Identifier, Identifier, EOF}, toks)
	assert.Equal(t, []string{"\u1c89", "\u088f", "a\u1acf", ""}, texts)
	assert.Equal(t, "Invalid or unexpected token", lexError("\u2e2f"))
	assert.Equal(t, "Invalid or unexpected token", lexError("a\u2e2f"))
	assert.Equal(t, "Invalid or unexpected token", lexError("\\u2E2F"))
	assert.Equal(t, "Invalid Unicode escape sequence", lexError("a\\u2E2F"))
}

func TestLexPrivateName(t *testing.T) {
	l := lexOne(t, "#priv")
	assert.Equal(t, PrivateIdent, l.tok)
	assert.Equal(t, "priv", l.val)
	assert.Equal(t, 0, l.start)
	assert.Equal(t, 5, l.end)
}

func TestLexCommentsAndNewlines(t *testing.T) {
	l := lexOne(t, "a // c\nb")
	assert.Equal(t, "a", l.val)
	l.next()
	assert.Equal(t, "b", l.val)
	assert.True(t, l.nlBefore)

	l = lexOne(t, "a /* c */ b")
	l.next()
	assert.False(t, l.nlBefore)

	l = lexOne(t, "a /* \n */ b")
	l.next()
	assert.True(t, l.nlBefore)
	assert.Equal(t, 2, l.file.LineCount())

	l = lexOne(t, "a b")
	l.next()
	assert.True(t, l.nlBefore)

	l = lexOne(t, "a //  b")
	l.next()
	assert.Equal(t, "b", l.val)
	assert.True(t, l.nlBefore)

	l = lexOne(t, "#!/usr/bin/env node\nfoo")
	assert.Equal(t, Identifier, l.tok)
	assert.Equal(t, "foo", l.val)
	assert.True(t, l.nlBefore)

	l = lexOne(t, string(rune(0xFEFF))+" a"+string(rune(0xA0))+"b\tc")
	assert.Equal(t, "a", l.val)
	l.next()
	assert.Equal(t, "b", l.val)
	assert.False(t, l.nlBefore)

	assert.Equal(t, "Unterminated comment", lexError("/* x"))
	assert.Equal(t, "", lexError("/**/ /* a\nb */ // c"))
}

func TestLexRegex(t *testing.T) {
	s, err := ParseScript("t.js", `x = /a[/]b\/c/gi;`, Options{})
	require.NoError(t, err)
	re := s.Body[0].(*ExprStmt).X.(*AssignExpr).Value.(*RegexLit)
	assert.Equal(t, `a[/]b\/c`, re.Pattern)
	assert.Equal(t, "gi", re.Flags)
	assert.Equal(t, `/a[/]b\/c/gi`, s.File.Src[re.Pos:re.End])

	s, err = ParseScript("t.js", "x = /=/dgimsuy;", Options{})
	require.NoError(t, err)
	assert.Equal(t, "=", s.Body[0].(*ExprStmt).X.(*AssignExpr).Value.(*RegexLit).Pattern)
	s, err = ParseScript("t.js", "x = /[a&&b]/dgimsvy;", Options{})
	require.NoError(t, err)
	assert.Equal(t, "dgimsvy", s.Body[0].(*ExprStmt).X.(*AssignExpr).Value.(*RegexLit).Flags)

	for src, want := range map[string]string{
		"/a/x":    "Invalid regular expression flags",
		"/a/gg":   "Invalid regular expression flags",
		"/a/uv":   "Invalid regular expression flags",
		"/a/vgu":  "Invalid regular expression flags",
		"/a":      "Invalid regular expression: missing /",
		"/a\n/":   "Invalid regular expression: missing /",
		"/a\\\n/": "Invalid regular expression: missing /",
		"/[/":     "Invalid regular expression: missing /",
	} {
		_, err := ParseScript("t.js", src, Options{})
		require.Error(t, err, src)
		assert.Equal(t, want, err.(*Error).Message(), src)
	}
}

func TestFilePosition(t *testing.T) {
	src := "ab\ncd\r\nef gh\rij"
	_, texts := lexAll(t, src)
	assert.Equal(t, []string{"ab", "cd", "ef", "gh", "ij", ""}, texts)
	f := newFile("t.js", src)
	l := &lexer{}
	l.init(f)
	for l.next(); l.tok != EOF; l.next() {
	}
	assert.Equal(t, 5, f.LineCount())
	check := func(pos, line, col int) {
		gotLine, gotCol := f.Position(pos)
		assert.Equal(t, [2]int{line, col}, [2]int{gotLine, gotCol}, "pos %d", pos)
	}
	check(0, 1, 1)
	check(2, 1, 3)
	check(3, 2, 1)
	check(7, 3, 1)
	check(12, 4, 1) // after the 3-byte U+2028
	check(15, 5, 1)
	check(len(src), 5, 3)

	// Columns count code points, not bytes.
	f2 := newFile("t.js", "é𠮷x")
	line, col := f2.Position(6)
	assert.Equal(t, 1, line)
	assert.Equal(t, 3, col)
}

// TestLineSeparatorInLiteralStartsLine: U+2028 and U+2029 are line
// terminators for positions everywhere, including inside string and template
// literals, where they are legal characters (FuzzParse finding: the line
// table skipped them there, so `'<LS>'0` reported its error at 1:4).
func TestLineSeparatorInLiteralStartsLine(t *testing.T) {
	for _, src := range []string{
		"'\u2028'0",      // string fast path
		"'\\x41\u2029'0", // string slow path
		"`\u2028`0",      // template fast path
		"`\\n\u2029`0",   // template slow path
	} {
		_, err := ParseScript("t.js", src, Options{})
		require.Error(t, err, src)
		e := err.(*Error)
		assert.Equal(t, [2]int{2, 2}, [2]int{e.Line, e.Col}, "%q: %v", src, err)
	}
}
