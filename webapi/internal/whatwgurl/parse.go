package whatwgurl

import "strings"

// state is a state of the basic URL parser.
type state uint8

const (
	stateNone state = iota
	stateSchemeStart
	stateScheme
	stateNoScheme
	stateSpecialRelativeOrAuthority
	statePathOrAuthority
	stateRelative
	stateRelativeSlash
	stateSpecialAuthoritySlashes
	stateSpecialAuthorityIgnoreSlashes
	stateAuthority
	stateHost
	stateHostname
	statePort
	stateFile
	stateFileSlash
	stateFileHost
	statePathStart
	statePath
	stateOpaquePath
	stateQuery
	stateFragment
)

// The bytes that end a run of bytes a state only adds to its buffer, as
// bits of stops.
const (
	stopScheme    uint8 = 1 << iota // no ASCII alphanumeric, +, - or .
	stopAuthority                   // @ / ? # \
	stopHost                        // : / ? # \ [ ]
	stopPort                        // no ASCII digit
	stopPath                        // / ? # \
)

var stops = func() (t [256]uint8) {
	for b := range 256 {
		if !(isAlpha(b) || '0' <= b && b <= '9' || b == '+' || b == '-' || b == '.') {
			t[b] |= stopScheme
		}
		if b < '0' || b > '9' {
			t[b] |= stopPort
		}
	}
	for _, b := range "@/?#\\" {
		t[b] |= stopAuthority
	}
	for _, b := range ":/?#\\[]" {
		t[b] |= stopHost
	}
	for _, b := range "/?#\\" {
		t[b] |= stopPath
	}
	return t
}()

// scan returns the index of the first byte from p on in stop, or len(in).
func scan(in string, p int, stop uint8) int {
	for p < len(in) && stops[in[p]]&stop == 0 {
		p++
	}
	return p
}

// acc builds a component. While it copies the input in order it is a slice
// of the input, so a component that needs no change costs no allocation;
// otherwise it spills into a buffer.
type acc struct {
	s     string // the value, when !inBuf
	end   int    // s == in[end-len(s):end] when end >= 0
	buf   []byte
	inBuf bool
}

func (a *acc) set(s string) {
	a.s, a.end, a.inBuf, a.buf = s, -1, false, a.buf[:0]
}

func (a *acc) value() string {
	if a.inBuf {
		return string(a.buf)
	}
	return a.s
}

func (a *acc) len() int {
	if a.inBuf {
		return len(a.buf)
	}
	return len(a.s)
}

func (a *acc) at(i int) byte {
	if a.inBuf {
		return a.buf[i]
	}
	return a.s[i]
}

func (a *acc) truncate(n int) {
	if a.inBuf {
		a.buf = a.buf[:n]
		return
	}
	if a.end >= 0 {
		a.end -= len(a.s) - n
	}
	a.s = a.s[:n]
}

func (a *acc) spill() {
	if !a.inBuf {
		a.buf = append(a.buf[:0], a.s...)
		a.inBuf = true
	}
}

// appendInput appends in[i:j].
func (a *acc) appendInput(in string, i, j int) {
	if i == j {
		return
	}
	if !a.inBuf {
		if a.s == "" {
			a.s, a.end = in[i:j], j
			return
		}
		if a.end == i {
			a.s, a.end = in[i-len(a.s):j], j
			return
		}
		a.spill()
	}
	a.buf = append(a.buf, in[i:j]...)
}

func (a *acc) appendString(s string) {
	if s == "" {
		return
	}
	if !a.inBuf {
		if a.s == "" {
			a.s, a.end = s, -1
			return
		}
		a.spill()
	}
	a.buf = append(a.buf, s...)
}

// appendEncoded appends in[i:j], UTF-8 percent-encoding the bytes in set.
func (a *acc) appendEncoded(in string, i, j int, set uint8) {
	k := i
	for x := i; x < j; x++ {
		if b := in[x]; pctSets[b]&set != 0 {
			a.appendInput(in, k, x)
			a.spill()
			a.buf = append(a.buf, '%', upperHex[b>>4], upperHex[b&15])
			k = x + 1
		}
	}
	a.appendInput(in, k, j)
}

// parser holds the state of a run of the basic URL parser. The URL record
// and the base are parameters of its methods rather than fields: the
// buffers of the accumulators escape, and a pointer next to them would make
// the record escape with them.
type parser struct {
	o          *Options
	in         string
	override   state
	user, pass acc
	path       acc
}

