// Promise and queueMicrotask: the order reactions run in, thenable adoption,
// the combinators on mixed inputs, AggregateError, finally, withResolvers
// and microtasks interleaved with reactions. run returns a promise, compared
// as {state, value} once the call has drained the job queue.
export function run() {
  // Two chains on one promise advance one step per job, in turn.
  const order = [];
  const p0 = Promise.resolve();
  p0.then(() => order.push("a1")).then(() => order.push("a2")).then(() => order.push("a3"));
  p0.then(() => order.push("b1")).then(() => order.push("b2"));
  let settle;
  const late = new Promise(res => { order.push("executor"); settle = res; });
  late.then(v => order.push("late1:" + v));
  late.then(v => order.push("late2:" + v));
  settle("x");
  settle("y");
  order.push("sync");

  // A native promise is adopted in two extra jobs, a thenable in one.
  const adopt = [];
  new Promise(res => res(Promise.resolve("native"))).then(v => adopt.push(v));
  Promise.resolve({then(res) { adopt.push("then:" + arguments.length); res("thenable"); res("again"); }}).then(v => adopt.push(v));
  Promise.resolve().then(() => adopt.push("t1")).then(() => adopt.push("t2")).then(() => adopt.push("t3")).then(() => adopt.push("t4"));
  const adoptErrors = Promise.all([
    Promise.resolve({then() { throw new RangeError("then threw"); }}).catch(e => e.name + ": " + e.message),
    Promise.resolve({get then() { throw "getter threw"; }}).catch(e => e),
    Promise.resolve({then(res) { res("first"); throw new Error("ignored"); }}),
    (() => { let res; const p = new Promise(r => res = r); res(p); return p.catch(e => e.constructor.name); })(),
    Promise.resolve().then(() => { throw new SyntaxError("handler"); }).then(() => "skipped", e => "caught " + e.name),
    Promise.reject(1).then(5).catch(e => "passed through " + e),
    Promise.resolve(2).catch(8).then(v => "passed through " + v),
  ]);
  const same = [Promise.resolve(p0) === p0, Promise.resolve(late) === late, Object.prototype.toString.call(p0), p0 instanceof Promise, typeof p0.then];

  // The combinators take values, promises and thenables in any mix.
  const mixed = () => [1, Promise.resolve(2), {then(r) { r(3); }}, new Promise(r => queueMicrotask(() => r(4))), "five"];
  const combinators = Promise.all([
    Promise.all(mixed()),
    Promise.all([1, Promise.reject("first"), Promise.reject("second")]).catch(e => "all rejected: " + e),
    Promise.all([]),
    Promise.all(new Set([Promise.resolve("set"), "iterable"])),
    Promise.all(1).catch(e => e.constructor.name),
    Promise.allSettled([...mixed(), Promise.reject("r"), {then(_, rej) { rej("thenable r"); }}]),
    Promise.allSettled([]),
    Promise.any([Promise.reject(1), {then(r) { r("thenable"); }}, Promise.resolve("resolved")]),
    Promise.any([new Promise(r => queueMicrotask(() => r("slow"))), Promise.reject("fast")]),
    Promise.race([new Promise(() => {}), {then(r) { r("thenable"); }}, Promise.resolve("resolved")]),
    Promise.race([Promise.reject("first"), Promise.resolve("second")]).catch(e => "race rejected: " + e),
    Promise.race(["plain", Promise.resolve("promise")]),
  ]);

  // Promise.any rejects with an AggregateError of every reason, in order.
  const aggregate = Promise.all([
    Promise.any([Promise.reject(new TypeError("a")), Promise.reject("b"), {then(_, rej) { rej("c"); }}]).catch(e => [
      e.constructor.name, e.name, e instanceof AggregateError, e instanceof Error, e.errors.map(String),
      Object.prototype.hasOwnProperty.call(e, "errors"), Object.getOwnPropertyDescriptor(e, "errors").enumerable,
    ]),
    Promise.any([]).catch(e => [e.name, e.errors.length]),
    Promise.any("ab").then(v => "string input " + v),
    (() => { const e = new AggregateError(new Set([1, "x"]), "msg"); return [e.message, e.errors, e.name, String(e)]; })(),
  ]);

  // finally passes the value or the reason through unless it throws.
  const fin = [];
  const finallyResults = Promise.all([
    Promise.resolve("value").finally(function () { fin.push("f1:" + arguments.length); return "ignored"; }),
    Promise.reject("reason").finally(() => { fin.push("f2"); }).catch(e => "kept " + e),
    Promise.resolve(1).finally(() => { throw "override"; }).catch(e => "overridden " + e),
    Promise.resolve(1).finally(() => Promise.reject("rejected override")).catch(e => e),
    Promise.resolve("waited").finally(() => new Promise(r => queueMicrotask(() => { fin.push("slow finally"); r("dropped"); }))),
    Promise.resolve("not a function").finally(5),
    Promise.reject("not a function").finally().catch(e => "kept " + e),
  ]);

  // withResolvers hands out the resolving functions of a new promise.
  const wr = Promise.withResolvers();
  const resolvers = [Object.keys(wr), typeof wr.resolve, typeof wr.reject, wr.promise instanceof Promise];
  queueMicrotask(() => { wr.resolve("resolved later"); wr.reject("ignored"); });
  const wr2 = Promise.withResolvers();
  wr2.reject("rejected");
  const withResolvers = Promise.all([wr.promise, wr2.promise.catch(e => "caught " + e)]);

  // Microtasks and reactions share one queue in FIFO order.
  const mt = [];
  queueMicrotask(() => {
    mt.push("m1");
    Promise.resolve().then(() => mt.push("p in m1"));
    queueMicrotask(() => mt.push("m in m1"));
  });
  Promise.resolve().then(() => { mt.push("p1"); queueMicrotask(() => mt.push("m in p1")); });
  queueMicrotask(function () { mt.push("m2:" + (this === undefined) + ":" + arguments.length); });
  Promise.reject("r").catch(() => mt.push("catch"));
  mt.push("sync");
  const mtErrors = [];
  for (const bad of [undefined, 1, "fn", {}]) {
    try { queueMicrotask(bad); mtErrors.push("no"); } catch (e) { mtErrors.push(e.constructor.name); }
  }

  // Subclasses and species: then builds its result with the species.
  const sub = [];
  class Tracked extends Promise {
    constructor(ex) { sub.push("ctor"); super(ex); }
  }
  class Plain extends Promise {
    static get [Symbol.species]() { return Promise; }
  }
  const t = Tracked.resolve(1);
  const species = [
    t instanceof Tracked, t.then(() => {}) instanceof Tracked, Tracked.all([t]) instanceof Tracked,
    Plain.resolve(1).then() instanceof Plain, Plain.resolve(1).then() instanceof Promise, Promise[Symbol.species] === Promise,
    Plain.withResolvers().promise instanceof Plain,
  ];

  // Errors thrown synchronously by the API.
  const errors = [];
  for (const f of [
    () => Promise(() => {}),
    () => new Promise(1),
    () => Promise.prototype.then.call({}, () => {}),
    () => Promise.resolve.call(1),
    () => Promise.all.call({}, []),
    () => Promise.prototype.finally.call(1),
  ]) {
    try { f(); errors.push("no"); } catch (e) { errors.push(e.constructor.name); }
  }
  const executorThrows = new Promise(() => { throw new Error("executor"); }).catch(e => e.message);

  return Promise.all([adoptErrors, combinators, aggregate, finallyResults, withResolvers, executorThrows]).then(
    ([adoptErrorsOut, combinatorsOut, aggregateOut, finallyOut, withResolversOut, executorOut]) => ({
      order, adopt, adoptErrors: adoptErrorsOut, same, combinators: combinatorsOut, aggregate: aggregateOut,
      fin, finally: finallyOut, resolvers, withResolvers: withResolversOut, mt, mtErrors, sub, species, errors,
      executor: executorOut,
    }));
}
