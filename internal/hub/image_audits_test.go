//go:build testing

package hub

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/hub/notifications"
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
		{name: "docker hub three-part tag stays within patch line", input: "postgres:15.2.3", registry: "docker.io", repository: "library/postgres", tag: "15.2.3", policy: imageAuditPolicySemverMinor},
		{name: "ghcr public", input: "ghcr.io/example/app:1.4", registry: "ghcr.io", repository: "example/app", tag: "1.4", policy: imageAuditPolicySemverMinor},
		{name: "third-party registry quay", input: "quay.io/example/app:1.0.0", registry: "quay.io", repository: "example/app", tag: "1.0.0", policy: imageAuditPolicySemverMinor},
		{name: "third-party registry gitlab", input: "registry.gitlab.com/group/project:2.4", registry: "registry.gitlab.com", repository: "group/project", tag: "2.4", policy: imageAuditPolicySemverMinor},
		{name: "self-hosted harbor latest", input: "harbor.example.com/team/svc:latest", registry: "harbor.example.com", repository: "team/svc", tag: "latest", policy: imageAuditPolicyDigestLatest},
		{name: "tag with variant suffix major", input: "postgres:15-alpine", registry: "docker.io", repository: "library/postgres", tag: "15-alpine", policy: imageAuditPolicySemverMajor},
		{name: "tag with variant suffix minor", input: "nginx:1.25-bookworm", registry: "docker.io", repository: "library/nginx", tag: "1.25-bookworm", policy: imageAuditPolicySemverMinor},
		{name: "tag with full semver and variant", input: "node:20.11.1-alpine3.19", registry: "docker.io", repository: "library/node", tag: "20.11.1-alpine3.19", policy: imageAuditPolicySemverMinor},
		{name: "non numeric tag stays unsupported", input: "nginx:stable-alpine", registry: "docker.io", repository: "library/nginx", tag: "stable-alpine", policy: imageAuditPolicyUnsupported},
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
	tag, found := selectAuditTag([]string{"15.2.0", "15.2.4", "15.3.0", "16.0.0"}, "15.2.3", current, imageAuditPolicySemverMinor)
	require.True(t, found)
	require.Equal(t, "15.2.4", tag)

	current, ok = parseNumericVersion("15")
	require.True(t, ok)
	tag, found = selectAuditTag([]string{"15.1.0", "15.3.2", "16.0.0"}, "15", current, imageAuditPolicySemverMajor)
	require.True(t, found)
	require.Equal(t, "15.3.2", tag)
}

func TestSelectAuditTagRespectsVariantSuffix(t *testing.T) {
	current, ok := parseNumericVersion("1.2.3-alpine")
	require.True(t, ok)
	require.Equal(t, "alpine", current.Variant)

	tag, found := selectAuditTag(
		[]string{"1.2.4", "1.2.4-bullseye", "1.2.5-alpine", "1.3.0-alpine", "1.2.6-alpine3.19"},
		"1.2.3-alpine",
		current,
		imageAuditPolicySemverMinor,
	)
	require.True(t, found)
	require.Equal(t, "1.2.5-alpine", tag, "must only consider candidates with the same -alpine suffix")
}

func TestSelectAuditTagPlainTagIgnoresVariants(t *testing.T) {
	current, ok := parseNumericVersion("1.2.3")
	require.True(t, ok)
	require.Equal(t, "", current.Variant)

	tag, found := selectAuditTag(
		[]string{"1.2.3-alpine", "1.2.4-alpine", "1.2.5", "1.3.0-bullseye"},
		"1.2.3",
		current,
		imageAuditPolicySemverMinor,
	)
	require.True(t, found)
	require.Equal(t, "1.2.5", tag)
}

func TestParseNumericVersionRejectsNonNumeric(t *testing.T) {
	for _, tag := range []string{"stable", "stable-alpine", "nightly", "edge", "latest-alpine"} {
		_, ok := parseNumericVersion(tag)
		require.Falsef(t, ok, "tag %q should not parse", tag)
	}
}

