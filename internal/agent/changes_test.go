package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/ReneP92/changeport/internal/events"
)

var watched = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

func messages(found []events.Event) []string {
	var out []string
	for _, e := range found {
		out = append(out, string(e.Kind)+": "+e.Message)
	}
	return out
}

func TestDiffTellsWhatChangedInPlainWords(t *testing.T) {
	before := Snapshot{
		Topics: map[string]TopicState{
			"orders":   {ID: "o1", Partitions: 3, Configs: map[string]string{"retention.ms": "604800000"}},
			"payments": {ID: "p1", Partitions: 1, Configs: map[string]string{"max.message.bytes": "2000000"}},
			"old":      {ID: "x1", Partitions: 1, Configs: map[string]string{}},
			"replaced": {ID: "r1", Partitions: 1, Configs: map[string]string{}},
		},
		ACLs: map[string]bool{"User:alice may read topic orders": true},
		Groups: map[string]GroupState{
			"billing":  {Epoch: -1, Members: map[string]Member{"m1": {ClientID: "billing-1", Host: "/10.0.0.1"}}},
			"shipping": {Epoch: 4, Members: map[string]Member{"s1": {ClientID: "shipping-1"}, "s2": {ClientID: "shipping-2"}}},
			"steady":   {Epoch: -1, Members: map[string]Member{"t1": {ClientID: "steady-1"}}},
			"leaving":  {Epoch: -1, Members: map[string]Member{"l1": {}, "l2": {}}},
		},
	}
	after := Snapshot{
		Topics: map[string]TopicState{
			// Retention changed, more partitions, and tiered storage turned
			// on by the agent itself, which is no news.
			"orders": {ID: "o1", Partitions: 6, Configs: map[string]string{"retention.ms": "86400000", "remote.storage.enable": "true"}},
			// Its own size limit removed, and compaction set.
			"payments": {ID: "p1", Partitions: 1, Configs: map[string]string{"cleanup.policy": "compact"}},
			"replaced": {ID: "r2", Partitions: 2, Configs: map[string]string{}},
			"new":      {ID: "n1", Partitions: 1, Configs: map[string]string{}},
		},
		ACLs: map[string]bool{"User:bob may write topics starting with logs-": true},
		Groups: map[string]GroupState{
			"billing":  {Epoch: -1, Members: map[string]Member{"m1": {ClientID: "billing-1", Host: "/10.0.0.1"}, "m2": {ClientID: "billing-2", Host: "/10.0.0.2"}}},
			"shipping": {Epoch: 5, Members: map[string]Member{"s1": {ClientID: "shipping-1"}, "s2": {ClientID: "shipping-2"}}},
			"steady":   {Epoch: -1, Members: map[string]Member{"t1": {ClientID: "steady-1"}}},
			"leaving":  {Epoch: -1, Members: map[string]Member{}},
			"fresh":    {Epoch: 1, Members: map[string]Member{"n1": {ClientID: "fresh-1"}}},
			// Offsets committed without members: nothing to tell.
			"offsets-only": {Epoch: 3, Members: map[string]Member{}},
		},
	}
	found := Diff(before, after, "acme", watched)
	want := []string{
		"topic.deleted: Topic old was deleted.",
		"topic.deleted: Topic replaced was deleted.",
		"topic.created: Topic new was created with 1 partition.",
		"topic.partitions: Topic orders went from 3 partitions to 6 partitions; new messages spread over all of them, so messages with one key may now go to another partition.",
		"topic.config: Topic orders: retention.ms changed from 604800000 (7 days) to 86400000 (1 day).",
		"topic.config: Topic payments: cleanup.policy set to compact; max.message.bytes back to the cluster's default (was 2000000 (2 MB)).",
		"topic.created: Topic replaced was created with 2 partitions.",
		"acl.added: Access granted: User:bob may write topics starting with logs-.",
		"acl.removed: Access revoked: User:alice may read topic orders.",
		"group.rebalanced: Consumer group billing rebalanced: billing-2 on 10.0.0.2 joined. It has 2 members.",
		"group.rebalanced: Consumer group fresh started: fresh-1 joined. It has 1 member.",
		"group.rebalanced: Consumer group leaving's last 2 members left; it consumes nothing until one joins.",
		"group.rebalanced: Consumer group shipping rebalanced: its partitions were assigned anew among its 2 members.",
	}
	got := messages(found)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("Diff() =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, e := range found {
		if e.Tenant != "acme" || !e.Time.Equal(watched) || e.ID == "" || e.Source != "agent" {
			t.Errorf("event %+v lacks its tenant, time, ID or source", e)
		}
		if strings.HasPrefix(string(e.Kind), "topic.") && e.Topic == "" {
			t.Errorf("event %q names no topic", e.Message)
		}
		if e.Kind == events.GroupRebalanced && e.ConsumerGroup == "" {
			t.Errorf("event %q names no group", e.Message)
		}
	}
	if found := Diff(after, after, "acme", watched); len(found) != 0 {
		t.Errorf("Diff() of a cluster with itself = %v, want nothing", messages(found))
	}
}

