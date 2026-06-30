//go:build testing

package hub

import (
	"testing"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/hub/notifications"
	"github.com/pocketbase/pocketbase/core"
	"github.com/stretchr/testify/require"
)

func mustTime(t *testing.T, layout, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(layout, value)
	require.NoError(t, err)
	return parsed
}

func TestIsMaintenanceWindowActive_Single(t *testing.T) {
	start := mustTime(t, time.RFC3339, "2026-07-01T02:00:00Z")
	end := mustTime(t, time.RFC3339, "2026-07-01T04:00:00Z")
	base := maintenanceSpec{Enabled: true, Strategy: "single", StartAt: start, EndAt: end}

	cases := []struct {
		name string
		spec maintenanceSpec
		now  time.Time
		want bool
	}{
		{"before window", base, mustTime(t, time.RFC3339, "2026-07-01T01:59:00Z"), false},
		{"at start", base, start, true},
		{"inside", base, mustTime(t, time.RFC3339, "2026-07-01T03:00:00Z"), true},
		{"at end", base, end, true},
		{"after window", base, mustTime(t, time.RFC3339, "2026-07-01T04:01:00Z"), false},
		{"disabled", maintenanceSpec{Enabled: false, Strategy: "single", StartAt: start, EndAt: end}, start, false},
		{"missing bounds", maintenanceSpec{Enabled: true, Strategy: "single"}, start, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, isMaintenanceWindowActive(tc.spec, tc.now))
		})
	}
}

func TestIsMaintenanceWindowActive_RecurringTZ(t *testing.T) {
	// Daily 02:00–04:00 in Europe/Paris (CEST = UTC+2 in July).
	spec := maintenanceSpec{
		Enabled:   true,
		Strategy:  "recurring",
		StartTime: "02:00",
		EndTime:   "04:00",
		Timezone:  "Europe/Paris",
	}
	cases := []struct {
		name string
		now  time.Time // UTC instant
		want bool
	}{
		// 03:00 Paris = 01:00 UTC → inside
		{"inside via tz", mustTime(t, time.RFC3339, "2026-07-01T01:00:00Z"), true},
		// 01:00 UTC is 03:00 Paris (inside); 02:30 UTC is 04:30 Paris → outside
		{"after via tz", mustTime(t, time.RFC3339, "2026-07-01T02:30:00Z"), false},
		// 23:30 UTC = 01:30 Paris next day → outside (before 02:00)
		{"before window paris", mustTime(t, time.RFC3339, "2026-06-30T23:30:00Z"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, isMaintenanceWindowActive(spec, tc.now))
		})
	}
}

func TestIsMaintenanceWindowActive_Weekday(t *testing.T) {
	// 2026-07-01 is a Wednesday (weekday 3). Window only on Wednesdays, 00:00–23:59 UTC.
	spec := maintenanceSpec{
		Enabled:   true,
		Strategy:  "recurring",
		StartTime: "00:00",
		EndTime:   "23:59",
		Weekdays:  []int{3},
		Timezone:  "UTC",
	}
	require.True(t, isMaintenanceWindowActive(spec, mustTime(t, time.RFC3339, "2026-07-01T12:00:00Z")))
	// 2026-07-02 is Thursday → not allowed.
	require.False(t, isMaintenanceWindowActive(spec, mustTime(t, time.RFC3339, "2026-07-02T12:00:00Z")))
}

func TestIsMaintenanceWindowActive_MidnightCrossing(t *testing.T) {
	// 23:00 → 02:00 UTC, only when it STARTS on a Wednesday (weekday 3).
	spec := maintenanceSpec{
		Enabled:   true,
		Strategy:  "recurring",
		StartTime: "23:00",
		EndTime:   "02:00",
		Weekdays:  []int{3},
		Timezone:  "UTC",
	}
	// Wed 23:30 → evening portion, allowed.
	require.True(t, isMaintenanceWindowActive(spec, mustTime(t, time.RFC3339, "2026-07-01T23:30:00Z")))
	// Thu 01:00 → early-morning portion belongs to Wed's window, allowed.
	require.True(t, isMaintenanceWindowActive(spec, mustTime(t, time.RFC3339, "2026-07-02T01:00:00Z")))
	// Thu 23:30 → evening portion belongs to Thu (not allowed).
	require.False(t, isMaintenanceWindowActive(spec, mustTime(t, time.RFC3339, "2026-07-02T23:30:00Z")))
	// Wed 12:00 → outside the time range entirely.
	require.False(t, isMaintenanceWindowActive(spec, mustTime(t, time.RFC3339, "2026-07-01T12:00:00Z")))
}

