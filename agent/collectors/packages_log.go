//go:build linux

package collectors

import (
	"errors"
	"log/slog"
	"os/exec"
	"strings"
)

// logPendingUpdatesError logs a failed pending-updates query with the command's own
// error output. A missing package manager is a permanent condition reported on every
// snapshot, so it is logged at debug level only.
func logPendingUpdatesError(tool string, err error) {
	if errors.Is(err, exec.ErrNotFound) {
		slog.Debug(tool+" not found, pending updates unavailable", "err", err)
		return
	}
	attrs := []any{"tool", tool, "err", err}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if stderr := strings.TrimSpace(string(exitErr.Stderr)); stderr != "" {
			const maxLen = 500
			if len(stderr) > maxLen {
				stderr = stderr[:maxLen] + "…"
			}
			attrs = append(attrs, "stderr", stderr)
		}
	}
	slog.Warn("pending updates query failed", attrs...)
}
