package engine

// The zone names are ported from ICU's TimeZoneFormat, TimeZoneNames and
// TimeZoneGenericNames (icu4c/source/i18n/tzfmt.cpp, tznames_impl.cpp and
// tzgnames.cpp; Unicode License v3; see THIRD_PARTY_NOTICES).

// Time zones of Intl.DateTimeFormat (ECMA-402 §6.5 and §11, ECMA-262
// §21.4.1.19 and IsTimeZoneOffsetString): the available named time zone
// identifiers (intl_tz_tables.go: the Zone and Link names of tzdata, each
// with its primary identifier, matched ASCII-case-insensitively), the offset
// time zones ("+05:30"), the realm's local zone, the wall clock of a time
// value in a zone and the zone's names as ICU formats them from CLDR's
// metazones.
//
// API CONTRACT: Intl.DateTimeFormat (builtin_intl_datetimeformat.go) is
// written against intlResolveZone, intlZoneOfLocation, intlZone.at,
// intlZone.appendName, intlZoneIDs and intlZonesOfRegion; keep their
// signatures and meaning.
//
// The rules of a named zone come from time.LoadLocation, once per primary
// identifier per process: the system's zoneinfo or, when the host program
// imports time/tzdata, Go's embedded copy. The engine does not import
// time/tzdata (about 450 KB). Without a time zone database "UTC", the
// Etc/GMT±n zones and the offset zones keep working and any other named
// zone throws a RangeError that says how to add one. A primary identifier
// that is a Link (Europe/Bratislava) loads its Zone when its own name is
// missing (Debian 13 moved the Links of tzdata's backward file to
// tzdata-legacy).
//
// ICU, the reference (Node 24's ICU 78.3, tzdata 2026c), builds its rules
// from tzdata's rearguard form and Go's TZif files come from the main form.
// They differ in the DST flag of the zones with a negative DST save
// (Europe/Dublin's winter, Morocco's Ramadan, Namibia 1994-2017, Prague's
// winter of 1946-47) and of British Columbia and Alberta from 2026-11-01,
// which ICU keeps in daylight time; in the spans the generator lists
// (tzICUDST in internal/gen/cldr/tz.go) at() takes ICU's flag from ICU's
// standard offset there. Offsets are Go's: LMT before a
// zone's first transition, the POSIX rule of the TZif footer after its last
// one, up to year 275760, as in ICU. A zoneinfo built with
// PACKRATDATA=backzone (Debian's, Ubuntu's, Go's embedded copy) has its own
// pre-1970 history for the zones tzdata's default build (ICU's) links to
// another country's zone.

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// intlZone is a resolved time zone. Zones of named identifiers are shared
// by the whole process (intlTZState.zones); they are immutable.
type intlZone struct {
	// id is the identifier resolvedOptions().timeZone reports: an available
	// named time zone identifier in the database's case ("Asia/Calcutta"
	// stays itself, ECMA-402's GetAvailableNamedTimeZoneIdentifier
	// [[Identifier]]), an offset ("+05:30"), or for the realm's local zone
	// its primary identifier.
	id string
	// loc holds the zone's rules; a fixed zone for an offset zone.
	loc *time.Location
	// prim is the index of the primary identifier in intlTZNames, -1 for an
	// offset zone and a local zone without a known name.
	prim int
	// info is the primary zone's region, metazones and DST spans; nil for
	// a zone without names but the localized GMT format (the offset zones
	// but +00:00, a local zone without a known name).
	info *intlZoneInfo
}

// intlZoneInfo is the record of a primary zone of intlTZZones.
type intlZoneInfo struct {
	region  string // the region, "" for none (001)
	primary bool   // the zone of its region in the generic location format
	spans   []intlDSTSpan
	uses    []intlMZUse
}

// intlDSTSpan is a span of time values [from, to) in which ICU's DST flag is
// not that of Go's zone data: there ICU has daylight time when the offset is
// not std (seconds).
type intlDSTSpan struct {
	from, to int64
	std      int
}

// intlMZUse is a metazone a zone uses in the time values [from, to).
type intlMZUse struct {
	mz       int
	from, to int64
}

// intlTZState is the decoded identifier table and the zones loaded so far.
type intlTZState struct {
	off [intlTZCount + 1]uint16 // the start of each identifier in intlTZNames
	mu  sync.Mutex              // guards infos, locs and failed
	// zones holds the zone of each identifier once resolved; infos and
	// locs those of the primary identifiers, and failed when the last
	// load of a primary identifier's rules failed.
	zones  [intlTZCount]atomic.Pointer[intlZone]
	infos  [intlTZCount]*intlZoneInfo
	locs   [intlTZCount]*time.Location
	failed map[int]time.Time
	// load loads a zone from the time zone database: time.LoadLocation
	// (tests replace it).
	load func(name string) (*time.Location, error)
}

var intlTZ = sync.OnceValue(newIntlTZState)

func newIntlTZState() *intlTZState {
	st := &intlTZState{load: time.LoadLocation}
	i, s := 0, intlTZNames
	for pos := 0; pos < len(s); pos++ {
		if s[pos] == ';' {
			i++
			st.off[i] = uint16(pos + 1)
		}
	}
	return st
}

