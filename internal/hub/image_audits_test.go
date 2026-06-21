//go:build testing

package hub

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/hub/notifications"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/stretchr/testify/require"
)

type mockImageRegistryClient struct {
	headDigests     map[string]string
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

func TestApplyTagRegexFilterPreservesCurrentTag(t *testing.T) {
	include := regexp.MustCompile(`^v3\.`)
	out := applyTagRegexFilter([]string{"v2.0.0", "v2.5.0", "v3.0.0", "v3.1.0"}, "v2.0.0", include, nil)
	require.Contains(t, out, "v2.0.0", "current tag must always be preserved")
	require.Contains(t, out, "v3.0.0")
	require.Contains(t, out, "v3.1.0")
	require.NotContains(t, out, "v2.5.0")
}

func TestApplyTagRegexFilterExcludeWins(t *testing.T) {
	exclude := regexp.MustCompile(`-rc\d*$`)
	out := applyTagRegexFilter([]string{"1.0.0", "1.0.1", "1.0.2-rc1"}, "1.0.0", nil, exclude)
	require.Equal(t, []string{"1.0.0", "1.0.1"}, out)
}

func TestApplyTagRegexFilterIncludeAndExcludeCombined(t *testing.T) {
	include := regexp.MustCompile(`^v3\.`)
	exclude := regexp.MustCompile(`-rc\d*$`)
	out := applyTagRegexFilter([]string{"v2.0.0", "v3.0.0", "v3.1.0-rc1", "v3.1.0"}, "v3.0.0", include, exclude)
	require.NotContains(t, out, "v2.0.0")
	require.NotContains(t, out, "v3.1.0-rc1")
	require.Contains(t, out, "v3.0.0")
	require.Contains(t, out, "v3.1.0")
}

func TestResolveImageAuditHonorsTagIncludeFilter(t *testing.T) {
	result := resolveImageAudit(context.Background(), mockImageRegistryClient{
		tags: map[string][]string{"docker.io/library/traefik": {
			"v2.10.0", "v2.11.0", "v3.0.0", "v3.1.0",
		}},
		headDigests: map[string]string{"docker.io/library/traefik:v2.11.0": "sha256:new"},
	}, imageAuditTarget{
		CurrentRef: "docker.io/library/traefik:v2.10.0",
		Registry:   "docker.io",
		Repository: "library/traefik",
		Tag:        "v2.10.0",
		Policy:     imageAuditPolicySemverMajor,
		TagInclude: regexp.MustCompile(`^v2\.`),
	})
	require.Equal(t, imageAuditStatusUpdateAvailable, result.Status)
	require.Equal(t, "v2.11.0", result.LineLatestTag, "must stay on v2.x because of tag_include")
	// Without the include filter, v3.x would have appeared as new major.
	require.False(t, result.HasMajorUpdate)
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
		tag          string
		major        int
		minor        int
		patch        int
		parts        int
		variant      string
		isPrerelease bool
	}{
		{tag: "8-jdk-slim", major: 8, parts: 1, variant: "jdk-slim"},
		{tag: "16-bullseye", major: 16, parts: 1, variant: "bullseye"},
		{tag: "1.25-alpine", major: 1, minor: 25, parts: 2, variant: "alpine"},
		{tag: "20.11.1-alpine3.19", major: 20, minor: 11, patch: 1, parts: 3, variant: "alpine3.19"},
		// rc1/alpha/beta now correctly classified as prereleases (not OS variants).
		{tag: "v3.12.4-rc1", major: 3, minor: 12, patch: 4, parts: 3, variant: "", isPrerelease: true},
		{tag: "v2.0.0-alpha.3", major: 2, minor: 0, patch: 0, parts: 3, variant: "", isPrerelease: true},
		{tag: "v1.0.0-beta", major: 1, minor: 0, patch: 0, parts: 3, variant: "", isPrerelease: true},
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
			require.Equal(t, tc.isPrerelease, v.IsPrerelease)
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
		tags:            map[string][]string{"ghcr.io/mealie-recipes/mealie": {"v2.2.5", "v2.8.0", "v3.15.2"}},
		headDigests:     map[string]string{"ghcr.io/mealie-recipes/mealie:v2.2.5": "sha256:current"},
		resolvedDigests: map[string]string{"ghcr.io/mealie-recipes/mealie:v2.2.5|amd64": "sha256:current"},
	}, imageAuditTarget{
		CurrentRef:   "ghcr.io/mealie-recipes/mealie:v2.2.5",
		Registry:     "ghcr.io",
		Repository:   "mealie-recipes/mealie",
		Tag:          "v2.2.5",
		LocalDigest:  "sha256:current",
		Architecture: "amd64",
		Policy:       imageAuditPolicySemverMinor,
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
		tags:            map[string][]string{"docker.io/library/nginx": {"1.29", "1.29.8", "1.30.0"}},
		headDigests:     map[string]string{"docker.io/library/nginx:1.29.8": "sha256:head-current"},
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
		tags:            map[string][]string{"docker.io/library/nginx": {"1.2.5", "1.8.0", "2.0.0"}},
		headDigests:     map[string]string{"docker.io/library/nginx:1.2.5": "sha256:remote-rebuilt"},
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
		Target:        imageAuditTarget{Tag: "1.2.5"},
		LineStatus:    imageAuditLineStatusPatchAvailable,
		LineLatestTag: "1.2.7",
		SameMajorTag:  "1.3.0",
		NewMajorTag:   "2.0.0",
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
		Target:        imageAuditTarget{Tag: "1.2.5"},
		LineStatus:    imageAuditLineStatusTagRebuilt,
		LineLatestTag: "1.2.5",
		SameMajorTag:  "1.2.5",
		NewMajorTag:   "",
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
		Status:         imageAuditStatusUpdateAvailable,
		LineStatus:     imageAuditLineStatusPatchAvailable,
		LineLatestTag:  "1.2.7",
		SameMajorTag:   "1.3.0",
		OverallTag:     "2.0.0",
		NewMajorTag:    "2.0.0",
		HasMajorUpdate: true,
		CheckedAt:      time.Now().UTC(),
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

func TestImageAuditEventSeverity(t *testing.T) {
	cases := []struct {
		name   string
		result imageAuditResult
		want   string
	}{
		{"major triggers warning", imageAuditResult{HasMajorUpdate: true}, "warning"},
		{"auth failure triggers warning", imageAuditResult{ErrorKind: imageAuditErrorAuth}, "warning"},
		{"definitive client error triggers warning", imageAuditResult{ErrorKind: imageAuditErrorClient}, "warning"},
		{"patch only is info", imageAuditResult{LineStatus: imageAuditLineStatusPatchAvailable}, "info"},
		{"transient registry error stays info", imageAuditResult{ErrorKind: imageAuditErrorRegistry}, "info"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, imageAuditEventSeverity(tc.result))
		})
	}
}

