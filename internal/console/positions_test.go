package console

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"k8s.io/utils/ptr"

	"github.com/ReneP92/changeport/internal/console/api"
	"github.com/ReneP92/changeport/internal/events"
)

func TestTopicPositions(t *testing.T) {
	at := time.Date(2026, 10, 3, 14, 5, 0, 0, time.UTC)
	positions := []PartitionPosition{
		// Moved back 400 of 1,000, of which 100 are only in tiered storage.
		{Topic: "orders", Partition: 0, Committed: 900, Start: 0, LocalStart: 600, End: 1000, Target: 500, TargetTime: at.Add(time.Minute)},
		// Moved forward past 50 it had not read.
		{Topic: "orders", Partition: 1, Committed: 450, Start: 100, LocalStart: 100, End: 1000, Target: 500, TargetTime: at},
		// Committed before the start: what retention deleted is not read again.
		{Topic: "orders", Partition: 2, Committed: 10, Start: 200, LocalStart: 200, End: 300, Target: 300},
		// No position yet, as in a replay.
		{Topic: "payments", Partition: 0, Committed: -1, Start: 0, LocalStart: 0, End: 70, Target: 0, TargetTime: at},
	}
	got := topicPositions(positions)
	want := []api.TopicPosition{
		{Topic: "orders", Partitions: 3, ReadAgain: 400, Skipped: 50 + 100, ToRead: 500 + 500, TieredMessages: 100, From: &at},
		{Topic: "payments", Partitions: 1, ToRead: 70, From: &at},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("topicPositions() =\n%+v\nwant\n%+v", got, want)
	}
	if got := topicPositions(nil); got == nil || len(got) != 0 {
		t.Errorf("topicPositions(nil) = %#v, want an empty list", got)
	}
}

func TestTieredSeconds(t *testing.T) {
	positions := []PartitionPosition{
		{Topic: "orders", Partition: 0, Start: 0, LocalStart: 600, End: 1000, Target: 0},
		{Topic: "orders", Partition: 1, Start: 0, LocalStart: 600, End: 1000, Target: 400},
		{Topic: "payments", Partition: 0, Start: 0, LocalStart: 0, End: 70, Target: 0},
	}
	// orders keeps its last 800 messages on disk, in 1.6 MiB, so the 800
	// tiered ones are 1.6 MiB too.
	sizes := map[string]float64{"orders": 800 * (4 << 20) / 2000, "payments": 1 << 20}
	tests := []struct {
		name    string
		limit   int64
		brokers int
		want    *int64
	}{
		// Two partitions on three brokers: two read at once.
		{"two brokers at 64 KiB/s", 64 << 10, 3, ptr.To(int64(13))},
		{"one broker at 64 KiB/s", 64 << 10, 1, ptr.To(int64(26))},
		{"no limit", 0, 3, nil},
	}
	for _, tt := range tests {
		got := tieredSeconds(positions, sizes, tt.limit, tt.brokers)
		if (got == nil) != (tt.want == nil) || (got != nil && *got != *tt.want) {
			t.Errorf("%s: tieredSeconds() = %v, want %v", tt.name, ptr.Deref(got, -1), ptr.Deref(tt.want, -1))
		}
	}
	if got := tieredSeconds(positions, map[string]float64{}, 64<<10, 3); got != nil {
		t.Errorf("tieredSeconds() without sizes = %d, want none", *got)
	}
	if got := tieredSeconds(positions[2:], sizes, 64<<10, 3); got != nil {
		t.Errorf("tieredSeconds() with nothing tiered = %d, want none", *got)
	}
}

func TestMessages(t *testing.T) {
	for n, want := range map[int64]string{0: "0 messages", 1: "1 message", 999: "999 messages", 1000: "1,000 messages", 2100000: "2,100,000 messages"} {
		if got := messages(n); got != want {
			t.Errorf("messages(%d) = %q, want %q", n, got, want)
		}
	}
}

