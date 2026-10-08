package syntax

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var tsOpts = Options{TypeScript: true}

// tsPairs are TypeScript sources and the JavaScript tsc emits for them,
// erased by hand; both must parse to the same tree.
var tsPairs = []struct{ name, ts, js string }{
	// Annotations
	{"var-types", `let x: number = 1, y: string; var z: any; const w: T = 0;`, `let x = 1, y; var z; const w = 0;`},
	{"definite", `let x!: number; var y!: string;`, `let x; var y;`},
	{"destructuring-type", `const { a, b }: T = o; const [c, d]: U = p; let { e = 1 }: V = q;`, `const { a, b } = o; const [c, d] = p; let { e = 1 } = q;`},
	{"params", `function f(a: number, b?: string, c: T = 1, ...d: any[]): void {}`, `function f(a, b, c = 1, ...d) {}`},
	{"pattern-params", `function f({ a }: T, [b]?: U, { c } = {} as V) {}`, `function f({ a }, [b], { c } = {}) {}`},
	{"this-param", `function f(this: Window, a: number) {} function g(this: void) {}`, `function f(a) {} function g() {}`},
	{"return-types", `function f(): T {} function* g(): Generator<T> {} async function h(): Promise<void> {}`, `function f() {} function* g() {} async function h() {}`},
	{"predicates", `function g(x: unknown): x is string { return true } function h(x: unknown): asserts x is string {} function k(this: Foo): asserts this {} function m(asserts: any): asserts is string {}`,
		`function g(x) { return true } function h(x) {} function k() {} function m(asserts) {}`},
	{"catch", `try {} catch (e: unknown) {} try {} catch ({ message }: any) {}`, `try {} catch (e) {} try {} catch ({ message }) {}`},
	{"for", `for (let i: number = 0; i < n; i++) {}`, `for (let i = 0; i < n; i++) {}`},
	{"accessors", `let o = { get x(): number { return 1 }, set x(v: number) {}, m<T>(a: T): T { return a }, async n<T>() {}, *g(): any {} };`,
		`let o = { get x() { return 1 }, set x(v) {}, m(a) { return a }, async n() {}, *g() {} };`},
	{"function-expr", `let fn = function <T>(a: T): T { return a }, gn = function* named<T>(): Iterator<T> {};`, `let fn = function (a) { return a }, gn = function* named() {};`},

	// Arrow functions
	{"arrows", `let f1 = (a?) => a, f2 = (a = 1, {b}: T = {}, [c]?: U) => 1, f3 = (...args: any[]): void => {}, f4 = (): void => {}, f5 = async (): Promise<void> => {}, f6 = async x => x;`,
		`let f1 = (a) => a, f2 = (a = 1, {b} = {}, [c]) => 1, f3 = (...args) => {}, f4 = () => {}, f5 = async () => {}, f6 = async x => x;`},
	{"generic-arrows", `let f7 = <T,>(x: T) => x, f8 = <const T extends readonly unknown[]>(x: T) => x, f9 = async <T>(x: T): Promise<T> => x, fa = <T, U = T>(x: T, y: U): [T, U] => [x, y];`,
		`let f7 = (x) => x, f8 = (x) => x, f9 = async (x) => x, fa = (x, y) => [x, y];`},
	{"arrow-object-return", `let r = (): { a: number } => ({ a: 1 }), s = (a): a is T => true;`, `let r = () => ({ a: 1 }), s = (a) => true;`},
	{"async-call-or-arrow", `x = async (a: number, b): Promise<void> => {}; y = async (a, b) => {}; z = async(a, b); w = async (a): T => a; v = async(...a); u = async (...a: T) => 1;`,
		`x = async (a, b) => {}; y = async (a, b) => {}; z = async(a, b); w = async (a) => a; v = async(...a); u = async (...a) => 1;`},
	{"async-ident", `async as T; async satisfies T; let r = async as => 1;`, `async; async; let r = async as => 1;`},
	{"cond-true-branch", `a ? (b): c => d; a ? (b): c => d : e; a ? (b: number): void => d() : e; a ? (b) : (c): d => e;`,
		`a ? (b) : c => d; a ? (b) => d : e; a ? (b) => d() : e; a ? (b) : (c) => e;`},
	{"cond-nested-arrow", `a ? x => (y): T => z : w; a ? x => (y) : T => z;`, `a ? x => (y) => z : w; a ? x => (y) : T => z;`},
	{"case-colon", `switch (x) { case (a): b; case (c): { d; } }`, `switch (x) { case (a): b; case (c): { d; } }`},
	{"object-value-arrow", `let o = { a: (b): c => d };`, `let o = { a: (b) => d };`},
	{"paren-not-arrow", `x = (a + b); y = (a, b); z = (a as T);`, `x = (a + b); y = (a, b); z = (a);`},
	{"casts-in-defaults", `x = (a = b!, { c = d as T } = e!) => a; [f!, g as any] = h; ({ i: j! } = k);`, `x = (a = b, { c = d } = e) => a; [f, g] = h; ({ i: j } = k);`},

	// Expressions
	{"casts", `let a = f<string>; let b = x as const; let c = y!.z!; let d = <any>w; let e = o satisfies T; let g = <Array<T>>y;`,
		`let a = f; let b = x; let c = y.z; let d = w; let e = o; let g = y;`},
	{"as-precedence", `x = a + b as T; y = a < b as T; z = (a as any) + 1; w = a as T as U[];`, `x = a + b; y = a < b; z = (a) + 1; w = a;`},
	{"non-null", `a?.b!.c; a!?.b; x! = 1; x!++; (x as any) = 1; [a!, b] = c; f!(); a!!; this!.x; new X!();`,
		`a?.b.c; a?.b; x = 1; x++; (x) = 1; [a, b] = c; f(); a; this.x; new X();`},
	{"type-args", `f<T>(x => x); new Map<string, number>(); Promise.all<[A, B]>([a, b]); obj.method<T>(); new C<T>; a?.b<T>(); a?.<T>(); f<T>` + "`x`" + `;`,
		`f(x => x); new Map(); Promise.all([a, b]); obj.method(); new C; a?.b(); a?.(); f` + "`x`" + `;`},
	{"type-args-split", `f<A<B>>(x); f<A<B<C>>>(y); g<Array<Array<T>>>(z); h<<T>() => T>(w);`, `f(x); f(y); g(z); h(w);`},
	{"type-args-vs-relational", `a < b > c; a < b >> c; a < b >= c; a < b && c > d; f(a < b, c > (d)); x = a<b>+c; y = a<b>-c;`,
		`a < b > c; a < b >> c; a < b >= c; a < b && c > d; f(a(d)); x = a < b > +c; y = a < b > -c;`},
	{"instantiation", "f<T>\n(x); f<T>\ng; let h = f<string>;", `f(x); f; g; let h = f;`},
	{"assertions", `let a1 = <T>(x); let a2 = <T>(x) => x; let a3 = <T>x; let a4 = <T>(x).y; let a5 = <T>(<U>(z));`,
		`let a1 = (x); let a2 = (x) => x; let a3 = x; let a4 = (x).y; let a5 = ((z));`},
	{"satisfies-object", `x = y satisfies { a: 1 }; let u = <T,>(a: T): a is T => true;`, `x = y; let u = (a) => true;`},

	// The type grammar
	{"type-literal", `type T<A extends B = C> = { a: string; b?: number; readonly [k: string]: any; (x: number): void; new (): T; m<U>(u: U): void; get x(): number; set x(v: number), new: 1, get: 2; set?(): void; readonly: true; "q": 1; 2: 3; [Symbol.iterator](): I };`, ``},
	{"mapped", `let x: { [K in keyof T as ` + "`get${K & string}`" + `]-?: T[K] extends infer U extends string ? U : never }; let y: { +readonly [P in K]+?: T }; let z: { readonly [P in K]: T };`, `let x; let y; let z;`},
	{"types", `let y: [a: string, b?: number, ...c: boolean[]] | readonly string[] | unique symbol | typeof import("m").T<number> | (() => void) | (new () => X) | (abstract new () => Y) | [string?, ...number[]];`, `let y;`},
	{"literal-types", `let t1: T["a"]["b"]; let t2: -1 | 1n | -2n | "s" | true | false | null | undefined | void | never | unknown | any | object | symbol | bigint; let t3: void = undefined;`, `let t1; let t2; let t3 = undefined;`},
	{"template-types", "let v1: `a${string}b${`c${number}`}`; let v2: `x`;", `let v1; let v2;`},
	{"function-types", `let f: ({a, b}: T) => void; let x: (typeof y)[]; let g: <T>(x: T) => T = (x) => x; let c: new <T>() => T; let d: (this: Window, ...a: any[]) => asserts this is Foo;`,
		`let f; let x; let g = (x) => x; let c; let d;`},
	{"conditional-types", `let c: A extends B ? C extends D ? 1 : 2 : 3; let i: T extends [infer H, ...infer R] ? H : never; let j: T extends (infer U extends string) ? U : 0; f<A extends B ? C : D>();`,
		`let c; let i; let j; f();`},
	{"query-and-keyof", `let t: typeof x[number]; let k: keyof typeof obj; let q: typeof a.b.c<T>; let u: A.B.C<D>; let w: import("m", { with: { "resolution-mode": "import" } }).X;`, `let t; let k; let q; let u; let w;`},
	{"leading-operators", `type T = | A | B; type U = & A & B; type V = | (() => void);`, ``},

	// Declarations
	{"interface", `interface I<T> extends A, B.C<T> { x: T; } let q: I<number>;`, `let q;`},
	{"type-in-block", `if (a) { interface X {} type Y = 1; }`, `if (a) {}`},
	{"declare", `declare const x: number; declare let y; declare var z: string, w; declare function f(): void; declare function g(eval: any, arguments: any): void; declare async function h(): Promise<void>; declare class C { m(): void; x: number; } declare module "foo" { export const a: number; export = a; import x = require("y"); } declare global { interface Window { x: 1 } } declare namespace Q { let a: number } declare enum E2 { A = 1, B } declare const enum E3 { A } declare abstract class D {} declare type T = 1; declare interface J {} declare module M.N { export function f(): void }`, ``},
	{"namespace-types", `namespace N { export type T = 1; namespace M { interface I {} } import X = A.B; import type Y = Z; } module O.P { } let z = 1;`, `let z = 1;`},
	{"overloads", "function ov(a: string): string;\nfunction ov(a: number): number;\nfunction ov(a: any) { return a; }\nexport function f(a: string): void;\nexport function f(a: any) {}\nexport default function g(): void;\nexport default function g() {}\nasync function h(): Promise<void>\nasync function h() {}",
		"function ov(a) { return a; }\nexport function f(a) {}\nexport default function g() {}\nasync function h() {}"},
	{"abstract-class", "abstract class AC { abstract m(): void; abstract x: number; n() {} }\nexport default abstract class {}", "class AC { n() {} }\nexport default class {}"},
	{"export-forms", `export type { A } from "m"; export type * from "n"; export type * as ns from "o"; export { type U } from "p"; export interface II {} export type TT = 1; export declare const dc: number; export namespace EN {} export abstract class EA {} export default interface X {}`,
		`export class EA {}`},
	{"export-type-specifiers", `export { type A as B, C }; let C = 1; export { type D } from "m"; export { type E, F } from "m";`, `export { C }; let C = 1; export { F } from "m";`},

	// Classes
	{"class", `class A<T> extends B<T> implements I, J<T> { [k: string]: any; m2(a: string): void; m2(a: any) {} private static readonly x?: number = 1; override y!: T; declare z: number; constructor() { super(); } constructor(); }`,
		`class A extends B { m2(a) {} static x = 1; y; constructor() { super(); } }`},
	{"class-fields", `class C { a: string; b?: number; c!: T; public d; protected e = 1; private f: string = ""; readonly g; static h: number; #i: number; static #j?: string; }`,
		`class C { a; b; c; d; e = 1; f = ""; g; static h; #i; static #j; }`},
	{"class-methods", `class C { public async m<T>(a: T): Promise<T> { return a } protected *g(): any {} get x(): number { return 1 } set x(v: number) {} static s?(): void {} private static async *t() {} get y(this: C): number { return 1 } m2?(): void; }`,
		`class C { async m(a) { return a } *g() {} get x() { return 1 } set x(v) {} static s() {} static async *t() {} get y() { return 1 } }`},
	{"class-expr", `const C2 = class<T> implements I {}; class D2 implements I {} class E2 extends f<T>() {} class F2 extends A.B<C> implements D.E<F> {}`,
		`const C2 = class {}; class D2 {} class E2 extends f() {} class F2 extends A.B {}`},
	{"class-keyword-names", `class K { public; declare; abstract; readonly; type; static; async; get; set; accessor; override; public() {} declare() {} readonly = 1; type?: string; static static: number; declare declare: number; readonly readonly: number; abstract?: string; get?(): void; get?() {} in() {} }`,
		`class K { public; declare; abstract; readonly; type; static; async; get; set; accessor; override; public() {} declare() {} readonly = 1; type; static static; readonly; abstract; get() {} in() {} }`},
	{"class-modifier-newlines", "class N { public\nfoo; async\nbar() {} static\nbaz; get\nqux() {} }", "class N { public\nfoo; async\nbar() {} static\nbaz; get\nqux() {} }"},
	{"class-index-static", `class S { static [k: string]: any; [Symbol.iterator](): Iterator<T> { return null } static {} }`, `class S { [Symbol.iterator]() { return null } static {} }`},

	// Contextual keywords as identifiers
	{"keywords", `let type = 1; type = 2; declare(); abstract.x; namespace.x; module.exports = 1; global.x = 1; let as = 1, satisfies = 2, is = 3, asserts = 4, infer = 5, keyof = 6, readonly = 7, unique = 8, out = 9, accessor = 10, override = 11, of = 12;`,
		`let type = 1; type = 2; declare(); abstract.x; namespace.x; module.exports = 1; global.x = 1; let as = 1, satisfies = 2, is = 3, asserts = 4, infer = 5, keyof = 6, readonly = 7, unique = 8, out = 9, accessor = 10, override = 11, of = 12;`},
	{"keyword-asi", "declare\nconst x = 1;\ntype\nFoo = 1;\nabstract\nclass Q {}\nnamespace\nM;", "declare\nconst x = 1;\ntype\nFoo = 1;\nabstract\nclass Q {}\nnamespace\nM;"},
	{"keyword-labels", `type: for (;;) break type; declare: { break declare; }`, `type: for (;;) break type; declare: { break declare; }`},
	{"asi-operators", "let a = b\nas\nT; let c = d\n!e; let f = g\nsatisfies", "let a = b\nas\nT; let c = d\n!e; let f = g\nsatisfies"},

	// Imports
	{"import-elision", `import { A, B } from "m"; import {} from "n"; import { type C } from "o"; import D, * as E from "p"; import F, { type G } from "q"; import "r"; let x: A = B; export { x };`,
		`import { B } from "m"; import "r"; let x = B; export { x };`},
	{"import-type", `import type { A } from "m"; import type B from "n"; import type * as C from "o"; import type D = require("p"); import type E = F.G; let x: A | B | C | D | E;`, `let x;`},
	{"import-type-names", `import { type as } from "m"; import { type as as } from "n"; import { type as as as } from "o"; import { type "x-y" as z } from "p"; import type from "q"; import { type as as2 } from "r"; as; type; z; as2;`,
		`import { type as as } from "n"; import type from "q"; import { type as as2 } from "r"; as; type; z; as2;`},
	{"import-type-from", `import type from from "m"; let f: from; import { type } from "n"; type;`, `let f; import { type } from "n"; type;`},
	{"import-shadowed", `import { A } from "m"; function g(A: number) { return A; } let y: typeof A; class K implements A {}`, `function g(A) { return A; } let y; class K {}`},
	{"import-value-uses", `import { A } from "m"; class C extends A {} import { B } from "n"; export { B }; import { D } from "o"; export default D; import * as E from "p"; E.f(); import { F } from "q"; let f = <F>g;`,
		`import { A } from "m"; class C extends A {} import { B } from "n"; export { B }; import { D } from "o"; export default D; import * as E from "p"; E.f(); let f = g;`},
	{"import-reexport-only", `import { A } from "m"; export { A } from "n";`, `export { A } from "n";`},
	{"import-alias", `import X = A.B; import Y = C; let x: X; let y: Y.Z;`, `let x; let y;`},
	{"export-types", `import type { A } from "m"; export { A }; interface I {} export { I }; type T = 1; export default T; namespace NS { export type U = 1 } export { NS as N };`, ``},
	{"export-declared", `declare const x: number; declare function f(): void; export { x }; export default f;`, `export default f;`},
	{"export-merged", `const V = 1; type V = number; export { V }; class W {} interface W {} export default W;`, `const V = 1; export { V }; class W {} export default W;`},

	// Review fixes
	{"ambient-imports", `declare module "m" { import x from "n"; import type { X } from "n"; import type Y from "n"; import z, { w } from "n"; import * as ns from "n"; import a = require("n"); import b = A.B; import "n"; export { x }; } declare namespace N { import x from "n"; import type X = Y.Z; } declare global { import type { T } from "t"; } let q = 1;`,
		`let q = 1;`},
	{"export-default-abstract", "export default abstract\nclass C {}", "export default abstract\nclass C {}"},
	{"export-default-abstract-expr", `export default abstract(1);`, `export default abstract(1);`},
	{"export-default-abstract-member", `export default abstract.x;`, `export default abstract.x;`},
	{"cast-targets", `x! = 1; (a as T) = b; (<T>a) = b; (<T>(a)) = b; [a as T] = b; ({ a: b as T } = c); (a as T)! = b; a.b! += c; <T>a++; ++(a as T); <T>(a)++;`,
		`x = 1; (a) = b; (a) = b; ((a)) = b; [a] = b; ({ a: b } = c); (a) = b; a.b += c; a++; ++(a); (a)++;`},
	{"instantiation-optional", `f<T>?.(); f<T>?.[0];`, `f?.(); f?.[0];`},
	{"declare-definite", `declare let x!: T; export declare let y!: U, z: V;`, ``},
	{"namespace-alias", `namespace N { import X = A.B; } const X = 1; X;`, `const X = 1; X;`},
	{"namespace-exports", `interface J {} namespace N { interface I {} export { I }; namespace K {} export { K, J } }`, ``},
	{"single-statement-decls", `if (a) type T = 1; l: type U = 1; if (a) interface I {} else declare const x: number; while (a) declare function f(): void; if (a) declare global {} if (a) namespace N {} if (b) declare module "m" {}`,
		`if (a) ; l: ; if (a) ; else ; while (a) ; if (a) ; if (a) ; if (b) ;`},
	{"elided-named-imports", `import D, { A } from "m"; D(); let y: A;`, `import D from "m"; D(); let y;`},
	{"block-type-not-exported", `{ interface I {} } export default I;`, `{} export default I;`},
}

