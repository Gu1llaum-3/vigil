//go:build linux

package collectors

import (
	"context"
	"errors"
	"log/slog"
	"os/exec"
	"regexp"
	"strings"
	"unicode/utf8"
)

// pendingUpdatesError logs a failed pending-updates query with the command's own error
// output, and returns the message the snapshot carries (PackageInfo.OutdatedError) so the
// hub shows the host's updates as unknown rather than none. A missing package manager is a
// permanent condition reported on every snapshot, so it is logged at debug level only.
//
// The message is shown to every hub user: credentials in URLs are masked, and it is valid
// UTF-8 (the hub rejects a snapshot holding invalid UTF-8, and apt/dnf errors follow the
// host's locale).
func pendingUpdatesError(ctx context.Context, tool string, err error) string {
	if errors.Is(err, exec.ErrNotFound) {
		slog.Debug(tool+" not found, pending updates unavailable", "err", err)
		return tool + " not found"
	}
	attrs := []any{"tool", tool, "err", err}
	msg := tool + " pending updates query failed: " + err.Error()
	if ctxErr := ctx.Err(); ctxErr != nil {
		msg = tool + " pending updates query did not finish in time (" + ctxErr.Error() + ")"
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if stderr := strings.TrimSpace(strings.ToValidUTF8(string(exitErr.Stderr), "�")); stderr != "" {
			attrs = append(attrs, "stderr", tail(stderr, 500))
			// The cause is usually at the end of apt/dnf's output.
			msg += ": " + tail(stderr, 300)
		}
	}
	slog.Warn("pending updates query failed", attrs...)
	return redactURLCredentials(msg)
}

// tail returns the last maxLen bytes of s, cut on a character boundary.
func tail(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	cut := len(s) - maxLen
	for cut < len(s) && !utf8.RuneStart(s[cut]) {
		cut++
	}
	return "…" + s[cut:]
}

var urlCredentials = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/@\s]+@`)

// redactURLCredentials masks the user:password part of the URLs in s. A token in a URL's
// path or query cannot be told apart from the rest and is left as is.
func redactURLCredentials(s string) string {
	return urlCredentials.ReplaceAllString(s, "${1}***@")
}
