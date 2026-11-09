// Package controller runs Changeport's Kubernetes controllers: today the one
// that turns every Customer into its dedicated Kafka cluster.
package controller

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/ReneP92/changeport/api/v1alpha1"
	"github.com/ReneP92/changeport/internal/cdc"
	"github.com/ReneP92/changeport/internal/mail"
	"github.com/ReneP92/changeport/internal/provisioning"
)

// Provisioner is the part of provisioning.Provisioner the controller uses.
type Provisioner interface {
	Reconcile(ctx context.Context, spec provisioning.Spec) (provisioning.Progress, error)
	Deprovision(ctx context.Context, spec provisioning.Spec) error
	Deleted(ctx context.Context, spec provisioning.Spec) (bool, string, error)
}

var _ Provisioner = (*provisioning.Provisioner)(nil)

// CustomerReconciler provisions every Customer's Kafka cluster from the one
// template, and deprovisions it before the Customer goes.
//
// It only ever requeues: it never blocks on Strimzi or cert-manager, so one
// slow cluster cannot hold up the others.
type CustomerReconciler struct {
	Client      client.Client
	Provisioner Provisioner
	// Platform is where this controller's clusters run, such as kind.
	Platform provisioning.Platform
	// PollInterval is how soon a cluster that is not ready yet, or not
	// deleted yet, is looked at again.
	PollInterval time.Duration
	// ResyncInterval is how often a ready cluster is converged to the
	// template again, which repairs changes made by hand.
	ResyncInterval time.Duration
	// Now is the time, for trials; tests replace it.
	Now func() time.Time
	// Mail sends a trial's contact the warning and the end of its trial
	// (internal/mail); without it, nobody is emailed. ConsoleURL is where
	// the emails' links lead.
	Mail       Mail
	ConsoleURL string
	// Reader reads a Customer fresh from the API server before a trial's
	// email, as Client's cache can lag behind the status that records it
	// was sent; without one, Client reads.
	Reader client.Reader
}

// Mail sends email (internal/mail).
type Mail interface {
	Send(ctx context.Context, template mail.Template, to []string, data any) error
}

func (r *CustomerReconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// SetupWithManager registers the reconciler with mgr. A change to a
// PostgresSource reconciles the Customer of its namespace, whose template
// renders what the sources run on.
func (r *CustomerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Customer{}).
		Watches(&v1alpha1.PostgresSource{}, handler.EnqueueRequestsFromMapFunc(customerOfSource), builder.WithPredicates(sourceChanged)).
		// A customer's alert settings set its alerts' thresholds.
		Watches(&v1alpha1.AlertSettings{}, handler.EnqueueRequestsFromMapFunc(customerOfSource)).
		Named("customer").
		WithOptions(controller.Options{MaxConcurrentReconciles: 4}).
		Complete(r)
}

// sourceChanged lets through the changes to a source that change what the
// template renders for it: its spec, its deletion, and its credentials
// reaching the Connect worker. The source controller's other status updates,
// such as the slot's every minute, do not reconcile the Customer.
var sourceChanged = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		before, ok1 := e.ObjectOld.(*v1alpha1.PostgresSource)
		after, ok2 := e.ObjectNew.(*v1alpha1.PostgresSource)
		if !ok1 || !ok2 {
			return true
		}
		return before.Generation != after.Generation ||
			!before.DeletionTimestamp.Equal(after.DeletionTimestamp) ||
			before.Status.PasswordSecret != after.Status.PasswordSecret
	},
}

// customerOfSource is the Customer whose namespace a source is in.
func customerOfSource(_ context.Context, source client.Object) []ctrl.Request {
	id, ok := strings.CutPrefix(source.GetNamespace(), provisioning.NamespacePrefix)
	if !ok {
		return nil
	}
	return []ctrl.Request{{NamespacedName: client.ObjectKey{Name: id}}}
}

// Reconcile implements reconcile.Reconciler.
func (r *CustomerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	result, err := r.reconcile(ctx, req)
	if apierrors.IsConflict(err) {
		// The Customer changed since it was read, and the change triggers
		// another reconciliation with the new version.
		return ctrl.Result{}, nil
	}
	return result, err
}

