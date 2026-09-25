package engine

import "testing"

func TestStringFromCodePoint(t *testing.T) {
	accTable(t, [][2]string{
		{`return String.fromCodePoint();`, `""`},
		{`return String.fromCodePoint(65, 0x1F600, 0x62).length;`, `4`},
		{`return String.fromCodePoint(0x1F600).codePointAt(0);`, `128512`},
		{`return String.fromCodePoint(0xD800).charCodeAt(0);`, `55296`},
		{`return String.fromCodePoint(-0, "66", {valueOf: function () { return 67; }});`, `"\u0000BC"`},
		{`return String.fromCodePoint(0x10FFFF).length;`, `2`},
		{`try { String.fromCodePoint(0x110000); } catch (e) { return [e.name, e.message]; }`, `["RangeError","Invalid code point 1114112"]`},
		{`try { String.fromCodePoint(1.5); } catch (e) { return e.name; }`, `"RangeError"`},
		{`try { String.fromCodePoint(-1); } catch (e) { return e.name; }`, `"RangeError"`},
		{`try { String.fromCodePoint(undefined); } catch (e) { return e.message; }`, `"Invalid code point NaN"`},
		{`try { String.fromCodePoint(Infinity); } catch (e) { return e.name; }`, `"RangeError"`},
		// Arguments are coerced one at a time; a bad one stops the loop.
		{`var seen = []; try { String.fromCodePoint({valueOf: function () { seen.push(1); return -1; }},
		    {valueOf: function () { seen.push(2); return 1; }}); } catch (e) {} return seen;`, `[1]`},
		{`return [String.fromCodePoint.length, String.fromCodePoint.name];`, `[1,"fromCodePoint"]`},
	})
}

func TestStringRaw(t *testing.T) {
	accTable(t, [][2]string{
		{`return String.raw({raw: ["a", "b", "c"]}, 1, 2, 3);`, `"a1b2c"`},
		{`return String.raw({raw: ["a", "b"]});`, `"ab"`},
		{`return String.raw({raw: "xyz"}, "-", "-");`, `"x-y-z"`},
		{`return String.raw({raw: {length: 0}}, 1);`, `""`},
		{`return String.raw({raw: {length: -1}});`, `""`},
		{`return String.raw({raw: {length: 1, 0: "only"}}, "ignored");`, `"only"`},
		{`return String.raw({raw: {length: 2.9, 0: "a", 1: "b"}}, "|");`, `"a|b"`},
		{`return String.raw({raw: [undefined, null]}, 0);`, `"undefined0null"`},
		{`try { String.raw(); } catch (e) { return e.name; }`, `"TypeError"`},
		{`try { String.raw({}); } catch (e) { return e.name; }`, `"TypeError"`},
		{`var log = []; var raw = {length: 2, get 0() { log.push("r0"); return "a"; }, get 1() { log.push("r1"); return "b"; }};
		  String.raw({raw: raw}, {toString: function () { log.push("s0"); return "x"; }}); return log;`, `["r0","s0","r1"]`},
		{`return [String.raw.length, String.raw.name];`, `[1,"raw"]`},
	})
}

func TestStringWellFormed(t *testing.T) {
	accTable(t, [][2]string{
		{`return ["abc".isWellFormed(), "a\uD83D\uDE00b".isWellFormed(), "a\uD800".isWellFormed(), "\uDC00a".isWellFormed()];`,
			`[true,true,false,false]`},
		{`return "a\uD800b\uDC00\uD83D\uDE00".toWellFormed() === "a\uFFFDb\uFFFD\uD83D\uDE00";`, `true`},
		{`return "\uDC00\uD800".toWellFormed() === "\uFFFD\uFFFD";`, `true`},
		{`var s = "ok"; return s.toWellFormed() === s;`, `true`},
		{`return String.prototype.isWellFormed.call(12);`, `true`},
		{`return String.prototype.toWellFormed.call({toString: function () { return "\uD800"; }}).charCodeAt(0);`, `65533`},
		{`try { String.prototype.isWellFormed.call(null); } catch (e) { return e.name; }`, `"TypeError"`},
		{`try { String.prototype.toWellFormed.call(undefined); } catch (e) { return e.name; }`, `"TypeError"`},
		{`return [String.prototype.isWellFormed.length, String.prototype.toWellFormed.length];`, `[0,0]`},
	})
}

