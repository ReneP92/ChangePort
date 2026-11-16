package diagnosis

import (
	"slices"
	"strings"
	"testing"
	"time"

	"k8s.io/utils/ptr"
)

// week is a topic's default retention, in milliseconds.
var week = ptr.To(int64(7 * 24 * 3600 * 1000))

// orders receives 350 messages a second into 6 partitions, kept a week.
func orders() Topic {
	return Topic{Name: "orders", Partitions: 6, RetentionMs: week, MessagesInPerSecond: 350, BytesInPerSecond: 350_000}
}

// billing reads orders with 3 consumers, at the given pace and lag.
func billing(consumed, lag, earlier float64) Group {
	return Group{
		Name: "billing", State: "Stable", Members: 3,
		Reads: []Read{{
			Topic: "orders", LagSeconds: lag, LagMessages: 100_000, LagSecondsEarlier: &earlier,
			ConsumedPerSecond: consumed, AssignedMembers: 3,
		}},
	}
}

// only runs one rule, by name.
func only(t *testing.T, name string, s Snapshot) []Finding {
	t.Helper()
	var found []Finding
	for _, f := range Diagnose(s) {
		if f.Rule == name {
			found = append(found, f)
		}
	}
	return found
}

func TestConsumerTooSlow(t *testing.T) {
	tests := []struct {
		name       string
		snapshot   Snapshot
		suggestion string
	}{
		{
			"fewer consumers than partitions: add some",
			Snapshot{Window: 15 * time.Minute, Topics: []Topic{orders()}, Groups: []Group{billing(200, 720, 240)}},
			"Run more consumers in billing. It has 3 consumers for the 6 partitions of orders, so up to 3 more can share the work.",
		},
		{
			"a consumer per partition already: make them faster",
			Snapshot{Window: 15 * time.Minute, Topics: []Topic{orders()}, Groups: []Group{func() Group {
				g := billing(200, 720, 240)
				g.Reads[0].AssignedMembers = 6
				return g
			}()}},
			"billing already has a consumer for each of the 6 partitions of orders. Make each one faster, for example by processing messages in batches, or add partitions to orders so that more consumers can share it. Adding partitions sends keys to other partitions, so for a while messages with the same key may arrive out of order.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found := only(t, "consumer-too-slow", tt.snapshot)
			if len(found) != 1 {
				t.Fatalf("found %v, want one finding", found)
			}
			f := found[0]
			if f.ID != "consumer-too-slow:billing/orders" || f.Severity != Warning || f.Title != "billing can't keep up with orders" {
				t.Errorf("finding = %+v", f)
			}
			if want := "billing consumes 200 messages a second of orders, which receives 350, so it falls further behind: 12 minutes now, 4 minutes 15 minutes ago."; f.Explanation != want {
				t.Errorf("explanation = %q\nwant %q", f.Explanation, want)
			}
			if f.Suggestion != tt.suggestion {
				t.Errorf("suggestion = %q\nwant %q", f.Suggestion, tt.suggestion)
			}
		})
	}

	keepingUp := []struct {
		name     string
		snapshot Snapshot
	}{
		{"consumes as fast as the topic receives", Snapshot{Window: 15 * time.Minute, Topics: []Topic{orders()}, Groups: []Group{billing(345, 720, 240)}}},
		{"behind, but catching up", Snapshot{Window: 15 * time.Minute, Topics: []Topic{orders()}, Groups: []Group{billing(200, 240, 720)}}},
		{"less than a minute behind", Snapshot{Window: 15 * time.Minute, Topics: []Topic{orders()}, Groups: []Group{billing(200, 50, 10)}}},
		{"a topic too quiet to tell", Snapshot{Window: 15 * time.Minute, Topics: []Topic{func() Topic { o := orders(); o.MessagesInPerSecond = 0.5; return o }()}, Groups: []Group{billing(0.1, 720, 240)}}},
		{"no lag known a window ago", Snapshot{Window: 15 * time.Minute, Topics: []Topic{orders()}, Groups: []Group{func() Group {
			g := billing(200, 720, 240)
			g.Reads[0].LagSecondsEarlier = nil
			return g
		}()}}},
		{"sharing its partitions out", Snapshot{Window: 15 * time.Minute, Topics: []Topic{orders()}, Groups: []Group{func() Group {
			g := billing(200, 720, 240)
			g.State, g.Reads[0].AssignedMembers = "PreparingRebalance", 0
			return g
		}()}}},
		{"members that no longer read the topic", Snapshot{Window: 15 * time.Minute, Topics: []Topic{orders()}, Groups: []Group{func() Group {
			g := billing(0, 720, 240)
			g.Reads[0].AssignedMembers = 0
			return g
		}()}}},
		{"replaying the topic from the start", Snapshot{Window: 15 * time.Minute, Topics: []Topic{orders()}, Groups: []Group{func() Group {
			g := billing(0, 720, 240)
			g.Reads[0].OffsetsReset = true
			return g
		}()}}},
		{"nobody consuming, which is another diagnosis", Snapshot{Window: 15 * time.Minute, Topics: []Topic{orders()}, Groups: []Group{func() Group {
			g := billing(0, 720, 240)
			g.Members, g.State = 0, "Empty"
			return g
		}()}}},
	}
	for _, tt := range keepingUp {
		t.Run(tt.name, func(t *testing.T) {
			if found := only(t, "consumer-too-slow", tt.snapshot); len(found) > 0 {
				t.Errorf("found %+v, want nothing", found)
			}
		})
	}
}

