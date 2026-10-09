//go:build testing

package ghupdate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeReleases answers the updater's requests by full URL and records them.
type fakeReleases struct {
	mu       sync.Mutex
	files    map[string][]byte
	status   map[string]int
	requests []string
}

func (f *fakeReleases) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	url := req.URL.String()
	f.requests = append(f.requests, url)
	body, ok := f.files[url]
	status := http.StatusOK
	if code, set := f.status[url]; set {
		status = code
	} else if !ok {
		status = http.StatusNotFound
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(body)), Request: req}, nil
}

const (
	testAPIURL   = "https://api.github.com/repos/owner/repo/releases/latest"
	testDownload = "https://github.com/owner/repo/releases/download/"
	testBinary   = "vigil"
	newBinary    = "#!/bin/sh\necho new\n"
	oldBinary    = "#!/bin/sh\necho old\n"
)

type updateFixture struct {
	t       *testing.T
	client  *fakeReleases
	exe     string
	dataDir string
	release release
	archive []byte
	asset   string
	// rename, when set, replaces os.Rename for the updater.
	rename func(oldpath, newpath string) error
}

// releaseAsset names the platform archive as .goreleaser.yml publishes it (name_template),
// written out rather than derived from archiveSuffix so a rename on either side fails here.
func releaseAssetName(binary string) string {
	return binary + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
}

// newUpdateFixture publishes tag with the assets of a real release (the agent archive
// first, the checksums signature before the checksums) and installs the old binary in a
// temp dir.
func newUpdateFixture(t *testing.T, tag string) *updateFixture {
	return newUpdateFixtureWith(t, tag, testBinary)
}

// newUpdateFixtureWith is newUpdateFixture with the hub binary stored as archived in the
// archive.
func newUpdateFixtureWith(t *testing.T, tag, archived string) *updateFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fixture builds tar.gz archives")
	}
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "archive.tar.gz")
	writeTarGz(t, archivePath, []tarEntry{{name: "LICENSE", body: "MIT"}, {name: archived, body: newBinary}})
	archive, err := os.ReadFile(archivePath)
	require.NoError(t, err)

	f := &updateFixture{
		t:       t,
		client:  &fakeReleases{files: map[string][]byte{}, status: map[string]int{}},
		exe:     filepath.Join(dir, "bin", testBinary),
		dataDir: filepath.Join(dir, "data"),
		archive: archive,
	}
	require.NoError(t, os.MkdirAll(filepath.Dir(f.exe), 0o755))
	require.NoError(t, os.WriteFile(f.exe, []byte(oldBinary), 0o755))

	asset := releaseAssetName(testBinary)
	agentAsset := releaseAssetName(testBinary + "-agent")
	version := strings.TrimPrefix(tag, "v")
	checksums := testBinary + "_" + version + "_checksums.txt"
	sum := sha256.Sum256(archive)
	f.asset = asset
	f.release = release{Tag: tag, Body: "notes", Assets: []*releaseAsset{
		{Name: agentAsset, DownloadUrl: testDownload + tag + "/" + agentAsset},
		{Name: asset, DownloadUrl: testDownload + tag + "/" + asset},
		{Name: testBinary + "-agent_" + version + "_amd64.deb", DownloadUrl: testDownload + tag + "/deb"},
		{Name: checksums + ".sigstore.json", DownloadUrl: testDownload + tag + "/" + checksums + ".sigstore.json"},
		{Name: checksums, DownloadUrl: testDownload + tag + "/checksums.txt"},
	}}
	f.client.files[testDownload+tag+"/"+agentAsset] = []byte("the agent archive")
	f.client.files[testDownload+tag+"/"+asset] = archive
	f.client.files[testDownload+tag+"/checksums.txt"] = fmt.Appendf(nil, "%x  %s\n%x  %s\n", sha256.Sum256([]byte("the agent archive")), agentAsset, sum, asset)
	return f
}