// basicParse runs the basic URL parser (URL Standard §4.4) on in, which
// holds no ASCII tab or newline, against base (nil: none) into url, starting
// in state override (stateNone: no state override). It reports false for
// failure. With a state override, url keeps what the parser changed before
// it returned or failed, as the standard's setters do.
func (o *Options) basicParse(in string, base, url *record, override state) bool {
	ps := parser{o: o, in: in, override: override}
	ps.user.set(url.username)
	ps.pass.set(url.password)
	ps.path.set(url.path)
	ok := ps.run(url, base)
	url.username, url.password, url.path = ps.user.value(), ps.pass.value(), ps.path.value()
	return ok
}

func (ps *parser) run(url, base *record) bool {
	in, override := ps.in, ps.override
	special := isSpecial(url.scheme)
	st := override
	if st == stateNone {
		st = stateSchemeStart
	}
	var atSignSeen, insideBrackets, passwordTokenSeen bool
	// The buffer of the standard is in[bufStart:p], but in the path state,
	// whose segment is in[segStart:p].
	bufStart, segStart := 0, 0
	for p := 0; ; p++ {
		c := -1
		if p < len(in) {
			c = int(in[p])
		}
		switch st {
		case stateSchemeStart:
			if isAlpha(c) {
				st, bufStart = stateScheme, p
			} else if override == stateNone {
				st = stateNoScheme
				p--
			} else {
				return false
			}

		case stateScheme:
			if p = scan(in, p, stopScheme); p < len(in) {
				c = int(in[p])
			} else {
				c = -1
			}
			if isAlpha(c) || '0' <= c && c <= '9' || c == '+' || c == '-' || c == '.' {
				break
			}
			if c != ':' {
				if override != stateNone {
					return false
				}
				st, p = stateNoScheme, -1
				break
			}
			scheme := lowerScheme(in[bufStart:p])
			if override != stateNone {
				if isSpecial(url.scheme) != isSpecial(scheme) {
					return true
				}
				if (ps.user.len() > 0 || ps.pass.len() > 0 || url.port != "") && scheme == "file" {
					return true
				}
				if url.scheme == "file" && url.hasHost && url.host == "" {
					return true
				}
			}
			url.scheme = scheme
			special = isSpecial(scheme)
			if override != stateNone {
				if url.port != "" && url.port == defaultPort(scheme) {
					url.port = ""
				}
				return true
			}
			switch {
			case scheme == "file":
				st = stateFile
			case special && base != nil && base.scheme == scheme:
				st = stateSpecialRelativeOrAuthority
			case special:
				st = stateSpecialAuthoritySlashes
			case p+1 < len(in) && in[p+1] == '/':
				st = statePathOrAuthority
				p++
			default:
				url.opaque = true
				ps.path.set("")
				st = stateOpaquePath
			}

		case stateNoScheme:
			switch {
			case base == nil || base.opaque && c != '#':
				return false
			case base.opaque:
				url.scheme = base.scheme
				special = isSpecial(url.scheme)
				ps.path.set(base.path)
				url.opaque = true
				url.query, url.hasQuery = base.query, base.hasQuery
				url.fragment, url.hasFrag = "", true
				st, bufStart = stateFragment, p+1
			case base.scheme != "file":
				st = stateRelative
				p--
			default:
				st = stateFile
				p--
			}

		case stateSpecialRelativeOrAuthority:
			if c == '/' && p+1 < len(in) && in[p+1] == '/' {
				st = stateSpecialAuthorityIgnoreSlashes
				p++
			} else {
				st = stateRelative
				p--
			}

		case statePathOrAuthority:
			if c == '/' {
				st, bufStart = stateAuthority, p+1
			} else {
				st = statePath
				p--
				segStart = p + 1
			}

		case stateRelative:
			url.scheme = base.scheme
			special = isSpecial(url.scheme)
			if c == '/' || special && c == '\\' {
				st = stateRelativeSlash
				break
			}
			ps.user.set(base.username)
			ps.pass.set(base.password)
			url.host, url.hasHost, url.port = base.host, base.hasHost, base.port
			ps.path.set(base.path)
			url.query, url.hasQuery = base.query, base.hasQuery
			switch c {
			case '?':
				url.query, url.hasQuery = "", true
				st, bufStart = stateQuery, p+1
			case '#':
				url.fragment, url.hasFrag = "", true
				st, bufStart = stateFragment, p+1
			case -1:
			default:
				url.query, url.hasQuery = "", false
				ps.shorten(url)
				st = statePath
				p--
				segStart = p + 1
			}

		case stateRelativeSlash:
			if special && (c == '/' || c == '\\') {
				st = stateSpecialAuthorityIgnoreSlashes
			} else if c == '/' {
				st, bufStart = stateAuthority, p+1
			} else {
				ps.user.set(base.username)
				ps.pass.set(base.password)
				url.host, url.hasHost, url.port = base.host, base.hasHost, base.port
				st = statePath
				p--
				segStart = p + 1
			}

		case stateSpecialAuthoritySlashes:
			st = stateSpecialAuthorityIgnoreSlashes
			if c == '/' && p+1 < len(in) && in[p+1] == '/' {
				p++
			} else {
				p--
			}

		case stateSpecialAuthorityIgnoreSlashes:
			if c != '/' && c != '\\' {
				st = stateAuthority
				p--
				bufStart = p + 1
			}

		case stateAuthority:
			if p = scan(in, p, stopAuthority); p < len(in) {
				c = int(in[p])
			} else {
				c = -1
			}
			if c == '@' {
				if atSignSeen {
					if passwordTokenSeen {
						ps.pass.appendString("%40")
					} else {
						ps.user.appendString("%40")
					}
				}
				atSignSeen = true
				i := bufStart
				if !passwordTokenSeen {
					if k := strings.IndexByte(in[bufStart:p], ':'); k >= 0 {
						ps.user.appendEncoded(in, bufStart, bufStart+k, userinfoSet)
						passwordTokenSeen = true
						i = bufStart + k + 1
					} else {
						ps.user.appendEncoded(in, bufStart, p, userinfoSet)
						i = p
					}
				}
				ps.pass.appendEncoded(in, i, p, userinfoSet)
				bufStart = p + 1
			} else if c == -1 || c == '/' || c == '?' || c == '#' || special && c == '\\' {
				if atSignSeen && bufStart == p {
					return false
				}
				st, p = stateHost, bufStart-1
			}

		case stateHost, stateHostname:
			if override != stateNone && url.scheme == "file" {
				st = stateFileHost
				p--
				bufStart = p + 1
				break
			}
			if p = scan(in, p, stopHost); p < len(in) {
				c = int(in[p])
			} else {
				c = -1
			}
			if c == ':' && !insideBrackets {
				if bufStart == p || override == stateHostname {
					return false
				}
				h, ok := ps.o.parseHost(in[bufStart:p], !special)
				if !ok {
					return false
				}
				url.host, url.hasHost = h, true
				st, bufStart = statePort, p+1
			} else if c == -1 || c == '/' || c == '?' || c == '#' || special && c == '\\' {
				end := p
				p--
				if special && bufStart == end {
					return false
				}
				if override != stateNone && bufStart == end && (ps.user.len() > 0 || ps.pass.len() > 0 || url.port != "") {
					return false
				}
				h, ok := ps.o.parseHost(in[bufStart:end], !special)
				if !ok {
					return false
				}
				url.host, url.hasHost = h, true
				st = statePathStart
				if override != stateNone {
					return true
				}
			} else if c == '[' {
				insideBrackets = true
			} else if c == ']' {
				insideBrackets = false
			}

		case statePort:
			if p = scan(in, p, stopPort); p < len(in) {
				c = int(in[p])
			} else {
				c = -1
			}
			if '0' <= c && c <= '9' {
				break
			}
			if c == -1 || c == '/' || c == '?' || c == '#' || special && c == '\\' || override != stateNone {
				if bufStart != p {
					port, ok := canonicalPort(in[bufStart:p])
					if !ok {
						return false
					}
					if port == defaultPort(url.scheme) {
						port = ""
					}
					url.port = port
					bufStart = p
					if override != stateNone {
						return true
					}
				}
				if override != stateNone {
					return false
				}
				st = statePathStart
				p--
			} else {
				return false
			}

		case stateFile:
			url.scheme = "file"
			special = true
			url.host, url.hasHost = "", true
			if c == '/' || c == '\\' {
				st = stateFileSlash
			} else if base != nil && base.scheme == "file" {
				url.host, url.hasHost = base.host, base.hasHost
				ps.path.set(base.path)
				url.query, url.hasQuery = base.query, base.hasQuery
				switch c {
				case '?':
					url.query, url.hasQuery = "", true
					st, bufStart = stateQuery, p+1
				case '#':
					url.fragment, url.hasFrag = "", true
					st, bufStart = stateFragment, p+1
				case -1:
				default:
					url.query, url.hasQuery = "", false
					if !startsWithWindowsDriveLetter(in[p:]) {
						ps.shorten(url)
					} else {
						ps.path.set("")
					}
					st = statePath
					p--
					segStart = p + 1
				}
			} else {
				st = statePath
				p--
				segStart = p + 1
			}

		case stateFileSlash:
			if c == '/' || c == '\\' {
				st, bufStart = stateFileHost, p+1
				break
			}
			if base != nil && base.scheme == "file" {
				url.host, url.hasHost = base.host, base.hasHost
				bp := base.path
				if !startsWithWindowsDriveLetter(in[p:]) && len(bp) >= 3 && isNormalizedWindowsDriveLetter(bp[1:3]) && (len(bp) == 3 || bp[3] == '/') {
					ps.path.appendString(bp[:3])
				}
			}
			st = statePath
			p--
			segStart = p + 1

		case stateFileHost:
			if p = scan(in, p, stopPath); p < len(in) {
				c = int(in[p])
			} else {
				c = -1
			}
			if c != -1 && c != '/' && c != '\\' && c != '?' && c != '#' {
				break
			}
			end := p
			p--
			buf := in[bufStart:end]
			if override == stateNone && isWindowsDriveLetter(buf) {
				st, segStart = statePath, bufStart
			} else if buf == "" {
				url.host, url.hasHost = "", true
				if override != stateNone {
					return true
				}
				st = statePathStart
			} else {
				h, ok := ps.o.parseHost(buf, !special)
				if !ok {
					return false
				}
				if h == "localhost" {
					h = ""
				}
				url.host, url.hasHost = h, true
				if override != stateNone {
					return true
				}
				st, bufStart = statePathStart, end
			}

		case statePathStart:
			switch {
			case special:
				st = statePath
				if c != '/' && c != '\\' {
					p--
				}
				segStart = p + 1
			case override == stateNone && c == '?':
				url.query, url.hasQuery = "", true
				st, bufStart = stateQuery, p+1
			case override == stateNone && c == '#':
				url.fragment, url.hasFrag = "", true
				st, bufStart = stateFragment, p+1
			case c != -1:
				st = statePath
				if c != '/' {
					p--
				}
				segStart = p + 1
			case override != stateNone && !url.hasHost:
				ps.path.appendString("/")
			}

		case statePath:
			if p = scan(in, p, stopPath); p < len(in) {
				c = int(in[p])
			} else {
				c = -1
			}
			slash := c == '/' || special && c == '\\'
			if !slash && c != -1 && (override != stateNone || c != '?' && c != '#') {
				break
			}
			ps.segment(url, segStart, p, slash)
			segStart = p + 1
			if c == '?' {
				url.query, url.hasQuery = "", true
				st, bufStart = stateQuery, p+1
			} else if c == '#' {
				url.fragment, url.hasFrag = "", true
				st, bufStart = stateFragment, p+1
			}

		case stateOpaquePath:
			switch c {
			case '?':
				url.query, url.hasQuery = "", true
				st, bufStart = stateQuery, p+1
			case '#':
				url.fragment, url.hasFrag = "", true
				st, bufStart = stateFragment, p+1
			case ' ':
				if p+1 < len(in) && (in[p+1] == '?' || in[p+1] == '#') {
					ps.path.appendString("%20")
				} else {
					ps.path.appendInput(in, p, p+1)
				}
			case -1:
			default:
				q := p + 1
				for q < len(in) && in[q] != '?' && in[q] != '#' && in[q] != ' ' {
					q++
				}
				ps.path.appendEncoded(in, p, q, c0Set)
				p = q - 1
			}

		case stateQuery:
			end := len(in)
			if override == stateNone {
				if k := strings.IndexByte(in[p:], '#'); k >= 0 {
					end = p + k
				}
			}
			set := querySet
			if special {
				set = specialQuerySet
			}
			url.query, url.hasQuery = encode(in[bufStart:end], set), true
			p = end
			if p < len(in) {
				url.fragment, url.hasFrag = "", true
				st, bufStart = stateFragment, p+1
			}

		case stateFragment:
			url.fragment, url.hasFrag = encode(in[bufStart:], fragmentSet), true
			p = len(in)
		}
		if p >= len(in) {
			return true
		}
	}
}

