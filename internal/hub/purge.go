package hub

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

const (
	dataRetentionSettingsCollection          = "data_retention_settings"
	dataRetentionSettingsSingletonKey        = "global"
	autoRetentionCronJobID                   = "vigilAutoRetention"
	autoRetentionCronExpr                    = "0 0 * * *"
	defaultMonitorEventsRetentionDays        = 30
	defaultNotificationLogsRetentionDays     = 30
	defaultSystemNotificationsRetentionDays  = 90
	defaultMonitorEventsManualDefaultDays    = 180
	defaultNotificationLogsManualDefaultDays = 180
	defaultOfflineAgentsManualDefaultDays    = 180
)

var allowedAutoRetentionDays = map[int]bool{30: true, 90: true, 180: true, 360: true}

type DataRetentionSettings struct {
	MonitorEventsRetentionDays        int `json:"monitor_events_retention_days"`
	NotificationLogsRetentionDays     int `json:"notification_logs_retention_days"`
	SystemNotificationsRetentionDays  int `json:"system_notifications_retention_days"`
	MonitorEventsManualDefaultDays    int `json:"monitor_events_manual_default_days"`
	NotificationLogsManualDefaultDays int `json:"notification_logs_manual_default_days"`
	OfflineAgentsManualDefaultDays    int `json:"offline_agents_manual_default_days"`
}

type AutomaticRetentionRunResult struct {
	MonitorEventsDeleted    int `json:"monitor_events_deleted"`
	NotificationLogsDeleted int `json:"notification_logs_deleted"`
	// SystemNotificationsDeleted counts the in-app notification feed entries purged.
	SystemNotificationsDeleted int    `json:"system_notifications_deleted"`
	Status                     string `json:"status"`
	Error                      string `json:"error,omitempty"`
	RanAt                      string `json:"ran_at"`
	SucceededAt                string `json:"succeeded_at,omitempty"`
}

func normalizeAutoRetentionDays(days, fallback int) int {
	if allowedAutoRetentionDays[days] {
		return days
	}
	return fallback
}

func normalizeManualDefaultDays(days, fallback int) int {
	if days <= 0 {
		return fallback
	}
	return days
}

func normalizeRetentionSettings(input DataRetentionSettings) DataRetentionSettings {
	return DataRetentionSettings{
		MonitorEventsRetentionDays:        normalizeAutoRetentionDays(input.MonitorEventsRetentionDays, defaultMonitorEventsRetentionDays),
		NotificationLogsRetentionDays:     normalizeAutoRetentionDays(input.NotificationLogsRetentionDays, defaultNotificationLogsRetentionDays),
		SystemNotificationsRetentionDays:  normalizeAutoRetentionDays(input.SystemNotificationsRetentionDays, defaultSystemNotificationsRetentionDays),
		MonitorEventsManualDefaultDays:    normalizeManualDefaultDays(input.MonitorEventsManualDefaultDays, defaultMonitorEventsManualDefaultDays),
		NotificationLogsManualDefaultDays: normalizeManualDefaultDays(input.NotificationLogsManualDefaultDays, defaultNotificationLogsManualDefaultDays),
		OfflineAgentsManualDefaultDays:    normalizeManualDefaultDays(input.OfflineAgentsManualDefaultDays, defaultOfflineAgentsManualDefaultDays),
	}
}

func defaultRetentionSettings() DataRetentionSettings {
	return normalizeRetentionSettings(DataRetentionSettings{})
}

func (h *Hub) getOrCreateRetentionSettingsRecord() (*core.Record, error) {
	rec, err := h.FindFirstRecordByFilter(dataRetentionSettingsCollection, "key = {:key}", dbx.Params{"key": dataRetentionSettingsSingletonKey})
	if err == nil {
		return rec, nil
	}

	col, colErr := h.FindCachedCollectionByNameOrId(dataRetentionSettingsCollection)
	if colErr != nil {
		return nil, colErr
	}
	rec = core.NewRecord(col)
	rec.Set("key", dataRetentionSettingsSingletonKey)
	defaults := defaultRetentionSettings()
	rec.Set("monitor_events_retention_days", defaults.MonitorEventsRetentionDays)
	rec.Set("notification_logs_retention_days", defaults.NotificationLogsRetentionDays)
	rec.Set("system_notifications_retention_days", defaults.SystemNotificationsRetentionDays)
	rec.Set("monitor_events_manual_default_days", defaults.MonitorEventsManualDefaultDays)
	rec.Set("notification_logs_manual_default_days", defaults.NotificationLogsManualDefaultDays)
	rec.Set("offline_agents_manual_default_days", defaults.OfflineAgentsManualDefaultDays)
	if saveErr := h.Save(rec); saveErr != nil {
		return nil, saveErr
	}
	return rec, nil
}

