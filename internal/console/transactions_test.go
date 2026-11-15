package console

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ReneP92/changeport/internal/console/api"
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
	want := `{"heldBack":12,"heldBackSince":"` + now.Add(-2*time.Minute).UTC().Format(time.RFC3339) + `","idempotent":1,"producers":[` +
		`{"inTransaction":true,"lastWrite":"` + now.Add(-time.Minute).UTC().Format(time.RFC3339) + `","partitions":2,"producerId":7,"transactionalId":"checkout-1"},` +
		`{"inTransaction":false,"lastWrite":"` + now.Add(-10*time.Minute).UTC().Format(time.RFC3339) + `","partitions":1,"producerId":9}]}`
	if status != 200 || strings.TrimSpace(body) != want {
		t.Errorf("GET the producers of acme-orders = %d %s\nwant %s", status, body, want)
	}
	if status, body := send(t, f.handler, http.MethodGet, "/api/customer/topics/__consumer_offsets/producers", member, ""); status != 404 {
		t.Errorf("GET the producers of an internal topic = %d %s, want 404", status, body)
	}

	// Thousands of short-lived idempotent producers show as a count.
	for id := int64(100); id < 130; id++ {
		f.clusters.producers["acme"] = append(f.clusters.producers["acme"], PartitionProducer{Topic: "acme-orders", ProducerID: id, LastWrite: now, OpenFrom: -1})
	}
	_, body = send(t, f.handler, http.MethodGet, "/api/customer/topics/acme-orders/producers", member, "")
	list := decode[api.TopicProducers](t, body)
	if list.Idempotent != 31 || len(list.Producers) != 11 || list.Producers[0].ProducerId != 100 {
		t.Errorf("producers = %d listed of %d idempotent, want 10 of 31 and the transactional one", len(list.Producers), list.Idempotent)
	}

	// Without the transactions, the producers still show.
	f.clusters.transactionsFail = errors.New("no broker answers")
	if status, body := send(t, f.handler, http.MethodGet, "/api/customer/topics/acme-orders/producers", member, ""); status != 200 || strings.Contains(body, "heldBack") {
		t.Errorf("GET the producers without the transactions = %d %s, want them without heldBack", status, body)
	}
	if _, body, _ := get(t, f.handler, "/api/customer/health", member); strings.Contains(body, "transactions") {
		t.Errorf("GET the health without the transactions = %s, want them left out", body)
	}
	f.clusters.transactionsFail = nil

	_, body, _ = get(t, f.handler, "/api/customer/health", member)
	if want := `"transactions":{"oldestStartedAt":"` + now.Add(-2*time.Minute).UTC().Format(time.RFC3339) + `","open":2}`; !strings.Contains(body, want) {
		t.Errorf("GET the health = %s, want %s", body, want)
	}
}
