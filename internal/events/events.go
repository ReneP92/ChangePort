// Package events keeps the changes in customers' clusters, which the
// console shows on a timeline over its graphs (docs/timeline.md), in
// VictoriaLogs: topics created, deleted and changed, ACLs, consumer group
// rebalances, broker restarts and throttling. Writers record events without
// waiting for the store; readers always select one customer's events.
package events

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ReneP92/changeport/internal/provisioning"
)

// Kind is what happened.
type Kind string

// The kinds of events.
const (
	// TopicCreated and TopicDeleted are a topic that appeared or went.
	TopicCreated Kind = "topic.created"
	TopicDeleted Kind = "topic.deleted"
	// TopicConfigChanged is a topic whose configuration changed.
	TopicConfigChanged Kind = "topic.config"
	// TopicPartitionsAdded is a topic that got more partitions.
	TopicPartitionsAdded Kind = "topic.partitions"
	// ACLAdded and ACLRemoved are a Kafka ACL that appeared or went.
	ACLAdded   Kind = "acl.added"
	ACLRemoved Kind = "acl.removed"
	// GroupRebalanced is a consumer group whose members changed, so it
	// assigned its partitions anew.
	GroupRebalanced Kind = "group.rebalanced"
	// BrokerRestarted is a broker that restarted, or whose pod was replaced.
	BrokerRestarted Kind = "broker.restarted"
	// ClientThrottled and ClientUnthrottled are a client the brokers started
	// and stopped throttling.
	ClientThrottled   Kind = "client.throttled"
	ClientUnthrottled Kind = "client.unthrottled"
)

// Kinds lists every kind.
func Kinds() []Kind {
	return []Kind{
		TopicCreated, TopicDeleted, TopicConfigChanged, TopicPartitionsAdded, ACLAdded, ACLRemoved,
		GroupRebalanced, BrokerRestarted, ClientThrottled, ClientUnthrottled,
	}
}

// Event is one change in a customer's cluster.
type Event struct {
	// ID identifies the event, so that one recorded twice shows once.
	ID     string    `json:"id"`
	Time   time.Time `json:"_time"`
	Tenant string    `json:"tenant"`
	Kind   Kind      `json:"kind"`
	// Message says what happened, in plain language.
	Message string `json:"_msg"`
	// What the event is about, where it applies.
	Topic         string `json:"topic,omitempty"`
	ConsumerGroup string `json:"consumer_group,omitempty"`
	Broker        string `json:"broker,omitempty"`
	ClientID      string `json:"client_id,omitempty"`
	// Actor is who made the change, where Changeport knows it.
	Actor string `json:"actor,omitempty"`
	// Source is what recorded the event: agent, console or provisioner.
	Source string `json:"source"`
}

// NewID returns a random event ID.
func NewID() string {
	id := make([]byte, 8)
	_, _ = rand.Read(id)
	return hex.EncodeToString(id)
}

// Client writes events to VictoriaLogs and reads them back.
type Client struct {
	base *url.URL
	http *http.Client
}

// NewClient returns a client of the VictoriaLogs at baseURL, such as
// http://victoria-logs.metrics.svc:9428.
func NewClient(baseURL string) (*Client, error) {
	base, err := url.Parse(strings.TrimSuffix(baseURL, "/"))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return nil, fmt.Errorf("VictoriaLogs' address %q is not an http(s) URL", baseURL)
	}
	return &Client{base: base, http: &http.Client{Timeout: 30 * time.Second}}, nil
}

// Write stores events. Each stream is a customer's, so that reading one
// customer's events never scans another's.
func (c *Client) Write(ctx context.Context, events ...Event) error {
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	for _, e := range events {
		if err := provisioning.ValidateCustomerID(e.Tenant); err != nil {
			return fmt.Errorf("event %s: %w", e.ID, err)
		}
		e.Time = e.Time.UTC()
		if err := encoder.Encode(e); err != nil {
			return err
		}
	}
	query := url.Values{"_stream_fields": {"tenant"}, "_time_field": {"_time"}, "_msg_field": {"_msg"}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base.String()+"/insert/jsonline?"+query.Encode(), &body)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/stream+json")
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("write events: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		text, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return fmt.Errorf("write events: %s: %s", response.Status, text)
	}
	return nil
}

