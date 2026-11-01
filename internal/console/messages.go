package console

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/ReneP92/changeport/internal/clusters"
)

// Messages reads a topic's messages for its tail, as the customer's tail
// user, which may read topics and nothing else (docs/console.md).
type Messages interface {
	// Open starts reading topic at start, or returns ErrNoSuchTopic, or an
	// error satisfying errors.Is(err, clusters.ErrNotReady).
	Open(ctx context.Context, customerID, topic string, start TailStart) (MessageReader, error)
}

// TailStart is where a tail starts in each partition.
type TailStart struct {
	// Recent is how many of the topic's last messages to start with, over
	// all its partitions; none starts with new messages only.
	Recent int
	// Resume, when set, is the offset to go on from in each partition it
	// names, as an earlier tail ended.
	Resume map[int32]int64
}

// MessageReader reads one tail's messages.
type MessageReader interface {
	// Poll returns the messages that arrived, waiting until ctx is done at
	// most.
	Poll(ctx context.Context) ([]Message, error)
	Close()
}

// Message is a message of a topic as the tail reads it.
type Message struct {
	Partition  int32
	Offset     int64
	Timestamp  time.Time
	Key, Value []byte
	Headers    []kgo.RecordHeader
}

// KubeMessages reads messages through the console's pool of clients.
type KubeMessages struct {
	Kafka *clusters.Pool
}

var _ Messages = KubeMessages{}

// Open implements Messages.
func (k KubeMessages) Open(ctx context.Context, customerID, topic string, start TailStart) (MessageReader, error) {
	client, err := k.Kafka.Reader(ctx, customerID)
	if err != nil {
		return nil, err
	}
	offsets, err := startOffsets(ctx, kadm.NewClient(client), topic, start)
	if err != nil {
		client.Close()
		return nil, err
	}
	client.AddConsumePartitions(map[string]map[int32]kgo.Offset{topic: offsets})
	return kafkaReader{client: client}, nil
}

// startOffsets returns where the tail starts in each partition of topic:
// where start resumes, else the last messages spread over the partitions,
// else the end.
func startOffsets(ctx context.Context, admin *kadm.Client, topic string, start TailStart) (map[int32]kgo.Offset, error) {
	ends, err := admin.ListEndOffsets(ctx, topic)
	if err != nil {
		return nil, fmt.Errorf("list the end offsets of %s: %w", topic, err)
	}
	starts, err := admin.ListStartOffsets(ctx, topic)
	if err != nil {
		return nil, fmt.Errorf("list the start offsets of %s: %w", topic, err)
	}
	if err := ends.Error(); err != nil {
		if errors.Is(err, kerr.UnknownTopicOrPartition) {
			return nil, ErrNoSuchTopic
		}
		return nil, fmt.Errorf("list the end offsets of %s: %w", topic, err)
	}
	partitions := ends[topic]
	if len(partitions) == 0 {
		return nil, ErrNoSuchTopic
	}
	perPartition := 0
	if start.Recent > 0 {
		perPartition = max(1, (start.Recent+len(partitions)-1)/len(partitions))
	}
	offsets := map[int32]kgo.Offset{}
	for partition, end := range partitions {
		first := end.Offset
		if s, ok := starts.Lookup(topic, partition); ok && s.Err == nil {
			first = s.Offset
		}
		at := max(first, end.Offset-int64(perPartition))
		if resume, ok := start.Resume[partition]; ok {
			at = min(max(resume, first), end.Offset)
		}
		offsets[partition] = kgo.NewOffset().At(at)
	}
	return offsets, nil
}

type kafkaReader struct {
	client *kgo.Client
}

// pollRecords caps the messages of one poll.
const pollRecords = 500

func (r kafkaReader) Poll(ctx context.Context) ([]Message, error) {
	fetches := r.client.PollRecords(ctx, pollRecords)
	if ctx.Err() == nil {
		for _, fetchErr := range fetches.Errors() {
			if !errors.Is(fetchErr.Err, context.DeadlineExceeded) && !errors.Is(fetchErr.Err, context.Canceled) {
				return nil, fmt.Errorf("read partition %d of %s: %w", fetchErr.Partition, fetchErr.Topic, fetchErr.Err)
			}
		}
	}
	var messages []Message
	fetches.EachRecord(func(record *kgo.Record) {
		messages = append(messages, Message{
			Partition: record.Partition, Offset: record.Offset, Timestamp: record.Timestamp,
			Key: record.Key, Value: record.Value, Headers: record.Headers,
		})
	})
	return messages, nil
}

func (r kafkaReader) Close() { r.client.Close() }
