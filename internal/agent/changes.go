package agent

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/ReneP92/changeport/internal/events"
	"github.com/ReneP92/changeport/internal/topics"
)

// ChangesAdmin is the part of the Kafka Admin API the change watcher uses;
// *kadm.Client implements it. It needs Describe and DescribeConfigs on
// topics, Describe on groups and on the cluster, which DescribeACLs needs,
// and never reads a message.
type ChangesAdmin interface {
	ListTopics(ctx context.Context, topics ...string) (kadm.TopicDetails, error)
	DescribeTopicConfigs(ctx context.Context, topics ...string) (kadm.ResourceConfigs, error)
	DescribeACLs(ctx context.Context, b *kadm.ACLBuilder) (kadm.DescribeACLsResults, error)
	ListGroupsByType(ctx context.Context, types []string, filterStates ...string) (kadm.ListedGroups, error)
	DescribeGroups(ctx context.Context, groups ...string) (kadm.DescribedGroups, error)
	DescribeConsumerGroups(ctx context.Context, groups ...string) (kadm.DescribedConsumerGroups, error)
}

var _ ChangesAdmin = (*kadm.Client)(nil)

// Recorder keeps events for the timeline; *events.Recorder implements it.
type Recorder interface {
	Record(events ...events.Event)
}

// Snapshot is what the change watcher compares from one look at the cluster
// to the next.
type Snapshot struct {
	Topics map[string]TopicState
	// ACLs are the cluster's ACLs, each as a sentence.
	ACLs   map[string]bool
	Groups map[string]GroupState
}

// TopicState is a topic, as far as its changes go.
type TopicState struct {
	ID         string
	Partitions int
	// Configs are the topic's own settings, those set on the topic rather
	// than inherited from the brokers.
	Configs map[string]string
}

// GroupState is a consumer group's membership.
type GroupState struct {
	// Epoch is a KIP-848 consumer group's epoch, which grows with each
	// change of its members or their assignment; -1 for a classic group.
	Epoch int32
	// Members are its members, by member ID.
	Members map[string]Member
}

// Member is a member of a consumer group.
type Member struct {
	ClientID, Host string
}

// Changes watches a customer's cluster for changes and records them as
// events for the timeline: topics created, deleted, given more partitions
// or reconfigured, ACLs added or removed, and consumer groups that
// rebalanced. Each look compares the cluster with the last; the first only
// sets the baseline. The platform's own topics, groups and users are left
// out, as the console leaves them out.
type Changes struct {
	Admin    ChangesAdmin
	Recorder Recorder
	Tenant   string
	// Now is the time events are stamped with; time.Now by default.
	Now func() time.Time

	last *Snapshot
}

// Watch looks at the cluster once and records what changed since the last
// look. A look that fails records nothing and keeps the last baseline.
func (c *Changes) Watch(ctx context.Context) error {
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	snapshot, err := TakeSnapshot(ctx, c.Admin)
	if err != nil {
		return err
	}
	if c.last != nil {
		found := Diff(*c.last, snapshot, c.Tenant, now())
		if len(found) > 0 {
			c.Recorder.Record(found...)
		}
	}
	c.last = &snapshot
	return nil
}

// groupTypes are the consumer groups the watcher follows: classic groups,
// described with DescribeGroups, and KIP-848 consumer groups, described
// with ConsumerGroupDescribe.
const (
	classicGroups  = "classic"
	consumerGroups = "consumer"
)

