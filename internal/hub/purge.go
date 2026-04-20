package hub

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	dataRetentionSettingsCollection          = "data_retention_settings"
	dataRetentionSettingsSingletonKey        = "global"
	autoRetentionCronJobID                   = "vigilAutoRetention"
	autoRetentionCronExpr                    = "0 0 * * *"
	defaultMonitorEventsRetentionDays        = 30
	defaultNotificationLogsRetentionDays     = 30
	defaultMonitorEventsManualDefaultDays    = 180
	defaultNotificationLogsManualDefaultDays = 180
	defaultOfflineAgentsManualDefaultDays    = 180
)

var allowedAutoRetentionDays = map[int]bool{30: true, 90: true, 180: true, 360: true}

type DataRetentionSettings struct {
	MonitorEventsRetentionDays        int `json:"monitor_events_retention_days"`
	NotificationLogsRetentionDays     int `json:"notification_logs_retention_days"`
	MonitorEventsManualDefaultDays    int `json:"monitor_events_manual_default_days"`
	NotificationLogsManualDefaultDays int `json:"notification_logs_manual_default_days"`
	OfflineAgentsManualDefaultDays    int `json:"offline_agents_manual_default_days"`
}

type AutomaticRetentionRunResult struct {
	MonitorEventsDeleted    int    `json:"monitor_events_deleted"`
	NotificationLogsDeleted int    `json:"notification_logs_deleted"`
	Status                  string `json:"status"`
	Error                   string `json:"error,omitempty"`
	RanAt                   string `json:"ran_at"`
	SucceededAt             string `json:"succeeded_at,omitempty"`
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
	rec.Set("monitor_events_manual_default_days", settings.MonitorEventsManualDefaultDays)
	rec.Set("notification_logs_manual_default_days", settings.NotificationLogsManualDefaultDays)
	rec.Set("offline_agents_manual_default_days", settings.OfflineAgentsManualDefaultDays)
	if err := h.Save(rec); err != nil {
		return DataRetentionSettings{}, err
	}
	return settings, nil
}

func countRows(app core.App, query string, params dbx.Params) (int, error) {
	if params == nil {
		params = dbx.Params{}
	}
	var row struct {
		Count int `db:"count"`
	}
	err := app.DB().NewQuery(query).Bind(params).One(&row)
	return row.Count, err
}

func deleteRows(app core.App, query string, params dbx.Params) error {
	if params == nil {
		params = dbx.Params{}
	}
	_, err := app.DB().NewQuery(query).Bind(params).Execute()
	return err
}

func (h *Hub) purgeMonitorEventsOlderThan(days int) (int, error) {
	if days <= 0 {
		return 0, fmt.Errorf("days must be greater than 0")
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -days)
	params := dbx.Params{"cutoff": cutoff}
	count, err := countRows(h, "SELECT COUNT(*) AS count FROM monitor_events WHERE checked_at < {:cutoff}", params)
	if err != nil || count == 0 {
		return count, err
	}
	return count, deleteRows(h, "DELETE FROM monitor_events WHERE checked_at < {:cutoff}", params)
}

func (h *Hub) purgeAllMonitorEvents() (int, error) {
	count, err := countRows(h, "SELECT COUNT(*) AS count FROM monitor_events", nil)
	if err != nil || count == 0 {
		return count, err
	}
	return count, deleteRows(h, "DELETE FROM monitor_events", nil)
}

func (h *Hub) purgeNotificationLogsOlderThan(days int) (int, error) {
	if days <= 0 {
		return 0, fmt.Errorf("days must be greater than 0")
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -days)
	params := dbx.Params{"cutoff": cutoff}
	count, err := countRows(h, "SELECT COUNT(*) AS count FROM notification_logs WHERE sent_at < {:cutoff}", params)
	if err != nil || count == 0 {
		return count, err
	}
	return count, deleteRows(h, "DELETE FROM notification_logs WHERE sent_at < {:cutoff}", params)
}

func (h *Hub) purgeAllNotificationLogs() (int, error) {
	count, err := countRows(h, "SELECT COUNT(*) AS count FROM notification_logs", nil)
	if err != nil || count == 0 {
		return count, err
	}
	return count, deleteRows(h, "DELETE FROM notification_logs", nil)
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

	monitorDeleted, monitorErr := h.purgeMonitorEventsOlderThan(settings.MonitorEventsRetentionDays)
	notificationDeleted, notificationErr := h.purgeNotificationLogsOlderThan(settings.NotificationLogsRetentionDays)
	result.MonitorEventsDeleted = monitorDeleted
	result.NotificationLogsDeleted = notificationDeleted

	if monitorErr != nil || notificationErr != nil {
		switch {
		case monitorErr != nil && notificationErr != nil:
			result.Error = fmt.Sprintf("monitor events: %v; notification logs: %v", monitorErr, notificationErr)
		case monitorErr != nil:
			result.Error = fmt.Sprintf("monitor events: %v", monitorErr)
		default:
			result.Error = fmt.Sprintf("notification logs: %v", notificationErr)
		}
	} else {
		result.Status = "success"
		result.SucceededAt = ranAt.Format(time.RFC3339)
	}

	if monitorErr != nil {
		slog.Warn("retention purge: failed to purge monitor events", "days", settings.MonitorEventsRetentionDays, "err", monitorErr)
	} else {
		slog.Info("retention purge: purged monitor events", "days", settings.MonitorEventsRetentionDays, "deleted", monitorDeleted)
	}
	if notificationErr != nil {
		slog.Warn("retention purge: failed to purge notification logs", "days", settings.NotificationLogsRetentionDays, "err", notificationErr)
	} else {
		slog.Info("retention purge: purged notification logs", "days", settings.NotificationLogsRetentionDays, "deleted", notificationDeleted)
	}

	return result
}