func TestNobodyConsuming(t *testing.T) {
	g := billing(0, 720, 240)
	g.Members, g.State = 0, "Empty"
	found := only(t, "nobody-consuming", Snapshot{Window: 15 * time.Minute, Topics: []Topic{orders()}, Groups: []Group{g}})
	if len(found) != 1 {
		t.Fatalf("found %v, want one finding", found)
	}
	if want := "billing has no consumers running, while orders receives 350 messages a second: it is 12 minutes behind, and falls further behind."; found[0].Explanation != want {
		t.Errorf("explanation = %q\nwant %q", found[0].Explanation, want)
	}

	// Lag older than the agent looks back reads the same each time; the
	// group still has nobody consuming.
	capped := g
	capped.Reads = []Read{{Topic: "orders", LagSeconds: 82800, Capped: true, LagMessages: 1e8, LagSecondsEarlier: ptr.To(82800.0)}}
	if found := only(t, "nobody-consuming", Snapshot{Window: 15 * time.Minute, Topics: []Topic{orders()}, Groups: []Group{capped}}); len(found) != 1 {
		t.Errorf("found %+v for a group over 23 hours behind, want one finding", found)
	}

	// A group nobody runs whose topic is quiet is no news.
	quiet := orders()
	quiet.MessagesInPerSecond = 0
	if found := only(t, "nobody-consuming", Snapshot{Window: 15 * time.Minute, Topics: []Topic{quiet}, Groups: []Group{g}}); len(found) > 0 {
		t.Errorf("found %+v for a quiet topic, want nothing", found)
	}
}

// A group an admin moved, or created to replay, waits for its consumers:
// the lag is the point of the move, so it is a note rather than a warning.
func TestNobodyConsumingAfterAMove(t *testing.T) {
	g := billing(0, 720, 240)
	g.Members, g.State = 0, "Empty"
	g.Moved = &Move{By: "Ada Lovelace", Ago: 4 * time.Minute}
	found := only(t, "nobody-consuming", Snapshot{Window: 15 * time.Minute, Topics: []Topic{orders()}, Groups: []Group{g}})
	if len(found) != 1 || found[0].Severity != Info {
		t.Fatalf("found %v, want one note", found)
	}
	if want := "Ada Lovelace moved billing in the console 4 minutes ago. It has no consumers running yet, so it is 12 minutes behind on orders, which receives 350 messages a second."; found[0].Explanation != want {
		t.Errorf("explanation = %q\nwant %q", found[0].Explanation, want)
	}
	if want := "Start the consumers of billing, with `group.id=billing`. They read from where it was moved, then keep up with orders."; found[0].Suggestion != want {
		t.Errorf("suggestion = %q\nwant %q", found[0].Suggestion, want)
	}
	g.Moved = &Move{Ago: 30 * time.Second, Replay: true}
	found = only(t, "nobody-consuming", Snapshot{Window: 15 * time.Minute, Topics: []Topic{orders()}, Groups: []Group{g}})
	if want := "An admin created billing in the console less than a minute ago to replay messages."; len(found) != 1 || !strings.HasPrefix(found[0].Explanation, want) {
		t.Errorf("found %+v, want an explanation starting %q", found, want)
	}
}

