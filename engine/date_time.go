package engine

import (
	"math"
	"time"
)

// Date time-value arithmetic (ES2024 §21.4.1) and the local time zone.
//
// Time values are float64 milliseconds since the epoch, but every calendar
// computation on a finite, clipped time value runs on int64 so it is exact:
// Day, YearFromTime and friends use floor division and Howard Hinnant's
// civil-calendar algorithms instead of the spec's floating-point formulas.
// MakeTime and MakeDate keep the IEEE-754 evaluation order the spec
// prescribes ("as if using the ECMAScript operators * and +"); the explicit
// float64 conversions stop the Go compiler from fusing a multiply and an add
// into one FMA instruction, which would round differently.
//
// The local time zone is a *time.Location: time.Local unless the host sets
// one per realm (RealmOptions.TimeZone, Realm.SetTimeZone). Offsets come from
// Go's tz database. Before a zone's first transition Go reports the zone's
// first standard offset, which for tzdata zones is local mean time (for
// example -4:56:02 for America/New_York before 1883); after the last
// transition it applies the zone's POSIX rule, so DST keeps alternating up to
// year 275760. V8 (through ICU) makes the same choices, including LMT offsets
// with seconds; getTimezoneOffset and the "GMT-0456" in toString truncate such
// offsets to whole minutes as V8 does.

const (
	msPerSecond = 1000
	msPerMinute = 60 * msPerSecond
	msPerHour   = 60 * msPerMinute
	msPerDay    = 24 * msPerHour
)

// maxTimeValue is the TimeClip bound (8.64e15 ms, ±100,000,000 days).
const maxTimeValue = 8.64e15

// maxLocalTimeValue bounds local time values that can still map to a time
// value within TimeClip range: no UTC offset reaches a day.
const maxLocalTimeValue = maxTimeValue + msPerDay

// timeClip implements TimeClip.
func timeClip(t float64) float64 {
	if t != t || math.IsInf(t, 0) || math.Abs(t) > maxTimeValue {
		return math.NaN()
	}
	return ToIntegerOrInfinityFloat(t)
}

func floorDiv(a, b int64) int64 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

func floorMod(a, b int64) int64 {
	m := a % b
	if m != 0 && ((m < 0) != (b < 0)) {
		m += b
	}
	return m
}

// daysFromCivil returns the number of days from 1970-01-01 to year-month-day
// (month 0-11, day 1-31) in the proleptic Gregorian calendar.
func daysFromCivil(y int64, month, day int) int64 {
	m := int64(month) + 1
	if m <= 2 {
		y--
	}
	era := floorDiv(y, 400)
	yoe := y - era*400 // [0, 399]
	mp := (m + 9) % 12 // March = 0
	doy := (153*mp+2)/5 + int64(day) - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy // [0, 146096]
	return era*146097 + doe - 719468
}

// civilFromDays is the inverse of daysFromCivil.
func civilFromDays(z int64) (year int64, month, day int) {
	z += 719468
	era := floorDiv(z, 146097)
	doe := z - era*146097                                  // [0, 146096]
	yoe := (doe - doe/1460 + doe/36524 - doe/146096) / 365 // [0, 399]
	y := yoe + era*400
	doy := doe - (365*yoe + yoe/4 - yoe/100) // [0, 365]
	mp := (5*doy + 2) / 153                  // [0, 11], March = 0
	day = int(doy - (153*mp+2)/5 + 1)
	if mp < 10 {
		month = int(mp) + 2
	} else {
		month = int(mp) - 10
		y++
	}
	return y, month, day
}

// dateFields is a broken-down time value.
type dateFields struct {
	year    int64
	month   int // 0-11
	day     int // 1-31
	weekday int // 0 = Sunday
	hour    int
	minute  int
	second  int
	ms      int
}

// splitTime breaks down an integral time value (UTC or local).
func splitTime(t int64) dateFields {
	days := floorDiv(t, msPerDay)
	within := t - days*msPerDay
	var f dateFields
	f.year, f.month, f.day = civilFromDays(days)
	f.weekday = int(floorMod(days+4, 7))
	f.hour = int(within / msPerHour)
	f.minute = int(within / msPerMinute % 60)
	f.second = int(within / msPerSecond % 60)
	f.ms = int(within % msPerSecond)
	return f
}

// makeTime implements MakeTime.
func makeTime(hour, min, sec, ms float64) float64 {
	if !isFinite(hour) || !isFinite(min) || !isFinite(sec) || !isFinite(ms) {
		return math.NaN()
	}
	h := ToIntegerOrInfinityFloat(hour)
	m := ToIntegerOrInfinityFloat(min)
	s := ToIntegerOrInfinityFloat(sec)
	milli := ToIntegerOrInfinityFloat(ms)
	return float64(float64(float64(h*msPerHour)+float64(m*msPerMinute))+float64(s*msPerSecond)) + milli
}

// maxMakeDayYear bounds the years MakeDay computes exactly: the day number
// stays below 2^53. Larger years are "out of range" (the spec lets MakeDay
// return NaN then); any such date is far outside TimeClip range unless a
// date argument of similar magnitude cancels it.
const maxMakeDayYear = 1e13

// makeDay implements MakeDay with exact integer arithmetic.
func makeDay(year, month, date float64) float64 {
	if !isFinite(year) || !isFinite(month) || !isFinite(date) {
		return math.NaN()
	}
	y := ToIntegerOrInfinityFloat(year)
	m := ToIntegerOrInfinityFloat(month)
	dt := ToIntegerOrInfinityFloat(date)
	ym := y + math.Floor(m/12)
	if !(math.Abs(ym) <= maxMakeDayYear) {
		return math.NaN()
	}
	mn := math.Mod(m, 12)
	if mn < 0 {
		mn += 12
	}
	days := daysFromCivil(int64(ym), int(mn), 1)
	return float64(float64(days)+dt) - 1
}

