package controller

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/ReneP92/changeport/api/v1alpha1"
	"github.com/ReneP92/changeport/internal/provisioning"
)

// sampleApp returns the customer's SampleApp while it runs, or nil. Once its
// stop time has come it deletes it, and the template stops rendering the
// sample app.
func (r *CustomerReconciler) sampleApp(ctx context.Context, spec provisioning.Spec) (*v1alpha1.SampleApp, error) {
	var sample v1alpha1.SampleApp
	err := r.Client.Get(ctx, client.ObjectKey{Namespace: spec.Namespace(), Name: v1alpha1.SampleAppName}, &sample)
	switch {
	case apierrors.IsNotFound(err):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("get the sample app of customer %s: %w", spec.CustomerID, err)
	case !sample.DeletionTimestamp.IsZero():
		return nil, nil
	case !r.now().Before(sample.Spec.StopAt.Time):
		// Deleting only the version read keeps a sample that the console
		// has just kept running.
		precondition := client.Preconditions{UID: &sample.UID, ResourceVersion: &sample.ResourceVersion}
		err := r.Client.Delete(ctx, &sample, precondition)
		switch {
		case apierrors.IsConflict(err):
			// It changed since it was read: it runs on, and the customer is
			// looked at again in a second, as its stop time has come.
			return &sample, nil
		case client.IgnoreNotFound(err) != nil:
			return nil, fmt.Errorf("stop the sample app of customer %s: %w", spec.CustomerID, err)
		}
		return nil, nil
	}
	return &sample, nil
}

// recordSampleApp records whether the sample app runs in its status, and
// returns how long until it stops. A sample app that changed meanwhile is
// recorded on the next look, which comes by its stop time at the latest.
func (r *CustomerReconciler) recordSampleApp(ctx context.Context, sample *v1alpha1.SampleApp, readiness provisioning.Readiness) (time.Duration, error) {
	status := v1alpha1.SampleAppStatus{Ready: readiness.Ready, Message: readiness.String()}
	if sample.Status != status {
		sample.Status = status
		err := r.Client.Status().Update(ctx, sample)
		if err != nil && !apierrors.IsNotFound(err) && !apierrors.IsConflict(err) {
			return 0, fmt.Errorf("record whether the sample app of namespace %s runs: %w", sample.Namespace, err)
		}
	}
	return sample.Spec.StopAt.Sub(r.now()), nil
}