func TestBehindRetention(t *testing.T) {
	day := ptr.To(int64(24 * 3600 * 1000))
	short := orders()
	short.RetentionMs = day

	behind := billing(200, 13*3600, 12*3600)
	found := only(t, "behind-retention", Snapshot{Window: 15 * time.Minute, Topics: []Topic{short}, Groups: []Group{behind}})
	if len(found) != 1 || found[0].Severity != Warning {
		t.Fatalf("found %v, want one warning", found)
	}
	if want := "billing is 13 hours behind on orders, which keeps messages for 24 hours. Unless it catches up, messages are deleted before it reads them."; found[0].Explanation != want {
		t.Errorf("explanation = %q\nwant %q", found[0].Explanation, want)
	}

	// Moved back by an admin, it says why it is behind.
	moved := behind
	moved.Moved = &Move{By: "Ada Lovelace", Ago: 10 * time.Minute}
	found = only(t, "behind-retention", Snapshot{Window: 15 * time.Minute, Topics: []Topic{short}, Groups: []Group{moved}})
	if want := "It is this far behind because Ada Lovelace moved billing in the console 10 minutes ago."; len(found) != 1 || !strings.HasSuffix(found[0].Explanation, want) {
		t.Errorf("found %+v, want an explanation ending %q", found, want)
	}

	lost := billing(200, 60, 30)
	lost.Reads[0].LostPartitions = 2
	found = only(t, "behind-retention", Snapshot{Window: 15 * time.Minute, Topics: []Topic{short}, Groups: []Group{lost}})
	if len(found) != 1 || found[0].Severity != Critical {
		t.Fatalf("found %v, want one critical finding", found)
	}
	if want := "orders keeps messages for 24 hours, and deleted messages billing had not read yet, on 2 of its 6 partitions."; found[0].Explanation != want {
		t.Errorf("explanation = %q\nwant %q", found[0].Explanation, want)
	}
	// On every partition, it says no more than that.
	lost.Reads[0].LostPartitions = 6
	found = only(t, "behind-retention", Snapshot{Window: 15 * time.Minute, Topics: []Topic{short}, Groups: []Group{lost}})
	if want := "orders keeps messages for 24 hours, and deleted messages billing had not read yet."; len(found) != 1 || found[0].Explanation != want {
		t.Errorf("found %+v, want %q", found, want)
	}

	// Compacted topics keep each key's latest message, however old.
	compacted := short
	compacted.Compacted = true
	if found := only(t, "behind-retention", Snapshot{Window: 15 * time.Minute, Topics: []Topic{compacted}, Groups: []Group{behind}}); len(found) > 0 {
		t.Errorf("found %+v for a compacted topic, want nothing", found)
	}
	// Far from a week's retention.
	if found := only(t, "behind-retention", Snapshot{Window: 15 * time.Minute, Topics: []Topic{orders()}, Groups: []Group{behind}}); len(found) > 0 {
		t.Errorf("found %+v, want nothing", found)
	}
}

func TestIdleConsumers(t *testing.T) {
	g := billing(350, 0, 0)
	g.Members, g.IdleMembers = 8, 2
	found := only(t, "idle-consumers", Snapshot{Window: 15 * time.Minute, Topics: []Topic{orders()}, Groups: []Group{g}})
	if len(found) != 1 || found[0].Severity != Info {
		t.Fatalf("found %v, want one note", found)
	}
	if want := "billing runs 8 consumers, but 2 of them read nothing: a partition is read by one consumer of a group at a time, and orders has 6 partitions."; found[0].Explanation != want {
		t.Errorf("explanation = %q\nwant %q", found[0].Explanation, want)
	}
	// Enough partitions, handed out topic by topic: another assignor.
	uneven := billing(350, 0, 0)
	uneven.Members, uneven.IdleMembers = 4, 1
	uneven.Reads = append(uneven.Reads, Read{Topic: "payments"})
	payments := Topic{Name: "payments", Partitions: 3}
	found = only(t, "idle-consumers", Snapshot{Window: 15 * time.Minute, Topics: []Topic{orders(), payments}, Groups: []Group{uneven}})
	if len(found) != 1 || !strings.Contains(found[0].Suggestion, "cooperative-sticky") {
		t.Errorf("found %+v, want a hint at the assignor", found)
	}

	// While a group rebalances, its members wait for partitions.
	g.State = "PreparingRebalance"
	if found := only(t, "idle-consumers", Snapshot{Window: 15 * time.Minute, Topics: []Topic{orders()}, Groups: []Group{g}}); len(found) > 0 {
		t.Errorf("found %+v during a rebalance, want nothing", found)
	}
}

