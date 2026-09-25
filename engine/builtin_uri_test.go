package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestURIEncodeTable(t *testing.T) {
	r := NewRealm()
	cases := []struct {
		fn   string
		in   Value
		want string
	}{
		{"encodeURIComponent", str("hello world"), "hello%20world"},
		{"encodeURIComponent", str("abcXYZ019-_.!~*'()"), "abcXYZ019-_.!~*'()"},
		{"encodeURIComponent", str(";/?:@&=+$,#"), "%3B%2F%3F%3A%40%26%3D%2B%24%2C%23"},
		{"encodeURIComponent", str("你好"), "%E4%BD%A0%E5%A5%BD"},
		{"encodeURIComponent", str("😀"), "%F0%9F%98%80"},
		{"encodeURIComponent", str("é"), "%C3%A9"},
		{"encodeURIComponent", str("\u0000\u007f"), "%00%7F"},
		{"encodeURIComponent", str(""), ""},
		{"encodeURIComponent", str("a=1&b=2"), "a%3D1%26b%3D2"},
		{"encodeURIComponent", str("data:image/png;base64,AA=="), "data%3Aimage%2Fpng%3Bbase64%2CAA%3D%3D"},
		{"encodeURIComponent", IntValue(42), "42"},
		{"encodeURIComponent", Undefined(), "undefined"},
		{"encodeURI", str("http://x.com/a b?q=1&r=é#f"), "http://x.com/a%20b?q=1&r=%C3%A9#f"},
		{"encodeURI", str(";/?:@&=+$,#"), ";/?:@&=+$,#"},
		{"encodeURI", str("[]{}|\\^\"<>`%"), "%5B%5D%7B%7D%7C%5C%5E%22%3C%3E%60%25"},
		{"encodeURI", str("你好/世界"), "%E4%BD%A0%E5%A5%BD/%E4%B8%96%E7%95%8C"},
	}
	for _, c := range cases {
		t.Run(c.fn+"/"+c.want, func(t *testing.T) {
			got, err := callGlobal(t, r, c.fn, c.in)
			require.NoError(t, err)
			assert.Equal(t, c.want, got.AsString().GoString())
		})
	}
	// Unescaped ASCII input is returned as the same string (zero-copy).
	in := FromGoString("no-escaping-needed")
	got, err := callGlobal(t, r, "encodeURIComponent", StringValue(in))
	require.NoError(t, err)
	assert.Same(t, in, got.AsString())
	// Lone surrogates throw.
	for _, in := range []Value{utf16Str(0xD800), utf16Str('a', 0xDC00, 'b'), utf16Str(0xD83D, 'x'), utf16Str(0xDE00, 0xD83D)} {
		for _, fn := range []string{"encodeURIComponent", "encodeURI"} {
			_, err := callGlobal(t, r, fn, in)
			assertErrorKind(t, err, KindURIError, "URI malformed")
		}
	}
}

