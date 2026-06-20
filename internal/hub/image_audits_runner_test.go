//go:build testing

package hub

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/stretchr/testify/require"
)

func TestClassifyRegistryError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"deadline", context.DeadlineExceeded, imageAuditErrorTimeout},
		{"unauthorized", &transport.Error{StatusCode: http.StatusUnauthorized}, imageAuditErrorAuth},
		{"forbidden", &transport.Error{StatusCode: http.StatusForbidden}, imageAuditErrorAuth},
		{"not_found", &transport.Error{StatusCode: http.StatusNotFound}, imageAuditErrorNotFound},
		{"server_error", &transport.Error{StatusCode: http.StatusBadGateway}, imageAuditErrorRegistry},
		{"net_timeout", &timeoutNetErr{}, imageAuditErrorTimeout},
		{"net_other", &netError{msg: "connection refused"}, imageAuditErrorNetwork},
		{"no_such_host", errors.New("dial tcp: lookup foo: no such host"), imageAuditErrorNetwork},
		{"unknown", errors.New("boom"), imageAuditErrorOther},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, classifyRegistryError(tc.err))
		})
	}
}

func TestCachingRegistryClientMemoizesListTags(t *testing.T) {
	var calls atomic.Int32
	inner := mockImageRegistryClient{
		tags: map[string][]string{"docker.io/library/nginx": {"1.0", "2.0"}},
	}
	wrapped := callCountingRegistryClient{inner: inner, listCalls: &calls}
	client := newCachingRegistryClient(wrapped)

	for i := 0; i < 3; i++ {
		out, err := client.ListTags(context.Background(), "docker.io/library/nginx")
		require.NoError(t, err)
		require.Len(t, out, 2)
	}
	require.EqualValues(t, 1, calls.Load(), "ListTags must be called once per repo per cycle")
}

func TestCachingRegistryClientCachesErrorsToo(t *testing.T) {
	var calls atomic.Int32
	wrapped := callCountingRegistryClient{
		inner:     mockImageRegistryClient{err: &transport.Error{StatusCode: http.StatusNotFound}},
		listCalls: &calls,
	}
	client := newCachingRegistryClient(wrapped)
	_, err1 := client.ListTags(context.Background(), "ghcr.io/missing/repo")
	_, err2 := client.ListTags(context.Background(), "ghcr.io/missing/repo")
	require.Error(t, err1)
	require.Error(t, err2)
	// Not retried (not retryable) and cached.
	require.EqualValues(t, 1, calls.Load())
}

func TestCachingRegistryClientWaitsForInFlightListTags(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	var innerCalls atomic.Int32
	client := newCachingRegistryClient(blockingListTagsClient{
		tags:    []string{"1.0", "1.1"},
		block:   release,
		started: started,
		calls:   &innerCalls,
	})

	firstResult := make(chan []string, 1)
	firstErr := make(chan error, 1)
	go func() {
		tags, err := client.ListTags(context.Background(), "docker.io/library/nginx")
		firstResult <- tags
		firstErr <- err
	}()
	<-started

	secondResult := make(chan []string, 1)
	secondErr := make(chan error, 1)
	go func() {
		tags, err := client.ListTags(context.Background(), "docker.io/library/nginx")
		secondResult <- tags
		secondErr <- err
	}()

	select {
	case <-secondResult:
		t.Fatal("second ListTags call returned before the first completed")
	case <-time.After(20 * time.Millisecond):
	}

	close(release)

	select {
	case tags := <-firstResult:
		require.Equal(t, []string{"1.0", "1.1"}, tags)
	case <-time.After(time.Second):
		t.Fatal("first ListTags call did not complete")
	}
	require.NoError(t, <-firstErr)

	select {
	case tags := <-secondResult:
		require.Equal(t, []string{"1.0", "1.1"}, tags)
	case <-time.After(time.Second):
		t.Fatal("second ListTags call did not complete")
	}
	require.NoError(t, <-secondErr)
	require.EqualValues(t, 1, innerCalls.Load())
}

