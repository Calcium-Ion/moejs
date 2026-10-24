package engine

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	// Go's copy of the database, for a host without zoneinfo.
	_ "time/tzdata"

	"github.com/stretchr/testify/require"
)

// intlTZData returns the data index of a data locale.
func intlTZData(t testing.TB, tag string) int {
	t.Helper()
	i, ok := intlDataIndex(tag)
	require.True(t, ok, tag)
	return i
}

// intlTZMustZone resolves a zone the test needs.
func intlTZMustZone(t testing.TB, name string) *intlZone {
	t.Helper()
	z, msg := intlResolveZone(name)
	require.Empty(t, msg, name)
	require.NotNil(t, z, name)
	return z
}

func TestIntlTZIdentifiers(t *testing.T) {
	require.Equal(t, intlTZCount, strings.Count(intlTZNames, ";"))
	require.Equal(t, 4*intlTZCount, len(intlTZLinks))
	for i := 1; i < intlTZCount; i++ {
		require.Less(t, intlTZCompare(intlTZName(i-1), intlTZName(i)), 0, intlTZName(i))
	}
	for i := range intlTZCount {
		name := intlTZName(i)
		for _, s := range []string{name, strings.ToLower(name), strings.ToUpper(name)} {
			j, ok := intlTZFind(s)
			require.True(t, ok, s)
			require.Equal(t, i, j, s)
		}
		p := intlTZPrimary(i)
		require.Equal(t, p, intlTZPrimary(p), "the primary of %s is primary", name)
		require.Equal(t, intlTZTarget(intlTZTarget(i)), intlTZTarget(i), "%s links to a Zone", name)
	}
	for _, s := range []string{"", "MEZ", "Pacific Time", "cnsha", "invalid", "Europe/İstanbul", "asıa/baku",
		"europe/brußels", "Factory", "Etc/Unknown", "SystemV/AST4", "US/Pacific-New", "ACT", "PST", "IST", "UTC/", "Asia/Tokyo/"} {
		_, ok := intlTZFind(s)
		require.False(t, ok, s)
	}
}

func TestIntlResolveZone(t *testing.T) {
	for _, c := range [][2]string{
		// The identifier itself in the database's case (ECMA-402's
		// [[Identifier]], test262 canonical-tz).
		{"Asia/Calcutta", "Asia/Calcutta"}, {"asia/kolkata", "Asia/Kolkata"}, {"Etc/UTC", "Etc/UTC"},
		{"Etc/GMT", "Etc/GMT"}, {"GMT", "GMT"}, {"utc", "UTC"}, {"etc/uct", "Etc/UCT"},
		{"Europe/Bratislava", "Europe/Bratislava"}, {"Australia/Canberra", "Australia/Canberra"},
		{"Atlantic/Jan_Mayen", "Atlantic/Jan_Mayen"}, {"Pacific/Truk", "Pacific/Truk"},
		{"US/Eastern", "US/Eastern"}, {"EST5EDT", "EST5EDT"}, {"ETC/GMT-14", "Etc/GMT-14"},
		{"America/Argentina/ComodRivadavia", "America/Argentina/ComodRivadavia"},
		// Offset zones, normalized (FormatOffsetTimeZoneIdentifier).
		{"+05:30", "+05:30"}, {"-00", "+00:00"}, {"-00:00", "+00:00"}, {"+0000", "+00:00"},
		{"+0300", "+03:00"}, {"+03", "+03:00"}, {"-14", "-14:00"}, {"-2100", "-21:00"}, {"+2227", "+22:27"},
	} {
		z := intlTZMustZone(t, c[0])
		require.Equal(t, c[1], z.id, c[0])
	}
	for _, s := range []string{"+3", "+24", "+23:0", "+130", "+13234", "+135678", "-7", "-10.50", "-10,50",
		"-24", "-014", "-210", "-2400", "-1:10", "-21:0", "+0:003", "+15:59:00", "+15:59.50", "+15:59,50",
		"+222700", "-02:3200", "-170100", "-22230", "−0900", "−10:00", "−05", "+05:60", "+1260"} {
		z, msg := intlResolveZone(s)
		require.Nil(t, z, s)
		require.Equal(t, "Invalid time zone specified: "+s, msg)
	}
	// Identifiers of one primary share the zone's rules; zones are shared.
	a, b := intlTZMustZone(t, "Asia/Calcutta"), intlTZMustZone(t, "Asia/Kolkata")
	require.Equal(t, a.prim, b.prim)
	require.Same(t, a.loc, b.loc)
	require.Same(t, a, intlTZMustZone(t, "ASIA/CALCUTTA"))
	for _, s := range []string{"UTC", "Etc/UTC", "GMT", "Etc/GMT0", "Zulu"} {
		require.Same(t, time.UTC, intlTZMustZone(t, s).loc, s)
	}
}