// intlTZName returns the identifier of an index of intlTZNames.
func intlTZName(i int) string {
	st := intlTZ()
	return intlTZNames[st.off[i] : st.off[i+1]-1]
}

// intlTZPrimary returns the index of an identifier's primary identifier.
func intlTZPrimary(i int) int { return int(intlTZLinks[4*i])<<8 | int(intlTZLinks[4*i+1]) }

// intlTZTarget returns the index of the Zone an identifier links to.
func intlTZTarget(i int) int { return int(intlTZLinks[4*i+2])<<8 | int(intlTZLinks[4*i+3]) }

// intlTZFind returns the index of the identifier that is an ASCII
// case-insensitive match for name.
func intlTZFind(name string) (int, bool) {
	if name == "" || len(name) > 64 {
		return 0, false
	}
	lo, hi := 0, intlTZCount
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		if intlTZCompare(intlTZName(m), name) < 0 {
			lo = m + 1
		} else {
			hi = m
		}
	}
	if lo < intlTZCount && intlTZCompare(intlTZName(lo), name) == 0 {
		return lo, true
	}
	return 0, false
}

// intlTZCompare compares two strings by their ASCII lowercase.
func intlTZCompare(a, b string) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return int(ca) - int(cb)
		}
	}
	return len(a) - len(b)
}

// intlResolveZone resolves the timeZone option of a DateTimeFormat (a
// string): an available named time zone identifier (any case) or an offset
// time zone. It returns nil and the message of the RangeError when the name
// is no time zone, or when the host has no zoneinfo for it.
func intlResolveZone(name string) (*intlZone, string) {
	if mins, ok := intlParseOffsetZone(name); ok {
		id := intlFormatOffsetZone(mins)
		return &intlZone{id: id, loc: time.FixedZone(id, mins*60), prim: -1, info: intlOffsetZoneInfo(mins)}, ""
	}
	i, ok := intlTZFind(name)
	if !ok {
		return nil, "Invalid time zone specified: " + name
	}
	return intlTZ().zone(i)
}

// intlZoneRetry is how long a zone the host could not load stays
// unavailable before the database is read for it again (a database
// installed meanwhile is then picked up).
const intlZoneRetry = 10 * time.Second

