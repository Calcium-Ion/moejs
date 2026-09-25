package engine

// Array.fromAsync runs its closure as an async function would: the steps up
// to the first await run in the call, and a fromAsync, reacting to each
// awaited promise, runs the rest from the reaction jobs.

// fromAsync is the state of an Array.fromAsync call between awaits. An
// iterable is read through the iterator it (an async-from-sync iterator for
// a sync iterable) with next method next; anything else through arrayLike
// with length n.
type fromAsync struct {
	promise   *Object
	mapper    *Object
	thisArg   Value
	a         *Object
	it, next  Value
	arrayLike *Object
	n, k      int64
	// err is the error thrown once the iterator is closed.
	err   error
	phase uint8
}

// The value a fromAsync awaits.
const (
	fromAsyncNext    = iota // the result of next()
	fromAsyncElement        // arrayLike[k]
	fromAsyncMapped         // the mapper's result for element k
	fromAsyncClosing        // the result of return() while closing on err
)

// arrayFromAsync implements Array.fromAsync(asyncItems, mapfn, thisArg).
func arrayFromAsync(r *Realm, this Value, args []Value) (Value, error) {
	s := &fromAsync{promise: r.newPromise(), thisArg: Arg(args, 2)}
	if err := s.start(r, this, Arg(args, 0), Arg(args, 1)); err != nil {
		return r.rejectWith(s.promise, err)
	}
	return ObjectValue(s.promise), nil
}

// start runs the closure up to its first await: it picks how items is read
// and creates A with the constructor c.
func (s *fromAsync) start(r *Realm, c, items, mapFn Value) error {
	if !mapFn.IsUndefined() {
		if !IsCallable(mapFn) {
			return r.TypeError("%s is not a function", r.DisplayString(mapFn))
		}
		s.mapper = mapFn.AsObject()
	}
	m, err := r.GetMethod(items, asyncIteratorKey)
	if err != nil {
		return err
	}
	async := !m.IsUndefined()
	if !async {
		if m, err = r.GetMethod(items, iteratorKey); err != nil {
			return err
		}
	}
	if m.IsUndefined() {
		if s.arrayLike, err = r.ToObject(items); err != nil {
			return err
		}
		if s.n, err = r.LengthOfArrayLike(s.arrayLike); err != nil {
			return err
		}
		if s.a, _, err = arrayCreateFrom(r, c, s.n, true); err != nil {
			return err
		}
		return s.element(r)
	}
	if s.it, err = r.Call(m, items, nil); err != nil {
		return err
	}
	if !s.it.IsObject() {
		if async {
			return r.TypeError("Result of the Symbol.asyncIterator method is not an object")
		}
		return r.TypeError("Result of the Symbol.iterator method is not an object")
	}
	if s.next, err = s.it.AsObject().Get(r, nextKey, s.it); err != nil {
		return err
	}
	if !async {
		s.it, s.next = r.newAsyncFromSyncIterator(s.it, s.next), ObjectValue(r.asyncIntr().asyncFromSyncNext)
	}
	if s.a, _, err = arrayCreateFrom(r, c, 0, false); err != nil {
		return err
	}
	return s.step(r)
}

// promiseSettled resumes the closure with the awaited result v and rejects
// the promise with what it throws.
func (s *fromAsync) promiseSettled(r *Realm, v Value, rejected bool) error {
	if err := s.resume(r, v, rejected); err != nil {
		_, err = r.rejectWith(s.promise, err)
		return err
	}
	return nil
}

func (s *fromAsync) resume(r *Realm, v Value, rejected bool) error {
	if s.phase == fromAsyncClosing {
		// AsyncIteratorClose with a throw completion throws it whatever
		// return did.
		return s.err
	}
	if rejected {
		if s.phase == fromAsyncMapped {
			return s.abrupt(r, r.Throw(v))
		}
		return r.Throw(v)
	}
	switch s.phase {
	case fromAsyncNext:
		if !v.IsObject() {
			return r.TypeError("Iterator result %s is not an object", r.DisplayString(v))
		}
		done, err := v.AsObject().Get(r, StringKey(AtomDone), v)
		if err != nil {
			return err
		}
		if ToBoolean(done) {
			return s.finish(r)
		}
		if v, err = v.AsObject().Get(r, StringKey(AtomValue), v); err != nil {
			return err
		}
	case fromAsyncMapped:
		return s.add(r, v)
	}
	if s.mapper == nil {
		return s.add(r, v)
	}
	mapped, err := r.callStack(s.mapper, s.thisArg, v, Int64Value(s.k))
	if err == nil {
		s.phase = fromAsyncMapped
		err = s.await(r, mapped)
	}
	if err != nil {
		return s.abrupt(r, err)
	}
	return nil
}

// await implements Await(v): s resumes with v's settlement.
func (s *fromAsync) await(r *Realm, v Value) error {
	p, err := r.promiseResolve(v)
	if err != nil {
		return err
	}
	r.promiseReact(p, s)
	return nil
}

// step calls next and awaits its result.
func (s *fromAsync) step(r *Realm) error {
	res, err := r.Call(s.next, s.it, nil)
	if err != nil {
		return err
	}
	s.phase = fromAsyncNext
	return s.await(r, res)
}

// element awaits arrayLike[k], or finishes once k reaches the length.
func (s *fromAsync) element(r *Realm) error {
	if s.k >= s.n {
		return s.finish(r)
	}
	v, err := getIndex(r, s.arrayLike, s.k)
	if err != nil {
		return err
	}
	s.phase = fromAsyncElement
	return s.await(r, v)
}

// add defines element k of A as v and reads the next one.
func (s *fromAsync) add(r *Realm, v Value) error {
	if err := s.a.CreateDataPropertyOrThrow(r, indexKey(r, s.k), v); err != nil {
		return s.abrupt(r, err)
	}
	s.k++
	if s.arrayLike != nil {
		return s.element(r)
	}
	return s.step(r)
}

// finish sets A's length to k and resolves the promise with A.
func (s *fromAsync) finish(r *Realm) error {
	if err := setLength(r, s.a, s.k); err != nil {
		return err
	}
	return r.resolvePromiseErr(s.promise, ObjectValue(s.a))
}

// abrupt handles err thrown while element k is mapped or defined: an
// iterator is closed first (IfAbruptCloseAsyncIterator), awaiting what its
// return method returns, then err is thrown.
func (s *fromAsync) abrupt(r *Realm, err error) error {
	if _, ok := r.thrownValue(err); !ok || s.arrayLike != nil {
		return err
	}
	m, cerr := r.GetMethod(s.it, returnKey)
	if cerr == nil && !m.IsUndefined() {
		var res Value
		if res, cerr = r.Call(m, s.it, nil); cerr == nil {
			s.phase, s.err = fromAsyncClosing, err
			if cerr = s.await(r, res); cerr == nil {
				return nil
			}
		}
	}
	if _, ok := r.thrownValue(cerr); cerr != nil && !ok {
		return cerr
	}
	return err
}
