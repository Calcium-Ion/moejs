package syntax

// Audit tests for parser robustness against adversarial input sizes.
// new-api compiles untrusted plugin source at
// upload time, so a Go stack overflow (`fatal error: stack overflow`, which
// kills the whole process and cannot be recovered) reachable from a parser
// call is a critical finding. Because such a crash kills the test binary,
// the depths are probed in a subprocess: the parent re-executes the test
// binary with MOEJS_AUDIT_PARSE_PROBES set and classifies the child's
// outcome for each probe.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// auditNestedSource builds a module of the requested nesting kind and depth.
func auditNestedSource(kind string, n int) string {
	rep := strings.Repeat
	switch kind {
	case "paren":
		return "const x = " + rep("(", n) + "1" + rep(")", n) + ";"
	case "array":
		return "const x = " + rep("[", n) + rep("]", n) + ";"
	case "object":
		return "const x = " + rep("{a:", n) + "1" + rep("}", n) + ";"
	case "block":
		return rep("{", n) + rep("}", n)
	case "if":
		return rep("if(1)", n) + ";"
	case "neg":
		// Spaces keep the lexer from reading `--` as the decrement operator.
		return "const x = " + rep("- ", n) + "1;"
	case "not":
		return "const x = " + rep("!", n) + "1;"
	case "member":
		return "const a = {}; const x = a" + rep(".b", n) + ";"
	case "plus":
		return "const a = 1; const x = " + strings.TrimSuffix(rep("a+", n), "+") + ";"
	case "func":
		return rep("function f(){", n) + rep("}", n)
	case "arrow":
		return "const x = " + rep("()=>", n) + "1;"
	case "call":
		return "const f = x => x; const x = " + rep("f(", n) + "1" + rep(")", n) + ";"
	case "ternary":
		return "const c = 1; const x = " + rep("c?", n) + "1" + rep(":0", n) + ";"
	case "template":
		return "const x = " + rep("`${", n) + "1" + rep("}`", n) + ";"
	case "semicolons":
		return rep(";", n)
	case "new":
		return "const f = x => x; const x = " + rep("new ", n) + "f;"
	case "pattern":
		return "const " + rep("[", n) + "a" + rep("]", n) + " = [];"
	case "class":
		return "const x = " + rep("class extends (", n) + "Object" + rep("){}", n) + ";"
	}
	panic("unknown kind " + kind)
}

// auditProbe is one nesting kind at one depth.
type auditProbe struct {
	kind string
	n    int
}

// TestAuditParserDepth is the subprocess child: parse every probe listed in
// MOEJS_AUDIT_PARSE_PROBES ("kind:n,kind:n,...") and print a PROBE line
// before and a RESULT line after each, so the parent can tell which probe a
// crash interrupted. It is a no-op when the env var is unset.
func TestAuditParserDepth(t *testing.T) {
	list := os.Getenv("MOEJS_AUDIT_PARSE_PROBES")
	if list == "" {
		t.Skip("subprocess child only (driven by TestAuditParserDepthMatrix)")
	}
	for item := range strings.SplitSeq(list, ",") {
		kind, n, _ := strings.Cut(item, ":")
		depth, _ := strconv.Atoi(n)
		fmt.Printf("PROBE %s %d\n", kind, depth)
		fmt.Printf("RESULT %s %d %s\n", kind, depth, auditParseOne(kind, depth))
	}
}

// auditParseOne parses one probe and describes the outcome; a recoverable
// panic is reported rather than killing the child.
func auditParseOne(kind string, n int) (res string) {
	src := auditNestedSource(kind, n)
	defer func() {
		if p := recover(); p != nil {
			res = fmt.Sprintf("panic: %v", p)
		}
	}()
	start := time.Now()
	_, err := ParseModule("deep.js", src, Options{})
	el := time.Since(start).Round(time.Millisecond)
	if err != nil {
		msg := err.Error()
		if i := strings.IndexByte(msg, '\n'); i >= 0 {
			msg = msg[:i]
		}
		return fmt.Sprintf("error %s (%s)", msg, el)
	}
	return fmt.Sprintf("ok (%s)", el)
}

type auditVerdict struct{ verdict, detail string }

