package engine

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stackLocation matches the locations in a trace of testdata/stack_v8.js.
var stackLocation = regexp.MustCompile(`t\.js:\d+:\d+`)

// stackShape returns the header and the first frames of a trace up to and
// including the frame of export, with locations replaced by F.
func stackShape(stack, export string) string {
	lines := strings.Split(stackLocation.ReplaceAllString(stack, "F"), "\n")
	out := lines[:1]
	for _, l := range lines[1:] {
		out = append(out, strings.TrimSpace(l))
		if strings.HasPrefix(strings.TrimSpace(l), "at "+export+" ") {
			break
		}
	}
	return strings.Join(out, " | ")
}

// TestErrorStackV8 compares the frames of testdata/stack_v8.js with Node
// 26's (V8's) output for the same module, where the exports are called as
// methods of the namespace ("Module.name") instead of from Go.
func TestErrorStackV8(t *testing.T) {
	src, err := os.ReadFile("testdata/stack_v8.js")
	require.NoError(t, err)
	f := evalModule(t, string(src))
	want := map[string]string{
		"aliased":         "Error: q | at Object.q [as w] (F)",
		"applyObj":        "Error: b | at Object.af (F)",
		"arrayFrom":       "Error: fr | at fromcb (F) | at Array.from (<anonymous>)",
		"arrayIter":       "Error: ai | at Array Iterator.f (F)",
		"asMethodSuffix":  "Error: s | at Object.g [as f] (F)",
		"bound":           "Error: b | at Object.bf (F)",
		"boundNull":       "Error: b | at bf (F)",
		"callAnonFn":      "Error: b | at Function.cf (F)",
		"callArr":         "Error: b | at Array.cf (F)",
		"callBool":        "Error: u | at Boolean.br (F)",
		"callFn":          "Error: b | at Q.cf (F)",
		"callNum":         "Error: u | at Number.nr (F)",
		"callObj":         "Error: b | at Object.cf (F)",
		"callStr":         "Error: u | at String.sr (F)",
		"callUndef":       "Error: u | at F",
		"customCtor":      "Error: pt | at Point.at (F)",
		"dupNames":        "Error: q | at Object.q (F)",
		"errorCall":       "Error: x | at ec (F)",
		"errorDotCall":    "Error: x | at ed (F)",
		"fnReceiverNamed": "Error: h | at Holder.make (F)",
		"forEachArrow":    "Error: c | at F | at Array.forEach (<anonymous>)",
		"forEachNamed":    "Error: c | at cb (F) | at Array.forEach (<anonymous>)",
		"getter":          "Error: g | at get g (F)",
		"globalFn":        "Error: g | at gf (F)",
		"jsonMethod":      "Error: jm | at JSON.f (F)",
		"mathMethod":      "Error: mm | at Math.f (F)",
		"method":          "Error: m | at Object.m (F)",
		"nested":          "Error: p | at Object.y (F)",
		"newAnon":         "Error: x | at new <anonymous> (F)",
		"newF":            "Error: F | at new F (F)",
		"nonIdentName":    "Error: nb | at a-b (F)",
		"nullProto":       "Error: np | at Object.f (F)",
		"nullRead":        "TypeError: Cannot read properties of null (reading 'x')",
		"objectKeys":      "TypeError: Cannot convert undefined or null to object | at Object.keys (<anonymous>)",
		"protoMethod":     "Error: pm | at Object.pm (F)",
		"renamed":         "Error: r | at zz (F)",
		"setter":          "Error: s | at set s (F)",
		"two":             "Error: i | at inner (F) | at outer (F)",

		// Classes: super() and Reflect.construct frames are constructs, a
		// class called without new has no frame of its own, and
		// Reflect.apply/construct show none either.
		"classCtorThrow":      "Error: t | at new T1 (F)",
		"classDefaultDerived": "Error: p | at new P1 (F) | at new C3 (F)",
		"classErrorSub":       "Error: x | at mk (F)",
		"classErrorSubCtor":   "Error: y | at mk (F)",
		"classExprAnon":       "Error: k | at K.m (F)",
		"classGetter":         "Error: g | at get g (F)",
		"classInherited":      "Error: m | at R2.m (F)",
		"classInit":           "Error: i | at <instance_members_initializer> (F) | at new I1 (F)",
		"classInitDerived":    "Error: i2 | at <instance_members_initializer> (F) | at new I2 (F)",
		"classMethod":         "Error: m | at Q3.m (F)",
		"classNewTargetFn":    "Error: n | at new N1 (F)",
		"classNoNew":          "TypeError: Class constructor P1 cannot be invoked without 'new'",
		"classPrivate":        "Error: pp | at #p (F) | at P2.m (F)",
		"classStatic":         "Error: s | at Q1.s (F)",
		"classStaticGetter":   "Error: g | at get g (F)",
		"classStaticInit":     "Error: si | at <static_initializer> (F)",
		"classSuper":          "Error: p | at new P1 (F) | at new C1 (F)",
		"classSuperSpread":    "Error: p | at new P1 (F) | at new C2 (F)",
		"mapClass":            "TypeError: Class constructor M cannot be invoked without 'new' | at Array.map (<anonymous>)",
		"mapClassNoNew":       "TypeError: Class constructor M2 cannot be invoked without 'new' | at Array.map (<anonymous>)",
		"reflApply":           "Error: ra | at Object.ra (F)",
		"reflApplyBad":        "TypeError: bad",
		"reflApplyNull":       "Error: ra | at ra (F)",
		"reflConstruct":       "Error: rc | at new RC (F)",
		"reflConstructArgs":   "TypeError: CreateListFromArrayLike called on non-object",
		"reflConstructBad":    "TypeError: bad",
		"reflConstructNT":     "Error: rc | at new RC (F)",
		"superNative":         "Error: sn | at new SN (F)",

		// Deviations from V8. V8 names the
		// function by the assignment target ("obj.prop"), knows the
		// receiver of frames entered by natives or implicitly ("Object.cb",
		// "Array.rv", "Object.toString") and prefers the constructor that
		// created the receiver to its prototype's `constructor` ("P.m").
		// A static block is a function of its own called by the static
		// initializer; V8 runs it inside <static_initializer>.
		"assigned":         "Error: p | at Object.prop (F)",
		"forEachThisArg":   "Error: c | at cb (F) | at Array.forEach (<anonymous>)",
		"reviver":          "Error: rv | at rv (F) | at JSON.parse (<anonymous>)",
		"stringConv":       "Error: ts | at toString (F) | at String (<anonymous>)",
		"changedCtor":      "Error: c | at Other.m (F)",
		"classStaticBlock": "Error: sb | at F | at <static_initializer> (F)",
	}
	names := make([]string, 0, len(want))
	for name := range want {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		stack, ok := f.call(name).(string)
		require.True(t, ok, name)
		got := stackShape(stack, name)
		if name == "jsonParse" {
			got = got[strings.Index(got, " | "):]
		}
		assert.Equal(t, want[name]+" | at "+name+" (F)", got, name)
	}
	stack := f.call("jsonParse").(string)
	assert.Equal(t, "at JSON.parse (<anonymous>) | at jsonParse (F)", strings.SplitN(stackShape(stack, "jsonParse"), " | ", 2)[1])
	// new.target (a class that never ran) is not on the stack: no frames.
	assert.Equal(t, "Error: n", f.call("reflConstructNative"))
}

