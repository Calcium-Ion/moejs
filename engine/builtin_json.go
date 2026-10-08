package engine

import (
	"math"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"
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

// jsonMaxDepth bounds nesting in parse, stringify and the reviver walk, as
// deep as ToGo exports: past it each is a RangeError. The parser runs no
// user code, so its recursion, a few hundred bytes of Go stack a level, is
// held by one parse at a time. Stringify and the reviver walk run user code
// that can start another walk, so each jsonCallLevels levels of theirs also
// count as one call against MaxCallDepth, which bounds the Go stack all the
// walks hold together. Stringify counts only past jsonScanDepth, so that a
// shallow value pays nothing: the walks that fit in MaxCallDepth when a
// toJSON at each one's last uncounted level starts the next hold under 8 MB.
const jsonMaxDepth = MaxToGoDepth

// jsonCallLevels is how many levels of a stringify or reviver walk count as
// one call.
const jsonCallLevels = 32

// --- parse -------------------------------------------------------------------------------

// JSONParse parses text with the JSON grammar and no reviver (Go-callable
// entry point). Errors are JavaScript SyntaxErrors, except that nesting
// deeper than MaxToGoDepth arrays and objects is a RangeError.
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
	if depth > 0 && depth%jsonCallLevels == 0 {
		if err := r.EnterCall(); err != nil {
			return Undefined(), err
		}
		defer r.ExitCall()
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
	r   *Realm
	src string // ASCII text or WTF-8 working copy
	// short holds recent short plain literals by content (strSlot): a
	// repeated value (a role, a type, a status) is one String.
	short [hostStrSlots]*String
	pos   int
	depth int
	// work counts the values parsed (only its low bits are read). It is an
	// int32 so that quiet shares its word: one word more in the parser
	// costs about a quarter of an instruction per byte of a long text
	// (ParseJSON of the multi-image bodies, 2.7 and 7.5 MiB: +2.4 % and
	// +4.1 % instructions, GOGC=off).
	work int32
	// quiet: observe no interrupt (a parse for FromGo, which may run where
	// no error can be returned, materializing a json.RawMessage).
	quiet bool
	stack []Value
	keys  []PropertyKey
}

// jsonStack is the stack a parse builds containers on: the values and keys
// of the containers still open. A parse runs no JavaScript (a reviver runs
// once it has returned), so the realm keeps one (realmLazy) that every
// parse reuses and leaves cleared, so that what it keeps pins nothing. A Go
// panic out of a parse skips the clearing; DropJobs does it at the host's
// boundary.
type jsonStack struct {
	vals []Value
	keys []PropertyKey
}

// jsonStackMax bounds the entries a kept stack has room for (4 KiB each);
// a parse that needed more grows its own from nil.
const jsonStackMax = 256

func (p *jsonParser) parse(text *String) (Value, error) {
	f := text
	if text.kind == strRope {
		f = text.flat(new(String))
	}
	if f.kind == strASCII {
		return p.parseText(f.s)
	}
	return p.parseText(wtf8FromUTF16(f.units()))
}

// parseText parses src, ASCII or WTF-8; the strings of the result may alias
// it.
func (p *jsonParser) parseText(src string) (Value, error) {
	p.src = src
	l := p.r.lazyState()
	if l.json == nil {
		l.json = &jsonStack{}
	}
	p.stack, p.keys = l.json.vals, l.json.keys
	p.skipWS()
	v, err := p.value()
	if err == nil {
		p.skipWS()
		if p.pos < len(p.src) {
			err = p.unexpected()
		}
	}
	l.json.keep(p.stack, p.keys)
	if err != nil {
		return Undefined(), err
	}
	return v, nil
}

// keep takes back the stack a parse grew, clearing the entries a failed
// parse left (those of the containers it was in).
func (s *jsonStack) keep(vals []Value, keys []PropertyKey) {
	clear(vals)
	clear(keys)
	if cap(vals) > jsonStackMax {
		vals = nil
	}
	if cap(keys) > jsonStackMax {
		keys = nil
	}
	s.vals, s.keys = vals[:0], keys[:0]
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
	if p.work&4095 == 0 && !p.quiet {
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

// str parses a string literal (p.pos at the opening quote). jsonScan stops
// at bytes >= 0x80 until it meets the first.
func (p *jsonParser) str() (*String, error) {
	start := p.pos + 1
	src := p.src
	high := uint64(swarMSB)
	for i := jsonScan(src, start, high); i < len(src); i = jsonScan(src, i+1, high) {
		switch c := src[i]; {
		case c == '"':
			p.pos = i + 1
			if high != 0 {
				return p.plain(src[start:i]), nil
			}
			p.r.chargeString(2 * (i - start))
			return FromUTF16(appendWTF8Units(nil, src[start:i])), nil
		case c == '\\':
			return p.strSlow(start, i)
		case c < 0x20:
			p.pos = i
			return nil, p.r.SyntaxError("Bad control character in string literal in JSON at position %d", i)
		}
		high = 0 // not ASCII
	}
	p.pos = len(src)
	return nil, p.r.SyntaxError("Unterminated string in JSON at position %d", len(src))
}

// strSlow continues a string literal from the first escape at i.
func (p *jsonParser) strSlow(start, i int) (*String, error) {
	src := p.src
	var sb StringBuilder
	var units []uint16 // scratch for the runs that are not ASCII
	sb.Grow(i - start + 16)
	run := start
	for i < len(src) {
		c := src[i]
		if c < 0x20 {
			p.pos = i
			return nil, p.r.SyntaxError("Bad control character in string literal in JSON at position %d", i)
		}
		units = writeWTF8Run(&sb, src[run:i], units)
		if c == '"' {
			p.pos = i + 1
			return p.r.builtString(&sb), nil
		}
		i++ // the backslash
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
		run = i + 1
		i = jsonScan(src, run, 0)
	}
	p.pos = len(src)
	return nil, p.r.SyntaxError("Unterminated string in JSON at position %d", len(src))
}

// writeWTF8Run appends the WTF-8 run s to sb, decoding it through scratch
// when it is not ASCII; it returns scratch for the next run.
func writeWTF8Run(sb *StringBuilder, s string, scratch []uint16) []uint16 {
	if isASCII(s) {
		sb.writeASCIIBytes(s)
		return scratch
	}
	scratch = appendWTF8Units(scratch[:0], s)
	sb.WriteUTF16(scratch)
	return scratch
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
		return p.r.RangeError("Maximum call stack size exceeded")
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
	clear(p.keys[kbase:])
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
	var o *Object
	var slots []Value
	switch len(vals) {
	case 1:
		x := &object1{}
		o, slots = &x.Object, x.buf[:]
	case 2:
		x := &object2{}
		o, slots = &x.Object, x.buf[:]
	case 3:
		x := &object3{}
		o, slots = &x.Object, x.buf[:]
	case 4:
		x := &object4{}
		o, slots = &x.Object, x.buf[:]
	case 5:
		x := &object5{}
		o, slots = &x.Object, x.buf[:]
	case 6:
		x := &object6{}
		o, slots = &x.Object, x.buf[:]
	case 7:
		x := &object7{}
		o, slots = &x.Object, x.buf[:]
	case 8:
		x := &object8{}
		o, slots = &x.Object, x.buf[:]
	default:
		o, slots = new(Object), make([]Value, len(vals))
	}
	copy(slots, vals)
	o.slots = slots
	r.chargeObject(unsafe.Sizeof(Object{}) + uintptr(len(slots)*valueSize))
	return initObject(o, ClassObject, shape)
}

// A parsed object or array of up to eight entries is one allocation of the
// bytes its two used to be: the even sizes are the literals' buckets
// (object.go, array.go), the odd ones these.
type (
	object1 struct {
		Object
		buf [1]Value
	}
	object3 struct {
		Object
		buf [3]Value
	}
	object5 struct {
		Object
		buf [5]Value
	}
	object7 struct {
		Object
		buf [7]Value
	}
	arrayObject1 struct {
		arrayObject
		buf [1]Value
	}
	arrayObject3 struct {
		arrayObject
		buf [3]Value
	}
	arrayObject5 struct {
		arrayObject
		buf [5]Value
	}
	arrayObject7 struct {
		arrayObject
		buf [7]Value
	}
)

// newJSONArray creates a dense array holding a copy of vals.
func (r *Realm) newJSONArray(vals []Value) *Object {
	var ao *arrayObject
	var items []Value
	switch len(vals) {
	case 1:
		x := &arrayObject1{}
		ao, items = &x.arrayObject, x.buf[:]
	case 2:
		x := &arrayObject2{}
		ao, items = &x.arrayObject, x.buf[:]
	case 3:
		x := &arrayObject3{}
		ao, items = &x.arrayObject, x.buf[:]
	case 4:
		x := &arrayObject4{}
		ao, items = &x.arrayObject, x.buf[:]
	case 5:
		x := &arrayObject5{}
		ao, items = &x.arrayObject, x.buf[:]
	case 6:
		x := &arrayObject6{}
		ao, items = &x.arrayObject, x.buf[:]
	case 7:
		x := &arrayObject7{}
		ao, items = &x.arrayObject, x.buf[:]
	case 8:
		x := &arrayObject8{}
		ao, items = &x.arrayObject, x.buf[:]
	default:
		ao, items = &arrayObject{}, make([]Value, len(vals))
	}
	copy(items, vals)
	return r.initArray(ao, items, uint32(len(vals)))
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
	a := p.r.newJSONArray(p.stack[base:])
	clear(p.stack[base:])
	p.stack = p.stack[:base]
	p.depth--
	return ObjectValue(a), nil
}

// --- stringify --------------------------------------------------------------------------------

// JSONStringify serializes v with no replacer and no indentation
// (Go-callable entry point). The result is nil when v is not serializable
// (undefined, functions, symbols). Nesting deeper than MaxToGoDepth arrays
// and objects is a RangeError, as is running out of call depth: each 32
// levels count as a call, since a toJSON method can start another stringify.
func (r *Realm) JSONStringify(v Value) (*String, error) {
	js := jsonStringifier{r: r}
	js.sb.Grow(max(256, int(r.jsonSizeHint)+int(r.jsonSizeHint)/8))
	if ok, err := js.serialize(v); !ok {
		return nil, err
	}
	r.jsonSizeHint = int32(js.sb.Len())
	return r.builtString(&js.sb), nil
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
	depth := r.callDepth
	ok, err := js.write(rv)
	if err != nil {
		r.callDepth = depth // the levels an error leaves entered (push)
		return Undefined(), err
	}
	if !ok {
		return Undefined(), nil
	}
	if err := js.sb.checkLength(r); err != nil {
		return Undefined(), err
	}
	r.jsonSizeHint = int32(js.sb.Len())
	return StringValue(r.builtString(&js.sb)), nil
}

type jsonStringifier struct {
	r            *Realm
	sb           StringBuilder
	replacerFn   Value // callable, or the zero Value
	propertyList []PropertyKey
	hasList      bool
	raw          bool    // AppendJSON: sb.b holds UTF-8 (quoteUTF8) and never upgrades
	inDst        bool    // AppendJSON: sb.b is still dst's spare capacity (own)
	plainEpoch   uint32  // protosLackToJSON's answer: protoEpoch+1 when positive
	gap          *String // nil for no indentation
	stack        []*Object
	stackBuf     [16]*Object
	deep         map[*Object]struct{} // stack[jsonScanDepth:], for the cycle check
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
	if v.IsObject() && r.lacksToJSON(v.AsObject(), &js.plainEpoch) {
		// No toJSON to call.
	} else if v.IsObject() || v.IsBigInt() {
		js.own() // the lookup (a getter, a proxy's trap) and toJSON run code
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

// checkpoint checks the interrupt and the length of the output.
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
	if s.kind == strRope {
		return js.quoteRope(s)
	}
	js.sb.WriteASCII('"')
	var err error
	switch {
	case s.json != 0: // plain, or a long FromGo string not scanned yet
		if int8(s.json) < 0 { // jsonDeferred
			if !s.scanJSON() {
				err = js.quoteASCII(s.s)
				break
			}
		}
		writeASCIIString(&js.sb, s.s)
	case s.kind == strASCII:
		err = js.quoteASCII(s.s)
	case js.raw:
		err = js.quoteUTF8(s.units())
	default:
		err = js.quoteUTF16(s.units())
	}
	if err != nil {
		return err
	}
	js.sb.WriteASCII('"')
	return nil
}

// quoteRope is quote of a rope, out of line so that the view (flat) does
// not grow quote's frame. A view knows nothing of its bytes (json 0).
//
//go:noinline
func (js *jsonStringifier) quoteRope(s *String) error {
	f := s.flat(new(String))
	js.sb.WriteASCII('"')
	var err error
	switch {
	case f.kind == strASCII:
		err = js.quoteASCII(f.s)
	case js.raw:
		err = js.quoteUTF8(f.units())
	default:
		err = js.quoteUTF16(f.units())
	}
	if err != nil {
		return err
	}
	js.sb.WriteASCII('"')
	return nil
}

// jsonScanDepth is the nesting up to which the cycle check scans the stack;
// the containers below it are kept in a set (jsonStringifier.deep).
const jsonScanDepth = 64

// push enters o, a container being serialized, or reports the cycle it
// closes. Past jsonScanDepth every jsonCallLevels-th level counts as a call
// (jsonMaxDepth): pop releases it, and on an error the entry point restores
// the depth. A value shallower than jsonScanDepth, the common case, pays
// one compare for the bookkeeping.
func (js *jsonStringifier) push(o *Object) error {
	n := len(js.stack)
	scan := js.stack
	if n > jsonScanDepth {
		scan = scan[:jsonScanDepth]
	}
	for _, e := range scan {
		if e == o {
			return js.r.TypeError("Converting circular structure to JSON")
		}
	}
	if n >= jsonScanDepth {
		if _, ok := js.deep[o]; ok {
			return js.r.TypeError("Converting circular structure to JSON")
		}
		if n >= jsonMaxDepth {
			return js.r.RangeError("Maximum call stack size exceeded")
		}
		if n%jsonCallLevels == 0 {
			if err := js.r.EnterCall(); err != nil {
				return err
			}
		}
		if js.deep == nil {
			js.deep = make(map[*Object]struct{})
		}
		js.deep[o] = struct{}{}
	}
	js.stack = append(js.stack, o)
	return nil
}

func (js *jsonStringifier) pop() {
	n := len(js.stack) - 1
	if n >= jsonScanDepth {
		delete(js.deep, js.stack[n])
		if n%jsonCallLevels == 0 {
			js.r.ExitCall()
		}
	}
	js.stack = js.stack[:n]
}

// newline starts a line indented by level gaps.
func (js *jsonStringifier) newline(level int) {
	js.sb.WriteASCII('\n')
	for range level {
		js.sb.WriteString(js.gap)
	}
}

// object implements SerializeJSONObject.
func (js *jsonStringifier) object(o *Object) error {
	r := js.r
	if o.flags&flagHasLazy != 0 {
		if ok, err := js.hostNode(o); ok || err != nil {
			return err
		}
	}
	if err := js.push(o); err != nil {
		return err
	}
	level := len(js.stack) // the members' indentation
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
				js.own() // a getter
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
				js.newline(level)
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
				js.own() // a getter, a proxy's trap
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
				js.newline(level)
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
		js.newline(level - 1)
	}
	js.sb.WriteASCII('}')
	js.pop()
	return js.sb.checkLength(r)
}

// proxy serializes a proxy: nothing for a callable one, otherwise an array
// or an object as IsArray decides, through its traps. It is placed between
// object and array for layout: its 224 bytes, padded, make up for what
// resolve, object and array shrank by with own out of line, so write,
// quote, push, object, array and escapeByte keep their 64-byte phase.
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
	if o.flags&flagHasLazy != 0 {
		if ok, err := js.hostNode(o); ok || err != nil {
			return err
		}
		o.materializeHost() // its elements, read below
	}
	if err := js.push(o); err != nil {
		return err
	}
	level := len(js.stack) // the elements' indentation
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
			js.newline(level)
		}
		var v Value
		if i < int64(len(o.elements)) && !o.elements[i].IsHole() {
			v = o.elements[i]
		} else {
			js.own() // a getter, of the element or a prototype's, a proxy's trap
			if v, err = o.Get(r, IndexKey(uint32(i)), ObjectValue(o)); err != nil {
				return err
			}
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
			js.sb.WriteGoString("null")
			if err := js.sb.checkLength(r); err != nil { // the indentation
				return err
			}
		}
	}
	if n > 0 && js.gap != nil {
		js.newline(level - 1)
	}
	js.sb.WriteASCII(']')
	js.pop()
	// Repeated references to one object repeat its output: check it.
	return js.sb.checkLength(r)
}

// escapeByte writes the escape of the byte c. It is last in the file for
// layout (see realm_calldata.go): there it keeps push, object and array on
// dev's 64-byte phase, which root BenchmarkCall's AppendJSON shows (about
// 25 of its 1,550 cycles when they moved by 32).
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
