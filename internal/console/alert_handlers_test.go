package console

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	"github.com/ReneP92/changeport/api/v1alpha1"
	"github.com/ReneP92/changeport/internal/console/api"
)

// fakeAlerts holds the customers' active alerts: acme's consumer group
// acme-billing is behind on acme-orders.
type fakeAlerts struct {
	mu     sync.Mutex
	alerts map[string][]api.Alert
}

func newFakeAlerts() *fakeAlerts {
	return &fakeAlerts{alerts: map[string][]api.Alert{
		"acme": {{
			Name: "ConsumerGroupLagHigh", Severity: "warn",
			Summary: "acme-billing is behind", Description: "Its oldest message on acme-orders is old.",
			StartsAt: time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC), Topic: ptr.To("acme-orders"), ConsumerGroup: ptr.To("acme-billing"),
		}},
	}}
}

func (f *fakeAlerts) Active(_ context.Context, customerID string) ([]api.Alert, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]api.Alert{}, f.alerts[customerID]...), nil
}

func (f *fakeAlerts) set(customerID string, alerts ...api.Alert) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.alerts[customerID] = alerts
}

// fakeAlertSettings keeps settings by customer, and the receivers' secrets
// by Secret name.
type fakeAlertSettings struct {
	settings map[string]*v1alpha1.AlertSettings
	secrets  map[string]string
	saves    int
}

func (f *fakeAlertSettings) Get(_ context.Context, customerID string) (*v1alpha1.AlertSettings, error) {
	if settings, ok := f.settings[customerID]; ok {
		return settings.DeepCopy(), nil
	}
	return nil, nil
}

func (f *fakeAlertSettings) Save(_ context.Context, customerID string, settings *v1alpha1.AlertSettings) error {
	current, exists := f.settings[customerID]
	if (settings.ResourceVersion == "" && exists) || (exists && current.ResourceVersion != settings.ResourceVersion) {
		return ErrAlertSettingsChanged
	}
	f.saves++
	settings.Namespace, settings.Name = namespaceOf(customerID), v1alpha1.AlertSettingsName
	settings.UID = types.UID("uid-" + customerID)
	settings.ResourceVersion = strconv.Itoa(f.saves)
	f.settings[customerID] = settings.DeepCopy()
	return nil
}

func (f *fakeAlertSettings) WriteReceiver(_ context.Context, settings *v1alpha1.AlertSettings, kind api.ReceiverKind, value string) (string, error) {
	if settings.UID == "" {
		return "", errors.New("the settings must exist before the Secret they own")
	}
	if f.secrets == nil {
		f.secrets = map[string]string{}
	}
	name := fmt.Sprintf("%s%s-%d", receiverSecretPrefix, strings.ToLower(string(kind)), len(f.secrets))
	f.secrets[name] = value
	return name, nil
}

func TestMembersReadAlertsAndOnlyAdminsChangeThem(t *testing.T) {
	f := newFixture(t)
	member := f.signer.token(t, session{user: "user_2", org: "org_acme", role: "member"})
	staff := f.signer.token(t, session{user: "user_6", org: staffOrg, role: "admin"})
	for name, token := range map[string]string{"a member": member, "staff": staff} {
		for _, r := range []struct{ method, path, body string }{
			{http.MethodPut, "/api/customer/alert-settings", defaultThresholds},
			{http.MethodPut, "/api/customer/alert-settings/receivers/webhook", `{"value":"https://alerts.example.com/"}`},
			{http.MethodDelete, "/api/customer/alert-settings/receivers/webhook", ""},
		} {
			if status, body := send(t, f.handler, r.method, r.path, token, r.body); status != 403 {
				t.Errorf("%s: %s %s = %d %s, want 403", name, r.method, r.path, status, body)
			}
		}
	}
	if len(f.alertSettings.settings) != 0 || len(f.alertSettings.secrets) != 0 {
		t.Errorf("refused requests changed the settings: %+v", f.alertSettings)
	}
}

