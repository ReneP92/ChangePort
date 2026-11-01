package events

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

var at = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

func TestQueriesSelectTheTenant(t *testing.T) {
	for _, c := range []struct {
		name  string
		query Query
		want  string
	}{
		{
			"everything",
			Query{Tenant: "acme", Start: at, End: at.Add(time.Hour)},
			`{tenant="acme"} _time:[2026-10-31T12:00:00Z, 2026-10-31T13:00:00Z) | sort by (_time) desc`,
		},
		{
			"a topic",
			Query{Tenant: "acme", Start: at, End: at.Add(time.Hour), Topics: []string{"orders"}},
			`{tenant="acme"} _time:[2026-10-31T12:00:00Z, 2026-10-31T13:00:00Z) (topic:="orders" or kind:="broker.restarted") | sort by (_time) desc`,
		},
		{
			"a group and its topics",
			Query{Tenant: "acme", Start: at, End: at.Add(time.Hour), Topics: []string{"a", "b"}, ConsumerGroup: `odd "group" \ name`},
			`{tenant="acme"} _time:[2026-10-31T12:00:00Z, 2026-10-31T13:00:00Z) (topic:="a" or topic:="b" or consumer_group:="odd \"group\" \\ name" or kind:="broker.restarted") | sort by (_time) desc`,
		},
	} {
		got, err := c.query.LogsQL()
		if err != nil || got != c.want {
			t.Errorf("%s: LogsQL() = %s, %v\nwant %s", c.name, got, err, c.want)
		}
	}
	for _, bad := range []Query{
		{Tenant: `acme"} or {tenant="globex`, Start: at, End: at.Add(time.Hour)},
		{Tenant: "acme", Start: at, End: at},
	} {
		if got, err := bad.LogsQL(); err == nil {
			t.Errorf("LogsQL() of %+v = %s, want an error", bad, got)
		}
	}
}

func TestMergeDropsRepeatedEvents(t *testing.T) {
	events := []Event{
		{ID: "console-1", Time: at.Add(30 * time.Second), Kind: TopicCreated, Topic: "orders", Actor: "ada@acme.example", Source: "console"},
		{ID: "agent-1", Time: at, Kind: TopicCreated, Topic: "orders", Source: "agent"},
		{ID: "agent-1", Time: at, Kind: TopicCreated, Topic: "orders", Source: "agent"},
		// Created again much later, outside the console.
		{ID: "agent-2", Time: at.Add(-time.Hour), Kind: TopicCreated, Topic: "orders", Source: "agent"},
		{ID: "agent-3", Time: at, Kind: TopicCreated, Topic: "payments", Source: "agent"},
		{ID: "agent-4", Time: at, Kind: TopicConfigChanged, Topic: "orders", Source: "agent"},
	}
	var ids []string
	for _, e := range Merge(events) {
		ids = append(ids, e.ID)
	}
	if want := []string{"console-1", "agent-2", "agent-3", "agent-4"}; !slices.Equal(ids, want) {
		t.Errorf("Merge() kept %v, want %v", ids, want)
	}
}

// failingStore fails its first writes, then keeps what it is given.
type failingStore struct {
	mu       sync.Mutex
	failures int
	written  []Event
}

func (s *failingStore) Write(_ context.Context, events ...Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failures > 0 {
		s.failures--
		return errors.New("unavailable")
	}
	s.written = append(s.written, events...)
	return nil
}

func (s *failingStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.written)
}

func TestRecorderWritesOnceTheStoreIsBack(t *testing.T) {
	store := &failingStore{failures: 2}
	recorder := NewRecorder(store, slog.New(slog.NewTextHandler(io.Discard, nil)), RecorderOptions{Every: 10 * time.Millisecond})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { recorder.Run(ctx); close(done) }()
	recorder.Record(Event{Tenant: "acme", Kind: TopicCreated, Topic: "orders"}, Event{Tenant: "acme", Kind: TopicDeleted, Topic: "orders"})
	deadline := time.Now().Add(5 * time.Second)
	for store.count() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	if len(store.written) != 2 || store.written[0].ID == "" || store.written[0].Time.IsZero() || store.written[1].Kind != TopicDeleted {
		t.Errorf("wrote %+v, want both events, in order, with IDs and times", store.written)
	}
}

