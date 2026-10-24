package engine

// The Intl.Locale info methods (ECMA-402 §15.3.16-15.3.22) and
// Intl.supportedValuesOf (§8.3.2), from CLDR's supplemental data
// (intl_supp_tables.go) and the lists of the constructors.
//
// DateTimeFormat has the Gregorian calendar only, so getCalendars gives
// ["gregory"] (every region's preferences filtered by AvailableCalendars)
// unless the locale names a calendar. moejs has no Intl.Collator: the
// collations of getCollations and supportedValuesOf("collation") are
// CLDR's collation types, as ICU lists them.

import (
	"slices"
	"strings"
)

func installIntlLocaleInfo(r *Realm, x *intlIntrinsics) {
	a := intlInfoNames()
	r.installBuiltins(x.LocalePrototype, []builtinDef{
		{a.getCalendars, intlLocaleGetCalendars, 0},
		{a.getCollations, intlLocaleGetCollations, 0},
		{a.getHourCycles, intlLocaleGetHourCycles, 0},
		{a.getNumberingSystems, intlLocaleGetNumberingSystems, 0},
		{a.getTimeZones, intlLocaleGetTimeZones, 0},
		{a.getTextInfo, intlLocaleGetTextInfo, 0},
		{a.getWeekInfo, intlLocaleGetWeekInfo, 0},
	})
	r.installBuiltins(x.Intl, []builtinDef{{a.supportedValuesOf, intlSupportedValuesOf, 1}})
}

// intlLocaleKeywordArray returns the array of a Locale's keyword value
// when it has one, else undefined.
func (r *Realm) intlLocaleKeywordArray(l *intlLocale, key int) Value {
	if v := l.kw[key]; v != nil {
		return ObjectValue(r.NewArrayFromSlice([]Value{StringValue(v)}))
	}
	return Undefined()
}

// intlStringArray makes an array of strings.
func (r *Realm) intlStringArray(list []string) *Object {
	items := make([]Value, len(list))
	for i, s := range list {
		items[i] = StringValue(r.intlString(s))
	}
	return r.NewArrayFromSlice(items)
}

// intlLocaleGetCalendars implements getCalendars (CalendarsOfLocale).
func intlLocaleGetCalendars(r *Realm, this Value, args []Value) (Value, error) {
	l, err := thisLocale(r, this, "getCalendars")
	if err != nil {
		return Undefined(), err
	}
	if v := r.intlLocaleKeywordArray(l, localeKeyCa); !v.IsUndefined() {
		return v, nil
	}
	return ObjectValue(r.intlStringArray([]string{"gregory"})), nil
}

// intlLocaleGetCollations implements getCollations (CollationsOfLocale):
// the collation types of the longest prefix of the locale that CLDR has
// collation data for, else root's.
func intlLocaleGetCollations(r *Realm, this Value, args []Value) (Value, error) {
	l, err := thisLocale(r, this, "getCollations")
	if err != nil {
		return Undefined(), err
	}
	if v := r.intlLocaleKeywordArray(l, localeKeyCo); !v.IsUndefined() {
		return v, nil
	}
	list, _ := intlRecord(intlCollations, "root")
	has := func(tag string) bool { _, ok := intlRecord(intlCollations, tag); return ok }
	if m := intlLookupIn(l.str.GoString(), has); m != "" {
		list, _ = intlRecord(intlCollations, m)
	}
	return ObjectValue(r.intlStringArray(strings.Fields(list))), nil
}

// intlLocaleGetHourCycles implements getHourCycles (HourCyclesOfLocale):
// CLDR's preferred hour cycle of the locale's language and region.
func intlLocaleGetHourCycles(r *Realm, this Value, args []Value) (Value, error) {
	l, err := thisLocale(r, this, "getHourCycles")
	if err != nil {
		return Undefined(), err
	}
	if v := r.intlLocaleKeywordArray(l, localeKeyHc); !v.IsUndefined() {
		return v, nil
	}
	region, override := intlRegionPreference(l.tag)
	hc := ""
	for _, reg := range [...]string{override, region} {
		if reg == "" || hc != "" {
			continue
		}
		v, ok := intlRecord(intlHourData, l.tag.language()+"_"+reg)
		if !ok {
			v, ok = intlRecord(intlHourData, reg)
		}
		if ok {
			hc = intlTimeDataCycle(v[0])
		}
	}
	if hc == "" {
		hc = "h23"
	}
	return ObjectValue(r.intlStringArray([]string{hc})), nil
}