func TestIntlZoneIDs(t *testing.T) {
	ids := intlZoneIDs()
	require.True(t, slices.IsSorted(ids))
	require.Len(t, ids, 445)
	for _, s := range []string{"UTC", "Etc/GMT+1", "Etc/GMT-14", "Asia/Kolkata", "Europe/Kyiv", "America/Argentina/Buenos_Aires",
		"Europe/Bratislava", "Arctic/Longyearbyen", "Pacific/Chuuk", "America/Nuuk", "Pacific/Kanton"} {
		require.True(t, slices.Contains(ids, s), s)
	}
	for _, s := range []string{"Etc/UTC", "Etc/GMT", "GMT", "Asia/Calcutta", "Europe/Kiev", "US/Eastern", "EST5EDT", "Factory"} {
		require.False(t, slices.Contains(ids, s), s)
	}
	// test262's isCanonicalizedStructurallyValidTimeZoneName (testIntl.js)
	// without its components that start with a dot (none does here, and Go's
	// regexp has no lookahead) and its SystemV names.
	component := `[A-Za-z_][A-Za-z.\-_]{0,13}`
	valid := regexp.MustCompile(`^(?:` + component + `(?:/` + component + `)*|(?:Etc/)?GMT[+-]\d{1,2}|CST6CDT|EST5EDT|MST7MDT|PST8PDT|NZ)$`)
	for _, id := range ids {
		z := intlTZMustZone(t, id)
		require.Equal(t, id, z.id)
		require.True(t, id == "UTC" || id != "Etc/UTC" && id != "Etc/GMT" && valid.MatchString(id), id)
	}
	require.Equal(t, []string{"America/Argentina/Buenos_Aires", "America/Argentina/Catamarca", "America/Argentina/Cordoba",
		"America/Argentina/Jujuy", "America/Argentina/La_Rioja", "America/Argentina/Mendoza", "America/Argentina/Rio_Gallegos",
		"America/Argentina/Salta", "America/Argentina/San_Juan", "America/Argentina/San_Luis", "America/Argentina/Tucuman",
		"America/Argentina/Ushuaia"}, intlZonesOfRegion("AR"))
	require.Equal(t, []string{"Asia/Tokyo"}, intlZonesOfRegion("JP"))
	require.Len(t, intlZonesOfRegion("US"), 29)
	require.Len(t, intlZonesOfRegion("001"), 27)
	require.Nil(t, intlZonesOfRegion("ZZ"))
	require.Nil(t, intlZonesOfRegion("419"))
}

