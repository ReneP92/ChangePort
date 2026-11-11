package console

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/ReneP92/changeport/api/v1alpha1"
	"github.com/ReneP92/changeport/internal/clusters"
	"github.com/ReneP92/changeport/internal/console/api"
	"github.com/ReneP92/changeport/internal/provisioning"
)

// SampleApps keeps the customers' sample apps, which the guided first ten
// minutes start (v1alpha1.SampleApp).
type SampleApps interface {
	// Get returns the customer's sample app, or nil while it runs none.
	Get(ctx context.Context, customerID string) (*v1alpha1.SampleApp, error)
	// Run starts the customer's sample app, or keeps it running, until
	// stopAt.
	Run(ctx context.Context, customerID string, stopAt time.Time) (*v1alpha1.SampleApp, error)
	// Stop stops the customer's sample app; stopping one that does not run
	// is not an error.
	Stop(ctx context.Context, customerID string) error
}

// KubeSampleApps keeps sample apps as SampleApp resources, through the
// Kubernetes API as the console's service account.
type KubeSampleApps struct {
	Client client.Client
}

var _ SampleApps = KubeSampleApps{}

func sampleAppKey(customerID string) client.ObjectKey {
	return client.ObjectKey{Namespace: namespaceOf(customerID), Name: v1alpha1.SampleAppName}
}

// Get implements SampleApps. A sample app being deleted no longer runs.
func (k KubeSampleApps) Get(ctx context.Context, customerID string) (*v1alpha1.SampleApp, error) {
	var sample v1alpha1.SampleApp
	err := k.Client.Get(ctx, sampleAppKey(customerID), &sample)
	switch {
	case apierrors.IsNotFound(err):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("get the sample app of customer %s: %w", customerID, err)
	case !sample.DeletionTimestamp.IsZero():
		return nil, nil
	}
	return &sample, nil
}

// Run implements SampleApps.
func (k KubeSampleApps) Run(ctx context.Context, customerID string, stopAt time.Time) (*v1alpha1.SampleApp, error) {
	var sample v1alpha1.SampleApp
	// The provisioner records the sample app's status meanwhile.
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := k.Client.Get(ctx, sampleAppKey(customerID), &sample)
		if apierrors.IsNotFound(err) {
			key := sampleAppKey(customerID)
			sample = v1alpha1.SampleApp{
				ObjectMeta: metav1.ObjectMeta{Namespace: key.Namespace, Name: key.Name},
				Spec:       v1alpha1.SampleAppSpec{StopAt: metav1.NewTime(stopAt)},
			}
			return k.Client.Create(ctx, &sample)
		}
		if err != nil {
			return err
		}
		sample.Spec.StopAt = metav1.NewTime(stopAt)
		return k.Client.Update(ctx, &sample)
	})
	if err != nil {
		return nil, fmt.Errorf("run the sample app of customer %s: %w", customerID, err)
	}
	return &sample, nil
}

// Stop implements SampleApps.
func (k KubeSampleApps) Stop(ctx context.Context, customerID string) error {
	key := sampleAppKey(customerID)
	sample := &v1alpha1.SampleApp{ObjectMeta: metav1.ObjectMeta{Namespace: key.Namespace, Name: key.Name}}
	if err := k.Client.Delete(ctx, sample); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("stop the sample app of customer %s: %w", customerID, err)
	}
	return nil
}

var sampleAdminsOnly = problem(http.StatusForbidden, "Admins only", "Ask an admin of your organization to start or stop the sample app.")

// sampleTopicTaken refuses to start the sample app over a topic of its name:
// stopping the sample deletes its topic, and would delete the customer's.
var sampleTopicTaken = problem(http.StatusConflict, "Topic "+provisioning.SampleTopic+" exists",
	"The sample app writes to a topic of that name, and deletes it when it stops. If you have just stopped the sample, its topic goes within a minute; if the topic is yours, the sample cannot run next to it.")

