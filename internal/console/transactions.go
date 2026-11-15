package console

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"k8s.io/utils/ptr"

	"github.com/ReneP92/changeport/internal/clusters"
	"github.com/ReneP92/changeport/internal/console/api"
	"github.com/ReneP92/changeport/internal/topics"
)

// Transaction is a transaction open on a customer's cluster, as its
// coordinator describes it (docs/connecting.md, "Delivery guarantees").
type Transaction struct {
	// ID is its transactional ID, and ProducerID the producer's.
	ID         string
	ProducerID int64
	// State is the coordinator's: Ongoing, PrepareCommit or PrepareAbort.
	State   string
	Started time.Time
	// Timeout is how long it may stay open before the coordinator aborts it.
	Timeout    time.Duration
	Partitions []TransactionPartition
}

// TransactionPartition is a partition a transaction wrote to, and how many
// messages read_committed consumers of it cannot read yet: those after its
// last stable offset, which an open transaction holds back.
type TransactionPartition struct {
	Topic     string
	Partition int32
	HeldBack  int64
}

// PartitionProducer is a producer Kafka keeps state for on a partition:
// one that writes idempotently or in transactions. Kafka keeps none for
// other producers, and knows producers by ID only, not by client ID.
type PartitionProducer struct {
	Topic      string
	Partition  int32
	ProducerID int64
	// TransactionalID names a transactional producer, when its coordinator
	// still knows it; "" for idempotent producers.
	TransactionalID string
	LastWrite       time.Time
	// OpenFrom is the offset of the first message of the transaction it
	// has open on the partition, or -1.
	OpenFrom int64
}

// openStates are the states of a transaction its coordinator has not
// finished.
var openStates = []string{"Ongoing", "PrepareCommit", "PrepareAbort"}

// Transactions implements Clusters.
func (k KubeClusters) Transactions(ctx context.Context, customerID string) ([]Transaction, error) {
	admin, err := k.Kafka.Admin(ctx, customerID)
	if err != nil {
		return nil, err
	}
	listed, err := admin.ListTransactions(ctx, nil, openStates)
	if err != nil {
		return nil, fmt.Errorf("list the transactions of customer %s: %w", customerID, err)
	}
	if len(listed) == 0 {
		return nil, nil
	}
	described, err := admin.DescribeTransactions(ctx, listed.TransactionalIDs()...)
	if err != nil {
		return nil, fmt.Errorf("describe the transactions of customer %s: %w", customerID, err)
	}
	var transactions []Transaction
	topics := map[string]bool{}
	for _, d := range described.Sorted() {
		if d.Err != nil || !slices.Contains(openStates, d.State) {
			continue
		}
		t := Transaction{
			ID: d.TxnID, ProducerID: d.ProducerID, State: d.State,
			Started: time.UnixMilli(d.StartTimestamp).UTC(), Timeout: time.Duration(d.TimeoutMillis) * time.Millisecond,
		}
		d.Topics.EachPartitions(func(topic string, partitions []int32) {
			// Offsets committed in a transaction go to Kafka's own topic.
			if strings.HasPrefix(topic, "__") {
				return
			}
			for _, partition := range partitions {
				t.Partitions = append(t.Partitions, TransactionPartition{Topic: topic, Partition: partition})
			}
			topics[topic] = true
		})
		slices.SortFunc(t.Partitions, func(a, b TransactionPartition) int {
			return cmp.Or(strings.Compare(a.Topic, b.Topic), cmp.Compare(a.Partition, b.Partition))
		})
		transactions = append(transactions, t)
	}
	if len(topics) > 0 {
		heldBack, err := k.heldBack(ctx, admin, slices.Sorted(maps.Keys(topics)))
		if err != nil {
			return nil, err
		}
		for i := range transactions {
			for j, p := range transactions[i].Partitions {
				transactions[i].Partitions[j].HeldBack = heldBack[p.Topic][p.Partition]
			}
		}
	}
	return transactions, nil
}

// heldBack returns the messages read_committed consumers cannot read yet on
// each partition of topics: from the last stable offset, which an open
// transaction holds back, to the high watermark.
func (k KubeClusters) heldBack(ctx context.Context, admin *kadm.Client, topics []string) (map[string]map[int32]int64, error) {
	ends, err := admin.ListEndOffsets(ctx, topics...)
	if err != nil {
		return nil, fmt.Errorf("list the end offsets of %v: %w", topics, err)
	}
	stable, err := admin.ListCommittedOffsets(ctx, topics...)
	if err != nil {
		return nil, fmt.Errorf("list the last stable offsets of %v: %w", topics, err)
	}
	heldBack := map[string]map[int32]int64{}
	ends.Each(func(end kadm.ListedOffset) {
		last, ok := stable.Lookup(end.Topic, end.Partition)
		if end.Err != nil || !ok || last.Err != nil {
			return
		}
		if heldBack[end.Topic] == nil {
			heldBack[end.Topic] = map[int32]int64{}
		}
		heldBack[end.Topic][end.Partition] = max(0, end.Offset-last.Offset)
	})
	return heldBack, nil
}

