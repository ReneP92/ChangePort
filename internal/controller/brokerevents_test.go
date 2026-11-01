package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/ReneP92/changeport/internal/events"
)

type recordedEvents struct{ events []events.Event }

func (r *recordedEvents) Record(e ...events.Event) { r.events = append(r.events, e...) }

func (r *recordedEvents) messages() []string {
	var out []string
	for _, e := range r.events {
		out = append(out, e.Message)
	}
	return out
}

func newBrokerEvents(t *testing.T) (*BrokerEvents, client.Client, *recordedEvents) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	kube := fake.NewClientBuilder().WithScheme(scheme).Build()
	recorded := &recordedEvents{}
	b := &BrokerEvents{
		Client: kube, Recorder: recorded, ReplacementGrace: 30 * time.Second,
		pods: map[types.NamespacedName]brokerPod{}, strimziRestarts: map[types.NamespacedName]time.Time{},
	}
	return b, kube, recorded
}

var brokerKey = types.NamespacedName{Namespace: "customer-acme", Name: "acme-dual-role-0"}

func brokerPodObject(uid string, created time.Time, restarts int32, exited *corev1.ContainerStateTerminated) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: brokerKey.Namespace, Name: brokerKey.Name, UID: types.UID(uid),
			CreationTimestamp: metav1.NewTime(created), Labels: map[string]string{BrokerLabel: "true"},
		},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
			Name: "kafka", RestartCount: restarts, LastTerminationState: corev1.ContainerState{Terminated: exited},
		}}},
	}
}

// put creates or replaces the broker pod as the test's cluster has it.
func put(t *testing.T, kube client.Client, pod *corev1.Pod) {
	t.Helper()
	_ = kube.Delete(context.Background(), &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: pod.Namespace, Name: pod.Name}})
	if err := kube.Create(context.Background(), pod); err != nil {
		t.Fatal(err)
	}
}

func TestStrimziRestartsAreRecordedWithWhy(t *testing.T) {
	b, kube, recorded := newBrokerEvents(t)
	at := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	for i, reason := range []string{"PodHasOldRevision", "CaCertRenewed", "SomethingNew"} {
		event := &corev1.Event{
			ObjectMeta: metav1.ObjectMeta{Namespace: "customer-acme", Name: "e" + string(rune('a'+i)), UID: types.UID("uid-" + reason)},
			Action:     StrimziPodRestart, Reason: reason, Message: "Rolling Pod acme-dual-role-0 due to something new happened",
			EventTime: metav1.NewMicroTime(at.Add(time.Duration(i) * time.Minute)),
			Related:   &corev1.ObjectReference{Kind: "Pod", Name: "acme-dual-role-0", Namespace: "customer-acme"},
		}
		if err := kube.Create(t.Context(), event); err != nil {
			t.Fatal(err)
		}
		if _, err := b.reconcileEvent(t.Context(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "customer-acme", Name: event.Name}}); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{
		"Broker acme-dual-role-0 restarted: a rolling update applied a change to the cluster, such as a new version or setting.",
		"Broker acme-dual-role-0 restarted: its certificates were renewed.",
		"Broker acme-dual-role-0 restarted: something new happened.",
	}
	if got := recorded.messages(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("recorded\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	e := recorded.events[0]
	if e.Tenant != "acme" || e.Kind != events.BrokerRestarted || e.Broker != "acme-dual-role-0" || !e.Time.Equal(at) || e.ID != "strimzi-uid-PodHasOldRevision" {
		t.Errorf("recorded %+v, want acme's broker restart at %v, identified by the Event", e, at)
	}
}

func TestBrokerCrashesAndReplacementsAreRecorded(t *testing.T) {
	b, kube, recorded := newBrokerEvents(t)
	request := ctrl.Request{NamespacedName: brokerKey}
	reconcilePod := func() ctrl.Result {
		t.Helper()
		result, err := b.reconcilePod(t.Context(), request)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	created := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	// Seen first: nothing to compare with.
	put(t, kube, brokerPodObject("p1", created, 0, nil))
	reconcilePod()
	// Its process runs out of memory, and restarts.
	crashed := created.Add(time.Hour)
	put(t, kube, brokerPodObject("p1", created, 1, &corev1.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137, FinishedAt: metav1.NewTime(crashed)}))
	reconcilePod()
	reconcilePod()
	// Strimzi rolls it: its Event comes first, and the new pod is no news.
	b.strimziRestarts[brokerKey] = created.Add(2 * time.Hour)
	put(t, kube, brokerPodObject("p2", created.Add(2*time.Hour+time.Minute), 0, nil))
	if result := reconcilePod(); result.RequeueAfter != 0 {
		t.Errorf("a pod Strimzi restarted waits %v, want no wait", result.RequeueAfter)
	}
	// Its node goes away: a new pod, and no Event from Strimzi.
	replaced := created.Add(5 * time.Hour)
	put(t, kube, brokerPodObject("p3", replaced, 0, nil))
	if result := reconcilePod(); result.RequeueAfter != 30*time.Second {
		t.Errorf("a new pod without Strimzi's Event waits %v, want the grace", result.RequeueAfter)
	}
	reconcilePod()
	want := []string{
		"Broker acme-dual-role-0 restarted: its Kafka process ran out of memory.",
		"Broker acme-dual-role-0 restarted: its pod was replaced, such as when it was evicted or its node went away.",
	}
	if got := recorded.messages(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("recorded\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if len(recorded.events) == 2 && (!recorded.events[0].Time.Equal(crashed) || !recorded.events[1].Time.Equal(replaced)) {
		t.Errorf("recorded at %v and %v, want when it crashed and when the new pod was created", recorded.events[0].Time, recorded.events[1].Time)
	}
}
