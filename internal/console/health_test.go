package console

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/ReneP92/changeport/api/v1alpha1"
	"github.com/ReneP92/changeport/internal/console/api"
	"github.com/ReneP92/changeport/internal/diagnosis"
	"github.com/ReneP92/changeport/internal/metrics"
)

// A history tells each step's worst level, healthy where the monitor looked
// and found nothing, and nothing where it did not look.
func TestHealthHistory(t *testing.T) {
	start := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	end := start.Add(4 * healthStep)
	step := func(n int) time.Time { return start.Add(time.Duration(n) * healthStep) }
	points := func(values map[int]float64) []metrics.Point {
		var out []metrics.Point
		for n := 1; n <= 4; n++ {
			if v, ok := values[n]; ok {
				out = append(out, metrics.Point{Time: step(n), Value: v})
			}
		}
		return out
	}
	levels := []metrics.Series{{Labels: map[string]string{"topic": "orders"}, Points: points(map[int]float64{2: 2, 3: 1})}}
	// The monitor looked in the first three steps, and not in the last.
	checked := []metrics.Series{{Labels: map[string]string{}, Points: points(map[int]float64{1: 1, 2: 1, 3: 1})}}
	history := healthHistory(start, end, levels, checked)
	var got []string
	for _, p := range history.Points {
		got = append(got, string(ptr.Deref(p.Level, "unknown")))
	}
	if want := "healthy atRisk needsAttention unknown"; strings.Join(got, " ") != want || history.StepSeconds != 900 {
		t.Errorf("history = %v every %ds, want %s every 900s", got, history.StepSeconds, want)
	}
}

func TestHealthHistoryNamesOneSubject(t *testing.T) {
	f := newFixture(t)
	admin := f.signer.token(t, session{user: "user_3", org: "org_acme", role: "admin"})
	for _, path := range []string{"/api/customer/health-history", "/api/customer/health-history?topic=a&consumerGroup=b"} {
		if status, body, _ := get(t, f.handler, path, admin); status != http.StatusBadRequest {
			t.Errorf("GET %s = %d %s, want 400", path, status, body)
		}
	}
}

// The monitor looks at every ready customer, and records which it looked
// at; one that is not ready is left out.
func TestHealthMonitorLooksAtReadyCustomers(t *testing.T) {
	f := newFixture(t)
	f.customers.customers[0].Status.Conditions = []metav1.Condition{{Type: v1alpha1.ConditionReady, Status: metav1.ConditionFalse, Reason: "Pending"}}
	f.server.lookAtHealth(t.Context())
	want := `
# HELP changeport_health_checked 1 for each customer whose cluster the console diagnosed in its last look.
# TYPE changeport_health_checked gauge
changeport_health_checked{tenant="acme"} 1
`
	if err := testutil.CollectAndCompare(f.server.HealthCollector(), strings.NewReader(want), "changeport_health_checked"); err != nil {
		t.Error(err)
	}
}

// Without the diagnoses, lists show no health rather than a healthy one.
func TestLevelsUnknown(t *testing.T) {
	if got := (levels{}).topic("orders"); got != nil {
		t.Errorf("unknown levels = %+v, want none", got)
	}
	known := levels{known: true}
	if got := known.group("billing"); got == nil || got.Level != api.Healthy || len(got.Diagnoses) != 0 {
		t.Errorf("a group without findings = %+v, want healthy", got)
	}
}

// The monitor exports the levels of the topics and groups that are not
// healthy, each with its own tenant.
func TestHealthMonitorExportsLevels(t *testing.T) {
	m := &healthMonitor{latest: map[string]levels{
		"acme": {
			known:  true,
			topics: map[string]diagnosis.Health{"orders": {Level: diagnosis.NeedsAttention}},
			groups: map[string]diagnosis.Health{"billing": {Level: diagnosis.AtRisk}},
		},
	}}
	want := `
# HELP changeport_health_level How healthy a topic or consumer group is, from its diagnoses: 1 needs attention, 2 at risk. Healthy ones have no series.
# TYPE changeport_health_level gauge
changeport_health_level{consumer_group="",tenant="acme",topic="orders"} 1
changeport_health_level{consumer_group="billing",tenant="acme",topic=""} 2
`
	if err := testutil.CollectAndCompare(m, strings.NewReader(want), "changeport_health_level"); err != nil {
		t.Error(err)
	}
}

// The lists carry each topic's and group's health.
func TestListsCarryHealth(t *testing.T) {
	f := newFixture(t)
	member := f.signer.token(t, session{user: "user_2", org: "org_acme", role: "member"})
	for _, path := range []string{"/api/customer/topics", "/api/customer/consumer-groups", "/api/customer/topology"} {
		if status, body, _ := get(t, f.handler, path, member); status != http.StatusOK || !strings.Contains(body, `"health":{"diagnoses":[],"level":"healthy"}`) {
			t.Errorf("GET %s = %d %s, want each with its health", path, status, body)
		}
	}
}

// A level beyond at risk, which no rule makes, is at risk rather than
// healthy.
func TestAPILevel(t *testing.T) {
	for level, want := range map[diagnosis.Level]api.HealthLevel{
		diagnosis.Healthy: api.Healthy, diagnosis.NeedsAttention: api.NeedsAttention,
		diagnosis.AtRisk: api.AtRisk, 10: api.AtRisk,
	} {
		if got := apiLevel(level); got != want {
			t.Errorf("apiLevel(%d) = %s, want %s", level, got, want)
		}
	}
}
