package hub

import (
	"context"
	"fmt"
	"net/http"
	"time"

	app "github.com/Gu1llaum-3/vigil"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpScopeKey carries the resolved API-key scope through the request context so the MCP
// handler can pick the tool set. (A plain struct key avoids collisions with other context values.)
type mcpScopeKey struct{}

// mcpRequestWithScope returns a copy of r tagged with the API-key scope. The /api/mcp route
// handler calls this so mcpHandler can serve a read-only vs read-write tool set.
func mcpRequestWithScope(r *http.Request, scope string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), mcpScopeKey{}, scope))
}

// mcpHandler returns the Streamable HTTP handler mounted at /api/mcp. It builds the read-only
// and read-write tool sets once and, per request, serves the one matching the caller's API-key
// scope — so a read-scoped key is never even offered a mutating tool (the per-tool scope gate).
// Stateless: the tools hold no per-session state.
func (h *Hub) mcpHandler() http.Handler {
	readSrv := h.newMCPServer(false)
	writeSrv := h.newMCPServer(true)
	opts := &mcpsdk.StreamableHTTPOptions{Stateless: true}
	return mcpsdk.NewStreamableHTTPHandler(func(r *http.Request) *mcpsdk.Server {
		if scope, _ := r.Context().Value(mcpScopeKey{}).(string); scope == apiScopeReadWrite {
			return writeSrv
		}
		return readSrv
	}, opts)
}

// newMCPServer builds a Vigil MCP server. Read-only tools are always registered; write tools
// (none yet — create/update/delete monitor is a planned phase) are registered only when
// includeWrite is true. A read-scoped key is served the includeWrite=false server, so it
// cannot reach a mutating tool even though the HTTP-method guard exempts /api/mcp.
func (h *Hub) newMCPServer(includeWrite bool) *mcpsdk.Server {
	s := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "vigil", Title: "Vigil", Version: app.Version}, nil)
	h.registerMCPReadTools(s)
	if includeWrite {
		// Phase 5: register read-write tools here (ReadOnlyHint:false). Exposed only to
		// read-write keys, so the read/read-write distinction is enforced by tool visibility.
		h.registerMCPWriteTools(s)
	}
	return s
}

// registerMCPWriteTools registers the mutating tools. Empty for now (v1 is read-only); the
// hook exists so write tools are added in exactly one scope-gated place.
func (h *Hub) registerMCPWriteTools(_ *mcpsdk.Server) {}

func (h *Hub) registerMCPReadTools(s *mcpsdk.Server) {
	readOnly := &mcpsdk.ToolAnnotations{ReadOnlyHint: true}

	type emptyInput struct{}

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "fleet_summary",
		Description: "Aggregated fleet overview: host counts (total/connected/offline), monitor up/total, hosts needing reboot or updates, outdated/security package totals, container counts and OS/patch distributions.",
		Annotations: readOnly,
	}, func(_ context.Context, _ *mcpsdk.CallToolRequest, _ emptyInput) (*mcpsdk.CallToolResult, DashboardSummary, error) {
		data, err := h.buildDashboard()
		if err != nil {
			return nil, DashboardSummary{}, err
		}
		summary, ok := data["summary"].(DashboardSummary)
		if !ok {
			return nil, DashboardSummary{}, fmt.Errorf("dashboard summary unavailable")
		}
		return nil, summary, nil
	})

	type hostsOutput struct {
		Hosts []HostOverviewRecord `json:"hosts"`
	}
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "list_hosts",
		Description: "List all monitored hosts with their connection status and latest lightweight metrics (CPU, memory, disk, network).",
		Annotations: readOnly,
	}, func(_ context.Context, _ *mcpsdk.CallToolRequest, _ emptyInput) (*mcpsdk.CallToolResult, hostsOutput, error) {
		hosts, err := h.loadHostsOverview()
		if err != nil {
			return nil, hostsOutput{}, err
		}
		return nil, hostsOutput{Hosts: hosts}, nil
	})

	type hostInput struct {
		ID string `json:"id" jsonschema:"the agent/host id"`
	}
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "get_host",
		Description: "Full detail for one host by id: identity, status, latest snapshot (OS, packages, repositories, reboot, Docker) and latest metrics.",
		Annotations: readOnly,
	}, func(_ context.Context, _ *mcpsdk.CallToolRequest, in hostInput) (*mcpsdk.CallToolResult, HostOverviewRecord, error) {
		rec, err := h.buildHostDetail(in.ID)
		if err != nil {
			return nil, HostOverviewRecord{}, err
		}
		return nil, rec, nil
	})

	type monitorsOutput struct {
		Groups []*MonitorGroupResponse `json:"groups"`
	}
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "list_monitors",
		Description: "List all uptime monitors grouped by their group, each with current status, last latency, and computed 24h average latency and 24h/30d uptime percentages.",
		Annotations: readOnly,
	}, func(_ context.Context, _ *mcpsdk.CallToolRequest, _ emptyInput) (*mcpsdk.CallToolResult, monitorsOutput, error) {
		groups, err := h.buildMonitorsResponse()
		if err != nil {
			return nil, monitorsOutput{}, err
		}
		return nil, monitorsOutput{Groups: groups}, nil
	})

	type monitorInput struct {
		ID string `json:"id" jsonschema:"the monitor id"`
	}
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "get_monitor",
		Description: "Detail for one monitor by id: type, target, current status, last latency, 24h average latency, 24h/30d uptime, and recent check points.",
		Annotations: readOnly,
	}, func(_ context.Context, _ *mcpsdk.CallToolRequest, in monitorInput) (*mcpsdk.CallToolResult, MonitorRecord, error) {
		rec, err := h.buildMonitorDetail(in.ID)
		if err != nil {
			return nil, MonitorRecord{}, err
		}
		return nil, rec, nil
	})

	type eventsInput struct {
		ID    string `json:"id" jsonschema:"the monitor id"`
		Limit int    `json:"limit,omitempty" jsonschema:"max events to return, newest first (default 100, max 5000)"`
		Since string `json:"since,omitempty" jsonschema:"RFC3339 lower bound on the check time"`
		Until string `json:"until,omitempty" jsonschema:"RFC3339 upper bound on the check time"`
	}
	type eventsOutput struct {
		Events []MonitorEventEntry `json:"events"`
	}
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "monitor_events",
		Description: "Historical check results for one monitor (status, latency_ms, message, timestamp), newest first. Use the time bounds and limit to build uptime / response-time reports over a window.",
		Annotations: readOnly,
	}, func(_ context.Context, _ *mcpsdk.CallToolRequest, in eventsInput) (*mcpsdk.CallToolResult, eventsOutput, error) {
		limit := in.Limit
		if limit <= 0 {
			limit = 100
		}
		if limit > 5000 {
			limit = 5000
		}
		var sincePtr, untilPtr *time.Time
		if in.Since != "" {
			t, err := time.Parse(time.RFC3339, in.Since)
			if err != nil {
				return nil, eventsOutput{}, err
			}
			sincePtr = &t
		}
		if in.Until != "" {
			t, err := time.Parse(time.RFC3339, in.Until)
			if err != nil {
				return nil, eventsOutput{}, err
			}
			untilPtr = &t
		}
		events, err := h.loadMonitorEvents(in.ID, limit, sincePtr, untilPtr)
		if err != nil {
			return nil, eventsOutput{}, err
		}
		return nil, eventsOutput{Events: events}, nil
	})
}
