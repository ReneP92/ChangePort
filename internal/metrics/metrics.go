// Package metrics reads customers' metrics from VictoriaMetrics for the
// console. Callers choose among named queries; the package writes the PromQL
// itself and always selects the tenant, so no request can read another
// customer's series or run arbitrary queries.
package metrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ReneP92/changeport/internal/provisioning"
)

// Query names one of the console's charts or tables.
type Query string

// The named queries. Rates are per second over 5 minutes.
const (
	// BytesIn is the bytes per second producers write, per topic.
	BytesIn Query = "bytes_in"
	// BytesOut is the bytes per second consumers read, per topic.
	BytesOut Query = "bytes_out"
	// MessagesIn is the messages per second producers write, per topic.
	MessagesIn Query = "messages_in"
	// LocalSize is the bytes each topic keeps on the brokers' disks, once,
	// not counting replicas.
	LocalSize Query = "local_size"
	// RemoteSize is the bytes each topic keeps in tiered storage.
	RemoteSize Query = "remote_size"
	// LagSeconds is the age of each consumer group's oldest unconsumed
	// message, per group and topic.
	LagSeconds Query = "lag_seconds"
	// LagMessages is the messages each consumer group has not consumed yet,
	// per group and topic.
	LagMessages Query = "lag_messages"
	// LagCapped is 1 where a consumer group's lag is older than the agent
	// looks up (23 hours), so LagSeconds is a lower bound, per group and
	// topic.
	LagCapped Query = "lag_capped"

	// UnderReplicated is the partitions with fewer in-sync replicas than
	// replicas, over all brokers.
	UnderReplicated Query = "under_replicated"
	// UnderMinISR is the partitions with fewer in-sync replicas than
	// min.insync.replicas, which refuse writes with acks=all.
	UnderMinISR Query = "under_min_isr"
	// OfflinePartitions is the partitions without a leader, which refuse
	// reads and writes, as the active controller counts them.
	OfflinePartitions Query = "offline_partitions"
	// ActiveControllers is the controllers acting as the active one: 1 in a
	// healthy cluster.
	ActiveControllers Query = "active_controllers"
	// ActiveBrokers is the brokers the active controller sees registered.
	ActiveBrokers Query = "active_brokers"
	// FencedBrokers is the brokers the active controller has fenced.
	FencedBrokers Query = "fenced_brokers"

	// ProducerBytes is the bytes per second each producer writes to each
	// topic, as producers report it themselves (KIP-714), per Kafka user,
	// client ID and topic.
	ProducerBytes Query = "producer_bytes"
	// ConsumerBytes is the bytes per second each consumer reads from each
	// topic, as consumers report it themselves, per Kafka user, client ID
	// and topic.
	ConsumerBytes Query = "consumer_bytes"

	// The diagnoses' (internal/diagnosis): over the filter's window, 15
	// minutes unless it says otherwise, so that a moment's spike or dip is
	// no diagnosis.

	// MessagesInRecent is the messages per second each topic received.
	MessagesInRecent Query = "messages_in_recent"
	// ConsumedMessages is the messages per second each consumer group
	// consumed of each topic, from its committed offsets: every client's,
	// whether or not it reports telemetry.
	ConsumedMessages Query = "consumed_messages"
	// LagSecondsEarlier is each group's lag on each topic a window ago.
	LagSecondsEarlier Query = "lag_seconds_earlier"
	// LostPartitions is the partitions where retention deleted messages a
	// group never read.
	LostPartitions Query = "lost_partitions"
	// OffsetResets is how often a group's offsets on a topic went back in
	// the window, as when it replays the topic.
	OffsetResets Query = "offset_resets"
	// PartitionSize is the bytes on the brokers' disks of each partition.
	PartitionSize Query = "partition_size"
	// ProducerSends, ProducerErrors and ProducerRetries are the records per
	// second each producer sent, failed to send and sent again, as it
	// reports them, per Kafka user and client ID, over the window. A client
	// instance's counters start from 0, and its series with its first push,
	// so they count from 0 there (increase_pure): a short-lived producer's
	// first push is not lost as the start of a rate.
	ProducerSends   Query = "producer_sends"
	ProducerErrors  Query = "producer_errors"
	ProducerRetries Query = "producer_retries"
	// ProducerTopicErrors is the records per second each producer failed to
	// deliver to each topic: refused by Kafka, or expired on the way.
	// Records the producer refused itself, before sending, count only in
	// ProducerErrors.
	ProducerTopicErrors Query = "producer_topic_errors"
	// ProducerAborts is the nanoseconds each producer spent aborting
	// transactions over the window, as it reports it: Kafka counts aborts
	// nowhere, so time spent aborting is the sign of them.
	ProducerAborts Query = "producer_aborts"
	// BytesRejected is the bytes each topic refused over the window as
	// larger than its max.message.bytes. Brokers create a topic's meter at
	// its first refusal, so the counter is taken to start from 0 there
	// (increase_pure) rather than its first sample counting as the start.
	BytesRejected Query = "bytes_rejected"

	// TopicHealth and GroupHealth are the worst level of each topic and
	// consumer group over the filter's window, as the console exports it
	// (internal/console/health.go): 1 needs attention, 2 at risk; healthy
	// subjects have no series. HealthChecked is whether the console looked
	// at the cluster at all over the window.
	TopicHealth   Query = "topic_health"
	GroupHealth   Query = "group_health"
	HealthChecked Query = "health_checked"

	// UsageIn and UsageOut are the bytes producers wrote and consumers read
	// over the window, per topic: each byte once, as the brokers count a
	// topic's bytes out without what replicas fetch from its leader. See
	// usageIncrease for how each counter's growth is counted.
	UsageIn  Query = "usage_in"
	UsageOut Query = "usage_out"
)

