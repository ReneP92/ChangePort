package console

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/ReneP92/changeport/api/v1alpha1"
	"github.com/ReneP92/changeport/internal/console/api"
	"github.com/ReneP92/changeport/internal/metrics"
)

// Operators see every cluster, the worst first, with its metrics, its
// version and its alerts, and the platform's alerts apart.
func TestOperatorsSeeTheFleet(t *testing.T) {
	f := newFixture(t)
	started := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	f.alerts.fleet = []FleetAlert{
		{Name: "KafkaOfflinePartitions", Severity: "page", Audience: "operator", Tenant: "globex", Scope: "cluster", Summary: "2 partitions of globex are offline", Runbook: "https://runbook#offline", StartsAt: started},
		{Name: "KafkaVolumeNearlyFull", Severity: "warn", Audience: "operator", Tenant: "globex", Scope: "cluster", Summary: "A volume of globex is 85% full", StartsAt: started.Add(-time.Hour)},
		// About acme, but not its cluster's health.
		{Name: "TrialEndingUndecided", Severity: "warn", Audience: "operator", Tenant: "acme", Summary: "The trial of acme ends in under two days without a decision", StartsAt: started},
		{Name: "ConsumerGroupLagHigh", Severity: "warn", Audience: "customer", Tenant: "acme", Summary: "billing is behind", StartsAt: started},
		{Name: "ScrapeTargetDown", Severity: "warn", Audience: "operator", Scope: "platform", Summary: "VictoriaLogs is down", StartsAt: started},
	}
	staff := f.signer.token(t, session{user: "user_6", org: staffOrg, role: "member"})
	status, body, _ := get(t, f.handler, "/api/operator/fleet", staff)
	if status != 200 {
		t.Fatalf("GET /api/operator/fleet = %d %s", status, body)
	}
	var fleet api.Fleet
	if err := json.Unmarshal([]byte(body), &fleet); err != nil {
		t.Fatal(err)
	}
	if fleet.KafkaVersion == "" || len(fleet.PlatformAlerts) != 1 || fleet.PlatformAlerts[0].Name != "ScrapeTargetDown" {
		t.Errorf("the platform = version %q, alerts %+v, want the platform's version and its one alert", fleet.KafkaVersion, fleet.PlatformAlerts)
	}
	if len(fleet.Clusters) != 2 || fleet.Clusters[0].Customer.Id != "globex" || fleet.Clusters[1].Customer.Id != "acme" {
		t.Fatalf("clusters = %+v, want globex, which is down, before acme", fleet.Clusters)
	}
	globex, acme := fleet.Clusters[0], fleet.Clusters[1]
	if globex.State != api.FleetClusterStateDown || !slices.Equal(globex.Reasons, []string{"2 partitions offline", "A volume 85% full", "2 alerts firing"}) {
		t.Errorf("globex = %s %q", globex.State, globex.Reasons)
	}
	// Pages first, and only the operators' alerts by name.
	if len(globex.Alerts) != 2 || globex.Alerts[0].Name != "KafkaOfflinePartitions" || ptr.Deref(globex.Alerts[0].Runbook, "") != "https://runbook#offline" || globex.CustomerAlerts != 0 {
		t.Errorf("globex's alerts = %+v and %d of its own", globex.Alerts, globex.CustomerAlerts)
	}
	if ptr.Deref(globex.VolumeUsed, 0) != 0.85 || ptr.Deref(globex.OfflinePartitions, 0) != 2 {
		t.Errorf("globex's volume %v, offline %v", globex.VolumeUsed, globex.OfflinePartitions)
	}
	// Its trial ending shows, but leaves it healthy.
	if acme.State != api.FleetClusterStateHealthy || len(acme.Reasons) != 0 || acme.Brokers != 1 || ptr.Deref(acme.ActiveBrokers, 0) != 1 ||
		ptr.Deref(acme.KafkaVersion, "") != "4.3.1" || acme.CustomerAlerts != 1 || len(acme.Alerts) != 1 || acme.Alerts[0].Name != "TrialEndingUndecided" {
		t.Errorf("acme = %+v", acme)
	}
	// The view is kept for a moment: a new alert shows on a later look.
	f.alerts.mu.Lock()
	f.alerts.fleet = nil
	f.alerts.mu.Unlock()
	if _, again, _ := get(t, f.handler, "/api/operator/fleet", staff); !strings.Contains(again, "ScrapeTargetDown") {
		t.Errorf("the fleet view was looked at again within %v", fleetTTL)
	}
}

