package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

var data = map[Template]any{
	TrialRequested: TrialRequest{
		ID: "tr-7f3k2q", Company: "Hooli", Email: "ada@example.com", Role: "Staff engineer",
		UseCase: "Orders <between> services", Throughput: "1 to 10 MB/s", Retention: "Up to a week",
		Start: "As soon as possible", Link: "https://console.example.com/operator/trials",
	},
	TrialApproved: TrialDecision{Company: "Hooli", Link: "https://console.example.com/", EndsAt: "22 November 2026, 09:00 UTC"},
	TrialDeclined: TrialDecision{Company: "Hooli", Reason: "We start with teams in the EU <for now>.", AskAgain: "15 November 2026"},
	TrialEnding:   Trial{Company: "Hooli", EndsAt: "22 November 2026, 09:00 UTC", Link: "https://console.example.com/"},
	TrialEnded:    Trial{Company: "Hooli", EndsAt: "22 November 2026, 09:00 UTC", Link: "https://console.example.com/"},
}

func TestEveryTemplateRenders(t *testing.T) {
	for template, values := range data {
		t.Run(string(template), func(t *testing.T) {
			message, err := Render(template, values)
			if err != nil {
				t.Fatal(err)
			}
			if message.Subject == "" || strings.ContainsAny(message.Subject, "\n<") {
				t.Errorf("subject %q, want one plain line", message.Subject)
			}
			if !strings.Contains(message.Text, "Hooli") || strings.Contains(message.Text, "<p>") || strings.Contains(message.Text, "\n\n\n") {
				t.Errorf("text %q, want plain text about Hooli", message.Text)
			}
			if !strings.HasPrefix(message.HTML, "<!doctype html>") || !strings.Contains(message.HTML, "<title>"+message.Subject+"</title>") {
				t.Errorf("HTML %q, want a page titled by the subject", message.HTML)
			}
		})
	}
}

func TestMessagesSayWhatMatters(t *testing.T) {
	render := func(template Template) Message {
		t.Helper()
		message, err := Render(template, data[template])
		if err != nil {
			t.Fatal(err)
		}
		return message
	}
	requested := render(TrialRequested)
	if requested.Subject != "New trial request from Hooli" || !strings.Contains(requested.Text, "Who: ada@example.com, Staff engineer") ||
		!strings.Contains(requested.Text, "https://console.example.com/operator/trials") {
		t.Errorf("the operators' message = %+v", requested)
	}
	// What people typed is text, never markup.
	if !strings.Contains(requested.HTML, "Orders &lt;between&gt; services") || !strings.Contains(requested.Text, "Orders <between> services") {
		t.Errorf("the use case is not escaped in HTML only: %s", requested.HTML)
	}
	if approved := render(TrialApproved); !strings.Contains(approved.HTML, `<a href="https://console.example.com/">Open your console</a>`) ||
		!strings.Contains(approved.Text, "until 22 November 2026, 09:00 UTC") {
		t.Errorf("the approval = %+v", approved)
	}
	if declined := render(TrialDeclined); !strings.Contains(declined.HTML, "We start with teams in the EU &lt;for now&gt;.") ||
		!strings.Contains(declined.Text, "again from 15 November 2026") {
		t.Errorf("the decline = %+v", declined)
	}
	if ending := render(TrialEnding); ending.Subject != "Your Changeport trial ends in 3 days" ||
		!strings.Contains(ending.Text, "ends on 22 November 2026, 09:00 UTC") {
		t.Errorf("the warning = %+v", ending)
	}
	if _, err := Render(TrialEnding, struct{ Company string }{"Hooli"}); err == nil {
		t.Error("rendering without the data the template needs succeeded")
	}
}

// flakySender fails its first failures sends, and records the rest.
type flakySender struct {
	failures int
	sent     []Message
}

func (f *flakySender) Send(_ context.Context, _ string, message Message) error {
	if f.failures > 0 {
		f.failures--
		return errors.New("421 try again later")
	}
	f.sent = append(f.sent, message)
	return nil
}

func TestMailerRetriesAndAuditsByTemplateAndRecipient(t *testing.T) {
	var log bytes.Buffer
	sender := &flakySender{failures: 2}
	mailer := &Mailer{Sender: sender, From: "trials@example.com", Logger: slog.New(slog.NewJSONHandler(&log, nil)), Attempts: 3, Backoff: time.Millisecond}
	if err := mailer.Send(t.Context(), TrialDeclined, []string{"ada@example.com"}, data[TrialDeclined]); err != nil {
		t.Fatal(err)
	}
	if len(sender.sent) != 1 || sender.sent[0].To[0] != "ada@example.com" {
		t.Fatalf("sent %+v, want the decline to ada after two retries", sender.sent)
	}
	if !strings.Contains(log.String(), `"msg":"mail sent","audit":true,"template":"trial-declined","to":["ada@example.com"]`) {
		t.Errorf("no audit line for the send in %s", log.String())
	}
	if strings.Contains(log.String(), "EU") || strings.Contains(log.String(), "Subject") {
		t.Errorf("the audit log says what the message says: %s", log.String())
	}

	log.Reset()
	sender.failures = 5
	if err := mailer.Send(t.Context(), TrialDeclined, []string{"ada@example.com"}, data[TrialDeclined]); err == nil {
		t.Error("a send that failed three times succeeded")
	}
	if !strings.Contains(log.String(), `"msg":"mail not sent","audit":true,"template":"trial-declined"`) {
		t.Errorf("no audit line for the failure in %s", log.String())
	}
}

