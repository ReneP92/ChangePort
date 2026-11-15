package console

import (
	"sync"
	"time"
)

// producerMemory keeps what the console saw of each customer's producers
// at its last look, so that the diagnoses tell what persists and what moves
// on by Kafka's own counts. A producer's timestamps are its own clock, which
// a replay, a mirror or a stream processor sets in the past, so they tell
// neither.
type producerMemory struct {
	mu        sync.Mutex
	customers map[string]*producerLook
}

// producerLook is one customer's producers at the console's last look.
type producerLook struct {
	at time.Time
	// sequences are each producer's last sequence on each partition, by
	// its epoch too, as a new epoch starts its sequences again.
	sequences map[producerKey]int32
	// openSince is when each transaction open on a partition was first
	// seen open there.
	openSince map[openKey]time.Time
}

type producerKey struct {
	topic      string
	partition  int32
	producerID int64
	epoch      int16
}

type openKey struct {
	topic      string
	partition  int32
	producerID int64
	from       int64
}

// lookAgain is how recent the last look must be for what moved since to
// say who writes now.
const lookAgain = 5 * time.Minute

// seen is what a look at a customer's producers tells, against the last.
type seen struct {
	// wrote are the producers that wrote since the last look: their
	// sequence moved on, or they are new. Nil when there was no recent
	// last look to compare with.
	wrote map[producerKey]bool
	// openSince is when each transaction open now was first seen open.
	openSince map[openKey]time.Time
}

func newProducerMemory() *producerMemory {
	return &producerMemory{customers: map[string]*producerLook{}}
}

// look remembers a customer's producers as Kafka describes them at now, and
// returns what moved on since the last look, and since when each open
// transaction has been open. Transactions no longer open are forgotten.
func (m *producerMemory) look(customerID string, producers []PartitionProducer, now time.Time) seen {
	m.mu.Lock()
	defer m.mu.Unlock()
	last := m.customers[customerID]
	look := &producerLook{at: now, sequences: map[producerKey]int32{}, openSince: map[openKey]time.Time{}}
	var result seen
	recent := last != nil && now.Sub(last.at) <= lookAgain && now.After(last.at)
	if recent {
		result.wrote = map[producerKey]bool{}
	}
	result.openSince = look.openSince
	for _, p := range producers {
		key := producerKey{p.Topic, p.Partition, p.ProducerID, p.Epoch}
		look.sequences[key] = p.LastSequence
		if recent {
			if before, ok := last.sequences[key]; !ok || before != p.LastSequence {
				result.wrote[key] = true
			}
		}
		if p.OpenFrom < 0 {
			continue
		}
		open := openKey{p.Topic, p.Partition, p.ProducerID, p.OpenFrom}
		look.openSince[open] = now
		if last != nil {
			if since, ok := last.openSince[open]; ok {
				look.openSince[open] = since
			}
		}
	}
	m.customers[customerID] = look
	return result
}
