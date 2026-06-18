package migrations

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Adds a composite index on monitor_events(monitor, checked_at). Every monitor read path
// filters "monitor = X AND checked_at >= Y" (and orders by checked_at), but the table only
// had two separate single-column indexes, so SQLite had to scan a monitor's whole history
// per query. The composite index turns those into bounded range scans / index-ordered
// top-N lookups. The standalone (monitor) index becomes redundant — the composite covers
// monitor-only lookups and the relation cascade-delete — so it is removed. The
// (checked_at) index is kept for the retention purge (DELETE ... WHERE checked_at < cutoff).
func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("monitor_events")
		if err != nil {
			return nil
		}

		data, err := collection.MarshalJSON()
		if err != nil {
			return err
		}
		var snapshot map[string]any
		if err := json.Unmarshal(data, &snapshot); err != nil {
			return err
		}

		const compositeIndex = "CREATE INDEX `idx_monitor_events_monitor_checked_at` ON `monitor_events` (`monitor`, `checked_at`)"

		rawIndexes, _ := snapshot["indexes"].([]any)
		next := make([]any, 0, len(rawIndexes)+1)
		hasComposite := false
		for _, raw := range rawIndexes {
			stmt, _ := raw.(string)
			if strings.Contains(stmt, "idx_monitor_events_monitor_checked_at") {
				hasComposite = true
			}
			// Drop the now-redundant single-column (monitor) index. The backticks pin the
			// exact name so the composite index (idx_monitor_events_monitor_checked_at) is
			// not matched here.
			if strings.Contains(stmt, "`idx_monitor_events_monitor`") {
				continue
			}
			next = append(next, raw)
		}
		if !hasComposite {
			next = append(next, compositeIndex)
		}
		snapshot["indexes"] = next

		updated, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		if err := collection.UnmarshalJSON(updated); err != nil {
			return err
		}
		return app.Save(collection)
	}, nil)
}