const defaultThresholds = `{"consumerLag":{"enabled":true,"seconds":600},"consumerStuck":{"enabled":true,"seconds":900},` +
	`"throttling":{"enabled":true,"milliseconds":100},"errors":{"enabled":true,"percent":5}}`

func TestAdminsChangeThresholds(t *testing.T) {
	f := newFixture(t)
	admin := f.signer.token(t, session{user: "user_3", org: "org_acme", role: "admin"})
	changed := strings.NewReplacer(`"seconds":600`, `"seconds":60`, `"throttling":{"enabled":true`, `"throttling":{"enabled":false`).Replace(defaultThresholds)
	status, body := send(t, f.handler, http.MethodPut, "/api/customer/alert-settings", admin, changed)
	settings := decode[api.AlertSettings](t, body)
	if status != 200 || settings.Thresholds.ConsumerLag.Seconds != 60 || settings.Thresholds.Throttling.Enabled ||
		settings.Defaults.ConsumerLag.Seconds != 600 || !settings.Defaults.Throttling.Enabled {
		t.Fatalf("changing the thresholds = %d %s", status, body)
	}
	stored := f.alertSettings.settings["acme"]
	if stored == nil || stored.Spec.ConsumerLag.Seconds != 60 || stored.Spec.Throttling.Enabled || stored.Spec.Errors.Percent != 5 {
		t.Errorf("stored %+v", stored)
	}
	if !strings.Contains(f.log.String(), `"msg":"alert thresholds changed","audit":true,"tenant":"acme","user":"user_3"`) {
		t.Errorf("no audit log of the change: %s", f.log)
	}
	if _, ok := f.alertSettings.settings["globex"]; ok {
		t.Error("acme's change made settings for globex")
	}

	for _, invalid := range []string{
		strings.Replace(defaultThresholds, `"seconds":600`, `"seconds":59`, 1),
		strings.Replace(defaultThresholds, `"seconds":900`, `"seconds":86401`, 1),
		strings.Replace(defaultThresholds, `"milliseconds":100`, `"milliseconds":0`, 1),
		strings.Replace(defaultThresholds, `"percent":5`, `"percent":101`, 1),
	} {
		if status, body := send(t, f.handler, http.MethodPut, "/api/customer/alert-settings", admin, invalid); status != 400 || !strings.Contains(body, "threshold must be from") {
			t.Errorf("changing the thresholds to %s = %d %s, want 400", invalid, status, body)
		}
	}
}

