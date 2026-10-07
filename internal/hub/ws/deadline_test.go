//go:build testing

package ws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/common"
	"github.com/lxzan/gws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// silentClient reads frames but never answers pings, like a peer whose packets are lost.
type silentClient struct{ gws.BuiltinEventHandler }

func (silentClient) OnPing(*gws.Conn, []byte) {}

// pongClient answers pings, like the real agent (agent/client.go OnPing).
type pongClient struct{ gws.BuiltinEventHandler }

func (pongClient) OnPing(conn *gws.Conn, payload []byte) { _ = conn.WritePong(payload) }

// busyClient answers pings but handles requests on its read loop, like the real agent:
// while a handler runs (a slow snapshot) it reads nothing, so no pong comes back.
type busyClient struct {
	gws.BuiltinEventHandler
	busy time.Duration
}

func (busyClient) OnPing(conn *gws.Conn, payload []byte) { _ = conn.WritePong(payload) }

func (c busyClient) OnMessage(_ *gws.Conn, message *gws.Message) {
	defer message.Close()
	time.Sleep(c.busy)
}

// connectAgent starts a hub-side endpoint using the real Handler, connects a client with the
// given handler, and returns the hub's WsConn for it.
func connectAgent(t *testing.T, client gws.Event) *WsConn {
	t.Helper()
	conns := make(chan *WsConn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := GetUpgrader().Upgrade(w, r)
		if err != nil {
			return
		}
		wsConn := NewWsConnection(conn)
		conn.Session().Store("wsConn", wsConn)
		conns <- wsConn
		go conn.ReadLoop()
	}))
	t.Cleanup(srv.Close)

	clientConn, _, err := gws.NewClient(client, &gws.ClientOption{Addr: "ws" + strings.TrimPrefix(srv.URL, "http")})
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientConn.NetConn().Close() })
	go clientConn.ReadLoop()

	select {
	case wsConn := <-conns:
		return wsConn
	case <-time.After(5 * time.Second):
		t.Fatal("hub never accepted the connection")
		return nil
	}
}

func withDeadline(t *testing.T, d time.Duration) {
	t.Helper()
	previous := deadlineNanos.Swap(int64(d))
	t.Cleanup(func() { deadlineNanos.Store(previous) })
}

// pingFor pings like manageAgentLifecycle, every interval, until the duration elapses or a
// ping fails.
func pingFor(wsConn *WsConn, interval, duration time.Duration) {
	stop := time.After(duration)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if wsConn.Ping() != nil {
				return
			}
		}
	}
}

func TestSilentAgentHitsReadDeadlineDespitePings(t *testing.T) {
	withDeadline(t, 400*time.Millisecond)
	wsConn := connectAgent(t, silentClient{})

	go pingFor(wsConn, 100*time.Millisecond, 3*time.Second)

	require.Eventually(t, func() bool { return !wsConn.IsConnected() }, 3*time.Second, 20*time.Millisecond,
		"the hub's own pings must not keep a peer that never answers alive")
}

func TestPongingAgentStaysConnected(t *testing.T) {
	withDeadline(t, 400*time.Millisecond)
	wsConn := connectAgent(t, pongClient{})

	pingFor(wsConn, 100*time.Millisecond, 1500*time.Millisecond)

	assert.True(t, wsConn.IsConnected(), "pongs from the agent extend the deadline")
}

func TestBusyAgentIsNotDisconnectedDuringARequest(t *testing.T) {
	withDeadline(t, 400*time.Millisecond)
	wsConn := connectAgent(t, busyClient{busy: 500 * time.Millisecond}) // > deadline, < 2×deadline

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := wsConn.SendRequest(ctx, common.GetHostSnapshot, nil)
	require.NoError(t, err)

	pingFor(wsConn, 100*time.Millisecond, 1500*time.Millisecond)

	assert.True(t, wsConn.IsConnected(), "an agent busy answering the hub's own request is alive")
}

func TestSilentAgentIsClosedOnceItsRequestsAreOver(t *testing.T) {
	withDeadline(t, 400*time.Millisecond)
	wsConn := connectAgent(t, silentClient{})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, err := wsConn.SendRequest(ctx, common.GetHostSnapshot, nil)
	require.NoError(t, err)

	go pingFor(wsConn, 100*time.Millisecond, 4*time.Second)

	// past the 400ms deadline, only the request (500ms + 200ms margin) keeps it open
	time.Sleep(550 * time.Millisecond)
	assert.True(t, wsConn.IsConnected(), "the request extends the deadline")

	require.Eventually(t, func() bool { return !wsConn.IsConnected() }, 4*time.Second, 20*time.Millisecond,
		"once the request has timed out, a peer that never answers is closed")
}

// Frequent polling (a short METRICS_INTERVAL) keeps a request in flight at all times: it must
// still not keep a dead peer alive beyond twice the deadline after it was last heard from.
func TestSilentAgentIsClosedDespitePeriodicRequests(t *testing.T) {
	withDeadline(t, 400*time.Millisecond)
	wsConn := connectAgent(t, silentClient{})

	stop := make(chan struct{})
	defer close(stop)
	go func() {
		// more often than timeout + margin: a request is always in flight
		ticker := time.NewTicker(150 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
				_, _ = wsConn.SendRequest(ctx, common.GetHostMetrics, nil)
				<-ctx.Done()
				cancel()
			}
		}
	}()
	go pingFor(wsConn, 100*time.Millisecond, 5*time.Second)

	require.Eventually(t, func() bool { return !wsConn.IsConnected() }, 5*time.Second, 20*time.Millisecond)
}

// A request waiting on a connection that closes must fail right away, not at its timeout.
func TestPendingRequestFailsWhenConnectionCloses(t *testing.T) {
	wsConn := connectAgent(t, silentClient{})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	errs := make(chan error, 1)
	go func() {
		_, err := wsConn.GetHostSnapshot(ctx)
		errs <- err
	}()
	require.Eventually(t, func() bool { return wsConn.requestManager.hasPendingRequests() }, 5*time.Second, 10*time.Millisecond)
	_ = wsConn.conn.Load().NetConn().Close() // the network drops: the read loop ends, OnClose runs

	select {
	case err := <-errs:
		assert.ErrorIs(t, err, gws.ErrConnClosed)
	case <-time.After(5 * time.Second):
		t.Fatal("the pending request waited for its timeout instead of failing on close")
	}
}
