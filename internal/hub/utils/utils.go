// Package utils provides utility functions for the hub.
package utils

import (
	"os"

	app "github.com/Gu1llaum-3/vigil"
)

// GetEnv retrieves an environment variable with the hub prefix, or falls back to the unprefixed key.
func GetEnv(key string) (value string, exists bool) {
	if value, exists = os.LookupEnv(app.HubEnvPrefix + key); exists {
		return value, exists
	}
	return os.LookupEnv(key)
}