func TestURIDecodeTable(t *testing.T) {
	r := NewRealm()
	cases := []struct {
		fn   string
		in   string
		want string
	}{
		{"decodeURIComponent", "hello%20world", "hello world"},
		{"decodeURIComponent", "%3B%2F%3F%3A%40%26%3D%2B%24%2C%23", ";/?:@&=+$,#"},
		{"decodeURIComponent", "%E4%BD%A0%E5%A5%BD", "你好"},
		{"decodeURIComponent", "%F0%9F%98%80", "😀"},
		{"decodeURIComponent", "%c3%a9", "é"},
		{"decodeURIComponent", "plain", "plain"},
		{"decodeURIComponent", "", ""},
		{"decodeURIComponent", "a%25b", "a%b"},
		{"decodeURIComponent", "%7F", "\u007f"},
		{"decodeURIComponent", "你好%20", "你好 "},
		{"decodeURI", "%3B%2F%3F%3A%40%26%3D%2B%24%2C%23", "%3B%2F%3F%3A%40%26%3D%2B%24%2C%23"},
		{"decodeURI", "a%20b%3fc%2Fd", "a b%3fc%2Fd"},
		{"decodeURI", "%E4%BD%A0", "你"},
		{"decodeURI", "%41%3d", "A%3d"},
	}
	for _, c := range cases {
		t.Run(c.fn+"/"+c.in, func(t *testing.T) {
			got, err := callGlobal(t, r, c.fn, str(c.in))
			require.NoError(t, err)
			assert.Equal(t, c.want, got.AsString().GoString())
		})
	}
	// No percent sign: the input string is returned as is.
	in := FromGoString("untouched")
	got, err := callGlobal(t, r, "decodeURIComponent", StringValue(in))
	require.NoError(t, err)
	assert.Same(t, in, got.AsString())
	bad := []string{
		"%", "%2", "%zz", "%G0", "%E4%BD", "%E4%BD%", "%E4%BDx", "%E4%41%A0", "%80", "%C0%80", "%ED%A0%BD", "%F8%80%80%80%80", "%FF", "%C3%28",
		"%F4%90%80%80", // above U+10FFFF
	}
	for _, in := range bad {
		for _, fn := range []string{"decodeURIComponent", "decodeURI"} {
			_, err := callGlobal(t, r, fn, str(in))
			assertErrorKind(t, err, KindURIError, "URI malformed")
		}
	}
	// Round trips.
	for _, s := range []string{"a b&c=d/e?f#g", "你好，世界", "😀🎉", "\u0000\u001f", "100%"} {
		enc, err := callGlobal(t, r, "encodeURIComponent", str(s))
		require.NoError(t, err)
		dec, err := callGlobal(t, r, "decodeURIComponent", enc)
		require.NoError(t, err)
		assert.Equal(t, s, dec.AsString().GoString())
		enc, err = callGlobal(t, r, "encodeURI", str(s))
		require.NoError(t, err)
		dec, err = callGlobal(t, r, "decodeURI", enc)
		require.NoError(t, err)
		assert.Equal(t, s, dec.AsString().GoString())
	}
	// Function metadata.
	fn, _ := r.Global.GetProp(r, StringKey(AtomEncodeURIComponent))
	lv, _ := fn.AsObject().GetProp(r, lengthKey)
	assert.Equal(t, IntValue(1), lv)
	nv, _ := fn.AsObject().GetProp(r, StringKey(AtomName))
	assert.Equal(t, "encodeURIComponent", nv.AsString().GoString())
	d, _ := r.Global.GetOwnProperty(StringKey(AtomEncodeURIComponent))
	assert.False(t, d.Enumerable())
	assert.True(t, d.Writable())
}

func TestEscapeUnescape(t *testing.T) {
	accTable(t, [][2]string{
		{`return escape("abcXYZ019@*_+-./");`, `"abcXYZ019@*_+-./"`},
		{`return escape(" !\"#$%&'(),:;<=>?[\\]^` + "`" + `{|}~");`,
			`"%20%21%22%23%24%25%26%27%28%29%2C%3A%3B%3C%3D%3E%3F%5B%5C%5D%5E%60%7B%7C%7D%7E"`},
		{`return escape("äĀ😀\u0000");`, `"%E4%u0100%uD83D%uDE00%00"`},
		{`return [escape(), escape(null), escape(1.5), escape({toString: function () { return "é"; }})];`,
			`["undefined","null","1.5","%E9"]`},
		{`return unescape("%E4%u0100%uD83D%uDE00") === "äĀ😀";`, `true`},
		{`return unescape("%u%u0%u00%u000%zz%4%%41%u004g%U0041%");`, `"%u%u0%u00%u000%zz%4%A%u004g%U0041%"`},
		{`return [unescape("%41%42"), unescape("%u0041%u00421"), unescape("abc"), unescape()];`, `["AB","AB1","abc","undefined"]`},
		{`var s = "a bÿĀ\ud800x"; return unescape(escape(s)) === s;`, `true`},
		{`return [escape.length, unescape.length, escape.name, unescape.name, Object.keys(globalThis).indexOf("escape")];`,
			`[1,1,"escape","unescape",-1]`},
		{`try { new escape(""); } catch (e) { return e.name; }`, `"TypeError"`},
	})
}

