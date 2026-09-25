package engine

// The Annex B.2.2 HTML methods (String.prototype.anchor and friends): each
// wraps ToString(this) in a tag, optionally with one attribute whose value
// has its double quotes escaped as &quot;.

// createHTML implements the CreateHTML abstract operation; attr == "" means
// no attribute (and value is not coerced).
func createHTML(r *Realm, this Value, method, tag, attr string, value Value) (Value, error) {
	s, err := thisStringValue(r, this, method)
	if err != nil {
		return Undefined(), err
	}
	var sb StringBuilder
	sb.WriteASCII('<')
	sb.WriteGoString(tag)
	if attr != "" {
		v, err := r.ToString(value)
		if err != nil {
			return Undefined(), err
		}
		sb.WriteASCII(' ')
		sb.WriteGoString(attr)
		sb.WriteGoString(`="`)
		for i, n := 0, v.Len(); i < n; i++ {
			if c := v.At(i); c == '"' {
				sb.WriteGoString("&quot;")
			} else {
				sb.WriteUnit(c)
			}
		}
		sb.WriteASCII('"')
	}
	sb.WriteASCII('>')
	sb.WriteString(s)
	sb.WriteGoString("</")
	sb.WriteGoString(tag)
	sb.WriteASCII('>')
	if err := sb.checkLength(r); err != nil {
		return Undefined(), err
	}
	return StringValue(sb.String()), nil
}

func stringProtoAnchor(r *Realm, this Value, args []Value) (Value, error) {
	return createHTML(r, this, "anchor", "a", "name", Arg(args, 0))
}

func stringProtoBig(r *Realm, this Value, args []Value) (Value, error) {
	return createHTML(r, this, "big", "big", "", Undefined())
}

func stringProtoBlink(r *Realm, this Value, args []Value) (Value, error) {
	return createHTML(r, this, "blink", "blink", "", Undefined())
}

func stringProtoBold(r *Realm, this Value, args []Value) (Value, error) {
	return createHTML(r, this, "bold", "b", "", Undefined())
}

func stringProtoFixed(r *Realm, this Value, args []Value) (Value, error) {
	return createHTML(r, this, "fixed", "tt", "", Undefined())
}

func stringProtoFontcolor(r *Realm, this Value, args []Value) (Value, error) {
	return createHTML(r, this, "fontcolor", "font", "color", Arg(args, 0))
}

func stringProtoFontsize(r *Realm, this Value, args []Value) (Value, error) {
	return createHTML(r, this, "fontsize", "font", "size", Arg(args, 0))
}

func stringProtoItalics(r *Realm, this Value, args []Value) (Value, error) {
	return createHTML(r, this, "italics", "i", "", Undefined())
}

func stringProtoLink(r *Realm, this Value, args []Value) (Value, error) {
	return createHTML(r, this, "link", "a", "href", Arg(args, 0))
}

func stringProtoSmall(r *Realm, this Value, args []Value) (Value, error) {
	return createHTML(r, this, "small", "small", "", Undefined())
}

func stringProtoStrike(r *Realm, this Value, args []Value) (Value, error) {
	return createHTML(r, this, "strike", "strike", "", Undefined())
}

func stringProtoSub(r *Realm, this Value, args []Value) (Value, error) {
	return createHTML(r, this, "sub", "sub", "", Undefined())
}

func stringProtoSup(r *Realm, this Value, args []Value) (Value, error) {
	return createHTML(r, this, "sup", "sup", "", Undefined())
}