func TestRenderContainerImageUpdateNotificationMessage(t *testing.T) {
	title, body, err := notifications.RenderMessage(notifications.Event{
		Kind:     notifications.EventContainerImageUpdateAvailable,
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

// ── transient-failure preservation (Commit 2) ────────────────────────────────

func transientFail() imageAuditResult {
	return imageAuditResult{Status: imageAuditStatusCheckFailed, ErrorKind: imageAuditErrorTimeout, Error: "context deadline exceeded"}
}

func TestDecideAuditPersistence(t *testing.T) {
	good := imageAuditStatusUpdateAvailable

	// success → written as-is, counter reset
	d := decideAuditPersistence(imageAuditResult{Status: imageAuditStatusUpToDate}, good, 2)
	require.False(t, d.preserveSuccess)
	require.Equal(t, imageAuditStatusUpToDate, d.status)
	require.Equal(t, 0, d.failures)
	require.Equal(t, "", d.lastCheckError)

	// definitive error (auth) → written as-is (not preserved), counter reset
	authFail := imageAuditResult{Status: imageAuditStatusCheckFailed, ErrorKind: imageAuditErrorAuth, Error: "401"}
	d = decideAuditPersistence(authFail, good, 1)
	require.False(t, d.preserveSuccess)
	require.Equal(t, imageAuditStatusCheckFailed, d.status)

	// transient failure with prior good, within grace → preserve last good
	d = decideAuditPersistence(transientFail(), good, 0)
	require.True(t, d.preserveSuccess)
	require.Equal(t, good, d.status)
	require.Equal(t, 1, d.failures)
	require.NotEmpty(t, d.lastCheckError)

	// transient failure reaching the grace limit → escalate to check_failed
	d = decideAuditPersistence(transientFail(), good, imageAuditFailureGraceCycles-1)
	require.False(t, d.preserveSuccess)
	require.Equal(t, imageAuditStatusCheckFailed, d.status)
	require.Equal(t, imageAuditFailureGraceCycles, d.failures)

	// transient failure with no prior good state → check_failed immediately
	d = decideAuditPersistence(transientFail(), "", 0)
	require.False(t, d.preserveSuccess)
	require.Equal(t, imageAuditStatusCheckFailed, d.status)
	require.Equal(t, 1, d.failures)

	// prior status "unknown" carries no data worth preserving → NOT preserved
	d = decideAuditPersistence(transientFail(), imageAuditStatusUnknown, 0)
	require.False(t, d.preserveSuccess, "unknown must not be preserved across transient failures")
	require.Equal(t, imageAuditStatusCheckFailed, d.status)

	// already-failing host keeps failing: stays check_failed and the counter keeps climbing
	d = decideAuditPersistence(transientFail(), imageAuditStatusCheckFailed, 4)
	require.False(t, d.preserveSuccess)
	require.Equal(t, imageAuditStatusCheckFailed, d.status)
	require.Equal(t, 5, d.failures, "consecutive_failures must keep incrementing while already failed")

	// a definitive client (4xx) error is not transient → check_failed immediately, even
	// with a prior good status (not masked behind the grace window)
	clientFail := imageAuditResult{Status: imageAuditStatusCheckFailed, ErrorKind: imageAuditErrorClient, Error: "400 Bad Request"}
	d = decideAuditPersistence(clientFail, good, 0)
	require.False(t, d.preserveSuccess, "client errors must surface immediately")
	require.Equal(t, imageAuditStatusCheckFailed, d.status)
	require.Equal(t, 0, d.failures, "definitive errors reset the transient counter")
}

func TestUpsertPreservesLastGoodOnTransientFailure(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupTestHub(hub, testApp)

	target := imageAuditTarget{AgentID: "agent1", ContainerID: "c1", ContainerName: "freshrss", CurrentRef: "lscr.io/x/freshrss:1.2.3"}
	good := imageAuditResult{Target: target, Status: imageAuditStatusUpdateAvailable, LatestTag: "1.3.0", LatestDigest: "sha256:new", CheckedAt: time.Now().UTC()}
	if _, err := hub.upsertContainerImageAudit(good); err != nil {
		t.Fatalf("seed good: %v", err)
	}

	get := func() *core.Record {
		rec, e := hub.FindFirstRecordByFilter(containerImageAuditsCollection, "agent = {:a} && container_id = {:c}", dbx.Params{"a": "agent1", "c": "c1"})
		if e != nil {
			t.Fatalf("find: %v", e)
		}
		return rec
	}

	// Stamp notification state + details on the seeded good record so we can assert the
	// preserve path leaves them untouched (no re-notify on recovery; no clobbered data).
	seed := get()
	seed.Set("last_notified_signature", "SENTINEL")
	seed.Set("details", map[string]any{"line_status": "patch_available"})
	require.NoError(t, hub.SaveNoValidate(seed))

	// Two transient failures: status stays update_available, latest_tag preserved.
	fail := imageAuditResult{Target: target, Status: imageAuditStatusCheckFailed, ErrorKind: imageAuditErrorTimeout, Error: "context deadline exceeded", CheckedAt: time.Now().UTC()}
	for i := 1; i <= 2; i++ {
		if _, err := hub.upsertContainerImageAudit(fail); err != nil {
			t.Fatalf("transient %d: %v", i, err)
		}
		rec := get()
		require.Equal(t, imageAuditStatusUpdateAvailable, rec.GetString("status"), "status must be preserved on transient failure %d", i)
		require.Equal(t, "1.3.0", rec.GetString("latest_tag"), "latest_tag preserved")
		require.EqualValues(t, i, int(numberAsFloat64(rec.Get("consecutive_failures"))))
		require.NotEmpty(t, rec.GetString("last_check_error"))
		require.NotEmpty(t, rec.GetString("last_check_error_at"), "errored-at timestamp must be stamped on preserve")
		// preserve must not touch notification state, the error field, or details
		require.Equal(t, "SENTINEL", rec.GetString("last_notified_signature"), "notification state must survive preserve (no re-notify)")
		require.Empty(t, rec.GetString("error"), "the error field must not be clobbered by a transient failure")
		var details map[string]any
		require.NoError(t, rec.UnmarshalJSONField("details", &details))
		require.Equal(t, "patch_available", details["line_status"], "details must not be clobbered on preserve")
	}

	// Third consecutive transient failure → escalate to check_failed.
	if _, err := hub.upsertContainerImageAudit(fail); err != nil {
		t.Fatalf("transient 3: %v", err)
	}
	require.Equal(t, imageAuditStatusCheckFailed, get().GetString("status"), "must escalate after grace cycles")

	// Recovery resets the counter and clears the soft error.
	recover := imageAuditResult{Target: target, Status: imageAuditStatusUpToDate, CheckedAt: time.Now().UTC()}
	if _, err := hub.upsertContainerImageAudit(recover); err != nil {
		t.Fatalf("recover: %v", err)
	}
	rec := get()
	require.Equal(t, imageAuditStatusUpToDate, rec.GetString("status"))
	require.EqualValues(t, 0, int(numberAsFloat64(rec.Get("consecutive_failures"))))
	require.Empty(t, rec.GetString("last_check_error"))
	require.Empty(t, rec.GetString("last_check_error_at"), "errored-at timestamp must clear on recovery")

	// A definitive (auth) failure after a good result surfaces immediately — not masked by
	// the transient grace window.
	authFail := imageAuditResult{Target: target, Status: imageAuditStatusCheckFailed, ErrorKind: imageAuditErrorAuth, Error: "401 Unauthorized", CheckedAt: time.Now().UTC()}
	if _, err := hub.upsertContainerImageAudit(authFail); err != nil {
		t.Fatalf("auth fail: %v", err)
	}
	require.Equal(t, imageAuditStatusCheckFailed, get().GetString("status"), "definitive auth error must surface immediately")
}
