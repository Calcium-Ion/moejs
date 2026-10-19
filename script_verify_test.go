package moejs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunScriptVerifier checks that a script whose bytecode the engine's
// verifier rejects (here its root left without registers; only a compiler
// bug could do it) does not run: RunScript returns the verifier's error,
// from SetHostDefined for a script with a direct eval (its referrer record)
// and from the engine's RunScript for the others, and declares nothing.
func TestRunScriptVerifier(t *testing.T) {
	for _, src := range []string{
		`var ran = [1].length;`,
		`var ran = [1].length; eval("ran = 2");`,
	} {
		s, err := CompileScript("v.js", src)
		require.NoError(t, err)
		s.code.NumRegs = 0
		rt := NewRuntime(Options{})
		_, err = rt.RunScript(s)
		require.Error(t, err, src)
		assert.Regexp(t, `^engine: bytecode: .*names registers`, err.Error(), src)
		probe, err := CompileScript("p.js", `typeof ran`)
		require.NoError(t, err)
		v, err := rt.RunScript(probe)
		require.NoError(t, err)
		assert.Equal(t, "undefined", v.String(), "%s declared its globals", src)
	}
}
