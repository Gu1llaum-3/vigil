package migrations

import (
	"slices"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// The in-app notification feed (the navbar bell) gets a retention window, purged by the
// daily retention job: data_retention_settings.system_notifications_retention_days (empty
// on existing hubs, which then use the default), and an index on occurred_at for the purge
// and for the newest-first history across categories. Also adds host_metrics to the
// system_notifications category values (metric alerts write it; the select never listed it).
func init() {
	m.Register(func(app core.App) error {
		settings, err := app.FindCollectionByNameOrId("data_retention_settings")
		if err != nil {
			return err
		}
		if settings.Fields.GetByName("system_notifications_retention_days") == nil {
			settings.Fields.Add(&core.NumberField{Id: "number6400000006", Name: "system_notifications_retention_days", Min: new(float64(1))})
		}
		if err := app.Save(settings); err != nil {
			return err
		}

		feed, err := app.FindCollectionByNameOrId("system_notifications")
		if err != nil {
			return err
		}
		feed.AddIndex("idx_system_notifications_occurred", false, "`occurred_at`", "")
		if category, ok := feed.Fields.GetByName("category").(*core.SelectField); ok && !slices.Contains(category.Values, "host_metrics") {
			category.Values = append(category.Values, "host_metrics")
		}
		return app.Save(feed)
	}, nil)
}