func TestIntlZoneAt(t *testing.T) {
	tv := float64(time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC).UnixMilli())
	wall, off, dst := intlTZMustZone(t, "Asia/Tokyo").at(tv)
	require.Equal(t, "2026-10-22 09:00", wall.Format("2006-01-02 15:04"))
	require.Equal(t, 9*3600, off)
	require.False(t, dst)
	// Europe/Dublin: ICU's (rearguard) DST is the summer.
	dub := intlTZMustZone(t, "Europe/Dublin")
	_, off, dst = dub.at(float64(time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC).UnixMilli()))
	require.Equal(t, 0, off)
	require.False(t, dst)
	_, off, dst = dub.at(float64(time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC).UnixMilli()))
	require.Equal(t, 3600, off)
	require.True(t, dst)
	// LMT before the first transition, the last rule at the end of time.
	ny := intlTZMustZone(t, "America/New_York")
	_, off, _ = ny.at(-8.64e15)
	require.Equal(t, -(4*3600 + 56*60 + 2), off)
	_, off, dst = ny.at(8.64e15)
	require.Equal(t, -4*3600, off)
	require.True(t, dst)
	_, off, _ = intlTZMustZone(t, "+05:30").at(0)
	require.Equal(t, 5*3600+30*60, off)
	_, off, _ = intlTZMustZone(t, "Etc/GMT+5").at(0)
	require.Equal(t, -5*3600, off)
}

func TestIntlZoneNames(t *testing.T) {
	en := intlTZData(t, "en")
	styles := []intlZoneNameStyle{intlZoneShort, intlZoneLong, intlZoneShortOffset, intlZoneLongOffset, intlZoneShortGeneric, intlZoneLongGeneric}
	names := func(zone, locale string, tm time.Time) string {
		z := intlTZMustZone(t, zone)
		var out []string
		for _, s := range styles {
			out = append(out, string(z.appendName(nil, intlTZData(t, locale), s, float64(tm.UnixMilli()))))
		}
		return strings.Join(out, " | ")
	}
	oct := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	jan := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	jul := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	require.Equal(t, "GMT+9 | Japan Standard Time | GMT+9 | GMT+09:00 | Japan Time | Japan Standard Time", names("Asia/Tokyo", "en", oct))
	require.Equal(t, "PST | Pacific Standard Time | GMT-8 | GMT-08:00 | PT | Pacific Time", names("America/Los_Angeles", "en", jan))
	require.Equal(t, "PDT | Pacific Daylight Time | GMT-7 | GMT-07:00 | PT | Pacific Time", names("America/Los_Angeles", "en", jul))
	require.Equal(t, "GMT | Greenwich Mean Time | GMT+0 | GMT+00:00 | Ireland Time | Ireland Time", names("Europe/Dublin", "en", jan))
	require.Equal(t, "GMT+1 | Irish Standard Time | GMT+1 | GMT+01:00 | Ireland Time | Ireland Time", names("Europe/Dublin", "en", jul))
	require.Equal(t, "UTC | Coordinated Universal Time | GMT+0 | GMT+00:00 | GMT+0 | GMT+00:00", names("UTC", "en", jan))
	require.Equal(t, "GMT+5:30 | GMT+05:30 | GMT+5:30 | GMT+05:30 | GMT+5:30 | GMT+05:30", names("+05:30", "en", jan))
	require.Equal(t, "GMT | Greenwich Mean Time | GMT+0 | GMT+00:00 | GMT | Greenwich Mean Time", names("+00:00", "en", jan))
	require.Equal(t, "UTC+0 | heure moyenne de Greenwich | UTC+0 | UTC+00:00 | heure : Côte d’Ivoire | heure moyenne de Greenwich", names("Africa/Abidjan", "fr", jan))
	require.Equal(t, "MST | Mountain Standard Time | GMT-7 | GMT-07:00 | MST | Mountain Standard Time", names("America/Phoenix", "en", jul))
	require.Equal(t, "GMT+1 | GMT+01:00 | GMT+1 | GMT+01:00 | United Kingdom Time | United Kingdom Time", names("Europe/London", "en", time.Date(1971, 1, 1, 0, 0, 0, 0, time.UTC)))
	// The region of the generic styles' reference zone: where Vancouver's
	// offset is not Los Angeles's (from 2026-11-01 Vancouver keeps
	// UTC-07:00 in tzdata 2026c and later, ICU's permanent PDT), for US the
	// name gets its location; for CA, whose reference zone Vancouver is,
	// it never does. The offsets are the host database's, the zones' rules.
	van := intlTZMustZone(t, "America/Vancouver")
	for _, tm := range []time.Time{jan, time.Date(2045, 1, 15, 0, 0, 0, 0, time.UTC)} {
		tv := float64(tm.UnixMilli())
		us := "Pacific Time"
		_, vanOff := tm.In(van.loc).Zone()
		_, laOff := tm.In(intlTZMustZone(t, "America/Los_Angeles").loc).Zone()
		if vanOff != laOff {
			us = "Pacific Time (Canada)"
		}
		require.Equal(t, us, string(van.appendNameFor(nil, en, "US", intlZoneLongGeneric, tv)), tm)
		require.Equal(t, us, string(van.appendName(nil, en, intlZoneLongGeneric, tv)), tm)
		require.Equal(t, "Pacific Time", string(van.appendNameFor(nil, en, "CA", intlZoneLongGeneric, tv)), tm)
	}
	old := time.Date(1800, 1, 1, 0, 0, 0, 0, time.UTC)
	require.Equal(t, "GMT-4:56:02 | GMT-04:56:02 | GMT-4:56:02 | GMT-04:56:02 | New York Time | New York Time", names("America/New_York", "en", old))
}

