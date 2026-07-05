package hub

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/Gu1llaum-3/vigil/internal/common"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const diskMonitorRulesCollection = "disk_monitor_rules"

var diskRuleModes = map[string]bool{"all": true, "include": true, "exclude": true}

// diskMonitorRule is one host's evaluatable disk-monitoring policy.
type diskMonitorRule struct {
	mode   string          // "all" | "include" | "exclude"
	mounts map[string]bool // set of mountpoints the mode refers to
}

// allows reports whether a mountpoint counts toward the monitored set under this rule.
func (r diskMonitorRule) allows(mountpoint string) bool {
	switch r.mode {
	case "include":
		return r.mounts[mountpoint]
	case "exclude":
		return !r.mounts[mountpoint]
	default: // "all"
		return true
	}
}

// diskRuleCache holds the per-host disk rules in memory so the per-poll worst-mount
// computation costs no DB query. Kept fresh via collection hooks + a boot warm.
type diskRuleCache struct {
	mu    sync.RWMutex
	rules map[string]diskMonitorRule // agentID → rule (absent = mode "all")
}

func ruleFromRecord(rec *core.Record) diskMonitorRule {
	var mounts []string
	_ = rec.UnmarshalJSONField("mounts", &mounts)
	set := make(map[string]bool, len(mounts))
	for _, m := range mounts {
		if m != "" {
			set[m] = true
		}
	}
	mode := rec.GetString("mode")
	if !diskRuleModes[mode] {
		mode = "all"
	}
	return diskMonitorRule{mode: mode, mounts: set}
}

// refreshDiskRuleCache reloads all disk-monitor rules into the in-memory cache.
func (h *Hub) refreshDiskRuleCache() error {
	records, err := h.FindAllRecords(diskMonitorRulesCollection)
	if err != nil {
		return err
	}
	rules := make(map[string]diskMonitorRule, len(records))
	for _, rec := range records {
		if agentID := rec.GetString("agent"); agentID != "" {
			rules[agentID] = ruleFromRecord(rec)
		}
	}
	h.diskRules.mu.Lock()
	h.diskRules.rules = rules
	h.diskRules.mu.Unlock()
	return nil
}

// monitoredWorstDisk returns the highest used% among the mounts this host actually monitors
// (per its include/exclude rule; default = all), plus that mount's name. `monitored` is true
// when at least one mount was retained; it is false when the host reports no mounts (legacy
// agent) OR the rule matches no mount (exclude-all / include-none) — callers distinguish those
// two by `len(mounts)`: legacy falls back to the agent's reported worst, a rule matching
// nothing mutes the disk alert (see diskMuted / the evaluator).
func (h *Hub) monitoredWorstDisk(agentID string, mounts []common.DiskMount) (pct float64, mount string, monitored bool) {
	h.diskRules.mu.RLock()
	rule, hasRule := h.diskRules.rules[agentID]
	h.diskRules.mu.RUnlock()
	for _, m := range mounts {
		if hasRule && !rule.allows(m.Mountpoint) {
			continue
		}
		monitored = true
		if m.UsedPercent > pct {
			pct = m.UsedPercent
			mount = m.Mountpoint
		}
	}
	return pct, mount, monitored
}

// diskMuted reports whether disk monitoring is muted for this host: it has a per-mount
// breakdown but the rule currently matches no mount. Legacy agents (no breakdown) are never
// muted — they keep evaluating the agent's reported worst.
func (h *Hub) diskMuted(agentID string, mounts []common.DiskMount) bool {
	if len(mounts) == 0 {
		return false
	}
	_, _, monitored := h.monitoredWorstDisk(agentID, mounts)
	return !monitored
}

// registerDiskRuleHooks keeps the cache in sync with the disk_monitor_rules collection
// (low-frequency, admin-edited — after-write hooks are safe, like metric_alerts/maintenance).
func (h *Hub) registerDiskRuleHooks() {
	reload := func(e *core.RecordEvent) error {
		if err := h.refreshDiskRuleCache(); err != nil {
			slog.Warn("disk rule cache refresh failed", "err", err)
		}
		return e.Next()
	}
	h.App.OnRecordAfterCreateSuccess(diskMonitorRulesCollection).BindFunc(reload)
	h.App.OnRecordAfterUpdateSuccess(diskMonitorRulesCollection).BindFunc(reload)
	h.App.OnRecordAfterDeleteSuccess(diskMonitorRulesCollection).BindFunc(reload)
}