// scope is what a query's series are about, which decides its filters.
type scope int

const (
	// perTopic queries have a series per topic.
	perTopic scope = iota
	// perGroup queries have a series per consumer group and topic.
	perGroup
	// perCluster queries have one series for the whole cluster.
	perCluster
	// perClient queries have a series per client and topic, of the
	// customer's own clients: the platform's, which connect with
	// certificates, are left out.
	perClient
	// perProducer queries have a series per client, over all its topics,
	// of the customer's own clients.
	perProducer
)

// definition is a query's PromQL, where %s is the series selector, and its
// scope.
type definition struct {
	expression string
	scope      scope
}

var definitions = map[Query]definition{
	BytesIn:     {`sum by (topic) (rate(kafka_server_brokertopicmetrics_bytesinpersec_total{%s}[5m]))`, perTopic},
	BytesOut:    {`sum by (topic) (rate(kafka_server_brokertopicmetrics_bytesoutpersec_total{%s}[5m]))`, perTopic},
	MessagesIn:  {`sum by (topic) (rate(kafka_server_brokertopicmetrics_messagesinpersec_total{%s}[5m]))`, perTopic},
	LocalSize:   {`sum by (topic) (max by (topic, partition) (kafka_log_log_size{%s}))`, perTopic},
	RemoteSize:  {`sum by (topic) (kafka_server_brokertopicmetrics_remotelogsizebytes{%s})`, perTopic},
	LagSeconds:  {`max by (consumer_group, topic) (changeport_consumergroup_lag_seconds{%s})`, perGroup},
	LagMessages: {`sum by (consumer_group, topic) (changeport_consumergroup_lag_messages{%s})`, perGroup},
	LagCapped:   {`max by (consumer_group, topic) (changeport_consumergroup_lag_capped{%s})`, perGroup},
	// Every broker reports its own partitions; every controller reports the
	// controller's counts, but only the active one knows them.
	UnderReplicated:   {`sum(kafka_server_replicamanager_underreplicatedpartitions{%s})`, perCluster},
	UnderMinISR:       {`sum(kafka_server_replicamanager_underminisrpartitioncount{%s})`, perCluster},
	OfflinePartitions: {`max(kafka_controller_kafkacontroller_offlinepartitionscount{%s})`, perCluster},
	ActiveControllers: {`sum(kafka_controller_kafkacontroller_activecontrollercount{%s})`, perCluster},
	// The active controller's count: a controller cut off from the others
	// keeps reporting what it last knew.
	ActiveBrokers: {`sum(kafka_controller_kafkacontroller_activebrokercount{%[1]s} * on (tenant, node_id) kafka_controller_kafkacontroller_activecontrollercount{%[1]s})`, perCluster},
	FencedBrokers: {`sum(kafka_controller_kafkacontroller_fencedbrokercount{%[1]s} * on (tenant, node_id) kafka_controller_kafkacontroller_activecontrollercount{%[1]s})`, perCluster},
	// A client pushes to one broker at a time, and both expose its series
	// for a while when it moves, so each client instance counts once.
	ProducerBytes: {`sum by (principal, client_id, topic) (max by (principal, client_id, client_instance_id, topic) (rate(clients_org_apache_kafka_producer_topic_byte_total{%s}[5m])))`, perClient},
	ConsumerBytes: {`sum by (principal, client_id, topic) (max by (principal, client_id, client_instance_id, topic) (rate(clients_org_apache_kafka_consumer_fetch_manager_bytes_consumed_total{%s}[5m])))`, perClient},

	MessagesInRecent: {`sum by (topic) (rate(kafka_server_brokertopicmetrics_messagesinpersec_total{%s}[{window}]))`, perTopic},
	// Committed offsets only grow, but for a reset; the slope over the
	// window is the rate, and a reset is no negative consumption.
	ConsumedMessages:  {`max by (consumer_group, topic) (clamp_min(deriv(changeport_consumergroup_committed_offsets{%s}[{window}]), 0))`, perGroup},
	LagSecondsEarlier: {`max by (consumer_group, topic) (changeport_consumergroup_lag_seconds{%s} offset {window})`, perGroup},
	LostPartitions:    {`max by (consumer_group, topic) (changeport_consumergroup_partitions_behind_log_start{%s})`, perGroup},
	OffsetResets:      {`max by (consumer_group, topic) (resets(changeport_consumergroup_committed_offsets{%s}[{window}]))`, perGroup},
	// Each replica of a partition holds the same; the largest is the leader's.
	PartitionSize:       {`max by (topic, partition) (kafka_log_log_size{%s})`, perTopic},
	ProducerSends:       {`sum by (principal, client_id) (max by (principal, client_id, client_instance_id) (increase_pure(clients_org_apache_kafka_producer_record_send_total{%s}[{window}]) / {seconds}))`, perProducer},
	ProducerErrors:      {`sum by (principal, client_id) (max by (principal, client_id, client_instance_id) (increase_pure(clients_org_apache_kafka_producer_record_error_total{%s}[{window}]) / {seconds}))`, perProducer},
	ProducerRetries:     {`sum by (principal, client_id) (max by (principal, client_id, client_instance_id) (increase_pure(clients_org_apache_kafka_producer_record_retry_total{%s}[{window}]) / {seconds}))`, perProducer},
	ProducerTopicErrors: {`sum by (principal, client_id, topic) (max by (principal, client_id, client_instance_id, topic) (increase_pure(clients_org_apache_kafka_producer_topic_record_error_total{%s}[{window}]) / {seconds}))`, perClient},
	ProducerAborts:      {`sum by (principal, client_id) (max by (principal, client_id, client_instance_id) (increase_pure(clients_org_apache_kafka_producer_txn_abort_time_ns_total{%s}[{window}])))`, perProducer},
	BytesRejected:       {`sum by (topic) (increase_pure(kafka_server_brokertopicmetrics_bytesrejectedpersec_total{%s}[{window}]))`, perTopic},

	TopicHealth:   {`max by (topic) (max_over_time(changeport_health_level{%s}[{window}]))`, perTopic},
	GroupHealth:   {`max by (consumer_group) (max_over_time(changeport_health_level{%s, consumer_group!=""}[{window}]))`, perGroup},
	HealthChecked: {`max(max_over_time(changeport_health_checked{%s}[{window}]))`, perCluster},

	UsageIn:  {usageIncrease("kafka_server_brokertopicmetrics_bytesinpersec_total"), perTopic},
	UsageOut: {usageIncrease("kafka_server_brokertopicmetrics_bytesoutpersec_total"), perTopic},
}

