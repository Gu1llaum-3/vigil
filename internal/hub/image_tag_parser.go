package hub

import (
	"regexp"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// knownVariantSuffixes lists OS/distro/build-flavor suffixes that look like
// semver prereleases but are actually image variants. Ordered longest-first so
// "-slim-bookworm" matches before "-bookworm".
var knownVariantSuffixes = []string{
	"-slim-bookworm",
	"-slim-bullseye",
	"-slim-buster",
	"-fpm-alpine",
	"-alpine3.21",
	"-alpine3.20",
	"-alpine3.19",
	"-alpine3.18",
	"-alpine3.17",
	"-alpine3.16",
	"-jdk-slim",
	"-jre-slim",
	"-jdk",
	"-jre",
	"-fpm",
	"-cli",
	"-apache",
	"-nginx",
	"-distroless",
	"-debian",
	"-ubuntu",
	"-alpine",
	"-bookworm",
	"-bullseye",
	"-buster",
	"-noble",
	"-jammy",
	"-focal",
	"-windowsservercore",
	"-nanoserver",
	"-slim",
}

// prereleaseRegex matches semver prerelease identifiers we want to exclude
// from the candidate set (rc/alpha/beta/dev/preview/snapshot/milestone/m\d+).
// Matched against the prerelease component as returned by Masterminds/semver,
// which strips the leading dash (e.g. "rc1", "alpha.1", "beta-2").
var prereleaseRegex = regexp.MustCompile(`(?i)^(rc|alpha|beta|dev|pre|preview|snapshot|milestone|m)([.\-_]?\d+(\.\d+)?)?$`)

// parsedTag is the structured representation of a Docker image tag.
type parsedTag struct {
	Version      *semver.Version
	Variant      string
	Parts        int
	IsPrerelease bool
	HasDots      bool
}

// splitVariant separates a Docker tag into its version part and variant
// suffix using a whitelist of known OS/distro variants. The returned variant
// is the suffix without the leading dash (e.g. "alpine", "bookworm").
func splitVariant(tag string) (versionPart, variant string) {
	lower := strings.ToLower(tag)
	for _, suffix := range knownVariantSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return tag[:len(tag)-len(suffix)], strings.TrimPrefix(suffix, "-")
		}
	}
	return tag, ""
}

// parseTag parses a Docker tag using Masterminds/semver with awareness of
// Docker-specific variant suffixes that semver would otherwise treat as
// prereleases. Returns ok=false for non-semver channel tags ("latest",
// "stable", "lts") and for unparsable strings.
func parseTag(tag string) (parsedTag, bool) {
	versionPart, variant := splitVariant(tag)

	v, err := semver.NewVersion(versionPart)
	if err != nil {
		return parsedTag{}, false
	}

	trimmedV := strings.TrimPrefix(versionPart, "v")
	hasDots := strings.Contains(trimmedV, ".")
	parts := 3
	switch strings.Count(trimmedV, ".") {
	case 0:
		parts = 1
	case 1:
		parts = 2
	}

	isPrerelease := false
	if pre := v.Prerelease(); pre != "" && variant == "" {
		// No whitelisted variant matched. The prerelease component is either a
		// real prerelease (rc1, alpha.1...) we should exclude from candidates,
		// or an unknown variant suffix (e.g. "8-jdk-custom") we should keep.
		if prereleaseRegex.MatchString(pre) {
			isPrerelease = true
		} else {
			variant = pre
		}
	}

	return parsedTag{
		Version:      v,
		Variant:      variant,
		Parts:        parts,
		IsPrerelease: isPrerelease,
		HasDots:      hasDots,
	}, true
}
