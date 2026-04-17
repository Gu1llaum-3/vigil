package providers

import (
	"context"
	"strings"
)

// InAppProvider is a virtual provider used only to persist notification_logs
// entries that the frontend can turn into local toast notifications.
type InAppProvider struct{}

func NewInAppProvider() *InAppProvider {
	return &InAppProvider{}
}

func (p *InAppProvider) Kind() string { return "in-app" }

func (p *InAppProvider) SensitiveConfigKeys() []string { return nil }

func (p *InAppProvider) ValidateConfig(_ map[string]any) error { return nil }

func (p *InAppProvider) Send(_ context.Context, _ Channel, msg Message) (string, error) {
	parts := []string{msg.Title}
	if body := strings.TrimSpace(msg.Body); body != "" {
		parts = append(parts, body)
	}
	return strings.Join(parts, "\n\n"), nil
}