func TestFrequentRebalances(t *testing.T) {
	g := billing(350, 0, 0)
	g.Rebalances = 9
	found := only(t, "frequent-rebalances", Snapshot{Window: 15 * time.Minute, Groups: []Group{g}})
	if len(found) != 1 || found[0].Explanation != "The members of billing changed 9 times in the last hour. Each time, its consumers share the partitions out again, and read less meanwhile." {
		t.Fatalf("found %+v", found)
	}
	g.Rebalances = 2
	if found := only(t, "frequent-rebalances", Snapshot{Window: 15 * time.Minute, Groups: []Group{g}}); len(found) > 0 {
		t.Errorf("found %+v, want nothing", found)
	}
}

func TestHotPartition(t *testing.T) {
	hot := orders()
	hot.Tiered = true
	hot.PartitionBytes = []float64{700e6, 60e6, 60e6, 60e6, 60e6, 60e6}
	found := only(t, "hot-partition", Snapshot{Window: 15 * time.Minute, Topics: []Topic{hot}})
	if len(found) != 1 {
		t.Fatalf("found %v, want one finding", found)
	}
	if want := "One of the 6 partitions of orders received 70% of its messages' bytes over the last day, where each would get 17% if keys were spread evenly. Messages with the same key always go to the same partition, so a few busy keys load one partition, and the one consumer reading it, more than the rest."; found[0].Explanation != want {
		t.Errorf("explanation = %q\nwant %q", found[0].Explanation, want)
	}

	even := hot
	even.PartitionBytes = []float64{100e6, 110e6, 90e6, 100e6, 105e6, 95e6}
	small := hot
	small.PartitionBytes = []float64{7e6, 1e5, 1e5}
	// Partitions added within the day start empty.
	grown := hot
	grown.PartitionsAdded = true
	// Topics that keep everything on the brokers show their whole history,
	// not how messages spread now.
	untiered := hot
	untiered.Tiered = false
	for _, topic := range []Topic{even, small, grown, untiered} {
		if found := only(t, "hot-partition", Snapshot{Window: 15 * time.Minute, Topics: []Topic{topic}}); len(found) > 0 {
			t.Errorf("found %+v for %v, want nothing", found, topic.PartitionBytes)
		}
	}
}

