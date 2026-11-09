package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/ReneP92/changeport/api/v1alpha1"
	"github.com/ReneP92/changeport/internal/mail"
)

// trialStart is when the trials of these tests start, two weeks before they
// end.
var trialStart = time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC)

// onTrial returns acme on a trial that ends at end, with the finalizer the
// controller adds.
func onTrial(end time.Time) *v1alpha1.Customer {
	customer := newCustomer()
	customer.Finalizers = []string{v1alpha1.Finalizer}
	customer.Spec.Trial = &v1alpha1.CustomerTrial{EndsAt: metav1.NewTime(end)}
	return customer
}

func TestReconcileWarnsThreeDaysBeforeATrialEnds(t *testing.T) {
	end := trialStart.Add(14 * 24 * time.Hour)
	r, c := newReconciler(t, &fakeProvisioner{progress: allReady}, onTrial(end))
	at := func(now time.Time) (time.Duration, *v1alpha1.Customer) {
		t.Helper()
		r.Now = func() time.Time { return now }
		result, err := reconcile(t, r, "acme")
		if err != nil {
			t.Fatal(err)
		}
		return result.RequeueAfter, get(t, c, "acme")
	}

	// Two weeks out, the trial runs and the resync comes first.
	requeue, customer := at(trialStart)
	ending := condition(t, customer, v1alpha1.ConditionTrialEnding)
	if ending.Status != metav1.ConditionFalse || ending.Reason != v1alpha1.ReasonTrialRunning ||
		ending.Message != "The trial ends on 2026-11-22 09:00 UTC" || customer.Status.TrialWarnedFor != nil {
		t.Errorf("two weeks out, TrialEnding = %+v, warned for %v; want running, not warned", ending, customer.Status.TrialWarnedFor)
	}
	if requeue != resyncInterval {
		t.Errorf("two weeks out, requeue after %s, want the resync %s", requeue, resyncInterval)
	}

	// Just before the warning, the controller comes back for it on time.
	warning := end.Add(-v1alpha1.TrialWarning)
	if requeue, _ := at(warning.Add(-30 * time.Second)); requeue != 30*time.Second {
		t.Errorf("30 seconds before the warning, requeue after %s, want 30s", requeue)
	}

	// At the warning, the customer is warned once, for this end.
	_, customer = at(warning)
	ending = condition(t, customer, v1alpha1.ConditionTrialEnding)
	if ending.Status != metav1.ConditionTrue || ending.Reason != v1alpha1.ReasonTrialEnding ||
		customer.Status.TrialWarnedFor == nil || !customer.Status.TrialWarnedFor.Time.Equal(end) {
		t.Errorf("at the warning, TrialEnding = %+v, warned for %v; want ending, warned for %s", ending, customer.Status.TrialWarnedFor, end)
	}
	warnedAt := ending.LastTransitionTime
	_, customer = at(warning.Add(time.Hour))
	if condition(t, customer, v1alpha1.ConditionTrialEnding).LastTransitionTime != warnedAt {
		t.Error("the warning changed an hour later, want it given once")
	}

	// Just before the end, the controller comes back for it on time.
	if requeue, _ := at(end.Add(-5 * time.Second)); requeue != 5*time.Second {
		t.Errorf("5 seconds before the end, requeue after %s, want 5s", requeue)
	}
	if got := testutil.ToFloat64(customerTrialEnds.WithLabelValues("acme")); got != float64(end.Unix()) {
		t.Errorf("changeport_customer_trial_ends_timestamp_seconds = %v, want %d", got, end.Unix())
	}
}

func TestReconcileDeletesACustomerWhoseTrialEnded(t *testing.T) {
	end := trialStart.Add(14 * 24 * time.Hour)
	provisioner := &fakeProvisioner{progress: allReady}
	r, c := newReconciler(t, provisioner, onTrial(end))
	r.Now = func() time.Time { return end }
	if _, err := reconcile(t, r, "acme"); err != nil {
		t.Fatal(err)
	}
	customer := get(t, c, "acme")
	if customer.DeletionTimestamp.IsZero() {
		t.Fatal("the Customer of an ended trial is not being deleted")
	}
	ending := condition(t, customer, v1alpha1.ConditionTrialEnding)
	if ending.Reason != v1alpha1.ReasonTrialEnded || ending.Message != "The trial ended on 2026-11-22 09:00 UTC; the customer is deprovisioned" {
		t.Errorf("TrialEnding = %+v, want it ended", ending)
	}
	if len(provisioner.calls) != 0 {
		t.Errorf("the provisioner was asked to %v, want nothing before the deletion", provisioner.calls)
	}

	// The deletion then goes the usual way.
	provisioner.deleted = true
	if _, err := reconcile(t, r, "acme"); err != nil {
		t.Fatal(err)
	}
	if len(provisioner.calls) != 1 || provisioner.calls[0] != "deprovision acme" {
		t.Errorf("after the deletion, the provisioner was asked to %v, want to deprovision acme", provisioner.calls)
	}
}

