package engine

// The toLocaleString methods of ECMA-402: Number and BigInt
// format with an Intl.NumberFormat, Date with an Intl.DateTimeFormat, made
// from the method's locales and options. Without both arguments the
// formatter depends only on the realm's default locale (and time zone), so
// the realm keeps one (realmIntl) and such a call allocates only its
// result. The Intl global is not defined for them: the formatters need
// none of its objects.

import "time"

// realmIntlCache holds the formatters of the toLocaleString methods called
// without locales and options.
type realmIntlCache struct {
	nf *intlNumberFormat
	// dtf are Date's toLocaleString, toLocaleDateString and
	// toLocaleTimeString formatters, made for the time zone dtfZone.
	dtf     [3]*intlDateTimeFormat
	dtfZone *time.Location
}

// intlNumberFormatFor returns the NumberFormat of Number.prototype and
// BigInt.prototype.toLocaleString(locales, options).
func (r *Realm) intlNumberFormatFor(locales, options Value) (*intlNumberFormat, error) {
	if !locales.IsUndefined() || !options.IsUndefined() {
		return r.intlNewNumberFormat(locales, options)
	}
	st := r.intlState()
	if st.cache.nf == nil {
		nf, err := r.intlNewNumberFormat(locales, options)
		if err != nil {
			return nil, err
		}
		st.cache.nf = nf
	}
	return st.cache.nf, nil
}

// The Date.prototype toLocale*String methods, by their index in
// realmIntlCache.dtf.
const (
	intlToLocaleString = iota
	intlToLocaleDateString
	intlToLocaleTimeString
)

// intlDateTimeFormatFor returns the DateTimeFormat of the toLocale*String
// method of Date.prototype with index method (CreateDateTimeFormat with
// required any/date/time and defaults all/date/time).
func (r *Realm) intlDateTimeFormatFor(method int, locales, options Value) (*intlDateTimeFormat, error) {
	required, defaults := intlRequireAny, intlDefaultsAll
	switch method {
	case intlToLocaleDateString:
		required, defaults = intlRequireDate, intlDefaultsDate
	case intlToLocaleTimeString:
		required, defaults = intlRequireTime, intlDefaultsTime
	}
	if !locales.IsUndefined() || !options.IsUndefined() {
		return r.intlNewDateTimeFormat(locales, options, required, defaults)
	}
	st := r.intlState()
	if zone := r.TimeZone(); st.cache.dtfZone != zone {
		st.cache.dtf = [3]*intlDateTimeFormat{}
		st.cache.dtfZone = zone
	}
	if st.cache.dtf[method] == nil {
		f, err := r.intlNewDateTimeFormat(locales, options, required, defaults)
		if err != nil {
			return nil, err
		}
		st.cache.dtf[method] = f
	}
	return st.cache.dtf[method], nil
}

// numberProtoToLocaleString implements Number.prototype.toLocaleString
// (ECMA-402).
func numberProtoToLocaleString(r *Realm, this Value, args []Value) (Value, error) {
	f, err := thisNumberValue(r, this, "toLocaleString")
	if err != nil {
		return Undefined(), err
	}
	nf, err := r.intlNumberFormatFor(intlArg(args, 0), intlArg(args, 1))
	if err != nil {
		return Undefined(), err
	}
	return StringValue(r.intlCharged(nf.formatFloat(f))), nil
}

// bigintProtoToLocaleString implements BigInt.prototype.toLocaleString
// (ECMA-402).
func bigintProtoToLocaleString(r *Realm, this Value, args []Value) (Value, error) {
	b, err := thisBigIntValue(r, this, "toLocaleString")
	if err == nil {
		err = r.checkBigIntFormat(b)
	}
	if err != nil {
		return Undefined(), err
	}
	nf, err := r.intlNumberFormatFor(intlArg(args, 0), intlArg(args, 1))
	if err != nil {
		return Undefined(), err
	}
	s, err := nf.format(r, BigIntValue(b))
	if err != nil {
		return Undefined(), err
	}
	return StringValue(r.intlCharged(s)), nil
}

// dateLocaleMethod makes Date.prototype's toLocaleString,
// toLocaleDateString and toLocaleTimeString (ECMA-402).
func dateLocaleMethod(name string, method int) NativeFunc {
	return func(r *Realm, this Value, args []Value) (Value, error) {
		tv, err := thisTimeValue(r, this, name)
		if err != nil {
			return Undefined(), err
		}
		if tv != tv {
			return StringValue(asciiString("Invalid Date")), nil
		}
		f, err := r.intlDateTimeFormatFor(method, intlArg(args, 0), intlArg(args, 1))
		if err != nil {
			return Undefined(), err
		}
		return StringValue(r.intlCharged(f.format(tv))), nil
	}
}

var (
	dateProtoToLocaleString     = dateLocaleMethod("toLocaleString", intlToLocaleString)
	dateProtoToLocaleDateString = dateLocaleMethod("toLocaleDateString", intlToLocaleDateString)
	dateProtoToLocaleTimeString = dateLocaleMethod("toLocaleTimeString", intlToLocaleTimeString)
)
