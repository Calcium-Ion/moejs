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

- Scope: `test/language`, `test/built-ins` and `test/harness`. `intl402`,
  `staging` and `annexB` do not run.
- Every test runs in a fresh `SharedIntrinsics: false` realm whose Date time
  zone is UTC, with a 10 s interrupt, on a pool of workers. A Go panic is a failure that records the
  panic text and the panicking function (RESULTS.md lists them).
- `onlyStrict` and default tests run once, with `"use strict";` prepended.
  `noStrict` and `raw` tests are skipped as "sloppy mode".
- `module` tests run as modules; those that import or re-export another
  module are skipped as "modules".
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
  or the compiler, before anything runs, and its name must match. A `runtime`
  error matches by the thrown value's `constructor.name`. A moejs "not
  supported yet" error is always a failure, even where an error is expected.
- `$262` has `createRealm`, `detachArrayBuffer`, `evalScript`, `global` and
  `gc`; there is no `agent`. `print` joins its arguments with spaces, strings
  as they are and anything else in its display form.
- The harness files a test includes (`assert.js`, `sta.js`, then `includes`)
  are compiled once per include list. Each moejs script has its own top-level
  scope, so the harness script ends by publishing its top-level bindings on
  the global object, and the test runs as a second script in the same realm.
  One consequence: a test's own top-level declarations do not become global
  object properties, so the few tests that rely on that (for example
  `harness/asyncHelpers-asyncTest-*`, which declare `$DONE`) fail.

## Files

- `REVISION`: the pinned test262 commit.
- `baseline.txt`: the sorted passing tests at that revision.
- `RESULTS.md`: pass, fail and skip counts per directory, skip reason and
  feature, the wall time and the top failure categories of the last `-update`.
