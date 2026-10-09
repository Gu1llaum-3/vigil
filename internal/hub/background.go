package hub

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"
)

// backgroundStopTimeout bounds how long the shutdown waits for the background goroutines.
const backgroundStopTimeout = 10 * time.Second

// goBackground runs fn in a goroutine the shutdown waits for (stopBackground), recovering
// from a panic. It refuses (false) once the shutdown has started. For the hub's long-lived
// background work — tickers, monitor checks, the notification dispatcher — which writes to
// the database: run with a raw `go`, it could still write while the database closes.
func (h *Hub) goBackground(subsystem string, fn func()) bool {
	h.bgMu.Lock()
	defer h.bgMu.Unlock()
	if h.bgStopped {
		return false
	}
	h.bgWG.Add(1)
	go func() {
		defer h.bgWG.Done()
		defer func() {
			if r := recover(); r != nil {
				slog.Error("background goroutine recovered from panic", "subsystem", subsystem, "panic", r, "stack", string(debug.Stack()))
			}
		}()
		fn()
	}()
	return true
}

// runBackground runs fn on the calling goroutine, counted like goBackground (for work
// started by someone else's goroutine, such as a cron job). It refuses once the shutdown
// has started.
func (h *Hub) runBackground(fn func()) bool {
	h.bgMu.Lock()
	if h.bgStopped {
		h.bgMu.Unlock()
		return false
	}
	h.bgWG.Add(1)
	h.bgMu.Unlock()
	defer h.bgWG.Done()
	fn()
	return true
}

// stopBackground refuses new background work, cancels backgroundContext and waits for the
// running work, at most timeout (a goroutine stuck on I/O must not hold the shutdown
// forever). It reports whether everything finished.
func (h *Hub) stopBackground(timeout time.Duration) bool {
	h.bgMu.Lock()
	h.bgStopped = true
	h.bgMu.Unlock()
	if h.cancelBg != nil {
		h.cancelBg()
	}
	done := make(chan struct{})
	go func() {
		h.bgWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		slog.Warn("Background work still running at shutdown", "waited", timeout)
		return false
	}
}

// backgroundContext is cancelled when the hub stops.
func (h *Hub) backgroundContext() context.Context {
	if h.bgCtx == nil {
		return context.Background()
	}
	return h.bgCtx
}