func TestTypeScriptPairs(t *testing.T) {
	for _, tt := range tsPairs {
		t.Run(tt.name, func(t *testing.T) {
			mt, err := ParseModule("t.ts", tt.ts, tsOpts)
			require.NoError(t, err)
			mj, err := ParseModule("t.js", tt.js, Options{})
			require.NoError(t, err)
			assert.Equal(t, Dump(mj), Dump(mt))
		})
	}
}

// TestTypeScriptRejects checks that TypeScript with run-time semantics fails
// naming the feature, and that invalid TypeScript fails.
func TestTypeScriptRejects(t *testing.T) {
	tests := []struct{ src, msg string }{
		{"enum E { A }", "t.ts:1:1: SyntaxError: A TypeScript enum is not supported: only erasable TypeScript is (see TODO.md)"},
		{"export enum E { A }", "t.ts:1:8: SyntaxError: A TypeScript enum is not supported: only erasable TypeScript is (see TODO.md)"},
		{"const enum E { A }", "t.ts:1:1: SyntaxError: A TypeScript const enum is not supported: only erasable TypeScript is (see TODO.md)"},
		{"if (x) { const enum E { A } }", "t.ts:1:10: SyntaxError: A TypeScript const enum is not supported: only erasable TypeScript is (see TODO.md)"},
		{"namespace N { export const x = 1 }", "t.ts:1:1: SyntaxError: A TypeScript namespace with values is not supported: only erasable TypeScript is (see TODO.md)"},
		{"namespace N { ; }", "t.ts:1:1: SyntaxError: A TypeScript namespace with values is not supported: only erasable TypeScript is (see TODO.md)"},
		{"namespace N { declare const x: number }", "t.ts:1:1: SyntaxError: A TypeScript namespace with values is not supported: only erasable TypeScript is (see TODO.md)"},
		{"module M.N { function f() {} }", "t.ts:1:1: SyntaxError: A TypeScript namespace with values is not supported: only erasable TypeScript is (see TODO.md)"},
		{"namespace A { declare namespace B { export const x: number } }", "t.ts:1:1: SyntaxError: A TypeScript namespace with values is not supported: only erasable TypeScript is (see TODO.md)"},
		{"namespace A { export { x } }", "t.ts:1:1: SyntaxError: A TypeScript namespace with values is not supported: only erasable TypeScript is (see TODO.md)"},
		{`module "m" {}`, "t.ts:1:1: SyntaxError: Only ambient modules can use quoted names."},
		{"global { }", "t.ts:1:1: SyntaxError: Augmentations for the global scope can only be directly nested in external modules or ambient module declarations."},
		{"class C { constructor(private x: number) {} }", "t.ts:1:23: SyntaxError: A parameter property is not supported: only erasable TypeScript is (see TODO.md)"},
		{"class C { constructor(readonly x) {} }", "t.ts:1:23: SyntaxError: A parameter property is not supported: only erasable TypeScript is (see TODO.md)"},
		{"x = (public a) => a;", "t.ts:1:6: SyntaxError: A parameter property is not supported: only erasable TypeScript is (see TODO.md)"},
		{`import x = require("m");`, "t.ts:1:1: SyntaxError: import = require is not supported: only erasable TypeScript is (see TODO.md)"},
		{`import x = A.B; x.y();`, "t.ts:1:8: SyntaxError: An import alias used as a value is not supported: only erasable TypeScript is (see TODO.md)"},
		{`import x = A.B; export { x };`, "t.ts:1:8: SyntaxError: An import alias used as a value is not supported: only erasable TypeScript is (see TODO.md)"},
		{`export import x = A.B;`, "t.ts:1:1: SyntaxError: export import is not supported: only erasable TypeScript is (see TODO.md)"},
		{`export = 1;`, "t.ts:1:1: SyntaxError: export = is not supported: only erasable TypeScript is (see TODO.md)"},
		{`export as namespace Foo;`, "t.ts:1:1: SyntaxError: export as namespace is not supported: only erasable TypeScript is (see TODO.md)"},
		{"class A { accessor x = 1; }", "t.ts:1:11: SyntaxError: An accessor field is not supported: only erasable TypeScript is (see TODO.md)"},
		{"@dec class A {}", "t.ts:1:1: SyntaxError: Invalid or unexpected token"},

		{"x = (this: T) => 1;", "t.ts:1:6: SyntaxError: An arrow function cannot have a 'this' parameter."},
		{"x = (a!) => a;", "t.ts:1:6: SyntaxError: Invalid destructuring assignment target"},
		{"x = (a as T) => a;", "t.ts:1:6: SyntaxError: Invalid destructuring assignment target"},
		{"x = (b, <T>a) => a;", "t.ts:1:12: SyntaxError: Invalid destructuring assignment target"},
		{"x = ({ a: b! }: T) => b;", "t.ts:1:11: SyntaxError: Invalid destructuring assignment target"},
		{"x = ([a satisfies T] = c) => a;", "t.ts:1:7: SyntaxError: Invalid destructuring assignment target"},
		{"x = async (...a!) => a;", "t.ts:1:15: SyntaxError: Invalid destructuring assignment target"},
		{"x = (f<T>) => f;", "t.ts:1:6: SyntaxError: Invalid destructuring assignment target"},
		{"x = (a: number);", "t.ts:1:7: SyntaxError: Unexpected token ':'"},
		{"x = (a?);", "t.ts:1:7: SyntaxError: Unexpected token '?'"},
		{"x = (a: T)\n=> a;", "t.ts:1:7: SyntaxError: Unexpected token ':'"},
		{"f<T>.x;", "t.ts:1:5: SyntaxError: An instantiation expression cannot be followed by a property access."},
		{"abstract class A { abstract m() {} }", "t.ts:1:29: SyntaxError: An abstract method cannot have an implementation."},
		{"let x = { m(): void; };", "t.ts:1:20: SyntaxError: Unexpected token ';'"},
		{"let f = function (): void;", "t.ts:1:26: SyntaxError: Unexpected token ';'"},
		{"type T = ;", "t.ts:1:10: SyntaxError: Unexpected token ';'"},
		{"let x: A<>;", "t.ts:1:10: SyntaxError: Unexpected token '>'"},
		{"let x: string?;", "t.ts:1:14: SyntaxError: Unexpected token '?'"},
		{"interface I { x: number = 1 }", "t.ts:1:25: SyntaxError: Unexpected token '='"},
		{"import { type x as } from 'm';", "t.ts:1:20: SyntaxError: Unexpected token '}'"},
		{"class C { x?!: number }", "t.ts:1:13: SyntaxError: Unexpected token '!'"},
		{"x = a as T = b;", "t.ts:1:5: SyntaxError: Invalid left-hand side in assignment"},
		{"a satisfies T = b;", "t.ts:1:1: SyntaxError: Invalid left-hand side in assignment"},
		{"a as T += 1;", "t.ts:1:1: SyntaxError: Invalid left-hand side in assignment"},
		{"a as T ||= 1;", "t.ts:1:1: SyntaxError: Invalid left-hand side in assignment"},
		{"<T>a = b;", "t.ts:1:4: SyntaxError: Invalid left-hand side in assignment"},
		{"<T>a.b = c;", "t.ts:1:4: SyntaxError: Invalid left-hand side in assignment"},
		{"<T>(a).b = c;", "t.ts:1:4: SyntaxError: Invalid left-hand side in assignment"},
		{"a as T++;", "t.ts:1:7: SyntaxError: Unexpected token '++'"},
		{"f<T>?.g;", "t.ts:1:5: SyntaxError: An instantiation expression cannot be followed by a property access."},
		{"function f() { type T = 1 } export { T };", "t.ts:1:38: SyntaxError: Export 'T' is not defined"},
		{"declare namespace D { interface I {} } export { I };", "t.ts:1:49: SyntaxError: Export 'I' is not defined"},
		{"namespace N { import type X = A.B; export { X } }", "t.ts:1:1: SyntaxError: A TypeScript namespace with values is not supported: only erasable TypeScript is (see TODO.md)"},
		{`namespace N { import x from "n"; }`, "t.ts:1:24: SyntaxError: Unexpected identifier 'from'"},
		{"if (a) abstract class C {}", "t.ts:1:17: SyntaxError: Unexpected token 'class'"},
	}
	for _, tt := range tests {
		_, err := ParseModule("t.ts", tt.src, tsOpts)
		assert.EqualError(t, err, tt.msg, tt.src)
	}
}

