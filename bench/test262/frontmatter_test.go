package test262

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseMeta(t *testing.T) {
	src := `// Copyright
/*---
esid: sec-foo
description: >
  includes: [not, a, key]
  flags: [indented]
info: |
  negative:
    phase: parse
includes: [compareArray.js,
  propertyHelper.js]
flags:
  - onlyStrict
  - 'module'
features: ["let", Symbol.iterator]
negative:
  phase: runtime
  type: TypeError
# flags: [comment]
---*/
throw 1;
`
	m, err := ParseMeta(src)
	if err != nil {
		t.Fatal(err)
	}
	want := &Meta{
		Includes: []string{"compareArray.js", "propertyHelper.js"},
		Flags:    []string{"onlyStrict", "module"},
		Features: []string{"let", "Symbol.iterator"},
		Negative: &Negative{Phase: "runtime", Type: "TypeError"},
	}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("ParseMeta = %+v (negative %+v), want %+v", m, m.Negative, want)
	}
	if !m.HasFlag("module") || m.HasFlag("raw") {
		t.Errorf("HasFlag: module %v, raw %v", m.HasFlag("module"), m.HasFlag("raw"))
	}

	for _, eol := range []string{"\r\n", "\r"} {
		src := strings.Join([]string{"/*---", "info: |", "  x", "includes: [a.js,", "  b.js]", "flags: [raw]", "---*/"}, eol)
		m, err := ParseMeta(src)
		want := &Meta{Includes: []string{"a.js", "b.js"}, Flags: []string{"raw"}}
		if err != nil || !reflect.DeepEqual(m, want) {
			t.Errorf("ParseMeta with %q line ends = %+v, %v, want %+v", eol, m, err, want)
		}
	}

	for _, bad := range []string{
		"no frontmatter",
		"/*---\nflags: [a\n",
		"/*---\nflags: [a\n---*/",
		"/*---\nflags: onlyStrict\n---*/",
		"/*---\nnegative:\n  phase: parse\n---*/",
	} {
		if m, err := ParseMeta(bad); err == nil {
			t.Errorf("ParseMeta(%q) = %+v, want an error", bad, m)
		}
	}
}

func TestSkip(t *testing.T) {
	s := &Suite{}
	for _, tc := range []struct {
		meta     Meta
		src      string
		features []string
		want     string
		blockers []string
	}{
		{Meta{}, "", nil, "", nil},
		{Meta{Flags: []string{"onlyStrict"}}, "", []string{"let"}, "", nil},
		{Meta{Flags: []string{"noStrict"}}, "", nil, "", nil},
		{Meta{Flags: []string{"raw"}}, "", nil, "", nil},
		{Meta{Flags: []string{"async"}}, "", nil, "", nil},
		{Meta{Flags: []string{"async"}}, "", []string{"Temporal", "ShadowRealm", "let"}, "feature: Temporal", []string{"Temporal", "ShadowRealm"}},
		{Meta{}, "", []string{"Temporal", "intl-normative-optional"}, "non-goal: intl-normative-optional", nil},
		{Meta{Flags: []string{"CanBlockIsTrue"}}, "", nil, "non-goal: Atomics.wait (CanBlockIsTrue)", nil},
		{Meta{}, "$262.agent.start(src);", nil, "non-goal: multi-agent SharedArrayBuffer ($262.agent)", nil},
	} {
		got, blockers := s.skip(&tc.meta, tc.src, tc.features)
		if got != tc.want || !reflect.DeepEqual(blockers, tc.blockers) {
			t.Errorf("skip(%+v, %q, %v) = %q %v, want %q %v", tc.meta, tc.src, tc.features, got, blockers, tc.want, tc.blockers)
		}
	}
}

func TestReportHelpers(t *testing.T) {
	for path, want := range map[string]string{
		"harness/assert-true.js":                   "harness",
		"language/expressions/addition/S11.6.1.js": "language/expressions",
		"built-ins/Array/prototype/map/length.js":  "built-ins/Array",
		"built-ins/parseInt/S15.1.2.2_A1_T1.js":    "built-ins/parseInt",
		"language/top-level.js":                    "language",
	} {
		if got := Group(path); got != want {
			t.Errorf("Group(%q) = %q, want %q", path, got, want)
		}
	}
	for msg, want := range map[string]string{
		"Test262Error: length is 3 Expected SameValue(«2», «3») to be true": "Test262Error: Expected SameValue(«…», «…») to be true",
		"TypeError: cannot read property 12 of undefined":                   "TypeError: cannot read property N of undefined",
		"Test262Error: #1.2: Expected a Test262Error but got a RangeError":  "Test262Error: Expected a Test262Error but got a RangeError",
		"ReferenceError: Int16Array is not defined":                         "ReferenceError: Int16Array is not defined",
	} {
		if got := Category(msg); got != want {
			t.Errorf("Category(%q) = %q, want %q", msg, got, want)
		}
	}
	for msg, want := range map[string]string{
		"SyntaxError: unexpected token":     "SyntaxError",
		"ReferenceError: x is not defined":  "ReferenceError",
		"Error: plain":                      "Error",
		"invalid assignment target":         "",
		"SyntaxError without a colon-space": "",
	} {
		if got := errorName(msg); got != want {
			t.Errorf("errorName(%q) = %q, want %q", msg, got, want)
		}
	}
}
