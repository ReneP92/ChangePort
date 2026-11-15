package console

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
	"k8s.io/utils/ptr"

	"github.com/ReneP92/changeport/internal/clusters"
	"github.com/ReneP92/changeport/internal/console/api"
	"github.com/ReneP92/changeport/internal/diagnosis"
	"github.com/ReneP92/changeport/internal/events"
	"github.com/ReneP92/changeport/internal/metrics"
	"github.com/ReneP92/changeport/internal/topics"
)

// diagnosisTTL is how long an environment's diagnoses are kept, unless the
// health monitor looks less often: the overview asks every 15 seconds, and
// the rules look at minutes.
const diagnosisTTL = 30 * time.Second

// snapshotTimeout bounds one diagnosis, so that a cluster that does not
// answer, such as one whose brokers are down, holds nothing up for long.
const snapshotTimeout = 30 * time.Second

// diagnosisCache keeps each environment's last diagnoses, by customer, and
// makes requests that miss at once share one diagnosis. ttl is how long
// they stay fresh: diagnosisTTL, or until the health monitor's next look.
type diagnosisCache struct {
	mu      sync.Mutex
	entries map[string]diagnosisEntry
	running singleflight.Group
	ttl     time.Duration
}

type diagnosisEntry struct {
	at       time.Time
	findings []diagnosis.Finding
}

// ListDiagnoses implements api.StrictServerInterface (docs/diagnosis.md).
func (s *Server) ListDiagnoses(ctx context.Context, _ api.ListDiagnosesRequestObject) (api.ListDiagnosesResponseObject, error) {
	customer, _, refused, err := s.customerOf(ctx)
	switch {
	case err != nil:
		return nil, err
	case refused != nil && refused.Status == http.StatusNotFound:
		return api.ListDiagnoses404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(*refused)}, nil
	case refused != nil:
		return api.ListDiagnoses403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(*refused)}, nil
	}
	findings, err := s.diagnose(ctx, customer.Name)
	if errors.Is(err, clusters.ErrNotReady) {
		return api.ListDiagnoses409JSONResponse{NotReadyJSONResponse: api.NotReadyJSONResponse(notReadyYet)}, nil
	}
	if err != nil {
		return nil, err
	}
	out := api.DiagnosisList{
		Diagnoses:     make([]api.Diagnosis, 0, len(findings)),
		WindowSeconds: int(s.config.diagnosisWindow().Seconds()),
	}
	for _, f := range findings {
		d := api.Diagnosis{
			Id: f.ID, Rule: f.Rule, Severity: api.DiagnosisSeverity(f.Severity),
			Title: f.Title, Explanation: f.Explanation, Suggestion: f.Suggestion,
		}
		if f.Subject.Topic != "" {
			d.Topic = ptr.To(f.Subject.Topic)
		}
		if f.Subject.ConsumerGroup != "" {
			d.ConsumerGroup = ptr.To(f.Subject.ConsumerGroup)
		}
		if f.Subject.ClientID != "" {
			d.ClientId = ptr.To(f.Subject.ClientID)
		}
		out.Diagnoses = append(out.Diagnoses, d)
	}
	return api.ListDiagnoses200JSONResponse(out), nil
}

// diagnose returns the customer's diagnoses, from the cache while fresh.
func (s *Server) diagnose(ctx context.Context, customerID string) ([]diagnosis.Finding, error) {
	if findings, ok := s.diagnoses.fresh(customerID); ok {
		return findings, nil
	}
	select {
	case result := <-s.diagnosing(ctx, customerID):
		if result.Err != nil {
			return nil, result.Err
		}
		return result.Val.([]diagnosis.Finding), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// fresh returns the customer's diagnoses while they are fresh.
func (c *diagnosisCache) fresh(customerID string) ([]diagnosis.Finding, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[customerID]
	if !ok || time.Since(entry.at) >= c.freshFor() {
		return nil, false
	}
	return entry.findings, true
}

func (c *diagnosisCache) freshFor() time.Duration { return max(diagnosisTTL, c.ttl) }

// diagnosing diagnoses the customer's cluster, or joins the diagnosis
// already running, and keeps the result. The diagnosis outlives the
// request that started it, up to snapshotTimeout, so that the next one
// finds it done.
func (s *Server) diagnosing(ctx context.Context, customerID string) <-chan singleflight.Result {
	return s.diagnoses.running.DoChan(customerID, func() (any, error) {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), snapshotTimeout)
		defer cancel()
		snapshot, err := s.snapshot(ctx, customerID)
		if err != nil {
			return nil, err
		}
		findings := diagnosis.Diagnose(snapshot)
		now := time.Now()
		s.diagnoses.mu.Lock()
		defer s.diagnoses.mu.Unlock()
		// Environments nobody looks at, or that are gone, are dropped.
		for id, entry := range s.diagnoses.entries {
			if now.Sub(entry.at) >= s.diagnoses.freshFor() {
				delete(s.diagnoses.entries, id)
			}
		}
		s.diagnoses.entries[customerID] = diagnosisEntry{at: now, findings: findings}
		return findings, nil
	})
}

