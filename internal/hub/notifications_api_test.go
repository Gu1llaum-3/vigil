//go:build testing

package hub

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/stretchr/testify/require"
)

func TestNotificationRuleChannelsFieldSupportsMultipleValues(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	collection, err := testApp.FindCollectionByNameOrId("notification_rules")
	require.NoError(t, err)

	field, ok := collection.Fields.GetByName("channels").(*core.RelationField)
	require.True(t, ok)
	require.Greater(t, field.MaxSelect, 1)

	channelOne, err := createTestRecord(testApp, "notification_channels", map[string]any{
		"name":    "channel-one",
		"kind":    "webhook",
		"enabled": true,
		"config":  map[string]any{"url": "https://example.com/one"},
	})
	require.NoError(t, err)

	channelTwo, err := createTestRecord(testApp, "notification_channels", map[string]any{
		"name":    "channel-two",
		"kind":    "webhook",
		"enabled": true,
		"config":  map[string]any{"url": "https://example.com/two"},
	})
	require.NoError(t, err)

	rule, err := createTestRecord(testApp, "notification_rules", map[string]any{
		"name":             "multi-channel-rule",
		"enabled":          true,
		"events":           []string{"monitor.down", "agent.offline"},
		"channels":         []string{channelOne.Id, channelTwo.Id},
		"min_severity":     "info",
		"throttle_seconds": 0,
	})
	require.NoError(t, err)

	stored, err := testApp.FindRecordById("notification_rules", rule.Id)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{channelOne.Id, channelTwo.Id}, stored.GetStringSlice("channels"))
}