func TestReconcileWarnsAnExtendedTrialAgain(t *testing.T) {
	end := trialStart.Add(14 * 24 * time.Hour)
	r, c := newReconciler(t, &fakeProvisioner{progress: allReady}, onTrial(end))
	r.Now = func() time.Time { return end.Add(-time.Hour) }
	if _, err := reconcile(t, r, "acme"); err != nil {
		t.Fatal(err)
	}

	// Extended by a week an hour before the end, the trial runs again.
	extended := end.Add(7 * 24 * time.Hour)
	customer := get(t, c, "acme")
	customer.Spec.Trial.EndsAt = metav1.NewTime(extended)
	if err := c.Update(t.Context(), customer); err != nil {
		t.Fatal(err)
	}
	if _, err := reconcile(t, r, "acme"); err != nil {
		t.Fatal(err)
	}
	customer = get(t, c, "acme")
	if ending := condition(t, customer, v1alpha1.ConditionTrialEnding); ending.Status != metav1.ConditionFalse || !customer.DeletionTimestamp.IsZero() {
		t.Errorf("after the extension, TrialEnding = %+v, deleting %v; want running", ending, !customer.DeletionTimestamp.IsZero())
	}

	// Three days before the new end, the customer is warned again.
	r.Now = func() time.Time { return extended.Add(-v1alpha1.TrialWarning) }
	if _, err := reconcile(t, r, "acme"); err != nil {
		t.Fatal(err)
	}
	if warned := get(t, c, "acme").Status.TrialWarnedFor; warned == nil || !warned.Time.Equal(extended) {
		t.Errorf("warned for %v, want the new end %s", warned, extended)
	}
}

func TestReconcileForgetsTheTrialOfAConvertedCustomer(t *testing.T) {
	end := trialStart.Add(14 * 24 * time.Hour)
	r, c := newReconciler(t, &fakeProvisioner{progress: allReady}, onTrial(end))
	r.Now = func() time.Time { return end.Add(-time.Hour) }
	if _, err := reconcile(t, r, "acme"); err != nil {
		t.Fatal(err)
	}
	customer := get(t, c, "acme")
	customer.Spec.Trial = nil
	if err := c.Update(t.Context(), customer); err != nil {
		t.Fatal(err)
	}
	// Past the old end, a converted customer stays.
	r.Now = func() time.Time { return end.Add(time.Hour) }
	result, err := reconcile(t, r, "acme")
	if err != nil {
		t.Fatal(err)
	}
	customer = get(t, c, "acme")
	if !customer.DeletionTimestamp.IsZero() || meta.FindStatusCondition(customer.Status.Conditions, v1alpha1.ConditionTrialEnding) != nil ||
		customer.Status.TrialWarnedFor != nil {
		t.Errorf("a converted customer: deleting %v, conditions %v, warned for %v; want none of the trial", !customer.DeletionTimestamp.IsZero(),
			customer.Status.Conditions, customer.Status.TrialWarnedFor)
	}
	if result.RequeueAfter != resyncInterval {
		t.Errorf("requeue after %s, want the resync", result.RequeueAfter)
	}
	if customerTrialEnds.DeleteLabelValues("acme") {
		t.Error("a converted customer still has changeport_customer_trial_ends_timestamp_seconds")
	}
}

// trialMail records the email the controller sends, and fails while fail
// is set.
type trialMail struct {
	sent []string
	fail bool
}

func (m *trialMail) Send(_ context.Context, template mail.Template, to []string, data any) error {
	if m.fail {
		return errors.New("the mail server is down")
	}
	trial := data.(mail.Trial)
	m.sent = append(m.sent, fmt.Sprintf("%s to %s: %s ends %s, %s", template, strings.Join(to, ","), trial.Company, trial.EndsAt, trial.Link))
	return nil
}

