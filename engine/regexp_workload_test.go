package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Calcium-Ion/moejs/syntax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every regular expression of the plugin workload (bench/testdata/plugins)
// and of the bench corpus runs on RE2 or the single-class fast path: the
// backtracking VM only takes patterns RE2 cannot run exactly. The
// backtracking corpus program exercises the VM on purpose and is skipped.
func TestRegExpWorkloadSelection(t *testing.T) {
	plugins, err := filepath.Glob("../bench/testdata/plugins/*/plugin.js")
	require.NoError(t, err)
	if len(plugins) == 0 {
		t.Skip("the new-api plugins are not fetched; run bench/testdata/plugins/fetch.sh")
	}
	corpus, err := filepath.Glob("../bench/corpus/*.js")
	require.NoError(t, err)
	require.NotEmpty(t, corpus)
	files := append(plugins, corpus...)
	r := NewRealm()
	total := 0
	for _, path := range files {
		if filepath.Base(path) == "regexp_bt.js" {
			continue
		}
		src, err := os.ReadFile(path)
		require.NoError(t, err)
		mod, err := syntax.ParseModule(path, string(src), syntax.Options{})
		require.NoError(t, err, path)
		syntax.Inspect(mod, func(n syntax.Node) bool {
			pattern, flags, ok := workloadRegExp(n)
			if !ok {
				return true
			}
			f, valid := parseRegExpFlags(flags)
			require.True(t, valid, "%s: /%s/%s", path, pattern, flags)
			c, err := r.compileRegExp(FromGoString(pattern), f)
			if pattern == "(" {
				assert.Error(t, err, "the corpus's invalid pattern")
				return true
			}
			require.NoError(t, err, "%s: /%s/%s", path, pattern, flags)
			assert.True(t, c.re != nil || c.simple != nil, "%s: /%s/%s must run on RE2 or the fast path", path, pattern, flags)
			total++
			return true
		})
	}
	assert.Greater(t, total, 50, "the workload's regular expressions were found")
	t.Logf("%d regular expressions in %d files", total, len(files))
}

// workloadRegExp returns the pattern and flags of a regular expression
// literal or of RegExp called with string literals.
func workloadRegExp(n syntax.Node) (pattern, flags string, ok bool) {
	var callee syntax.Expr
	var args []syntax.Expr
	switch n := n.(type) {
	case *syntax.RegexLit:
		return n.Pattern, n.Flags, true
	case *syntax.NewExpr:
		callee, args = n.Callee, n.Args
	case *syntax.CallExpr:
		callee, args = n.Callee, n.Args
	default:
		return "", "", false
	}
	if id, isIdent := callee.(*syntax.Ident); !isIdent || id.Name != "RegExp" || len(args) == 0 || len(args) > 2 {
		return "", "", false
	}
	for i, a := range args {
		s, isStr := a.(*syntax.StringLit)
		if !isStr {
			return "", "", false
		}
		if i == 0 {
			pattern = s.Value
		} else {
			flags = s.Value
		}
	}
	return pattern, flags, true
}