func (r *CustomerReconciler) reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var customer v1alpha1.Customer
	if err := r.Client.Get(ctx, req.NamespacedName, &customer); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !customer.DeletionTimestamp.IsZero() {
		return r.deprovision(ctx, &customer)
	}
	if trial := customer.Spec.Trial; trial != nil && !r.now().Before(trial.EndsAt.Time) {
		return ctrl.Result{}, r.endTrial(ctx, &customer)
	}
	spec, err := r.spec(&customer)
	if err != nil {
		// Only a change to the Customer can fix it, which triggers a new
		// reconciliation.
		return ctrl.Result{}, r.notReady(ctx, &customer, v1alpha1.ReasonInvalidSpec, err.Error())
	}
	if spec.Sources, err = r.sources(ctx, spec); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.ensureMetadata(ctx, &customer); err != nil {
		return ctrl.Result{}, err
	}

	progress, err := r.Provisioner.Reconcile(ctx, spec)
	if err != nil {
		// Returning the error retries with backoff.
		if statusErr := r.notReady(ctx, &customer, v1alpha1.ReasonProvisioningFailed, err.Error()); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{}, err
	}
	status := customer.Status.DeepCopy()
	status.BootstrapAddress = spec.BootstrapAddress()
	parts := []struct {
		condition string
		readiness provisioning.Readiness
		// waiting is Ready's reason while the cluster waits for this part.
		waiting string
	}{
		{v1alpha1.ConditionCertificateIssued, progress.Certificate, v1alpha1.ReasonCertificatePending},
		{v1alpha1.ConditionKafkaReady, progress.Kafka, v1alpha1.ReasonKafkaPending},
		{v1alpha1.ConditionUsersReady, progress.Users, v1alpha1.ReasonUsersPending},
		{v1alpha1.ConditionAgentAvailable, progress.Agent, v1alpha1.ReasonAgentPending},
	}
	ready := metav1.Condition{Type: v1alpha1.ConditionReady, Status: metav1.ConditionTrue, Reason: v1alpha1.ReasonDone, Message: "Clients can connect to " + spec.BootstrapAddress()}
	for _, part := range parts {
		condition := metav1.Condition{Type: part.condition, Status: metav1.ConditionTrue, Reason: v1alpha1.ReasonDone, Message: part.readiness.String()}
		if !part.readiness.Ready {
			condition.Status, condition.Reason = metav1.ConditionFalse, v1alpha1.ReasonPending
			// Ready names the first part the cluster waits for.
			if ready.Status == metav1.ConditionTrue {
				ready.Status, ready.Reason = metav1.ConditionFalse, part.waiting
				ready.Message = part.readiness.String()
			}
		}
		setCondition(status, customer.Generation, condition)
	}
	setCondition(status, customer.Generation, ready)
	cdcReady := metav1.Condition{Type: v1alpha1.ConditionCDCReady, Status: metav1.ConditionTrue, Reason: v1alpha1.ReasonDone, Message: progress.CDC.String()}
	if !progress.CDC.Ready {
		cdcReady.Status, cdcReady.Reason = metav1.ConditionFalse, v1alpha1.ReasonPending
	}
	setCondition(status, customer.Generation, cdcReady)
	untilTrial, onTrial := r.trial(&customer, status)
	// The warning goes out before it is recorded, so that a send that fails
	// is tried again rather than lost.
	if warned := status.TrialWarnedFor; warned != nil && (customer.Status.TrialWarnedFor == nil || !customer.Status.TrialWarnedFor.Equal(warned)) {
		fresh, err := r.fresh(ctx, &customer)
		if err != nil {
			return ctrl.Result{}, err
		}
		if fresh.Status.TrialWarnedFor == nil || !fresh.Status.TrialWarnedFor.Equal(warned) {
			if err := r.mailTrial(ctx, &customer, mail.TrialEnding); err != nil {
				return ctrl.Result{}, err
			}
		}
	}
	markProvisioned(status)
	if err := r.updateStatus(ctx, &customer, status); err != nil {
		return ctrl.Result{}, err
	}
	settings, err := r.alertSettings(ctx, spec.Namespace())
	if err != nil {
		return ctrl.Result{}, err
	}
	recordAlertThresholds(customer.Name, settings)
	requeue := r.ResyncInterval
	if !progress.Ready() || !progress.CDC.Ready {
		requeue = r.PollInterval
	}
	if onTrial {
		// A trial's warning and end come on time, not with the next resync.
		requeue = min(requeue, max(untilTrial, time.Second))
	}
	return ctrl.Result{RequeueAfter: requeue}, nil
}

