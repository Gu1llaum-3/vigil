package hub

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/hub/notifications"
	"github.com/pocketbase/pocketbase/core"
)

const (
	monitorStatusUnknown = -1
	monitorStatusDown    = 0
	monitorStatusUp      = 1
	// monitorStatusPending marks a check that FAILED but has not yet reached the failure
	// threshold (so the monitor's effective status is still up). It is written only to
	// monitor_events rows — never to a monitor's own `status` — so the sparkline can show
	// "failing but not yet down" (amber) before the monitor flips down (red), Uptime-Kuma
	// style. Pending events are excluded from the uptime denominator.
	monitorStatusPending = 2
)

var (
	pingLookPath       = exec.LookPath
	pingCommandContext = exec.CommandContext
	pingLatencyPattern = regexp.MustCompile(`time[=<]?\s*([0-9]+(?:\.[0-9]+)?)\s*ms`)
)

// MonitorScheduler manages per-monitor check goroutines.
type MonitorScheduler struct {
	hub       *Hub
	ctx       context.Context
	startedAt time.Time
	cancels   sync.Map // monitorID → context.CancelFunc
	mu        sync.Mutex
}

const monitorStartupGracePeriod = 5 * time.Minute

func newMonitorScheduler(hub *Hub) *MonitorScheduler {
	return &MonitorScheduler{hub: hub}
}

// start loads all active monitors and starts their check goroutines.
func (ms *MonitorScheduler) start(ctx context.Context) {
	ms.ctx = ctx
	ms.startedAt = time.Now()
	if ms.hub == nil || ms.hub.DB() == nil {
		slog.Warn("Skipping monitor scheduler start: database not ready")
		return
	}
	monitors, err := ms.hub.FindRecordsByFilter("monitors", "active = true", "", 0, 0)
	if err != nil {
		slog.Warn("Failed to load monitors on startup", "err", err)
		return
	}
	for _, m := range monitors {
		ms.startMonitor(m.Id)
	}
	slog.Info("Monitor scheduler started", "count", len(monitors))
}

// startMonitor starts or restarts the check goroutine for a monitor.
func (ms *MonitorScheduler) startMonitor(monitorID string) {
	if ms.ctx == nil {
		return
	}
	ms.mu.Lock()
	defer ms.mu.Unlock()
	if cancel, ok := ms.cancels.LoadAndDelete(monitorID); ok {
		cancel.(context.CancelFunc)()
	}
	monCtx, cancel := context.WithCancel(ms.ctx)
	ms.cancels.Store(monitorID, cancel)
	goSafe("monitor check", func() { ms.runMonitor(monCtx, monitorID) })
}

// stopMonitor cancels the check goroutine for a monitor.
func (ms *MonitorScheduler) stopMonitor(monitorID string) {
	if cancel, ok := ms.cancels.LoadAndDelete(monitorID); ok {
		cancel.(context.CancelFunc)()
	}
}

func (ms *MonitorScheduler) runMonitor(ctx context.Context, monitorID string) {
	ms.doCheck(ctx, monitorID)

	for {
		monitor, err := ms.hub.FindRecordById("monitors", monitorID)
		if err != nil {
			return
		}
		interval := time.Duration(monitor.GetInt("interval")) * time.Second
		if interval < 30*time.Second {
			interval = 30 * time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
			ms.doCheck(ctx, monitorID)
		}
	}
}

func (ms *MonitorScheduler) doCheck(ctx context.Context, monitorID string) {
	monitor, err := ms.hub.FindRecordById("monitors", monitorID)
	if err != nil {
		return
	}

	monitorType := monitor.GetString("type")
	timeout := time.Duration(monitor.GetInt("timeout")) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var status int
	var latencyMs int64
	var msg string

	start := time.Now()
	switch monitorType {
	case "http":
		status, msg = checkHTTP(checkCtx, monitor)
		latencyMs = time.Since(start).Milliseconds()
	case "ping":
		status, latencyMs, msg = checkPing(checkCtx, monitor)
	case "tcp":
		status, msg = checkTCP(checkCtx, monitor)
		latencyMs = time.Since(start).Milliseconds()
	case "dns":
		status, msg = checkDNS(checkCtx, monitor)
		latencyMs = time.Since(start).Milliseconds()
	case "push":
		status, msg = checkPush(monitor)
	default:
		return
	}

	// Inverted monitors treat a reachable target as the alert condition, so flip
	// up<->down before the result is persisted and notifications are derived.
	// Only reachability checks are invertible — never push (a missing heartbeat is
	// a real outage, not a "reachable" signal), matching how the UI exposes it.
	if monitorType != "push" && monitor.GetBool("inverted") {
		status, msg = invertMonitorResult(status, msg)
	}

	ms.saveResult(monitor, status, latencyMs, msg)
}

