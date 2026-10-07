//go:build testing

package hub

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The hub key is read once: agent handshakes reuse it instead of reading and parsing the
// file each time (and a key file that disappears mid-run does not become a new identity).
func TestGetSSHKeyIsCached(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	first, err := hub.GetSSHKey("")
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(testApp.DataDir(), "id_ed25519")))

	again, err := hub.GetSSHKey("")
	require.NoError(t, err)
	assert.Equal(t, first.PublicKey().Marshal(), again.PublicKey().Marshal())
	_, statErr := os.Stat(filepath.Join(testApp.DataDir(), "id_ed25519"))
	assert.True(t, os.IsNotExist(statErr), "no new key may be generated once one is loaded")
}

func TestGetSSHKeyConcurrentUse(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, err := hub.GetSSHKey("")
			assert.NoError(t, err)
		}()
		go func() {
			defer wg.Done()
			_ = hub.publicKey()
		}()
	}
	wg.Wait()
	assert.NotEmpty(t, hub.publicKey())
}
