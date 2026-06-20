package hub

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

var retryAfter = time.After

const (
	// imageAuditParallelism is the maximum number of registry HOSTS audited
	// concurrently. runImageAuditPool processes each host's containers sequentially in
	// one goroutine, so a single registry is never hit concurrently; this bounds how many
	// distinct registries run in parallel.
	imageAuditParallelism = 4

	// imageAuditMaxRetries is the number of additional attempts beyond the first
	// try when a registry call hits a transient error. Kept small: retries share
	// the per-container budget and a retry storm is what caused the timeouts.
	imageAuditMaxRetries = 2

	// imageAuditPerContainerTimeout is a generous safety net so one container
	// cannot run forever; the real per-operation bound is imageAuditPerCallTimeout.
	imageAuditPerContainerTimeout = 90 * time.Second

	imageAuditRetryDelay    = 250 * time.Millisecond
	imageAuditMaxRetryDelay = 2 * time.Second
)

// Tunable as vars so tests can shrink them. imageAuditPerCallTimeout bounds a single
// registry call (so a slow ListTags cannot starve the following HeadDigest);
// imageAuditPerHostDelay paces consecutive CONTAINERS audited on the same registry host.
var (
	imageAuditPerCallTimeout = 30 * time.Second
	imageAuditPerHostDelay   = 300 * time.Millisecond
)

// Error kinds surfaced through imageAuditResult.ErrorKind. These help the UI
// distinguish recoverable misconfigurations (auth) from genuine outages.
const (
	imageAuditErrorAuth     = "auth_failed"
	imageAuditErrorNotFound = "not_found"
	imageAuditErrorTimeout  = "timeout"
	imageAuditErrorNetwork  = "network"
	imageAuditErrorRegistry = "registry_error"
	// imageAuditErrorClient is a definitive 4xx client error (bad request, unprocessable,
	// gone, …) other than auth/not-found/rate-limit — not transient, not retryable.
	imageAuditErrorClient = "client_error"
	imageAuditErrorOther  = "unknown"
)

// classifyRegistryError maps a registry call error into a stable error kind.
func classifyRegistryError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return imageAuditErrorTimeout
	}
	var terr *transport.Error
	if errors.As(err, &terr) {
		switch terr.StatusCode {
		case 401, 403:
			return imageAuditErrorAuth
		case 404:
			return imageAuditErrorNotFound
		case 408, 425, 429:
			// 408 Request Timeout / 425 Too Early / 429 Too Many Requests are transient
			// (the server is asking us to come back), so they recover on a later attempt.
			return imageAuditErrorRegistry
		}
		if terr.StatusCode >= 500 {
			return imageAuditErrorRegistry
		}
		if terr.StatusCode >= 400 {
			// definitive client error (400/410/422/…): surface it, do not treat as transient
			return imageAuditErrorClient
		}
		return imageAuditErrorRegistry
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return imageAuditErrorTimeout
		}
		return imageAuditErrorNetwork
	}
	if strings.Contains(strings.ToLower(err.Error()), "no such host") {
		return imageAuditErrorNetwork
	}
	return imageAuditErrorOther
}

// isRetryableRegistryError returns true for errors that warrant a backoff and
// retry. Auth and not-found are not retried; transient network/5xx are. Timeouts
// are NOT retried: a retry would only consume more of the shared budget, and the
// next audit cycle re-checks anyway.
func isRetryableRegistryError(err error) bool {
	switch classifyRegistryError(err) {
	case imageAuditErrorNetwork, imageAuditErrorRegistry:
		return true
	default:
		return false
	}
}

// cachingRegistryClient memoizes ListTags calls per repository within an
// audit cycle and retries transient failures with a fixed backoff. It is safe
// for concurrent use. A new instance must be created per audit cycle so the
// cache does not outlive its freshness window.
type cachingRegistryClient struct {
	inner imageRegistryClient

	tagsMu sync.Mutex
	tags   map[string]*tagsCacheEntry
}