func TestReconcileEmailsATrialsContactOnce(t *testing.T) {
	end := trialStart.Add(14 * 24 * time.Hour)
	customer := onTrial(end)
	customer.Spec.Trial.Contact = "ada@example.com"
	r, c := newReconciler(t, &fakeProvisioner{progress: allReady}, customer)
	sent := &trialMail{}
	r.Mail, r.ConsoleURL = sent, "https://console.example.com"
	at := func(now time.Time) error {
		t.Helper()
		r.Now = func() time.Time { return now }
		_, err := reconcile(t, r, "acme")
		return err
	}
	warning := end.Add(-v1alpha1.TrialWarning)
	if err := at(warning.Add(-time.Minute)); err != nil || len(sent.sent) != 0 {
		t.Fatalf("before the warning: %v, sent %v; want nothing", err, sent.sent)
	}

	// A send that fails is not recorded, and is tried again.
	sent.fail = true
	if err := at(warning); err == nil {
		t.Error("reconciling while the warning cannot be sent succeeded, want it retried")
	}
	if warned := get(t, c, "acme").Status.TrialWarnedFor; warned != nil {
		t.Errorf("an unsent warning was recorded for %s", warned)
	}
	sent.fail = false
	for _, now := range []time.Time{warning, warning.Add(time.Hour), warning.Add(24 * time.Hour)} {
		if err := at(now); err != nil {
			t.Fatal(err)
		}
	}
	want := "trial-ending to ada@example.com: Acme Corp ends 22 November 2026, 09:00 UTC, https://console.example.com/"
	if len(sent.sent) != 1 || sent.sent[0] != want {
		t.Errorf("sent %v, want the warning once:\n%s", sent.sent, want)
	}

	// At the end, the contact hears once, before the Customer goes.
	if err := at(end); err != nil {
		t.Fatal(err)
	}
	if len(sent.sent) != 2 || !strings.HasPrefix(sent.sent[1], "trial-ended to ada@example.com: Acme Corp") {
		t.Errorf("sent %v, want the end after the warning", sent.sent)
	}
}

func TestReconcileEmailsNobodyWithoutAContact(t *testing.T) {
	end := trialStart.Add(14 * 24 * time.Hour)
	r, _ := newReconciler(t, &fakeProvisioner{progress: allReady}, onTrial(end))
	sent := &trialMail{}
	r.Mail = sent
	for _, now := range []time.Time{end.Add(-v1alpha1.TrialWarning), end} {
		r.Now = func() time.Time { return now }
		if _, err := reconcile(t, r, "acme"); err != nil {
			t.Fatal(err)
		}
	}
	if len(sent.sent) != 0 {
		t.Errorf("sent %v for a trial without a contact", sent.sent)
	}
}

// TestReconcileDoesNotEmailAgainFromAStaleCache reconciles from a cache that
// has not caught up with the status recording a trial's email, as happens
// right after the reconciler's own update: the API server says it was sent,
// so it is not sent again.
func TestReconcileDoesNotEmailAgainFromAStaleCache(t *testing.T) {
	end := trialStart.Add(14 * 24 * time.Hour)
	stale := onTrial(end)
	stale.Spec.Trial.Contact = "ada@example.com"
	sent := &trialMail{}

	// The warning, sent and recorded, which the cache does not show yet.
	warned := stale.DeepCopy()
	warned.Status.TrialWarnedFor = warned.Spec.Trial.EndsAt.DeepCopy()
	r, _ := newReconciler(t, &fakeProvisioner{progress: allReady}, stale.DeepCopy())
	r.Reader = newReconcilerClient(t, warned)
	r.Mail, r.Now = sent, func() time.Time { return end.Add(-time.Hour) }
	if _, err := reconcile(t, r, "acme"); err != nil {
		t.Fatal(err)
	}
	if len(sent.sent) != 0 {
		t.Errorf("sent %v again from a stale cache, want nothing", sent.sent)
	}

	// The end, sent and recorded, which the cache does not show yet either.
	ended := warned.DeepCopy()
	ended.Status.Conditions = []metav1.Condition{{
		Type: v1alpha1.ConditionTrialEnding, Status: metav1.ConditionTrue, Reason: v1alpha1.ReasonTrialEnded,
		Message: "ended", LastTransitionTime: metav1.Now(),
	}}
	r, _ = newReconciler(t, &fakeProvisioner{progress: allReady}, warned.DeepCopy())
	r.Reader = newReconcilerClient(t, ended)
	r.Mail, r.Now = sent, func() time.Time { return end }
	if _, err := reconcile(t, r, "acme"); err != nil {
		t.Fatal(err)
	}
	if len(sent.sent) != 0 {
		t.Errorf("sent %v again from a stale cache, want nothing", sent.sent)
	}
}

// newReconcilerClient is a client holding objects, as the API server does.
func newReconcilerClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	_, c := newReconciler(t, &fakeProvisioner{}, objects...)
	return c
}
