package console

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/ReneP92/changeport/api/v1alpha1"
)

func kubeCustomers(t *testing.T, customers ...client.Object) KubeCustomers {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return KubeCustomers{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(customers...).Build()}
}

func labelled(id, org string) *v1alpha1.Customer {
	return &v1alpha1.Customer{ObjectMeta: metav1.ObjectMeta{Name: id, Labels: map[string]string{v1alpha1.ClerkOrganizationLabel: org}}}
}

func TestKubeCustomersByOrganization(t *testing.T) {
	k := kubeCustomers(t, labelled("acme", "org_acme"), labelled("globex", "org_globex"))
	customer, err := k.ByOrganization(t.Context(), "org_acme")
	if err != nil || customer == nil || customer.Name != "acme" {
		t.Errorf("ByOrganization(org_acme) = %v, %v, want acme", customer, err)
	}
	for _, org := range []string{"org_unknown", "", "not a label value!"} {
		if customer, err := k.ByOrganization(t.Context(), org); err != nil || customer != nil {
			t.Errorf("ByOrganization(%q) = %v, %v, want none", org, customer, err)
		}
	}
}

func TestKubeCustomersRefuseSharedOrganizations(t *testing.T) {
	k := kubeCustomers(t, labelled("acme", "org_shared"), labelled("globex", "org_shared"))
	if customer, err := k.ByOrganization(t.Context(), "org_shared"); err == nil {
		t.Errorf("ByOrganization() of an organization with two customers = %v, want an error", customer)
	}
}

func TestKubeCustomersGet(t *testing.T) {
	k := kubeCustomers(t, labelled("acme", "org_acme"))
	if customer, err := k.Get(t.Context(), "acme"); err != nil || customer == nil {
		t.Errorf("Get(acme) = %v, %v", customer, err)
	}
	for _, id := range []string{"initech", "Not_An_ID", "../acme"} {
		if customer, err := k.Get(t.Context(), id); err != nil || customer != nil {
			t.Errorf("Get(%q) = %v, %v, want none", id, customer, err)
		}
	}
	list, err := k.List(t.Context())
	if err != nil || len(list) != 1 {
		t.Errorf("List() = %v, %v", list, err)
	}
}

// TestKubeCustomersFromTheWatch watches Customers as the console's service
// account, as the console does: it finds customers by organization through
// the watch, and reads a single customer fresh from the API server.
func TestKubeCustomersFromTheWatch(t *testing.T) {
	admin, consoleClient, consoleConfig := startAPIServerAsTheConsole(t)
	ctx := t.Context()
	// Counts the requests the console sends the API server.
	var requests atomic.Int64
	counted := rest.CopyConfig(consoleConfig)
	counted.Wrap(func(next http.RoundTripper) http.RoundTripper {
		return roundTripper(func(r *http.Request) (*http.Response, error) {
			requests.Add(1)
			return next.RoundTrip(r)
		})
	})
	watched, err := WatchCustomers(ctx, counted, consoleClient.Scheme())
	if err != nil {
		t.Fatal(err)
	}
	direct, err := client.New(counted, client.Options{Scheme: consoleClient.Scheme()})
	if err != nil {
		t.Fatal(err)
	}
	customers := KubeCustomers{Client: direct, Cache: watched}
	acme := labelled("acme", "org_acme")
	acme.Spec = v1alpha1.CustomerSpec{DisplayName: "Acme", Profile: "dev"}
	if err := admin.Create(ctx, acme); err != nil {
		t.Fatal(err)
	}
	if got, err := customers.Get(ctx, "acme"); err != nil || got == nil {
		t.Errorf("Get() of a customer created a moment ago = %v, %v", got, err)
	}
	// Deleting it shows at once, as the operator who deprovisioned it waits
	// to see.
	acme.Finalizers = []string{v1alpha1.Finalizer}
	if err := admin.Update(ctx, acme); err != nil {
		t.Fatal(err)
	}
	if err := customers.Delete(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	if got, err := customers.Get(ctx, "acme"); err != nil || got == nil || got.DeletionTimestamp == nil {
		t.Errorf("Get() right after Delete() = %v, %v; want it being deleted", got, err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		got, err := customers.ByOrganization(ctx, "org_acme")
		if err != nil {
			t.Fatal(err)
		}
		if got != nil {
			if got.Name != "acme" || len(got.ManagedFields) != 0 {
				t.Errorf("ByOrganization() = %s with %d managed fields, want acme without", got.Name, len(got.ManagedFields))
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the watch never delivered acme")
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Every request's lookup is answered from memory: a burst of them sends
	// the API server nothing, so nothing waits on its rate limit.
	before := requests.Load()
	for range 200 {
		if _, err := customers.ByOrganization(ctx, "org_acme"); err != nil {
			t.Fatal(err)
		}
		if _, err := customers.List(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if sent := requests.Load() - before; sent != 0 {
		t.Errorf("400 lookups sent the API server %d requests, want none", sent)
	}
	if err := watched.List(ctx, &v1alpha1.PostgresSourceList{}); err == nil {
		t.Error("the customer cache read PostgresSources, want it to hold only Customers")
	}
}

// TestWatchCustomersFailsWithoutAccess fails rather than hang when the API
// server refuses the watch, as for a role without the watch verb.
func TestWatchCustomersFailsWithoutAccess(t *testing.T) {
	_, consoleClient, consoleConfig := startAPIServerAsTheConsole(t)
	stranger := rest.CopyConfig(consoleConfig)
	stranger.Impersonate = rest.ImpersonationConfig{UserName: "system:serviceaccount:default:stranger"}
	defer func(timeout time.Duration) { syncTimeout = timeout }(syncTimeout)
	syncTimeout = 3 * time.Second
	if _, err := WatchCustomers(t.Context(), stranger, consoleClient.Scheme()); err == nil || !strings.Contains(err.Error(), "did not sync") {
		t.Errorf("WatchCustomers() without access = %v, want it to fail", err)
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
