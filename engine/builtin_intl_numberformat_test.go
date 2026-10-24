package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The expected values are Node 24.22's (ICU 78.3), but for PluralRules'
// notation option and BigInt values, which follow ECMA-402 and test262 where
// V8 13.6 has neither.
func TestIntlNumberFormat(t *testing.T) {
	runProtoCases(t, []protoCase{
		{"shape", `const d = Object.getOwnPropertyDescriptor(Intl.NumberFormat.prototype, "format"); return [Intl.NumberFormat.length, Intl.NumberFormat.name, typeof d.get, d.set, d.get.name, Intl.NumberFormat.prototype[Symbol.toStringTag], Object.getOwnPropertyNames(Intl.NumberFormat.prototype).sort().join(" "), Intl.NumberFormat.supportedLocalesOf.length].join()`, `0,NumberFormat,function,,get format,Intl.NumberFormat,constructor format formatRange formatRangeToParts formatToParts resolvedOptions,1`},
		{"plural shape", `return [Intl.PluralRules.length, Intl.PluralRules.name, Intl.PluralRules.prototype[Symbol.toStringTag], Object.getOwnPropertyNames(Intl.PluralRules.prototype).sort().join(" "), Intl.PluralRules.prototype.select.length, Intl.PluralRules.prototype.selectRange.length].join()`, `0,PluralRules,Intl.PluralRules,constructor resolvedOptions select selectRange,1,2`},
		{"bound format", `const nf = new Intl.NumberFormat("en"); const f = nf.format; return [f === nf.format, f.name === "", f.length, f(1234.5), [1, 22, 333].map(nf.format).join("|"), "prototype" in f].join()`, `true,true,1,1,234.5,1|22|333,false`},
		{"call", `const nf = Intl.NumberFormat("de"); return [nf instanceof Intl.NumberFormat, nf.format(1234.5), Object.prototype.toString.call(nf)].join()`, `true,1.234,5,[object Intl.NumberFormat]`},
		{"plural call", `return Intl.PluralRules("en")`, `!TypeError`},
		{"issue money", `return new Intl.NumberFormat("ja-JP", { style: "currency", currency: "JPY" }).format(1250)`, `￥1,250`},
		{"currencies", `return [["en-US", "USD"], ["de", "EUR"], ["fr", "EUR"], ["zh-CN", "CNY"], ["zh-TW", "TWD"], ["ko", "KRW"], ["ru", "RUB"], ["ja", "USD"], ["es", "USD"], ["pt", "BRL"], ["it", "EUR"]].map(([l, c]) => new Intl.NumberFormat(l, {style: "currency", currency: c}).format(-1234.567)).join("|")`, `-$1,234.57|-1.234,57 €|-1 234,57 €|-¥1,234.57|-$1,234.57|-₩1,235|-1 234,57 ₽|-$1,234.57|-1234,57 US$|-R$ 1.234,57|-1234,57 €`},
		{"currency displays", `return ["code", "narrowSymbol", "name"].map(d => new Intl.NumberFormat("en", {style: "currency", currency: "USD", currencyDisplay: d}).format(1)).join("|") + "|" + new Intl.NumberFormat("ru", {style: "currency", currency: "RUB", currencyDisplay: "name"}).format(2)`, `USD 1.00|$1.00|1.00 US dollars|2,00 российского рубля`},
		{"accounting", `const nf = new Intl.NumberFormat("en", {style: "currency", currency: "USD", currencySign: "accounting"}); return [nf.format(-3), nf.format(3), nf.format(-0)].join("|")`, `($3.00)|$3.00|($0.00)`},
		{"digits", `return [new Intl.NumberFormat("en", {minimumIntegerDigits: 3, minimumFractionDigits: 2}).format(5), new Intl.NumberFormat("en", {maximumSignificantDigits: 2}).format(1255), new Intl.NumberFormat("en", {roundingIncrement: 5, maximumFractionDigits: 2, minimumFractionDigits: 2}).format(1.234), new Intl.NumberFormat("en", {trailingZeroDisplay: "stripIfInteger", minimumFractionDigits: 2}).format(2), new Intl.NumberFormat("en", {roundingMode: "floor", maximumFractionDigits: 0}).format(-1.5)].join("|")`, `005.00|1,300|1.25|2|-2`},
		{"exact", `const nf = new Intl.NumberFormat("en", {maximumFractionDigits: 20}); return [nf.format("1.23456789012345678901"), nf.format(123456789012345678901234567890n), nf.format("-0"), nf.format("1e-7"), nf.format(" 0x10 "), nf.format("x")].join("|")`, `1.23456789012345678901|123,456,789,012,345,678,901,234,567,890|-0|0.0000001|16|NaN`},
		{"grouping", `return [[false, 12345], ["min2", 1234], ["always", 1234], ["auto", 1234]].map(([g, x]) => new Intl.NumberFormat("es", {useGrouping: g}).format(x)).join("|") + "|" + new Intl.NumberFormat("es").format(12345)`, `12345|1234|1.234|1234|12.345`},
		{"signs", `return ["auto", "always", "exceptZero", "negative", "never"].map(s => [-1, -0, 0, 1].map(x => new Intl.NumberFormat("en", {signDisplay: s}).format(x)).join(" ")).join("|")`, `-1 -0 0 1|-1 -0 +0 +1|-1 0 0 +1|-1 0 0 1|1 0 0 1`},
		{"percent", `return [new Intl.NumberFormat("en", {style: "percent"}).format(0.256), new Intl.NumberFormat("de", {style: "percent", minimumFractionDigits: 1}).format(-0.256), new Intl.NumberFormat("fr", {style: "percent"}).format(1)].join("|")`, `26%|-25,6 %|100 %`},
		{"notations", `return [["scientific", 123456], ["engineering", 123456], ["scientific", 0.00012], ["compact", 1234567]].map(([n, x]) => new Intl.NumberFormat("en", {notation: n}).format(x)).join("|") + "|" + ["zh", "ja", "ko", "de", "ru"].map(l => new Intl.NumberFormat(l, {notation: "compact", compactDisplay: "long"}).format(123456789)).join("|")`, `1.235E5|123.456E3|1.2E-4|1.2M|1.2亿|1.2億|1.2억|123 Millionen|123 миллиона`},
		{"units", `return [["kilometer-per-hour", "long"], ["kilobyte-per-second", "short"], ["celsius", "narrow"], ["liter", "long"], ["percent", "short"], ["mile-per-gallon", "long"]].map(([u, d]) => new Intl.NumberFormat("en", {style: "unit", unit: u, unitDisplay: d}).format(2)).join("|") + "|" + new Intl.NumberFormat("ru", {style: "unit", unit: "hour", unitDisplay: "long"}).format(5)`, `2 kilometers per hour|2 kB/s|2°C|2 liters|2%|2 miles per gallon|5 часов`},
		{"numbering", `return [new Intl.NumberFormat("en-u-nu-arab").format(1234.5), new Intl.NumberFormat("en", {numberingSystem: "deva"}).format(1234.5), new Intl.NumberFormat("zh-u-nu-hanidec").resolvedOptions().numberingSystem, new Intl.NumberFormat("en-u-nu-native").resolvedOptions().locale, new Intl.NumberFormat("en-u-nu-arab", {numberingSystem: "thai"}).resolvedOptions().locale].join("|")`, `١٬٢٣٤٫٥|१,२३४.५|hanidec|en|en`},
		{"parts", `return JSON.stringify(new Intl.NumberFormat("de", {style: "currency", currency: "EUR"}).formatToParts(-1234.5)) + JSON.stringify(new Intl.NumberFormat("en", {notation: "compact", compactDisplay: "long"}).formatToParts(1.5e6)) + JSON.stringify(new Intl.NumberFormat("en").formatToParts(NaN))`, `[{"type":"minusSign","value":"-"},{"type":"integer","value":"1"},{"type":"group","value":"."},{"type":"integer","value":"234"},{"type":"decimal","value":","},{"type":"fraction","value":"50"},{"type":"literal","value":" "},{"type":"currency","value":"€"}][{"type":"integer","value":"1"},{"type":"decimal","value":"."},{"type":"fraction","value":"5"},{"type":"literal","value":" "},{"type":"compact","value":"million"}][{"type":"nan","value":"NaN"}]`},
		{"range", `return [new Intl.NumberFormat("en", {style: "currency", currency: "USD"}).formatRange(3, 5), new Intl.NumberFormat("de", {style: "currency", currency: "EUR"}).formatRange(3, 5), new Intl.NumberFormat("en", {maximumFractionDigits: 0}).formatRange(2.9, 3.1), new Intl.NumberFormat("ja").formatRange(1, 10), new Intl.NumberFormat("en", {style: "unit", unit: "mile", unitDisplay: "long"}).formatRange(1, 2)].join("|")`, `$3.00 – $5.00|3,00–5,00 €|~3|1～10|1–2 miles`},
		{"range parts", `return JSON.stringify(new Intl.NumberFormat("en", {style: "currency", currency: "USD"}).formatRangeToParts(3, 5))`, `[{"type":"currency","value":"$","source":"startRange"},{"type":"integer","value":"3","source":"startRange"},{"type":"decimal","value":".","source":"startRange"},{"type":"fraction","value":"00","source":"startRange"},{"type":"literal","value":" – ","source":"shared"},{"type":"currency","value":"$","source":"endRange"},{"type":"integer","value":"5","source":"endRange"},{"type":"decimal","value":".","source":"endRange"},{"type":"fraction","value":"00","source":"endRange"}]`},
		{"range errors", `const nf = new Intl.NumberFormat(); const r = []; for (const [x, y] of [[NaN, 1], [1, undefined], [1, NaN]]) { try { nf.formatRange(x, y) } catch (e) { r.push(e.constructor.name) } } return r.join()`, `RangeError,TypeError,RangeError`},
		{"resolvedOptions", `return JSON.stringify(new Intl.NumberFormat("ja-JP", {style: "currency", currency: "jpy"}).resolvedOptions()) + JSON.stringify(new Intl.NumberFormat("en", {notation: "compact"}).resolvedOptions()) + JSON.stringify(new Intl.NumberFormat("de", {style: "unit", unit: "meter", maximumSignificantDigits: 3}).resolvedOptions())`, `{"locale":"ja-JP","numberingSystem":"latn","style":"currency","currency":"JPY","currencyDisplay":"symbol","currencySign":"standard","minimumIntegerDigits":1,"minimumFractionDigits":0,"maximumFractionDigits":0,"useGrouping":"auto","notation":"standard","signDisplay":"auto","roundingIncrement":1,"roundingMode":"halfExpand","roundingPriority":"auto","trailingZeroDisplay":"auto"}{"locale":"en","numberingSystem":"latn","style":"decimal","minimumIntegerDigits":1,"minimumFractionDigits":0,"maximumFractionDigits":0,"minimumSignificantDigits":1,"maximumSignificantDigits":2,"useGrouping":"min2","notation":"compact","compactDisplay":"short","signDisplay":"auto","roundingIncrement":1,"roundingMode":"halfExpand","roundingPriority":"morePrecision","trailingZeroDisplay":"auto"}{"locale":"de","numberingSystem":"latn","style":"unit","unit":"meter","unitDisplay":"short","minimumIntegerDigits":1,"minimumSignificantDigits":1,"maximumSignificantDigits":3,"useGrouping":"auto","notation":"standard","signDisplay":"auto","roundingIncrement":1,"roundingMode":"halfExpand","roundingPriority":"auto","trailingZeroDisplay":"auto"}`},
		{"option order", `const log = []; const o = new Proxy({}, {get(t, k) { log.push(k); return undefined }}); new Intl.NumberFormat("en", o); return log.join()`, `localeMatcher,numberingSystem,style,currency,currencyDisplay,currencySign,unit,unitDisplay,notation,minimumIntegerDigits,minimumFractionDigits,maximumFractionDigits,minimumSignificantDigits,maximumSignificantDigits,roundingIncrement,roundingMode,roundingPriority,trailingZeroDisplay,compactDisplay,useGrouping,signDisplay`},
		{"plural option order", `const log = []; const o = new Proxy({}, {get(t, k) { log.push(k); return undefined }}); new Intl.PluralRules("en", o); return log.join()`, `localeMatcher,type,notation,compactDisplay,minimumIntegerDigits,minimumFractionDigits,maximumFractionDigits,minimumSignificantDigits,maximumSignificantDigits,roundingIncrement,roundingMode,roundingPriority,trailingZeroDisplay`},
		{"errors", `const r = []; for (const o of [{style: "currency"}, {style: "unit"}, {currency: "US"}, {unit: "meters"}, {useGrouping: "yes"}, {roundingIncrement: 3}, {roundingIncrement: 5, maximumSignificantDigits: 2}, {roundingIncrement: 5, maximumFractionDigits: 2}, {minimumFractionDigits: 3, maximumFractionDigits: 1}, {numberingSystem: "a"}, {minimumIntegerDigits: 22}, {style: "bogus"}]) { try { new Intl.NumberFormat("en", o); r.push("ok") } catch (e) { r.push(e.constructor.name) } } return r.join()`, `TypeError,TypeError,RangeError,RangeError,RangeError,RangeError,TypeError,RangeError,RangeError,RangeError,RangeError,RangeError`},
		{"useGrouping options", `return [undefined, true, false, "true", "false", "min2", "always", 0, ""].map(g => String(new Intl.NumberFormat("en", {useGrouping: g}).resolvedOptions().useGrouping)).join() + "|" + new Intl.NumberFormat("en", {notation: "compact", useGrouping: "false"}).resolvedOptions().useGrouping`, `auto,always,false,auto,auto,min2,always,false,false|min2`},
		{"supportedLocalesOf", `return Intl.NumberFormat.supportedLocalesOf(["de-AT", "zxx", "ja-JP", "zh-TW", "fr"]).join() + "|" + Intl.PluralRules.supportedLocalesOf(["de-AT", "zxx", "ars", "cy"]).join()`, `de-AT,ja-JP,zh-TW,fr|de-AT,ars,cy`},
		{"plurals", `const c = new Intl.PluralRules("en"), o = new Intl.PluralRules("en", {type: "ordinal"}); return [[0, 1, 2, "1.0", 1n, 10n ** 30n + 1n].map(x => c.select(x)).join(" "), [1, 2, 3, 4, 11, 21, 22, 23, 101].map(x => o.select(x)).join(" "), new Intl.PluralRules("ar").resolvedOptions().pluralCategories.join(" "), new Intl.PluralRules("ru").select(1.5), new Intl.PluralRules("pt-PT").select(0), new Intl.PluralRules("pt").select(0), new Intl.PluralRules("fr").selectRange(0, 1), new Intl.PluralRules("en").selectRange(102, 201)].join("|")`, "other one other one one other|one two few other other one two few one|zero one two few many other|other|other|one|one|other"},
		{"plural notation", `const s = new Intl.PluralRules("fr"), c = new Intl.PluralRules("fr", {notation: "compact"}); return [1e6, 1.5e6, 1e-6].map(x => s.select(x) + "/" + c.select(x)).join(" ")`, `many/many other/many one/one`},
		{"plural resolvedOptions", `return JSON.stringify(new Intl.PluralRules("en", {type: "ordinal", minimumSignificantDigits: 2}).resolvedOptions())`, `{"locale":"en","type":"ordinal","notation":"standard","minimumIntegerDigits":1,"minimumSignificantDigits":2,"maximumSignificantDigits":21,"pluralCategories":["one","two","few","other"],"roundingIncrement":1,"roundingMode":"halfExpand","roundingPriority":"auto","trailingZeroDisplay":"auto"}`},
		{"plural errors", `const r = []; for (const f of [() => new Intl.PluralRules("en", {type: "x"}), () => new Intl.PluralRules("en").selectRange(1), () => new Intl.PluralRules("en").selectRange(NaN, 1), () => Intl.PluralRules.prototype.select.call({}, 1)]) { try { f(); r.push("ok") } catch (e) { r.push(e.constructor.name) } } return r.join()`, `RangeError,TypeError,RangeError,TypeError`},
		{"receivers", `const r = []; for (const f of [() => Intl.NumberFormat.prototype.formatToParts.call({}, 1), () => Object.getOwnPropertyDescriptor(Intl.NumberFormat.prototype, "format").get.call(new Intl.PluralRules()), () => Intl.NumberFormat.prototype.resolvedOptions.call(1)]) { try { f(); r.push("ok") } catch (e) { r.push(e.constructor.name) } } return r.join()`, `TypeError,TypeError,TypeError`},
		{"regional", `return ["zh-TW", "zh-SG"].map(l => { const nf = new Intl.NumberFormat(l, {notation: "compact"}); return nf.resolvedOptions().locale + ":" + nf.format(123456789) + ":" + new Intl.NumberFormat(l, {style: "currency", currency: "TWD"}).format(1250) }).join("|")`, `zh-TW:1.2億:$1,250.00|zh-SG:1.2亿:NT$1,250.00`},
		// zh-HK formats with zh-Hant-HK's data, which has compact patterns of
		// its own, and zh-Hant's, never zh-Hans's.
		{"zh-HK", `const nf = new Intl.NumberFormat("zh-HK", {notation: "compact"}); return nf.resolvedOptions().locale + ":" + nf.format(123456789) + ":" + nf.format(1234)`, `zh-HK:123M:1.2K`},
		{"subclass", `class N extends Intl.NumberFormat {} class P extends Intl.PluralRules {} const n = new N("de"), p = new P("de"); return [n instanceof N, n.format(1.5), p instanceof P, p.select(1)].join()`, `true,1,5,true,one`},
	})
}

