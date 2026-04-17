// Package agent implements the application agent that connects to the hub.
//
// The agent runs on remote systems and communicates with the hub over WebSocket.
// Implement your data collection logic in the handler registry (see handlers.go).
package agent

import (
	"log/slog"
	"strings"

	app "github.com/Gu1llaum-3/vigil"
	"github.com/Gu1llaum-3/vigil/agent/utils"
	gossh "golang.org/x/crypto/ssh"
)

// Agent is the main agent instance.
type Agent struct {
	connectionManager *ConnectionManager
	handlerRegistry   *HandlerRegistry
	dataDir           string
	keys              []gossh.PublicKey
}

// NewAgent creates a new agent with the given data directory for persisting data.
func NewAgent(dataDir ...string) (agent *Agent, err error) {
	agent = &Agent{}

	agent.dataDir, err = GetDataDir(dataDir...)
	if err != nil {
		slog.Warn("Data directory not found")
	} else {
		slog.Info("Data directory", "path", agent.dataDir)
	}

	// Set up slog log level from LOG_LEVEL env var
	if logLevelStr, exists := utils.GetEnv("LOG_LEVEL"); exists {
		switch strings.ToLower(logLevelStr) {
		case "debug":
			slog.SetLogLoggerLevel(slog.LevelDebug)
		case "warn":
			slog.SetLogLoggerLevel(slog.LevelWarn)
		case "error":
			slog.SetLogLoggerLevel(slog.LevelError)
		}
	}

	slog.Debug("Version", "v", app.Version)

	// initialize connection manager
	agent.connectionManager = newConnectionManager(agent)

	// initialize handler registry with default handlers
	agent.handlerRegistry = NewHandlerRegistry()

	return agent, nil
}

// Start initializes and starts the agent.
// keys are the hub's public keys used to verify hub identity over WebSocket.
func (a *Agent) Start(keys []gossh.PublicKey) error {
	a.keys = keys
	return a.connectionManager.Start()
}

// getFingerprint returns the agent's unique fingerprint.
func (a *Agent) getFingerprint() string {
	return GetFingerprint(a.dataDir)
}
