package topics

import (
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ReneP92/changeport/internal/provisioning"
)

func devPolicy(t *testing.T) Policy {
	t.Helper()
	p, err := ForProfile(provisioning.DevProfile)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

var quiet = Cluster{Topics: []string{"orders", "payments.eu"}, Replicas: 100, Brokers: 1}

func valid() Request {
	return Request{Name: "invoices", Partitions: 3, Retention: 7 * 24 * time.Hour, CleanupPolicy: Delete}
}

func TestCheckTieredTopic(t *testing.T) {
	topic, err := devPolicy(t).Check(valid(), quiet)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"retention.ms": "604800000", "cleanup.policy": "delete", "remote.storage.enable": "true"}
	if topic.Name != "invoices" || topic.Partitions != 3 || topic.ReplicationFactor != 1 || !maps.Equal(topic.Configs, want) {
		t.Errorf("Check() = %+v, want 3 partitions, replication factor 1 and %v", topic, want)
	}
}

func TestCheckShortRetentionKeepsTieringValid(t *testing.T) {
	request := valid()
	request.Retention = time.Hour
	topic, err := devPolicy(t).Check(request, quiet)
	if err != nil {
		t.Fatal(err)
	}
	if topic.Configs["local.retention.ms"] != "3600000" || topic.Configs["remote.storage.enable"] != "true" {
		t.Errorf("configs = %v, want a tiered topic keeping an hour locally", topic.Configs)
	}
	request.Retention = 24 * time.Hour
	topic, _ = devPolicy(t).Check(request, quiet)
	if _, ok := topic.Configs["local.retention.ms"]; ok {
		t.Errorf("a day's retention sets local.retention.ms: %v", topic.Configs)
	}
}

func TestCheckCompactedTopicIsNotTiered(t *testing.T) {
	request := valid()
	request.CleanupPolicy = Compact
	topic, err := devPolicy(t).Check(request, quiet)
	if err != nil {
		t.Fatal(err)
	}
	if _, tiered := topic.Configs["remote.storage.enable"]; tiered || topic.Configs["cleanup.policy"] != "compact" {
		t.Errorf("configs = %v, want a compacted topic without tiering", topic.Configs)
	}
}

func TestCheckRefuses(t *testing.T) {
	tests := []struct {
		name    string
		change  func(*Request)
		cluster Cluster
		field   string
		kind    error
	}{
		{"no name", func(r *Request) { r.Name = "" }, quiet, "name", ErrInvalid},
		{"a long name", func(r *Request) { r.Name = strings.Repeat("a", 201) }, quiet, "name", ErrInvalid},
		{"a dot", func(r *Request) { r.Name = "." }, quiet, "name", ErrInvalid},
		{"spaces", func(r *Request) { r.Name = "my topic" }, quiet, "name", ErrInvalid},
		{"an internal name", func(r *Request) { r.Name = "__consumer_offsets" }, quiet, "name", ErrInvalid},
		{"the platform's prefix", func(r *Request) { r.Name = "Changeport-audit" }, quiet, "name", ErrInvalid},
		{"Strimzi's prefix", func(r *Request) { r.Name = "strimzi-store" }, quiet, "name", ErrInvalid},
		{"Kafka Connect's prefix", func(r *Request) { r.Name = "connect-offsets" }, quiet, "name", ErrInvalid},
		{"a taken name", func(r *Request) { r.Name = "orders" }, quiet, "name", ErrConflict},
		{"a dot for an underscore", func(r *Request) { r.Name = "payments_eu" }, quiet, "name", ErrConflict},
		{"no partitions", func(r *Request) { r.Partitions = 0 }, quiet, "partitions", ErrInvalid},
		{"more than the profile allows", func(r *Request) { r.Partitions = 13 }, quiet, "partitions", ErrInvalid},
		{"more than the brokers hold", func(r *Request) { r.Partitions = 12 }, Cluster{Replicas: 995, Brokers: 1}, "partitions", ErrInvalid},
		{"a retention that is no preset", func(r *Request) { r.Retention = 90 * time.Minute }, quiet, "retention", ErrInvalid},
		{"retention forever", func(r *Request) { r.Retention = -1 }, quiet, "retention", ErrInvalid},
		{"another cleanup policy", func(r *Request) { r.CleanupPolicy = "compact,delete" }, quiet, "cleanupPolicy", ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := valid()
			tt.change(&request)
			_, err := devPolicy(t).Check(request, tt.cluster)
			var fieldErr *FieldError
			if !errors.As(err, &fieldErr) || fieldErr.Field != tt.field || !errors.Is(err, tt.kind) {
				t.Errorf("Check() = %v, want a %v on %s", err, tt.kind, tt.field)
			}
		})
	}
}

func TestPolicyPerProfile(t *testing.T) {
	for name, want := range map[string]Policy{
		"dev":        {MaxPartitions: 12, ReplicationFactor: 1},
		"dev-ha":     {MaxPartitions: 24, ReplicationFactor: 3},
		"production": {MaxPartitions: 48, ReplicationFactor: 3},
	} {
		profile, err := provisioning.ProfileByName(name)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := ForProfile(profile); err != nil || got != want {
			t.Errorf("ForProfile(%s) = %+v, %v, want %+v", name, got, err, want)
		}
	}
	// Every profile has a policy.
	for _, name := range slices.Sorted(maps.Keys(provisioning.Profiles)) {
		if _, err := ForProfile(provisioning.Profiles[name]); err != nil {
			t.Errorf("profile %s: %v", name, err)
		}
	}
}

