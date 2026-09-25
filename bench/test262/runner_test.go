package test262

import (
	"os"
	"path/filepath"
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

// TestRunner runs the runner over a synthetic checkout: modes, skips,
// negative tests, $262 and the interrupt of every realm a test creates.
func TestRunner(t *testing.T) {
	root := t.TempDir()
	for name, src := range miniHarness {
		writeFile(t, filepath.Join(root, "harness", name), src)
	}
	tests := []struct {
		path, src string
		status    Status
		message   string // prefix
	}{
		{"pass.js", `assert.sameValue(1 + 1, 2);`, Pass, ""},
		{"fail.js", `assert.sameValue(1, 2, "one");`, Fail, "Test262Error: one Expected SameValue(«1», «2») to be true"},
		{"strict.js", "/*---\nflags: [onlyStrict]\n---*/\nassert.sameValue((function () { return this; })(), undefined);", Pass, ""},
		{"sloppy.js", "/*---\nflags: [noStrict]\n---*/\n", Skip, SkipSloppy},
		{"raw.js", "/*---\nflags: [raw]\n---*/\n", Skip, SkipSloppy},
		{"async.js", "/*---\nflags: [async]\n---*/\nPromise.resolve().then(() => $DONE());", Pass, ""},
		{"async-fail.js", "/*---\nflags: [async]\n---*/\nPromise.reject(new Test262Error(\"no\")).then(() => $DONE(), $DONE);", Fail, "Test262Error: no"},
		{"async-pending.js", "/*---\nflags: [async]\n---*/\nnew Promise(() => {}).then(() => $DONE());", Fail, "the async test did not call $DONE"},
		{"async-module.js", "/*---\nflags: [async, module]\n---*/\nqueueMicrotask(() => $DONE());", Pass, ""},
		{"async-loop.js", "/*---\nflags: [async]\n---*/\n(function tick() { queueMicrotask(tick); })();", Fail, "timeout ("},
		{"async-throws.js", "/*---\nflags: [async]\n---*/\nqueueMicrotask(() => { throw new Test262Error(\"job\"); });\n$DONE();", Fail, "Test262Error: job"},
		{"feature.js", "/*---\nfeatures: [let, Temporal]\n---*/\n", Skip, "feature: Temporal"},
		{"implied.js", "/*---\nincludes: [temporalHelper.js]\n---*/\n", Skip, "feature: Temporal"},
		{"nongoal.js", "/*---\nfeatures: [Intl.Locale]\n---*/\n", Skip, "non-goal: Intl.Locale"},
		{"neg-runtime.js", "/*---\nnegative:\n  phase: runtime\n  type: TypeError\n---*/\nnull.x;", Pass, ""},
		{"neg-runtime-wrong.js", "/*---\nnegative:\n  phase: runtime\n  type: TypeError\n---*/\nundefinedName;", Fail, "expected a TypeError, got ReferenceError: "},
		{"neg-thrown.js", "/*---\nnegative:\n  phase: runtime\n  type: Test262Error\n---*/\nthrow new Test262Error('x');", Pass, ""},
		{"neg-parse.js", "/*---\nnegative:\n  phase: parse\n  type: SyntaxError\n---*/\n$DONOTEVALUATE();\nvar var;", Pass, ""},
		{"neg-parse-valid.js", "/*---\nnegative:\n  phase: parse\n  type: SyntaxError\n---*/\n$DONOTEVALUATE();", Fail, "expected a parse-phase SyntaxError, but the test compiled"},
		{"neg-completes.js", "/*---\nnegative:\n  phase: runtime\n  type: TypeError\n---*/\n", Fail, "expected a runtime-phase TypeError, but the test completed"},
		{"module.js", "/*---\nflags: [module]\n---*/\nexport var x = 1;\nassert.sameValue(x, 1);", Pass, ""},
		{"module-import.js", "/*---\nflags: [module]\n---*/\nimport { x } from './x_FIXTURE.js';", Skip, SkipModules},
		{"realm.js", `var other = $262.createRealm();
assert.sameValue(other.evalScript("var y = 1; y + 1"), 2);
assert.sameValue(other.global === $262.global, false);
assert.sameValue($262.global, globalThis);
var e;
try { $262.evalScript("var var;"); } catch (err) { e = err; }
assert.sameValue(e.constructor, SyntaxError);
$262.gc();`, Pass, ""},
		{"loop.js", `for (;;) {}`, Fail, "timeout ("},
		{"loop-realm.js", `$262.createRealm().evalScript("for (;;) {}");`, Fail, "timeout ("},
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
	writeFile(t, filepath.Join(root, "test", "language", "x_FIXTURE.js"), "export var x = 1;")
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
		t.Errorf("Discover found %d tests (%v), want %d without the fixture", len(found), found, len(tests))
	}

	defer func(d time.Duration) { Timeout = d }(Timeout)
	Timeout = 100 * time.Millisecond
	for i, res := range s.Run(paths, 4) {
		tc := tests[i]
		if res.Path != paths[i] || res.Status != tc.status || !strings.HasPrefix(res.Message, tc.message) || (tc.message == "" && res.Message != "") {
			t.Errorf("%s: status %d %q, want %d %q", paths[i], res.Status, res.Message, tc.status, tc.message)
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
