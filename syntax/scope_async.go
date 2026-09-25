package syntax

// checkAwait checks an await expression or a for await statement at pos:
// it must be in the body of an async function or arrow, or at the top level
// of a module, which then evaluates asynchronously (Module.Async).
func (r *resolver) checkAwait(pos int) {
	n := len(r.frames) - 1
	fn := r.frames[n].fn
	switch {
	case fn == nil && r.module != nil:
		if !r.module.Async || pos < r.firstAwait {
			r.firstAwait = pos
		}
		r.module.Async = true
	case fn == nil || !fn.IsAsync:
		r.fail(pos, "await is only valid in async functions and the top level bodies of modules")
	case len(r.inParams) > 0 && r.inParams[len(r.inParams)-1].frame == n:
		r.fail(pos, "Illegal await-expression in formal parameters of async function")
	}
}