func TestRetryRegistryCallRetriesTransientErrors(t *testing.T) {
	var calls int
	out, err := retryRegistryCall(context.Background(), func() (string, error) {
		calls++
		if calls < 3 {
			return "", &transport.Error{StatusCode: http.StatusBadGateway}
		}
		return "ok", nil
	})
	require.NoError(t, err)
	require.Equal(t, "ok", out)
	require.Equal(t, 3, calls)
}

func TestRetryRegistryCallUsesConfiguredRetryBudget(t *testing.T) {
	oldRetryAfter := retryAfter
	retryAfter = func(time.Duration) <-chan time.Time {
		ch := make(chan time.Time, 1)
		ch <- time.Now()
		return ch
	}
	defer func() { retryAfter = oldRetryAfter }()

	// Budget = 1 initial attempt + imageAuditMaxRetries retries; succeed on the last.
	wantCalls := 1 + imageAuditMaxRetries
	var calls int
	out, err := retryRegistryCall(context.Background(), func() (string, error) {
		calls++
		if calls < wantCalls {
			return "", &transport.Error{StatusCode: http.StatusBadGateway}
		}
		return "ok", nil
	})
	require.NoError(t, err)
	require.Equal(t, "ok", out)
	require.Equal(t, wantCalls, calls)
}

func TestRetryRegistryCallDoesNotRetryTimeouts(t *testing.T) {
	// A timeout must not be retried: retrying only burns the shared budget.
	var calls int
	_, err := retryRegistryCall(context.Background(), func() (string, error) {
		calls++
		return "", context.DeadlineExceeded
	})
	require.Error(t, err)
	require.Equal(t, 1, calls, "timeouts must not be retried")

	calls = 0
	_, err = retryRegistryCall(context.Background(), func() (string, error) {
		calls++
		return "", &timeoutNetErr{}
	})
	require.Error(t, err)
	require.Equal(t, 1, calls, "net timeouts must not be retried")
}

func TestRetryRegistryCallDoesNotRetryAuthFailures(t *testing.T) {
	var calls int
	_, err := retryRegistryCall(context.Background(), func() (string, error) {
		calls++
		return "", &transport.Error{StatusCode: http.StatusUnauthorized}
	})
	require.Error(t, err)
	require.Equal(t, 1, calls, "auth errors must not be retried")
}

func TestRetryRegistryCallStopsOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := retryRegistryCall(ctx, func() (string, error) {
		return "", &transport.Error{StatusCode: http.StatusBadGateway}
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled))
}

func TestRunImageAuditPoolRespectsParallelism(t *testing.T) {
	// Distinct registry hosts run in parallel up to the parallelism limit.
	var inflight, peak atomic.Int32
	inner := slowMockClient{
		tags:     []string{"1.0.0", "1.0.1", "1.1.0"},
		inflight: &inflight,
		peak:     &peak,
		delay:    30 * time.Millisecond,
	}

	targets := make([]imageAuditTarget, 12)
	for i := range targets {
		host := fmt.Sprintf("reg%d.example.com", i) // 12 distinct hosts
		targets[i] = imageAuditTarget{
			AgentID:     "a",
			ContainerID: fmt.Sprintf("c%d", i),
			Tag:         "1.0.0",
			Registry:    host,
			Repository:  "app",
			CurrentRef:  host + "/app:1.0.0",
			Policy:      imageAuditPolicySemverMinor,
		}
	}

	results := runImageAuditPool(context.Background(), inner, targets, 3)
	require.Len(t, results, 12)
	require.LessOrEqual(t, peak.Load(), int32(3), "must not exceed parallelism limit")
	require.Greater(t, peak.Load(), int32(1), "distinct hosts should parallelize")
}

