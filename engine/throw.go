package engine

// ThrownValue returns the value a catch sees when a native function returns
// err (NativeFunc): an *Exception's value, and for any other error an Error
// whose message is err.Error() and which keeps err for Exception.Unwrap. ok
// is false for nil and for an *InterruptedError, which no catch sees. It
// runs no user code.
func (r *Realm) ThrownValue(err error) (v Value, ok bool) {
	if err == nil {
		return Undefined(), false
	}
	return r.thrownValue(err)
}
