package providers

import (
	"context"
	"fmt"
	"time"
)

// Channel holds the persisted configuration for a notification channel.
type Channel struct {
	ID     string
	Kind   string
	Config map[string]any
}

// Message is the pre-rendered payload passed to providers for delivery.
type Message struct {
	Title        string
	Body         string
	Severity     string // "info", "warning", "critical"
	EventKind    string
	ResourceID   string
	ResourceName string
	ResourceType string
	Previous     string
	Current      string
	Timestamp    time.Time
}

// Provider defines the interface for a notification delivery backend.
type Provider interface {
	Kind() string
	// Send delivers the message and returns a short payload preview for logging.
	Send(ctx context.Context, ch Channel, msg Message) (payloadPreview string, err error)
	// ValidateConfig checks provider-specific configuration fields.
	ValidateConfig(raw map[string]any) error
	// SensitiveConfigKeys lists config keys that must be redacted in API responses.
	SensitiveConfigKeys() []string
}

var registry = map[string]Provider{}

// Register adds a provider to the global registry.
func Register(p Provider) {
	registry[p.Kind()] = p
}

// Get returns a provider by kind.
func Get(kind string) (Provider, bool) {
	p, ok := registry[kind]
	return p, ok
}

// Kinds returns all registered provider kinds.
func Kinds() []string {
	kinds := make([]string, 0, len(registry))
	for k := range registry {
		kinds = append(kinds, k)
	}
	return kinds
}

// configString reads a string value from a config map.
func configString(raw map[string]any, key string) (string, bool) {
	v, ok := raw[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok && s != ""
}

// requiredConfigString reads a required string value; returns an error if missing or empty.
func requiredConfigString(raw map[string]any, key string) (string, error) {
	v, ok := configString(raw, key)
	if !ok {
		return "", fmt.Errorf("missing required config field %q", key)
	}
	return v, nil
}