func TestRunImageAuditPoolPacesSameHost(t *testing.T) {
	// The inter-container delay is applied BETWEEN consecutive containers on the same host
	// (not before the first, not between distinct hosts, not between a container's internal
	// calls). Assert deterministically by counting retryAfter invocations.
	var delays atomic.Int32
	oldRetryAfter := retryAfter
	retryAfter = func(time.Duration) <-chan time.Time {
		delays.Add(1)
		ch := make(chan time.Time, 1)
		ch <- time.Now()
		return ch
	}
	defer func() { retryAfter = oldRetryAfter }()

	inner := slowMockClient{tags: []string{"1.0.0"}, inflight: new(atomic.Int32), peak: new(atomic.Int32)}

	mk := func(id, host string) imageAuditTarget {
		return imageAuditTarget{
			AgentID: "a", ContainerID: id, Tag: "1.0.0", Registry: host, Repository: "app",
			CurrentRef: host + "/app:1.0.0", Policy: imageAuditPolicySemverMinor,
		}
	}

	// 3 containers on ONE host → 2 inter-container delays (no delay before the first); no
	// registry call errors, so retryAfter is only invoked by the pacing logic.
	delays.Store(0)
	runImageAuditPool(context.Background(), inner, []imageAuditTarget{
		mk("c0", "reg.example.com"), mk("c1", "reg.example.com"), mk("c2", "reg.example.com"),
	}, 4)
	require.EqualValues(t, 2, delays.Load(), "N same-host containers must pace N-1 times")

	// 3 containers on DISTINCT hosts → each is the first in its group → 0 delays.
	delays.Store(0)
	runImageAuditPool(context.Background(), inner, []imageAuditTarget{
		mk("c0", "a.example.com"), mk("c1", "b.example.com"), mk("c2", "c.example.com"),
	}, 4)
	require.EqualValues(t, 0, delays.Load(), "distinct hosts must not be paced")
}

func TestRunImageAuditPoolSerializesSameHost(t *testing.T) {
	// All targets on the same registry host must be audited one at a time, even with
	// parallelism > 1, so the host is never hit concurrently.
	var inflight, peak atomic.Int32
	inner := slowMockClient{
		tags:     []string{"1.0.0", "1.0.1"},
		inflight: &inflight,
		peak:     &peak,
		delay:    20 * time.Millisecond,
	}

	targets := make([]imageAuditTarget, 6)
	for i := range targets {
		targets[i] = imageAuditTarget{
			AgentID:     "a",
			ContainerID: fmt.Sprintf("c%d", i),
			Tag:         "1.0.0",
			Registry:    "docker.io",
			Repository:  "library/x",
			CurrentRef:  "docker.io/library/x:1.0.0",
			Policy:      imageAuditPolicySemverMinor,
		}
	}

	results := runImageAuditPool(context.Background(), inner, targets, 4)
	require.Len(t, results, 6)
	require.EqualValues(t, 1, peak.Load(), "same-host targets must never run concurrently")
}

// ── test helpers ─────────────────────────────────────────────────────────────

type netError struct {
	msg     string
	timeout bool
}

func (e *netError) Error() string   { return e.msg }
func (e *netError) Timeout() bool   { return e.timeout }
func (e *netError) Temporary() bool { return false }

var _ net.Error = (*netError)(nil)

type timeoutNetErr struct{}

func (*timeoutNetErr) Error() string   { return "i/o timeout" }
func (*timeoutNetErr) Timeout() bool   { return true }
func (*timeoutNetErr) Temporary() bool { return true }

var _ net.Error = (*timeoutNetErr)(nil)

type callCountingRegistryClient struct {
	inner     imageRegistryClient
	listCalls *atomic.Int32
}

func (c callCountingRegistryClient) HeadDigest(ctx context.Context, ref string) (string, error) {
	return c.inner.HeadDigest(ctx, ref)
}
func (c callCountingRegistryClient) ResolvedDigest(ctx context.Context, ref, arch string) (string, error) {
	return c.inner.ResolvedDigest(ctx, ref, arch)
}
func (c callCountingRegistryClient) ListTags(ctx context.Context, repo string) ([]string, error) {
	if c.listCalls != nil {
		c.listCalls.Add(1)
	}
	return c.inner.ListTags(ctx, repo)
}

type blockingListTagsClient struct {
	tags    []string
	block   <-chan struct{}
	started chan<- struct{}
	calls   *atomic.Int32
}

func (c blockingListTagsClient) HeadDigest(context.Context, string) (string, error) {
	return "", nil
}