// segment ends the path segment in[start:end] of the path state; slash
// reports whether a slash ends it.
func (ps *parser) segment(url *record, start, end int, slash bool) {
	in := ps.in
	seg := in[start:end]
	switch {
	case isDoubleDot(seg):
		ps.shorten(url)
		if !slash {
			ps.path.appendString("/")
		}
	case isSingleDot(seg):
		if !slash {
			ps.path.appendString("/")
		}
	case url.scheme == "file" && ps.path.len() == 0 && isWindowsDriveLetter(seg) && seg[1] != ':':
		ps.path.appendString("/")
		ps.path.appendInput(in, start, start+1)
		ps.path.appendString(":")
	case start > 0 && in[start-1] == '/':
		ps.path.appendEncoded(in, start-1, end, pathSet)
	default:
		ps.path.appendString("/")
		ps.path.appendEncoded(in, start, end, pathSet)
	}
}

// shorten shortens url's path.
func (ps *parser) shorten(url *record) {
	a := &ps.path
	n := a.len()
	if url.scheme == "file" && n == 3 && a.at(0) == '/' && isAlpha(int(a.at(1))) && a.at(2) == ':' {
		return
	}
	for n > 0 {
		n--
		if a.at(n) == '/' {
			break
		}
	}
	a.truncate(n)
}

