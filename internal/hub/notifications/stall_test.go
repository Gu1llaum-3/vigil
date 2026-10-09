//go:build testing

package notifications

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/hub/notifications/providers"
	"github.com/pocketbase/pocketbase/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An SMTP server that accepts the connection then stalls must not hold a worker: Send
// returns at its context's deadline, and abandoned sends are capped.
func TestEmailSendIsBoundedByItsContext(t *testing.T) {
	env := newDispatchEnv(t)
	release := make(chan struct{})
	defer close(release)
	env.app.OnMailerSend().BindFunc(func(e *core.MailerEvent) error {
		<-release // the stalled server
		return nil
	})
	p := &providers.EmailProvider{App: env.app}
	ch := providers.Channel{Config: map[string]any{"to": "a@example.com"}}
	send := func(timeout time.Duration) (time.Duration, error) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		start := time.Now()
		_, err := p.Send(ctx, ch, providers.Message{Title: "t"})
		return time.Since(start), err
	}

	took, err := send(50 * time.Millisecond)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, took, 2*time.Second)

	for range providers.MaxPendingEmails - 1 {
		_, err := send(time.Millisecond)
		require.ErrorIs(t, err, context.DeadlineExceeded)
	}
	took, err = send(5 * time.Second)
	require.ErrorContains(t, err, "still stuck")
	assert.Less(t, took, time.Second, "refused at once while too many sends are stuck")
}

// blockingProvider blocks every send until released or its context ends.
type blockingProvider struct {
	release chan struct{}
	started chan struct{}
}

