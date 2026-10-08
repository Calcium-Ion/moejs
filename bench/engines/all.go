package engines

// All returns the engines in the order the report tables use: moejs, then
// the baselines. QuickJS appears three times: with the quickjs-go defaults,
// tuned the way a pooled host would run it (see QuickJSEngine), and as
// modernc.org/quickjs, QuickJS translated to pure Go.
func All() []Engine {
	return []Engine{NewMoejsEngine(), NewSobekEngine(), NewQuickJSEngine(), NewQuickJSTunedEngine(), NewModerncQuickJSEngine(), NewV8Engine()}
}

// PureGo returns the engines that do not cross a cgo boundary and therefore
// share the Go garbage collector.
func PureGo() []Engine { return []Engine{NewMoejsEngine(), NewSobekEngine()} }

// Cgo reports whether e runs a C engine behind cgo (QuickJS in both
// configurations, V8): values cross as JSON text and the engine heap lives
// outside the Go collector.
func Cgo(e Engine) bool {
	switch e.(type) {
	case *QuickJSEngine, *V8Engine:
		return true
	}
	return false
}

// ByName looks an engine up by its Name().
func ByName(name string) Engine {
	for _, e := range All() {
		if e.Name() == name {
			return e
		}
	}
	return nil
}
