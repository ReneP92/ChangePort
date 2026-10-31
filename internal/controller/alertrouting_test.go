package controller

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"

	"github.com/ReneP92/changeport/api/v1alpha1"
)

func baseAlertmanagerConfig(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "deploy", "base", "alerting", "alertmanager.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type amConfig struct {
	Route struct {
		Receiver string `json:"receiver"`
		Routes   []struct {
			Receiver string   `json:"receiver"`
			Matchers []string `json:"matchers"`
		} `json:"routes"`
	} `json:"route"`
	Receivers []map[string]any `json:"receivers"`
}

// Each customer's alerts go only to its own receivers, before the operators'
// routes, which stay as they are.
func TestAlertmanagerConfigRoutesEachCustomerToItsOwnReceivers(t *testing.T) {
	base := baseAlertmanagerConfig(t)
	out, err := AlertmanagerConfig(base, []CustomerReceivers{
		{Tenant: "globex", Webhook: "https://hooks.globex.example/alerts"},
		{Tenant: "acme", Slack: "https://hooks.slack.com/services/T/B/acme", PagerDuty: "acme-routing-key"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var config amConfig
	if err := yaml.Unmarshal(out, &config); err != nil {
		t.Fatal(err)
	}
	var baseConfig amConfig
	if err := yaml.Unmarshal(base, &baseConfig); err != nil {
		t.Fatal(err)
	}
	routes := config.Route.Routes
	if len(routes) != len(baseConfig.Route.Routes)+2 {
		t.Fatalf("%d routes, want the base's %d and one per customer", len(routes), len(baseConfig.Route.Routes))
	}
	for i, want := range []struct{ receiver, tenant string }{{"customer-acme", "acme"}, {"customer-globex", "globex"}} {
		got := routes[i]
		if got.Receiver != want.receiver || strings.Join(got.Matchers, ",") != `audience="customer",tenant="`+want.tenant+`"` {
			t.Errorf("route %d = %+v, want %s matching only tenant %s's alerts for customers", i, got, want.receiver, want.tenant)
		}
	}
	for i, route := range baseConfig.Route.Routes {
		if got := routes[i+2]; got.Receiver != route.Receiver || strings.Join(got.Matchers, ",") != strings.Join(route.Matchers, ",") {
			t.Errorf("base route %d = %+v, want %+v", i, got, route)
		}
	}
	// Each customer's secrets are in its own receiver only.
	receivers := map[string]string{}
	for _, r := range config.Receivers {
		data, err := yaml.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		receivers[r["name"].(string)] = string(data)
	}
	acme, globex := receivers["customer-acme"], receivers["customer-globex"]
	if !strings.Contains(acme, "hooks.slack.com/services/T/B/acme") || !strings.Contains(acme, "acme-routing-key") || strings.Contains(acme, "globex") {
		t.Errorf("acme's receiver:\n%s", acme)
	}
	if !strings.Contains(globex, "https://hooks.globex.example/alerts") || strings.Contains(globex, "acme") || strings.Contains(globex, "slack") {
		t.Errorf("globex's receiver:\n%s", globex)
	}
}

func TestAlertmanagerConfigRefusesAnInvalidTenant(t *testing.T) {
	if _, err := AlertmanagerConfig(baseAlertmanagerConfig(t), []CustomerReceivers{{Tenant: `acme",tenant=~".*`, Webhook: "https://x.example"}}); err == nil {
		t.Error("a tenant that is no customer ID was accepted")
	}
}

// amtool, of the Alertmanager image deploy/base/alerting runs, accepts the
// configuration with customers; it runs where Docker does (just test).
func TestAlertmanagerConfigPassesAmtool(t *testing.T) {
	if os.Getenv("CHANGEPORT_DOCKER_TESTS") == "" {
		t.Skip("CHANGEPORT_DOCKER_TESTS is not set; just test sets it")
	}
	out, err := AlertmanagerConfig(baseAlertmanagerConfig(t), []CustomerReceivers{
		{Tenant: "acme", Slack: "https://hooks.slack.com/services/T/B/C", PagerDuty: "0123456789abcdef0123456789abcdef", Webhook: "https://hooks.acme.example/alerts"},
	})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join("..", "..", "deploy", "base", "alerting", "alerting.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	image := ""
	for line := range strings.SplitSeq(string(manifest), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "image: "); ok && strings.Contains(value, "alertmanager") {
			image = value
			break
		}
	}
	// On stdin, which the container's user reads whoever owns the test's files.
	check := exec.CommandContext(t.Context(), "docker", "run", "--rm", "-i", "--entrypoint", "amtool", image, "check-config", "/dev/stdin")
	check.Stdin = bytes.NewReader(out)
	output, err := check.CombinedOutput()
	if err != nil {
		t.Fatalf("amtool refuses the configuration: %v\n%s\n%s", err, output, out)
	}
}

func newAlertRoutingReconciler(t *testing.T, objects ...client.Object) (*AlertRoutingReconciler, client.Client) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	base := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: AlertmanagerNamespace, Name: AlertmanagerBaseConfig},
		Data:       map[string]string{alertmanagerConfigKey: string(baseAlertmanagerConfig(t))},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(append(objects, base)...).Build()
	return &AlertRoutingReconciler{Client: c, ResyncInterval: time.Minute, Now: func() time.Time { return testNow }}, c
}

func receiverSecret(name, value string, created time.Time) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "customer-acme", Name: name, CreationTimestamp: metav1.NewTime(created)},
		Type:       v1alpha1.AlertReceiverSecretType,
		Data:       map[string][]byte{v1alpha1.AlertReceiverKey: []byte(value)},
	}
}