// TestIntlZoneNoDatabase loads zones as a host without zoneinfo would: UTC,
// Etc/GMT±n and the offset zones work, a named zone throws the RangeError
// that says how to add one, and a Link the database lacks loads its Zone.
func TestIntlZoneNoDatabase(t *testing.T) {
	st := newIntlTZState()
	var asked []string
	st.load = func(name string) (*time.Location, error) {
		asked = append(asked, name)
		return nil, errors.New("unknown time zone " + name)
	}
	for _, name := range []string{"UTC", "Etc/UTC", "GMT", "Etc/GMT+5", "Etc/GMT-14"} {
		i, ok := intlTZFind(name)
		require.True(t, ok)
		z, msg := st.zone(i)
		require.Empty(t, msg, name)
		require.Equal(t, name, z.id)
	}
	i, _ := intlTZFind("asia/tokyo")
	z, msg := st.zone(i)
	require.Nil(t, z)
	require.Equal(t, `Time zone Asia/Tokyo is unavailable: the host has no time zone database for it `+
		`(install the system's tzdata package, or add import _ "time/tzdata" to the Go program)`, msg)
	require.Equal(t, []string{"Asia/Tokyo"}, asked)
	require.Nil(t, st.zones[i].Load(), "a failure is not cached")
	// The failure is remembered for a while, then a database installed
	// meanwhile is picked up.
	_, msg = st.zone(i)
	require.NotEmpty(t, msg)
	require.Equal(t, []string{"Asia/Tokyo"}, asked)
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	require.NoError(t, err)
	st.load = func(name string) (*time.Location, error) {
		asked = append(asked, name)
		return tokyo, nil
	}
	st.failed[intlTZPrimary(i)] = time.Now().Add(-intlZoneRetry)
	z, msg = st.zone(i)
	require.Empty(t, msg)
	require.Same(t, tokyo, z.loc)
	require.Equal(t, []string{"Asia/Tokyo", "Asia/Tokyo"}, asked)
	// A primary Link the database lacks loads its Zone.
	asked = nil
	prague, err := time.LoadLocation("Europe/Prague")
	require.NoError(t, err)
	st.load = func(name string) (*time.Location, error) {
		asked = append(asked, name)
		if name == "Europe/Prague" {
			return prague, nil
		}
		return nil, errors.New("unknown time zone " + name)
	}
	i, _ = intlTZFind("Europe/Bratislava")
	z, msg = st.zone(i)
	require.Empty(t, msg)
	require.Equal(t, "Europe/Bratislava", z.id)
	require.Same(t, prague, z.loc)
	require.Equal(t, []string{"Europe/Bratislava", "Europe/Prague"}, asked)
	// A Link loads its primary's rules, once.
	asked = nil
	i, _ = intlTZFind("Asia/Calcutta")
	kolkata, err := time.LoadLocation("Asia/Kolkata")
	require.NoError(t, err)
	st.load = func(name string) (*time.Location, error) {
		asked = append(asked, name)
		return kolkata, nil
	}
	z, _ = st.zone(i)
	j, _ := intlTZFind("Asia/Kolkata")
	z2, _ := st.zone(j)
	require.Same(t, z.loc, z2.loc)
	require.Equal(t, []string{"Asia/Kolkata"}, asked)
}