// invertMonitorResult flips an up/down result for inverted monitors (a reachable
// target becomes the alert condition). The raw check message is kept verbatim —
// the "inverted" context is shown in the UI (badge / Mode cell), not baked into
// the stored message. An unknown status is left unchanged.
func invertMonitorResult(status int, msg string) (int, string) {
	switch status {
	case monitorStatusUp:
		return monitorStatusDown, msg
	case monitorStatusDown:
		return monitorStatusUp, msg
	default:
		return status, msg
	}
}

func (ms *MonitorScheduler) inStartupGracePeriod() bool {
	if ms.startedAt.IsZero() {
		return false
	}
	return time.Since(ms.startedAt) < monitorStartupGracePeriod
}

func (ms *MonitorScheduler) saveResult(monitor *core.Record, status int, latencyMs int64, msg string) {
	monitorID := monitor.Id

	failureThreshold := monitor.GetInt("failure_threshold")
	if monitor.Get("failure_threshold") == nil {
		failureThreshold = 3
	} else if failureThreshold < 0 {
		failureThreshold = 0
	}
	failureCount := monitor.GetInt("failure_count")
	previousStatus := monitor.GetInt("status")
	effectiveStatus := status

	if status == monitorStatusUp {
		failureCount = 0
	} else {
		failureCount++
		if failureCount >= failureThreshold && !ms.shouldDelayDownTransition(previousStatus, failureThreshold) {
			effectiveStatus = monitorStatusDown
		} else {
			effectiveStatus = previousStatus
		}
	}

	// A failed check that is still UNDER the failure threshold is recorded as "pending" so
	// the sparkline shows amber before red. This keys on the failure count, not on
	// effectiveStatus: once the count reaches the threshold the check is recorded as down
	// even if the startup grace delays the monitor's own status flip — so a genuine outage
	// during a hub restart still counts toward downtime instead of being hidden as pending.
	// The monitor's own status and the notification path are unaffected.
	eventStatus := status
	if status == monitorStatusDown && failureCount < failureThreshold {
		eventStatus = monitorStatusPending
	}

	now := time.Now().UTC()
	// Flag the check if it falls inside an active maintenance window covering this monitor, so
	// the uptime aggregates can exclude it. Read from the in-memory cache → no DB query here.
	inMaintenance := ms.hub.monitorUnderMaintenance(monitorID, now)

	col, err := ms.hub.FindCachedCollectionByNameOrId("monitor_events")
	if err == nil {
		event := core.NewRecord(col)
		event.Set("monitor", monitorID)
		event.Set("status", eventStatus)
		event.Set("latency_ms", latencyMs)
		event.Set("msg", msg)
		event.Set("checked_at", now)
		event.Set("maintenance", inMaintenance)
		if saveErr := ms.hub.SaveNoValidate(event); saveErr != nil {
			slog.Warn("Failed to save monitor event", "monitor", monitorID, "err", saveErr)
		}
	}

	monitor.Set("failure_count", failureCount)
	monitor.Set("status", effectiveStatus)
	monitor.Set("last_checked_at", time.Now())
	monitor.Set("last_latency_ms", latencyMs)
	monitor.Set("last_msg", msg)
	if saveErr := ms.hub.SaveNoValidate(monitor); saveErr != nil {
		slog.Warn("Failed to update monitor status", "monitor", monitorID, "err", saveErr)
		return
	}

	// Emit notification on status transition (skip unknown initial state)
	if effectiveStatus != previousStatus && previousStatus != monitorStatusUnknown {
		evt := notifications.Event{
			Kind:       notifications.KindForMonitor(effectiveStatus),
			OccurredAt: time.Now(),
			Resource: notifications.ResourceRef{
				ID:   monitorID,
				Name: monitor.GetString("name"),
				Type: "monitor",
			},
			Previous: monitorStatusName(previousStatus),
			Current:  monitorStatusName(effectiveStatus),
			Details:  map[string]any{"last_msg": msg, "latency_ms": latencyMs},
		}
		ms.hub.emitNotification(evt)
	}
}

func monitorStatusName(status int) string {
	switch status {
	case monitorStatusUp:
		return "up"
	case monitorStatusDown:
		return "down"
	default:
		return "unknown"
	}
}

func (ms *MonitorScheduler) shouldDelayDownTransition(previousStatus, failureThreshold int) bool {
	if failureThreshold <= 1 {
		return false
	}

	// Only soften the initial unknown state after hub boot; established monitors
	// should still transition down according to their configured threshold.
	return previousStatus == monitorStatusUnknown && ms.inStartupGracePeriod()
}

