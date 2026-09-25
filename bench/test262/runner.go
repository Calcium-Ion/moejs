package test262

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Calcium-Ion/moejs/bytecode"
	"github.com/Calcium-Ion/moejs/compiler"
	"github.com/Calcium-Ion/moejs/engine"
	"github.com/Calcium-Ion/moejs/syntax"
)

// Dirs are the directories under test/ that the suite runs. intl402,
// annexB and staging are out of scope.
var Dirs = []string{"language", "built-ins", "harness"}

// Timeout is the interrupt deadline of one test (a variable for the
// runner's own tests).
var Timeout = 10 * time.Second

// The skip reasons that do not name a feature.
const (
	SkipSloppy  = "sloppy mode"
	SkipModules = "modules"
)

// The lines doneprintHandle.js prints for an async test.
const (
	asyncComplete = "Test262:AsyncTestComplete"
	asyncFailure  = "Test262:AsyncTestFailure:"
)

// Status is the outcome of a test.
type Status uint8

const (
	Pass Status = iota
	Fail
	Skip
)

// Result is the outcome of one test file.
type Result struct {
	Path     string   // relative to test/, with forward slashes
	Status   Status   //
	Message  string   // the skip reason, or the failure (first line)
	Features []string // listed and include-implied, sorted
	Blockers []string // the unimplemented features of a skipped test
	Panic    bool     // the failure is a Go panic
	Duration time.Duration
}

// Suite runs the tests of one test262 checkout.
type Suite struct {
	Root    string // the checkout: test/ and harness/
	implied map[string][]string
	bundles sync.Map // include list -> *bundle
}

// NewSuite opens the checkout at root.
func NewSuite(root string) (*Suite, error) {
	implied, err := loadImplied(root)
	if err != nil {
		return nil, err
	}
	return &Suite{Root: root, implied: implied}, nil
}

// Discover lists the test files of Dirs (fixtures excluded) whose path
// relative to test/ starts with prefix, sorted.
func (s *Suite) Discover(prefix string) ([]string, error) {
	var paths []string
	for _, dir := range Dirs {
		err := filepath.WalkDir(filepath.Join(s.Root, "test", dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".js") || strings.HasSuffix(p, "_FIXTURE.js") {
				return err
			}
			rel, err := filepath.Rel(filepath.Join(s.Root, "test"), p)
			if rel = filepath.ToSlash(rel); err == nil && strings.HasPrefix(rel, prefix) {
				paths = append(paths, rel)
			}
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(paths)
	return paths, nil
}

// Workers is the size of the worker pool: GOMAXPROCS, but 8 by default
// while the machine is shared, or MOEJS_TEST262_WORKERS when it is set.
func Workers() int {
	n := min(runtime.GOMAXPROCS(0), 8)
	if v, err := strconv.Atoi(os.Getenv("MOEJS_TEST262_WORKERS")); err == nil && v > 0 {
		n = min(v, runtime.GOMAXPROCS(0))
	}
	return n
}

// Run runs paths on a pool of workers and returns the results in order.
func (s *Suite) Run(paths []string, workers int) []Result {
	results := make([]Result, len(paths))
	var next atomic.Int64
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1)) - 1
				if i >= len(paths) {
					return
				}
				results[i] = s.RunTest(paths[i])
			}
		}()
	}
	wg.Wait()
	return results
}

// RunTest runs one test (path relative to test/) and never panics: a Go
// panic in the engine is recorded as a failure with the panic text.
func (s *Suite) RunTest(path string) (res Result) {
	start := time.Now()
	res.Path = path
	defer func() {
		if p := recover(); p != nil {
			res.Status, res.Panic = Fail, true
			res.Message = fmt.Sprintf("panic: %v%s", p, panicSite(debug.Stack()))
		}
		res.Duration = time.Since(start)
	}()
	b, err := os.ReadFile(filepath.Join(s.Root, "test", filepath.FromSlash(path)))
	if err != nil {
		res.Status, res.Message = Fail, err.Error()
		return res
	}
	src := string(b)
	meta, err := ParseMeta(src)
	if err != nil {
		res.Status, res.Message = Fail, "frontmatter: "+err.Error()
		return res
	}
	res.Features = s.features(path, meta)
	if res.Message, res.Blockers = s.skip(meta, src, res.Features); res.Message != "" {
		res.Status = Skip
		return res
	}
	res.Status, res.Message = s.execute(path, meta, src)
	return res
}

// features returns the test's features plus those its includes imply. A
// test of the typed array directories needs TypedArray even when it lists
// only BigInt (the BigInt64Array constructor tests).
func (s *Suite) features(path string, m *Meta) []string {
	fs := slices.Clone(m.Features)
	for _, inc := range m.Includes {
		fs = append(fs, s.implied[inc]...)
	}
	if strings.HasPrefix(path, "built-ins/TypedArray") {
		fs = append(fs, "TypedArray")
	}
	slices.Sort(fs)
	return slices.Compact(fs)
}