func TestMailOff(t *testing.T) {
	var log bytes.Buffer
	mailer, err := New(t.Context(), "", "", slog.New(slog.NewJSONHandler(&log, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := mailer.Send(t.Context(), TrialEnded, []string{"ada@example.com"}, data[TrialEnded]); err != nil || !strings.Contains(log.String(), "mail is off") {
		t.Errorf("with mail off, Send() = %v and logged %s", err, log.String())
	}
	for where, from := range map[string]string{
		"smtp://":                 "trials@example.com",
		"pop3://mail.example.com": "trials@example.com",
		"smtp://mailpit:1025":     "Changeport <trials@example.com>",
	} {
		if _, err := New(t.Context(), where, from, slog.Default()); err == nil {
			t.Errorf("New(%q, %q) succeeded", where, from)
		}
	}
	if m, err := New(t.Context(), "smtp://mailpit.mail:1025", "trials@example.com", slog.Default()); err != nil || m.Sender != (SMTP{Addr: "mailpit.mail:1025"}) {
		t.Errorf("New(smtp://mailpit.mail:1025) = %+v, %v", m, err)
	}
}

// TestSMTPToMailpit sends through the Mailpit image kind runs and reads the
// message back from its API; it runs where Docker does (just test).
func TestSMTPToMailpit(t *testing.T) {
	if os.Getenv("CHANGEPORT_DOCKER_TESTS") == "" {
		t.Skip("CHANGEPORT_DOCKER_TESTS is not set; just test sets it")
	}
	smtpPort, apiPort := freePort(t), freePort(t)
	manifest, err := os.ReadFile("../../deploy/overlays/kind/mailpit.yaml")
	if err != nil {
		t.Fatal(err)
	}
	image := ""
	for line := range strings.SplitSeq(string(manifest), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "image: "); ok {
			image = value
		}
	}
	name := fmt.Sprintf("changeport-mail-test-%d", smtpPort)
	if output, err := exec.CommandContext(t.Context(), "docker", "run", "-d", "--rm", "--name", name,
		"-p", fmt.Sprintf("127.0.0.1:%d:1025", smtpPort), "-p", fmt.Sprintf("127.0.0.1:%d:8025", apiPort), image).CombinedOutput(); err != nil {
		t.Fatalf("start Mailpit: %v\n%s", err, output)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })
	api := fmt.Sprintf("http://127.0.0.1:%d/api/v1", apiPort)
	waitFor(t, api+"/messages")

	mailer := &Mailer{Sender: SMTP{Addr: fmt.Sprintf("127.0.0.1:%d", smtpPort)}, From: "trials@changeport.test", Logger: slog.Default(), Attempts: 3, Backoff: 500 * time.Millisecond}
	if err := mailer.Send(t.Context(), TrialApproved, []string{"ada@example.com"}, TrialDecision{
		Company: "Hööli", Link: "https://console.example.com/", EndsAt: "22 November 2026, 09:00 UTC",
	}); err != nil {
		t.Fatal(err)
	}
	var list struct {
		Messages []struct {
			ID      string
			Subject string
			From    struct{ Address string }
			To      []struct{ Address string }
		}
	}
	getJSON(t, api+"/messages", &list)
	if len(list.Messages) != 1 || list.Messages[0].Subject != "Your Changeport trial is approved" ||
		list.Messages[0].From.Address != "trials@changeport.test" || list.Messages[0].To[0].Address != "ada@example.com" {
		t.Fatalf("Mailpit has %+v, want the approval to ada", list.Messages)
	}
	var message struct{ Text, HTML string }
	getJSON(t, api+"/message/"+list.Messages[0].ID, &message)
	if !strings.Contains(message.Text, "Your trial request for Hööli is approved.") || !strings.Contains(message.HTML, `<a href="https://console.example.com/">`) {
		t.Errorf("the message read back = %+v", message)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	return listener.Addr().(*net.TCPAddr).Port
}

func waitFor(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		request, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, url, http.NoBody)
		if response, err := http.DefaultClient.Do(request); err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not answer", url)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func getJSON(t *testing.T, url string, into any) {
	t.Helper()
	request, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, url, http.NoBody)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if err := json.NewDecoder(response.Body).Decode(into); err != nil {
		t.Fatal(err)
	}
}
