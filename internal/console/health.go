package console

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/ReneP92/changeport/api/v1alpha1"
	"github.com/ReneP92/changeport/internal/console/api"
	"github.com/ReneP92/changeport/internal/diagnosis"
	"github.com/ReneP92/changeport/internal/metrics"
)

// levels are the health of a customer's topics and consumer groups, from
// its diagnoses (docs/diagnosis.md), for the lists that show it.
type levels struct {
	known          bool
	topics, groups map[string]diagnosis.Health
}

// healthLevels returns the customer's levels. They are a help to the lists
// rather than a need: without the diagnoses, the lists show no health
// rather than fail.
func (s *Server) healthLevels(ctx context.Context, customerID string) levels {
	findings, err := s.diagnose(ctx, customerID)
	if err != nil {
		s.logger.WarnContext(ctx, "lists without health", "customer", customerID, "error", err)
		return levels{}
	}
	topics, groups := diagnosis.Levels(findings)
	return levels{known: true, topics: topics, groups: groups}
}

func (l levels) topic(name string) *api.HealthStatus { return l.of(l.topics, name) }
func (l levels) group(name string) *api.HealthStatus { return l.of(l.groups, name) }

func (l levels) of(subjects map[string]diagnosis.Health, name string) *api.HealthStatus {
	if !l.known {
		return nil
	}
	h := subjects[name]
	return &api.HealthStatus{Level: apiLevel(h.Level), Diagnoses: append([]string{}, h.Findings...)}
}

// apiLevel is a level as the API names it.
func apiLevel(level diagnosis.Level) api.HealthLevel {
	switch level {
	case diagnosis.AtRisk:
		return api.AtRisk
	case diagnosis.NeedsAttention:
		return api.NeedsAttention
	default:
		return api.Healthy
	}
}

// healthStep is the step of a health history: 96 steps make a day.
const healthStep = 15 * time.Minute

// GetHealthHistory implements api.StrictServerInterface: a topic's or a
// group's worst level in each step of the last 24 hours, as the health
// monitor recorded it.
func (s *Server) GetHealthHistory(ctx context.Context, request api.GetHealthHistoryRequestObject) (api.GetHealthHistoryResponseObject, error) {
	topic, group := ptr.Deref(request.Params.Topic, ""), ptr.Deref(request.Params.ConsumerGroup, "")
	if (topic == "") == (group == "") {
		return api.GetHealthHistory400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse(problem(http.StatusBadRequest, "Invalid health history", "Name a topic or a consumer group."))}, nil
	}
	customer, _, refused, err := s.customerOf(ctx)
	switch {
	case err != nil:
		return nil, err
	case refused != nil && refused.Status == http.StatusNotFound:
		return api.GetHealthHistory404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(*refused)}, nil
	case refused != nil:
		return api.GetHealthHistory403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(*refused)}, nil
	}
	end := time.Now().Truncate(healthStep).Add(healthStep)
	start := end.Add(-24 * time.Hour)
	q, filter := metrics.TopicHealth, metrics.Filter{Topic: topic, Window: healthStep}
	if group != "" {
		q, filter = metrics.GroupHealth, metrics.Filter{ConsumerGroup: group, Window: healthStep}
	}
	levels, err := s.metrics.Range(ctx, customer.Name, q, filter, start, end, healthStep)
	if err != nil {
		return nil, err
	}
	checked, err := s.metrics.Range(ctx, customer.Name, metrics.HealthChecked, metrics.Filter{Window: healthStep}, start, end, healthStep)
	if err != nil {
		return nil, err
	}
	return api.GetHealthHistory200JSONResponse(healthHistory(start, end, levels, checked)), nil
}

