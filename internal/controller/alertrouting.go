package controller

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/yaml"

	"github.com/ReneP92/changeport/api/v1alpha1"
	"github.com/ReneP92/changeport/internal/provisioning"
)

// The objects of Alertmanager's configuration, in its namespace: the base,
// with the operators' routes and receivers, which deploy/base/alerting
// applies, and the configuration Alertmanager runs, which the
// AlertRoutingReconciler writes.
const (
	AlertmanagerNamespace     = "metrics"
	AlertmanagerBaseConfig    = "alertmanager-config"
	AlertmanagerRunningConfig = "alertmanager-generated"
	alertmanagerConfigKey     = "alertmanager.yaml"
)

// receiverSecretGrace is how long a receiver Secret that no AlertSettings
// names is kept: the console writes the Secret before it names it.
const receiverSecretGrace = 10 * time.Minute

// AlertRoutingReconciler writes Alertmanager's configuration: the base, with
// a route and a receiver for each customer that has receivers in its
// AlertSettings (docs/alerts.md). A customer's route matches only alerts
// meant for customers that carry its own tenant, so one customer's alerts
// never reach another's receivers. Alertmanager reloads the configuration
// once it changes. The reconciler also deletes the receiver Secrets that no
// AlertSettings names any longer.
type AlertRoutingReconciler struct {
	Client client.Client
	// ResyncInterval is how often the configuration is written again
	// without a change, such as after a customer's namespace is deleted.
	ResyncInterval time.Duration
	// Now is the time receiver Secrets' ages are measured at; time.Now by
	// default.
	Now func() time.Time
}

// alertRoutingRequest is the reconciler's one request: Alertmanager's
// configuration.
var alertRoutingRequest = ctrl.Request{NamespacedName: types.NamespacedName{Namespace: AlertmanagerNamespace, Name: AlertmanagerRunningConfig}}

// SetupWithManager registers the reconciler with mgr: any customer's alert
// settings and the base configuration lead to Alertmanager's configuration.
func (r *AlertRoutingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	toConfig := handler.EnqueueRequestsFromMapFunc(func(context.Context, client.Object) []ctrl.Request {
		return []ctrl.Request{alertRoutingRequest}
	})
	base := predicate.NewPredicateFuncs(func(o client.Object) bool {
		return o.GetNamespace() == AlertmanagerNamespace && o.GetName() == AlertmanagerBaseConfig
	})
	return ctrl.NewControllerManagedBy(mgr).
		Named("alertrouting").
		Watches(&v1alpha1.AlertSettings{}, toConfig).
		Watches(&corev1.ConfigMap{}, toConfig, builder.WithPredicates(base)).
		Complete(r)
}

// Reconcile implements reconcile.Reconciler.
func (r *AlertRoutingReconciler) Reconcile(ctx context.Context, _ ctrl.Request) (ctrl.Result, error) {
	var base corev1.ConfigMap
	if err := r.Client.Get(ctx, client.ObjectKey{Namespace: AlertmanagerNamespace, Name: AlertmanagerBaseConfig}, &base); err != nil {
		if apierrors.IsNotFound(err) {
			// No alerting on this cluster.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get Alertmanager's base configuration: %w", err)
	}
	var settings v1alpha1.AlertSettingsList
	if err := r.Client.List(ctx, &settings); err != nil {
		return ctrl.Result{}, fmt.Errorf("list the alert settings: %w", err)
	}
	var customers []CustomerReceivers
	for i := range settings.Items {
		s := &settings.Items[i]
		tenant, ok := strings.CutPrefix(s.Namespace, provisioning.NamespacePrefix)
		if !ok || s.Name != v1alpha1.AlertSettingsName || !s.DeletionTimestamp.IsZero() {
			continue
		}
		receivers, err := r.receivers(ctx, s)
		if err != nil {
			return ctrl.Result{}, err
		}
		if receivers.any() {
			receivers.Tenant = tenant
			customers = append(customers, receivers)
		}
		if err := r.deleteUnnamedSecrets(ctx, s); err != nil {
			return ctrl.Result{}, err
		}
	}
	config, err := AlertmanagerConfig([]byte(base.Data[alertmanagerConfigKey]), customers)
	if err != nil {
		return ctrl.Result{}, err
	}
	if err := r.write(ctx, config); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: r.ResyncInterval}, nil
}