func TestParseNumericVersionAcceptsVariants(t *testing.T) {
	cases := []struct {
		tag     string
		major   int
		minor   int
		patch   int
		parts   int
		variant string
	}{
		{tag: "8-jdk-slim", major: 8, parts: 1, variant: "jdk-slim"},
		{tag: "16-bullseye", major: 16, parts: 1, variant: "bullseye"},
		{tag: "1.25-alpine", major: 1, minor: 25, parts: 2, variant: "alpine"},
		{tag: "20.11.1-alpine3.19", major: 20, minor: 11, patch: 1, parts: 3, variant: "alpine3.19"},
		{tag: "v3.12.4-rc1", major: 3, minor: 12, patch: 4, parts: 3, variant: "rc1"},
	}
	for _, tc := range cases {
		t.Run(tc.tag, func(t *testing.T) {
			v, ok := parseNumericVersion(tc.tag)
			require.True(t, ok)
			require.Equal(t, tc.major, v.Major)
			require.Equal(t, tc.minor, v.Minor)
			require.Equal(t, tc.patch, v.Patch)
			require.Equal(t, tc.parts, v.Parts)
			require.Equal(t, tc.variant, v.Variant)
		})
	}
}

func TestResolveImageAuditSemverMinorWithVariantSuffix(t *testing.T) {
	result := resolveImageAudit(context.Background(), mockImageRegistryClient{
		tags: map[string][]string{"docker.io/library/postgres": {
			"15.2.3", "15.2.4", "15.3.0",
			"15.2.3-alpine", "15.2.4-alpine", "15.2.5-alpine", "15.3.0-alpine", "16.0.0-alpine",
		}},
		headDigests: map[string]string{"docker.io/library/postgres:15.2.5-alpine": "sha256:new-alpine"},
	}, imageAuditTarget{
		CurrentRef: "docker.io/library/postgres:15.2.3-alpine",
		Registry:   "docker.io",
		Repository: "library/postgres",
		Tag:        "15.2.3-alpine",
		Policy:     imageAuditPolicySemverMinor,
	})
	require.Equal(t, imageAuditStatusUpdateAvailable, result.Status)
	require.Equal(t, imageAuditLineStatusPatchAvailable, result.LineStatus)
	require.Equal(t, "15.2.5-alpine", result.LatestTag)
	require.Equal(t, "15.2.5-alpine", result.LineLatestTag)
	require.Equal(t, "15.3.0-alpine", result.SameMajorTag)
	require.Equal(t, "16.0.0-alpine", result.OverallTag)
	require.True(t, result.HasMajorUpdate)
	require.Equal(t, "16.0.0-alpine", result.NewMajorTag)
}

func TestResolveImageAuditThirdPartyRegistry(t *testing.T) {
	result := resolveImageAudit(context.Background(), mockImageRegistryClient{
		tags:        map[string][]string{"quay.io/prometheus/prometheus": {"v2.50.0", "v2.50.1", "v2.51.0", "v3.0.0"}},
		headDigests: map[string]string{"quay.io/prometheus/prometheus:v2.50.1": "sha256:new"},
	}, imageAuditTarget{
		CurrentRef: "quay.io/prometheus/prometheus:v2.50.0",
		Registry:   "quay.io",
		Repository: "prometheus/prometheus",
		Tag:        "v2.50.0",
		Policy:     imageAuditPolicySemverMinor,
	})
	require.Equal(t, imageAuditStatusUpdateAvailable, result.Status)
	require.Equal(t, "v2.50.1", result.LineLatestTag)
	require.Equal(t, "v2.51.0", result.SameMajorTag)
	require.Equal(t, "v3.0.0", result.OverallTag)
	require.True(t, result.HasMajorUpdate)
}

func TestResolveImageAuditDigestLatest(t *testing.T) {
	result := resolveImageAudit(context.Background(), mockImageRegistryClient{
		resolvedDigests: map[string]string{"docker.io/library/nginx:latest|amd64": "sha256:new"},
	}, imageAuditTarget{
		CurrentRef:   "docker.io/library/nginx:latest",
		Architecture: "amd64",
		LocalDigest:  "sha256:old-manifest",
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
		LocalDigest:  "sha256:old-manifest",
		Tag:          "latest",
		Policy:       imageAuditPolicyDigestLatest,
	})
	require.Equal(t, imageAuditStatusUpdateAvailable, result.Status)
}

func TestResolveImageAuditDigestLatestUpToDateMatchesManifestDigest(t *testing.T) {
	result := resolveImageAudit(context.Background(), mockImageRegistryClient{
		resolvedDigests: map[string]string{"docker.io/library/nginx:latest|arm64": "sha256:current-manifest"},
	}, imageAuditTarget{
		CurrentRef:   "docker.io/library/nginx:latest",
		Architecture: "arm64",
		LocalImageID: "sha256:local-config",
		LocalDigest:  "sha256:current-manifest",
		Tag:          "latest",
		Policy:       imageAuditPolicyDigestLatest,
	})
	require.Equal(t, imageAuditStatusUpToDate, result.Status)
}