// zone returns the zone of identifier i, loading its rules on first use.
// The database is read outside st.mu, so a slow or failing load holds up
// no other zone; a failure is remembered for intlZoneRetry.
func (st *intlTZState) zone(i int) (*intlZone, string) {
	if z := st.zones[i].Load(); z != nil {
		return z, ""
	}
	p := intlTZPrimary(i)
	st.mu.Lock()
	loc, failed := st.locs[p], st.failed[p]
	st.mu.Unlock()
	if loc == nil {
		if !failed.IsZero() && time.Since(failed) < intlZoneRetry {
			return nil, intlZoneUnavailable(i)
		}
		var err error
		if loc, err = st.loadZone(p); err != nil {
			st.mu.Lock()
			if st.failed == nil {
				st.failed = map[int]time.Time{}
			}
			st.failed[p] = time.Now()
			st.mu.Unlock()
			return nil, intlZoneUnavailable(i)
		}
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if z := st.zones[i].Load(); z != nil {
		return z, ""
	}
	if st.locs[p] == nil {
		st.locs[p] = loc // the first of concurrent loads
		delete(st.failed, p)
	}
	z := &intlZone{id: intlTZName(i), loc: st.locs[p], prim: p, info: st.infoLocked(p)}
	st.zones[i].Store(z)
	return z, ""
}

// intlZoneUnavailable is the message of the RangeError for identifier i
// when the host has no rules for it.
func intlZoneUnavailable(i int) string {
	return "Time zone " + intlTZName(i) + " is unavailable: the host has no time zone database for it " +
		"(install the system's tzdata package, or add import _ \"time/tzdata\" to the Go program)"
}

// info returns the record of a primary zone.
func (st *intlTZState) info(p int) *intlZoneInfo {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.infoLocked(p)
}

func (st *intlTZState) infoLocked(p int) *intlZoneInfo {
	if in := st.infos[p]; in != nil {
		return in
	}
	in := intlDecodeZoneInfo(p)
	st.infos[p] = in
	return in
}

// loadZone loads the rules of a primary identifier: UTC and the Etc/GMT±n
// zones are fixed, any other comes from the time zone database, by its name
// or else by the Zone it links to.
func (st *intlTZState) loadZone(p int) (*time.Location, error) {
	name := intlTZName(p)
	if name == "UTC" {
		return time.UTC, nil
	}
	if h, ok := strings.CutPrefix(name, "Etc/GMT"); ok && len(h) >= 2 && (h[0] == '+' || h[0] == '-') {
		n := 0
		for _, c := range h[1:] {
			n = n*10 + int(c-'0')
		}
		// Etc/GMT+5 is five hours west of Greenwich, UTC-05:00.
		if h[0] == '+' {
			n = -n
		}
		return time.FixedZone(name, n*3600), nil
	}
	loc, err := st.load(name)
	if err != nil {
		if t := intlTZTarget(p); t != p {
			if loc, err2 := st.load(intlTZName(t)); err2 == nil {
				return loc, nil
			}
		}
		return nil, err
	}
	return loc, nil
}

// intlDecodeZoneInfo decodes the intlTZZones record of a primary zone.
func intlDecodeZoneInfo(p int) *intlZoneInfo {
	s := intlTZZones
	for range p {
		s = s[1+int(s[0]):]
	}
	r := s[1 : 1+int(s[0])]
	in := &intlZoneInfo{}
	if r[:2] != "00" {
		in.region = r[:2]
	}
	flags := r[2]
	in.primary = flags&1 != 0
	r = r[3:]
	if flags&2 != 0 { // CLDR's identifier: the names use the primary's
		r = r[strings.IndexByte(r, ';')+1:]
	}
	const msPerMin = 60000
	if flags&4 != 0 {
		n := int(r[0])
		r = r[1:]
		in.spans = make([]intlDSTSpan, n)
		for k := range n {
			zz, w := intlUvarint(r)
			r = r[w:]
			from := int64(zz >> 1) // zigzag
			if zz&1 != 0 {
				from = ^from
			}
			length, w := intlUvarint(r)
			r = r[w:]
			zs, w := intlUvarint(r)
			r = r[w:]
			std := int(zs >> 1)
			if zs&1 != 0 {
				std = ^std
			}
			in.spans[k] = intlDSTSpan{from * msPerMin, 1 << 62, std * 60}
			if length != 0 {
				in.spans[k].to = (from + int64(length)) * msPerMin
			}
		}
	}
	for r != "" {
		h, w := intlUvarint(r)
		r = r[w:]
		u := intlMZUse{mz: int(h >> 2), from: intlMZDefaultFrom, to: intlMZDefaultTo}
		if h&2 != 0 {
			v, w := intlUvarint(r)
			r = r[w:]
			u.from = int64(v) * msPerMin
		}
		if h&1 != 0 {
			v, w := intlUvarint(r)
			r = r[w:]
			u.to = int64(v) * msPerMin
		}
		in.uses = append(in.uses, u)
	}
	return in
}

// ICU's bounds of a metazone use without from or to: 1970-01-01 00:00 and
// 9999-12-31 23:59 UTC, in ms.
const (
	intlMZDefaultFrom = 0
	intlMZDefaultTo   = 253402300740000
)

// intlZeroOffsetInfo is the zone record of the offset zone +00:00, which ICU
// names as Etc/GMT: the metazone GMT (from 1970 to 9999) and no region.
var intlZeroOffsetInfo = sync.OnceValue(func() *intlZoneInfo {
	return &intlZoneInfo{uses: []intlMZUse{{intlTZMetazoneGMT, intlMZDefaultFrom, intlMZDefaultTo}}}
})

// intlOffsetZoneInfo returns the zone record of an offset zone: none but
// for +00:00.
func intlOffsetZoneInfo(mins int) *intlZoneInfo {
	if mins != 0 {
		return nil
	}
	return intlZeroOffsetInfo()
}

// intlUvarint decodes a uvarint at the start of s.
func intlUvarint(s string) (uint64, int) {
	var v uint64
	for i := 0; i < len(s) && i < 10; i++ {
		v |= uint64(s[i]&0x7f) << (7 * i)
		if s[i] < 0x80 {
			return v, i + 1
		}
	}
	return v, len(s)
}

// intlParseOffsetZone implements IsTimeZoneOffsetString and
// ParseTimeZoneOffsetString for a time zone identifier: ±HH, ±HHMM or
// ±HH:MM (UTCOffset[~SubMinutePrecision], an ASCII sign) in minutes.
func intlParseOffsetZone(s string) (int, bool) {
	if len(s) != 3 && len(s) != 5 && len(s) != 6 || s[0] != '+' && s[0] != '-' {
		return 0, false
	}
	digits := func(t string) (int, bool) {
		if t[0] < '0' || t[0] > '9' || t[1] < '0' || t[1] > '9' {
			return 0, false
		}
		return int(t[0]-'0')*10 + int(t[1]-'0'), true
	}
	h, ok := digits(s[1:3])
	if !ok || h > 23 {
		return 0, false
	}
	m := 0
	switch len(s) {
	case 5:
		m, ok = digits(s[3:5])
	case 6:
		if s[3] != ':' {
			return 0, false
		}
		m, ok = digits(s[4:6])
	}
	if !ok || m > 59 {
		return 0, false
	}
	mins := h*60 + m
	if s[0] == '-' {
		mins = -mins
	}
	return mins, true
}

// intlFormatOffsetZone implements FormatOffsetTimeZoneIdentifier: ±HH:MM.
func intlFormatOffsetZone(mins int) string {
	b := make([]byte, 0, 6)
	if mins < 0 {
		b, mins = append(b, '-'), -mins
	} else {
		b = append(b, '+')
	}
	b = appendPadded(b, int64(mins/60), 2)
	b = append(b, ':')
	b = appendPadded(b, int64(mins%60), 2)
	return string(b)
}

// intlLastLocal caches the last zone intlZoneOfLocation made.
var intlLastLocal atomic.Pointer[intlZone]

// intlZoneOfLocation returns the zone of a realm's local time zone
// (Realm.TimeZone: a *time.Location from LoadLocation, time.UTC,
// time.FixedZone or time.Local), the default time zone of DateTimeFormat.
// Its identifier is the primary identifier of the location's name (for
// time.Local the system's: $TZ, /etc/localtime's link, /etc/timezone), the
// offset of a fixed zone without a known name, else "UTC"; its rules are
// always loc's.
func intlZoneOfLocation(loc *time.Location) *intlZone {
	if c := intlLastLocal.Load(); c != nil && c.loc == loc {
		return c
	}
	z := intlNewLocalZone(loc)
	intlLastLocal.Store(z)
	return z
}

func intlNewLocalZone(loc *time.Location) *intlZone {
	name := loc.String()
	switch {
	case name == "Local":
		name = intlSystemZoneName()
	case strings.HasPrefix(name, "/"):
		name = intlZoneNameOfPath(name)
	}
	if i, ok := intlTZFind(name); ok {
		st := intlTZ()
		p := intlTZPrimary(i)
		return &intlZone{id: intlTZName(p), loc: loc, prim: p, info: st.info(p)}
	}
	if start, end := time.Unix(0, 0).In(loc).ZoneBounds(); start.IsZero() && end.IsZero() {
		if _, off := time.Unix(0, 0).In(loc).Zone(); off%60 == 0 && off > -24*3600 && off < 24*3600 {
			return &intlZone{id: intlFormatOffsetZone(off / 60), loc: loc, prim: -1, info: intlOffsetZoneInfo(off / 60)}
		}
	}
	return &intlZone{id: "UTC", loc: loc, prim: -1}
}

// intlSystemZoneName returns the name of the system's time zone, the zone of
// time.Local when Go names it "Local": $TZ (without its ':', a path in a
// zoneinfo directory), else the link of /etc/localtime, else /etc/timezone;
// "" when it has none.
var intlSystemZoneName = sync.OnceValue(func() string {
	if tz, ok := os.LookupEnv("TZ"); ok {
		tz = strings.TrimPrefix(tz, ":")
		switch {
		case tz == "":
			return "UTC"
		case !strings.HasPrefix(tz, "/"):
			return tz
		case tz != "/etc/localtime":
			return intlZoneNameOfPath(tz)
		}
	}
	if n := intlZoneNameOfPath("/etc/localtime"); n != "" {
		return n
	}
	if b, err := os.ReadFile("/etc/timezone"); err == nil {
		return strings.TrimSpace(string(b))
	}
	return ""
})

// intlZoneNameOfPath returns the zone name of a zoneinfo file's path (the
// part after a "zoneinfo/" directory, without posix/ or right/), following
// symbolic links one at a time until a path has one; "" when none has.
func intlZoneNameOfPath(p string) string {
	for range 8 {
		if i := strings.LastIndex(p, "zoneinfo/"); i >= 0 {
			n := p[i+len("zoneinfo/"):]
			if s, ok := strings.CutPrefix(n, "posix/"); ok {
				n = s
			} else if s, ok := strings.CutPrefix(n, "right/"); ok {
				n = s
			}
			return n
		}
		r, err := os.Readlink(p)
		if err != nil {
			return ""
		}
		if !filepath.IsAbs(r) {
			r = filepath.Join(filepath.Dir(p), r)
		}
		p = r
	}
	return ""
}

// at returns the wall clock of a time value (finite and within the range of
// TimeClip) in the zone, its offset from UTC in seconds and whether it is
// daylight saving time.
func (z *intlZone) at(tv float64) (t time.Time, offset int, dst bool) {
	ms := int64(tv)
	t = time.UnixMilli(ms).In(z.loc)
	if z.loc == time.UTC {
		return t, 0, false
	}
	_, offset = t.Zone()
	dst = t.IsDST()
	if z.info != nil {
		for _, s := range z.info.spans {
			if s.from <= ms && ms < s.to {
				dst = offset != s.std
				break
			}
		}
	}
	return t, offset, dst
}

// intlZoneNameStyle is the timeZoneName option.
type intlZoneNameStyle uint8

const (
	intlZoneShort intlZoneNameStyle = iota + 1
	intlZoneLong
	intlZoneShortOffset
	intlZoneLongOffset
	intlZoneShortGeneric
	intlZoneLongGeneric
)

// The six names of a metazone or zone record, the bits of its mask.
const (
	intlTZLongGeneric = iota
	intlTZLongStandard
	intlTZLongDaylight
	intlTZShortGeneric
	intlTZShortStandard
	intlTZShortDaylight
	intlTZCity // the exemplar city of a zone record
)

// appendName appends the zone's name at a time value in a data locale
// (intlDataIndex) and style, as ICU's TimeZoneFormat does with the
// patterns z, zzzz, O, OOOO, v and vvvv: the localized GMT format for the
// offset styles and as the fallback of the others, the zone's or its
// metazone's specific names for short and long, the metazone's generic name
// or the generic location format ("Japan Time", "Los Angeles Time") for
// shortGeneric and longGeneric. Digits are ASCII.
func (z *intlZone) appendName(b []byte, data int, style intlZoneNameStyle, tv float64) []byte {
	return z.appendNameFor(b, data, "", style, tv)
}

// appendNameFor is appendName for a locale with a region ("CA" of en-CA):
// ICU takes the metazone's reference zone of the formatter locale's region
// in the generic styles. "" is the region of the data locale's likely
// subtags (en: US).
func (z *intlZone) appendNameFor(b []byte, data int, region string, style intlZoneNameStyle, tv float64) []byte {
	_, off, dst := z.at(tv)
	l := intlTZLocaleOf(data)
	if region == "" {
		region = l.hdr[intlTZLikelyRegion]
	}
	short := style == intlZoneShort || style == intlZoneShortOffset || style == intlZoneShortGeneric
	if z.info != nil {
		var n intlTZString
		switch style {
		case intlZoneShort, intlZoneLong:
			typ := intlTZLongStandard
			if dst {
				typ++
			}
			if short {
				typ += intlTZShortGeneric
			}
			n = l.displayName(z, typ, int64(tv))
		case intlZoneShortGeneric, intlZoneLongGeneric:
			n = l.genericName(z, region, short, int64(tv), off, dst)
		}
		if n.n != 0 {
			return n.appendTo(b)
		}
	}
	return l.appendGMT(b, off, short)
}

// intlTZString is a name of intlTZLocaleNames: up to seven pieces (the
// shared prefix, the own part and the shared suffix of an affix-coded name,
// or a format's text around its values) whose concatenation it stands for,
// n being their count; a piece whose bit is set in under has spaces for its
// '_' (a city named by its identifier).
type intlTZString struct {
	p     [7]string
	n     int
	under uint8
}

func (s *intlTZString) add(p string, under bool) {
	if p == "" || s.n == len(s.p) {
		return
	}
	s.p[s.n] = p
	if under {
		s.under |= 1 << s.n
	}
	s.n++
}

func (s *intlTZString) appendTo(b []byte) []byte {
	for k, p := range s.p[:s.n] {
		if s.under&(1<<k) == 0 {
			b = append(b, p...)
			continue
		}
		for i := 0; i < len(p); i++ {
			c := p[i]
			if c == '_' {
				c = ' '
			}
			b = append(b, c)
		}
	}
	return b
}

// equalFold reports whether two names (without cities) are equal ignoring
// ASCII case, ICU's caseCompare for the names CLDR has.
func (s *intlTZString) equalFold(t *intlTZString) bool {
	i, j := 0, 0 // pieces
	a, b := "", ""
	for {
		for a == "" && i < s.n {
			a, i = s.p[i], i+1
		}
		for b == "" && j < t.n {
			b, j = t.p[j], j+1
		}
		if a == "" || b == "" {
			return a == b
		}
		n := min(len(a), len(b))
		if intlTZCompare(a[:n], b[:n]) != 0 {
			return false
		}
		a, b = a[n:], b[n:]
	}
}

// intlTZLocale is a data locale's decoded intlTZLocaleNames record.
type intlTZLocale struct {
	s      string
	hdr    [11]string                 // the header's fields
	mz     [intlTZMetazoneCount]int32 // the offset of each metazone's mask
	zones  []uint32                   // identifier index<<16 | offset of the mask, sorted
	region []uint16                   // the offset of each region's territory name
}

// The header fields of intlTZLocale.
const (
	intlTZGMTPrefix = iota
	intlTZGMTSuffix
	intlTZPosH // the offset patterns: +H, +H:mm, +H:mm:ss, then negative
	intlTZPosHM
	intlTZPosHMS
	intlTZNegH
	intlTZNegHM
	intlTZNegHMS
	intlTZRegionFormat
	intlTZFallbackFormat
	intlTZLikelyRegion
)

var intlTZLocales [intlDataIndexCount]struct {
	once sync.Once
	l    *intlTZLocale
}

// intlTZLocaleOf returns the decoded names of a data locale, decoding them on
// first use.
func intlTZLocaleOf(data int) *intlTZLocale {
	c := &intlTZLocales[data]
	c.once.Do(func() { c.l = intlDecodeTZLocale(intlTZTable.record(data)) })
	return c.l
}

func intlDecodeTZLocale(s string) *intlTZLocale {
	l := &intlTZLocale{s: s}
	pos := 0
	for i := range l.hdr {
		var v intlTZString
		v, pos = l.field(pos, "")
		l.hdr[i] = v.p[0] // literal or a reference: one piece
	}
	for i := range l.mz {
		l.mz[i] = int32(pos)
		mask := s[pos]
		pos++
		for k := range 6 {
			if mask&(1<<k) != 0 {
				pos = intlTZSkip(s, pos)
			}
		}
	}
	for s[pos] != 0 {
		idx := int(s[pos]&0x7f)<<7 | int(s[pos+1]&0x7f)
		l.zones = append(l.zones, uint32(idx)<<16|uint32(pos+2))
		mask := s[pos+2]
		pos += 3
		for k := range 7 {
			if mask&(1<<k) != 0 {
				pos = intlTZSkip(s, pos)
			}
		}
	}
	pos++
	n := len(intlTZRegions) / 2
	l.region = make([]uint16, n)
	for i := range n {
		l.region[i] = uint16(pos)
		pos = intlTZSkip(s, pos)
	}
	return l
}

// intlTZSkip returns the offset after the field at pos.
func intlTZSkip(s string, pos int) int {
	switch s[pos] {
	case 1: // affixes: two length bytes, then the own part
		pos += 3
	case 2: // a reference: four bytes
		return pos + 6
	}
	return pos + strings.IndexByte(s[pos:], 0) + 1
}

// field reads the field at pos, first being the record's first name for the
// affixes, and returns it and the offset after it.
func (l *intlTZLocale) field(pos int, first string) (intlTZString, int) {
	s := l.s
	switch s[pos] {
	case 1:
		pre, suf := int(s[pos+1])-1, int(s[pos+2])-1
		end := pos + 3 + strings.IndexByte(s[pos+3:], 0)
		var v intlTZString
		v.add(first[:pre], false)
		v.add(s[pos+3:end], false)
		v.add(first[len(first)-suf:], false)
		return v, end + 1
	case 2:
		other := intlTZLocaleNames[s[pos+1]&0x7f]
		off := int(s[pos+2]&0x7f)<<14 | int(s[pos+3]&0x7f)<<7 | int(s[pos+4]&0x7f)
		var v intlTZString
		v.add(other[off:off+strings.IndexByte(other[off:], 0)], false)
		return v, pos + 6
	}
	end := pos + strings.IndexByte(s[pos:], 0)
	var v intlTZString
	v.add(s[pos:end], false)
	return v, end + 1
}

// record returns name k of the record whose mask is at pos (bits 0-5 the
// names, bit 6 a zone record's exemplar city, which comes first).
func (l *intlTZLocale) record(pos, k int) intlTZString {
	mask := l.s[pos]
	if mask&(1<<k) == 0 {
		return intlTZString{}
	}
	pos++
	first := ""
	if mask&(1<<intlTZCity) != 0 {
		if k == intlTZCity {
			v, _ := l.field(pos, "")
			return v
		}
		pos = intlTZSkip(l.s, pos)
	}
	for j := range 6 {
		if mask&(1<<j) == 0 {
			continue
		}
		v, next := l.field(pos, first)
		if j == k {
			return v
		}
		if first == "" {
			first = v.p[0]
		}
		pos = next
	}
	return intlTZString{}
}

// zoneName returns name k of a primary zone's record, empty when it has
// none.
func (l *intlTZLocale) zoneName(prim, k int) intlTZString {
	if prim < 0 {
		return intlTZString{}
	}
	i, ok := slices.BinarySearchFunc(l.zones, uint32(prim), func(e, t uint32) int { return int(e>>16) - int(t) })
	if !ok {
		return intlTZString{}
	}
	return l.record(int(l.zones[i]&0xffff), k)
}

// mzName returns name k of a metazone.
func (l *intlTZLocale) mzName(mz, k int) intlTZString { return l.record(int(l.mz[mz]), k) }

// metazone returns the metazone a zone uses at a time value, -1 for none.
func (in *intlZoneInfo) metazone(ms int64) int {
	for _, u := range in.uses {
		if u.from <= ms && ms < u.to {
			return u.mz
		}
	}
	return -1
}

// displayName is ICU's TimeZoneNames::getDisplayName: name k of the zone,
// else of its metazone at the time value.
func (l *intlTZLocale) displayName(z *intlZone, k int, ms int64) intlTZString {
	if n := l.zoneName(z.prim, k); n.n != 0 {
		return n
	}
	if mz := z.info.metazone(ms); mz >= 0 {
		return l.mzName(mz, k)
	}
	return intlTZString{}
}

// intlDSTCheckRange is ICU's kDstCheckRange, 184 days in ms.
const intlDSTCheckRange = 184 * 24 * 3600 * 1000

// genericName is ICU's TZGNCore::getDisplayName for the generic long and
// short types: the zone's generic name; else, with a metazone, its standard
// name when the zone observes no DST within 184 days, else the metazone's
// generic name (with the partial location when the zone's offsets differ
// from the metazone's reference zone for the region); else the generic
// location format.
func (l *intlTZLocale) genericName(z *intlZone, region string, short bool, ms int64, off int, dst bool) intlTZString {
	gen, std := intlTZLongGeneric, intlTZLongStandard
	if short {
		gen, std = intlTZShortGeneric, intlTZShortStandard
	}
	if n := l.zoneName(z.prim, gen); n.n != 0 {
		return n
	}
	if mz := z.info.metazone(ms); mz >= 0 {
		if !dst && !z.dstNear(ms) {
			if s := l.displayName(z, std, ms); s.n != 0 {
				if g := l.mzName(mz, gen); !s.equalFold(&g) {
					return s
				}
			}
		}
		if n := l.mzName(mz, gen); n.n != 0 {
			if g := intlTZReference(mz, region); g >= 0 && g != z.prim {
				if gz, msg := intlTZ().zone(g); msg == "" {
					_, goff0, _ := gz.at(float64(ms))
					_, goff, gdst := gz.at(float64(ms + int64(off-goff0)*1000))
					if goff != off || gdst != dst {
						return l.partialLocation(z, mz, n)
					}
				}
			}
			return n
		}
	}
	return l.genericLocation(z)
}

// dstNear reports whether the transition before the time value (at it or
// earlier) left daylight time, or the one after it enters daylight time,
// within 184 days (ICU's check in formatGenericNonLocationName). The
// transitions are those of Go's zone periods and the edges of the DST spans;
// one that changes neither the offset nor the DST flag (only the
// abbreviation) is none.
func (z *intlZone) dstNear(ms int64) bool {
	_, off, dst := z.at(float64(ms))
	cur := ms
	for range 8 {
		s, ok := z.change(cur, false)
		if !ok || ms-s >= intlDSTCheckRange {
			break
		}
		_, poff, pdst := z.at(float64(s - 1))
		if pdst {
			return true
		}
		if poff != off || pdst != dst {
			break
		}
		cur = s - 1
	}
	cur = ms
	for range 8 {
		e, ok := z.change(cur, true)
		if !ok || e-ms >= intlDSTCheckRange {
			break
		}
		_, noff, ndst := z.at(float64(e))
		if ndst {
			return true
		}
		if noff != off || ndst != dst {
			break
		}
		cur = e
	}
	return false
}

// change returns the latest transition at or before a time value, or with
// next the earliest after it: the start or end of Go's zone period or a DST
// span's edge.
func (z *intlZone) change(ms int64, next bool) (int64, bool) {
	start, end := time.UnixMilli(ms).In(z.loc).ZoneBounds()
	var c int64
	ok := false
	if next && !end.IsZero() {
		c, ok = end.UnixMilli(), true
	} else if !next && !start.IsZero() {
		c, ok = start.UnixMilli(), true
	}
	if z.info != nil {
		for _, sp := range z.info.spans {
			for _, e := range [2]int64{sp.from, sp.to} {
				if next && e > ms && (!ok || e < c) || !next && e <= ms && (!ok || e > c) {
					c, ok = e, true
				}
			}
		}
	}
	return c, ok
}

// genericLocation is ICU's generic location format: the region format with
// the territory of the region's zone, else with the zone's exemplar city;
// empty for a zone without a region.
func (l *intlTZLocale) genericLocation(z *intlZone) intlTZString {
	if z.info.region == "" {
		return intlTZString{}
	}
	var v intlTZString
	if z.info.primary {
		v = l.territory(z.info.region)
	} else {
		v = l.city(z.prim)
	}
	return intlTZFormat(l.hdr[intlTZRegionFormat], v, intlTZString{})
}

// partialLocation is ICU's getPartialLocationName: the fallback format with
// the metazone's generic name and the territory, when the zone is the
// metazone's reference zone in its region, else the zone's exemplar city.
func (l *intlTZLocale) partialLocation(z *intlZone, mz int, name intlTZString) intlTZString {
	var loc intlTZString
	if z.info.region != "" && intlTZReference(mz, z.info.region) == z.prim {
		loc = l.territory(z.info.region)
	} else {
		loc = l.city(z.prim)
	}
	return intlTZFormat(l.hdr[intlTZFallbackFormat], loc, name)
}

// city returns a zone's exemplar city: the record's, else the identifier's
// last part with spaces for '_'.
func (l *intlTZLocale) city(prim int) intlTZString {
	if c := l.zoneName(prim, intlTZCity); c.n != 0 {
		return c
	}
	id := intlTZName(prim)
	var c intlTZString
	c.add(id[strings.LastIndexByte(id, '/')+1:], true)
	return c
}

// territory returns the name of a region of intlTZRegions.
func (l *intlTZLocale) territory(region string) intlTZString {
	lo, hi := 0, len(intlTZRegions)/2
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		if intlTZRegions[2*m:2*m+2] < region {
			lo = m + 1
		} else {
			hi = m
		}
	}
	if lo*2 >= len(intlTZRegions) || intlTZRegions[2*lo:2*lo+2] != region {
		return intlTZString{}
	}
	v, _ := l.field(int(l.region[lo]), "")
	return v
}