// --- Admin API (gated by requireAdminRole on the routes) ---

type diskMonitorRulePayload struct {
	ID      string   `json:"id"`
	Agent   string   `json:"agent"`
	Mode    string   `json:"mode"`
	Mounts  []string `json:"mounts"`
	Created string   `json:"created"`
	Updated string   `json:"updated"`
}

func diskMonitorRuleResponse(rec *core.Record) diskMonitorRulePayload {
	var mounts []string
	_ = rec.UnmarshalJSONField("mounts", &mounts)
	if mounts == nil {
		mounts = []string{}
	}
	return diskMonitorRulePayload{
		ID:      rec.Id,
		Agent:   rec.GetString("agent"),
		Mode:    rec.GetString("mode"),
		Mounts:  mounts,
		Created: rec.GetString("created"),
		Updated: rec.GetString("updated"),
	}
}

func (h *Hub) listDiskMonitorRules(e *core.RequestEvent) error {
	records, err := h.FindAllRecords(diskMonitorRulesCollection)
	if err != nil {
		return err
	}
	out := make([]diskMonitorRulePayload, 0, len(records))
	for _, rec := range records {
		out = append(out, diskMonitorRuleResponse(rec))
	}
	return e.JSON(http.StatusOK, out)
}

// upsertDiskMonitorRule creates or updates the rule for one agent (unique on agent). Sending
// mode "all" with no mounts is the default; a rule row is still kept so the UI can show it.
func (h *Hub) upsertDiskMonitorRule(e *core.RequestEvent) error {
	var body diskMonitorRulePayload
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("Invalid request body", err)
	}
	body.Agent = strings.TrimSpace(body.Agent)
	if body.Agent == "" {
		return e.BadRequestError("agent is required", nil)
	}
	if body.Mode == "" {
		body.Mode = "all"
	}
	if !diskRuleModes[body.Mode] {
		return e.BadRequestError("mode must be all, include or exclude", nil)
	}
	mounts := body.Mounts
	if mounts == nil {
		mounts = []string{}
	}
	// An include rule with no mounts would monitor nothing (silently muting disk for a host
	// with real filesystems) — almost always a mistake, so reject it.
	if body.Mode == "include" && len(mounts) == 0 {
		return e.BadRequestError("include mode needs at least one mount", nil)
	}
	if _, err := h.FindRecordById("agents", body.Agent); err != nil {
		return e.BadRequestError("unknown agent", err)
	}
	err := h.upsertByUnique(diskMonitorRulesCollection, "agent = {:agent}", dbx.Params{"agent": body.Agent}, func(rec *core.Record) {
		rec.Set("agent", body.Agent)
		rec.Set("mode", body.Mode)
		rec.Set("mounts", mounts)
	})
	if err != nil {
		return e.BadRequestError("Failed to save disk monitor rule", err)
	}
	// upsertByUnique saves via SaveNoValidate; refresh the cache explicitly here rather than
	// relying only on the after-write hook, so the next poll sees the new rule immediately.
	if err := h.refreshDiskRuleCache(); err != nil {
		slog.Warn("disk rule cache refresh failed", "err", err)
	}
	rec, err := h.FindFirstRecordByFilter(diskMonitorRulesCollection, "agent = {:agent}", dbx.Params{"agent": body.Agent})
	if err != nil {
		return err
	}
	return e.JSON(http.StatusOK, diskMonitorRuleResponse(rec))
}

func (h *Hub) deleteDiskMonitorRule(e *core.RequestEvent) error {
	id := e.Request.PathValue("id")
	rec, err := h.FindRecordById(diskMonitorRulesCollection, id)
	if err != nil {
		return e.NotFoundError("Disk monitor rule not found", err)
	}
	if err := h.Delete(rec); err != nil {
		return err
	}
	if err := h.refreshDiskRuleCache(); err != nil {
		slog.Warn("disk rule cache refresh failed", "err", err)
	}
	return e.JSON(http.StatusOK, map[string]any{"ok": true})
}
