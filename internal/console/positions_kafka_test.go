package console

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/ReneP92/changeport/internal/console/api"
)

// kafkaWithGroup is a cluster where orders holds 10 messages at 1.000s to
// 1.009s in partition 0 and 2 at 2.000s and 2.001s in partition 1, and group
// billing is at 8 and 2 on them, and at 1 on the platform's topic
// changeport.x.
func kafkaWithGroup(t *testing.T) (*kfake.Cluster, *kadm.Client) {
	t.Helper()
	cluster, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(2, "orders"), kfake.SeedTopics(1, "changeport.x"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cluster.Close)
	client, err := kgo.NewClient(kgo.SeedBrokers(cluster.ListenAddrs()...), kgo.RecordPartitioner(kgo.ManualPartitioner()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	produce := func(topic string, partition int32, millisecond int64) {
		record := &kgo.Record{Topic: topic, Partition: partition, Timestamp: time.UnixMilli(millisecond), Value: []byte("x")}
		if err := client.ProduceSync(t.Context(), record).FirstErr(); err != nil {
			t.Fatal(err)
		}
	}
	for i := range int64(10) {
		produce("orders", 0, 1000+i)
	}
	produce("orders", 1, 2000)
	produce("orders", 1, 2001)
	produce("changeport.x", 0, 3000)
	admin := kadm.NewClient(client)
	offsets := kadm.Offsets{}
	offsets.Add(kadm.Offset{Topic: "orders", Partition: 0, At: 8, LeaderEpoch: -1})
	offsets.Add(kadm.Offset{Topic: "orders", Partition: 1, At: 2, LeaderEpoch: -1})
	offsets.Add(kadm.Offset{Topic: "changeport.x", Partition: 0, At: 1, LeaderEpoch: -1})
	if err := admin.CommitAllOffsets(t.Context(), "billing", offsets); err != nil {
		t.Fatal(err)
	}
	return cluster, admin
}

func TestGroupPositionsFromKafka(t *testing.T) {
	_, admin := kafkaWithGroup(t)
	at := func(ms int64) time.Time { return time.UnixMilli(ms).UTC() }
	tests := []struct {
		name string
		to   Position
		want []PartitionPosition
	}{
		{"a time", Position{To: api.PositionChangeToTime, Time: time.UnixMilli(1005)}, []PartitionPosition{
			{Topic: "orders", Partition: 0, Committed: 8, End: 10, Target: 5, TargetTime: at(1005)},
			{Topic: "orders", Partition: 1, Committed: 2, End: 2, Target: 0, TargetTime: at(2000)},
		}},
		// Past every message, the group goes to the ends, with no time.
		{"a time past the end", Position{To: api.PositionChangeToTime, Time: time.UnixMilli(5000)}, []PartitionPosition{
			{Topic: "orders", Partition: 0, Committed: 8, End: 10, Target: 10},
			{Topic: "orders", Partition: 1, Committed: 2, End: 2, Target: 2},
		}},
		// Before the epoch, Kafka would read the time as a special lookup.
		{"a time before the epoch", Position{To: api.PositionChangeToTime, Time: time.Date(1960, 1, 1, 0, 0, 0, 0, time.UTC)}, []PartitionPosition{
			{Topic: "orders", Partition: 0, Committed: 8, End: 10, Target: 0, TargetTime: at(1000)},
			{Topic: "orders", Partition: 1, Committed: 2, End: 2, Target: 0, TargetTime: at(2000)},
		}},
		{"the earliest", Position{To: api.PositionChangeToEarliest}, []PartitionPosition{
			{Topic: "orders", Partition: 0, Committed: 8, End: 10, Target: 0, TargetTime: at(1000)},
			{Topic: "orders", Partition: 1, Committed: 2, End: 2, Target: 0, TargetTime: at(2000)},
		}},
		{"the latest", Position{To: api.PositionChangeToLatest}, []PartitionPosition{
			{Topic: "orders", Partition: 0, Committed: 8, End: 10, Target: 10},
			{Topic: "orders", Partition: 1, Committed: 2, End: 2, Target: 2},
		}},
	}
	for _, tt := range tests {
		// The group's topics, without the platform's it reads too.
		got, err := groupPositions(t.Context(), admin, "billing", nil, tt.to)
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("%s: positions\n%+v\nwant\n%+v", tt.name, got, tt.want)
		}
	}

	if _, err := groupPositions(t.Context(), admin, "billing", []string{"unknown"}, Position{To: api.PositionChangeToLatest}); !errors.Is(err, ErrNoSuchTopic) {
		t.Errorf("an unknown topic = %v, want ErrNoSuchTopic", err)
	}
	if got, err := groupPositions(t.Context(), admin, "nobody", nil, Position{To: api.PositionChangeToLatest}); err != nil || len(got) != 0 {
		t.Errorf("a group without positions = %v, %v, want none", got, err)
	}
	// A new group on named topics has no position on them yet.
	got, err := groupPositions(t.Context(), admin, "nobody", []string{"orders"}, Position{To: api.PositionChangeToEarliest})
	if err != nil || len(got) != 2 || got[0].Committed != -1 || got[0].Target != 0 {
		t.Errorf("a new group on orders = %+v, %v, want 2 partitions without positions", got, err)
	}
}

func TestCommitGroupOffsetsToKafka(t *testing.T) {
	cluster, admin := kafkaWithGroup(t)
	positions := []PartitionPosition{{Topic: "orders", Partition: 0, Target: 3}, {Topic: "orders", Partition: 1, Target: 1}}
	committed, err := commitGroupOffsets(t.Context(), admin, "billing-replay", positions)
	if err != nil || len(committed) != 2 {
		t.Fatalf("committing = %+v, %v", committed, err)
	}
	fetched, err := admin.FetchOffsets(t.Context(), "billing-replay")
	if err != nil {
		t.Fatal(err)
	}
	if fetched["orders"][0].At != 3 || fetched["orders"][1].At != 1 {
		t.Errorf("committed %+v, want 3 and 1", fetched)
	}

	// Kafka refuses while the group has a member.
	consumer, err := kgo.NewClient(kgo.SeedBrokers(cluster.ListenAddrs()...), kgo.ConsumerGroup("busy"), kgo.ConsumeTopics("orders"))
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	deadline := time.Now().Add(30 * time.Second)
	for {
		consumer.PollRecords(t.Context(), 1)
		described, err := admin.DescribeGroups(t.Context(), "busy")
		if err == nil && described["busy"].State == "Stable" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("group busy did not become stable: %+v, %v", described, err)
		}
	}
	if _, err := commitGroupOffsets(t.Context(), admin, "busy", positions); !errors.Is(err, ErrGroupActive) {
		t.Errorf("committing to a group with a member = %v, want ErrGroupActive", err)
	}
}
