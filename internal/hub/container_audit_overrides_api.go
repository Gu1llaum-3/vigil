package hub

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type containerAuditOverridePayload struct {
	ID            string `json:"id"`
	Agent         string `json:"agent"`
	ContainerName string `json:"container_name"`
	Policy        string `json:"policy"`
	Notes         string `json:"notes"`
	Created       string `json:"created"`
	Updated       string `json:"updated"`
}

func containerAuditOverrideResponse(rec *core.Record) containerAuditOverridePayload {
	return containerAuditOverridePayload{
		ID:            rec.Id,
		Agent:         rec.GetString("agent"),
		ContainerName: rec.GetString("container_name"),
		Policy:        rec.GetString("policy"),
		Notes:         rec.GetString("notes"),
		Created:       rec.GetString("created"),
		Updated:       rec.GetString("updated"),
	}
}

func validOverridePolicy(policy string) bool {
	switch policy {
	case auditOverrideDigest, auditOverridePatch, auditOverrideMinor, auditOverrideDisabled:
		return true
	}
	return false
}

func (h *Hub) listContainerAuditOverrides(e *core.RequestEvent) error {
	records, err := h.FindAllRecords(containerAuditOverridesCollection)
	if err != nil {
		return err
	}
	out := make([]containerAuditOverridePayload, 0, len(records))
	for _, rec := range records {
		out = append(out, containerAuditOverrideResponse(rec))
	}
	return e.JSON(http.StatusOK, out)
}

// upsertContainerAuditOverride accepts {agent, container_name, policy, notes}.
// policy="auto" (or empty) deletes any existing override; otherwise the record
// is created or updated. Uniqueness is enforced on (agent, container_name).
func (h *Hub) upsertContainerAuditOverride(e *core.RequestEvent) error {
	var body containerAuditOverridePayload
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("Invalid request body", err)
	}
	body.Agent = strings.TrimSpace(body.Agent)
	body.ContainerName = strings.TrimSpace(body.ContainerName)
	body.Policy = strings.TrimSpace(body.Policy)
	if body.Agent == "" || body.ContainerName == "" {
		return e.BadRequestError("agent and container_name are required", nil)
	}

	existing, findErr := h.FindFirstRecordByFilter(
		containerAuditOverridesCollection,
		"agent = {:agent} && container_name = {:name}",
		dbx.Params{"agent": body.Agent, "name": body.ContainerName},
	)

	// Reset to auto = delete the record.
	if body.Policy == "" || body.Policy == "auto" {
		if findErr == nil {
			if err := h.Delete(existing); err != nil {
				return err
			}
		}
		h.applyOverrideToAuditRecords(body.Agent, body.ContainerName, "")
		return e.JSON(http.StatusOK, containerAuditOverridePayload{
			Agent:         body.Agent,
			ContainerName: body.ContainerName,
			Policy:        "auto",
		})
	}

	if !validOverridePolicy(body.Policy) {
		return e.BadRequestError("invalid policy", errors.New(body.Policy))
	}

	if findErr != nil {
		collection, err := h.FindCachedCollectionByNameOrId(containerAuditOverridesCollection)
		if err != nil {
			return err
		}
		existing = core.NewRecord(collection)
		existing.Set("agent", body.Agent)
		existing.Set("container_name", body.ContainerName)
	}
	existing.Set("policy", body.Policy)
	existing.Set("notes", body.Notes)

	if err := h.Save(existing); err != nil {
		return e.BadRequestError("Failed to save override", err)
	}
	h.applyOverrideToAuditRecords(body.Agent, body.ContainerName, body.Policy)
	return e.JSON(http.StatusOK, containerAuditOverrideResponse(existing))
}

func (h *Hub) deleteContainerAuditOverride(e *core.RequestEvent) error {
	id := e.Request.PathValue("id")
	rec, err := h.FindFirstRecordByFilter(containerAuditOverridesCollection, "id = {:id}", dbx.Params{"id": id})
	if err != nil {
		return e.NotFoundError("Override not found", err)
	}
	agent := rec.GetString("agent")
	containerName := rec.GetString("container_name")
	if err := h.Delete(rec); err != nil {
		return err
	}
	h.applyOverrideToAuditRecords(agent, containerName, "")
	return e.JSON(http.StatusOK, map[string]any{"ok": true})
}

// applyOverrideToAuditRecords reflects an override change into the matching
// container_image_audits records so the dashboard updates immediately, without
// waiting for the next audit cycle. Only the visible status field is touched:
//
//   - newPolicy == "disabled": stamp status=disabled.
//   - newPolicy == "" (auto) or any other policy: if the current status is
//     "disabled", reset to "unknown" so the next audit run re-evaluates it.
//     Other statuses are left alone — the freshly-changed override will be
//     applied on the next cycle (or on "Check images now").
func (h *Hub) applyOverrideToAuditRecords(agentID, containerName, newPolicy string) {
	if agentID == "" || containerName == "" {
		return
	}
	records, err := h.FindRecordsByFilter(
		containerImageAuditsCollection,
		"agent = {:agent} && container_name = {:name}",
		"-checked_at",
		0,
		0,
		dbx.Params{"agent": agentID, "name": containerName},
	)
	if err != nil || len(records) == 0 {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, rec := range records {
		current := rec.GetString("status")
		switch newPolicy {
		case auditOverrideDisabled:
			if current == imageAuditStatusDisabled {
				continue
			}
			rec.Set("status", imageAuditStatusDisabled)
			rec.Set("error", "")
			rec.Set("checked_at", now)
			rec.Set("last_notified_signature", "")
			rec.Set("last_notified_at", "")
		default:
			if current != imageAuditStatusDisabled {
				continue
			}
			rec.Set("status", imageAuditStatusUnknown)
			rec.Set("checked_at", now)
		}
		_ = h.SaveNoValidate(rec)
	}
}
