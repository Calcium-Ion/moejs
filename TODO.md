# Not supported yet

moejs implements the parts of ECMAScript that real plugin code uses, and
grows from there. The features below are not implemented. Each one fails with
an error that names it, at compile time where the syntax allows, otherwise
when it is used; none of them runs silently with the wrong result.

## Language

- Import attributes (`with { type: "json" }`, and the options argument of
  `import(specifier, options)`) and JSON modules, `import defer`, and source
  phase imports (`import source`)

## Builtins

- Iterator helpers (`Iterator.from`, `Iterator.prototype.map` and friends)
  and the `getOrInsert` upsert methods
- `Intl`, and locale-tailored `localeCompare` and `toLocale*` methods
  (`localeCompare` uses the CLDR root collation order)
- `FinalizationRegistry`
- The legacy static accessors of `RegExp` (`RegExp.$1`–`$9`, `input`,
  `lastMatch`, `lastParen`, `leftContext`, `rightContext` and their `$_`,
  `$&`, `$+`, `` $` `` and `$'` aliases)
- Timers (`setTimeout` and friends)

## Known wrong results

These run, with a result that differs from the specification:

- Decimal text to number: a number with more than 800 significant digits
  before its point or exponent, or an exponent of 100000 or more offset by a
  long run of zeros, is misrounded by Go's `strconv`. An exact conversion is
  only needed for text longer than 800 characters, which both cases need.
  Sobek's result is the same.
- A compound assignment or update of a computed member (`o[k] += v`,
  `o[k]++`) converts the key with ToPropertyKey twice, once to read the
  property and once to write it, instead of once before the read: a key
  whose `toString` or `valueOf` has side effects runs them twice.
- In strict code, an assignment to an undeclared name whose right-hand side
  creates it (`x = (globalThis.x = 1)`) stores the value instead of
  throwing a ReferenceError: the name is resolved when the value is stored,
  not before the right-hand side runs.
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

## Host API

- `ParseJSON` straight from the bytes (it copies them into a string first),
  and an `AppendJSON` that writes UTF-8 into the destination without an
  intermediate string
- Source maps
- A per-realm heap or allocation budget
