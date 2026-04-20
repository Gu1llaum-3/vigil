//go:build linux

package collectors

import (
	"context"
	"os"
	"os/exec"

	"github.com/Gu1llaum-3/vigil/internal/common"
)

// CollectReboot checks whether a system reboot is required.
func CollectReboot(ctx context.Context, osFamily string) (common.RebootInfo, error) {
	switch osFamily {
	case "Debian":
		return collectRebootDebian()
	case "RedHat":
		return collectRebootRedHat(ctx)
	default:
		return common.RebootInfo{}, nil
	}
}

func collectRebootDebian() (common.RebootInfo, error) {
	_, err := os.Stat("/run/reboot-required")
	if err == nil {
		reason := ""
		data, readErr := os.ReadFile("/run/reboot-required.pkgs")
		if readErr == nil {
			reason = string(data)
		}
		return common.RebootInfo{Required: true, Reason: reason}, nil
	}
	return common.RebootInfo{Required: false}, nil
}

func collectRebootRedHat(ctx context.Context) (common.RebootInfo, error) {
	cmd := exec.CommandContext(ctx, "needs-restarting", "-r")
	err := cmd.Run()
	if err != nil {
		// Exit code 1 means reboot needed
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return common.RebootInfo{Required: true, Reason: "kernel or core library updated"}, nil
		}
	}
	return common.RebootInfo{Required: false}, nil
}
