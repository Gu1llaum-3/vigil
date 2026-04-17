package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// GChatProvider delivers notifications to a Google Chat incoming webhook.
type GChatProvider struct {
	client *http.Client
}

func NewGChatProvider() *GChatProvider {
	return &GChatProvider{client: &http.Client{Timeout: 10 * time.Second}}
}

func (p *GChatProvider) Kind() string                { return "gchat" }
func (p *GChatProvider) SensitiveConfigKeys() []string { return []string{"url"} }

func (p *GChatProvider) ValidateConfig(raw map[string]any) error {
	_, err := requiredConfigString(raw, "url")
	return err
}

func (p *GChatProvider) Send(ctx context.Context, ch Channel, msg Message) (string, error) {
	webhookURL, err := requiredConfigString(ch.Config, "url")
	if err != nil {
		return "", err
	}

	payload := map[string]any{
		"cardsV2": []map[string]any{
			{
				"cardId": "notification",
				"card": map[string]any{
					"header": map[string]any{
						"title":    msg.Title,
						"subtitle": msg.Severity,
					},
					"sections": []map[string]any{
						{
							"widgets": []map[string]any{
								{
									"textParagraph": map[string]string{
										"text": msg.Body,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("gchat: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("gchat: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("gchat: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("gchat: unexpected status %d", resp.StatusCode)
	}
	return fmt.Sprintf("webhook → %d", resp.StatusCode), nil
}