// TestIntlNumberFormatAllocs checks that a constructed formatter formats a
// Number allocating only the result.
func TestIntlNumberFormatAllocs(t *testing.T) {
	r := NewRealmWith(RealmOptions{})
	for _, c := range []struct {
		locales string
		opts    map[string]string
		x       float64
	}{
		{"ja-JP", map[string]string{"style": "currency", "currency": "JPY"}, 1250},
		{"en-US", map[string]string{"style": "currency", "currency": "USD"}, -1234.5},
		{"en-US", nil, 1234567.891},
		{"de", map[string]string{"style": "unit", "unit": "kilometer-per-hour", "unitDisplay": "long"}, 12.5},
		{"fr", map[string]string{"notation": "compact"}, 1234567},
	} {
		opts := r.NewObjectCap(len(c.opts))
		for k, v := range c.opts {
			opts.addNamed(r, InternKey(k), propCell{value: StringValue(FromGoString(v)), attrs: attrDefault})
		}
		nf, err := r.intlNewNumberFormat(StringValue(FromGoString(c.locales)), ObjectValue(opts))
		if err != nil {
			t.Fatal(err)
		}
		nf.formatFloat(c.x)
		if n := testing.AllocsPerRun(100, func() { nf.formatFloat(c.x) }); n != 1 {
			t.Errorf("%s %v: %v allocations per format", c.locales, c.opts, n)
		}
	}
}

