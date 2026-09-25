package engine

// PropertyDescriptor is the spec's Property Descriptor record. Absent fields
// are tracked in present; attribute values live in attrs.
type PropertyDescriptor struct {
	Value   Value
	Get     Value // getter or undefined
	Set     Value // setter or undefined
	present uint8
	attrs   uint8
}

const (
	descHasValue uint8 = 1 << iota
	descHasGet
	descHasSet
	descHasWritable
	descHasEnumerable
	descHasConfigurable
)

// DataDescriptor builds a complete data descriptor.
func DataDescriptor(v Value, attrs uint8) PropertyDescriptor {
	return PropertyDescriptor{
		Value:   v,
		Get:     Undefined(),
		Set:     Undefined(),
		present: descHasValue | descHasWritable | descHasEnumerable | descHasConfigurable,
		attrs:   attrs &^ attrAccessor,
	}
}

// AccessorDescriptor builds a complete accessor descriptor; get/set may be nil.
func AccessorDescriptor(get, set *Object, attrs uint8) PropertyDescriptor {
	d := PropertyDescriptor{
		Get:     Undefined(),
		Set:     Undefined(),
		present: descHasGet | descHasSet | descHasEnumerable | descHasConfigurable,
		attrs:   (attrs &^ attrWritable) | attrAccessor,
	}
	if get != nil {
		d.Get = ObjectValue(get)
	}
	if set != nil {
		d.Set = ObjectValue(set)
	}
	return d
}

// SetValue sets [[Value]].
func (d *PropertyDescriptor) SetValue(v Value) { d.Value = v; d.present |= descHasValue }

// SetGet sets [[Get]] (object or undefined).
func (d *PropertyDescriptor) SetGet(v Value) { d.Get = v; d.present |= descHasGet }

// SetSet sets [[Set]] (object or undefined).
func (d *PropertyDescriptor) SetSet(v Value) { d.Set = v; d.present |= descHasSet }

// SetWritable sets [[Writable]].
func (d *PropertyDescriptor) SetWritable(b bool) { d.setFlag(descHasWritable, attrWritable, b) }

// SetEnumerable sets [[Enumerable]].
func (d *PropertyDescriptor) SetEnumerable(b bool) { d.setFlag(descHasEnumerable, attrEnumerable, b) }

// SetConfigurable sets [[Configurable]].
func (d *PropertyDescriptor) SetConfigurable(b bool) {
	d.setFlag(descHasConfigurable, attrConfigurable, b)
}

func (d *PropertyDescriptor) setFlag(has, bit uint8, b bool) {
	d.present |= has
	if b {
		d.attrs |= bit
	} else {
		d.attrs &^= bit
	}
}

// HasValue reports whether [[Value]] is present.
func (d PropertyDescriptor) HasValue() bool { return d.present&descHasValue != 0 }

// HasGet reports whether [[Get]] is present.
func (d PropertyDescriptor) HasGet() bool { return d.present&descHasGet != 0 }

// HasSet reports whether [[Set]] is present.
func (d PropertyDescriptor) HasSet() bool { return d.present&descHasSet != 0 }

// HasWritable reports whether [[Writable]] is present.
func (d PropertyDescriptor) HasWritable() bool { return d.present&descHasWritable != 0 }

// HasEnumerable reports whether [[Enumerable]] is present.
func (d PropertyDescriptor) HasEnumerable() bool { return d.present&descHasEnumerable != 0 }

// HasConfigurable reports whether [[Configurable]] is present.
func (d PropertyDescriptor) HasConfigurable() bool { return d.present&descHasConfigurable != 0 }

// Writable returns [[Writable]] (false when absent).
func (d PropertyDescriptor) Writable() bool { return d.attrs&attrWritable != 0 }

// Enumerable returns [[Enumerable]] (false when absent).
func (d PropertyDescriptor) Enumerable() bool { return d.attrs&attrEnumerable != 0 }

// Configurable returns [[Configurable]] (false when absent).
func (d PropertyDescriptor) Configurable() bool { return d.attrs&attrConfigurable != 0 }

// IsAccessorDescriptor reports whether [[Get]] or [[Set]] is present.
func (d PropertyDescriptor) IsAccessorDescriptor() bool {
	return d.present&(descHasGet|descHasSet) != 0
}

// IsDataDescriptor reports whether [[Value]] or [[Writable]] is present.
func (d PropertyDescriptor) IsDataDescriptor() bool {
	return d.present&(descHasValue|descHasWritable) != 0
}

// IsGenericDescriptor reports whether the descriptor is neither data nor accessor.
func (d PropertyDescriptor) IsGenericDescriptor() bool {
	return d.present&(descHasGet|descHasSet|descHasValue|descHasWritable) == 0
}

// Attrs returns the attribute bits (attrAccessor set for accessor descriptors).
func (d PropertyDescriptor) Attrs() uint8 { return d.attrs }

