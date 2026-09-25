package syntax

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// fuzzSeeds cover the grammar and the early errors; FuzzParse also seeds
// from the differential corpus (bench/corpus).
var fuzzSeeds = []string{
	"",
	"let x = 1; x += 2; x **= 2; x ??= 3;",
	"const {a, b: [c = 1, ...d], ...e} = o;",
	"function f(a, b = 2, ...c) { return a ?? b?.[c] ?? f?.(1); }",
	"l: for (let i = 0; i < 3; i++) { if (i) continue l; else break; }",
	"for (const k in o) {} for (const v of a) {} do ; while (0)",
	"switch (x) { case 1: let y; break; default: }",
	"try { throw new Error('x') } catch ({message}) {} finally {}",
	"const t = `a${b}c${`d${e}`}`; tag`x${y}\\u{`;",
	"const r = /a(?<n>b)+[^c-d]\\u{1F600}/giu;",
	"x = a ? b : c, d = () => ({}), e = async () => await 1;",
	"class A extends B { #p = 1; static m() { super.m(); } get g() {} }",
	"function* g() { yield* [1]; } async function h() { for await (const x of y); }",
	"export default function () {} export { a as b }; import c, * as d from 'e';",
	"0x1F + 0o7 + 0b1 + 1e-7 + .5 + 1_000n + 08",
	"a = b\n++c\nreturn",
	"'\\u{10FFFF}\\x41\\0'; \"\\\r\n\";",
	"if (a) function f() {}",
	"with (o) {}",
	"({ __proto__: null, __proto__: 1 })",
	"let let = 1;",
	"a => a; (a, b) => { 'use strict'; }; ((a)) => a",
	"new.target; import.meta; delete x;",
	"\u2028\u2029\ufeff/* \r\n */ // x\r",
	"x = {a, b: 1, [c]: 2, d() {}, get e() {}, set e(v) {}, ...f};",
	"let [a, , b] = c; ({a: d.e, f: g[0]} = h);",
	"function f() { return () => arguments[0] + arguments.length; } arguments;",
	"t`a${b}\\01${c}\\u{110000}`; a.b`x`; new t`y`(1); t`a``b`; `\\xg`;",
	"x = /(?<a>.)\\k<a>|[\\p{L}--\\q{ab}]/v; y = /(?i:[a-z])(?=\\d)*\\8{/; z = /(?<a>x)(?<a>y)/;",
	"a = /[z-a]/; b = /\\p{Script=Greek}+/u; c = x / y / z;",
	"x = { \\u0069f: 1, g\\u0065t() {} }.\\u0069f; \\u0061sync; var \\u0069f; \\u0061sync function f() {}",
	"a?.b`x`; a?.b.c`y`; const ᾩ\\u200d = 1, \\u{1D49C} = 2; ({ \\u0061sync: 1 }); \\u{62}reak: ;",
	"class A { static #s; #m() {} get #g() {} set #g(v) {} static { this.#s = #m in this; } [k] = 1; 'q'; static async\n x() {} }",
	"class B extends (a, b) { constructor() { super(...a); super.x = new.target; } set\n *g() {} get = 1; static = 2; accessor }",
	"(class extends null { x = () => super.y; static { var await; } })",
	"for (const [k, {v = 1, ...r}] of m) { for (let x of [...a, ...b]) break; }",
	"({[Symbol.iterator]() { return this; }, [Symbol.toPrimitive]: h}); [a, ...[b, c]] = d;",
	"async function* g(a = 1) { const x = await (yield* h()); for await (const [k] of yield) yield await x; } ({ async *m() {}, async [k]() {} });",
	"class A { async *#g() { yield; } static async s() { await super.s(); } async\n x() {} } async () => { for await (x.y of z); }",
	"await x; for await (const y of z) await y; export const v = await 0, w = await (async () => await 1)(); l: for await (let a of b) continue l;",
	"var await; async\nfunction f() {} x = async function () { return await await 1; }; await => 1; function* g() { yield\n1; } async (a) => a;",
	"function* g(x = yield) {} (async function await() {}); async (a = await 1) => a; function* h() { yield\n* 1; }",
}

