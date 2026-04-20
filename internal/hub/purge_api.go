package hub

import (
	"fmt"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

type purgeSettingsResponse = DataRetentionSettings

type purgeRunInput struct {
	Scope string `json:"scope"`
	Mode  string `json:"mode"`
	Days  int    `json:"days"`
}

type purgeRunResponse struct {
	Scope        string `json:"scope"`
	Mode         string `json:"mode"`
	DeletedCount int    `json:"deleted_count"`
}

func (h *Hub) getPurgeSettings(e *core.RequestEvent) error {
	settings, err := h.getRetentionSettings()
	if err != nil {
		return err
	}
	return e.JSON(http.StatusOK, purgeSettingsResponse(settings))
}

func (h *Hub) updatePurgeSettings(e *core.RequestEvent) error {
	var input DataRetentionSettings
	if err := e.BindBody(&input); err != nil {
		return e.BadRequestError("Invalid request body", err)
	}
	if !allowedAutoRetentionDays[input.MonitorEventsRetentionDays] {
		return e.BadRequestError(fmt.Sprintf("invalid monitor_events_retention_days: %d", input.MonitorEventsRetentionDays), nil)
	}
	if !allowedAutoRetentionDays[input.NotificationLogsRetentionDays] {
		return e.BadRequestError(fmt.Sprintf("invalid notification_logs_retention_days: %d", input.NotificationLogsRetentionDays), nil)
	}
	if input.MonitorEventsManualDefaultDays <= 0 || input.NotificationLogsManualDefaultDays <= 0 || input.OfflineAgentsManualDefaultDays <= 0 {
		return e.BadRequestError("manual default days must be greater than 0", nil)
	}

	settings, err := h.updateRetentionSettings(input)
	if err != nil {
		return err
	}
	return e.JSON(http.StatusOK, purgeSettingsResponse(settings))
}

func (h *Hub) runPurge(e *core.RequestEvent) error {
	var input purgeRunInput
	if err := e.BindBody(&input); err != nil {
		return e.BadRequestError("Invalid request body", err)
	}

	var (
		deleted int
		err     error
	)

	switch input.Scope {
	case "monitor_events":
		switch input.Mode {
		case "older_than_days":
			deleted, err = h.purgeMonitorEventsOlderThan(input.Days)
		case "all":
			deleted, err = h.purgeAllMonitorEvents()
		default:
			return e.BadRequestError("Invalid purge mode", nil)
		}
	case "notification_logs":
		switch input.Mode {
		case "older_than_days":
			deleted, err = h.purgeNotificationLogsOlderThan(input.Days)
		case "all":
			deleted, err = h.purgeAllNotificationLogs()
		default:
			return e.BadRequestError("Invalid purge mode", nil)
		}
	case "offline_agents":
		switch input.Mode {
		case "older_than_days":
			deleted, err = h.purgeOfflineAgentsOlderThan(input.Days)
		case "all":
			deleted, err = h.purgeAllOfflineAgents()
		default:
			return e.BadRequestError("Invalid purge mode", nil)
		}
	default:
		return e.BadRequestError("Invalid purge scope", nil)
	}

	if err != nil {
		return err
	}

	return e.JSON(http.StatusOK, purgeRunResponse{
		Scope:        input.Scope,
		Mode:         input.Mode,
		DeletedCount: deleted,
	})
}
