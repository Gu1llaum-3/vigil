package hub

import (
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/hub/notifications"
	"github.com/pocketbase/pocketbase/core"
)

const maintenanceCollection = "maintenance"

// maintenanceScope mirrors notification_rules.filter: empty = global, otherwise the
// window only covers the listed monitors/agents.
type maintenanceScope struct {
	MonitorIDs []string `json:"monitor_ids"`
	AgentIDs   []string `json:"agent_ids"`
}

// maintenanceSpec is the time-relevant subset of a maintenance record, extracted so the
// active-window computation is a pure function — unit-testable without the database.
type maintenanceSpec struct {
	Enabled    bool
	Strategy   string // "single" | "recurring"
	StartAt    time.Time
	EndAt      time.Time
	StartTime  string // "HH:MM", local to Timezone
	EndTime    string // "HH:MM", local to Timezone
	Weekdays   []int  // 0=Sunday … 6=Saturday; empty = every day
	ActiveFrom time.Time
	ActiveTo   time.Time
	Timezone   string
}

// isMaintenanceWindowActive reports whether the window is active at now. Single windows
// use absolute start/end instants; recurring windows match a local time-of-day range
// (in the window's timezone), optional weekday set, and optional calendar date bounds.
func isMaintenanceWindowActive(s maintenanceSpec, now time.Time) bool {
	if !s.Enabled {
		return false
	}
	switch s.Strategy {
	case "single":
		if s.StartAt.IsZero() || s.EndAt.IsZero() {
			return false
		}
		return !now.Before(s.StartAt) && !now.After(s.EndAt)
	case "recurring":
		return recurringWindowActive(s, now)
	default:
		return false
	}
}

func recurringWindowActive(s maintenanceSpec, now time.Time) bool {
	startMin, ok1 := parseHHMM(s.StartTime)
	endMin, ok2 := parseHHMM(s.EndTime)
	if !ok1 || !ok2 || startMin == endMin {
		return false
	}

	loc := loadLocationOrUTC(s.Timezone)
	nowLocal := now.In(loc)
	nowMin := nowLocal.Hour()*60 + nowLocal.Minute()

	// Determine whether now is inside the time-of-day window and, if so, the local date on
	// which the current occurrence STARTED. Weekday gating and the calendar-date bounds are
	// both checked against that start date, so a midnight-crossing window's early-morning
	// tail is attributed to the day it began on (consistent on all three checks).
	var occurrence time.Time
	switch {
	case startMin < endMin:
		// Same-day window.
		if nowMin < startMin || nowMin >= endMin {
			return false
		}
		occurrence = nowLocal
	case nowMin >= startMin:
		// Crossing midnight, evening portion → started today.
		occurrence = nowLocal
	case nowMin < endMin:
		// Crossing midnight, early-morning tail → started the previous day.
		occurrence = nowLocal.AddDate(0, 0, -1)
	default:
		return false
	}

	if !weekdayAllowed(s.Weekdays, occurrence.Weekday()) {
		return false
	}
	// active_from/active_to are calendar dates the admin picked, stored as midnight UTC, so
	// their UTC date IS that calendar date — compare it directly (no .In(loc), which would
	// shift the day for non-UTC zones) against the occurrence's local start date.
	occDate := dateOnly(occurrence)
	if !s.ActiveFrom.IsZero() && occDate < dateOnly(s.ActiveFrom.UTC()) {
		return false
	}
	if !s.ActiveTo.IsZero() && occDate > dateOnly(s.ActiveTo.UTC()) {
		return false
	}
	return true
}

// maintenanceOccurrence is a concrete [Start, End] interval of a window — what the chart
// bands are drawn from. Unlike isMaintenanceWindowActive (a point-in-time check), this
// materializes the actual intervals overlapping a range.
type maintenanceOccurrence struct {
	Start time.Time
	End   time.Time
}

// maintenanceOccurrences returns the concrete intervals of a window that intersect
// [since, until]. Single windows yield their absolute interval (if it overlaps); recurring
// windows enumerate each daily occurrence (honoring weekday set, calendar bounds, and
// midnight crossing) that overlaps the range. Pure — unit-testable without the database.
func maintenanceOccurrences(s maintenanceSpec, since, until time.Time) []maintenanceOccurrence {
	if !s.Enabled || since.After(until) {
		return nil
	}
	switch s.Strategy {
	case "single":
		if s.StartAt.IsZero() || s.EndAt.IsZero() || !intervalOverlaps(s.StartAt, s.EndAt, since, until) {
			return nil
		}
		return []maintenanceOccurrence{{Start: s.StartAt, End: s.EndAt}}
	case "recurring":
		return recurringOccurrences(s, since, until)
	default:
		return nil
	}
}