// makeDate implements MakeDate.
func makeDate(day, t float64) float64 {
	if !isFinite(day) || !isFinite(t) {
		return math.NaN()
	}
	tv := float64(day*msPerDay) + t
	if !isFinite(tv) {
		return math.NaN()
	}
	return tv
}

// makeFullYear implements MakeFullYear (years 0-99 map to 1900-1999).
func makeFullYear(y float64) float64 {
	if y != y {
		return y
	}
	t := ToIntegerOrInfinityFloat(y)
	if t >= 0 && t <= 99 {
		return 1900 + t
	}
	return t
}

func isFinite(f float64) bool { return f == f && !math.IsInf(f, 0) }

// dayOf returns Day(t) for a finite integral time value.
func dayOf(t int64) int64 { return floorDiv(t, msPerDay) }

// timeWithinDay returns TimeWithinDay(t) for a finite integral time value.
func timeWithinDay(t int64) int64 { return floorMod(t, msPerDay) }

// realmHost is the realm's host configuration: the clock and the local time
// zone of Date, the hooks of import() and import.meta (import.go) and the
// limit of the code it compiles from strings (eval.go). It is nil until the
// host sets one of them, so realms that keep the defaults (time.Now,
// time.Local, no import hooks, DefaultMaxDynamicSource, dynamic code
// allowed) pay nothing for it.
type realmHost struct {
	now       func() time.Time
	loc       *time.Location
	imports   *ImportHooks
	maxSource int  // MaxDynamicSource; zero: DefaultMaxDynamicSource (eval.go)
	noDynamic bool // DisableDynamicCode (eval.go)
}

// SetNow sets the clock used by Date.now() and new Date(); nil restores
// time.Now. Hosts freeze or offset time with it.
func (r *Realm) SetNow(now func() time.Time) {
	if r.host == nil {
		if now == nil {
			return
		}
		r.host = &realmHost{}
	}
	r.host.now = now
}

// SetTimeZone sets the realm's local time zone for Date (the local getters
// and setters, toString, parsing of strings without an offset and the
// time.Time exported for a Date); nil restores time.Local.
func (r *Realm) SetTimeZone(loc *time.Location) {
	if r.host == nil {
		if loc == nil {
			return
		}
		r.host = &realmHost{}
	}
	r.host.loc = loc
}

// TimeZone returns the realm's local time zone.
func (r *Realm) TimeZone() *time.Location {
	if r.host != nil && r.host.loc != nil {
		return r.host.loc
	}
	return time.Local
}

// now returns the realm's current time.
func (r *Realm) now() time.Time {
	if r.host != nil && r.host.now != nil {
		return r.host.now()
	}
	return time.Now()
}

// nowTimeValue is the time value of the realm's current time.
func (r *Realm) nowTimeValue() float64 { return timeClip(float64(r.now().UnixMilli())) }

// DateTime converts a finite time value to a time.Time in the realm's time
// zone (the Go value a Date exports as).
func (r *Realm) DateTime(tv float64) time.Time {
	return time.UnixMilli(int64(tv)).In(r.TimeZone())
}

// utcOffset returns the offset in ms of the local time zone at the finite
// integral UTC time value t (LocalTZA(t, true)).
func (r *Realm) utcOffset(t int64) int64 {
	loc := r.TimeZone()
	if loc == time.UTC {
		return 0
	}
	_, off := time.Unix(floorDiv(t, msPerSecond), 0).In(loc).Zone()
	return int64(off) * msPerSecond
}

// zoneAt returns the offset in ms and the abbreviation of the local time
// zone at the finite integral UTC time value t.
func (r *Realm) zoneAt(t int64) (int64, string) {
	name, off := time.Unix(floorDiv(t, msPerSecond), 0).In(r.TimeZone()).Zone()
	return int64(off) * msPerSecond, name
}

// localTime implements LocalTime(t) for a finite integral time value.
func (r *Realm) localTime(t int64) int64 { return t + r.utcOffset(t) }

// utcFromLocal implements UTC(t) for a finite integral local time value
// (t - LocalTZA(t, false)). A local time repeated by a backward transition
// resolves to its first occurrence and a local time skipped by a forward
// transition is interpreted with the offset before the transition, as the
// spec requires. The candidate offsets are those one day either side of t,
// so a zone with two transitions within two days (none in current tzdata)
// may resolve a repeated or skipped time to the other candidate.
func (r *Realm) utcFromLocal(t int64) int64 {
	if r.TimeZone() == time.UTC {
		return t
	}
	early := r.utcOffset(t - msPerDay)
	late := r.utcOffset(t + msPerDay)
	if early == late {
		return t - early
	}
	u1, u2 := t-early, t-late
	ok1 := r.utcOffset(u1) == early
	ok2 := r.utcOffset(u2) == late
	switch {
	case ok1 && ok2:
		return min(u1, u2)
	case ok1:
		return u1
	case ok2:
		return u2
	}
	return t - early
}

// utc implements UTC(t) followed by TimeClip for a local time value.
func (r *Realm) utc(t float64) float64 {
	if !isFinite(t) || math.Abs(t) > maxLocalTimeValue {
		return math.NaN()
	}
	return timeClip(float64(r.utcFromLocal(int64(t))))
}
