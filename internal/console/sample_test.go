package console

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/ReneP92/changeport/api/v1alpha1"
	"github.com/ReneP92/changeport/internal/console/api"
	"github.com/ReneP92/changeport/internal/provisioning"
	"github.com/ReneP92/changeport/internal/topics"
)

// fakeSampleApps keeps sample apps by customer.
type fakeSampleApps struct {
	apps map[string]*v1alpha1.SampleApp
}

func (f *fakeSampleApps) Get(_ context.Context, customerID string) (*v1alpha1.SampleApp, error) {
	if sample, ok := f.apps[customerID]; ok {
		return sample.DeepCopy(), nil
	}
	return nil, nil
}

func (f *fakeSampleApps) Run(_ context.Context, customerID string, stopAt time.Time) (*v1alpha1.SampleApp, error) {
	sample, ok := f.apps[customerID]
	if !ok {
		sample = &v1alpha1.SampleApp{ObjectMeta: metav1.ObjectMeta{Namespace: namespaceOf(customerID), Name: v1alpha1.SampleAppName}}
		f.apps[customerID] = sample
	}
	sample.Spec.StopAt = metav1.NewTime(stopAt)
	return sample.DeepCopy(), nil
}

func (f *fakeSampleApps) Stop(_ context.Context, customerID string) error {
	delete(f.apps, customerID)
	return nil
}

func decodeSampleApp(t *testing.T, body string) api.SampleApp {
	t.Helper()
	var sample api.SampleApp
	if err := json.Unmarshal([]byte(body), &sample); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	return sample
}

// sampleTopics lists the topics marked as the sample app's.
func sampleTopics(t *testing.T, f *fixture, token string) []string {
	t.Helper()
	_, body, _ := get(t, f.handler, "/api/customer/topics", token)
	var list api.TopicList
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	var marked []string
	for _, topic := range list.Topics {
		if topic.Sample != nil && *topic.Sample {
			marked = append(marked, topic.Name)
		}
	}
	return marked
}

func TestOnlyAdminsStartAndStopTheSampleApp(t *testing.T) {
	f := newFixture(t)
	member := f.signer.token(t, session{user: "user_2", org: "org_acme", role: "member"})
	staff := f.signer.token(t, session{user: "user_6", org: staffOrg, role: "admin"})
	for name, token := range map[string]string{"a member": member, "staff": staff} {
		for _, method := range []string{"PUT", "DELETE"} {
			if status, body := send(t, f.handler, method, "/api/customer/sample", token, ""); status != 403 {
				t.Errorf("%s %s the sample app = %d %s, want 403", name, method, status, body)
			}
		}
	}
	if len(f.sampleApps.apps) != 0 {
		t.Errorf("sample apps = %v, want none", f.sampleApps.apps)
	}
}