// TestTypeScriptOff checks that TypeScript syntax is not JavaScript: the
// option is off unless set, and ParseScript rejects it.
func TestTypeScriptOff(t *testing.T) {
	for _, src := range []string{"let x: number;", "f<T>(x) => 1;", "interface I {}", "x as T;", "x!;"} {
		_, err := ParseModule("t.js", src, Options{})
		assert.Error(t, err, src)
	}
	_, err := ParseScript("t.ts", "let x = 1;", tsOpts)
	assert.EqualError(t, err, "t.ts:1:1: SyntaxError: TypeScript is only supported in modules")
}

// TestTypeScriptPositions checks that positions refer to the TypeScript
// text.
func TestTypeScriptPositions(t *testing.T) {
	src := "let x: { a: number } = { a: 1 };\nfunction f<T>(a: T, b?: string): T { return a; }\nlet y = x as any, z = <any>f;\n"
	m, err := ParseModule("t.ts", src, tsOpts)
	require.NoError(t, err)
	fn := m.Body[1].(*FuncDecl).Func
	assert.Equal(t, "function f<T>(a: T, b?: string): T { return a; }", src[fn.Pos:fn.End])
	assert.Equal(t, 2, fn.Length)
	init := m.Body[2].(*VarDecl).Decls[1].Init.(*Ident)
	assert.Equal(t, "f", src[init.Pos:init.End])
	line, col := m.File.Position(init.Pos)
	assert.Equal(t, [2]int{3, 28}, [2]int{line, col})

	_, err = ParseModule("t.ts", "let a: Map<string, number> = new Map();\nlet b: number = c +;\n", tsOpts)
	assert.EqualError(t, err, "t.ts:2:20: SyntaxError: Unexpected token ';'")
}