func FuzzParse(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add(s)
	}
	files, _ := filepath.Glob("../bench/corpus/*.js")
	for _, p := range files {
		if b, err := os.ReadFile(p); err == nil {
			f.Add(string(b))
		}
	}
	f.Fuzz(func(t *testing.T, src string) {
		m, err := ParseModule("fuzz.js", src, Options{})
		fuzzCheckParse(t, src, m, err)
		s, err := ParseScript("fuzz.js", src, Options{})
		fuzzCheckParse(t, src, s, err)
	})
}

// fuzzCheckParse checks one parse outcome: an error is a *Error at a valid
// position; a tree has a consistent scope annotation, dumps identically on a
// second parse, and parses to the same tree with AllowUnsupported (which
// only lifts feature rejections).
func fuzzCheckParse(t *testing.T, src string, prog Node, err error) {
	t.Helper()
	if err != nil {
		se, ok := err.(*Error)
		if !ok {
			t.Fatalf("error is %T, want *syntax.Error: %v", err, err)
		}
		fuzzCheckError(t, src, se)
		return
	}
	var scope *Scope
	var again Node
	var agErr error
	switch p := prog.(type) {
	case *Module:
		scope = p.Scope
		again, agErr = ParseModule("fuzz.js", src, Options{AllowUnsupported: true})
	case *Script:
		scope = p.Scope
		again, agErr = ParseScript("fuzz.js", src, Options{AllowUnsupported: true})
	}
	fuzzCheckScopes(t, prog, scope)
	if agErr != nil {
		t.Fatalf("parses without AllowUnsupported but not with it: %v", agErr)
	}
	if d1, d2 := Dump(prog), Dump(again); d1 != d2 {
		t.Fatalf("AllowUnsupported changed the tree:\n%s\n---\n%s", d1, d2)
	}
}

func fuzzCheckError(t *testing.T, src string, e *Error) {
	t.Helper()
	if e.Name != "fuzz.js" || !strings.HasPrefix(e.Msg, errPrefix) {
		t.Fatalf("malformed error %#v", e)
	}
	if e.Pos < 0 || e.Pos > len(src) {
		t.Fatalf("error position %d outside the %d-byte source: %v", e.Pos, len(src), e)
	}
	if line, col := fuzzPosition(src, e.Pos); e.Line != line || e.Col != col {
		t.Fatalf("error at offset %d reports %d:%d, want %d:%d: %v", e.Pos, e.Line, e.Col, line, col, e)
	}
}

// fuzzPosition is the reference offset -> (line, col) mapping: lines end at
// LF, CR (CRLF counts once), U+2028 and U+2029; columns count code points
// from 1 (a byte that does not continue a UTF-8 sequence starts one).
func fuzzPosition(src string, pos int) (line, col int) {
	line, start := 1, 0
	for i := 0; i < pos; {
		r, size := utf8.DecodeRuneInString(src[i:])
		switch {
		case r == '\n', r == '\u2028', r == '\u2029', r == '\r' && (i+1 >= len(src) || src[i+1] != '\n'):
			line++
			start = i + size
		}
		i += size
	}
	return line, runeStarts(src[start:pos]) + 1
}

// fuzzCheckScopes checks the scope pass: every scope's bindings name their
// scope and slot and are unique by name, children point at their parent,
// and every identifier bound to a binding matches its name and belongs to
// a scope of this tree.
func fuzzCheckScopes(t *testing.T, prog Node, root *Scope) {
	t.Helper()
	if root == nil {
		t.Fatal("parsed program has no scope")
	}
	scopes := map[*Scope]bool{}
	var walk func(s *Scope)
	walk = func(s *Scope) {
		scopes[s] = true
		for i, b := range s.Bindings {
			if b.Scope != s || b.Slot != i {
				t.Fatalf("binding %q: scope/slot (%p, %d), want (%p, %d)", b.Name, b.Scope, b.Slot, s, i)
			}
			if got := s.Lookup(b.Name); got != b {
				t.Fatalf("binding %q is declared twice in one %s scope", b.Name, s.Kind)
			}
		}
		for _, c := range s.Children {
			if c.Parent != s {
				t.Fatalf("%s scope's parent is not the scope listing it", c.Kind)
			}
			walk(c)
		}
	}
	walk(root)
	Inspect(prog, func(n Node) bool {
		if id, ok := n.(*Ident); ok && id.Binding != nil {
			b := id.Binding
			if b.Name != id.Name || !scopes[b.Scope] {
				t.Fatalf("identifier %q at %d is bound to %q outside the scope tree", id.Name, id.Pos, b.Name)
			}
		}
		return true
	})
}