// intlTimeDataCycle maps a timeData hour symbol to its hour cycle.
func intlTimeDataCycle(c byte) string {
	switch c {
	case 'h':
		return "h12"
	case 'K':
		return "h11"
	case 'k':
		return "h24"
	}
	return "h23"
}

// intlLocaleGetNumberingSystems implements getNumberingSystems
// (NumberingSystemsOfLocale): the default numbering system of the
// NumberFormat locale the locale matches, else latn.
func intlLocaleGetNumberingSystems(r *Realm, this Value, args []Value) (Value, error) {
	l, err := thisLocale(r, this, "getNumberingSystems")
	if err != nil {
		return Undefined(), err
	}
	if v := r.intlLocaleKeywordArray(l, localeKeyNu); !v.IsUndefined() {
		return v, nil
	}
	nu := "latn"
	if m := intlLookupIn(l.str.GoString(), intlHasData); m != "" {
		nu = intlDefaultNumberingSystem(m)
	}
	return ObjectValue(r.intlStringArray([]string{nu})), nil
}

// intlLocaleGetTimeZones implements getTimeZones (TimeZonesOfLocale):
// undefined for a locale without a region.
func intlLocaleGetTimeZones(r *Realm, this Value, args []Value) (Value, error) {
	l, err := thisLocale(r, this, "getTimeZones")
	if err != nil {
		return Undefined(), err
	}
	region := l.tag.region()
	if region == "" {
		return Undefined(), nil
	}
	return ObjectValue(r.intlStringArray(intlZonesOfRegion(region))), nil
}

// intlLocaleGetTextInfo implements getTextInfo (TextDirectionOfLocale): the
// direction of the locale's script, or of its likely script.
func intlLocaleGetTextInfo(r *Realm, this Value, args []Value) (Value, error) {
	l, err := thisLocale(r, this, "getTextInfo")
	if err != nil {
		return Undefined(), err
	}
	a := intlInfoNames()
	script := l.tag.script()
	if script == "" {
		t := l.tag.clone()
		t.maximize()
		script = t.script()
	}
	dir := Undefined()
	if script != "" {
		dir = StringValue(a.ltr)
		for i := 0; i+4 <= len(intlRTLScripts); i += 4 {
			if intlRTLScripts[i:i+4] == script {
				dir = StringValue(a.rtl)
				break
			}
		}
	}
	o := r.NewObjectCap(1)
	o.addNamed(r, StringKey(a.direction), propCell{value: dir, attrs: attrDefault})
	return ObjectValue(o), nil
}

// intlLocaleGetWeekInfo implements getWeekInfo (WeekInfoOfLocale).
func intlLocaleGetWeekInfo(r *Realm, this Value, args []Value) (Value, error) {
	l, err := thisLocale(r, this, "getWeekInfo")
	if err != nil {
		return Undefined(), err
	}
	region, override := intlRegionPreference(l.tag)
	if override != "" {
		region = override
	}
	var week [4]int // firstDay, weekendStart, weekendEnd, minDays
	groups := intlWeekData
	for i := range week {
		g := groups
		if end := strings.IndexByte(groups, '|'); end >= 0 {
			g, groups = groups[:end], groups[end+1:]
		}
		week[i] = intlWeekValue(g, region)
	}
	if fw := l.kw[localeKeyFw]; fw != nil {
		if d := weekdayUValueToNumber(fw.GoString()); d != 0 {
			week[0] = d
		}
	}
	var weekend []Value
	for d := week[1]; ; d = d%7 + 1 {
		weekend = append(weekend, IntValue(d))
		if d == week[2] || len(weekend) == 7 {
			break
		}
	}
	slices.SortFunc(weekend, func(a, b Value) int { return int(a.AsNumber() - b.AsNumber()) })
	a := intlInfoNames()
	o := r.NewObjectCap(2)
	o.addNamed(r, StringKey(a.firstDay), propCell{value: IntValue(week[0]), attrs: attrDefault})
	o.addNamed(r, StringKey(a.weekend), propCell{value: ObjectValue(r.NewArrayFromSlice(weekend)), attrs: attrDefault})
	return ObjectValue(o), nil
}