// TakeSnapshot looks at the cluster's topics, ACLs and consumer groups.
func TakeSnapshot(ctx context.Context, admin ChangesAdmin) (Snapshot, error) {
	snapshot := Snapshot{Topics: map[string]TopicState{}, ACLs: map[string]bool{}, Groups: map[string]GroupState{}}
	details, err := admin.ListTopics(ctx)
	if err != nil {
		return snapshot, fmt.Errorf("list topics: %w", err)
	}
	var names []string
	for _, topic := range details.Sorted() {
		if topic.Err != nil || topic.IsInternal || internal(topic.Topic) || topics.Reserved(topic.Topic) {
			continue
		}
		names = append(names, topic.Topic)
		snapshot.Topics[topic.Topic] = TopicState{ID: topic.ID.String(), Partitions: len(topic.Partitions), Configs: map[string]string{}}
	}
	if len(names) > 0 {
		configs, err := admin.DescribeTopicConfigs(ctx, names...)
		if err != nil {
			return snapshot, fmt.Errorf("describe topic configs: %w", err)
		}
		for _, rc := range configs {
			state, ok := snapshot.Topics[rc.Name]
			if !ok {
				continue
			}
			if rc.Err != nil {
				if errors.Is(rc.Err, kerrUnknownTopic) {
					// Deleted since the listing.
					delete(snapshot.Topics, rc.Name)
					continue
				}
				return snapshot, fmt.Errorf("describe configs of %s: %w", rc.Name, rc.Err)
			}
			for _, config := range rc.Configs {
				if config.Source == kmsg.ConfigSourceDynamicTopicConfig {
					state.Configs[config.Key] = value(&config)
				}
			}
		}
	}

	acls, err := admin.DescribeACLs(ctx, kadm.NewACLs().AnyResource().Allow().Deny().AllowHosts().DenyHosts().
		Operations(kadm.OpAny).ResourcePatternType(kadm.ACLPatternAny))
	if err != nil {
		return snapshot, fmt.Errorf("describe ACLs: %w", err)
	}
	for _, result := range acls {
		if result.Err != nil {
			return snapshot, fmt.Errorf("describe ACLs: %w", result.Err)
		}
		for _, acl := range result.Described {
			// Only the platform's users authenticate with certificates.
			if strings.HasPrefix(acl.Principal, "User:CN=") {
				continue
			}
			snapshot.ACLs[describeACL(acl)] = true
		}
	}

	for _, kind := range []string{classicGroups, consumerGroups} {
		listed, err := admin.ListGroupsByType(ctx, []string{kind})
		if err != nil {
			return snapshot, fmt.Errorf("list %s groups: %w", kind, err)
		}
		var groups []string
		for _, group := range listed.Groups() {
			if !platformGroup(group) {
				groups = append(groups, group)
			}
		}
		if len(groups) == 0 {
			continue
		}
		if kind == classicGroups {
			described, err := admin.DescribeGroups(ctx, groups...)
			if err != nil {
				return snapshot, fmt.Errorf("describe groups: %w", err)
			}
			for _, group := range described {
				if group.Err != nil {
					continue
				}
				state := GroupState{Epoch: -1, Members: map[string]Member{}}
				for _, member := range group.Members {
					state.Members[member.MemberID] = Member{ClientID: member.ClientID, Host: member.ClientHost}
				}
				snapshot.Groups[group.Group] = state
			}
			continue
		}
		described, err := admin.DescribeConsumerGroups(ctx, groups...)
		if err != nil {
			return snapshot, fmt.Errorf("describe consumer groups: %w", err)
		}
		for _, group := range described {
			if group.Err != nil {
				continue
			}
			state := GroupState{Epoch: group.Epoch, Members: map[string]Member{}}
			for _, member := range group.Members {
				state.Members[member.MemberID] = Member{ClientID: member.ClientID, Host: member.ClientHost}
			}
			snapshot.Groups[group.Group] = state
		}
	}
	return snapshot, nil
}

// platformGroup reports whether a consumer group is the platform's own,
// such as the Connect worker's or the schema registry's.
func platformGroup(group string) bool { return strings.HasPrefix(group, "changeport") }

// describeACL says what an ACL allows or denies, as a sentence.
func describeACL(acl kadm.DescribedACL) string {
	verb := "may"
	if acl.Permission == kmsg.ACLPermissionTypeDeny {
		verb = "may not"
	}
	operation := strings.ReplaceAll(strings.ToLower(acl.Operation.String()), "_", " ")
	resource := strings.ReplaceAll(strings.ToLower(acl.Type.String()), "_", " ")
	name := acl.Name
	switch {
	case acl.Type == kmsg.ACLResourceTypeCluster:
		name = ""
	case name == "*":
		resource += "s"
		name = "(all)"
	case acl.Pattern == kadm.ACLPatternPrefixed:
		resource += "s"
		name = "starting with " + name
	}
	sentence := strings.Join(strings.Fields(fmt.Sprintf("%s %s %s %s %s", acl.Principal, verb, operation, resource, name)), " ")
	if acl.Host != "*" && acl.Host != "" {
		sentence += " from " + acl.Host
	}
	return sentence
}

