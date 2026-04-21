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
