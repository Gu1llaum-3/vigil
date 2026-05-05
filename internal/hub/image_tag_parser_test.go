//go:build testing

package hub

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSplitVariantWhitelist(t *testing.T) {
	cases := []struct {
		tag         string
		versionPart string
		variant     string
	}{
		{"1.25-alpine", "1.25", "alpine"},
		{"1.25-alpine3.19", "1.25", "alpine3.19"},
		{"15-bookworm", "15", "bookworm"},
		{"8-jdk-slim", "8", "jdk-slim"},
		{"8-jdk", "8", "jdk"},
		{"8.2-fpm-alpine", "8.2", "fpm-alpine"},
		{"3.19", "3.19", ""},
		{"latest", "latest", ""},
	}
	for _, tc := range cases {
		t.Run(tc.tag, func(t *testing.T) {
			v, variant := splitVariant(tc.tag)
			require.Equal(t, tc.versionPart, v)
			require.Equal(t, tc.variant, variant)
		})
	}
}

func TestParseTagRejectsBuildIDsWhenComparedAgainstDottedVersion(t *testing.T) {
	// Bare numeric tags like "608111629" parse as semver 608111629.0.0 — that is
	// the build-ID trap we guard against in selectAuditTag via HasDots.
	parsed, ok := parseTag("608111629")
	require.True(t, ok)
	require.False(t, parsed.HasDots)
	require.Equal(t, 1, parsed.Parts)
}

func TestParseTagDetectsPrereleases(t *testing.T) {
	cases := []struct {
		tag          string
		isPrerelease bool
	}{
		{"1.0.0-rc1", true},
		{"1.0.0-rc.1", true},
		{"1.0.0-alpha", true},
		{"1.0.0-alpha.3", true},
		{"1.0.0-beta-2", true},
		{"1.0.0-dev", true},
		{"1.0.0-snapshot", true},
		{"1.0.0-m1", true},
		{"1.0.0", false},
		{"1.0.0-alpine", false}, // whitelisted variant, not prerelease
		{"1.0.0-bookworm", false},
	}
	for _, tc := range cases {
		t.Run(tc.tag, func(t *testing.T) {
			parsed, ok := parseTag(tc.tag)
			require.True(t, ok)
			require.Equal(t, tc.isPrerelease, parsed.IsPrerelease)
		})
	}
}

func TestSelectAuditTagSkipsPrereleasesWhenCurrentIsStable(t *testing.T) {
	current, ok := parseNumericVersion("1.2.3")
	require.True(t, ok)
	tag, found := selectAuditTag(
		[]string{"1.2.4", "1.2.5-rc1", "1.2.5-beta", "1.2.6-alpha.1", "1.3.0-rc1"},
		"1.2.3", current, imageAuditPolicySemverMinor,
	)
	require.True(t, found)
	require.Equal(t, "1.2.4", tag, "must not jump from stable to a prerelease")
}

func TestSelectAuditTagAcceptsPrereleasesWhenCurrentIsPrerelease(t *testing.T) {
	current, ok := parseNumericVersion("1.2.3-rc1")
	require.True(t, ok)
	require.True(t, current.IsPrerelease)
	tag, found := selectAuditTag(
		[]string{"1.2.3-rc1", "1.2.3-rc2", "1.2.3"},
		"1.2.3-rc1", current, imageAuditPolicySemverMinor,
	)
	require.True(t, found)
	// 1.2.3 (stable) > 1.2.3-rc2 in semver ordering
	require.Equal(t, "1.2.3", tag)
}

func TestSelectAuditTagFiltersBuildIDsWhenCurrentHasDots(t *testing.T) {
	current, ok := parseNumericVersion("1.2.3")
	require.True(t, ok)
	require.True(t, current.HasDots)
	tag, found := selectAuditTag(
		[]string{"1.2.4", "608111629", "20240515"},
		"1.2.3", current, imageAuditPolicySemverMinor,
	)
	require.True(t, found)
	require.Equal(t, "1.2.4", tag, "pure-numeric build IDs must be filtered out")
}

func TestSelectLatestSemverTagSkipsPrereleasesAndBuildIDs(t *testing.T) {
	tag, _, found := selectLatestSemverTag(
		[]string{"1.0.0", "2.0.0", "3.0.0-rc1", "608111629"}, "",
	)
	require.True(t, found)
	require.Equal(t, "2.0.0", tag)
}

func TestNormalizeImageAuditRefAcceptsExtendedVariants(t *testing.T) {
	cases := []struct {
		input  string
		tag    string
		policy string
	}{
		{"php:8.2-fpm-alpine", "8.2-fpm-alpine", imageAuditPolicySemverMinor},
		{"openjdk:11-jdk-slim", "11-jdk-slim", imageAuditPolicySemverMajor},
		{"openjdk:11-jre", "11-jre", imageAuditPolicySemverMajor},
		{"node:20.11.1-alpine3.19", "20.11.1-alpine3.19", imageAuditPolicySemverMinor},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			_, _, tag, policy, err := normalizeImageAuditRef(tc.input)
			require.NoError(t, err)
			require.Equal(t, tc.tag, tag)
			require.Equal(t, tc.policy, policy)
		})
	}
}
