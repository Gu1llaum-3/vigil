//go:build testing

package collectors

import "testing"

// TestIsPseudoFs locks the disk-alert false-positive fix: pseudo/virtual and read-only-image
// filesystems (squashfs snaps, overlay, tmpfs, iso9660/udf) are excluded, while real local
// filesystems are kept — including an ext4/xfs that happens to live on a loop device or under
// /snap (the fstype-only check never over-matches on device/mountpoint). Network filesystems
// are NOT this function's concern (handled by isNetworkFs).
func TestIsPseudoFs(t *testing.T) {
	cases := map[string]bool{
		"squashfs": true, // the reported false positive (/snap/* are squashfs)
		"SquashFS": true, // case-insensitive
		"overlay":  true,
		"tmpfs":    true,
		"iso9660":  true, // mounted ISO / read-only image, always ~100% full
		"udf":      true,
		"ext4":     false, // real local fs — kept (even on a loop device or under /snap)
		"xfs":      false,
		"btrfs":    false,
		"":         false,
		"nfs":      false, // network → excluded separately by isNetworkFs, not here
	}
	for fstype, want := range cases {
		if got := isPseudoFs(fstype); got != want {
			t.Errorf("isPseudoFs(%q) = %v, want %v", fstype, got, want)
		}
	}
}
