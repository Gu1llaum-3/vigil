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

// TeamsProvider delivers notifications to a Microsoft Teams incoming webhook.
type TeamsProvider struct {
	client *http.Client
}

func NewTeamsProvider() *TeamsProvider {
	return &TeamsProvider{client: &http.Client{Timeout: 10 * time.Second}}
}

func (p *TeamsProvider) Kind() string                { return "teams" }
func (p *TeamsProvider) SensitiveConfigKeys() []string { return []string{"url"} }

func (p *TeamsProvider) ValidateConfig(raw map[string]any) error {
	_, err := requiredConfigString(raw, "url")
	return err
}

func (p *TeamsProvider) Send(ctx context.Context, ch Channel, msg Message) (string, error) {
	webhookURL, err := requiredConfigString(ch.Config, "url")
	if err != nil {
		return "", err
	}

	themeColor := strings.TrimPrefix(severityColor(msg.Severity), "#")

	payload := map[string]any{
		"@type":      "MessageCard",
		"@context":   "http://schema.org/extensions",
		"summary":    msg.Title,
		"themeColor": themeColor,
		"sections": []map[string]any{
			{
				"activityTitle": msg.Title,
				"activityText":  msg.Body,
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("teams: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("teams: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("teams: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("teams: unexpected status %d", resp.StatusCode)
	}
	return fmt.Sprintf("webhook → %d", resp.StatusCode), nil
}
