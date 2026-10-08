# Not supported yet

moejs implements the parts of ECMAScript that real plugin code uses, and
grows from there. The features below are not implemented. Each one fails with
an error that names it, at compile time where the syntax allows, otherwise
when it is used; none of them runs silently with the wrong result.

## Language

- Import attributes (`with { type: "json" }`, and the options argument of
  `import(specifier, options)`) and JSON modules, `import defer`, and source
  phase imports (`import source`)
- Explicit resource management: `using` and `await using` declarations,
  `DisposableStack`, `AsyncDisposableStack`, `SuppressedError` and
  `Symbol.dispose`
- Decorators

## TypeScript

`CompileTS` erases TypeScript's type syntax and runs what is left (see
docs/guide.md). It does not check types. TypeScript that has run-time
semantics fails with a `SyntaxError` that names it:

- `enum` and `const enum` declarations, outside `declare`
- Namespaces and modules with values (a namespace that holds only types is
  erased, as tsc erases it)
- Constructor parameter properties (`constructor(private x: T)`)
- `import x = require("m")`, `export import x = A.B`, and an alias
  `import x = A.B` used as a value
- `export =` and `export as namespace`
- `accessor` fields (part of the decorators proposal; decorators are listed
  under Language)
- TSX

## Builtins

- Iterator helpers (`Iterator.from`, `Iterator.prototype.map` and friends)
  and the `getOrInsert` upsert methods
- `RegExp.escape`, `Atomics.pause`, `ShadowRealm` and `Temporal`
- JSON source text access: `JSON.rawJSON`, `JSON.isRawJSON` and the third
  (`context`) argument of a `JSON.parse` reviver
- The legacy `caller` and `arguments` properties of sloppy functions:
  reading `f.caller` or `f.arguments` throws a TypeError, as it does for a
  strict function
- `Intl`, and locale-tailored `localeCompare` and `toLocale*` methods
  (`localeCompare` uses the CLDR root collation order)
- `FinalizationRegistry`
- Timers on a bare `Runtime`, which has none: `setTimeout` is a
  ReferenceError there. The `eventloop` package installs Node.js's
  `setTimeout`, `setInterval`, `setImmediate` and their clear functions,
  without `timers/promises`, the `AbortSignal` options, `util.promisify`
  of the timer functions, the `Symbol.dispose` method of a `Timeout`, and
  Node's warning for a delay above 2147483647 ms

## Known wrong results

These run, with a result that differs from the specification:

- Decimal text to number: a number with more than 800 significant digits
  before its point or exponent, or an exponent of 100000 or more offset by a
  long run of zeros, is misrounded by Go's `strconv`. An exact conversion is
  only needed for text longer than 800 characters, which both cases need.
  Sobek's result is the same.
- In strict code, an undeclared name assigned by destructuring
  (`[x] = iterable`, `({a: x} = o)`), or by a strict function inside the
  scope of a sloppy direct `eval` or of a `with` statement, is resolved
  when the value is stored, not when the target is evaluated: if the
  iterator, a getter or the right-hand side creates the global in between,
  the value is stored instead of throwing a ReferenceError. Elsewhere, a
  plain assignment (`x = (globalThis.x = 1)`) throws.
- `super[k] += v` and the other compound and logical assignments, and
  `super[k]++` and `++super[k]`, convert `k` to a property key before they
  read `super[k]`. In a method whose home object's prototype is null,
  `k`'s `toString` runs before the TypeError, as in V8; the specification
  throws first, when the read converts the null base to an object, as
  moejs does for a plain read or assignment.
- `import()` in the code of an indirect `eval` or of a `Function`
  constructor passes a nil `referrer` to the `Resolver` when the script or
  module calling it uses neither `import()`, `import.meta` nor a direct
  `eval` itself (the specification passes that script or module): only
  such code keeps a record of its script or module, so that other code
  pays nothing. With `engine` alone, a module run by `EvaluateModule`
  without a graph has no record either. The `referrer` is nil too, and the
  code's stack frames are named `<anonymous>`, when a job calls `eval` or a
  constructor directly, with no code of a script or module running
  (`Promise.resolve(s).then(eval)`): that is the specification's default
  host, while a browser passes the script or module that called `then`
  (HostMakeJobCallback).

- `Function.prototype.toString` of a function compiled by `CompileTS`
  returns its TypeScript source, types included. Node replaces the types
  with blanks before it runs the code, so its `toString` returns the text
  with blanks where the types were.
- `CompileTS` decides which imports are types without type information, as
  esbuild and tsc with `isolatedModules` do: `export { X }` of an imported
  `X` keeps the import even when the exporting module declares `X` as a type
  only (write `export { type X }`), and a name that a computed property key
  in a type reads (`{ [k.x]: T }`) does not keep its import, where tsc keeps
  it.

## Limits

- One realm per runtime: JavaScript cannot create another realm, so the
  specification's cross-realm behaviour (a constructor falling back to the
  realm of its `newTarget`, for one) never arises, and a function or
  generator runs only in the runtime that created it (the host API returns
  `ErrForeign` for one of another runtime).
- Listing the keys of a typed array of more than 2^24 elements
  (`Object.keys`, `for`-`in`, `Reflect.ownKeys`) throws a RangeError, where
  V8 lists them. Reading and writing its elements is not limited.

## Host API

- `ParseJSON` straight from the bytes (it copies them into a string first)
- Source maps
- The outcome of a module's top-level `await` that settles after `Load`
  returned `ErrModulePending`, as with the `eventloop` package: the
  module's exports show its progress, and a rejection reaches the host only
  through the rejection tracker
- A per-realm heap or allocation budget, which would also bound what a
  result costs the host: a value that repeats one object many times, whose
  text and `Unmarshal` copies are as large as if it did not, and the buffer
  `AppendJSON` reserves from its last output
