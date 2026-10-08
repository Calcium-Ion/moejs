package engine

import (
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestConcatNumber covers string + number without the number's own string
// (concatNumber): every number format, both orders, the flat/rope boundary
// and the strings it leaves to the generic path (empty, UTF-16, ropes).
func TestConcatNumber(t *testing.T) {
	long := strings.Repeat("x", 60)
	accTable(t, [][2]string{
		{`return ["a" + 0, "a" + -0, 0 + "a", -0 + "a", "a" + NaN, "a" + Infinity, -Infinity + "a", "a" + 1e21, "a" + 1.5e-7,
		          "a" + 0.1 + 0.2, "a" + (0.1 + 0.2), "a" + -5, -5 + "a", "a" + 9007199254740993, "a" + 123456789012345680000, "a" + 5e-324];`,
			`["a0","a0","0a","0a","aNaN","aInfinity","-Infinitya","a1e+21","a1.5e-7","a0.10.2","a0.30000000000000004","a-5","-5a","a9007199254740992","a123456789012345680000","a5e-324"]`},
		{`var s = "` + long + `"; var r = [];
		  for (var n of [1, 12, 123, 1234, 12345]) { var x = s + n, y = n + s; r.push(x.length, x.slice(58), y.slice(0, 7), x === s + String(n), y === String(n) + s); }
		  return r;`, `[61,"xx1","1xxxxxx",true,true,62,"xx12","12xxxxx",true,true,63,"xx123","123xxxx",true,true,64,"xx1234","1234xxx",true,true,65,"xx12345","12345xx",true,true]`},
		{`var e = ""; var u = "é"; var rope = "` + long + `" + "` + long + `";
		  return [e + 7, 7 + e, u + 7, 7 + u, (rope + 7).length, (rope + 7).slice(-2), (7 + rope).slice(0, 2)];`,
			`["7","7","é7","7é",121,"x7","7x"]`},
		// Objects still go through ToPrimitive.
		{`var o = {valueOf() { return 3; }, toString() { return "T"; }};
		  return ["a" + o, o + "a", "a" + [1, 2], "a" + 1n, "a" + true, "a" + null, "a" + undefined];`,
			`["a3","3a","a1,2","a1","atrue","anull","aundefined"]`},
	})
}

// TestCharStrings covers the one-unit strings: str[i], a String wrapper's
// indices, charAt and String.fromCharCode, over ASCII, other BMP units, lone
// surrogates and ropes.
func TestCharStrings(t *testing.T) {
	accTable(t, [][2]string{
		{`var s = "aé𐀀z", w = new String(s), r = [];
		  for (var i = 0; i < s.length; i++) r.push(s[i] === s.charAt(i), s[i] === w[i], s[i].length, s[i].charCodeAt(0), s[i] === String.fromCharCode(s.charCodeAt(i)));
		  return [r, s[9], w[9], Object.getOwnPropertyDescriptor(w, 1)];`,
			`[[true,true,1,97,true,true,true,1,233,true,true,true,1,55296,true,true,true,1,56320,true,true,true,1,122,true],null,null,{"value":"é","writable":false,"enumerable":true,"configurable":false}]`},
		{`var rope = ""; for (var i = 0; i < 100; i++) rope += String.fromCharCode(0x61 + i % 3, 0x4e00 + i);
		  var m = new Map(); for (var i = 0; i < rope.length; i++) m.set(rope[i], (m.get(rope[i]) || 0) + 1);
		  return [m.size, m.get("a"), m.get("一"), rope[1] === "一", rope[199] === String.fromCharCode(0x4e00 + 99)];`,
			`[103,34,1,true,true]`},
		{`var o = {}; var s = "kéy"; o[s[0]] = 1; o[s[1]] = 2; return [o.k, o["é"], Object.keys(o)];`, `[1,2,["k","é"]]`},
	})
}

// TestCharStringsShared hashes the shared one-character strings from
// several realms at once (the race detector checks that it writes nothing).
func TestCharStringsShared(t *testing.T) {
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := accEval(t, true, `var m = new Map(), s = "abcabcxyz"; for (var i = 0; i < s.length; i++) m.set(s[i], i);
			  var set = new Set("hello".split("")); return [m.size, m.get("a"), set.size, String.fromCharCode(98) === s.charAt(1)];`)
			assert.Equal(t, `[6,3,4,true]`, got)
		}()
	}
	wg.Wait()
}
