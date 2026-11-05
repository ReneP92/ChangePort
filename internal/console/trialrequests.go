package console

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/ReneP92/changeport/api/v1alpha1"
)

// TrialRequests reads, creates and decides the TrialRequest resources.
type TrialRequests interface {
	// List returns every trial request, newest first.
	List(ctx context.Context) ([]v1alpha1.TrialRequest, error)
	// ByUser returns the trial requests of a Clerk user, newest first.
	ByUser(ctx context.Context, userID string) ([]v1alpha1.TrialRequest, error)
	// Get returns a trial request by ID, or nil.
	Get(ctx context.Context, id string) (*v1alpha1.TrialRequest, error)
	// Create creates a trial request, filling in what the API server sets,
	// or returns ErrTrialRequestExists.
	Create(ctx context.Context, request *v1alpha1.TrialRequest) error
	// Delete deletes a trial request.
	Delete(ctx context.Context, id string) error
	// Decide writes a request's status, or returns ErrTrialRequestChanged
	// when the request changed since it was read.
	Decide(ctx context.Context, request *v1alpha1.TrialRequest) error
}

// ErrTrialRequestExists means a trial request of that ID exists already.
var ErrTrialRequestExists = errors.New("the trial request exists")

// ErrTrialRequestChanged means a trial request changed since it was read,
// such as when another operator decided first.
var ErrTrialRequestChanged = errors.New("the trial request changed")

// KubeTrialRequests reads, creates and decides TrialRequests through the
// Kubernetes API.
type KubeTrialRequests struct {
	Client client.Client
}

var _ TrialRequests = KubeTrialRequests{}

// List implements TrialRequests.
func (k KubeTrialRequests) List(ctx context.Context) ([]v1alpha1.TrialRequest, error) {
	var list v1alpha1.TrialRequestList
	if err := k.Client.List(ctx, &list); err != nil {
		return nil, fmt.Errorf("list trial requests: %w", err)
	}
	return newestFirst(list.Items), nil
}

// ByUser implements TrialRequests, through the user label the console sets
// on every TrialRequest.
func (k KubeTrialRequests) ByUser(ctx context.Context, userID string) ([]v1alpha1.TrialRequest, error) {
	if len(validation.IsValidLabelValue(userID)) > 0 {
		return nil, nil
	}
	var list v1alpha1.TrialRequestList
	if err := k.Client.List(ctx, &list, client.MatchingLabels{v1alpha1.ClerkUserLabel: userID}); err != nil {
		return nil, fmt.Errorf("list the trial requests of user %s: %w", userID, err)
	}
	return newestFirst(list.Items), nil
}

// Get implements TrialRequests. No trial request has an invalid ID.
func (k KubeTrialRequests) Get(ctx context.Context, id string) (*v1alpha1.TrialRequest, error) {
	if !validTrialRequestID(id) {
		return nil, nil
	}
	var request v1alpha1.TrialRequest
	err := k.Client.Get(ctx, types.NamespacedName{Name: id}, &request)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get trial request %s: %w", id, err)
	}
	return &request, nil
}

// Create implements TrialRequests.
func (k KubeTrialRequests) Create(ctx context.Context, request *v1alpha1.TrialRequest) error {
	err := k.Client.Create(ctx, request)
	if apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("trial request %s: %w", request.Name, ErrTrialRequestExists)
	}
	if err != nil {
		return fmt.Errorf("create trial request %s: %w", request.Name, err)
	}
	return nil
}

// Delete implements TrialRequests. A request that is gone already is not an
// error.
func (k KubeTrialRequests) Delete(ctx context.Context, id string) error {
	err := k.Client.Delete(ctx, &v1alpha1.TrialRequest{ObjectMeta: metav1.ObjectMeta{Name: id}})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete trial request %s: %w", id, err)
	}
	return nil
}

// Decide implements TrialRequests. The update carries the resource version
// the request was read at, so a request someone else decided meanwhile is
// refused.
func (k KubeTrialRequests) Decide(ctx context.Context, request *v1alpha1.TrialRequest) error {
	err := k.Client.Status().Update(ctx, request)
	if apierrors.IsConflict(err) || apierrors.IsNotFound(err) {
		return fmt.Errorf("trial request %s: %w", request.Name, ErrTrialRequestChanged)
	}
	if err != nil {
		return fmt.Errorf("decide trial request %s: %w", request.Name, err)
	}
	return nil
}

// newestFirst sorts trial requests by when they were made, newest first,
// and by ID among those made in the same second, so every reader sees the
// same order.
func newestFirst(requests []v1alpha1.TrialRequest) []v1alpha1.TrialRequest {
	slices.SortFunc(requests, func(a, b v1alpha1.TrialRequest) int {
		if c := b.CreationTimestamp.Compare(a.CreationTimestamp.Time); c != 0 {
			return c
		}
		return cmp.Compare(a.Name, b.Name)
	})
	return requests
}
