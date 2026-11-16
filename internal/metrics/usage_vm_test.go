package metrics

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"
)

// TestUsageAgainstVictoriaMetrics runs the usage queries on a VictoriaMetrics
// that holds counters as the brokers report them, with the store's samples
// missing around the window's start, as while it restarts: the usage counts
// what each counter grew by, not its whole value, and a topic born in the
// window from 0. It needs a VictoriaMetrics to import into, at
// CHANGEPORT_VICTORIA_METRICS_URL (hack/usage-test.sh), and removes what it
// imported.
func TestUsageAgainstVictoriaMetrics(t *testing.T) {
	base := os.Getenv("CHANGEPORT_VICTORIA_METRICS_URL")
	if base == "" {
		t.Skip("CHANGEPORT_VICTORIA_METRICS_URL is not set")
	}
	tenant := fmt.Sprintf("usage-test-%d", rand.N(1_000_000))
	// A midnight two days ago, within the store's retention.
	midnight := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -2)
	end := midnight.Add(12 * time.Hour)
	const step = 30 * time.Second
	const perStep = 100.0

	// old: from a day before until the end, 1e12 bytes already and 100 more
	// every 30 seconds, without samples from 10 minutes before midnight to
	// 10 minutes after. new: born at 06:00, 100 bytes at its first sample.
	type series struct {
		Metric     map[string]string `json:"metric"`
		Values     []float64         `json:"values"`
		Timestamps []int64           `json:"timestamps"`
	}
	metric := func(topic string) map[string]string {
		return map[string]string{"__name__": "kafka_server_brokertopicmetrics_bytesinpersec_total", "tenant": tenant, "topic": topic}
	}
	old, born := series{Metric: metric("old")}, series{Metric: metric("new")}
	value := 1e12
	for at := midnight.Add(-24 * time.Hour); !at.After(end); at = at.Add(step) {
		value += perStep
		if at.After(midnight.Add(-10*time.Minute)) && at.Before(midnight.Add(10*time.Minute)) {
			continue
		}
		old.Values, old.Timestamps = append(old.Values, value), append(old.Timestamps, at.UnixMilli())
	}
	for at, v := midnight.Add(6*time.Hour), perStep; !at.After(end); at, v = at.Add(step), v+perStep {
		born.Values, born.Timestamps = append(born.Values, v), append(born.Timestamps, at.UnixMilli())
	}
	var body bytes.Buffer
	for _, s := range []series{old, born} {
		if err := json.NewEncoder(&body).Encode(s); err != nil {
			t.Fatal(err)
		}
	}
	post := func(path string, values url.Values, body *bytes.Buffer) {
		t.Helper()
		target := base + path
		if values != nil {
			target += "?" + values.Encode()
		}
		var reader *bytes.Buffer
		if body == nil {
			reader = &bytes.Buffer{}
		} else {
			reader = body
		}
		response, err := http.Post(target, "application/json", reader)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode >= 300 {
			t.Fatalf("POST %s = %s", path, response.Status)
		}
	}
	post("/api/v1/import", nil, &body)
	t.Cleanup(func() {
		post("/api/v1/admin/tsdb/delete_series", url.Values{"match[]": {fmt.Sprintf(`{tenant=%q}`, tenant)}}, nil)
	})
	post("/internal/force_flush", nil, nil)

	client, err := NewClient(base)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]float64{}
	for attempt := 0; attempt < 30 && len(got) < 2; attempt++ {
		time.Sleep(time.Second)
		result, err := client.Instant(t.Context(), tenant, UsageIn, Filter{Window: end.Sub(midnight)}, end)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range result {
			got[s.Labels["topic"]] = s.Points[0].Value
		}
	}
	// old grew 100 every 30 seconds over the 12 hours; what it grew while
	// the store had no samples of it, the 10 minutes after midnight, is not
	// known, and is left out rather than its whole value counted.
	if v := got["old"]; v < (12*120-20)*perStep || v > 12*120*perStep {
		t.Errorf("old grew %.0f bytes over the window, want about %.0f, at most the gap's less", v, 12*120*perStep)
	}
	// new counts from 0: 100 at 06:00 and 100 more every 30 seconds.
	if want := (6*120 + 1) * perStep; got["new"] != want {
		t.Errorf("new grew %.0f bytes, want %.0f", got["new"], want)
	}
}
