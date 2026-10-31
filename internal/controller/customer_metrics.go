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
// (docs/metrics-labels.md): a cluster that is not ready, and how many
// brokers a ready one should have.
var (
	customerReady = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "changeport_customer_ready",
		Help: "1 while a customer's Customer is Ready, else 0.",
	}, []string{"tenant"})
	customerBrokers = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "changeport_customer_brokers",
		Help: "The brokers a customer's cluster runs, as its profile says.",
	}, []string{"tenant", "profile"})
	customerProvisioned = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "changeport_customer_provisioned_timestamp_seconds",
		Help: "When a customer's cluster first became Ready, in seconds since the epoch; absent before.",
	}, []string{"tenant"})
)

func init() {
	metrics.Registry.MustRegister(customerReady, customerBrokers, customerProvisioned)
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
}

// forgetCustomerMetrics removes a deprovisioned customer's series: a cluster
// being deleted raises no alerts.
func forgetCustomerMetrics(tenant string) {
	customerReady.DeleteLabelValues(tenant)
	customerBrokers.DeletePartialMatch(prometheus.Labels{"tenant": tenant})
	customerProvisioned.DeleteLabelValues(tenant)
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
