package test262

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

var (
	update   = flag.Bool("update", false, "rewrite baseline.txt and RESULTS.md from this run")
	prefix   = flag.String("test262.prefix", "", "run only the tests whose path under test/ starts with this prefix")
	failures = flag.Bool("test262.failures", false, "log every failing test with its error")
)

// newlyPassingShown bounds the list of newly passing tests in the log.
const newlyPassingShown = 30

// checkoutDir is where fetch.sh puts the checkout.
func checkoutDir() string {
	if d := os.Getenv("MOEJS_TEST262_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "moejs", "test262")
}

// TestTest262 runs the suite and compares the passing tests with
// baseline.txt: it fails when a baseline test stops passing and logs the
// newly passing ones. -update rewrites baseline.txt and RESULTS.md.
func TestTest262(t *testing.T) {
	if testing.Short() {
		t.Skip("test262 does not run under -short")
	}
	root := checkoutDir()
	if _, err := os.Stat(filepath.Join(root, "test")); err != nil {
		t.Skipf("no test262 checkout at %s; run bench/test262/fetch.sh", root)
	}
	revision, err := os.ReadFile("REVISION")
	if err != nil {
		t.Fatal(err)
	}
	want, _, _ := strings.Cut(string(revision), "\n")
	head, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("git rev-parse in %s: %v", root, err)
	}
	if got := strings.TrimSpace(string(head)); got != want {
		t.Fatalf("the checkout at %s is at %s, REVISION pins %s; run bench/test262/fetch.sh", root, got, want)
	}
	if *update && *prefix != "" {
		t.Fatal("-update rewrites the whole baseline; run it without -test262.prefix")
	}

	suite, err := NewSuite(root)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := suite.Discover(*prefix)
	if err != nil {
		t.Fatal(err)
	}
	workers := Workers()
	start := time.Now()
	results := suite.Run(paths, workers)
	wall := time.Since(start)

	var passing []string
	var c counts
	for _, res := range results {
		c.add(res.Status)
		switch {
		case res.Status == Pass:
			passing = append(passing, res.Path)
		case res.Status == Fail && *failures:
			t.Logf("FAIL %s: %s", res.Path, res.Message)
		}
	}
	t.Logf("%d tests in %v on %d workers: pass %d, fail %d, skip %d", len(results), wall.Round(time.Millisecond), workers, c.pass, c.fail, c.skip)

	baseline, err := readBaseline("baseline.txt", *prefix)
	if err != nil && !*update {
		t.Fatal(err)
	}
	byPath := make(map[string]Result, len(results))
	for _, res := range results {
		byPath[res.Path] = res
	}
	var lost, gained []string
	for _, p := range baseline {
		if _, ok := slices.BinarySearch(passing, p); !ok {
			lost = append(lost, p)
		}
	}
	for _, p := range passing {
		if _, ok := slices.BinarySearch(baseline, p); !ok {
			gained = append(gained, p)
		}
	}
	if len(gained) > 0 {
		t.Logf("%d tests pass that are not in baseline.txt (run with -update to add them), the first %d:\n  %s",
			len(gained), min(len(gained), newlyPassingShown), strings.Join(gained[:min(len(gained), newlyPassingShown)], "\n  "))
	}
	if len(lost) > 0 {
		var lines []string
		for _, p := range lost {
			res, ok := byPath[p]
			switch {
			case !ok:
				lines = append(lines, p+": not found")
			case res.Status == Skip:
				lines = append(lines, p+": skipped: "+res.Message)
			default:
				lines = append(lines, p+": "+res.Message)
			}
		}
		msg := strings.Join(lines, "\n  ")
		if *update {
			t.Logf("WARNING: %d baseline tests no longer pass and leave the baseline:\n  %s", len(lost), msg)
		} else {
			t.Errorf("%d baseline tests no longer pass:\n  %s", len(lost), msg)
		}
	}

	if *update {
		if err := os.WriteFile("baseline.txt", []byte(strings.Join(passing, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		rep := &Report{Revision: string(revision), Results: results, Wall: wall, Workers: workers}
		if err := os.WriteFile("RESULTS.md", []byte(rep.Markdown()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// readBaseline returns the sorted baseline entries that start with prefix.
func readBaseline(path, prefix string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		if line = strings.TrimSpace(line); line != "" && strings.HasPrefix(line, prefix) {
			out = append(out, line)
		}
	}
	slices.Sort(out)
	return out, nil
}
