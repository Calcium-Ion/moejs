package engine

import (
	"strings"
	"testing"
)

// collChains are orderings produced by golang.org/x/text/collate with the
// root locale (CLDR root, the en-US order); each chain is strictly
// increasing except where an element is joined to the previous one by "=".
var collChains = []string{
	`ad < ae < aE < Ae < AE < aé < æ < Æ < af`,
	`oe < œ < Œ < of`,
	`sr < ss < SS < ß < ẞ < st`,
	`o < ó < ö < ø < Ø < oa < öa < øa < p`,
	`d < đ < Đ < ð < Ð < da < db < đb < ðb < e`,
	`l < ł < la < lb < łb < m`,
	`h < ħ < i`,
	`z < zz < þ < Þ`,
	`i < I < İ < ı < j`,
	`n < ŋ < o`,
	`a < A < á < Á < à < ä < ab < aB < Ab < AB < b`,
	`resume < Resume < résumé < Résumé < resumes`,
	`cote < coté < côte < côté`,
	"a\u0000 = a = a\u00ad = a\u200b < ab",
	`μ < µ < σ < Σ < ς < ω < Ω`,
	`あ < ア < ｱ < か < カ < が < ガ`,
	`가 < 가가 < 각 < 간 < 개`,
	`01 < 1 < 1/2 < ½ < 10 < 2 < ² < 9`,
	`strasse < Strasse < Straße`,
	`dž < ǆ < Dž < DŽ < ǅ < Ǆ`,
	`fi < ﬁ < fj`,
	"\t < \n < \r < \u3000 < \u00a0 < _ < - < – < — < , < ; < : < ! < ? < . < … < ' < \" < ( < ) < [ < ] < { < } < @ < * < / < \\ < & < # < % < ` < ^ < + < < < = < > < | < ~ < $ < 0 < 9 < a < A < z < Z",
	`a < b < Z`,
	`1 = ١ < 2`,
	`z < α < Ω < а < я < 一 < 丁`,
	`ď < Ḑ < ḍ < đ < Đ < ð`,
	`и < иа < иб < ийа < й < Й < йа < к`,
	`е < ё < еа < ёа < еб`,
	`а < аа < аб < ӑ < ӓ < ӓа < б`,
	`у < уа < уб < ў < ўа < ӱ`,
	`і < іа < ї < їа`,
	`г < ґ < Ґ < ґа < гг < ғ`,
	`о < оо < ӧ`,
	`э < ээ < ӭ`,
	`ж < ӂ < жж < ӝ`,
	"\u0439 = \u0438\u0306 < \u0439\u0301 < \u0439\u0323 = \u0438\u0323\u0306 = \u0438\u0306\u0323",
}

func splitCollChain(chain string) (items []string, rels []string) {
	// Items are separated by " < " or " = "; a lone "<" or "=" item is the
	// character itself.
	fields := strings.Split(chain, " ")
	for i := 0; i < len(fields); i++ {
		if i > 0 {
			rels = append(rels, fields[i])
			i++
		}
		items = append(items, fields[i])
	}
	return items, rels
}

func TestCollateChains(t *testing.T) {
	r := NewRealm()
	for _, chain := range collChains {
		items, rels := splitCollChain(chain)
		for i := 1; i < len(items); i++ {
			want := -1
			if rels[i-1] == "=" {
				want = 0
			}
			a, b := FromGoString(items[i-1]), FromGoString(items[i])
			got, err := collateCompare(r, a, b)
			if err != nil {
				t.Fatal(err)
			}
			back, _ := collateCompare(r, b, a)
			if got != want || back != -want {
				t.Errorf("%q vs %q: got %d/%d, want %d", items[i-1], items[i], got, back, want)
			}
		}
		// Transitivity across the whole chain.
		for i := range items {
			for j := i + 1; j < len(items); j++ {
				got, _ := collateCompare(r, FromGoString(items[i]), FromGoString(items[j]))
				if got > 0 {
					t.Errorf("%q vs %q: got %d, want <= 0", items[i], items[j], got)
				}
			}
		}
	}
}