func (f *updateFixture) run(current string, config Config) (bool, error) {
	f.t.Helper()
	raw, err := json.Marshal(f.release)
	require.NoError(f.t, err)
	if _, set := f.client.files[testAPIURL]; !set {
		f.client.files[testAPIURL] = raw
	}
	config.Owner, config.Repo = "owner", "repo"
	config.ArchiveExecutable = testBinary
	config.HttpClient = f.client
	config.DataDir = f.dataDir
	config.Context = context.Background()
	p := &updater{config: config, currentVersion: current, executable: func() (string, error) { return f.exe, nil }, rename: f.rename}
	return p.update()
}

// installed returns the binary now in place, and checks nothing is left beside it.
func (f *updateFixture) installed() string {
	f.t.Helper()
	data, err := os.ReadFile(f.exe)
	require.NoError(f.t, err)
	entries, err := os.ReadDir(filepath.Dir(f.exe))
	require.NoError(f.t, err)
	assert.Len(f.t, entries, 1, "no .old (or other) file left beside the binary")
	tmp, _ := os.ReadDir(f.dataDir)
	assert.Empty(f.t, tmp, "the download directory is removed")
	return string(data)
}

// /releases/latest never returns a prerelease; the beta tags here pin checkUpgrade's
// ordering, which a mirror or a future channel could rely on.
func TestUpdateInstallsANewerRelease(t *testing.T) {
	for _, c := range []struct{ current, tag string }{
		{"0.2.16-beta", "v0.2.17-beta"},
		{"0.2.17-beta", "v0.2.17"},
		{"0.2.17", "v0.3.0"},
	} {
		t.Run(c.current+"→"+c.tag, func(t *testing.T) {
			f := newUpdateFixture(t, c.tag)
			updated, err := f.run(c.current, Config{})
			require.NoError(t, err)
			assert.True(t, updated)
			assert.Equal(t, newBinary, f.installed())
		})
	}
}

func TestUpdateNothingNewer(t *testing.T) {
	for _, current := range []string{"0.3.0", "0.4.0", "v0.3.0"} {
		t.Run(current, func(t *testing.T) {
			f := newUpdateFixture(t, "v0.3.0")
			updated, err := f.run(current, Config{})
			require.NoError(t, err)
			assert.False(t, updated)
			assert.Equal(t, oldBinary, f.installed())
			assert.Equal(t, []string{testAPIURL}, f.client.requests, "nothing downloaded")
		})
	}
}

// The hub archive is picked among the release assets, not the agent's, and is checked
// against the checksums file, not its signature.
func TestUpdatePicksTheHubAssets(t *testing.T) {
	f := newUpdateFixture(t, "v0.3.0")
	updated, err := f.run("0.2.17", Config{})
	require.NoError(t, err)
	assert.True(t, updated)
	assert.Equal(t, newBinary, f.installed())
	assert.Equal(t, []string{testAPIURL, testDownload + "v0.3.0/" + f.asset, testDownload + "v0.3.0/checksums.txt"}, f.client.requests)
}

func TestUpdateWindowsExecutableName(t *testing.T) {
	f := newUpdateFixtureWith(t, "v0.3.0", testBinary+".exe")
	updated, err := f.run("0.2.17", Config{})
	require.NoError(t, err)
	assert.True(t, updated)
	assert.Equal(t, newBinary, f.installed())
}

