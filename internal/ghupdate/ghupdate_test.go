package ghupdate

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReleaseFindAssetBySuffix(t *testing.T) {
	r := release{
		Assets: []*releaseAsset{
			{Name: "test1.zip", Id: 1},
			{Name: "test2.zip", Id: 2},
			{Name: "test22.zip", Id: 22},
			{Name: "test3.zip", Id: 3},
		},
	}

	asset, err := r.findAssetBySuffix("2.zip")
	if err != nil {
		t.Fatalf("Expected nil, got err: %v", err)
	}

	if asset.Id != 2 {
		t.Fatalf("Expected asset with id %d, got %v", 2, asset)
	}
}

func TestExtractFailure(t *testing.T) {
	testDir := t.TempDir()

	// Test with missing zip file
	missingZipPath := filepath.Join(testDir, "missing_test.zip")
	extractedPath := filepath.Join(testDir, "zip_extract")

	if err := extract(missingZipPath, extractedPath); err == nil {
		t.Fatal("Expected Extract to fail due to missing zip file")
	}

	// Test with missing tar.gz file
	missingTarPath := filepath.Join(testDir, "missing_test.tar.gz")

	if err := extract(missingTarPath, extractedPath); err == nil {
		t.Fatal("Expected Extract to fail due to missing tar.gz file")
	}
}

func TestCheckUpgrade(t *testing.T) {
	for _, tc := range []struct {
		current, latest string
		allowMajor      bool
		want            upgradeDecision
	}{
		{"0.2.17-beta", "v0.2.17-beta", false, upgradeNone},
		{"0.2.17-beta", "v0.2.16", false, upgradeNone},
		{"0.2.16", "v0.2.17", false, upgradeAllowed},
		{"0.2.17-beta", "v0.3.0", false, upgradeAllowed},
		{"0.2.17-beta", "v1.0.0", false, upgradeMajorRefused},
		{"0.2.17-beta", "v1.0.0", true, upgradeAllowed},
		{"1.4.2", "v2.0.0", false, upgradeMajorRefused},
		{"1.4.2", "v1.5.0", false, upgradeAllowed},
	} {
		got, err := checkUpgrade(tc.current, tc.latest, tc.allowMajor)
		require.NoError(t, err)
		assert.Equal(t, tc.want, got, "%s → %s (allow major: %v)", tc.current, tc.latest, tc.allowMajor)
	}

	_, err := checkUpgrade("0.0.0-dev", "not-a-version", false)
	assert.Error(t, err, "a malformed tag is an error, not a panic")
	_, err = checkUpgrade("garbage", "v1.0.0", false)
	assert.Error(t, err)
}
