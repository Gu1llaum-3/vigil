//go:build testing

package hub

import (
	"sync"
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

// TestUpsertByUniqueConcurrent exercises the retry-on-conflict path: many goroutines
// upsert the same unique key at once (as the connect-time and ticker collection paths
// can), and the result must be exactly one record with no error escaping the helper.
func TestUpsertByUniqueConcurrent(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	const agentID = "agentupsert002"
	const workers = 8

	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			errs <- hub.upsertByUnique(hostMetricCurrentCollection, "agent = {:agent}", dbx.Params{"agent": agentID}, func(rec *core.Record) {
				rec.Set("agent", agentID)
				rec.Set("cpu_percent", float64(n))
				rec.Set("collected_at", "2026-06-18T00:00:00Z")
			})
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}

	records, err := hub.FindRecordsByFilter(hostMetricCurrentCollection, "agent = {:agent}", "", 0, 0, dbx.Params{"agent": agentID})
	require.NoError(t, err)
	require.Len(t, records, 1)
}
