package engine

// Unmapped (strict) arguments objects. Every function is strict, so the
// mapped exotic form never arises; a function gets an arguments object only
// when its body or a nested arrow references `arguments` (the compiler sets
// bytecode.Function.HasArguments and enterFrame calls newArguments).

// argumentsObject co-allocates an arguments object with its three named
// slots (length, @@iterator, callee) and up to four elements.
type argumentsObject struct {
	Object
	slots [3]Value
	elems [4]Value
}

// argumentsShape returns the realm's shape of a fresh arguments object:
// Object.prototype root + length + @@iterator + callee.
func (r *Realm) argumentsShape() *Shape {
	l := r.lazyState()
	if l.argumentsShape == nil {
		l.argumentsShape = r.plainRoot.
			addProperty(r, lengthKey, attrHidden).
			addProperty(r, SymbolKey(SymIterator), attrHidden).
			addProperty(r, StringKey(AtomCallee), attrAccessor)
	}
	return l.argumentsShape
}

// newArguments implements CreateUnmappedArgumentsObject: indexed data
// properties copied from args, a non-enumerable length, @@iterator =
// %Array.prototype.values% and a %ThrowTypeError% callee accessor.
func (r *Realm) newArguments(args []Value) *Object {
	x := &argumentsObject{}
	o := initObject(&x.Object, ClassArguments, r.argumentsShape())
	o.slots = x.slots[:]
	o.slots[0] = IntValue(len(args))
	o.slots[1] = ObjectValue(r.arrayValuesFn)
	o.slots[2] = accessorValue(r.restrictedAccessor)
	if n := len(args); n > 0 {
		elems := x.elems[:0:len(x.elems)]
		if n > len(x.elems) {
			elems = make([]Value, 0, n)
		}
		o.elements = append(elems, args...)
	}
	return o
}

// plainArguments reports whether the arguments object o still has
// %Array.prototype.values% as its own @@iterator data property, so iterating
// it cannot run user code beyond its element getters.
func (r *Realm) plainArguments(o *Object) bool {
	p, attrs, ok := o.lookupNamed(SymbolKey(SymIterator))
	return ok && attrs&attrAccessor == 0 && p.IsObject() && p.AsObject() == r.arrayValuesFn
}
