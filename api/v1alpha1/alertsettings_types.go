package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AlertSettingsName is the name of a customer's one AlertSettings, in its
// namespace.
const AlertSettingsName = "alerts"

// AlertReceiverSecretType is the type of the Secrets the console writes a
// receiver's secret into, once, under AlertReceiverKey.
const AlertReceiverSecretType = "changeport.io/alert-receiver"

// AlertReceiverKey is the key of a receiver's secret in its Secret: the Slack
// webhook URL, the PagerDuty routing key or the webhook URL.
const AlertReceiverKey = "value"

// ConsumerLagAlert is the alert on a consumer group that falls behind.
type ConsumerLagAlert struct {
	// Enabled turns the alert on.
	// +kubebuilder:default=true
	// +optional
	Enabled bool `json:"enabled"`

	// Seconds is how far behind a group may fall, as the age of the oldest
	// message it has not consumed.
	// +kubebuilder:validation:Minimum=60
	// +kubebuilder:validation:Maximum=86400
	// +kubebuilder:default=600
	// +optional
	Seconds int64 `json:"seconds"`
}

// ConsumerStuckAlert is the alert on a consumer group that stopped committing
// while its topic grows.
type ConsumerStuckAlert struct {
	// Enabled turns the alert on.
	// +kubebuilder:default=true
	// +optional
	Enabled bool `json:"enabled"`

	// Seconds is how long a group may go without committing while messages
	// wait for it.
	// +kubebuilder:validation:Minimum=300
	// +kubebuilder:validation:Maximum=86400
	// +kubebuilder:default=900
	// +optional
	Seconds int64 `json:"seconds"`
}

// ThrottlingAlert is the alert on clients the brokers throttle.
type ThrottlingAlert struct {
	// Enabled turns the alert on.
	// +kubebuilder:default=true
	// +optional
	Enabled bool `json:"enabled"`

	// Milliseconds is the longest a client may be throttled for at a time.
	// +kubebuilder:validation:Minimum=10
	// +kubebuilder:validation:Maximum=60000
	// +kubebuilder:default=100
	// +optional
	Milliseconds int64 `json:"milliseconds"`
}

// ErrorsAlert is the alert on failing requests: the brokers' failed requests
// per topic, and the clients' own failed records.
type ErrorsAlert struct {
	// Enabled turns the alert on.
	// +kubebuilder:default=true
	// +optional
	Enabled bool `json:"enabled"`

	// Percent is the share of requests or records that may fail.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	// +kubebuilder:default=5
	// +optional
	Percent int64 `json:"percent"`
}

// AlertReceiver is where else a customer's alerts go: a write-once Secret of
// type AlertReceiverSecretType in the customer's namespace, which the
// console writes and only the provisioner reads.
type AlertReceiver struct {
	// Secret names the Secret, which holds the receiver's secret under
	// AlertReceiverKey.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^alert-receiver-[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Secret string `json:"secret"`

	// Hint shows customers which receiver it is without the secret, such as
	// the webhook's host.
	// +kubebuilder:validation:MaxLength=200
	// +optional
	Hint string `json:"hint,omitempty"`
}

// AlertReceivers are where a customer's alerts go besides the console.
type AlertReceivers struct {
	// Slack posts to a Slack incoming webhook.
	// +optional
	Slack *AlertReceiver `json:"slack,omitempty"`

	// PagerDuty raises incidents through a PagerDuty Events API v2
	// integration's routing key.
	// +optional
	PagerDuty *AlertReceiver `json:"pagerDuty,omitempty"`

	// Webhook posts Alertmanager's webhook payload to an HTTPS URL.
	// +optional
	Webhook *AlertReceiver `json:"webhook,omitempty"`
}

// AlertSettingsSpec is how a customer's alerts about their own applications
// fire, and where they go (docs/alerts.md).
type AlertSettingsSpec struct {
	// ConsumerLag is the alert on a consumer group that falls behind.
	// +kubebuilder:default={}
	// +optional
	ConsumerLag ConsumerLagAlert `json:"consumerLag"`

	// ConsumerStuck is the alert on a consumer group that stopped committing
	// while its topic grows.
	// +kubebuilder:default={}
	// +optional
	ConsumerStuck ConsumerStuckAlert `json:"consumerStuck"`

	// Throttling is the alert on clients the brokers throttle.
	// +kubebuilder:default={}
	// +optional
	Throttling ThrottlingAlert `json:"throttling"`

	// Errors is the alert on failing requests.
	// +kubebuilder:default={}
	// +optional
	Errors ErrorsAlert `json:"errors"`

	// Receivers are where the alerts go besides the console.
	// +kubebuilder:default={}
	// +optional
	Receivers AlertReceivers `json:"receivers"`
}

// DefaultAlertSettings are the settings of a customer without AlertSettings,
// as the CRD's defaults fill them in.
var DefaultAlertSettings = AlertSettingsSpec{
	ConsumerLag:   ConsumerLagAlert{Enabled: true, Seconds: 600},
	ConsumerStuck: ConsumerStuckAlert{Enabled: true, Seconds: 900},
	Throttling:    ThrottlingAlert{Enabled: true, Milliseconds: 100},
	Errors:        ErrorsAlert{Enabled: true, Percent: 5},
}

// AlertSettings are a customer's settings of the alerts about their own
// applications: consumer lag, stuck consumers, throttling and errors, and
// where the alerts go besides the console. A customer has at most one, named
// alerts, in its namespace; without it the defaults apply.
//
// +kubebuilder:object:root=true
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'alerts'",message="a customer's alert settings are named alerts"
type AlertSettings struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec AlertSettingsSpec `json:"spec"`
}

// AlertSettingsList is a list of AlertSettings.
//
// +kubebuilder:object:root=true
type AlertSettingsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AlertSettings `json:"items"`
}
