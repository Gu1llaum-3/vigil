package collectors

import "strings"

// skipFSTypes are pseudo/virtual or read-only-image filesystem types that are never real,
// writable local storage and must be excluded from both the storage inventory and the
// disk-usage metrics/alerts. Notably squashfs (snap images), overlay, and iso9660/udf
// (mounted optical/image media) are always ~100% full by design — keying the exclusion on
// the *fstype* (rather than device/mountpoint heuristics) fixes the snap false-positive
// without ever dropping a genuine ext4/xfs filesystem that happens to live on a loop device
// or under /snap. Kept in this platform-agnostic file (the collectors are Linux-only) so the
// filter can be unit-tested on any OS.
var skipFSTypes = map[string]bool{
	"proc": true, "sysfs": true, "devtmpfs": true, "devpts": true,
	"tmpfs": true, "cgroup": true, "cgroup2": true, "pstore": true,
	"securityfs": true, "debugfs": true, "tracefs": true, "hugetlbfs": true,
	"mqueue": true, "fusectl": true, "overlay": true, "squashfs": true,
	"efivarfs": true, "bpf": true, "autofs": true, "configfs": true,
	"iso9660": true, "udf": true,
}

// isPseudoFs reports whether a filesystem type is pseudo/virtual or read-only-image media
// (see skipFSTypes) and should be excluded from the storage inventory and disk metrics. The
// fstype is matched case-insensitively.
func isPseudoFs(fstype string) bool {
	return skipFSTypes[strings.ToLower(fstype)]
}
