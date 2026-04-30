// Package hub handles serving the web UI and managing the PocketBase app.
package hub

import (
	"context"
	"crypto/ed25519"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/hub/notifications"
	"github.com/Gu1llaum-3/vigil/internal/hub/utils"
	"github.com/Gu1llaum-3/vigil/internal/users"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"golang.org/x/crypto/ssh"
)

// Hub is the application. It embeds the PocketBase app and keeps references to subcomponents.
type Hub struct {
	core.App
	um                 *users.UserManager
	pubKey             string
	signer             ssh.Signer
	appURL             string
	agentConns         sync.Map // agentID (string) → *ws.WsConn
	notificationReadAt sync.Map // userID (string) → time.Time
	monitorScheduler   *MonitorScheduler
	notifier           *notifications.Dispatcher
	credentialsKey     []byte
}

// NewHub creates a new Hub instance with default configuration.
func NewHub(app core.App) *Hub {
	hub := &Hub{App: app}
	hub.um = users.NewUserManager(hub)
	hub.monitorScheduler = newMonitorScheduler(hub)
	hub.notifier = notifications.New(hub)
	_ = onAfterBootstrapAndMigrations(app, hub.initialize)
	return hub
}

// onAfterBootstrapAndMigrations ensures the provided function runs after the database is set up and migrations are applied.
func onAfterBootstrapAndMigrations(app core.App, fn func(app core.App) error) error {
	if app.IsBootstrapped() {
		return fn(app)
	}
	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		if err := fn(e.App); err != nil {
			return err
		}
		return e.Next()
	})
	return nil
}

// StartHub sets up event handlers and starts the PocketBase server.
func (h *Hub) StartHub() error {
	ctx, cancel := context.WithCancel(context.Background())

	h.App.OnServe().BindFunc(func(e *core.ServeEvent) error {
		// register middlewares
		h.registerMiddlewares(e)
		// register api routes
		if err := h.registerApiRoutes(e); err != nil {
			return err
		}
		// start server
		if err := h.startServer(e); err != nil {
			return err
		}
		// start periodic snapshot collection
		interval := parseSnapshotInterval()
		slog.Info("Snapshot ticker started", "interval", interval)
		go h.startSnapshotTicker(ctx, interval)
		if err := h.registerScheduledJobs(); err != nil {
			return err
		}
		// start monitor scheduler
		go h.monitorScheduler.start(ctx)
		// start notification dispatcher
		go h.notifier.Start(ctx)
		return e.Next()
	})

	h.App.OnTerminate().BindFunc(func(e *core.TerminateEvent) error {
		cancel()
		return e.Next()
	})

	// handle default values for user / user_settings creation
	h.App.OnRecordCreate("users").BindFunc(h.um.InitializeUserRole)
	h.App.OnRecordCreate("user_settings").BindFunc(h.um.InitializeUserSettings)

	// Stop monitor goroutine when the record is deleted (cascade from user action)
	h.App.OnRecordAfterDeleteSuccess("monitors").BindFunc(func(e *core.RecordEvent) error {
		h.monitorScheduler.stopMonitor(e.Record.Id)
		return e.Next()
	})

	pb, ok := h.App.(*pocketbase.PocketBase)
	if !ok {
		cancel()
		return errors.New("not a pocketbase app")
	}
	return pb.Start()
}

