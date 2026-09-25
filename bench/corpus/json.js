export function run() {
  const text = '{"a":1,"b":[true,null,"x",1.5e3],"c":{"d":"\\u00e9\\n","e":-0.0,"f":1e21,"g":12345678901234567890},"h":"日本語","i":"\\ud83d\\ude00"}';
  const v = JSON.parse(text);
  const cyc = { x: 1 }; cyc.self = cyc;
  let cycErr;
  try { JSON.stringify(cyc); } catch (e) { cycErr = e instanceof TypeError; }
  let parseErr;
  try { JSON.parse("{bad"); } catch (e) { parseErr = e instanceof SyntaxError; }
  const revived = JSON.parse('{"n":"1","m":{"n":"2"}}', (k, val) => (k === "n" ? Number(val) : val));
  // A replacer list long enough to be deduplicated through a set.
  const longKeys = [], longObj = { 3: "three" };
  for (let i = 0; i < 80; i++) { longKeys.push("k" + (i % 50)); longObj["k" + (79 - i)] = i; }
  longKeys.push(3, "3", new String("k1"), new Number(3), "k60", {}, true);
  return {
    replacerLong: JSON.stringify(longObj, longKeys),
    parsed: v, keys: Object.keys(v), nested: v.c.d, big: v.c.g, neg0: String(1 / v.c.e),
    str: JSON.stringify(v), pretty: JSON.stringify({ a: [1, { b: 2 }], c: "x" }, null, 2), tabbed: JSON.stringify([1, [2]], null, "\t"),
    replacer: JSON.stringify({ a: 1, b: 2, c: { a: 3, d: 4 } }, ["a", "c"]),
    replacerFn: JSON.stringify({ a: 1, b: "x", c: [1, 2] }, (k, val) => (typeof val === "number" ? val * 2 : val)),
    special: JSON.stringify({ u: undefined, f: function () {}, n: NaN, i: -Infinity, d: new Date(0), nested: [undefined, function () {}] }),
    toJSON: JSON.stringify({ toJSON() { return { custom: true }; } }),
    escapes: JSON.stringify("\u2028\u2029\"\\\b\f\n\r\t\u0001<>&'"),
    numbers: JSON.stringify([0, -0, 1e21, 1e-7, 123456789012345680000, 0.1 + 0.2, 5e-324, 1.7976931348623157e308]),
    order: JSON.stringify({ b: 1, a: 2, 10: 3, 2: 4, "-1": 5, "01": 6 }),
    errors: [cycErr, parseErr], revived: revived, spaced: JSON.stringify({ a: 1 }, null, 20).length, indentStr: JSON.stringify([1], null, "abcdefghijkl"),
    roundtrip: JSON.parse(JSON.stringify({ x: [1, "2", null, { y: false }] })), primitiveTop: [JSON.stringify("s"), JSON.stringify(1), JSON.stringify(null), JSON.stringify(true), JSON.parse("\"\\/\"")],
  };
}