// GetterObject returns the getter or nil.
func (d PropertyDescriptor) GetterObject() *Object {
	if d.Get.IsObject() {
		return d.Get.AsObject()
	}
	return nil
}

// SetterObject returns the setter or nil.
func (d PropertyDescriptor) SetterObject() *Object {
	if d.Set.IsObject() {
		return d.Set.AsObject()
	}
	return nil
}

// CheckAccessorFunction returns the TypeError ToPropertyDescriptor throws
// for a getter (or, when setter is set, a setter) that is neither callable
// nor undefined, and nil otherwise. Strings print unquoted.
func (r *Realm) CheckAccessorFunction(v Value, setter bool) error {
	if IsCallable(v) || v.IsUndefined() {
		return nil
	}
	kind, display := "Getter", r.DisplayString(v)
	if setter {
		kind = "Setter"
	}
	if v.IsString() {
		display = v.AsString().GoString()
	}
	return r.TypeError("%s must be a function: %s", kind, display)
}

// descriptorFromCell converts stored property data to a complete descriptor.
func descriptorFromCell(c propCell) PropertyDescriptor {
	if c.attrs&attrAccessor != 0 {
		a := c.value.asAccessor()
		return AccessorDescriptor(a.Get, a.Set, c.attrs)
	}
	return DataDescriptor(c.value, c.attrs)
}

// cellFromDescriptor converts a complete descriptor to storage form.
func cellFromDescriptor(d PropertyDescriptor) propCell {
	if d.IsAccessorDescriptor() {
		return propCell{
			value: accessorValue(&Accessor{Get: d.GetterObject(), Set: d.SetterObject()}),
			attrs: (d.attrs &^ attrWritable) | attrAccessor,
		}
	}
	return propCell{value: d.Value, attrs: d.attrs &^ attrAccessor}
}

// ValidateAndApplyPropertyDescriptor implements the spec operation for an
// ordinary property. current is nil when the property does not exist. When o
// is nil the operation only validates. It returns false when the change is
// not allowed.
func ValidateAndApplyPropertyDescriptor(r *Realm, o *Object, key PropertyKey, extensible bool, desc PropertyDescriptor, current *PropertyDescriptor) bool {
	if current == nil {
		if !extensible {
			return false
		}
		if o == nil {
			return true
		}
		cell := propCell{attrs: desc.attrs & (attrWritable | attrEnumerable | attrConfigurable)}
		if desc.IsAccessorDescriptor() {
			cell.attrs = (cell.attrs &^ attrWritable) | attrAccessor
			cell.value = accessorValue(&Accessor{Get: desc.GetterObject(), Set: desc.SetterObject()})
		} else {
			cell.value = desc.Value
			if !desc.HasValue() {
				cell.value = Undefined()
			}
		}
		o.addProp(r, key, cell)
		return true
	}
	if desc.present == 0 {
		return true
	}
	if !current.Configurable() {
		if desc.HasConfigurable() && desc.Configurable() {
			return false
		}
		if desc.HasEnumerable() && desc.Enumerable() != current.Enumerable() {
			return false
		}
		if !desc.IsGenericDescriptor() && desc.IsAccessorDescriptor() != current.IsAccessorDescriptor() {
			return false
		}
		if current.IsAccessorDescriptor() {
			if desc.HasGet() && !SameValue(desc.Get, current.Get) {
				return false
			}
			if desc.HasSet() && !SameValue(desc.Set, current.Set) {
				return false
			}
		} else if !current.Writable() {
			if desc.HasWritable() && desc.Writable() {
				return false
			}
			if desc.HasValue() && !SameValue(desc.Value, current.Value) {
				return false
			}
		}
	}
	if o == nil {
		return true
	}
	// Merge into a complete descriptor.
	merged := *current
	if desc.IsAccessorDescriptor() && !current.IsAccessorDescriptor() {
		merged = AccessorDescriptor(nil, nil, current.attrs&(attrEnumerable|attrConfigurable))
	} else if desc.IsDataDescriptor() && current.IsAccessorDescriptor() {
		merged = DataDescriptor(Undefined(), current.attrs&(attrEnumerable|attrConfigurable))
	}
	if desc.HasValue() {
		merged.Value = desc.Value
	}
	if desc.HasGet() {
		merged.Get = desc.Get
	}
	if desc.HasSet() {
		merged.Set = desc.Set
	}
	for _, f := range [...]struct{ has, bit uint8 }{
		{descHasWritable, attrWritable}, {descHasEnumerable, attrEnumerable}, {descHasConfigurable, attrConfigurable},
	} {
		if desc.present&f.has != 0 {
			merged.attrs = (merged.attrs &^ f.bit) | (desc.attrs & f.bit)
		}
	}
	o.replaceProp(r, key, cellFromDescriptor(merged))
	return true
}