func TestProducerErrors(t *testing.T) {
	failing := Producer{Principal: "User:admin", ClientID: "checkout", SendsPerSecond: 100, ErrorsPerSecond: 3, RetriesPerSecond: 3}
	found := only(t, "producer-errors", Snapshot{Window: 15 * time.Minute, Producers: []Producer{failing}})
	if len(found) != 1 || found[0].Severity != Warning || found[0].ID != "producer-errors:User:admin/checkout" {
		t.Fatalf("found %+v, want one warning", found)
	}
	// Without topics to blame, the producer refused them itself.
	if want := "3% of the records checkout tried to send in the last 15 minutes failed in the producer itself, before reaching Kafka: 3 a second of 103."; found[0].Explanation != want {
		t.Errorf("explanation = %q\nwant %q", found[0].Explanation, want)
	}
	if !strings.Contains(found[0].Suggestion, "`max.request.size` (1 MB by default)") {
		t.Errorf("suggestion = %q, want the producer's size limit", found[0].Suggestion)
	}

	// They failed on their way to a topic that refused nothing for size,
	// such as one that refused a single message, before the window.
	refused := failing
	refused.TopicErrorsPerSecond = map[string]float64{"orders": 3}
	found = only(t, "producer-errors", Snapshot{Window: 15 * time.Minute, Producers: []Producer{refused}, Topics: []Topic{{Name: "orders"}}})
	if want := "3% of the records checkout tried to send in the last 15 minutes failed on their way to orders, or were refused there: 3 a second of 103."; len(found) != 1 || found[0].Explanation != want || found[0].Title != "Records from checkout fail to be delivered" {
		t.Fatalf("found %+v\nwant %q", found, want)
	}
	if !strings.Contains(found[0].Suggestion, "the topic's `max.message.bytes`") || !strings.Contains(found[0].Suggestion, "`delivery.timeout.ms`") {
		t.Errorf("suggestion = %q, want sizes and timeouts among the causes", found[0].Suggestion)
	}

	// Records a topic refused as too large are the oversized messages'.
	tooLarge := Snapshot{Window: 15 * time.Minute, Producers: []Producer{refused}, Topics: []Topic{{Name: "orders", BytesRejected: 3e6}}}
	if found := only(t, "producer-errors", tooLarge); len(found) > 0 {
		t.Errorf("found %+v, want the oversized messages to explain them", found)
	}

	// Records that fail before they are sent count as errors only: a
	// producer all of whose records fail is still found, at 100%.
	stuck := Producer{Principal: "User:admin", ClientID: "checkout", ErrorsPerSecond: 50}
	found = only(t, "producer-errors", Snapshot{Window: 15 * time.Minute, Producers: []Producer{stuck}})
	if len(found) != 1 || !strings.HasPrefix(found[0].Explanation, "100% of the records") {
		t.Errorf("found %+v, want 100%% failing", found)
	}

	// Two applications with the default client ID, under different users.
	twins := []Producer{
		{Principal: "User:a", ClientID: "producer-1", SendsPerSecond: 10, ErrorsPerSecond: 1},
		{Principal: "User:b", ClientID: "producer-1", SendsPerSecond: 10, ErrorsPerSecond: 1},
	}
	found = only(t, "producer-errors", Snapshot{Window: 15 * time.Minute, Producers: twins})
	if len(found) != 2 || found[0].ID == found[1].ID {
		t.Errorf("found %+v, want two findings with their own IDs", found)
	}

	retrying := Producer{ClientID: "checkout", SendsPerSecond: 100, RetriesPerSecond: 20}
	found = only(t, "producer-errors", Snapshot{Window: 15 * time.Minute, Producers: []Producer{retrying}})
	if len(found) != 1 || found[0].Severity != Info {
		t.Fatalf("found %+v, want one note", found)
	}

	for _, p := range []Producer{
		{ClientID: "fine", SendsPerSecond: 100, ErrorsPerSecond: 0.1, RetriesPerSecond: 1},
		{ClientID: "quiet", SendsPerSecond: 0.04, ErrorsPerSecond: 0.04},
	} {
		if found := only(t, "producer-errors", Snapshot{Window: 15 * time.Minute, Producers: []Producer{p}}); len(found) > 0 {
			t.Errorf("found %+v for %s, want nothing", found, p.ClientID)
		}
	}
}

func TestOversizedMessages(t *testing.T) {
	s := Snapshot{
		Window: 15 * time.Minute,
		Topics: []Topic{
			{Name: "uploads", MaxMessageBytes: 1048588, BytesRejected: 43.5e6},
			{Name: "orders"},
		},
		Producers: []Producer{
			{ClientID: "uploader", SendsPerSecond: 1, ErrorsPerSecond: 1, TopicErrorsPerSecond: map[string]float64{"uploads": 1}},
			{ClientID: "checkout", SendsPerSecond: 10, TopicErrorsPerSecond: map[string]float64{"orders": 0}},
		},
	}
	found := only(t, "oversized-messages", s)
	if len(found) != 1 || found[0].Severity != Warning || found[0].ID != "oversized-messages:uploads" {
		t.Fatalf("found %+v, want one warning about uploads", found)
	}
	if want := "Over the last 15 minutes, uploads refused 43.5 MB sent in batches larger than 1.0 MB, the most it accepts (`max.message.bytes`). They came from uploader. A message that large on its own is not kept: unless its producer handles the error, it is lost. Java producers split batches of smaller messages and send them again."; found[0].Explanation != want {
		t.Errorf("explanation = %q\nwant %q", found[0].Explanation, want)
	}
	for _, want := range []string{"object storage", "`compression.type=zstd`", "`max.request.size`"} {
		if !strings.Contains(found[0].Suggestion, want) {
			t.Errorf("suggestion = %q, want it to mention %s", found[0].Suggestion, want)
		}
	}

	// Producers without telemetry go unnamed, and an unknown limit unsaid.
	s.Producers, s.Topics[0].MaxMessageBytes = nil, 0
	found = only(t, "oversized-messages", s)
	if len(found) != 1 || !strings.Contains(found[0].Explanation, "larger than the most it accepts (`max.message.bytes`). A message") {
		t.Errorf("found %+v, want no producers named", found)
	}
}

