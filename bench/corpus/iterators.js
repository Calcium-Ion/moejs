// Iteration protocol: for-of, spread, destructuring and Array.from over
// builtin and user iterators, close-on-abrupt-exit, Symbol basics.
function range(n) {
  let i = 0;
  const log = [];
  const it = {
    [Symbol.iterator]() { return this; },
    next() { log.push("n" + i); return i < n ? {value: i++, done: false} : {value: "end", done: true}; },
    return(v) { log.push("r"); return {done: true, value: v}; },
  };
  return {it, log};
}

// closer returns an iterable whose iterators log their steps to log, yield
// 1, 2, ... (undefined if holes) and whose return method returns ret(name).
function closer(log, name, ret, holes) {
  let n = 0;
  return {[Symbol.iterator]() { return {
    next() { log.push(name + " next " + (++n)); return {value: holes ? undefined : n, done: false}; },
    return() { log.push(name + " return"); return ret(name); },
  }; }};
}

// closeAtLoop exits for-of loops from inside try statements of their
// bodies: the iterator closes where the loop completes, so a throwing or
// non-object return() replaces the exit's completion past the body's catch
// clauses and closes the loops around it with the throw completion.
function closeAtLoop() {
  const log = [];
  const it = (name, ret, holes) => closer(log, name, ret, holes);
  const thr = name => { throw new Error(name + " retThrow"); };
  const inner = (name, e) => log.push(name + " inner caught " + e.message);
  const fns = [
    function s() { for (const x of it("s", thr)) { try { return "ret " + x; } catch (e) { inner("s", e); } } return "end"; },
    function b() { outer: for (const y of [1]) { for (const x of it("b", () => 5)) { try { break outer; } catch (e) { inner("b", e); } } } return "end"; },
    function c() { outer: for (const y of [1, 2]) { for (const x of it("c" + y, thr)) { try { try { continue outer; } finally { log.push("c fin"); } } catch (e) { inner("c", e); } } } return "end"; },
    function d() { let r = 0; for (const o of [1, 2]) { try { for (const x of it("d" + o, thr)) { try { break; } catch (e) { inner("d", e); } } } catch (e) { log.push("d caught " + e.message); r++; } } return "end " + r; },
    function e() { for (const x of it("e1", thr)) { for (const y of it("e2", () => ({}))) { try { return "ret"; } catch (err) { inner("e", err); } } } },
    function f() { for (const x of it("f", thr)) { try { break; } catch (e) { inner("f", e); } finally { log.push("f fin"); } } return "end"; },
    function lb() { lbl: { for (const x of it("lb", thr)) { try { break lbl; } catch (e) { inner("lb", e); } } } return "end"; },
    function fin() { try { } finally { for (const x of it("fin", thr)) { try { break; } catch (e) { inner("fin", e); } } } return "end"; },
    function cat() { for (const x of it("cat", thr)) { try { throw 1; } catch { try { break; } catch (e) { inner("cat", e); } } } return "end"; },
    function tc() { for (const x of it("tc", thr)) { try { try { return "ret"; } catch (e) { inner("tc1", e); } } catch (e) { inner("tc2", e); } } },
  ];
  for (const fn of fns) {
    try { log.push(fn.name + " = " + fn()); } catch (e) { log.push(fn.name + " threw " + (e instanceof TypeError ? "TypeError" : e.message)); }
  }
  const gens = [
    function* g() { for (const x of it("g", thr)) { try { yield x; } catch (e) { inner("g", e); } } },
    function* p() { try { const [a = yield 1] = it("p", thr); log.push("p after " + a); } catch (e) { log.push("p caught " + e.message); } return "end"; },
    function* gg() { for (const a of it("gg1", thr)) { try { for (const b of it("gg2", () => ({}))) { try { yield b; } catch (e) { inner("gg2", e); } } } catch (e) { inner("gg1", e); } } },
    function* gh() { for (const a of it("gh1", () => ({}))) { try { for (const b of it("gh2", thr)) { try { yield b; } catch (e) { inner("gh2", e); } } } catch (e) { inner("gh1", e); } } return "end"; },
    function* q() { for (const x of it("q1", thr)) { const [a = yield 1] = it("q2", () => ({}), true); } },
    function* r() { for (const x of it("r1", thr)) { try { const [[a = yield 1]] = [it("r2", () => ({}), true)]; } catch (e) { inner("r", e); } } },
    function* u() { for (const x of it("u1", thr)) { const [a = yield 1] = it("u2", thr, true); } },
  ];
  for (const G of gens) {
    const g = G();
    g.next();
    try { log.push(G.name + ".return " + JSON.stringify(g.return("R"))); } catch (e) { log.push(G.name + ".return threw " + e.message); }
    log.push(G.name + ".next " + JSON.stringify(g.next()));
  }
  return log;
}

