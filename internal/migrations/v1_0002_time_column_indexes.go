package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Indexes on the time column of the append-only tables purged by age, which only had
// (agent|rule|..., time) indexes: the retention DELETEs (and the fleet metrics range) scanned
// the whole table. monitor_events and system_notifications already have theirs.
func init() {
	m.Register(func(app core.App) error {
		for _, idx := range []struct{ collection, name, column string }{
			{"host_metric_samples", "idx_host_metric_samples_collected_at", "collected_at"},
			{"container_metric_samples", "idx_container_metric_samples_collected_at", "collected_at"},
			{"notification_logs", "idx_notification_logs_sent_at", "sent_at"},
		} {
			collection, err := app.FindCollectionByNameOrId(idx.collection)
			if err != nil {
				return err
			}
			collection.AddIndex(idx.name, false, "`"+idx.column+"`", "")
			if err := app.Save(collection); err != nil {
				return err
			}
		}
		return nil
	}, nil)
}
