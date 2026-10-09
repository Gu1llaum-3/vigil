//go:build testing

package notifications

import (
	"context"
	"errors"
	"net/mail"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/hub/notifications/providers"
	_ "github.com/Gu1llaum-3/vigil/internal/migrations"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/mailer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeProvider records what it is asked to send and fails the first failures calls.
type fakeProvider struct {
	mu       sync.Mutex
	failures int
	err      error
	sent     []providers.Message
	channels []string
	attempts int
}

func (p *fakeProvider) Kind() string                        { return "fake" }
func (p *fakeProvider) SensitiveConfigKeys() []string       { return []string{"secret"} }
func (p *fakeProvider) ValidateConfig(map[string]any) error { return nil }
func (p *fakeProvider) Send(_ context.Context, ch providers.Channel, msg providers.Message) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.attempts++
	if p.attempts <= p.failures {
		return "", p.err
	}
	p.sent = append(p.sent, msg)
	p.channels = append(p.channels, ch.ID)
	return "preview of " + msg.Title, nil
}

var (
	migratedOnce sync.Once
	migratedDir  string
	migratedErr  error
)

// copyMigratedData puts a migrated, empty database in dir; the migrations run once per
// package run, they are the slow part of a test database.
func copyMigratedData(t *testing.T, dir string) {
	t.Helper()
	migratedOnce.Do(func() {
		migratedDir, migratedErr = os.MkdirTemp("", "vigil-notifications-test")
		if migratedErr != nil {
			return
		}
		app := core.NewBaseApp(core.BaseAppConfig{DataDir: migratedDir})
		if migratedErr = app.Bootstrap(); migratedErr != nil {
			return
		}
		migratedErr = app.RunAllMigrations()
		_ = app.ClearBootstrap()
	})
	require.NoError(t, migratedErr)
	for _, name := range []string{"data.db", "auxiliary.db"} {
		raw, err := os.ReadFile(filepath.Join(migratedDir, name))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), raw, 0o600))
	}
}

func TestMain(m *testing.M) {
	code := m.Run()
	if migratedDir != "" {
		_ = os.RemoveAll(migratedDir)
	}
	os.Exit(code)
}

type dispatchEnv struct {
	t        *testing.T
	app      core.App
	d        *Dispatcher
	provider *fakeProvider
	user     string
}

func newDispatchEnv(t *testing.T) *dispatchEnv {
	t.Helper()
	dir := t.TempDir()
	copyMigratedData(t, dir)
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: dir})
	require.NoError(t, app.Bootstrap())
	t.Cleanup(func() { _ = app.ClearBootstrap() })

	users, err := app.FindCollectionByNameOrId("users")
	require.NoError(t, err)
	user := core.NewRecord(users)
	user.Set("email", "admin@example.com")
	user.SetPassword("password123")
	user.Set("role", "admin")
	require.NoError(t, app.Save(user))

	d := New(app)
	d.retryDelays = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	provider := &fakeProvider{err: errors.New("boom")}
	d.providerMap["fake"] = provider
	return &dispatchEnv{t: t, app: app, d: d, provider: provider, user: user.Id}
}

func (env *dispatchEnv) record(collection string, fields map[string]any) string {
	env.t.Helper()
	col, err := env.app.FindCollectionByNameOrId(collection)
	require.NoError(env.t, err)
	rec := core.NewRecord(col)
	for k, v := range fields {
		rec.Set(k, v)
	}
	require.NoError(env.t, env.app.SaveNoValidate(rec))
	return rec.Id
}

func (env *dispatchEnv) channel(fields map[string]any) string {
	env.t.Helper()
	base := map[string]any{"name": "ch-" + time.Now().Format("150405.000000000"), "kind": "fake", "enabled": true, "config": map[string]any{"secret": "s3cret"}}
	for k, v := range fields {
		base[k] = v
	}
	return env.record("notification_channels", base)
}

func (env *dispatchEnv) rule(fields map[string]any) string {
	env.t.Helper()
	base := map[string]any{"name": "rule-" + time.Now().Format("150405.000000000"), "enabled": true, "created_by": env.user}
	for k, v := range fields {
		base[k] = v
	}
	return env.record("notification_rules", base)
}

// reset removes the rules and logs and forgets what was sent, keeping the database.
func (env *dispatchEnv) reset() {
	env.t.Helper()
	for _, collection := range []string{"notification_rules", "notification_logs"} {
		_, err := env.app.DB().NewQuery("DELETE FROM " + collection).Execute()
		require.NoError(env.t, err)
	}
	*env.provider = fakeProvider{err: env.provider.err}
	env.d.throttleCache = map[string]time.Time{}
}

type logRow struct {
	Rule, Channel, CreatedBy, ChannelKind, EventKind, ResourceID, ResourceName, ResourceType, Status, Error, Preview string
}

