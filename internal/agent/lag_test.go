package agent

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
)

var testNow = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

// steadyAnchors are the anchor offsets of a partition that received one
// message per second for its whole life, ending at offset end now. Offset o
// was produced (end-o) seconds ago; before the partition's first message the
// first offset at or after a time is 0.
func steadyAnchors(end int64) []int64 {
	offsets := make([]int64, len(Anchors))
	for i, anchor := range Anchors {
		offsets[i] = max(0, end-int64(anchor.Seconds()))
	}
	return offsets
}

func TestAge(t *testing.T) {
	const end = 100_000 // a little over 27 hours of one message per second
	tests := []struct {
		name      string
		committed int64
		trueAge   time.Duration
		maxError  time.Duration
		capped    bool
	}{
		{"caught up", end, 0, 0, false},
		{"one second behind", end - 1, time.Second, 0, false},
		{"ten seconds behind", end - 10, 10 * time.Second, 0, false},
		{"a minute behind", end - 60, time.Minute, 0, false},
		{"seven minutes behind", end - 420, 7 * time.Minute, 0, false},
		{"five hours behind", end - 5*3600, 5 * time.Hour, 0, false},
		{"over the oldest anchor", end - 90_000, 23 * time.Hour, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			age, capped := Age(tt.committed, end, testNow, steadyAnchors(end))
			if capped != tt.capped {
				t.Errorf("capped = %v, want %v", capped, tt.capped)
			}
			if diff := (age - tt.trueAge).Abs(); diff > tt.maxError+time.Millisecond {
				t.Errorf("Age() = %s, want %s within %s", age, tt.trueAge, tt.maxError)
			}
		})
	}
}

// TestAgeBeforeTheFirstMessage covers a partition younger than an anchor:
// the anchor's offset is the first offset, produced after the anchor. The
// estimate stays between the two anchors around the true age, 15m and 20m.
func TestAgeBeforeTheFirstMessage(t *testing.T) {
	const end = 1000 // the first message is 1000 seconds, almost 17m, old
	age, capped := Age(0, end, testNow, steadyAnchors(end))
	if capped || age < 15*time.Minute || age > 20*time.Minute {
		t.Errorf("Age() = %s (capped %v), want between the anchors around the true age, 15m and 20m", age, capped)
	}
}

// TestAgeOfBurstyTraffic checks the bound under uneven traffic: 100 messages
// arrived in one second 40 minutes ago, then nothing until 1000 messages in
// the last minute. Interpolation cannot know where in the gap the burst was,
// but the estimate stays between the two anchors around the true age, 30m
// and 45m.
func TestAgeOfBurstyTraffic(t *testing.T) {
	// Offsets 0-99 were produced 40 minutes ago, 100-1099 over the last minute.
	offsetAt := func(ago time.Duration) int64 { // first offset at or after now-ago
		switch {
		case ago >= 40*time.Minute:
			return 0
		case ago > time.Minute:
			return 100
		default:
			return 1100 - int64(1000*ago/time.Minute)
		}
	}
	anchors := make([]int64, len(Anchors))
	for i, anchor := range Anchors {
		anchors[i] = offsetAt(anchor)
	}
	age, capped := Age(50, 1100, testNow, anchors)
	if capped || age < 30*time.Minute || age > 45*time.Minute {
		t.Errorf("Age() = %s (capped %v), want between the anchors around the true 40m, 30m and 45m", age, capped)
	}
}

// fakeLagAdmin is a cluster where every partition received one message per
// second, ending at its end offset now.
type fakeLagAdmin struct {
	groups    map[string]string                     // group name to type
	committed map[string]map[string]map[int32]int64 // group, topic, partition
	ends      map[string]map[int32]int64
	listed    [][]string // the topics of every ListOffsetsAfterMilli call
	// anchorErr, when set, fails every partition of anchor lookups with it.
	anchorErr error
}

func (f *fakeLagAdmin) ListGroupsByType(_ context.Context, types []string, _ ...string) (kadm.ListedGroups, error) {
	listed := kadm.ListedGroups{}
	for name, kind := range f.groups {
		if slices.Contains(types, kind) {
			listed[name] = kadm.ListedGroup{Group: name, ProtocolType: "consumer"}
		}
	}
	return listed, nil
}

func (f *fakeLagAdmin) FetchManyOffsets(_ context.Context, groups ...string) kadm.FetchOffsetsResponses {
	responses := kadm.FetchOffsetsResponses{}
	for _, group := range groups {
		fetched := kadm.OffsetResponses{}
		for topic, partitions := range f.committed[group] {
			for partition, at := range partitions {
				fetched.Add(kadm.OffsetResponse{Offset: kadm.Offset{Topic: topic, Partition: partition, At: at}})
			}
		}
		responses[group] = kadm.FetchOffsetsResponse{Group: group, Fetched: fetched}
	}
	return responses
}

