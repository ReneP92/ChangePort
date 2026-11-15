// Package agent keeps the settings the platform owns in place on a customer's
// Kafka cluster. It turns on tiered storage for every topic that can use it:
// Kafka has no broker-wide default for remote.storage.enable, so topics
// customers create would otherwise keep all their history on the brokers'
// disks. It keeps client telemetry (KIP-714) in place (telemetry.go), and
// measures consumer lag in seconds (lag.go).
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

const (
	remoteStorageEnable = "remote.storage.enable"
	cleanupPolicy       = "cleanup.policy"
	retentionMs         = "retention.ms"
	localRetentionMs    = "local.retention.ms"
)

// kerrUnknownTopic is the error for a topic deleted while the agent works on it.
var kerrUnknownTopic = kerr.UnknownTopicOrPartition

// Admin is the part of the Kafka Admin API the agent uses; *kadm.Client
// implements it.
type Admin interface {
	ListTopics(ctx context.Context, topics ...string) (kadm.TopicDetails, error)
	DescribeTopicConfigs(ctx context.Context, topics ...string) (kadm.ResourceConfigs, error)
	AlterTopicConfigs(ctx context.Context, configs []kadm.AlterConfig, topics ...string) (kadm.AlterConfigsResponses, error)
}

var _ Admin = (*kadm.Client)(nil)

// State is what the agent found a topic to be.
type State string

// The states of a topic.
const (
	// Tiered topics offload finished segments to tiered storage.
	Tiered State = "tiered"
	// Enable marks a topic the agent is about to tier.
	Enable State = "enable"
	// Compacted topics cannot be tiered in Kafka 4.3.
	Compacted State = "compacted"
	// OptedOut topics have remote.storage.enable=false set explicitly; the
	// agent leaves the customer's choice alone.
	OptedOut State = "opted_out"
)

// Decide returns what the agent does with a topic, given its configs.
func Decide(configs []kadm.Config) State {
	var remote, policy *kadm.Config
	for i := range configs {
		switch configs[i].Key {
		case remoteStorageEnable:
			remote = &configs[i]
		case cleanupPolicy:
			policy = &configs[i]
		}
	}
	switch {
	case remote != nil && strings.EqualFold(value(remote), "true"):
		return Tiered
	case policy != nil && strings.Contains(value(policy), "compact"):
		return Compacted
	case remote != nil && remote.Source == kmsg.ConfigSourceDynamicTopicConfig:
		return OptedOut
	default:
		return Enable
	}
}

// localRetention returns the local.retention.ms that tiering a topic
// needs, or "" for the brokers' own: Kafka refuses tiered storage for a
// topic whose retention is shorter than the day the brokers keep on disk
// (log.local.retention.ms), so such a topic keeps only its retention
// locally, as the console sets for the topics it creates
// (internal/topics). A local retention the topic sets itself is left alone.
func localRetention(configs []kadm.Config) string {
	var retention, local *kadm.Config
	for i := range configs {
		switch configs[i].Key {
		case retentionMs:
			retention = &configs[i]
		case localRetentionMs:
			local = &configs[i]
		}
	}
	if retention == nil || local == nil || local.Source == kmsg.ConfigSourceDynamicTopicConfig {
		return ""
	}
	r, err := strconv.ParseInt(value(retention), 10, 64)
	if err != nil {
		return ""
	}
	l, err := strconv.ParseInt(value(local), 10, 64)
	// -1 keeps messages for ever, and a local -2 follows the retention.
	if err != nil || r < 0 || l < 0 || l <= r {
		return ""
	}
	return value(retention)
}

func value(c *kadm.Config) string {
	if c.Value == nil {
		return ""
	}
	return *c.Value
}

// internal reports whether a topic belongs to Kafka or the platform, such as
// __consumer_offsets and __remote_log_metadata.
func internal(topic string) bool { return strings.HasPrefix(topic, "__") }

// Reconciler turns on tiered storage for the topics that can use it.
type Reconciler struct {
	Admin   Admin
	Logger  *slog.Logger
	Metrics *Metrics
}

