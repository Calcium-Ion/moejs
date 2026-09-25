package engine

// Static atoms for BigInt and Proxy. They live apart from atoms.go so
// parallel additions to that table merge mechanically; staticAtom
// deduplicates names.
var (
	AtomAsIntN     = staticAtom("asIntN")
	AtomAsUintN    = staticAtom("asUintN")
	AtomProxy      = staticAtom("Proxy")
	AtomProxyLower = staticAtom("proxy")
	AtomRevocable  = staticAtom("revocable")
	AtomRevoke     = staticAtom("revoke")
)
