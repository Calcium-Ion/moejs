// BigInt: operators, mixed-type errors, comparisons with numbers and
// strings, SameValueZero keys, conversions, BigInt.asIntN/asUintN and the
// prototype methods. Values are shown as strings so 1n and 1 stay apart;
// errors compare by name. Left out where Sobek deviates from the spec:
// BigInt() is a TypeError (Sobek 0n), and Number(2n ** 64n) is
// 18446744073709552000 (Sobek 0 for every bigint of 64 bits or more).
const show = (x) => (typeof x === "bigint" ? x + "n" : x);
const t = (f) => { try { return show(f()); } catch (e) { return e.name; } };
export function run() {
  const r = {};
  const big = 2n ** 100n;
  r.arith = [1n + 2n, 10n - 20n, 6n * 7n, 7n / 2n, -7n / 2n, 7n / -2n, 7n % 2n, -7n % 2n, 7n % -2n, 2n ** 64n, (-2n) ** 3n, (-2n) ** 4n, 0n ** 0n, 1n ** 1000n, (-1n) ** 1001n, big * big - 1n, big / 3n, big % 1000007n, 3n ** 200n % 998244353n].map(show);
  r.arithErrors = [() => 1n / 0n, () => 1n % 0n, () => 0n / 0n, () => 2n ** -1n, () => 1n + 1, () => 1 * 1n, () => 1n - true, () => 1n >>> 0n, () => +1n, () => Math.abs(1n), () => 1n + undefined, () => 1n + Symbol()].map(t);
  r.concat = [1n + "x", "x" + 2n, `${3n}`, -5n + "", [1n, 2n] + "", String(-0n), 1n + { toString() { return "o"; } }];
  let x = 1n;
  const steps = [x++, x, ++x, x--, --x, -x, -(-x)];
  r.unary = [-5n, -0n, ~5n, ~-1n, ~0n, ...steps].map(show);
  r.bitwise = [5n & 3n, -5n & 3n, 5n | -3n, -5n ^ 3n, -5n & -3n, 1n << 100n, -1n << 3n, -9n >> 1n, 5n >> -2n, 5n << -1n, 1n >> 1000n, -1n >> 1000n, big >> 99n, (big - 1n) & 0xffn, -big | 1n].map(show);
  r.compare = [1n < 2, 2n > 1.5, 1n < 1.5, 1n > 0.5, 1n < NaN, 1n > NaN, 1n < Infinity, -Infinity < 1n, big > 1e30, big < 1e31, 2n > "1", "10" > 9n, "x" < 1n, "x" > 1n, 1n <= "1", "0x10" > 15n, 1n < 2n, -1n > -2n, 5n >= 5n, 1n > "1.5", " 3 " > 2n];
  r.looseEq = [1n == 1, 1n == 1.5, 1n == "1", 1n == "x", 1n == "0x1", 0n == false, 1n == true, 2n == true, 0n == "", 0n == null, 1n == Object(1n), Object(1n) == 1, 1n != 2, 1n == NaN, big == 2 ** 100, 1n == [1]];
  r.strictEq = [1n === 1, 1n === 1n, big === 2n ** 100n, Object.is(0n, -0n), Object.is(1n, 1n), Object.is(1n, 1), 1n !== 1n];
  const m = new Map([[1n, "a"], [big, "big"]]);
  const s = new Set([1n, 1n, 2n, BigInt(2), 2]);
  r.keys = [m.get(1n), m.get(BigInt(1)), m.get(1), m.get(2n ** 100n), m.has(-0n), s.size, s.has(2n), s.has(2), [1n, 2n].includes(1n), [1n].includes(1), [0n].indexOf(0n), [NaN, 0n].indexOf(0n), [1n].lastIndexOf(BigInt("1"))];
  const o = {};
  o[1n] = "one";
  o[-2n] = "neg";
  r.propertyKeys = [Object.keys(o), o["1"], o[-2n], { [3n]: 1 }.hasOwnProperty("3"), [10, 20][1n], "abc"[2n]];
  r.truthy = [typeof 1n, typeof Object(1n), Boolean(0n), !!1n, !0n, 0n ? "t" : "f", 0n || "x", 1n && "y", 0n ?? "z"].map(show);
  r.ctor = [() => BigInt(1), () => BigInt(-0), () => BigInt("0x10"), () => BigInt("0o17"), () => BigInt("0b101"), () => BigInt(" 12 "), () => BigInt(""), () => BigInt("-7"), () => BigInt(true), () => BigInt(false), () => BigInt(Number.MAX_SAFE_INTEGER), () => BigInt(1e21), () => BigInt(-(2 ** 70)), () => BigInt({ valueOf() { return 5; } }), () => BigInt(Object(9n)), () => BigInt("1.5"), () => BigInt("1e3"), () => BigInt("-0x1"), () => BigInt("12n"), () => BigInt(1.5), () => BigInt(NaN), () => BigInt(Infinity), () => BigInt(undefined), () => BigInt(null), () => BigInt(Symbol()), () => new BigInt(1)].map(t);
  r.toNumber = [Number(5n), Number(-5n), Number(2n ** 53n + 1n), Number(-(2n ** 60n)), Number(Object(3n)), parseInt(10n), parseFloat(2n)];
  r.asN = [() => BigInt.asIntN(8, 255n), () => BigInt.asIntN(8, 128n), () => BigInt.asIntN(8, 127n), () => BigInt.asIntN(8, -129n), () => BigInt.asUintN(8, -1n), () => BigInt.asUintN(8, 256n), () => BigInt.asUintN(64, -1n), () => BigInt.asIntN(64, 2n ** 63n), () => BigInt.asIntN(64, 2n ** 63n - 1n), () => BigInt.asIntN(0, 5n), () => BigInt.asUintN(0, 5n), () => BigInt.asUintN(1, 3n), () => BigInt.asIntN(1, 1n), () => BigInt.asIntN(200, -big), () => BigInt.asUintN(3, "7"), () => BigInt.asIntN(3, true), () => BigInt.asIntN(2.9, 3n), () => BigInt.asIntN(-1, 1n), () => BigInt.asIntN(8, 1), () => BigInt.asUintN(2 ** 53, 1n)].map(t);
  r.proto = [() => (255n).toString(16), () => (-255n).toString(2), () => (35n).toString(36), () => big.toString(32), () => (0n).toString(), () => (7n).toString(undefined), () => (7n).toString(1), () => (7n).toString(37), () => (5n).toLocaleString(), () => (5n).valueOf(), () => Object(5n).valueOf(), () => Object(5n).toString(), () => Object(5n) + 1n, () => BigInt.prototype.toString.call(Object(6n)), () => BigInt.prototype.valueOf.call(1), () => BigInt.prototype.toString.call("1"), () => BigInt.prototype.valueOf.call({}), () => Object.prototype.toString.call(1n), () => Object.prototype.toString.call(Object(1n))].map(t);
  r.json = [() => JSON.stringify(1n), () => JSON.stringify({ a: 1n }), () => JSON.stringify([Object(1n)]), () => JSON.stringify({ a: 1n }, (k, v) => (typeof v === "bigint" ? v.toString() : v))].map(t);
  r.literals = [0x10n, 0o17n, 0b101n, 0n, 1_000_000n, 0xffff_ffff_ffff_ffffn].map(show);
  r.meta = [BigInt.length, BigInt.name, BigInt.asIntN.length, BigInt.asUintN.length, BigInt.prototype.toString.length, BigInt.prototype.constructor === BigInt, Object.getPrototypeOf(1n) === BigInt.prototype, 1n instanceof BigInt, Object(1n) instanceof BigInt, typeof BigInt.prototype.valueOf, "BigInt" in globalThis];
  let f = 1n;
  for (let i = 1n; i <= 50n; i++) f *= i;
  r.factorial = [show(f), f.toString().length, (2n ** 1000n).toString().length];
  const mixed = [3n, 1, 2n, -1n, 0.5, 10n].sort((a, b) => (a < b ? -1 : a > b ? 1 : 0));
  r.sorted = mixed.map(show);
  r.exported = [5n, -(2n ** 60n), Object(7n)];
  return r;
}
