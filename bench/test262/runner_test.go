package test262

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// miniHarness is a stand-in for the harness files the runner always loads.
var miniHarness = map[string]string{
	"sta.js": `function Test262Error(message) { this.message = message || ""; }
var $DONOTEVALUATE = function () { throw "Test262: This statement should not be evaluated."; };`,
	"assert.js": `function assert(v, m) { if (v !== true) throw new Test262Error(m); }
assert.sameValue = function (a, b, m) {
  if (a !== b) throw new Test262Error(m + " Expected SameValue(«" + a + "», «" + b + "») to be true");
};`,
	"doneprintHandle.js": `function $DONE(error) {
  print(error ? "Test262:AsyncTestFailure:Test262Error: " + error.message : "Test262:AsyncTestComplete");
}`,
	"temporalHelper.js": `var usesTemporal = true;`,
	"features.yml":      "# include: [features]\ntemporalHelper.js: [Temporal]\n",
}

// TestFeaturesByPath checks the features a test's directory implies.
func TestFeaturesByPath(t *testing.T) {
	s := &Suite{}
	m := &Meta{Features: []string{"BigInt"}}
	for path, want := range map[string]string{
		"built-ins/TypedArrayConstructors/BigInt64Array/name.js": "BigInt,TypedArray",
		"built-ins/TypedArray/prototype/at/name.js":              "BigInt,TypedArray",
		"built-ins/BigInt/asIntN/name.js":                        "BigInt",
	} {
		if got := strings.Join(s.features(path, m), ","); got != want {
			t.Errorf("features(%s) = %s, want %s", path, got, want)
		}
	}
}

// outcome is the expected status and message (a prefix) of one run.
type outcome struct {
	status  Status
	message string
}

// once is the outcome of a test that runs in one mode.
func once(status Status, message string) []outcome { return []outcome{{status, message}} }

// both is the outcome of a default test in each of its modes, strict and
// sloppy.
func both(status Status, message string) []outcome {
	return []outcome{{status, message}, {status, message}}
}

