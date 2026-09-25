package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errorKinds = []struct {
	kind ErrorKind
	name string
}{
	{KindError, "Error"}, {KindTypeError, "TypeError"}, {KindRangeError, "RangeError"}, {KindSyntaxError, "SyntaxError"},
	{KindReferenceError, "ReferenceError"}, {KindEvalError, "EvalError"}, {KindURIError, "URIError"},
}

func TestErrorFamilyShape(t *testing.T) {
	r := NewRealm()
	for _, k := range errorKinds {
		ctor := r.ErrorConstructorFor(k.kind)
		proto := r.ErrorPrototypeFor(k.kind)
		cv := ObjectValue(ctor)
		assertNativeShape(t, r, cv, k.name, 1)
		assert.Same(t, ctor, jsGlobal(t, r, k.name).AsObject())
		assert.Same(t, proto, jsGet(t, r, cv, "prototype").AsObject())
		assertOwnAttrs(t, r, ctor, "prototype", false, false, false)
		assert.Same(t, ctor, jsGet(t, r, ObjectValue(proto), "constructor").AsObject())
		assertOwnAttrs(t, r, proto, "constructor", true, false, true)
		assert.Equal(t, k.name, jsString(t, jsGet(t, r, ObjectValue(proto), "name")))
		assert.Equal(t, "", jsString(t, jsGet(t, r, ObjectValue(proto), "message")))
		assertOwnAttrs(t, r, proto, "name", true, false, true)
		assertOwnAttrs(t, r, proto, "message", true, false, true)
		assert.Equal(t, ClassObject, proto.Class(), "prototypes are ordinary objects, not Error instances")
		protoTag, err := r.Call(jsGet(t, r, ObjectValue(r.ObjectPrototype), "toString"), ObjectValue(proto), nil)
		require.NoError(t, err)
		assert.Equal(t, "[object Object]", jsString(t, protoTag))
		if k.kind == KindError {
			assert.Same(t, r.ObjectPrototype, proto.Proto())
			assert.Same(t, r.FunctionPrototype, ctor.Proto())
			assert.True(t, proto.HasOwnProperty(StringKey(AtomToString)))
		} else {
			assert.Same(t, r.ErrorPrototype, proto.Proto())
			assert.Same(t, r.ErrorConstructorFor(KindError), ctor.Proto(), "NativeError constructors inherit from Error")
			assert.False(t, proto.HasOwnProperty(StringKey(AtomToString)), "toString is inherited from Error.prototype")
		}
		// Callable with and without new; both yield the same kind of object.
		called, err := r.Call(cv, Undefined(), []Value{str("m")})
		require.NoError(t, err)
		constructed, err := r.Construct(cv, []Value{str("m")}, nil)
		require.NoError(t, err)
		for _, e := range []Value{called, constructed} {
			eo := e.AsObject()
			assert.Equal(t, ClassError, eo.Class())
			assert.Same(t, proto, eo.Proto())
			assert.Equal(t, []string{"message", "stack"}, keyNames(eo.OwnPropertyKeys()))
			assert.Equal(t, "object", TypeOf(e).GoString())
			ok, err := r.InstanceOf(e, cv)
			require.NoError(t, err)
			assert.True(t, ok)
			ok, err = r.InstanceOf(e, ObjectValue(r.ErrorConstructorFor(KindError)))
			require.NoError(t, err)
			assert.True(t, ok, "every kind is an instanceof Error")
			tag, err := r.Call(jsGet(t, r, ObjectValue(r.ObjectPrototype), "toString"), e, nil)
			require.NoError(t, err)
			assert.Equal(t, "[object Error]", jsString(t, tag))
			assert.Equal(t, k.name+": m", jsString(t, jsCall(t, r, e, "toString")))
		}
	}
	plainErr, _ := r.Construct(ObjectValue(r.ErrorConstructorFor(KindError)), nil, nil)
	ok, _ := r.InstanceOf(plainErr, ObjectValue(r.ErrorConstructorFor(KindTypeError)))
	assert.False(t, ok)
}

