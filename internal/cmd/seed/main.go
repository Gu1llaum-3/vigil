// Command seed injects a synthetic "demo-host" agent plus ~24h of host metric
// history into the local dev database, so the dashboard and host-detail charts
// can be previewed without deploying real agents.
//
// Usage (from the repo root):
//
//	make dev-hub          # once, to create vigil_data/ and the collections (then Ctrl-C)
//	go run ./internal/cmd/seed
//	make dev              # dev-server + hub, then open the "demo-host" detail page
//
// Re-running refreshes the demo data idempotently. It writes to the same
// vigil_data/ directory the dev hub uses; run it while the hub is stopped.
package main

import (
	"log"
	"math"
	"time"

	app "github.com/Gu1llaum-3/vigil"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"

	_ "github.com/Gu1llaum-3/vigil/internal/migrations"
)

const (
	demoName     = "demo-host"
	sampleStep   = 5 * time.Minute
	historySpan  = 24 * time.Hour
	demoToken    = "demo-seed-token"
	demoVersion  = "0.0.0-demo"
	ramBytes     = 16 * 1024 * 1024 * 1024 // 16 GiB
	diskBytes    = 200 * 1024 * 1024 * 1024
	diskUsedPerc = 84.0
)

func main() {
	pb := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: app.HubDataDirName})
	if err := pb.Bootstrap(); err != nil {
		log.Fatalf("bootstrap failed: %v", err)
	}

	if _, err := pb.FindCollectionByNameOrId("host_metric_samples"); err != nil {
		log.Fatalf("collections not found in %s — run `make dev-hub` once first to initialize the DB, then re-run the seeder", app.HubDataDirName)
	}

	agent, err := upsertDemoAgent(pb)
	if err != nil {
		log.Fatalf("agent: %v", err)
	}
	if err := upsertSnapshot(pb, agent.Id); err != nil {
		log.Fatalf("snapshot: %v", err)
	}
	n, err := seedHostMetrics(pb, agent.Id)
	if err != nil {
		log.Fatalf("metrics: %v", err)
	}

	log.Printf("seeded %q (%s) with %d host metric samples over %s", demoName, agent.Id, n, historySpan)
	log.Printf("start the dev hub + server (`make dev`) and open the %q host to preview the charts", demoName)
}

func upsertDemoAgent(pb *pocketbase.PocketBase) (*core.Record, error) {
	rec, err := pb.FindFirstRecordByFilter("agents", "name = {:name}", dbx.Params{"name": demoName})
	if err != nil {
		col, cerr := pb.FindCollectionByNameOrId("agents")
		if cerr != nil {
			return nil, cerr
		}
		rec = core.NewRecord(col)
		rec.Set("token", demoToken)
		rec.Set("fingerprint", "demo-seed-fingerprint")
	}
	rec.Set("name", demoName)
	rec.Set("status", "connected")
	rec.Set("version", demoVersion)
	rec.Set("last_seen", types.NowDateTime())
	if err := pb.SaveNoValidate(rec); err != nil {
		return nil, err
	}
	return rec, nil
}

func upsertSnapshot(pb *pocketbase.PocketBase, agentID string) error {
	rec, err := pb.FindFirstRecordByFilter("host_snapshots", "agent = {:a}", dbx.Params{"a": agentID})
	if err != nil {
		col, cerr := pb.FindCollectionByNameOrId("host_snapshots")
		if cerr != nil {
			return cerr
		}
		rec = core.NewRecord(col)
		rec.Set("agent", agentID)
	}
	now := time.Now().UTC()
	rec.Set("collected_at", now.Format(time.RFC3339))
	rec.Set("data", map[string]any{
		"hostname":       demoName,
		"primary_ip":     "10.0.0.42",
		"os":             map[string]any{"family": "Debian", "name": "Debian GNU/Linux", "version": "12"},
		"kernel":         "6.1.0-demo",
		"architecture":   "x86_64",
		"uptime_seconds": 86400 * 7,
		"resources":      map[string]any{"cpu_model": "Demo CPU @ 3.0GHz", "cpu_cores": 8, "ram_mb": 16384, "swap_mb": 2048},
		"network":        map[string]any{"gateway": "10.0.0.1", "dns_servers": []string{"1.1.1.1"}},
		"storage": []map[string]any{{
			"device": "/dev/sda1", "mountpoint": "/", "fs_type": "ext4",
			"total_bytes": diskBytes, "used_bytes": int64(diskBytes * diskUsedPerc / 100),
			"available_bytes": int64(diskBytes * (100 - diskUsedPerc) / 100), "used_percent": diskUsedPerc,
		}},
		"packages":     map[string]any{"installed_count": 512, "outdated_count": 3, "security_count": 1},
		"repositories": []any{},
		"reboot":       map[string]any{"required": false, "reason": ""},
		"docker":       map[string]any{"state": "running", "container_count": 4, "running_count": 4, "containers": []any{}},
		"collected_at": now.Format(time.RFC3339),
	})
	return pb.SaveNoValidate(rec)
}

