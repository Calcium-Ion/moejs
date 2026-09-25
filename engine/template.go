package engine

import "github.com/Calcium-Ion/moejs/bytecode"

// Tagged-template objects (GetTemplateObject). Each GetTemplate site owns one
// inline-cache slot; the realm's template object for the site is created on
// the site's first evaluation and the slot remembers it (loc = 1 + index
// into realmLazy.templates), so repeated evaluations return the same object
// and realms or code without tagged templates pay nothing.

// templateObject returns the template object of the ConstTemplate k for the
// site whose inline-cache slot is e.
func (r *Realm) templateObject(k *bytecode.Const, e *ICEntry) *Object {
	if e.loc != 0 {
		return r.lazy.templates[e.loc-1]
	}
	n := len(k.Cooked)
	vals := make([]Value, 2*n)
	cooked, raw := vals[:n:n], vals[n:]
	for i, s := range k.Cooked {
		if s == bytecode.UndefinedCooked {
			cooked[i] = Undefined()
		} else {
			cooked[i] = StringValue(fromWTF8(s))
		}
		raw[i] = StringValue(FromGoString(k.Raw[i]))
	}
	rawObj := r.NewArrayFromSlice(raw)
	rawObj.Freeze(r)
	o := r.NewArrayFromSlice(cooked)
	o.shape = o.shape.addProperty(r, StringKey(AtomRaw), 0)
	o.slots = append(o.slots, ObjectValue(rawObj))
	o.Freeze(r)
	l := r.lazyState()
	l.templates = append(l.templates, o)
	e.loc = uint32(len(l.templates))
	return o
}

// fromWTF8 converts a compiler string constant (WTF-8: lone surrogates are
// encoded as three-byte sequences) to a String.
func fromWTF8(s string) *String {
	if isASCII(s) {
		return FromGoString(s)
	}
	return FromUTF16(appendWTF8Units(nil, s))
}
