package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// notification_channels.config holds webhook URLs, tokens and SMTP passwords. The admin read
// rules let /api/collections and realtime return it in clear, while /api/app/notifications
// redacts it; nothing reads the collection directly, so it is closed (superusers only).
func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("notification_channels")
		if err != nil {
			return err
		}
		collection.ListRule = nil
		collection.ViewRule = nil
		return app.Save(collection)
	}, nil)
}