// familyNetwork narrows a dial network to a single IP family when ipFamily is "ipv4"/"ipv6",
// so HTTP/TCP checks can be pinned (e.g. to dodge a flaky IPv6 path to a Cloudflare-proxied
// target). Empty (or any other value) keeps the requested network — Go's default dual-stack
// Happy Eyeballs. Only tcp* is narrowed; other networks pass through unchanged.
func familyNetwork(network, ipFamily string) string {
	switch network {
	case "tcp", "tcp4", "tcp6":
		switch ipFamily {
		case "ipv4":
			return "tcp4"
		case "ipv6":
			return "tcp6"
		}
	}
	return network
}

func checkHTTP(ctx context.Context, monitor *core.Record) (status int, msg string) {
	url := monitor.GetString("url")
	if url == "" {
		return monitorStatusDown, "Missing URL"
	}
	method := monitor.GetString("http_method")
	if method == "" {
		method = "GET"
	}
	keyword := monitor.GetString("keyword")
	keywordInvert := monitor.GetBool("keyword_invert")

	acceptedCodes := []int{200}
	if raw := monitor.Get("http_accepted_codes"); raw != nil {
		if codes, ok := raw.([]interface{}); ok && len(codes) > 0 {
			parsed := make([]int, 0, len(codes))
			for _, c := range codes {
				if f, ok := c.(float64); ok {
					parsed = append(parsed, int(f))
				}
			}
			if len(parsed) > 0 {
				acceptedCodes = parsed
			}
		}
	}

	// Optional per-monitor IP-family pin (auto/ipv4/ipv6): narrow the dial network so an
	// IPv6-path outage doesn't take down a dual-stack (e.g. Cloudflare-proxied) target.
	ipFamily := monitor.GetString("ip_family")
	dialer := newGuardedDialer()
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
			// SSRF guard: reject loopback/link-local/metadata (and optionally private)
			// resolved addresses; also covers redirects and DNS rebinding.
			DialContext: func(dctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.DialContext(dctx, familyNetwork(network, ipFamily), addr)
			},
		},
	}

	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return monitorStatusDown, fmt.Sprintf("Invalid URL: %s", err)
	}
	req.Header.Set("User-Agent", "Vigil-Monitor/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return monitorStatusDown, fmt.Sprintf("Connection failed: %s", err)
	}
	defer resp.Body.Close()

	codeOK := false
	for _, code := range acceptedCodes {
		if resp.StatusCode == code {
			codeOK = true
			break
		}
	}
	if !codeOK {
		return monitorStatusDown, fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	if keyword != "" {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if readErr != nil {
			return monitorStatusDown, fmt.Sprintf("Failed to read body: %s", readErr)
		}
		found := strings.Contains(string(body), keyword)
		if keywordInvert && found {
			return monitorStatusDown, fmt.Sprintf("Keyword '%s' found (inverted match)", keyword)
		}
		if !keywordInvert && !found {
			return monitorStatusDown, fmt.Sprintf("Keyword '%s' not found", keyword)
		}
	}

	return monitorStatusUp, fmt.Sprintf("HTTP %d", resp.StatusCode)
}

func checkPing(ctx context.Context, monitor *core.Record) (status int, latencyMs int64, msg string) {
	hostname := strings.TrimSpace(monitor.GetString("hostname"))
	if hostname == "" {
		return monitorStatusDown, 0, "Missing hostname"
	}

	if _, err := pingLookPath("ping"); err != nil {
		return monitorStatusDown, 0, "Ping executable not available on hub"
	}

	count := monitor.GetInt("ping_count")
	if count <= 0 {
		count = 1
	}
	perRequestTimeout := monitor.GetInt("ping_per_request_timeout")
	if perRequestTimeout <= 0 {
		perRequestTimeout = 2
	}

	args := []string{"-n"}
	switch monitor.GetString("ping_ip_family") {
	case "ipv4":
		args = append(args, "-4")
	case "ipv6":
		args = append(args, "-6")
	}
	args = append(args, "-c", strconv.Itoa(count), "-W", strconv.Itoa(perRequestTimeout), hostname)

	start := time.Now()
	cmd := pingCommandContext(ctx, "ping", args...)
	out, err := cmd.CombinedOutput()
	latencyMs = time.Since(start).Milliseconds()
	output := strings.TrimSpace(string(out))

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return monitorStatusDown, latencyMs, fmt.Sprintf("Ping timed out after %s", time.Since(start).Round(time.Second))
		}
		if output == "" {
			return monitorStatusDown, latencyMs, fmt.Sprintf("Ping failed: %v", err)
		}
		return monitorStatusDown, latencyMs, compactMonitorMessage(output)
	}

	if parsed, ok := parsePingLatency(output); ok {
		latencyMs = parsed
	}

	return monitorStatusUp, latencyMs, "Ping successful"
}

func parsePingLatency(output string) (int64, bool) {
	if avg, ok := parsePingSummaryLatency(output); ok {
		return avg, true
	}
	matches := pingLatencyPattern.FindStringSubmatch(output)
	if len(matches) != 2 {
		return 0, false
	}

	ms, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, false
	}

	return int64(math.Round(ms)), true
}

