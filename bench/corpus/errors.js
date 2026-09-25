function thrower(kind) {
  if (kind === "type") throw new TypeError("bad type");
  if (kind === "range") throw new RangeError("out of range");
  if (kind === "plain") throw "plain string";
  if (kind === "obj") throw { code: 42 };
  if (kind === "ref") return undefinedVariable;
  if (kind === "call") return null.method();
  if (kind === "cause") throw new Error("outer", { cause: new Error("inner") });
  return "ok";
}
function attempt(kind) {
  try {
    return { ok: thrower(kind) };
  } catch (e) {
    return {
      name: e && e.name, message: kind === "call" ? typeof (e && e.message) : e && e.message, isError: e instanceof Error, type: typeof e, str: kind === "call" ? e.name : String(e), hasStack: typeof (e && e.stack) === "string",
      cause: e && e.cause instanceof Error ? e.cause.message : undefined, ownKeys: e instanceof Error ? Object.keys(e) : Object.keys(e || {}), exported: e instanceof Error ? undefined : e,
    };
  } finally {
    globalThis.__finallyRan = (globalThis.__finallyRan || 0) + 1;
  }
}
export function run() {
  globalThis.__finallyRan = 0;
  const results = ["type", "range", "plain", "obj", "ref", "call", "cause", "none"].map(attempt);
  let nested = "";
  try { try { throw new Error("a"); } finally { nested += "f1"; } } catch (e) { nested += e.message; } finally { nested += "f2"; }
  const completion = (() => { try { return "try"; } finally { nested += "f3"; } })();
  const loop = (() => { const out = []; for (let i = 0; i < 5; i++) { try { if (i === 1) continue; if (i === 3) break; out.push(i); } finally { out.push("f" + i); } } return out; })();
  const err = new Error("msg");
  err.name = "Custom";
  err.extra = 1;
  const e2 = new SyntaxError();
  return {
    results, finallyRan: globalThis.__finallyRan, nested, completion, loop,
    custom: [err.name, String(err), err.toString(), Object.keys(err), JSON.stringify(err), err.message, Error.prototype.name, TypeError.prototype.name, e2.message, String(e2), String(new Error(undefined)), String(new Error(null))],
    stackShape: (() => { const s = new Error("x").stack.split("\n"); return [s[0], s.length > 1, /at /.test(s[1] || "")]; })(),
    rethrow: (() => { try { try { throw new TypeError("t"); } catch (e) { throw e; } } catch (e) { return e.constructor === TypeError && e.message; } })(),
    errorsAsValues: [new Error("v").message, Error("no new").message, new RangeError("r") instanceof RangeError, new EvalError("e").name, new URIError("u").name, new ReferenceError("f").name],
    typeErrors: (() => { const out = []; for (const f of [() => undefined.x, () => (void 0)(), () => ({}).x.y, () => null[0], () => 1(), () => new 1(), () => [].reduce((a, b) => a), () => "x".repeat(-1), () => new Array(-1), () => (1).toFixed(101), () => decodeURIComponent("%"), () => JSON.parse("")]) { try { f(); out.push("no"); } catch (e) { out.push(e.constructor.name); } } return out; })(),
  };
}