func TestAtobBtoa(t *testing.T) {
	accTable(t, [][2]string{
		{`return ["", "f", "fo", "foo", "foob", "fooba", "foobar"].map(btoa);`,
			`["","Zg==","Zm8=","Zm9v","Zm9vYg==","Zm9vYmE=","Zm9vYmFy"]`},
		{`return [btoa("ÿþ"), btoa(12), btoa(null), btoa(undefined)];`, `["//4=","MTI=","bnVsbA==","dW5kZWZpbmVk"]`},
		{`return ["Zm9vYmFy", "Zm9vYg", "Zm9vYg==", "Zm9vYmE", " Zm9v\nYg==\t", "Zm 9v\fYm\rFy", "", "===="].map(function (s) {
		    try { return atob(s); } catch (e) { return e.name; } });`,
			`["foobar","foob","foob","fooba","foob","foobar","","InvalidCharacterError"]`},
		{`return ["Zm9vY", "Zm=9", "Zg=", "Zm9vYg===", "Zm9v=", "é", "Zm9v!", "Zm9　v", "Zm9vYg=a"].map(function (s) {
		    try { return atob(s); } catch (e) { return e.name; } });`,
			`["InvalidCharacterError","InvalidCharacterError","InvalidCharacterError","InvalidCharacterError","InvalidCharacterError","InvalidCharacterError","InvalidCharacterError","InvalidCharacterError","InvalidCharacterError"]`},
		// Unused bits after the last byte are ignored.
		{`return [atob("Zh"), atob("Zm9"), atob("Zm+").length, atob("//4=").charCodeAt(0), atob("//4=").charCodeAt(1)];`, `["f","fo",2,255,254]`},
		{`try { atob("a"); } catch (e) { return [e.name, e.code, e instanceof Error, e.message, Object.keys(e).length, String(e)]; }`,
			`["InvalidCharacterError",5,true,"The string to be decoded is not correctly encoded.",0,"InvalidCharacterError: The string to be decoded is not correctly encoded."]`},
		{`try { btoa("Ā"); } catch (e) { return [e.name, e.code, e.message]; }`, `["InvalidCharacterError",5,"Invalid character"]`},
		{`try { btoa("😀"); } catch (e) { return e.name; }`, `"InvalidCharacterError"`},
		{`var out = []; try { atob(); } catch (e) { out.push(e.name); } try { btoa(); } catch (e) { out.push(e.name); } return out;`,
			`["TypeError","TypeError"]`},
		{`try { atob(undefined); } catch (e) { return e.name; }`, `"InvalidCharacterError"`},
		{`return atob(null).length;`, `3`},
		{`var s = ""; for (var i = 0; i < 256; i++) s += String.fromCharCode(i); return atob(btoa(s)) === s && btoa(atob(btoa(s))) === btoa(s);`, `true`},
		{`return [atob.length, btoa.length, atob.name, btoa.name, Object.keys(globalThis).indexOf("atob")];`, `[1,1,"atob","btoa",-1]`},
		{`var log = []; try { btoa({toString: function () { log.push("s"); return "ā"; }}); } catch (e) { log.push(e.name); } return log;`,
			`["s","InvalidCharacterError"]`},
	})
}

// TestURIGlobalAttrs checks the string-coding globals' bindings: writable
// and configurable in a mutable realm, read-only in a shared one (the
// lockdown model of every intrinsic binding).
func TestURIGlobalAttrs(t *testing.T) {
	for _, shared := range []bool{false, true} {
		r := NewRealmWith(RealmOptions{SharedIntrinsics: shared})
		for _, d := range uriFunctions {
			p, ok := r.Global.GetOwnProperty(StringKey(d.name))
			require.True(t, ok, d.name.GoString())
			assert.False(t, p.Enumerable(), d.name.GoString())
			assert.Equal(t, !shared, p.Writable(), "%s shared=%v", d.name.GoString(), shared)
			assert.Equal(t, !shared, p.Configurable(), "%s shared=%v", d.name.GoString(), shared)
		}
	}
}