func TestErrorConstructorSemantics(t *testing.T) {
	r := NewRealm()
	ctor := ObjectValue(r.ErrorConstructorFor(KindTypeError))
	mk := func(args ...Value) *Object {
		v, err := r.Construct(ctor, args, nil)
		require.NoError(t, err)
		return v.AsObject()
	}
	ownMessage := func(e *Object) (string, bool) {
		v, ok := e.GetOwnDataValue(StringKey(AtomMessage))
		if !ok {
			return "", false
		}
		return v.AsString().GoString(), true
	}
	// Message coercion.
	for _, c := range []struct {
		arg  Value
		want string
	}{{str("m"), "m"}, {IntValue(42), "42"}, {Null(), "null"}, {True(), "true"}, {jsArray(r, IntValue(1), IntValue(2)), "1,2"}, {str(""), ""}} {
		msg, ok := ownMessage(mk(c.arg))
		assert.True(t, ok, c.arg.String())
		assert.Equal(t, c.want, msg, c.arg.String())
	}
	for _, e := range []*Object{mk(), mk(Undefined())} {
		_, ok := ownMessage(e)
		assert.False(t, ok, "undefined message leaves no own property")
		assert.Equal(t, []string{"stack"}, keyNames(e.OwnPropertyKeys()))
		assert.Equal(t, "", jsString(t, jsGet(t, r, ObjectValue(e), "message")), "message is inherited")
	}
	withToString := r.NewObject()
	withToString.DefineOwnDataFast(r, StringKey(AtomToString), nativeFn(r, func(Value, []Value) (Value, error) { return str("custom"), nil }), attrHidden)
	msg, _ := ownMessage(mk(ObjectValue(withToString)))
	assert.Equal(t, "custom", msg)
	_, err := r.Construct(ctor, []Value{SymbolValue(SymIterator)}, nil)
	assert.ErrorContains(t, err, "Cannot convert a Symbol value to a string")
	e := mk(str("m"))
	assertOwnAttrs(t, r, e, "message", true, false, true)
	assert.Empty(t, e.OwnEnumerableStringKeys())
	assert.Equal(t, map[string]any{}, r.ToGo(ObjectValue(e)), "an error has no enumerable own properties to export")

	// options.cause.
	causeOf := func(e *Object) (Value, bool) { return e.GetOwnDataValue(StringKey(AtomCause)) }
	_, ok := causeOf(mk(str("m"), ObjectValue(r.NewObject())))
	assert.False(t, ok, "options without cause installs nothing")
	for _, opts := range []Value{str("cause"), IntValue(1), Null(), Undefined(), True()} {
		_, ok := causeOf(mk(str("m"), opts))
		assert.False(t, ok, "non-object options are ignored: %s", opts.String())
	}
	opts := r.NewObject()
	mustSet(t, r, opts, "cause", Undefined())
	c, ok := causeOf(mk(str("m"), ObjectValue(opts)))
	assert.True(t, ok, "an explicit undefined cause is installed")
	assert.True(t, c.IsUndefined())
	mustSet(t, r, opts, "cause", str("root"))
	withCause := mk(str("m"), ObjectValue(opts))
	c, _ = causeOf(withCause)
	assert.Equal(t, "root", jsString(t, c))
	assertOwnAttrs(t, r, withCause, "cause", true, false, true)
	assert.Equal(t, []string{"message", "stack", "cause"}, keyNames(withCause.OwnPropertyKeys()))
	inherited := r.NewObjectWithProto(opts)
	c, ok = causeOf(mk(str("m"), ObjectValue(inherited)))
	assert.True(t, ok, "HasProperty walks the prototype chain")
	assert.Equal(t, "root", jsString(t, c))
	var log []string
	getterOpts := r.NewObject()
	getterOpts.DefineOwnAccessorFast(r, StringKey(AtomCause), nativeFn(r, func(Value, []Value) (Value, error) {
		log = append(log, "cause")
		return IntValue(7), nil
	}).AsObject(), nil, attrConfigurable)
	msgObj := r.NewObject()
	msgObj.DefineOwnDataFast(r, StringKey(AtomToString), nativeFn(r, func(Value, []Value) (Value, error) {
		log = append(log, "message")
		return str("m"), nil
	}), attrHidden)
	c, _ = causeOf(mk(ObjectValue(msgObj), ObjectValue(getterOpts)))
	assert.Equal(t, IntValue(7), c)
	assert.Equal(t, []string{"message", "cause"}, log, "message is coerced before the cause is read")
	throwing := r.NewObject()
	throwing.DefineOwnAccessorFast(r, StringKey(AtomCause), throwingFn(r, "cause getter").AsObject(), nil, attrConfigurable)
	_, err = r.Construct(ctor, []Value{str("m"), ObjectValue(throwing)}, nil)
	assert.EqualError(t, err, "TypeError: cause getter")

	// newTarget with its own prototype (class extends Error shape).
	nt := r.NewNativeFunction(AtomEmpty, 0, nil)
	sub := r.NewObjectWithProto(r.ErrorPrototypeFor(KindTypeError))
	mustSet(t, r, sub, "name", str("MyError"))
	nt.DefineOwnDataFast(r, StringKey(AtomPrototype), ObjectValue(sub), attrHidden)
	v, err := r.Construct(ctor, []Value{str("x")}, nt)
	require.NoError(t, err)
	assert.Same(t, sub, v.AsObject().Proto())
	assert.Equal(t, "MyError: x", jsString(t, jsCall(t, r, v, "toString")))
	ok, _ = r.InstanceOf(v, ctor)
	assert.True(t, ok)
	badNT := r.NewNativeFunction(AtomEmpty, 0, nil)
	badNT.DefineOwnDataFast(r, StringKey(AtomPrototype), IntValue(1), attrHidden)
	v, err = r.Construct(ctor, nil, badNT)
	require.NoError(t, err)
	assert.Same(t, r.ErrorPrototypeFor(KindTypeError), v.AsObject().Proto(), "non-object prototype falls back to the intrinsic")
}

