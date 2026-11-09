// Package mail sends Changeport's email in plain language, each message as
// text with a small HTML version: to operators about trial requests, and to
// requesters about their trials. Every send is retried, and audited by
// template and recipients only, never by what it says.
package mail

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// Template names a message.
type Template string

// The messages, with the data each renders.
const (
	// TrialRequested tells operators of a new trial request: TrialRequest.
	TrialRequested Template = "trial-requested"
	// TrialApproved tells a requester their trial is approved: TrialDecision.
	TrialApproved Template = "trial-approved"
	// TrialDeclined tells a requester their request is declined, and why:
	// TrialDecision.
	TrialDeclined Template = "trial-declined"
	// TrialEnding tells a customer their trial ends in three days: Trial.
	TrialEnding Template = "trial-ending"
	// TrialEnded tells a customer their trial has ended: Trial.
	TrialEnded Template = "trial-ended"
)

// TrialRequest is what TrialRequested says.
type TrialRequest struct {
	ID, Company, Email, Role, UseCase, Throughput, Retention, Start string
	// Link opens the operator console's trial requests.
	Link string
}

// TrialDecision is what TrialApproved and TrialDeclined say.
type TrialDecision struct {
	Company string
	// Link opens the console, for an approval.
	Link string
	// EndsAt is when an approved trial ends.
	EndsAt string
	// Reason is why a request was declined, and AskAgain when its requester
	// may ask again.
	Reason, AskAgain string
}

// Trial is what TrialEnding and TrialEnded say.
type Trial struct {
	Company, EndsAt string
	// Link opens the console.
	Link string
}

// Message is a rendered message.
type Message struct {
	To                  []string
	Subject, Text, HTML string
}

// Sender delivers one message from an address.
type Sender interface {
	Send(ctx context.Context, from string, message Message) error
}

// Mailer renders and sends messages. Without a Sender, mail is off: sends
// are logged and dropped, as on a laptop without a mail server.
type Mailer struct {
	Sender Sender
	// From is the address messages come from, and replies go to.
	From   string
	Logger *slog.Logger
	// Attempts is how often a send is tried, Backoff how long the first
	// retry waits, doubling after.
	Attempts int
	Backoff  time.Duration
}

// Send renders template with data and sends it to to, retrying.
func (m *Mailer) Send(ctx context.Context, template Template, to []string, data any) error {
	if len(to) == 0 {
		return errors.New("mail to nobody")
	}
	message, err := Render(template, data)
	if err != nil {
		return err
	}
	message.To = to
	if m.Sender == nil {
		m.Logger.InfoContext(ctx, "mail is off; not sent", "template", string(template), "to", to)
		return nil
	}
	attempts, wait := max(m.Attempts, 1), m.Backoff
	for attempt := 1; ; attempt++ {
		err = m.Sender.Send(ctx, m.From, message)
		if err == nil {
			m.Logger.InfoContext(ctx, "mail sent", "audit", true, "template", string(template), "to", to)
			return nil
		}
		if attempt == attempts || ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
		wait *= 2
	}
	m.Logger.ErrorContext(ctx, "mail not sent", "audit", true, "template", string(template), "to", to, "error", err)
	return fmt.Errorf("send %s to %v: %w", template, to, err)
}

// FormatTime is a time as messages say it, such as 22 November 2026,
// 09:00 UTC.
func FormatTime(t time.Time) string {
	return t.UTC().Format("2 January 2006, 15:04 UTC")
}
