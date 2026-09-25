export function run() {
  const d = new Date(1700000000123);
  const iso = new Date("2026-09-23T10:20:30.456Z");
  const dateOnly = new Date("2026-09-23");
  const invalid = new Date("not a date");
  let invalidIsoErr = false;
  try { invalid.toISOString(); } catch (e) { invalidIsoErr = e instanceof RangeError; }
  return {
    date: [d.getTime(), d.valueOf(), d.toISOString(), d.toJSON(), +d, iso.getTime(), dateOnly.getTime(), Number.isNaN(invalid.getTime()), invalidIsoErr, JSON.stringify({ d }), typeof Date.now(), Date.now() > 1600000000000, new Date(d).getTime(), new Date(0).toISOString(), new Date(-1).toISOString(), new Date(8.64e15).toISOString(), Number.isNaN(new Date(8.64e15 + 1).getTime()), d > iso, d - iso, String(d.getTime() === new Date(d.toISOString()).getTime())],
    encode: [encodeURIComponent("a b/é?&=#日本"), encodeURI("https://x.com/a b/é?q=1&r=#frag"), encodeURIComponent("😀"), encodeURIComponent("-_.!~*'()"), encodeURIComponent(";,/?:@&=+$"), encodeURI(";,/?:@&=+$")],
    decode: [decodeURIComponent("a%20b%2F%C3%A9%3F"), decodeURI("https://x.com/a%20b/%C3%A9?q=1%26r"), decodeURIComponent("%F0%9F%98%80"), decodeURI("%2F%3F%23"), decodeURIComponent("%2F%3F%23"), decodeURIComponent("plain")],
    uriErrors: (() => { const out = []; for (const s of ["%", "%E0%A4%A", "%FF", "\ud800"]) { try { out.push(s === "\ud800" ? encodeURIComponent(s) : decodeURIComponent(s)); } catch (e) { out.push(e.name); } } return out; })(),
  };
}
