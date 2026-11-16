package console

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"k8s.io/utils/ptr"

	"github.com/ReneP92/changeport/api/v1alpha1"
	"github.com/ReneP92/changeport/internal/clusters"
	"github.com/ReneP92/changeport/internal/console/api"
	"github.com/ReneP92/changeport/internal/events"
	"github.com/ReneP92/changeport/internal/metrics"
	"github.com/ReneP92/changeport/internal/provisioning"
	"github.com/ReneP92/changeport/internal/topics"
)

// Position is where an admin moves a consumer group: the earliest message
// kept, the latest, or the first at or after a time.
type Position struct {
	To   api.PositionChangeTo
	Time time.Time
}

// PartitionPosition is where a consumer group is on one partition, and
// where a Position puts it.
type PartitionPosition struct {
	Topic     string
	Partition int32
	// Committed is the group's committed offset, or -1 without one.
	Committed int64
	// Start is the partition's first offset kept and End the next one to be
	// written; LocalStart is its first offset on the brokers' disks, those
	// before it being in tiered storage.
	Start, End, LocalStart int64
	// Target is the offset the Position puts the group at, and TargetTime
	// the time of the message there; zero at End, where there is none yet.
	Target     int64
	TargetTime time.Time
}

// ErrGroupActive means Kafka refused to move a consumer group because it
// has members, which commit their own positions.
var ErrGroupActive = errors.New("the consumer group has members")

// GroupPositions implements Clusters.
func (k KubeClusters) GroupPositions(ctx context.Context, customerID, group string, names []string, to Position) ([]PartitionPosition, error) {
	admin, err := k.Kafka.Admin(ctx, customerID)
	if err != nil {
		return nil, err
	}
	positions, err := groupPositions(ctx, admin, group, names, to)
	if err != nil {
		return nil, fmt.Errorf("customer %s: %w", customerID, err)
	}
	return positions, nil
}