func benchIntlNumberFormat(b *testing.B, locales string, opts map[string]string) (*Realm, *intlNumberFormat) {
	r := NewRealmWith(RealmOptions{})
	o := r.NewObjectCap(len(opts))
	for k, v := range opts {
		o.addNamed(r, InternKey(k), propCell{value: StringValue(FromGoString(v)), attrs: attrDefault})
	}
	nf, err := r.intlNewNumberFormat(StringValue(FromGoString(locales)), ObjectValue(o))
	if err != nil {
		b.Fatal(err)
	}
	return r, nf
}

// BenchmarkIntlNumberFormatFloat formats a Number with a constructed
// formatter (the format function's work without the call).
func BenchmarkIntlNumberFormatFloat(b *testing.B) {
	for _, c := range []struct {
		name, locales string
		opts          map[string]string
	}{
		{"ja-JPY", "ja-JP", map[string]string{"style": "currency", "currency": "JPY"}},
		{"en-USD", "en-US", map[string]string{"style": "currency", "currency": "USD"}},
		{"en-decimal", "en-US", nil},
	} {
		b.Run(c.name, func(b *testing.B) {
			_, nf := benchIntlNumberFormat(b, c.locales, c.opts)
			b.ReportAllocs()
			for i := range b.N {
				nf.formatFloat(1250.5 + float64(i&1023))
			}
		})
	}
}

