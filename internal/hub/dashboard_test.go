//go:build testing

package hub

import (
	"testing"

	"github.com/Gu1llaum-3/vigil/internal/common"
	"github.com/stretchr/testify/assert"
)

func TestClassifyPatchStatus(t *testing.T) {
	tests := []struct {
		name     string
		snapshot common.HostSnapshotResponse
		expected string
	}{
		{
			name: "reboot has highest priority",
			snapshot: common.HostSnapshotResponse{
				Reboot: common.RebootInfo{Required: true},
				Packages: common.PackageInfo{
					SecurityCount:      2,
					OutdatedCount:      4,
					LastUpgradeKnown:   true,
					LastUpgradeAgeDays: 90,
				},
			},
			expected: patchStatusRebootRequired,
		},
		{
			name: "security beats stale",
			snapshot: common.HostSnapshotResponse{
				Packages: common.PackageInfo{
					SecurityCount:      1,
					OutdatedCount:      3,
					LastUpgradeKnown:   true,
					LastUpgradeAgeDays: 45,
				},
			},
			expected: patchStatusSecurityUpdates,
		},
		{
			name: "stale when updates older than 30 days",
			snapshot: common.HostSnapshotResponse{
				Packages: common.PackageInfo{
					OutdatedCount:      2,
					LastUpgradeKnown:   true,
					LastUpgradeAgeDays: 31,
				},
			},
			expected: patchStatusStaleUpdates,
		},
		{
			name: "unknown when updates exist but age is missing",
			snapshot: common.HostSnapshotResponse{
				Packages: common.PackageInfo{
					OutdatedCount:    2,
					LastUpgradeKnown: false,
				},
			},
			expected: patchStatusUnknown,
		},
		{
			name: "compliant when updates are recent",
			snapshot: common.HostSnapshotResponse{
				Packages: common.PackageInfo{
					OutdatedCount:      1,
					LastUpgradeKnown:   true,
					LastUpgradeAgeDays: 10,
				},
			},
			expected: patchStatusCompliant,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, classifyPatchStatus(tc.snapshot))
		})
	}
}

func TestContainerSeverity(t *testing.T) {
	intPtr := func(v int) *int { return &v }

	tests := []struct {
		name      string
		container common.ContainerInfo
		expected  containerSeverityLevel
	}{
		{
			name:      "running is ok",
			container: common.ContainerInfo{Status: "running"},
			expected:  severityOK,
		},
		{
			name:      "restarting is warning",
			container: common.ContainerInfo{Status: "restarting"},
			expected:  severityWarning,
		},
		{
			name:      "dead is error",
			container: common.ContainerInfo{Status: "dead"},
			expected:  severityError,
		},
		{
			name:      "exited 0 is neutral (one-shot job finished cleanly)",
			container: common.ContainerInfo{Status: "exited", ExitCode: intPtr(0)},
			expected:  severityNeutral,
		},
		{
			name:      "exited non-zero is error",
			container: common.ContainerInfo{Status: "exited", ExitCode: intPtr(137)},
			expected:  severityError,
		},
		{
			name:      "exited without exit code defaults to neutral",
			container: common.ContainerInfo{Status: "exited"},
			expected:  severityNeutral,
		},
		{
			name:      "paused is neutral",
			container: common.ContainerInfo{Status: "paused"},
			expected:  severityNeutral,
		},
		{
			name:      "created is neutral",
			container: common.ContainerInfo{Status: "created"},
			expected:  severityNeutral,
		},
		{
			name:      "unknown status is neutral",
			container: common.ContainerInfo{Status: "something-weird"},
			expected:  severityNeutral,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, containerSeverity(tc.container))
		})
	}
}