func (c blockingListTagsClient) ResolvedDigest(context.Context, string, string) (string, error) {
	return "", nil
}

func (c blockingListTagsClient) ListTags(_ context.Context, _ string) ([]string, error) {
	if c.calls != nil {
		c.calls.Add(1)
	}
	if c.started != nil {
		select {
		case c.started <- struct{}{}:
		default:
		}
	}
	<-c.block
	return c.tags, nil
}

type slowMockClient struct {
	tags        []string
	headDigests map[string]string
	delay       time.Duration
	inflight    *atomic.Int32
	peak        *atomic.Int32
}

func (s slowMockClient) trackInflight() func() {
	now := s.inflight.Add(1)
	for {
		old := s.peak.Load()
		if now <= old || s.peak.CompareAndSwap(old, now) {
			break
		}
	}
	return func() { s.inflight.Add(-1) }
}

func (s slowMockClient) HeadDigest(_ context.Context, ref string) (string, error) {
	defer s.trackInflight()()
	time.Sleep(s.delay)
	return s.headDigests[ref], nil
}

func (s slowMockClient) ResolvedDigest(_ context.Context, ref, arch string) (string, error) {
	defer s.trackInflight()()
	time.Sleep(s.delay)
	return "", nil
}

func (s slowMockClient) ListTags(_ context.Context, _ string) ([]string, error) {
	defer s.trackInflight()()
	time.Sleep(s.delay)
	return s.tags, nil
}

// ── per-host serialization / throttle / per-call timeout (Commit 1) ───────────

// hostTrackingClient records, per registry host, concurrent in-flight calls and the
// global peak, and can block each call (respecting ctx) to exercise timing.
type hostTrackingClient struct {
	mu          sync.Mutex
	inflight    map[string]int
	peakPerHost map[string]int
	globalIn    int
	globalPeak  int
	starts      []time.Time

	sleep     time.Duration // per-call hold (HeadDigest/ResolvedDigest)
	listBlock time.Duration // ListTags hold (for the per-call timeout test)
}

func newHostTrackingClient() *hostTrackingClient {
	return &hostTrackingClient{inflight: map[string]int{}, peakPerHost: map[string]int{}}
}