// receivers reads the secrets of a customer's receivers. A receiver whose
// Secret is missing or not one the console wrote is left out.
func (r *AlertRoutingReconciler) receivers(ctx context.Context, settings *v1alpha1.AlertSettings) (CustomerReceivers, error) {
	var out CustomerReceivers
	read := func(receiver *v1alpha1.AlertReceiver) (string, error) {
		if receiver == nil {
			return "", nil
		}
		var secret corev1.Secret
		err := r.Client.Get(ctx, client.ObjectKey{Namespace: settings.Namespace, Name: receiver.Secret}, &secret)
		if apierrors.IsNotFound(err) {
			ctrl.LoggerFrom(ctx).Info("a receiver's Secret is missing", "namespace", settings.Namespace, "secret", receiver.Secret)
			return "", nil
		}
		if err != nil {
			return "", fmt.Errorf("get receiver Secret %s/%s: %w", settings.Namespace, receiver.Secret, err)
		}
		if string(secret.Type) != v1alpha1.AlertReceiverSecretType {
			return "", nil
		}
		return strings.TrimSpace(string(secret.Data[v1alpha1.AlertReceiverKey])), nil
	}
	var err error
	if out.Slack, err = read(settings.Spec.Receivers.Slack); err != nil {
		return out, err
	}
	if out.PagerDuty, err = read(settings.Spec.Receivers.PagerDuty); err != nil {
		return out, err
	}
	if out.Webhook, err = read(settings.Spec.Receivers.Webhook); err != nil {
		return out, err
	}
	return out, nil
}

// deleteUnnamedSecrets deletes a customer's receiver Secrets that its
// settings no longer name, such as one a new URL replaced, once they are
// older than the console needs to name a Secret it wrote.
func (r *AlertRoutingReconciler) deleteUnnamedSecrets(ctx context.Context, settings *v1alpha1.AlertSettings) error {
	named := map[string]bool{}
	for _, receiver := range []*v1alpha1.AlertReceiver{settings.Spec.Receivers.Slack, settings.Spec.Receivers.PagerDuty, settings.Spec.Receivers.Webhook} {
		if receiver != nil {
			named[receiver.Secret] = true
		}
	}
	var secrets corev1.SecretList
	if err := r.Client.List(ctx, &secrets, client.InNamespace(settings.Namespace)); err != nil {
		return fmt.Errorf("list the Secrets of %s: %w", settings.Namespace, err)
	}
	for i := range secrets.Items {
		secret := &secrets.Items[i]
		if string(secret.Type) != v1alpha1.AlertReceiverSecretType || named[secret.Name] || r.now().Sub(secret.CreationTimestamp.Time) < receiverSecretGrace {
			continue
		}
		if err := r.Client.Delete(ctx, secret, client.Preconditions{UID: &secret.UID}); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("delete receiver Secret %s/%s: %w", secret.Namespace, secret.Name, err)
		}
	}
	return nil
}

// write writes the configuration Alertmanager runs, unless it has it.
func (r *AlertRoutingReconciler) write(ctx context.Context, config []byte) error {
	var secret corev1.Secret
	key := client.ObjectKey{Namespace: AlertmanagerNamespace, Name: AlertmanagerRunningConfig}
	err := r.Client.Get(ctx, key, &secret)
	switch {
	case apierrors.IsNotFound(err):
		secret = corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: key.Namespace, Name: key.Name, Labels: map[string]string{
				"app.kubernetes.io/managed-by": provisioning.ManagedBy,
			}},
			Data: map[string][]byte{alertmanagerConfigKey: config},
		}
		if err := r.Client.Create(ctx, &secret); err != nil {
			return fmt.Errorf("create Alertmanager's configuration: %w", err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("get Alertmanager's configuration: %w", err)
	case string(secret.Data[alertmanagerConfigKey]) == string(config):
		return nil
	}
	secret.Data = map[string][]byte{alertmanagerConfigKey: config}
	if err := r.Client.Update(ctx, &secret); err != nil {
		return fmt.Errorf("update Alertmanager's configuration: %w", err)
	}
	ctrl.LoggerFrom(ctx).Info("wrote Alertmanager's configuration")
	return nil
}

