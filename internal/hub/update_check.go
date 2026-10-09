package hub

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	app "github.com/Gu1llaum-3/vigil"
	"github.com/Gu1llaum-3/vigil/internal/ghupdate"
	"github.com/blang/semver"
	"github.com/pocketbase/pocketbase/core"
)

// UpdateInfo is the answer of GET /api/app/update (CHECK_UPDATES=true): the latest release
// when it is newer than the running hub, empty otherwise.
type UpdateInfo struct {
	Version string `json:"v"`
	Url     string `json:"url"`
}

const (
	updateCheckInterval = 6 * time.Hour
	// A failed check is retried after this delay rather than on every request (GitHub's
	// unauthenticated API allows 60 requests an hour).
	updateCheckRetry   = 5 * time.Minute
	updateCheckTimeout = 10 * time.Second
)

// updateChecker caches the latest-release check. Requests share it: one check at a time,
// the others wait for its answer. A failed check serves the last successful answer when
// there is one.
type updateChecker struct {
	client  ghupdate.HttpClient
	current string
	timeout time.Duration
	now     func() time.Time
	logger  *slog.Logger

	mu          sync.Mutex
	info        UpdateInfo
	lastSuccess time.Time
	lastErr     error
	lastFailure time.Time
}

func newUpdateChecker(client ghupdate.HttpClient, current string) *updateChecker {
	return &updateChecker{client: client, current: current, timeout: updateCheckTimeout, now: time.Now, logger: slog.Default()}
}

func (c *updateChecker) check(ctx context.Context) (UpdateInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if !c.lastSuccess.IsZero() && now.Sub(c.lastSuccess) < updateCheckInterval {
		return c.info, nil
	}
	if c.lastErr != nil && now.Sub(c.lastFailure) < updateCheckRetry {
		return c.lastAnswer()
	}
	// The fetch is shared: one caller going away must not cancel it for the others.
	info, err := c.fetch(context.WithoutCancel(ctx))
	if err != nil {
		c.logger.Warn("Update check failed", "err", err)
		c.lastErr, c.lastFailure = err, now
		return c.lastAnswer()
	}
	c.info, c.lastSuccess, c.lastErr = info, now, nil
	return info, nil
}

// lastAnswer is the last successful answer, or the error when there is none.
func (c *updateChecker) lastAnswer() (UpdateInfo, error) {
	if c.lastSuccess.IsZero() {
		return UpdateInfo{}, c.lastErr
	}
	return c.info, nil
}

func (c *updateChecker) fetch(ctx context.Context) (UpdateInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	latest, err := ghupdate.FetchLatestRelease(ctx, c.client, "")
	if err != nil {
		return UpdateInfo{}, err
	}
	current, err := semver.Parse(strings.TrimPrefix(c.current, "v"))
	if err != nil {
		return UpdateInfo{}, fmt.Errorf("cannot parse the running version %q: %w", c.current, err)
	}
	latestVersion, err := semver.Parse(strings.TrimPrefix(latest.Tag, "v"))
	if err != nil {
		return UpdateInfo{}, fmt.Errorf("cannot parse the latest release tag %q: %w", latest.Tag, err)
	}
	if !latestVersion.GT(current) {
		return UpdateInfo{}, nil
	}
	return UpdateInfo{Version: strings.TrimPrefix(latest.Tag, "v"), Url: latest.Url}, nil
}

// getUpdate returns the latest release when it is newer than the running hub.
func (c *updateChecker) getUpdate(e *core.RequestEvent) error {
	info, err := c.check(e.Request.Context())
	if err != nil {
		return e.Error(http.StatusBadGateway, "Cannot check for updates right now.", nil)
	}
	return e.JSON(http.StatusOK, info)
}

func newHubUpdateChecker() *updateChecker {
	return newUpdateChecker(http.DefaultClient, app.Version)
}
