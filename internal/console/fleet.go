package console

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
	"k8s.io/utils/ptr"

	"github.com/ReneP92/changeport/api/v1alpha1"
	"github.com/ReneP92/changeport/internal/console/api"
	"github.com/ReneP92/changeport/internal/metrics"
	"github.com/ReneP92/changeport/internal/provisioning"
)

// The operators' fleet view (docs/console.md): every customer's cluster, its
// state and why, from what the console reads of each customer, asked once
// for the whole fleet.

// fleetTTL is how long the fleet view is kept: the page asks every 30
// seconds, and the metrics behind it are scraped every 30.
const fleetTTL = 15 * time.Second

// fleetTimeout bounds a look at the fleet, well within the console's write
// timeout: a source that has not answered by then, such as a slow API server
// for the Kafka versions, leaves its part out rather than hold the page.
const fleetTimeout = 20 * time.Second

// fleetVersionReads is how many Kafka resources the fleet view reads at once.
const fleetVersionReads = 8

// fullVolume is the share of its volume a broker may use before its cluster
// is degraded, as KafkaVolumeNearlyFull warns.
const fullVolume = 0.8

// fleetCache keeps the last fleet view, and makes requests that miss at once
// share one look.
type fleetCache struct {
	mu      sync.Mutex
	at      time.Time
	view    api.Fleet
	running singleflight.Group
}

