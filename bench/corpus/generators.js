// Generators: laziness and the order of side effects, sent values,
// return/throw through finally, yield* delegation, generator methods and
// the generator protocol under for-of, spread, destructuring and Array.from.
function* counter(log, n) {
  log.push("start");
  try {
    for (let i = 0; i < n; i++) {
      const got = yield i;
      log.push("got " + got);
    }
    return "done";
  } finally {
    log.push("cleanup");
  }
}

function* nat() { let n = 0; for (;;) yield n++; }
function* take(it, k) { for (const x of it) { if (k-- <= 0) return; yield x; } }
function* map(it, f) { for (const x of it) yield f(x); }
function* walk(t) { if (!t) return 0; const l = yield* walk(t.l); yield t.v; const r = yield* walk(t.r); return l + r + 1; }

class Tree {
  constructor(...vs) { this.vs = vs; }
  *[Symbol.iterator]() { yield* this.vs; }
  static *pairs(o) { for (const k of Object.keys(o)) yield [k, o[k]]; }
}

const errName = f => { try { f(); return "no error"; } catch (e) { return e.constructor.name; } };

export function run() {
  const lazy = [];
  const it = counter(lazy, 3);
  lazy.push("created");
  const steps = [it.next("ignored"), it.next("a"), it.next("b"), it.next("c"), it.next("after")].map(r => [r.value, r.done]);

  const early = [];
  const e = counter(early, 5);
  e.next();
  const ret = [e.return(42), e.next()].map(r => [r.value, r.done]);

  const thrown = [];
  const t = counter(thrown, 5);
  t.next();
  const throwErr = (() => { try { t.throw(new RangeError("x")); } catch (err) { return err.constructor.name; } })();

  const caught = [];
  function* resilient() { for (;;) { try { yield "ok"; } catch (err) { caught.push(err); yield "caught " + err; } } }
  const r = resilient();
  const resumed = [r.next().value, r.throw("boom").value, r.next().value, r.return("end").value, r.next().done];

  const overridden = (function* () { try { yield 1; } finally { return "finally wins"; } })();
  overridden.next();
  const yieldInFinally = (function* () { try { yield 1; } finally { yield "cleanup step"; } })();
  yieldInFinally.next();

  const delegLog = [];
  function* inner() { try { const x = yield "i1"; delegLog.push("inner got " + x); yield "i2"; return "inner result"; } finally { delegLog.push("inner fin"); } }
  function* outer() { const v = yield* inner(); delegLog.push("outer got " + v); yield* [v.length]; }
  const deleg = [...outer()];
  const closed = [];
  const o2 = (function* () { try { yield* inner(); } finally { closed.push("outer fin"); } })();
  o2.next();
  o2.return("stop");

  const tree = {v: 4, l: {v: 2, l: {v: 1}, r: {v: 3}}, r: {v: 6, l: {v: 5}}};
  const w = walk(tree);
  const inorder = [];
  let step;
  while (!(step = w.next()).done) inorder.push(step.value);

  const forOfLog = [];
  for (const x of counter(forOfLog, 10)) { if (x === 2) break; }
  const [d1, , d3] = counter([], 10);
  const [head, ...tail] = take(nat(), 4);
  const spread = [...map(take(nat(), 5), x => x * x)];
  const from = Array.from(take(nat(), 3), (x, i) => x + i * 10);
  const maxOf = Math.max(...take(nat(), 7));
  const fromMap = [...new Map(Tree.pairs({a: 1, b: 2})).entries()];
  const setSize = new Set(map(take(nat(), 6), x => x % 3)).size;
  const obj = {*gen() { yield this === obj; }, *[Symbol.iterator]() { yield "self"; }};

  const g = function* named() {};
  const GFP = Object.getPrototypeOf(g);
  const proto = [
    typeof g.prototype, g.name, g.length, (function* (a, b) {}).length,
    Object.getPrototypeOf(g()) === g.prototype, Object.getPrototypeOf(g.prototype) === GFP.prototype,
    Object.prototype.toString.call(g()), Object.prototype.toString.call(g), GFP.constructor.name,
    typeof GFP.prototype.next, g()[Symbol.iterator]() instanceof g, Object.keys(GFP.prototype).length,
    Object.getOwnPropertyNames(g).sort().join(), g.hasOwnProperty("caller"),
  ];

  let running;
  function* reentrant() { running.next(); }
  running = reentrant();
  const errors = [
    errName(() => new g()),
    errName(() => new obj.gen()),
    errName(() => running.next()),
    errName(() => GFP.prototype.next.call({})),
    errName(() => [...(function* () { yield* 5; })()]),
    errName(() => (function* () { yield* {[Symbol.iterator]() { return {next() { return 1; }}; }}; })().next()),
  ];

  return {
    lazy, steps, early, ret, thrown, throwErr, resumed, caught,
    overridden: overridden.return(0), yieldInFinally: [yieldInFinally.return("r"), yieldInFinally.next()],
    deleg, delegLog, closed, inorder, treeSize: step.value, forOfLog, destr: [d1, d3, head, tail], spread, from, maxOf,
    fromMap, setSize, tree: [...new Tree("x", "y")], methods: [...obj.gen(), ...obj], proto, errors,
  };
}