func TestAdminsSetReceiversThatNobodyReadsBack(t *testing.T) {
	f := newFixture(t)
	admin := f.signer.token(t, session{user: "user_3", org: "org_acme", role: "admin"})
	const webhook = "https://alerts.example.com/changeport?token=s3cr3t"
	status, body := send(t, f.handler, http.MethodPut, "/api/customer/alert-settings/receivers/webhook", admin, `{"value":" `+webhook+` "}`)
	if status != 200 || !strings.Contains(body, `"receivers":{"webhook":{"hint":"alerts.example.com"}}`) || strings.Contains(body, "s3cr3t") {
		t.Fatalf("setting the webhook = %d %s", status, body)
	}
	stored := f.alertSettings.settings["acme"]
	if stored == nil || stored.Spec.Receivers.Webhook == nil || f.alertSettings.secrets[stored.Spec.Receivers.Webhook.Secret] != webhook {
		t.Fatalf("stored %+v with the Secrets %v, want the webhook's Secret named", stored, f.alertSettings.secrets)
	}
	if status, body, _ := get(t, f.handler, "/api/customer/alert-settings", admin); status != 200 || strings.Contains(body, "s3cr3t") ||
		!strings.Contains(body, `"hint":"alerts.example.com"`) {
		t.Errorf("reading the settings = %d %s, want the hint only", status, body)
	}
	if strings.Contains(f.log.String(), "s3cr3t") {
		t.Errorf("the log shows the secret: %s", f.log)
	}

	// A new webhook is a new Secret.
	first := stored.Spec.Receivers.Webhook.Secret
	if status, body := send(t, f.handler, http.MethodPut, "/api/customer/alert-settings/receivers/webhook", admin, `{"value":"https://hooks.example.org/"}`); status != 200 ||
		f.alertSettings.settings["acme"].Spec.Receivers.Webhook.Secret == first {
		t.Errorf("changing the webhook = %d %s, want a new Secret", status, body)
	}
	for _, c := range []struct {
		path, body string
		status     int
		want       string
	}{
		{"/api/customer/alert-settings/receivers/webhook", `{"value":"http://alerts.example.com/"}`, 400, "must start with https://"},
		{"/api/customer/alert-settings/receivers/slack", `{"value":"https://hooks.slack.com/services/T0"}`, 400, "Enter the whole Slack"},
		{"/api/customer/alert-settings/receivers/email", `{"value":"ops@example.com"}`, 404, "No such receiver"},
	} {
		if status, body := send(t, f.handler, http.MethodPut, c.path, admin, c.body); status != c.status || !strings.Contains(body, c.want) {
			t.Errorf("PUT %s %s = %d %s, want %d with %q", c.path, c.body, status, body, c.status, c.want)
		}
	}

	status, body = send(t, f.handler, http.MethodDelete, "/api/customer/alert-settings/receivers/webhook", admin, "")
	if status != 200 || !strings.Contains(body, `"receivers":{}`) || f.alertSettings.settings["acme"].Spec.Receivers.Webhook != nil {
		t.Errorf("removing the webhook = %d %s", status, body)
	}
	if status, body := send(t, f.handler, http.MethodDelete, "/api/customer/alert-settings/receivers/slack", admin, ""); status != 200 {
		t.Errorf("removing a receiver that is not set = %d %s, want 200", status, body)
	}
	if _, ok := f.alertSettings.settings["globex"]; ok {
		t.Error("acme's receivers made settings for globex")
	}
}

func TestAlertsStreamLiveUntilTheTokenExpires(t *testing.T) {
	f := newFixture(t)
	server := httptest.NewServer(f.handler)
	defer server.Close()
	expires := time.Now().Add(3 * time.Second)
	member := f.signer.token(t, session{user: "user_2", org: "org_acme", role: "member", expires: expires})
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/api/customer/alerts/stream", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+member)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "text/event-stream" || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("the stream answered %s with %v", response.Status, response.Header)
	}
	events := make(chan string)
	go func() {
		defer close(events)
		scanner := bufio.NewScanner(response.Body)
		var event strings.Builder
		for scanner.Scan() {
			if scanner.Text() != "" {
				event.WriteString(scanner.Text() + "\n")
				continue
			}
			events <- event.String()
			event.Reset()
		}
	}()
	next := func() string {
		select {
		case event := <-events:
			return event
		case <-time.After(5 * time.Second):
			t.Fatal("no event within 5 seconds")
			return ""
		}
	}
	if first := next(); !strings.HasPrefix(first, "event: alerts\ndata: {\"alerts\":[{") || !strings.Contains(first, `"consumerGroup":"acme-billing"`) {
		t.Errorf("the first event is %q, want acme's alerts", first)
	}
	// The group caught up.
	f.alerts.set("acme")
	if cleared := next(); cleared != "event: alerts\ndata: {\"alerts\":[]}\n" {
		t.Errorf("after the alert cleared the event is %q", cleared)
	}
	// The stream ends with the token, and the browser opens a new one.
	for event := range events {
		t.Errorf("an event after the alerts stopped changing: %q", event)
	}
	if ended := time.Now(); ended.Before(expires.Add(-time.Second)) || ended.After(expires.Add(2*time.Second)) {
		t.Errorf("the stream ended at %v, want at the token's expiry, %v", ended, expires)
	}
}
