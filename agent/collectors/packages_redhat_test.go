//go:build testing && linux

package collectors

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDnfOutdatedPackagesGraceful(t *testing.T) {
	// Verify the function handles environments without dnf gracefully
	result, _ := dnfOutdatedPackages(context.Background())
	assert.NotNil(t, result)
}

func TestRpmInstalledCountGraceful(t *testing.T) {
	count, err := rpmInstalledCount(context.Background())
	if err != nil {
		t.Skip("rpm not available")
	}
	assert.GreaterOrEqual(t, count, 0)
}

func TestDnfLastUpgradeTimeGraceful(t *testing.T) {
	_, _, _ = dnfLastUpgradeTime(context.Background())
}
