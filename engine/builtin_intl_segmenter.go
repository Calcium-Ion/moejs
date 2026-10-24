package engine

// Intl.Segmenter (ECMA-402 §19): grapheme, word and sentence segmentation
// of UAX #29 (segment.go) with no locale tailoring, so every CLDR locale is
// available and segments alike. Without dictionaries, words of scripts
// written without spaces (Chinese, Japanese, Thai, ...) are the UAX #29
// default: one per ideograph, unlike ICU's dictionary words.
//
// A Segments object keeps the flat code units of its string (segText); its
// iterators and containing step the break algorithms from an index, so a
// segment costs its data object and its substring.

import "unsafe"

// intlSegmenter is the internal of an Intl.Segmenter.
type intlSegmenter struct {
	locale *String
	gran   segGranularity
}

// intlSegments is the internal of a Segments object and of a segment
// iterator, whose next segment starts at pos. shape is the shape of its
// segment data objects in the realm. A Segments object remembers the
// segment containing found last, [lastStart, lastEnd) (lastEnd 0 for none),
// so that a caller walking the text costs a step per segment, as ICU keeps
// the boundaries around its last position.
type intlSegments struct {
	str          *String
	text         segText
	pos          int
	shape        *Shape
	lastStart    int32
	lastEnd      int32
	gran         segGranularity
	lastWordLike bool
}

func installIntlSegmenter(r *Realm, x *intlIntrinsics, a *intlAtoms) {
	x.SegmenterPrototype = r.newIntrinsic(ClassObject, r.ObjectPrototype, 4)
	x.SegmenterCtor = r.newConstructor(a.segmenterName, 0, requireNew("Intl.Segmenter"), intlSegmenterConstruct, x.SegmenterPrototype)
	r.installBuiltins(x.SegmenterCtor, []builtinDef{{a.supportedLocalesOf, intlSupportedLocalesOf, 1}})
	r.installBuiltins(x.SegmenterPrototype, []builtinDef{
		{a.resolvedOptions, intlSegmenterResolvedOptions, 0},
		{a.segment, intlSegmenterSegment, 1},
	})
	r.installToStringTag(x.SegmenterPrototype, a.segmenterTag)

	x.SegmentsPrototype = r.newIntrinsic(ClassObject, r.ObjectPrototype, 2)
	r.installBuiltins(x.SegmentsPrototype, []builtinDef{{a.containing, intlSegmentsContaining, 1}})
	r.installSymbolMethod(x.SegmentsPrototype, SymIterator, atomIteratorFn, intlSegmentsIterator, 0, attrHidden)

	x.SegmentIteratorPrototype = r.newIntrinsic(ClassObject, r.IteratorPrototype, 2)
	x.segmentIterNextFn = r.NewNativeFunction(AtomNext, 0, intlSegmentIteratorNext)
	r.installOrReplace(x.SegmentIteratorPrototype, StringKey(AtomNext), propCell{value: ObjectValue(x.segmentIterNextFn), attrs: attrHidden})
	r.installToStringTag(x.SegmentIteratorPrototype, a.segmentIteratorTag)
}

// intlSegmenterConstruct implements Intl.Segmenter(locales, options).
func intlSegmenterConstruct(r *Realm, args []Value, newTarget *Object) (Value, error) {
	const ctor = "Intl.Segmenter"
	x := r.intlIntr()
	proto, err := r.GetPrototypeFromConstructor(newTarget, x.SegmenterCtor, x.SegmenterPrototype)
	if err != nil {
		return Undefined(), err
	}
	requested, err := r.canonicalizeLocaleList(intlArg(args, 0))
	if err != nil {
		return Undefined(), err
	}
	options, err := r.getOptionsObject(intlArg(args, 1))
	if err != nil {
		return Undefined(), err
	}
	if _, err := r.intlLocaleMatcher(options, ctor); err != nil {
		return Undefined(), err
	}
	locale := r.intlResolveLocale(requested)
	a := intlNames()
	g, err := r.intlStringOption(options, ctor, a.granularity, a.grapheme, a.word, a.sentence)
	if err != nil {
		return Undefined(), err
	}
	s := &intlSegmenter{locale: r.intlString(locale)}
	switch g {
	case a.word:
		s.gran = segWord
	case a.sentence:
		s.gran = segSentence
	}
	r.markPrototype(proto)
	o := r.newObject(ClassIntlSegmenter, r.rootShapeFor(proto))
	o.internal = s
	return ObjectValue(o), nil
}

// intlSegmenterResolvedOptions implements
// Intl.Segmenter.prototype.resolvedOptions().
func intlSegmenterResolvedOptions(r *Realm, this Value, args []Value) (Value, error) {
	s, err := thisIntl[*intlSegmenter](r, this, ClassIntlSegmenter, "Intl.Segmenter.prototype.resolvedOptions")
	if err != nil {
		return Undefined(), err
	}
	a := intlNames()
	g := a.grapheme
	switch s.gran {
	case segWord:
		g = a.word
	case segSentence:
		g = a.sentence
	}
	o := r.NewObjectCap(2)
	o.addNamed(r, StringKey(a.locale), propCell{value: StringValue(s.locale), attrs: attrDefault})
	o.addNamed(r, StringKey(a.granularity), propCell{value: StringValue(g), attrs: attrDefault})
	return ObjectValue(o), nil
}