// trial sets the TrialEnding condition of a customer on a trial, removes it
// from one that is not, and returns how long until the trial next needs
// looking at: its warning, then its end. From three days before the end,
// the customer is warned, once per end: TrialWarnedFor records the end it
// was warned about, so that a trial extended past the warning is warned
// about again.
func (r *CustomerReconciler) trial(customer *v1alpha1.Customer, status *v1alpha1.CustomerStatus) (time.Duration, bool) {
	trial := customer.Spec.Trial
	if trial == nil {
		meta.RemoveStatusCondition(&status.Conditions, v1alpha1.ConditionTrialEnding)
		status.TrialWarnedFor = nil
		return 0, false
	}
	now, end := r.now(), trial.EndsAt.Time
	warning := end.Add(-v1alpha1.TrialWarning)
	condition := metav1.Condition{
		Type: v1alpha1.ConditionTrialEnding, Status: metav1.ConditionFalse, Reason: v1alpha1.ReasonTrialRunning,
		Message: "The trial ends on " + formatEnd(end),
	}
	if now.Before(warning) {
		setCondition(status, customer.Generation, condition)
		return warning.Sub(now), true
	}
	condition.Status, condition.Reason = metav1.ConditionTrue, v1alpha1.ReasonTrialEnding
	condition.Message = "The trial ends on " + formatEnd(end) + "; the customer was warned"
	setCondition(status, customer.Generation, condition)
	if status.TrialWarnedFor == nil || !status.TrialWarnedFor.Equal(&trial.EndsAt) {
		status.TrialWarnedFor = trial.EndsAt.DeepCopy()
	}
	return end.Sub(now), true
}

// endTrial records that a customer's trial has ended and deletes the
// Customer, whose cluster then goes as any deleted customer's does, through
// the finalizer. The deletion only goes ahead for the Customer as read, so a
// trial extended or converted meanwhile is kept.
func (r *CustomerReconciler) endTrial(ctx context.Context, customer *v1alpha1.Customer) error {
	// The contact hears once, before the end is recorded.
	fresh, err := r.fresh(ctx, customer)
	if err != nil {
		return err
	}
	if ending := meta.FindStatusCondition(fresh.Status.Conditions, v1alpha1.ConditionTrialEnding); ending == nil || ending.Reason != v1alpha1.ReasonTrialEnded {
		if err := r.mailTrial(ctx, customer, mail.TrialEnded); err != nil {
			return err
		}
	}
	status := customer.Status.DeepCopy()
	setCondition(status, customer.Generation, metav1.Condition{
		Type: v1alpha1.ConditionTrialEnding, Status: metav1.ConditionTrue, Reason: v1alpha1.ReasonTrialEnded,
		Message: "The trial ended on " + formatEnd(customer.Spec.Trial.EndsAt.Time) + "; the customer is deprovisioned",
	})
	if err := r.updateStatus(ctx, customer, status); err != nil {
		return err
	}
	ctrl.LoggerFrom(ctx).Info("The trial ended; deleting the customer", "endsAt", customer.Spec.Trial.EndsAt.Time)
	err = r.Client.Delete(ctx, customer, client.Preconditions{UID: &customer.UID, ResourceVersion: &customer.ResourceVersion})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete the customer of an ended trial: %w", err)
	}
	return nil
}

// fresh reads customer from the API server rather than the cache, whose
// status may not have caught up with the reconciler's last update: a trial's
// email is sent only when the status does not record it yet.
func (r *CustomerReconciler) fresh(ctx context.Context, customer *v1alpha1.Customer) (*v1alpha1.Customer, error) {
	reader := r.Reader
	if reader == nil {
		reader = r.Client
	}
	var fresh v1alpha1.Customer
	if err := reader.Get(ctx, client.ObjectKeyFromObject(customer), &fresh); err != nil {
		return nil, fmt.Errorf("read the customer before its trial's email: %w", err)
	}
	return &fresh, nil
}

