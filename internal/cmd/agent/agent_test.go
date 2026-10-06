package main

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func TestLoadPublicKeys(t *testing.T) {
	// Generate a test key
	_, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(priv)
	require.NoError(t, err)
	pubKey := ssh.MarshalAuthorizedKey(signer.PublicKey())

	tests := []struct {
		name        string
		opts        cmdOptions
		envVars     map[string]string
		setupFiles  map[string][]byte
		wantErr     bool
		errContains string
	}{
		{
			name: "load key from flag",
			opts: cmdOptions{
				key: string(pubKey),
			},
		},
		{
			name: "load key from env var",
			envVars: map[string]string{
				"KEY": string(pubKey),
			},
		},
		{
			name: "load key from file",
			envVars: map[string]string{
				"KEY_FILE": "testkey.pub",
			},
			setupFiles: map[string][]byte{
				"testkey.pub": pubKey,
			},
		},
		{
			// Without a key the hub challenge can never pass, so fail fast at startup.
			name:        "error when no key is configured",
			wantErr:     true,
			errContains: "no hub public key configured",
		},
		{
			name: "error on empty KEY_FILE",
			envVars: map[string]string{
				"KEY_FILE": "empty.pub",
			},
			setupFiles: map[string][]byte{
				"empty.pub": []byte("# no key here\n\n"),
			},
			wantErr:     true,
			errContains: "no hub public key configured",
		},
		{
			name: "error on empty KEY_FILE path",
			envVars: map[string]string{
				"KEY_FILE": "",
			},
			wantErr:     true,
			errContains: "no hub public key configured",
		},
		{
			// A blank KEY is a misconfiguration, not a request to fall back to KEY_FILE.
			name: "error on whitespace-only KEY",
			envVars: map[string]string{
				"KEY":      "   ",
				"KEY_FILE": "testkey.pub",
			},
			setupFiles: map[string][]byte{
				"testkey.pub": pubKey,
			},
			wantErr:     true,
			errContains: "no hub public key configured",
		},
		{
			name: "error on whitespace-only key flag",
			opts: cmdOptions{
				key: "  \n",
			},
			wantErr:     true,
			errContains: "no hub public key configured",
		},
		{
			name: "error on invalid key file",
			envVars: map[string]string{
				"KEY_FILE": "nonexistent.pub",
			},
			wantErr:     true,
			errContains: "failed to read key file",
		},
		{
			name: "error on invalid key data",
			opts: cmdOptions{
				key: "invalid-key-data",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for test files
			if len(tt.setupFiles) > 0 {
				tmpDir := t.TempDir()
				for name, content := range tt.setupFiles {
					path := filepath.Join(tmpDir, name)
					err := os.WriteFile(path, content, 0600)
					require.NoError(t, err)
					if tt.envVars != nil {
						tt.envVars["KEY_FILE"] = path
					}
				}
			}

			// Isolate from the developer's shell (e.g. KEY exported for make dev).
			for _, k := range []string{"KEY", "KEY_FILE", "VIGIL_AGENT_KEY", "VIGIL_AGENT_KEY_FILE"} {
				t.Setenv(k, "")
				os.Unsetenv(k)
			}

			// Set up environment
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			keys, err := tt.opts.loadPublicKeys()
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			assert.Len(t, keys, 1)
			assert.Equal(t, signer.PublicKey().Type(), keys[0].Type())
		})
	}
}

func TestParseFlags(t *testing.T) {
	// Save original command line arguments and restore after test
	oldArgs := os.Args
	defer func() {
		os.Args = oldArgs
		pflag.CommandLine = pflag.NewFlagSet(os.Args[0], pflag.ExitOnError)
	}()

	tests := []struct {
		name     string
		args     []string
		expected cmdOptions
	}{
		{
			name:     "no flags",
			args:     []string{"cmd"},
			expected: cmdOptions{key: ""},
		},
		{
			name:     "key flag only",
			args:     []string{"cmd", "-key", "testkey"},
			expected: cmdOptions{key: "testkey"},
		},
		{
			name:     "key flag double dash",
			args:     []string{"cmd", "--key", "testkey"},
			expected: cmdOptions{key: "testkey"},
		},
		{
			name:     "key flag short",
			args:     []string{"cmd", "-k", "testkey"},
			expected: cmdOptions{key: "testkey"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset flags for each test
			pflag.CommandLine = pflag.NewFlagSet(tt.args[0], pflag.ExitOnError)
			os.Args = tt.args

			var opts cmdOptions
			opts.parse()
			pflag.Parse()

			assert.Equal(t, tt.expected, opts)
		})
	}
}
