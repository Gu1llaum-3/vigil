//go:build testing

package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeReleaseAPI serves tag as the latest release, or fails when err is set.
type fakeReleaseAPI struct {
	mu    sync.Mutex
	tag   string
	err   error
	block bool // wait for the request context instead of answering
	calls atomic.Int32
}

func (f *fakeReleaseAPI) Do(req *http.Request) (*http.Response, error) {
	f.calls.Add(1)
	f.mu.Lock()
	tag, err, block := f.tag, f.err, f.block
	f.mu.Unlock()
	if block {
		<-req.Context().Done()
		return nil, req.Context().Err()
	}
	if err != nil {
		return nil, err
	}
	body := `{"tag_name":"` + tag + `","html_url":"https://example.com/` + tag + `"}`
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewBufferString(body))}, nil
}

func (f *fakeReleaseAPI) set(tag string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tag, f.err = tag, err
}

func newTestUpdateChecker(api *fakeReleaseAPI, current string) (*updateChecker, *time.Time) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	c := newUpdateChecker(api, current)
	c.now = func() time.Time { return now }
	return c, &now
}

func TestUpdateCheckerCachesSuccess(t *testing.T) {
	api := &fakeReleaseAPI{tag: "v0.3.0"}
	c, now := newTestUpdateChecker(api, "0.2.17-beta")

	info, err := c.check(context.Background())
	require.NoError(t, err)
	assert.Equal(t, UpdateInfo{Version: "0.3.0", Url: "https://example.com/v0.3.0"}, info)

	*now = now.Add(5 * time.Hour)
	_, err = c.check(context.Background())
	require.NoError(t, err)
	assert.EqualValues(t, 1, api.calls.Load(), "cached for 6 h")

	// After the cache expires, an older or equal release clears a stale answer (the hub
	// was updated meanwhile).
	*now = now.Add(2 * time.Hour)
	api.set("v0.2.17-beta", nil)
	info, err = c.check(context.Background())
	require.NoError(t, err)
	assert.Equal(t, UpdateInfo{}, info)
	assert.EqualValues(t, 2, api.calls.Load())
}

// A failed check is retried after a short delay, not after 6 hours, and is reported.
func TestUpdateCheckerRetriesFailures(t *testing.T) {
	api := &fakeReleaseAPI{err: errors.New("network down")}
	c, now := newTestUpdateChecker(api, "0.2.17")

	_, err := c.check(context.Background())
	require.Error(t, err)
	_, err = c.check(context.Background())
	require.Error(t, err, "still failing within the retry delay")
	assert.EqualValues(t, 1, api.calls.Load(), "GitHub is not hammered")

	*now = now.Add(updateCheckRetry + time.Second)
	api.set("v0.2.18", nil)
	info, err := c.check(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "0.2.18", info.Version)

	// Once a check succeeded, a failure serves that answer.
	api.set("not-a-version", nil)
	*now = now.Add(7 * time.Hour)
	info, err = c.check(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "0.2.18", info.Version)
	require.Error(t, c.lastErr, "an unparsable tag is a failure, not a panic")
}

// A caller that goes away does not fail the shared check.
func TestUpdateCheckerIgnoresCallerCancellation(t *testing.T) {
	api := &fakeReleaseAPI{tag: "v0.3.0"}
	c, _ := newTestUpdateChecker(api, "0.2.17")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	info, err := c.check(ctx)
	require.NoError(t, err)
	assert.Equal(t, "0.3.0", info.Version)
}

func TestUpdateEndpoint(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)
	call := func(c *updateChecker) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		e := &core.RequestEvent{App: testApp}
		e.Request = httptest.NewRequest(http.MethodGet, "/api/app/update", nil)
		e.Response = rec
		err := c.getUpdate(e)
		if err != nil {
			var apiErr *router.ApiError
			require.ErrorAs(t, err, &apiErr)
			rec.Code = apiErr.Status
			body, _ := json.Marshal(apiErr)
			rec.Body = bytes.NewBuffer(body)
		}
		return rec
	}

	rec := call(newUpdateChecker(&fakeReleaseAPI{tag: "v0.3.0"}, "0.2.17"))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"v":"0.3.0","url":"https://example.com/v0.3.0"}`, rec.Body.String())

	rec = call(newUpdateChecker(&fakeReleaseAPI{tag: "v0.2.17"}, "0.2.17"))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"v":"","url":""}`, rec.Body.String())

	rec = call(newUpdateChecker(&fakeReleaseAPI{err: errors.New("down")}, "0.2.17"))
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), "Cannot check for updates")
}

func TestUpdateCheckerTimesOut(t *testing.T) {
	api := &fakeReleaseAPI{block: true}
	c, _ := newTestUpdateChecker(api, "0.2.17")
	c.timeout = 50 * time.Millisecond

	start := time.Now()
	_, err := c.check(context.Background())
	require.Error(t, err)
	assert.Less(t, time.Since(start), 5*time.Second, "a GitHub that never answers does not hang the request")
}

func TestUpdateCheckerConcurrent(t *testing.T) {
	api := &fakeReleaseAPI{tag: "v0.3.0"}
	c := newUpdateChecker(api, "0.2.17")
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			info, err := c.check(context.Background())
			assert.NoError(t, err)
			assert.Equal(t, "0.3.0", info.Version)
		})
	}
	wg.Wait()
	assert.EqualValues(t, 1, api.calls.Load(), "concurrent requests share one check")
}
