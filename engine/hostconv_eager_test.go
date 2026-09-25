package engine

import (
	"encoding/json"
	"fmt"
)

// This file keeps an eager FromGo as the reference the lazy
// conversion (hostlazy.go) is checked against: TestHostLazyDifferential
// converts random Go trees both ways and compares every observable result.

// eagerMaxDepth bounds recursion in the eager conversion.
const eagerMaxDepth = 128

// eagerFromGo converts eagerly: a pre-walk records every map's entries
// in build order and counts the nodes, then the build walk carves the whole
// tree out of one slab per kind.
func eagerFromGo(r *Realm, v any) (Value, error) {
	st := new(eagerState)
	st.r = r
	st.plan(v, 0)
	st.arena.init(&st.counts)
	res, err := st.value(v, 0)
	st.finish()
	return res, err
}

// eagerState is the scratch of one eager conversion: the pre-walk output,
// the slab counts and the slabs.
type eagerState struct {
	r      *Realm
	counts eagerCounts
	arena  eagerArena

	// Every map seen by the pre-walk, entries in build order (orderPairs),
	// in the order the build walk visits maps; xxxPos is the build cursor.
	// Kept between conversions (cleared of host data) so steady-state hooks
	// allocate no scratch.
	anyPairs  []hostPair[any]
	strPairs  []hostPair[string]
	listPairs []hostPair[[]string]
	anyPos    int
	strPos    int
	listPos   int
}

// eagerCounts sizes the slabs. They are an upper bound on what the build
// uses: index keys are counted as slots but stored as elements and maps
// beyond maxShapeProps switch to dictionary storage; when a slab is short
// the carve functions fall back to the heap, so a miscount is never unsafe.
type eagerCounts struct {
	objects, arrays, values, strings int
}

// eagerArena is the slab storage of one conversion.
type eagerArena struct {
	objects []Object
	arrays  []arrayObject
	values  []Value
	strings []String
}

// finish drops the conversion's slabs (the result owns them now) and the
// recorded host data, keeping the pair capacity for the next call.
func (st *eagerState) finish() {
	st.arena = eagerArena{}
	st.counts = eagerCounts{}
	clear(st.anyPairs)
	clear(st.strPairs)
	clear(st.listPairs)
	st.anyPairs = trimScratch(st.anyPairs)
	st.strPairs = trimScratch(st.strPairs)
	st.listPairs = trimScratch(st.listPairs)
	st.anyPos, st.strPos, st.listPos = 0, 0, 0
}

// --- pre-walk -------------------------------------------------------------------

// plan mirrors value's type switch: it records map entries and tallies the
// slabs. Types the switch does not know (host hook types) allocate through
// their own paths and are not counted.
func (st *eagerState) plan(v any, depth int) {
	if depth > eagerMaxDepth {
		return
	}
	switch x := v.(type) {
	case string:
		st.countString(x)
	case map[string]any:
		st.planAnyMap(x, depth)
	case map[string]string:
		if x == nil {
			return
		}
		st.counts.objects++
		st.counts.values += len(x)
		start := len(st.strPairs)
		for k, s := range x {
			st.strPairs = append(st.strPairs, hostPair[string]{k, s})
			st.countString(s)
		}
		orderPairs(st.strPairs[start:])
	case map[string][]string:
		if x == nil {
			return
		}
		st.counts.objects++
		st.counts.values += len(x)
		start := len(st.listPairs)
		for k, list := range x {
			st.listPairs = append(st.listPairs, hostPair[[]string]{k, list})
			st.countStringSlice(list)
		}
		orderPairs(st.listPairs[start:])
	case []any:
		st.counts.arrays++
		st.counts.values += len(x)
		for _, e := range x {
			st.plan(e, depth+1)
		}
	case []string:
		st.countStringSlice(x)
	case []map[string]any:
		st.counts.arrays++
		st.counts.values += len(x)
		for _, m := range x {
			st.planAnyMap(m, depth+1)
		}
	}
}

// planAnyMap records a map[string]any (the common case); nested values are
// planned in build order, which is the order the build walk converts them.
func (st *eagerState) planAnyMap(m map[string]any, depth int) {
	if m == nil {
		return
	}
	st.counts.objects++
	st.counts.values += len(m)
	if st.anyPairs == nil {
		st.anyPairs = make([]hostPair[any], 0, initialPairs)
	}
	start := len(st.anyPairs)
	for k, e := range m {
		st.anyPairs = append(st.anyPairs, hostPair[any]{k, e})
	}
	seg := st.anyPairs[start:]
	orderPairs(seg)
	// seg stays valid if a nested map grows st.anyPairs: the ordered copy
	// travels with the reallocation and the build reads the new slice.
	for i := range seg {
		st.plan(seg[i].v, depth+1)
	}
}