func retentionSettingsFromRecord(rec *core.Record) DataRetentionSettings {
	return normalizeRetentionSettings(DataRetentionSettings{
		MonitorEventsRetentionDays:        rec.GetInt("monitor_events_retention_days"),
		NotificationLogsRetentionDays:     rec.GetInt("notification_logs_retention_days"),
		SystemNotificationsRetentionDays:  rec.GetInt("system_notifications_retention_days"),
		MonitorEventsManualDefaultDays:    rec.GetInt("monitor_events_manual_default_days"),
		NotificationLogsManualDefaultDays: rec.GetInt("notification_logs_manual_default_days"),
		OfflineAgentsManualDefaultDays:    rec.GetInt("offline_agents_manual_default_days"),
	})
}

func (h *Hub) getRetentionSettings() (DataRetentionSettings, error) {
	rec, err := h.getOrCreateRetentionSettingsRecord()
	if err != nil {
		return DataRetentionSettings{}, err
	}
	return retentionSettingsFromRecord(rec), nil
}

func (h *Hub) updateRetentionSettings(input DataRetentionSettings) (DataRetentionSettings, error) {
	rec, err := h.getOrCreateRetentionSettingsRecord()
	if err != nil {
		return DataRetentionSettings{}, err
	}
	settings := normalizeRetentionSettings(input)
	rec.Set("monitor_events_retention_days", settings.MonitorEventsRetentionDays)
	rec.Set("notification_logs_retention_days", settings.NotificationLogsRetentionDays)
	rec.Set("system_notifications_retention_days", settings.SystemNotificationsRetentionDays)
	rec.Set("monitor_events_manual_default_days", settings.MonitorEventsManualDefaultDays)
	rec.Set("notification_logs_manual_default_days", settings.NotificationLogsManualDefaultDays)
	rec.Set("offline_agents_manual_default_days", settings.OfflineAgentsManualDefaultDays)
	if err := h.Save(rec); err != nil {
		return DataRetentionSettings{}, err
	}
	return settings, nil
}

// purgeBatchSize bounds each retention DELETE, so the SQLite writer lock is released between
// batches instead of being held for the whole purge (metric and monitor writes wait on it).
var purgeBatchSize = 5000

// retentionPurgeTarget is an append-only table purged by age; column is its time column,
// which must be indexed (TestRetentionPurgesUseTimeIndexes).
type retentionPurgeTarget struct {
	table, column string
}

var (
	monitorEventsPurge          = retentionPurgeTarget{"monitor_events", "checked_at"}
	notificationLogsPurge       = retentionPurgeTarget{"notification_logs", "sent_at"}
	systemNotificationsPurge    = retentionPurgeTarget{"system_notifications", "occurred_at"}
	hostMetricSamplesPurge      = retentionPurgeTarget{"host_metric_samples", "collected_at"}
	containerMetricSamplesPurge = retentionPurgeTarget{"container_metric_samples", "collected_at"}
)

var retentionPurgeTargets = []retentionPurgeTarget{
	monitorEventsPurge, notificationLogsPurge, systemNotificationsPurge, hostMetricSamplesPurge, containerMetricSamplesPurge,
}

// selectBatchSQL selects the next batch to delete (rowid: PocketBase ids are a TEXT primary
// key, not the rowid alias).
func (t retentionPurgeTarget) selectBatchSQL() string {
	return "SELECT rowid FROM " + t.table + " WHERE " + t.column + " < {:cutoff} LIMIT {:limit}"
}

// purgeOlderThan deletes the rows of target older than days, in batches of purgeBatchSize,
// and returns how many it deleted. The cutoff uses the format PocketBase stores dates in, so
// the comparison is exact.
func (h *Hub) purgeOlderThan(target retentionPurgeTarget, days int) (int, error) {
	if days <= 0 {
		return 0, fmt.Errorf("days must be greater than 0")
	}
	return h.deleteInBatches(target.table, target.selectBatchSQL(), dbx.Params{"cutoff": types.NowDateTime().AddDate(0, 0, -days).String()})
}

