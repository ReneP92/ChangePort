package console

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ReneP92/changeport/internal/console/api"
	"github.com/ReneP92/changeport/internal/events"
)

// fakeEvents holds acme's timeline: its orders topic created an hour ago,
// billing rebalanced, a broker restart, and a change long ago.
type fakeEvents struct {
	mu     sync.Mutex
	events []events.Event
	// queries are the queries read, the last last.
	queries []events.Query
}

func newFakeEvents() *fakeEvents {
	now := time.Now()
	return &fakeEvents{events: []events.Event{
		{ID: "1", Time: now.Add(-50 * time.Minute), Tenant: "acme", Kind: events.TopicCreated, Topic: "acme-orders", Message: "Topic acme-orders was created with 3 partitions."},
		{ID: "2", Time: now.Add(-20 * time.Minute), Tenant: "acme", Kind: events.GroupRebalanced, ConsumerGroup: "billing", Message: "Consumer group billing rebalanced."},
		{ID: "3", Time: now.Add(-10 * time.Minute), Tenant: "acme", Kind: events.BrokerRestarted, Broker: "acme-dual-role-0", Message: "Broker acme-dual-role-0 restarted."},
		{ID: "4", Time: now.Add(-3 * time.Hour), Tenant: "acme", Kind: events.TopicConfigChanged, Topic: "acme-orders", Message: "Topic acme-orders: retention.ms changed."},
	}}
}

// Read answers as VictoriaLogs answers Query.LogsQL: the tenant's events in
// the range, those about the topics, the group, or a broker, newest first.
func (f *fakeEvents) Read(_ context.Context, q events.Query) ([]events.Event, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, q)
	var out []events.Event
	for _, e := range f.events {
		about := (len(q.Topics) == 0 && q.ConsumerGroup == "") || slices.Contains(q.Topics, e.Topic) ||
			(q.ConsumerGroup != "" && e.ConsumerGroup == q.ConsumerGroup) || e.Kind == events.BrokerRestarted
		if e.Tenant == q.Tenant && !e.Time.Before(q.Start) && e.Time.Before(q.End) && about {
			out = append(out, e)
		}
	}
	slices.SortFunc(out, func(a, b events.Event) int { return b.Time.Compare(a.Time) })
	return out, false, nil
}

func (f *fakeEvents) last() events.Query {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.queries[len(f.queries)-1]
}

type fakeRecorder struct {
	mu     sync.Mutex
	events []events.Event
}

func (f *fakeRecorder) Record(e ...events.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e...)
}

// fakeUsers names the users it knows, and fails for the others.
type fakeUsers map[string]string

func (f fakeUsers) Name(_ context.Context, userID string) (string, error) {
	if name, ok := f[userID]; ok {
		return name, nil
	}
	return "", errors.New("no such user")
}

func TestEventsOfARangeATopicAndAGroup(t *testing.T) {
	f := newFixture(t)
	member := f.signer.token(t, session{user: "user_2", org: "org_acme", role: "member"})
	ids := func(path string) []string {
		t.Helper()
		status, body, _ := get(t, f.handler, path, member)
		if status != http.StatusOK {
			t.Fatalf("GET %s = %d %s", path, status, body)
		}
		var out []string
		for _, e := range decode[api.EventList](t, body).Events {
			out = append(out, e.Id)
		}
		return out
	}
	if got := ids("/api/customer/events"); !slices.Equal(got, []string{"3", "2", "1"}) {
		t.Errorf("the last hour's events are %v, want 3, 2, 1", got)
	}
	if q := f.events.last(); q.Tenant != "acme" || time.Since(q.Start) < 59*time.Minute || q.End.Before(time.Now()) {
		t.Errorf("read %+v, want acme's last hour up to now", q)
	}
	if got := ids("/api/customer/events?range=6h"); !slices.Equal(got, []string{"3", "2", "1", "4"}) {
		t.Errorf("six hours' events are %v, want all four", got)
	}
	if got := ids("/api/customer/events?range=6h&topic=acme-orders"); !slices.Equal(got, []string{"3", "1", "4"}) {
		t.Errorf("acme-orders' events are %v, want the restart and the topic's", got)
	}
	// A group's events include those of the topics it reads.
	if got := ids("/api/customer/events?range=6h&consumerGroup=billing"); !slices.Equal(got, []string{"3", "2", "1", "4"}) {
		t.Errorf("billing's events are %v, want its own, its topic's and the restart", got)
	}
	if q := f.events.last(); q.ConsumerGroup != "billing" || !slices.Equal(q.Topics, []string{"acme-orders"}) {
		t.Errorf("read %+v, want billing and the topic it reads", q)
	}
	if status, body, _ := get(t, f.handler, "/api/customer/events?range=1y", member); status != http.StatusBadRequest {
		t.Errorf("GET events for a year = %d %s, want 400", status, body)
	}
}

