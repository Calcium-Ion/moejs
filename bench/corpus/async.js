// Async functions and await: the order resumptions interleave with
// reactions, awaiting values, promises and thenables, await in every kind of
// expression and statement, errors thrown and caught across awaits, finally,
// async arrows, methods and class members, and the combinators over async
// calls. run returns a promise, compared as {state, value} once the call has
// drained the job queue.
export async function run(ctx) {
  // An await of a value or native promise resumes one job later, a thenable
  // two (the resolve-thenable job, then the reaction).
  const order = [];
  const ticks = (label, n) => {
    let p = Promise.resolve();
    for (let i = 0; i < n; i++) p = p.then(() => { order.push(label + i); });
    return p;
  };
  const steps = (async () => {
    order.push("a0");
    await null;
    order.push("a1");
    await Promise.resolve("p");
    order.push("a2");
    await { then(r) { order.push("then"); r(); } };
    order.push("a3");
    return "done";
  })();
  const t = ticks("t", 6);
  order.push("sync");
  const stepsResult = await steps;
  await t;

  // An async function returning a promise adopts it: two jobs more than
  // returning a value.
  const adopt = [];
  const direct = async () => "direct";
  const adopted = async () => Promise.resolve("adopted");
  adopted().then(v => adopt.push(v));
  direct().then(v => adopt.push(v));
  Promise.resolve().then(() => adopt.push("j1")).then(() => adopt.push("j2")).then(() => adopt.push("j3"));
  await ticks("x", 4);

  // await in expressions and statements.
  const v = x => Promise.resolve(x);
  const exprs = {
    arith: (await v(2)) * (await v(3)) + await 4,
    template: `${await v("a")}-${await v("b")}`,
    array: [await v(1), ...(await v([2, 3])), await 4],
    object: { [await v("k")]: await v("value"), ...(await v({ spread: true })) },
    call: Math.max(await v(1), await v(9), 5),
    cond: (await v(false)) ? "yes" : await v("no"),
    logical: ((await v(null)) ?? (await v(0))) || await v("fallback"),
    typeofAwait: typeof await v(1n),
    destructured: await (async () => { const { a = await v("dflt"), b: [c, d = await v("d")] } = { b: ["c"] }; return [a, c, d]; })(),
    chained: (await v({ f() { return this.g; }, g: "method" })).f(),
    comma: (await v(1), await v(2)),
  };

  const loops = [];
  for (let i = 0; i < 3; i++) {
    await null;
    loops.push(() => i);
  }
  const captured = loops.map(f => f());
  let sum = 0;
  for (const x of [1, 2, 3]) sum += await v(x);
  let n = 0;
  while ((await v(n)) < 3) n++;
  const labelled = [];
  outer: for (const i of [1, 2, 3]) {
    for (const j of [1, 2, 3]) {
      if (await v(j === 2)) continue outer;
      if (await v(i === 3)) break outer;
      labelled.push(i + ":" + j);
    }
  }
  const sw = [];
  for (const k of ["a", "b", "z"]) {
    switch (await v(k)) {
      case await v("a"): sw.push("A"); break;
      case "b": sw.push("B");
      default: sw.push("default " + k);
    }
  }
  function* gen() { yield 1; yield 2; yield 3; }
  const fromGen = [];
  for (const g of gen()) fromGen.push(await v(g * 10));

  // Errors cross awaits: a throw after an await rejects the call, a
  // rejection throws where the await stands, finally runs and can override.
  const errors = [];
  const failLater = async (msg) => { await null; throw new TypeError(msg); };
  try { await failLater("later"); } catch (e) { errors.push(e.name + ": " + e.message); }
  try { await Promise.reject("raw"); } catch (e) { errors.push("raw " + e); }
  try { await { then(_, reject) { reject(new RangeError("thenable")); } }; } catch (e) { errors.push(e.constructor.name + ": " + e.message); }
  try { await { get then() { throw new SyntaxError("then getter"); } }; } catch (e) { errors.push(e.name); }
  errors.push(await failLater("caught by catch").catch(e => "catch " + e.message));
  const fin = [];
  const withFinally = async (mode) => {
    try {
      await null;
      if (mode === "throw") throw new Error("body");
      return "body";
    } catch (e) {
      fin.push("catch " + e.message);
      await null;
      return "from catch";
    } finally {
      fin.push("finally " + mode);
      await null;
      if (mode === "override") return "from finally";
    }
  };
  const finResults = [await withFinally("return"), await withFinally("throw"), await withFinally("override")];
  const rejectedWith = await (async () => { try { await null; throw 1; } finally { await Promise.resolve(); } })().then(() => "no", e => "rejected " + e);
  const syncThrow = async () => { throw new Error("sync part"); };
  const beforeAwait = syncThrow();
  const beforeState = await beforeAwait.then(() => "fulfilled", e => "rejected " + e.message);

  // Async arrows keep `this` of the enclosing function; methods and class
  // members get their own. (Sobek loses the enclosing `arguments` in an
  // async arrow, so the corpus does not read it.)
  const self = {
    name: "self",
    async method(x) { await null; return this.name + ":" + x + ":" + arguments.length; },
    arrow() { return (async () => { await null; return this.name; })(); },
  };
  class Base {
    async greet() { await null; return "base " + this.id; }
    static async make(id) { await null; const o = new this(); o.id = id; return o; }
  }
  class Derived extends Base {
    async greet() { const s = await super.greet(); return "derived(" + s + ")"; }
    async #secret() { await null; return "secret " + this.id; }
    reveal() { return this.#secret(); }
  }
  const obj = await Derived.make(7);
  const members = [
    await self.method("m", 2), await self.arrow(), await obj.greet(), await obj.reveal(),
    obj instanceof Derived,
  ];

  // Functions: metadata, and what calling one returns.
  const af = async function named(a, b) {};
  const meta = [
    af.name, af.length, typeof af, af() instanceof Promise, Object.prototype.toString.call(af),
    Object.getPrototypeOf(af) === Function.prototype, Object.getPrototypeOf(af) === Object.getPrototypeOf(async () => {}),
    "prototype" in af, (async () => {}).hasOwnProperty("prototype"),
  ];
  try { new af(); meta.push("constructed"); } catch (e) { meta.push(e.name); }
  const p1 = af(), p2 = af();
  meta.push(p1 === p2, Object.getPrototypeOf(p1) === Promise.prototype);

  // Recursion and nested calls.
  const fib = async k => k < 2 ? k : (await fib(k - 1)) + (await fib(k - 2));
  const deep = async d => d === 0 ? "bottom" : "(" + await deep(d - 1) + ")";

  // The combinators over async calls.
  const delay = async (x, hops) => { for (let i = 0; i < hops; i++) await null; return x; };
  const failAfter = async (x, hops) => { await delay(null, hops); throw x; };
  const combinators = {
    all: await Promise.all([delay("a", 3), delay("b", 1), "c"]),
    race: await Promise.race([delay("slow", 3), delay("fast", 1)]),
    allSettled: (await Promise.allSettled([delay(1, 1), failAfter("no", 2)])).map(r => r.status + ":" + (r.value ?? r.reason)),
    any: await Promise.any([failAfter("x", 1), delay("any", 2)]),
    anyRejected: await Promise.any([failAfter("e1", 2), failAfter("e2", 1)]).catch(e => e.constructor.name + " " + e.errors.join()),
    allRejected: await Promise.all([delay(1, 2), failAfter("first", 1)]).catch(e => "rejected " + e),
  };

  // Microtasks and reactions interleave with resumptions.
  const mixed = [];
  queueMicrotask(() => mixed.push("micro1"));
  const m = (async () => { mixed.push("m0"); await null; mixed.push("m1"); queueMicrotask(() => mixed.push("micro2")); await null; mixed.push("m2"); })();
  Promise.resolve().then(() => mixed.push("r1")).then(() => mixed.push("r2")).then(() => mixed.push("r3"));
  mixed.push("sync");
  await m;
  await ticks("y", 3);

  return {
    order, stepsResult, adopt, exprs, captured, sum, n, labelled, sw, fromGen,
    errors, fin, finResults, rejectedWith, beforeState, members, meta,
    fib: await fib(12), deep: await deep(5), combinators, mixed,
    model: await v(ctx.model),
  };
}