// GetFleet implements api.StrictServerInterface, for operators.
func (s *Server) GetFleet(ctx context.Context, _ api.GetFleetRequestObject) (api.GetFleetResponseObject, error) {
	principal, err := mustPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if !s.operator(principal) {
		return api.GetFleet403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(operatorsOnly)}, nil
	}
	s.fleet.mu.Lock()
	view, fresh := s.fleet.view, time.Since(s.fleet.at) < fleetTTL
	s.fleet.mu.Unlock()
	if fresh {
		return api.GetFleet200JSONResponse(view), nil
	}
	looking := s.fleet.running.DoChan("fleet", func() (any, error) {
		// The look outlives the request that started it, up to
		// fleetTimeout, so that the next one finds it done.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), fleetTimeout)
		defer cancel()
		view, err := s.lookAtFleet(ctx)
		if err != nil {
			return nil, err
		}
		s.fleet.mu.Lock()
		s.fleet.at, s.fleet.view = time.Now(), view
		s.fleet.mu.Unlock()
		return view, nil
	})
	select {
	case result := <-looking:
		if result.Err != nil {
			return nil, result.Err
		}
		return api.GetFleet200JSONResponse(result.Val.(api.Fleet)), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// fleetQueries are the metrics the fleet view takes, each into its field.
var fleetQueries = []metrics.FleetQuery{
	metrics.FleetActiveControllers, metrics.FleetActiveBrokers, metrics.FleetFencedBrokers,
	metrics.FleetOfflinePartitions, metrics.FleetUnderMinISR, metrics.FleetUnderReplicated, metrics.FleetVolumeUsed,
}

// lookAtFleet reads every customer, the fleet's metrics, the alerts and each
// running cluster's Kafka version, at once. Only the customers are needed:
// a source that fails leaves its part out, and says why in the log.
func (s *Server) lookAtFleet(ctx context.Context) (api.Fleet, error) {
	customers, err := s.customers.List(ctx)
	if err != nil {
		return api.Fleet{}, err
	}
	now := time.Now()
	sources, ctx := errgroup.WithContext(ctx)
	var mu sync.Mutex
	measured := map[metrics.FleetQuery]map[string]float64{}
	for _, q := range fleetQueries {
		sources.Go(func() error {
			values, err := s.metrics.Fleet(ctx, q, now)
			if err != nil {
				s.logger.WarnContext(ctx, "the fleet view without a metric", "query", q, "error", err)
				return nil
			}
			mu.Lock()
			defer mu.Unlock()
			measured[q] = values
			return nil
		})
	}
	var alerts []FleetAlert
	alertsKnown := false
	sources.Go(func() error {
		found, err := s.alerts.Fleet(ctx)
		if err != nil {
			s.logger.WarnContext(ctx, "the fleet view without alerts", "error", err)
			return nil
		}
		alerts, alertsKnown = found, true
		return nil
	})
	versions := make([]string, len(customers))
	reads := make(chan struct{}, fleetVersionReads)
	for i := range customers {
		if customers[i].DeletionTimestamp != nil {
			continue
		}
		sources.Go(func() error {
			select {
			case reads <- struct{}{}:
				defer func() { <-reads }()
			case <-ctx.Done():
				return nil
			}
			version, err := s.clusters.KafkaVersion(ctx, customers[i].Name)
			if err != nil {
				s.logger.WarnContext(ctx, "the fleet view without a Kafka version", "customer", customers[i].Name, "error", err)
				return nil
			}
			versions[i] = version
			return nil
		})
	}
	_ = sources.Wait() // Every source reports its own failure.

	view := api.Fleet{
		Clusters:       make([]api.FleetCluster, 0, len(customers)),
		PlatformAlerts: []api.OperatorAlert{},
		KafkaVersion:   provisioning.KafkaVersion,
		CheckedAt:      now.UTC(),
	}
	// An operator alert about a customer that no longer exists, such as one
	// from a deleted cluster's last series, shows with the platform's, so
	// that every page is somewhere.
	known := map[string]bool{}
	for i := range customers {
		known[customers[i].Name] = true
	}
	byTenant := map[string][]FleetAlert{}
	for _, alert := range alerts {
		switch {
		case alert.Audience == "operator" && (alert.Scope == "platform" || !known[alert.Tenant]):
			view.PlatformAlerts = append(view.PlatformAlerts, operatorAlert(alert))
		case known[alert.Tenant]:
			byTenant[alert.Tenant] = append(byTenant[alert.Tenant], alert)
		}
	}
	sortOperatorAlerts(view.PlatformAlerts)
	for i := range customers {
		c := &customers[i]
		cluster := api.FleetCluster{Customer: toAPI(c), Alerts: []api.OperatorAlert{}}
		if profile, err := provisioning.ProfileByName(c.Spec.Profile); err == nil {
			cluster.Brokers = profile.Replicas
		}
		count := func(q metrics.FleetQuery) *int {
			if value, ok := measured[q][c.Name]; ok {
				return ptr.To(int(value))
			}
			return nil
		}
		cluster.ActiveControllers = count(metrics.FleetActiveControllers)
		cluster.ActiveBrokers = count(metrics.FleetActiveBrokers)
		cluster.FencedBrokers = count(metrics.FleetFencedBrokers)
		cluster.OfflinePartitions = count(metrics.FleetOfflinePartitions)
		cluster.UnderMinIsrPartitions = count(metrics.FleetUnderMinISR)
		cluster.UnderReplicatedPartitions = count(metrics.FleetUnderReplicated)
		if used, ok := measured[metrics.FleetVolumeUsed][c.Name]; ok {
			cluster.VolumeUsed = ptr.To(used)
		}
		if versions[i] != "" {
			cluster.KafkaVersion = ptr.To(versions[i])
		}
		var health []api.OperatorAlert
		for _, alert := range byTenant[c.Name] {
			switch {
			case alert.Audience == "customer":
				cluster.CustomerAlerts++
			case alert.Scope == "cluster":
				health = append(health, operatorAlert(alert))
				fallthrough
			default:
				cluster.Alerts = append(cluster.Alerts, operatorAlert(alert))
			}
		}
		sortOperatorAlerts(cluster.Alerts)
		sortOperatorAlerts(health)
		judge(&cluster, c, health, alertsKnown)
		view.Clusters = append(view.Clusters, cluster)
	}
	slices.SortStableFunc(view.Clusters, func(a, b api.FleetCluster) int {
		return cmp.Or(
			cmp.Compare(slices.Index(stateOrder, a.State), slices.Index(stateOrder, b.State)),
			cmp.Compare(a.Customer.DisplayName, b.Customer.DisplayName),
			cmp.Compare(a.Customer.Environment, b.Customer.Environment),
			cmp.Compare(a.Customer.Id, b.Customer.Id),
		)
	})
	return view, nil
}

// stateOrder is the order the fleet view lists its clusters in: the ones
// that need an operator first.
var stateOrder = []api.FleetClusterState{
	api.FleetClusterStateDown, api.FleetClusterStateDegraded, api.FleetClusterStateUnknown,
	api.FleetClusterStateStarting, api.FleetClusterStateDeleting, api.FleetClusterStateHealthy,
}

// judge sets the cluster's state and its reasons, the worst first, from its
// provisioning, its metrics and the operators' alerts about its health
// (health; their other alerts about the customer, such as a trial ending or
// a Postgres source failing, show with it but leave its state):
//
//   - down when clients cannot use it or a part of it: no active
//     controller or broker, partitions offline, or fewer in sync than
//     producers with acks=all need, or a page about its health, which the
//     rules keep for what hurts customers now;
//   - degraded when it works with less to spare than it should: not ready,
//     as while Strimzi rolls a change through it, brokers not active or
//     fenced, two controllers both active, partitions under-replicated, a
//     volume nearly full, or a warning about its health;
//   - unknown when it is ready but no broker reports metrics: its brokers
//     are down or not scraped, which its pages tell apart once they fire;
//   - starting until it is first ready, and deleting while it is removed,
//     unless its health alerts say worse.
func judge(cluster *api.FleetCluster, c *v1alpha1.Customer, health []api.OperatorAlert, alertsKnown bool) {
	paged := slices.ContainsFunc(health, func(a api.OperatorAlert) bool { return a.Severity == "page" })
	// The alerts' summaries say why when the metrics do not, such as for
	// tiered storage that fails to copy; otherwise they are counted.
	alerting := func(reasons []string) []string {
		switch {
		case len(health) == 0:
			return reasons
		case len(reasons) == 0:
			for _, alert := range health {
				reasons = append(reasons, alert.Summary)
			}
			return reasons
		}
		return append(reasons, plural(len(health), "alert")+" firing")
	}
	switch {
	case c.DeletionTimestamp != nil:
		cluster.State, cluster.Reasons = api.FleetClusterStateDeleting, []string{cluster.Customer.Status.Message}
		return
	case c.Status.ProvisionedAt == nil && !cluster.Customer.Ready:
		cluster.State, cluster.Reasons = api.FleetClusterStateStarting, slices.Concat(alerting(nil), []string{cluster.Customer.Status.Message})
		switch {
		case paged:
			cluster.State = api.FleetClusterStateDown
		case len(health) > 0:
			cluster.State = api.FleetClusterStateDegraded
		}
		return
	}
	var down, degraded []string
	if !cluster.Customer.Ready {
		degraded = append(degraded, "Not ready: "+cluster.Customer.Status.Message)
	}
	known := func(field *int) (int, bool) {
		if field == nil {
			return 0, false
		}
		return *field, true
	}
	controllers, controllersKnown := known(cluster.ActiveControllers)
	switch {
	case controllersKnown && controllers == 0:
		down = append(down, "No active controller")
	case controllersKnown && controllers > 1:
		degraded = append(degraded, fmt.Sprintf("%d controllers active at once", controllers))
	}
	// Two active controllers each count the brokers, so the count means
	// nothing until one gives way.
	if n, ok := known(cluster.ActiveBrokers); ok && n == 0 {
		down = append(down, "No active broker")
	} else if ok && n < cluster.Brokers && controllers <= 1 {
		degraded = append(degraded, fmt.Sprintf("%d of %d brokers active", n, cluster.Brokers))
	}
	if n, ok := known(cluster.OfflinePartitions); ok && n > 0 {
		down = append(down, plural(n, "partition")+" offline")
	}
	if n, ok := known(cluster.UnderMinIsrPartitions); ok && n > 0 {
		down = append(down, plural(n, "partition")+" under min in-sync replicas, refusing acks=all writes")
	}
	if n, ok := known(cluster.FencedBrokers); ok && n > 0 {
		degraded = append(degraded, plural(n, "broker")+" fenced")
	}
	if n, ok := known(cluster.UnderReplicatedPartitions); ok && n > 0 {
		degraded = append(degraded, plural(n, "partition")+" under-replicated")
	}
	if cluster.VolumeUsed != nil && *cluster.VolumeUsed >= fullVolume {
		degraded = append(degraded, fmt.Sprintf("A volume %.0f%% full", *cluster.VolumeUsed*100))
	}
	measured := cluster.ActiveControllers != nil || cluster.ActiveBrokers != nil || cluster.OfflinePartitions != nil ||
		cluster.UnderMinIsrPartitions != nil || cluster.UnderReplicatedPartitions != nil
	switch {
	case len(down) > 0 || paged:
		cluster.State, cluster.Reasons = api.FleetClusterStateDown, alerting(slices.Concat(down, degraded))
	case !measured:
		cluster.State, cluster.Reasons = api.FleetClusterStateUnknown, alerting(slices.Concat([]string{"No broker reports metrics: its brokers are down, or not scraped"}, degraded))
	case len(degraded) > 0 || len(health) > 0:
		cluster.State, cluster.Reasons = api.FleetClusterStateDegraded, alerting(degraded)
	default:
		cluster.State, cluster.Reasons = api.FleetClusterStateHealthy, []string{}
		if !alertsKnown {
			cluster.Reasons = []string{"Alertmanager did not answer, so its alerts are not known"}
		}
	}
}

func operatorAlert(a FleetAlert) api.OperatorAlert {
	alert := api.OperatorAlert{Name: a.Name, Severity: a.Severity, Summary: a.Summary, StartsAt: a.StartsAt}
	if a.Runbook != "" {
		alert.Runbook = ptr.To(a.Runbook)
	}
	return alert
}

// sortOperatorAlerts puts pages before warnings, then the oldest first.
func sortOperatorAlerts(alerts []api.OperatorAlert) {
	slices.SortStableFunc(alerts, func(a, b api.OperatorAlert) int {
		paged := func(alert api.OperatorAlert) int {
			if alert.Severity == "page" {
				return 0
			}
			return 1
		}
		return cmp.Or(
			cmp.Compare(paged(a), paged(b)),
			a.StartsAt.Compare(b.StartsAt),
			cmp.Compare(a.Name, b.Name),
		)
	})
}
