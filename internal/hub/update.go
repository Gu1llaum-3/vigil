package hub

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	app "github.com/Gu1llaum-3/vigil"
	"github.com/Gu1llaum-3/vigil/internal/ghupdate"
	"github.com/spf13/cobra"
)

// Update updates app to the latest version
func Update(cmd *cobra.Command, _ []string) {
	dataDir := os.TempDir()

	// Prefer the local data directory when running from an unpacked release.
	localDataDir := "./" + app.HubDataDirName
	if _, err := os.Stat(localDataDir); err == nil {
		dataDir = localDataDir
	}

	// Check if china-mirrors flag is set
	useMirror, _ := cmd.Flags().GetBool("china-mirrors")
	allowMajor, _ := cmd.Flags().GetBool("allow-major")

	// Get the executable path before update
	exePath, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}

	updated, err := ghupdate.Update(ghupdate.Config{
		ArchiveExecutable: app.HubBinary,
		DataDir:           dataDir,
		UseMirror:         useMirror,
		MirrorHost:        app.ReleaseMirrorHost,
		AllowMajor:        allowMajor,
	})
	if err != nil {
		log.Fatal(err)
	}
	if !updated {
		return
	}

	// make sure the file is executable
	if err := os.Chmod(exePath, 0755); err != nil {
		fmt.Printf("Warning: failed to set executable permissions: %v\n", err)
	}

	// Fix SELinux context if necessary
	if err := ghupdate.HandleSELinuxContext(exePath); err != nil {
		ghupdate.ColorPrintf(ghupdate.ColorYellow, "Warning: SELinux context handling: %v", err)
	}

	// The new binary runs once the hub restarts; its migrations are preceded by a backup
	// (backupBeforePendingMigrations).
	restartService(filepath.Dir(exePath))
}

// RestartPendingFile is written next to the binary when the updater cannot restart the hub
// itself (it runs unprivileged from the vigil-hub-update unit); that unit's ExecStartPost,
// which runs as root, restarts vigil-hub when it finds it (install-hub.sh).
const RestartPendingFile = ".restart-pending"

// The system calls restartService makes, replaced in tests.
var (
	serviceGOOS     = runtime.GOOS
	serviceGeteuid  = os.Geteuid
	serviceLookPath = exec.LookPath
	serviceRun      = func(name string, args ...string) error { return exec.Command(name, args...).Run() }
)

// restartService restarts the hub service so the new binary takes over. Without root it
// cannot: under systemd it leaves RestartPendingFile for the update unit instead.
func restartService(exeDir string) {
	if serviceGOOS != "windows" && serviceGeteuid() != 0 {
		if _, err := serviceLookPath("systemctl"); serviceGOOS == "linux" && err == nil {
			marker := filepath.Join(exeDir, RestartPendingFile)
			if err := os.WriteFile(marker, nil, 0o644); err != nil {
				ghupdate.ColorPrintf(ghupdate.ColorYellow, "Warning: cannot write %s: %v", marker, err)
			}
			ghupdate.ColorPrint(ghupdate.ColorYellow, "Not running as root: the update unit restarts the service, otherwise restart it manually: sudo systemctl restart "+app.HubServiceName)
			return
		}
		ghupdate.ColorPrint(ghupdate.ColorYellow, "Not running as root: restart the hub manually so the new version takes over.")
		return
	}
	// The service installed by install-hub.sh is vigil-hub; older setups may use vigil.
	for _, name := range []string{app.HubServiceName, app.AppName} {
		if restartNamedService(name) {
			return
		}
	}
	ghupdate.ColorPrint(ghupdate.ColorYellow, "Service restart not attempted. If running as a service, restart manually.")
}

// restartNamedService restarts name under systemd, OpenRC or FreeBSD rc if it is running
// there, and reports whether it found it.
func restartNamedService(name string) bool {
	managers := []struct {
		tool          string
		status, start []string
	}{
		{"systemctl", []string{"is-active", "--quiet", name + ".service"}, []string{"restart", name + ".service"}},
		{"rc-service", []string{name, "status"}, []string{name, "restart"}},
		{"service", []string{name, "status"}, []string{name, "restart"}},
	}
	for _, m := range managers {
		if _, err := serviceLookPath(m.tool); err != nil {
			continue
		}
		if serviceRun(m.tool, m.status...) != nil {
			continue
		}
		ghupdate.ColorPrint(ghupdate.ColorYellow, "Restarting "+name+"...")
		if err := serviceRun(m.tool, m.start...); err != nil {
			ghupdate.ColorPrintf(ghupdate.ColorYellow, "Warning: failed to restart %s: %v", name, err)
			ghupdate.ColorPrint(ghupdate.ColorYellow, "Please restart it manually.")
		} else {
			ghupdate.ColorPrint(ghupdate.ColorGreen, "Service restarted successfully")
		}
		return true
	}
	return false
}
