package engine

import (
	"math"
	"strconv"
	"strings"
)

// JSONOptions are the options of AppendJSONWith.
type JSONOptions struct {
	// Strict makes a value that JSON.stringify writes otherwise or drops
	// an error, a *LossyJSONError, rather than the standard result: an
	// undefined, function or symbol value, which an object leaves out and
	// an array writes as null; NaN, Infinity and -Infinity, written as
	// null; and an object whose data JSON does not see, which it writes as
	// {} or with its own enumerable properties only: a Map, Set, WeakMap,
	// WeakSet, WeakRef, Promise, RegExp, Error (of any kind, subclasses
	// included), ArrayBuffer, SharedArrayBuffer or DataView. The value
	// itself counts too (AppendJSON writes null for it), and the check
	// applies to what toJSON and the replacer return, so a toJSON that
	// returns undefined fails and a Map with a toJSON method does not. -0
	// is written as 0, which equals it, as without Strict.
	Strict bool
}

// LossyJSONError is the error of a strict AppendJSONWith: the value at
// Path has no JSON text of its own.
type LossyJSONError struct {
	// Path is where the value is: $ for the value itself, then .name or
	// ["name"] for an object's property and [i] for an array's element, as
	// in $.messages[3].content.
	Path string
	// Value names the value: "undefined", "a function", "a symbol", "NaN",
	// "Infinity", "-Infinity", or for an object whose data JSON does not
	// see "a Map", "a Set", "a WeakMap", "a WeakSet", "a WeakRef", "a
	// Promise", "a RegExp", "an Error", "an ArrayBuffer", "a
	// SharedArrayBuffer" or "a DataView".
	Value string
	// Omitted reports that JSON.stringify would leave the property out (an
	// object's undefined, function or symbol); otherwise it writes null,
	// or for an Object loss an object.
	Omitted bool
	// Object reports that the value is an object whose data JSON does not
	// see: JSON.stringify writes it as {}, or with its own enumerable
	// properties only.
	Object bool
}

func (e *LossyJSONError) Error() string {
	switch {
	case e.Omitted:
		return "moejs: " + e.Path + " is " + e.Value + ", which JSON leaves out"
	case e.Object:
		return "moejs: " + e.Path + " is " + e.Value + ", whose contents JSON does not write"
	}
	return "moejs: " + e.Path + " is " + e.Value + ", which JSON writes as null"
}

// jsonLoss is a strict stringify's error on its way out of the walk: each
// container it leaves adds the property or element it was in (in, at),
// innermost first, and AppendJSONWith exports it.
type jsonLoss struct {
	what    string
	number  bool // a number that is not finite, which is null anywhere
	object  bool // an object whose data JSON does not see (jsonOpaque)
	omitted bool
	path    []string // the segments, innermost first
}

func (e *jsonLoss) Error() string { return e.export().Error() }

// newJSONLoss returns the loss of v, a value with no JSON text: undefined,
// a symbol, a callable object or a number that is not finite.
func newJSONLoss(v Value) *jsonLoss {
	switch {
	case v.IsUndefined():
		return &jsonLoss{what: "undefined"}
	case v.IsSymbol():
		return &jsonLoss{what: "a symbol"}
	case v.IsNumber():
		f := v.AsNumber()
		switch {
		case math.IsInf(f, 1):
			return &jsonLoss{what: "Infinity", number: true}
		case math.IsInf(f, -1):
			return &jsonLoss{what: "-Infinity", number: true}
		}
		return &jsonLoss{what: "NaN", number: true}
	}
	return &jsonLoss{what: "a function"}
}

// jsonOpaque returns the loss of o, an object a strict stringify is about
// to write through the object path, when JSON does not see its data: what
// its class keeps in internal slots (a Map's entries, a RegExp's pattern,
// an Error's message, a buffer's bytes). nil for the other objects.
func jsonOpaque(o *Object) *jsonLoss {
	var what string
	switch o.class {
	case ClassMap:
		what = "a Map"
	case ClassSet:
		what = "a Set"
	case ClassWeakMap:
		what = "a WeakMap"
	case ClassWeakSet:
		what = "a WeakSet"
	case ClassWeakRef:
		what = "a WeakRef"
	case ClassPromise:
		what = "a Promise"
	case ClassRegExp:
		what = "a RegExp"
	case ClassError:
		what = "an Error"
	case ClassArrayBuffer:
		what = "an ArrayBuffer"
	case ClassSharedArrayBuffer:
		what = "a SharedArrayBuffer"
	case ClassDataView:
		what = "a DataView"
	default:
		return nil
	}
	return &jsonLoss{what: what, object: true}
}

// in adds the property key of the object the loss is in: a property left
// out when the loss is its value's.
func (e *jsonLoss) in(key PropertyKey) *jsonLoss {
	if e.path == nil && !e.number && !e.object {
		e.omitted = true
	}
	name := key.GoString()
	if isPathName(name) {
		e.path = append(e.path, "."+name)
	} else {
		e.path = append(e.path, "["+strconv.Quote(name)+"]")
	}
	return e
}

// at adds the index of the array element the loss is in.
func (e *jsonLoss) at(i int64) *jsonLoss {
	e.path = append(e.path, "["+strconv.FormatInt(i, 10)+"]")
	return e
}

// jsonLossIn is err leaving the property key of an object: in when err is
// a loss.
func jsonLossIn(err error, key PropertyKey) error {
	if e, ok := err.(*jsonLoss); ok {
		return e.in(key)
	}
	return err
}

// jsonLossAt is err leaving the element i of an array: at when err is a
// loss.
func jsonLossAt(err error, i int64) error {
	if e, ok := err.(*jsonLoss); ok {
		return e.at(i)
	}
	return err
}

// export returns the LossyJSONError of e.
func (e *jsonLoss) export() *LossyJSONError {
	var b strings.Builder
	b.WriteByte('$')
	for i := len(e.path) - 1; i >= 0; i-- {
		b.WriteString(e.path[i])
	}
	return &LossyJSONError{Path: b.String(), Value: e.what, Omitted: e.omitted, Object: e.object}
}

// isPathName reports whether a property name reads as .name in a path:
// ASCII letters, digits, _ and $, not starting with a digit.
func isPathName(s string) bool {
	if s == "" || s[0] >= '0' && s[0] <= '9' {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '$') {
			return false
		}
	}
	return true
}
