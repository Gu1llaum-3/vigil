package migrations

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Deletes any existing loadavg metric_alerts rows. The loadavg threshold semantics
// changed from an absolute load average to load-PER-CORE (so one global threshold means
// the same thing on a 1-core and a 64-core host). Stored absolute values (e.g. warning 4)
// would be silently reinterpreted as 4/core and effectively never fire, so the rows are
// cleared to reset to the new per-core defaults; admins reconfigure with the new meaning.
// Other metrics (cpu/memory/disk) are unaffected.
func init() {
	m.Register(func(app core.App) error {
		records, err := app.FindAllRecords("metric_alerts", dbx.HashExp{"metric": "loadavg"})
		if err != nil {
			return nil // collection missing or none → nothing to do
		}
		for _, rec := range records {
			if err := app.Delete(rec); err != nil {
				return err
			}
		}
		return nil
	}, nil)
}
