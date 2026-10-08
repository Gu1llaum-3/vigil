//go:build testing

package hub

import (
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The time-based purges find their rows through an index on the time column, not a full
// table scan holding the writer lock.
func TestRetentionPurgesUseTimeIndexes(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	for _, target := range retentionPurgeTargets {
		var plan []struct {
			Detail string `db:"detail"`
		}
		require.NoError(t, hub.DB().NewQuery("EXPLAIN QUERY PLAN "+target.selectBatchSQL()).
			Bind(dbx.Params{"cutoff": "2026-01-01 00:00:00.000Z", "limit": 10}).All(&plan))
		var details []string
		for _, row := range plan {
			details = append(details, row.Detail)
		}
		joined := strings.Join(details, " | ")
		// SEARCH on (column<?), not a SCAN of an index that merely contains the column.
		assert.Contains(t, joined, "SEARCH "+target.table, "%s: %s", target.table, joined)
		assert.Contains(t, joined, "("+target.column+"<?)", "%s: %s", target.table, joined)
	}
}

func TestRetentionPurgeDeletesInBatches(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)
	agent, err := createTestRecord(hub, "agents", map[string]any{"name": "a", "token": "t"})
	require.NoError(t, err)

	now := time.Now().UTC()
	for i := 0; i < 7; i++ {
		_, err := createTestRecord(hub, "host_metric_samples", map[string]any{"agent": agent.Id, "collected_at": now.AddDate(0, 0, -10).Add(time.Duration(i) * time.Minute)})
		require.NoError(t, err)
	}
	kept, err := createTestRecord(hub, "host_metric_samples", map[string]any{"agent": agent.Id, "collected_at": now.AddDate(0, 0, -1)})
	require.NoError(t, err)

	defer func(size int) { purgeBatchSize = size }(purgeBatchSize)
	purgeBatchSize = 3
	deleted, err := hub.purgeHostMetricSamplesOlderThan(7)
	require.NoError(t, err)
	assert.Equal(t, 7, deleted, "across three batches")
	left, err := hub.FindRecordsByFilter("host_metric_samples", "", "", 0, 0)
	require.NoError(t, err)
	require.Len(t, left, 1)
	assert.Equal(t, kept.Id, left[0].Id)
}

func TestRetentionPurgeCutoff(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)
	agent, err := createTestRecord(hub, "agents", map[string]any{"name": "a", "token": "t"})
	require.NoError(t, err)

	cutoff := time.Now().UTC().AddDate(0, 0, -7)
	older, err := createTestRecord(hub, "host_metric_samples", map[string]any{"agent": agent.Id, "collected_at": cutoff.Add(-time.Second)})
	require.NoError(t, err)
	newer, err := createTestRecord(hub, "host_metric_samples", map[string]any{"agent": agent.Id, "collected_at": cutoff.Add(time.Minute)})
	require.NoError(t, err)

	deleted, err := hub.purgeHostMetricSamplesOlderThan(7)
	require.NoError(t, err)
	assert.Equal(t, 1, deleted)
	_, err = hub.FindRecordById("host_metric_samples", older.Id)
	assert.Error(t, err, "older than the cutoff")
	_, err = hub.FindRecordById("host_metric_samples", newer.Id)
	assert.NoError(t, err)

	all, err := hub.purgeAll(hostMetricSamplesPurge.table)
	require.NoError(t, err)
	assert.Equal(t, 1, all)
}

func TestFleetMetricsWithoutTimeIndex(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)
	agent, err := createTestRecord(hub, "agents", map[string]any{"name": "a", "token": "t"})
	require.NoError(t, err)
	_, err = createTestRecord(hub, "host_metric_samples", map[string]any{"agent": agent.Id, "collected_at": time.Now().UTC(), "cpu_percent": 42})
	require.NoError(t, err)

	_, err = hub.DB().NewQuery("DROP INDEX " + fleetMetricsTimeIndex).Execute()
	require.NoError(t, err)
	series, err := hub.loadFleetMetricsSeries(time.Now().Add(-time.Hour), 60, map[string]string{})
	require.NoError(t, err, "a missing index degrades the plan, not the page")
	assert.NotEmpty(t, series["cpu"])
}