func (st *eagerState) countString(s string) {
	if len(s) != 0 {
		st.counts.strings++
	}
}

func (st *eagerState) countStringSlice(list []string) {
	st.counts.arrays++
	st.counts.values += len(list)
	for _, s := range list {
		st.countString(s)
	}
}

// --- slabs ------------------------------------------------------------------------

func (a *eagerArena) init(c *eagerCounts) {
	if c.objects > 0 {
		a.objects = make([]Object, 0, c.objects)
	}
	if c.arrays > 0 {
		a.arrays = make([]arrayObject, 0, c.arrays)
	}
	if c.values > 0 {
		a.values = make([]Value, 0, c.values)
	}
	if c.strings > 0 {
		a.strings = make([]String, 0, c.strings)
	}
}

// object returns a zeroed Object from the slab (or the heap when exhausted).
func (a *eagerArena) object() *Object {
	if n := len(a.objects); n < cap(a.objects) {
		a.objects = a.objects[:n+1]
		return &a.objects[n]
	}
	return new(Object)
}

func (a *eagerArena) array() *arrayObject {
	if n := len(a.arrays); n < cap(a.arrays) {
		a.arrays = a.arrays[:n+1]
		return &a.arrays[n]
	}
	return new(arrayObject)
}

// valueSlice returns a full-length slice of n zero values.
func (a *eagerArena) valueSlice(n int) []Value {
	if start := len(a.values); start+n <= cap(a.values) {
		a.values = a.values[:start+n]
		return a.values[start : start+n : start+n]
	}
	return make([]Value, n)
}

// str is FromGoString with the String header carved from the slab.
func (a *eagerArena) str(g string) *String {
	if len(g) == 0 {
		return emptyString
	}
	var s *String
	if n := len(a.strings); n < cap(a.strings) {
		a.strings = a.strings[:n+1]
		s = &a.strings[n]
	} else {
		s = new(String)
	}
	if isASCII(g) {
		*s = String{s: g, n: int32(len(g)), kind: strASCII}
		return s
	}
	u := appendUTF16(make([]uint16, 0, len(g)), g)
	*s = String{u: u, n: int32(len(u)), kind: strUTF16}
	return s
}

// --- build walk ----------------------------------------------------------------------

func (st *eagerState) value(v any, depth int) (Value, error) {
	if depth > eagerMaxDepth {
		return Undefined(), ErrFromGoDepth
	}
	r := st.r
	switch x := v.(type) {
	case nil:
		return Null(), nil
	case Value:
		return x, nil
	case bool:
		return Bool(x), nil
	case string:
		return StringValue(st.arena.str(x)), nil
	case float64:
		return NumberValue(x), nil
	case float32:
		return NumberValue(float64(x)), nil
	case int:
		return IntValue(x), nil
	case int64:
		return Int64Value(x), nil
	case int32:
		return IntValue(int(x)), nil
	case int16:
		return IntValue(int(x)), nil
	case int8:
		return IntValue(int(x)), nil
	case uint:
		return NumberValue(float64(x)), nil
	case uint64:
		return NumberValue(float64(x)), nil
	case uint32:
		return NumberValue(float64(x)), nil
	case uint16:
		return IntValue(int(x)), nil
	case uint8:
		return IntValue(int(x)), nil
	case json.Number:
		return jsonNumberValue(x)
	case map[string]any:
		return st.anyMap(x, depth)
	case map[string]string:
		if x == nil {
			return Null(), nil
		}
		seg := st.strPairs[st.strPos : st.strPos+len(x)]
		st.strPos += len(x)
		return eagerBuildMap(st, seg, depth, eagerConvString{})
	case map[string][]string:
		if x == nil {
			return Null(), nil
		}
		seg := st.listPairs[st.listPos : st.listPos+len(x)]
		st.listPos += len(x)
		return eagerBuildMap(st, seg, depth, eagerConvStringSlice{})
	case []any:
		items := st.arena.valueSlice(len(x))
		for i, e := range x {
			ev, err := st.value(e, depth+1)
			if err != nil {
				return Undefined(), err
			}
			items[i] = ev
		}
		return ObjectValue(st.newArray(items)), nil
	case []string:
		return st.stringSlice(x), nil
	case []map[string]any:
		items := st.arena.valueSlice(len(x))
		for i, m := range x {
			mv, err := st.anyMap(m, depth+1)
			if err != nil {
				return Undefined(), err
			}
			items[i] = mv
		}
		return ObjectValue(st.newArray(items)), nil
	case NativeFunc:
		return ObjectValue(r.NewNativeFunction(AtomEmpty, 0, x)), nil
	case func(*Realm, Value, []Value) (Value, error):
		return ObjectValue(r.NewNativeFunction(AtomEmpty, 0, NativeFunc(x))), nil
	case *Object:
		return ObjectValue(x), nil
	case *String:
		return StringValue(x), nil
	}
	return Undefined(), fmt.Errorf("engine: FromGo: unsupported Go type %T", v)
}