func TestPartitionsAdded(t *testing.T) {
	s := Snapshot{
		Topics: []Topic{
			{Name: "orders", Partitions: 6, PartitionsAdded: true},
			{Name: "unread", Partitions: 6, PartitionsAdded: true},
			{Name: "payments", Partitions: 3},
		},
		Groups: []Group{{Name: "billing", Reads: []Read{{Topic: "orders"}, {Topic: "payments"}}}},
	}
	found := only(t, "partitions-added", s)
	if len(found) != 1 || found[0].Severity != Info || found[0].Subject.Topic != "orders" {
		t.Fatalf("found %+v, want a note about orders only", found)
	}
	if !strings.Contains(found[0].Explanation, "has 6 partitions now") || !strings.Contains(found[0].Suggestion, "`auto.offset.reset=latest`") {
		t.Errorf("found %+v", found[0])
	}
}

func TestOpenTransaction(t *testing.T) {
	s := Snapshot{
		Topics: []Topic{{Name: "orders", Partitions: 6}, {Name: "payments", Partitions: 1}},
		Transactions: []Transaction{
			{ID: "checkout-1", State: "Ongoing", Open: 4 * time.Minute, Timeout: 10 * time.Minute, Partitions: []TransactionPartition{
				{Topic: "orders", Partition: 0, HeldBack: 80},
				{Topic: "orders", Partition: 3, HeldBack: 40},
				{Topic: "payments", Partition: 0, HeldBack: 5},
				{Topic: "audit", Partition: 0},
			}},
			// A younger one on the same partition: its offsets are the
			// oldest's, which holds the last stable offset.
			{ID: "checkout-2", State: "Ongoing", Open: 2 * time.Minute, Partitions: []TransactionPartition{{Topic: "orders", Partition: 0, HeldBack: 80}}},
			// Young, or holding nothing back: fine.
			{ID: "fresh", State: "Ongoing", Open: 20 * time.Second, Partitions: []TransactionPartition{{Topic: "orders", Partition: 5, HeldBack: 9}}},
			{ID: "idle", State: "Ongoing", Open: 5 * time.Minute, Partitions: []TransactionPartition{{Topic: "orders", Partition: 4}}},
			// Stuck committing, as when a partition cannot take its markers.
			{ID: "stuck", State: "PrepareCommit", Open: 5 * time.Minute, Partitions: []TransactionPartition{{Topic: "orders", Partition: 1, HeldBack: 7}}},
		},
	}
	found := only(t, "open-transaction", s)
	var ids []string
	for _, f := range found {
		ids = append(ids, f.ID)
	}
	if want := []string{"open-transaction:orders/checkout-1", "open-transaction:orders/stuck", "open-transaction:payments/checkout-1"}; !slices.Equal(ids, want) {
		t.Fatalf("found %v, want %v", ids, want)
	}
	if want := "Transaction checkout-1 has been open for 4 minutes. Until it finishes, read_committed consumers cannot read past its start on 2 partitions of the 6 partitions of orders: about 120 messages wait behind it. Kafka aborts it once it has been open for 10 minutes."; found[0].Explanation != want {
		t.Errorf("explanation = %q\nwant %q", found[0].Explanation, want)
	}
	if !strings.Contains(found[2].Explanation, "past its start on payments: about 5 messages") {
		t.Errorf("explanation = %q, want the whole of payments", found[2].Explanation)
	}
	if !strings.Contains(found[1].Explanation, "has been committing for 5 minutes") || !strings.Contains(found[1].Suggestion, "has a leader") {
		t.Errorf("stuck = %+v, want it committing, and what markers wait for", found[1])
	}
	if !strings.Contains(found[0].Suggestion, "40 seconds in franz-go") {
		t.Errorf("suggestion = %q, want each client's default timeout", found[0].Suggestion)
	}
	// A transaction holding a topic back is the topic's concern.
	topics, _ := Levels(found)
	if topics["orders"].Level != NeedsAttention {
		t.Errorf("orders = %+v, want it to need attention", topics["orders"])
	}
}