func isAlpha(c int) bool { return 'a' <= c|0x20 && c|0x20 <= 'z' }

func isSpecial(scheme string) bool {
	switch scheme {
	case "ftp", "file", "http", "https", "ws", "wss":
		return true
	}
	return false
}

// defaultPort returns the default port of scheme, serialized; "" for null.
func defaultPort(scheme string) string {
	switch scheme {
	case "http", "ws":
		return "80"
	case "https", "wss":
		return "443"
	case "ftp":
		return "21"
	}
	return ""
}

// lowerScheme returns the ASCII scheme s lowercased, without allocating for
// a special scheme.
func lowerScheme(s string) string {
	for i := 0; i < len(s); i++ {
		if 'A' <= s[i] && s[i] <= 'Z' {
			for _, k := range [...]string{"https", "http", "wss", "ws", "file", "ftp"} {
				if strings.EqualFold(s, k) {
					return k
				}
			}
			return strings.ToLower(s)
		}
	}
	return s
}

// lowerASCII returns s with A-Z lowercased; s itself when it has none.
func lowerASCII(s string) string {
	for i := 0; i < len(s); i++ {
		if 'A' <= s[i] && s[i] <= 'Z' {
			return strings.ToLower(s)
		}
	}
	return s
}

// canonicalPort returns the port the ASCII digits s denote, serialized, or
// false when it is not a 16-bit unsigned integer.
func canonicalPort(s string) (string, bool) {
	for len(s) > 1 && s[0] == '0' {
		s = s[1:]
	}
	if len(s) > 5 {
		return "", false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		n = n*10 + int(s[i]-'0')
	}
	return s, n <= 0xffff
}

func isWindowsDriveLetter(s string) bool {
	return len(s) == 2 && isAlpha(int(s[0])) && (s[1] == ':' || s[1] == '|')
}

func isNormalizedWindowsDriveLetter(s string) bool {
	return len(s) == 2 && isAlpha(int(s[0])) && s[1] == ':'
}

func startsWithWindowsDriveLetter(s string) bool {
	if len(s) < 2 || !isWindowsDriveLetter(s[:2]) {
		return false
	}
	return len(s) == 2 || s[2] == '/' || s[2] == '\\' || s[2] == '?' || s[2] == '#'
}

func isSingleDot(s string) bool {
	return s == "." || len(s) == 3 && strings.EqualFold(s, "%2e")
}

func isDoubleDot(s string) bool {
	switch len(s) {
	case 2:
		return s == ".."
	case 4:
		return strings.EqualFold(s, ".%2e") || strings.EqualFold(s, "%2e.")
	case 6:
		return strings.EqualFold(s, "%2e%2e")
	}
	return false
}