export function run() {
  const forOf = [];
  for (const x of [1, 2, 3]) forOf.push(x * 2);
  for (const ch of "a😀b") forOf.push(ch);
  for (const [k, v] of Object.entries({p: 1, q: 2})) forOf.push(k + v);

  const early = range(5);
  for (const x of early.it) { if (x === 1) break; }
  const thrown = range(5);
  try { for (const x of thrown.it) { if (x === 2) throw new Error("stop"); } } catch (e) { thrown.log.push(e.message); }
  const done = range(2);
  const spread = [...done.it];
  const destr = range(10);
  const [a, , b] = destr.it;
  const rest = range(4);
  const [first, ...others] = rest.it;
  const from = Array.from(range(3).it, (x, i) => x * 10 + i);

  const arr = ["x", "y"];
  const ai = arr[Symbol.iterator]();
  const steps = [ai.next(), ai.next(), ai.next(), ai.next()].map(r => [r.value, r.done]);
  const keys = [...arr.keys()], entries = [...arr.entries()], values = [...arr.values()];
  const si = "ab"[Symbol.iterator]();
  const protoChain = [
    typeof Symbol.iterator, Symbol.iterator.toString(), Symbol("d").description, Symbol().description,
    Object.getPrototypeOf(ai) === Object.getPrototypeOf([].values()),
    ai[Symbol.iterator]() === ai, typeof si.next, Object.prototype.toString.call(ai), Object.prototype.toString.call(si),
    Array.prototype[Symbol.iterator] === Array.prototype.values,
  ];

  const sym = Symbol("tag");
  const reg = Symbol.for("app.key");
  const o = {[sym]: 1, plain: 2};
  const symbols = [
    typeof sym, sym.toString(), String(sym), Symbol.keyFor(reg), Symbol.keyFor(sym), Symbol.for("app.key") === reg,
    Object.keys(o).join(), Object.getOwnPropertySymbols(o).length, o[sym], JSON.stringify(o), sym in o,
    (() => { try { return "" + sym; } catch (e) { return e.constructor.name; } })(),
  ];

  const args = ((...xs) => [...xs])(1, 2, 3);
  const mapped = [..."abc"].map((c, i) => c + i);
  const nested = [[1, [2, 3]], [4, [5, 6]]].map(([x, [y, z]]) => x + y + z);
  const typeErr = (() => { try { const [q] = {}; return q; } catch (e) { return e.constructor.name; } })();
  const notIter = (() => { try { for (const q of 5) {} return "no"; } catch (e) { return e.constructor.name; } })();
  const badNext = (() => { try { [...{[Symbol.iterator]() { return {next() { return 1; }}; }}]; return "no"; } catch (e) { return e.constructor.name; } })();

  return {
    forOf, early: early.log, thrown: thrown.log, done: [spread, done.log], destr: [a, b, destr.log], rest: [first, others, rest.log],
    from, steps, keys, entries, values, protoChain, symbols, args, mapped, nested, errors: [typeErr, notIter, badNext],
    closeAtLoop: closeAtLoop(),
  };
}