// mailTrial emails a trial's contact about it, if it has one.
func (r *CustomerReconciler) mailTrial(ctx context.Context, customer *v1alpha1.Customer, template mail.Template) error {
	trial := customer.Spec.Trial
	if r.Mail == nil || trial == nil || trial.Contact == "" {
		return nil
	}
	return r.Mail.Send(ctx, template, []string{trial.Contact}, mail.Trial{
		Company: customer.Spec.DisplayName,
		EndsAt:  mail.FormatTime(trial.EndsAt.Time),
		Link:    strings.TrimSuffix(r.ConsoleURL, "/") + "/",
	})
}

// formatEnd is a trial's end as conditions say it, such as 2026-11-22 09:00
// UTC.
func formatEnd(end time.Time) string {
	return end.UTC().Format("2006-01-02 15:04 UTC")
}

// alertSettings returns the customer's alert settings, or the defaults.
func (r *CustomerReconciler) alertSettings(ctx context.Context, namespace string) (v1alpha1.AlertSettingsSpec, error) {
	var settings v1alpha1.AlertSettings
	err := r.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: v1alpha1.AlertSettingsName}, &settings)
	switch {
	case apierrors.IsNotFound(err):
		return v1alpha1.DefaultAlertSettings, nil
	case err != nil:
		return v1alpha1.AlertSettingsSpec{}, fmt.Errorf("get the alert settings: %w", err)
	}
	return settings.Spec, nil
}

// deprovision deletes a deleted Customer's cluster, unless it is protected,
// and lets the Customer go once everything is gone.
func (r *CustomerReconciler) deprovision(ctx context.Context, customer *v1alpha1.Customer) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(customer, v1alpha1.Finalizer) {
		return ctrl.Result{}, nil
	}
	if customer.Spec.DeletionProtection {
		// Turning protection off changes the Customer, which triggers a new
		// reconciliation.
		return ctrl.Result{}, r.notReady(ctx, customer, v1alpha1.ReasonDeletionProtected,
			"The Customer is deleted, but its Kafka cluster is kept until deletion protection is turned off")
	}
	spec, err := r.spec(customer)
	if err != nil {
		return ctrl.Result{}, r.notReady(ctx, customer, v1alpha1.ReasonInvalidSpec, err.Error())
	}
	if err := r.Provisioner.Deprovision(ctx, spec); err != nil {
		return ctrl.Result{}, err
	}
	deleted, left, err := r.Provisioner.Deleted(ctx, spec)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !deleted {
		if err := r.notReady(ctx, customer, v1alpha1.ReasonDeprovisioning, left); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: r.PollInterval}, nil
	}
	patch := client.MergeFromWithOptions(customer.DeepCopy(), client.MergeFromWithOptimisticLock{})
	controllerutil.RemoveFinalizer(customer, v1alpha1.Finalizer)
	if err := r.Client.Patch(ctx, customer, patch); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove the finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

// spec is the provisioning spec of customer on the controller's platform.
func (r *CustomerReconciler) spec(customer *v1alpha1.Customer) (provisioning.Spec, error) {
	profile, err := provisioning.ProfileByName(customer.Spec.Profile)
	if err != nil {
		return provisioning.Spec{}, err
	}
	spec := provisioning.Spec{
		CustomerID: customer.Name, Profile: profile, Platform: r.Platform,
		DeletionProtection: customer.Spec.DeletionProtection,
	}
	return spec, spec.Validate()
}

// sources lists the customer's Postgres CDC sources, by name, including those
// being deleted: they need their runtime until their slot is dropped.
func (r *CustomerReconciler) sources(ctx context.Context, spec provisioning.Spec) ([]provisioning.Source, error) {
	var list v1alpha1.PostgresSourceList
	if err := r.Client.List(ctx, &list, client.InNamespace(spec.Namespace())); err != nil {
		return nil, fmt.Errorf("list the Postgres sources of customer %s: %w", spec.CustomerID, err)
	}
	sources := make([]provisioning.Source, len(list.Items))
	for i, source := range list.Items {
		sources[i] = provisioning.Source{Name: source.Name, Connector: connector(&source)}
	}
	slices.SortFunc(sources, func(a, b provisioning.Source) int { return strings.Compare(a.Name, b.Name) })
	return sources, nil
}