func TestIsMaintenanceWindowActive_DateBounds(t *testing.T) {
	spec := maintenanceSpec{
		Enabled:    true,
		Strategy:   "recurring",
		StartTime:  "00:00",
		EndTime:    "23:59",
		Timezone:   "UTC",
		ActiveFrom: mustTime(t, time.RFC3339, "2026-07-01T00:00:00Z"),
		ActiveTo:   mustTime(t, time.RFC3339, "2026-07-03T00:00:00Z"),
	}
	require.False(t, isMaintenanceWindowActive(spec, mustTime(t, time.RFC3339, "2026-06-30T12:00:00Z")))
	require.True(t, isMaintenanceWindowActive(spec, mustTime(t, time.RFC3339, "2026-07-02T12:00:00Z")))
	require.True(t, isMaintenanceWindowActive(spec, mustTime(t, time.RFC3339, "2026-07-03T12:00:00Z")))
	require.False(t, isMaintenanceWindowActive(spec, mustTime(t, time.RFC3339, "2026-07-04T12:00:00Z")))
}

// Date bounds are calendar dates the admin picked (stored as midnight UTC); they must not
// shift a day for a non-UTC zone, and a midnight-crossing tail must be attributed to the
// occurrence's start date for the bound check (regression for the review findings).
func TestIsMaintenanceWindowActive_DateBoundsTZ(t *testing.T) {
	// Daily 00:00–23:59 in America/New_York (UTC-4 in July), active through 2026-07-03.
	spec := maintenanceSpec{
		Enabled:   true,
		Strategy:  "recurring",
		StartTime: "00:00",
		EndTime:   "23:59",
		Timezone:  "America/New_York",
		ActiveTo:  mustTime(t, time.RFC3339, "2026-07-03T00:00:00Z"),
	}
	// 2026-07-03 18:00 New York (= 22:00Z) is the last covered day → active.
	require.True(t, isMaintenanceWindowActive(spec, mustTime(t, time.RFC3339, "2026-07-03T22:00:00Z")))
	// 2026-07-04 12:00 New York is past the bound → inactive.
	require.False(t, isMaintenanceWindowActive(spec, mustTime(t, time.RFC3339, "2026-07-04T16:00:00Z")))

	// Midnight-crossing 23:00→02:00 UTC, active through 2026-07-03: the 01:00 tail on
	// 2026-07-04 belongs to the 2026-07-03 occurrence, so it stays within the bound.
	crossing := maintenanceSpec{
		Enabled:   true,
		Strategy:  "recurring",
		StartTime: "23:00",
		EndTime:   "02:00",
		Timezone:  "UTC",
		ActiveTo:  mustTime(t, time.RFC3339, "2026-07-03T00:00:00Z"),
	}
	require.True(t, isMaintenanceWindowActive(crossing, mustTime(t, time.RFC3339, "2026-07-04T01:00:00Z")))
	// The 2026-07-04 evening start is past the bound → inactive.
	require.False(t, isMaintenanceWindowActive(crossing, mustTime(t, time.RFC3339, "2026-07-04T23:30:00Z")))
}

func TestMaintenanceCoversEvent(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	newWindow := func(scope map[string]any) *core.Record {
		col, err := hub.FindCachedCollectionByNameOrId(maintenanceCollection)
		require.NoError(t, err)
		rec := core.NewRecord(col)
		rec.Set("scope", scope)
		return rec
	}

	monEvt := notifications.Event{Resource: notifications.ResourceRef{Type: "monitor", ID: "mon1"}}
	agentEvt := notifications.Event{Resource: notifications.ResourceRef{Type: "agent", ID: "agentA"}}
	containerEvt := notifications.Event{
		Resource: notifications.ResourceRef{Type: "container_image", ID: "agentA|cid"},
		Details:  map[string]any{"agent_id": "agentA", "container_name": "nginx"},
	}

	// Global scope covers everything.
	global := newWindow(map[string]any{})
	require.True(t, maintenanceCoversEvent(global, monEvt))
	require.True(t, maintenanceCoversEvent(global, agentEvt))
	require.True(t, maintenanceCoversEvent(global, containerEvt))

	// Targeted to agentA: covers the agent and its containers, not an unrelated monitor.
	scoped := newWindow(map[string]any{"agent_ids": []string{"agentA"}})
	require.True(t, maintenanceCoversEvent(scoped, agentEvt))
	require.True(t, maintenanceCoversEvent(scoped, containerEvt))
	require.False(t, maintenanceCoversEvent(scoped, monEvt))

	// Targeted to a specific monitor only.
	monScoped := newWindow(map[string]any{"monitor_ids": []string{"mon1"}})
	require.True(t, maintenanceCoversEvent(monScoped, monEvt))
	require.False(t, maintenanceCoversEvent(monScoped, agentEvt))
}