// usageIncrease is how much a byte counter grew over the window, per topic,
// counting each series from where it was before the window when it has a
// sample in the 31 days before (increase), and from 0 when it has none, as a
// series born in the window, such as a new topic's, starts there
// (increase_pure). increase_pure alone would count a series' whole value
// whenever the store missed its samples around the window's start, as while
// it restarts; increase alone would miss what a new topic received before
// its first sample. What a counter grew while the store had no samples of it
// at a window's start is left out, rather than guessed. A broker's restart
// resets its counters, which both see.
func usageIncrease(counter string) string {
	series := counter + "{%[1]s}"
	return `sum by (topic) ((increase_pure(` + series + `[{window}]) unless count_over_time(` + series + `[31d] offset {window})) or increase(` + series + `[{window}]))`
}

// Queries lists every named query.
func Queries() []Query {
	return []Query{
		BytesIn, BytesOut, MessagesIn, LocalSize, RemoteSize, LagSeconds, LagMessages, LagCapped,
		UnderReplicated, UnderMinISR, OfflinePartitions, ActiveControllers, ActiveBrokers, FencedBrokers,
		ProducerBytes, ConsumerBytes,
		MessagesInRecent, ConsumedMessages, LagSecondsEarlier, LostPartitions, OffsetResets, PartitionSize,
		ProducerSends, ProducerErrors, ProducerRetries, ProducerTopicErrors, ProducerAborts, BytesRejected,
		TopicHealth, GroupHealth, HealthChecked, UsageIn, UsageOut,
	}
}