// intlTZFormat substitutes {0} and {1} of a pattern ("{0} Time", "{1}
// ({0})").
func intlTZFormat(pattern string, v0, v1 intlTZString) intlTZString {
	var out intlTZString
	for pattern != "" {
		i := strings.IndexByte(pattern, '{')
		if i < 0 || i+2 >= len(pattern) || pattern[i+2] != '}' || pattern[i+1] != '0' && pattern[i+1] != '1' {
			out.add(pattern, false)
			break
		}
		out.add(pattern[:i], false)
		v := &v0
		if pattern[i+1] == '1' {
			v = &v1
		}
		for k, p := range v.p[:v.n] {
			out.add(p, v.under&(1<<k) != 0)
		}
		pattern = pattern[i+3:]
	}
	return out
}

// intlTZGoldenOffsets returns the offset of each metazone's record in
// intlTZGolden.
var intlTZGoldenOffsets = sync.OnceValue(func() *[intlTZMetazoneCount]uint16 {
	var t [intlTZMetazoneCount]uint16
	pos := 0
	for i := range t {
		t[i] = uint16(pos)
		pos += 1 + 4*int(intlTZGolden[pos])
	}
	return &t
})

// intlTZReference is ICU's getReferenceZoneID: the primary index of a
// metazone's reference zone in a region, else in 001; -1 for none.
func intlTZReference(mz int, region string) int {
	pos := int(intlTZGoldenOffsets()[mz])
	n := int(intlTZGolden[pos])
	rec := intlTZGolden[pos+1 : pos+1+4*n]
	for i := 0; i < len(rec); i += 4 {
		if rec[i:i+2] == region {
			return int(rec[i+2])<<8 | int(rec[i+3])
		}
	}
	if n > 0 && rec[:2] == "00" {
		return int(rec[2])<<8 | int(rec[3])
	}
	return -1
}

