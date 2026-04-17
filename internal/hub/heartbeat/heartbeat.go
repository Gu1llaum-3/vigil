// Package heartbeat sends periodic outbound pings to an external monitoring
// endpoint (e.g. BetterStack, Uptime Kuma, Healthchecks.io) so operators can
// monitor the app without exposing it to the internet.
package heartbeat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	app "github.com/Gu1llaum-3/vigil"
	"github.com/pocketbase/pocketbase/core"
)

// Default values for heartbeat configuration.
const (
	defaultInterval = 60 // seconds
	httpTimeout     = 10 * time.Second
)

// Payload is the JSON body sent with each heartbeat request.
type Payload struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
	Version   string `json:"version"`
}

// Config holds heartbeat settings read from environment variables.
type Config struct {
	URL      string // endpoint to ping
	Interval int    // seconds between pings
	Method   string // HTTP method (GET or POST, default POST)
}

// Heartbeat manages the periodic outbound health check.
type Heartbeat struct {
	app    core.App
	config Config
	client *http.Client
}

// New creates a Heartbeat if configuration is present.
// Returns nil if HEARTBEAT_URL is not set (feature disabled).
func New(pbApp core.App, getEnv func(string) (string, bool)) *Heartbeat {
	heartbeatURL, _ := getEnv("HEARTBEAT_URL")
	heartbeatURL = strings.TrimSpace(heartbeatURL)
	if pbApp == nil || heartbeatURL == "" {
		return nil
	}

	interval := defaultInterval
	if v, ok := getEnv("HEARTBEAT_INTERVAL"); ok {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			interval = parsed
		}
	}

	method := http.MethodPost
	if v, ok := getEnv("HEARTBEAT_METHOD"); ok {
		v = strings.ToUpper(strings.TrimSpace(v))
		if v == http.MethodGet || v == http.MethodHead {
			method = v
		}
	}

	return &Heartbeat{
		app: pbApp,
		config: Config{
			URL:      heartbeatURL,
			Interval: interval,
			Method:   method,
		},
		client: &http.Client{Timeout: httpTimeout},
	}
}

// Start begins the heartbeat loop. It blocks and should be called in a goroutine.
// The loop runs until the provided stop channel is closed.
func (hb *Heartbeat) Start(stop <-chan struct{}) {
	sanitizedURL := sanitizeHeartbeatURL(hb.config.URL)
	hb.app.Logger().Info("Heartbeat enabled",
		"url", sanitizedURL,
		"interval", fmt.Sprintf("%ds", hb.config.Interval),
		"method", hb.config.Method,
	)

	// Send an initial heartbeat immediately on startup.
	hb.send()

	ticker := time.NewTicker(time.Duration(hb.config.Interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			hb.send()
		}
	}
}

// Send performs a single heartbeat ping. Exposed for the test-heartbeat API endpoint.
func (hb *Heartbeat) Send() error {
	return hb.send()
}

// GetConfig returns the current heartbeat configuration.
func (hb *Heartbeat) GetConfig() Config {
	return hb.config
}

func (hb *Heartbeat) send() error {
	var req *http.Request
	var err error
	method := normalizeMethod(hb.config.Method)

	if method == http.MethodGet || method == http.MethodHead {
		req, err = http.NewRequest(method, hb.config.URL, nil)
	} else {
		payload := &Payload{
			Status:    "ok",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Version:   app.Version,
		}
		body, jsonErr := json.Marshal(payload)
		if jsonErr != nil {
			hb.app.Logger().Error("Heartbeat: failed to marshal payload", "err", jsonErr)
			return jsonErr
		}
		req, err = http.NewRequest(http.MethodPost, hb.config.URL, bytes.NewReader(body))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
		}
	}

	if err != nil {
		hb.app.Logger().Error("Heartbeat: failed to create request", "err", err)
		return err
	}

	req.Header.Set("User-Agent", "App-Heartbeat")

	resp, err := hb.client.Do(req)
	if err != nil {
		hb.app.Logger().Error("Heartbeat: request failed", "url", sanitizeHeartbeatURL(hb.config.URL), "err", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		hb.app.Logger().Warn("Heartbeat: non-success response",
			"url", sanitizeHeartbeatURL(hb.config.URL),
			"status", resp.StatusCode,
		)
		return fmt.Errorf("heartbeat endpoint returned status %d", resp.StatusCode)
	}

	return nil
}

func normalizeMethod(method string) string {
	upper := strings.ToUpper(strings.TrimSpace(method))
	if upper == http.MethodGet || upper == http.MethodHead || upper == http.MethodPost {
		return upper
	}
	return http.MethodPost
}

func sanitizeHeartbeatURL(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "<invalid-url>"
	}
	return parsed.Scheme + "://" + parsed.Host
}
