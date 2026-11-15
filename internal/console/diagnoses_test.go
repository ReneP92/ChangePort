package console

import (
	"strings"
	"testing"
	"time"

	"k8s.io/utils/ptr"

	"github.com/ReneP92/changeport/internal/diagnosis"
	"github.com/ReneP92/changeport/internal/metrics"
)

// A snapshot joins Kafka's description of the cluster with its metrics, and
// the rules find in it what is wrong: here, a consumer group that can't keep
// up, and one that lost messages to retention.
func TestDiagnosesFromKafkaAndMetrics(t *testing.T) {
	day := ptr.To(int64(24 * 3600 * 1000))
	settings := []TopicSettings{
		{Name: "orders", Partitions: 6, RetentionMs: day, CleanupPolicy: "delete"},
		{Name: "payments", Partitions: 3, RetentionMs: day, CleanupPolicy: "delete"},
		{Name: "uploads", Partitions: 3, RetentionMs: day, CleanupPolicy: "delete", MaxMessageBytes: 1048588},
		{Name: "__consumer_offsets", Partitions: 50, CleanupPolicy: "compact"},
	}
	groups := []ConsumerGroup{
		{Name: "billing", State: "Stable", Members: []GroupMember{
			{ClientID: "billing", Assigned: map[string]int{"orders": 2}},
			{ClientID: "billing", Assigned: map[string]int{"orders": 2}},
			{ClientID: "billing", Assigned: map[string]int{"orders": 2}},
			// A fourth member reads nothing: it is idle.
			{ClientID: "billing", Assigned: map[string]int{}},
		}},
		{Name: "audit", State: "Stable", Members: []GroupMember{{ClientID: "audit", Assigned: map[string]int{"payments": 3}}}},
		// The platform's own group is left out.
		{Name: "changeport-registry-x", State: "Stable", Members: []GroupMember{{ClientID: "apicurio"}}},
	}
	group := func(v float64, group, topic string) metrics.Series {
		return series(v, "consumer_group", group, "topic", topic)
	}
	values := map[metrics.Query][]metrics.Series{
		metrics.MessagesInRecent:  {series(350, "topic", "orders"), series(10, "topic", "payments")},
		metrics.BytesIn:           {series(350_000, "topic", "orders"), series(10_000, "topic", "payments")},
		metrics.ProducerBytes:     {series(350_000, "topic", "orders", "principal", "User:admin", "client_id", "checkout")},
		metrics.LagSeconds:        {group(720, "billing", "orders"), group(60, "audit", "payments"), group(5, "changeport-registry-x", "orders")},
		metrics.LagMessages:       {group(100_000, "billing", "orders"), group(600, "audit", "payments")},
		metrics.LagSecondsEarlier: {group(240, "billing", "orders"), group(60, "audit", "payments")},
		metrics.ConsumedMessages:  {group(200, "billing", "orders"), group(10, "audit", "payments")},
		metrics.LostPartitions:    {group(0, "billing", "orders"), group(1, "audit", "payments")},
		// uploads refuses what the uploader sends it as too large.
		metrics.BytesRejected:       {series(2e6, "topic", "uploads")},
		metrics.ProducerSends:       {series(1, "principal", "User:admin", "client_id", "uploader")},
		metrics.ProducerErrors:      {series(1, "principal", "User:admin", "client_id", "uploader")},
		metrics.ProducerTopicErrors: {series(1, "topic", "uploads", "principal", "User:admin", "client_id", "uploader")},
	}
	snapshot := buildSnapshot(settings, groups, values, changes{rebalances: map[string]int{"billing": 2}})

	if len(snapshot.Topics) != 3 {
		t.Errorf("topics = %+v, want orders, payments and uploads only", snapshot.Topics)
	}
	var found []string
	for _, f := range diagnosis.Diagnose(snapshot) {
		found = append(found, f.ID)
	}
	// payments gets 10 KB/s from producers without telemetry, all of it.
	// The uploader's errors are uploads' refusals, not a diagnosis of their
	// own.
	want := "behind-retention:audit/payments consumer-too-slow:billing/orders oversized-messages:uploads idle-consumers:billing producers-without-telemetry:payments"
	if got := strings.Join(found, " "); got != want {
		t.Errorf("found %s\nwant %s", got, want)
	}
	for _, f := range diagnosis.Diagnose(snapshot) {
		if f.Rule == "oversized-messages" && !strings.Contains(f.Explanation, "in batches larger than 1.0 MB, the most it accepts (`max.message.bytes`). They came from uploader.") {
			t.Errorf("oversized messages = %q, want the topic's limit and the producer", f.Explanation)
		}
	}
	for _, g := range snapshot.Groups {
		if g.Name == "billing" && (g.Members != 4 || g.IdleMembers != 1 || g.Rebalances != 2 || g.Reads[0].AssignedMembers != 3) {
			t.Errorf("billing = %+v, want 4 members, 1 idle, 2 rebalances and 3 reading orders", g)
		}
	}
}