func TestRecorderDropsTheOldestPastItsCapacity(t *testing.T) {
	store := &failingStore{}
	recorder := NewRecorder(store, slog.New(slog.NewTextHandler(io.Discard, nil)), RecorderOptions{Capacity: 2})
	for i := range 3 {
		recorder.Record(Event{ID: strconv.Itoa(i), Tenant: "acme", Kind: TopicCreated})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	recorder.Run(ctx)
	if len(store.written) != 2 || store.written[0].ID != "1" || store.written[1].ID != "2" {
		t.Errorf("wrote %+v, want the two newest events", store.written)
	}
}

// TestAgainstVictoriaLogs writes and reads events through the VictoriaLogs
// image the platform runs; it runs where Docker does (just test).
func TestAgainstVictoriaLogs(t *testing.T) {
	if os.Getenv("CHANGEPORT_DOCKER_TESTS") == "" {
		t.Skip("CHANGEPORT_DOCKER_TESTS is not set; just test sets it")
	}
	client := startVictoriaLogs(t)
	now := time.Now().UTC().Truncate(time.Second)
	written := []Event{
		{ID: "1", Time: now.Add(-3 * time.Minute), Tenant: "acme", Kind: TopicCreated, Topic: "orders", Message: "Topic orders was created", Source: "agent"},
		{ID: "2", Time: now.Add(-2 * time.Minute), Tenant: "acme", Kind: GroupRebalanced, ConsumerGroup: "billing", Topic: "", Message: "billing rebalanced", Source: "agent"},
		{ID: "3", Time: now.Add(-time.Minute), Tenant: "acme", Kind: BrokerRestarted, Broker: "acme-dual-role-0", Message: "restarted", Source: "provisioner"},
		{ID: "4", Time: now.Add(-time.Minute), Tenant: "globex", Kind: TopicCreated, Topic: "orders", Message: "globex's", Source: "agent"},
		{ID: "1", Time: now.Add(-3 * time.Minute), Tenant: "acme", Kind: TopicCreated, Topic: "orders", Message: "Topic orders was created", Source: "agent"},
	}
	if err := client.Write(t.Context(), written...); err != nil {
		t.Fatal(err)
	}
	read := func(q Query) []string {
		t.Helper()
		var ids []string
		deadline := time.Now().Add(20 * time.Second)
		for {
			events, _, err := client.Read(t.Context(), q)
			if err != nil {
				t.Fatal(err)
			}
			ids = ids[:0]
			for _, e := range events {
				ids = append(ids, e.ID)
			}
			// Written events become searchable within a second or so.
			if len(ids) > 0 || time.Now().After(deadline) {
				return ids
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	window := Query{Tenant: "acme", Start: now.Add(-time.Hour), End: now.Add(time.Minute)}
	if got := read(window); !slices.Equal(got, []string{"3", "2", "1"}) {
		t.Errorf("acme's events are %v, want 3, 2, 1, newest first, each once", got)
	}
	topic := window
	topic.Topics = []string{"orders"}
	if got := read(topic); !slices.Equal(got, []string{"3", "1"}) {
		t.Errorf("acme's events about orders are %v, want the restart and the topic's", got)
	}
	group := window
	group.ConsumerGroup = "billing"
	if got := read(group); !slices.Equal(got, []string{"3", "2"}) {
		t.Errorf("acme's events about billing are %v, want the restart and the group's", got)
	}
	globex := window
	globex.Tenant = "globex"
	if got := read(globex); !slices.Equal(got, []string{"4"}) {
		t.Errorf("globex's events are %v, want its own", got)
	}
	limited := window
	limited.Limit = 2
	if events, more, err := client.Read(t.Context(), limited); err != nil || len(events) != 2 || !more {
		t.Errorf("Read() with a limit = %d events, more %t, %v; want 2 and more", len(events), more, err)
	}
}

// startVictoriaLogs runs the VictoriaLogs image of deploy/base/victoria-logs
// on a free port for the test.
func startVictoriaLogs(t *testing.T) *Client {
	t.Helper()
	manifest, err := os.ReadFile("../../deploy/base/victoria-logs/victoria-logs.yaml")
	if err != nil {
		t.Fatal(err)
	}
	image := ""
	for line := range strings.SplitSeq(string(manifest), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "image: "); ok {
			image = value
			break
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	name := fmt.Sprintf("changeport-events-test-%d", port)
	if output, err := exec.CommandContext(t.Context(), "docker", "run", "-d", "--rm", "--name", name,
		"-p", fmt.Sprintf("127.0.0.1:%d:9428", port), image).CombinedOutput(); err != nil {
		t.Fatalf("start VictoriaLogs: %v\n%s", err, output)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(60 * time.Second)
	for {
		request, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, base+"/health", http.NoBody)
		if response, err := http.DefaultClient.Do(request); err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("VictoriaLogs did not start within a minute")
		}
		time.Sleep(200 * time.Millisecond)
	}
	client, err := NewClient(base)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestAlertmanagerHandlerRecordsThrottling(t *testing.T) {
	recorded := &failingStore{}
	recorder := NewRecorder(recorded, slog.New(slog.NewTextHandler(io.Discard, nil)), RecorderOptions{})
	handler := AlertmanagerHandler(recorder, slog.New(slog.NewTextHandler(io.Discard, nil)))
	body := `{"version":"4","status":"resolved","alerts":[
	  {"status":"resolved","fingerprint":"f1","startsAt":"2026-10-31T12:00:00Z","endsAt":"2026-10-31T12:05:00Z",
	   "labels":{"alertname":"TimelineClientThrottled","audience":"timeline","tenant":"acme","client_id":"loader"}},
	  {"status":"firing","fingerprint":"f2","startsAt":"2026-10-31T12:10:00Z","endsAt":"0001-01-01T00:00:00Z",
	   "labels":{"alertname":"TimelineClientThrottled","audience":"timeline","tenant":"acme","client_id":"writer"}},
	  {"status":"firing","fingerprint":"f3","startsAt":"2026-10-31T12:10:00Z",
	   "labels":{"alertname":"ConsumerGroupLagHigh","audience":"customer","tenant":"acme"}},
	  {"status":"firing","fingerprint":"f4","startsAt":"2026-10-31T12:10:00Z",
	   "labels":{"alertname":"TimelineClientThrottled","audience":"timeline","tenant":"Not A Tenant"}}
	]}`
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/alertmanager", strings.NewReader(body)))
	if response.Code != http.StatusOK {
		t.Fatalf("the handler answered %d", response.Code)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	recorder.Run(ctx)
	var got []string
	for _, e := range recorded.written {
		got = append(got, fmt.Sprintf("%s %s %s %s", e.ID, e.Time.Format(time.TimeOnly), e.Kind, e.ClientID))
	}
	want := []string{
		"throttled-f1-1793448000 12:00:00 client.throttled loader",
		"unthrottled-f1-1793448000 12:05:00 client.unthrottled loader",
		"throttled-f2-1793448600 12:10:00 client.throttled writer",
	}
	if !slices.Equal(got, want) {
		t.Errorf("recorded %v, want %v", got, want)
	}
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), method, "/alertmanager", http.NoBody))
		if response.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s answered %d, want 405", method, response.Code)
		}
	}
}
