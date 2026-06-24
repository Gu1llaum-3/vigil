//go:build testing

package hub

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestHostOverviewIncludesTags locks the agent free-text tags round-trip: tags set
// on an agent record surface (verbatim) in the host overview/detail payload, and an
// agent without tags yields an empty slice (stable JSON, never null).
func TestHostOverviewIncludesTags(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	tagged, err := createTestRecord(hub, "agents", map[string]any{
		"name":  "web-1",
		"token": "tok-web-1",
		"tags":  []string{"prod", "eu-west"},
	})
	require.NoError(t, err)

	// Re-fetch so we exercise the stored JSON round-trip, not just the in-memory set.
	fetched, err := hub.FindRecordById("agents", tagged.Id)
	require.NoError(t, err)
	require.Equal(t, []string{"prod", "eu-west"}, buildHostOverviewRecord(fetched, nil, nil).Tags)

	bare, err := createTestRecord(hub, "agents", map[string]any{
		"name":  "web-2",
		"token": "tok-web-2",
	})
	require.NoError(t, err)
	bareFetched, err := hub.FindRecordById("agents", bare.Id)
	require.NoError(t, err)
	require.Equal(t, []string{}, buildHostOverviewRecord(bareFetched, nil, nil).Tags)
}
