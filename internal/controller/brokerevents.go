package controller

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/ReneP92/changeport/internal/events"
	"github.com/ReneP92/changeport/internal/provisioning"
)

// StrimziReportingController reports the Kubernetes Events of the Strimzi
// cluster operator, and StrimziPodRestart is the action of those it records
// when it restarts a pod.
const (
	StrimziReportingController = "strimzi.io/cluster-operator"
	StrimziPodRestart          = "StrimziInitiatedPodRestart"
)

// BrokerLabel marks the pods that run brokers.
const BrokerLabel = "strimzi.io/broker-role"

// EventRecorder keeps events for the timeline; *events.Recorder implements
// it.
type EventRecorder interface {
	Record(events ...events.Event)
}

// BrokerEvents records the restarts of customers' brokers for the timeline
// (docs/timeline.md): those Strimzi makes, as its Kubernetes Events say why;
// a broker's process that exits and restarts; and a broker's pod that is
// replaced otherwise, such as when its node goes away.
type BrokerEvents struct {
	Client   client.Client
	Recorder EventRecorder
	// ReplacementGrace is how long a new broker pod waits for Strimzi's
	// Event that it restarted the pod, before it counts as replaced
	// otherwise.
	ReplacementGrace time.Duration

	mu sync.Mutex
	// pods are the broker pods last seen, and strimziRestarts when Strimzi
	// last restarted each.
	pods            map[types.NamespacedName]brokerPod
	strimziRestarts map[types.NamespacedName]time.Time
}

type brokerPod struct {
	uid      types.UID
	restarts int32
	// replacing is set while a new pod waits out ReplacementGrace.
	replacing bool
}

// SetupWithManager registers the two halves with mgr. The manager's cache
// must hold only broker pods, and only Strimzi's Events (BrokerEventsCache).
func (b *BrokerEvents) SetupWithManager(mgr ctrl.Manager) error {
	b.pods = map[types.NamespacedName]brokerPod{}
	b.strimziRestarts = map[types.NamespacedName]time.Time{}
	customer := predicate.NewPredicateFuncs(func(o client.Object) bool {
		return strings.HasPrefix(o.GetNamespace(), provisioning.NamespacePrefix)
	})
	if err := ctrl.NewControllerManagedBy(mgr).
		Named("strimzi-restarts").
		For(&corev1.Event{}, builder.WithPredicates(customer, predicate.Funcs{
			// Only new Events; an update repeats one.
			UpdateFunc: func(event.UpdateEvent) bool { return false },
		})).
		Complete(reconcileFunc(b.reconcileEvent)); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		Named("broker-pods").
		For(&corev1.Pod{}, builder.WithPredicates(customer)).
		Complete(reconcileFunc(b.reconcilePod))
}

// BrokerEventsCache narrows the manager's cache to what BrokerEvents
// watches: the pods of brokers, and Strimzi's Events.
func BrokerEventsCache() map[client.Object]cache.ByObject {
	return map[client.Object]cache.ByObject{
		&corev1.Pod{}:   {Label: labels.SelectorFromSet(labels.Set{BrokerLabel: "true"})},
		&corev1.Event{}: {Field: fields.OneTermEqualSelector("reportingComponent", StrimziReportingController)},
	}
}

// reconcileFunc is a function as a reconciler.
type reconcileFunc func(context.Context, ctrl.Request) (ctrl.Result, error)

func (f reconcileFunc) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	return f(ctx, request)
}

// reconcileEvent records a restart Strimzi made, with why.
func (b *BrokerEvents) reconcileEvent(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	var event corev1.Event
	if err := b.Client.Get(ctx, request.NamespacedName, &event); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if event.Action != StrimziPodRestart || event.Related == nil || event.Related.Kind != "Pod" {
		return ctrl.Result{}, nil
	}
	tenant, ok := strings.CutPrefix(event.Namespace, provisioning.NamespacePrefix)
	if !ok {
		return ctrl.Result{}, nil
	}
	at := eventTime(&event)
	pod := types.NamespacedName{Namespace: event.Namespace, Name: event.Related.Name}
	b.mu.Lock()
	if at.After(b.strimziRestarts[pod]) {
		b.strimziRestarts[pod] = at
	}
	b.mu.Unlock()
	b.Recorder.Record(events.Event{
		ID: "strimzi-" + string(event.UID), Time: at, Tenant: tenant, Kind: events.BrokerRestarted, Broker: pod.Name,
		Message: fmt.Sprintf("Broker %s restarted: %s.", pod.Name, strimziReason(event.Reason, event.Message)),
		Source:  "provisioner",
	})
	return ctrl.Result{}, nil
}

