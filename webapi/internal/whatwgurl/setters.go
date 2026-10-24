package whatwgurl

// The setters of the URL API (URL Standard §6.1). Each one runs the
// standard's steps on u's record and keeps what they changed, also when the
// basic URL parser fails part way, as the standard does.

// cannotHaveUsernamePasswordPort reports whether r's host is null or empty,
// or its scheme is "file".
func (r *record) cannotHaveUsernamePasswordPort() bool {
	return !r.hasHost || r.host == "" || r.scheme == "file"
}

// update runs the basic URL parser on v with u's record and state override
// st, and makes the result u's record.
func (u *URL) update(r *record, v string, st state) {
	u.opts.basicParse(removeTabOrNewline(v), nil, r, st)
	u.set(r, u.href)
}

// SetProtocol runs the protocol setter.
func (u *URL) SetProtocol(v string) {
	r := u.record()
	u.update(&r, v+":", stateSchemeStart)
}

// SetUsername runs the username setter.
func (u *URL) SetUsername(v string) {
	r := u.record()
	if r.cannotHaveUsernamePasswordPort() {
		return
	}
	r.username = encode(v, userinfoSet)
	u.set(&r, u.href)
}

// SetPassword runs the password setter.
func (u *URL) SetPassword(v string) {
	r := u.record()
	if r.cannotHaveUsernamePasswordPort() {
		return
	}
	r.password = encode(v, userinfoSet)
	u.set(&r, u.href)
}

// SetHost runs the host setter.
func (u *URL) SetHost(v string) {
	r := u.record()
	if r.opaque {
		return
	}
	u.update(&r, v, stateHost)
}

// SetHostname runs the hostname setter.
func (u *URL) SetHostname(v string) {
	r := u.record()
	if r.opaque {
		return
	}
	u.update(&r, v, stateHostname)
}

// SetPort runs the port setter.
func (u *URL) SetPort(v string) {
	r := u.record()
	if r.cannotHaveUsernamePasswordPort() {
		return
	}
	if v == "" {
		r.port = ""
		u.set(&r, u.href)
		return
	}
	u.update(&r, v, statePort)
}

// SetPathname runs the pathname setter.
func (u *URL) SetPathname(v string) {
	r := u.record()
	if r.opaque {
		return
	}
	r.path = ""
	u.update(&r, v, statePathStart)
}

// SetSearch runs the search setter. The caller updates a URLSearchParams
// object from Query afterwards.
func (u *URL) SetSearch(v string) {
	r := u.record()
	if v == "" {
		r.query, r.hasQuery = "", false
		u.set(&r, u.href)
		return
	}
	if v[0] == '?' {
		v = v[1:]
	}
	r.query, r.hasQuery = "", true
	u.update(&r, v, stateQuery)
}

// SetHash runs the hash setter.
func (u *URL) SetHash(v string) {
	r := u.record()
	if v == "" {
		r.fragment, r.hasFrag = "", false
		u.set(&r, u.href)
		return
	}
	if v[0] == '#' {
		v = v[1:]
	}
	r.fragment, r.hasFrag = "", true
	u.update(&r, v, stateFragment)
}

// SetQuery sets u's query to q, or to null when !ok, as URLSearchParams's
// update steps do; q must be ASCII that needs no percent-encoding, such as
// AppendForm's output. (The URL Standard no longer strips trailing spaces
// from an opaque path here: the parser percent-encodes a space before a
// query or fragment instead.)
func (u *URL) SetQuery(q string, ok bool) {
	r := u.record()
	r.query, r.hasQuery = q, ok
	u.set(&r, u.href)
}