func TestAlertRoutingWritesTheConfigurationAndForgetsOldReceivers(t *testing.T) {
	settings := &v1alpha1.AlertSettings{
		ObjectMeta: metav1.ObjectMeta{Namespace: "customer-acme", Name: v1alpha1.AlertSettingsName},
		Spec:       v1alpha1.DefaultAlertSettings,
	}
	settings.Spec.Receivers.Webhook = &v1alpha1.AlertReceiver{Secret: "alert-receiver-webhook-2"}
	r, c := newAlertRoutingReconciler(t, settings,
		receiverSecret("alert-receiver-webhook-2", "https://hooks.acme.example/new", testNow.Add(-time.Hour)),
		// Replaced an hour ago, and just written, not named yet.
		receiverSecret("alert-receiver-webhook-1", "https://hooks.acme.example/old", testNow.Add(-time.Hour)),
		receiverSecret("alert-receiver-slack-3", "https://hooks.slack.com/services/T/B/C", testNow.Add(-time.Minute)),
	)
	result, err := r.Reconcile(t.Context(), alertRoutingRequest)
	if err != nil {
		t.Fatal(err)
	}
	if result.RequeueAfter != time.Minute {
		t.Errorf("requeue after %v, want the resync interval", result.RequeueAfter)
	}
	var written corev1.Secret
	if err := c.Get(t.Context(), client.ObjectKey{Namespace: AlertmanagerNamespace, Name: AlertmanagerRunningConfig}, &written); err != nil {
		t.Fatal(err)
	}
	config := string(written.Data[alertmanagerConfigKey])
	if !strings.Contains(config, "https://hooks.acme.example/new") || strings.Contains(config, "/old") || strings.Contains(config, "hooks.slack.com/services/T/B/C") {
		t.Errorf("configuration:\n%s", config)
	}
	err = c.Get(t.Context(), client.ObjectKey{Namespace: "customer-acme", Name: "alert-receiver-webhook-1"}, &corev1.Secret{})
	if !apierrors.IsNotFound(err) {
		t.Errorf("the replaced receiver Secret is still there: %v", err)
	}
	if err := c.Get(t.Context(), client.ObjectKey{Namespace: "customer-acme", Name: "alert-receiver-slack-3"}, &corev1.Secret{}); err != nil {
		t.Errorf("a receiver Secret just written was deleted: %v", err)
	}

	// Without receivers, the customer has no route.
	settings.Spec.Receivers.Webhook = nil
	if err := c.Update(t.Context(), settings); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(t.Context(), alertRoutingRequest); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(t.Context(), client.ObjectKey{Namespace: AlertmanagerNamespace, Name: AlertmanagerRunningConfig}, &written); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(written.Data[alertmanagerConfigKey]), "customer-acme") {
		t.Errorf("a customer without receivers has a route:\n%s", written.Data[alertmanagerConfigKey])
	}
}