// Diff returns the events between two looks at a tenant's cluster, at now.
func Diff(before, after Snapshot, tenant string, now time.Time) []events.Event {
	var found []events.Event
	event := func(kind events.Kind, message string) events.Event {
		return events.Event{ID: events.NewID(), Time: now, Tenant: tenant, Kind: kind, Message: message, Source: "agent"}
	}
	for _, name := range slices.Sorted(maps.Keys(before.Topics)) {
		if then, ok := after.Topics[name]; !ok || then.ID != before.Topics[name].ID {
			e := event(events.TopicDeleted, fmt.Sprintf("Topic %s was deleted.", name))
			e.Topic = name
			found = append(found, e)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(after.Topics)) {
		current, was := after.Topics[name], before.Topics[name]
		if _, ok := before.Topics[name]; !ok || was.ID != current.ID {
			e := event(events.TopicCreated, fmt.Sprintf("Topic %s was created with %s.", name, plural(current.Partitions, "partition")))
			e.Topic = name
			found = append(found, e)
			continue
		}
		if current.Partitions > was.Partitions {
			e := event(events.TopicPartitionsAdded, fmt.Sprintf("Topic %s went from %s to %s; new messages spread over all of them, so messages with one key may now go to another partition.",
				name, plural(was.Partitions, "partition"), plural(current.Partitions, "partition")))
			e.Topic = name
			found = append(found, e)
		}
		if changes := configChanges(was.Configs, current.Configs); len(changes) > 0 {
			e := event(events.TopicConfigChanged, fmt.Sprintf("Topic %s: %s.", name, strings.Join(changes, "; ")))
			e.Topic = name
			found = append(found, e)
		}
	}
	for _, acl := range slices.Sorted(maps.Keys(after.ACLs)) {
		if !before.ACLs[acl] {
			found = append(found, event(events.ACLAdded, "Access granted: "+acl+"."))
		}
	}
	for _, acl := range slices.Sorted(maps.Keys(before.ACLs)) {
		if !after.ACLs[acl] {
			found = append(found, event(events.ACLRemoved, "Access revoked: "+acl+"."))
		}
	}
	for _, name := range slices.Sorted(maps.Keys(after.Groups)) {
		was, existed := before.Groups[name]
		if !existed {
			// A group seen for the first time has no epoch to compare.
			was = GroupState{Epoch: -1}
		}
		message, ok := rebalance(name, was, after.Groups[name])
		if ok && !existed {
			message = fmt.Sprintf("Consumer group %s started: %s joined. It has %s.", name,
				membersWord(slices.Collect(maps.Values(after.Groups[name].Members))), plural(len(after.Groups[name].Members), "member"))
		}
		if ok {
			e := event(events.GroupRebalanced, message)
			e.ConsumerGroup = name
			found = append(found, e)
		}
	}
	return found
}

// rebalance says how a consumer group's members changed, if they did: a
// classic group's member IDs, or a consumer group's epoch.
func rebalance(name string, before, after GroupState) (string, bool) {
	var joined, left []Member
	for _, id := range slices.Sorted(maps.Keys(after.Members)) {
		if _, ok := before.Members[id]; !ok {
			joined = append(joined, after.Members[id])
		}
	}
	for _, id := range slices.Sorted(maps.Keys(before.Members)) {
		if _, ok := after.Members[id]; !ok {
			left = append(left, before.Members[id])
		}
	}
	// A member that restarts joins again under a new member ID, from the
	// same client and host.
	var rejoined []Member
	for i := 0; i < len(joined); {
		at := slices.Index(left, joined[i])
		if at < 0 {
			i++
			continue
		}
		rejoined = append(rejoined, joined[i])
		joined = slices.Delete(joined, i, i+1)
		left = slices.Delete(left, at, at+1)
	}
	epochMoved := after.Epoch >= 0 && before.Epoch >= 0 && after.Epoch != before.Epoch
	if len(joined) == 0 && len(left) == 0 && len(rejoined) == 0 && !epochMoved {
		return "", false
	}
	count := len(after.Members)
	switch {
	case count == 0:
		return fmt.Sprintf("Consumer group %s's last %s left; it consumes nothing until one joins.",
			name, pluralWord(len(left), "member")), true
	case len(joined) == 0 && len(left) == 0 && len(rejoined) == 0:
		return fmt.Sprintf("Consumer group %s rebalanced: its partitions were assigned anew among its %s.", name, plural(count, "member")), true
	}
	var what []string
	if len(rejoined) > 0 {
		what = append(what, membersWord(rejoined)+" rejoined")
	}
	if len(joined) > 0 {
		what = append(what, membersWord(joined)+" joined")
	}
	if len(left) > 0 {
		what = append(what, membersWord(left)+" left")
	}
	return fmt.Sprintf("Consumer group %s rebalanced: %s. It has %s.", name, strings.Join(what, " and "), plural(count, "member")), true
}

// membersWord names one member by its client and host, or counts several.
func membersWord(members []Member) string {
	if len(members) > 1 {
		return plural(len(members), "member")
	}
	m := members[0]
	name := cmp.Or(m.ClientID, "a member")
	if host := strings.TrimPrefix(m.Host, "/"); host != "" {
		name += " on " + host
	}
	return name
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

// pluralWord is "member" or "3 members".
func pluralWord(n int, word string) string {
	if n == 1 {
		return word
	}
	return plural(n, word)
}

// configChanges says how a topic's own settings changed. The agent's own
// change, turning on tiered storage, is no news.
func configChanges(before, after map[string]string) []string {
	var changes []string
	keys := map[string]bool{}
	for key := range before {
		keys[key] = true
	}
	for key := range after {
		keys[key] = true
	}
	for _, key := range slices.Sorted(maps.Keys(keys)) {
		was, had := before[key]
		now, has := after[key]
		switch {
		case had && has && was == now:
		case key == remoteStorageEnable && has && strings.EqualFold(now, "true"):
		case !had:
			changes = append(changes, fmt.Sprintf("%s set to %s", key, configValue(key, now)))
		case !has:
			changes = append(changes, fmt.Sprintf("%s back to the cluster's default (was %s)", key, configValue(key, was)))
		default:
			changes = append(changes, fmt.Sprintf("%s changed from %s to %s", key, configValue(key, was), configValue(key, now)))
		}
	}
	return changes
}

// configValue writes a setting's value for people: durations and sizes in
// words besides the number.
func configValue(key, value string) string {
	n, err := strconv.ParseInt(value, 10, 64)
	switch {
	case err != nil:
		return value
	case n == -1 && (strings.HasSuffix(key, ".ms") || strings.HasSuffix(key, ".bytes")):
		return value + " (no limit)"
	case strings.HasSuffix(key, ".ms") && n > 0:
		return fmt.Sprintf("%s (%s)", value, duration(time.Duration(n)*time.Millisecond))
	case strings.HasSuffix(key, ".bytes") && n > 0:
		return fmt.Sprintf("%s (%s)", value, bytesWord(n))
	}
	return value
}

// duration writes d in its largest whole unit, or minutes and seconds.
func duration(d time.Duration) string {
	for _, unit := range []struct {
		size time.Duration
		name string
	}{{24 * time.Hour, "day"}, {time.Hour, "hour"}, {time.Minute, "minute"}, {time.Second, "second"}} {
		if d >= unit.size && d%unit.size == 0 {
			return plural(int(d/unit.size), unit.name)
		}
	}
	return d.String()
}

// bytesWord writes a size in powers of 1,000.
func bytesWord(n int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	value, unit := float64(n), 0
	for value >= 1000 && unit < len(units)-1 {
		value /= 1000
		unit++
	}
	if value == float64(int64(value)) {
		return fmt.Sprintf("%d %s", int64(value), units[unit])
	}
	return fmt.Sprintf("%.1f %s", value, units[unit])
}