// BenchmarkIntlNewNumberFormat constructs a formatter (the constructor's
// work without the object).
func BenchmarkIntlNewNumberFormat(b *testing.B) {
	for _, c := range []struct {
		name, locales string
		opts          map[string]string
	}{
		{"ja-JPY", "ja-JP", map[string]string{"style": "currency", "currency": "JPY"}},
		{"en-USD", "en-US", map[string]string{"style": "currency", "currency": "USD"}},
		{"en-decimal", "en-US", nil},
	} {
		b.Run(c.name, func(b *testing.B) {
			r, _ := benchIntlNumberFormat(b, c.locales, c.opts)
			o := r.NewObjectCap(len(c.opts))
			for k, v := range c.opts {
				o.addNamed(r, InternKey(k), propCell{value: StringValue(FromGoString(v)), attrs: attrDefault})
			}
			loc := StringValue(FromGoString(c.locales))
			b.ReportAllocs()
			for range b.N {
				if _, err := r.intlNewNumberFormat(loc, ObjectValue(o)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// TestIntlMemoryLimitNumberFormat: the strings NumberFormat returns, its
// formatters and PluralRules objects are charged, so a script that keeps
// them stops at the limit (each loop keeps 80-120 MB under 64 MB). A number
// formats to at most ~410 characters (309 integer digits, 100 fraction
// digits; larger values are infinite), so the loops keep many strings.
func TestIntlMemoryLimitNumberFormat(t *testing.T) {
	const big = `const X = 1.7976931348623157e308, nf = new Intl.NumberFormat("en", {useGrouping: false, minimumFractionDigits: 100}), keep = [];`
	for name, body := range map[string]string{
		"format":             big + `for (let i = 0; i < 200000; i++) keep.push(nf.format(X)); return keep.length`,
		"formatToParts":      big + `for (let i = 0; i < 150000; i++) keep.push(nf.formatToParts(X)); return keep.length`,
		"formatRange":        big + `for (let i = 0; i < 100000; i++) keep.push(nf.formatRange(X / 2, X)); return keep.length`,
		"formatRangeToParts": big + `for (let i = 0; i < 80000; i++) keep.push(nf.formatRangeToParts(X / 2, X)); return keep.length`,
		"NumberFormat":       `const keep = []; for (let i = 0; i < 60000; i++) keep.push(new Intl.NumberFormat("en", {style: "currency", currency: "USD"})); return keep.length`,
		"PluralRules":        `const keep = []; for (let i = 0; i < 150000; i++) keep.push(new Intl.PluralRules("en")); return keep.length`,
	} {
		t.Run(name, func(t *testing.T) {
			hit, res, allocated := intlMemLimitRun(t, 64<<20, body)
			assert.True(t, hit, "result %s, charged %d bytes", res, allocated)
		})
	}
}
