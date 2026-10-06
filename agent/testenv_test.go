//go:build testing

package agent

import (
	"os"
	"testing"

	app "github.com/Gu1llaum-3/vigil"
)

// unsetAgentEnv removes the given agent variables, in both their VIGIL_AGENT_-prefixed
// and plain forms, for the duration of the test. A prefixed variable wins even when it
// is empty, so setting it to "" is not enough: it has to be unset. t.Setenv records the
// original value so it is restored afterwards.
func unsetAgentEnv(t *testing.T, names ...string) {
	t.Helper()
	for _, name := range names {
		for _, k := range []string{app.AgentEnvPrefix + name, name} {
			t.Setenv(k, "")
			os.Unsetenv(k)
		}
	}
}
