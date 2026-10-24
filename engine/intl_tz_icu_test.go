package engine

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// intlTZFixture is testdata/intl_tz_icu.json.gz (intl_tz_icu.js).
type intlTZFixture struct {
	Node, ICU, TZ, CLDR string
	R                   []string // per identifier: Node's resolvedOptions().timeZone, "!" and the error
	O                   map[string]struct {
		I, S int        // the offset at -8.64e15 and at 1800-01-01
		T    string     // the transitions: "seconds since the previous:offset" in base 36, ','-separated
		F    [][2]int64 // the far instants: [time value / 1000, offset]
	}
	// N holds per locale its distinct names U and per zone the indexes
	// (base 36, ','-separated, index*n for a run) of its names, styles × T
	// in order.
	N map[string]struct {
		U []string
		Z map[string]string
		T []float64 // the instants
	}
	S []string            // Intl.supportedValuesOf("timeZone")
	G map[string][]string // region → getTimeZones()
}

func loadIntlTZFixture(t *testing.T) *intlTZFixture {
	t.Helper()
	f, err := os.Open("testdata/intl_tz_icu.json.gz")
	require.NoError(t, err)
	defer f.Close()
	zr, err := gzip.NewReader(f)
	require.NoError(t, err)
	data, err := io.ReadAll(zr)
	require.NoError(t, err)
	var fx intlTZFixture
	require.NoError(t, json.Unmarshal(data, &fx))
	t.Logf("Node %s, ICU %s, tzdata %s, CLDR %s; moejs tzdata %s, system zoneinfo %s", fx.Node, fx.ICU, fx.TZ, fx.CLDR, tzdataVersion, intlTZSystemVersion())
	return &fx
}

// intlTZSystemVersion returns the release of the system's zoneinfo
// (tzdata.zi's first line), "" when unknown.
func intlTZSystemVersion() string {
	b, err := os.ReadFile("/usr/share/zoneinfo/tzdata.zi")
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(string(b), "\n")
	return strings.TrimPrefix(line, "# version ")
}

// intlTZPrimaryOf returns the primary identifier of a name, "" when it is
// none.
func intlTZPrimaryOf(name string) string {
	i, ok := intlTZFind(name)
	if !ok {
		return ""
	}
	return intlTZName(intlTZPrimary(i))
}

// intlTZBackzone reports whether a primary zone is one that the default
// build of tzdata (ICU's) makes a Link to another zone, and the other zone's
// offset at a time value: a zoneinfo built with PACKRATDATA=backzone
// (Debian's, Go's embedded copy) keeps the zone's own pre-1970 history, ICU
// has the target's.
func intlTZBackzone(zone string, tv float64) (int, bool) {
	i, _ := intlTZFind(zone)
	tg := intlTZTarget(i)
	if tg == i {
		return 0, false
	}
	loc, err := time.LoadLocation(intlTZName(tg))
	if err != nil {
		return 0, false
	}
	_, off := time.UnixMilli(int64(tv)).In(loc).Zone()
	return off, true
}

