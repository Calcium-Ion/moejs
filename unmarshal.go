package moejs

import "encoding/json"

// Unmarshal stores v in the value target points to as json.Unmarshal stores
// the text AppendJSON writes for v, and returns the error json.Unmarshal (or
// AppendJSON) would. A value made of plain objects, arrays, strings,
// numbers, booleans and null, and arguments FromGo converted that it
// returns untouched (in an `any` of target), goes into target without the
// text: no JSON is written or parsed, and strings are not copied (the Go
// string of an ASCII string is the one v holds, as with ToGo). Anything else
// — a toJSON method, a getter, a proxy, a Date, a target type that
// unmarshals itself, a value target's type rejects — makes it fall back to
// AppendJSON and json.Unmarshal, with their result and error; JavaScript
// code then runs as AppendJSON runs it. When AppendJSON would fail (a
// BigInt, a cycle, a Go value it cannot write, a text past the string length
// limit), target is left as it was: v is checked, its Go values included,
// before anything is written, and a value whose text could pass the limit
// (counting six bytes a character of most strings) goes through the round
// trip. An interrupt is observed every 4096 values, as AppendJSON observes
// it, in the check and again in the store; an argument copied into an
// `any` counts a value per 4096 bytes of each of its Go strings, whether a
// string itself or in a map[string]string, a []string or another of its
// containers. One pending at the call fails a large value before anything
// is written, and not a small one. AppendJSON also observes one inside a
// long JavaScript string it escapes, which Unmarshal, copying no string,
// does not. After an error of json.Unmarshal (a value target's type
// rejects) or an interrupt, target may hold part of v, as with
// json.Unmarshal. A string of a value ParseJSON produced may share the
// parsed text and keep it alive for as long as the host holds it:
// strings.Clone a string kept beyond the request.
func (rt *Runtime) Unmarshal(v Value, target any) (err error) {
	ok, err := rt.realm.Unmarshal(v, target)
	if ok || err != nil {
		return err
	}
	data, err := rt.AppendJSON(nil, v)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

// ToGoInto stores v in the value target points to as json.Unmarshal stores
// the text json.Marshal writes for what ToGo returns for v, and returns the
// error ToGo, json.Marshal or json.Unmarshal would. A value made of
// ordinary objects (class instances and objects with a null prototype
// included), arrays, strings, numbers, booleans, null and undefined, and
// arguments FromGo converted that it returns untouched (in an `any` of
// target), goes into target without the round trip: no Go map or slice of
// ToGo is built, no JSON is written or parsed, and strings are not copied.
// Anything else (a getter, a proxy, a Date, a Map, a typed array, a BigInt,
// a function, a target type that unmarshals itself, a value target's type
// rejects) makes it fall back to ToGo, json.Marshal and json.Unmarshal,
// with their result and error. When ToGo or json.Marshal fails (a getter
// that throws, a NaN or an infinity, a cycle), target is left as it was.
// After an error of json.Unmarshal, target may hold part of v, as with
// json.Unmarshal.
//
// The result differs from Unmarshal's where ToGo's Go value differs from
// AppendJSON's text: a member whose value is undefined is kept, as null;
// -0 stays -0; a NaN or an infinity is json.Marshal's error; toJSON is not
// called; an argument FromGo converted that JavaScript has not modified is
// its Go value as json.Marshal writes it (a nil slice as null). No
// interrupt is observed, as ToGo observes none for such a value. A string
// of a value ParseJSON produced may share the parsed text and keep it alive
// for as long as the host holds it: strings.Clone a string kept beyond the
// request.
func (rt *Runtime) ToGoInto(v Value, target any) error {
	if rt.realm.ToGoInto(v, target) {
		return nil
	}
	g, err := rt.ToGo(v)
	if err != nil {
		return err
	}
	data, err := json.Marshal(g)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}