// eventTime is when an Event happened, as whichever of its times is set.
func eventTime(e *corev1.Event) time.Time {
	switch {
	case !e.EventTime.IsZero():
		return e.EventTime.Time
	case !e.LastTimestamp.IsZero():
		return e.LastTimestamp.Time
	case !e.FirstTimestamp.IsZero():
		return e.FirstTimestamp.Time
	}
	return e.CreationTimestamp.Time
}

// strimziReason says why Strimzi restarted a broker, from the reason of its
// Event; an unknown reason falls back to Strimzi's own words.
func strimziReason(reason, message string) string {
	switch reason {
	case "ManualRollingUpdate":
		return "an operator asked Strimzi to roll it"
	case "PodHasOldRevision":
		return "a rolling update applied a change to the cluster, such as a new version or setting"
	case "ConfigChangeRequiresRestart":
		return "a change of its configuration needed a restart"
	case "CaCertRenewed", "CaCertHasOldGeneration", "CaCertRemoved", "ClientCaCertKeyReplaced", "ClusterCaCertKeyReplaced",
		"CustomListenerCaCertChange", "KafkaCertificatesChanged":
		return "its certificates were renewed"
	case "FileSystemResizeNeeded", "JbodVolumesChanged":
		return "its storage was resized"
	case "PodForceRestartOnError", "PodStuck", "PodUnresponsive":
		return "it was stuck or did not respond"
	}
	if _, why, ok := strings.Cut(message, " due to "); ok {
		return strings.TrimSuffix(why, ".")
	}
	return "Strimzi rolled it (" + reason + ")"
}

// reconcilePod records a broker's process that restarted, and a broker's
// pod that was replaced without Strimzi restarting it.
func (b *BrokerEvents) reconcilePod(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	var pod corev1.Pod
	err := b.Client.Get(ctx, request.NamespacedName, &pod)
	if apierrors.IsNotFound(err) {
		// The pod's replacement, if any, compares with what was last seen.
		return ctrl.Result{}, nil
	}
	if err != nil {
		return ctrl.Result{}, err
	}
	tenant, ok := strings.CutPrefix(pod.Namespace, provisioning.NamespacePrefix)
	if !ok || pod.Labels[BrokerLabel] != "true" {
		return ctrl.Result{}, nil
	}
	restarts, exited := kafkaRestarts(&pod)
	b.mu.Lock()
	defer b.mu.Unlock()
	last, seen := b.pods[request.NamespacedName]
	current := brokerPod{uid: pod.UID, restarts: restarts}
	switch {
	case !seen:
		// Seen for the first time: a new cluster, or the provisioner just
		// started; nothing to compare with.
	case last.uid != pod.UID || last.replacing:
		created := pod.CreationTimestamp.Time
		// Strimzi records its restart before it deletes the pod.
		if b.strimziRestarts[request.NamespacedName].After(created.Add(-10 * time.Minute)) {
			break
		}
		if !last.replacing {
			current.replacing = true
			b.pods[request.NamespacedName] = current
			return ctrl.Result{RequeueAfter: b.ReplacementGrace}, nil
		}
		b.Recorder.Record(events.Event{
			ID: "replaced-" + string(pod.UID), Time: created, Tenant: tenant, Kind: events.BrokerRestarted, Broker: pod.Name,
			Message: fmt.Sprintf("Broker %s restarted: its pod was replaced, such as when it was evicted or its node went away.", pod.Name),
			Source:  "provisioner",
		})
	case restarts > last.restarts && exited != nil:
		b.Recorder.Record(events.Event{
			ID:   fmt.Sprintf("crashed-%s-%d", pod.UID, restarts),
			Time: exited.FinishedAt.Time, Tenant: tenant, Kind: events.BrokerRestarted, Broker: pod.Name,
			Message: fmt.Sprintf("Broker %s restarted: its Kafka process %s.", pod.Name, exitReason(exited)),
			Source:  "provisioner",
		})
	}
	b.pods[request.NamespacedName] = current
	return ctrl.Result{}, nil
}

// kafkaRestarts returns how often the Kafka container of a broker pod
// restarted, and how it last exited, if it did.
func kafkaRestarts(pod *corev1.Pod) (int32, *corev1.ContainerStateTerminated) {
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == "kafka" {
			return status.RestartCount, status.LastTerminationState.Terminated
		}
	}
	return 0, nil
}

// exitReason says how a broker's process ended.
func exitReason(t *corev1.ContainerStateTerminated) string {
	switch t.Reason {
	case "OOMKilled":
		return "ran out of memory"
	case "Completed":
		return "stopped"
	}
	return fmt.Sprintf("exited with code %d", t.ExitCode)
}
