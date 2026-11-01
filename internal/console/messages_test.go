package console

import (
	"errors"
	"testing"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestStartOffsets(t *testing.T) {
	cluster, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(2, "orders"))
	if err != nil {
		t.Fatal(err)
	}
	defer cluster.Close()
	client, err := kgo.NewClient(kgo.SeedBrokers(cluster.ListenAddrs()...), kgo.RecordPartitioner(kgo.ManualPartitioner()))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	// 10 messages in partition 0, 2 in partition 1.
	for i := range 12 {
		partition := int32(0)
		if i >= 10 {
			partition = 1
		}
		if err := client.ProduceSync(t.Context(), &kgo.Record{Topic: "orders", Partition: partition, Value: []byte("x")}).FirstErr(); err != nil {
			t.Fatal(err)
		}
	}
	admin := kadm.NewClient(client)
	at := func(start TailStart) map[int32]int64 {
		t.Helper()
		offsets, err := startOffsets(t.Context(), admin, "orders", start)
		if err != nil {
			t.Fatal(err)
		}
		out := map[int32]int64{}
		for partition, offset := range offsets {
			out[partition] = offset.EpochOffset().Offset
		}
		return out
	}
	for name, c := range map[string]struct {
		start TailStart
		want  map[int32]int64
	}{
		// 6 recent messages, 3 from each partition, as far as each has them.
		"recent": {TailStart{Recent: 6}, map[int32]int64{0: 7, 1: 0}},
		"latest": {TailStart{}, map[int32]int64{0: 10, 1: 2}},
		// Resumed where an earlier tail ended, within what the partitions
		// hold; a partition it does not name starts as asked.
		"resume":              {TailStart{Resume: map[int32]int64{0: 4}}, map[int32]int64{0: 4, 1: 2}},
		"resume past the end": {TailStart{Resume: map[int32]int64{0: 99, 1: 1}}, map[int32]int64{0: 10, 1: 1}},
	} {
		if got := at(c.start); got[0] != c.want[0] || got[1] != c.want[1] || len(got) != 2 {
			t.Errorf("%s: start offsets %v, want %v", name, got, c.want)
		}
	}
	if _, err := startOffsets(t.Context(), admin, "missing", TailStart{}); !errors.Is(err, ErrNoSuchTopic) {
		t.Errorf("start offsets of a missing topic: %v, want ErrNoSuchTopic", err)
	}
}
