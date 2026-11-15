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

// Kafka's open transactions and what the console saw of the producers
// become the snapshot's transactions, idempotent producers and hanging
// transactions, by persistence rather than the producers' timestamps.
func TestJoinTransactions(t *testing.T) {
	start := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	memory := newProducerMemory()
	transactions := []Transaction{{
		ID: "checkout-1", ProducerID: 7, State: "Ongoing", Started: start.Add(-3 * time.Minute), Timeout: 10 * time.Minute,
		Partitions: []TransactionPartition{{Topic: "orders", Partition: 0, HeldBack: 12}},
	}}
	// Timestamps far in the past, as a replay's or a stream processor's.
	old := start.Add(-48 * time.Hour)
	producers := func(sequence int32) []PartitionProducer {
		return []PartitionProducer{
			// Open, and its coordinator has it open there.
			{Topic: "orders", Partition: 0, ProducerID: 7, TransactionalID: "checkout-1", LastSequence: sequence, LastWrite: old, OpenFrom: 100},
			// Open from the same offset all along, unknown to a coordinator.
			{Topic: "orders", Partition: 1, ProducerID: 8, LastSequence: 4, LastWrite: old, OpenFrom: 55},
			// Its coordinator has the transaction, but not on this partition.
			{Topic: "orders", Partition: 2, ProducerID: 7, TransactionalID: "checkout-1", LastSequence: 9, LastWrite: old, OpenFrom: 9},
			// payments' producers write; quiet's does not.
			{Topic: "payments", Partition: 0, ProducerID: 9, LastSequence: sequence, LastWrite: old, OpenFrom: -1},
			{Topic: "quiet", Partition: 0, ProducerID: 11, LastSequence: 3, LastWrite: old, OpenFrom: -1},
		}
	}
	join := func(now time.Time, sequence int32) diagnosis.Snapshot {
		snapshot := diagnosis.Snapshot{Window: 15 * time.Minute, Topics: []diagnosis.Topic{{Name: "orders"}, {Name: "payments"}, {Name: "quiet"}}}
		saw := memory.look("acme", producers(sequence), now)
		joinTransactions(&snapshot, transactions, producers(sequence), &saw, now)
		return snapshot
	}

	first := join(start, 10)
	if len(first.Transactions) != 1 || first.Transactions[0].Open != 3*time.Minute || first.Transactions[0].Partitions[0].HeldBack != 12 {
		t.Errorf("transactions = %+v", first.Transactions)
	}
	// Without a last look, nobody is known to write, nor to hang.
	if first.Topics[1].IdempotentProducers != nil || len(first.Topics[0].Hanging) != 0 {
		t.Errorf("first look = %+v, want writers unknown and nothing hanging", first.Topics)
	}

	// A minute on, payments' producer wrote; nothing has hung long enough.
	second := join(start.Add(time.Minute), 11)
	if *second.Topics[1].IdempotentProducers != 1 || *second.Topics[2].IdempotentProducers != 0 || len(second.Topics[0].Hanging) != 0 {
		t.Errorf("second look = %+v", second.Topics)
	}

	// Past any transaction's timeout, open from the same offsets: those
	// their coordinator does not have open hang, however old or new their
	// timestamps.
	third := join(start.Add(4*time.Minute), 12)
	if len(third.Topics[0].Hanging) != 0 {
		t.Errorf("hanging after 4 minutes = %+v, want none", third.Topics[0].Hanging)
	}
	for minute := 8; minute <= 17; minute += 3 {
		join(start.Add(time.Duration(minute)*time.Minute), int32(12+minute))
	}
	last := join(start.Add(17*time.Minute), 40)
	hanging := last.Topics[0].Hanging
	if len(hanging) != 2 || hanging[0].Partition != 1 || hanging[0].Open != 17*time.Minute || hanging[1].Partition != 2 {
		t.Errorf("hanging on orders = %+v, want partitions 1 and 2, open 17 minutes", hanging)
	}

	// Without the producers, nothing is said of them.
	unknown := diagnosis.Snapshot{Topics: []diagnosis.Topic{{Name: "orders"}}}
	joinTransactions(&unknown, transactions, nil, nil, start)
	if unknown.Topics[0].IdempotentProducers != nil || len(unknown.Transactions) != 1 {
		t.Errorf("without producers = %+v", unknown)
	}
}

// A transaction seen open again from another offset is another one; a long
// gap between looks says nothing of who wrote in between.
func TestProducerMemory(t *testing.T) {
	memory := newProducerMemory()
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	open := func(from int64) []PartitionProducer {
		return []PartitionProducer{{Topic: "orders", ProducerID: 7, LastSequence: int32(from), OpenFrom: from}}
	}
	memory.look("acme", open(10), now)
	saw := memory.look("acme", open(20), now.Add(time.Minute))
	if since := saw.openSince[openKey{"orders", 0, 7, 20}]; !since.Equal(now.Add(time.Minute)) {
		t.Errorf("a new transaction open since %v, want the second look", since)
	}
	if !saw.wrote[producerKey{"orders", 0, 7, 0}] {
		t.Error("a producer whose sequence moved on did not count as writing")
	}
	if saw := memory.look("acme", open(20), now.Add(time.Hour)); saw.wrote != nil {
		t.Errorf("after an hour without a look, wrote = %v, want unknown", saw.wrote)
	}
	if saw := memory.look("globex", open(20), now); saw.wrote != nil || len(saw.openSince) != 1 {
		t.Errorf("another customer's first look = %+v", saw)
	}
}