// registryHost extracts the registry host from an image reference or repository path
// ("lscr.io/linuxserver/freshrss" → "lscr.io", "postgres:18" → "index.docker.io"), so
// all Docker Hub repos are grouped under one host. Falls back to the raw string if
// unparseable.
func registryHost(ref string) string {
	if r, err := name.ParseReference(ref); err == nil {
		return r.Context().RegistryStr()
	}
	if repo, err := name.NewRepository(ref); err == nil {
		return repo.RegistryStr()
	}
	return ref
}

// withCallTimeout runs fn with a fresh per-call timeout derived from ctx, so a slow
// ListTags cannot starve the following HeadDigest. Per-registry-host serialization +
// inter-container pacing are handled once, in runImageAuditPool (one sequential goroutine
// per host), so no per-call host lock is needed here.
func withCallTimeout[T any](ctx context.Context, fn func(context.Context) (T, error)) (T, error) {
	callCtx, cancel := context.WithTimeout(ctx, imageAuditPerCallTimeout)
	defer cancel()
	return fn(callCtx)
}

type tagsCacheEntry struct {
	ready chan struct{}
	tags  []string
	err   error
}

func newCachingRegistryClient(inner imageRegistryClient) *cachingRegistryClient {
	return &cachingRegistryClient{
		inner: inner,
		tags:  make(map[string]*tagsCacheEntry),
	}
}

func (c *cachingRegistryClient) HeadDigest(ctx context.Context, imageRef string) (string, error) {
	return retryRegistryCall(ctx, func() (string, error) {
		return withCallTimeout(ctx, func(callCtx context.Context) (string, error) {
			return c.inner.HeadDigest(callCtx, imageRef)
		})
	})
}

func (c *cachingRegistryClient) ResolvedDigest(ctx context.Context, imageRef, architecture string) (string, error) {
	return retryRegistryCall(ctx, func() (string, error) {
		return withCallTimeout(ctx, func(callCtx context.Context) (string, error) {
			return c.inner.ResolvedDigest(callCtx, imageRef, architecture)
		})
	})
}

func (c *cachingRegistryClient) ListTags(ctx context.Context, repository string) ([]string, error) {
	c.tagsMu.Lock()
	if entry, ok := c.tags[repository]; ok {
		c.tagsMu.Unlock()
		select {
		case <-entry.ready:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return entry.tags, entry.err
	}
	// Reserve the slot under the lock so concurrent calls for the same repo
	// wait on the in-flight result instead of duplicating the request.
	entry := &tagsCacheEntry{ready: make(chan struct{})}
	c.tags[repository] = entry
	c.tagsMu.Unlock()

	tags, err := retryRegistrySlice(ctx, func() ([]string, error) {
		return withCallTimeout(ctx, func(callCtx context.Context) ([]string, error) {
			return c.inner.ListTags(callCtx, repository)
		})
	})
	c.tagsMu.Lock()
	entry.tags = tags
	entry.err = err
	// A per-caller context cancellation/deadline is specific to THIS caller's budget, not
	// a property of the repository — do not memoize it, or the next caller in the cycle
	// (and any same-repo sibling) would inherit a failure it never actually hit. Drop the
	// entry so a subsequent call re-fetches with its own budget.
	if err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		delete(c.tags, repository)
	}
	close(entry.ready)
	c.tagsMu.Unlock()
	return tags, err
}

func retryRegistryCall(ctx context.Context, fn func() (string, error)) (string, error) {
	var lastErr error
	delay := imageAuditRetryDelay
	for attempt := 0; attempt <= imageAuditMaxRetries; attempt++ {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		out, err := fn()
		if err == nil {
			return out, nil
		}
		lastErr = err
		if !isRetryableRegistryError(err) {
			return "", err
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-retryAfter(delay):
		}
		if delay *= 2; delay > imageAuditMaxRetryDelay {
			delay = imageAuditMaxRetryDelay
		}
	}
	return "", lastErr
}