// purgeAll empties table, in batches like the age-based purges.
func (h *Hub) purgeAll(table string) (int, error) {
	return h.deleteInBatches(table, "SELECT rowid FROM "+table+" LIMIT {:limit}", dbx.Params{})
}

// deleteInBatches deletes the rows selected by selectBatch (a rowid query taking {:limit}),
// purgeBatchSize at a time, each batch its own write transaction. On error it returns how
// many rows the earlier batches deleted.
func (h *Hub) deleteInBatches(table, selectBatch string, params dbx.Params) (int, error) {
	params["limit"] = purgeBatchSize
	deleted := 0
	for {
		// The hub is stopping: leave the rest to the next run rather than hold the shutdown.
		if err := h.backgroundContext().Err(); err != nil {
			return deleted, err
		}
		res, err := h.DB().NewQuery("DELETE FROM " + table + " WHERE rowid IN (" + selectBatch + ")").Bind(params).Execute()
		if err != nil {
			return deleted, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return deleted, err
		}
		deleted += int(n)
		if n < int64(purgeBatchSize) {
			return deleted, nil
		}
	}
}

func (h *Hub) purgeMonitorEventsOlderThan(days int) (int, error) {
	return h.purgeOlderThan(monitorEventsPurge, days)
}

func (h *Hub) purgeAllMonitorEvents() (int, error) {
	return h.purgeAll(monitorEventsPurge.table)
}

func (h *Hub) purgeNotificationLogsOlderThan(days int) (int, error) {
	return h.purgeOlderThan(notificationLogsPurge, days)
}

func (h *Hub) purgeAllNotificationLogs() (int, error) {
	return h.purgeAll(notificationLogsPurge.table)
}

// purgeSystemNotificationsOlderThan trims the in-app notification feed (the navbar bell).
func (h *Hub) purgeSystemNotificationsOlderThan(days int) (int, error) {
	return h.purgeOlderThan(systemNotificationsPurge, days)
}

func (h *Hub) purgeOfflineAgentsOlderThan(days int) (int, error) {
	if days <= 0 {
		return 0, fmt.Errorf("days must be greater than 0")
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -days)
	records, err := h.FindRecordsByFilter("agents", "status = 'offline' && last_seen != '' && last_seen < {:cutoff}", "", 0, 0, dbx.Params{"cutoff": cutoff})
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, rec := range records {
		if err := h.Delete(rec); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

func (h *Hub) purgeAllOfflineAgents() (int, error) {
	records, err := h.FindRecordsByFilter("agents", "status = 'offline'", "", 0, 0)
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, rec := range records {
		if err := h.Delete(rec); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

func (h *Hub) runAutomaticRetentionPurge() AutomaticRetentionRunResult {
	ranAt := time.Now().UTC()
	result := AutomaticRetentionRunResult{
		Status: "failed",
		RanAt:  ranAt.Format(time.RFC3339),
	}

	settings, err := h.getRetentionSettings()
	if err != nil {
		result.Error = err.Error()
		slog.Warn("retention purge: failed to load settings", "err", err)
		return result
	}

	var failures []string
	purge := func(what string, days int, run func(int) (int, error)) int {
		deleted, err := run(days)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", what, err))
			slog.Warn("retention purge: failed to purge "+what, "days", days, "err", err)
			return deleted
		}
		slog.Info("retention purge: purged "+what, "days", days, "deleted", deleted)
		return deleted
	}
	result.MonitorEventsDeleted = purge("monitor events", settings.MonitorEventsRetentionDays, h.purgeMonitorEventsOlderThan)
	result.NotificationLogsDeleted = purge("notification logs", settings.NotificationLogsRetentionDays, h.purgeNotificationLogsOlderThan)
	result.SystemNotificationsDeleted = purge("in-app notifications", settings.SystemNotificationsRetentionDays, h.purgeSystemNotificationsOlderThan)

	if len(failures) > 0 {
		result.Error = strings.Join(failures, "; ")
	} else {
		result.Status = "success"
		result.SucceededAt = ranAt.Format(time.RFC3339)
	}

	return result
}
