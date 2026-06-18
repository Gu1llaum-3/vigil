//go:build testing

package hub

import (
	"errors"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/stretchr/testify/require"
)

// TestUpsertByUniqueSequential verifies the basic find-then-update behavior: a second
// upsert for the same unique key updates the existing record instead of inserting.
func TestUpsertByUniqueSequential(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	const agentID = "agentupsert001"
	apply := func(cpu float64) func(*core.Record) {
		return func(rec *core.Record) {
			rec.Set("agent", agentID)
			rec.Set("cpu_percent", cpu)
			rec.Set("collected_at", "2026-06-18T00:00:00Z")
		}
	}

	require.NoError(t, hub.upsertByUnique(hostMetricCurrentCollection, "agent = {:agent}", dbx.Params{"agent": agentID}, apply(10)))
	require.NoError(t, hub.upsertByUnique(hostMetricCurrentCollection, "agent = {:agent}", dbx.Params{"agent": agentID}, apply(20)))

	records, err := hub.FindRecordsByFilter(hostMetricCurrentCollection, "agent = {:agent}", "", 0, 0, dbx.Params{"agent": agentID})
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.InDelta(t, 20.0, records[0].GetFloat("cpu_percent"), 0.001)
}

// TestIsUniqueConstraintErr covers the predicate that drives the retry-on-conflict path
// in upsertByUnique. The retry only fires when a concurrent insert is recognized, and
// PocketBase surfaces that as a validation-style message ("Value must be unique."), not
// the raw SQLite text — so both forms must match while unrelated errors must not.
func TestIsUniqueConstraintErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"pocketbase validation", errors.New("agent: Value must be unique."), true},
		{"raw sqlite", errors.New("UNIQUE constraint failed: host_metric_current.agent"), true},
		{"uppercase sqlite", errors.New("UNIQUE CONSTRAINT FAILED"), true},
		{"unrelated", errors.New("database is closed"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, isUniqueConstraintErr(tc.err))
		})
	}
}
