// ArrayBuffer and DataView: construction, slice, the getters and setters of
// every element type in both byte orders, conversions on store, bounds and
// the byte-parsing shapes plugins use them for.
export function run() {
  const b = new ArrayBuffer(16);
  const buffers = [
    b.byteLength, Object.prototype.toString.call(b), b instanceof ArrayBuffer, typeof ArrayBuffer.prototype.slice,
    new ArrayBuffer().byteLength, new ArrayBuffer(2.9).byteLength, new ArrayBuffer("3").byteLength,
    ArrayBuffer.isView(b), ArrayBuffer.isView(new DataView(b)), ArrayBuffer.isView({}),
    ArrayBuffer.length, DataView.length, ArrayBuffer.name, DataView.name,
  ];

  const v = new DataView(b);
  v.setUint32(0, 0x11223344);
  v.setUint16(4, 0x5566, true);
  v.setInt8(6, -2);
  v.setUint8(7, 300);
  v.setFloat32(8, 1.1);
  v.setInt32(12, -123456789, true);
  const bytes = [];
  for (let i = 0; i < 16; i++) bytes.push(v.getUint8(i));
  const reads = [
    v.getUint32(0).toString(16), v.getUint32(0, true).toString(16), v.getInt16(0), v.getUint16(4), v.getUint16(4, true),
    v.getInt8(6), v.getUint8(6), v.getUint8(7), v.getFloat32(8), v.getFloat32(8, true), v.getInt32(12, true), v.getInt32(12),
    v.getUint32(12, true), v.getFloat64(0), v.getFloat64(8, true),
  ];
  const bigints = [
    String(v.getBigInt64(0)), String(v.getBigUint64(0)), String(v.getBigInt64(8, true)), String(v.getBigUint64(8, true)),
    typeof v.getBigInt64(0), v.getBigUint64.length,
  ];
  const big = new DataView(new ArrayBuffer(8));
  for (const x of [0n, -1n, 1n, 2n ** 63n, -(2n ** 63n) - 1n, 2n ** 64n + 7n, -(2n ** 70n) + 3n, true, "42", "-0x1f"]) {
    big.setBigInt64(0, x);
    bigints.push(String(big.getBigInt64(0)), String(big.getBigUint64(0, true)));
    big.setBigUint64(0, x, true);
    bigints.push(String(big.getBigUint64(0, true)), big.getUint8(0));
  }

  const w = new DataView(new ArrayBuffer(16));
  const stores = [];
  for (const x of [0, -0, 1.5, -1.5, 2.5, 255.5, 256, -129, 40000, 2 ** 31, 2 ** 32 + 5, -(2 ** 53), NaN, Infinity, -Infinity, "12", true, null, undefined]) {
    w.setInt8(0, x); w.setUint8(1, x); w.setInt16(2, x); w.setUint16(4, x, true); w.setInt32(8, x); w.setUint32(12, x, true);
    stores.push([w.getInt8(0), w.getUint8(1), w.getInt16(2), w.getUint16(4, true), w.getInt32(8), w.getUint32(12, true)].join());
  }
  const floats = [];
  for (const x of [0.1, -0, 1 / 3, 3.4028235677973366e38, 1e39, -1e-46, 1e-45, 5e-324, NaN]) {
    w.setFloat32(0, x); w.setFloat64(8, x, true);
    const f32 = w.getFloat32(0);
    floats.push(String(f32), String(w.getFloat64(8, true)), Object.is(f32, -0), w.getUint32(0).toString(16));
  }

  const sub = new DataView(b, 4, 8);
  const tail = new DataView(b, 10);
  const views = [
    sub.byteOffset, sub.byteLength, sub.buffer === b, sub.getUint16(0, true), tail.byteLength, new DataView(b, 16).byteLength,
    Object.prototype.toString.call(sub),
  ];

  const s = b.slice(4, 12);
  const sv = new DataView(s);
  sv.setUint8(0, 0);
  const slices = [
    s.byteLength, sv.getUint8(1), v.getUint8(4), b.slice(-4).byteLength, b.slice(12, 4).byteLength, b.slice(0, 100).byteLength,
    b.slice(-100, 2).byteLength, b.slice().byteLength, b.slice(1.9, 3.9).byteLength,
  ];

  const errors = [];
  for (const f of [
    () => new DataView(b, 17),
    () => new DataView(b, 8, 9),
    () => new DataView({}),
    () => v.getUint32(14),
    () => v.getUint8(-1),
    () => v.setFloat64(9, 1),
    () => new ArrayBuffer(-1),
    () => ArrayBuffer(1),
    () => DataView(b),
    () => DataView.prototype.getUint8.call(b, 0),
    () => ArrayBuffer.prototype.slice.call(v),
    () => v.setBigInt64(0, 1),
    () => v.setBigUint64(0, "x"),
    () => v.setBigInt64(12, 1n),
  ]) {
    try { f(); errors.push("none"); } catch (e) { errors.push(e.name); }
  }

  // A length-prefixed record: u16 count, then (u8 tag, f64 LE value) pairs.
  const rec = new DataView(new ArrayBuffer(2 + 3 * 9));
  rec.setUint16(0, 3);
  [[1, 0.5], [2, -8], [7, 1e21]].forEach(([tag, x], i) => { rec.setUint8(2 + i * 9, tag); rec.setFloat64(3 + i * 9, x, true); });
  const parsed = [];
  for (let i = 0, n = rec.getUint16(0); i < n; i++) parsed.push([rec.getUint8(2 + i * 9), rec.getFloat64(3 + i * 9, true)]);

  return [buffers, bytes, reads, bigints, stores, floats, views, slices, errors, parsed];
}