func TestIntlZoneOfLocation(t *testing.T) {
	load := func(name string) *time.Location {
		loc, err := time.LoadLocation(name)
		require.NoError(t, err)
		return loc
	}
	ny := load("America/New_York")
	data, err := os.ReadFile("/usr/share/zoneinfo/America/New_York")
	if err != nil {
		t.Skip("no system zoneinfo:", err)
	}
	custom, err := time.LoadLocationFromTZData("Custom/Zone", data)
	require.NoError(t, err)
	eastern, err := time.LoadLocationFromTZData("US/Eastern", data)
	require.NoError(t, err)
	jan := float64(time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC).UnixMilli())
	for _, c := range []struct {
		loc  *time.Location
		id   string
		off  int
		name string // the en long name in January 2026
	}{
		{time.UTC, "UTC", 0, "Coordinated Universal Time"},
		{ny, "America/New_York", -5 * 3600, "Eastern Standard Time"},
		{load("Europe/Bratislava"), "Europe/Bratislava", 3600, "Central European Standard Time"},
		{eastern, "America/New_York", -5 * 3600, "Eastern Standard Time"}, // a Link: its primary
		{custom, "UTC", -5 * 3600, "GMT-05:00"},                           // unknown: UTC, loc's offsets
		{time.FixedZone("X", 5*3600+1800), "+05:30", 5*3600 + 1800, "GMT+05:30"},
		{time.FixedZone("", 0), "+00:00", 0, "Greenwich Mean Time"},
		{time.FixedZone("UTC", 0), "UTC", 0, "Coordinated Universal Time"},
		{time.FixedZone("Odd", 30), "UTC", 30, "GMT+00:00:30"},
	} {
		z := intlZoneOfLocation(c.loc)
		require.Equal(t, c.id, z.id, c.loc.String())
		require.Same(t, c.loc, z.loc)
		_, off, _ := z.at(jan)
		require.Equal(t, c.off, off, c.loc.String())
		require.Equal(t, c.name, string(z.appendName(nil, intlTZData(t, "en"), intlZoneLong, jan)), c.loc.String())
		require.Same(t, z, intlZoneOfLocation(c.loc), "cached")
	}
	for _, c := range [][2]string{
		{"/usr/share/zoneinfo/Asia/Tokyo", "Asia/Tokyo"}, {"/usr/share/zoneinfo/posix/Asia/Tokyo", "Asia/Tokyo"},
		{"/var/db/timezone/zoneinfo/Europe/Paris", "Europe/Paris"}, {"/etc/nonexistent", ""},
	} {
		require.Equal(t, c[1], intlZoneNameOfPath(c[0]), c[0])
	}
}

