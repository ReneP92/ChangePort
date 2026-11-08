package controller

import (
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/ReneP92/changeport/api/v1alpha1"
	"github.com/ReneP92/changeport/internal/provisioning"
)

// The metrics of the customers' clusters, by tenant, which the Customer
// controller exports and the alerts of deploy/base/alerting watch
// (docs/metrics-labels.md): a cluster that is not ready, how many brokers a
// ready one should have, and when a trial ends.
var (
	customerReady = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "changeport_customer_ready",
		Help: "1 while a customer's Customer is Ready, else 0.",
	}, []string{"tenant"})
	customerBrokers = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "changeport_customer_brokers",
		Help: "The brokers a customer's cluster runs, as its profile says.",
	}, []string{"tenant", "profile"})
	alertThreshold = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "changeport_alert_threshold",
		Help: "The threshold of a customer's alert about its own applications, as its alert settings say, by alert; absent while the alert is off.",
	}, []string{"tenant", "alert"})
	customerProvisioned = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "changeport_customer_provisioned_timestamp_seconds",
		Help: "When a customer's cluster first became Ready, in seconds since the epoch; absent before.",
	}, []string{"tenant"})
	customerTrialEnds = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "changeport_customer_trial_ends_timestamp_seconds",
		Help: "When a customer's trial ends, in seconds since the epoch; absent for a customer not on a trial.",
	}, []string{"tenant"})
)

func init() {
	metrics.Registry.MustRegister(customerReady, customerBrokers, customerProvisioned, customerTrialEnds, alertThreshold)
}

// recordCustomer records a Customer's status.
func recordCustomer(customer *v1alpha1.Customer, status *v1alpha1.CustomerStatus) {
	tenant := customer.Name
	ready := 0.0
	if meta.IsStatusConditionTrue(status.Conditions, v1alpha1.ConditionReady) {
		ready = 1
	}
	customerReady.WithLabelValues(tenant).Set(ready)
	customerBrokers.DeletePartialMatch(prometheus.Labels{"tenant": tenant})
	if profile, err := provisioning.ProfileByName(customer.Spec.Profile); err == nil {
		customerBrokers.WithLabelValues(tenant, profile.Name).Set(float64(profile.Replicas))
	}
	if status.ProvisionedAt != nil {
		customerProvisioned.WithLabelValues(tenant).Set(float64(status.ProvisionedAt.Unix()))
	} else {
		customerProvisioned.DeleteLabelValues(tenant)
	}
	if trial := customer.Spec.Trial; trial != nil {
		customerTrialEnds.WithLabelValues(tenant).Set(float64(trial.EndsAt.Unix()))
	} else {
		customerTrialEnds.DeleteLabelValues(tenant)
	}
}

// forgetCustomerMetrics removes a deprovisioned customer's series: a cluster
// being deleted raises no alerts.
func forgetCustomerMetrics(tenant string) {
	customerReady.DeleteLabelValues(tenant)
	customerBrokers.DeletePartialMatch(prometheus.Labels{"tenant": tenant})
	customerProvisioned.DeleteLabelValues(tenant)
	customerTrialEnds.DeleteLabelValues(tenant)
	alertThreshold.DeletePartialMatch(prometheus.Labels{"tenant": tenant})
}

// recordAlertThresholds records the thresholds of a customer's alerts about
// its own applications (deploy/base/alerting/rules/customers.yaml), in the
// units the rules compare them in; an alert that is off has none.
func recordAlertThresholds(tenant string, settings v1alpha1.AlertSettingsSpec) {
	thresholds := []struct {
		alert   string
		enabled bool
		value   float64
	}{
		{"ConsumerGroupLagHigh", settings.ConsumerLag.Enabled, float64(settings.ConsumerLag.Seconds)},
		{"ConsumerGroupStuck", settings.ConsumerStuck.Enabled, float64(settings.ConsumerStuck.Seconds)},
		{"ClientThrottled", settings.Throttling.Enabled, float64(settings.Throttling.Milliseconds)},
		{"TopicRequestErrors", settings.Errors.Enabled, float64(settings.Errors.Percent) / 100},
		{"ClientRecordErrors", settings.Errors.Enabled, float64(settings.Errors.Percent) / 100},
	}
	for _, t := range thresholds {
		if t.enabled {
			alertThreshold.WithLabelValues(tenant, t.alert).Set(t.value)
		} else {
			alertThreshold.DeleteLabelValues(tenant, t.alert)
		}
	}
}

// markProvisioned sets when the cluster first became Ready: when its Ready
// condition last turned true, the first time it is.
func markProvisioned(status *v1alpha1.CustomerStatus) {
	if status.ProvisionedAt != nil {
		return
	}
	if ready := meta.FindStatusCondition(status.Conditions, v1alpha1.ConditionReady); ready != nil && ready.Status == metav1.ConditionTrue {
		at := ready.LastTransitionTime
		status.ProvisionedAt = &at
	}
}
