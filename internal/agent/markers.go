package agent

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// Markers tells a consumer group that has read every message of a partition
// from one with messages still to read. A transaction ends with a marker
// that takes an offset of its own but is no message, so a consumer that
// commits the offset after its last message, as librdkafka-based clients
// and franz-go do, stays an offset or so short of the partition's end on a
// topic written in transactions, such as a Postgres source's.
type Markers interface {
	// OnlyMarkers reports whether the offsets from from up to end of
	// topic's partition hold nothing a read_committed consumer reads:
	// transaction markers, and messages of aborted transactions.
	OnlyMarkers(ctx context.Context, topic string, partition int32, from, end int64) (bool, error)
}

// markerGap is the most offsets a lagging partition may be behind for the
// collector to look at them: a transaction's marker, or a few. Larger gaps
// hold messages.
const markerGap = 16

// KafkaMarkers implements Markers with a fetch from the partition's leader.
// It needs Read on topics. It looks at the record batches' headers only,
// and keeps nothing of the messages it fetches along with them.
type KafkaMarkers struct {
	Client *kgo.Client
}

var _ Markers = KafkaMarkers{}

// markerFetchBytes bounds what the fetch returns: enough for markerGap
// markers, which are batches of a single small record. Kafka returns the
// first batch whole even when it is larger, and the headers of later
// batches suffice.
const markerFetchBytes = 4 << 10

// OnlyMarkers implements Markers.
func (k KafkaMarkers) OnlyMarkers(ctx context.Context, topic string, partition int32, from, end int64) (bool, error) {
	metadata, err := kadm.NewClient(k.Client).Metadata(ctx, topic)
	if err != nil {
		return false, fmt.Errorf("metadata of %s: %w", topic, err)
	}
	details, ok := metadata.Topics[topic].Partitions[partition]
	if !ok || details.Err != nil {
		return false, fmt.Errorf("no leader of %s-%d", topic, partition)
	}
	request := kmsg.NewPtrFetchRequest()
	request.MaxWaitMillis, request.MaxBytes, request.IsolationLevel = 0, markerFetchBytes, 1
	fetchTopic := kmsg.NewFetchRequestTopic()
	fetchTopic.Topic, fetchTopic.TopicID = topic, metadata.Topics[topic].ID
	fetchPartition := kmsg.NewFetchRequestTopicPartition()
	fetchPartition.Partition, fetchPartition.FetchOffset, fetchPartition.PartitionMaxBytes = partition, from, markerFetchBytes
	fetchPartition.CurrentLeaderEpoch, fetchPartition.LogStartOffset = -1, -1
	fetchTopic.Partitions = append(fetchTopic.Partitions, fetchPartition)
	request.Topics = append(request.Topics, fetchTopic)
	response, err := request.RequestWith(ctx, k.Client.Broker(int(details.Leader)))
	if err != nil {
		return false, fmt.Errorf("fetch %s-%d: %w", topic, partition, err)
	}
	if err := kerr.ErrorForCode(response.ErrorCode); err != nil {
		return false, fmt.Errorf("fetch %s-%d: %w", topic, partition, err)
	}
	if len(response.Topics) != 1 || len(response.Topics[0].Partitions) != 1 {
		return false, fmt.Errorf("fetch %s-%d: no answer for the partition", topic, partition)
	}
	answer := response.Topics[0].Partitions[0]
	if err := kerr.ErrorForCode(answer.ErrorCode); err != nil {
		return false, fmt.Errorf("fetch %s-%d: %w", topic, partition, err)
	}
	aborted := map[int64]int64{}
	for _, a := range answer.AbortedTransactions {
		if first, ok := aborted[a.ProducerID]; !ok || a.FirstOffset < first {
			aborted[a.ProducerID] = a.FirstOffset
		}
	}
	return onlyMarkers(answer.RecordBatches, aborted, from, end), nil
}

// The fields of a record batch's header (Kafka's message format v2) that
// onlyMarkers reads, by their position.
const (
	batchBaseOffset      = 0
	batchLength          = 8
	batchAttributes      = 21
	batchLastOffsetDelta = 23
	batchProducerID      = 43
	batchHeaderSize      = 61

	attributeTransactional = 0x10
	attributeControl       = 0x20
)

// onlyMarkers reports whether the record batches cover the offsets from
// from up to end with markers and aborted transactions' batches only:
// aborted maps a producer ID to the first offset of its aborted
// transaction, as the fetch answers it. Batches the fetch cut short count by
// their header, which comes first. Offsets the batches do not reach count
// as messages, so a short answer never hides lag.
func onlyMarkers(batches []byte, aborted map[int64]int64, from, end int64) bool {
	next := from
	for len(batches) >= batchHeaderSize && next < end {
		base := int64(binary.BigEndian.Uint64(batches[batchBaseOffset:])) //nolint:gosec // G115: Kafka's int64
		length := int64(binary.BigEndian.Uint32(batches[batchLength:]))
		attributes := binary.BigEndian.Uint16(batches[batchAttributes:])
		last := base + int64(binary.BigEndian.Uint32(batches[batchLastOffsetDelta:]))
		producer := int64(binary.BigEndian.Uint64(batches[batchProducerID:])) //nolint:gosec // G115: Kafka's int64
		if last >= next {
			// Offsets before base with no batch, as compaction leaves them,
			// hold no message either.
			first, abortedTransaction := aborted[producer]
			skipped := attributes&attributeControl != 0 ||
				attributes&attributeTransactional != 0 && abortedTransaction && first <= base
			if !skipped {
				return false
			}
			next = last + 1
		}
		if size := batchLength + 4 + length; size < int64(len(batches)) {
			batches = batches[size:]
		} else {
			break
		}
	}
	return next >= end
}
