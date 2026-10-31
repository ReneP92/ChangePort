package console

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/ReneP92/changeport/api/v1alpha1"
	"github.com/ReneP92/changeport/internal/console/api"
)

func TestAlertmanagerAlertsAreTheCustomersOwn(t *testing.T) {
	var query string
	alertmanager := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/alerts" {
			http.NotFound(w, r)
			return
		}
		query = r.URL.RawQuery
		// The second alert is another customer's, which Alertmanager would
		// not return; the client drops it all the same.
		_, _ = w.Write([]byte(`[
		  {"labels":{"alertname":"ConsumerGroupLagHigh","audience":"customer","tenant":"acme","severity":"warn","topic":"orders","consumer_group":"billing"},
		   "annotations":{"summary":"billing is behind","description":"Its oldest message"},"startsAt":"2026-10-31T10:00:00Z"},
		  {"labels":{"alertname":"ConsumerGroupLagHigh","audience":"customer","tenant":"globex","severity":"warn"},
		   "annotations":{"summary":"globex's"},"startsAt":"2026-10-31T11:00:00Z"},
		  {"labels":{"alertname":"ClientThrottled","audience":"customer","tenant":"acme","severity":"warn","client_id":"loader"},
		   "annotations":{"summary":"loader is throttled"},"startsAt":"2026-10-31T10:30:00Z"}
		]`))
	}))
	defer alertmanager.Close()
	alerts, err := AlertmanagerAlerts{URL: alertmanager.URL + "/"}.Active(t.Context(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"active=true", "silenced=false", "inhibited=false", "filter=audience%3D%22customer%22", "filter=tenant%3D%22acme%22"} {
		if !strings.Contains(query, want) {
			t.Errorf("asked Alertmanager for %s, want %s in it", query, want)
		}
	}
	if len(alerts) != 2 || alerts[0].Name != "ClientThrottled" || *alerts[0].ClientId != "loader" || alerts[0].Topic != nil ||
		alerts[1].Summary != "billing is behind" || *alerts[1].Topic != "orders" || *alerts[1].ConsumerGroup != "billing" ||
		!alerts[1].StartsAt.Equal(time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("Active() = %+v, want acme's two alerts, newest first", alerts)
	}

	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer broken.Close()
	if _, err := (AlertmanagerAlerts{URL: broken.URL}).Active(t.Context(), "acme"); err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("Active() from a failing Alertmanager = %v, want its status", err)
	}
}

// resolver resolves every host to its addresses.
type resolver []netip.Addr

func (r resolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) { return r, nil }

func TestReceiversAreCheckedAndHinted(t *testing.T) {
	public := Receivers{Resolver: resolver{netip.MustParseAddr("93.184.215.14")}}
	private := Receivers{Resolver: resolver{netip.MustParseAddr("93.184.215.14"), netip.MustParseAddr("10.0.0.7")}}
	local := Receivers{AllowPrivate: true}
	for _, c := range []struct {
		name      string
		receivers Receivers
		kind      api.ReceiverKind
		value     string
		hint      string // or the start of the error
		fails     bool
	}{
		{"a Slack webhook", public, api.Slack, " https://hooks.slack.com/services/T0123/B0456/s3cr3t ", "Webhook B0456 of workspace T0123", false},
		{"another Slack URL", public, api.Slack, "https://hooks.example.com/services/T0123/B0456/s3cr3t", "enter a Slack incoming webhook URL", true},
		{"half a Slack webhook", public, api.Slack, "https://hooks.slack.com/services/T0123", "enter the whole Slack", true},
		{"Slack over HTTP", public, api.Slack, "http://hooks.slack.com/services/T0123/B0456/s3cr3t", "enter a Slack", true},
		{"a routing key", public, api.PagerDuty, "0123456789abcdefABCDEF0123456789", "Routing key ending in 6789", false},
		{"a short key", public, api.PagerDuty, "R012345", "enter the integration's routing key", true},
		{"a webhook", public, api.Webhook, "https://alerts.example.com:8443/changeport?token=s3cr3t", "alerts.example.com:8443", false},
		{"a webhook over HTTP", public, api.Webhook, "http://alerts.example.com/changeport", "the webhook's URL must start with https://", true},
		{"credentials before the host", public, api.Webhook, "https://user:pass@alerts.example.com/", "put the webhook's credentials", true},
		{"a private webhook", private, api.Webhook, "https://alerts.example.com/", "alerts.example.com is not on the public internet", true},
		{"a metadata service", Receivers{}, api.Webhook, "https://169.254.169.254/latest", "169.254.169.254 is not on the public internet", true},
		{"no URL", public, api.Webhook, "alerts", "enter the webhook's URL", true},
		{"the test webhook on kind", local, api.Webhook, "http://alert-receiver.metrics.svc:8080/customer-acme", "alert-receiver.metrics.svc:8080", false},
		{"no such receiver", public, "email", "a@example.com", "there is no receiver email", true},
	} {
		hint, err := c.receivers.Check(t.Context(), c.kind, c.value)
		switch {
		case c.fails && (err == nil || !strings.HasPrefix(err.Error(), c.hint)):
			t.Errorf("%s: Check() = %q, %v, want an error starting %q", c.name, hint, err, c.hint)
		case !c.fails && (err != nil || hint != c.hint):
			t.Errorf("%s: Check() = %q, %v, want %q", c.name, hint, err, c.hint)
		case strings.Contains(hint, "s3cr3t"):
			t.Errorf("%s: the hint %q shows the secret", c.name, hint)
		}
	}
}

