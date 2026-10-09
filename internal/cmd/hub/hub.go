package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
	// Embed the IANA timezone database in the binary so time.LoadLocation works in any
	// runtime environment — notably the Alpine/scratch Docker image, which ships without
	// system tzdata. Without this, maintenance windows reject valid zones like
	// "Europe/Paris" with "Invalid timezone" on the deployed hub.
	_ "time/tzdata"

	"github.com/Gu1llaum-3/vigil"
	"github.com/Gu1llaum-3/vigil/internal/hub"
	_ "github.com/Gu1llaum-3/vigil/internal/migrations"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"
	"github.com/spf13/cobra"
)

func main() {
	// handle health check first to prevent unneeded execution
	if len(os.Args) > 3 && os.Args[1] == "health" {
		url := os.Args[3]
		if err := checkHealth(url); err != nil {
			log.Fatal(err)
		}
		fmt.Print("ok")
		return
	}

	// update only replaces the binary: it must not open (or migrate) the database, nor
	// need a writable working directory (the systemd update unit runs from /).
	if len(os.Args) > 1 && os.Args[1] == "update" {
		cmd := newUpdateCmd()
		cmd.SetArgs(os.Args[2:])
		if err := cmd.Execute(); err != nil {
			os.Exit(1)
		}
		return
	}

	baseApp := getBaseApp()
	h := hub.NewHub(baseApp)
	// Resolved at bootstrap: PocketBase registers serve and superuser in Start.
	h.BackupBeforeMigrations(func() bool {
		cmd, args, err := baseApp.RootCmd.Find(os.Args[1:])
		return err == nil && appliesMigrations(cmd, args)
	})
	if err := h.StartHub(); err != nil {
		log.Fatal(err)
	}
}

// appliesMigrations reports whether cmd (with its remaining args) runs the pending
// migrations: serve, and migrate up (migrate's default).
func appliesMigrations(cmd *cobra.Command, args []string) bool {
	switch cmd.Name() {
	case "serve":
		return true
	case "migrate":
		for _, arg := range args {
			if !strings.HasPrefix(arg, "-") {
				return arg == "up"
			}
		}
		return true
	}
	return false
}

// getBaseApp creates a new PocketBase app with the default config
func getBaseApp() *pocketbase.PocketBase {
	isDev := os.Getenv("ENV") == "dev"

	baseApp := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir: app.HubDataDirName,
		DefaultDev:     isDev,
	})
	baseApp.RootCmd.Version = app.Version
	baseApp.RootCmd.Use = app.AppName
	baseApp.RootCmd.Short = ""
	// add update command (listed in the help; main runs it without the app)
	baseApp.RootCmd.AddCommand(newUpdateCmd())
	// add health command
	baseApp.RootCmd.AddCommand(newHealthCmd())

	// enable auto creation of migration files when making collection changes in the Admin UI
	migratecmd.MustRegister(baseApp, baseApp.RootCmd, migratecmd.Config{
		Automigrate: isDev,
		Dir:         "internal/migrations",
	})

	return baseApp
}

func newUpdateCmd() *cobra.Command {
	updateCmd := &cobra.Command{
		Use:   "update",
		Short: "Update " + app.AppName + " to the latest version",
		Run:   hub.Update,
	}
	updateCmd.Flags().Bool("china-mirrors", false, "Use the configured release mirror instead of GitHub")
	updateCmd.Flags().Bool("allow-major", false, "Also install a new major version (read its release notes first)")
	return updateCmd
}

func newHealthCmd() *cobra.Command {
	var baseURL string

	healthCmd := &cobra.Command{
		Use:   "health",
		Short: "Check health of running hub",
		Run: func(cmd *cobra.Command, args []string) {
			if err := checkHealth(baseURL); err != nil {
				log.Fatal(err)
			}
			os.Exit(0)
		},
	}
	healthCmd.Flags().StringVar(&baseURL, "url", "", "base URL")
	// Only fails for an unknown flag name, i.e. a programming error.
	if err := healthCmd.MarkFlagRequired("url"); err != nil {
		panic(err)
	}
	return healthCmd
}

// checkHealth checks the health of the hub.
func checkHealth(baseURL string) error {
	client := &http.Client{
		Timeout: time.Second * 3,
	}
	healthURL := baseURL + "/api/health"
	resp, err := client.Get(healthURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("%s returned status %d", healthURL, resp.StatusCode)
	}
	return nil
}