// Query selects one customer's events.
type Query struct {
	Tenant     string
	Start, End time.Time
	// Topics and ConsumerGroup narrow the events to those about the topics
	// or the group; broker restarts, which affect everything, stay.
	Topics        []string
	ConsumerGroup string
	// Limit caps the events, the newest first; at most MaxEvents.
	Limit int
}

// MaxEvents caps the events of one query.
const MaxEvents = 1000

// LogsQL returns the query's LogsQL, which always selects the tenant's
// stream.
func (q Query) LogsQL() (string, error) {
	if err := provisioning.ValidateCustomerID(q.Tenant); err != nil {
		return "", err
	}
	if !q.Start.Before(q.End) {
		return "", errors.New("the time range is empty")
	}
	logsQL := fmt.Sprintf(`{tenant=%s} _time:[%s, %s)`, strconv.Quote(q.Tenant),
		q.Start.UTC().Format(time.RFC3339Nano), q.End.UTC().Format(time.RFC3339Nano))
	var about []string
	for _, topic := range q.Topics {
		about = append(about, "topic:="+strconv.Quote(topic))
	}
	if q.ConsumerGroup != "" {
		about = append(about, "consumer_group:="+strconv.Quote(q.ConsumerGroup))
	}
	if len(about) > 0 {
		about = append(about, "kind:="+strconv.Quote(string(BrokerRestarted)))
		logsQL += " (" + strings.Join(about, " or ") + ")"
	}
	return logsQL + " | sort by (_time) desc", nil
}

// Read returns the events of q, the newest first, and whether there were
// more than its limit.
func (c *Client) Read(ctx context.Context, q Query) ([]Event, bool, error) {
	logsQL, err := q.LogsQL()
	if err != nil {
		return nil, false, err
	}
	limit := min(cmp.Or(q.Limit, MaxEvents), MaxEvents)
	form := url.Values{"query": {logsQL}, "limit": {strconv.Itoa(limit + 1)}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base.String()+"/select/logsql/query", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, false, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := c.http.Do(request)
	if err != nil {
		return nil, false, fmt.Errorf("read the events of customer %s: %w", q.Tenant, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		text, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return nil, false, fmt.Errorf("read the events of customer %s: %s: %s", q.Tenant, response.Status, text)
	}
	var events []Event
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			continue
		}
		var e Event
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			return nil, false, fmt.Errorf("read an event of customer %s: %w", q.Tenant, err)
		}
		if e.Tenant != q.Tenant {
			// The query selects the tenant's stream; this keeps a mistake in
			// it from showing a customer another's events.
			continue
		}
		events = append(events, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, false, fmt.Errorf("read the events of customer %s: %w", q.Tenant, err)
	}
	events = Merge(events)
	more := len(events) > limit
	if more {
		events = events[:limit]
	}
	return events, more, nil
}

// sameChange is how far apart the console's record of a change and the
// agent's, which finds it within its interval, may be.
const sameChange = 2 * time.Minute

// Merge drops repeated events, the newest first: those recorded twice under
// one ID, and the agent's record of a topic the console created or deleted,
// whose record names who did it.
func Merge(events []Event) []Event {
	seen := map[string]bool{}
	out := make([]Event, 0, len(events))
	for _, e := range events {
		if e.ID != "" && seen[e.ID] {
			continue
		}
		seen[e.ID] = true
		out = append(out, e)
	}
	return slices.DeleteFunc(out, func(e Event) bool {
		if e.Actor != "" || (e.Kind != TopicCreated && e.Kind != TopicDeleted) {
			return false
		}
		return slices.ContainsFunc(out, func(other Event) bool {
			return other.Actor != "" && other.Kind == e.Kind && other.Topic == e.Topic &&
				other.Time.Sub(e.Time).Abs() <= sameChange
		})
	})
}