func TestErrorPrototypeToString(t *testing.T) {
	r := NewRealm()
	toString := jsGet(t, r, ObjectValue(r.ErrorPrototype), "toString")
	assertNativeShape(t, r, toString, "toString", 0)
	mkObj := func(fields map[string]Value) Value {
		o := r.NewObject()
		for k, v := range fields {
			mustSet(t, r, o, k, v)
		}
		return ObjectValue(o)
	}
	cases := []struct {
		this Value
		want string
	}{
		{mkObj(map[string]Value{"name": str("N"), "message": str("m")}), "N: m"},
		{mkObj(map[string]Value{"name": str(""), "message": str("m")}), "m"},
		{mkObj(map[string]Value{"name": str("N"), "message": str("")}), "N"},
		{mkObj(map[string]Value{"name": str(""), "message": str("")}), ""},
		{mkObj(map[string]Value{"message": str("m")}), "Error: m"},
		{mkObj(map[string]Value{"name": Undefined(), "message": Undefined()}), "Error"},
		{mkObj(map[string]Value{"name": IntValue(5), "message": IntValue(6)}), "5: 6"},
		{mkObj(map[string]Value{"name": Null(), "message": Null()}), "null: null"},
		{mkObj(nil), "Error"},
		{ObjectValue(r.ErrorPrototype), "Error"},
		{ObjectValue(r.ErrorPrototypeFor(KindRangeError)), "RangeError"},
		{ObjectValue(r.NewError(KindTypeError, "bad")), "TypeError: bad"},
		{ObjectValue(r.NewError(KindError, "")), "Error"},
		{ObjectValue(r.NewError(KindURIError, "é")), "URIError: é"},
	}
	for _, c := range cases {
		res, err := r.Call(toString, c.this, nil)
		require.NoError(t, err)
		assert.Equal(t, c.want, jsString(t, res))
	}
	for _, bad := range []Value{Undefined(), Null(), IntValue(1), str("Error")} {
		_, err := r.Call(toString, bad, nil)
		assert.EqualError(t, err, "TypeError: Error.prototype.toString requires that 'this' be an Object", bad.String())
	}
	// name is read (and coerced) before message; getter errors propagate.
	var log []string
	o := r.NewObject()
	o.DefineOwnAccessorFast(r, StringKey(AtomName), nativeFn(r, func(Value, []Value) (Value, error) {
		log = append(log, "name")
		return str("N"), nil
	}).AsObject(), nil, attrConfigurable)
	o.DefineOwnAccessorFast(r, StringKey(AtomMessage), nativeFn(r, func(Value, []Value) (Value, error) {
		log = append(log, "message")
		return str("m"), nil
	}).AsObject(), nil, attrConfigurable)
	res, err := r.Call(toString, ObjectValue(o), nil)
	require.NoError(t, err)
	assert.Equal(t, "N: m", jsString(t, res))
	assert.Equal(t, []string{"name", "message"}, log)
	bad := r.NewObject()
	bad.DefineOwnAccessorFast(r, StringKey(AtomName), throwingFn(r, "name getter").AsObject(), nil, attrConfigurable)
	_, err = r.Call(toString, ObjectValue(bad), nil)
	assert.EqualError(t, err, "TypeError: name getter")
	_, err = r.Call(toString, mkObj(map[string]Value{"name": SymbolValue(SymIterator)}), nil)
	assert.ErrorContains(t, err, "Symbol")
	// Instances resolve name through the prototype chain and own overrides.
	e := r.NewError(KindRangeError, "r")
	assert.Equal(t, "RangeError: r", jsString(t, jsCall(t, r, ObjectValue(e), "toString")))
	mustSet(t, r, e, "name", str("Renamed"))
	assert.Equal(t, "Renamed: r", jsString(t, jsCall(t, r, ObjectValue(e), "toString")))
	s, err := r.ToString(ObjectValue(e))
	require.NoError(t, err)
	assert.Equal(t, "Renamed: r", s.GoString())
}