// seedHostMetrics clears any previous demo samples and writes a fresh ~24h history
// with a realistic-looking pattern, plus the latest-only current record.
func seedHostMetrics(pb *pocketbase.PocketBase, agentID string) (int, error) {
	if err := deleteByAgent(pb, "host_metric_samples", agentID); err != nil {
		return 0, err
	}
	if err := deleteByAgent(pb, "host_metric_current", agentID); err != nil {
		return 0, err
	}

	samplesCol, err := pb.FindCollectionByNameOrId("host_metric_samples")
	if err != nil {
		return 0, err
	}

	end := time.Now().UTC()
	start := end.Add(-historySpan)
	count := 0
	var last map[string]float64
	for t := start; !t.After(end); t = t.Add(sampleStep) {
		m := metricsAt(t, start)
		rec := core.NewRecord(samplesCol)
		applyMetrics(rec, agentID, t, m)
		if err := pb.SaveNoValidate(rec); err != nil {
			return count, err
		}
		count++
		last = m
	}

	// latest-only current record (unique per agent)
	curCol, err := pb.FindCollectionByNameOrId("host_metric_current")
	if err != nil {
		return count, err
	}
	cur := core.NewRecord(curCol)
	applyMetrics(cur, agentID, end, last)
	if err := pb.SaveNoValidate(cur); err != nil {
		return count, err
	}
	return count, nil
}

// metricsAt produces a plausible metric point for time t (no randomness needed:
// sine waves + periodic spikes give a natural-looking curve).
func metricsAt(t, start time.Time) map[string]float64 {
	mins := t.Sub(start).Minutes()

	cpu := 4 + 2.5*math.Sin(mins/37) + 1.5*math.Sin(mins/11)
	if int(mins)%173 < 5 { // occasional CPU spikes
		cpu += 18
	}
	cpu = clamp(cpu, 0.5, 100)

	memPct := clamp(50+6*math.Sin(mins/220)+1.5*math.Sin(mins/19), 5, 95)
	memUsed := memPct / 100 * ramBytes

	// network mostly idle with periodic bursts (bytes/sec)
	var rx, tx float64
	if int(mins)%97 < 4 {
		rx = 2_000_000 + 4_000_000*math.Abs(math.Sin(mins))
		tx = 1_000_000 + 3_000_000*math.Abs(math.Cos(mins))
	} else {
		rx = 20_000 + 15_000*math.Abs(math.Sin(mins/5))
		tx = 12_000 + 10_000*math.Abs(math.Cos(mins/5))
	}

	// Raw load average (demo host has 8 cores; the UI divides by cores). Base sits low
	// (~1.5-3 raw → ~0.2-0.4/core) with a spike up to ~8 raw (~1.0/core, crossing the
	// default warning line) during the periodic CPU spike. load1 is noisiest, load15
	// smoothest, mirroring how real load averages lag.
	loadBase := 1.6 + 1.1*math.Sin(mins/37) + 0.5*math.Sin(mins/11)
	loadSpike := 0.0
	if int(mins)%173 < 5 {
		loadSpike = 6
	}
	load1 := clamp(loadBase+loadSpike+0.4*math.Sin(mins/2), 0.05, 40)
	load5 := clamp(loadBase+0.7*loadSpike, 0.05, 40)
	load15 := clamp(loadBase*0.9+0.3*loadSpike, 0.05, 40)

	return map[string]float64{
		"cpu_percent":         round2(cpu),
		"memory_total_bytes":  ramBytes,
		"memory_used_bytes":   round2(memUsed),
		"memory_used_percent": round2(memPct),
		"disk_total_bytes":    diskBytes,
		"disk_used_bytes":     diskBytes * diskUsedPerc / 100,
		"disk_used_percent":   diskUsedPerc,
		"network_rx_bps":      round2(rx),
		"network_tx_bps":      round2(tx),
		"load1":               round2(load1),
		"load5":               round2(load5),
		"load15":              round2(load15),
	}
}

func applyMetrics(rec *core.Record, agentID string, t time.Time, m map[string]float64) {
	rec.Set("agent", agentID)
	rec.Set("collected_at", t.Format(time.RFC3339))
	for k, v := range m {
		rec.Set(k, v)
	}
}

func deleteByAgent(pb *pocketbase.PocketBase, collection, agentID string) error {
	recs, err := pb.FindRecordsByFilter(collection, "agent = {:a}", "", 0, 0, dbx.Params{"a": agentID})
	if err != nil {
		return err
	}
	for _, r := range recs {
		if err := pb.Delete(r); err != nil {
			return err
		}
	}
	return nil
}

func clamp(v, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, v))
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
