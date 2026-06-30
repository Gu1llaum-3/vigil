package hub

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type maintenancePayload struct {
	ID          string           `json:"id"`
	Title       string           `json:"title"`
	Description string           `json:"description"`
	Enabled     bool             `json:"enabled"`
	Severity    string           `json:"severity"`
	Strategy    string           `json:"strategy"`
	StartAt     string           `json:"start_at"`
	EndAt       string           `json:"end_at"`
	StartTime   string           `json:"start_time"`
	EndTime     string           `json:"end_time"`
	Weekdays    []int            `json:"weekdays"`
	ActiveFrom  string           `json:"active_from"`
	ActiveTo    string           `json:"active_to"`
	Timezone    string           `json:"timezone"`
	Scope       maintenanceScope `json:"scope"`
	Created     string           `json:"created"`
	Updated     string           `json:"updated"`
}

// activeMaintenancePayload is the slimmed shape served to all authenticated users for
// the banner — definition internals (scope, schedule) are not exposed here.
type activeMaintenancePayload struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Severity    string `json:"severity"`
	EndsAt      string `json:"ends_at,omitempty"`
}

func maintenanceResponse(rec *core.Record) maintenancePayload {
	var weekdays []int
	_ = rec.UnmarshalJSONField("weekdays", &weekdays)
	var scope maintenanceScope
	_ = rec.UnmarshalJSONField("scope", &scope)
	return maintenancePayload{
		ID:          rec.Id,
		Title:       rec.GetString("title"),
		Description: rec.GetString("description"),
		Enabled:     rec.GetBool("enabled"),
		Severity:    rec.GetString("severity"),
		Strategy:    rec.GetString("strategy"),
		StartAt:     formatRecordDateTime(rec, "start_at"),
		EndAt:       formatRecordDateTime(rec, "end_at"),
		StartTime:   rec.GetString("start_time"),
		EndTime:     rec.GetString("end_time"),
		Weekdays:    weekdays,
		ActiveFrom:  formatRecordDateTime(rec, "active_from"),
		ActiveTo:    formatRecordDateTime(rec, "active_to"),
		Timezone:    rec.GetString("timezone"),
		Scope:       scope,
		Created:     rec.GetString("created"),
		Updated:     rec.GetString("updated"),
	}
}

var maintenanceSeverities = map[string]bool{"info": true, "warning": true, "critical": true}

// parseMaintenanceDate accepts RFC3339 or a bare date; "" means unset.
func parseMaintenanceDate(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t.UTC().Format(time.RFC3339), nil
	}
	if t, err := time.Parse("2006-01-02", value); err == nil {
		return t.UTC().Format(time.RFC3339), nil
	}
	return "", errors.New("invalid date (expected RFC3339 or YYYY-MM-DD)")
}

func validateMaintenancePayload(body *maintenancePayload) error {
	body.Title = strings.TrimSpace(body.Title)
	if body.Title == "" {
		return errors.New("title is required")
	}
	if body.Severity == "" {
		body.Severity = "info"
	}
	if !maintenanceSeverities[body.Severity] {
		return errors.New("invalid severity")
	}

	switch body.Strategy {
	case "single":
		start, err := parseMaintenanceDate(body.StartAt)
		if err != nil || start == "" {
			return errors.New("a one-time window needs a valid start")
		}
		end, err := parseMaintenanceDate(body.EndAt)
		if err != nil || end == "" {
			return errors.New("a one-time window needs a valid end")
		}
		if !(end > start) {
			return errors.New("end must be after start")
		}
		body.StartAt, body.EndAt = start, end
	case "recurring":
		if _, ok := parseHHMM(body.StartTime); !ok {
			return errors.New("invalid start time (expected HH:MM)")
		}
		if _, ok := parseHHMM(body.EndTime); !ok {
			return errors.New("invalid end time (expected HH:MM)")
		}
		if body.StartTime == body.EndTime {
			return errors.New("start and end time must differ")
		}
		if body.Timezone == "" {
			return errors.New("a recurring window needs a timezone")
		}
		if _, err := time.LoadLocation(body.Timezone); err != nil {
			return errors.New("invalid timezone")
		}
		for _, d := range body.Weekdays {
			if d < 0 || d > 6 {
				return errors.New("weekdays must be 0–6")
			}
		}
		from, err := parseMaintenanceDate(body.ActiveFrom)
		if err != nil {
			return errors.New("invalid active-from date")
		}
		to, err := parseMaintenanceDate(body.ActiveTo)
		if err != nil {
			return errors.New("invalid active-to date")
		}
		if from != "" && to != "" && to < from {
			return errors.New("active-to must not be before active-from")
		}
		body.ActiveFrom, body.ActiveTo = from, to
	default:
		return errors.New("strategy must be 'single' or 'recurring'")
	}
	return nil
}

