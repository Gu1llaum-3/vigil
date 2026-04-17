package providers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// NtfyProvider delivers notifications to a ntfy topic.
type NtfyProvider struct {
	client *http.Client
}

func NewNtfyProvider() *NtfyProvider {
	return &NtfyProvider{client: &http.Client{Timeout: 10 * time.Second}}
}

func (p *NtfyProvider) Kind() string                { return "ntfy" }
func (p *NtfyProvider) SensitiveConfigKeys() []string { return []string{"token"} }

func (p *NtfyProvider) ValidateConfig(raw map[string]any) error {
	_, err := requiredConfigString(raw, "url")
	return err
}

func (p *NtfyProvider) Send(ctx context.Context, ch Channel, msg Message) (string, error) {
	topicURL, err := requiredConfigString(ch.Config, "url")
	if err != nil {
		return "", err
	}

	priority := 3
	if pv, ok := ch.Config["priority"]; ok {
		switch v := pv.(type) {
		case float64:
			priority = int(v)
		case int:
			priority = v
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, topicURL, strings.NewReader(msg.Body))
	if err != nil {
		return "", fmt.Errorf("ntfy: build request: %w", err)
	}
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("X-Title", msg.Title)
	req.Header.Set("X-Priority", strconv.Itoa(priority))

	if token, ok := configString(ch.Config, "token"); ok {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ntfy: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("ntfy: unexpected status %d", resp.StatusCode)
	}
	return fmt.Sprintf("%s → %d", topicURL, resp.StatusCode), nil
}
