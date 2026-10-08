package engine

// The ES2015+ String members: String.fromCodePoint, String.raw,
// isWellFormed/toWellFormed, the toLocale*Case pair and the Annex B
// trimLeft/trimRight aliases.

// stringAliases pairs each Annex B alias with the method it names.
var stringAliases = [...][2]*String{{AtomTrimLeft, AtomTrimStart}, {AtomTrimRight, AtomTrimEnd}}

const stringAliasCount = len(stringAliases)

// installStringAliases defines trimLeft and trimRight as the trimStart and
// trimEnd function objects themselves (Annex B.2.2.15-16), so their names
// stay "trimStart"/"trimEnd".
func installStringAliases(r *Realm) {
	proto := r.StringPrototype
	proto.ReserveSlots(r, stringAliasCount)
	for _, a := range stringAliases {
		r.installOrReplace(proto, StringKey(a[0]), propCell{value: bootstrapOwnValue(proto, StringKey(a[1])), attrs: attrHidden})
	}
}

// bootstrapOwnValue reads an own data property during intrinsic setup. Like
// installOrReplace it walks the shape chain: GetOwnDataValue would build the
// lookup table of a large prototype's shape in every mutable realm.
func bootstrapOwnValue(o *Object, key PropertyKey) Value {
	if o.flags&flagDict == 0 {
		for n := o.shape; n.count != 0; n = n.parent {
			if n.key == key {
				return o.slots[n.slot]
			}
		}
	}
	v, _ := o.GetOwnDataValue(key)
	return v
}

// stringFromCodePoint implements String.fromCodePoint(...codePoints).
func stringFromCodePoint(r *Realm, this Value, args []Value) (Value, error) {
	var sb StringBuilder
	for i, a := range args {
		f, err := r.ToNumber(a)
		if err != nil {
			return Undefined(), err
		}
		if !isIntegralNumber(f) || f < 0 || f > 0x10FFFF {
			return Undefined(), r.RangeError("Invalid code point %s", NumberToGoString(f))
		}
		sb.WriteRune(rune(f))
		if err := interruptEvery(r, int64(i)); err != nil {
			return Undefined(), err
		}
	}
	if err := sb.checkLength(r); err != nil {
		return Undefined(), err
	}
	return StringValue(r.builtString(&sb)), nil
}

// stringRaw implements String.raw(template, ...substitutions).
func stringRaw(r *Realm, this Value, args []Value) (Value, error) {
	cooked, err := r.ToObject(Arg(args, 0))
	if err != nil {
		return Undefined(), err
	}
	rawVal, err := cooked.Get(r, StringKey(AtomRaw), ObjectValue(cooked))
	if err != nil {
		return Undefined(), err
	}
	literals, err := r.ToObject(rawVal)
	if err != nil {
		return Undefined(), err
	}
	n, err := r.LengthOfArrayLike(literals)
	if err != nil {
		return Undefined(), err
	}
	if n <= 0 {
		return StringValue(emptyString), nil
	}
	subs := args[min(len(args), 1):]
	var sb StringBuilder
	for k := int64(0); ; k++ {
		v, err := getIndex(r, literals, k)
		if err != nil {
			return Undefined(), err
		}
		s, err := r.ToString(v)
		if err != nil {
			return Undefined(), err
		}
		sb.WriteString(s)
		if err := sb.checkLength(r); err != nil {
			return Undefined(), err
		}
		if k+1 == n {
			return StringValue(r.builtString(&sb)), nil
		}
		if k < int64(len(subs)) {
			s, err := r.ToString(subs[k])
			if err != nil {
				return Undefined(), err
			}
			sb.WriteString(s)
			if err := sb.checkLength(r); err != nil {
				return Undefined(), err
			}
		}
		if err := interruptEvery(r, k); err != nil {
			return Undefined(), err
		}
	}
}

func stringProtoIsWellFormed(r *Realm, this Value, args []Value) (Value, error) {
	s, err := thisStringValue(r, this, "isWellFormed")
	if err != nil {
		return Undefined(), err
	}
	return Bool(s.IsWellFormed()), nil
}

// stringProtoToWellFormed replaces every lone surrogate with U+FFFD; a
// well-formed string is returned as is.
func stringProtoToWellFormed(r *Realm, this Value, args []Value) (Value, error) {
	s, err := thisStringValue(r, this, "toWellFormed")
	if err != nil {
		return Undefined(), err
	}
	if s.IsWellFormed() {
		return StringValue(s), nil
	}
	u := append([]uint16(nil), s.UTF16()...) // UTF16, not units: s may be a rope
	for i := 0; i < len(u); i++ {
		c := u[i]
		switch {
		case c < 0xD800 || c >= 0xE000:
		case c < 0xDC00 && i+1 < len(u) && u[i+1] >= 0xDC00 && u[i+1] < 0xE000:
			i++
		default:
			u[i] = 0xFFFD
		}
	}
	return StringValue(FromUTF16(u)), nil
}

// The toLocale*Case pair ignores its locales argument: without Intl the
// host locale is en-US, whose mappings are the non-locale ones (no Turkish
// dotless i, no Lithuanian dot retention).

func stringProtoToLocaleLowerCase(r *Realm, this Value, args []Value) (Value, error) {
	s, err := thisStringValue(r, this, "toLocaleLowerCase")
	if err != nil {
		return Undefined(), err
	}
	out, err := stringToLower(r, s)
	return caseConverted(r, out, err)
}

func stringProtoToLocaleUpperCase(r *Realm, this Value, args []Value) (Value, error) {
	s, err := thisStringValue(r, this, "toLocaleUpperCase")
	if err != nil {
		return Undefined(), err
	}
	out, err := stringToUpper(r, s)
	return caseConverted(r, out, err)
}