func TestDescribeACL(t *testing.T) {
	for _, c := range []struct {
		acl  kadm.DescribedACL
		want string
	}{
		{kadm.DescribedACL{
			Principal: "User:alice", Host: "*", Type: kmsg.ACLResourceTypeTopic, Name: "orders", Pattern: kadm.ACLPatternLiteral,
			Operation: kadm.OpRead, Permission: kmsg.ACLPermissionTypeAllow,
		}, "User:alice may read topic orders"},
		{kadm.DescribedACL{
			Principal: "User:bob", Host: "10.0.0.1", Type: kmsg.ACLResourceTypeGroup, Name: "*", Pattern: kadm.ACLPatternLiteral,
			Operation: kadm.OpDescribe, Permission: kmsg.ACLPermissionTypeDeny,
		}, "User:bob may not describe groups (all) from 10.0.0.1"},
		{kadm.DescribedACL{
			Principal: "User:carol", Host: "*", Type: kmsg.ACLResourceTypeCluster, Name: "kafka-cluster", Pattern: kadm.ACLPatternLiteral,
			Operation: kadm.OpDescribeConfigs, Permission: kmsg.ACLPermissionTypeAllow,
		}, "User:carol may describe configs cluster"},
		{kadm.DescribedACL{
			Principal: "User:dan", Host: "*", Type: kmsg.ACLResourceTypeTransactionalId, Name: "tx-", Pattern: kadm.ACLPatternPrefixed,
			Operation: kadm.OpWrite, Permission: kmsg.ACLPermissionTypeAllow,
		}, "User:dan may write transactional ids starting with tx-"},
	} {
		if got := describeACL(c.acl); got != c.want {
			t.Errorf("describeACL() = %q, want %q", got, c.want)
		}
	}
}

// fakeChangesAdmin is a cluster that the test changes between looks.
type fakeChangesAdmin struct {
	topics  kadm.TopicDetails
	configs kadm.ResourceConfigs
	acls    kadm.DescribedACLs
	classic kadm.DescribedGroups
}

func (f *fakeChangesAdmin) ListTopics(context.Context, ...string) (kadm.TopicDetails, error) {
	return f.topics, nil
}

func (f *fakeChangesAdmin) DescribeTopicConfigs(context.Context, ...string) (kadm.ResourceConfigs, error) {
	return f.configs, nil
}

func (f *fakeChangesAdmin) DescribeACLs(context.Context, *kadm.ACLBuilder) (kadm.DescribeACLsResults, error) {
	return kadm.DescribeACLsResults{{Described: f.acls}}, nil
}

func (f *fakeChangesAdmin) ListGroupsByType(_ context.Context, types []string, _ ...string) (kadm.ListedGroups, error) {
	listed := kadm.ListedGroups{}
	if types[0] == classicGroups {
		for name := range f.classic {
			listed[name] = kadm.ListedGroup{Group: name}
		}
	}
	return listed, nil
}

func (f *fakeChangesAdmin) DescribeGroups(_ context.Context, groups ...string) (kadm.DescribedGroups, error) {
	out := kadm.DescribedGroups{}
	for _, g := range groups {
		out[g] = f.classic[g]
	}
	return out, nil
}

func (f *fakeChangesAdmin) DescribeConsumerGroups(context.Context, ...string) (kadm.DescribedConsumerGroups, error) {
	return kadm.DescribedConsumerGroups{}, nil
}

type collected struct{ events []events.Event }

func (c *collected) Record(e ...events.Event) { c.events = append(c.events, e...) }

func TestWatchRecordsChangesAfterItsBaseline(t *testing.T) {
	retention := "604800000"
	admin := &fakeChangesAdmin{
		topics: kadm.TopicDetails{
			"orders":             {Topic: "orders", Partitions: kadm.PartitionDetails{0: {}}},
			"__consumer_offsets": {Topic: "__consumer_offsets", IsInternal: true},
			"changeport.events":  {Topic: "changeport.events", Partitions: kadm.PartitionDetails{0: {}}},
		},
		configs: kadm.ResourceConfigs{{Name: "orders", Configs: []kadm.Config{
			{Key: "retention.ms", Value: &retention, Source: kmsg.ConfigSourceDynamicTopicConfig},
			{Key: "segment.bytes", Value: kadm.StringPtr("1073741824"), Source: kmsg.ConfigSourceDefaultConfig},
		}}},
		acls: kadm.DescribedACLs{
			{Principal: "User:CN=changeport-agent", Host: "*", Type: kmsg.ACLResourceTypeTopic, Name: "*", Operation: kadm.OpDescribe, Permission: kmsg.ACLPermissionTypeAllow},
		},
		classic: kadm.DescribedGroups{
			"billing":              {Group: "billing", Members: []kadm.DescribedGroupMember{{MemberID: "m1", ClientID: "billing-1"}}},
			"changeport-connect-x": {Group: "changeport-connect-x", Members: []kadm.DescribedGroupMember{{MemberID: "c1"}}},
		},
	}
	recorded := &collected{}
	changes := &Changes{Admin: admin, Recorder: recorded, Tenant: "acme", Now: func() time.Time { return watched }}
	if err := changes.Watch(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(recorded.events) != 0 {
		t.Fatalf("the first look recorded %v, want only a baseline", messages(recorded.events))
	}
	retention = "86400000"
	admin.classic["billing"] = kadm.DescribedGroup{Group: "billing", Members: []kadm.DescribedGroupMember{{MemberID: "m2", ClientID: "billing-1"}}}
	admin.classic["changeport-connect-x"] = kadm.DescribedGroup{Group: "changeport-connect-x", Members: []kadm.DescribedGroupMember{{MemberID: "c2"}}}
	admin.acls = append(admin.acls, kadm.DescribedACL{Principal: "User:CN=changeport-console", Host: "*", Type: kmsg.ACLResourceTypeGroup, Name: "*", Operation: kadm.OpRead})
	if err := changes.Watch(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"topic.config: Topic orders: retention.ms changed from 604800000 (7 days) to 86400000 (1 day).",
		"group.rebalanced: Consumer group billing rebalanced: billing-1 rejoined. It has 1 member.",
	}
	if got := messages(recorded.events); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("the second look recorded\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
