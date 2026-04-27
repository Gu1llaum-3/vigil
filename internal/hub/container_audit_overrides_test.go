//go:build testing

package hub

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Gu1llaum-3/vigil/internal/common"
	"github.com/stretchr/testify/require"
)

func TestMapOverrideToInternalPolicy(t *testing.T) {
	require.Equal(t, imageAuditPolicyDigestLatest, mapOverrideToInternalPolicy(auditOverrideDigest))
	require.Equal(t, imageAuditPolicySemverMinor, mapOverrideToInternalPolicy(auditOverridePatch))
	require.Equal(t, imageAuditPolicySemverMajor, mapOverrideToInternalPolicy(auditOverrideMinor))
	require.Equal(t, "", mapOverrideToInternalPolicy(auditOverrideDisabled))
	require.Equal(t, "", mapOverrideToInternalPolicy("auto"))
	require.Equal(t, "", mapOverrideToInternalPolicy(""))
}

func setupContainerAuditFixture(t *testing.T, containerName, imageRef string) (*Hub, string) {
	t.Helper()
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	t.Cleanup(func() { cleanupTestHub(hub, testApp) })

	agent, err := createTestRecord(testApp, "agents", map[string]any{
		"name":        "host-1",
		"token":       "token-1",
		"fingerprint": "fingerprint-1",
		"status":      "connected",
	})
	require.NoError(t, err)

	snapshot := common.HostSnapshotResponse{
		Architecture: "amd64",
		Docker: common.DockerInfo{
			State: "available",
			Containers: []common.ContainerInfo{
				{ID: "c1", Name: containerName, Image: imageRef, ImageRef: imageRef},
			},
		},
	}
	payload, err := json.Marshal(snapshot)
	require.NoError(t, err)

	_, err = createTestRecord(testApp, "host_snapshots", map[string]any{
		"agent": agent.Id,
		"data":  string(payload),
	})
	require.NoError(t, err)

	return hub, agent.Id
}

func TestCollectAuditResultsAppliesDisabledOverride(t *testing.T) {
	hub, agentID := setupContainerAuditFixture(t, "web", "docker.io/library/nginx:1.2.3-alpine")

	_, err := createTestRecord(hub.App, containerAuditOverridesCollection, map[string]any{
		"agent":          agentID,
		"container_name": "web",
		"policy":         auditOverrideDisabled,
	})
	require.NoError(t, err)

	results, _, err := hub.collectContainerImageAuditResults(context.Background(), mockImageRegistryClient{})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, imageAuditStatusDisabled, results[0].Status)
}

func TestCollectAuditResultsAppliesDigestOverride(t *testing.T) {
	// Container uses a semver tag — without override it'd take the semver_minor
	// path. The "digest" override forces digest_latest, so we expect the audit
	// to compare against the resolved digest of the current ref.
	hub, agentID := setupContainerAuditFixture(t, "api", "docker.io/library/nginx:1.2.3-alpine")

	_, err := createTestRecord(hub.App, containerAuditOverridesCollection, map[string]any{
		"agent":          agentID,
		"container_name": "api",
		"policy":         auditOverrideDigest,
	})
	require.NoError(t, err)

	mock := mockImageRegistryClient{
		resolvedDigests: map[string]string{
			"docker.io/library/nginx:1.2.3-alpine|amd64": "sha256:remote",
		},
	}
	results, _, err := hub.collectContainerImageAuditResults(context.Background(), mock)
	require.NoError(t, err)
	require.Len(t, results, 1)
	// LocalImageID is empty in this fixture, so digest_latest returns "unknown"
	// — what we care about is that the policy was switched, which we observe
	// via the LatestDigest equal to the mocked resolvedDigest (only the digest
	// path consults resolvedDigests).
	require.Equal(t, imageAuditPolicyDigestLatest, results[0].Target.Policy)
	require.Equal(t, "sha256:remote", results[0].LatestDigest)
}

func TestCollectAuditResultsRescuesUnsupportedTagWithDigestOverride(t *testing.T) {
	// "stable" is non-numeric → auto-deduced as Unsupported. Digest override
	// rescues it by switching to digest_latest.
	hub, agentID := setupContainerAuditFixture(t, "cache", "docker.io/library/redis:stable")

	_, err := createTestRecord(hub.App, containerAuditOverridesCollection, map[string]any{
		"agent":          agentID,
		"container_name": "cache",
		"policy":         auditOverrideDigest,
	})
	require.NoError(t, err)

	mock := mockImageRegistryClient{
		resolvedDigests: map[string]string{
			"docker.io/library/redis:stable|amd64": "sha256:redis-remote",
		},
	}
	results, _, err := hub.collectContainerImageAuditResults(context.Background(), mock)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.NotEqual(t, imageAuditStatusUnsupported, results[0].Status)
	require.Equal(t, imageAuditPolicyDigestLatest, results[0].Target.Policy)
}

func TestCollectAuditResultsWithoutOverridePreservesAutoPolicy(t *testing.T) {
	hub, _ := setupContainerAuditFixture(t, "web", "docker.io/library/nginx:1.2.3-alpine")
	mock := mockImageRegistryClient{
		tags: map[string][]string{"docker.io/library/nginx": {"1.2.3-alpine", "1.2.4-alpine"}},
	}
	results, _, err := hub.collectContainerImageAuditResults(context.Background(), mock)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, imageAuditPolicySemverMinor, results[0].Target.Policy)
}
