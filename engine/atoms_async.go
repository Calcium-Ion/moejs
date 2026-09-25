package engine

// Static atoms for generators and async functions. They live apart from
// atoms.go so parallel additions to that table merge mechanically.
var (
	AtomGenerator         = staticAtom("Generator")
	AtomGeneratorFunction = staticAtom("GeneratorFunction")
	AtomAsyncFunction     = staticAtom("AsyncFunction")

	AtomAsyncGenerator         = staticAtom("AsyncGenerator")
	AtomAsyncGeneratorFunction = staticAtom("AsyncGeneratorFunction")
	atomAsyncIteratorFn        = staticAtom("[Symbol.asyncIterator]")

	AtomFromAsync = staticAtom("fromAsync")
)
