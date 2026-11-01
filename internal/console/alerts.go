package console

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/ReneP92/changeport/api/v1alpha1"
	"github.com/ReneP92/changeport/internal/cdc"
	"github.com/ReneP92/changeport/internal/console/api"
)

// Alerts reads the active alerts about customers' own applications
// (docs/alerts.md).
type Alerts interface {
	// Active returns a customer's active alerts, newest first.
	Active(ctx context.Context, customerID string) ([]api.Alert, error)
}

// AlertmanagerAlerts reads the alerts from Alertmanager's API.
type AlertmanagerAlerts struct {
	// URL is Alertmanager's, such as http://alertmanager.metrics.svc:9093.
	URL  string
	HTTP *http.Client
}

var _ Alerts = AlertmanagerAlerts{}

// gettableAlert is an alert as Alertmanager's API returns it.
type gettableAlert struct {
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	StartsAt    time.Time         `json:"startsAt"`
}

// Active implements Alerts. It asks only for the customer's own alerts that
// are not silenced or inhibited: those for customers, of the customer's
// tenant.
func (a AlertmanagerAlerts) Active(ctx context.Context, customerID string) ([]api.Alert, error) {
	query := url.Values{
		"active": {"true"}, "silenced": {"false"}, "inhibited": {"false"},
		"filter": {`audience="customer"`, fmt.Sprintf("tenant=%q", customerID)},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(a.URL, "/")+"/api/v2/alerts?"+query.Encode(), http.NoBody)
	if err != nil {
		return nil, err
	}
	response, err := cmp.Or(a.HTTP, http.DefaultClient).Do(request)
	if err != nil {
		return nil, fmt.Errorf("ask Alertmanager for the alerts of customer %s: %w", customerID, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return nil, fmt.Errorf("ask Alertmanager for the alerts of customer %s: %s: %s", customerID, response.Status, body)
	}
	var alerts []gettableAlert
	if err := json.NewDecoder(response.Body).Decode(&alerts); err != nil {
		return nil, fmt.Errorf("read the alerts of customer %s: %w", customerID, err)
	}
	out := make([]api.Alert, 0, len(alerts))
	for _, alert := range alerts {
		if alert.Labels["tenant"] != customerID || alert.Labels["audience"] != "customer" {
			// Alertmanager filtered them already; this keeps a mistake in
			// that from showing a customer another's alerts.
			continue
		}
		out = append(out, toAPIAlert(alert))
	}
	sortAlerts(out)
	return out, nil
}

func toAPIAlert(a gettableAlert) api.Alert {
	optional := func(label string) *string {
		if value := a.Labels[label]; value != "" {
			return ptr.To(value)
		}
		return nil
	}
	return api.Alert{
		Name:          a.Labels["alertname"],
		Severity:      a.Labels["severity"],
		Summary:       a.Annotations["summary"],
		Description:   a.Annotations["description"],
		StartsAt:      a.StartsAt.UTC(),
		Topic:         optional("topic"),
		ConsumerGroup: optional("consumer_group"),
		ClientId:      optional("client_id"),
		Source:        optional("source"),
	}
}

// sortAlerts puts the newest alerts first, and the others in a stable order.
func sortAlerts(alerts []api.Alert) {
	slices.SortFunc(alerts, func(a, b api.Alert) int {
		return cmp.Or(b.StartsAt.Compare(a.StartsAt), strings.Compare(a.Name, b.Name), strings.Compare(a.Summary, b.Summary))
	})
}

// AlertSettingsStore reads and writes customers' AlertSettings.
type AlertSettingsStore interface {
	// Get returns a customer's settings, or nil when it has none and the
	// defaults apply.
	Get(ctx context.Context, customerID string) (*v1alpha1.AlertSettings, error)
	// Save creates settings that are new, and updates the others, refusing
	// settings changed meanwhile with ErrAlertSettingsChanged.
	Save(ctx context.Context, customerID string, settings *v1alpha1.AlertSettings) error
	// WriteReceiver writes a receiver's secret into a new write-once Secret,
	// which the saved settings own, and returns its name.
	WriteReceiver(ctx context.Context, settings *v1alpha1.AlertSettings, kind api.ReceiverKind, value string) (string, error)
}

// ErrAlertSettingsChanged means the settings changed since they were read.
var ErrAlertSettingsChanged = errors.New("the alert settings changed meanwhile")

// receiverSecretPrefix starts the names of the receivers' Secrets; the
// admission policy in deploy/base/console allows the console no others of
// their type.
const receiverSecretPrefix = "alert-receiver-"

// KubeAlertSettings keeps settings as AlertSettings resources, through the
// Kubernetes API as the console's service account.
type KubeAlertSettings struct {
	Client client.Client
}

var _ AlertSettingsStore = KubeAlertSettings{}

// Get implements AlertSettingsStore.
func (k KubeAlertSettings) Get(ctx context.Context, customerID string) (*v1alpha1.AlertSettings, error) {
	var settings v1alpha1.AlertSettings
	err := k.Client.Get(ctx, client.ObjectKey{Namespace: namespaceOf(customerID), Name: v1alpha1.AlertSettingsName}, &settings)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get the alert settings of customer %s: %w", customerID, err)
	}
	return &settings, nil
}

