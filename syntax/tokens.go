// Package syntax implements the moejs front end: a hand-written lexer, a
// recursive-descent parser producing a compact AST, strict-mode early
// errors, and a scope-resolution pass whose annotations the compiler
// consumes directly.
//
// Source is always strict-mode ECMAScript. ParseModule parses an ES module;
// ParseScript parses a strict-mode script for the runtime's RunString
// helper.
package syntax

import "strconv"

// Token is a lexical token kind. Punctuators and keywords each have their own
// kind so the parser switches on small integers rather than comparing text.
type Token uint8

// Token kinds. The order groups punctuators, then literals, then keywords so
// that IsKeyword is a range check.
const (
	EOF Token = iota
	Illegal

	// Punctuators.
	LBrace      // {
	RBrace      // }
	LParen      // (
	RParen      // )
	LBrack      // [
	RBrack      // ]
	Dot         // .
	Ellipsis    // ...
	Semicolon   // ;
	Comma       // ,
	Lt          // <
	Gt          // >
	LtEq        // <=
	GtEq        // >=
	Eq          // ==
	NotEq       // !=
	StrictEq    // ===
	StrictNeq   // !==
	Plus        // +
	Minus       // -
	Mul         // *
	Div         // /
	Rem         // %
	Exp         // **
	Inc         // ++
	Dec         // --
	Shl         // <<
	Shr         // >>
	UShr        // >>>
	BitAnd      // &
	BitOr       // |
	BitXor      // ^
	Not         // !
	BitNot      // ~
	LogAnd      // &&
	LogOr       // ||
	Nullish     // ??
	Question    // ?
	QuestionDot // ?.
	Colon       // :
	Assign      // =
	AddAssign   // +=
	SubAssign   // -=
	MulAssign   // *=
	DivAssign   // /=
	RemAssign   // %=
	ExpAssign   // **=
	ShlAssign   // <<=
	ShrAssign   // >>=
	UShrAssign  // >>>=
	AndAssign   // &=
	OrAssign    // |=
	XorAssign   // ^=
	LogAndAssign
	LogOrAssign
	NullishAssign
	Arrow // =>
	Hash  // # (private names, unsupported but tokenised for a clear error)

	// Literals and names.
	Identifier
	PrivateIdent
	EscapedWord // a reserved word spelled with \u escapes: only an IdentifierName
	Number
	BigInt
	String
	Template // a template chunk; see Lexer.template
	Regex

	// Keywords (reserved words) and contextual keywords. Everything from
	// keywordStart onward is spelled by an identifier-like sequence.
	keywordStart
	KwAwait
	KwBreak
	KwCase
	KwCatch
	KwClass
	KwConst
	KwContinue
	KwDebugger
	KwDefault
	KwDelete
	KwDo
	KwElse
	KwEnum
	KwExport
	KwExtends
	KwFalse
	KwFinally
	KwFor
	KwFunction
	KwIf
	KwImport
	KwIn
	KwInstanceof
	KwNew
	KwNull
	KwReturn
	KwSuper
	KwSwitch
	KwThis
	KwThrow
	KwTrue
	KwTry
	KwTypeof
	KwVar
	KwVoid
	KwWhile
	KwWith
	KwYield
	// Strict-mode future reserved words.
	KwImplements
	KwInterface
	KwLet
	KwPackage
	KwPrivate
	KwProtected
	KwPublic
	KwStatic
	keywordEnd

	tokenCount
)

