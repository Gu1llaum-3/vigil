//go:build testing

package hub

import (
	"testing"

	"github.com/Gu1llaum-3/vigil/internal/common"
	"github.com/pocketbase/dbx"
	"github.com/stretchr/testify/require"
)

func TestMonitoredWorstDisk(t *testing.T) {
	mounts := []common.DiskMount{
		{Mountpoint: "/", UsedPercent: 40},
		{Mountpoint: "/data", UsedPercent: 95},
		{Mountpoint: "/boot", UsedPercent: 60},
	}
	h := &Hub{}

	// Legacy agent (no per-mount data) → monitored=false so the caller keeps agent values,
	// and diskMuted stays false (legacy is not a mute).
	if _, _, monitored := h.monitoredWorstDisk("a1", nil); monitored {
		t.Fatal("no mounts must report monitored=false")
	}
	require.False(t, h.diskMuted("a1", nil))

	// No rule → worst of all mounts.
	pct, mount, monitored := h.monitoredWorstDisk("a1", mounts)
	require.True(t, monitored)
	require.Equal(t, 95.0, pct)
	require.Equal(t, "/data", mount)

	// Exclude /data → worst becomes /boot.
	h.diskRules.rules = map[string]diskMonitorRule{"a1": {mode: "exclude", mounts: map[string]bool{"/data": true}}}
	pct, mount, _ = h.monitoredWorstDisk("a1", mounts)
	require.Equal(t, 60.0, pct)
	require.Equal(t, "/boot", mount)
	require.False(t, h.diskMuted("a1", mounts))

	// Include only / → 40, even though /data is fuller.
	h.diskRules.rules = map[string]diskMonitorRule{"a1": {mode: "include", mounts: map[string]bool{"/": true}}}
	pct, mount, _ = h.monitoredWorstDisk("a1", mounts)
	require.Equal(t, 40.0, pct)
	require.Equal(t, "/", mount)

	// Exclude everything → monitored=false → the host is muted (alert cleared, no fire).
	h.diskRules.rules = map[string]diskMonitorRule{"a1": {mode: "exclude", mounts: map[string]bool{"/": true, "/data": true, "/boot": true}}}
	_, _, monitored = h.monitoredWorstDisk("a1", mounts)
	require.False(t, monitored)
	require.True(t, h.diskMuted("a1", mounts))

	// A host without a rule is unaffected by another host's rule.
	pct, _, _ = h.monitoredWorstDisk("other", mounts)
	require.Equal(t, 95.0, pct)
}

// TestPersistHostMetricsUsesMonitoredWorst locks the full D2 wiring: a rule excluding a mount
// makes the persisted disk_max_used_percent/disk_max_mount reflect the worst *monitored* mount,
// not the agent's blind worst.
func TestPersistHostMetricsUsesMonitoredWorst(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	agent, err := createTestRecord(hub, "agents", map[string]any{"name": "h1", "token": "tok-h1"})
	require.NoError(t, err)
	_, err = createTestRecord(hub, diskMonitorRulesCollection, map[string]any{
		"agent":  agent.Id,
		"mode":   "exclude",
		"mounts": []string{"/data"},
	})
	require.NoError(t, err)
	require.NoError(t, hub.refreshDiskRuleCache())

	hub.persistHostMetrics(agent.Id, common.HostMetricsResponse{
		DiskUsedPercent:    40,
		DiskMaxUsedPercent: 95, // agent's blind worst is /data
		DiskMaxMount:       "/data",
		DiskMounts: []common.DiskMount{
			{Mountpoint: "/", UsedPercent: 40},
			{Mountpoint: "/data", UsedPercent: 95},
			{Mountpoint: "/boot", UsedPercent: 60},
		},
	})

	rec, err := hub.FindFirstRecordByFilter(hostMetricCurrentCollection, "agent = {:a}", dbx.Params{"a": agent.Id})
	require.NoError(t, err)
	require.Equal(t, 60.0, numberAsFloat64(rec.Get("disk_max_used_percent")), "/data excluded → worst is /boot 60")
	require.Equal(t, "/boot", rec.GetString("disk_max_mount"))
}

// TestPersistHostMetricsMutedKeepsRealValue: when the rule matches no mount (muted), the
// worst is NOT folded to 0 — the persisted disk_max_used_percent keeps the agent's reported
// value so history doesn't hide a real full disk (the alert is muted separately).
func TestPersistHostMetricsMutedKeepsRealValue(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	agent, err := createTestRecord(hub, "agents", map[string]any{"name": "h2", "token": "tok-h2"})
	require.NoError(t, err)
	_, err = createTestRecord(hub, diskMonitorRulesCollection, map[string]any{
		"agent":  agent.Id,
		"mode":   "exclude",
		"mounts": []string{"/", "/data"},
	})
	require.NoError(t, err)
	require.NoError(t, hub.refreshDiskRuleCache())

	metrics := common.HostMetricsResponse{
		DiskUsedPercent:    40,
		DiskMaxUsedPercent: 95,
		DiskMaxMount:       "/data",
		DiskMounts: []common.DiskMount{
			{Mountpoint: "/", UsedPercent: 40},
			{Mountpoint: "/data", UsedPercent: 95},
		},
	}
	require.True(t, hub.diskMuted(agent.Id, metrics.DiskMounts), "every mount excluded → muted")
	hub.persistHostMetrics(agent.Id, metrics)

	rec, err := hub.FindFirstRecordByFilter(hostMetricCurrentCollection, "agent = {:a}", dbx.Params{"a": agent.Id})
	require.NoError(t, err)
	require.Equal(t, 95.0, numberAsFloat64(rec.Get("disk_max_used_percent")), "muted host keeps real reported worst, not fake 0")
}
