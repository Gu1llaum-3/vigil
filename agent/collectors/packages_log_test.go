//go:build testing && linux

package collectors

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/fxamacker/cbor/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gu1llaum-3/vigil/internal/common"
)

// The error goes to the hub in the snapshot: it must stay valid UTF-8 whatever the cut (an
// invalid string fails the hub's decoding of the whole snapshot), keep the end of the
// command's output, and mask URL credentials.
func TestPendingUpdatesErrorMessage(t *testing.T) {
	accented := strings.Repeat("é", 400) // 800 bytes: any cut may land inside a character
	stderr := accented + "\nErreur : impossible de télécharger https://user:s3cret@repo.example.com/el9/repodata/repomd.xml\nFin de l'erreur"
	fakeFailingCommand(t, "dnf", stderr+"\xff", 1)

	info, err := collectPackagesRedHat(context.Background())
	require.NoError(t, err)
	msg := info.OutdatedError
	assert.True(t, utf8.ValidString(msg), msg)
	assert.Contains(t, msg, "dnf pending updates query failed: exit status 1")
	assert.Contains(t, msg, "Fin de l'erreur", "the end of the output is kept")
	assert.Contains(t, msg, "https://***@repo.example.com/")
	assert.NotContains(t, msg, "s3cret")

	// The hub decodes it.
	raw, err := cbor.Marshal(common.PackageInfo{OutdatedError: msg})
	require.NoError(t, err)
	var back common.PackageInfo
	require.NoError(t, cbor.Unmarshal(raw, &back))
	assert.Equal(t, msg, back.OutdatedError)

	for cut := 290; cut < 310; cut++ {
		assert.True(t, utf8.ValidString(tail(accented[:cut*2]+"x", 300)))
	}
}

func TestPendingUpdatesErrorTimeout(t *testing.T) {
	dir := fakeBinDir(t)
	writeScript(t, dir, "dnf", "#!/bin/sh\nsleep 5\n")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	info, err := collectPackagesRedHat(ctx)
	require.NoError(t, err)
	assert.Contains(t, info.OutdatedError, "did not finish in time")
}

func TestRedactURLCredentials(t *testing.T) {
	for in, want := range map[string]string{
		"https://user:pass@repo.example.com/x":  "https://***@repo.example.com/x",
		"http://token@mirror/x and ftp://a:b@c": "http://***@mirror/x and ftp://***@c",
		"https://repo.example.com/x":            "https://repo.example.com/x",
		"mail me at a@b.c":                      "mail me at a@b.c",
	} {
		assert.Equal(t, want, redactURLCredentials(in), in)
	}
}

// Repository URLs go to the hub too: their credentials are masked.
func TestRepositoryURLCredentialsAreMasked(t *testing.T) {
	dir := t.TempDir()
	list := dir + "/private.list"
	require.NoError(t, os.WriteFile(list, []byte("deb https://user:s3cret@apt.example.com/debian bookworm main\n"), 0o644))
	repos, err := parseAptSourcesFile(list)
	require.NoError(t, err)
	require.Len(t, repos, 1)
	assert.Equal(t, "https://***@apt.example.com/debian", repos[0].URL)
}