func (p *blockingProvider) Kind() string                        { return "blocking" }
func (p *blockingProvider) SensitiveConfigKeys() []string       { return nil }
func (p *blockingProvider) ValidateConfig(map[string]any) error { return nil }
func (p *blockingProvider) Send(ctx context.Context, _ providers.Channel, _ providers.Message) (string, error) {
	p.started <- struct{}{}
	select {
	case <-p.release:
		return "released", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// The channels of an event are delivered concurrently: a stuck channel does not delay the
// others.
func TestDispatchChannelsConcurrently(t *testing.T) {
	env := newDispatchEnv(t)
	blocking := &blockingProvider{release: make(chan struct{}), started: make(chan struct{}, 1)}
	env.d.providerMap["blocking"] = blocking
	slow := env.channel(map[string]any{"kind": "blocking"})
	fast := env.channel(nil)
	env.rule(map[string]any{"events": []string{"monitor.down"}, "channels": []string{slow, fast}})

	done := make(chan struct{})
	go func() {
		env.d.process(context.Background(), monitorDown("m1"))
		close(done)
	}()
	<-blocking.started
	require.Eventually(t, func() bool {
		env.provider.mu.Lock()
		defer env.provider.mu.Unlock()
		return len(env.provider.sent) == 1
	}, 2*time.Second, 5*time.Millisecond, "the working channel is not held by the stuck one")
	close(blocking.release)
	<-done
	assert.Len(t, env.logs(), 2)
}

// A channel that keeps failing is skipped for a while instead of holding a worker through
// every retry of every event; the skips are logged once per opening.
func TestDispatchSkipsAFailingChannel(t *testing.T) {
	env := newDispatchEnv(t)
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	env.d.now = func() time.Time { return now }
	env.provider.failures = 1000
	channel := env.channel(nil)
	env.rule(map[string]any{"events": []string{"monitor.down"}, "channels": []string{channel}})
	attemptsPerDelivery := maxRetries + 1
	skippedLogs := func() int {
		n := 0
		for _, l := range env.logs() {
			if strings.HasPrefix(l.Error, "skipped:") {
				assert.Equal(t, channel, l.Channel)
				n++
			}
		}
		return n
	}

	for range breakerThreshold {
		env.d.process(context.Background(), monitorDown("m1"))
	}
	assert.Equal(t, breakerThreshold*attemptsPerDelivery, env.provider.attempts)

	for range 3 {
		env.d.process(context.Background(), monitorDown("m1"))
	}
	assert.Equal(t, breakerThreshold*attemptsPerDelivery, env.provider.attempts, "skipped: the provider is not called")
	assert.Equal(t, 1, skippedLogs(), "one log per opening, not one per skipped event")

	// After the cooldown a delivery probes the channel; it fails: open again, logged again.
	now = now.Add(breakerCooldown + time.Second)
	env.d.process(context.Background(), monitorDown("m1"))
	assert.Equal(t, (breakerThreshold+1)*attemptsPerDelivery, env.provider.attempts)
	env.d.process(context.Background(), monitorDown("m1"))
	assert.Equal(t, (breakerThreshold+1)*attemptsPerDelivery, env.provider.attempts)
	assert.Equal(t, 2, skippedLogs())

	// The probe succeeds: closed, and a single failure afterwards does not reopen it.
	now = now.Add(breakerCooldown + time.Second)
	env.provider.failures = 0
	env.provider.attempts = 0
	env.d.process(context.Background(), monitorDown("m1"))
	require.Len(t, env.provider.sent, 1)
	env.provider.failures, env.provider.attempts = 1000, 0
	env.d.process(context.Background(), monitorDown("m1"))
	env.d.process(context.Background(), monitorDown("m1"))
	assert.Equal(t, 2*attemptsPerDelivery, env.provider.attempts, "two failures after a success: still closed")
}

// When the cooldown ends, a single delivery probes the channel; the others stay skipped
// until its outcome is recorded.
func TestBreakerSingleProbe(t *testing.T) {
	d := New(nil)
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	d.now = func() time.Time { return now }
	for range breakerThreshold {
		d.recordDelivery("c", false)
	}
	skip, logIt, _ := d.breakerCheck("c")
	assert.True(t, skip)
	assert.True(t, logIt)
	skip, logIt, _ = d.breakerCheck("c")
	assert.True(t, skip)
	assert.False(t, logIt)

	now = now.Add(breakerCooldown)
	skip, _, _ = d.breakerCheck("c")
	assert.False(t, skip, "the probe")
	skip, _, _ = d.breakerCheck("c")
	assert.True(t, skip, "while the probe runs")

	d.ResetChannel("c") // edited or tested successfully
	skip, _, _ = d.breakerCheck("c")
	assert.False(t, skip)
}

// A channel that does not answer within the delivery budget counts as a failed delivery.
func TestDispatchSlowChannelCountsAsFailure(t *testing.T) {
	env := newDispatchEnv(t)
	env.d.sendTimeout = 20 * time.Millisecond
	blocking := &blockingProvider{release: make(chan struct{}), started: make(chan struct{}, 100)}
	env.d.providerMap["blocking"] = blocking
	env.rule(map[string]any{"events": []string{"monitor.down"}, "channels": []string{env.channel(map[string]any{"kind": "blocking"})}})
	for range breakerThreshold + 1 {
		env.d.process(context.Background(), monitorDown("m1"))
	}
	assert.Len(t, blocking.started, breakerThreshold, "the fourth event skips the channel")
	for _, l := range env.logs()[:breakerThreshold] {
		assert.Equal(t, "context deadline exceeded", l.Error)
	}
}

// Rules are served concurrently too: a rule stuck on its channel does not delay another.
func TestDispatchRulesConcurrently(t *testing.T) {
	env := newDispatchEnv(t)
	blocking := &blockingProvider{release: make(chan struct{}), started: make(chan struct{}, 1)}
	env.d.providerMap["blocking"] = blocking
	env.rule(map[string]any{"events": []string{"monitor.down"}, "channels": []string{env.channel(map[string]any{"kind": "blocking"})}})
	env.rule(map[string]any{"events": []string{"monitor.down"}, "channels": []string{env.channel(nil)}})

	done := make(chan struct{})
	go func() {
		env.d.process(context.Background(), monitorDown("m1"))
		close(done)
	}()
	<-blocking.started
	require.Eventually(t, func() bool {
		env.provider.mu.Lock()
		defer env.provider.mu.Unlock()
		return len(env.provider.sent) == 1
	}, 2*time.Second, 5*time.Millisecond)
	close(blocking.release)
	<-done
}

// A panicking provider is contained.
func TestDispatchSurvivesAPanickingProvider(t *testing.T) {
	env := newDispatchEnv(t)
	env.d.providerMap["panicking"] = panickingProvider{}
	env.rule(map[string]any{"events": []string{"monitor.down"}, "channels": []string{env.channel(map[string]any{"kind": "panicking"}), env.channel(nil)}})
	require.NotPanics(t, func() { env.d.process(context.Background(), monitorDown("m1")) })
	assert.Len(t, env.provider.sent, 1)
}

type panickingProvider struct{}

func (panickingProvider) Kind() string                        { return "panicking" }
func (panickingProvider) SensitiveConfigKeys() []string       { return nil }
func (panickingProvider) ValidateConfig(map[string]any) error { return nil }
func (panickingProvider) Send(context.Context, providers.Channel, providers.Message) (string, error) {
	panic("boom")
}

// Events dropped on a full queue are reported in the delivery history, once per report.
func TestDispatchReportsDroppedEvents(t *testing.T) {
	env := newDispatchEnv(t)
	env.d.events = make(chan Event, 1)
	for range 4 {
		env.d.Dispatch(monitorDown("m1"))
	}
	env.d.reportDropped()
	env.d.reportDropped()
	logs := env.logs()
	require.Len(t, logs, 1)
	assert.Equal(t, "failed", logs[0].Status)
	assert.True(t, strings.Contains(logs[0].Error, "3 events dropped"), logs[0].Error)
}