// groupPositions is GroupPositions with an admin client.
func groupPositions(ctx context.Context, admin *kadm.Client, group string, names []string, to Position) ([]PartitionPosition, error) {
	committed, err := admin.FetchOffsets(ctx, group)
	if errors.Is(err, kerr.GroupIDNotFound) {
		// A group Kafka does not know has no positions yet.
		committed, err = kadm.OffsetResponses{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetch the offsets of group %s: %w", group, err)
	}
	if len(names) == 0 {
		// The group's own topics, not the platform's it may read too.
		names = slices.DeleteFunc(slices.Sorted(maps.Keys(committed)), topics.Reserved)
	}
	if len(names) == 0 {
		return nil, nil
	}
	type lister func(context.Context, ...string) (kadm.ListedOffsets, error)
	list := func(what string, fn lister) (kadm.ListedOffsets, error) {
		listed, err := fn(ctx, names...)
		if err == nil {
			err = listed.Error()
		}
		if errors.Is(err, kerr.UnknownTopicOrPartition) {
			return nil, fmt.Errorf("topics %s: %w", strings.Join(names, ", "), ErrNoSuchTopic)
		}
		if err != nil {
			return nil, fmt.Errorf("list the %s offsets: %w", what, err)
		}
		return listed, nil
	}
	// The first message at or after a time, which gives its time too; from
	// the epoch, that is the earliest message kept. Kafka reads negative
	// times as special lookups, so earlier times start at the epoch. Listed
	// before the ends, so that under a busy producer no target passes its
	// partition's end.
	var after kadm.ListedOffsets
	switch to.To {
	case api.PositionChangeToEarliest:
		after, err = list("earliest", afterMilli(admin, 0))
	case api.PositionChangeToTime:
		after, err = list("timed", afterMilli(admin, max(to.Time.UnixMilli(), 0)))
	}
	if err != nil {
		return nil, err
	}
	starts, err := list("start", admin.ListStartOffsets)
	if err != nil {
		return nil, err
	}
	locals, err := list("local start", admin.ListLocalLogStartOffsets)
	if err != nil {
		return nil, err
	}
	ends, err := list("end", admin.ListEndOffsets)
	if err != nil {
		return nil, err
	}
	var positions []PartitionPosition
	for _, topic := range names {
		for _, partition := range slices.Sorted(maps.Keys(ends[topic])) {
			end := ends[topic][partition].Offset
			p := PartitionPosition{Topic: topic, Partition: partition, Committed: -1, End: end, Target: end}
			p.Start, p.LocalStart = starts[topic][partition].Offset, locals[topic][partition].Offset
			if c, ok := committed[topic][partition]; ok && c.Err == nil {
				p.Committed = c.At
			}
			if a, ok := after.Lookup(topic, partition); ok && a.Offset >= 0 {
				p.Target = min(a.Offset, end)
				if to.To == api.PositionChangeToEarliest {
					p.Target = p.Start
				}
				if p.Target < end && a.Timestamp >= 0 {
					p.TargetTime = time.UnixMilli(a.Timestamp).UTC()
				}
			}
			positions = append(positions, p)
		}
	}
	return positions, nil
}

// afterMilli lists the first offsets at or after a millisecond.
func afterMilli(admin *kadm.Client, millisecond int64) func(context.Context, ...string) (kadm.ListedOffsets, error) {
	return func(ctx context.Context, topics ...string) (kadm.ListedOffsets, error) {
		return admin.ListOffsetsAfterMilli(ctx, millisecond, topics...)
	}
}

// CommitGroupOffsets implements Clusters, as the offsets user.
func (k KubeClusters) CommitGroupOffsets(ctx context.Context, customerID, group string, positions []PartitionPosition) ([]PartitionPosition, error) {
	admin, err := k.Kafka.Offsets(ctx, customerID)
	if err != nil {
		return nil, err
	}
	committed, err := commitGroupOffsets(ctx, admin, group, positions)
	if err != nil {
		return committed, fmt.Errorf("customer %s: %w", customerID, err)
	}
	return committed, nil
}

// commitGroupOffsets is CommitGroupOffsets with an admin client.
func commitGroupOffsets(ctx context.Context, admin *kadm.Client, group string, positions []PartitionPosition) ([]PartitionPosition, error) {
	offsets := make(kadm.Offsets)
	for _, p := range positions {
		offsets.Add(kadm.Offset{Topic: p.Topic, Partition: p.Partition, At: p.Target, LeaderEpoch: -1})
	}
	responses, err := admin.CommitOffsets(ctx, group, offsets)
	if err != nil {
		return nil, fmt.Errorf("commit the offsets of group %s: %w", group, err)
	}
	var committed []PartitionPosition
	var failed error
	for _, p := range positions {
		response, ok := responses[p.Topic][p.Partition]
		switch {
		case !ok:
			failed = cmp.Or(failed, fmt.Errorf("partition %s-%d is missing from the response", p.Topic, p.Partition))
		case response.Err != nil:
			failed = cmp.Or(failed, response.Err)
		default:
			committed = append(committed, p)
		}
	}
	for _, active := range []error{kerr.UnknownMemberID, kerr.IllegalGeneration, kerr.RebalanceInProgress, kerr.StaleMemberEpoch, kerr.FencedMemberEpoch} {
		if errors.Is(failed, active) {
			return committed, fmt.Errorf("group %s: %w (%w)", group, ErrGroupActive, failed)
		}
	}
	if failed != nil {
		return committed, fmt.Errorf("commit the offsets of group %s: %w", group, failed)
	}
	return committed, nil
}

// groupName is what the console lets a replay's new group be called.
var groupName = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,200}$`)

var groupAdminsOnly = problem(http.StatusForbidden, "Admins only", "Ask an admin of your organization to move consumer groups.")

// positionPlan is a move of a consumer group, or a replay into a new one,
// checked and worked out.
type positionPlan struct {
	customer  *v1alpha1.Customer
	principal Principal
	position  Position
	// from is the group in the request's path; group is the one that moves,
	// the same or, for a replay, the new one.
	from, group string
	members     []string
	positions   []PartitionPosition
}

// planPosition checks a move or a replay and works out where the group
// goes on every partition of its topics, or says why it cannot.
func (s *Server) planPosition(ctx context.Context, from string, body *api.PositionChange, replay bool) (*positionPlan, *api.Problem, error) {
	customer, principal, refused, err := s.customerOf(ctx)
	switch {
	case err != nil:
		return nil, nil, err
	case refused != nil:
		return nil, refused, nil
	case !principal.Admin():
		return nil, &groupAdminsOnly, nil
	case platformGroup(from):
		return nil, ptr.To(noSuchGroup(from)), nil
	case body == nil:
		return nil, ptr.To(problem(http.StatusBadRequest, "Say where the group goes", "Choose the earliest message, the latest or a time.")), nil
	}
	plan := &positionPlan{customer: customer, principal: principal, position: Position{To: body.To}, from: from, group: from}
	field := func(name, title, detail string) (*positionPlan, *api.Problem, error) {
		p := problem(http.StatusBadRequest, title, detail)
		p.Field = ptr.To(name)
		return nil, &p, nil
	}
	switch body.To {
	case api.PositionChangeToEarliest, api.PositionChangeToLatest:
	case api.PositionChangeToTime:
		if body.Time == nil {
			return field("time", "Choose a time", "Say from when the group reads.")
		}
		// A minute's grace for the browser's clock.
		if body.Time.After(time.Now().Add(time.Minute)) {
			return field("time", "The time is in the future", "Choose a time up to now.")
		}
		plan.position.Time = *body.Time
	default:
		return field("to", "Choose where the group goes", "The earliest message, the latest or a time.")
	}
	if replay {
		into := ptr.Deref(body.Into, "")
		switch {
		case into == "":
			return field("into", "Name the new group", "A replay reads into a new consumer group, which your service then uses.")
		case !groupName.MatchString(into):
			return field("into", "The name is not allowed", "Use letters, digits, dots, dashes and underscores, at most 200.")
		case topics.Reserved(into) || into == from:
			return field("into", "The name is taken", fmt.Sprintf(
				"Choose a name other than %s, and not one starting with %s, which belong to Kafka and the platform.",
				from, listOr(topics.ReservedPrefixes)))
		}
		plan.group = into
	} else if body.Into != nil {
		return field("into", "Replays create a group", "Leave out into to move the group itself.")
	}
	var names []string
	for _, topic := range ptr.Deref(body.Topics, nil) {
		if topics.Reserved(topic) {
			return nil, ptr.To(noSuchTopic(topic)), nil
		}
		if !slices.Contains(names, topic) {
			names = append(names, topic)
		}
	}

	groups, err := s.clusters.ConsumerGroups(ctx, customer.Name)
	if errors.Is(err, clusters.ErrNotReady) {
		return nil, &notReadyYet, nil
	}
	if err != nil {
		return nil, nil, err
	}
	i := slices.IndexFunc(groups, func(g ConsumerGroup) bool { return g.Name == from })
	if i < 0 {
		return nil, ptr.To(noSuchGroup(from)), nil
	}
	if replay && slices.ContainsFunc(groups, func(g ConsumerGroup) bool { return g.Name == plan.group }) {
		p := problem(http.StatusConflict, "The group exists", fmt.Sprintf("There is a consumer group %s already. Choose another name.", plan.group))
		p.Field = ptr.To("into")
		return nil, &p, nil
	}
	if !replay {
		plan.members = groups[i].ClientIDs()
	}
	plan.positions, err = s.clusters.GroupPositions(ctx, customer.Name, from, names, plan.position)
	switch {
	case errors.Is(err, clusters.ErrNotReady):
		return nil, &notReadyYet, nil
	case errors.Is(err, ErrNoSuchTopic):
		detail := fmt.Sprintf("A topic consumer group %s has a position on no longer exists. Name its topics that do.", from)
		if len(names) > 0 {
			detail = fmt.Sprintf("Not every one of %s exists.", strings.Join(names, ", "))
		}
		return nil, ptr.To(problem(http.StatusNotFound, "No such topic", detail)), nil
	case err != nil:
		return nil, nil, err
	case len(plan.positions) == 0:
		return field("topics", "Choose the topics", fmt.Sprintf("Consumer group %s has no position on any topic yet; say which topics to move it on.", from))
	}
	if replay {
		// Kafka lets anyone commit to a group without members, so a group
		// the description missed, as when describing it failed, must not
		// have its positions overwritten either.
		existing, err := s.clusters.GroupPositions(ctx, customer.Name, plan.group, nil, Position{To: api.PositionChangeToLatest})
		if err != nil && !errors.Is(err, ErrNoSuchTopic) {
			return nil, nil, err
		}
		if len(existing) > 0 || err != nil {
			p := problem(http.StatusConflict, "The group exists", fmt.Sprintf("Consumer group %s has positions already. Choose another name.", plan.group))
			p.Field = ptr.To("into")
			return nil, &p, nil
		}
		// The new group has no position of its own.
		for i := range plan.positions {
			plan.positions[i].Committed = -1
		}
	}
	return plan, nil, nil
}

// preview says what the plan's positions mean, with how long reading
// history back from tiered storage takes at least.
func (s *Server) preview(ctx context.Context, plan *positionPlan, positions []PartitionPosition) api.PositionPreview {
	preview := api.PositionPreview{Group: plan.group, Members: plan.members, Topics: topicPositions(positions)}
	if preview.Members == nil {
		preview.Members = []string{}
	}
	if !slices.ContainsFunc(preview.Topics, func(t api.TopicPosition) bool { return t.TieredMessages > 0 }) {
		return preview
	}
	profile, err := provisioning.ProfileByName(plan.customer.Spec.Profile)
	if err != nil {
		return preview
	}
	series, err := s.metrics.Instant(ctx, plan.customer.Name, metrics.LocalSize, metrics.Filter{}, time.Now())
	if err != nil {
		s.logger.WarnContext(ctx, "reading the topics' sizes failed", "tenant", plan.customer.Name, "error", err)
		return preview
	}
	sizes := map[string]float64{}
	for _, one := range series {
		sizes[one.Labels["topic"]] += value(one)
	}
	preview.TieredSeconds = tieredSeconds(positions, sizes, profile.RemoteFetchBytesPerSecond, profile.Replicas)
	return preview
}

// topicPositions sums partitions' positions up per topic, in their order.
func topicPositions(positions []PartitionPosition) []api.TopicPosition {
	result := []api.TopicPosition{}
	for _, p := range positions {
		if len(result) == 0 || result[len(result)-1].Topic != p.Topic {
			result = append(result, api.TopicPosition{Topic: p.Topic})
		}
		t := &result[len(result)-1]
		t.Partitions++
		t.ToRead += p.End - p.Target
		if p.LocalStart > p.Target {
			t.TieredMessages += min(p.LocalStart, p.End) - p.Target
		}
		if p.Committed >= 0 {
			// Before the start, the messages are gone either way.
			at := max(p.Committed, p.Start)
			t.ReadAgain += max(at-p.Target, 0)
			t.Skipped += max(p.Target-at, 0)
		}
		if !p.TargetTime.IsZero() && (t.From == nil || p.TargetTime.Before(*t.From)) {
			t.From = ptr.To(p.TargetTime)
		}
	}
	return result
}

// tieredSeconds is at least how long reading the positions' messages from
// tiered storage takes, when every broker reads at its limit. A topic's
// messages are taken to be as large as those on the brokers' disks, from its
// local sizes: the disks hold its offsets from the local start, while
// tiered storage holds an unknown share of them too. It is nil when no
// message comes from there or their size is unknown.
func tieredSeconds(positions []PartitionPosition, localSizes map[string]float64, limit int64, brokers int) *int64 {
	local, tiered := map[string]int64{}, map[string]int64{}
	partitions := 0
	for _, p := range positions {
		local[p.Topic] += p.End - p.LocalStart
		if p.LocalStart > p.Target {
			tiered[p.Topic] += min(p.LocalStart, p.End) - p.Target
			partitions++
		}
	}
	var bytes float64
	for topic, n := range tiered {
		if local[topic] <= 0 || localSizes[topic] <= 0 {
			return nil
		}
		bytes += float64(n) * localSizes[topic] / float64(local[topic])
	}
	if bytes == 0 || limit <= 0 {
		return nil
	}
	// Partitions' leaders spread over the brokers, each reading at its limit.
	readers := max(min(brokers, partitions), 1)
	return ptr.To(int64(math.Ceil(bytes / float64(limit*int64(readers)))))
}

// PreviewGroupPosition implements api.StrictServerInterface.
func (s *Server) PreviewGroupPosition(ctx context.Context, request api.PreviewGroupPositionRequestObject) (api.PreviewGroupPositionResponseObject, error) {
	replay := request.Body != nil && request.Body.Into != nil
	plan, refused, err := s.planPosition(ctx, request.Group, request.Body, replay)
	switch {
	case err != nil:
		return nil, err
	case refused == nil:
		return api.PreviewGroupPosition200JSONResponse(s.preview(ctx, plan, plan.positions)), nil
	case refused.Status == http.StatusBadRequest:
		return api.PreviewGroupPosition400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse(*refused)}, nil
	case refused.Status == http.StatusForbidden:
		return api.PreviewGroupPosition403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(*refused)}, nil
	case refused.Status == http.StatusNotFound:
		return api.PreviewGroupPosition404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(*refused)}, nil
	}
	return api.PreviewGroupPosition409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(*refused)}, nil
}

// MoveGroup implements api.StrictServerInterface: organization admins move
// a consumer group without members to the earliest message, the latest or
// a time, and every move is audited and recorded on the timeline.
func (s *Server) MoveGroup(ctx context.Context, request api.MoveGroupRequestObject) (api.MoveGroupResponseObject, error) {
	plan, refused, err := s.planPosition(ctx, request.Group, request.Body, false)
	if err == nil && refused == nil && len(plan.members) > 0 {
		refused = ptr.To(groupHasMembers(plan.group, plan.members))
	}
	var preview api.PositionPreview
	if err == nil && refused == nil {
		preview, refused, err = s.commitPosition(ctx, plan)
	}
	switch {
	case err != nil:
		return nil, err
	case refused == nil:
		return api.MoveGroup200JSONResponse(preview), nil
	case refused.Status == http.StatusBadRequest:
		return api.MoveGroup400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse(*refused)}, nil
	case refused.Status == http.StatusForbidden:
		return api.MoveGroup403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(*refused)}, nil
	case refused.Status == http.StatusNotFound:
		return api.MoveGroup404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(*refused)}, nil
	}
	return api.MoveGroup409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(*refused)}, nil
}

// ReplayGroup implements api.StrictServerInterface: organization admins
// create a consumer group at a point in the history of another group's
// topics, and every replay is audited and recorded on the timeline.
func (s *Server) ReplayGroup(ctx context.Context, request api.ReplayGroupRequestObject) (api.ReplayGroupResponseObject, error) {
	plan, refused, err := s.planPosition(ctx, request.Group, request.Body, true)
	var preview api.PositionPreview
	if err == nil && refused == nil {
		preview, refused, err = s.commitPosition(ctx, plan)
	}
	switch {
	case err != nil:
		return nil, err
	case refused == nil:
		return api.ReplayGroup201JSONResponse(preview), nil
	case refused.Status == http.StatusBadRequest:
		return api.ReplayGroup400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse(*refused)}, nil
	case refused.Status == http.StatusForbidden:
		return api.ReplayGroup403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(*refused)}, nil
	case refused.Status == http.StatusNotFound:
		return api.ReplayGroup404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(*refused)}, nil
	}
	return api.ReplayGroup409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(*refused)}, nil
}

// commitPosition commits the plan's offsets to its group. What Kafka
// commits is audited and recorded, however the request ends.
func (s *Server) commitPosition(ctx context.Context, plan *positionPlan) (api.PositionPreview, *api.Problem, error) {
	ctx = context.WithoutCancel(ctx)
	committed, err := s.clusters.CommitGroupOffsets(ctx, plan.customer.Name, plan.group, plan.positions)
	preview := s.preview(ctx, plan, committed)
	if len(committed) > 0 {
		s.auditPosition(ctx, plan, preview)
		s.recordPosition(ctx, plan, preview)
	}
	switch {
	case errors.Is(err, clusters.ErrNotReady):
		return preview, &notReadyYet, nil
	case errors.Is(err, ErrGroupActive):
		p := groupHasMembers(plan.group, nil)
		if len(committed) > 0 {
			p.Detail = ptr.To(fmt.Sprintf("Consumers joined %s meanwhile, so it moved on %s only. Stop its consumers, then move it again.", plan.group, plural(len(committed), "partition")))
		}
		return preview, &p, nil
	case err != nil && len(committed) > 0:
		s.logger.ErrorContext(ctx, "moving a consumer group partly failed", "tenant", plan.customer.Name, "group", plan.group, "error", err)
		return preview, ptr.To(problem(http.StatusConflict, "The group moved on some partitions only",
			fmt.Sprintf("Kafka moved %s on %s of %d. Look at it again, and try again.", plan.group, plural(len(committed), "partition"), len(plan.positions)))), nil
	case err != nil:
		return preview, nil, err
	}
	return preview, nil, nil
}

// groupHasMembers is why Kafka does not move a group that has consumers.
func groupHasMembers(group string, members []string) api.Problem {
	detail := fmt.Sprintf("Consumers are reading %s, and they keep their own position. Stop them, then move it.", group)
	if len(members) > 0 {
		detail = fmt.Sprintf("%s reading %s, and they keep their own position. Stop them, then move it.", strings.Join(members, ", "), group)
		if len(members) == 1 {
			detail = fmt.Sprintf("%s is reading %s, and it keeps its own position. Stop it, then move the group.", members[0], group)
		}
	}
	return problem(http.StatusConflict, "Stop its consumers first", detail)
}

// totals are the plan's messages read again, skipped and to read, over its
// topics.
func totals(preview api.PositionPreview) (readAgain, skipped, toRead int64) {
	for _, t := range preview.Topics {
		readAgain += t.ReadAgain
		skipped += t.Skipped
		toRead += t.ToRead
	}
	return readAgain, skipped, toRead
}

// auditPosition logs a move or a replay with who made it.
func (s *Server) auditPosition(ctx context.Context, plan *positionPlan, preview api.PositionPreview) {
	readAgain, skipped, toRead := totals(preview)
	names := make([]string, 0, len(preview.Topics))
	for _, t := range preview.Topics {
		names = append(names, t.Topic)
	}
	change := "consumer group moved"
	if plan.group != plan.from {
		change = "consumer group replayed"
	}
	// Where the group was, as kafka-consumer-groups.sh --reset-offsets
	// --from-file reads it, to undo the move by (docs/runbook.md).
	var before []string
	for _, p := range plan.positions {
		if p.Committed >= 0 {
			before = append(before, fmt.Sprintf("%s,%d,%d", p.Topic, p.Partition, p.Committed))
		}
	}
	s.audit(ctx, plan.principal, plan.customer.Name, change, "group", plan.group, "from_group", plan.from,
		"to", string(plan.position.To), "time", plan.position.Time, "topics", names,
		"read_again", readAgain, "skipped", skipped, "to_read", toRead, "offsets_before", strings.Join(before, ";"))
}

// recordPosition keeps a move or a replay for the timeline, on the group
// whose position changed, with who made it.
func (s *Server) recordPosition(ctx context.Context, plan *positionPlan, preview api.PositionPreview) {
	if s.recorder == nil {
		return
	}
	actor := s.userNames.name(ctx, plan.principal.UserID)
	readAgain, skipped, toRead := totals(preview)
	replay := plan.group != plan.from
	where := positionWords(plan.position, replay)
	event := events.Event{Time: time.Now(), Tenant: plan.customer.Name, ConsumerGroup: plan.group, Actor: actor, Source: "console"}
	if replay {
		event.Kind = events.GroupReplayed
		event.Message = fmt.Sprintf("%s created consumer group %s in the console, to read what %s reads %s: %s.",
			actor, plan.group, plan.from, where, messages(toRead))
	} else {
		event.Kind = events.GroupMoved
		effect := "nothing to read again or skip"
		switch {
		case readAgain > 0 && skipped > 0:
			effect = fmt.Sprintf("it reads %s again and skips %s", messages(readAgain), messages(skipped))
		case readAgain > 0:
			effect = fmt.Sprintf("it reads %s again", messages(readAgain))
		case skipped > 0:
			effect = "it skips " + messages(skipped)
		}
		event.Message = fmt.Sprintf("%s moved consumer group %s %s in the console; %s.", actor, plan.group, where, effect)
	}
	s.recorder.Record(event)
}

// positionWords says where a position is, after "moved it" or, for a
// replay, "to read it".
func positionWords(p Position, replay bool) string {
	switch {
	case p.To == api.PositionChangeToEarliest && replay:
		return "from the earliest message kept"
	case p.To == api.PositionChangeToEarliest:
		return "to the earliest message kept"
	case p.To == api.PositionChangeToLatest && replay:
		return "from now on"
	case p.To == api.PositionChangeToLatest:
		return "to the latest message"
	case replay:
		return "from " + p.Time.UTC().Format("Jan 2, 15:04 UTC")
	}
	return "to " + p.Time.UTC().Format("Jan 2, 15:04 UTC")
}

// listOr is "a, b or c".
func listOr(words []string) string {
	if len(words) <= 1 {
		return strings.Join(words, "")
	}
	return strings.Join(words[:len(words)-1], ", ") + " or " + words[len(words)-1]
}

// messages is "1 message" or "2,100 messages".
func messages(n int64) string {
	digits := strconv.FormatInt(n, 10)
	var grouped []string
	for len(digits) > 3 {
		grouped = append([]string{digits[len(digits)-3:]}, grouped...)
		digits = digits[:len(digits)-3]
	}
	number := strings.Join(append([]string{digits}, grouped...), ",")
	if n == 1 {
		return number + " message"
	}
	return number + " messages"
}
