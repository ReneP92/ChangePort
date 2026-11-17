package metrics

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"
)

// The tests here run the console's queries on a VictoriaMetrics that holds
// series as the brokers report them in situations that are hard to stage:
// the store's samples missing around a window's start, or a broker cut off
// from the others. They need a VictoriaMetrics to import into, at
// CHANGEPORT_VICTORIA_METRICS_URL (hack/usage-test.sh), and remove what they
// imported.

// imported is one series to import, in VictoriaMetrics' JSON line format.
type imported struct {
	Metric     map[string]string `json:"metric"`
	Values     []float64         `json:"values"`
	Timestamps []int64           `json:"timestamps"`
}

// importSeries imports series under a tenant of their own, which it returns,
// and removes them once the test is over. Each series' metric names its
// __name__ and labels but the tenant.
func importSeries(t *testing.T, prefix string, series ...imported) (*Client, string) {
	t.Helper()
	base := os.Getenv("CHANGEPORT_VICTORIA_METRICS_URL")
	if base == "" {
		t.Skip("CHANGEPORT_VICTORIA_METRICS_URL is not set")
	}
	tenant := fmt.Sprintf("%s-%d", prefix, rand.N(1_000_000))
	var body bytes.Buffer
	for _, s := range series {
		s.Metric["tenant"] = tenant
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
		if body == nil {
			body = &bytes.Buffer{}
		}
		response, err := http.Post(target, "application/json", body)
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
	return client, tenant
}

// eventually runs q until it returns a series for each of want's keys, by
// label, or 30 seconds pass, as imports take a moment to be searchable.
func eventually(t *testing.T, client *Client, tenant string, q Query, filter Filter, at time.Time, label string, want int) map[string]float64 {
	t.Helper()
	got := map[string]float64{}
	for attempt := 0; attempt < 30 && len(got) < want; attempt++ {
		time.Sleep(time.Second)
		result, err := client.Instant(t.Context(), tenant, q, filter, at)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range result {
			got[s.Labels[label]] = s.Points[0].Value
		}
	}
	return got
}

// TestUsageAgainstVictoriaMetrics: with the store's samples missing around
// the window's start, as while it restarts, usage counts what each counter
// grew by, not its whole value, and a topic born in the window from 0.
func TestUsageAgainstVictoriaMetrics(t *testing.T) {
	// A midnight two days ago, within the store's retention.
	midnight := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -2)
	end := midnight.Add(12 * time.Hour)
	const step = 30 * time.Second
	const perStep = 100.0

	// old: from a day before until the end, 1e12 bytes already and 100 more
	// every 30 seconds, without samples from 10 minutes before midnight to
	// 10 minutes after. new: born at 06:00, 100 bytes at its first sample.
	metric := func(topic string) map[string]string {
		return map[string]string{"__name__": "kafka_server_brokertopicmetrics_bytesinpersec_total", "topic": topic}
	}
	old, born := imported{Metric: metric("old")}, imported{Metric: metric("new")}
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
	client, tenant := importSeries(t, "usage-test", old, born)

	got := eventually(t, client, tenant, UsageIn, Filter{Window: end.Sub(midnight)}, end, "topic", 2)
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

// TestControllerCountsAgainstVictoriaMetrics: with a broker cut off from the
// others, which keeps reporting the brokers it last knew, the active and
// fenced brokers are the active controller's counts (hack/chaos-test.sh).
func TestControllerCountsAgainstVictoriaMetrics(t *testing.T) {
	at := time.Now().Truncate(time.Minute)
	// Each node's counts: node 1 is cut off, and still counts what it last
	// knew, more active brokers and more fenced ones than now; node 0 is the
	// active controller.
	nodes := []struct {
		id                    string
		active, brokers, fenc float64
	}{{"0", 1, 2, 1}, {"1", 0, 3, 2}, {"2", 0, 2, 1}}
	var series []imported
	for _, n := range nodes {
		for name, v := range map[string]float64{
			"kafka_controller_kafkacontroller_activecontrollercount": n.active,
			"kafka_controller_kafkacontroller_activebrokercount":     n.brokers,
			"kafka_controller_kafkacontroller_fencedbrokercount":     n.fenc,
		} {
			series = append(series, imported{
				Metric:     map[string]string{"__name__": name, "node_id": n.id},
				Values:     []float64{v, v},
				Timestamps: []int64{at.Add(-time.Minute).UnixMilli(), at.UnixMilli()},
			})
		}
	}
	client, tenant := importSeries(t, "controller-test", series...)
	for q, want := range map[Query]float64{ActiveBrokers: 2, FencedBrokers: 1} {
		got := eventually(t, client, tenant, q, Filter{}, at, "", 1)
		if got[""] != want {
			t.Errorf("%s = %v, want %v, the active controller's", q, got[""], want)
		}
	}
}

// TestFleetAgainstVictoriaMetrics: each fleet query gives every tenant what
// its per-cluster query gives that tenant alone, here two clusters, one with
// a broker cut off as above and partitions behind, and the fullest volume
// of each.
func TestFleetAgainstVictoriaMetrics(t *testing.T) {
	at := time.Now().Truncate(time.Minute)
	gauge := func(name string, value float64, labels ...string) imported {
		metric := map[string]string{"__name__": name}
		for i := 0; i+1 < len(labels); i += 2 {
			metric[labels[i]] = labels[i+1]
		}
		return imported{Metric: metric, Values: []float64{value, value}, Timestamps: []int64{at.Add(-time.Minute).UnixMilli(), at.UnixMilli()}}
	}
	// node: active controller, active brokers, fenced brokers, under-
	// replicated, under min ISR, offline.
	cluster := func(nodes [][6]float64, volumes ...float64) []imported {
		var series []imported
		for i, n := range nodes {
			id := strconv.Itoa(i)
			series = append(series,
				gauge("kafka_controller_kafkacontroller_activecontrollercount", n[0], "node_id", id),
				gauge("kafka_controller_kafkacontroller_activebrokercount", n[1], "node_id", id),
				gauge("kafka_controller_kafkacontroller_fencedbrokercount", n[2], "node_id", id),
				gauge("kafka_server_replicamanager_underreplicatedpartitions", n[3], "node_id", id),
				gauge("kafka_server_replicamanager_underminisrpartitioncount", n[4], "node_id", id),
				gauge("kafka_controller_kafkacontroller_offlinepartitionscount", n[5], "node_id", id),
			)
		}
		for i, used := range volumes {
			claim := "data-" + strconv.Itoa(i)
			series = append(series,
				gauge("kubelet_volume_stats_used_bytes", used, "persistentvolumeclaim", claim),
				gauge("kubelet_volume_stats_capacity_bytes", 100, "persistentvolumeclaim", claim))
		}
		return series
	}
	client, cut := importSeries(t, "fleet-test", cluster([][6]float64{{1, 2, 1, 3, 1, 0}, {0, 3, 2, 2, 0, 0}, {0, 2, 1, 0, 0, 0}}, 40, 85, 60)...)
	_, healthy := importSeries(t, "fleet-test", cluster([][6]float64{{0, 3, 0, 0, 0, 0}, {1, 3, 0, 0, 0, 0}, {0, 3, 0, 0, 0, 0}})...)

	// fleet waits until q has tenant's series, as imports take a moment to
	// be searchable.
	fleet := func(q FleetQuery, tenant string) (float64, bool) {
		for attempt := 0; attempt < 30; attempt++ {
			values, err := client.Fleet(t.Context(), q, at)
			if err != nil {
				t.Fatal(err)
			}
			if value, ok := values[tenant]; ok {
				return value, true
			}
			time.Sleep(time.Second)
		}
		return 0, false
	}
	twins := map[FleetQuery]Query{
		FleetUnderReplicated: UnderReplicated, FleetUnderMinISR: UnderMinISR, FleetOfflinePartitions: OfflinePartitions,
		FleetActiveControllers: ActiveControllers, FleetActiveBrokers: ActiveBrokers, FleetFencedBrokers: FencedBrokers,
	}
	for q, twin := range twins {
		for _, tenant := range []string{cut, healthy} {
			want := eventually(t, client, tenant, twin, Filter{}, at, "", 1)[""]
			if value, ok := fleet(q, tenant); !ok || value != want {
				t.Errorf("%s for %s = %v (%v), want %v, as %s gives", q, tenant, value, ok, want, twin)
			}
		}
	}
	// The cut-off cluster counts its active controller's view: 2 active, 1
	// fenced; its fullest volume is 85% full, and the other reports none.
	if used, _ := fleet(FleetVolumeUsed, cut); used != 0.85 {
		t.Errorf("the fullest volume of %s is %v full, want 0.85", cut, used)
	}
	volumes, err := client.Fleet(t.Context(), FleetVolumeUsed, at)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := volumes[healthy]; ok {
		t.Errorf("%s reports no volumes, but has %v", healthy, volumes[healthy])
	}
}