// A failed replacement puts the running binary back; when even that fails, the previous
// binary is kept (as .old) and named in the error.
func TestUpdateReplaceFailures(t *testing.T) {
	replaceErr := errors.New("disk full")
	// isReplace tells the replacement (extracted binary → exe) from the other renames.
	isReplace := func(f *updateFixture, oldpath, newpath string) bool {
		return newpath == f.exe && !strings.HasSuffix(oldpath, ".old")
	}

	t.Run("reverted", func(t *testing.T) {
		f := newUpdateFixture(t, "v0.3.0")
		f.rename = func(oldpath, newpath string) error {
			if isReplace(f, oldpath, newpath) {
				return replaceErr
			}
			return os.Rename(oldpath, newpath)
		}
		updated, err := f.run("0.2.17", Config{})
		require.ErrorIs(t, err, replaceErr)
		assert.False(t, updated)
		assert.Equal(t, oldBinary, f.installed())
	})

	t.Run("revert fails too", func(t *testing.T) {
		f := newUpdateFixture(t, "v0.3.0")
		f.rename = func(oldpath, newpath string) error {
			if newpath == f.exe {
				return replaceErr // both the replacement and the revert
			}
			return os.Rename(oldpath, newpath)
		}
		_, err := f.run("0.2.17", Config{})
		require.ErrorIs(t, err, replaceErr)
		assert.Contains(t, err.Error(), f.exe+".old")
		old, readErr := os.ReadFile(f.exe + ".old")
		require.NoError(t, readErr, "the previous binary is not deleted")
		assert.Equal(t, oldBinary, string(old))
	})

	t.Run("cross-device copy", func(t *testing.T) {
		f := newUpdateFixture(t, "v0.3.0")
		f.rename = func(oldpath, newpath string) error {
			if isReplace(f, oldpath, newpath) {
				return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: syscall.EXDEV}
			}
			return os.Rename(oldpath, newpath)
		}
		updated, err := f.run("0.2.17", Config{})
		require.NoError(t, err)
		assert.True(t, updated)
		assert.Equal(t, newBinary, f.installed())
	})

	t.Run("cross-device copy fails", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root reads unreadable files")
		}
		f := newUpdateFixture(t, "v0.3.0")
		f.rename = func(oldpath, newpath string) error {
			if isReplace(f, oldpath, newpath) {
				require.NoError(t, os.Chmod(oldpath, 0)) // the copy cannot read it
				return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: syscall.EXDEV}
			}
			return os.Rename(oldpath, newpath)
		}
		_, err := f.run("0.2.17", Config{})
		require.ErrorContains(t, err, "failed replacing the executable")
		assert.Equal(t, oldBinary, f.installed())
	})
}

func TestUpdateMajorVersion(t *testing.T) {
	f := newUpdateFixture(t, "v1.0.0")
	updated, err := f.run("0.2.17-beta", Config{})
	require.NoError(t, err)
	assert.False(t, updated, "a new major version is not installed by default")
	assert.Equal(t, oldBinary, f.installed())
	assert.Equal(t, []string{testAPIURL}, f.client.requests)

	f = newUpdateFixture(t, "v1.0.0")
	updated, err = f.run("0.2.17-beta", Config{AllowMajor: true})
	require.NoError(t, err)
	assert.True(t, updated)
	assert.Equal(t, newBinary, f.installed())
}

