package engine

import "sync"

// Atoms of the binary data builtins (ArrayBuffer, SharedArrayBuffer,
// DataView, the typed arrays and Atomics) and of TextEncoder and
// TextDecoder. Only the global names are static, because coldGlobalKeys
// needs them at package init, and f16round, which the Math table names. The
// property names are interned with the first group of the process that
// needs them (binaryNames, textNames), so a process that never names one of
// its globals carries none of them. They live apart from
// atoms.go so parallel additions to that table merge mechanically;
// staticAtom deduplicates names.
var (
	AtomArrayBuffer       = staticAtom("ArrayBuffer")
	AtomSharedArrayBuffer = staticAtom("SharedArrayBuffer")
	AtomDataView          = staticAtom("DataView")

	// The typed array constructors, in elemType order (typedArrayCtorNames).
	AtomInt8Array         = staticAtom("Int8Array")
	AtomUint8Array        = staticAtom("Uint8Array")
	AtomUint8ClampedArray = staticAtom("Uint8ClampedArray")
	AtomInt16Array        = staticAtom("Int16Array")
	AtomUint16Array       = staticAtom("Uint16Array")
	AtomInt32Array        = staticAtom("Int32Array")
	AtomUint32Array       = staticAtom("Uint32Array")
	AtomFloat16Array      = staticAtom("Float16Array")
	AtomFloat32Array      = staticAtom("Float32Array")
	AtomFloat64Array      = staticAtom("Float64Array")
	AtomBigInt64Array     = staticAtom("BigInt64Array")
	AtomBigUint64Array    = staticAtom("BigUint64Array")

	AtomAtomics  = staticAtom("Atomics")
	AtomF16round = staticAtom("f16round")

	AtomTextEncoder = staticAtom("TextEncoder")
	AtomTextDecoder = staticAtom("TextDecoder")
)

// typedArrayCtorNames names the typed array constructor of each elemType.
var typedArrayCtorNames = [...]*String{
	AtomInt8Array, AtomUint8Array, AtomUint8ClampedArray, AtomInt16Array,
	AtomUint16Array, AtomInt32Array, AtomUint32Array, AtomFloat16Array,
	AtomFloat32Array, AtomFloat64Array, AtomBigInt64Array, AtomBigUint64Array,
}

// binaryAtoms holds the property names of the binary data builtins.
type binaryAtoms struct {
	byteLength, byteOffset, maxByteLength, buffer *String
	resizable, growable, detached                 *String
	resize, grow, transferToFixedLength, isView   *String
	bytesPerElement, subarray, typedArray         *String
	// The Uint8Array base64 and hex members, their options and results.
	fromBase64, fromHex, toBase64, toHex     *String
	setFromBase64, setFromHex                *String
	alphabet, lastChunkHandling, omitPadding *String
	read, written                            *String
	// getToStringTag names the getter of %TypedArray%.prototype[@@toStringTag].
	getToStringTag *String
	// The Atomics functions.
	and, compareExchange, exchange, isLockFree, load, notify, or *String
	store, wait, xor                                             *String
	// get and set name the DataView.prototype methods of each
	// dataViewTypes entry.
	get, set [len(dataViewTypes)]*String
}

// binaryNames interns the binary property names once per process; the
// struct keeps them alive, as the frozen shared group that uses them would.
var binaryNames = sync.OnceValue(func() *binaryAtoms {
	a := &binaryAtoms{
		byteLength:            lateAtom("byteLength"),
		byteOffset:            lateAtom("byteOffset"),
		maxByteLength:         lateAtom("maxByteLength"),
		buffer:                lateAtom("buffer"),
		resizable:             lateAtom("resizable"),
		growable:              lateAtom("growable"),
		detached:              lateAtom("detached"),
		resize:                lateAtom("resize"),
		grow:                  lateAtom("grow"),
		transferToFixedLength: lateAtom("transferToFixedLength"),
		isView:                lateAtom("isView"),
		bytesPerElement:       lateAtom("BYTES_PER_ELEMENT"),
		subarray:              lateAtom("subarray"),
		typedArray:            lateAtom("TypedArray"),
		fromBase64:            lateAtom("fromBase64"),
		fromHex:               lateAtom("fromHex"),
		toBase64:              lateAtom("toBase64"),
		toHex:                 lateAtom("toHex"),
		setFromBase64:         lateAtom("setFromBase64"),
		setFromHex:            lateAtom("setFromHex"),
		alphabet:              lateAtom("alphabet"),
		lastChunkHandling:     lateAtom("lastChunkHandling"),
		omitPadding:           lateAtom("omitPadding"),
		read:                  lateAtom("read"),
		written:               lateAtom("written"),
		getToStringTag:        asciiString("get [Symbol.toStringTag]"),
		and:                   lateAtom("and"),
		compareExchange:       lateAtom("compareExchange"),
		exchange:              lateAtom("exchange"),
		isLockFree:            lateAtom("isLockFree"),
		load:                  lateAtom("load"),
		notify:                lateAtom("notify"),
		or:                    lateAtom("or"),
		store:                 lateAtom("store"),
		wait:                  lateAtom("wait"),
		xor:                   lateAtom("xor"),
	}
	for i, d := range dataViewTypes {
		a.get[i] = lateAtom("get" + d.name)
		a.set[i] = lateAtom("set" + d.name)
	}
	return a
})

// textAtoms holds the property names of TextEncoder and TextDecoder.
type textAtoms struct {
	encoding, encode, encodeInto, decode *String
	fatal, ignoreBOM, stream             *String
	utf8                                 *String // "utf-8", the encoding getters' value
}

// textNames interns them once per process, as binaryNames does.
var textNames = sync.OnceValue(func() *textAtoms {
	return &textAtoms{
		encoding:   lateAtom("encoding"),
		encode:     lateAtom("encode"),
		encodeInto: lateAtom("encodeInto"),
		decode:     lateAtom("decode"),
		fatal:      lateAtom("fatal"),
		ignoreBOM:  lateAtom("ignoreBOM"),
		stream:     lateAtom("stream"),
		utf8:       asciiString("utf-8"),
	}
})

// lateAtom interns a builtin name after package init: the static atom of the
// name if another table has one, else the process-wide atom, which is what
// compiled code and hosts get for it (InternKey).
func lateAtom(name string) *String {
	if a, ok := staticAtoms[name]; ok {
		return a
	}
	return globalASCIIAtom(name)
}
