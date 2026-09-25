export function run() {
  return {
    parse: [parseInt("42px"), parseInt("0x1f"), parseInt("101", 2), parseInt(""), parseFloat("3.14abc"), parseFloat(".5"), parseFloat("-.5e2"), Number(""), Number(" 12 "), Number("0b101"), Number("0o17"), Number("1_000"), Number(null), Number(undefined), Number([]), Number([7]), Number([1, 2]), Number(true), Number("Infinity"), Number("-0")].map(String),
    checks: [Number.isInteger(5.0), Number.isInteger(5.5), Number.isSafeInteger(2 ** 53), Number.isNaN("x"), isNaN("x"), Number.isFinite("1"), isFinite("1"), Number.MAX_SAFE_INTEGER, Number.EPSILON > 0, Number.MIN_VALUE, Number.MAX_VALUE],
    math: [Math.max(1, 5, 3), Math.min(), Math.max(), Math.round(2.5), Math.round(-2.5), Math.round(0.49999999999999994), Math.floor(-1.5), Math.ceil(-1.5), Math.trunc(-1.7), Math.sign(-3), Math.abs(-0), Math.pow(2, 10), 2 ** 0.5, Math.sqrt(2), Math.cbrt(27), Math.hypot(3, 4), Math.log2(8), Math.log10(1000), Math.clz32(1), Math.imul(3, 4), Math.fround(5.5), Math.atan2(1, 1), Math.PI, Math.E].map(String),
    ops: [7 % 3, -7 % 3, 7 % -3, 5.5 % 2, 1 / 3, 0.1 + 0.2, 1e21 + 1, 2 ** 53 + 1, 9007199254740993, -0 === 0, String(1 / 0), String(-1 / 0), 0 / 0 !== 0 / 0],
    bitwise: [5 & 3, 5 | 3, 5 ^ 3, ~5, 1 << 31, -1 >>> 0, -8 >> 1, 2 ** 32 | 0, 1.9 | 0, -1.9 | 0, 4294967296 >>> 0, "12" & 6],
    toString: [(255).toString(2), (255).toString(36), (-255).toString(16), (0.5).toString(2), (1e21).toString(), (123.456).toString(), (100).toString(), (1e-7).toString(), (-1e-7).toString(), (0.000001).toString(), (123456789).toString(), (1.5e300).toString()],
    fixed: [(0).toFixed(2), (1.005).toFixed(2), (1.45).toFixed(1), (-1.5).toFixed(0), (1e21).toFixed(2), (123.456).toFixed(0), (0.1).toFixed(20)],
    precision: [(123.456).toPrecision(2), (0.000123).toPrecision(2), (123456).toPrecision(2), (1.5).toPrecision(1), (99.99).toPrecision(3)],
    exp: [(12345).toExponential(), (12345).toExponential(1), (0.00015).toExponential(2), (0).toExponential(1)],
    compare: ["10" < "9", 10 < 9, "10" < 9, "a" < "b", null == undefined, null === undefined, null == 0, "" == 0, "0" == false, [] == false, [1] == 1, NaN == NaN, "1" == 1, undefined == 0],
    incdec: (() => { let i = 1; const a = i++; const b = ++i; let s = "5"; s++; return [a, b, i, s, typeof s]; })(),
    compound: (() => { let x = 2; x **= 3; x %= 5; x <<= 2; x ??= 9; let y = null; y ??= 7; let z = 0; z ||= 4; let w = 1; w &&= 6; return [x, y, z, w]; })(),
    conversions: [+"", +" ", +"1e3", +[], +{}, +true, +"12abc", String(123), String(-0), `${-0}`, (-0).toString(), 0.1 * 3, 1e16 + 1, 123456789 * 987654321].map(String),
    bigint: [typeof 10n, String(123n), 10n === 10n, 10n === 11n],
  };
}