func (env *dispatchEnv) logs() []logRow {
	env.t.Helper()
	recs, err := env.app.FindRecordsByFilter("notification_logs", "", "sent_at", 0, 0)
	require.NoError(env.t, err)
	out := make([]logRow, 0, len(recs))
	for _, r := range recs {
		out = append(out, logRow{
			Rule: r.GetString("rule"), Channel: r.GetString("channel"), CreatedBy: r.GetString("created_by"),
			ChannelKind: r.GetString("channel_kind"), EventKind: r.GetString("event_kind"),
			ResourceID: r.GetString("resource_id"), ResourceName: r.GetString("resource_name"),
			ResourceType: r.GetString("resource_type"), Status: r.GetString("status"),
			Error: r.GetString("error"), Preview: r.GetString("payload_preview"),
		})
	}
	return out
}

func monitorDown(id string) Event {
	return Event{Kind: EventMonitorDown, OccurredAt: time.Now(), Resource: ResourceRef{ID: id, Name: "site " + id, Type: "monitor"}, Previous: "up", Current: "down"}
}

func TestDispatchSendsAndLogs(t *testing.T) {
	env := newDispatchEnv(t)
	ch1, ch2 := env.channel(nil), env.channel(nil)
	rule := env.rule(map[string]any{"events": []string{"monitor.down"}, "channels": []string{ch1, ch2}})

	env.d.process(context.Background(), monitorDown("m1"))

	require.Len(t, env.provider.sent, 2)
	assert.ElementsMatch(t, []string{ch1, ch2}, env.provider.channels, "channels are sent concurrently, in any order")
	msg := env.provider.sent[0]
	assert.Equal(t, "critical", msg.Severity)
	assert.Equal(t, "monitor.down", msg.EventKind)
	assert.Equal(t, "m1", msg.ResourceID)
	assert.NotEmpty(t, msg.Title)
	logs := env.logs()
	require.Len(t, logs, 2)
	if logs[0].Channel != ch1 {
		logs[0], logs[1] = logs[1], logs[0]
	}
	assert.Equal(t, logRow{
		Rule: rule, Channel: ch1, CreatedBy: env.user, ChannelKind: "fake", EventKind: "monitor.down",
		ResourceID: "m1", ResourceName: "site m1", ResourceType: "monitor", Status: "sent",
		Preview: "preview of " + msg.Title,
	}, logs[0])
}

// Rules that do not apply send nothing and log nothing.
func TestDispatchRuleSelection(t *testing.T) {
	cases := map[string]struct {
		rule  map[string]any
		event Event
		sent  bool
	}{
		"disabled rule":         {map[string]any{"enabled": false, "events": []string{"monitor.down"}}, monitorDown("m1"), false},
		"event not listed":      {map[string]any{"events": []string{"monitor.up"}}, monitorDown("m1"), false},
		"no events":             {map[string]any{"events": []string{}}, monitorDown("m1"), false},
		"monitor in filter":     {map[string]any{"events": []string{"monitor.down"}, "filter": map[string]any{"monitor_ids": []string{"m1"}}}, monitorDown("m1"), true},
		"monitor not in filter": {map[string]any{"events": []string{"monitor.down"}, "filter": map[string]any{"monitor_ids": []string{"m2"}}}, monitorDown("m1"), false},
		// A filter covers only what it lists, as a maintenance scope does.
		"agent filter, monitor event": {map[string]any{"events": []string{"monitor.down"}, "filter": map[string]any{"agent_ids": []string{"a1"}}}, monitorDown("m1"), false},
		"empty filter lists":          {map[string]any{"events": []string{"monitor.down"}, "filter": map[string]any{"agent_ids": []string{}, "monitor_ids": []string{}}}, monitorDown("m1"), true},
		"agent in filter": {map[string]any{"events": []string{"agent.offline"}, "filter": map[string]any{"agent_ids": []string{"a1"}}},
			Event{Kind: EventAgentOffline, Resource: ResourceRef{ID: "a1", Type: "agent"}}, true},
		"agent not in filter": {map[string]any{"events": []string{"agent.offline"}, "filter": map[string]any{"agent_ids": []string{"a2"}}},
			Event{Kind: EventAgentOffline, Resource: ResourceRef{ID: "a1", Type: "agent"}}, false},
		"container on a filtered host": {map[string]any{"events": []string{"container_image.update_available"}, "filter": map[string]any{"agent_ids": []string{"a1"}}},
			Event{Kind: EventContainerImageUpdateAvailable, Resource: ResourceRef{ID: "a1|c1", Type: "container_image"}, Details: map[string]any{"agent_id": "a1"}}, true},
		"container host from its id": {map[string]any{"events": []string{"container_image.update_available"}, "filter": map[string]any{"agent_ids": []string{"a1"}}},
			Event{Kind: EventContainerImageUpdateAvailable, Resource: ResourceRef{ID: "a1|c1", Type: "container_image"}}, true},
		"container on another host": {map[string]any{"events": []string{"container_image.update_available"}, "filter": map[string]any{"agent_ids": []string{"a2"}}},
			Event{Kind: EventContainerImageUpdateAvailable, Resource: ResourceRef{ID: "a1|c1", Type: "container_image"}, Details: map[string]any{"agent_id": "a1"}}, false},
		"below min severity": {map[string]any{"events": []string{"monitor.up"}, "min_severity": "warning"},
			Event{Kind: EventMonitorUp, Resource: ResourceRef{ID: "m1", Type: "monitor"}}, false},
		"severity override meets min": {map[string]any{"events": []string{"container_image.update_available"}, "min_severity": "warning"},
			Event{Kind: EventContainerImageUpdateAvailable, Severity: "critical", Resource: ResourceRef{ID: "a1|c1", Type: "container_image"}}, true},
	}
	env := newDispatchEnv(t)
	channel := env.channel(nil)
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			env.reset()
			fields := map[string]any{"channels": []string{channel}}
			for k, v := range c.rule {
				fields[k] = v
			}
			env.rule(fields)
			env.d.process(context.Background(), c.event)
			if c.sent {
				assert.Len(t, env.provider.sent, 1)
				assert.Len(t, env.logs(), 1)
			} else {
				assert.Empty(t, env.provider.sent)
				assert.Empty(t, env.logs())
			}
		})
	}
}