// ErrUnknownQuery is returned for a query name that is not one of Queries.
var ErrUnknownQuery = errors.New("unknown query")

// ParseQuery returns the named query, or ErrUnknownQuery.
func ParseQuery(name string) (Query, error) {
	if _, ok := definitions[Query(name)]; !ok {
		return "", fmt.Errorf("%w %q", ErrUnknownQuery, name)
	}
	return Query(name), nil
}

// Filter narrows a query to one topic or consumer group; empty fields match
// everything. Window is how far back the diagnoses' queries look, 15
// minutes when zero.
type Filter struct {
	Topic         string
	ConsumerGroup string
	Window        time.Duration
}

// DefaultWindow is how far back the diagnoses' queries look unless a
// filter says otherwise.
const DefaultWindow = 15 * time.Minute

// Expr returns the PromQL of q for tenant's series that match filter.
func Expr(q Query, tenant string, filter Filter) (string, error) {
	d, ok := definitions[q]
	if !ok {
		return "", fmt.Errorf("%w %q", ErrUnknownQuery, q)
	}
	if err := provisioning.ValidateCustomerID(tenant); err != nil {
		return "", err
	}
	selector := []string{`tenant=` + quote(tenant)}
	switch {
	case d.scope == perCluster && (filter.Topic != "" || filter.ConsumerGroup != ""):
		return "", fmt.Errorf("query %s is about the whole cluster", q)
	case d.scope == perTopic && filter.ConsumerGroup != "":
		return "", fmt.Errorf("query %s has no consumer groups", q)
	case d.scope == perProducer && filter.Topic != "":
		return "", fmt.Errorf("query %s is about clients over all their topics", q)
	case filter.Topic != "":
		selector = append(selector, `topic=`+quote(filter.Topic))
	case d.scope == perTopic || d.scope == perClient:
		// Broker metrics without a topic label are totals over all topics,
		// as are clients' own.
		selector = append(selector, `topic!=""`)
	}
	if d.scope == perClient || d.scope == perProducer {
		if filter.ConsumerGroup != "" {
			return "", fmt.Errorf("query %s has no consumer groups", q)
		}
		selector = append(selector, `principal!~"User:CN=.*"`)
	}
	if filter.ConsumerGroup != "" {
		selector = append(selector, `consumer_group=`+quote(filter.ConsumerGroup))
	}
	window := filter.Window
	if window <= 0 {
		window = DefaultWindow
	}
	seconds := int(window.Seconds())
	expression := strings.NewReplacer("{window}", fmt.Sprintf("%ds", seconds), "{seconds}", strconv.Itoa(seconds)).Replace(d.expression)
	return fmt.Sprintf(expression, strings.Join(selector, ", ")), nil
}

// quote writes s as a PromQL string literal.
func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(s) + `"`
}

// Point is one value of a series.
type Point struct {
	Time  time.Time
	Value float64
}

// Series is one result series: its labels, such as topic, and its points.
type Series struct {
	Labels map[string]string
	Points []Point
}

// Client queries VictoriaMetrics' Prometheus API.
type Client struct {
	base *url.URL
	http *http.Client
}