// appendGMT appends the localized GMT format of an offset in seconds (ICU's
// formatOffsetLocalizedGMT as ICU 78 formats the patterns O and OOOO and the
// fallback, without the zero format: "GMT+0"): the prefix, the offset
// pattern with hours (two digits long, one short), minutes (two digits;
// dropped when zero in the short form) and seconds (when not zero), and the
// suffix.
func (l *intlTZLocale) appendGMT(b []byte, off int, short bool) []byte {
	pat := intlTZPosH
	if off < 0 {
		pat, off = intlTZNegH, -off
	}
	h, m, s := off/3600, off/60%60, off%60
	switch {
	case s != 0:
		pat += 2
	case m != 0 || !short:
		pat++
	}
	b = append(b, l.hdr[intlTZGMTPrefix]...)
	p := l.hdr[pat]
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c != 'H' && c != 'm' && c != 's' {
			b = append(b, c)
			continue
		}
		for i+1 < len(p) && p[i+1] == c {
			i++
		}
		switch c {
		case 'H':
			w := 2
			if short {
				w = 1
			}
			b = appendPadded(b, int64(h), w)
		case 'm':
			b = appendPadded(b, int64(m), 2)
		default:
			b = appendPadded(b, int64(s), 2)
		}
	}
	return append(b, l.hdr[intlTZGMTSuffix]...)
}