func (f *fakeLagAdmin) offsets(topics []string, at func(end int64) int64) kadm.ListedOffsets {
	listed := kadm.ListedOffsets{}
	for _, topic := range topics {
		for partition, end := range f.ends[topic] {
			if listed[topic] == nil {
				listed[topic] = map[int32]kadm.ListedOffset{}
			}
			listed[topic][partition] = kadm.ListedOffset{Topic: topic, Partition: partition, Offset: at(end)}
		}
	}
	return listed
}

func (f *fakeLagAdmin) ListEndOffsets(_ context.Context, topics ...string) (kadm.ListedOffsets, error) {
	return f.offsets(topics, func(end int64) int64 { return end }), nil
}

func (f *fakeLagAdmin) ListOffsetsAfterMilli(_ context.Context, millisecond int64, topics ...string) (kadm.ListedOffsets, error) {
	f.listed = append(f.listed, slices.Clone(topics))
	ago := testNow.Sub(time.UnixMilli(millisecond))
	listed := f.offsets(topics, func(end int64) int64 { return max(0, end-int64(ago.Seconds())) })
	if f.anchorErr != nil {
		for _, partitions := range listed {
			for partition, o := range partitions {
				o.Err, o.Offset = f.anchorErr, -1
				partitions[partition] = o
			}
		}
	}
	return listed, nil
}

func newFakeLagAdmin() *fakeLagAdmin {
	return &fakeLagAdmin{
		groups: map[string]string{"billing": "classic", "shipping": "consumer", "audit-share": "share"},
		committed: map[string]map[string]map[int32]int64{
			// 3 minutes behind on partition 0, 30 seconds on partition 1.
			"billing": {"orders": {0: 10_000 - 180, 1: 10_000 - 30}, "__consumer_offsets": {0: 5}},
			// Caught up.
			"shipping":    {"payments": {0: 500}},
			"audit-share": {"orders": {0: 0}},
		},
		ends: map[string]map[int32]int64{"orders": {0: 10_000, 1: 10_000}, "payments": {0: 500}},
	}
}

func newTestLagCollector(admin LagAdmin) *LagCollector {
	c := NewLagCollector(admin, prometheus.NewRegistry())
	c.Now = func() time.Time { return testNow }
	return c
}

