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
	// Track concurrent in-flight ListTags calls.
	var inflight, peak atomic.Int32
	tags := []string{"1.0.0", "1.0.1", "1.1.0"}
	headDigests := map[string]string{"docker.io/library/x:1.0.1": "sha256:new"}
	inner := slowMockClient{
		tags:        tags,
		headDigests: headDigests,
		inflight:    &inflight,
		peak:        &peak,
		delay:       30 * time.Millisecond,
	}

	targets := make([]imageAuditTarget, 12)
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

	results := runImageAuditPool(context.Background(), inner, targets, 3)
	require.Len(t, results, 12)
	require.LessOrEqual(t, peak.Load(), int32(3), "must not exceed parallelism limit")
	require.Greater(t, peak.Load(), int32(1), "should actually parallelize")
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

type slowMockClient struct {
	tags        []string
	headDigests map[string]string
	delay       time.Duration
	inflight    *atomic.Int32
	peak        *atomic.Int32
	mu          sync.Mutex
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
