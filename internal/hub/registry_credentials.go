package hub

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/pocketbase/dbx"
)

const (
	registryCredentialsCollection = "registry_credentials"
	credentialsKeyFilename        = "credentials.key"
	credentialsKeySize            = 32 // AES-256
	redactedSecretMarker          = "**REDACTED**"
)

// loadOrCreateCredentialsKey reads <dataDir>/credentials.key, generating it
// (32 random bytes, mode 0600) on first run. The key encrypts registry
// credential secrets at rest.
func loadOrCreateCredentialsKey(dataDir string) ([]byte, error) {
	keyPath := path.Join(dataDir, credentialsKeyFilename)
	existing, err := os.ReadFile(keyPath)
	if err == nil {
		if len(existing) != credentialsKeySize {
			return nil, fmt.Errorf("%s has unexpected size %d (want %d)", keyPath, len(existing), credentialsKeySize)
		}
		return existing, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to read %s: %w", keyPath, err)
	}

	key := make([]byte, credentialsKeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("failed to generate credentials key: %w", err)
	}
	if err := os.WriteFile(keyPath, key, 0o600); err != nil {
		return nil, fmt.Errorf("failed to write credentials key to %s: %w", keyPath, err)
	}
	return key, nil
}

// encryptSecret returns the AES-GCM ciphertext and nonce for plaintext.
func encryptSecret(key, plaintext []byte) (ciphertext, nonce []byte, err error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	return gcm.Seal(nil, nonce, plaintext, nil), nonce, nil
}

// decryptSecret returns the plaintext for an AES-GCM ciphertext + nonce pair.
func decryptSecret(key, ciphertext, nonce []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, errors.New("invalid nonce size")
	}
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// hubRegistryKeychain resolves registry credentials stored in PocketBase. When
// no credential matches the requested registry it returns authn.Anonymous so a
// MultiKeychain can fall through to the next provider (host docker config).
type hubRegistryKeychain struct {
	h *Hub
}

func (k hubRegistryKeychain) Resolve(target authn.Resource) (authn.Authenticator, error) {
	if k.h == nil || len(k.h.credentialsKey) == 0 {
		return authn.Anonymous, nil
	}
	registry := normalizeRegistry(target.RegistryStr())
	rec, err := k.h.FindFirstRecordByFilter(
		registryCredentialsCollection,
		"registry = {:registry}",
		dbx.Params{"registry": registry},
	)
	if err != nil {
		// Not found → let the next keychain try.
		return authn.Anonymous, nil
	}

	ciphertext, err := base64.StdEncoding.DecodeString(rec.GetString("password_ciphertext"))
	if err != nil {
		return authn.Anonymous, nil
	}
	nonce, err := base64.StdEncoding.DecodeString(rec.GetString("password_nonce"))
	if err != nil {
		return authn.Anonymous, nil
	}
	plaintext, err := decryptSecret(k.h.credentialsKey, ciphertext, nonce)
	if err != nil {
		return authn.Anonymous, nil
	}

	return &authn.Basic{
		Username: rec.GetString("username"),
		Password: string(plaintext),
	}, nil
}

// registryKeychain returns the multi-keychain used by image audits: hub-stored
// credentials first, then the host's docker config keychain, then anonymous.
func (h *Hub) registryKeychain() authn.Keychain {
	return authn.NewMultiKeychain(hubRegistryKeychain{h: h}, authn.DefaultKeychain)
}