func TestDispatchChannels(t *testing.T) {
	env := newDispatchEnv(t)
	disabled := env.channel(map[string]any{"enabled": false})
	unknown := env.channel(map[string]any{"kind": "carrier-pigeon"})
	ok := env.channel(nil)
	env.rule(map[string]any{"events": []string{"monitor.down"}, "channels": []string{disabled, unknown, "missing", ok}})

	env.d.process(context.Background(), monitorDown("m1"))
	assert.Equal(t, []string{ok}, env.provider.channels, "disabled, unknown-kind and missing channels are skipped")
	assert.Len(t, env.logs(), 1)
}

func TestDispatchThrottle(t *testing.T) {
	env := newDispatchEnv(t)
	rule := env.rule(map[string]any{"events": []string{"monitor.down", "host.metric_exceeded"}, "channels": []string{env.channel(nil)}, "throttle_seconds": 3600})

	env.d.process(context.Background(), monitorDown("m1"))
	env.d.process(context.Background(), monitorDown("m1"))
	env.d.process(context.Background(), monitorDown("m2")) // another resource
	metric := func(name, tier string) Event {
		return Event{Kind: EventHostMetricExceeded, Resource: ResourceRef{ID: "a1", Type: "agent"}, Details: map[string]any{"metric": name, "tier": tier}}
	}
	env.d.process(context.Background(), metric("cpu", "warning"))
	env.d.process(context.Background(), metric("disk", "warning")) // another metric
	env.d.process(context.Background(), metric("cpu", "critical")) // an escalation
	env.d.process(context.Background(), metric("cpu", "critical"))

	assert.Len(t, env.provider.sent, 5)
	var throttled []logRow
	for _, l := range env.logs() {
		if l.Status == "throttled" {
			throttled = append(throttled, l)
		}
	}
	require.Len(t, throttled, 2)
	assert.Equal(t, logRow{Rule: rule, CreatedBy: env.user, EventKind: "monitor.down", ResourceID: "m1", ResourceName: "site m1", ResourceType: "monitor", Status: "throttled"}, throttled[0])

	// Without throttle_seconds nothing is throttled.
	env2 := newDispatchEnv(t)
	env2.rule(map[string]any{"events": []string{"monitor.down"}, "channels": []string{env2.channel(nil)}})
	env2.d.process(context.Background(), monitorDown("m1"))
	env2.d.process(context.Background(), monitorDown("m1"))
	assert.Len(t, env2.provider.sent, 2)
}

