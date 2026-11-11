package v1alpha1

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SampleAppName is the name of a customer's one SampleApp, in its namespace.
const SampleAppName = "sample"

// SampleAppRunTime is how long a sample app runs once started, or kept
// running, from the console.
const SampleAppRunTime = time.Hour

// SampleAppSpec is how long a customer's sample app runs.
type SampleAppSpec struct {
	// StopAt is when the sample app stops on its own: its Kafka user, its
	// topic with the messages in it and its Deployment are removed, and so
	// is the SampleApp.
	StopAt metav1.Time `json:"stopAt"`
}

// SampleAppStatus is how far a sample app is.
type SampleAppStatus struct {
	// Ready is whether the sample app runs: its Kafka user, its topic and its
	// Deployment are ready.
	// +optional
	Ready bool `json:"ready,omitempty"`
	// Message says how far the sample app is, for operators, such as what
	// it waits for, or why it cannot run on the platform.
	// +optional
	Message string `json:"message,omitempty"`
}

// SampleApp runs a sample shop in a customer's cluster, for the console's
// guided first ten minutes: a producer writes order events to the topic
// sample.orders, and the consumer group sample.order-emails reads them. A
// customer has at most one, named sample, in its namespace; it stops at
// spec.stopAt.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Stop at",type=string,JSONPath=`.spec.stopAt`
// +kubebuilder:printcolumn:name="Ready",type=boolean,JSONPath=`.status.ready`
// +kubebuilder:printcolumn:name="Message",type=string,JSONPath=`.status.message`,priority=1
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'sample'",message="a customer's sample app is named sample"
type SampleApp struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec SampleAppSpec `json:"spec"`
	// +optional
	Status SampleAppStatus `json:"status,omitempty"`
}

// SampleAppList is a list of SampleApps.
//
// +kubebuilder:object:root=true
type SampleAppList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SampleApp `json:"items"`
}
