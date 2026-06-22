// Package mcp exposes Vigil's data to MCP clients (e.g. an AI assistant) over a Streamable
// HTTP endpoint mounted on the hub router at /api/mcp. Tools are thin, read-only wrappers
// over the hub's existing data; authentication is handled upstream by the API-key middleware
// (a valid "Authorization: Bearer vk_..." is required to reach the endpoint).
package mcp

import (
	"context"
	"net/http"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewServer builds the Vigil MCP server with its tool set. (Phase 0 spike: a single `ping`
// tool to validate the endpoint end-to-end; the real read tools land in Phase 2.)
func NewServer(version string) *mcpsdk.Server {
	s := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "vigil", Title: "Vigil", Version: version}, nil)
	registerPingTool(s)
	return s
}

type pingInput struct{}

type pingOutput struct {
	Message string `json:"message" jsonschema:"a static pong response"`
}

func registerPingTool(s *mcpsdk.Server) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "ping",
		Description: "Health check — returns \"pong\". Confirms the Vigil MCP endpoint is reachable and the API key is valid.",
		Annotations: &mcpsdk.ToolAnnotations{ReadOnlyHint: true},
	}, func(_ context.Context, _ *mcpsdk.CallToolRequest, _ pingInput) (*mcpsdk.CallToolResult, pingOutput, error) {
		return nil, pingOutput{Message: "pong"}, nil
	})
}

// Handler returns the Streamable HTTP handler to mount on the hub router at /api/mcp. The
// same server instance is reused across sessions (the tools are stateless reads).
func Handler(version string) http.Handler {
	srv := NewServer(version)
	return mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return srv }, nil)
}
