package syntax

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pluginDir is the new-api task plugin corpus, fetched into the bench
// module's test data by bench/testdata/plugins/fetch.sh.
const pluginDir = "../bench/testdata/plugins"

// pluginExports is the exact set of exported names per plugin, derived once
// from the corpus and checked in as expected values.
var pluginExports = map[string][]string{
	"alibaba":   {"buildContentRequest", "buildQueryRequest", "buildSubmitRequest", "extractUsage", "extractUsageOnComplete", "extractUsageOnSubmit", "listArtifacts", "meta", "native", "parseSubmitEventDelta", "parseSubmitResponse", "parseTaskResult", "protocols"},
	"doubao":    {"buildContentRequest", "buildQueryRequest", "buildSubmitRequest", "extractUsage", "extractUsageOnComplete", "extractUsageOnSubmit", "listArtifacts", "meta", "native", "parseSubmitResponse", "parseTaskResult", "protocols"},
	"google":    {"buildContentRequest", "buildQueryRequest", "buildSubmitRequest", "extractUsage", "extractUsageOnComplete", "listArtifacts", "meta", "parseSubmitResponse", "parseTaskResult", "protocols"},
	"hailuo":    {"buildContentRequest", "buildQueryRequest", "buildSubmitRequest", "extractUsage", "extractUsageOnComplete", "listArtifacts", "meta", "parseSubmitResponse", "parseTaskResult", "protocols"},
	"jimeng":    {"buildContentRequest", "buildQueryRequest", "buildSubmitRequest", "extractUsage", "extractUsageOnComplete", "listArtifacts", "meta", "native", "parseSubmitResponse", "parseTaskResult", "protocols"},
	"kling":     {"buildContentRequest", "buildQueryRequest", "buildSubmitRequest", "extractUsage", "extractUsageOnComplete", "listArtifacts", "meta", "native", "parseSubmitResponse", "parseTaskResult", "protocols"},
	"sora":      {"buildContentRequest", "buildQueryRequest", "buildSubmitRequest", "extractUsage", "extractUsageOnComplete", "listArtifacts", "meta", "parseSubmitResponse", "parseTaskResult", "protocols"},
	"sunoapi":   {"buildBatchQueryRequest", "buildContentRequest", "buildQueryRequest", "buildSubmitRequest", "extractUsage", "extractUsageOnComplete", "listArtifacts", "meta", "native", "parseBatchResult", "parseSubmitResponse", "parseTaskResult", "protocols"},
	"vertex-ai": {"buildContentRequest", "buildQueryRequest", "buildSubmitRequest", "extractUsage", "extractUsageOnComplete", "listArtifacts", "meta", "parseSubmitResponse", "parseTaskResult", "protocols"},
	"vidu":      {"buildContentRequest", "buildQueryRequest", "buildSubmitRequest", "extractUsage", "extractUsageOnComplete", "listArtifacts", "meta", "parseSubmitResponse", "parseTaskResult", "protocols"},
}

type pluginSource struct {
	name string
	path string
	src  string
}

func loadPlugins(tb testing.TB) []pluginSource {
	tb.Helper()
	paths, err := filepath.Glob(filepath.Join(pluginDir, "*", "plugin.js"))
	require.NoError(tb, err)
	if len(paths) == 0 {
		tb.Skip("the new-api plugins are not fetched; run bench/testdata/plugins/fetch.sh")
	}
	require.Len(tb, paths, len(pluginExports), "plugin corpus changed; update pluginExports")
	sort.Strings(paths)
	plugins := make([]pluginSource, 0, len(paths))
	for _, path := range paths {
		src, err := os.ReadFile(path)
		require.NoError(tb, err)
		plugins = append(plugins, pluginSource{name: filepath.Base(filepath.Dir(path)), path: path, src: string(src)})
	}
	return plugins
}

func TestParsePlugins(t *testing.T) {
	for _, plugin := range loadPlugins(t) {
		t.Run(plugin.name, func(t *testing.T) {
			m, err := ParseModule(plugin.path, plugin.src, Options{})
			require.NoError(t, err)
			require.NotNil(t, m.Scope)
			names := make([]string, 0, len(m.Exports))
			for _, e := range m.Exports {
				names = append(names, e.Name)
				assert.NotNil(t, e.Binding, "export %s has no binding", e.Name)
				assert.True(t, e.Binding.Exported)
			}
			sort.Strings(names)
			assert.Equal(t, pluginExports[plugin.name], names)
			assert.Equal(t, len(plugin.src), m.End)
		})
	}
}

func TestPluginsResolveAllIdentifiers(t *testing.T) {
	// Every identifier reference in the corpus either resolves to a binding
	// or names one of the globals the host provides.
	globals := map[string]bool{
		"undefined": true, "NaN": true, "Infinity": true, "Object": true, "Array": true, "String": true,
		"Number": true, "Boolean": true, "Error": true, "TypeError": true, "RangeError": true, "JSON": true,
		"Math": true, "RegExp": true, "Date": true, "parseInt": true, "parseFloat": true, "isNaN": true,
		"isFinite": true, "encodeURIComponent": true, "decodeURIComponent": true, "encodeURI": true,
		"decodeURI": true, "utils": true, "console": true, "globalThis": true, "SyntaxError": true,
		"ReferenceError": true,
	}
	for _, plugin := range loadPlugins(t) {
		m, err := ParseModule(plugin.path, plugin.src, Options{})
		require.NoError(t, err)
		unresolved := map[string]bool{}
		var visit func(n Node)
		visit = func(n Node) {
			Inspect(n, func(n Node) bool {
				switch n := n.(type) {
				case *MemberExpr:
					visit(n.Object)
					if n.Computed {
						visit(n.Prop)
					}
					return false
				case *Property:
					if n.Computed && n.Key != nil {
						visit(n.Key)
					}
					visit(n.Value)
					return false
				case *PatternProp:
					if n.Computed {
						visit(n.Key)
					}
					visit(n.Value)
					return false
				case *LabeledStmt:
					visit(n.Body)
					return false
				case *BreakStmt, *ContinueStmt:
					return false
				case *Function:
					for _, p := range n.Params {
						visit(p)
					}
					if n.Rest != nil {
						visit(n.Rest)
					}
					if n.Body != nil {
						visit(n.Body)
					}
					if n.ExprBody != nil {
						visit(n.ExprBody)
					}
					return false
				case *Ident:
					if n.Binding == nil && !globals[n.Name] {
						unresolved[n.Name] = true
					}
				}
				return true
			})
		}
		visit(m)
		assert.Empty(t, unresolved, "%s: unresolved identifiers", plugin.name)
	}
}

func BenchmarkParsePlugins(b *testing.B) {
	plugins := loadPlugins(b)
	total := 0
	for _, p := range plugins {
		total += len(p.src)
	}
	b.SetBytes(int64(total))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for _, p := range plugins {
			if _, err := ParseModule(p.path, p.src, Options{}); err != nil {
				b.Fatal(err)
			}
		}
	}
}