func TestValidateMaintenancePayload(t *testing.T) {
	cases := []struct {
		name    string
		body    maintenancePayload
		wantErr bool
	}{
		{"valid single", maintenancePayload{Title: "x", Strategy: "single", StartAt: "2026-07-01T02:00:00Z", EndAt: "2026-07-01T04:00:00Z"}, false},
		{"single end before start", maintenancePayload{Title: "x", Strategy: "single", StartAt: "2026-07-01T04:00:00Z", EndAt: "2026-07-01T02:00:00Z"}, true},
		{"single missing end", maintenancePayload{Title: "x", Strategy: "single", StartAt: "2026-07-01T02:00:00Z"}, true},
		{"missing title", maintenancePayload{Strategy: "single", StartAt: "2026-07-01T02:00:00Z", EndAt: "2026-07-01T04:00:00Z"}, true},
		{"valid recurring", maintenancePayload{Title: "x", Strategy: "recurring", StartTime: "02:00", EndTime: "04:00", Timezone: "Europe/Paris", Weekdays: []int{1, 3}}, false},
		{"recurring bad time", maintenancePayload{Title: "x", Strategy: "recurring", StartTime: "25:00", EndTime: "04:00", Timezone: "UTC"}, true},
		{"recurring equal times", maintenancePayload{Title: "x", Strategy: "recurring", StartTime: "02:00", EndTime: "02:00", Timezone: "UTC"}, true},
		{"recurring no tz", maintenancePayload{Title: "x", Strategy: "recurring", StartTime: "02:00", EndTime: "04:00"}, true},
		{"recurring bad tz", maintenancePayload{Title: "x", Strategy: "recurring", StartTime: "02:00", EndTime: "04:00", Timezone: "Mars/Olympus"}, true},
		{"recurring bad weekday", maintenancePayload{Title: "x", Strategy: "recurring", StartTime: "02:00", EndTime: "04:00", Timezone: "UTC", Weekdays: []int{9}}, true},
		{"bad strategy", maintenancePayload{Title: "x", Strategy: "weekly"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := tc.body
			err := validateMaintenancePayload(&body)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestUnderMaintenanceSuppresses verifies an active window suppresses a covered event at
// the shared chokepoint, and leaves uncovered/inactive events alone.
func TestUnderMaintenanceSuppresses(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	now := time.Now().UTC()
	// An active global single window spanning now.
	_, err = createTestRecord(hub, maintenanceCollection, map[string]any{
		"title":    "active global",
		"enabled":  true,
		"strategy": "single",
		"start_at": now.Add(-time.Hour).Format(time.RFC3339),
		"end_at":   now.Add(time.Hour).Format(time.RFC3339),
		"scope":    map[string]any{},
	})
	require.NoError(t, err)

	monEvt := notifications.Event{Resource: notifications.ResourceRef{Type: "monitor", ID: "mon1"}}
	require.True(t, hub.underMaintenance(monEvt, now))
	require.True(t, hub.isNotificationSuppressed(monEvt))

	// A disabled window does not suppress.
	hub2, testApp2, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub2, testApp2)
	_, err = createTestRecord(hub2, maintenanceCollection, map[string]any{
		"title":    "disabled",
		"enabled":  false,
		"strategy": "single",
		"start_at": now.Add(-time.Hour).Format(time.RFC3339),
		"end_at":   now.Add(time.Hour).Format(time.RFC3339),
		"scope":    map[string]any{},
	})
	require.NoError(t, err)
	require.False(t, hub2.underMaintenance(monEvt, now))
}
