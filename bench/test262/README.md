# test262

The tc39/test262 conformance suite against moejs. The checkout is pinned in
`REVISION` and lives outside the repository, in `~/.cache/moejs/test262` by
default.

## Commands

```sh
# Fetch (or move) the checkout to the pinned revision.
bench/test262/fetch.sh

# Run the suite and compare with baseline.txt (from the bench module).
cd bench
go test -run TestTest262 -timeout 30m ./test262/

# Rewrite baseline.txt and RESULTS.md after a change that moves the numbers.
go test -run TestTest262 -timeout 30m ./test262/ -update

# Run one directory and log every failure with its error.
go test -run TestTest262 -v ./test262/ -test262.prefix built-ins/JSON/ -test262.failures
```

`TestTest262` skips under `-short` and when there is no checkout, and fails
when the checkout is not at `REVISION`. It fails when a test in
`baseline.txt` stops passing, and logs how many pass that are not in the
baseline (the first 30). `-update` refuses to run with `-test262.prefix`,
because it rewrites the whole baseline.

Environment:

- `MOEJS_TEST262_DIR`: the checkout, instead of `~/.cache/moejs/test262`
  (fetch.sh honours it too).
- `MOEJS_TEST262_WORKERS`: the size of the worker pool, instead of
  min(GOMAXPROCS, 8).

## Rules

- Scope: `test/language`, `test/built-ins`, `test/annexB` and
  `test/harness`. `intl402` and `staging` do not run. RESULTS.md reports
  `language` + `built-ins` and `annexB` on separate lines.
- Every test runs in a fresh `SharedIntrinsics: false` realm whose Date time
  zone is UTC, with a 10 s interrupt, on a pool of workers. A Go panic is a failure that records the
  panic text and the panicking function (RESULTS.md lists them).
- Modes follow the flags. `onlyStrict` tests run once in strict mode, with
  `"use strict";` prepended; `noStrict` tests run once in sloppy mode, as
  they are; `raw` tests run once, verbatim and without any harness. Default
  tests run twice, strict and then sloppy. The strict run is named by the
  test's path (`language/x.js`) and the sloppy run by the path plus
  ` [sloppy]` (`language/x.js [sloppy]`), in `baseline.txt` and in failure
  logs alike. Every run counts as one test in every total, so a default file
  counts twice; a skipped default file is skipped twice, so totals do not
  move as features land.
- `module` tests run as modules. A module that imports or re-exports another
  is linked with it first (`engine.LinkModules`): a specifier names a file
  relative to the directory of the importing module, the test's own file is
  the test's module, and the other modules are compiled once per process.
- `async` tests also include `doneprintHandle.js`, whose `$DONE` prints a
  result line. The job queue drains before the script or module returns, so
  a test passes when it printed `Test262:AsyncTestComplete` and fails with
  the rest of a `Test262:AsyncTestFailure:` line, or when it printed neither.
- `features.go` lists the feature tags moejs implements. A test that needs any
  other feature (listed, or implied by an include through
  `harness/features.yml`) is skipped with the feature named. Intl, the
  `Atomics.wait` family (`CanBlockIsTrue`, `$262.agent`) and
  `FinalizationRegistry` are skipped as non-goals. Implementing a feature
  adds it to the table and regenerates the baseline.
- Negative tests: a `parse` or `resolution` error must be raised by the parser
  or the compiler, before anything runs, and its name must match. A module
  that does not parse or an import that does not resolve while the test is
  linked is a `resolution` error. A `runtime`
  error matches by the thrown value's `constructor.name`. A moejs "not
  supported yet" error is always a failure, even where an error is expected.
- `$262` has `createRealm`, `detachArrayBuffer`, `evalScript`, `global` and
  `gc`; there is no `agent`. `print` joins its arguments with spaces, strings
  as they are and anything else in its display form.
- The harness files a test includes (`assert.js`, `sta.js`, then `includes`)
  are compiled once per include list and mode: strict (with `"use strict";`
  prepended) for strict and module runs, sloppy for sloppy runs. The harness
  and the test are separate scripts run in one realm, so they share the
  global object and the global declarative environment: harness `var` and
  function declarations are global object properties, and its `let`, `const`
  and `class` declarations are global lexical bindings, as in a browser.
- `$262.evalScript` runs its source as a sloppy script unless it starts with
  a `"use strict"` directive.

## Files

- `REVISION`: the pinned test262 commit.
- `baseline.txt`: the sorted passing tests at that revision.
- `RESULTS.md`: pass, fail and skip counts per directory, skip reason and
  feature, the wall time and the top failure categories of the last `-update`.