var tokenNames = [...]string{
	EOF:           "end of input",
	Illegal:       "illegal token",
	LBrace:        "{",
	RBrace:        "}",
	LParen:        "(",
	RParen:        ")",
	LBrack:        "[",
	RBrack:        "]",
	Dot:           ".",
	Ellipsis:      "...",
	Semicolon:     ";",
	Comma:         ",",
	Lt:            "<",
	Gt:            ">",
	LtEq:          "<=",
	GtEq:          ">=",
	Eq:            "==",
	NotEq:         "!=",
	StrictEq:      "===",
	StrictNeq:     "!==",
	Plus:          "+",
	Minus:         "-",
	Mul:           "*",
	Div:           "/",
	Rem:           "%",
	Exp:           "**",
	Inc:           "++",
	Dec:           "--",
	Shl:           "<<",
	Shr:           ">>",
	UShr:          ">>>",
	BitAnd:        "&",
	BitOr:         "|",
	BitXor:        "^",
	Not:           "!",
	BitNot:        "~",
	LogAnd:        "&&",
	LogOr:         "||",
	Nullish:       "??",
	Question:      "?",
	QuestionDot:   "?.",
	Colon:         ":",
	Assign:        "=",
	AddAssign:     "+=",
	SubAssign:     "-=",
	MulAssign:     "*=",
	DivAssign:     "/=",
	RemAssign:     "%=",
	ExpAssign:     "**=",
	ShlAssign:     "<<=",
	ShrAssign:     ">>=",
	UShrAssign:    ">>>=",
	AndAssign:     "&=",
	OrAssign:      "|=",
	XorAssign:     "^=",
	LogAndAssign:  "&&=",
	LogOrAssign:   "||=",
	NullishAssign: "??=",
	Arrow:         "=>",
	Hash:          "#",
	Identifier:    "identifier",
	PrivateIdent:  "private name",
	EscapedWord:   "escaped keyword",
	Number:        "number",
	BigInt:        "bigint",
	String:        "string",
	Template:      "template",
	Regex:         "regular expression",
	keywordStart:  "",
	KwAwait:       "await",
	KwBreak:       "break",
	KwCase:        "case",
	KwCatch:       "catch",
	KwClass:       "class",
	KwConst:       "const",
	KwContinue:    "continue",
	KwDebugger:    "debugger",
	KwDefault:     "default",
	KwDelete:      "delete",
	KwDo:          "do",
	KwElse:        "else",
	KwEnum:        "enum",
	KwExport:      "export",
	KwExtends:     "extends",
	KwFalse:       "false",
	KwFinally:     "finally",
	KwFor:         "for",
	KwFunction:    "function",
	KwIf:          "if",
	KwImport:      "import",
	KwIn:          "in",
	KwInstanceof:  "instanceof",
	KwNew:         "new",
	KwNull:        "null",
	KwReturn:      "return",
	KwSuper:       "super",
	KwSwitch:      "switch",
	KwThis:        "this",
	KwThrow:       "throw",
	KwTrue:        "true",
	KwTry:         "try",
	KwTypeof:      "typeof",
	KwVar:         "var",
	KwVoid:        "void",
	KwWhile:       "while",
	KwWith:        "with",
	KwYield:       "yield",
	KwImplements:  "implements",
	KwInterface:   "interface",
	KwLet:         "let",
	KwPackage:     "package",
	KwPrivate:     "private",
	KwProtected:   "protected",
	KwPublic:      "public",
	KwStatic:      "static",
	keywordEnd:    "",
}

// String returns the spelling of punctuators and keywords and a description
// for the other kinds.
func (t Token) String() string {
	if int(t) < len(tokenNames) && tokenNames[t] != "" {
		return tokenNames[t]
	}
	return "token(" + strconv.Itoa(int(t)) + ")"
}

// IsKeyword reports whether t is spelled like an identifier but reserved in
// strict mode (including the strict-mode future reserved words and `let`,
// `await`, `yield`).
func (t Token) IsKeyword() bool { return t > keywordStart && t < keywordEnd }

// isAssignOp reports whether t is `=` or a compound assignment operator.
func (t Token) isAssignOp() bool { return t >= Assign && t <= NullishAssign }

// keywords maps reserved spellings to their token. Built at init from
// tokenNames so the two never drift apart.
var keywords map[string]Token

func init() {
	keywords = make(map[string]Token, int(keywordEnd-keywordStart))
	for t := keywordStart + 1; t < keywordEnd; t++ {
		keywords[tokenNames[t]] = t
	}
}

// lookupKeyword returns the keyword token for an identifier spelling, or
// Identifier. It avoids the map for spellings whose length cannot be a
// keyword and for common short identifiers.
func lookupKeyword(s string) Token {
	if len(s) < 2 || len(s) > 10 {
		return Identifier
	}
	if c := s[0]; c < 'a' || c > 'y' {
		return Identifier
	}
	if t, ok := keywords[s]; ok {
		return t
	}
	return Identifier
}

// binaryPrec returns the precedence of a binary operator token, or 0 when t
// is not a binary operator. Higher binds tighter. `??` shares the level of
// `||`; mixing them without parentheses is rejected by the parser.
func binaryPrec(t Token) int {
	switch t {
	case Nullish, LogOr:
		return 1
	case LogAnd:
		return 2
	case BitOr:
		return 3
	case BitXor:
		return 4
	case BitAnd:
		return 5
	case Eq, NotEq, StrictEq, StrictNeq:
		return 6
	case Lt, Gt, LtEq, GtEq, KwInstanceof, KwIn:
		return 7
	case Shl, Shr, UShr:
		return 8
	case Plus, Minus:
		return 9
	case Mul, Div, Rem:
		return 10
	case Exp:
		return 11
	}
	return 0
}