func TestLagCollectorMeasures(t *testing.T) {
	admin := newFakeLagAdmin()
	c := newTestLagCollector(admin)
	if err := c.Measure(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := `
# HELP changeport_consumergroup_lag_capped 1 when the lag is older than the oldest time the agent looks up (23 hours), so lag_seconds is a lower bound.
# TYPE changeport_consumergroup_lag_capped gauge
changeport_consumergroup_lag_capped{consumer_group="billing",topic="orders"} 0
changeport_consumergroup_lag_capped{consumer_group="shipping",topic="payments"} 0
# HELP changeport_consumergroup_lag_messages Messages the consumer group has not consumed yet, summed over the topic's partitions.
# TYPE changeport_consumergroup_lag_messages gauge
changeport_consumergroup_lag_messages{consumer_group="billing",topic="orders"} 210
changeport_consumergroup_lag_messages{consumer_group="shipping",topic="payments"} 0
# HELP changeport_consumergroup_lag_seconds Age of the oldest message the consumer group has not consumed yet, the highest over the topic's partitions.
# TYPE changeport_consumergroup_lag_seconds gauge
changeport_consumergroup_lag_seconds{consumer_group="billing",topic="orders"} 180
changeport_consumergroup_lag_seconds{consumer_group="shipping",topic="payments"} 0
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(want), "changeport_consumergroup_lag_capped",
		"changeport_consumergroup_lag_messages", "changeport_consumergroup_lag_seconds"); err != nil {
		t.Error(err)
	}
	// Anchors are only looked up for the lagging topic.
	for _, topics := range admin.listed {
		if !slices.Equal(topics, []string{"orders"}) {
			t.Fatalf("looked up anchors for %v, want only the lagging topic orders", topics)
		}
	}
	if len(admin.listed) != len(Anchors) {
		t.Errorf("looked up %d anchors, want %d", len(admin.listed), len(Anchors))
	}
}

// When a group last committed: since the agent started at the earliest, and
// kept while its offsets do not move.
func TestLagCollectorTellsWhenGroupsLastCommitted(t *testing.T) {
	admin := newFakeLagAdmin()
	c := newTestLagCollector(admin)
	committedAt := func() map[string]float64 {
		t.Helper()
		at := map[string]float64{}
		metrics := make(chan prometheus.Metric, 100)
		c.Collect(metrics)
		close(metrics)
		for metric := range metrics {
			if !strings.Contains(metric.Desc().String(), "changeport_consumergroup_committed_timestamp_seconds") {
				continue
			}
			var m dto.Metric
			if err := metric.Write(&m); err != nil {
				t.Fatal(err)
			}
			at[m.GetLabel()[0].GetValue()] = m.GetGauge().GetValue()
		}
		return at
	}
	if err := c.Measure(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := committedAt(); got["billing"] != float64(testNow.Unix()) || got["shipping"] != float64(testNow.Unix()) {
		t.Fatalf("committed at %v, want the first measurement's time", got)
	}

	// A minute later, billing commits and shipping does not.
	start := testNow
	c.Now = func() time.Time { return start.Add(time.Minute) }
	admin.committed["billing"]["orders"][0] += 50
	if err := c.Measure(t.Context()); err != nil {
		t.Fatal(err)
	}
	got := committedAt()
	if got["billing"] != float64(start.Add(time.Minute).Unix()) || got["shipping"] != float64(start.Unix()) {
		t.Errorf("committed at %v, want billing now and shipping a minute ago", got)
	}
}

func TestLagCollectorForgetsGroupsThatAreGone(t *testing.T) {
	admin := newFakeLagAdmin()
	c := newTestLagCollector(admin)
	if err := c.Measure(t.Context()); err != nil {
		t.Fatal(err)
	}
	delete(admin.groups, "billing")
	if err := c.Measure(t.Context()); err != nil {
		t.Fatal(err)
	}
	if n := testutil.CollectAndCount(c, "changeport_consumergroup_lag_seconds"); n != 1 {
		t.Errorf("%d lag series after the billing group is gone, want 1", n)
	}
}

func TestLagCollectorCapsSeries(t *testing.T) {
	c := newTestLagCollector(newFakeLagAdmin())
	c.MaxSeries = 1
	if err := c.Measure(t.Context()); err != nil {
		t.Fatal(err)
	}
	if n := testutil.CollectAndCount(c, "changeport_consumergroup_lag_seconds"); n != 1 {
		t.Errorf("%d lag series with a cap of 1", n)
	}
	if got := testutil.ToFloat64(c.dropped); got != 1 {
		t.Errorf("dropped series = %v, want 1", got)
	}
}

// TestLagCollectorCarriesLagForwardWhenLookupsFail covers transient lookup
// errors, for example while a topic switches to tiered storage: a failed
// anchor must not read as "no message since", which would overstate the lag,
// and a group that consumed nothing since must not look less behind than it
// is.
func TestLagCollectorCarriesLagForwardWhenLookupsFail(t *testing.T) {
	admin := newFakeLagAdmin()
	admin.anchorErr = kerr.OffsetNotAvailable
	c := newTestLagCollector(admin)
	if err := c.Measure(t.Context()); !errors.Is(err, kerr.OffsetNotAvailable) {
		t.Fatalf("Measure() = %v, want the lookup error", err)
	}
	if n := testutil.CollectAndCount(c, "changeport_consumergroup_lag_seconds"); n != 1 {
		t.Errorf("%d lag series after failed lookups, want only the caught-up group", n)
	}

	admin.anchorErr = nil
	if err := c.Measure(t.Context()); err != nil {
		t.Fatal(err)
	}
	// 30 seconds later the lookups fail, billing has consumed nothing, and
	// 20 more messages arrived on orders.
	start := testNow
	c.Now = func() time.Time { return start.Add(30 * time.Second) }
	admin.anchorErr = kerr.RequestTimedOut
	admin.ends["orders"][0] += 20
	if err := c.Measure(t.Context()); !errors.Is(err, kerr.RequestTimedOut) {
		t.Fatalf("Measure() = %v, want the lookup error", err)
	}
	want := `
# HELP changeport_consumergroup_lag_messages Messages the consumer group has not consumed yet, summed over the topic's partitions.
# TYPE changeport_consumergroup_lag_messages gauge
changeport_consumergroup_lag_messages{consumer_group="billing",topic="orders"} 230
changeport_consumergroup_lag_messages{consumer_group="shipping",topic="payments"} 0
# HELP changeport_consumergroup_lag_seconds Age of the oldest message the consumer group has not consumed yet, the highest over the topic's partitions.
# TYPE changeport_consumergroup_lag_seconds gauge
changeport_consumergroup_lag_seconds{consumer_group="billing",topic="orders"} 210
changeport_consumergroup_lag_seconds{consumer_group="shipping",topic="payments"} 0
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(want),
		"changeport_consumergroup_lag_messages", "changeport_consumergroup_lag_seconds"); err != nil {
		t.Errorf("a group that consumed nothing must carry its lag forward: %v", err)
	}

	// Another 30 seconds on, billing has consumed some: its last measurement
	// stays, as the lookups that would place its new offset still fail.
	c.Now = func() time.Time { return start.Add(time.Minute) }
	admin.committed["billing"]["orders"][0] += 100
	if err := c.Measure(t.Context()); err == nil {
		t.Fatal("Measure() = nil, want the lookup error")
	}
	if err := testutil.CollectAndCompare(c, strings.NewReader(want),
		"changeport_consumergroup_lag_messages", "changeport_consumergroup_lag_seconds"); err != nil {
		t.Errorf("a group that moved must keep its last measurement: %v", err)
	}
}

func TestLagCollectorWithoutGroups(t *testing.T) {
	c := newTestLagCollector(&fakeLagAdmin{})
	if err := c.Measure(t.Context()); err != nil {
		t.Fatal(err)
	}
	if n := testutil.CollectAndCount(c); n != 0 {
		t.Errorf("%d series without groups", n)
	}
}
