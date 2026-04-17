package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// SlackProvider delivers notifications to a Slack incoming webhook.
type SlackProvider struct {
	client *http.Client
}

func NewSlackProvider() *SlackProvider {
	return &SlackProvider{client: &http.Client{Timeout: 10 * time.Second}}
}

func (p *SlackProvider) Kind() string                { return "slack" }
func (p *SlackProvider) SensitiveConfigKeys() []string { return []string{"url"} }

func (p *SlackProvider) ValidateConfig(raw map[string]any) error {
	_, err := requiredConfigString(raw, "url")
	return err
}

func (p *SlackProvider) Send(ctx context.Context, ch Channel, msg Message) (string, error) {
	url, err := requiredConfigString(ch.Config, "url")
	if err != nil {
		return "", err
	}

	color := severityColor(msg.Severity)

	payload := map[string]any{
		"text": msg.Title,
		"attachments": []map[string]any{
			{
				"color": color,
				"blocks": []map[string]any{
					{
						"type": "section",
						"text": map[string]string{
							"type": "mrkdwn",
							"text": fmt.Sprintf("*%s*\n%s", msg.Title, msg.Body),
						},
					},
				},
				"fallback": msg.Title,
			},
		},
	}

	if username, ok := configString(ch.Config, "username"); ok {
		payload["username"] = username
	}
	if icon, ok := configString(ch.Config, "icon_emoji"); ok {
		payload["icon_emoji"] = icon
	}
	if channel, ok := configString(ch.Config, "channel"); ok {
		payload["channel"] = channel
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("slack: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("slack: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("slack: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("slack: unexpected status %d", resp.StatusCode)
	}
	return fmt.Sprintf("webhook → %d", resp.StatusCode), nil
}

func severityColor(severity string) string {
	switch severity {
	case "critical":
		return "#d73a49"
	case "warning":
		return "#e36209"
	case "info":
		return "#0075ca"
	default:
		return "#586069"
	}
}
