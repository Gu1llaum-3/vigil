package hub

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/common"
	"github.com/Gu1llaum-3/vigil/internal/hub/ws"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// upsertHostSnapshot saves or updates a snapshot for a given agent.
func (h *Hub) upsertHostSnapshot(agentId string, snapshot common.HostSnapshotResponse) {
	dataBytes, err := json.Marshal(snapshot)
	if err != nil {
		slog.Warn("Failed to marshal snapshot", "agent", agentId, "err", err)
		return
	}

	rec, err := h.FindFirstRecordByFilter("host_snapshots", "agent = {:agent}", dbx.Params{"agent": agentId})
	if err != nil {
		// No existing record — create one
		col, colErr := h.FindCachedCollectionByNameOrId("host_snapshots")
		if colErr != nil {
			slog.Warn("host_snapshots collection not found", "err", colErr)
			return
		}
		rec = core.NewRecord(col)
		rec.Set("agent", agentId)
	}

	rec.Set("data", string(dataBytes))
	rec.Set("collected_at", snapshot.CollectedAt)
	if err := h.SaveNoValidate(rec); err != nil {
		slog.Warn("Failed to save host snapshot", "agent", agentId, "err", err)
	}
}

// collectAllSnapshots triggers snapshot collection for all connected agents and returns (refreshed, failed) counts.
func (h *Hub) collectAllSnapshots(ctx context.Context) (refreshed, failed int) {
	agents, err := h.FindRecordsByFilter("agents", "status = 'connected'", "", 0, 0)
	if err != nil {
		return 0, 0
	}

	var (
		wg sync.WaitGroup
		mu sync.Mutex
	)

	for _, agent := range agents {
		agentId := agent.Id
		connVal, ok := h.agentConns.Load(agentId)
		if !ok {
			mu.Lock()
			failed++
			mu.Unlock()
			continue
		}
		conn, ok := connVal.(*ws.WsConn)
		if !ok {
			mu.Lock()
			failed++
			mu.Unlock()
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			agentCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()

			snapshot, snapshotErr := conn.GetHostSnapshot(agentCtx)
			mu.Lock()
			defer mu.Unlock()
			if snapshotErr != nil {
				slog.Warn("Snapshot collection failed", "agent", agentId, "err", snapshotErr)
				failed++
				return
			}
			h.upsertHostSnapshot(agentId, snapshot)
			refreshed++
		}()
	}

	wg.Wait()
	return refreshed, failed
}

// refreshSnapshots is the HTTP handler for POST /api/app/refresh-snapshots.
// It triggers an immediate snapshot collection for all connected agents.
func (h *Hub) refreshSnapshots(e *core.RequestEvent) error {
	refreshed, failed := h.collectAllSnapshots(e.Request.Context())
	return e.JSON(http.StatusOK, map[string]int{"refreshed": refreshed, "failed": failed})
}

// startSnapshotTicker runs a background goroutine that periodically collects snapshots from all connected agents.
func (h *Hub) startSnapshotTicker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refreshed, failed := h.collectAllSnapshots(ctx)
			slog.Info("Periodic snapshot collection", "refreshed", refreshed, "failed", failed)
		}
	}
}
