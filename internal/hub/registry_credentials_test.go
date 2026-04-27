//go:build testing

package hub

import (
	"crypto/rand"
	"encoding/base64"
	"io"
	"os"
	"path"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/stretchr/testify/require"
)

func TestEncryptSecretRoundTrip(t *testing.T) {
	key := make([]byte, credentialsKeySize)
	_, err := io.ReadFull(rand.Reader, key)
	require.NoError(t, err)

	plaintext := []byte("super-secret-token-42")
	ciphertext, nonce, err := encryptSecret(key, plaintext)
	require.NoError(t, err)
	require.NotEqual(t, plaintext, ciphertext)
	require.NotEmpty(t, nonce)

	decrypted, err := decryptSecret(key, ciphertext, nonce)
	require.NoError(t, err)
	require.Equal(t, plaintext, decrypted)
}

func TestEncryptSecretWrongKeyFails(t *testing.T) {
	keyA := make([]byte, credentialsKeySize)
	keyB := make([]byte, credentialsKeySize)
	_, _ = io.ReadFull(rand.Reader, keyA)
	_, _ = io.ReadFull(rand.Reader, keyB)

	ciphertext, nonce, err := encryptSecret(keyA, []byte("hello"))
	require.NoError(t, err)

	_, err = decryptSecret(keyB, ciphertext, nonce)
	require.Error(t, err)
}

func TestLoadOrCreateCredentialsKeyPersistsAndReloads(t *testing.T) {
	dir := t.TempDir()
	first, err := loadOrCreateCredentialsKey(dir)
	require.NoError(t, err)
	require.Len(t, first, credentialsKeySize)

	info, err := os.Stat(path.Join(dir, credentialsKeyFilename))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	second, err := loadOrCreateCredentialsKey(dir)
	require.NoError(t, err)
	require.Equal(t, first, second)
}

func TestHubRegistryKeychainResolvesStoredCredential(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	hub.credentialsKey = make([]byte, credentialsKeySize)
	_, err = io.ReadFull(rand.Reader, hub.credentialsKey)
	require.NoError(t, err)

	ciphertext, nonce, err := encryptSecret(hub.credentialsKey, []byte("ghp_xxx"))
	require.NoError(t, err)
	_, err = createTestRecord(testApp, registryCredentialsCollection, map[string]any{
		"name":                "github",
		"registry":            "ghcr.io",
		"username":            "octocat",
		"password_ciphertext": base64.StdEncoding.EncodeToString(ciphertext),
		"password_nonce":      base64.StdEncoding.EncodeToString(nonce),
	})
	require.NoError(t, err)

	ref, err := name.ParseReference("ghcr.io/octocat/app:1.0", name.WeakValidation)
	require.NoError(t, err)
	auth, err := hub.registryKeychain().Resolve(ref.Context())
	require.NoError(t, err)
	cfg, err := auth.Authorization()
	require.NoError(t, err)
	require.Equal(t, "octocat", cfg.Username)
	require.Equal(t, "ghp_xxx", cfg.Password)
}

func TestHubRegistryKeychainFallsThroughOnNoMatch(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	hub.credentialsKey = make([]byte, credentialsKeySize)
	_, _ = io.ReadFull(rand.Reader, hub.credentialsKey)

	ref, err := name.ParseReference("docker.io/library/nginx:latest", name.WeakValidation)
	require.NoError(t, err)
	auth, err := hubRegistryKeychain{h: hub}.Resolve(ref.Context())
	require.NoError(t, err)
	require.Equal(t, authn.Anonymous, auth)
}

func TestHubRegistryKeychainNormalizesDockerHubAlias(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	hub.credentialsKey = make([]byte, credentialsKeySize)
	_, _ = io.ReadFull(rand.Reader, hub.credentialsKey)

	ciphertext, nonce, err := encryptSecret(hub.credentialsKey, []byte("hub-pat"))
	require.NoError(t, err)
	// User stored credential as "docker.io"; the registry resource will report
	// "index.docker.io" — the keychain must normalize before lookup.
	_, err = createTestRecord(testApp, registryCredentialsCollection, map[string]any{
		"name":                "docker hub",
		"registry":            "docker.io",
		"username":            "alice",
		"password_ciphertext": base64.StdEncoding.EncodeToString(ciphertext),
		"password_nonce":      base64.StdEncoding.EncodeToString(nonce),
	})
	require.NoError(t, err)

	ref, err := name.ParseReference("nginx:latest", name.WeakValidation)
	require.NoError(t, err)
	auth, err := hub.registryKeychain().Resolve(ref.Context())
	require.NoError(t, err)
	cfg, err := auth.Authorization()
	require.NoError(t, err)
	require.Equal(t, "alice", cfg.Username)
	require.Equal(t, "hub-pat", cfg.Password)
}
