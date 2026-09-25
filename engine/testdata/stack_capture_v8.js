const first = (s) => s.split("\n")[0];
export function desc() { const o = {}; Error.captureStackTrace(o); const d = Object.getOwnPropertyDescriptor(o, "stack"); return [typeof d.get, typeof d.set, d.enumerable, d.configurable].join(); }
export function plainHeader() { const o = {}; Error.captureStackTrace(o); return o.stack; }
export function lazyHeader() { const o = {}; Error.captureStackTrace(o); o.message = "late"; o.name = "N"; return first(o.stack); }
export function customHeader() { const h = { name: "H", message: "hm" }; Error.captureStackTrace(h); return h.stack; }
export function nonObject() { try { Error.captureStackTrace(1); } catch (e) { return String(e); } }
export function ret() { return String(Error.captureStackTrace({})) + " " + Error.captureStackTrace.length; }
export function stlDesc() { const d = Object.getOwnPropertyDescriptor(Error, "stackTraceLimit"); return [d.value, d.writable, d.enumerable, d.configurable].join(); }
export function cstDesc() { const d = Object.getOwnPropertyDescriptor(Error, "captureStackTrace"); return [d.writable, d.enumerable, d.configurable].join(); }
function a() { return b(); }
function b() { const x = {}; Error.captureStackTrace(x, b); return x; }
export function skipFn() { return a().stack; }
function notOnStack() {}
export function skipMissing() { const x = {}; Error.captureStackTrace(x, notOnStack); return x.stack; }
export function skipBound() { const x = {}; Error.captureStackTrace(x, skipBound.bind(null)); return x.stack; }
function withLimit(v, f) { const old = Error.stackTraceLimit; Error.stackTraceLimit = v; try { return f(); } finally { Error.stackTraceLimit = old; } }
export function limit0() { return withLimit(0, () => new Error("z").stack); }
export function limitUndef() { return withLimit(undefined, () => String(new Error("z").stack)); }
export function limitStr() { return withLimit("3", () => String(new Error("z").stack)); }
export function limitFrac() { return withLimit(1.9, function lf() { return new Error("z").stack; }); }
export function limitNeg() { return withLimit(-5, () => new Error("z").stack); }
export function limitNaN() { return withLimit(NaN, () => new Error("z").stack); }
export function limitDeleted() { return withLimit(10, () => { delete Error.stackTraceLimit; const s = String(new Error("z").stack); Error.stackTraceLimit = 10; return s; }); }
export function limitCapture() { return withLimit(undefined, () => { const o = {}; Error.captureStackTrace(o); return String(o.stack); }); }
function rec(n) { return n === 0 ? new Error("deep") : rec(n - 1); }
export function deep() { return String(rec(40).stack.split("\n").length); }
export function deepLimit() { return withLimit(25, () => String(rec(40).stack.split("\n").length)); }
export function deepInf() { return withLimit(Infinity, () => String(rec(40).stack.split("\n").length)); }
export function frozen() { try { Error.captureStackTrace(Object.freeze({})); } catch (x) { return String(x); } }
export function recap() { const e = new Error("r"); delete e.stack; Error.captureStackTrace(e); return e.stack; }
export function recapErr() { const e = new TypeError("t"); Error.captureStackTrace(e); return e.stack; }
export function protoStack() { return String(Object.getOwnPropertyDescriptor(Error.prototype, "stack")); }
export function multiLine() { return new Error("a\nb").stack; }
export function emptyName() { const e = new Error("m"); e.name = ""; return first(e.stack); }
export function assigned() { const o = {}; Error.captureStackTrace(o); o.stack = 5; return String(o.stack); }
export function wrapper() { function MyErr(m) { const e = new Error(m); Error.captureStackTrace(e, MyErr); return e; } return MyErr("w").stack; }
export function capSelf() { const o = {}; (function cs() { Error.captureStackTrace(o); })(); return o.stack; }
