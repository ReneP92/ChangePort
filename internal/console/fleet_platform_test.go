package console

import (
	"encoding/json"
	"log/slog"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/ReneP92/changeport/api/v1alpha1"
	"github.com/ReneP92/changeport/internal/metrics"
)

// TestFleetAgainstThePlatform judges a customer's cluster as the fleet view
// does, from the platform's own metrics store and Alertmanager on kind and
// the customer as Kubernetes has it, while hack/chaos-test.sh disrupts the
// cluster. CHANGEPORT_FLEET_EXPECT says what it must find, separated by
// semicolons: state=<state>, and reason=<reason> and alert=<name> for each
// reason and alert it must have, such as state=degraded;reason=1 broker
// fenced.
func TestFleetAgainstThePlatform(t *testing.T) {
	metricsURL, alertmanagerURL := os.Getenv("CHANGEPORT_METRICS_URL"), os.Getenv("CHANGEPORT_ALERTMANAGER_URL")
	customerFile, expect := os.Getenv("CHANGEPORT_FLEET_CUSTOMER"), os.Getenv("CHANGEPORT_FLEET_EXPECT")
	if metricsURL == "" || alertmanagerURL == "" || customerFile == "" || expect == "" {
		t.Skip("CHANGEPORT_METRICS_URL, CHANGEPORT_ALERTMANAGER_URL, CHANGEPORT_FLEET_CUSTOMER and CHANGEPORT_FLEET_EXPECT are not set; the chaos test sets them")
	}
	data, err := os.ReadFile(customerFile)
	if err != nil {
		t.Fatal(err)
	}
	var customer v1alpha1.Customer
	if err := json.Unmarshal(data, &customer); err != nil {
		t.Fatal(err)
	}
	client, err := metrics.NewClient(metricsURL)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		customers: &fakeCustomers{customers: []v1alpha1.Customer{customer}},
		metrics:   client,
		alerts:    AlertmanagerAlerts{URL: alertmanagerURL},
		clusters:  newFakeClusters(),
		logger:    slog.New(slog.NewTextHandler(t.Output(), nil)),
	}
	view, err := s.lookAtFleet(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	cluster := view.Clusters[0]
	alerts := make([]string, 0, len(cluster.Alerts))
	for _, alert := range cluster.Alerts {
		alerts = append(alerts, alert.Name)
	}
	for want := range strings.SplitSeq(expect, ";") {
		key, value, _ := strings.Cut(want, "=")
		switch key {
		case "state":
			if string(cluster.State) != value {
				t.Errorf("%s is %s, want %s", customer.Name, cluster.State, value)
			}
		case "reason":
			if !slices.Contains(cluster.Reasons, value) {
				t.Errorf("%s's reasons are %q, want %q among them", customer.Name, cluster.Reasons, value)
			}
		case "alert":
			if !slices.Contains(alerts, value) {
				t.Errorf("%s's alerts are %q, want %s among them", customer.Name, alerts, value)
			}
		default:
			t.Fatalf("%q is no state=, reason= or alert= expectation", want)
		}
	}
	if t.Failed() {
		t.Logf("%s in the fleet view: %+v", customer.Name, cluster)
	}
}