func (r *AlertRoutingReconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// CustomerReceivers are where one customer's alerts go: the Slack webhook
// URL, the PagerDuty routing key and the webhook URL, each empty if unset.
type CustomerReceivers struct {
	Tenant                    string
	Slack, PagerDuty, Webhook string
}

func (c CustomerReceivers) any() bool { return c.Slack != "" || c.PagerDuty != "" || c.Webhook != "" }

// customerReceiver names a customer's receiver in Alertmanager's
// configuration.
func customerReceiver(tenant string) string { return "customer-" + tenant }

// AlertmanagerConfig returns the base configuration with, first among the
// routes, a route for each customer's alerts and a receiver with the
// customer's Slack, PagerDuty and webhook configurations. Tenants are
// customer IDs, which need no quoting in a matcher; the receivers' secrets
// are YAML values, never part of a template.
func AlertmanagerConfig(base []byte, customers []CustomerReceivers) ([]byte, error) {
	var config map[string]any
	if err := yaml.Unmarshal(base, &config); err != nil {
		return nil, fmt.Errorf("read Alertmanager's base configuration: %w", err)
	}
	route, _ := config["route"].(map[string]any)
	if route == nil {
		return nil, errors.New("Alertmanager's base configuration has no route") //nolint:staticcheck // ST1005: Alertmanager is a name
	}
	receivers, _ := config["receivers"].([]any)
	routes, _ := route["routes"].([]any)
	customers = slices.Clone(customers)
	slices.SortFunc(customers, func(a, b CustomerReceivers) int { return strings.Compare(a.Tenant, b.Tenant) })
	var customerRoutes []any
	for _, c := range customers {
		if err := provisioning.ValidateCustomerID(c.Tenant); err != nil {
			return nil, fmt.Errorf("customer %q: %w", c.Tenant, err)
		}
		customerRoutes = append(customerRoutes, map[string]any{
			"receiver": customerReceiver(c.Tenant),
			"matchers": []any{`audience="customer"`, fmt.Sprintf(`tenant=%q`, c.Tenant)},
			"group_by": []any{"alertname", "tenant", "consumer_group", "topic", "client_id", "source"},
		})
		receiver := map[string]any{"name": customerReceiver(c.Tenant)}
		if c.Slack != "" {
			receiver["slack_configs"] = []any{map[string]any{
				"api_url":       c.Slack,
				"send_resolved": true,
				"title":         `{{ if eq .Status "firing" }}Changeport{{ else }}Resolved{{ end }}: {{ .CommonAnnotations.summary }}`,
				"text":          `{{ .CommonAnnotations.description }}`,
			}}
		}
		if c.PagerDuty != "" {
			receiver["pagerduty_configs"] = []any{map[string]any{
				"routing_key":   c.PagerDuty,
				"send_resolved": true,
				"description":   `{{ .CommonAnnotations.summary }}`,
			}}
		}
		if c.Webhook != "" {
			receiver["webhook_configs"] = []any{map[string]any{"url": c.Webhook, "send_resolved": true}}
		}
		receivers = append(receivers, receiver)
	}
	route["routes"] = append(customerRoutes, routes...)
	config["receivers"] = receivers
	out, err := yaml.Marshal(config)
	if err != nil {
		return nil, err
	}
	return append([]byte("# Written by the provisioner from the ConfigMap "+AlertmanagerBaseConfig+" and the customers' AlertSettings; do not edit.\n"), out...), nil
}