func TestTopicChangesInTheConsoleNameWhoMadeThem(t *testing.T) {
	f := newFixture(t)
	admin := f.signer.token(t, session{user: "user_3", org: "org_acme", role: "admin"})
	if status, body := send(t, f.handler, http.MethodPost, "/api/customer/topics", admin, newTopic); status != http.StatusCreated {
		t.Fatalf("creating a topic = %d %s", status, body)
	}
	if status, body := send(t, f.handler, http.MethodDelete, "/api/customer/topics/acme-orders", admin, ""); status != http.StatusNoContent {
		t.Fatalf("deleting a topic = %d %s", status, body)
	}
	// Someone Clerk cannot name is named by their ID.
	unknown := f.signer.token(t, session{user: "user_9", org: "org_acme", role: "admin"})
	if status, body := send(t, f.handler, http.MethodDelete, "/api/customer/topics/acme-orders", unknown, ""); status != http.StatusNoContent {
		t.Fatalf("deleting a topic = %d %s", status, body)
	}
	var got []string
	for _, e := range f.recorded.events {
		if e.Tenant != "acme" || e.Source != "console" || e.Time.IsZero() {
			t.Errorf("recorded %+v, without its tenant, source or time", e)
		}
		got = append(got, string(e.Kind)+" "+e.Topic+" "+e.Actor+": "+e.Message)
	}
	want := []string{
		"topic.created invoices Ada Lovelace: Ada Lovelace created topic invoices with 3 partitions in the console.",
		"topic.deleted acme-orders Ada Lovelace: Ada Lovelace deleted topic acme-orders in the console.",
		"topic.deleted acme-orders user_9: user_9 deleted topic acme-orders in the console.",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("recorded\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestConsumerGroupLagHistory(t *testing.T) {
	f := newFixture(t)
	member := f.signer.token(t, session{user: "user_2", org: "org_acme", role: "member"})
	status, body, _ := get(t, f.handler, "/api/customer/consumer-groups/billing/lag?range=6h", member)
	history := decode[api.ConsumerGroupLagHistory](t, body)
	if status != http.StatusOK || history.Range != "6h" || history.StepSeconds != 300 || len(history.Topics) != 1 ||
		history.Topics[0].Topic != "acme-orders" || len(history.Topics[0].LagSeconds) < 70 {
		t.Errorf("billing's lag = %d %s", status, body)
	}
	if status, body, _ := get(t, f.handler, "/api/customer/consumer-groups/changeport-registry/lag", member); status != http.StatusNotFound {
		t.Errorf("the platform's group's lag = %d %s, want 404", status, body)
	}
}

func TestThroughputOfOneTopic(t *testing.T) {
	f := newFixture(t)
	member := f.signer.token(t, session{user: "user_2", org: "org_acme", role: "member"})
	status, body, _ := get(t, f.handler, "/api/customer/throughput?topic=acme-orders", member)
	throughput := decode[api.Throughput](t, body)
	// One topic of 10 bytes per second, rather than the two of the cluster.
	if status != http.StatusOK || len(throughput.BytesIn) == 0 || throughput.BytesIn[0].Value != 10 {
		t.Errorf("acme-orders' throughput = %d %s", status, body)
	}
}