// intlSegmenterSegment implements Intl.Segmenter.prototype.segment(string)
// (CreateSegmentsObject).
func intlSegmenterSegment(r *Realm, this Value, args []Value) (Value, error) {
	s, err := thisIntl[*intlSegmenter](r, this, ClassIntlSegmenter, "Intl.Segmenter.prototype.segment")
	if err != nil {
		return Undefined(), err
	}
	str, err := r.ToString(intlArg(args, 0))
	if err != nil {
		return Undefined(), err
	}
	sg := &intlSegments{str: str, gran: s.gran, shape: r.segmentDataShape(s.gran)}
	if a, ok := str.ASCII(); ok {
		sg.text.ascii = a
	} else {
		sg.text.u = str.UTF16()
	}
	o := r.newObject(ClassIntlSegments, r.rootShapeFor(r.intlIntr().SegmentsPrototype))
	o.internal = sg
	return ObjectValue(o), nil
}

// intlSegmentsContaining implements %IntlSegmentsPrototype%.containing(index).
func intlSegmentsContaining(r *Realm, this Value, args []Value) (Value, error) {
	sg, err := thisIntl[*intlSegments](r, this, ClassIntlSegments, "%Segments.prototype%.containing")
	if err != nil {
		return Undefined(), err
	}
	n, err := r.ToIntegerOrInfinity(intlArg(args, 0))
	if err != nil {
		return Undefined(), err
	}
	if n < 0 || n >= float64(sg.text.len()) {
		return Undefined(), nil
	}
	i := int(n)
	start, end, wordLike := int(sg.lastStart), int(sg.lastEnd), sg.lastWordLike
	if i >= end && end != 0 {
		// The segment after the last one, which a walk reaches next.
		start = end
		end, wordLike = segNext(sg.gran, &sg.text, start)
	}
	if i < start || i >= end {
		start = segStart(sg.gran, &sg.text, i)
		end, wordLike = segNext(sg.gran, &sg.text, start)
	}
	sg.lastStart, sg.lastEnd, sg.lastWordLike = int32(start), int32(end), wordLike
	return r.segmentData(sg, start, end, wordLike), nil
}

// intlSegmentsIterator implements %IntlSegmentsPrototype%[@@iterator]()
// (CreateSegmentIterator).
func intlSegmentsIterator(r *Realm, this Value, args []Value) (Value, error) {
	sg, err := thisIntl[*intlSegments](r, this, ClassIntlSegments, "%Segments.prototype%[@@iterator]")
	if err != nil {
		return Undefined(), err
	}
	it := *sg
	it.pos = 0
	o := r.newObject(ClassIntlSegmentIterator, r.rootShapeFor(r.intlIntr().SegmentIteratorPrototype))
	o.internal = &it
	return ObjectValue(o), nil
}

// intlSegmentIteratorNext implements %IntlSegmentIteratorPrototype%.next().
func intlSegmentIteratorNext(r *Realm, this Value, args []Value) (Value, error) {
	it, err := thisIntl[*intlSegments](r, this, ClassIntlSegmentIterator, "%SegmentIterator.prototype%.next")
	if err != nil {
		return Undefined(), err
	}
	v, done := it.step(r)
	return r.createIterResult(v, done), nil
}

// step advances a segment iterator: the data object of its next segment,
// or done.
func (it *intlSegments) step(r *Realm) (Value, bool) {
	start := it.pos
	if start >= it.text.len() {
		return Undefined(), true
	}
	end, wordLike := segNext(it.gran, &it.text, start)
	it.pos = end
	return r.segmentData(it, start, end, wordLike), false
}

// segmentDataObject co-allocates a segment data object with its slots:
// segment, index and input, and isWordLike for word granularity.
type segmentDataObject struct {
	obj   Object
	slots [3]Value
}

type wordSegmentDataObject struct {
	obj   Object
	slots [4]Value
}

// segmentDataShape returns the shape of the segment data objects of a
// granularity, which Segments objects keep.
func (r *Realm) segmentDataShape(g segGranularity) *Shape {
	a := intlNames()
	s := r.plainRoot.addProperty(r, StringKey(a.segment), attrDefault).
		addProperty(r, StringKey(AtomIndex), attrDefault).
		addProperty(r, StringKey(AtomInput), attrDefault)
	if g == segWord {
		s = s.addProperty(r, StringKey(a.isWordLike), attrDefault)
	}
	return s
}

// segmentData implements CreateSegmentDataObject in one allocation, and
// one for the segment unless it is one ASCII character.
func (r *Realm) segmentData(sg *intlSegments, start, end int, wordLike bool) Value {
	var o *Object
	if sg.gran == segWord {
		d := &wordSegmentDataObject{}
		r.chargeObject(unsafe.Sizeof(wordSegmentDataObject{}))
		d.slots[3] = Bool(wordLike)
		o = &d.obj
		o.slots = d.slots[:]
	} else {
		d := &segmentDataObject{}
		r.chargeObject(unsafe.Sizeof(segmentDataObject{}))
		o = &d.obj
		o.slots = d.slots[:]
	}
	o.shape = sg.shape
	o.proto = r.ObjectPrototype
	o.class = ClassObject
	o.flags = flagExtensible
	var seg *String
	if c := sg.text.at(start); end-start == 1 && c < 0x80 {
		seg = singleCharStrings[c]
	} else {
		seg = r.substring(sg.str, start, end)
	}
	o.slots[0] = StringValue(seg)
	o.slots[1] = IntValue(start)
	o.slots[2] = StringValue(sg.str)
	return ObjectValue(o)
}