// intervalOverlaps reports whether [start, end] and [since, until] overlap with positive
// width: a window that merely touches the range at an edge (end == since, or start == until)
// is excluded so it doesn't render as a degenerate zero-width band.
func intervalOverlaps(start, end, since, until time.Time) bool {
	return end.After(since) && start.Before(until)
}

// Known limitation: on the once-a-year DST spring-forward day, a start_time falling
// strictly inside the skipped hour (e.g. 02:30 where 02:00–03:00 is skipped) is normalized
// forward by time.Date, so the band edge can sit up to an hour off from the instant
// recurringWindowActive/notification-suppression considers active. On-the-hour starts agree.
func recurringOccurrences(s maintenanceSpec, since, until time.Time) []maintenanceOccurrence {
	startMin, ok1 := parseHHMM(s.StartTime)
	endMin, ok2 := parseHHMM(s.EndTime)
	if !ok1 || !ok2 || startMin == endMin {
		return nil
	}
	loc := loadLocationOrUTC(s.Timezone)
	sinceLocal := since.In(loc)
	untilLocal := until.In(loc)
	// Candidate start-dates run from the day BEFORE `since` (to catch a midnight-crossing
	// occurrence that began earlier and still overlaps) through `until`, in the window's tz.
	day := time.Date(sinceLocal.Year(), sinceLocal.Month(), sinceLocal.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -1)
	lastDay := time.Date(untilLocal.Year(), untilLocal.Month(), untilLocal.Day(), 0, 0, 0, 0, loc)

	var out []maintenanceOccurrence
	for !day.After(lastDay) {
		d := day
		day = day.AddDate(0, 0, 1)

		// Weekday and calendar bounds are checked against the occurrence's START date, exactly
		// as recurringWindowActive attributes a midnight-crossing window to the day it began on.
		if !weekdayAllowed(s.Weekdays, d.Weekday()) {
			continue
		}
		occDate := dateOnly(d)
		if !s.ActiveFrom.IsZero() && occDate < dateOnly(s.ActiveFrom.UTC()) {
			continue
		}
		if !s.ActiveTo.IsZero() && occDate > dateOnly(s.ActiveTo.UTC()) {
			continue
		}

		start := time.Date(d.Year(), d.Month(), d.Day(), startMin/60, startMin%60, 0, 0, loc)
		endDay := d
		if startMin >= endMin {
			endDay = d.AddDate(0, 0, 1) // crosses midnight → ends next day
		}
		end := time.Date(endDay.Year(), endDay.Month(), endDay.Day(), endMin/60, endMin%60, 0, 0, loc)

		if !intervalOverlaps(start, end, since, until) {
			continue
		}
		out = append(out, maintenanceOccurrence{Start: start, End: end})
	}
	return out
}

// maintenanceScopeCoversMonitor / …Agent report whether a window's scope covers a resource
// referenced directly by id (used by the per-resource occurrence endpoints). An empty scope
// is global; otherwise only the listed ids of the matching kind are covered.
func maintenanceScopeCoversMonitor(scope maintenanceScope, monitorID string) bool {
	if len(scope.MonitorIDs) == 0 && len(scope.AgentIDs) == 0 {
		return true
	}
	return containsString(scope.MonitorIDs, monitorID)
}

func maintenanceScopeCoversAgent(scope maintenanceScope, agentID string) bool {
	if len(scope.MonitorIDs) == 0 && len(scope.AgentIDs) == 0 {
		return true
	}
	return containsString(scope.AgentIDs, agentID)
}

// loadLocationOrUTC resolves an IANA timezone, falling back to UTC (with a warning) when
// the zone is unknown — e.g. a window saved against a zone the deployment's tzdata later
// drops. The API validates the zone at write time, so this only bites on environment drift.
func loadLocationOrUTC(name string) *time.Location {
	if name == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		slog.Warn("maintenance: unknown timezone, evaluating in UTC", "timezone", name, "err", err)
		return time.UTC
	}
	return loc
}

