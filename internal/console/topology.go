package console

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"k8s.io/utils/ptr"

	"github.com/ReneP92/changeport/internal/clusters"
	"github.com/ReneP92/changeport/internal/console/api"
	"github.com/ReneP92/changeport/internal/metrics"
	"github.com/ReneP92/changeport/internal/topics"
)

// unreported is how much of a topic's bytes in no producer may report
// before the map shows clients without telemetry writing it: a share of the
// topic's traffic, and a floor, as producers' and brokers' counts differ a
// little in time and in what they count.
const (
	unreportedShare = 0.1
	unreportedFloor = 100.0
)

// GetTopology implements api.StrictServerInterface: the producers, topics
// and consumer groups of the customer's cluster and how data flows between
// them now (docs/console.md). Producers come from what they report of
// themselves (KIP-714); consumer groups from their committed offsets and
// members; traffic no producer reports is the topic's unreported bytes in.
// The platform's own clients, topics and groups are left out.
func (s *Server) GetTopology(ctx context.Context, _ api.GetTopologyRequestObject) (api.GetTopologyResponseObject, error) {
	customer, _, refused, err := s.customerOf(ctx)
	switch {
	case err != nil:
		return nil, err
	case refused != nil && refused.Status == http.StatusNotFound:
		return api.GetTopology404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(*refused)}, nil
	case refused != nil:
		return api.GetTopology403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(*refused)}, nil
	}
	settings, err := s.clusters.Topics(ctx, customer.Name)
	if errors.Is(err, clusters.ErrNotReady) {
		return api.GetTopology409JSONResponse{NotReadyJSONResponse: api.NotReadyJSONResponse(notReadyYet)}, nil
	}
	if err != nil {
		return nil, err
	}
	members, err := s.clusters.GroupMembers(ctx, customer.Name)
	if err != nil {
		return nil, err
	}
	sources, _, err := s.sourceTopics(ctx, customer.Name)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	values := map[metrics.Query][]metrics.Series{}
	for _, q := range []metrics.Query{
		metrics.BytesIn, metrics.BytesOut, metrics.ProducerBytes, metrics.ConsumerBytes, metrics.LagSeconds, metrics.LagMessages, metrics.LagCapped,
	} {
		if values[q], err = s.metrics.Instant(ctx, customer.Name, q, metrics.Filter{}, now); err != nil {
			return nil, err
		}
	}
	return api.GetTopology200JSONResponse(buildTopology(settings, members, sources, values)), nil
}

// value is a series' one value.
func value(series metrics.Series) float64 {
	if len(series.Points) == 0 {
		return 0
	}
	return series.Points[0].Value
}

