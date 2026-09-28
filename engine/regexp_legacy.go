package engine

// RegExp.prototype.compile (ES2025 B.2.4.1, with the realm and
// [[LegacyFeaturesEnabled]] checks of the legacy RegExp features
// proposal). The legacy static accessors (RegExp.$1-$9, input, lastMatch
// and the rest) are not implemented: every successful match, the fast
// paths included, would have to record its subject and captures in the
// realm, and a realm would have to create the accessors.

var compileKey = StringKey(AtomCompile)

// regexpProtoCompile reinitializes a RegExp in place: RegExpInitialize with
// the source and flags of a RegExp pattern, or pattern and flags coerced
// to strings. Only a RegExp whose legacy is the calling realm qualifies: r
// is the realm of the compile function called (a native runs with the
// realm that calls it, and moejs has no cross-realm functions), so a RegExp
// of another realm, which a host may hand over, or a subclass instance
// throws a TypeError, and so does a shared object, which no program's realm
// created. The payload is replaced before lastIndex is set to 0, which
// throws when lastIndex is not writable, as the spec's Set does after
// RegExpInitialize has recompiled. The paths that hold the payload while
// user code runs (execLastIndex, regexpReplace, simpleReplace) read the
// program again or keep the one they started with, as the spec's.
func regexpProtoCompile(r *Realm, this Value, args []Value) (Value, error) {
	o, d, err := thisRegExp(r, this, "compile")
	if err != nil {
		return Undefined(), err
	}
	if d.legacy != r {
		return Undefined(), r.TypeError("RegExp.prototype.compile requires a RegExp created by this realm's RegExp constructor")
	}
	pattern, flags := Arg(args, 0), Arg(args, 1)
	ps, fs := AtomEmpty, AtomEmpty
	if _, pd, ok := isRegExpObject(pattern); ok {
		if !flags.IsUndefined() {
			return Undefined(), r.TypeError("Cannot supply flags when constructing one RegExp from another")
		}
		ps, fs = pd.source, pd.flags
	} else {
		if !pattern.IsUndefined() {
			if ps, err = r.ToString(pattern); err != nil {
				return Undefined(), err
			}
		}
		if !flags.IsUndefined() {
			if fs, err = r.ToString(flags); err != nil {
				return Undefined(), err
			}
		}
	}
	f, ascii := fs.ASCII()
	fl, valid := parseRegExpFlags(f)
	if !ascii || !valid {
		return Undefined(), r.SyntaxError("Invalid flags supplied to RegExp constructor '%s'", fs.GoString())
	}
	c, err := r.compileRegExp(ps, fl)
	if err != nil {
		return Undefined(), err
	}
	if canonical := fl.String(); canonical != f {
		fs = asciiString(canonical)
	}
	d.source, d.flags, d.c = ps, fs, c
	if err := r.setRegExpLastIndex(o, 0); err != nil {
		return Undefined(), err
	}
	return this, nil
}

// installRegExpCompile defines RegExp.prototype.compile, last: at once while
// building the shared template, on first use in a mutable realm
// (pendingCompile), where it would cost every realm a function, a shape
// and a transition few programs use.
func installRegExpCompile(r *Realm) {
	p := r.RegExpPrototype
	if r.buildingShared {
		p.addNamed(r, compileKey, compileCell(r))
		return
	}
	p.mustBeMutable()
	p.internal = (*pendingCompile)(r)
	p.flags |= flagHasLazy
	if p.flags&flagDict == 0 && !p.shape.shared {
		p.shape.noFill = true
	}
}

func compileCell(r *Realm) propCell {
	return propCell{value: ObjectValue(r.NewNativeFunction(AtomCompile, 2, regexpProtoCompile)), attrs: attrHidden}
}

// pendingCompile is the internal of %RegExp.prototype% in a mutable realm
// until compile is defined: by a lookup of compile, or before a write,
// redefinition or deletion of any key and before the key list, after the
// properties the prototype has then, where the shared template has it
// (installRegExpCompile runs last). A lookup of any other key finds the
// prototype as it is, so the pristine RegExp paths (regexpGuard) and the
// inline caches of its methods work as without it; the prototype's shape
// is noFill meanwhile, as a deferInstall object's is, so no cache filled
// for compile on another object of that shape hits it.
type pendingCompile Realm

// define defines compile on p, whose internal is pc.
func (pc *pendingCompile) define(p *Object) {
	r := (*Realm)(pc)
	p.internal = nil
	p.flags &^= flagHasLazy
	p.addNamed(r, compileKey, compileCell(r))
}

// onlyCompilePending reports whether o, which has flagDict or flagHasLazy,
// is a shape-mode %RegExp.prototype% with only compile pending: a lookup of
// any other key may read its shape.
func (o *Object) onlyCompilePending() bool {
	_, ok := o.internal.(*pendingCompile)
	return ok && o.flags&flagDict == 0
}
