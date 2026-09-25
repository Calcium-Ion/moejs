# Not supported yet

moejs implements the parts of ECMAScript that real plugin code uses, and
grows from there. The features below are not implemented. Each one fails with
an error that names it, at compile time where the syntax allows, otherwise
when it is used; none of them runs silently with the wrong result.

## Language

- `import` (static and dynamic), `export ... from`, `import.meta`, and
  programs of more than one module
- Sloppy mode: modules always run in strict mode, so there is no `with`, no
  mapped `arguments` object and no sloppy-only syntax
- Direct and indirect `eval`, `new Function`
- Annex B syntax: labelled function declarations, HTML-like comments, legacy
  octal escapes

## Builtins

- Iterator helpers (`Iterator.from`, `Iterator.prototype.map` and friends)
  and the `getOrInsert` upsert methods
- `Intl`, and locale-tailored `localeCompare` and `toLocale*` methods
  (`localeCompare` uses the CLDR root collation order)
- `FinalizationRegistry`
- `RegExp.prototype.compile` and the Annex B regular expression grammar
- Timers (`setTimeout` and friends)

## Known wrong results

These run, with a result that differs from the specification (and agrees
with Sobek's):

- Decimal text to number: a number with more than 800 significant digits
  before its point or exponent, or an exponent of 100000 or more offset by a
  long run of zeros, is misrounded by Go's `strconv`. An exact conversion is
  only needed for text longer than 800 characters, which both cases need.

## Host API

- `ParseJSON` straight from the bytes (it copies them into a string first),
  and an `AppendJSON` that writes UTF-8 into the destination without an
  intermediate string
- Scripts (the API compiles and loads modules only), source maps
- A per-realm heap or allocation budget