func parseHHMM(s string) (int, bool) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 2 {
		return 0, false
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

func weekdayAllowed(weekdays []int, wd time.Weekday) bool {
	if len(weekdays) == 0 {
		return true
	}
	for _, d := range weekdays {
		if d == int(wd) {
			return true
		}
	}
	return false
}

// dateOnly collapses a time to a comparable yyyymmdd integer in its own location.
func dateOnly(t time.Time) int {
	y, m, d := t.Date()
	return y*10000 + int(m)*100 + d
}

func specFromRecord(rec *core.Record) maintenanceSpec {
	var weekdays []int
	_ = rec.UnmarshalJSONField("weekdays", &weekdays)
	return maintenanceSpec{
		Enabled:    rec.GetBool("enabled"),
		Strategy:   rec.GetString("strategy"),
		StartAt:    rec.GetDateTime("start_at").Time(),
		EndAt:      rec.GetDateTime("end_at").Time(),
		StartTime:  rec.GetString("start_time"),
		EndTime:    rec.GetString("end_time"),
		Weekdays:   weekdays,
		ActiveFrom: rec.GetDateTime("active_from").Time(),
		ActiveTo:   rec.GetDateTime("active_to").Time(),
		Timezone:   rec.GetString("timezone"),
	}
}

// activeMaintenances returns the enabled maintenance windows that are active at now.
func (h *Hub) activeMaintenances(now time.Time) ([]*core.Record, error) {
	records, err := h.FindRecordsByFilter(maintenanceCollection, "enabled = true", "", 0, 0)
	if err != nil {
		return nil, err
	}
	active := make([]*core.Record, 0, len(records))
	for _, rec := range records {
		if isMaintenanceWindowActive(specFromRecord(rec), now) {
			active = append(active, rec)
		}
	}
	return active, nil
}

// maintenanceCoversEvent reports whether a window's scope covers the event's resource.
// An empty scope is global. A container_image event is covered by its parent agent's id.
func maintenanceCoversEvent(rec *core.Record, evt notifications.Event) bool {
	var scope maintenanceScope
	_ = rec.UnmarshalJSONField("scope", &scope)
	if len(scope.MonitorIDs) == 0 && len(scope.AgentIDs) == 0 {
		return true
	}
	switch evt.Resource.Type {
	case "monitor":
		return containsString(scope.MonitorIDs, evt.Resource.ID)
	case "agent":
		return containsString(scope.AgentIDs, evt.Resource.ID)
	case "container_image":
		agentID := parentAgentID(evt)
		return agentID != "" && containsString(scope.AgentIDs, agentID)
	}
	return false
}

// underMaintenance reports whether the event's resource is currently inside an active
// maintenance window. Fails open (logs, does not suppress) on a DB error — the same
// principle as mute suppression: never silently swallow an alert.
func (h *Hub) underMaintenance(evt notifications.Event, now time.Time) bool {
	active, err := h.activeMaintenances(now)
	if err != nil {
		slog.Warn("maintenance lookup failed; not suppressing", "err", err)
		return false
	}
	for _, rec := range active {
		if maintenanceCoversEvent(rec, evt) {
			return true
		}
	}
	return false
}

// windowEndsAt returns the instant the current occurrence of the window ends, used by the
// banner to show a countdown. Single windows return EndAt; recurring windows resolve the
// end of the occurrence covering now (handling the midnight-crossing case). Zero if N/A.
func windowEndsAt(s maintenanceSpec, now time.Time) time.Time {
	if s.Strategy == "single" {
		return s.EndAt
	}
	if s.Strategy != "recurring" {
		return time.Time{}
	}
	startMin, ok1 := parseHHMM(s.StartTime)
	endMin, ok2 := parseHHMM(s.EndTime)
	if !ok1 || !ok2 {
		return time.Time{}
	}
	loc := loadLocationOrUTC(s.Timezone)
	nowLocal := now.In(loc)
	endToday := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), endMin/60, endMin%60, 0, 0, loc)
	if startMin < endMin {
		return endToday
	}
	// Crossing midnight: the evening portion ends tomorrow, the early-morning portion today.
	if nowLocal.Hour()*60+nowLocal.Minute() >= startMin {
		return endToday.AddDate(0, 0, 1)
	}
	return endToday
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