// fixtureWithPositions has an empty consumer group billing, at offset 800
// of 1,000 on both partitions of acme-orders, the first starting at 0 with
// the oldest 300 in tiered storage.
func fixtureWithPositions(t *testing.T) *fixture {
	f := newFixture(t)
	f.clusters.described = map[string][]ConsumerGroup{"acme": {{Name: "billing", State: "Empty"}, {Name: "search", State: "Empty"}}}
	// A group with positions, which Kafka did not describe.
	f.clusters.positions["acme/undescribed"] = []PartitionPosition{{Topic: "acme-orders", Committed: 5, End: 1000}}
	f.clusters.positions["acme/billing"] = []PartitionPosition{
		{Topic: "acme-orders", Partition: 0, Committed: 800, Start: 0, LocalStart: 300, End: 1000},
		{Topic: "acme-orders", Partition: 1, Committed: 800, Start: 0, LocalStart: 0, End: 1000},
	}
	return f
}

func TestPreviewGroupPosition(t *testing.T) {
	f := fixtureWithPositions(t)
	admin := f.signer.token(t, session{user: "user_3", org: "org_acme", role: "admin"})
	status, body := send(t, f.handler, http.MethodPost, "/api/customer/consumer-groups/billing/position/preview", admin,
		`{"to":"time","time":"2026-10-03T14:05:00Z"}`)
	want := `{"group":"billing","members":[],"topics":[{"from":"2026-10-03T14:05:00Z","partitions":2,"readAgain":600,"skipped":0,"tieredMessages":0,"toRead":1000,"topic":"acme-orders"}]}` + "\n"
	if status != 200 || body != want {
		t.Errorf("preview = %d %s, want 200 %s", status, body, want)
	}
	status, body = send(t, f.handler, http.MethodPost, "/api/customer/consumer-groups/billing/position/preview", admin, `{"to":"earliest"}`)
	if status != 200 || !strings.Contains(body, `"readAgain":1600`) || !strings.Contains(body, `"tieredMessages":300`) {
		t.Errorf("preview to the earliest = %d %s, want 1,600 read again, 300 of them tiered", status, body)
	}
	status, body = send(t, f.handler, http.MethodPost, "/api/customer/consumer-groups/billing/position/preview", admin, `{"to":"earliest","into":"billing-replay"}`)
	if status != 200 || !strings.Contains(body, `"group":"billing-replay"`) || !strings.Contains(body, `"readAgain":0,"skipped":0,"tieredMessages":300,"toRead":2000`) {
		t.Errorf("preview of a replay = %d %s, want 2,000 to read in billing-replay", status, body)
	}
	if len(f.clusters.committed) != 0 {
		t.Errorf("a preview committed %v", f.clusters.committed)
	}

	// The preview names a group's members, which a move must wait for.
	f.clusters.described["acme"][0].Members = []GroupMember{{ClientID: "billing-1"}, {ClientID: "billing-2"}}
	if status, body := send(t, f.handler, http.MethodPost, "/api/customer/consumer-groups/billing/position/preview", admin, `{"to":"latest"}`); status != 200 || !strings.Contains(body, `"members":["billing-1","billing-2"]`) {
		t.Errorf("preview of a group with members = %d %s", status, body)
	}
}