func TestHangingTransaction(t *testing.T) {
	s := Snapshot{Topics: []Topic{
		{Name: "orders", Hanging: []HangingTransaction{
			{Partition: 2, ProducerID: 4001, From: 1200, Open: 40 * time.Minute},
			{Partition: 5, ProducerID: 4002, TransactionalID: "checkout-1", From: 9, Open: 2 * time.Hour},
		}},
		{Name: "payments", Hanging: []HangingTransaction{{Partition: 0, ProducerID: 4003, From: 3, Open: 16 * time.Minute}}},
	}}
	found := only(t, "hanging-transaction", s)
	if len(found) != 2 || found[0].Severity != Critical || found[0].Title != "Hanging transactions block 2 partitions of orders" ||
		found[1].Title != "A hanging transaction blocks partition 0 of payments" {
		t.Fatalf("found %+v, want one critical finding per topic", found)
	}
	if want := "On orders, partition 2 has had a transaction of a producer with ID 4001 open from offset 1200 for at least 40 minutes; partition 5 has had a transaction of transactional producer checkout-1 open from offset 9 for at least 2 hours: longer than any transaction may last (15 minutes), and its coordinator no longer has it open."; !strings.HasPrefix(found[0].Explanation, want) {
		t.Errorf("explanation = %q\nwant it to start %q", found[0].Explanation, want)
	}
	if !strings.Contains(found[0].Suggestion, "orders, partition 2 from offset 1200 and partition 5 from offset 9.") {
		t.Errorf("suggestion = %q, want what support needs", found[0].Suggestion)
	}
}

// Every finding has an ID of its own, which the console keys its list on.
func TestDiagnoseGivesEachFindingItsOwnID(t *testing.T) {
	s := Snapshot{
		Window: 15 * time.Minute,
		Topics: []Topic{{Name: "orders", Partitions: 6, Hanging: []HangingTransaction{{Partition: 1, ProducerID: 1, From: 1}, {Partition: 2, ProducerID: 2, From: 2}}}},
		Transactions: []Transaction{
			{ID: "a", State: "Ongoing", Open: time.Hour, Partitions: []TransactionPartition{{Topic: "orders", Partition: 0, HeldBack: 1}}},
			{ID: "b", State: "Ongoing", Open: 2 * time.Hour, Partitions: []TransactionPartition{{Topic: "orders", Partition: 3, HeldBack: 1}}},
		},
	}
	seen := map[string]bool{}
	for _, f := range Diagnose(s) {
		if seen[f.ID] {
			t.Errorf("two findings share ID %s", f.ID)
		}
		seen[f.ID] = true
	}
}

func TestTransactionsAborted(t *testing.T) {
	s := Snapshot{Window: 15 * time.Minute, Producers: []Producer{
		{Principal: "User:admin", ClientID: "checkout", AbortedTransactions: true},
		{Principal: "User:admin", ClientID: "billing"},
	}}
	found := only(t, "transactions-aborted", s)
	if len(found) != 1 || found[0].Severity != Info || found[0].ID != "transactions-aborted:User:admin/checkout" {
		t.Fatalf("found %+v, want a note about checkout", found)
	}
	if !strings.Contains(found[0].Explanation, "Kafka counts aborts nowhere, so how many is not known.") {
		t.Errorf("explanation = %q, want it honest about the count", found[0].Explanation)
	}
}

func TestProducersNotIdempotent(t *testing.T) {
	none, some := 0, 2
	s := Snapshot{Window: 15 * time.Minute, Topics: []Topic{
		{Name: "orders", MessagesInPerSecond: 40, IdempotentProducers: &none},
		{Name: "payments", MessagesInPerSecond: 40, IdempotentProducers: &some},
		{Name: "quiet", MessagesInPerSecond: 0.2, IdempotentProducers: &none},
		{Name: "unknown", MessagesInPerSecond: 40},
		{Name: "shop.public.orders", MessagesInPerSecond: 40, IdempotentProducers: &none, Source: true},
	}}
	found := only(t, "producers-not-idempotent", s)
	if len(found) != 1 || found[0].Subject.Topic != "orders" || found[0].Severity != Info {
		t.Fatalf("found %+v, want a note about orders only", found)
	}
	if !strings.Contains(found[0].Suggestion, "`enable.idempotence=true` and `acks=all`") {
		t.Errorf("suggestion = %q", found[0].Suggestion)
	}
}