// Save implements AlertSettingsStore.
func (k KubeAlertSettings) Save(ctx context.Context, customerID string, settings *v1alpha1.AlertSettings) error {
	var err error
	if settings.ResourceVersion == "" {
		settings.Namespace, settings.Name = namespaceOf(customerID), v1alpha1.AlertSettingsName
		err = k.Client.Create(ctx, settings)
	} else {
		err = k.Client.Update(ctx, settings)
	}
	if apierrors.IsAlreadyExists(err) || apierrors.IsConflict(err) {
		return fmt.Errorf("customer %s: %w", customerID, ErrAlertSettingsChanged)
	}
	if err != nil {
		return fmt.Errorf("save the alert settings of customer %s: %w", customerID, err)
	}
	return nil
}

// WriteReceiver implements AlertSettingsStore. Should the settings not name
// the Secret in the end, the provisioner deletes it.
func (k KubeAlertSettings) WriteReceiver(ctx context.Context, settings *v1alpha1.AlertSettings, kind api.ReceiverKind, value string) (string, error) {
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	name := fmt.Sprintf("%s%s-%x", receiverSecretPrefix, strings.ToLower(string(kind)), suffix)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: settings.Namespace, Name: name,
			Labels: map[string]string{"changeport.io/alert-receiver": strings.ToLower(string(kind))},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: v1alpha1.GroupVersion.String(), Kind: "AlertSettings", Name: settings.Name, UID: settings.UID,
			}},
		},
		Type:      v1alpha1.AlertReceiverSecretType,
		Immutable: ptr.To(true),
		Data:      map[string][]byte{v1alpha1.AlertReceiverKey: []byte(value)},
	}
	if err := k.Client.Create(ctx, secret); err != nil {
		return "", fmt.Errorf("write the %s receiver of %s: %w", kind, settings.Namespace, err)
	}
	return name, nil
}

// Receivers checks receivers' secrets before the console writes them.
type Receivers struct {
	// AllowPrivate lets webhooks be on private networks and plain HTTP, for
	// local development, where the test webhook is in the cluster.
	AllowPrivate bool
	// Resolver looks webhooks' hosts up; the default is net.DefaultResolver.
	Resolver interface {
		LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
	}
}

var pagerDutyKey = regexp.MustCompile(`^[A-Za-z0-9]{32}$`)

// Check returns a receiver's hint, which shows which receiver it is without
// its secret, or why the value is not one.
func (r Receivers) Check(ctx context.Context, kind api.ReceiverKind, value string) (string, error) {
	value = strings.TrimSpace(value)
	switch kind {
	case api.Slack:
		u, err := url.Parse(value)
		if err != nil || u.Scheme != "https" || u.Host != "hooks.slack.com" || !strings.HasPrefix(u.Path, "/services/") || u.User != nil {
			return "", errors.New("enter a Slack incoming webhook URL, which starts with https://hooks.slack.com/services/")
		}
		// The path is the workspace, the webhook and its secret.
		parts := strings.Split(strings.TrimPrefix(u.Path, "/services/"), "/")
		if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
			return "", errors.New("enter the whole Slack incoming webhook URL, with its workspace, webhook and secret")
		}
		return "Webhook " + parts[1] + " of workspace " + parts[0], nil
	case api.PagerDuty:
		if !pagerDutyKey.MatchString(value) {
			return "", errors.New("enter the integration's routing key, the 32 letters and digits of its integration key")
		}
		return "Routing key ending in " + value[len(value)-4:], nil
	case api.Webhook:
		return r.checkWebhook(ctx, value)
	}
	return "", fmt.Errorf("there is no receiver %s", kind)
}

func (r Receivers) checkWebhook(ctx context.Context, value string) (string, error) {
	u, err := url.Parse(value)
	switch {
	case err != nil || u.Host == "" || u.Opaque != "":
		return "", errors.New("enter the webhook's URL, such as https://alerts.example.com/changeport")
	case u.Scheme != "https" && (!r.AllowPrivate || u.Scheme != "http"):
		return "", errors.New("the webhook's URL must start with https://")
	case u.User != nil:
		return "", errors.New("put the webhook's credentials in its path or query, not before its host")
	}
	host := u.Hostname()
	if !r.AllowPrivate {
		var resolver interface {
			LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
		} = net.DefaultResolver
		if r.Resolver != nil {
			resolver = r.Resolver
		}
		addrs, err := resolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(addrs) == 0 {
			return "", fmt.Errorf("%s does not resolve to an address", host)
		}
		for _, addr := range addrs {
			if !cdc.Public(addr) {
				return "", fmt.Errorf("%s is not on the public internet", host)
			}
		}
	}
	return u.Host, nil
}
