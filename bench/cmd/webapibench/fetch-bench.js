// fetch-bench.js measures fetch in Node.js and in moejs with package webapi,
// against the server of webapibench -serve: a small JSON request at 1 and 8
// concurrent, a large body read with getReader, and a stream of server-sent
// events. It prints one JSON line per measurement.
const BASE = globalThis.BASE ?? process.env.BASE;
const print = globalThis.print ?? console.log;
const mem = globalThis.memstats ?? (() => null); // [mallocs, bytes] in moejs
// cpu is the process's CPU time (user + system) in microseconds.
const cpu = globalThis.cpuusage ?? (() => { const u = process.cpuUsage(); return u.user + u.system; });
const ROUNDS = Number(globalThis.ROUNDS ?? process.env.ROUNDS ?? 5);

const REQ = JSON.stringify({ model: "m", stream: false, messages: [{ role: "user", content: "Say hello." }] });

async function small(n, conc) {
  let i = 0;
  async function worker() {
    while (i++ < n) {
      const r = await fetch(BASE + "/small", { method: "POST", headers: { "content-type": "application/json", authorization: "Bearer k" }, body: REQ });
      const j = await r.json();
      if (!j.choices) throw new Error("bad response");
    }
  }
  await Promise.all(Array.from({ length: conc }, worker));
}

async function stream(mb) {
  const r = await fetch(BASE + "/stream?mb=" + mb);
  const rd = r.body.getReader();
  let bytes = 0, chunks = 0;
  for (;;) {
    const { done, value } = await rd.read();
    if (done) break;
    bytes += value.length;
    chunks++;
  }
  return { bytes, chunks };
}

async function sse(n) {
  const r = await fetch(BASE + "/sse?n=" + n);
  const rd = r.body.getReader();
  const dec = new TextDecoder();
  let buf = "", events = 0, chunks = 0;
  for (;;) {
    const { done, value } = await rd.read();
    if (done) break;
    chunks++;
    buf += dec.decode(value, { stream: true });
    let i;
    while ((i = buf.indexOf("\n\n")) >= 0) {
      JSON.parse(buf.slice(6, i));
      events++;
      buf = buf.slice(i + 2);
    }
  }
  if (events !== n) throw new Error("events " + events);
  return { events, chunks };
}

async function measure(name, units, f) {
  await f(); // warm-up
  for (let round = 0; round < ROUNDS; round++) {
    const m0 = mem();
    const c0 = cpu();
    const t0 = performance.now();
    const extra = await f();
    const ms = performance.now() - t0;
    const c1 = cpu();
    const m1 = mem();
    const row = { name, round, ms: +ms.toFixed(2), units, usPerUnit: +(ms * 1000 / units).toFixed(3), cpuUsPerUnit: +((c1 - c0) / units).toFixed(3) };
    if (extra) Object.assign(row, extra);
    if (extra && extra.bytes) row.MBps = +(extra.bytes / ms / 1000).toFixed(1);
    if (extra && extra.chunks) {
      row.usPerChunk = +(ms * 1000 / extra.chunks).toFixed(3);
      row.cpuUsPerChunk = +((c1 - c0) / extra.chunks).toFixed(3);
    }
    if (m0) {
      row.allocsPerUnit = +((m1[0] - m0[0]) / units).toFixed(1);
      row.bytesPerUnit = Math.round((m1[1] - m0[1]) / units);
      if (extra && extra.chunks) row.allocsPerChunk = +((m1[0] - m0[0]) / extra.chunks).toFixed(2);
    }
    print(JSON.stringify(row));
  }
}

const only = globalThis.ONLY ?? process.env.ONLY ?? "";
const want = name => only === "" || only.split(",").includes(name);
(async () => {
  if (want("small1")) await measure("small1", 2000, () => small(2000, 1));
  if (want("small8")) await measure("small8", 4000, () => small(4000, 8));
  if (want("stream")) await measure("stream", 1, () => stream(256));
  if (want("sse")) await measure("sse", 20000, () => sse(20000));
})().catch(e => { print("ERROR " + (e && e.stack || e)); });