// TestErrorCaptureStackTrace compares Error.captureStackTrace and Error.
// stackTraceLimit in testdata/stack_capture_v8.js with Node 26's output.
func TestErrorCaptureStackTrace(t *testing.T) {
	src, err := os.ReadFile("testdata/stack_capture_v8.js")
	require.NoError(t, err)
	f := evalModule(t, string(src))
	// The trace up to the export's frame, which Node shows as "Module.name".
	want := map[string]string{
		"assigned":     "5",
		"capSelf":      "Error | at cs (F) | at capSelf (F)",
		"cstDesc":      "true,false,true",
		"customHeader": "H: hm | at customHeader (F)",
		"desc":         "function,function,false,true",
		"emptyName":    "m",
		"frozen":       "TypeError: Cannot define property stack, object is not extensible",
		"lazyHeader":   "N: late",
		"limit0":       "Error: z",
		"limitCapture": "undefined",
		"limitDeleted": "undefined",
		"limitFrac":    "Error: z | at lf (F)",
		"limitNaN":     "Error: z",
		"limitNeg":     "Error: z",
		"limitStr":     "undefined",
		"limitUndef":   "undefined",
		"multiLine":    "Error: a\nb | at multiLine (F)",
		"nonObject":    "TypeError: invalid_argument",
		"plainHeader":  "Error | at plainHeader (F)",
		"protoStack":   "undefined",
		"recap":        "Error: r | at recap (F)",
		"recapErr":     "TypeError: t | at recapErr (F)",
		"ret":          "undefined 2",
		"skipBound":    "Error | at skipBound (F)",
		"skipFn":       "Error | at a (F) | at skipFn (F)",
		"skipMissing":  "Error",
		"stlDesc":      "10,true,true,true",
		"wrapper":      "Error: w | at wrapper (F)",
		// Header plus 10 frames, plus 25, and all 44 live frames (41 of rec,
		// the arrow, withLimit, deepInf) where Node adds its module frames.
		"deep":      "11",
		"deepLimit": "26",
		"deepInf":   "45",
	}
	names := make([]string, 0, len(want))
	for name := range want {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		got, ok := f.call(name).(string)
		require.True(t, ok, name)
		if name == "multiLine" {
			got = strings.Replace(got, "\n", "\\n", 1)
			assert.Equal(t, strings.Replace(want[name], "\n", "\\n", 1), stackShape(got, name), name)
			continue
		}
		assert.Equal(t, want[name], stackShape(got, name), name)
	}
}

