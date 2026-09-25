package engine

// Atoms of the keyed collections, AggregateError and the Set methods.
// (Names that other builtin groups may also intern get a collection-specific
// Go name: staticAtom deduplicates the strings themselves.)
var (
	AtomMapName             = staticAtom("Map")
	AtomSetName             = staticAtom("Set")
	AtomWeakMap             = staticAtom("WeakMap")
	AtomWeakSet             = staticAtom("WeakSet")
	AtomWeakRef             = staticAtom("WeakRef")
	AtomMapIterator         = staticAtom("Map Iterator")
	AtomSetIterator         = staticAtom("Set Iterator")
	AtomAggregateError      = staticAtom("AggregateError")
	AtomErrors              = staticAtom("errors")
	AtomGroupBy             = staticAtom("groupBy")
	AtomDeref               = staticAtom("deref")
	AtomAdd                 = staticAtom("add")
	AtomClear               = staticAtom("clear")
	AtomDelete              = staticAtom("delete")
	atomCollHas             = staticAtom("has")
	AtomUnion               = staticAtom("union")
	AtomIntersection        = staticAtom("intersection")
	AtomDifference          = staticAtom("difference")
	AtomSymmetricDifference = staticAtom("symmetricDifference")
	AtomIsSubsetOf          = staticAtom("isSubsetOf")
	AtomIsSupersetOf        = staticAtom("isSupersetOf")
	AtomIsDisjointFrom      = staticAtom("isDisjointFrom")
)