// Reconcile looks at every topic once and turns on tiered storage where
// Decide says so. It returns the first error after trying every topic.
func (r *Reconciler) Reconcile(ctx context.Context) error {
	err := r.reconcile(ctx)
	if err != nil {
		r.Metrics.reconciles.WithLabelValues("error").Inc()
		return err
	}
	r.Metrics.reconciles.WithLabelValues("ok").Inc()
	r.Metrics.lastSuccess.SetToCurrentTime()
	return nil
}

func (r *Reconciler) reconcile(ctx context.Context) error {
	details, err := r.Admin.ListTopics(ctx)
	if err != nil {
		return fmt.Errorf("list topics: %w", err)
	}
	var topics []string
	for _, topic := range details.Sorted() {
		if topic.Err == nil && !topic.IsInternal && !internal(topic.Topic) {
			topics = append(topics, topic.Topic)
		}
	}
	if len(topics) == 0 {
		r.Metrics.setTopics(nil)
		return nil
	}
	configs, err := r.Admin.DescribeTopicConfigs(ctx, topics...)
	if err != nil {
		return fmt.Errorf("describe topic configs: %w", err)
	}
	counts := map[State]int{}
	// The topics to tier, by the local retention they need with it.
	enable := map[string][]string{}
	var errs []error
	for _, rc := range configs {
		if rc.Err != nil {
			// A topic deleted since the listing is not an error.
			if !errors.Is(rc.Err, kerrUnknownTopic) {
				errs = append(errs, fmt.Errorf("describe configs of %s: %w", rc.Name, rc.Err))
			}
			continue
		}
		state := Decide(rc.Configs)
		if state == Enable {
			local := localRetention(rc.Configs)
			enable[local] = append(enable[local], rc.Name)
			continue
		}
		counts[state]++
	}
	for _, local := range slices.Sorted(maps.Keys(enable)) {
		set := []kadm.AlterConfig{{Op: kadm.SetConfig, Name: remoteStorageEnable, Value: kadm.StringPtr("true")}}
		if local != "" {
			set = append(set, kadm.AlterConfig{Op: kadm.SetConfig, Name: localRetentionMs, Value: kadm.StringPtr(local)})
		}
		responses, err := r.Admin.AlterTopicConfigs(ctx, set, enable[local]...)
		if err != nil {
			errs = append(errs, fmt.Errorf("alter topic configs: %w", err))
			continue
		}
		for _, response := range responses {
			if response.Err != nil {
				if !errors.Is(response.Err, kerrUnknownTopic) {
					errs = append(errs, fmt.Errorf("turn on tiered storage for %s: %w (%s)", response.Name, response.Err, response.ErrMessage))
				}
				continue
			}
			counts[Tiered]++
			r.Metrics.enabled.Inc()
			r.Logger.Info("turned on tiered storage", "topic", response.Name)
		}
	}
	r.Metrics.setTopics(counts)
	return errors.Join(errs...)
}

// Metrics are the agent's own Prometheus metrics.
type Metrics struct {
	reconciles  *prometheus.CounterVec
	enabled     prometheus.Counter
	topics      *prometheus.GaugeVec
	lastSuccess prometheus.Gauge
}

// NewMetrics registers the agent's metrics with registry.
func NewMetrics(registry prometheus.Registerer) *Metrics {
	m := &Metrics{
		reconciles: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "changeport_agent_reconciles_total",
			Help: "Reconciliations of the topic settings, by result (ok or error).",
		}, []string{"result"}),
		enabled: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "changeport_agent_tiering_enabled_total",
			Help: "Topics the agent turned tiered storage on for.",
		}),
		topics: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "changeport_agent_topics",
			Help: "Topics at the last reconciliation, by state: tiered, compacted (cannot be tiered) or opted_out (remote.storage.enable=false set explicitly).",
		}, []string{"state"}),
		lastSuccess: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "changeport_agent_last_success_timestamp_seconds",
			Help: "When the last reconciliation succeeded.",
		}),
	}
	registry.MustRegister(m.reconciles, m.enabled, m.topics, m.lastSuccess)
	for _, result := range []string{"ok", "error"} {
		m.reconciles.WithLabelValues(result)
	}
	return m
}

func (m *Metrics) setTopics(counts map[State]int) {
	for _, state := range []State{Tiered, Compacted, OptedOut} {
		m.topics.WithLabelValues(string(state)).Set(float64(counts[state]))
	}
}
