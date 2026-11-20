package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
)

// A partition written in transactions: committed messages at 0 and 2, each
// followed by its commit marker, then an aborted message at 4 and its abort
// marker at 5.
func transactionalPartition(t *testing.T) *kfake.Cluster {
	t.Helper()
	cluster, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(1, "shop.public.orders"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cluster.Close)
	producer, err := kgo.NewClient(kgo.SeedBrokers(cluster.ListenAddrs()...), kgo.TransactionalID("connector"), kgo.DefaultProduceTopic("shop.public.orders"))
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	for _, end := range []kgo.TransactionEndTry{kgo.TryCommit, kgo.TryCommit, kgo.TryAbort} {
		if err := producer.BeginTransaction(); err != nil {
			t.Fatal(err)
		}
		if err := producer.ProduceSync(t.Context(), kgo.StringRecord("change")).FirstErr(); err != nil {
			t.Fatal(err)
		}
		if err := producer.EndTransaction(t.Context(), end); err != nil {
			t.Fatal(err)
		}
	}
	return cluster
}

func TestKafkaMarkers(t *testing.T) {
	cluster := transactionalPartition(t)
	client, err := kgo.NewClient(kgo.SeedBrokers(cluster.ListenAddrs()...))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	markers := KafkaMarkers{Client: client}
	for _, c := range []struct {
		from, end int64
		want      bool
	}{
		// A commit marker, the aborted message and its marker.
		{3, 6, true},
		// The marker after the first message only.
		{1, 2, true},
		// The second message.
		{2, 6, false},
		{0, 6, false},
	} {
		only, err := markers.OnlyMarkers(t.Context(), "shop.public.orders", 0, c.from, c.end)
		if err != nil {
			t.Fatal(err)
		}
		if only != c.want {
			t.Errorf("only markers from %d up to %d = %v, want %v", c.from, c.end, only, c.want)
		}
	}
}

// A short answer never hides lag: offsets the batches do not reach count as
// messages.
func TestOnlyMarkersOfNoBatches(t *testing.T) {
	if onlyMarkers(nil, nil, 3, 4) {
		t.Error("no batches cover the offsets, yet they count as markers")
	}
}

// markersAt is a Markers that knows which partitions end in markers only.
type markersAt map[string]bool

func (m markersAt) OnlyMarkers(_ context.Context, topic string, _ int32, _, _ int64) (bool, error) {
	return m[topic], nil
}

// A group that stops short of a partition's end by its transactions'
// markers has read every message; one an offset short of a message has not.
func TestLagCollectorSkipsTheMarkersAtTheEnd(t *testing.T) {
	admin := newFakeLagAdmin()
	admin.groups = map[string]string{"warehouse": "consumer"}
	admin.committed = map[string]map[string]map[int32]int64{"warehouse": {"shop.public.orders": {0: 99}, "payments": {0: 499}}}
	admin.ends = map[string]map[int32]int64{"shop.public.orders": {0: 100}, "payments": {0: 500}}
	c := newTestLagCollector(admin)
	c.Markers = markersAt{"shop.public.orders": true}
	if err := c.Measure(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := `
# HELP changeport_consumergroup_lag_messages Messages the consumer group has not consumed yet, summed over the topic's partitions.
# TYPE changeport_consumergroup_lag_messages gauge
changeport_consumergroup_lag_messages{consumer_group="warehouse",topic="payments"} 1
changeport_consumergroup_lag_messages{consumer_group="warehouse",topic="shop.public.orders"} 0
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(want), "changeport_consumergroup_lag_messages"); err != nil {
		t.Error(err)
	}
}