func TestDispatchRetries(t *testing.T) {
	t.Run("succeeds after failures", func(t *testing.T) {
		env := newDispatchEnv(t)
		env.provider.failures = 2
		env.rule(map[string]any{"events": []string{"monitor.down"}, "channels": []string{env.channel(nil)}})
		env.d.process(context.Background(), monitorDown("m1"))
		assert.Equal(t, 3, env.provider.attempts)
		logs := env.logs()
		require.Len(t, logs, 1)
		assert.Equal(t, "sent", logs[0].Status)
	})
	t.Run("gives up", func(t *testing.T) {
		env := newDispatchEnv(t)
		env.provider.failures = 100
		env.rule(map[string]any{"events": []string{"monitor.down"}, "channels": []string{env.channel(nil)}})
		env.d.process(context.Background(), monitorDown("m1"))
		assert.Equal(t, maxRetries+1, env.provider.attempts)
		logs := env.logs()
		require.Len(t, logs, 1)
		assert.Equal(t, "failed", logs[0].Status)
		assert.Equal(t, "boom", logs[0].Error)
	})
	t.Run("out of time while waiting to retry", func(t *testing.T) {
		env := newDispatchEnv(t)
		env.d.retryDelays = []time.Duration{time.Hour, time.Hour, time.Hour}
		env.provider.failures = 100
		env.rule(map[string]any{"events": []string{"monitor.down"}, "channels": []string{env.channel(nil)}})
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		env.d.process(ctx, monitorDown("m1"))
		assert.Equal(t, 1, env.provider.attempts)
		logs := env.logs()
		require.Len(t, logs, 1)
		assert.Equal(t, "failed", logs[0].Status)
		assert.Equal(t, "context deadline exceeded (last error: boom)", logs[0].Error)
	})
}

// Every provider kind declares the config keys holding secrets; RedactConfig hides them
// (non-empty values only) and keeps the rest.
func TestRedactConfig(t *testing.T) {
	New(nil) // registers the providers
	cases := map[string]struct{ config, want map[string]any }{
		"slack":                   {map[string]any{"url": "https://hooks.slack.com/x", "channel": "#ops"}, map[string]any{"url": "**REDACTED**", "channel": "#ops"}},
		"teams":                   {map[string]any{"url": "https://teams/x"}, map[string]any{"url": "**REDACTED**"}},
		"gchat":                   {map[string]any{"url": "https://chat/x"}, map[string]any{"url": "**REDACTED**"}},
		"ntfy":                    {map[string]any{"url": "https://ntfy.sh/t", "token": "tk", "priority": 4.0}, map[string]any{"url": "**REDACTED**", "token": "**REDACTED**", "priority": 4.0}},
		"gotify":                  {map[string]any{"url": "https://g", "token": "tk"}, map[string]any{"url": "https://g", "token": "**REDACTED**"}},
		"webhook":                 {map[string]any{"url": "https://w", "headers": map[string]any{"Authorization": "x"}}, map[string]any{"url": "**REDACTED**", "headers": "**REDACTED**"}},
		"email":                   {map[string]any{"to": "a@b.c"}, map[string]any{"to": "a@b.c"}},
		"in-app":                  {map[string]any{}, map[string]any{}},
		"empty secret kept empty": {map[string]any{"url": ""}, map[string]any{"url": ""}},
	}
	for name, c := range cases {
		kind := name
		if name == "empty secret kept empty" {
			kind = "slack"
		}
		assert.Equal(t, c.want, RedactConfig(kind, c.config), name)
	}
	assert.Nil(t, RedactConfig("slack", nil))
}

func TestEmailProvider(t *testing.T) {
	env := newDispatchEnv(t)
	var sent []*mailer.Message
	env.app.OnMailerSend().BindFunc(func(e *core.MailerEvent) error {
		sent = append(sent, e.Message)
		return nil // captured, not sent
	})
	env.app.Settings().Meta.AppName = "Vigil"
	env.app.Settings().Meta.SenderAddress = "vigil@example.com"
	p := &providers.EmailProvider{App: env.app}
	msg := providers.Message{Title: "Monitor down", Body: "line 1\nline 2"}

	preview, err := p.Send(context.Background(), providers.Channel{Config: map[string]any{"to": "a@example.com, b@example.com", "cc": "c@example.com", "bcc": "d@example.com"}}, msg)
	require.NoError(t, err)
	require.Len(t, sent, 1)
	m := sent[0]
	assert.Equal(t, "Monitor down", m.Subject)
	assert.Equal(t, mail.Address{Name: "Vigil", Address: "vigil@example.com"}, m.From)
	assert.Equal(t, []mail.Address{{Address: "a@example.com"}, {Address: "b@example.com"}}, m.To)
	assert.Equal(t, []mail.Address{{Address: "c@example.com"}}, m.Cc)
	assert.Equal(t, []mail.Address{{Address: "d@example.com"}}, m.Bcc)
	assert.Equal(t, "line 1\nline 2", m.Text)
	assert.Equal(t, "<p>line 1<br>line 2</p>", m.HTML)
	assert.Contains(t, preview, "a@example.com")

	_, err = p.Send(context.Background(), providers.Channel{Config: map[string]any{"to": " , "}}, msg)
	assert.Error(t, err, "no valid address")
	_, err = p.Send(context.Background(), providers.Channel{Config: map[string]any{}}, msg)
	assert.Error(t, err)
}