// buildTopology puts the cluster's topics, its groups' members and the
// metrics together.
func buildTopology(settings []TopicSettings, members map[string][]string, sources map[string]string, values map[metrics.Query][]metrics.Series) api.Topology {
	out := api.Topology{
		Producers: []api.TopologyProducer{}, Topics: []api.TopologyTopic{}, Groups: []api.TopologyGroup{},
		Writes: []api.TopologyWrite{}, Reads: []api.TopologyRead{},
	}
	byTopic := map[string]*api.TopologyTopic{}
	for _, t := range settings {
		if topics.Reserved(t.Name) {
			continue
		}
		topic := api.TopologyTopic{Name: t.Name}
		if source, ok := sources[t.Name]; ok {
			topic.Source = ptr.To(source)
		}
		out.Topics = append(out.Topics, topic)
	}
	for i := range out.Topics {
		byTopic[out.Topics[i].Name] = &out.Topics[i]
	}
	for _, series := range values[metrics.BytesIn] {
		if topic, ok := byTopic[series.Labels["topic"]]; ok {
			topic.BytesInPerSecond = float32(value(series))
		}
	}
	for _, series := range values[metrics.BytesOut] {
		if topic, ok := byTopic[series.Labels["topic"]]; ok {
			topic.BytesOutPerSecond = float32(value(series))
		}
	}

	// Producers, by Kafka user and client ID, and what each writes where.
	producers := map[string]*api.TopologyProducer{}
	reported := map[string]float64{}
	for _, series := range values[metrics.ProducerBytes] {
		topic, principal, clientID := series.Labels["topic"], series.Labels["principal"], series.Labels["client_id"]
		if _, ok := byTopic[topic]; !ok {
			continue
		}
		id := principal + "/" + clientID
		producer, ok := producers[id]
		if !ok {
			producer = &api.TopologyProducer{Id: id, ClientId: clientID, Principal: principal}
			producers[id] = producer
		}
		rate := value(series)
		producer.BytesPerSecond += float32(rate)
		reported[topic] += rate
		out.Writes = append(out.Writes, api.TopologyWrite{Producer: id, Topic: topic, BytesPerSecond: float32(rate)})
	}
	for _, producer := range producers {
		out.Producers = append(out.Producers, *producer)
	}
	for i := range out.Topics {
		topic := &out.Topics[i]
		unreported := float64(topic.BytesInPerSecond) - reported[topic.Name]
		if unreported > unreportedFloor && unreported > unreportedShare*float64(topic.BytesInPerSecond) {
			topic.UnreportedBytesInPerSecond = float32(unreported)
		}
	}

	// Consumer groups: those with committed offsets on the topics they read,
	// and those whose members have committed nothing yet.
	type read struct{ group, topic string }
	reads := map[read]*api.TopologyRead{}
	groups := map[string]*api.TopologyGroup{}
	group := func(name string) *api.TopologyGroup {
		g, ok := groups[name]
		if !ok {
			g = &api.TopologyGroup{Name: name, Members: len(members[name])}
			groups[name] = g
		}
		return g
	}
	for _, series := range values[metrics.LagSeconds] {
		key := read{series.Labels["consumer_group"], series.Labels["topic"]}
		if _, ok := byTopic[key.topic]; !ok || platformGroup(key.group) {
			continue
		}
		seconds := value(series)
		reads[key] = &api.TopologyRead{Group: key.group, Topic: key.topic, LagSeconds: float32(seconds)}
		g := group(key.group)
		if g.LagSeconds == nil || float64(*g.LagSeconds) < seconds {
			g.LagSeconds = ptr.To(float32(seconds))
		}
	}
	for _, series := range values[metrics.LagMessages] {
		if r, ok := reads[read{series.Labels["consumer_group"], series.Labels["topic"]}]; ok {
			r.LagMessages = int64(value(series))
			g := groups[r.Group]
			g.LagMessages = ptr.To(ptr.Deref(g.LagMessages, 0) + r.LagMessages)
		}
	}
	for _, series := range values[metrics.LagCapped] {
		if r, ok := reads[read{series.Labels["consumer_group"], series.Labels["topic"]}]; ok {
			r.Capped = value(series) == 1
		}
	}
	for name := range members {
		if !platformGroup(name) && len(members[name]) > 0 {
			group(name)
		}
	}
	// What a group's members report reading of each topic.
	groupsOf := map[string][]string{}
	for name, clientIDs := range members {
		for _, clientID := range clientIDs {
			groupsOf[clientID] = append(groupsOf[clientID], name)
		}
	}
	for _, series := range values[metrics.ConsumerBytes] {
		topic, clientID := series.Labels["topic"], series.Labels["client_id"]
		for _, name := range groupsOf[clientID] {
			if r, ok := reads[read{name, topic}]; ok {
				r.BytesPerSecond = ptr.To(ptr.Deref(r.BytesPerSecond, 0) + float32(value(series)))
			}
		}
	}
	for _, g := range groups {
		out.Groups = append(out.Groups, *g)
	}
	for _, r := range reads {
		out.Reads = append(out.Reads, *r)
	}

	slices.SortFunc(out.Producers, func(a, b api.TopologyProducer) int { return cmp.Compare(a.Id, b.Id) })
	slices.SortFunc(out.Topics, func(a, b api.TopologyTopic) int { return cmp.Compare(a.Name, b.Name) })
	slices.SortFunc(out.Groups, func(a, b api.TopologyGroup) int { return cmp.Compare(a.Name, b.Name) })
	slices.SortFunc(out.Writes, func(a, b api.TopologyWrite) int {
		return cmp.Or(cmp.Compare(a.Producer, b.Producer), cmp.Compare(a.Topic, b.Topic))
	})
	slices.SortFunc(out.Reads, func(a, b api.TopologyRead) int {
		return cmp.Or(cmp.Compare(a.Group, b.Group), cmp.Compare(a.Topic, b.Topic))
	})
	return out
}
