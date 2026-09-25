package engine

import (
	"math"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

// JSON builtins. parse scans bytes (ASCII text zero-copy; a
// UTF-16 text is converted once to a WTF-8 working copy so lone surrogates
// survive) and builds objects directly into the realm's shape tree, so
// documents with the same key order share shapes with each other and with
// FromGo. stringify streams into a pre-sized StringBuilder.

var jsonMethods = []builtinDef{
	{AtomParse, jsonParse, 2},
	{AtomStringify, jsonStringify, 3},
}

func installJSON(r *Realm) {
	r.installBuiltins(r.JSON, jsonMethods)
}

// jsonMaxDepth bounds nesting in parse and stringify.
const jsonMaxDepth = 512

// --- parse -------------------------------------------------------------------------------

// JSONParse parses text with the JSON grammar and no reviver (Go-callable
// entry point). Errors are JavaScript SyntaxErrors.
func (r *Realm) JSONParse(text *String) (Value, error) {
	p := jsonParser{r: r}
	return p.parse(text)
}

func jsonParse(r *Realm, this Value, args []Value) (Value, error) {
	text, err := r.ToString(Arg(args, 0))
	if err != nil {
		return Undefined(), err
	}
	p := jsonParser{r: r}
	v, err := p.parse(text)
	if err != nil {
		return Undefined(), err
	}
	reviver := Arg(args, 1)
	if !IsCallable(reviver) {
		return v, nil
	}
	root := r.NewObject()
	root.DefineOwnDataFast(r, StringKey(AtomEmpty), v, attrDefault)
	return r.internalizeJSONProperty(root, StringKey(AtomEmpty), reviver, 0)
}

// internalizeJSONProperty implements InternalizeJSONProperty; depth counts
// the containers above holder. The parse bounds it by jsonMaxDepth, but the
// reviver can grow the structure as it is walked (a new array or a proxy per
// level), so past that the walk is a RangeError, not a Go stack overflow.
func (r *Realm) internalizeJSONProperty(holder *Object, name PropertyKey, reviver Value, depth int) (Value, error) {
	if depth > jsonMaxDepth {
		return Undefined(), r.RangeError("Maximum call stack size exceeded")
	}
	val, err := holder.Get(r, name, ObjectValue(holder))
	if err != nil {
		return Undefined(), err
	}
	if val.IsObject() {
		o := val.AsObject()
		isArr, err := r.isArray(val)
		if err != nil {
			return Undefined(), err
		}
		if isArr {
			// The length comes from a proxy's get trap as easily as from an
			// array, so it may be up to 2^53-1: walk it as an int64 with
			// canonical keys past 2^32-2 and stay interruptible.
			n, err := r.LengthOfArrayLike(o)
			if err != nil {
				return Undefined(), err
			}
			for i := int64(0); i < n; i++ {
				if err := r.reviveJSONElement(o, indexKey(r, i), reviver, depth); err != nil {
					return Undefined(), err
				}
				if err := interruptEvery(r, i); err != nil {
					return Undefined(), err
				}
			}
		} else {
			keys, err := r.enumerableOwnKeys(o)
			if err != nil {
				return Undefined(), err
			}
			for _, k := range keys {
				if err := r.reviveJSONElement(o, k, reviver, depth); err != nil {
					return Undefined(), err
				}
			}
		}
	}
	return r.Call(reviver, ObjectValue(holder), []Value{StringValue(name.ToJSString(r)), val})
}

// reviveJSONElement revives o[k] one level below depth and deletes or
// redefines it with the result.
func (r *Realm) reviveJSONElement(o *Object, k PropertyKey, reviver Value, depth int) error {
	nv, err := r.internalizeJSONProperty(o, k, reviver, depth+1)
	if err != nil {
		return err
	}
	if nv.IsUndefined() {
		_, err = r.deleteProperty(o, k)
		return err
	}
	_, err = o.CreateDataProperty(r, k, nv)
	return err
}

type jsonParser struct {
	r     *Realm
	src   string // ASCII text or WTF-8 working copy
	ascii bool
	pos   int
	depth int
	work  int
	stack []Value
	keys  []PropertyKey
}

func (p *jsonParser) parse(text *String) (Value, error) {
	if text.kind == strRope {
		text.flatten()
	}
	if text.kind == strASCII {
		p.src, p.ascii = text.s, true
	} else {
		p.src = wtf8FromUTF16(text.u)
	}
	p.skipWS()
	v, err := p.value()
	if err != nil {
		return Undefined(), err
	}
	p.skipWS()
	if p.pos < len(p.src) {
		return Undefined(), p.unexpected()
	}
	return v, nil
}

// wtf8FromUTF16 encodes code units as UTF-8, with lone surrogates encoded as
// generalized (WTF-8) three-byte sequences so they round-trip.
func wtf8FromUTF16(u []uint16) string {
	b := make([]byte, 0, len(u)+len(u)/2)
	for i := 0; i < len(u); i++ {
		c := u[i]
		switch {
		case c < 0x80:
			b = append(b, byte(c))
		case c >= 0xD800 && c < 0xE000:
			if c < 0xDC00 && i+1 < len(u) && u[i+1] >= 0xDC00 && u[i+1] < 0xE000 {
				b = utf8.AppendRune(b, utf16.DecodeRune(rune(c), rune(u[i+1])))
				i++
				continue
			}
			b = append(b, 0xE0|byte(c>>12), 0x80|byte(c>>6)&0x3F, 0x80|byte(c)&0x3F)
		default:
			b = utf8.AppendRune(b, rune(c))
		}
	}
	return bytesToString(b)
}

func (p *jsonParser) skipWS() {
	for p.pos < len(p.src) {
		switch p.src[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *jsonParser) unexpected() error {
	if p.pos >= len(p.src) {
		return p.r.SyntaxError("Unexpected end of JSON input")
	}
	tok := string(p.src[p.pos])
	if p.src[p.pos] >= 0x80 {
		rn, _ := utf8.DecodeRuneInString(p.src[p.pos:])
		tok = string(rn)
	}
	return p.r.SyntaxError("Unexpected token %s in JSON at position %d", tok, p.pos)
}

func (p *jsonParser) value() (Value, error) {
	p.work++
	if p.work&4095 == 0 {
		if err := p.r.CheckInterrupt(); err != nil {
			return Undefined(), err
		}
	}
	if p.pos >= len(p.src) {
		return Undefined(), p.unexpected()
	}
	switch c := p.src[p.pos]; c {
	case '{':
		return p.object()
	case '[':
		return p.array()
	case '"':
		s, err := p.str()
		if err != nil {
			return Undefined(), err
		}
		return StringValue(s), nil
	case 't':
		return p.literal("true", True())
	case 'f':
		return p.literal("false", False())
	case 'n':
		return p.literal("null", Null())
	default:
		if c == '-' || (c >= '0' && c <= '9') {
			return p.number()
		}
	}
	return Undefined(), p.unexpected()
}

func (p *jsonParser) literal(text string, v Value) (Value, error) {
	if len(p.src)-p.pos >= len(text) && p.src[p.pos:p.pos+len(text)] == text {
		p.pos += len(text)
		return v, nil
	}
	// Report the first mismatching character like V8.
	for i := range len(text) {
		if p.pos+i >= len(p.src) || p.src[p.pos+i] != text[i] {
			p.pos += i
			break
		}
	}
	return Undefined(), p.unexpected()
}

func (p *jsonParser) number() (Value, error) {
	start := p.pos
	neg := p.src[p.pos] == '-'
	if neg {
		p.pos++
	}
	if p.pos >= len(p.src) {
		return Undefined(), p.unexpected()
	}
	switch c := p.src[p.pos]; {
	case c == '0':
		p.pos++
	case c >= '1' && c <= '9':
		for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
			p.pos++
		}
	default:
		return Undefined(), p.unexpected()
	}
	isInt := true
	if p.pos < len(p.src) && p.src[p.pos] == '.' {
		isInt = false
		p.pos++
		if err := p.requireDigits(); err != nil {
			return Undefined(), err
		}
	}
	if p.pos < len(p.src) && (p.src[p.pos] == 'e' || p.src[p.pos] == 'E') {
		isInt = false
		p.pos++
		if p.pos < len(p.src) && (p.src[p.pos] == '+' || p.src[p.pos] == '-') {
			p.pos++
		}
		if err := p.requireDigits(); err != nil {
			return Undefined(), err
		}
	}
	text := p.src[start:p.pos]
	if isInt && len(text) <= 16 {
		digits := text
		if neg {
			digits = text[1:]
		}
		var v int64
		for i := range len(digits) {
			v = v*10 + int64(digits[i]-'0')
		}
		if neg {
			if v == 0 {
				return NumberValue(negativeZero), nil
			}
			v = -v
		}
		return Int64Value(v), nil
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil {
		// Only ErrRange is possible after validation: ±Inf or 0 is correct.
		return NumberValue(f), nil
	}
	return NumberValue(f), nil
}

func (p *jsonParser) requireDigits() error {
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
		p.pos++
	}
	if p.pos == start {
		return p.unexpected()
	}
	return nil
}

// str parses a string literal (p.pos at the opening quote).
func (p *jsonParser) str() (*String, error) {
	start := p.pos + 1
	i := start
	src := p.src
	nonASCII := false
	for i < len(src) {
		c := src[i]
		if c == '"' {
			if !nonASCII {
				p.pos = i + 1
				return asciiString(src[start:i]), nil
			}
			p.pos = i + 1
			return FromUTF16(appendWTF8Units(nil, src[start:i])), nil
		}
		if c == '\\' {
			return p.strSlow(start, i)
		}
		if c < 0x20 {
			p.pos = i
			return nil, p.r.SyntaxError("Bad control character in string literal in JSON at position %d", i)
		}
		if c >= 0x80 {
			nonASCII = true
		}
		i++
	}
	p.pos = len(src)
	return nil, p.r.SyntaxError("Unterminated string in JSON at position %d", len(src))
}

// strSlow continues a string literal from the first escape at i.
func (p *jsonParser) strSlow(start, i int) (*String, error) {
	src := p.src
	var sb StringBuilder
	sb.Grow(i - start + 16)
	units := appendWTF8Units(nil, src[start:i])
	sb.WriteUTF16(units)
	runStart := i
	for i < len(src) {
		c := src[i]
		switch {
		case c == '"':
			sb.WriteUTF16(appendWTF8Units(nil, src[runStart:i]))
			p.pos = i + 1
			return sb.String(), nil
		case c == '\\':
			sb.WriteUTF16(appendWTF8Units(nil, src[runStart:i]))
			i++
			if i >= len(src) {
				p.pos = i
				return nil, p.unexpected()
			}
			switch src[i] {
			case '"':
				sb.WriteASCII('"')
			case '\\':
				sb.WriteASCII('\\')
			case '/':
				sb.WriteASCII('/')
			case 'b':
				sb.WriteASCII('\b')
			case 'f':
				sb.WriteASCII('\f')
			case 'n':
				sb.WriteASCII('\n')
			case 'r':
				sb.WriteASCII('\r')
			case 't':
				sb.WriteASCII('\t')
			case 'u':
				if i+4 >= len(src) {
					p.pos = len(src)
					return nil, p.unexpected()
				}
				var v uint16
				for k := 1; k <= 4; k++ {
					d := digitValue(src[i+k])
					if d < 0 || d >= 16 {
						p.pos = i + k
						return nil, p.r.SyntaxError("Bad Unicode escape in JSON at position %d", p.pos)
					}
					v = v<<4 | uint16(d)
				}
				sb.WriteUnit(v)
				i += 4
			default:
				p.pos = i
				return nil, p.r.SyntaxError("Bad escaped character in JSON at position %d", i)
			}
			i++
			runStart = i
		case c < 0x20:
			p.pos = i
			return nil, p.r.SyntaxError("Bad control character in string literal in JSON at position %d", i)
		default:
			i++
		}
	}
	p.pos = len(src)
	return nil, p.r.SyntaxError("Unterminated string in JSON at position %d", len(src))
}

// appendWTF8Units decodes a WTF-8 byte run into UTF-16 code units.
func appendWTF8Units(u []uint16, s string) []uint16 {
	for i := 0; i < len(s); {
		c := s[i]
		if c < 0x80 {
			u = append(u, uint16(c))
			i++
			continue
		}
		if c == 0xED && i+2 < len(s) && s[i+1] >= 0xA0 && s[i+1] <= 0xBF && s[i+2] >= 0x80 && s[i+2] <= 0xBF {
			u = append(u, 0xD000|uint16(s[i+1]&0x3F)<<6|uint16(s[i+2]&0x3F))
			i += 3
			continue
		}
		rn, w := utf8.DecodeRuneInString(s[i:])
		i += w
		if rn >= 0x10000 {
			hi, lo := utf16.EncodeRune(rn)
			u = append(u, uint16(hi), uint16(lo))
			continue
		}
		u = append(u, uint16(rn))
	}
	return u
}

// key parses an object key into a PropertyKey without allocating for
// plain ASCII names that are already interned.
func (p *jsonParser) key() (PropertyKey, error) {
	if p.pos >= len(p.src) || p.src[p.pos] != '"' {
		return PropertyKey{}, p.unexpected()
	}
	start := p.pos + 1
	i := start
	src := p.src
	for i < len(src) {
		c := src[i]
		if c == '"' {
			p.pos = i + 1
			return p.r.KeyFromGoString(src[start:i]), nil
		}
		if c == '\\' || c < 0x20 || c >= 0x80 {
			break
		}
		i++
	}
	s, err := p.str()
	if err != nil {
		return PropertyKey{}, err
	}
	return p.r.KeyFromString(s), nil
}

// jsonKeyScanLimit is the object size above which JSON.parse indexes keys
// for duplicate detection instead of scanning them.
const jsonKeyScanLimit = 64

func (p *jsonParser) enter() error {
	p.depth++
	if p.depth > jsonMaxDepth {
		return p.r.SyntaxError("JSON nesting too deep")
	}
	return nil
}

func (p *jsonParser) object() (Value, error) {
	if err := p.enter(); err != nil {
		return Undefined(), err
	}
	p.pos++ // '{'
	p.skipWS()
	r := p.r
	if p.pos < len(p.src) && p.src[p.pos] == '}' {
		p.pos++
		p.depth--
		return ObjectValue(r.NewObject()), nil
	}
	base, kbase := len(p.stack), len(p.keys)
	// Duplicate keys (last wins, first position kept) are found by scanning
	// the object's keys while it is small and through an index once it has
	// more than jsonKeyScanLimit, so a wide object stays linear.
	var index map[PropertyKey]int
	for {
		p.skipWS()
		k, err := p.key()
		if err != nil {
			return Undefined(), err
		}
		p.skipWS()
		if p.pos >= len(p.src) || p.src[p.pos] != ':' {
			return Undefined(), p.unexpected()
		}
		p.pos++
		p.skipWS()
		v, err := p.value()
		if err != nil {
			return Undefined(), err
		}
		seg := p.keys[kbase:]
		dup := -1
		if index != nil {
			if i, ok := index[k]; ok {
				dup = i
			}
		} else {
			for i := range seg {
				if seg[i] == k {
					dup = i
					break
				}
			}
		}
		if dup >= 0 {
			p.stack[base+dup] = v
		} else {
			if index != nil {
				index[k] = len(seg)
			} else if len(seg) == jsonKeyScanLimit {
				index = make(map[PropertyKey]int, 2*jsonKeyScanLimit)
				for i, key := range seg {
					index[key] = i
				}
				index[k] = len(seg)
			}
			p.keys = append(p.keys, k)
			p.stack = append(p.stack, v)
		}
		p.skipWS()
		if p.pos >= len(p.src) {
			return Undefined(), p.unexpected()
		}
		if c := p.src[p.pos]; c == ',' {
			p.pos++
			continue
		} else if c == '}' {
			p.pos++
			break
		}
		return Undefined(), p.unexpected()
	}
	keys, vals := p.keys[kbase:], p.stack[base:]
	o := r.buildJSONObject(keys, vals)
	clear(p.stack[base:])
	p.stack = p.stack[:base]
	p.keys = p.keys[:kbase]
	p.depth--
	return ObjectValue(o), nil
}

// buildJSONObject creates an object with the given own properties in order,
// walking the realm's shape tree so equal key sequences share a Shape.
func (r *Realm) buildJSONObject(keys []PropertyKey, vals []Value) *Object {
	hasIndex := false
	for _, k := range keys {
		if k.IsIndex() {
			hasIndex = true
			break
		}
	}
	if hasIndex || len(keys) > maxShapeProps {
		o := r.NewObject()
		for i, k := range keys {
			o.addProp(r, k, propCell{value: vals[i], attrs: attrDefault})
		}
		return o
	}
	shape := r.plainRoot
	for _, k := range keys {
		shape = shape.addProperty(r, k, attrDefault)
	}
	o := r.newObject(ClassObject, shape)
	slots := make([]Value, len(vals))
	copy(slots, vals)
	o.slots = slots
	return o
}

func (p *jsonParser) array() (Value, error) {
	if err := p.enter(); err != nil {
		return Undefined(), err
	}
	p.pos++ // '['
	p.skipWS()
	if p.pos < len(p.src) && p.src[p.pos] == ']' {
		p.pos++
		p.depth--
		return ObjectValue(p.r.NewArrayFromSlice(nil)), nil
	}
	base := len(p.stack)
	for {
		p.skipWS()
		v, err := p.value()
		if err != nil {
			return Undefined(), err
		}
		p.stack = append(p.stack, v)
		p.skipWS()
		if p.pos >= len(p.src) {
			return Undefined(), p.unexpected()
		}
		if c := p.src[p.pos]; c == ',' {
			p.pos++
			continue
		} else if c == ']' {
			p.pos++
			break
		}
		return Undefined(), p.unexpected()
	}
	items := make([]Value, len(p.stack)-base)
	copy(items, p.stack[base:])
	clear(p.stack[base:])
	p.stack = p.stack[:base]
	p.depth--
	return ObjectValue(p.r.NewArrayFromSlice(items)), nil
}

// --- stringify --------------------------------------------------------------------------------

// JSONStringify serializes v with no replacer and no indentation
// (Go-callable entry point). The result is nil when v is not serializable
// (undefined, functions, symbols).
func (r *Realm) JSONStringify(v Value) (*String, error) {
	js := jsonStringifier{r: r}
	js.stack = js.stackBuf[:0]
	js.sb.Grow(max(256, int(r.jsonSizeHint)+int(r.jsonSizeHint)/8))
	rv, err := js.resolve(v, StringKey(AtomEmpty), nil)
	if err != nil {
		return nil, err
	}
	ok, err := js.write(rv)
	if err != nil || !ok {
		return nil, err
	}
	r.jsonSizeHint = int32(js.sb.Len())
	return js.sb.String(), nil
}

func jsonStringify(r *Realm, this Value, args []Value) (Value, error) {
	value, replacer, space := Arg(args, 0), Arg(args, 1), Arg(args, 2)
	js := jsonStringifier{r: r}
	js.stack = js.stackBuf[:0]
	if IsCallable(replacer) {
		js.replacerFn = replacer
	} else if isArr, err := r.isArray(replacer); err != nil {
		return Undefined(), err
	} else if isArr {
		if err := js.setPropertyList(replacer.AsObject()); err != nil {
			return Undefined(), err
		}
	}
	if space.IsObject() {
		switch space.AsObject().class {
		case ClassNumber:
			f, err := r.ToNumber(space)
			if err != nil {
				return Undefined(), err
			}
			space = NumberValue(f)
		case ClassString:
			s, err := r.ToString(space)
			if err != nil {
				return Undefined(), err
			}
			space = StringValue(s)
		}
	}
	switch {
	case space.IsNumber():
		n := min(10, ToIntegerOrInfinityFloat(space.AsNumber()))
		if n >= 1 {
			js.gap = asciiString("          "[:int(n)])
		}
	case space.IsString():
		if s := space.AsString(); s.Len() > 0 {
			js.gap = s.Substring(0, min(10, s.Len()))
		}
	}
	js.sb.Grow(max(256, int(r.jsonSizeHint)+int(r.jsonSizeHint)/8))
	var root *Object
	if js.replacerFn.IsObject() {
		root = r.NewObject()
		root.DefineOwnDataFast(r, StringKey(AtomEmpty), value, attrDefault)
	}
	rv, err := js.resolve(value, StringKey(AtomEmpty), root)
	if err != nil {
		return Undefined(), err
	}
	ok, err := js.write(rv)
	if err != nil {
		return Undefined(), err
	}
	if !ok {
		return Undefined(), nil
	}
	if err := js.sb.checkLength(r); err != nil {
		return Undefined(), err
	}
	r.jsonSizeHint = int32(js.sb.Len())
	return StringValue(js.sb.String()), nil
}

type jsonStringifier struct {
	r            *Realm
	sb           StringBuilder
	replacerFn   Value // callable, or the zero Value
	propertyList []PropertyKey
	hasList      bool
	gap          *String // nil for no indentation
	indent       *String
	stack        []*Object
	stackBuf     [16]*Object
	work         int
}

// setPropertyList implements the replacer-array branch of JSON.stringify.
func (js *jsonStringifier) setPropertyList(arr *Object) error {
	r := js.r
	js.hasList = true
	n, err := r.LengthOfArrayLike(arr)
	if err != nil {
		return err
	}
	var set map[PropertyKey]struct{} // the keys of a long list
	for i := int64(0); i < n; i++ {
		v, err := getIndex(r, arr, i)
		if err != nil {
			return err
		}
		// A proxy's length can make the list as long as it likes.
		if err := interruptEvery(r, i); err != nil {
			return err
		}
		var item *String
		switch {
		case v.IsString():
			item = v.AsString()
		case v.IsNumber():
			item = NumberToString(v.AsNumber())
		case v.IsObject() && (v.AsObject().class == ClassString || v.AsObject().class == ClassNumber):
			if item, err = r.ToString(v); err != nil {
				return err
			}
		default:
			continue
		}
		set = js.addListKey(set, r.KeyFromString(item))
	}
	return nil
}

// listSetMin is the property list length from which addListKey keeps a set
// of the keys.
const listSetMin = 32

// addListKey appends k to the property list unless it is there already. A
// short list is scanned; a longer one keeps set, which addListKey creates
// and returns, so that a long replacer array costs linear time.
func (js *jsonStringifier) addListKey(set map[PropertyKey]struct{}, k PropertyKey) map[PropertyKey]struct{} {
	if set != nil {
		if _, ok := set[k]; !ok {
			set[k] = struct{}{}
			js.propertyList = append(js.propertyList, k)
		}
		return set
	}
	for _, e := range js.propertyList {
		if e == k {
			return nil
		}
	}
	js.propertyList = append(js.propertyList, k)
	if len(js.propertyList) == listSetMin {
		set = make(map[PropertyKey]struct{}, 2*listSetMin)
		for _, e := range js.propertyList {
			set[e] = struct{}{}
		}
	}
	return set
}

// resolve performs the value-transforming steps of SerializeJSONProperty:
// toJSON, the replacer function and unwrapping of boxed primitives.
func (js *jsonStringifier) resolve(v Value, key PropertyKey, holder *Object) (Value, error) {
	r := js.r
	if v.IsObject() || v.IsBigInt() {
		toJSON, err := r.GetV(v, StringKey(AtomToJSON))
		if err != nil {
			return Undefined(), err
		}
		if IsCallable(toJSON) {
			if v, err = r.Call(toJSON, v, []Value{StringValue(key.ToJSString(r))}); err != nil {
				return Undefined(), err
			}
		}
	}
	if js.replacerFn.IsObject() {
		var err error
		if v, err = r.Call(js.replacerFn, ObjectValue(holder), []Value{StringValue(key.ToJSString(r)), v}); err != nil {
			return Undefined(), err
		}
	}
	if v.IsObject() {
		o := v.AsObject()
		switch o.class {
		case ClassNumber:
			f, err := r.ToNumber(v)
			if err != nil {
				return Undefined(), err
			}
			return NumberValue(f), nil
		case ClassString:
			s, err := r.ToString(v)
			if err != nil {
				return Undefined(), err
			}
			return StringValue(s), nil
		case ClassBoolean, ClassBigInt:
			pv, _ := o.PrimitiveValue()
			return pv, nil
		}
	}
	return v, nil
}

// write serializes a resolved value; ok is false when the value has no JSON
// representation (undefined, function, symbol).
func (js *jsonStringifier) write(v Value) (bool, error) {
	if err := js.tick(); err != nil {
		return false, err
	}
	switch v.Type() {
	case TypeNull:
		js.sb.WriteGoString("null")
	case TypeBoolean:
		if v.AsBool() {
			js.sb.WriteGoString("true")
		} else {
			js.sb.WriteGoString("false")
		}
	case TypeString:
		if err := js.quote(v.AsString()); err != nil {
			return false, err
		}
	case TypeNumber:
		js.number(v.AsNumber())
	case TypeBigInt:
		return false, js.r.TypeError("Do not know how to serialize a BigInt")
	case TypeObject:
		o := v.AsObject()
		if o.class == ClassFunction {
			return false, nil
		}
		if o.class == ClassArray {
			return true, js.array(o)
		}
		if o.class == ClassProxy {
			return js.proxy(o)
		}
		return true, js.object(o)
	default:
		return false, nil
	}
	// One primitive was appended (a quoted string is the unbounded case).
	return true, js.sb.checkLength(js.r)
}

// tick counts one unit of work, a value written or a property visited and
// skipped, and checks the length and the interrupt every 4096.
func (js *jsonStringifier) tick() error {
	js.work++
	if js.work&4095 == 0 {
		return js.checkpoint()
	}
	return nil
}

// checkpoint checks the interrupt and the length of the output, which an
// array hole's "null" and its indentation add to unchecked: each hole is a
// unit of work, so a few thousand at most (tens of megabytes at the depth
// limit) are written past the limit before the RangeError.
func (js *jsonStringifier) checkpoint() error {
	if err := js.sb.checkLength(js.r); err != nil {
		return err
	}
	return js.r.CheckInterrupt()
}

func (js *jsonStringifier) number(f float64) {
	if f != f || math.IsInf(f, 0) {
		js.sb.WriteGoString("null")
		return
	}
	var buf [32]byte
	writeASCIIBytes(&js.sb, AppendNumber(buf[:0], f))
}

// writeASCIIBytes appends ASCII bytes to sb without an intermediate string.
func writeASCIIBytes(sb *StringBuilder, b []byte) {
	// bytesToString does not retain b: writeASCIIBytes copies before
	// returning and the caller keeps ownership of the buffer.
	sb.writeASCIIBytes(bytesToString(b))
}

const lowerHex = "0123456789abcdef"

// quote implements QuoteJSONString (well-formed: lone surrogates as \uXXXX);
// the interrupt flag is checked every interruptStride units of a long string.
func (js *jsonStringifier) quote(s *String) error {
	sb := &js.sb
	if s.kind == strRope {
		s.flatten()
	}
	sb.WriteASCII('"')
	if s.kind == strASCII {
		str := s.s
		run := 0
		for base := 0; base < len(str); base += interruptStride {
			if base > 0 {
				if err := js.r.CheckInterrupt(); err != nil {
					return err
				}
			}
			for i := base; i < min(base+interruptStride, len(str)); i++ {
				c := str[i]
				if c >= 0x20 && c != '"' && c != '\\' {
					continue
				}
				writeASCIIString(sb, str[run:i])
				run = i + 1
				js.escapeByte(c)
			}
		}
		writeASCIIString(sb, str[run:])
		sb.WriteASCII('"')
		return nil
	}
	u := s.u
	for i := 0; i < len(u); i++ {
		if err := interruptEvery(js.r, int64(i)); err != nil {
			return err
		}
		c := u[i]
		switch {
		case c < 0x20 || c == '"' || c == '\\':
			js.escapeByte(byte(c))
		case c >= 0xD800 && c < 0xE000:
			if c < 0xDC00 && i+1 < len(u) && u[i+1] >= 0xDC00 && u[i+1] < 0xE000 {
				sb.WriteUnit(c)
				sb.WriteUnit(u[i+1])
				i++
				continue
			}
			sb.WriteGoString(`\u`)
			sb.WriteASCII(lowerHex[c>>12])
			sb.WriteASCII(lowerHex[c>>8&15])
			sb.WriteASCII(lowerHex[c>>4&15])
			sb.WriteASCII(lowerHex[c&15])
		default:
			sb.WriteUnit(c)
		}
	}
	sb.WriteASCII('"')
	return nil
}

func (js *jsonStringifier) escapeByte(c byte) {
	sb := &js.sb
	switch c {
	case '"':
		sb.WriteGoString(`\"`)
	case '\\':
		sb.WriteGoString(`\\`)
	case '\b':
		sb.WriteGoString(`\b`)
	case '\f':
		sb.WriteGoString(`\f`)
	case '\n':
		sb.WriteGoString(`\n`)
	case '\r':
		sb.WriteGoString(`\r`)
	case '\t':
		sb.WriteGoString(`\t`)
	default:
		sb.WriteGoString(`\u00`)
		sb.WriteASCII(lowerHex[c>>4])
		sb.WriteASCII(lowerHex[c&15])
	}
}

func (js *jsonStringifier) push(o *Object) error {
	for _, e := range js.stack {
		if e == o {
			return js.r.TypeError("Converting circular structure to JSON")
		}
	}
	if len(js.stack) >= jsonMaxDepth {
		return js.r.RangeError("Maximum call stack size exceeded")
	}
	js.stack = append(js.stack, o)
	return nil
}

func (js *jsonStringifier) pop() { js.stack = js.stack[:len(js.stack)-1] }

func (js *jsonStringifier) newline(indent *String) {
	js.sb.WriteASCII('\n')
	if indent != nil {
		js.sb.WriteString(indent)
	}
}

// object implements SerializeJSONObject.
func (js *jsonStringifier) object(o *Object) error {
	r := js.r
	if err := js.push(o); err != nil {
		return err
	}
	stepback := js.indent
	if js.gap != nil {
		var err error
		if js.indent, err = r.Concat(orEmpty(js.indent), js.gap); err != nil {
			return err
		}
	}
	js.sb.WriteASCII('{')
	first := true
	if o.flags&flagHasLazy != 0 {
		o.materializeHost()
	}
	if !js.hasList && o.flags&(flagDict|flagHasLazy) == 0 && len(o.elements) == 0 && (o.dict == nil || o.dict.sparse == nil) && o.class != ClassString && o.class != ClassProxy && o.class != ClassTypedArray {
		// Fast path: plain shape-mode object without indexed properties.
		// The shape's property list is the spec's key snapshot; slots are
		// read directly while the shape is unchanged and through [[Get]]
		// once user code (toJSON, replacer) has mutated the object.
		shape := o.shape
		props := shape.Props()
		for i, p := range props {
			if p.attrs&attrEnumerable == 0 || !p.key.IsString() {
				if err := js.tick(); err != nil {
					return err
				}
				continue
			}
			var v Value
			if o.shape == shape && p.attrs&attrAccessor == 0 {
				v = o.slots[i]
			} else {
				var err error
				if v, err = o.Get(r, p.key, ObjectValue(o)); err != nil {
					return err
				}
			}
			var err error
			if v, err = js.resolve(v, p.key, o); err != nil {
				return err
			}
			if v.IsUndefined() || (v.IsObject() && v.AsObject().IsCallable()) || v.IsSymbol() {
				if err := js.tick(); err != nil {
					return err
				}
				continue
			}
			if !first {
				js.sb.WriteASCII(',')
			}
			first = false
			if js.gap != nil {
				js.newline(js.indent)
			}
			if err := js.quote(p.key.String()); err != nil {
				return err
			}
			js.sb.WriteASCII(':')
			if js.gap != nil {
				js.sb.WriteASCII(' ')
			}
			if _, err := js.write(v); err != nil {
				return err
			}
		}
	} else {
		keys := js.propertyList
		if !js.hasList {
			var err error
			if keys, err = r.countedEnumerableOwnKeys(o, &js.work); err != nil {
				return err
			}
		}
		for _, k := range keys {
			var v Value
			if c, ok := o.getOwnCell(k); ok && c.attrs&attrAccessor == 0 {
				v = c.value
			} else {
				var err error
				if v, err = o.Get(r, k, ObjectValue(o)); err != nil {
					return err
				}
			}
			v, err := js.resolve(v, k, o)
			if err != nil {
				return err
			}
			if v.IsUndefined() || (v.IsObject() && v.AsObject().IsCallable()) || v.IsSymbol() {
				if err := js.tick(); err != nil {
					return err
				}
				continue
			}
			if !first {
				js.sb.WriteASCII(',')
			}
			first = false
			if js.gap != nil {
				js.newline(js.indent)
			}
			if k.IsString() {
				err = js.quote(k.String())
			} else {
				err = js.quote(k.ToJSString(r))
			}
			if err != nil {
				return err
			}
			js.sb.WriteASCII(':')
			if js.gap != nil {
				js.sb.WriteASCII(' ')
			}
			if _, err := js.write(v); err != nil {
				return err
			}
		}
	}
	if !first && js.gap != nil {
		js.newline(stepback)
	}
	js.sb.WriteASCII('}')
	js.pop()
	js.indent = stepback
	return js.sb.checkLength(r)
}

// proxy serializes a proxy: nothing for a callable one, otherwise an array
// or an object as IsArray decides, through its traps.
func (js *jsonStringifier) proxy(o *Object) (bool, error) {
	if o.IsCallable() {
		return false, nil
	}
	isArr, err := js.r.isArray(ObjectValue(o))
	if err != nil {
		return false, err
	}
	if isArr {
		return true, js.array(o)
	}
	return true, js.object(o)
}

// array implements SerializeJSONArray.
func (js *jsonStringifier) array(o *Object) error {
	r := js.r
	if err := js.push(o); err != nil {
		return err
	}
	stepback := js.indent
	if js.gap != nil {
		var err error
		if js.indent, err = r.Concat(orEmpty(js.indent), js.gap); err != nil {
			return err
		}
	}
	n, err := r.LengthOfArrayLike(o)
	if err != nil {
		return err
	}
	js.sb.WriteASCII('[')
	for i := range n {
		if i > 0 {
			js.sb.WriteASCII(',')
		}
		if js.gap != nil {
			js.newline(js.indent)
		}
		v, err := o.GetIndex(r, uint32(i))
		if err != nil {
			return err
		}
		v, err = js.resolve(v, IndexKey(uint32(i)), o)
		if err != nil {
			return err
		}
		ok, err := js.write(v)
		if err != nil {
			return err
		}
		if !ok {
			// Checked at the next checkpoint.
			js.sb.WriteGoString("null")
		}
	}
	if n > 0 && js.gap != nil {
		js.newline(stepback)
	}
	js.sb.WriteASCII(']')
	js.pop()
	js.indent = stepback
	// Repeated references to one object repeat its output: check it.
	return js.sb.checkLength(r)
}

func orEmpty(s *String) *String {
	if s == nil {
		return emptyString
	}
	return s
}
