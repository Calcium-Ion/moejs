package engine

import "slices"

// propCell is a property's storage: a data value or an accessorValue when
// attrs has attrAccessor.
type propCell struct {
	value Value
	attrs uint8
}

// Accessor is the getter/setter pair of an accessor property. Either may be
// nil (undefined).
type Accessor struct {
	Get *Object
	Set *Object
}

// dictEntry is one insertion-ordered named property in dictionary mode.
type dictEntry struct {
	key  PropertyKey
	cell propCell
	live bool
}

// dictProps backs objects in dictionary mode (named properties) and/or with
// sparse indexed properties, and holds the weak-collection entries of an
// object used as a WeakMap/WeakSet key.
type dictProps struct {
	index   map[PropertyKey]int32 // named key -> position in entries
	entries []dictEntry           // insertion order with tombstones
	dead    int
	sparse  map[uint32]propCell // indexed properties outside dense storage
	weak    *weakSide           // entries of the weak collections keyed by the object (builtin_weak.go)
}

func (d *dictProps) lookup(key PropertyKey) (*propCell, bool) {
	i, ok := d.index[key]
	if !ok {
		return nil, false
	}
	return &d.entries[i].cell, true
}

func (d *dictProps) add(key PropertyKey, cell propCell) {
	if d.index == nil {
		d.index = make(map[PropertyKey]int32, 8)
	}
	d.index[key] = int32(len(d.entries))
	d.entries = append(d.entries, dictEntry{key: key, cell: cell, live: true})
}

func (d *dictProps) remove(key PropertyKey) bool {
	i, ok := d.index[key]
	if !ok {
		return false
	}
	delete(d.index, key)
	d.entries[i] = dictEntry{}
	d.dead++
	if d.dead > 8 && d.dead > len(d.entries)/2 {
		d.compact()
	}
	return true
}

func (d *dictProps) compact() {
	live := d.entries[:0]
	for _, e := range d.entries {
		if e.live {
			live = append(live, e)
		}
	}
	clear(d.entries[len(live):])
	d.entries = live
	d.dead = 0
	for i := range d.entries {
		d.index[d.entries[i].key] = int32(i)
	}
}

// namedCount returns the number of live named properties.
func (d *dictProps) namedCount() int { return len(d.index) }

// sparseKeys returns the sparse indices in ascending order.
func (d *dictProps) sparseKeys() []uint32 {
	if len(d.sparse) == 0 {
		return nil
	}
	keys := make([]uint32, 0, len(d.sparse))
	for k := range d.sparse {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