// sampleCaller returns the caller's customer for the sample app's routes,
// or the problem why there is none; admin asks for an organization admin.
func (s *Server) sampleCaller(ctx context.Context, admin bool) (*customerContext, *api.Problem, error) {
	customer, principal, refused, err := s.customerOf(ctx)
	if err != nil || refused != nil {
		return nil, refused, err
	}
	if admin && !principal.Admin() {
		return nil, &sampleAdminsOnly, nil
	}
	return &customerContext{customer: customer, principal: principal}, nil, nil
}

func toAPISampleApp(sample *v1alpha1.SampleApp) api.SampleApp {
	result := api.SampleApp{State: api.SampleAppStateStopped, Topic: provisioning.SampleTopic, Group: provisioning.SampleGroup}
	if sample != nil {
		result.State = api.SampleAppStateStarting
		if sample.Status.Ready {
			result.State = api.SampleAppStateRunning
		}
		result.StopsAt = &sample.Spec.StopAt.Time
	}
	return result
}

// GetSampleApp implements api.StrictServerInterface.
func (s *Server) GetSampleApp(ctx context.Context, _ api.GetSampleAppRequestObject) (api.GetSampleAppResponseObject, error) {
	caller, refused, err := s.sampleCaller(ctx, false)
	switch {
	case err != nil:
		return nil, err
	case refused != nil && forbidden(refused):
		return api.GetSampleApp403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(*refused)}, nil
	case refused != nil:
		return api.GetSampleApp404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(*refused)}, nil
	}
	sample, err := s.sampleApps.Get(ctx, caller.id())
	if err != nil {
		return nil, err
	}
	return api.GetSampleApp200JSONResponse(toAPISampleApp(sample)), nil
}

// StartSampleApp implements api.StrictServerInterface: the sample app runs
// for an hour from now, whether it ran already or not.
func (s *Server) StartSampleApp(ctx context.Context, _ api.StartSampleAppRequestObject) (api.StartSampleAppResponseObject, error) {
	caller, refused, err := s.sampleCaller(ctx, true)
	switch {
	case err != nil:
		return nil, err
	case refused != nil && forbidden(refused):
		return api.StartSampleApp403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(*refused)}, nil
	case refused != nil:
		return api.StartSampleApp404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(*refused)}, nil
	}
	running, err := s.sampleApps.Get(ctx, caller.id())
	if err != nil {
		return nil, err
	}
	if running == nil {
		existing, err := s.clusters.Topics(ctx, caller.id())
		if errors.Is(err, clusters.ErrNotReady) {
			return api.StartSampleApp409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(notReadyYet)}, nil
		}
		if err != nil {
			return nil, err
		}
		if slices.ContainsFunc(existing, func(t TopicSettings) bool { return t.Name == provisioning.SampleTopic }) {
			return api.StartSampleApp409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(sampleTopicTaken)}, nil
		}
	}
	stopAt := time.Now().Add(v1alpha1.SampleAppRunTime).Truncate(time.Second)
	sample, err := s.sampleApps.Run(ctx, caller.id(), stopAt)
	if err != nil {
		return nil, err
	}
	action := "sample app started"
	if running != nil {
		action = "sample app kept running"
	}
	s.audit(ctx, caller.principal, caller.id(), action, "stopAt", stopAt)
	return api.StartSampleApp200JSONResponse(toAPISampleApp(sample)), nil
}

// StopSampleApp implements api.StrictServerInterface.
func (s *Server) StopSampleApp(ctx context.Context, _ api.StopSampleAppRequestObject) (api.StopSampleAppResponseObject, error) {
	caller, refused, err := s.sampleCaller(ctx, true)
	switch {
	case err != nil:
		return nil, err
	case refused != nil && forbidden(refused):
		return api.StopSampleApp403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(*refused)}, nil
	case refused != nil:
		return api.StopSampleApp404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(*refused)}, nil
	}
	if err := s.sampleApps.Stop(ctx, caller.id()); err != nil {
		return nil, err
	}
	s.audit(ctx, caller.principal, caller.id(), "sample app stopped")
	return api.StopSampleApp204Response{}, nil
}
