package syntax

// checkYield rejects a yield outside the body of a generator: in any other
// function, an arrow or a class initializer, where strict code reserves the
// word, and in the generator's own parameter list.
func (r *resolver) checkYield(e *YieldExpr) {
	if fn := r.currentFn(); fn == nil || !fn.IsGenerator {
		r.fail(e.Pos, "Unexpected strict mode reserved word")
	}
	if n := len(r.inParams); n > 0 && r.inParams[n-1].frame == len(r.frames)-1 {
		r.fail(e.Pos, "Yield expression not allowed in formal parameter")
	}
}