func TestPositionRefusals(t *testing.T) {
	f := fixtureWithPositions(t)
	admin := f.signer.token(t, session{user: "user_3", org: "org_acme", role: "admin"})
	member := f.signer.token(t, session{user: "user_2", org: "org_acme", role: "member"})
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	tests := []struct {
		name, token, method, path, body, want string
		status                                int
	}{
		{"a member's preview", member, http.MethodPost, "billing/position/preview", `{"to":"latest"}`, "Admins only", 403},
		{"a member's move", member, http.MethodPut, "billing/position", `{"to":"latest"}`, "Admins only", 403},
		{"a member's replay", member, http.MethodPost, "billing/replays", `{"to":"earliest","into":"again"}`, "Admins only", 403},
		{"a platform group", admin, http.MethodPut, "changeport-connect/position", `{"to":"latest"}`, "No such consumer group", 404},
		{"an unknown group", admin, http.MethodPut, "unknown/position", `{"to":"latest"}`, "No such consumer group", 404},
		{"an unknown topic", admin, http.MethodPut, "billing/position", `{"to":"latest","topics":["unknown"]}`, "No such topic", 404},
		{"a reserved topic", admin, http.MethodPut, "billing/position", `{"to":"latest","topics":["__consumer_offsets"]}`, "No such topic", 404},
		{"a time without one", admin, http.MethodPut, "billing/position", `{"to":"time"}`, `"field":"time"`, 400},
		{"a time in the future", admin, http.MethodPut, "billing/position", `{"to":"time","time":"` + future + `"}`, "in the future", 400},
		{"a move into another group", admin, http.MethodPut, "billing/position", `{"to":"latest","into":"other"}`, `"field":"into"`, 400},
		{"a group without positions", admin, http.MethodPut, "search/position", `{"to":"latest"}`, `"field":"topics"`, 400},
		{"a replay without a name", admin, http.MethodPost, "billing/replays", `{"to":"earliest"}`, `"field":"into"`, 400},
		{"a replay with a bad name", admin, http.MethodPost, "billing/replays", `{"to":"earliest","into":"a b"}`, "not allowed", 400},
		{"a replay into itself", admin, http.MethodPost, "billing/replays", `{"to":"earliest","into":"billing"}`, "taken", 400},
		{"a replay into a platform group", admin, http.MethodPost, "billing/replays", `{"to":"earliest","into":"changeport-x"}`, "taken", 400},
		{"a replay into Connect's groups", admin, http.MethodPost, "billing/replays", `{"to":"earliest","into":"Connect-x"}`, "taken", 400},
		{"a replay into a group with positions", admin, http.MethodPost, "billing/replays", `{"to":"earliest","into":"undescribed"}`, "has positions already", 409},
		{"a replay into a group that exists", admin, http.MethodPost, "billing/replays", `{"to":"earliest","into":"search"}`, "exists", 409},
	}
	for _, tt := range tests {
		status, body := send(t, f.handler, tt.method, "/api/customer/consumer-groups/"+tt.path, tt.token, tt.body)
		if status != tt.status || !strings.Contains(body, tt.want) {
			t.Errorf("%s = %d %s, want %d with %s", tt.name, status, body, tt.status, tt.want)
		}
	}
	if len(f.clusters.committed) != 0 || len(f.recorded.events) != 0 {
		t.Errorf("refused requests committed %v and recorded %v", f.clusters.committed, f.recorded.events)
	}
}

func TestMoveGroup(t *testing.T) {
	f := fixtureWithPositions(t)
	admin := f.signer.token(t, session{user: "user_3", org: "org_acme", role: "admin"})
	status, body := send(t, f.handler, http.MethodPut, "/api/customer/consumer-groups/billing/position", admin, `{"to":"time","time":"2026-10-03T14:05:00Z"}`)
	if status != 200 || !strings.Contains(body, `"readAgain":600`) {
		t.Fatalf("moving a group = %d %s", status, body)
	}
	var targets []int64
	for _, p := range f.clusters.committed["acme/billing"] {
		targets = append(targets, p.Target)
	}
	if !slices.Equal(targets, []int64{500, 500}) {
		t.Errorf("committed %v, want 500 on both partitions", targets)
	}
	if !strings.Contains(f.log.String(), `"msg":"consumer group moved","audit":true,"tenant":"acme","user":"user_3","organization":"org_acme","group":"billing","from_group":"billing","to":"time","time":"2026-10-03T14:05:00Z","topics":["acme-orders"],"read_again":600,"skipped":0,"to_read":1000,"offsets_before":"acme-orders,0,800;acme-orders,1,800"`) {
		t.Errorf("no audit line for the move in %s", f.log.String())
	}
	if len(f.recorded.events) != 1 {
		t.Fatalf("recorded %+v, want the move", f.recorded.events)
	}
	e := f.recorded.events[0]
	if e.Kind != events.GroupMoved || e.ConsumerGroup != "billing" || e.Actor != "Ada Lovelace" ||
		e.Message != "Ada Lovelace moved consumer group billing to Oct 3, 14:05 UTC in the console; it reads 600 messages again." {
		t.Errorf("recorded %+v", e)
	}

	// Kafka moves a group only while it has no members.
	f.clusters.described["acme"][0].Members = []GroupMember{{ClientID: "billing-1"}}
	delete(f.clusters.committed, "acme/billing")
	status, body = send(t, f.handler, http.MethodPut, "/api/customer/consumer-groups/billing/position", admin, `{"to":"latest"}`)
	if status != 409 || !strings.Contains(body, "billing-1 is reading billing") {
		t.Errorf("moving a group with members = %d %s, want 409 naming them", status, body)
	}
	if len(f.clusters.committed) != 0 {
		t.Errorf("a group with members was moved: %v", f.clusters.committed)
	}
}