func (c *hostTrackingClient) track(ctx context.Context, ref string, block time.Duration) error {
	host := registryHost(ref)
	c.mu.Lock()
	c.inflight[host]++
	c.globalIn++
	if c.inflight[host] > c.peakPerHost[host] {
		c.peakPerHost[host] = c.inflight[host]
	}
	if c.globalIn > c.globalPeak {
		c.globalPeak = c.globalIn
	}
	c.starts = append(c.starts, time.Now())
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.inflight[host]--
		c.globalIn--
		c.mu.Unlock()
	}()
	if block <= 0 {
		block = c.sleep
	}
	if block > 0 {
		select {
		case <-time.After(block):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (c *hostTrackingClient) HeadDigest(ctx context.Context, ref string) (string, error) {
	if err := c.track(ctx, ref, 0); err != nil {
		return "", err
	}
	return "sha256:head", nil
}
func (c *hostTrackingClient) ResolvedDigest(ctx context.Context, ref, _ string) (string, error) {
	if err := c.track(ctx, ref, 0); err != nil {
		return "", err
	}
	return "sha256:resolved", nil
}
func (c *hostTrackingClient) ListTags(ctx context.Context, repo string) ([]string, error) {
	if err := c.track(ctx, repo, c.listBlock); err != nil {
		return nil, err
	}
	return []string{"1.0.0"}, nil
}

func withAuditTunables(t *testing.T, perCall, perHostDelay time.Duration) {
	t.Helper()
	oc, od := imageAuditPerCallTimeout, imageAuditPerHostDelay
	imageAuditPerCallTimeout, imageAuditPerHostDelay = perCall, perHostDelay
	t.Cleanup(func() { imageAuditPerCallTimeout, imageAuditPerHostDelay = oc, od })
}

// Per-host serialization and inter-container pacing are now properties of
// runImageAuditPool (one sequential goroutine per host), covered by
// TestRunImageAuditPoolSerializesSameHost and TestRunImageAuditPoolPacesSameHost. The
// caching client itself only provides the per-call timeout (below) and the ListTags memo.

func TestCachingClientPerCallTimeoutIsIsolated(t *testing.T) {
	// Each call gets its own timeout: a slow ListTags times out on its own without
	// starving a fast HeadDigest of the same host.
	withAuditTunables(t, 50*time.Millisecond, 0)
	inner := newHostTrackingClient()
	inner.listBlock = 500 * time.Millisecond // exceeds the per-call timeout
	client := newCachingRegistryClient(inner)

	_, err := client.ListTags(context.Background(), "docker.io/library/postgres")
	require.Error(t, err, "slow ListTags must hit its own per-call timeout")
	require.True(t, errors.Is(err, context.DeadlineExceeded))

	digest, err := client.HeadDigest(context.Background(), "docker.io/library/postgres:18")
	require.NoError(t, err, "fast HeadDigest must succeed despite the slow ListTags")
	require.Equal(t, "sha256:head", digest)
}

// ── review fixes: classifier taxonomy + singleflight ctx handling ─────────────

func TestClassifyRegistryClientVsTransient(t *testing.T) {
	// 4xx-other (definitive client errors) must be classified as client_error, which is
	// neither transient (not preserved) nor retryable. 429 stays transient/registry.
	require.Equal(t, imageAuditErrorClient, classifyRegistryError(&transport.Error{StatusCode: 400}))
	require.Equal(t, imageAuditErrorClient, classifyRegistryError(&transport.Error{StatusCode: 422}))
	require.Equal(t, imageAuditErrorClient, classifyRegistryError(&transport.Error{StatusCode: 410}))
	require.Equal(t, imageAuditErrorRegistry, classifyRegistryError(&transport.Error{StatusCode: 429}))
	require.Equal(t, imageAuditErrorRegistry, classifyRegistryError(&transport.Error{StatusCode: 503}))
	// 408 Request Timeout / 425 Too Early are transient, not definitive client errors.
	require.Equal(t, imageAuditErrorRegistry, classifyRegistryError(&transport.Error{StatusCode: 408}))
	require.Equal(t, imageAuditErrorRegistry, classifyRegistryError(&transport.Error{StatusCode: 425}))

	require.False(t, isTransientAuditError(imageAuditErrorClient), "client errors must not be preserved")
	require.False(t, isRetryableRegistryError(&transport.Error{StatusCode: 400}), "client errors must not be retried")
	require.True(t, isTransientAuditError(imageAuditErrorRegistry))
}

// ctxOnceClient returns the given error on the first ListTags then succeeds, counting calls.
type ctxOnceClient struct {
	calls    *atomic.Int32
	firstErr error
	tags     []string
}

func (c ctxOnceClient) HeadDigest(context.Context, string) (string, error) { return "sha256:x", nil }
func (c ctxOnceClient) ResolvedDigest(context.Context, string, string) (string, error) {
	return "sha256:x", nil
}
func (c ctxOnceClient) ListTags(context.Context, string) ([]string, error) {
	if c.calls.Add(1) == 1 {
		return nil, c.firstErr
	}
	return c.tags, nil
}

func TestCachingClientDoesNotCacheContextErrors(t *testing.T) {
	withAuditTunables(t, 5*time.Second, 0)
	var calls atomic.Int32
	client := newCachingRegistryClient(ctxOnceClient{calls: &calls, firstErr: context.DeadlineExceeded, tags: []string{"1.0.0"}})

	// First caller hits a per-caller deadline; it must NOT poison the shared cache.
	_, err := client.ListTags(context.Background(), "docker.io/library/x")
	require.ErrorIs(t, err, context.DeadlineExceeded)

	// Next caller in the cycle re-fetches and succeeds (entry was dropped, not memoized).
	tags, err := client.ListTags(context.Background(), "docker.io/library/x")
	require.NoError(t, err)
	require.Equal(t, []string{"1.0.0"}, tags)
	require.EqualValues(t, 2, calls.Load(), "ctx-error result must not be cached")
}
