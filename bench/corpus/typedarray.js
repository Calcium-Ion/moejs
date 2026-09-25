// Typed arrays: construction from every source, element conversions on
// store, the prototype methods, sort orders, views over one buffer and the
// byte-crunching shapes plugins use them for (hex, checksums, packing).
export function run() {
  const ctors = [Int8Array, Uint8Array, Uint8ClampedArray, Int16Array, Uint16Array, Int32Array, Uint32Array, Float32Array, Float64Array];
  const shapes = ctors.map((C) => [C.name, C.length, C.BYTES_PER_ELEMENT, C.prototype.BYTES_PER_ELEMENT, Object.getPrototypeOf(C).name,
    Object.prototype.toString.call(new C(1)), new C(3).length, new C(3).byteLength]);

  const stores = [];
  for (const x of [0, -0, 1.5, -1.5, 2.5, 255.5, 256, -129, 40000, 2 ** 31, 2 ** 32 + 5, -(2 ** 53), NaN, Infinity, -Infinity, "12", true, null, undefined, 0.5, 254.5]) {
    stores.push(ctors.map((C) => { const a = new C(1); a[0] = x; return String(a[0]); }).join());
  }

  const sources = [
    Array.from(new Uint8Array([1, 2, 300])),
    Array.from(new Int16Array({ length: 3, 0: -1, 2: "7" })),
    Array.from(new Float32Array(new Set([0.1, 2]))),
    Array.from(new Uint16Array(new Int8Array([-1, 5]))),
    Array.from(new Int32Array(new Float64Array([1.9, -1.9, 3e9]))),
    Array.from(Uint8Array.from({ length: 3, 0: "1", 1: 2, 2: 3.7 })),
    Array.from(Int8Array.from([1, 2, 3], (x, i) => x * 100 + i)),
    Float64Array.of(1, "2", NaN).join(),
    Array.from(new Uint8Array([5, 6, 7]).entries()).join(";"),
    [...new Uint8Array([9, 8]).keys()],
  ];

  const buf = new ArrayBuffer(16);
  const u8 = new Uint8Array(buf);
  const u32 = new Uint32Array(buf, 4, 2);
  const f64 = new Float64Array(buf, 8, 1);
  u32[0] = 0x11223344;
  f64[0] = -2.5;
  const views = [
    Array.from(u8).join(), u32.byteOffset, u32.byteLength, u32.length, u32.buffer === buf, u32[1],
    new Uint8Array(buf, 8).length, new Int16Array(buf, 2, 3).byteLength,
    Array.from(new Uint8Array(buf).subarray(4, 8)).join(), new Uint8Array(buf).subarray(-2).length,
    new Uint8Array(buf).subarray(4, 8).byteOffset,
  ];

  const a = new Int16Array([5, -3, 9, 0, -3, 12, 7]);
  const methods = [
    a.indexOf(-3), a.lastIndexOf(-3), a.includes(12), a.indexOf(4), a.indexOf(-3, 2), a.lastIndexOf(-3, -4),
    a.join("|"), a.toString(), a.at(-1), a.at(9),
    a.every((x) => x > -5), a.some((x) => x > 10), a.find((x) => x > 6), a.findIndex((x) => x > 6),
    a.findLast((x) => x < 0), a.findLastIndex((x) => x < 0),
    a.reduce((s, x) => s + x, 0), a.reduceRight((s, x) => s + "," + x, ""),
    Array.from(a.map((x) => x * 3000)).join(), Array.from(a.filter((x) => x % 2)).join(),
    Array.from(a.slice(2, -2)).join(), Array.from(a.slice(-2)).join(),
    Array.from(new Int16Array(a).reverse()).join(), Array.from(new Int16Array(a).fill(4, 1, 3)).join(),
    Array.from(new Int16Array(a).copyWithin(0, 4)).join(), Array.from(new Int16Array(a).copyWithin(2, 0, 3)).join(),
  ];
  const each = [];
  a.forEach((x, i, arr) => each.push(i + ":" + x + ":" + (arr === a)));

  const set = new Uint8Array(8);
  set.set([1, 2, 3], 2);
  set.set(new Uint16Array([258, 7]), 6);
  const overlap = new Uint8Array([1, 2, 3, 4, 5, 6, 7, 8]);
  overlap.set(overlap.subarray(0, 5), 3);
  const sets = [Array.from(set).join(), Array.from(overlap).join()];

  const sorts = [
    Array.from(new Float64Array([3, -1, 10, 2.5, -Infinity, 0, 100]).sort()).join(),
    Array.from(new Int32Array([10, 9, 1, -5, 2 ** 31 - 1, -(2 ** 31)]).sort()).join(),
    Array.from(new Uint32Array([4294967295, 10, 2]).sort()).join(),
    Array.from(new Uint8Array([200, 3, 50]).sort((x, y) => y - x)).join(),
    Array.from(new Int8Array([3, 1, 2, 1]).sort((x, y) => x - y)).join(),
    Array.from(new Float32Array([0.5, NaN, -0.25]).sort()).join(),
  ];
  const big = new Float64Array(3000);
  for (let i = 0, s = 7; i < big.length; i++) { s = (s * 48271) % 2147483647; big[i] = (s % 2000) - 1000; }
  big.sort();
  let sorted = true;
  for (let i = 1; i < big.length; i++) if (big[i - 1] > big[i]) sorted = false;
  sorts.push(sorted, big[0], big[big.length - 1]);

  class Bytes extends Uint8Array {
    hex() { return Array.from(this, (b) => b.toString(16).padStart(2, "0")).join(""); }
  }
  const bytes = Bytes.from([0, 15, 16, 255]);
  const subclass = [bytes.hex(), bytes instanceof Bytes, bytes.slice(1) instanceof Bytes, bytes.subarray(2).hex(), bytes.map((b) => b ^ 0xff).hex()];

  // CRC-32 of a UTF-8 string with a Uint32Array table.
  const table = new Uint32Array(256);
  for (let n = 0; n < 256; n++) {
    let c = n;
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    table[n] = c;
  }
  const text = unescape(encodeURIComponent("The quick brown fox — 快速"));
  const data = new Uint8Array(text.length);
  for (let i = 0; i < text.length; i++) data[i] = text.charCodeAt(i);
  let crc = 0xffffffff;
  for (const b of data) crc = table[(crc ^ b) & 0xff] ^ (crc >>> 8);
  crc = (crc ^ 0xffffffff) >>> 0;

  // Pack three float samples and a header into one buffer, then read them back.
  const packed = new ArrayBuffer(4 + 3 * 4);
  new Uint16Array(packed, 0, 2).set([3, 0xbeef]);
  new Float32Array(packed, 4).set([0.25, -1.5, 1e10]);
  const header = new Uint16Array(packed, 0, 2);
  const samples = Array.from(new Float32Array(packed, 4, header[0]));
  const crunch = [crc.toString(16), data.length, Array.from(header).join(), samples.join(), Array.from(new Uint8Array(packed, 0, 4)).join()];

  const errors = [];
  for (const f of [
    () => new Uint8Array(-1),
    () => new Int32Array(new ArrayBuffer(8), 1),
    () => new Int32Array(new ArrayBuffer(7)),
    () => new Int16Array(new ArrayBuffer(4), 2, 2),
    () => Uint8Array(1),
    () => new Uint8Array(4).set([1, 2], 3),
    () => new Uint8Array(1).set([], -1),
    () => new Uint8Array(0).reduce((x, y) => x),
    () => Uint8Array.prototype.join.call([1]),
    () => new Uint8Array(1).map(1),
    () => Object.freeze(new Uint8Array(1)),
  ]) {
    try { f(); errors.push("none"); } catch (e) { errors.push(e.name); }
  }

  const exotic = new Uint8Array(2);
  exotic[5] = 1;
  exotic.x = 1;
  const keys = [Object.keys(exotic).join(), 5 in exotic, 1 in exotic, exotic[5], JSON.stringify(exotic), Object.getOwnPropertyNames(exotic).length];

  const b64 = new BigInt64Array([1n, -2n, 2n ** 63n]);
  const bu64 = new BigUint64Array(b64.buffer);
  const bigints = [Array.from(b64).map(String).join(), Array.from(bu64).map(String).join(), String(b64.sort()), typeof b64[0],
    String(BigInt64Array.from([5n, 3n], (x) => x * 2n)), b64.includes(1n)];
  for (const f of [() => new BigInt64Array([1]), () => { new Int8Array(1)[0] = 1n; }, () => new BigInt64Array(new Uint8Array(1))]) {
    try { f(); bigints.push("none"); } catch (e) { bigints.push(e.name); }
  }

  return [shapes, stores, sources, views, methods, each, sets, sorts, subclass, crunch, errors, keys, bigints];
}
