package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/ReneP92/changeport/api/v1alpha1"
	"github.com/ReneP92/changeport/internal/provisioning"
)

func newSampleApp(stopAt time.Time) *v1alpha1.SampleApp {
	return &v1alpha1.SampleApp{
		ObjectMeta: metav1.ObjectMeta{Namespace: "customer-acme", Name: v1alpha1.SampleAppName},
		Spec:       v1alpha1.SampleAppSpec{StopAt: metav1.NewTime(stopAt)},
	}
}

func getSampleApp(t *testing.T, c client.Client) (*v1alpha1.SampleApp, error) {
	t.Helper()
	var sample v1alpha1.SampleApp
	err := c.Get(t.Context(), client.ObjectKey{Namespace: "customer-acme", Name: v1alpha1.SampleAppName}, &sample)
	return &sample, err
}

func TestReconcileRunsTheSampleAppUntilItsStopTime(t *testing.T) {
	now := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	starting := allReady
	starting.Sample = provisioning.Readiness{Object: "Deployment customer-acme/changeport-sample", Status: "0 of 1 replicas updated, 0 available"}
	provisioner := &fakeProvisioner{progress: starting}
	r, c := newReconciler(t, provisioner, newCustomer(), newSampleApp(now.Add(time.Hour)))
	r.Now = func() time.Time { return now }

	result, err := reconcile(t, r, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if !provisioner.last.Sample {
		t.Error("the template does not render the sample app while it runs")
	}
	if result.RequeueAfter != pollInterval {
		t.Errorf("requeue after %v, want %v while the sample app starts", result.RequeueAfter, pollInterval)
	}
	if sample, err := getSampleApp(t, c); err != nil || sample.Status.Ready {
		t.Fatalf("sample app = %+v, %v; want it not ready yet", sample.Status, err)
	}
	if ready := condition(t, get(t, c, "acme"), v1alpha1.ConditionReady); ready.Status != metav1.ConditionTrue {
		t.Errorf("Ready = %s %s, want True without the sample app", ready.Status, ready.Message)
	}

	// Once it runs, the customer is looked at again when it stops, before
	// the next resync.
	provisioner.progress.Sample = provisioning.Readiness{Ready: true, Status: "The sample app runs"}
	now = now.Add(56 * time.Minute)
	if result, err = reconcile(t, r, "acme"); err != nil {
		t.Fatal(err)
	}
	if want := 4 * time.Minute; result.RequeueAfter != want {
		t.Errorf("requeue after %v, want %v, when the sample app stops", result.RequeueAfter, want)
	}
	if sample, err := getSampleApp(t, c); err != nil || !sample.Status.Ready {
		t.Fatalf("sample app = %+v, %v; want it ready", sample.Status, err)
	}

	// At its stop time it goes, and the template stops rendering it.
	now = now.Add(4 * time.Minute)
	provisioner.progress = allReady
	if _, err = reconcile(t, r, "acme"); err != nil {
		t.Fatal(err)
	}
	if provisioner.last.Sample {
		t.Error("the template renders the sample app past its stop time")
	}
	if _, err := getSampleApp(t, c); !apierrors.IsNotFound(err) {
		t.Errorf("the sample app is still there past its stop time: %v", err)
	}
}

func TestReconcileRendersNoSampleAppWithoutOne(t *testing.T) {
	provisioner := &fakeProvisioner{progress: allReady}
	r, _ := newReconciler(t, provisioner, newCustomer(),
		// Another customer's sample app is not acme's.
		&v1alpha1.SampleApp{ObjectMeta: metav1.ObjectMeta{Namespace: "customer-globex", Name: v1alpha1.SampleAppName}, Spec: v1alpha1.SampleAppSpec{StopAt: metav1.NewTime(time.Now().Add(time.Hour))}})
	result, err := reconcile(t, r, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if provisioner.last.Sample {
		t.Error("the template renders a sample app acme does not run")
	}
	if result.RequeueAfter != resyncInterval {
		t.Errorf("requeue after %v, want %v", result.RequeueAfter, resyncInterval)
	}
}

// A sample app the console keeps running just as it stops runs on, and is
// looked at again in a second rather than with the next resync.
func TestReconcileKeepsASampleAppThatChangedAsItStops(t *testing.T) {
	now := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	provisioner := &fakeProvisioner{progress: allReady}
	provisioner.progress.Sample = provisioning.Readiness{Ready: true, Status: "The sample app runs"}
	r, c := newReconciler(t, provisioner, newCustomer(), newSampleApp(now))
	r.Now = func() time.Time { return now }
	r.Client = interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			if _, ok := obj.(*v1alpha1.SampleApp); ok {
				return apierrors.NewConflict(v1alpha1.GroupVersion.WithResource("sampleapps").GroupResource(), obj.GetName(), errors.New("changed"))
			}
			return c.Delete(ctx, obj, opts...)
		},
	})
	result, err := reconcile(t, r, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if !provisioner.last.Sample {
		t.Error("the template stopped rendering a sample app that changed as it stopped")
	}
	if result.RequeueAfter != time.Second {
		t.Errorf("requeue after %v, want a second", result.RequeueAfter)
	}
	if sample, err := getSampleApp(t, c); err != nil || sample.Status.Message != "The sample app runs" {
		t.Errorf("sample app = %+v, %v; want its status recorded", sample.Status, err)
	}
}