func retryRegistrySlice(ctx context.Context, fn func() ([]string, error)) ([]string, error) {
	var lastErr error
	delay := imageAuditRetryDelay
	for attempt := 0; attempt <= imageAuditMaxRetries; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		out, err := fn()
		if err == nil {
			return out, nil
		}
		lastErr = err
		if !isRetryableRegistryError(err) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-retryAfter(delay):
		}
		if delay *= 2; delay > imageAuditMaxRetryDelay {
			delay = imageAuditMaxRetryDelay
		}
	}
	return nil, lastErr
}

// runImageAuditPool resolves the given audit targets in parallel using a
// semaphore-bounded worker pool. Each target gets its own per-container
// context with a fixed timeout. Results preserve input order.
func runImageAuditPool(ctx context.Context, registryClient imageRegistryClient, targets []imageAuditTarget, parallelism int) []imageAuditResult {
	if parallelism <= 0 {
		parallelism = imageAuditParallelism
	}
	results := make([]imageAuditResult, len(targets))

	// Group target indices by registry host and process each host's group sequentially in
	// its own goroutine, with `parallelism` host-groups running concurrently. This keeps
	// one in-flight request per registry (the gentleness goal) WITHOUT parking a worker
	// slot while blocked on a per-host lock — distinct registries genuinely run in
	// parallel instead of queueing behind a busy host (e.g. Docker-Hub-heavy fleets).
	groups := map[string][]int{}
	order := make([]string, 0)
	for i, target := range targets {
		host := registryHost(firstNonEmpty(target.CurrentRef, target.Registry))
		if _, ok := groups[host]; !ok {
			order = append(order, host)
		}
		groups[host] = append(groups[host], i)
	}

	auditOne := func(i int) {
		auditCtx, cancel := context.WithTimeout(ctx, imageAuditPerContainerTimeout)
		defer cancel()
		resolved := resolveImageAudit(auditCtx, registryClient, targets[i])
		if resolved.Error != "" && resolved.ErrorKind == "" {
			// resolveImageAudit doesn't currently classify errors itself;
			// fall back to a best-effort classification from the message.
			resolved.ErrorKind = classifyRegistryErrorMessage(resolved.Error)
		}
		results[i] = resolved
	}

	sem := make(chan struct{}, parallelism)
	var wg sync.WaitGroup
	for _, host := range order {
		idxs := groups[host]
		wg.Add(1)
		sem <- struct{}{}
		go func(idxs []int) {
			defer wg.Done()
			defer func() { <-sem }()
			for n, i := range idxs {
				if ctx.Err() != nil {
					return
				}
				// Pace consecutive requests to the same host (gentle on rate limits),
				// but only BETWEEN containers — the calls within one container are already
				// causally serial, so they don't need spacing.
				if n > 0 {
					select {
					case <-retryAfter(imageAuditPerHostDelay):
					case <-ctx.Done():
						return
					}
				}
				auditOne(i)
			}
		}(idxs)
	}
	wg.Wait()
	return results
}

// classifyRegistryErrorMessage is a fallback for cases where we only have the
// stringified error (from a stored result). It is intentionally lossy.
func classifyRegistryErrorMessage(msg string) string {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "unauthorized") || strings.Contains(lower, "denied") || strings.Contains(lower, "forbidden"):
		return imageAuditErrorAuth
	case strings.Contains(lower, "not found") || strings.Contains(lower, "manifest unknown"):
		return imageAuditErrorNotFound
	case strings.Contains(lower, "deadline exceeded") || strings.Contains(lower, "timeout"):
		return imageAuditErrorTimeout
	case strings.Contains(lower, "no such host") || strings.Contains(lower, "connection refused"):
		return imageAuditErrorNetwork
	default:
		return imageAuditErrorOther
	}
}