// Kafka's open transactions and the partitions' producers become the
// snapshot's transactions, idempotent producers and hanging transactions.
func TestJoinTransactions(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	snapshot := diagnosis.Snapshot{Window: 15 * time.Minute, Topics: []diagnosis.Topic{{Name: "orders"}, {Name: "payments"}, {Name: "quiet"}}}
	transactions := []Transaction{{
		ID: "checkout-1", ProducerID: 7, State: "Ongoing", Started: now.Add(-3 * time.Minute), Timeout: 10 * time.Minute,
		Partitions: []TransactionPartition{{Topic: "orders", Partition: 0, HeldBack: 12}},
	}}
	producers := []PartitionProducer{
		// Open, and its coordinator has it open: not hanging.
		{Topic: "orders", Partition: 0, ProducerID: 7, TransactionalID: "checkout-1", LastWrite: now.Add(-20 * time.Minute), OpenFrom: 100},
		// Open, idle past any timeout, unknown to a coordinator: hanging.
		{Topic: "orders", Partition: 1, ProducerID: 8, LastWrite: now.Add(-40 * time.Minute), OpenFrom: 55},
		// Its coordinator has the transaction, but not on this partition.
		{Topic: "orders", Partition: 2, ProducerID: 7, TransactionalID: "checkout-1", LastWrite: now.Add(-30 * time.Minute), OpenFrom: 9},
		// Recent idempotent writers of payments; none of quiet.
		{Topic: "payments", Partition: 0, ProducerID: 9, LastWrite: now.Add(-time.Minute), OpenFrom: -1},
		{Topic: "payments", Partition: 1, ProducerID: 9, LastWrite: now.Add(-time.Minute), OpenFrom: -1},
		{Topic: "payments", Partition: 1, ProducerID: 10, LastWrite: now.Add(-2 * time.Minute), OpenFrom: -1},
		{Topic: "quiet", Partition: 0, ProducerID: 11, LastWrite: now.Add(-2 * time.Hour), OpenFrom: -1},
	}
	joinTransactions(&snapshot, transactions, producers, true, now)

	if len(snapshot.Transactions) != 1 || snapshot.Transactions[0].Open != 3*time.Minute || snapshot.Transactions[0].Partitions[0].HeldBack != 12 {
		t.Errorf("transactions = %+v", snapshot.Transactions)
	}
	orders, payments, quiet := snapshot.Topics[0], snapshot.Topics[1], snapshot.Topics[2]
	if len(orders.Hanging) != 2 || orders.Hanging[0].Partition != 1 || orders.Hanging[0].Idle != 40*time.Minute || orders.Hanging[1].Partition != 2 {
		t.Errorf("hanging on orders = %+v, want partitions 1 and 2", orders.Hanging)
	}
	if *payments.IdempotentProducers != 2 || *quiet.IdempotentProducers != 0 || *orders.IdempotentProducers != 0 {
		t.Errorf("idempotent producers: orders %d, payments %d, quiet %d, want 0, 2 and 0",
			*orders.IdempotentProducers, *payments.IdempotentProducers, *quiet.IdempotentProducers)
	}

	// Without the producers, nothing is said of them.
	unknown := diagnosis.Snapshot{Topics: []diagnosis.Topic{{Name: "orders"}}}
	joinTransactions(&unknown, nil, nil, false, now)
	if unknown.Topics[0].IdempotentProducers != nil {
		t.Errorf("idempotent producers = %v, want unknown", *unknown.Topics[0].IdempotentProducers)
	}
}