// TestIntlLocalZone runs the test binary with several values of TZ and
// checks the zone of time.Local.
func TestIntlLocalZone(t *testing.T) {
	jan := float64(time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC).UnixMilli())
	if os.Getenv("MOEJS_INTL_TZ_CHILD") != "" {
		z := intlZoneOfLocation(time.Local)
		_, off, _ := z.at(jan)
		fmt.Printf("%s %d\n", z.id, off)
		return
	}
	if _, err := os.Stat("/usr/share/zoneinfo/Europe/Bratislava"); err != nil {
		t.Skip("no system zoneinfo:", err)
	}
	run := func(env ...string) string {
		cmd := exec.Command(os.Args[0], "-test.run=^TestIntlLocalZone$")
		for _, e := range os.Environ() {
			if !strings.HasPrefix(e, "TZ=") {
				cmd.Env = append(cmd.Env, e)
			}
		}
		cmd.Env = append(cmd.Env, append(env, "MOEJS_INTL_TZ_CHILD=1")...)
		out, err := cmd.Output()
		require.NoError(t, err, env)
		line, _, _ := strings.Cut(string(out), "\n")
		return line
	}
	for _, c := range [][2]string{
		{"Asia/Tokyo", "Asia/Tokyo 32400"}, {":Asia/Tokyo", "Asia/Tokyo 32400"},
		{"/usr/share/zoneinfo/Asia/Tokyo", "Asia/Tokyo 32400"}, {":/usr/share/zoneinfo/Asia/Tokyo", "Asia/Tokyo 32400"},
		{"/usr/share/zoneinfo/Europe/Bratislava", "Europe/Bratislava 3600"}, {"America/New_York", "America/New_York -18000"},
		{"", "UTC 0"}, {"UTC", "UTC 0"}, {"Etc/UTC", "UTC 0"}, {"Foo/Bar", "UTC 0"}, {"UTC0", "UTC 0"},
	} {
		require.Equal(t, c[1], run("TZ="+c[0]), "TZ=%q", c[0])
	}
	// TZ unset: Go's time.Local has the rules of /etc/localtime (UTC
	// without one), which intlSystemZoneName names by its link, else by
	// /etc/timezone (a copied /etc/localtime has no link).
	want := "UTC 0"
	if data, err := os.ReadFile("/etc/localtime"); err == nil {
		name := intlZoneNameOfPath("/etc/localtime")
		if b, err := os.ReadFile("/etc/timezone"); name == "" && err == nil {
			name = strings.TrimSpace(string(b))
		}
		if loc, err := time.LoadLocationFromTZData(name, data); err == nil {
			z := intlNewLocalZone(loc)
			_, off, _ := z.at(jan)
			want = fmt.Sprintf("%s %d", z.id, off)
		}
	}
	require.Equal(t, want, run(), "TZ unset")
}

// TestIntlZoneConcurrent resolves and names every zone from several
// goroutines (run with -race).
func TestIntlZoneConcurrent(t *testing.T) {
	ids := intlZoneIDs()
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var b []byte
			for k := range ids {
				id := ids[(k+g*57)%len(ids)]
				z, msg := intlResolveZone(strings.ToLower(id))
				if msg != "" {
					t.Error(msg)
					return
				}
				b = z.appendName(b[:0], g%intlDataLocaleCount, intlZoneLongGeneric, 1.7e12)
			}
		}()
	}
	wg.Wait()
}

// TestIntlZoneAllocs checks that at() and appendName into a buffer with
// room allocate nothing once the zone and the locale are decoded.
func TestIntlZoneAllocs(t *testing.T) {
	b := make([]byte, 0, 128)
	for _, zone := range []string{"America/Los_Angeles", "Asia/Tokyo", "Africa/Casablanca", "America/Phoenix", "Europe/Dublin", "+05:30"} {
		z := intlTZMustZone(t, zone)
		for _, data := range []int{intlTZData(t, "en"), intlTZData(t, "ja"), intlTZData(t, "fr")} {
			for s := intlZoneShort; s <= intlZoneLongGeneric; s++ {
				z.appendName(b[:0], data, s, 1.7e12)
				n := testing.AllocsPerRun(100, func() { b = z.appendName(b[:0], data, s, 1.7e12) })
				require.Zero(t, n, "%s %d %d", zone, data, s)
			}
		}
		n := testing.AllocsPerRun(100, func() { z.at(1.7e12) })
		require.Zero(t, n, zone)
	}
	n := testing.AllocsPerRun(100, func() { intlResolveZone("america/los_angeles") })
	require.Zero(t, n)
}

