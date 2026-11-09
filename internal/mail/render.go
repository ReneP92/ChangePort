package mail

import (
	"bytes"
	"embed"
	"fmt"
	htmltemplate "html/template"
	"strings"
	texttemplate "text/template"
)

//go:embed templates/*.tmpl
var files embed.FS

// The templates, parsed once: each file defines its subject, its text and
// its HTML, which the layout wraps.
var (
	texts = texttemplate.Must(texttemplate.New("").Option("missingkey=error").ParseFS(files, "templates/*.tmpl"))
	htmls = htmltemplate.Must(htmltemplate.New("").Option("missingkey=error").ParseFS(files, "templates/*.tmpl"))
)

// Render renders template with data into a message without recipients.
func Render(template Template, data any) (Message, error) {
	var subject, text, html bytes.Buffer
	if err := texts.ExecuteTemplate(&subject, string(template)+"-subject", data); err != nil {
		return Message{}, fmt.Errorf("render the subject of %s: %w", template, err)
	}
	if err := texts.ExecuteTemplate(&text, string(template)+"-text", data); err != nil {
		return Message{}, fmt.Errorf("render the text of %s: %w", template, err)
	}
	page := struct {
		Subject string
		Body    any
	}{Subject: strings.TrimSpace(subject.String())}
	var body bytes.Buffer
	if err := htmls.ExecuteTemplate(&body, string(template)+"-html", data); err != nil {
		return Message{}, fmt.Errorf("render the HTML of %s: %w", template, err)
	}
	page.Body = htmltemplate.HTML(body.String()) //nolint:gosec // rendered by html/template above
	if err := htmls.ExecuteTemplate(&html, "layout", page); err != nil {
		return Message{}, fmt.Errorf("render the layout of %s: %w", template, err)
	}
	return Message{Subject: page.Subject, Text: strings.TrimSpace(text.String()) + "\n", HTML: html.String()}, nil
}
