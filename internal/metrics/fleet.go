package metrics

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

// FleetQuery is a query over every customer's cluster at once, with a series
// per tenant, for the operators' fleet view (docs/console.md). Fleet queries
// are kept apart from Query, whose every expression is scoped to one tenant,
// so that nothing a customer asks can reach them.
type FleetQuery string

// The fleet queries: each is its per-cluster Query's, by tenant, which
// internal/metrics's tests hold them to against VictoriaMetrics.
const (
	FleetUnderReplicated   FleetQuery = "fleet_under_replicated"
	FleetUnderMinISR       FleetQuery = "fleet_under_min_isr"
	FleetOfflinePartitions FleetQuery = "fleet_offline_partitions"
	FleetActiveControllers FleetQuery = "fleet_active_controllers"
	FleetActiveBrokers     FleetQuery = "fleet_active_brokers"
	FleetFencedBrokers     FleetQuery = "fleet_fenced_brokers"
	// FleetVolumeUsed is the share of its volume the fullest broker of each
	// cluster uses, from 0 to 1, where the platform reports volumes: kind's
	// do not.
	FleetVolumeUsed FleetQuery = "fleet_volume_used"
)

// fleetDefinitions are the fleet queries' PromQL. Only the customers'
// clusters have a tenant. A cluster with no broker reporting has none of
// these, rather than counts of 0: its brokers may be down, or merely not
// scraped, which the fleet view does not tell apart until its alerts do.
var fleetDefinitions = map[FleetQuery]string{
	FleetUnderReplicated:   `sum by (tenant) (kafka_server_replicamanager_underreplicatedpartitions{tenant!=""})`,
	FleetUnderMinISR:       `sum by (tenant) (kafka_server_replicamanager_underminisrpartitioncount{tenant!=""})`,
	FleetOfflinePartitions: `max by (tenant) (kafka_controller_kafkacontroller_offlinepartitionscount{tenant!=""})`,
	FleetActiveControllers: `sum by (tenant) (kafka_controller_kafkacontroller_activecontrollercount{tenant!=""})`,
	FleetActiveBrokers:     `sum by (tenant) (kafka_controller_kafkacontroller_activebrokercount{tenant!=""} * on (tenant, node_id) kafka_controller_kafkacontroller_activecontrollercount{tenant!=""})`,
	FleetFencedBrokers:     `sum by (tenant) (kafka_controller_kafkacontroller_fencedbrokercount{tenant!=""} * on (tenant, node_id) kafka_controller_kafkacontroller_activecontrollercount{tenant!=""})`,
	FleetVolumeUsed:        `max by (tenant) (kubelet_volume_stats_used_bytes{tenant!=""} / kubelet_volume_stats_capacity_bytes{tenant!=""})`,
}

// Fleet returns q at one moment, by tenant. A tenant without series is not
// in the map.
func (c *Client) Fleet(ctx context.Context, q FleetQuery, at time.Time) (map[string]float64, error) {
	expression, ok := fleetDefinitions[q]
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrUnknownQuery, q)
	}
	series, err := c.get(ctx, "/api/v1/query", url.Values{"query": {expression}, "time": {unix(at)}})
	if err != nil {
		return nil, err
	}
	values := make(map[string]float64, len(series))
	for _, s := range series {
		if tenant := s.Labels["tenant"]; tenant != "" && len(s.Points) == 1 {
			values[tenant] = s.Points[0].Value
		}
	}
	return values, nil
}
