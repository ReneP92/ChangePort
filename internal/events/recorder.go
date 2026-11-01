package events

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Store is where a Recorder writes events.
type Store interface {
	Write(ctx context.Context, events ...Event) error
}

// Recorder records events without waiting for the store: it keeps them in
// memory and writes them in batches, again and again while the store is
// unavailable, so that a change never waits for, or fails on, its timeline.
// Past its capacity it drops the oldest events.
type Recorder struct {
	store  Store
	logger *slog.Logger
	// capacity is how many events wait at most.
	capacity int
	// every is how long the recorder waits after a failed write, doubling
	// with each further one up to a minute.
	every time.Duration

	mu      sync.Mutex
	pending []Event
	wake    chan struct{}

	written, dropped, failures prometheus.Counter
}

// RecorderOptions configure a Recorder.
type RecorderOptions struct {
	// Capacity is how many events wait at most; 10,000 by default.
	Capacity int
	// Every is how long the recorder waits after a failed write, doubling
	// with each further one up to a minute; a second by default.
	Every time.Duration
	// Registry, if any, gets the recorder's metrics.
	Registry prometheus.Registerer
}

// NewRecorder returns a recorder that writes to store once Run runs.
func NewRecorder(store Store, logger *slog.Logger, options RecorderOptions) *Recorder {
	r := &Recorder{
		store: store, logger: logger, capacity: options.Capacity, every: options.Every, wake: make(chan struct{}, 1),
		written: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "changeport_events_written_total", Help: "Events written to the timeline's store.",
		}),
		dropped: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "changeport_events_dropped_total", Help: "Events dropped because too many waited for the timeline's store.",
		}),
		failures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "changeport_events_write_failures_total", Help: "Failed writes to the timeline's store, which are retried.",
		}),
	}
	if r.capacity <= 0 {
		r.capacity = 10_000
	}
	if r.every <= 0 {
		r.every = time.Second
	}
	if options.Registry != nil {
		options.Registry.MustRegister(r.written, r.dropped, r.failures)
	}
	return r
}

// Record keeps events for the next write. Events without an ID or a time
// get them.
func (r *Recorder) Record(events ...Event) {
	r.mu.Lock()
	for _, e := range events {
		if e.ID == "" {
			e.ID = NewID()
		}
		if e.Time.IsZero() {
			e.Time = time.Now()
		}
		r.pending = append(r.pending, e)
	}
	if over := len(r.pending) - r.capacity; over > 0 {
		r.pending = append(r.pending[:0], r.pending[over:]...)
		r.dropped.Add(float64(over))
	}
	r.mu.Unlock()
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// maxBatch is the most events one write sends.
const maxBatch = 500

// Run writes the recorded events as they come until ctx is done, waiting
// longer after each failed write, then tries once more for a few seconds.
func (r *Recorder) Run(ctx context.Context) {
	backoff := r.every
	for {
		wake := r.wake
		if r.flushAll(ctx) {
			backoff = r.every
		} else {
			// Wait out the backoff rather than write at each new event.
			backoff = min(2*backoff, time.Minute)
			wake = nil
		}
		select {
		case <-ctx.Done():
			flush, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			r.flushAll(flush)
			cancel()
			return
		case <-wake:
		case <-time.After(backoff):
		}
	}
}

// flushAll writes the waiting events, and reports whether it could.
func (r *Recorder) flushAll(ctx context.Context) bool {
	for {
		r.mu.Lock()
		empty := len(r.pending) == 0
		r.mu.Unlock()
		if empty {
			return true
		}
		if !r.flush(ctx) {
			return false
		}
	}
}

// flush writes a batch of waiting events, and reports whether it could.
func (r *Recorder) flush(ctx context.Context) bool {
	r.mu.Lock()
	batch := r.pending[:min(len(r.pending), maxBatch)]
	batch = append([]Event(nil), batch...)
	r.mu.Unlock()
	if len(batch) == 0 {
		return true
	}
	if err := r.store.Write(ctx, batch...); err != nil {
		r.failures.Inc()
		if ctx.Err() == nil {
			r.logger.Warn("could not write events to the timeline; trying again", "events", len(batch), "error", err)
		}
		return false
	}
	r.written.Add(float64(len(batch)))
	r.mu.Lock()
	// Events dropped meanwhile may have shifted the batch; drop what was
	// written by ID.
	written := make(map[string]bool, len(batch))
	for _, e := range batch {
		written[e.ID] = true
	}
	kept := r.pending[:0]
	for _, e := range r.pending {
		if !written[e.ID] {
			kept = append(kept, e)
		}
	}
	r.pending = kept
	r.mu.Unlock()
	return true
}