// diagnosisQueries are the metrics a snapshot takes.
var diagnosisQueries = []metrics.Query{
	metrics.MessagesInRecent, metrics.BytesIn, metrics.ProducerBytes, metrics.PartitionSize,
	metrics.LagSeconds, metrics.LagMessages, metrics.LagCapped, metrics.LagSecondsEarlier,
	metrics.ConsumedMessages, metrics.LostPartitions, metrics.OffsetResets,
	metrics.ProducerSends, metrics.ProducerErrors, metrics.ProducerRetries,
}

// snapshot puts together what the rules look at: what Kafka says of the
// customer's topics and groups now, their metrics over the last minutes and
// the group changes of the last hour. The platform's own topics and groups
// are left out.
func (s *Server) snapshot(ctx context.Context, customerID string) (diagnosis.Snapshot, error) {
	settings, err := s.clusters.Topics(ctx, customerID)
	if err != nil {
		return diagnosis.Snapshot{}, err
	}
	described, err := s.clusters.ConsumerGroups(ctx, customerID)
	if err != nil {
		return diagnosis.Snapshot{}, err
	}
	now, window := time.Now(), s.config.diagnosisWindow()
	values := map[metrics.Query][]metrics.Series{}
	for _, q := range diagnosisQueries {
		if values[q], err = s.metrics.Instant(ctx, customerID, q, metrics.Filter{Window: window}, now); err != nil {
			return diagnosis.Snapshot{}, err
		}
	}
	sources, _, err := s.sourceTopics(ctx, customerID)
	if err != nil {
		return diagnosis.Snapshot{}, err
	}
	changes := s.recentChanges(ctx, customerID, now)
	snapshot := buildSnapshot(settings, described, values, changes)
	snapshot.Window = window
	for i := range snapshot.Topics {
		_, snapshot.Topics[i].Source = sources[snapshot.Topics[i].Name]
	}
	return snapshot, nil
}

// changes are what the timeline says of the last while: each group's
// rebalances in the last hour, and the topics that got partitions in the
// last day.
type changes struct {
	rebalances      map[string]int
	partitionsAdded map[string]bool
}

// recentChanges reads the changes from the timeline. It is a help rather
// than a need: without it, the rules that read it find nothing, and the
// others still run.
func (s *Server) recentChanges(ctx context.Context, customerID string, now time.Time) changes {
	found := changes{rebalances: map[string]int{}, partitionsAdded: map[string]bool{}}
	read := func(since time.Duration, kind events.Kind) []events.Event {
		read, _, err := s.events.Read(ctx, events.Query{
			Tenant: customerID, Start: now.Add(-since), End: now, Kinds: []events.Kind{kind}, Limit: events.MaxEvents,
		})
		if err != nil {
			s.logger.WarnContext(ctx, "diagnoses without the timeline", "customer", customerID, "error", err)
		}
		return read
	}
	for _, change := range read(time.Hour, events.GroupRebalanced) {
		if change.Rebalance && change.ConsumerGroup != "" {
			found.rebalances[change.ConsumerGroup]++
		}
	}
	for _, change := range read(24*time.Hour, events.TopicPartitionsAdded) {
		found.partitionsAdded[change.Topic] = true
	}
	return found
}

