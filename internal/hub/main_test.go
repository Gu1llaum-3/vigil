//go:build testing

package hub_test

import (
	"os"
	"testing"

	"github.com/Gu1llaum-3/vigil/internal/tests/pbtemplate"
)

// TestMain starts every test hub of this binary (in-package and hub_test) from a data
// dir migrated once, instead of replaying all migrations per test.
func TestMain(m *testing.M) {
	os.Exit(pbtemplate.Run(m.Run))
}
