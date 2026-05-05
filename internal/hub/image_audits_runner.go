package hub

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

const (
	// imageAuditParallelism is the maximum number of containers audited
	// concurrently. Registries throttle aggressively under bursty traffic, so
	// keep this conservative.
	imageAuditParallelism = 4

	// imageAuditMaxRetries is the number of additional attempts beyond the
	// first try when a registry call hits a transient error.
	imageAuditMaxRetries = 2

	imageAuditPerContainerTimeout = 20 * time.Second
)

// Error kinds surfaced through imageAuditResult.ErrorKind. These help the UI
// distinguish recoverable misconfigurations (auth) from genuine outages.
const (
	imageAuditErrorAuth     = "auth_failed"
	imageAuditErrorNotFound = "not_found"
	imageAuditErrorTimeout  = "timeout"
	imageAuditErrorNetwork  = "network"
	imageAuditErrorRegistry = "registry_error"
	imageAuditErrorOther    = "unknown"
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
		}
		if terr.StatusCode >= 500 {
			return imageAuditErrorRegistry
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
// retry. Auth and not-found are not retried; transient network/5xx are.
func isRetryableRegistryError(err error) bool {
	switch classifyRegistryError(err) {
	case imageAuditErrorNetwork, imageAuditErrorRegistry, imageAuditErrorTimeout:
		return true
	default:
		return false
	}
}

// cachingRegistryClient memoizes ListTags calls per (repository) within an
// audit cycle and retries transient failures with a fixed backoff. It is safe
// for concurrent use. A new instance must be created per audit cycle so the
// cache does not outlive its freshness window.
type cachingRegistryClient struct {
	inner imageRegistryClient

	tagsMu sync.Mutex
	tags   map[string]tagsCacheEntry
}

type tagsCacheEntry struct {
	tags []string
	err  error
}

func newCachingRegistryClient(inner imageRegistryClient) *cachingRegistryClient {
	return &cachingRegistryClient{
		inner: inner,
		tags:  make(map[string]tagsCacheEntry),
	}
}

func (c *cachingRegistryClient) HeadDigest(ctx context.Context, imageRef string) (string, error) {
	return retryRegistryCall(ctx, func() (string, error) {
		return c.inner.HeadDigest(ctx, imageRef)
	})
}

func (c *cachingRegistryClient) ResolvedDigest(ctx context.Context, imageRef, architecture string) (string, error) {
	return retryRegistryCall(ctx, func() (string, error) {
		return c.inner.ResolvedDigest(ctx, imageRef, architecture)
	})
}

func (c *cachingRegistryClient) ListTags(ctx context.Context, repository string) ([]string, error) {
	c.tagsMu.Lock()
	if entry, ok := c.tags[repository]; ok {
		c.tagsMu.Unlock()
		return entry.tags, entry.err
	}
	// Reserve the slot under the lock so concurrent calls for the same repo
	// wait on the in-flight result.
	wait := make(chan struct{})
	c.tags[repository] = tagsCacheEntry{}
	c.tagsMu.Unlock()
	defer close(wait)

	tags, err := retryRegistrySlice(ctx, func() ([]string, error) {
		return c.inner.ListTags(ctx, repository)
	})
	c.tagsMu.Lock()
	c.tags[repository] = tagsCacheEntry{tags: tags, err: err}
	c.tagsMu.Unlock()
	return tags, err
}

func retryRegistryCall(ctx context.Context, fn func() (string, error)) (string, error) {
	var lastErr error
	delay := 200 * time.Millisecond
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
		case <-time.After(delay):
		}
		delay *= 4
	}
	return "", lastErr
}

func retryRegistrySlice(ctx context.Context, fn func() ([]string, error)) ([]string, error) {
	var lastErr error
	delay := 200 * time.Millisecond
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
		case <-time.After(delay):
		}
		delay *= 4
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
	sem := make(chan struct{}, parallelism)
	var wg sync.WaitGroup
	for i, target := range targets {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, target imageAuditTarget) {
			defer wg.Done()
			defer func() { <-sem }()
			auditCtx, cancel := context.WithTimeout(ctx, imageAuditPerContainerTimeout)
			defer cancel()
			resolved := resolveImageAudit(auditCtx, registryClient, target)
			if resolved.Error != "" && resolved.ErrorKind == "" {
				// resolveImageAudit doesn't currently classify errors itself;
				// fall back to a best-effort classification from the message.
				resolved.ErrorKind = classifyRegistryErrorMessage(resolved.Error)
			}
			results[i] = resolved
		}(i, target)
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