func BenchmarkIntlResolveZone(b *testing.B) {
	intlResolveZone("America/Los_Angeles")
	for b.Loop() {
		intlResolveZone("america/los_angeles")
	}
}

func BenchmarkIntlZoneAt(b *testing.B) {
	// Hourly through 2026, then through 2100, after the TZif data's last
	// transition (Go's POSIX rule).
	for _, c := range []struct {
		name string
		year int
	}{{"2026", 2026}, {"2100", 2100}} {
		b.Run(c.name, func(b *testing.B) {
			z, _ := intlResolveZone("America/Los_Angeles")
			start := float64(time.Date(c.year, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli())
			tv := start
			for b.Loop() {
				z.at(tv)
				if tv += 3.6e6; tv >= start+365*86400e3 {
					tv = start
				}
			}
		})
	}
}

func BenchmarkIntlZoneName(b *testing.B) {
	styles := []string{"short", "long", "shortOffset", "longOffset", "shortGeneric", "longGeneric"}
	for _, zone := range []string{"America/Los_Angeles", "Asia/Tokyo"} {
		for _, loc := range []string{"en", "ja"} {
			for s := intlZoneShort; s <= intlZoneLongGeneric; s++ {
				b.Run(zone+"/"+loc+"/"+styles[s-1], func(b *testing.B) {
					z, _ := intlResolveZone(zone)
					data := intlTZData(b, loc)
					buf := make([]byte, 0, 64)
					for b.Loop() {
						buf = z.appendName(buf[:0], data, s, 1.7e12)
					}
				})
			}
		}
	}
}

// BenchmarkIntlTZFirstUse measures the decoding the first use of each part
// does: the identifier table, a zone's record, a locale's names.
func BenchmarkIntlTZFirstUse(b *testing.B) {
	b.Run("identifiers", func(b *testing.B) {
		for b.Loop() {
			newIntlTZState()
		}
	})
	b.Run("zoneInfo", func(b *testing.B) {
		i, _ := intlTZFind("America/Los_Angeles")
		for b.Loop() {
			intlDecodeZoneInfo(i)
		}
	})
	for _, loc := range []string{"en", "ja", "ru"} {
		b.Run("names/"+loc, func(b *testing.B) {
			s := intlTZLocaleNames[intlTZData(b, loc)]
			for b.Loop() {
				intlDecodeTZLocale(s)
			}
		})
	}
	b.Run("load/America/Los_Angeles", func(b *testing.B) {
		for b.Loop() {
			st := newIntlTZState()
			i, _ := intlTZFind("America/Los_Angeles")
			st.zone(i)
		}
	})
}

// TestIntlFailedZoneLoadRemembered: a zone the host has no rules for is not
// looked up in the zoneinfo sources again on every use.
func TestIntlFailedZoneLoadRemembered(t *testing.T) {
	st := newIntlTZState()
	n := 0
	st.load = func(name string) (*time.Location, error) {
		n++
		return nil, errors.New("unknown time zone " + name)
	}
	i, _ := intlTZFind("America/New_York")
	for range 1000 {
		z, _ := st.zone(i)
		require.Nil(t, z)
	}
	require.Equal(t, 1, n, "a failed load is retried on every use")
}

// TestIntlSupportedZonesLoad: on a host without zoneinfo,
// Intl.supportedValuesOf("timeZone") lists only zones a DateTimeFormat
// accepts (test262's timeZones-accepted-by-DateTimeFormat), UTC and
// Etc/GMT±n among them.
func TestIntlSupportedZonesLoad(t *testing.T) {
	if os.Getenv("MOEJS_NOZONE_CHILD") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestIntlSupportedZonesLoad$", "-test.count=1")
		cmd.Env = append(os.Environ(), "MOEJS_NOZONE_CHILD=1")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "child:\n%s", out)
		return
	}
	intlTZ().load = func(name string) (*time.Location, error) { return nil, errors.New("unknown time zone " + name) }
	r := NewRealmWith(RealmOptions{TimeZone: time.UTC})
	got := evalIn(t, r, `const zones = Intl.supportedValuesOf("timeZone"), bad = [];
for (const z of zones) { try { new Intl.DateTimeFormat("en", {timeZone: z}) } catch (e) { bad.push(z) } }
return [bad.length, zones.length, zones.every(z => z == "UTC" || /^Etc\/GMT[-+]\d+$/.test(z))].join()`)
	require.Equal(t, "0,27,true", got, "supportedValuesOf lists zones DateTimeFormat rejects")
}

