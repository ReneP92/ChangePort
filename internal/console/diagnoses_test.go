package console

import (
	"strings"
	"testing"

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