func TestErrorStackString(t *testing.T) {
	r := NewRealm()
	e := r.NewError(KindTypeError, "boom")
	desc, ok := e.GetOwnProperty(StringKey(AtomStack))
	require.True(t, ok)
	assert.True(t, desc.IsAccessorDescriptor())
	assert.False(t, desc.Enumerable())
	assert.True(t, desc.Configurable())
	assert.Equal(t, "TypeError: boom", jsString(t, jsGet(t, r, ObjectValue(e), "stack")), "without interpreter hooks the stack is the header")
	assert.Equal(t, []any{}, r.ToGo(jsCall(t, r, ObjectValue(r.ObjectCtor), "keys", ObjectValue(e))))
	r.SetStackHooks(
		func(r *Realm) []StackFrame {
			return []StackFrame{{PC: 3}, {PC: 9}}
		},
		func(r *Realm, frames []StackFrame) string {
			return "    at decode (plugin.js:12:5)\n    at <anonymous> (plugin.js:40:1)"
		},
	)
	v, err := r.Construct(ObjectValue(r.ErrorConstructorFor(KindError)), []Value{str("hook fired")}, nil)
	require.NoError(t, err)
	assert.Equal(t, "Error: hook fired\n    at decode (plugin.js:12:5)\n    at <anonymous> (plugin.js:40:1)", jsString(t, jsGet(t, r, v, "stack")))
	// The header is formatted at the first read, so renames before that are
	// reflected; later renames are not (the string is cached).
	late, _ := r.Construct(ObjectValue(r.ErrorConstructorFor(KindError)), []Value{str("m")}, nil)
	mustSet(t, r, late.AsObject(), "name", str("Custom"))
	assert.Equal(t, "Custom: m\n    at decode (plugin.js:12:5)\n    at <anonymous> (plugin.js:40:1)", jsString(t, jsGet(t, r, late, "stack")))
	mustSet(t, r, late.AsObject(), "name", str("Again"))
	assert.Equal(t, "Custom: m\n    at decode (plugin.js:12:5)\n    at <anonymous> (plugin.js:40:1)", jsString(t, jsGet(t, r, late, "stack")))
	// Assignment and deletion behave like a data property would.
	require.NoError(t, late.AsObject().SetProp(r, StringKey(AtomStack), str("mine")))
	assert.Equal(t, "mine", jsString(t, jsGet(t, r, late, "stack")))
	assert.Equal(t, True(), jsCall(t, r, late, "hasOwnProperty", str("stack")))
	assert.True(t, late.AsObject().Delete(r, StringKey(AtomStack)))
	assert.True(t, jsGet(t, r, late, "stack").IsUndefined())
	// Exceptions raised by natives are proper TypeError instances.
	err = r.TypeError("native %s", "failure")
	var exc *Exception
	require.ErrorAs(t, err, &exc)
	ok, _ = r.InstanceOf(exc.Value, ObjectValue(r.ErrorConstructorFor(KindTypeError)))
	assert.True(t, ok)
	assert.Equal(t, "TypeError: native failure", exc.Error())
	assert.Equal(t, "TypeError: native failure\n    at decode (plugin.js:12:5)\n    at <anonymous> (plugin.js:40:1)", jsString(t, jsGet(t, r, exc.Value, "stack")))
}