func TestResolveImageAuditDigestLatestNotFooledByConfigDigest(t *testing.T) {
	// Regression: the local config digest (docker image inspect .Id) must not be
	// compared against the registry manifest digest — they are different things
	// and would never match for an up-to-date image.
	result := resolveImageAudit(context.Background(), mockImageRegistryClient{
		resolvedDigests: map[string]string{"docker.io/library/nginx:latest|amd64": "sha256:remote-manifest"},
	}, imageAuditTarget{
		CurrentRef:   "docker.io/library/nginx:latest",
		Architecture: "amd64",
		LocalImageID: "sha256:local-config",
		LocalDigest:  "sha256:remote-manifest",
		Tag:          "latest",
		Policy:       imageAuditPolicyDigestLatest,
	})
	require.Equal(t, imageAuditStatusUpToDate, result.Status)
}

func TestResolveImageAuditDigestLatestUnknownWithoutLocalDigest(t *testing.T) {
	result := resolveImageAudit(context.Background(), mockImageRegistryClient{
		resolvedDigests: map[string]string{"docker.io/library/nginx:latest|arm64": "sha256:remote-image"},
	}, imageAuditTarget{
		CurrentRef:   "docker.io/library/nginx:latest",
		Architecture: "arm64",
		LocalImageID: "sha256:local-config-only",
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
	require.Equal(t, imageAuditLineStatusPatchAvailable, result.LineStatus)
	require.Equal(t, "15.2.4", result.LatestTag)
	require.Equal(t, "15.2.4", result.LineLatestTag)
	require.Equal(t, "15.3.0", result.SameMajorTag)
	require.Equal(t, "15.3.0", result.OverallTag)
}

func TestResolveImageAuditPinnedTagTracksPatchLineAndShowsNewMajor(t *testing.T) {
	result := resolveImageAudit(context.Background(), mockImageRegistryClient{
		tags:        map[string][]string{"ghcr.io/mealie-recipes/mealie": {"v2.2.0", "v2.2.5", "v2.8.0", "v3.15.2"}},
		headDigests: map[string]string{"ghcr.io/mealie-recipes/mealie:v2.2.5": "sha256:new"},
	}, imageAuditTarget{
		CurrentRef: "ghcr.io/mealie-recipes/mealie:v2.2.0",
		Registry:   "ghcr.io",
		Repository: "mealie-recipes/mealie",
		Tag:        "v2.2.0",
		Policy:     imageAuditPolicySemverMinor,
	})
	require.Equal(t, imageAuditStatusUpdateAvailable, result.Status)
	require.Equal(t, imageAuditLineStatusPatchAvailable, result.LineStatus)
	require.Equal(t, "v2.2.5", result.LatestTag)
	require.Equal(t, "v2.2.5", result.LineLatestTag)
	require.Equal(t, "v2.8.0", result.SameMajorTag)
	require.Equal(t, "v3.15.2", result.OverallTag)
	require.True(t, result.HasMajorUpdate)
	require.Equal(t, "v3.15.2", result.NewMajorTag)
}

func TestResolveImageAuditUpToDatePatchLineStillShowsNewMajor(t *testing.T) {
	result := resolveImageAudit(context.Background(), mockImageRegistryClient{
		tags:        map[string][]string{"ghcr.io/mealie-recipes/mealie": {"v2.2.5", "v2.8.0", "v3.15.2"}},
		headDigests: map[string]string{"ghcr.io/mealie-recipes/mealie:v2.2.5": "sha256:current"},
		resolvedDigests: map[string]string{"ghcr.io/mealie-recipes/mealie:v2.2.5|amd64": "sha256:current"},
	}, imageAuditTarget{
		CurrentRef: "ghcr.io/mealie-recipes/mealie:v2.2.5",
		Registry:   "ghcr.io",
		Repository: "mealie-recipes/mealie",
		Tag:        "v2.2.5",
		LocalDigest:  "sha256:current",
		Architecture: "amd64",
		Policy:     imageAuditPolicySemverMinor,
	})
	require.Equal(t, imageAuditStatusUpToDate, result.Status)
	require.Equal(t, imageAuditStatusUpToDate, result.LineStatus)
	require.Equal(t, "v2.2.5", result.LineLatestTag)
	require.Equal(t, "v2.8.0", result.SameMajorTag)
	require.Equal(t, "v3.15.2", result.OverallTag)
	require.True(t, result.HasMajorUpdate)
	require.Equal(t, "v3.15.2", result.NewMajorTag)
}

func TestResolveImageAuditFloatingMinorTagUsesResolvedDigestForUpToDate(t *testing.T) {
	result := resolveImageAudit(context.Background(), mockImageRegistryClient{
		tags: map[string][]string{"docker.io/library/nginx": {"1.29", "1.29.8", "1.30.0"}},
		headDigests: map[string]string{"docker.io/library/nginx:1.29.8": "sha256:head-current"},
		resolvedDigests: map[string]string{"docker.io/library/nginx:1.29|amd64": "sha256:current"},
	}, imageAuditTarget{
		CurrentRef:   "docker.io/library/nginx:1.29",
		Registry:     "docker.io",
		Repository:   "library/nginx",
		Tag:          "1.29",
		LocalDigest:  "sha256:current",
		Architecture: "amd64",
		Policy:       imageAuditPolicySemverMinor,
	})
	require.Equal(t, imageAuditStatusUpToDate, result.Status)
	require.Equal(t, imageAuditStatusUpToDate, result.LineStatus)
	require.Equal(t, "1.29.8", result.LineLatestTag)
	require.Equal(t, "1.30.0", result.SameMajorTag)
	require.Equal(t, "1.30.0", result.OverallTag)
	require.False(t, result.HasMajorUpdate)
}

func TestResolveImageAuditPinnedTagDetectsRebuiltDigest(t *testing.T) {
	result := resolveImageAudit(context.Background(), mockImageRegistryClient{
		tags: map[string][]string{"docker.io/library/nginx": {"1.2.5", "1.8.0", "2.0.0"}},
		headDigests: map[string]string{"docker.io/library/nginx:1.2.5": "sha256:remote-rebuilt"},
		resolvedDigests: map[string]string{"docker.io/library/nginx:1.2.5|amd64": "sha256:remote-rebuilt"},
	}, imageAuditTarget{
		CurrentRef:   "docker.io/library/nginx:1.2.5",
		Registry:     "docker.io",
		Repository:   "library/nginx",
		Tag:          "1.2.5",
		LocalDigest:  "sha256:local-old",
		Architecture: "amd64",
		Policy:       imageAuditPolicySemverMinor,
	})
	require.Equal(t, imageAuditStatusUpdateAvailable, result.Status)
	require.Equal(t, imageAuditLineStatusTagRebuilt, result.LineStatus)
	require.Equal(t, "1.2.5", result.LineLatestTag)
	require.Equal(t, "1.8.0", result.SameMajorTag)
	require.Equal(t, "2.0.0", result.OverallTag)
	require.True(t, result.HasMajorUpdate)
	require.Equal(t, "2.0.0", result.NewMajorTag)
	require.Equal(t, "sha256:remote-rebuilt", result.LatestImageID)
}

func TestResolveImageAuditRegistryFailure(t *testing.T) {
	result := resolveImageAudit(context.Background(), mockImageRegistryClient{err: errors.New("boom")}, imageAuditTarget{
		CurrentRef: "docker.io/library/nginx:latest",
		Policy:     imageAuditPolicyDigestLatest,
	})
	require.Equal(t, imageAuditStatusCheckFailed, result.Status)
	require.Equal(t, "boom", result.Error)
}

func TestBuildImageNotificationTargets(t *testing.T) {
	result := imageAuditResult{
		Target: imageAuditTarget{Tag: "1.2.5"},
		LineStatus: imageAuditLineStatusPatchAvailable,
		LineLatestTag: "1.2.7",
		SameMajorTag: "1.3.0",
		NewMajorTag: "2.0.0",
	}

	targets := buildImageNotificationTargets(result)
	require.Equal(t, []imageNotificationTarget{
		{Scope: "line", Tag: "1.2.7"},
		{Scope: "same_major", Tag: "1.3.0"},
		{Scope: "new_major", Tag: "2.0.0"},
	}, targets)
	require.Equal(t, "line:1.2.7|same_major:1.3.0|new_major:2.0.0", buildImageNotificationSignature(targets))
}

func TestBuildImageNotificationTargetsIgnoresRebuiltOnlyChange(t *testing.T) {
	result := imageAuditResult{
		Target: imageAuditTarget{Tag: "1.2.5"},
		LineStatus: imageAuditLineStatusTagRebuilt,
		LineLatestTag: "1.2.5",
		SameMajorTag: "1.2.5",
		NewMajorTag: "",
	}

	targets := buildImageNotificationTargets(result)
	require.Empty(t, targets)
	require.Empty(t, buildImageNotificationSignature(targets))
}

func TestUpsertContainerImageAuditNotifiesOnlyWhenSignatureChanges(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	agent, err := createTestRecord(testApp, "agents", map[string]any{
		"name":        "host-1",
		"token":       "token-1",
		"fingerprint": "fingerprint-1",
		"status":      "connected",
	})
	require.NoError(t, err)

	result := imageAuditResult{
		Target: imageAuditTarget{
			AgentID:       agent.Id,
			ContainerID:   "container-1",
			ContainerName: "web",
			CurrentRef:    "docker.io/library/nginx:1.2.5",
			Tag:           "1.2.5",
		},
		Status:        imageAuditStatusUpdateAvailable,
		LineStatus:    imageAuditLineStatusPatchAvailable,
		LineLatestTag: "1.2.7",
		SameMajorTag:  "1.3.0",
		OverallTag:    "2.0.0",
		NewMajorTag:   "2.0.0",
		HasMajorUpdate: true,
		CheckedAt:     time.Now().UTC(),
	}

	notified, err := hub.upsertContainerImageAudit(result)
	require.NoError(t, err)
	require.True(t, notified)

	rec, err := hub.FindFirstRecordByFilter(containerImageAuditsCollection, "agent = {:agent} && container_id = {:container_id}", map[string]any{"agent": agent.Id, "container_id": "container-1"})
	require.NoError(t, err)
	require.Equal(t, "line:1.2.7|same_major:1.3.0|new_major:2.0.0", rec.GetString("last_notified_signature"))
	require.NotEmpty(t, rec.GetString("last_notified_at"))

	notified, err = hub.upsertContainerImageAudit(result)
	require.NoError(t, err)
	require.False(t, notified)

	result.LineLatestTag = "1.2.8"
	result.CheckedAt = result.CheckedAt.Add(time.Hour)
	notified, err = hub.upsertContainerImageAudit(result)
	require.NoError(t, err)
	require.True(t, notified)

	rec, err = hub.FindFirstRecordByFilter(containerImageAuditsCollection, "agent = {:agent} && container_id = {:container_id}", map[string]any{"agent": agent.Id, "container_id": "container-1"})
	require.NoError(t, err)
	require.Equal(t, "line:1.2.8|same_major:1.3.0|new_major:2.0.0", rec.GetString("last_notified_signature"))

	result.LineStatus = imageAuditStatusUpToDate
	result.LineLatestTag = "1.2.5"
	result.SameMajorTag = "1.2.5"
	result.NewMajorTag = ""
	result.CheckedAt = result.CheckedAt.Add(time.Hour)
	notified, err = hub.upsertContainerImageAudit(result)
	require.NoError(t, err)
	require.False(t, notified)

	rec, err = hub.FindFirstRecordByFilter(containerImageAuditsCollection, "agent = {:agent} && container_id = {:container_id}", map[string]any{"agent": agent.Id, "container_id": "container-1"})
	require.NoError(t, err)
	require.Empty(t, rec.GetString("last_notified_signature"))
	require.Empty(t, rec.GetString("last_notified_at"))
}

func TestRenderContainerImageUpdateNotificationMessage(t *testing.T) {
	title, body, err := notifications.RenderMessage(notifications.Event{
		Kind: notifications.EventContainerImageUpdateAvailable,
		Resource: notifications.ResourceRef{Name: "web"},
		Details: map[string]any{
			"agent_name":     "host-1",
			"current_ref":    "docker.io/library/nginx:1.2.5",
			"update_targets": []string{"patch line 1.2.7", "same major 1.3.0", "new major 2.0.0"},
		},
	})
	require.NoError(t, err)
	require.Equal(t, `Container image update available for "web"`, title)
	require.Contains(t, body, `Container "web" on agent "host-1" uses docker.io/library/nginx:1.2.5.`)
	require.Contains(t, body, `patch line 1.2.7, same major 1.3.0, new major 2.0.0`)
}