// TestIntlTZICU compares the identifiers, offsets and names with Node's.
// Every difference is either listed by its reason or fails the test.
func TestIntlTZICU(t *testing.T) {
	fx := loadIntlTZFixture(t)
	require.Len(t, fx.R, intlTZCount, "the fixture is of these tables")

	// Identifiers: Node canonicalizes to CLDR's identifiers (V8 does not
	// implement canonical-tz), moejs keeps the identifier
	// (GetAvailableNamedTimeZoneIdentifier's [[Identifier]]).
	canon := map[string]int{}
	for i, r := range fx.R {
		id := intlTZName(i)
		if strings.HasPrefix(r, "!") {
			t.Errorf("Node rejects %s: %s", id, r)
			continue
		}
		if p := intlTZPrimaryOf(r); p != intlTZName(intlTZPrimary(i)) {
			t.Errorf("%s: Node's %s has the primary %s, moejs's primary is %s", id, r, p, intlTZName(intlTZPrimary(i)))
		}
		if r != id {
			canon[r]++
		}
	}
	t.Logf("identifiers: %d, Node reports another for %d (its CLDR canonical identifier or UTC)", intlTZCount, len(canon))

	// supportedValuesOf: Node lists CLDR's canonical identifiers and leaves
	// out UTC and Etc/GMT±n.
	var nodeSet []string
	for _, s := range fx.S {
		p := intlTZPrimaryOf(s)
		require.NotEmpty(t, p, s)
		nodeSet = append(nodeSet, p)
	}
	slices.Sort(nodeSet)
	var missing []string
	for _, id := range intlZoneIDs() {
		if _, ok := slices.BinarySearch(nodeSet, id); !ok {
			missing = append(missing, id)
			if id != "UTC" && !strings.HasPrefix(id, "Etc/GMT") {
				t.Errorf("supportedValuesOf: Node lacks %s", id)
			}
		}
	}
	require.Equal(t, len(intlZoneIDs()), len(nodeSet)+len(missing))
	renamed := 0
	for _, s := range fx.S {
		if p := intlTZPrimaryOf(s); p != s {
			renamed++
		}
	}
	t.Logf("supportedValuesOf: moejs %d, Node %d (%d by CLDR's identifier); Node lacks %v", len(intlZoneIDs()), len(fx.S), renamed, missing)

	// getTimeZones per region.
	for region, zones := range fx.G {
		var want []string
		for _, z := range zones {
			p := intlTZPrimaryOf(z)
			// Node's 001 has the SystemV zones (ICU's, not tzdata's) and
			// both Etc/GMT and Etc/UTC (UTC's identifiers).
			if p == "" && region == "001" && strings.HasPrefix(z, "SystemV/") {
				continue
			}
			if p != "" && !slices.Contains(want, p) {
				want = append(want, p)
			}
		}
		sort.Strings(want)
		require.Equal(t, want, intlZonesOfRegion(region), region)
	}
	t.Logf("getTimeZones: %d regions equal", len(fx.G))

	// The offsets and names depend on the time zone database: they are
	// compared on a system zoneinfo of the fixture's release.
	if v := intlTZSystemVersion(); v != fx.TZ {
		t.Skipf("offsets and names: the system zoneinfo is %q, the fixture's tzdata %s", v, fx.TZ)
	}

	// Offsets.
	checks, backzone := 0, map[string]int{}
	check := func(zone string, tv float64, want int, what string) {
		z := intlTZMustZone(t, zone)
		_, got, _ := z.at(tv)
		checks++
		if got == want {
			return
		}
		if off, ok := intlTZBackzone(zone, tv); ok && off == want && tv < 0 {
			backzone[zone]++
			return
		}
		t.Errorf("%s %s at %s: offset %d, Node %d", zone, what, time.UnixMilli(int64(tv)).UTC().Format(time.RFC3339), got, want)
	}
	for zone, o := range fx.O {
		check(zone, -8.64e15, o.I, "far past")
		check(zone, float64(time.Date(1800, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()), o.S, "1800")
		prev, sec := o.S, time.Date(1800, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
		for _, tr := range strings.Split(o.T, ",") {
			if tr == "" {
				continue
			}
			ds, os, _ := strings.Cut(tr, ":")
			d, err1 := strconv.ParseInt(ds, 36, 64)
			off, err2 := strconv.ParseInt(os, 36, 32)
			require.NoError(t, errors.Join(err1, err2), tr)
			sec += d
			check(zone, float64(sec*1000-1000), prev, "before a transition")
			check(zone, float64(sec*1000), int(off), "at a transition")
			prev = int(off)
		}
		for _, f := range o.F {
			check(zone, float64(f[0]*1000), int(f[1]), "far future")
		}
	}
	var bz []string
	for z, n := range backzone {
		bz = append(bz, fmt.Sprintf("%s %d", z, n))
	}
	slices.Sort(bz)
	t.Logf("offsets: %d checks, %d zones; pre-1970 backzone history of the system zoneinfo (ICU has the default build's Link target): %d zones %v",
		checks, len(fx.O), len(backzone), bz)

	// Names.
	styles := []intlZoneNameStyle{intlZoneShort, intlZoneLong, intlZoneShortOffset, intlZoneLongOffset, intlZoneShortGeneric, intlZoneLongGeneric}
	styleNames := []string{"short", "long", "shortOffset", "longOffset", "shortGeneric", "longGeneric"}
	names, diffs := 0, map[string]int{}
	failed := map[string]int{} // zone → reported failures
	for loc, ln := range fx.N {
		data := intlTZData(t, loc)
		for zone, list := range ln.Z {
			z := intlTZMustZone(t, zone)
			var idx []int
			for _, run := range strings.Split(list, ",") {
				x, ns, _ := strings.Cut(run, "*")
				n, err := strconv.Atoi(ns)
				if ns == "" {
					n, err = 1, nil
				}
				require.NoError(t, err, run)
				v, err := strconv.ParseInt(x, 36, 32)
				require.NoError(t, err, run)
				for range n {
					idx = append(idx, int(v))
				}
			}
			require.Len(t, idx, len(styles)*len(ln.T), zone)
			for k, n := range idx {
				want := ln.U[n]
				s, tv := k/len(ln.T), ln.T[k%len(ln.T)]
				got := string(z.appendName(nil, data, styles[s], tv))
				names++
				if got == want {
					continue
				}
				if why := intlTZNameDiff(zone, tv); why != "" {
					diffs[why]++
					continue
				}
				if failed[zone]++; failed[zone] <= 6 {
					t.Errorf("%s %s %s at %s: %q, Node %q", loc, zone, styleNames[s], time.UnixMilli(int64(tv)).UTC().Format("2006-01-02"), got, want)
				}
			}
		}
	}
	t.Logf("names: %d compared; allowed differences %v", names, diffs)
	if len(failed) > 0 {
		t.Errorf("names differ in %d zones: %v", len(failed), failed)
	}
}

// intlTZNameDiff returns the reason a name may differ from Node's, "" for
// none.
func intlTZNameDiff(zone string, tv float64) string {
	z, _ := intlResolveZone(zone)
	if z == nil || z.prim < 0 {
		return ""
	}
	if off, ok := intlTZBackzone(zone, tv); ok && tv < 0 {
		if _, got, _ := z.at(tv); got != off {
			return "backzone offset before 1970"
		}
	}
	return ""
}
