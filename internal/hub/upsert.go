package hub

import (
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// upsertByUnique finds the record matching filter (which is expected to target a
// unique key), runs apply on it, and saves it without validation. apply must set
// every field of the record, including the column(s) that form the unique key, so
// that a freshly created record is complete.
//
// If a concurrent writer inserts the same unique key in the window between the find
// and the save, SaveNoValidate fails with a UNIQUE constraint error. In that case the
// operation is retried once: the second find now sees the concurrent record and turns
// the insert into an update. This mirrors the find-then-save pattern used across the
// hub (host snapshots, metrics, image audits, overrides, enrollment tokens) while
// making it safe under the concurrent collection paths (connect-time + periodic ticker).
func (h *Hub) upsertByUnique(collection, filter string, params dbx.Params, apply func(*core.Record)) error {
	const maxAttempts = 4
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		rec, err := h.FindFirstRecordByFilter(collection, filter, params)
		if err != nil {
			col, colErr := h.FindCachedCollectionByNameOrId(collection)
			if colErr != nil {
				return colErr
			}
			rec = core.NewRecord(col)
		}
		apply(rec)
		saveErr := h.SaveNoValidate(rec)
		if saveErr == nil {
			return nil
		}
		if !isUniqueConstraintErr(saveErr) {
			return saveErr
		}
		// A concurrent writer won the insert. Re-find on the next attempt so the
		// conflicting insert turns into an update once the winning row is visible.
		lastErr = saveErr
	}
	return lastErr
}

func isUniqueConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	// Match both the raw SQLite error ("UNIQUE constraint failed: ...") and the
	// friendly validation-style message PocketBase surfaces ("...: Value must be unique.").
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") || strings.Contains(msg, "must be unique")
}
