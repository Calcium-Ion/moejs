function fib(n) { return n < 2 ? n : fib(n - 1) + fib(n - 2); }
function counter() { let c = 0; return { inc: () => ++c, get: () => c }; }
function sum(...xs) { return xs.reduce((a, b) => a + b, 0); }
function defaults(a, b = a * 2, { c = 3, d } = {}, [e = 5] = []) { return [a, b, c, d, e]; }
export function run() {
  const c = counter(); c.inc(); c.inc();
  let sw = "";
  for (const v of [1, "1", 2, 3, 99]) { switch (v) { case 1: sw += "one"; break; case "1": sw += "str"; case 2: sw += "two"; break; default: sw += "d"; } }
  let labeled = 0;
  outer: for (let i = 0; i < 5; i++) { for (let j = 0; j < 5; j++) { if (j === 2) continue outer; if (i === 3) break outer; labeled += i * 10 + j; } }
  let w = 0, dw = 0;
  while (w < 5) w++;
  do { dw++; } while (dw < 3);
  const closures = []; for (let i = 0; i < 3; i++) closures.push(() => i);
  const varClosures = []; for (var k = 0; k < 3; k++) varClosures.push(() => k);
  let forIn = []; for (const key in { b: 1, a: 2, 1: 3 }) forIn.push(key);
  const tdz = (() => { try { x; let x = 1; return "no"; } catch (e) { return e instanceof ReferenceError; } })();
  const constAssign = (() => { const q = 1; try { q = 2; return "no"; } catch (e) { return e instanceof TypeError; } })();
  const comma = (1, 2, 3);
  const ternary = fib(5) > 4 ? "big" : "small";
  const hoisted = hoistedFn();
  function hoistedFn() { return typeof hoistedVar; var hoistedVar = 1; }
  const iife = (function (n) { return n * 2; })(21);
  const arrowThis = { v: 1, f() { return [1, 2].map(x => x + this.v); } }.f();
  const recursion = (function fact(n) { return n <= 1 ? 1 : n * fact(n - 1); })(10);
  const strictThis = (function () { return this === undefined; })();
  const voids = [void 0, typeof void 0, !!"", !!"0", !![], !!{}, !!NaN, !!-0, !!0n];
  const deleteRes = (() => { const o = { a: 1 }; return [delete o.a, delete o.a, "a" in o]; })();
  const args = (() => { const inner = (...r) => r.length; return [inner(), inner(1, 2, 3), inner(...[1, 2], 3), sum(1, 2, 3), Math.max(...[1, 5, 3])]; })();
  const strConcat = (() => { let s = ""; for (let i = 0; i < 100; i++) s += i % 10; return [s.length, s.slice(0, 12), s.indexOf("9876")]; })();
  return { fib: fib(15), counter: c.get(), sw, labeled, w, dw, closures: closures.map(f => f()), varClosures: varClosures.map(f => f()), forIn, tdz, constAssign, comma, ternary, hoisted, iife, arrowThis, recursion, strictThis, voids, deleteRes, args, strConcat, defaults: [defaults(1), defaults(1, 5, { c: 7, d: 8 }, [9])], typeofUndeclared: typeof notDeclaredAnywhere, chained: [[1, 2, 3].map(x => x * 2).filter(x => x > 2).reduce((a, b) => a + b)], exponent: [2 ** 3 ** 2, (-2) ** 2], inExpr: "a" in { a: 1 }, globalThis: typeof globalThis, undefinedGlobal: globalThis.undefined, NaNGlobal: Number.isNaN(globalThis.NaN) };
}