// TestTypeScriptThisLength checks that a this parameter is not a parameter.
func TestTypeScriptThisLength(t *testing.T) {
	m, err := ParseModule("t.ts", "function f(this: T, a: number, b = 1) {} class C { m(this: C, ...r: any[]) {} }", tsOpts)
	require.NoError(t, err)
	f := m.Body[0].(*FuncDecl).Func
	assert.Equal(t, 2, f.ParamCount)
	assert.Equal(t, 1, f.Length)
	mm := m.Body[1].(*ClassDecl).Class.Members[0].Value.(*Function)
	assert.Equal(t, 0, mm.ParamCount)
	assert.True(t, mm.HasRest)
}

// TestTypeScriptElision checks the module records after the scope pass
// dropped the imports used only as types.
func TestTypeScriptElision(t *testing.T) {
	m, err := ParseModule("t.ts", `import { A } from "a"; import { B, C } from "b"; export { D } from "d"; import * as E from "e"; import "f"; export * from "g"; import { H } from "a";
let x: A = C; E; export { x }; let y: H;`, tsOpts)
	require.NoError(t, err)
	var specs []string
	for _, r := range m.Requests {
		specs = append(specs, r.Specifier)
	}
	assert.Equal(t, []string{"b", "d", "e", "f", "g"}, specs)
	require.Len(t, m.Imports, 2)
	assert.Equal(t, "C", m.Imports[0].Name)
	assert.Equal(t, 0, m.Imports[0].Request)
	assert.True(t, m.Imports[1].Namespace)
	assert.Equal(t, 2, m.Imports[1].Request)
	require.Len(t, m.Reexports, 1)
	assert.Equal(t, 1, m.Reexports[0].Request)
	require.Len(t, m.Stars, 1)
	assert.Equal(t, 4, m.Stars[0].Request)
	for i, b := range m.Scope.Bindings {
		assert.Equal(t, i, b.Slot)
		assert.NotContains(t, []string{"A", "B", "H"}, b.Name)
	}
	assert.Equal(t, "x", m.Exports[0].Name)
}