// skip returns the reason a test does not run, if any, and the unimplemented
// features it needs. Non-goals come first, then unimplemented features, then
// the modes moejs does not support yet.
func (s *Suite) skip(m *Meta, src string, features []string) (string, []string) {
	for _, f := range features {
		if nonGoal(f) {
			return "non-goal: " + f, nil
		}
	}
	if m.HasFlag("CanBlockIsTrue") {
		return "non-goal: Atomics.wait (CanBlockIsTrue)", nil
	}
	if strings.Contains(src, "$262.agent") {
		return "non-goal: multi-agent SharedArrayBuffer ($262.agent)", nil
	}
	var blockers []string
	for _, f := range features {
		if !implemented[f] {
			blockers = append(blockers, f)
		}
	}
	if len(blockers) > 0 {
		return "feature: " + blockers[0], blockers
	}
	if m.HasFlag("noStrict") || m.HasFlag("raw") {
		return SkipSloppy, nil
	}
	return "", nil
}

// execute compiles and runs a test that is not skipped.
func (s *Suite) execute(path string, m *Meta, src string) (Status, string) {
	// The test is parsed and compiled before anything is evaluated, so a
	// parse-phase negative test never runs.
	module := m.HasFlag("module")
	var code *bytecode.Function
	var err error
	if module {
		// The resolver rejects module requests as unsupported, so look for
		// them in a parse that allows unsupported constructs; a syntax error
		// shows again in the real parse.
		if mod, err := syntax.ParseModule(path, src, syntax.Options{AllowUnsupported: true}); err == nil && importsModules(mod) {
			return Skip, SkipModules
		}
		var mod *syntax.Module
		if mod, err = syntax.ParseModule(path, src, syntax.Options{}); err == nil {
			code, err = compiler.CompileModule(mod)
		}
	} else {
		code, err = compileScript(path, "\"use strict\";\n"+src)
	}
	neg := m.Negative
	if err != nil {
		msg := compileMessage(err)
		if neg != nil && (neg.Phase == "parse" || neg.Phase == "resolution") && !unsupported(msg) {
			if errorName(msg) == neg.Type {
				return Pass, ""
			}
			return Fail, "expected a " + neg.Phase + "-phase " + neg.Type + ", got " + msg
		}
		return Fail, msg
	}
	if neg != nil && neg.Phase == "parse" {
		return Fail, "expected a parse-phase " + neg.Type + ", but the test compiled"
	}
	async := m.HasFlag("async")
	names := m.Includes
	if async {
		names = append([]string{"doneprintHandle.js"}, names...)
	}
	includes, err := s.includes(names)
	if err != nil {
		return Fail, "includes: " + err.Error()
	}

	h := &host{}
	r := h.newRealm()
	timer := time.AfterFunc(Timeout, h.interrupt)
	defer timer.Stop()
	if _, err := r.RunScript(includes); err != nil {
		return Fail, "includes: " + runMessage(r, err)
	}
	if module {
		_, err = r.EvaluateModule(code)
	} else {
		_, err = r.RunScript(code)
	}
	switch {
	case err == nil && neg == nil && async:
		return asyncOutcome(h.printed)
	case err == nil && neg == nil:
		return Pass, ""
	case err == nil:
		return Fail, "expected a " + neg.Phase + "-phase " + neg.Type + ", but the test completed"
	}
	var ie *engine.InterruptedError
	if errors.As(err, &ie) {
		return Fail, fmt.Sprintf("timeout (%v)", Timeout)
	}
	msg := runMessage(r, err)
	if neg == nil || unsupported(msg) {
		return Fail, msg
	}
	var exc *engine.Exception
	if errors.As(err, &exc) && constructorName(r, exc.Value) == neg.Type {
		return Pass, ""
	}
	return Fail, "expected a " + neg.Type + ", got " + msg
}

// asyncOutcome is the outcome of an async test that completed. The job
// queue drains before RunScript and EvaluateModule return, so the test has
// called $DONE by then if it ever will: it passes on the line
// doneprintHandle.js prints for $DONE(), fails with the error $DONE(error)
// printed, and fails when neither was printed.
func asyncOutcome(printed []string) (Status, string) {
	for _, line := range printed {
		if line == asyncComplete {
			return Pass, ""
		}
		if msg, ok := strings.CutPrefix(line, asyncFailure); ok {
			return Fail, msg
		}
	}
	return Fail, "the async test did not call $DONE"
}

// importsModules reports whether a module requests other modules, which
// needs a module loader.
func importsModules(m *syntax.Module) bool {
	for _, st := range m.Body {
		switch x := st.(type) {
		case *syntax.ImportDecl:
			return true
		case *syntax.ExportAll:
			return true
		case *syntax.ExportNamed:
			if x.Source != nil {
				return true
			}
		}
	}
	return false
}

