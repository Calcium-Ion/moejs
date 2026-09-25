package engines

// All returns the engines in the order the report tables use: moejs, then
// the three baselines.
func All() []Engine {
	return []Engine{NewMoejsEngine(), NewSobekEngine(), NewQuickJSEngine(), NewV8Engine()}
}

// PureGo returns the engines that do not cross a cgo boundary and therefore
// share the Go garbage collector.
func PureGo() []Engine { return []Engine{NewMoejsEngine(), NewSobekEngine()} }

// ByName looks an engine up by its Name().
func ByName(name string) Engine {
	for _, e := range All() {
		if e.Name() == name {
			return e
		}
	}
	return nil
}