func TestCollateLoneSurrogates(t *testing.T) {
	r := NewRealm()
	hi, lo := FromUTF16([]uint16{0xD800}), FromUTF16([]uint16{0xDC00})
	pair := FromUTF16([]uint16{0xD83D, 0xDE00})
	for _, c := range [][2]*String{{FromGoString("z"), hi}, {hi, lo}, {FromGoString("一"), hi}} {
		if got, _ := collateCompare(r, c[0], c[1]); got != -1 {
			t.Errorf("%v vs %v: got %d", c[0].UTF16(), c[1].UTF16(), got)
		}
	}
	if got, _ := collateCompare(r, pair, FromGoString("\U0001F600")); got != 0 {
		t.Errorf("surrogate pair vs its code point: got %d", got)
	}
}

func TestLocaleCompare(t *testing.T) {
	accTable(t, [][2]string{
		{`return ["a".localeCompare("b"), "b".localeCompare("a"), "a".localeCompare("a")];`, `[-1,1,0]`},
		{`return ["a".localeCompare("B"), "Z".localeCompare("a"), "a".localeCompare("A")];`, `[-1,1,-1]`},
		{`return ["\u00e4".localeCompare("z"), "r\u00e9sum\u00e9".localeCompare("resume"), "\u00e9".localeCompare("e\u0301")];`, `[-1,1,0]`},
		// The CE pairs of test262 15.5.4.9_CE.js.
		{`var pairs = [["o\u0308", "\u00f6"], ["\u00e4\u0323", "a\u0323\u0308"], ["a\u0308\u0323", "a\u0323\u0308"],
		    ["\u1ea1\u0308", "a\u0323\u0308"], ["\u00e4\u0306", "a\u0308\u0306"], ["\u0103\u0308", "a\u0306\u0308"],
		    ["\u1111\u1171\u11b6", "\ud4db"], ["\u212b", "\u00c5"], ["\u212b", "A\u030a"], ["x\u031b\u0323", "x\u0323\u031b"],
		    ["\u1ef1", "\u1ee5\u031b"], ["\u1ef1", "u\u031b\u0323"], ["\u1ef1", "\u01b0\u0323"], ["\u1ef1", "u\u0323\u031b"],
		    ["\u00c7", "C\u0327"], ["q\u0307\u0323", "q\u0323\u0307"], ["\uac00", "\u1100\u1161"], ["\u2126", "\u03a9"],
		    ["\u1e69", "s\u0323\u0307"], ["\u1e0b\u0323", "d\u0323\u0307"], ["\u1e0b\u0323", "\u1e0d\u0307"],
		    ["\ud834\udd5e", "\ud834\udd57\ud834\udd65"], ["\ud87e\udc2b", "\u5317"]];
		  return pairs.filter(function (p) { return p[0].localeCompare(p[1]) !== 0; });`, `[]`},
		{`return ["a", "B", "c", "A", "b", "\u00e1", "C"].sort(function (x, y) { return x.localeCompare(y); }).join("");`,
			`"aAábBcC"`},
		{`return ["item10", "item2", "Item1", "item-1", "item_1"].sort(function (x, y) { return x.localeCompare(y); });`,
			`["item_1","item-1","Item1","item10","item2"]`},
		// that is coerced; the locales and options arguments are ignored.
		{`return ["undefined".localeCompare(), "null".localeCompare(null), "1".localeCompare(1)];`, `[0,0,0]`},
		{`return ["\u00e4".localeCompare("z", "sv"), "a".localeCompare("A", "en", {sensitivity: "base"})];`, `[-1,-1]`},
		{`return [" ".localeCompare("\r"), " ".localeCompare("\u3000"), "a b".localeCompare("a\u00a0b"), "a b".localeCompare("a_b")];`,
			`[1,-1,-1,-1]`},
		{`return String.prototype.localeCompare.call(12, "12");`, `0`},
		{`try { String.prototype.localeCompare.call(null, "a"); } catch (e) { return e.name; }`, `"TypeError"`},
		{`var log = []; String.prototype.localeCompare.call({toString: function () { log.push("this"); return ""; }},
		    {toString: function () { log.push("that"); return ""; }}); return log;`, `["this","that"]`},
		{`return [String.prototype.localeCompare.length, String.prototype.localeCompare.name];`, `[1,"localeCompare"]`},
	})
}