// TestErrorStackTraceLimitShared checks that a realm over shared intrinsics
// keeps the default limit and cannot change it.
func TestErrorStackTraceLimitShared(t *testing.T) {
	f := evalModuleWith(t, `
function rec(n) { return n === 0 ? new Error("d") : rec(n - 1); }
export function depth() { return rec(20).stack.split("\n").length; }
export function set() { try { Error.stackTraceLimit = 1; return "set"; } catch (e) { return String(e); } }
export function capture() { const o = {}; Error.captureStackTrace(o); return o.stack; }
`, RealmOptions{SharedIntrinsics: true})
	assert.EqualValues(t, 11, f.call("depth"))
	assert.Contains(t, f.call("set"), "TypeError")
	assert.EqualValues(t, 11, f.call("depth"))
	assert.Regexp(t, `^Error\n    at capture \(\w+\.js:\d+:\d+\)$`, f.call("capture"))
}

// TestErrorStackCaptureAllocs checks that a shallow throw records its frames
// in the error object and a deep one past the scratch buffer still works.
func TestErrorStackCaptureAllocs(t *testing.T) {
	f := evalModule(t, `
export function make() { return new Error("x"); }
function rec(n) { return n === 0 ? new Error("d") : rec(n - 1); }
export function deep(n) { Error.stackTraceLimit = n; try { return rec(3 * n).stack; } finally { Error.stackTraceLimit = 10; } }
`)
	fn, ok := f.env.GetBindingValue("make")
	require.True(t, ok)
	allocs := testing.AllocsPerRun(100, func() {
		_, _ = f.r.Call(fn, Undefined(), nil)
	})
	// The error object is the only allocation: its frames live in the
	// object's inline buffer.
	assert.Equal(t, 1.0, allocs)
	for _, n := range []int{2, 3, stackScratch - 1, stackScratch, stackScratch + 1, 3 * stackScratch} {
		stack := f.call("deep", n).(string)
		lines := strings.Split(stack, "\n")
		assert.Len(t, lines, n+1, "limit %d", n)
		assert.True(t, strings.HasPrefix(lines[n], "    at rec (t.js:"), "limit %d: %s", n, lines[n])
	}
}
