package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// SMTP sends through an SMTP server without authentication or TLS, such as
// Mailpit on kind (deploy/overlays/kind/mailpit.yaml). The AWS environments
// send through SES instead.
type SMTP struct {
	// Addr is the server, as host:port.
	Addr string
}

var _ Sender = SMTP{}

// Send implements Sender.
func (s SMTP) Send(ctx context.Context, from string, message Message) error {
	body, err := mimeMessage(from, message)
	if err != nil {
		return err
	}
	dialer := net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", s.Addr)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", s.Addr, err)
	}
	defer func() { _ = conn.Close() }()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	host, _, _ := net.SplitHostPort(s.Addr)
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("greet %s: %w", s.Addr, err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("from %s: %w", from, err)
	}
	for _, to := range message.To {
		if err := client.Rcpt(to); err != nil {
			return fmt.Errorf("to %s: %w", to, err)
		}
	}
	data, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := data.Write(body); err != nil {
		return err
	}
	if err := data.Close(); err != nil {
		return err
	}
	return client.Quit()
}

// mimeMessage is message as an email with a text and an HTML part.
func mimeMessage(from string, message Message) ([]byte, error) {
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		return nil, err
	}
	boundary := "changeport-" + hex.EncodeToString(random)
	domain := from[strings.LastIndex(from, "@")+1:]
	var b bytes.Buffer
	header := func(name, value string) { fmt.Fprintf(&b, "%s: %s\r\n", name, value) }
	header("From", from)
	header("To", strings.Join(message.To, ", "))
	header("Subject", mime.QEncoding.Encode("utf-8", message.Subject))
	header("Date", time.Now().UTC().Format(time.RFC1123Z))
	header("Message-ID", "<"+hex.EncodeToString(random)+"@"+domain+">")
	header("MIME-Version", "1.0")
	header("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
	b.WriteString("\r\n")
	for _, part := range []struct{ contentType, content string }{
		{"text/plain", message.Text},
		{"text/html", message.HTML},
	} {
		fmt.Fprintf(&b, "--%s\r\nContent-Type: %s; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", boundary, part.contentType)
		encoder := quotedprintable.NewWriter(&b)
		if _, err := encoder.Write([]byte(part.content)); err != nil {
			return nil, err
		}
		if err := encoder.Close(); err != nil {
			return nil, err
		}
		b.WriteString("\r\n")
	}
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return b.Bytes(), nil
}
