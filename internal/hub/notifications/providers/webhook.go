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

// WebhookProvider sends an HTTP POST request with a JSON payload.
type WebhookProvider struct {
	client *http.Client
}

// NewWebhookProvider creates a WebhookProvider with a 10s timeout.
func NewWebhookProvider() *WebhookProvider {
	return &WebhookProvider{
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *WebhookProvider) Kind() string { return "webhook" }

func (p *WebhookProvider) SensitiveConfigKeys() []string { return []string{"headers"} }

func (p *WebhookProvider) ValidateConfig(raw map[string]any) error {
	_, err := requiredConfigString(raw, "url")
	return err
}

func (p *WebhookProvider) Send(ctx context.Context, ch Channel, msg Message) (string, error) {
	url, err := requiredConfigString(ch.Config, "url")
	if err != nil {
		return "", err
	}

	method := "POST"
	if m, ok := configString(ch.Config, "method"); ok {
		method = strings.ToUpper(m)
	}

	payload := map[string]any{
		"title":         msg.Title,
		"body":          msg.Body,
		"severity":      msg.Severity,
		"event_kind":    msg.EventKind,
		"resource_id":   msg.ResourceID,
		"resource_name": msg.ResourceName,
		"resource_type": msg.ResourceType,
		"previous":      msg.Previous,
		"current":       msg.Current,
		"timestamp":     msg.Timestamp.UTC().Format(time.RFC3339),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("webhook: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("webhook: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if rawHeaders, ok := ch.Config["headers"]; ok {
		if headers, ok := rawHeaders.(map[string]any); ok {
			for k, v := range headers {
				if sv, ok := v.(string); ok {
					req.Header.Set(k, sv)
				}
			}
		}
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("webhook: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("webhook: unexpected status %d", resp.StatusCode)
	}

	preview := fmt.Sprintf("%s %s → %d", method, url, resp.StatusCode)
	return preview, nil
}
