package mail

import (
	"context"
	"fmt"
	"log/slog"
	"net/mail"
	"net/url"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
)

// New returns a Mailer for where, from from: smtp://host:port for an SMTP
// server, such as Mailpit on kind; ses for Amazon SES in the AWS region of
// the environment; or nothing, for mail off.
func New(ctx context.Context, where, from string, logger *slog.Logger) (*Mailer, error) {
	mailer := &Mailer{From: from, Logger: logger, Attempts: 4, Backoff: time.Second}
	if where == "" {
		return mailer, nil
	}
	if address, err := mail.ParseAddress(from); err != nil || address.Address != from {
		return nil, fmt.Errorf("mail from %q: name an address, such as trials@example.com", from)
	}
	switch target, err := url.Parse(where); {
	case err == nil && target.Scheme == "smtp" && target.Host != "":
		mailer.Sender = SMTP{Addr: target.Host}
	case where == "ses":
		aws, err := config.LoadDefaultConfig(ctx)
		if err != nil {
			return nil, fmt.Errorf("load the AWS configuration for SES: %w", err)
		}
		mailer.Sender = SES{Client: sesv2.NewFromConfig(aws)}
	default:
		return nil, fmt.Errorf("mail through %q: use smtp://host:port or ses", where)
	}
	return mailer, nil
}