// TestIntlSupportedZonesConcurrent: on a host where some zones do not
// load, 32 goroutines calling Intl.supportedValuesOf("timeZone") at once
// all get the same list, each listed zone constructs a DateTimeFormat, and
// none that fails is listed. Zones that load only later are listed once
// their failures expire, as DateTimeFormat then accepts them.
func TestIntlSupportedZonesConcurrent(t *testing.T) {
	if os.Getenv("MOEJS_INTL_USABLE_CHILD") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestIntlSupportedZonesConcurrent$", "-test.count=1", "-test.v")
		cmd.Env = append(os.Environ(), "MOEJS_INTL_USABLE_CHILD=1")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "child:\n%s", out)
		t.Logf("child:\n%s", out)
		return
	}
	st := intlTZ()
	var mu sync.Mutex
	broken := true
	st.load = func(name string) (*time.Location, error) {
		mu.Lock()
		b := broken
		mu.Unlock()
		if b && strings.Contains(name, "o") {
			return nil, errors.New("unknown time zone " + name)
		}
		return time.LoadLocation(name)
	}
	const n = 32
	lists := make([]string, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for g := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			r := NewRealmWith(RealmOptions{SharedIntrinsics: g%2 == 0, TimeZone: time.UTC})
			lists[g] = evalIn(t, r, `const z = Intl.supportedValuesOf("timeZone"); for (const x of z) new Intl.DateTimeFormat("en", {timeZone: x}); return z.join()`)
		}()
	}
	close(start)
	wg.Wait()
	for g := range n {
		require.Equal(t, lists[0], lists[g], "goroutine %d", g)
	}
	zones := strings.Split(lists[0], ",")
	for _, z := range zones {
		p, ok := intlTZFind(z)
		require.True(t, ok, z)
		if name, target := intlTZName(p), intlTZName(intlTZTarget(p)); strings.Contains(name, "o") && strings.Contains(target, "o") && !strings.HasPrefix(z, "Etc/") {
			t.Errorf("a zone that does not load is listed: %s", z)
		}
	}
	require.Less(t, len(zones), len(intlZoneIDs()), "the zones that do not load are left out")
	require.True(t, slices.IsSorted(zones))
	// The host's zoneinfo appears later: once the failures expire, the
	// zones load, and the list, which lacked some, is made again.
	mu.Lock()
	broken = false
	mu.Unlock()
	r := NewRealmWith(RealmOptions{TimeZone: time.UTC})
	const probe = `return Intl.supportedValuesOf("timeZone").includes("Asia/Tokyo") + ", " + Intl.supportedValuesOf("timeZone").length`
	require.Equal(t, fmt.Sprintf("false, %d", len(zones)), evalIn(t, r, probe), "within the retry time")
	st.mu.Lock()
	for p := range st.failed {
		st.failed[p] = time.Now().Add(-intlZoneRetry)
	}
	st.mu.Unlock()
	intlUsableZones.mu.Lock()
	intlUsableZones.at = time.Now().Add(-intlZoneRetry)
	intlUsableZones.mu.Unlock()
	require.Equal(t, fmt.Sprintf("true, %d", len(intlZoneIDs())), evalIn(t, r, probe), "after it")
	t.Logf("%d of %d zones listed before the database was complete", len(zones), len(intlZoneIDs()))
}