// auditParseVerdicts runs the probes in one child and classifies each
// outcome. A probe that kills the child (a Go stack overflow is fatal) is
// classified from the crash output, and a new child resumes after it.
func auditParseVerdicts(t *testing.T, probes []auditProbe) []auditVerdict {
	t.Helper()
	verdicts := make([]auditVerdict, 0, len(probes))
	for len(verdicts) < len(probes) {
		items := make([]string, 0, len(probes))
		for _, p := range probes[len(verdicts):] {
			items = append(items, p.kind+":"+strconv.Itoa(p.n))
		}
		cmd := exec.Command(os.Args[0], "-test.run", "^TestAuditParserDepth$", "-test.count=1", "-test.timeout", "300s")
		cmd.Env = append(os.Environ(), "MOEJS_AUDIT_PARSE_PROBES="+strings.Join(items, ","))
		out, err := cmd.CombinedOutput()
		s := string(out)
		for line := range strings.SplitSeq(s, "\n") {
			res, ok := strings.CutPrefix(line, "RESULT ")
			if !ok || len(verdicts) == len(probes) {
				continue
			}
			p := probes[len(verdicts)]
			res, ok = strings.CutPrefix(res, p.kind+" "+strconv.Itoa(p.n)+" ")
			switch {
			case !ok:
				verdicts = append(verdicts, auditVerdict{"UNKNOWN", "out of order: " + line})
			case strings.HasPrefix(res, "panic:"):
				verdicts = append(verdicts, auditVerdict{"PANIC", res})
			case strings.HasPrefix(res, "ok"):
				verdicts = append(verdicts, auditVerdict{"ok", strings.TrimSpace(res[len("ok"):])})
			case strings.HasPrefix(res, "error"):
				verdicts = append(verdicts, auditVerdict{"SyntaxError", strings.TrimSpace(res[len("error"):])})
			default:
				verdicts = append(verdicts, auditVerdict{"UNKNOWN", line})
			}
		}
		if len(verdicts) == len(probes) {
			break
		}
		// The child died inside the next probe.
		switch {
		case strings.Contains(s, "fatal error: stack overflow"), strings.Contains(s, "goroutine stack exceeds"):
			verdicts = append(verdicts, auditVerdict{"STACK-OVERFLOW (process crash)", ""})
		case strings.Contains(s, "panic:"):
			verdicts = append(verdicts, auditVerdict{"PANIC", afterPrefix(s, "panic:")})
		case strings.Contains(s, "test timed out"):
			verdicts = append(verdicts, auditVerdict{"TIMEOUT", ""})
		default:
			verdicts = append(verdicts, auditVerdict{"UNKNOWN", fmt.Sprintf("exit=%v out=%.300s", err, s)})
		}
	}
	return verdicts
}

func afterPrefix(s, p string) string {
	i := strings.Index(s, p)
	rest := s[i+len(p):]
	if j := strings.IndexByte(rest, '\n'); j >= 0 {
		rest = rest[:j]
	}
	return strings.TrimSpace(rest)
}

// TestAuditParserDepthMatrix probes every nesting kind at increasing depths
// in a subprocess and logs the verdict. It fails only on PANIC or UNKNOWN;
// stack overflows are reported by TestAuditParserStackOverflow so that the
// crash finding is isolated behind a single Skip.
func TestAuditParserDepthMatrix(t *testing.T) {
	t.Parallel()
	var probes []auditProbe
	for _, p := range []struct {
		kind   string
		depths []int
	}{
		{"paren", []int{5000, 50000, 500000}},
		{"array", []int{5000, 50000, 500000}},
		{"object", []int{5000, 50000, 500000}},
		{"block", []int{5000, 50000, 500000}},
		{"if", []int{5000, 50000, 500000}},
		{"neg", []int{5000, 50000, 500000}},
		{"not", []int{5000, 50000, 500000}},
		{"member", []int{100000}},
		{"plus", []int{100000}},
		{"func", []int{5000, 50000, 100000}},
		{"arrow", []int{5000, 50000, 100000}},
		{"call", []int{5000, 50000, 500000}},
		{"ternary", []int{5000, 50000, 500000}},
		{"template", []int{5000, 50000}},
		{"semicolons", []int{20_000_000}},
	} {
		for _, n := range p.depths {
			probes = append(probes, auditProbe{p.kind, n})
		}
	}
	for i, v := range auditParseVerdicts(t, probes) {
		p := probes[i]
		t.Logf("%-10s depth=%-8d -> %s %s", p.kind, p.n, v.verdict, v.detail)
		if v.verdict == "PANIC" || v.verdict == "UNKNOWN" {
			t.Errorf("%s depth=%d: %s %s", p.kind, p.n, v.verdict, v.detail)
		}
	}
}

