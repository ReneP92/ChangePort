package console

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/ReneP92/changeport/api/v1alpha1"
	"github.com/ReneP92/changeport/internal/console/api"
	"github.com/ReneP92/changeport/internal/metrics"
)

// usageMetrics answers the usage queries for 15 November 2026 at 13:00 UTC:
// orders processed i bytes in and 2i out on the i-th day of the period,
// 7 and 3 today so far, and payments only over the whole period; the
// platform's __consumer_offsets is in every answer, to be left out.
type usageMetrics struct {
	t *testing.T
	// ranges are the daily ranges asked for.
	ranges []string
}

var usageNow = time.Date(2026, 11, 15, 13, 0, 0, 0, time.UTC)

func usageSeries(topic string, at time.Time, v float64) metrics.Series {
	return metrics.Series{Labels: map[string]string{"topic": topic}, Points: []metrics.Point{{Time: at, Value: v}}}
}

func (m *usageMetrics) Instant(_ context.Context, _ string, q metrics.Query, filter metrics.Filter, at time.Time) ([]metrics.Series, error) {
	reserved := usageSeries("__consumer_offsets", at, 999)
	switch {
	case q == metrics.LocalSize:
		return []metrics.Series{usageSeries("orders", at, 40), reserved}, nil
	case q == metrics.RemoteSize:
		return []metrics.Series{usageSeries("payments", at, 60), reserved}, nil
	case filter.Window == 13*time.Hour && q == metrics.UsageIn:
		return []metrics.Series{usageSeries("orders", at, 7), reserved}, nil
	case filter.Window == 13*time.Hour && q == metrics.UsageOut:
		return []metrics.Series{usageSeries("orders", at, 3), reserved}, nil
	case filter.Window == 29*24*time.Hour+13*time.Hour && q == metrics.UsageIn:
		return []metrics.Series{usageSeries("orders", at, 500), usageSeries("payments", at, 100.4), reserved}, nil
	case filter.Window == 29*24*time.Hour+13*time.Hour && q == metrics.UsageOut:
		return []metrics.Series{usageSeries("orders", at, 300), usageSeries("payments", at, 900), reserved}, nil
	}
	m.t.Errorf("unexpected instant query %s with %+v", q, filter)
	return nil, nil
}

func (m *usageMetrics) Range(_ context.Context, _ string, q metrics.Query, filter metrics.Filter, start, end time.Time, step time.Duration) ([]metrics.Series, error) {
	m.ranges = append(m.ranges, fmt.Sprintf("%s %v %s..%s every %v", q, filter.Window, start.Format(time.DateOnly), end.Format(time.DateOnly), step))
	scale := 1.0
	if q == metrics.UsageOut {
		scale = 2
	}
	orders := metrics.Series{Labels: map[string]string{"topic": "orders"}}
	reserved := metrics.Series{Labels: map[string]string{"topic": "__consumer_offsets"}}
	for t, i := start, 1.0; !t.After(end); t, i = t.Add(step), i+1 {
		orders.Points = append(orders.Points, metrics.Point{Time: t, Value: i * scale})
		reserved.Points = append(reserved.Points, metrics.Point{Time: t, Value: 999})
	}
	return []metrics.Series{orders, reserved}, nil
}