// NewClient returns a client for the VictoriaMetrics at baseURL, such as
// http://victoria-metrics.metrics.svc:8428, which comes from the console's
// configuration.
func NewClient(baseURL string) (*Client, error) {
	base, err := url.Parse(strings.TrimSuffix(baseURL, "/"))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return nil, fmt.Errorf("VictoriaMetrics' address %q is not an http(s) URL", baseURL)
	}
	return &Client{base: base, http: &http.Client{Timeout: 30 * time.Second}}, nil
}

// MaxPoints caps the points of one series in a range query.
const MaxPoints = 1000

// Range returns q for tenant from start to end, one point per step.
func (c *Client) Range(ctx context.Context, tenant string, q Query, filter Filter, start, end time.Time, step time.Duration) ([]Series, error) {
	expression, err := Expr(q, tenant, filter)
	if err != nil {
		return nil, err
	}
	switch {
	case step < time.Second:
		return nil, fmt.Errorf("step %v is shorter than a second", step)
	case !end.After(start):
		return nil, errors.New("the range ends before it starts")
	case end.Sub(start)/step > MaxPoints:
		return nil, fmt.Errorf("the range has more than %d points; use a longer step", MaxPoints)
	}
	params := url.Values{
		"query": {expression},
		"start": {unix(start)},
		"end":   {unix(end)},
		"step":  {strconv.FormatFloat(step.Seconds(), 'f', -1, 64)},
	}
	return c.get(ctx, "/api/v1/query_range", params)
}

// Instant returns q for tenant at one moment: one point per series.
func (c *Client) Instant(ctx context.Context, tenant string, q Query, filter Filter, at time.Time) ([]Series, error) {
	expression, err := Expr(q, tenant, filter)
	if err != nil {
		return nil, err
	}
	return c.get(ctx, "/api/v1/query", url.Values{"query": {expression}, "time": {unix(at)}})
}

func unix(t time.Time) string {
	return strconv.FormatFloat(float64(t.UnixMilli())/1000, 'f', -1, 64)
}

// response is the Prometheus API's response to a query.
type response struct {
	Status    string `json:"status"`
	ErrorType string `json:"errorType"`
	Error     string `json:"error"`
	Data      struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  []any             `json:"value"`
			Values [][]any           `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

// get queries one of the API's fixed paths on the configured server; only
// the query parameters come from the request.
func (c *Client) get(ctx context.Context, path string, params url.Values) ([]Series, error) {
	target := *c.base
	target.Path += path
	target.RawQuery = params.Encode()
	// The server is the configured one; requests only choose the query, which
	// Expr builds with the tenant and quoted label values.
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), http.NoBody) //nolint:gosec // G704: fixed server, see above
	if err != nil {
		return nil, err
	}
	reply, err := c.http.Do(request) //nolint:gosec // G704: fixed server, see above
	if err != nil {
		return nil, fmt.Errorf("query VictoriaMetrics: %w", err)
	}
	defer reply.Body.Close()
	body, err := io.ReadAll(io.LimitReader(reply.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("read VictoriaMetrics' response: %w", err)
	}
	var decoded response
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("VictoriaMetrics answered %s: %.200s", reply.Status, body)
	}
	if decoded.Status != "success" {
		return nil, fmt.Errorf("VictoriaMetrics refused the query (%s): %s", decoded.ErrorType, decoded.Error)
	}
	series := make([]Series, 0, len(decoded.Data.Result))
	for _, result := range decoded.Data.Result {
		s := Series{Labels: result.Metric}
		samples := result.Values
		if result.Value != nil {
			samples = [][]any{result.Value}
		}
		for _, sample := range samples {
			point, err := parsePoint(sample)
			if err != nil {
				return nil, err
			}
			s.Points = append(s.Points, point)
		}
		series = append(series, s)
	}
	return series, nil
}

// parsePoint reads a [unix seconds, "value"] pair.
func parsePoint(sample []any) (Point, error) {
	if len(sample) != 2 {
		return Point{}, fmt.Errorf("a sample has %d fields, want 2", len(sample))
	}
	seconds, ok := sample[0].(float64)
	text, ok2 := sample[1].(string)
	if !ok || !ok2 {
		return Point{}, fmt.Errorf("malformed sample %v", sample)
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return Point{}, fmt.Errorf("sample value %q: %w", text, err)
	}
	return Point{Time: time.UnixMilli(int64(seconds * 1000)), Value: value}, nil
}