// TestAuditParserStackOverflow: before the MaxNestingDepth guard the
// recursive-descent parser had no depth limit: 1,000,000 nested parentheses
// (a 1 MB source), `[`, `{a:` or `f(`; 1,000,000 nested `function f(){` or
// `()=>`; 2,000,000 nested blocks or `if(1)`; 3,000,000 `!` all overflowed
// the Go stack (1 GB default), a fatal, unrecoverable process crash —
// measured 2026-09-23: 500,000 deep parsed in ~250 ms, 1,000,000 deep `(`
// crashed. new-api compiles untrusted plugin source at upload, so this
// killed the host process. Every probe must now be a clean SyntaxError
// (fail loudly, never crash).
func TestAuditParserStackOverflow(t *testing.T) {
	t.Parallel()
	probes := []auditProbe{{"paren", 1_000_000}, {"array", 1_000_000}, {"object", 1_000_000}, {"call", 1_000_000}, {"func", 1_000_000}, {"arrow", 1_000_000}, {"block", 2_000_000}, {"if", 2_000_000}, {"not", 3_000_000}, {"neg", 3_000_000}, {"ternary", 1_000_000}, {"template", 1_000_000}}
	for i, v := range auditParseVerdicts(t, probes) {
		p := probes[i]
		if v.verdict != "SyntaxError" || !strings.Contains(v.detail, "Nesting too deep") {
			t.Errorf("%s depth=%d: %s %s (want SyntaxError: Nesting too deep)", p.kind, p.n, v.verdict, v.detail)
		}
	}
}

// auditNestedOpts keeps unsupported syntax (classes) in the tree so the
// depth probes exercise the parser and the scope pass, not the feature gate.
var auditNestedOpts = Options{AllowUnsupported: true}

// auditNestedMax finds the deepest n of a nesting kind that still parses;
// the units of MaxNestingDepth per source level differ by kind.
func auditNestedMax(t *testing.T, kind string) int {
	t.Helper()
	lo, hi := 1, MaxNestingDepth+1 // lo parses, hi does not
	if _, err := ParseModule("deep.js", auditNestedSource(kind, hi), auditNestedOpts); err == nil {
		t.Fatalf("%s: depth %d parsed; the guard is not on this path", kind, hi)
	}
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if _, err := ParseModule("deep.js", auditNestedSource(kind, mid), auditNestedOpts); err == nil {
			lo = mid
		} else {
			hi = mid
		}
	}
	return lo
}

// auditNestingKinds are the nesting shapes whose recursion the guard bounds
// (member and plus chains are loops and semicolons are a list).
var auditNestingKinds = []string{"paren", "array", "object", "block", "if", "neg", "not", "func", "arrow", "call", "ternary", "template", "new", "pattern", "class"}

// TestParserNestingLimit pins the guard in-process: every nesting kind is
// accepted to a depth that real code never reaches and rejected one level
// past it with the nesting error, and a source at the limit parses in
// bounded time.
func TestParserNestingLimit(t *testing.T) {
	for _, kind := range auditNestingKinds {
		t.Run(kind, func(t *testing.T) {
			deepest := auditNestedMax(t, kind)
			if deepest < MaxNestingDepth/4 {
				t.Errorf("deepest accepted %s nesting is %d, want at least %d", kind, deepest, MaxNestingDepth/4)
			}
			_, err := ParseModule("deep.js", auditNestedSource(kind, deepest+1), auditNestedOpts)
			if err == nil || !strings.Contains(err.Error(), "SyntaxError: Nesting too deep") {
				t.Errorf("%s depth=%d: got %v, want SyntaxError: Nesting too deep", kind, deepest+1, err)
			}
			var se *Error
			if !errors.As(err, &se) {
				t.Errorf("%s: error is %T, want *Error", kind, err)
			}
			t.Logf("%s: deepest accepted %d", kind, deepest)
		})
	}
}