// intlZoneIDList holds the primary identifiers, sorted.
var intlZoneIDList = sync.OnceValue(func() []string {
	var list []string
	for i := range intlTZCount {
		if intlTZPrimary(i) == i {
			list = append(list, intlTZName(i))
		}
	}
	slices.Sort(list)
	return list
})

// intlZoneIDs returns the primary identifiers
// (AvailablePrimaryTimeZoneIdentifiers), sorted by code units. The slice is
// shared: callers must not modify it.
func intlZoneIDs() []string { return intlZoneIDList() }

// intlUsableZoneIDs returns the identifiers of
// Intl.supportedValuesOf("timeZone"): the primary identifiers whose rules
// this host's time zone database has, so that a DateTimeFormat accepts each
// of them. The first call loads every zone not loaded yet to check it and
// keeps the rules, as a DateTimeFormat would (all 445 zones take about
// 620 KB). A list that lacks some zones is checked again once
// intlZoneRetry has passed, as DateTimeFormat retries them, so a database
// installed later is listed. The slice is shared: callers must not modify
// it.
func intlUsableZoneIDs() []string {
	u := &intlUsableZones
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.list != nil && (u.complete || time.Since(u.at) < intlZoneRetry) {
		return u.list
	}
	st := intlTZ()
	all := intlZoneIDs()
	var list []string
	for k, name := range all {
		p, _ := intlTZFind(name)
		st.mu.Lock()
		ok, failed := st.locs[p] != nil, st.failed[p]
		st.mu.Unlock()
		if !ok && (failed.IsZero() || time.Since(failed) >= intlZoneRetry) {
			loc, err := st.loadZone(p)
			ok = err == nil
			st.mu.Lock()
			switch {
			case ok && st.locs[p] == nil:
				st.locs[p] = loc
				delete(st.failed, p)
			case !ok:
				if st.failed == nil {
					st.failed = map[int]time.Time{}
				}
				st.failed[p] = time.Now()
			}
			st.mu.Unlock()
		}
		switch {
		case !ok && list == nil:
			list = append(make([]string, 0, len(all)), all[:k]...)
		case ok && list != nil:
			list = append(list, name)
		}
	}
	u.complete = list == nil
	if u.complete {
		list = all
	}
	u.list, u.at = list, time.Now()
	return list
}

// intlUsableZones is the list of intlUsableZoneIDs.
var intlUsableZones struct {
	mu       sync.Mutex
	list     []string
	complete bool      // every zone loads: the list is final
	at       time.Time // when the list was made
}

// intlZonesOfRegion returns the zones of a region for
// Intl.Locale.prototype.getTimeZones, sorted: the primary identifiers of the
// zones tzdata's zone.tab gives the region (CLDR's for the others), as ICU's
// canonical location zones; for "001" the zones without a region (UTC and
// Etc/GMT±n). Nil when the region has none.
func intlZonesOfRegion(region string) []string {
	key := region
	if key == "001" {
		key = "00"
	}
	if len(key) != 2 {
		return nil
	}
	var list []string
	s := intlTZZones
	for i := 0; s != ""; i++ {
		n := int(s[0])
		if n > 0 && s[1:3] == key {
			list = append(list, intlTZName(i))
		}
		s = s[1+n:]
	}
	slices.Sort(list)
	return list
}
