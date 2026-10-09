package providers

import (
	"context"
	"fmt"
	"net/mail"
	"strings"
	"sync/atomic"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/mailer"
)

// MaxPendingEmails caps the sends abandoned at their deadline that may still be running.
const MaxPendingEmails = 4

// EmailProvider sends notifications via SMTP using the PocketBase mailer.
//
// PocketBase's SMTP client dials and talks with no deadline: a server that accepts the
// connection then stalls would hold the caller forever. Send runs it in a goroutine and
// returns at its context's deadline; that goroutine cannot be stopped, so at most
// MaxPendingEmails may be left running, beyond which Send fails at once.
type EmailProvider struct {
	App     core.App
	pending atomic.Int32
}

func (p *EmailProvider) Kind() string { return "email" }

func (p *EmailProvider) SensitiveConfigKeys() []string { return nil }

func (p *EmailProvider) ValidateConfig(raw map[string]any) error {
	_, err := requiredConfigString(raw, "to")
	return err
}

func (p *EmailProvider) Send(ctx context.Context, ch Channel, msg Message) (string, error) {
	to, err := requiredConfigString(ch.Config, "to")
	if err != nil {
		return "", err
	}

	toAddresses := parseEmailAddresses(to)
	if len(toAddresses) == 0 {
		return "", fmt.Errorf("email provider: invalid 'to' address: %q", to)
	}

	settings := p.App.Settings()
	from := mail.Address{
		Name:    settings.Meta.AppName,
		Address: settings.Meta.SenderAddress,
	}
	if from.Address == "" {
		from.Address = "noreply@vigil.local"
	}

	htmlBody := "<p>" + strings.ReplaceAll(msg.Body, "\n", "<br>") + "</p>"

	message := &mailer.Message{
		From:    from,
		To:      toAddresses,
		Subject: msg.Title,
		HTML:    htmlBody,
		Text:    msg.Body,
	}

	if cc, ok := configString(ch.Config, "cc"); ok {
		message.Cc = parseEmailAddresses(cc)
	}
	if bcc, ok := configString(ch.Config, "bcc"); ok {
		message.Bcc = parseEmailAddresses(bcc)
	}

	if err := p.sendBounded(ctx, message); err != nil {
		return "", fmt.Errorf("email send: %w", err)
	}

	preview := fmt.Sprintf("to=%s subject=%q", to, msg.Title)
	return preview, nil
}

func (p *EmailProvider) sendBounded(ctx context.Context, message *mailer.Message) error {
	if p.pending.Add(1) > MaxPendingEmails {
		p.pending.Add(-1)
		return fmt.Errorf("%d earlier sends are still stuck on the SMTP server (they end only when it answers or the hub restarts)", MaxPendingEmails)
	}
	done := make(chan error, 1)
	go func() {
		defer p.pending.Add(-1)
		done <- p.App.NewMailClient().Send(message)
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func parseEmailAddresses(raw string) []mail.Address {
	var result []mail.Address
	for _, addr := range strings.Split(raw, ",") {
		addr = strings.TrimSpace(addr)
		if addr != "" {
			result = append(result, mail.Address{Address: addr})
		}
	}
	return result
}
