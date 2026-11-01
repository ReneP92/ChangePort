package console

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/ReneP92/changeport/internal/console/api"
)

// fakeMessages has acme's orders topic, whose readers give their messages
// in their first poll.
type fakeMessages struct {
	mu sync.Mutex
	// messages are what a new reader gives.
	messages []Message
	// opened are the tails opened, as topic and start; open is how many
	// readers are open now.
	opened []string
	open   int
}

func (f *fakeMessages) Open(_ context.Context, customerID, topic string, start TailStart) (MessageReader, error) {
	if topic != customerID+"-orders" {
		return nil, ErrNoSuchTopic
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opened = append(f.opened, fmt.Sprintf("%s recent=%d resume=%v", topic, start.Recent, start.Resume))
	f.open++
	return &fakeReader{messages: f, pending: append([]Message(nil), f.messages...)}, nil
}

type fakeReader struct {
	messages *fakeMessages
	pending  []Message
}

func (r *fakeReader) Poll(ctx context.Context) ([]Message, error) {
	if len(r.pending) == 0 {
		<-ctx.Done()
		return nil, nil
	}
	out := r.pending
	r.pending = nil
	return out, nil
}

func (r *fakeReader) Close() {
	r.messages.mu.Lock()
	defer r.messages.mu.Unlock()
	r.messages.open--
}

// openTail opens a tail as token and returns its events, as event: data lines,
// once the stream ends.
func openTail(t *testing.T, server *httptest.Server, path, token string) (int, []string) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+path, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return response.StatusCode, nil
	}
	var events []string
	var event string
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			events = append(events, event+": "+strings.TrimPrefix(line, "data: "))
		}
	}
	return response.StatusCode, events
}

func TestOnlyAdminsTailTopics(t *testing.T) {
	f := newFixture(t)
	server := httptest.NewServer(f.handler)
	defer server.Close()
	for name, token := range map[string]string{
		"a member": f.signer.token(t, session{user: "user_2", org: "org_acme", role: "member"}),
		"staff":    f.signer.token(t, session{user: "user_6", org: staffOrg, role: "admin"}),
	} {
		if status, body, _ := get(t, f.handler, "/api/customer/topics/acme-orders/tail", token); status != http.StatusForbidden {
			t.Errorf("%s tailing a topic = %d %s, want 403", name, status, body)
		}
	}
	admin := f.signer.token(t, session{user: "user_3", org: "org_acme", role: "admin"})
	for _, c := range []struct {
		path   string
		status int
	}{
		{"/api/customer/topics/acme-payments/tail", http.StatusNotFound},
		// The platform's own topics are not the customer's to read.
		{"/api/customer/topics/__consumer_offsets/tail", http.StatusNotFound},
		{"/api/customer/topics/changeport.connect.offsets/tail", http.StatusNotFound},
		{"/api/customer/topics/acme-orders/tail?resume=0-12", http.StatusBadRequest},
	} {
		if status, body, _ := get(t, f.handler, c.path, admin); status != c.status {
			t.Errorf("GET %s = %d %s, want %d", c.path, status, body, c.status)
		}
	}
	if len(f.messages.opened) != 0 {
		t.Errorf("refused tails opened %v", f.messages.opened)
	}
}

