package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// GotifyProvider delivers notifications to a Gotify server.
type GotifyProvider struct {
	client *http.Client
}

func NewGotifyProvider() *GotifyProvider {
	return &GotifyProvider{client: newGuardedHTTPClient(10 * time.Second)}
}

func (p *GotifyProvider) Kind() string                  { return "gotify" }
func (p *GotifyProvider) SensitiveConfigKeys() []string { return []string{"token"} }

func (p *GotifyProvider) ValidateConfig(raw map[string]any) error {
	if _, err := requiredConfigString(raw, "url"); err != nil {
		return err
	}
	_, err := requiredConfigString(raw, "token")
	return err
}

func (p *GotifyProvider) Send(ctx context.Context, ch Channel, msg Message) (string, error) {
	baseURL, err := requiredConfigString(ch.Config, "url")
	if err != nil {
		return "", err
	}
	token, err := requiredConfigString(ch.Config, "token")
	if err != nil {
		return "", err
	}

	priority := 5
	if pv, ok := ch.Config["priority"]; ok {
		switch v := pv.(type) {
		case float64:
			priority = int(v)
		case int:
			priority = v
		}
	}

	// The token goes in a header, not the query string, so it stays out of URLs (and logs).
	endpoint := strings.TrimSuffix(baseURL, "/") + "/message"

	payload := map[string]any{
		"title":    msg.Title,
		"message":  msg.Body,
		"priority": priority,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("gotify: marshal payload: %w", err)
	}

	req, err := newRequest(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("gotify: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gotify-Key", token)

	resp, err := doRequest(p.client, req)
	if err != nil {
		return "", fmt.Errorf("gotify: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("gotify: unexpected status %d", resp.StatusCode)
	}
	return fmt.Sprintf("%s → %d", baseURL, resp.StatusCode), nil
}