func TestReplicaBudgetCountsReplication(t *testing.T) {
	p, err := ForProfile(provisioning.ProductionProfile)
	if err != nil {
		t.Fatal(err)
	}
	request := valid()
	request.Partitions = 10
	// 3 brokers hold 3,000 replicas; 10 partitions of 3 replicas need 30.
	if _, err := p.Check(request, Cluster{Replicas: 2970, Brokers: 3}); err != nil {
		t.Errorf("Check() at the budget = %v", err)
	}
	if _, err := p.Check(request, Cluster{Replicas: 2971, Brokers: 3}); err == nil {
		t.Error("Check() past the budget succeeded")
	}
}

func TestCheckChange(t *testing.T) {
	week := int64(7 * 24 * time.Hour / time.Millisecond)
	weekly := Current{Partitions: 3, RetentionMs: &week}
	partitions := func(n int) *int { return &n }
	retention := func(d time.Duration) *time.Duration { return &d }
	tests := []struct {
		name    string
		current Current
		change  Change
		want    Alteration
	}{
		{"more partitions", weekly, Change{Partitions: partitions(6)}, Alteration{Partitions: 6}},
		{"the same partitions", weekly, Change{Partitions: partitions(3)}, Alteration{}},
		{"the same retention", weekly, Change{Retention: retention(7 * 24 * time.Hour)}, Alteration{}},
		{
			"a shorter retention than the brokers' day keeps tiering valid", weekly,
			Change{Retention: retention(time.Hour)},
			Alteration{Set: map[string]string{"retention.ms": "3600000", "local.retention.ms": "3600000"}},
		},
		{
			"a day or more leaves the brokers' default",
			Current{Partitions: 3},
			Change{Retention: retention(3 * 24 * time.Hour)},
			Alteration{Set: map[string]string{"retention.ms": "259200000"}, Remove: []string{"local.retention.ms"}},
		},
		{
			// The agent tiers it within 30 seconds.
			"a topic not tiered yet",
			Current{Partitions: 3, RetentionMs: &week},
			Change{Retention: retention(time.Hour)},
			Alteration{Set: map[string]string{"retention.ms": "3600000", "local.retention.ms": "3600000"}},
		},
		{
			"both", weekly,
			Change{Partitions: partitions(4), Retention: retention(14 * 24 * time.Hour)},
			Alteration{Partitions: 4, Set: map[string]string{"retention.ms": "1209600000"}, Remove: []string{"local.retention.ms"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := devPolicy(t).CheckChange(tt.current, tt.change, quiet)
			if err != nil {
				t.Fatal(err)
			}
			if got.Partitions != tt.want.Partitions || !maps.Equal(got.Set, tt.want.Set) || !slices.Equal(got.Remove, tt.want.Remove) {
				t.Errorf("CheckChange() = %+v, want %+v", got, tt.want)
			}
			if got.Empty() != tt.want.Empty() {
				t.Errorf("Empty() = %v, want %v", got.Empty(), tt.want.Empty())
			}
		})
	}
}

func TestCheckChangeRefuses(t *testing.T) {
	week := int64(7 * 24 * time.Hour / time.Millisecond)
	weekly := Current{Partitions: 3, RetentionMs: &week}
	partitions := func(n int) *int { return &n }
	retention := func(d time.Duration) *time.Duration { return &d }
	tests := []struct {
		name    string
		current Current
		change  Change
		cluster Cluster
		field   string
		message string
	}{
		{"fewer partitions", weekly, Change{Partitions: partitions(2)}, quiet, "partitions", "not removed: keep at least 3"},
		{"too many partitions", weekly, Change{Partitions: partitions(13)}, quiet, "partitions", "at most 12"},
		{"beyond the replica budget", weekly, Change{Partitions: partitions(12)}, Cluster{Replicas: 995, Brokers: 1}, "partitions", "9 more partitions would exceed"},
		{"a retention not offered", weekly, Change{Retention: retention(2 * time.Hour)}, quiet, "retention", "presets"},
		{"the retention of a compacted topic", Current{Partitions: 3, Compacted: true}, Change{Retention: retention(time.Hour)}, quiet, "retention", "compacted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := devPolicy(t).CheckChange(tt.current, tt.change, tt.cluster)
			var fieldErr *FieldError
			if !errors.As(err, &fieldErr) || !errors.Is(err, ErrInvalid) || fieldErr.Field != tt.field || !strings.Contains(fieldErr.Message, tt.message) {
				t.Errorf("CheckChange() = %v, want a %s error saying %q", err, tt.field, tt.message)
			}
		})
	}
}

// A topic created outside the console with more copies than the profile's
// counts its own toward the replica budget.
func TestCheckChangeCountsTheTopicsOwnReplicas(t *testing.T) {
	more := 9
	current := Current{Partitions: 3, ReplicationFactor: 3}
	if _, err := devPolicy(t).CheckChange(current, Change{Partitions: &more}, Cluster{Replicas: 985, Brokers: 1}); err == nil {
		t.Error("CheckChange() = nil, want the 18 more replicas to exceed the budget")
	}
	if _, err := devPolicy(t).CheckChange(Current{Partitions: 3}, Change{Partitions: &more}, Cluster{Replicas: 985, Brokers: 1}); err != nil {
		t.Errorf("CheckChange() = %v, want 6 more replicas to fit", err)
	}
}

func TestRetention(t *testing.T) {
	for ms, want := range map[int64]bool{3600000: true, 0: false, -1: false, 288230376155311744: false} {
		if d, ok := Retention(ms); ok != want || (ok && d.Milliseconds() != ms) {
			t.Errorf("Retention(%d) = %v, %v, want ok %v", ms, d, ok, want)
		}
	}
}
