package engine

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestIntlDateTimeFormatNode runs the cases of testdata/intl_date_cases.js
// and compares their outputs with Node 24's (testdata/intl_date_node.json.gz,
// written by intl_date_node.mjs): format, formatToParts, resolvedOptions,
// formatRange and formatRangeToParts over the data locales, option sets
// and time values. The differences it allows are listed in
// intlDateNodeAllowed with their reasons.
func TestIntlDateTimeFormatNode(t *testing.T) {
	f, err := os.Open("testdata/intl_date_node.json.gz")
	require.NoError(t, err)
	defer f.Close()
	zr, err := gzip.NewReader(f)
	require.NoError(t, err)
	data, err := io.ReadAll(zr)
	require.NoError(t, err)
	var fixture struct {
		Node, ICU, Names string
		Outputs          []string
	}
	require.NoError(t, json.Unmarshal(data, &fixture))
	src, err := os.ReadFile("testdata/intl_date_cases.js")
	require.NoError(t, err)

	r := NewRealmWith(RealmOptions{TimeZone: time.UTC})
	v, err := runScriptValue(t, r, "JSON.stringify("+strings.TrimSuffix(strings.TrimSpace(string(src)), ";")+")")
	require.NoError(t, err)
	var got [][2]string
	require.NoError(t, json.Unmarshal([]byte(v.AsString().GoString()), &got))
	require.Equal(t, len(fixture.Outputs), len(got), "case count")
	names := make([]string, len(got))
	for i, c := range got {
		names[i] = c[0]
	}
	sum := sha256.Sum256([]byte(strings.Join(names, "\n")))
	require.Equal(t, fixture.Names, hex.EncodeToString(sum[:]), "the case names differ from the fixture's")

	// INTL_DATE_DUMP names a file that receives every difference with its
	// reason.
	allowed := map[string]int{}
	var dump, failed strings.Builder
	failures := 0
	for i, node := range fixture.Outputs {
		name, moe := got[i][0], got[i][1]
		if node == moe {
			continue
		}
		reason := intlDateNodeAllowed(name, node, moe)
		line := fmt.Sprintf("%s [%s]\n  node %q\n  moe  %q\n", name, reason, node, moe)
		dump.WriteString(line)
		if reason == "" {
			failures++
			failed.WriteString(line)
			continue
		}
		allowed[reason]++
	}
	t.Logf("Node %s, ICU %s: %d cases, allowed differences %v", fixture.Node, fixture.ICU, len(got), allowed)
	if p := os.Getenv("INTL_DATE_DUMP"); p != "" {
		os.WriteFile(p, []byte(dump.String()), 0o644)
	}
	if failures > 0 {
		s := failed.String()
		if len(s) > 4000 {
			s = s[:4000]
		}
		t.Errorf("%d cases differ from Node:\n%s", failures, s)
	}
}

// intlJSONField returns the raw value of a field of a JSON object's text.
func intlJSONField(s, name string) string {
	i := strings.Index(s, `"`+name+`":`)
	if i < 0 {
		return ""
	}
	s = s[i+len(name)+3:]
	if end := strings.IndexAny(s, ",}"); end >= 0 {
		return s[:end]
	}
	return s
}

// intlDateNodeAllowed returns why a case may differ from Node, "" when it
// may not.
func intlDateNodeAllowed(c, node, moe string) string {
	switch {
	case strings.Contains(node, `"Asia/Calcutta"`) && node == strings.ReplaceAll(moe, `"Asia/Kolkata"`, `"Asia/Calcutta"`):
		// resolvedOptions().timeZone: V8 reports ICU's canonical ID; the
		// specification (and test262's timezone-not-canonicalized.js) keeps
		// the identifier given.
		return "time zone identifier"
	case strings.HasPrefix(c, "ja {") && strings.Contains(c, `"hour12":true`):
		// [[hourCycle12]] of ja: h11, the first 12-hour cycle of Japan's
		// allowed hours in CLDR ("H K h"), which test262's
		// hourCycle-default.js asserts; V8 has h11 only for a locale with
		// the region JP.
		return "ja hour12"
	case strings.HasPrefix(c, "en-u-hc-h11 ") && strings.Contains(c, `"hour12":true`):
		// hour12 sets the hc option to null, which ResolveLocale takes over
		// the locale's hc keyword: [[hourCycle12]] of en, h12, without the
		// keyword. V8 keeps the keyword and makes it the default of
		// hourCycle12.
		return "hour12 over the hc keyword"
	}
	return ""
}