func TestFleetStates(t *testing.T) {
	provisioned := metav1.NewTime(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	customer := func(change func(*v1alpha1.Customer)) *v1alpha1.Customer {
		c := &v1alpha1.Customer{ObjectMeta: metav1.ObjectMeta{Name: "acme", Generation: 1}, Spec: v1alpha1.CustomerSpec{Profile: "production"}}
		c.Status.ProvisionedAt = &provisioned
		c.Status.Conditions = []metav1.Condition{{Type: v1alpha1.ConditionReady, Status: "True", Reason: "Done", Message: "Clients can connect", ObservedGeneration: 1}}
		if change != nil {
			change(c)
		}
		return c
	}
	// healthy is a production cluster with every broker active.
	healthy := func(change func(*api.FleetCluster)) api.FleetCluster {
		cluster := api.FleetCluster{
			Brokers: 3, ActiveControllers: ptr.To(1), ActiveBrokers: ptr.To(3), FencedBrokers: ptr.To(0),
			OfflinePartitions: ptr.To(0), UnderMinIsrPartitions: ptr.To(0), UnderReplicatedPartitions: ptr.To(0),
			Alerts: []api.OperatorAlert{},
		}
		if change != nil {
			change(&cluster)
		}
		return cluster
	}
	notReady := func(c *v1alpha1.Customer) {
		c.Status.Conditions[0].Status, c.Status.Conditions[0].Reason, c.Status.Conditions[0].Message = "False", "KafkaNotReady", "Kafka is not ready"
	}
	// Strimzi rolls a change through the brokers, while clients keep working.
	rolling := func(c *v1alpha1.Customer) {
		c.Status.Conditions[0].Status, c.Status.Conditions[0].Reason = "False", "KafkaNotReady"
		c.Status.Conditions[0].Message = "Kafka customer-acme/acme: operator has not reconciled the latest spec"
	}
	copyErrors := api.OperatorAlert{Name: "TieredStorageCopyErrors", Severity: "warn", Summary: "Brokers of acme fail to copy to tiered storage"}
	offline := api.OperatorAlert{Name: "KafkaOfflinePartitions", Severity: "page", Summary: "2 partitions of acme are offline"}
	notReadyLong := api.OperatorAlert{Name: "CustomerNotReady", Severity: "page", Summary: "The cluster of acme has not been ready for 15 minutes"}
	tests := []struct {
		name     string
		customer *v1alpha1.Customer
		cluster  api.FleetCluster
		health   []api.OperatorAlert
		known    bool
		state    api.FleetClusterState
		reasons  []string
	}{
		{"all well", customer(nil), healthy(nil), nil, true, api.FleetClusterStateHealthy, []string{}},
		{"all well, alerts unknown", customer(nil), healthy(nil), nil, false, api.FleetClusterStateHealthy, []string{"Alertmanager did not answer, so its alerts are not known"}},
		{
			"a broker cut off", customer(nil), healthy(func(c *api.FleetCluster) {
				c.ActiveBrokers, c.FencedBrokers, c.UnderReplicatedPartitions = ptr.To(2), ptr.To(1), ptr.To(12)
			}), nil, true,
			api.FleetClusterStateDegraded,
			[]string{"2 of 3 brokers active", "1 broker fenced", "12 partitions under-replicated"},
		},
		{
			"too few in sync", customer(nil), healthy(func(c *api.FleetCluster) { c.UnderMinIsrPartitions = ptr.To(4) }), nil, true,
			api.FleetClusterStateDown,
			[]string{"4 partitions under min in-sync replicas, refusing acks=all writes"},
		},
		{
			"no controller", customer(nil), healthy(func(c *api.FleetCluster) { c.ActiveControllers, c.ActiveBrokers = ptr.To(0), ptr.To(0) }), nil, true,
			api.FleetClusterStateDown,
			[]string{"No active controller", "No active broker"},
		},
		{
			"a volume nearly full", customer(nil), healthy(func(c *api.FleetCluster) { c.VolumeUsed = ptr.To(0.92) }), nil, true,
			api.FleetClusterStateDegraded,
			[]string{"A volume 92% full"},
		},
		{
			"only an alert sees it", customer(nil), healthy(nil),
			[]api.OperatorAlert{copyErrors},
			true,
			api.FleetClusterStateDegraded,
			[]string{"Brokers of acme fail to copy to tiered storage"},
		},
		{
			"a page beside the metrics' reasons", customer(nil), healthy(func(c *api.FleetCluster) { c.OfflinePartitions = ptr.To(2) }),
			[]api.OperatorAlert{offline, copyErrors},
			true,
			api.FleetClusterStateDown,
			[]string{"2 partitions offline", "2 alerts firing"},
		},
		{
			"a page the metrics do not see", customer(nil), healthy(nil),
			[]api.OperatorAlert{notReadyLong},
			true,
			api.FleetClusterStateDown,
			[]string{"The cluster of acme has not been ready for 15 minutes"},
		},
		{
			"two controllers active at once", customer(nil), healthy(func(c *api.FleetCluster) { c.ActiveControllers, c.ActiveBrokers = ptr.To(2), ptr.To(6) }), nil, true,
			api.FleetClusterStateDegraded,
			[]string{"2 controllers active at once"},
		},
		{
			"no broker reports metrics", customer(nil),
			api.FleetCluster{Brokers: 3, Alerts: []api.OperatorAlert{}},
			nil, true,
			api.FleetClusterStateUnknown,
			[]string{"No broker reports metrics: its brokers are down, or not scraped"},
		},
		{
			"a change rolling through, metrics healthy", customer(rolling), healthy(nil), nil, true,
			api.FleetClusterStateDegraded,
			[]string{"Not ready: Kafka customer-acme/acme: operator has not reconciled the latest spec"},
		},
		{"was ready, is not", customer(notReady), healthy(nil), nil, true, api.FleetClusterStateDegraded, []string{"Not ready: Kafka is not ready"}},
		{
			"stuck starting, paged", customer(func(c *v1alpha1.Customer) { notReady(c); c.Status.ProvisionedAt = nil }),
			api.FleetCluster{},
			[]api.OperatorAlert{notReadyLong},
			true,
			api.FleetClusterStateDown,
			[]string{"The cluster of acme has not been ready for 15 minutes", "Kafka is not ready"},
		},
		{
			"starting", customer(func(c *v1alpha1.Customer) { notReady(c); c.Status.ProvisionedAt = nil }),
			api.FleetCluster{},
			nil, true,
			api.FleetClusterStateStarting,
			[]string{"Kafka is not ready"},
		},
		{
			"deleting", customer(func(c *v1alpha1.Customer) { c.DeletionTimestamp = &provisioned }), healthy(nil), nil, true,
			api.FleetClusterStateDeleting,
			[]string{"Waiting for the provisioning controller to delete the cluster"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cluster := test.cluster
			cluster.Customer = toAPI(test.customer)
			judge(&cluster, test.customer, test.health, test.known)
			if cluster.State != test.state || !slices.Equal(cluster.Reasons, test.reasons) {
				t.Errorf("judged %s %q, want %s %q", cluster.State, cluster.Reasons, test.state, test.reasons)
			}
		})
	}
}

// downMetrics and downAlertmanager answer nothing, as while they restart.
type downMetrics struct{ fakeMetrics }

func (downMetrics) Fleet(context.Context, metrics.FleetQuery, time.Time) (map[string]float64, error) {
	return nil, errors.New("VictoriaMetrics is restarting")
}

type downAlertmanager struct{}

func (downAlertmanager) Active(context.Context, string) ([]api.Alert, error) {
	return nil, errors.New("Alertmanager is restarting")
}

func (downAlertmanager) Fleet(context.Context) ([]FleetAlert, error) {
	return nil, errors.New("Alertmanager is restarting")
}

// versionReads records whose Kafka version was read.
type versionReads struct {
	*fakeClusters
	mu   sync.Mutex
	read []string
}

func (v *versionReads) KafkaVersion(ctx context.Context, id string) (string, error) {
	v.mu.Lock()
	v.read = append(v.read, id)
	v.mu.Unlock()
	return v.fakeClusters.KafkaVersion(ctx, id)
}

// Without the metrics store and Alertmanager, the fleet view still lists
// every cluster, as unknown, saying why; and a cluster being removed has
// no version read.
func TestFleetWithoutItsSources(t *testing.T) {
	customers := newFakeCustomers()
	gone := metav1.NewTime(time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC))
	customers.customers[0].DeletionTimestamp = &gone // globex
	clusters := &versionReads{fakeClusters: newFakeClusters()}
	s := &Server{customers: customers, metrics: downMetrics{}, alerts: downAlertmanager{}, clusters: clusters, logger: slog.New(slog.DiscardHandler)}
	view, err := s.lookAtFleet(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Clusters) != 2 {
		t.Fatalf("clusters = %+v, want both", view.Clusters)
	}
	acme, globex := view.Clusters[0], view.Clusters[1]
	if acme.Customer.Id != "acme" || acme.State != api.FleetClusterStateUnknown ||
		!slices.Equal(acme.Reasons, []string{"No broker reports metrics: its brokers are down, or not scraped"}) {
		t.Errorf("acme = %s %q, want unknown", acme.State, acme.Reasons)
	}
	if globex.State != api.FleetClusterStateDeleting || globex.KafkaVersion != nil {
		t.Errorf("globex = %s, version %v, want being removed, unread", globex.State, globex.KafkaVersion)
	}
	if !slices.Equal(clusters.read, []string{"acme"}) {
		t.Errorf("read the Kafka versions of %q, want acme's alone", clusters.read)
	}
}