// connector is a source's Debezium connector: none until the source
// controller has handed its password to the Connect worker, and none once
// the source is deleted, so that the connector lets go of the slot before
// the source controller drops it.
func connector(source *v1alpha1.PostgresSource) *provisioning.Connector {
	if !source.DeletionTimestamp.IsZero() || source.Status.PasswordSecret == "" {
		return nil
	}
	names, err := cdc.NamesFor(source.Name, source.Spec.ID)
	if err != nil {
		// The CRD's validation keeps such sources out.
		return nil
	}
	tables := make([]cdc.Table, len(source.Spec.Tables))
	for i, table := range source.Spec.Tables {
		tables[i] = cdc.Table{Schema: table.Schema, Name: table.Name}
	}
	connection := source.Spec.Connection
	return &provisioning.Connector{
		Name:               names.Connector,
		Host:               connection.Host,
		Port:               int(connection.Port),
		Database:           connection.Database,
		Role:               names.Role,
		Publication:        names.Publication,
		Slot:               names.Slot,
		TLSMode:            cmp.Or(connection.TLS.Mode, v1alpha1.TLSVerifyFull),
		CA:                 connection.TLS.CA != "",
		TableIncludeList:   cdc.TableIncludeList(tables),
		RetentionMs:        source.Spec.RetentionMs,
		Running:            source.Spec.State != v1alpha1.StateStopped,
		CredentialsVersion: CredentialsVersion(source),
	}
}

// CredentialsVersion names the version of a source's credentials in the
// Connect worker: its password Secret, and a digest of its CA.
func CredentialsVersion(source *v1alpha1.PostgresSource) string {
	digest := sha256.Sum256([]byte(source.Spec.Connection.TLS.CA))
	return source.Status.PasswordSecret + "/" + hex.EncodeToString(digest[:4])
}

// ensureMetadata adds the finalizer, before anything is created that it must
// delete, and keeps the Clerk organization label equal to the spec.
func (r *CustomerReconciler) ensureMetadata(ctx context.Context, customer *v1alpha1.Customer) error {
	original := customer.DeepCopy()
	controllerutil.AddFinalizer(customer, v1alpha1.Finalizer)
	labels := customer.GetLabels()
	if id := customer.Spec.ClerkOrganizationID; id != "" {
		if labels == nil {
			labels = map[string]string{}
		}
		labels[v1alpha1.ClerkOrganizationLabel] = id
	} else {
		delete(labels, v1alpha1.ClerkOrganizationLabel)
	}
	customer.SetLabels(labels)
	if equality.Semantic.DeepEqual(original.ObjectMeta, customer.ObjectMeta) {
		return nil
	}
	patch := client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})
	if err := r.Client.Patch(ctx, customer, patch); err != nil {
		return fmt.Errorf("add the finalizer and labels: %w", err)
	}
	return nil
}

// notReady sets only the Ready condition, to False, for states in which the
// parts of the cluster are not looked at.
func (r *CustomerReconciler) notReady(ctx context.Context, customer *v1alpha1.Customer, reason, message string) error {
	updated := customer.Status.DeepCopy()
	setCondition(updated, customer.Generation, metav1.Condition{Type: v1alpha1.ConditionReady, Status: metav1.ConditionFalse, Reason: reason, Message: message})
	return r.updateStatus(ctx, customer, updated)
}

// updateStatus writes status unless it is what the Customer already has, so
// that a cluster that does not change causes no writes.
func (r *CustomerReconciler) updateStatus(ctx context.Context, customer *v1alpha1.Customer, status *v1alpha1.CustomerStatus) error {
	status.ObservedGeneration = customer.Generation
	if customer.DeletionTimestamp.IsZero() {
		recordCustomer(customer, status)
	} else {
		forgetCustomerMetrics(customer.Name)
	}
	if equality.Semantic.DeepEqual(customer.Status, *status) {
		return nil
	}
	customer.Status = *status
	if err := r.Client.Status().Update(ctx, customer); err != nil {
		return fmt.Errorf("update the status: %w", err)
	}
	return nil
}

// setCondition sets a condition for generation; its transition time only
// changes with its status.
func setCondition(status *v1alpha1.CustomerStatus, generation int64, condition metav1.Condition) {
	condition.ObservedGeneration = generation
	meta.SetStatusCondition(&status.Conditions, condition)
}