func parsePingSummaryLatency(output string) (int64, bool) {
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "=") || !strings.Contains(line, "/") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(parts[1]))
		if len(fields) == 0 {
			continue
		}
		stats := strings.Split(fields[0], "/")
		if len(stats) < 2 {
			continue
		}
		avg, err := strconv.ParseFloat(stats[1], 64)
		if err == nil {
			return int64(math.Round(avg)), true
		}
	}
	return 0, false
}

func compactMonitorMessage(output string) string {
	message := strings.Join(strings.Fields(output), " ")
	if len(message) > 180 {
		return message[:177] + "..."
	}
	if message == "" {
		return "Check failed"
	}
	return message
}

func checkTCP(ctx context.Context, monitor *core.Record) (status int, msg string) {
	hostname := monitor.GetString("hostname")
	port := monitor.GetInt("port")
	if hostname == "" || port == 0 {
		return monitorStatusDown, "Missing hostname or port"
	}

	// SSRF guard: block loopback/link-local/metadata (and optionally private) targets.
	// Honor the per-monitor IP-family pin (auto/ipv4/ipv6) like the HTTP check.
	dialer := newGuardedDialer()
	network := familyNetwork("tcp", monitor.GetString("ip_family"))
	conn, err := dialer.DialContext(ctx, network, fmt.Sprintf("%s:%d", hostname, port))
	if err != nil {
		return monitorStatusDown, fmt.Sprintf("Connection failed: %s", err)
	}
	conn.Close()
	return monitorStatusUp, "TCP connection successful"
}

func checkDNS(ctx context.Context, monitor *core.Record) (status int, msg string) {
	host := monitor.GetString("dns_host")
	if host == "" {
		return monitorStatusDown, "Missing DNS host"
	}
	dnsType := monitor.GetString("dns_type")
	if dnsType == "" {
		dnsType = "A"
	}
	dnsServer := monitor.GetString("dns_server")

	resolver := net.DefaultResolver
	if dnsServer != "" {
		if !strings.Contains(dnsServer, ":") {
			dnsServer += ":53"
		}
		resolver = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "udp", dnsServer)
			},
		}
	}

	switch dnsType {
	case "A", "AAAA":
		addrs, err := resolver.LookupHost(ctx, host)
		if err != nil {
			return monitorStatusDown, fmt.Sprintf("DNS lookup failed: %s", err)
		}
		return monitorStatusUp, fmt.Sprintf("Resolved: %s", strings.Join(addrs, ", "))
	case "CNAME":
		cname, err := resolver.LookupCNAME(ctx, host)
		if err != nil {
			return monitorStatusDown, fmt.Sprintf("CNAME lookup failed: %s", err)
		}
		return monitorStatusUp, fmt.Sprintf("CNAME: %s", cname)
	case "MX":
		mxs, err := resolver.LookupMX(ctx, host)
		if err != nil {
			return monitorStatusDown, fmt.Sprintf("MX lookup failed: %s", err)
		}
		return monitorStatusUp, fmt.Sprintf("%d MX record(s)", len(mxs))
	case "NS":
		nss, err := resolver.LookupNS(ctx, host)
		if err != nil {
			return monitorStatusDown, fmt.Sprintf("NS lookup failed: %s", err)
		}
		return monitorStatusUp, fmt.Sprintf("%d NS record(s)", len(nss))
	case "TXT":
		txts, err := resolver.LookupTXT(ctx, host)
		if err != nil {
			return monitorStatusDown, fmt.Sprintf("TXT lookup failed: %s", err)
		}
		return monitorStatusUp, fmt.Sprintf("%d TXT record(s)", len(txts))
	default:
		addrs, err := resolver.LookupHost(ctx, host)
		if err != nil {
			return monitorStatusDown, fmt.Sprintf("DNS lookup failed: %s", err)
		}
		return monitorStatusUp, fmt.Sprintf("Resolved: %s", strings.Join(addrs, ", "))
	}
}

func checkPush(monitor *core.Record) (status int, msg string) {
	interval := monitor.GetInt("interval")
	if interval <= 0 {
		interval = 60
	}
	lastPushAt := monitor.GetDateTime("last_push_at")
	if lastPushAt.IsZero() {
		return monitorStatusDown, "No heartbeat received yet"
	}
	grace := time.Duration(interval)*time.Second + 30*time.Second
	elapsed := time.Since(lastPushAt.Time())
	if elapsed > grace {
		return monitorStatusDown, fmt.Sprintf("No heartbeat for %s (expected every %ds)", elapsed.Round(time.Second), interval)
	}
	return monitorStatusUp, fmt.Sprintf("Heartbeat received %s ago", elapsed.Round(time.Second))
}