func TestStringToLocaleCase(t *testing.T) {
	accTable(t, [][2]string{
		{`return ["ÀBC".toLocaleLowerCase(), "àbc".toLocaleUpperCase()];`, `["àbc","ÀBC"]`},
		{`return ["ß".toLocaleUpperCase(), "\u0130".toLocaleLowerCase().length];`, `["SS",2]`},
		// The locales argument is ignored: en-US mappings everywhere.
		{`return ["I".toLocaleLowerCase("tr"), "i".toLocaleUpperCase("tr-TR")];`, `["i","I"]`},
		{`return "ΑΣ".toLocaleLowerCase();`, `"ας"`},
		{`return String.prototype.toLocaleUpperCase.call(true);`, `"TRUE"`},
		{`try { String.prototype.toLocaleLowerCase.call(null); } catch (e) { return e.name; }`, `"TypeError"`},
		{`return [String.prototype.toLocaleLowerCase.length, String.prototype.toLocaleUpperCase.name];`, `[0,"toLocaleUpperCase"]`},
	})
}

func TestStringTrimAliases(t *testing.T) {
	accTable(t, [][2]string{
		{`return [String.prototype.trimLeft === String.prototype.trimStart, String.prototype.trimRight === String.prototype.trimEnd];`,
			`[true,true]`},
		{`return [String.prototype.trimLeft.name, String.prototype.trimRight.name];`, `["trimStart","trimEnd"]`},
		{`return [" a ".trimLeft(), " a ".trimRight()];`, `["a "," a"]`},
		// Configurable only in a mutable realm; shared intrinsics are frozen.
		{`var d = Object.getOwnPropertyDescriptor(String.prototype, "trimLeft");
		  return [d.enumerable, d.configurable === !Object.isFrozen(String.prototype)];`, `[false,true]`},
	})
}

func TestStringHTMLMethods(t *testing.T) {
	accTable(t, [][2]string{
		{`return ["x".anchor("n"), "x".big(), "x".blink(), "x".bold(), "x".fixed()];`,
			`["<a name=\"n\">x</a>","<big>x</big>","<blink>x</blink>","<b>x</b>","<tt>x</tt>"]`},
		{`return ["x".fontcolor("red"), "x".fontsize(7), "x".italics(), "x".link("u")];`,
			`["<font color=\"red\">x</font>","<font size=\"7\">x</font>","<i>x</i>","<a href=\"u\">x</a>"]`},
		{`return ["x".small(), "x".strike(), "x".sub(), "x".sup()];`,
			`["<small>x</small>","<strike>x</strike>","<sub>x</sub>","<sup>x</sup>"]`},
		{`return "x".anchor('a"b"c');`, `"<a name=\"a&quot;b&quot;c\">x</a>"`},
		{`return "x".anchor();`, `"<a name=\"undefined\">x</a>"`},
		{`return String.prototype.bold.call(12);`, `"<b>12</b>"`},
		{`return "é".link("ü");`, `"<a href=\"ü\">é</a>"`},
		// this is coerced before the attribute value.
		{`var log = []; String.prototype.anchor.call({toString: function () { log.push("this"); return ""; }},
		    {toString: function () { log.push("attr"); return ""; }}); return log;`, `["this","attr"]`},
		{`try { String.prototype.big.call(undefined); } catch (e) { return e.name; }`, `"TypeError"`},
		{`try { String.prototype.anchor.call(null, {toString: function () { throw 1; }}); } catch (e) { return e.name; }`, `"TypeError"`},
		{`return [String.prototype.anchor.length, String.prototype.big.length, String.prototype.fontsize.length, String.prototype.sup.name];`,
			`[1,0,1,"sup"]`},
	})
}