// buildSnapshot joins Kafka's description of the topics and groups with
// their metrics, by name.
func buildSnapshot(settings []TopicSettings, described []ConsumerGroup, values map[metrics.Query][]metrics.Series, recent changes) diagnosis.Snapshot {
	var snapshot diagnosis.Snapshot
	byTopic := map[string]*diagnosis.Topic{}
	snapshot.Topics = make([]diagnosis.Topic, 0, len(settings))
	for _, t := range settings {
		if topics.Reserved(t.Name) {
			continue
		}
		snapshot.Topics = append(snapshot.Topics, diagnosis.Topic{
			Name: t.Name, Partitions: t.Partitions, RetentionMs: t.RetentionMs,
			Compacted: t.CleanupPolicy == "compact", Tiered: t.Tiered,
			PartitionsAdded: recent.partitionsAdded[t.Name],
		})
	}
	for i := range snapshot.Topics {
		byTopic[snapshot.Topics[i].Name] = &snapshot.Topics[i]
	}
	each := func(q metrics.Query, fn func(t *diagnosis.Topic, series metrics.Series)) {
		for _, series := range values[q] {
			if t, ok := byTopic[series.Labels["topic"]]; ok {
				fn(t, series)
			}
		}
	}
	each(metrics.MessagesInRecent, func(t *diagnosis.Topic, series metrics.Series) { t.MessagesInPerSecond = value(series) })
	each(metrics.BytesIn, func(t *diagnosis.Topic, series metrics.Series) { t.BytesInPerSecond = value(series) })
	each(metrics.PartitionSize, func(t *diagnosis.Topic, series metrics.Series) {
		t.PartitionBytes = append(t.PartitionBytes, value(series))
	})
	reported := map[string]float64{}
	for _, series := range values[metrics.ProducerBytes] {
		reported[series.Labels["topic"]] += value(series)
	}
	for i := range snapshot.Topics {
		t := &snapshot.Topics[i]
		t.UnreportedBytesInPerSecond = max(0, t.BytesInPerSecond-reported[t.Name])
	}

	// Groups: those Kafka describes, and those with offsets but gone from
	// its description since.
	byGroup := map[string]*diagnosis.Group{}
	group := func(name string) *diagnosis.Group {
		if g, ok := byGroup[name]; ok {
			return g
		}
		g := &diagnosis.Group{Name: name, Rebalances: recent.rebalances[name]}
		byGroup[name] = g
		return g
	}
	assigned := map[string]map[string]int{}
	for _, d := range described {
		if platformGroup(d.Name) {
			continue
		}
		g := group(d.Name)
		g.State, g.Members = d.State, len(d.Members)
		assigned[d.Name] = map[string]int{}
		for _, member := range d.Members {
			reads := 0
			for topic, partitions := range member.Assigned {
				if partitions > 0 {
					assigned[d.Name][topic]++
					reads++
				}
			}
			if reads == 0 {
				g.IdleMembers++
			}
		}
	}
	type key struct{ group, topic string }
	reads := map[key]*diagnosis.Read{}
	var order []key
	for _, series := range values[metrics.LagSeconds] {
		k := key{series.Labels["consumer_group"], series.Labels["topic"]}
		if platformGroup(k.group) || byTopic[k.topic] == nil {
			continue
		}
		group(k.group)
		reads[k] = &diagnosis.Read{Topic: k.topic, LagSeconds: value(series), AssignedMembers: assigned[k.group][k.topic]}
		order = append(order, k)
	}
	read := func(q metrics.Query, fn func(r *diagnosis.Read, v float64)) {
		for _, series := range values[q] {
			if r, ok := reads[key{series.Labels["consumer_group"], series.Labels["topic"]}]; ok {
				fn(r, value(series))
			}
		}
	}
	read(metrics.LagMessages, func(r *diagnosis.Read, v float64) { r.LagMessages = int64(v) })
	read(metrics.LagCapped, func(r *diagnosis.Read, v float64) { r.Capped = v == 1 })
	read(metrics.LagSecondsEarlier, func(r *diagnosis.Read, v float64) { r.LagSecondsEarlier = ptr.To(v) })
	read(metrics.ConsumedMessages, func(r *diagnosis.Read, v float64) { r.ConsumedPerSecond = v })
	read(metrics.LostPartitions, func(r *diagnosis.Read, v float64) { r.LostPartitions = int(v) })
	read(metrics.OffsetResets, func(r *diagnosis.Read, v float64) { r.OffsetsReset = v > 0 })
	for _, k := range order {
		g := byGroup[k.group]
		g.Reads = append(g.Reads, *reads[k])
	}
	snapshot.Groups = make([]diagnosis.Group, 0, len(byGroup))
	for _, g := range byGroup {
		snapshot.Groups = append(snapshot.Groups, *g)
	}

	producers := map[string]*diagnosis.Producer{}
	producer := func(series metrics.Series) *diagnosis.Producer {
		id := series.Labels["principal"] + "/" + series.Labels["client_id"]
		if p, ok := producers[id]; ok {
			return p
		}
		p := &diagnosis.Producer{Principal: series.Labels["principal"], ClientID: series.Labels["client_id"]}
		producers[id] = p
		return p
	}
	for _, series := range values[metrics.ProducerSends] {
		producer(series).SendsPerSecond = value(series)
	}
	for _, series := range values[metrics.ProducerErrors] {
		producer(series).ErrorsPerSecond = value(series)
	}
	for _, series := range values[metrics.ProducerRetries] {
		producer(series).RetriesPerSecond = value(series)
	}
	for _, p := range producers {
		snapshot.Producers = append(snapshot.Producers, *p)
	}
	return snapshot
}
