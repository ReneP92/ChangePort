package console

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestTopicProducers(t *testing.T) {
	f := newFixture(t)
	member := f.signer.token(t, session{user: "user_2", org: "org_acme", role: "member"})
	now := time.Now().Truncate(time.Second)
	f.clusters.producers = map[string][]PartitionProducer{"acme": {
		{Topic: "acme-orders", Partition: 0, ProducerID: 7, TransactionalID: "checkout-1", LastWrite: now.Add(-time.Minute), OpenFrom: 100},
		{Topic: "acme-orders", Partition: 1, ProducerID: 7, TransactionalID: "checkout-1", LastWrite: now.Add(-3 * time.Minute), OpenFrom: -1},
		{Topic: "acme-orders", Partition: 1, ProducerID: 9, LastWrite: now.Add(-10 * time.Minute), OpenFrom: -1},
		{Topic: "acme-payments", Partition: 0, ProducerID: 11, LastWrite: now, OpenFrom: -1},
	}}
	f.clusters.transactions = map[string][]Transaction{"acme": {
		{ID: "checkout-1", ProducerID: 7, State: "Ongoing", Started: now.Add(-2 * time.Minute), Partitions: []TransactionPartition{
			{Topic: "acme-orders", Partition: 0, HeldBack: 12}, {Topic: "acme-payments", Partition: 0, HeldBack: 3},
		}},
		// A second transaction on the same partition holds back the same messages.
		{ID: "checkout-2", ProducerID: 8, State: "Ongoing", Started: now.Add(-time.Minute), Partitions: []TransactionPartition{
			{Topic: "acme-orders", Partition: 0, HeldBack: 12},
		}},
	}}
	status, body := send(t, f.handler, http.MethodGet, "/api/customer/topics/acme-orders/producers", member, "")
	want := `{"heldBack":12,"producers":[` +
		`{"inTransaction":true,"lastWrite":"` + now.Add(-time.Minute).UTC().Format(time.RFC3339) + `","partitions":2,"producerId":7,"transactionalId":"checkout-1"},` +
		`{"inTransaction":false,"lastWrite":"` + now.Add(-10*time.Minute).UTC().Format(time.RFC3339) + `","partitions":1,"producerId":9}]}`
	if status != 200 || strings.TrimSpace(body) != want {
		t.Errorf("GET the producers of acme-orders = %d %s\nwant %s", status, body, want)
	}
	if status, body := send(t, f.handler, http.MethodGet, "/api/customer/topics/__consumer_offsets/producers", member, ""); status != 404 {
		t.Errorf("GET the producers of an internal topic = %d %s, want 404", status, body)
	}

	_, body, _ = get(t, f.handler, "/api/customer/health", member)
	if want := `"transactions":{"oldestStartedAt":"` + now.Add(-2*time.Minute).UTC().Format(time.RFC3339) + `","open":2}`; !strings.Contains(body, want) {
		t.Errorf("GET the health = %s, want %s", body, want)
	}
}
