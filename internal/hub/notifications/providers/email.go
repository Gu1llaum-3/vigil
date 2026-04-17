package providers

import (
	"context"
	"fmt"
	"net/mail"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/mailer"
)

// EmailProvider sends notifications via SMTP using the PocketBase mailer.
type EmailProvider struct {
	App core.App
}

func (p *EmailProvider) Kind() string { return "email" }

func (p *EmailProvider) SensitiveConfigKeys() []string { return nil }

func (p *EmailProvider) ValidateConfig(raw map[string]any) error {
	_, err := requiredConfigString(raw, "to")
	return err
}

func (p *EmailProvider) Send(_ context.Context, ch Channel, msg Message) (string, error) {
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

	if err := p.App.NewMailClient().Send(message); err != nil {
		return "", fmt.Errorf("email send: %w", err)
	}

	preview := fmt.Sprintf("to=%s subject=%q", to, msg.Title)
	return preview, nil
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