// Producers implements Clusters. Kafka authorizes describing producers as
// reading a topic, so it asks as the tail user, which reads no messages
// here; the console's own user names the transactional ones.
func (k KubeClusters) Producers(ctx context.Context, customerID string, topics ...string) ([]PartitionProducer, error) {
	if len(topics) == 0 {
		return nil, nil
	}
	admin, err := k.Kafka.Admin(ctx, customerID)
	if err != nil {
		return nil, err
	}
	details, err := admin.ListTopics(ctx, topics...)
	if err != nil {
		return nil, fmt.Errorf("list the partitions of %v: %w", topics, err)
	}
	set := kadm.TopicsSet{}
	for _, d := range details {
		if d.Err == nil {
			set.Add(d.Topic, d.Partitions.Numbers()...)
		}
	}
	if len(set) == 0 {
		return nil, nil
	}
	reading, err := k.Kafka.Reading(ctx, customerID)
	if err != nil {
		return nil, err
	}
	described, err := reading.DescribeProducers(ctx, set)
	if err != nil {
		return nil, fmt.Errorf("describe the producers of customer %s: %w", customerID, err)
	}
	var producers []PartitionProducer
	ids := map[int64]bool{}
	described.EachPartition(func(p kadm.DescribedProducersPartition) {
		if p.Err != nil {
			return
		}
		p.ActiveProducers.Each(func(d kadm.DescribedProducer) {
			producers = append(producers, PartitionProducer{
				Topic: d.Topic, Partition: d.Partition, ProducerID: d.ProducerID,
				LastWrite: time.UnixMilli(d.LastTimestamp).UTC(), OpenFrom: d.CurrentTxnStartOffset,
			})
			ids[d.ProducerID] = true
		})
	})
	if len(ids) == 0 {
		return producers, nil
	}
	listed, err := admin.ListTransactions(ctx, slices.Sorted(maps.Keys(ids)), nil)
	if err != nil {
		return nil, fmt.Errorf("name the transactional producers of customer %s: %w", customerID, err)
	}
	names := map[int64]string{}
	for _, t := range listed.Sorted() {
		names[t.ProducerID] = t.TxnID
	}
	for i := range producers {
		producers[i].TransactionalID = names[producers[i].ProducerID]
	}
	slices.SortFunc(producers, func(a, b PartitionProducer) int {
		return cmp.Or(strings.Compare(a.Topic, b.Topic), cmp.Compare(a.Partition, b.Partition), cmp.Compare(a.ProducerID, b.ProducerID))
	})
	return producers, nil
}

// ListTopicProducers implements api.StrictServerInterface: the producers
// Kafka keeps state for on a topic, which members may see too, as they show
// no messages.
func (s *Server) ListTopicProducers(ctx context.Context, request api.ListTopicProducersRequestObject) (api.ListTopicProducersResponseObject, error) {
	customer, _, refused, err := s.customerOf(ctx)
	switch {
	case err != nil:
		return nil, err
	case refused != nil && refused.Status == http.StatusNotFound:
		return api.ListTopicProducers404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(*refused)}, nil
	case refused != nil:
		return api.ListTopicProducers403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(*refused)}, nil
	case topics.Reserved(request.Topic):
		return api.ListTopicProducers404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(noSuchTopic(request.Topic))}, nil
	}
	producers, err := s.clusters.Producers(ctx, customer.Name, request.Topic)
	if errors.Is(err, clusters.ErrNotReady) {
		return api.ListTopicProducers409JSONResponse{NotReadyJSONResponse: api.NotReadyJSONResponse(notReadyYet)}, nil
	}
	if err != nil {
		return nil, err
	}
	transactions, err := s.clusters.Transactions(ctx, customer.Name)
	if err != nil {
		return nil, err
	}
	out := api.ListTopicProducers200JSONResponse{Producers: []api.TopicProducer{}}
	// Each partition's held-back messages count once, however many
	// transactions are open on it.
	heldBack := map[int32]int64{}
	for _, t := range transactions {
		for _, p := range t.Partitions {
			if p.Topic == request.Topic {
				heldBack[p.Partition] = p.HeldBack
			}
		}
	}
	for _, n := range heldBack {
		out.HeldBack += n
	}
	byID := map[int64]*api.TopicProducer{}
	for _, p := range producers {
		if p.Topic != request.Topic {
			continue
		}
		producer, ok := byID[p.ProducerID]
		if !ok {
			producer = &api.TopicProducer{ProducerId: p.ProducerID, LastWrite: p.LastWrite.UTC()}
			if p.TransactionalID != "" {
				producer.TransactionalId = ptr.To(p.TransactionalID)
			}
			byID[p.ProducerID] = producer
		}
		producer.Partitions++
		if p.LastWrite.After(producer.LastWrite) {
			producer.LastWrite = p.LastWrite.UTC()
		}
		producer.InTransaction = producer.InTransaction || p.OpenFrom >= 0
	}
	for _, producer := range byID {
		out.Producers = append(out.Producers, *producer)
	}
	slices.SortFunc(out.Producers, func(a, b api.TopicProducer) int {
		return cmp.Or(b.LastWrite.Compare(a.LastWrite), cmp.Compare(a.ProducerId, b.ProducerId))
	})
	return out, nil
}

// transactionsSummary says how many transactions are open, and since when
// the oldest, or nil when Kafka does not describe them.
func (s *Server) transactionsSummary(ctx context.Context, customerID string) *api.TransactionsSummary {
	transactions, err := s.clusters.Transactions(ctx, customerID)
	if err != nil {
		s.logger.WarnContext(ctx, "the transactions are unknown", "customer", customerID, "error", err)
		return nil
	}
	summary := &api.TransactionsSummary{Open: len(transactions)}
	for _, t := range transactions {
		if summary.OldestStartedAt == nil || t.Started.Before(*summary.OldestStartedAt) {
			summary.OldestStartedAt = ptr.To(t.Started.UTC())
		}
	}
	return summary
}