// intlWeekValue returns a region's value in a group of intlWeekData: the
// first record listing the region, else the default record's.
func intlWeekValue(g, region string) int {
	def := 0
	for first := true; g != ""; first = false {
		end := strings.IndexByte(g, ';')
		rec := g[:end]
		g = g[end+1:]
		v, regions, _ := strings.Cut(rec, ":")
		n := 0
		for _, c := range v {
			n = n*10 + int(c-'0')
		}
		if first {
			def = n
			continue
		}
		for regions != "" {
			reg := regions
			if sp := strings.IndexByte(regions, ' '); sp >= 0 {
				reg, regions = regions[:sp], regions[sp+1:]
			} else {
				regions = ""
			}
			if reg == region {
				return n
			}
		}
	}
	return def
}

// weekdayUValueToNumber implements WeekdayUValueToNumber (ECMA-402
// §15.5.16): 0 for a string that is no first-day identifier.
func weekdayUValueToNumber(fw string) int {
	for i, d := range [...]string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"} {
		if fw == d {
			return i + 1
		}
	}
	return 0
}

// intlRegionPreference implements RegionPreference (ECMA-402 §15.5.8):
// the locale's region (from its region subtag, its -u-sd subdivision or
// its likely subtags; "001" when none) and its -u-rg override ("" when
// none).
func intlRegionPreference(t *langTag) (region, override string) {
	region = t.region()
	if region == "" {
		region = intlSubdivisionRegion(t, "sd")
	}
	if region == "" {
		m := t.clone()
		m.maximize()
		region = m.region()
	}
	if region == "" {
		region = "001"
	}
	return region, intlSubdivisionRegion(t, "rg")
}

// intlSubdivisionRegion implements CanonicalUnicodeSubdivision (ECMA-402
// §15.5.7): the canonical region of a -u-rg or -u-sd subdivision.
func intlSubdivisionRegion(t *langTag, key string) string {
	sd, ok := t.uKeyword(key)
	if !ok || len(sd) < 3 || len(sd) > 7 || !ltAllAlnum(sd) {
		return ""
	}
	n := 0
	switch {
	case ltIsAlpha(sd[0]) && ltIsAlpha(sd[1]):
		n = 2
	case len(sd) >= 4 && ltIsDigit(sd[0]) && ltIsDigit(sd[1]) && ltIsDigit(sd[2]):
		n = 3
	default:
		return ""
	}
	if len(sd)-n < 1 || len(sd)-n > 4 {
		return ""
	}
	c, ok := parseLangTag("und-" + sd[:n])
	if !ok {
		return ""
	}
	c.canonicalize()
	return c.region()
}

// intlSupportedValuesOf implements Intl.supportedValuesOf(key).
func intlSupportedValuesOf(r *Realm, this Value, args []Value) (Value, error) {
	k, err := r.ToString(intlArg(args, 0))
	if err != nil {
		return Undefined(), err
	}
	var list []string
	switch k.GoString() {
	case "calendar":
		list = []string{"gregory"}
	case "collation":
		list = intlAllCollations()
	case "currency":
		list = intlCurrencyCodes()
	case "numberingSystem":
		list = intlNumberingSystems()
	case "timeZone":
		list = intlUsableZoneIDs()
	case "unit":
		list = intlSimpleUnits()
	default:
		return Undefined(), r.RangeError("Invalid key : %s", k.GoString())
	}
	return ObjectValue(r.intlStringArray(list)), nil
}

// intlAllCollations returns every collation type of intlCollations,
// sorted.
func intlAllCollations() []string {
	var list []string
	s := intlCollations
	for s != "" {
		end := strings.IndexByte(s, ';')
		_, types, _ := strings.Cut(s[:end], ":")
		for _, t := range strings.Fields(types) {
			if !slices.Contains(list, t) {
				list = append(list, t)
			}
		}
		s = s[end+1:]
	}
	slices.Sort(list)
	return list
}