// TestKubeAlertSettingsAsTheConsole runs KubeAlertSettings as the console's
// service account, which the admission policy allows only write-once
// receiver Secrets.
func TestKubeAlertSettingsAsTheConsole(t *testing.T) {
	admin, console := startAPIServer(t)
	ctx := t.Context()
	if err := admin.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "customer-acme"}}); err != nil {
		t.Fatal(err)
	}
	store := KubeAlertSettings{Client: console}
	if settings, err := store.Get(ctx, "acme"); settings != nil || err != nil {
		t.Fatalf("Get() without settings = %+v, %v, want none", settings, err)
	}
	settings := &v1alpha1.AlertSettings{Spec: v1alpha1.DefaultAlertSettings}
	if err := store.Save(ctx, "acme", settings); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, "acme", &v1alpha1.AlertSettings{Spec: v1alpha1.DefaultAlertSettings}); !errors.Is(err, ErrAlertSettingsChanged) {
		t.Errorf("creating the settings again = %v, want ErrAlertSettingsChanged", err)
	}
	name, err := store.WriteReceiver(ctx, settings, api.PagerDuty, "0123456789abcdefABCDEF0123456789")
	if err != nil {
		t.Fatal(err)
	}
	var secret corev1.Secret
	if err := admin.Get(ctx, client.ObjectKey{Namespace: "customer-acme", Name: name}, &secret); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(name, "alert-receiver-pagerduty-") || secret.Type != v1alpha1.AlertReceiverSecretType ||
		string(secret.Data[v1alpha1.AlertReceiverKey]) != "0123456789abcdefABCDEF0123456789" ||
		len(secret.OwnerReferences) != 1 || secret.OwnerReferences[0].UID != settings.UID {
		t.Errorf("the receiver's Secret %s is %+v, want the key, owned by the settings", name, secret)
	}
	settings.Spec.Receivers.PagerDuty = &v1alpha1.AlertReceiver{Secret: name, Hint: "Routing key ending in 6789"}
	settings.Spec.ConsumerLag.Seconds = 60
	if err := store.Save(ctx, "acme", settings); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ctx, "acme")
	if err != nil || got.Spec.ConsumerLag.Seconds != 60 || got.Spec.Receivers.PagerDuty.Secret != name {
		t.Fatalf("Get() = %+v, %v", got, err)
	}
	// Settings read before the change are stale.
	stale := settings.DeepCopy()
	stale.ResourceVersion = "1"
	if err := store.Save(ctx, "acme", stale); !errors.Is(err, ErrAlertSettingsChanged) {
		t.Errorf("saving stale settings = %v, want ErrAlertSettingsChanged", err)
	}

	if err := console.Get(ctx, client.ObjectKey{Namespace: "customer-acme", Name: name}, &corev1.Secret{}); !apierrors.IsForbidden(err) {
		t.Errorf("the console reading a receiver's Secret = %v, want forbidden", err)
	}
	for what, secret := range map[string]*corev1.Secret{
		"a mutable Secret": {
			ObjectMeta: metav1.ObjectMeta{Namespace: "customer-acme", Name: receiverSecretPrefix + "x"},
			Type:       v1alpha1.AlertReceiverSecretType, Data: map[string][]byte{"value": []byte("x")},
		},
		"another key": {
			ObjectMeta: metav1.ObjectMeta{Namespace: "customer-acme", Name: receiverSecretPrefix + "x"},
			Type:       v1alpha1.AlertReceiverSecretType, Immutable: ptr.To(true), Data: map[string][]byte{"password": []byte("x")},
		},
		"another name": {
			ObjectMeta: metav1.ObjectMeta{Namespace: "customer-acme", Name: passwordSecretPrefix + "x"},
			Type:       v1alpha1.AlertReceiverSecretType, Immutable: ptr.To(true), Data: map[string][]byte{"value": []byte("x")},
		},
		"another namespace": {
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: receiverSecretPrefix + "x"},
			Type:       v1alpha1.AlertReceiverSecretType, Immutable: ptr.To(true), Data: map[string][]byte{"value": []byte("x")},
		},
	} {
		if err := refusedByPolicy(t, console, secret); err != nil {
			t.Errorf("the console creating %s = %v, want the admission policy's refusal", what, err)
		}
	}
}
