//go:build testing

package hub

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type mockImageRegistryClient struct {
	headDigests     map[string]string
	imageIDs        map[string]string
	resolvedDigests map[string]string
	tags            map[string][]string
	err             error
}

func (m mockImageRegistryClient) HeadDigest(ctx context.Context, imageRef string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.headDigests[imageRef], nil
}

func (m mockImageRegistryClient) ResolvedDigest(ctx context.Context, imageRef, architecture string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.resolvedDigests[imageRef+"|"+architecture], nil
}

func (m mockImageRegistryClient) ListTags(ctx context.Context, repository string) ([]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.tags[repository], nil
}

func TestNormalizeImageAuditRef(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		registry   string
		repository string
		tag        string
		policy     string
	}{
		{name: "docker hub latest", input: "nginx:latest", registry: "docker.io", repository: "library/nginx", tag: "latest", policy: imageAuditPolicyDigestLatest},
		{name: "docker hub major", input: "postgres:15", registry: "docker.io", repository: "library/postgres", tag: "15", policy: imageAuditPolicySemverMajor},
		{name: "docker hub three-part tag stays within major", input: "postgres:15.2.3", registry: "docker.io", repository: "library/postgres", tag: "15.2.3", policy: imageAuditPolicySemverMajor},
		{name: "ghcr public", input: "ghcr.io/example/app:1.4", registry: "ghcr.io", repository: "example/app", tag: "1.4", policy: imageAuditPolicySemverMinor},
		{name: "unsupported registry", input: "quay.io/example/app:1.0.0", registry: "quay.io", repository: "example/app", tag: "1.0.0", policy: imageAuditPolicyUnsupported},
		{name: "unsupported tag", input: "nginx:stable-alpine", registry: "docker.io", repository: "library/nginx", tag: "stable-alpine", policy: imageAuditPolicyUnsupported},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			registry, repository, tag, policy, err := normalizeImageAuditRef(tc.input)
			require.NoError(t, err)
			require.Equal(t, tc.registry, registry)
			require.Equal(t, tc.repository, repository)
			require.Equal(t, tc.tag, tag)
			require.Equal(t, tc.policy, policy)
		})
	}
}

func TestSelectAuditTag(t *testing.T) {
	current, ok := parseNumericVersion("15.2.3")
	require.True(t, ok)
	tag, found := selectAuditTag([]string{"15.2.0", "15.2.4", "15.3.0", "16.0.0"}, current, imageAuditPolicySemverMinor)
	require.True(t, found)
	require.Equal(t, "15.2.4", tag)

	current, ok = parseNumericVersion("15")
	require.True(t, ok)
	tag, found = selectAuditTag([]string{"15.1.0", "15.3.2", "16.0.0"}, current, imageAuditPolicySemverMajor)
	require.True(t, found)
	require.Equal(t, "15.3.2", tag)
}

func TestResolveImageAuditDigestLatest(t *testing.T) {
	result := resolveImageAudit(context.Background(), mockImageRegistryClient{
		resolvedDigests: map[string]string{"docker.io/library/nginx:latest|amd64": "sha256:new"},
	}, imageAuditTarget{
		CurrentRef:   "docker.io/library/nginx:latest",
		Architecture: "amd64",
		LocalImageID: "sha256:old-image",
		Tag:          "latest",
		Policy:       imageAuditPolicyDigestLatest,
	})
	require.Equal(t, imageAuditStatusUpdateAvailable, result.Status)
	require.Equal(t, "sha256:new", result.LatestDigest)
	require.Equal(t, "sha256:new", result.LatestImageID)
}

func TestResolveImageAuditDigestLatestChecksResolvedRemoteDigestForPlatform(t *testing.T) {
	result := resolveImageAudit(context.Background(), mockImageRegistryClient{
		resolvedDigests: map[string]string{"docker.io/library/nginx:latest|arm64": "sha256:new"},
	}, imageAuditTarget{
		CurrentRef:   "docker.io/library/nginx:latest",
		Architecture: "arm64",
		LocalImageID: "sha256:old-image",
		Tag:          "latest",
		Policy:       imageAuditPolicyDigestLatest,
	})
	require.Equal(t, imageAuditStatusUpdateAvailable, result.Status)
}

func TestResolveImageAuditDigestLatestUsesRemoteImageIDForPlatform(t *testing.T) {
	result := resolveImageAudit(context.Background(), mockImageRegistryClient{
		resolvedDigests: map[string]string{"docker.io/library/nginx:latest|arm64": "sha256:local-image"},
	}, imageAuditTarget{
		CurrentRef:   "docker.io/library/nginx:latest",
		Architecture: "arm64",
		LocalImageID: "sha256:local-image",
		Tag:          "latest",
		Policy:       imageAuditPolicyDigestLatest,
	})
	require.Equal(t, imageAuditStatusUpToDate, result.Status)
}

func TestResolveImageAuditDigestLatestUnknownWithoutLocalImageID(t *testing.T) {
	result := resolveImageAudit(context.Background(), mockImageRegistryClient{
		resolvedDigests: map[string]string{"docker.io/library/nginx:latest|arm64": "sha256:remote-image"},
	}, imageAuditTarget{
		CurrentRef:   "docker.io/library/nginx:latest",
		Architecture: "arm64",
		Tag:          "latest",
		Policy:       imageAuditPolicyDigestLatest,
	})
	require.Equal(t, imageAuditStatusUnknown, result.Status)
}

func TestResolveImageAuditSemverMinor(t *testing.T) {
	result := resolveImageAudit(context.Background(), mockImageRegistryClient{
		tags:        map[string][]string{"docker.io/library/postgres": {"15.2.3", "15.2.4", "15.3.0"}},
		headDigests: map[string]string{"docker.io/library/postgres:15.2.4": "sha256:new"},
	}, imageAuditTarget{
		CurrentRef: "docker.io/library/postgres:15.2.3",
		Registry:   "docker.io",
		Repository: "library/postgres",
		Tag:        "15.2.3",
		Policy:     imageAuditPolicySemverMinor,
	})
	require.Equal(t, imageAuditStatusUpdateAvailable, result.Status)
	require.Equal(t, "15.2.4", result.LatestTag)
}

func TestResolveImageAuditThreePartTagTracksLatestWithinMajor(t *testing.T) {
	result := resolveImageAudit(context.Background(), mockImageRegistryClient{
		tags:        map[string][]string{"ghcr.io/mealie-recipes/mealie": {"v2.2.0", "v2.8.0", "v3.15.2"}},
		headDigests: map[string]string{"ghcr.io/mealie-recipes/mealie:v2.8.0": "sha256:new"},
	}, imageAuditTarget{
		CurrentRef: "ghcr.io/mealie-recipes/mealie:v2.2.0",
		Registry:   "ghcr.io",
		Repository: "mealie-recipes/mealie",
		Tag:        "v2.2.0",
		Policy:     imageAuditPolicySemverMajor,
	})
	require.Equal(t, imageAuditStatusUpdateAvailable, result.Status)
	require.Equal(t, "v2.8.0", result.LatestTag)
}

func TestResolveImageAuditRegistryFailure(t *testing.T) {
	result := resolveImageAudit(context.Background(), mockImageRegistryClient{err: errors.New("boom")}, imageAuditTarget{
		CurrentRef: "docker.io/library/nginx:latest",
		Policy:     imageAuditPolicyDigestLatest,
	})
	require.Equal(t, imageAuditStatusCheckFailed, result.Status)
	require.Equal(t, "boom", result.Error)
}