// bundle is a compiled include list, built once per process.
type bundle struct {
	once sync.Once
	code *bytecode.Function
	err  error
}

// includes returns the compiled harness for a test: assert.js and sta.js,
// then names (doneprintHandle.js first for an async test, then the test's
// includes).
func (s *Suite) includes(names []string) (*bytecode.Function, error) {
	all := []string{"assert.js", "sta.js"}
	for _, n := range names {
		if !slices.Contains(all, n) {
			all = append(all, n)
		}
	}
	v, _ := s.bundles.LoadOrStore(strings.Join(all, ","), &bundle{})
	b := v.(*bundle)
	b.once.Do(func() { b.code, b.err = s.compileIncludes(all) })
	return b.code, b.err
}

// compileIncludes compiles harness files as one script that ends by
// publishing its top-level bindings on the global object. Every moejs
// script has its own top-level scope, so the test, a separate script run
// next in the same realm, reaches the harness through the global object,
// as it would reach the global declarations of a concatenated script.
func (s *Suite) compileIncludes(names []string) (*bytecode.Function, error) {
	var src strings.Builder
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(s.Root, "harness", n))
		if err != nil {
			return nil, err
		}
		if _, err := syntax.ParseScript(n, string(b), syntax.Options{}); err != nil {
			return nil, fmt.Errorf("%s: %s", n, compileMessage(err))
		}
		src.Write(b)
		src.WriteString("\n;\n")
	}
	sc, err := syntax.ParseScript("harness", src.String(), syntax.Options{})
	if err != nil {
		return nil, fmt.Errorf("%s: %s", strings.Join(names, " + "), compileMessage(err))
	}
	for _, b := range sc.Scope.Bindings {
		fmt.Fprintf(&src, "globalThis.%s = %s;\n", b.Name, b.Name)
	}
	code, err := compileScript("harness", src.String())
	if err != nil {
		return nil, fmt.Errorf("%s: %s", strings.Join(names, " + "), compileMessage(err))
	}
	return code, nil
}

// compileMessage is a parse or compile error without its position.
func compileMessage(err error) string {
	var se *syntax.Error
	var ce *compiler.Error
	switch {
	case errors.As(err, &se):
		return se.Msg
	case errors.As(err, &ce):
		return ce.Msg
	}
	return err.Error()
}

// runMessage is the first line of a runtime error: "Name: message" for a
// thrown object with a constructor name (the engine renders only Error
// objects that way, and the harness throws Test262Error objects).
func runMessage(r *engine.Realm, err error) string {
	msg, _, _ := strings.Cut(err.Error(), "\n")
	var exc *engine.Exception
	if !errors.As(err, &exc) || !exc.Value.IsObject() || strings.Contains(msg, ": ") {
		return msg
	}
	name := constructorName(r, exc.Value)
	m, err := exc.Value.AsObject().GetProp(r, r.KeyFromGoString("message"))
	if name == "" || err != nil || !m.IsString() {
		return msg
	}
	msg, _, _ = strings.Cut(name+": "+m.String(), "\n")
	return msg
}

// unsupported reports a moejs "not supported yet" error, which is a failure
// even where the test expects an error.
func unsupported(msg string) bool {
	return strings.Contains(msg, "not supported yet")
}

var errorNameRE = regexp.MustCompile(`^([A-Za-z0-9]*Error): `)

// errorName is the constructor name a parse or compile error message
// starts with, or "".
func errorName(msg string) string {
	if m := errorNameRE.FindStringSubmatch(msg); m != nil {
		return m[1]
	}
	return ""
}

// constructorName is v.constructor.name, or "" when v is not an object or
// the lookup fails.
func constructorName(r *engine.Realm, v engine.Value) string {
	if !v.IsObject() {
		return ""
	}
	c, err := v.AsObject().GetProp(r, r.KeyFromGoString("constructor"))
	if err != nil || !c.IsObject() {
		return ""
	}
	n, err := c.AsObject().GetProp(r, r.KeyFromGoString("name"))
	if err != nil || !n.IsString() {
		return ""
	}
	return n.String()
}

// panicSite returns " at <function> (<file>:<line>)" for the frame that
// panicked, taken from a debug.Stack() trace.
func panicSite(stack []byte) string {
	lines := strings.Split(string(stack), "\n")
	for i := 0; i+3 < len(lines); i++ {
		if strings.HasPrefix(lines[i], "panic(") {
			fn := lines[i+2]
			if j := strings.LastIndex(fn, "("); j > 0 {
				fn = fn[:j]
			}
			file := strings.TrimSpace(lines[i+3])
			if j := strings.LastIndex(file, " +0x"); j > 0 {
				file = file[:j]
			}
			return " at " + fn + " (" + filepath.Base(file) + ")"
		}
	}
	return ""
}