// Every failure leaves the running binary in place.
func TestUpdateFailures(t *testing.T) {
	asset := releaseAssetName(testBinary)
	for name, c := range map[string]struct {
		breakIt func(f *updateFixture)
		want    string
	}{
		"checksum mismatch": {func(f *updateFixture) {
			f.client.files[testDownload+"v0.3.0/"+asset] = append([]byte{}, f.archive[:len(f.archive)-1]...)
		}, "checksum mismatch"},
		"no checksum entry": {func(f *updateFixture) {
			f.client.files[testDownload+"v0.3.0/checksums.txt"] = []byte("abc  other.tar.gz\n")
		}, "no checksum entry"},
		"no checksums file": {func(f *updateFixture) {
			f.release.Assets = f.release.Assets[:3]
		}, "no checksums file"},
		"checksums download fails": {func(f *updateFixture) {
			f.client.status[testDownload+"v0.3.0/checksums.txt"] = http.StatusBadGateway
		}, "failed to download checksums"},
		"archive download fails": {func(f *updateFixture) {
			f.client.status[testDownload+"v0.3.0/"+asset] = http.StatusNotFound
		}, "failed to send download file request"},
		"no asset for this platform": {func(f *updateFixture) {
			f.release.Assets[1].Name = "vigil_plan9_mips.tar.gz"
		}, "missing asset"},
		"executable missing from the archive": {func(f *updateFixture) {
			path := filepath.Join(t.TempDir(), "a.tar.gz")
			writeTarGz(t, path, []tarEntry{{name: "LICENSE", body: "MIT"}})
			archive, err := os.ReadFile(path)
			require.NoError(t, err)
			sum := sha256.Sum256(archive)
			f.client.files[testDownload+"v0.3.0/"+asset] = archive
			f.client.files[testDownload+"v0.3.0/checksums.txt"] = fmt.Appendf(nil, "%x  %s\n", sum, asset)
		}, "executable in the extracted path is missing"},
		"invalid release tag": {func(f *updateFixture) {
			f.release.Tag = "latest"
		}, "cannot parse the latest release tag"},
		"API error": {func(f *updateFixture) {
			f.client.status[testAPIURL] = http.StatusForbidden
			f.client.files[testAPIURL] = []byte(`{"message":"API rate limit exceeded"}`)
		}, "failed to fetch latest releases"},
		"malformed API response": {func(f *updateFixture) {
			f.client.files[testAPIURL] = []byte(`<html>`)
		}, "invalid character"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newUpdateFixture(t, "v0.3.0")
			c.breakIt(f)
			var updated bool
			var err error
			require.NotPanics(t, func() { updated, err = f.run("0.2.17", Config{}) })
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.want)
			assert.False(t, updated)
			assert.Equal(t, oldBinary, f.installed())
		})
	}

	t.Run("invalid running version", func(t *testing.T) {
		f := newUpdateFixture(t, "v0.3.0")
		_, err := f.run("dev", Config{})
		require.ErrorContains(t, err, "cannot parse the running version")
		assert.Equal(t, oldBinary, f.installed())
	})
}

// With a mirror, the release metadata and the archive come from it, but the checksums
// always come from GitHub, so a mirror cannot supply both a binary and its digest.
func TestUpdateThroughMirror(t *testing.T) {
	const mirror = "mirror.example"
	f := newUpdateFixture(t, "v0.3.0")
	asset := f.asset
	raw, err := json.Marshal(f.release)
	require.NoError(t, err)
	mirrorAPI := "https://" + mirror + "/repos/owner/repo/releases/latest?api=true"
	mirrorArchive := "https://" + mirror + "/owner/repo/releases/download/v0.3.0/" + asset
	f.client.files[mirrorAPI] = raw
	f.client.files[mirrorArchive] = f.archive
	// Present (so run does not publish the release there) but failing: GitHub's API must
	// not be used.
	f.client.files[testAPIURL] = nil
	f.client.status[testAPIURL] = http.StatusTeapot

	updated, err := f.run("0.2.17", Config{UseMirror: true, MirrorHost: mirror})
	require.NoError(t, err)
	assert.True(t, updated)
	assert.Equal(t, newBinary, f.installed())
	assert.Equal(t, []string{mirrorAPI, mirrorArchive, testDownload + "v0.3.0/checksums.txt"}, f.client.requests)

	// A mirror serving a tampered archive is caught by the GitHub checksums.
	f = newUpdateFixture(t, "v0.3.0")
	f.client.files[mirrorAPI] = raw
	f.client.files[mirrorArchive] = []byte("tampered")
	f.client.files[testAPIURL] = nil
	f.client.status[testAPIURL] = http.StatusTeapot
	_, err = f.run("0.2.17", Config{UseMirror: true, MirrorHost: mirror})
	require.ErrorContains(t, err, "checksum mismatch")
	assert.Equal(t, oldBinary, f.installed())
}