// TestTypeScriptLinear checks that speculative parsing stays linear on
// inputs built to defeat it: a speculation that fails reads its text again
// at most a fixed number of times, and nesting past MaxNestingDepth fails.
func TestTypeScriptLinear(t *testing.T) {
	shapes := map[string]func(n int) string{
		"comparisons":     func(n int) string { return "x = a" + strings.Repeat(" < b", n) + ";\n" },
		"comparisons-par": func(n int) string { return "x = a" + strings.Repeat(" < (b", n) + strings.Repeat(")", n) + ";\n" },
		"parens":          func(n int) string { return "x = " + strings.Repeat("(", n) + "a" + strings.Repeat(")", n) + ";\n" },
		"param-defaults": func(n int) string {
			return "x = " + strings.Repeat("(a = ", n) + "0" + strings.Repeat(") => 0", n) + ";\n"
		},
		"generic-default": func(n int) string {
			return "x = " + strings.Repeat("<T>(a = ", n) + "0" + strings.Repeat(") => 0", n) + ";\n"
		},
		"assertions":   func(n int) string { return "x = " + strings.Repeat("<T>(", n) + "a" + strings.Repeat(")", n) + ";\n" },
		"conditionals": func(n int) string { return "x = " + strings.Repeat("a ? (b) : ", n) + "c;\n" },
		"return-types": func(n int) string {
			return "x = " + strings.Repeat("a ? (b): T => ", n) + "c" + strings.Repeat(" : d", n) + ";\n"
		},
		"type-args": func(n int) string { return "f(a < b, c > (d));\n" },
		"types":     func(n int) string { return "let x: " + strings.Repeat("A<", n) + "B" + strings.Repeat(">", n) + ";\n" },
	}
	for name, shape := range shapes {
		// A unit nests a thousand levels; a megabyte of them.
		unit := shape(1000)
		src := strings.Repeat(unit, max(1, 1<<20/len(unit)))
		p := newParser("t.ts", src, nil, true)
		p.ts, p.tsx = true, &tsShared{}
		require.NoError(t, p.run(func() { p.parseProgram() }), name)
		assert.LessOrEqual(t, p.tsx.rewound, 2*len(src), name)
	}
	for _, src := range []string{
		"x = a" + strings.Repeat(" < b", 1<<18) + ";",
		"x = " + strings.Repeat("<T>(", 1<<18),
		"x = " + strings.Repeat("(", 1<<18),
		"let x: " + strings.Repeat("A<", 1<<18),
		"x = " + strings.Repeat("a ? (b): T => ", 1<<16),
	} {
		_, err := ParseModule("t.ts", src, tsOpts)
		assert.ErrorContains(t, err, "Nesting too deep", src[:20])
	}
}
