package providers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/netguard"
)

// newGuardedHTTPClient builds the HTTP client used by every outbound provider. The
// destination URL is user-supplied, so the client dials through the shared SSRF guard to
// refuse loopback/link-local/cloud-metadata targets (defeating SSRF against the hub host).
// Private/LAN ranges are intentionally allowed — internal Slack/Mattermost/webhook
// endpoints are a primary use case — unless MONITOR_ALLOW_PRIVATE_TARGETS disabled the
// guard entirely.
//
// Redirects are not followed: Go forwards custom headers (a webhook's, Gotify's token) to a
// redirect target on another host, and a delivery endpoint has no reason to redirect. The
// 3xx is then reported as an unexpected status.
func newGuardedHTTPClient(timeout time.Duration) *http.Client {
	client := netguard.NewGuardedClient(timeout, false)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return client
}

// newRequest and doRequest build and send a provider request without the URL in their
// errors: the URL may be the credential (a Slack/Teams/GChat/webhook URL, an ntfy topic),
// and errors are stored in notification_logs and returned by the channel test.
func newRequest(ctx context.Context, method, rawURL string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	return req, withoutURL(err)
}

func doRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, withoutURL(err)
	}
	return resp, nil
}

func withoutURL(err error) error {
	if urlErr := (*url.Error)(nil); errors.As(err, &urlErr) {
		return fmt.Errorf("%s %s", urlErr.Op, urlErr.Err)
	}
	return err
}

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
