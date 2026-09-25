// Plugin-style code: host-shaped objects in, mixed transformations, host-shaped objects out.
function trimmed(v) { return String(v || "").trim(); }
function walk(input) {
  const texts = [], images = [];
  for (const item of Array.isArray(input) ? input : []) {
    if (typeof item === "string") { texts.push(item); continue; }
    if (!item || typeof item !== "object" || Array.isArray(item)) continue;
    const content = item.content === undefined ? [item] : Array.isArray(item.content) ? item.content : [item.content];
    for (const part of content) {
      if (typeof part === "string") { texts.push(part); continue; }
      if (!part || typeof part !== "object") continue;
      if (["input_text", "text"].includes(part.type) && typeof part.text === "string") texts.push(part.text);
      if (["input_image", "image_url"].includes(part.type)) { let u = part.image_url; if (u && typeof u === "object") u = u.url; if (trimmed(u)) images.push(trimmed(u)); }
    }
  }
  return { texts, images };
}
export function run(ctx) {
  const req = ctx.body.value;
  const parsed = walk(req.input);
  const headers = {};
  for (const name of Object.keys(ctx.headers || {})) headers[name.toLowerCase()] = ctx.headers[name];
  const metadata = {};
  for (const [k, v] of Object.entries(req.metadata || {})) if (["string", "number", "boolean"].includes(typeof v)) metadata[k] = v;
  const size = /^(\d+)\s*[xX×*]\s*(\d+)$/.exec(trimmed(req.size));
  return {
    kind: "submit", model: trimmed(req.model), prompt: parsed.texts.join("\n").replace(/\s+/g, " ").trim(), imageCount: parsed.images.length, firstImagePrefix: (parsed.images[0] || "").slice(0, 22),
    headers, hasAuth: Object.prototype.hasOwnProperty.call(headers, "authorization"), metadata, size: size ? { w: Number(size[1]), h: Number(size[2]), pixels: Number(size[1]) * Number(size[2]) } : null,
    n: Number.isInteger(req.n) && req.n > 0 ? req.n : 1, seconds: Number(req.seconds || req.duration || 4), promptBytes: encodeURIComponent(parsed.texts.join("\n")).length,
    passthrough: Object.assign({}, req, { model: "rewritten", input: undefined }), keys: Object.keys(req).sort(), nullish: [req.missing ?? "dflt", req.metadata?.nested?.ignored, req.metadata?.trace ?? null],
    numbers: [req.n, req.metadata.priority, req.n * 1.5, String(req.n), req.n === 2, JSON.stringify(req.n), typeof req.n, req.metadata.priority / 2],
    echoed: { arr: [1, "two", null, true, { deep: [[]] }], empty: {}, emptyArr: [], zero: 0, negZero: -0, float: 1.25, big: 1e21, small: 1e-7, str: "日本語 😀 \u0000\t" },
  };
}