func TestErrorAllocations(t *testing.T) {
	r := NewRealm()
	ctor := ObjectValue(r.ErrorConstructorFor(KindError))
	msg := []Value{str("message")}
	allocs := testing.AllocsPerRun(200, func() {
		if _, err := r.Construct(ctor, msg, nil); err != nil {
			t.Fatal(err)
		}
	})
	assert.Equal(t, 1.0, allocs, "new Error(msg) is one allocation (object, payload and slots co-allocated)")
	opts := r.NewObject()
	mustSet(t, r, opts, "cause", IntValue(1))
	withCause := []Value{str("message"), ObjectValue(opts)}
	allocs = testing.AllocsPerRun(200, func() {
		if _, err := r.Construct(ctor, withCause, nil); err != nil {
			t.Fatal(err)
		}
	})
	assert.LessOrEqual(t, allocs, 2.0, "cause adds at most the slot growth")
}

func TestErrorIsError(t *testing.T) {
	accTable(t, [][2]string{
		{`return [Error.isError(new Error), Error.isError(TypeError("x")), Error.isError(new RangeError),
		    Error.isError(new URIError), Error.isError(new Error("m", {cause: 1}))];`, `[true,true,true,true,true]`},
		{`try { null.x; } catch (e) { return Error.isError(e); }`, `true`},
		{`return [Error.isError(Error.prototype), Error.isError(TypeError.prototype),
		    Error.isError(Object.create(Error.prototype)), Error.isError({name: "Error", message: "m", stack: ""})];`,
			`[false,false,false,false]`},
		{`var e = new Error; Object.setPrototypeOf(e, null); return Error.isError(e);`, `true`},
		{`return [Error.isError(), Error.isError(undefined), Error.isError(null), Error.isError(0),
		    Error.isError("Error"), Error.isError(Error), Error.isError([])];`, `[false,false,false,false,false,false,false]`},
		{`return [TypeError.isError === Error.isError, Object.hasOwn(TypeError, "isError"),
		    Error.isError.length, Error.isError.name];`, `[true,false,1,"isError"]`},
		{`var d = Object.getOwnPropertyDescriptor(Error, "isError");
		  return [d.enumerable, typeof d.value];`, `[false,"function"]`},
		{`try { new Error.isError(); } catch (e) { return e.name; }`, `"TypeError"`},
	})
}
