package console

import (
	"context"
	"errors"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/ReneP92/changeport/api/v1alpha1"
	"github.com/ReneP92/changeport/internal/provisioning"
)

// Customers reads, creates and deletes the Customer resources.
type Customers interface {
	// ByOrganization returns the customer of a Clerk organization, or nil.
	ByOrganization(ctx context.Context, organizationID string) (*v1alpha1.Customer, error)
	// List returns every customer.
	List(ctx context.Context) ([]v1alpha1.Customer, error)
	// Get returns a customer by ID, or nil.
	Get(ctx context.Context, id string) (*v1alpha1.Customer, error)
	// Create creates a customer, filling in what the API server sets, or
	// returns ErrCustomerExists.
	Create(ctx context.Context, customer *v1alpha1.Customer) error
	// Delete deletes a customer, whose cluster the provisioning controller
	// then deletes, or returns ErrNoSuchCustomer.
	Delete(ctx context.Context, id string) error
}

// ErrCustomerExists means a customer of that ID exists already.
var ErrCustomerExists = errors.New("the customer exists")

// ErrNoSuchCustomer means there is no customer of that ID.
var ErrNoSuchCustomer = errors.New("no such customer")

// KubeCustomers reads, creates and deletes Customers through the Kubernetes
// API. Nearly every console request looks its customer up by organization,
// so those lookups and the list of every customer come from Cache, a watch
// of every Customer kept in memory, and never wait on the API server. A
// single customer, which operators view and act on right after changing
// it, is read fresh through Client, as are creations and deletions.
type KubeCustomers struct {
	Client client.Client
	// Cache reads Customers; without one, reads go through Client.
	Cache client.Reader
}

func (k KubeCustomers) reader() client.Reader {
	if k.Cache != nil {
		return k.Cache
	}
	return k.Client
}

var _ Customers = KubeCustomers{}

// ByOrganization implements Customers, through the organization label the
// provisioning controller keeps on every Customer.
func (k KubeCustomers) ByOrganization(ctx context.Context, organizationID string) (*v1alpha1.Customer, error) {
	if len(validation.IsValidLabelValue(organizationID)) > 0 {
		return nil, nil
	}
	var list v1alpha1.CustomerList
	if err := k.reader().List(ctx, &list, client.MatchingLabels{v1alpha1.ClerkOrganizationLabel: organizationID}); err != nil {
		return nil, fmt.Errorf("list the customers of organization %s: %w", organizationID, err)
	}
	switch len(list.Items) {
	case 0:
		return nil, nil
	case 1:
		return &list.Items[0], nil
	default:
		// One organization is one customer; anything else needs an operator.
		return nil, fmt.Errorf("organization %s belongs to %d customers", organizationID, len(list.Items))
	}
}

// List implements Customers.
func (k KubeCustomers) List(ctx context.Context) ([]v1alpha1.Customer, error) {
	var list v1alpha1.CustomerList
	if err := k.reader().List(ctx, &list); err != nil {
		return nil, fmt.Errorf("list customers: %w", err)
	}
	return list.Items, nil
}

// Get implements Customers. No customer has an invalid ID.
func (k KubeCustomers) Get(ctx context.Context, id string) (*v1alpha1.Customer, error) {
	if provisioning.ValidateCustomerID(id) != nil {
		return nil, nil
	}
	var customer v1alpha1.Customer
	err := k.Client.Get(ctx, types.NamespacedName{Name: id}, &customer)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get customer %s: %w", id, err)
	}
	return &customer, nil
}

// Create implements Customers.
func (k KubeCustomers) Create(ctx context.Context, customer *v1alpha1.Customer) error {
	err := k.Client.Create(ctx, customer)
	if apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("customer %s: %w", customer.Name, ErrCustomerExists)
	}
	if err != nil {
		return fmt.Errorf("create customer %s: %w", customer.Name, err)
	}
	return nil
}

// Delete implements Customers. It returns once the deletion has begun.
func (k KubeCustomers) Delete(ctx context.Context, id string) error {
	if provisioning.ValidateCustomerID(id) != nil {
		return fmt.Errorf("customer %s: %w", id, ErrNoSuchCustomer)
	}
	err := k.Client.Delete(ctx, &v1alpha1.Customer{ObjectMeta: metav1.ObjectMeta{Name: id}})
	if apierrors.IsNotFound(err) {
		return fmt.Errorf("customer %s: %w", id, ErrNoSuchCustomer)
	}
	if err != nil {
		return fmt.Errorf("delete customer %s: %w", id, err)
	}
	return nil
}

// syncTimeout bounds how long WatchCustomers waits for the first list.
var syncTimeout = time.Minute

// WatchCustomers watches every Customer and keeps them in memory, as
// KubeCustomers' Cache, until ctx is done. It returns once it holds them
// all.
func WatchCustomers(ctx context.Context, config *rest.Config, scheme *runtime.Scheme) (cache.Cache, error) {
	customers, err := cache.New(config, cache.Options{
		Scheme: scheme,
		// Only Customers: reading anything else from it is a bug, rather
		// than a new watch the console's role does not allow.
		ReaderFailOnMissingInformer: true,
		DefaultTransform:            cache.TransformStripManagedFields(),
	})
	if err != nil {
		return nil, fmt.Errorf("create the customer cache: %w", err)
	}
	if _, err := customers.GetInformer(ctx, &v1alpha1.Customer{}); err != nil {
		return nil, fmt.Errorf("watch customers: %w", err)
	}
	go func() { _ = customers.Start(ctx) }()
	// A watch the API server refuses, such as without the role's watch
	// verb, never syncs; the console then fails rather than hang.
	syncing, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()
	if !customers.WaitForCacheSync(syncing) {
		return nil, fmt.Errorf("the customer cache did not sync within %s; may the console watch customers?", syncTimeout)
	}
	return customers, nil
}