func TestUsage(t *testing.T) {
	m := &usageMetrics{t: t}
	s := &Server{metrics: m}
	usage, err := s.usage(t.Context(), "acme", usageNow)
	if err != nil {
		t.Fatal(err)
	}
	// One range per direction, over the 29 full days: each point at a
	// midnight counts the day before it.
	if want := "usage_in 24h0m0s 2026-10-18..2026-11-15 every 24h0m0s"; len(m.ranges) != 2 || !strings.Contains(strings.Join(m.ranges, "|"), want) {
		t.Errorf("ranges %v, want %q and its usage_out", m.ranges, want)
	}
	if len(usage.Days) != 30 {
		t.Fatalf("%d days, want 30", len(usage.Days))
	}
	first, before, today := usage.Days[0], usage.Days[28], usage.Days[29]
	if got := fmt.Sprintf("%s %d %d", first.Date.Format(time.DateOnly), first.BytesIn, first.BytesOut); got != "2026-10-17 1 2" {
		t.Errorf("the first day = %s, want 2026-10-17 with 1 in and 2 out", got)
	}
	if got := fmt.Sprintf("%s %d %d", before.Date.Format(time.DateOnly), before.BytesIn, before.BytesOut); got != "2026-11-14 29 58" {
		t.Errorf("yesterday = %s, want 2026-11-14 with 29 in and 58 out", got)
	}
	if got := fmt.Sprintf("%s %d %d", today.Date.Format(time.DateOnly), today.BytesIn, today.BytesOut); got != "2026-11-15 7 3" {
		t.Errorf("today = %s, want 2026-11-15 with 7 in and 3 out so far", got)
	}

	// The period's totals and each topic's, the most processed first,
	// without the platform's topics.
	if usage.BytesIn != 600 || usage.BytesOut != 1200 || usage.LocalBytes != 40 || usage.RemoteBytes != 60 {
		t.Errorf("totals %d in, %d out, %d local, %d remote, want 600, 1200, 40 and 60", usage.BytesIn, usage.BytesOut, usage.LocalBytes, usage.RemoteBytes)
	}
	want := []api.TopicUsage{
		{Topic: "payments", BytesIn: 100, BytesOut: 900, RemoteBytes: 60},
		{Topic: "orders", BytesIn: 500, BytesOut: 300, LocalBytes: 40},
	}
	if fmt.Sprint(usage.Topics) != fmt.Sprint(want) {
		t.Errorf("topics %+v, want %+v", usage.Topics, want)
	}
}

func TestUsageRoutes(t *testing.T) {
	f := newFixture(t)
	member := f.signer.token(t, session{user: "user_2", org: "org_acme", role: "member"})
	staff := f.signer.token(t, session{user: "user_6", org: staffOrg, role: "member"})

	// Members see their environment's usage, and their organization's.
	if status, body := send(t, f.handler, http.MethodGet, "/api/customer/usage", member, ""); status != 200 ||
		!strings.Contains(body, `"topic":"acme-orders"`) || strings.Count(body, `"date":`) != 30 {
		t.Errorf("GET /api/customer/usage = %d %s, want 30 days and acme-orders", status, body)
	}
	if status, body := send(t, f.handler, http.MethodGet, "/api/customer/usage/environments", member, ""); status != 200 ||
		body != `{"environments":[{"bytesIn":1024,"bytesOut":1024,"customerID":"acme"}]}`+"\n" {
		t.Errorf("GET /api/customer/usage/environments = %d %s", status, body)
	}

	// Operators see every customer's; customers see none of it.
	if status, body := send(t, f.handler, http.MethodGet, "/api/operator/usage", member, ""); status != 403 {
		t.Errorf("a member's GET /api/operator/usage = %d %s, want 403", status, body)
	}
	status, body := send(t, f.handler, http.MethodGet, "/api/operator/usage", staff, "")
	if status != 200 || !strings.Contains(body, `{"bytesIn":1024,"bytesOut":1024,"customerID":"acme"}`) || !strings.Contains(body, `"customerID":"globex"`) {
		t.Errorf("an operator's GET /api/operator/usage = %d %s, want every customer", status, body)
	}
}

// failingMetrics answers for every tenant but one.
type failingMetrics struct {
	Metrics
	failing string
}

func (m failingMetrics) Instant(ctx context.Context, tenant string, q metrics.Query, filter metrics.Filter, at time.Time) ([]metrics.Series, error) {
	if tenant == m.failing {
		return nil, errors.New("the store is down")
	}
	return m.Metrics.Instant(ctx, tenant, q, filter, at)
}

// A customer the metrics store does not answer for is not known; the others
// still are.
func TestCustomersUsageWithoutTheStore(t *testing.T) {
	s := &Server{metrics: failingMetrics{Metrics: fakeMetrics{}, failing: "globex"}, logger: slog.New(slog.DiscardHandler)}
	usage := s.customersUsage(t.Context(), []v1alpha1.Customer{
		{ObjectMeta: metav1.ObjectMeta{Name: "acme"}}, {ObjectMeta: metav1.ObjectMeta{Name: "globex"}},
	})
	if len(usage) != 2 || usage[0].BytesIn == nil || *usage[0].BytesIn != 1024 || usage[1].CustomerID != "globex" || usage[1].BytesIn != nil || usage[1].BytesOut != nil {
		t.Errorf("usage = %+v, want acme's and globex's not known", usage)
	}
}