// applyMaintenancePayload writes a validated payload onto a record, clearing the fields
// that do not apply to the chosen strategy so a flip between single/recurring can't leave
// stale schedule data behind.
func applyMaintenancePayload(rec *core.Record, body maintenancePayload) {
	rec.Set("title", body.Title)
	rec.Set("description", body.Description)
	rec.Set("enabled", body.Enabled)
	rec.Set("severity", body.Severity)
	rec.Set("strategy", body.Strategy)
	rec.Set("scope", body.Scope)

	if body.Strategy == "single" {
		rec.Set("start_at", body.StartAt)
		rec.Set("end_at", body.EndAt)
		rec.Set("start_time", "")
		rec.Set("end_time", "")
		rec.Set("weekdays", []int{})
		rec.Set("active_from", "")
		rec.Set("active_to", "")
		rec.Set("timezone", "")
		return
	}
	// recurring
	rec.Set("start_time", body.StartTime)
	rec.Set("end_time", body.EndTime)
	rec.Set("weekdays", body.Weekdays)
	rec.Set("active_from", body.ActiveFrom)
	rec.Set("active_to", body.ActiveTo)
	rec.Set("timezone", body.Timezone)
	rec.Set("start_at", "")
	rec.Set("end_at", "")
}

func (h *Hub) listMaintenanceWindows(e *core.RequestEvent) error {
	records, err := h.FindAllRecords(maintenanceCollection)
	if err != nil {
		return err
	}
	out := make([]maintenancePayload, 0, len(records))
	for _, rec := range records {
		out = append(out, maintenanceResponse(rec))
	}
	return e.JSON(http.StatusOK, out)
}

func (h *Hub) createMaintenanceWindow(e *core.RequestEvent) error {
	var body maintenancePayload
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("Invalid request body", err)
	}
	if err := validateMaintenancePayload(&body); err != nil {
		return e.BadRequestError(err.Error(), nil)
	}
	collection, err := h.FindCachedCollectionByNameOrId(maintenanceCollection)
	if err != nil {
		return err
	}
	rec := core.NewRecord(collection)
	applyMaintenancePayload(rec, body)
	if e.Auth != nil {
		rec.Set("created_by", e.Auth.Id)
	}
	if err := h.Save(rec); err != nil {
		return e.BadRequestError("Failed to save maintenance window", err)
	}
	return e.JSON(http.StatusOK, maintenanceResponse(rec))
}

func (h *Hub) updateMaintenanceWindow(e *core.RequestEvent) error {
	id := e.Request.PathValue("id")
	rec, err := h.FindFirstRecordByFilter(maintenanceCollection, "id = {:id}", dbx.Params{"id": id})
	if err != nil {
		return e.NotFoundError("Maintenance window not found", err)
	}
	var body maintenancePayload
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("Invalid request body", err)
	}
	if err := validateMaintenancePayload(&body); err != nil {
		return e.BadRequestError(err.Error(), nil)
	}
	applyMaintenancePayload(rec, body)
	if err := h.Save(rec); err != nil {
		return e.BadRequestError("Failed to save maintenance window", err)
	}
	return e.JSON(http.StatusOK, maintenanceResponse(rec))
}

func (h *Hub) deleteMaintenanceWindow(e *core.RequestEvent) error {
	id := e.Request.PathValue("id")
	rec, err := h.FindFirstRecordByFilter(maintenanceCollection, "id = {:id}", dbx.Params{"id": id})
	if err != nil {
		return e.NotFoundError("Maintenance window not found", err)
	}
	if err := h.Delete(rec); err != nil {
		return err
	}
	return e.JSON(http.StatusOK, map[string]any{"ok": true})
}

// getActiveMaintenance returns the currently-active windows for the banner. Available to
// all authenticated users (read-only, no scope/schedule internals).
func (h *Hub) getActiveMaintenance(e *core.RequestEvent) error {
	now := time.Now()
	active, err := h.activeMaintenances(now)
	if err != nil {
		return err
	}
	out := make([]activeMaintenancePayload, 0, len(active))
	for _, rec := range active {
		endsAt := ""
		if ends := windowEndsAt(specFromRecord(rec), now); !ends.IsZero() {
			endsAt = ends.UTC().Format(time.RFC3339)
		}
		out = append(out, activeMaintenancePayload{
			ID:          rec.Id,
			Title:       rec.GetString("title"),
			Description: rec.GetString("description"),
			Severity:    firstNonEmpty(rec.GetString("severity"), "info"),
			EndsAt:      endsAt,
		})
	}
	return e.JSON(http.StatusOK, out)
}