func TestATailShowsMessagesWithinItsRateAndIsAudited(t *testing.T) {
	f := newFixture(t)
	for i := range 70 {
		f.messages.messages = append(f.messages.messages, Message{
			Partition: int32(i % 2), Offset: int64(i), Timestamp: time.Date(2026, 11, 1, 12, 0, i, 0, time.UTC),
			Key: []byte(fmt.Sprintf("order-%d", i)), Value: []byte(fmt.Sprintf(`{"id":%d,"secret":"s3cr3t"}`, i)),
			Headers: []kgo.RecordHeader{{Key: "trace", Value: []byte{0xff, 0xfe}}},
		})
	}
	server := httptest.NewServer(f.handler)
	defer server.Close()
	admin := f.signer.token(t, session{user: "user_3", org: "org_acme", role: "admin", expires: time.Now().Add(2 * time.Second)})
	status, events := openTail(t, server, "/api/customer/topics/acme-orders/tail?from=latest&resume=0:12,1:7", admin)
	if status != http.StatusOK {
		t.Fatalf("tailing = %d", status)
	}
	var shown []api.TailMessage
	var skipped []string
	for _, e := range events {
		kind, data, _ := strings.Cut(e, ": ")
		switch kind {
		case "message":
			var m api.TailMessage
			if err := json.Unmarshal([]byte(data), &m); err != nil {
				t.Fatal(err)
			}
			shown = append(shown, m)
		case "skipped":
			skipped = append(skipped, data)
		}
	}
	// The recent messages it starts with at most; the rest are counted.
	if len(shown) != tailRecent || len(skipped) != 1 || skipped[0] != `{"skipped":20}` {
		t.Fatalf("showed %d messages and skipped %v, want %d and 20", len(shown), skipped, tailRecent)
	}
	first := shown[0]
	if first.Key == nil || first.Key.Format != api.Text || first.Key.Text != "order-0" ||
		first.Value == nil || first.Value.Format != api.Json || first.Value.Text != `{"id":0,"secret":"s3cr3t"}` ||
		len(first.Headers) != 1 || first.Headers[0].Value.Format != api.Binary || first.Headers[0].Value.Text != "fffe" {
		encoded, _ := json.Marshal(first)
		t.Errorf("the first message is %s", encoded)
	}
	if want := "acme-orders recent=0 resume=map[0:12 1:7]"; len(f.messages.opened) != 1 || f.messages.opened[0] != want {
		t.Errorf("opened %v, want %s", f.messages.opened, want)
	}
	logged := f.log.String()
	for _, want := range []string{
		`"msg":"topic tail started","audit":true,"tenant":"acme","user":"user_3","organization":"org_acme","topic":"acme-orders","recent":0,"resumed":true`,
		`"msg":"topic tail ended","audit":true,"tenant":"acme","user":"user_3","organization":"org_acme","topic":"acme-orders","shown":50,"skipped":20`,
	} {
		if !strings.Contains(logged, want) {
			t.Errorf("the log lacks %s:\n%s", want, logged)
		}
	}
	if strings.Contains(logged, "s3cr3t") || strings.Contains(logged, "order-") {
		t.Errorf("the log holds a message: %s", logged)
	}
	if f.messages.open != 0 {
		t.Errorf("%d readers are still open after the tail ended", f.messages.open)
	}
}

func TestACustomerRunsAFewTailsAtOnce(t *testing.T) {
	f := newFixture(t)
	server := httptest.NewServer(f.handler)
	defer server.Close()
	admin := f.signer.token(t, session{user: "user_3", org: "org_acme", role: "admin", expires: time.Now().Add(3 * time.Second)})
	var wg sync.WaitGroup
	for range tailsPerCustomer {
		wg.Go(func() { openTail(t, server, "/api/customer/topics/acme-orders/tail", admin) })
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		f.messages.mu.Lock()
		open := f.messages.open
		f.messages.mu.Unlock()
		if open == tailsPerCustomer || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if status, body, _ := get(t, f.handler, "/api/customer/topics/acme-orders/tail", admin); status != http.StatusTooManyRequests {
		t.Errorf("one tail too many = %d %s, want 429", status, body)
	}
	// Another customer's tails are their own.
	globex := f.signer.token(t, session{user: "user_7", org: "org_globex", role: "admin", expires: time.Now().Add(time.Second)})
	if status, _ := openTail(t, server, "/api/customer/topics/globex-orders/tail", globex); status != http.StatusOK {
		t.Errorf("globex's tail = %d, want 200", status)
	}
	wg.Wait()
	if status, _ := openTail(t, server, "/api/customer/topics/acme-orders/tail", f.signer.token(t,
		session{user: "user_3", org: "org_acme", role: "admin", expires: time.Now().Add(time.Second)})); status != http.StatusOK {
		t.Errorf("a tail once the others ended = %d, want 200", status)
	}
}
