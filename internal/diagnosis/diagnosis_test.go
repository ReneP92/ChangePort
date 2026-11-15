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
	if want := "3% of the records checkout tried to send in the last 15 minutes failed: 3 a second of 103."; found[0].Explanation != want {
		t.Errorf("explanation = %q\nwant %q", found[0].Explanation, want)
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