// When consumers join between the check and the commit, Kafka refuses, and
// what it moved anyway is audited and recorded.
func TestMoveGroupJoinedMeanwhile(t *testing.T) {
	f := fixtureWithPositions(t)
	admin := f.signer.token(t, session{user: "user_3", org: "org_acme", role: "admin"})
	f.clusters.commitFails = fmt.Errorf("group billing: %w", ErrGroupActive)
	status, body := send(t, f.handler, http.MethodPut, "/api/customer/consumer-groups/billing/position", admin, `{"to":"latest"}`)
	if status != 409 || !strings.Contains(body, "moved on 1 partition only") {
		t.Errorf("moving a group joined meanwhile = %d %s, want 409 saying what moved", status, body)
	}
	if len(f.recorded.events) != 1 || !strings.Contains(f.recorded.events[0].Message, "it skips 200 messages") {
		t.Errorf("recorded %+v, want the partition that moved", f.recorded.events)
	}
}

func TestReplayGroup(t *testing.T) {
	f := fixtureWithPositions(t)
	admin := f.signer.token(t, session{user: "user_3", org: "org_acme", role: "admin"})
	status, body := send(t, f.handler, http.MethodPost, "/api/customer/consumer-groups/billing/replays", admin, `{"to":"earliest","into":"billing-replay"}`)
	if status != 201 || !strings.Contains(body, `"group":"billing-replay"`) || !strings.Contains(body, `"toRead":2000`) {
		t.Fatalf("replaying = %d %s", status, body)
	}
	if len(f.clusters.committed["acme/billing-replay"]) != 2 || len(f.clusters.committed["acme/billing"]) != 0 {
		t.Errorf("committed %v, want both partitions to billing-replay only", f.clusters.committed)
	}
	if !strings.Contains(f.log.String(), `"msg":"consumer group replayed","audit":true,"tenant":"acme","user":"user_3","organization":"org_acme","group":"billing-replay","from_group":"billing","to":"earliest"`) {
		t.Errorf("no audit line for the replay in %s", f.log.String())
	}
	if len(f.recorded.events) != 1 {
		t.Fatalf("recorded %+v, want the replay", f.recorded.events)
	}
	e := f.recorded.events[0]
	if e.Kind != events.GroupReplayed || e.ConsumerGroup != "billing-replay" ||
		e.Message != "Ada Lovelace created consumer group billing-replay in the console, to read what billing reads from the earliest message kept: 2,000 messages." {
		t.Errorf("recorded %+v", e)
	}

	// The group replayed from may have members: it does not move.
	f.clusters.described["acme"][0].Members = []GroupMember{{ClientID: "billing-1"}}
	if status, body := send(t, f.handler, http.MethodPost, "/api/customer/consumer-groups/billing/replays", admin, `{"to":"latest","into":"billing-2"}`); status != 201 {
		t.Errorf("replaying from a group with members = %d %s", status, body)
	}
}