// parseSnapshotInterval reads SNAPSHOT_INTERVAL from env and returns the parsed duration.
// Defaults to 5 minutes; enforces a minimum of 1 minute.
func parseSnapshotInterval() time.Duration {
	// 15 minutes: package/repo collection runs apt-get or dnf subprocesses that are
	// CPU-intensive and potentially network-dependent. Agent liveness (up/down) is
	// tracked separately via WebSocket Ping every 30s and is unaffected by this interval.
	const defaultInterval = 15 * time.Minute
	const minInterval = time.Minute
	raw, ok := utils.GetEnv("SNAPSHOT_INTERVAL")
	if !ok || raw == "" {
		return defaultInterval
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < minInterval {
		slog.Warn("Invalid SNAPSHOT_INTERVAL, using default", "value", raw, "default", defaultInterval)
		return defaultInterval
	}
	return d
}

// initialize sets up initial configuration (collections, settings, etc.)
func (h *Hub) initialize(app core.App) error {
	settings := app.Settings()
	settings.Batch.Enabled = true
	if appURL, isSet := utils.GetEnv("APP_URL"); isSet {
		h.appURL = appURL
		settings.Meta.AppURL = appURL
	}
	if err := app.Save(settings); err != nil {
		return err
	}
	if err := h.bootstrapInitialUsers(app); err != nil {
		return err
	}
	// Pre-load the SSH key so h.pubKey is available before any agent connects.
	if _, err := h.GetSSHKey(app.DataDir()); err != nil {
		return fmt.Errorf("failed to initialize SSH key: %w", err)
	}
	key, err := loadOrCreateCredentialsKey(app.DataDir())
	if err != nil {
		return fmt.Errorf("failed to initialize credentials key: %w", err)
	}
	h.credentialsKey = key
	return setCollectionAuthSettings(app)
}

func (h *Hub) bootstrapInitialUsers(app core.App) error {
	totalUsers, err := app.CountRecords("users")
	if err != nil || totalUsers > 0 {
		return nil
	}

	email, _ := utils.GetEnv("USER_EMAIL")
	password, _ := utils.GetEnv("USER_PASSWORD")
	if email == "" || password == "" {
		return nil
	}

	usersCollection, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		return err
	}
	user := core.NewRecord(usersCollection)
	user.SetEmail(email)
	user.SetPassword(password)
	user.SetVerified(true)
	user.Set("role", "admin")
	if err := app.Save(user); err != nil {
		return err
	}

	superusersCollection, err := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		return err
	}
	superuser := core.NewRecord(superusersCollection)
	superuser.SetEmail(email)
	superuser.SetPassword(password)
	return app.Save(superuser)
}

// GetSSHKey generates an ED25519 key pair if it doesn't exist and returns the signer.
func (h *Hub) GetSSHKey(dataDir string) (ssh.Signer, error) {
	if h.signer != nil {
		return h.signer, nil
	}

	if dataDir == "" {
		dataDir = h.DataDir()
	}

	privateKeyPath := path.Join(dataDir, "id_ed25519")

	existingKey, err := os.ReadFile(privateKeyPath)
	if err == nil {
		private, err := ssh.ParsePrivateKey(existingKey)
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %s", err)
		}
		pubKeyBytes := ssh.MarshalAuthorizedKey(private.PublicKey())
		h.pubKey = strings.TrimSuffix(string(pubKeyBytes), "\n")
		return private, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to read %s: %w", privateKeyPath, err)
	}

	_, privKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, err
	}
	privKeyPem, err := ssh.MarshalPrivateKey(privKey, "")
	if err != nil {
		return nil, err
	}

	if err := os.WriteFile(privateKeyPath, pem.EncodeToMemory(privKeyPem), 0600); err != nil {
		return nil, fmt.Errorf("failed to write private key to %q: err: %w", privateKeyPath, err)
	}

	sshPrivate, _ := ssh.NewSignerFromSigner(privKey)
	pubKeyBytes := ssh.MarshalAuthorizedKey(sshPrivate.PublicKey())
	h.pubKey = strings.TrimSuffix(string(pubKeyBytes), "\n")

	h.Logger().Info("ed25519 key pair generated successfully.")
	h.Logger().Info("Saved to: " + privateKeyPath)

	return sshPrivate, err
}

// MakeLink formats a link with the app URL and path segments.
func (h *Hub) MakeLink(parts ...string) string {
	base := strings.TrimSuffix(h.Settings().Meta.AppURL, "/")
	for _, part := range parts {
		if part == "" {
			continue
		}
		base = fmt.Sprintf("%s/%s", base, url.PathEscape(part))
	}
	return base
}