func TestProducersWithoutTelemetry(t *testing.T) {
	unseen := orders()
	unseen.UnreportedBytesInPerSecond = 40_000
	found := only(t, "producers-without-telemetry", Snapshot{Window: 15 * time.Minute, Topics: []Topic{unseen}})
	if len(found) != 1 || found[0].Explanation != "orders receives 40.0 KB a second from producers that send no telemetry, so the data flow map cannot show them, nor their errors." {
		t.Fatalf("found %+v", found)
	}
	// A Postgres source's topics are written by Changeport's own Connect
	// worker.
	source := unseen
	source.Source = true
	if found := only(t, "producers-without-telemetry", Snapshot{Window: 15 * time.Minute, Topics: []Topic{source}}); len(found) > 0 {
		t.Errorf("found %+v for a source's topic, want nothing", found)
	}
	// A sliver of a busy topic's traffic is no news.
	unseen.UnreportedBytesInPerSecond = 10_000
	if found := only(t, "producers-without-telemetry", Snapshot{Window: 15 * time.Minute, Topics: []Topic{unseen}}); len(found) > 0 {
		t.Errorf("found %+v, want nothing", found)
	}
}

// The most severe findings come first, and each rule's findings carry its
// name.
func TestDiagnoseOrdersBySeverity(t *testing.T) {
	short := orders()
	short.RetentionMs = ptr.To(int64(24 * 3600 * 1000))
	short.UnreportedBytesInPerSecond = 300_000
	lost := billing(200, 720, 240)
	lost.Reads[0].LostPartitions = 1
	found := Diagnose(Snapshot{Window: 15 * time.Minute, Topics: []Topic{short}, Groups: []Group{lost}})
	var severities []Severity
	for _, f := range found {
		severities = append(severities, f.Severity)
	}
	if !slices.Equal(severities, []Severity{Critical, Warning, Info}) {
		t.Errorf("severities = %v, want critical, warning, info", severities)
	}
	if found := Diagnose(Snapshot{Window: 15 * time.Minute}); found == nil || len(found) != 0 {
		t.Errorf("Diagnose(empty) = %#v, want an empty list", found)
	}
}

func TestRate(t *testing.T) {
	for perSecond, want := range map[float64]string{
		0.004: "0", 0.25: "0.25", 2: "2", 2.5: "2.5", 350: "350", 1250.4: "1,250", 1234567: "1,234,567",
	} {
		if got := rate(perSecond); got != want {
			t.Errorf("rate(%v) = %q, want %q", perSecond, got, want)
		}
	}
}

func TestDuration(t *testing.T) {
	for seconds, want := range map[float64]string{
		1: "1 second", 45: "45 seconds", 60: "1 minute", 720: "12 minutes", 3600: "1 hour",
		4800: "1 hour 20 minutes", 13 * 3600: "13 hours", 36 * 3600: "36 hours", 7 * 86400: "7 days",
	} {
		if got := duration(seconds); got != want {
			t.Errorf("duration(%v) = %q, want %q", seconds, got, want)
		}
	}
}

// A level comes from the findings about a subject: warnings need attention,
// anything critical is at risk, and notes leave it healthy. A finding about
// a group reading a topic is the group's.
func TestLevels(t *testing.T) {
	findings := []Finding{
		{ID: "a", Severity: Warning, Subject: Subject{ConsumerGroup: "billing", Topic: "orders"}},
		{ID: "b", Severity: Critical, Subject: Subject{ConsumerGroup: "billing", Topic: "payments"}},
		{ID: "c", Severity: Warning, Subject: Subject{Topic: "orders"}},
		{ID: "d", Severity: Info, Subject: Subject{Topic: "events"}},
		{ID: "e", Severity: Info, Subject: Subject{ConsumerGroup: "audit"}},
		{ID: "f", Severity: Warning, Subject: Subject{ClientID: "checkout"}},
	}
	topics, groups := Levels(findings)
	if h := groups["billing"]; h.Level != AtRisk || !slices.Equal(h.Findings, []string{"a", "b"}) {
		t.Errorf("billing = %+v, want at risk from a and b", h)
	}
	if h := topics["orders"]; h.Level != NeedsAttention || !slices.Equal(h.Findings, []string{"c"}) {
		t.Errorf("orders = %+v, want needing attention from c alone", h)
	}
	if _, ok := topics["events"]; ok {
		t.Errorf("a note made events = %+v, want it healthy", topics["events"])
	}
	if _, ok := groups["audit"]; ok {
		t.Errorf("a note made audit = %+v, want it healthy", groups["audit"])
	}
	if len(topics) != 1 || len(groups) != 1 {
		t.Errorf("topics %v, groups %v: a client's finding is neither's", topics, groups)
	}
}
