package console

import (
	"errors"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/ReneP92/changeport/api/v1alpha1"
)

// TestKubeTrialRequestsAsTheConsole makes, finds and decides trial requests
// through the API server as the console's service account, whose role may
// not change a request once made.
func TestKubeTrialRequestsAsTheConsole(t *testing.T) {
	_, console := startAPIServer(t)
	ctx := t.Context()
	requests := KubeTrialRequests{Client: console}
	request := func(id, user string) *v1alpha1.TrialRequest {
		return &v1alpha1.TrialRequest{
			ObjectMeta: metav1.ObjectMeta{Name: id, Labels: map[string]string{v1alpha1.ClerkUserLabel: user}},
			Spec: v1alpha1.TrialRequestSpec{
				ClerkUserID: user, Email: "ada@example.com", Company: "Hooli", Role: "CTO", UseCase: "Orders",
				Throughput: v1alpha1.ThroughputUnder1MBps, Retention: v1alpha1.RetentionUpTo7Days, PreferredStart: "2026-12-01",
			},
		}
	}
	mine := request("tr-mine22", "user_2ada")
	for _, r := range []*v1alpha1.TrialRequest{mine, request("tr-babs22", "user_2bob")} {
		if err := requests.Create(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	if err := requests.Create(ctx, request("tr-mine22", "user_2ada")); !errors.Is(err, ErrTrialRequestExists) {
		t.Errorf("creating tr-mine22 again = %v, want ErrTrialRequestExists", err)
	}
	if all, err := requests.List(ctx); err != nil || len(all) != 2 {
		t.Errorf("List() = %d requests, %v; want 2", len(all), err)
	}
	if found, err := requests.ByUser(ctx, "user_2ada"); err != nil || len(found) != 1 || found[0].Name != "tr-mine22" {
		t.Errorf("ByUser() = %v, %v; want tr-mine22", found, err)
	}
	if missing, err := requests.Get(ctx, "Not-An-ID"); missing != nil || err != nil {
		t.Errorf("Get() of an invalid ID = %v, %v", missing, err)
	}

	// Another operator reads the request as well, and decides first.
	got, err := requests.Get(ctx, "tr-mine22")
	if err != nil || got == nil {
		t.Fatalf("Get() = %v, %v", got, err)
	}
	stale := got.DeepCopy()
	got.Status = v1alpha1.TrialRequestStatus{
		Phase: v1alpha1.TrialRequestDeclined, DecidedBy: "user_2op", DecidedAt: &metav1.Time{Time: time.Now()}, Reason: "Not yet",
	}
	if err := requests.Decide(ctx, got); err != nil {
		t.Fatal(err)
	}
	stale.Status = v1alpha1.TrialRequestStatus{
		Phase: v1alpha1.TrialRequestApproved, DecidedBy: "user_2op", DecidedAt: &metav1.Time{Time: time.Now()}, Customer: "c-abcdef", OrganizationID: "org_2abc",
	}
	if err := requests.Decide(ctx, stale); !errors.Is(err, ErrTrialRequestChanged) {
		t.Errorf("deciding a request decided meanwhile = %v, want ErrTrialRequestChanged", err)
	}

	got.Spec.Company = "Initech"
	if err := console.Update(ctx, got); !apierrors.IsForbidden(err) {
		t.Errorf("the console changing a request = %v, want it forbidden", err)
	}
	if err := requests.Delete(ctx, "tr-babs22"); err != nil {
		t.Fatal(err)
	}
	if err := requests.Delete(ctx, "tr-babs22"); err != nil {
		t.Errorf("deleting a deleted request = %v, want nil", err)
	}
}