// healthHistory puts each step's level together: the worst recorded, or
// healthy where the monitor looked and found nothing, or unknown where it
// did not look.
func healthHistory(start, end time.Time, levels, checked []metrics.Series) api.HealthHistory {
	at := func(series []metrics.Series) map[int64]float64 {
		values := map[int64]float64{}
		for _, one := range series {
			for _, p := range one.Points {
				values[p.Time.Unix()] = max(values[p.Time.Unix()], p.Value)
			}
		}
		return values
	}
	worst, looked := at(levels), at(checked)
	history := api.HealthHistory{StepSeconds: int(healthStep.Seconds()), Points: []api.HealthPoint{}}
	for t := start.Add(healthStep); !t.After(end); t = t.Add(healthStep) {
		point := api.HealthPoint{Time: t}
		if _, ok := looked[t.Unix()]; ok {
			point.Level = ptr.To(apiLevel(diagnosis.Level(worst[t.Unix()])))
		}
		history.Points = append(history.Points, point)
	}
	return history
}

// healthMonitor exports the level of every topic and consumer group of
// every ready customer, for the health histories: changeport_health_level
// for those not healthy, and changeport_health_checked for each customer
// it looked at, so that a history tells healthy from not looked at.
type healthMonitor struct {
	mu     sync.Mutex
	latest map[string]levels
}

var (
	healthLevelDesc = prometheus.NewDesc("changeport_health_level",
		"How healthy a topic or consumer group is, from its diagnoses: 1 needs attention, 2 at risk. Healthy ones have no series.",
		[]string{"tenant", "topic", "consumer_group"}, nil)
	healthCheckedDesc = prometheus.NewDesc("changeport_health_checked",
		"1 for each customer whose cluster the console diagnosed in its last look.",
		[]string{"tenant"}, nil)
)

// Describe implements prometheus.Collector.
func (m *healthMonitor) Describe(ch chan<- *prometheus.Desc) {
	ch <- healthLevelDesc
	ch <- healthCheckedDesc
}

// Collect implements prometheus.Collector with the last look.
func (m *healthMonitor) Collect(ch chan<- prometheus.Metric) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for tenant, l := range m.latest {
		ch <- prometheus.MustNewConstMetric(healthCheckedDesc, prometheus.GaugeValue, 1, tenant)
		for topic, h := range l.topics {
			ch <- prometheus.MustNewConstMetric(healthLevelDesc, prometheus.GaugeValue, float64(h.Level), tenant, topic, "")
		}
		for group, h := range l.groups {
			ch <- prometheus.MustNewConstMetric(healthLevelDesc, prometheus.GaugeValue, float64(h.Level), tenant, "", group)
		}
	}
}

// HealthCollector returns the collector of the levels WatchHealth records,
// for the console's metrics endpoint.
func (s *Server) HealthCollector() prometheus.Collector { return s.health }

// WatchHealth diagnoses every ready customer's cluster every interval
// until ctx ends, so that the health histories have no gaps while nobody
// looks, and the next look at a page finds the diagnoses ready.
func (s *Server) WatchHealth(ctx context.Context, interval time.Duration) {
	if len(s.config.missing()) > 0 {
		// Without its configuration the console serves nothing to look at.
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		s.lookAtHealth(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// lookAtHealth diagnoses each ready customer once. A customer whose cluster
// cannot be diagnosed is left out of the look, so its history shows a gap
// rather than health it did not see.
func (s *Server) lookAtHealth(ctx context.Context) {
	customers, err := s.customers.List(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "health: list the customers", "error", err)
		return
	}
	latest := map[string]levels{}
	for _, customer := range customers {
		ready := meta.FindStatusCondition(customer.Status.Conditions, v1alpha1.ConditionReady)
		if !customer.DeletionTimestamp.IsZero() || ready == nil || ready.Status != metav1.ConditionTrue {
			continue
		}
		findings, err := s.diagnose(ctx, customer.Name)
		if err != nil {
			s.logger.WarnContext(ctx, "health: diagnose", "customer", customer.Name, "error", err)
			continue
		}
		topics, groups := diagnosis.Levels(findings)
		latest[customer.Name] = levels{known: true, topics: topics, groups: groups}
	}
	s.health.mu.Lock()
	s.health.latest = latest
	s.health.mu.Unlock()
}