func TestTheSampleAppRunsForAnHour(t *testing.T) {
	f := newFixture(t)
	admin := f.signer.token(t, session{user: "user_3", org: "org_acme", role: "admin"})
	member := f.signer.token(t, session{user: "user_2", org: "org_acme", role: "member"})

	before := time.Now()
	status, body := send(t, f.handler, "PUT", "/api/customer/sample", admin, "")
	if status != 200 {
		t.Fatalf("PUT the sample app = %d %s", status, body)
	}
	started := decodeSampleApp(t, body)
	if started.State != api.SampleAppStateStarting || started.StopsAt == nil ||
		started.StopsAt.Before(before.Add(time.Hour).Truncate(time.Second)) || started.StopsAt.After(time.Now().Add(time.Hour)) {
		t.Errorf("started sample app = %+v, want it starting for an hour", started)
	}
	if !strings.Contains(f.log.String(), `"msg":"sample app started","audit":true,"tenant":"acme","user":"user_3"`) {
		t.Errorf("no audit line for the start in %s", f.log.String())
	}

	// Members see it run, once the provisioner reports it ready.
	f.sampleApps.apps["acme"].Status.Ready = true
	_, body, _ = get(t, f.handler, "/api/customer/sample", member)
	if got := decodeSampleApp(t, body); got.State != api.SampleAppStateRunning || !got.StopsAt.Equal(*started.StopsAt) {
		t.Errorf("GET the sample app = %+v, want it running until %v", got, started.StopsAt)
	}

	// While it runs, its topic is marked and cannot be deleted.
	if marked := sampleTopics(t, f, member); len(marked) != 0 {
		t.Errorf("topics marked as the sample's = %v, want none before its topic exists", marked)
	}
	if status, body := send(t, f.handler, "DELETE", "/api/customer/topics/"+provisioning.SampleTopic, admin, ""); status != 409 || !strings.Contains(body, "Stop the sample app") {
		t.Errorf("DELETE the sample's topic = %d %s, want 409", status, body)
	}

	// Keeping it running moves its stop time, without a check of its topic,
	// which it owns.
	if err := f.clusters.CreateTopic(t.Context(), "acme", topics.Topic{Name: provisioning.SampleTopic, Partitions: 3, ReplicationFactor: 1}); err != nil {
		t.Fatal(err)
	}
	if status, body = send(t, f.handler, "PUT", "/api/customer/sample", admin, ""); status != 200 {
		t.Fatalf("PUT the running sample app = %d %s", status, body)
	}
	if kept := decodeSampleApp(t, body); kept.State != api.SampleAppStateRunning || kept.StopsAt.Before(*started.StopsAt) {
		t.Errorf("kept sample app = %+v, want it running past %v", kept, started.StopsAt)
	}
	if !strings.Contains(f.log.String(), `"msg":"sample app kept running"`) {
		t.Errorf("no audit line for keeping it running in %s", f.log.String())
	}
	if marked := sampleTopics(t, f, member); len(marked) != 1 || marked[0] != provisioning.SampleTopic {
		t.Errorf("topics marked as the sample's = %v, want %s", marked, provisioning.SampleTopic)
	}

	if status, body = send(t, f.handler, "DELETE", "/api/customer/sample", admin, ""); status != 204 {
		t.Fatalf("DELETE the sample app = %d %s", status, body)
	}
	_, body, _ = get(t, f.handler, "/api/customer/sample", member)
	if got := decodeSampleApp(t, body); got.State != api.SampleAppStateStopped || got.StopsAt != nil {
		t.Errorf("stopped sample app = %+v", got)
	}
	if !strings.Contains(f.log.String(), `"msg":"sample app stopped"`) {
		t.Errorf("no audit line for the stop in %s", f.log.String())
	}
}

// A topic of the sample's name that the sample does not own keeps it from
// starting: stopping it would delete the topic.
func TestTheSampleAppLeavesATopicOfItsNameAlone(t *testing.T) {
	f := newFixture(t)
	admin := f.signer.token(t, session{user: "user_3", org: "org_acme", role: "admin"})
	if err := f.clusters.CreateTopic(t.Context(), "acme", topics.Topic{Name: provisioning.SampleTopic, Partitions: 1, ReplicationFactor: 1}); err != nil {
		t.Fatal(err)
	}
	if status, body := send(t, f.handler, "PUT", "/api/customer/sample", admin, ""); status != 409 || !strings.Contains(body, "Topic sample.orders exists") {
		t.Errorf("PUT the sample app = %d %s, want 409", status, body)
	}
	if len(f.sampleApps.apps) != 0 {
		t.Errorf("sample apps = %v, want none", f.sampleApps.apps)
	}

	f.clusters.notReady["globex"] = true
	globex := f.signer.token(t, session{user: "user_7", org: "org_globex", role: "admin"})
	if status, body := send(t, f.handler, "PUT", "/api/customer/sample", globex, ""); status != 409 || !strings.Contains(body, "not ready yet") {
		t.Errorf("PUT the sample app on a cluster that is not ready = %d %s, want 409", status, body)
	}
}

func TestKubeSampleAppsRunsKeepsAndStops(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&v1alpha1.SampleApp{}).Build()
	apps := KubeSampleApps{Client: c}
	if sample, err := apps.Get(t.Context(), "acme"); err != nil || sample != nil {
		t.Fatalf("Get before a start = %v, %v; want none", sample, err)
	}
	first := time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)
	if _, err := apps.Run(t.Context(), "acme", first); err != nil {
		t.Fatal(err)
	}
	later := first.Add(30 * time.Minute)
	if _, err := apps.Run(t.Context(), "acme", later); err != nil {
		t.Fatal(err)
	}
	sample, err := apps.Get(t.Context(), "acme")
	if err != nil || sample == nil {
		t.Fatalf("Get = %v, %v", sample, err)
	}
	if sample.Namespace != "customer-acme" || sample.Name != v1alpha1.SampleAppName || !sample.Spec.StopAt.Time.Equal(later) {
		t.Errorf("sample app = %s/%s until %v, want customer-acme/sample until %v", sample.Namespace, sample.Name, sample.Spec.StopAt, later)
	}
	for range 2 {
		if err := apps.Stop(t.Context(), "acme"); err != nil {
			t.Fatal(err)
		}
	}
	if sample, err := apps.Get(t.Context(), "acme"); err != nil || sample != nil {
		t.Errorf("Get after a stop = %v, %v; want none", sample, err)
	}
}