// anyMap consumes the entries planAnyMap recorded for m.
func (st *eagerState) anyMap(m map[string]any, depth int) (Value, error) {
	if m == nil {
		return Null(), nil
	}
	seg := st.anyPairs[st.anyPos : st.anyPos+len(m)]
	st.anyPos += len(m)
	return eagerBuildMap(st, seg, depth, eagerConvAny{})
}

// newArray is NewArrayFromSlice over an arena-allocated array object.
func (st *eagerState) newArray(items []Value) *Object {
	return st.r.initArray(st.arena.array(), items, uint32(len(items)))
}

func (st *eagerState) stringSlice(list []string) Value {
	items := st.arena.valueSlice(len(list))
	for i, s := range list {
		items[i] = StringValue(st.arena.str(s))
	}
	return ObjectValue(st.newArray(items))
}

// eagerValueConv converts one map value; the zero-size implementations let
// eagerBuildMap stay generic over the map's value type.
type eagerValueConv[V any] interface {
	conv(st *eagerState, v V, depth int) (Value, error)
}

type (
	eagerConvAny         struct{}
	eagerConvString      struct{}
	eagerConvStringSlice struct{}
)

func (eagerConvAny) conv(st *eagerState, v any, depth int) (Value, error) {
	return st.value(v, depth)
}

func (eagerConvString) conv(st *eagerState, v string, _ int) (Value, error) {
	return StringValue(st.arena.str(v)), nil
}

func (eagerConvStringSlice) conv(st *eagerState, v []string, _ int) (Value, error) {
	return st.stringSlice(v), nil
}

// eagerBuildMap builds the object for the entries of one map, in the order
// orderPairs left them: the named keys (sorted, laid out by a cached Shape)
// followed by the canonical array indices (stored as elements). Values are
// converted in that order, which is the order the pre-walk recorded any
// nested maps in.
func eagerBuildMap[V any, C eagerValueConv[V]](st *eagerState, entries []hostPair[V], depth int, c C) (Value, error) {
	r := st.r
	if len(entries) == 0 {
		return ObjectValue(initObject(st.arena.object(), ClassObject, r.plainRoot)), nil
	}
	named := entries
	h := uint64(14695981039346656037)
	for i := range entries {
		k := entries[i].k
		if _, ok := parseArrayIndex(k); ok {
			named = entries[:i]
			break
		}
		for j := range len(k) {
			h = (h ^ uint64(k[j])) * 1099511628211
		}
		h = (h ^ 0xFF) * 1099511628211
	}
	indexKeys := entries[len(named):]

	var o *Object
	if len(named) > maxShapeProps {
		o = initObject(st.arena.object(), ClassObject, r.plainRoot)
		for i := range named {
			ev, err := c.conv(st, named[i].v, depth+1)
			if err != nil {
				return Undefined(), err
			}
			o.addNamed(r, StringKey(r.InternGoString(named[i].k)), propCell{value: ev, attrs: attrDefault})
		}
	} else {
		shape := hostShapeFor(r, h, named).shape
		o = initObject(st.arena.object(), ClassObject, shape)
		slots := st.arena.valueSlice(len(named))
		for i := range named {
			ev, err := c.conv(st, named[i].v, depth+1)
			if err != nil {
				return Undefined(), err
			}
			slots[i] = ev
		}
		o.slots = slots
	}
	for i := range indexKeys {
		ev, err := c.conv(st, indexKeys[i].v, depth+1)
		if err != nil {
			return Undefined(), err
		}
		idx, _ := parseArrayIndex(indexKeys[i].k)
		o.addIndex(r, idx, propCell{value: ev, attrs: attrDefault})
	}
	return ObjectValue(o), nil
}