// TestRunner runs the runner over a synthetic checkout: modes, skips,
// negative tests, $262 and the interrupt of every realm a test creates.
func TestRunner(t *testing.T) {
	root := t.TempDir()
	for name, src := range miniHarness {
		writeFile(t, filepath.Join(root, "harness", name), src)
	}
	tests := []struct {
		path, src string
		want      []outcome // strict, then sloppy for a default test
	}{
		{"pass.js", `assert.sameValue(1 + 1, 2);`, both(Pass, "")},
		{"fail.js", `assert.sameValue(1, 2, "one");`, both(Fail, "Test262Error: one Expected SameValue(«1», «2») to be true")},
		{"modes.js", `assert.sameValue((function () { return this; })(), undefined, "this");`,
			[]outcome{{Pass, ""}, {Fail, "Test262Error: this Expected SameValue(«[object Object]», «undefined») to be true"}}},
		{"strict.js", "/*---\nflags: [onlyStrict]\n---*/\nassert.sameValue((function () { return this; })(), undefined);", once(Pass, "")},
		{"sloppy.js", "/*---\nflags: [noStrict]\n---*/\nimplicit = 1;\nwith ({}) assert.sameValue(globalThis.implicit, 1);", once(Pass, "")},
		{"raw.js", "/*---\nflags: [raw]\n---*/\nif (typeof assert !== \"undefined\") throw new Error(\"the harness ran\");\nwith ({}) {}", once(Pass, "")},
		{"harness-global.js", `var declared = 1; assert.sameValue(globalThis.declared, 1); assert.sameValue(typeof globalThis.Test262Error, "function");`, both(Pass, "")},
		{"async.js", "/*---\nflags: [async]\n---*/\nPromise.resolve().then(() => $DONE());", both(Pass, "")},
		{"async-fail.js", "/*---\nflags: [async]\n---*/\nPromise.reject(new Test262Error(\"no\")).then(() => $DONE(), $DONE);", both(Fail, "Test262Error: no")},
		{"async-pending.js", "/*---\nflags: [async]\n---*/\nnew Promise(() => {}).then(() => $DONE());", both(Fail, "the async test did not call $DONE")},
		{"async-module.js", "/*---\nflags: [async, module]\n---*/\nqueueMicrotask(() => $DONE());", once(Pass, "")},
		{"async-loop.js", "/*---\nflags: [async, onlyStrict]\n---*/\n(function tick() { queueMicrotask(tick); })();", once(Fail, "timeout (")},
		{"async-throws.js", "/*---\nflags: [async]\n---*/\nqueueMicrotask(() => { throw new Test262Error(\"job\"); });\n$DONE();", both(Fail, "Test262Error: job")},
		{"feature.js", "/*---\nfeatures: [let, Temporal]\n---*/\n", both(Skip, "feature: Temporal")},
		{"implied.js", "/*---\nincludes: [temporalHelper.js]\n---*/\n", both(Skip, "feature: Temporal")},
		{"nongoal.js", "/*---\nfeatures: [Intl.Locale]\n---*/\n", both(Skip, "non-goal: Intl.Locale")},
		{"neg-runtime.js", "/*---\nnegative:\n  phase: runtime\n  type: TypeError\n---*/\nnull.x;", both(Pass, "")},
		{"neg-runtime-wrong.js", "/*---\nnegative:\n  phase: runtime\n  type: TypeError\n---*/\nundefinedName;", both(Fail, "expected a TypeError, got ReferenceError: ")},
		{"neg-thrown.js", "/*---\nnegative:\n  phase: runtime\n  type: Test262Error\n---*/\nthrow new Test262Error('x');", both(Pass, "")},
		{"neg-parse.js", "/*---\nnegative:\n  phase: parse\n  type: SyntaxError\n---*/\n$DONOTEVALUATE();\nvar var;", both(Pass, "")},
		{"neg-parse-strict.js", "/*---\nnegative:\n  phase: parse\n  type: SyntaxError\n---*/\n$DONOTEVALUATE();\nwith ({}) {}",
			[]outcome{{Pass, ""}, {Fail, "expected a parse-phase SyntaxError, but the test compiled"}}},
		{"neg-parse-valid.js", "/*---\nnegative:\n  phase: parse\n  type: SyntaxError\n---*/\n$DONOTEVALUATE();", both(Fail, "expected a parse-phase SyntaxError, but the test compiled")},
		{"neg-completes.js", "/*---\nnegative:\n  phase: runtime\n  type: TypeError\n---*/\n", both(Fail, "expected a runtime-phase TypeError, but the test completed")},
		{"module.js", "/*---\nflags: [module]\n---*/\nexport var x = 1;\nassert.sameValue(x, 1);", once(Pass, "")},
		{"module-import.js", "/*---\nflags: [module]\n---*/\nimport { x } from './x_FIXTURE.js';\nassert.sameValue(x, 1);", once(Pass, "")},
		{"module-self.js", "/*---\nflags: [module]\n---*/\nimport * as self from './module-self.js';\nimport { x } from './dir/y_FIXTURE.js';\nexport const z = x;\nassert.sameValue(self.z, 1);", once(Pass, "")},
		{"module-throws.js", "/*---\nflags: [module]\nnegative:\n  phase: runtime\n  type: TypeError\n---*/\nimport './throws_FIXTURE.js';", once(Pass, "")},
		{"module-tla.js", "/*---\nflags: [async, module]\n---*/\nimport { later } from './tla_FIXTURE.js';\nassert.sameValue(later, 2);\n$DONE();", once(Pass, "")},
		{"module-missing.js", "/*---\nflags: [module]\nnegative:\n  phase: resolution\n  type: SyntaxError\n---*/\nimport { nope } from './x_FIXTURE.js';", once(Pass, "")},
		{"module-bad-fixture.js", "/*---\nflags: [module]\nnegative:\n  phase: resolution\n  type: SyntaxError\n---*/\nimport './bad_FIXTURE.js';", once(Pass, "")},
		{"module-linked.js", "/*---\nflags: [module]\nnegative:\n  phase: resolution\n  type: SyntaxError\n---*/\nimport './x_FIXTURE.js';", once(Fail, "expected a resolution-phase SyntaxError, but the test linked")},
		{"module-no-file.js", "/*---\nflags: [module]\n---*/\nimport './none_FIXTURE.js';", once(Fail, "language/module-no-file.js:4:8: cannot resolve module \"./none_FIXTURE.js\": open ")},
		{"realm.js", `var other = $262.createRealm();
assert.sameValue(other.evalScript("var y = 1; y + 1"), 2);
assert.sameValue(other.global === $262.global, false);
assert.sameValue($262.global, globalThis);
assert.sameValue($262.evalScript("(function () { return this; })()"), globalThis, "evalScript is sloppy");
var e;
try { $262.evalScript("var var;"); } catch (err) { e = err; }
assert.sameValue(e.constructor, SyntaxError);
$262.gc();`, both(Pass, "")},
		{"loop.js", `for (;;) {}`, both(Fail, "timeout (")},
		{"loop-realm.js", "/*---\nflags: [noStrict]\n---*/\n$262.createRealm().evalScript(\"for (;;) {}\");", once(Fail, "timeout (")},
		{"bad-frontmatter.js", "/*---\nflags: [a\n---*/\n", once(Fail, "frontmatter: ")},
	}
	var paths []string
	for _, tc := range tests {
		src := tc.src
		if !strings.HasPrefix(src, "/*---") {
			src = "/*---\n---*/\n" + src
		}
		writeFile(t, filepath.Join(root, "test", "language", tc.path), src)
		paths = append(paths, "language/"+tc.path)
	}
	for name, src := range map[string]string{
		"x_FIXTURE.js":      "export var x = 1;",
		"dir/y_FIXTURE.js":  "export { x } from '../x_FIXTURE.js';",
		"throws_FIXTURE.js": "null.x;",
		"tla_FIXTURE.js":    "export let later = 1; await 0; later = 2;",
		"bad_FIXTURE.js":    "export var var;",
	} {
		writeFile(t, filepath.Join(root, "test", "language", filepath.FromSlash(name)), src)
	}
	for _, dir := range Dirs {
		if err := os.MkdirAll(filepath.Join(root, "test", dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	s, err := NewSuite(root)
	if err != nil {
		t.Fatal(err)
	}
	found, err := s.Discover("language/")
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != len(tests) {
		t.Errorf("Discover found %d tests (%v), want %d without the fixtures", len(found), found, len(tests))
	}

	defer func(d time.Duration) { Timeout = d }(Timeout)
	Timeout = 100 * time.Millisecond
	results := s.Run(paths, 4)
	for i, tc := range tests {
		// A default test's second result is its sloppy run.
		names := []string{paths[i], paths[i] + SloppySuffix}[:len(tc.want)]
		for j, want := range tc.want {
			if len(results) == 0 {
				t.Fatalf("%s: missing result %d", paths[i], j)
			}
			res := results[0]
			results = results[1:]
			if res.Path != names[j] || res.Status != want.status || !strings.HasPrefix(res.Message, want.message) || (want.message == "" && res.Message != "") {
				t.Errorf("%s: status %d %q, want %s %d %q", res.Path, res.Status, res.Message, names[j], want.status, want.message)
			}
		}
	}
	if len(results) > 0 {
		t.Errorf("%d unexpected results, the first %s", len(results), results[0].Path)
	}
}

// TestModes checks the modes a test's flags select.
func TestModes(t *testing.T) {
	for _, tc := range []struct {
		flags []string
		want  []mode
	}{
		{nil, []mode{strictMode, sloppyMode}},
		{[]string{"async"}, []mode{strictMode, sloppyMode}},
		{[]string{"onlyStrict"}, []mode{strictMode}},
		{[]string{"noStrict"}, []mode{sloppyMode}},
		{[]string{"raw"}, []mode{rawMode}},
		{[]string{"module"}, []mode{moduleMode}},
		{[]string{"module", "async"}, []mode{moduleMode}},
	} {
		if got := modes(&Meta{Flags: tc.flags}); !slices.Equal(got, tc.want) {
			t.Errorf("modes(%v) = %v, want %v", tc.flags, got, tc.want)
		}
	}
}

func writeFile(t *testing.T, path, src string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}
