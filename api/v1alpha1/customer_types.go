package v1alpha1

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// ClerkOrganizationLabel holds a Customer's Clerk organization ID, so the
	// console can find the Customer of a signed-in user with a label selector.
	// The provisioning controller keeps it equal to spec.clerkOrganizationID.
	ClerkOrganizationLabel = "changeport.io/clerk-organization"

	// EnvironmentLabel holds a Customer's environment, so the console finds an
	// organization's environment with one label selector, beside
	// ClerkOrganizationLabel. The provisioning controller keeps it equal to
	// spec.environment.
	EnvironmentLabel = "changeport.io/environment"

	// DefaultEnvironment is the environment of a customer that names none:
	// an organization's first, and every customer from before environments.
	DefaultEnvironment = "production"

	// Finalizer keeps a deleted Customer until the provisioning controller has
	// deleted its Kafka cluster.
	Finalizer = "changeport.io/deprovision"
)

// The condition types of a Customer, in the order its cluster gets ready.
const (
	// ConditionCertificateIssued is True once cert-manager has issued the
	// certificate of the cluster's public listener. The Kafka cluster is only
	// created after it.
	ConditionCertificateIssued = "CertificateIssued"
	// ConditionKafkaReady is True once Strimzi reports the Kafka cluster ready.
	ConditionKafkaReady = "KafkaReady"
	// ConditionUsersReady is True once Strimzi has created the cluster's Kafka
	// users and their credentials.
	ConditionUsersReady = "UsersReady"
	// ConditionAgentAvailable is True once the Kafka agent next to the cluster
	// is running, or when the platform runs no agent.
	ConditionAgentAvailable = "AgentAvailable"
	// ConditionReady is True when all of the above are, so customers can
	// connect. While it is False, its reason says what the cluster waits for.
	ConditionReady = "Ready"
	// ConditionCDCReady is True once the Kafka Connect worker and the schema
	// registry of the customer's Postgres CDC sources run, or when the
	// customer has none. Ready does not include it.
	ConditionCDCReady = "CDCReady"

	// ConditionTrialEnding is set on a customer on a trial only: False while
	// the trial runs, True from three days before it ends, when the customer
	// is warned. At the end, the provisioning controller deletes the
	// Customer.
	ConditionTrialEnding = "TrialEnding"
)

// The reasons of a Customer's conditions.
const (
	// ReasonDone is the reason of every True condition.
	ReasonDone = "Done"
	// ReasonPending is the reason of a part of the cluster that is not ready
	// yet.
	ReasonPending = "Pending"

	// Ready's reasons while it is False: the part the cluster waits for.
	ReasonCertificatePending = "CertificatePending"
	ReasonKafkaPending       = "KafkaPending"
	ReasonUsersPending       = "UsersPending"
	ReasonAgentPending       = "AgentPending"

	// ReasonInvalidSpec means the platform cannot run the spec, until the
	// Customer changes.
	ReasonInvalidSpec = "InvalidSpec"
	// ReasonProvisioningFailed means applying the template failed; the
	// controller retries with backoff.
	ReasonProvisioningFailed = "ProvisioningFailed"
	// ReasonDeletionProtected means the Customer is deleted but its cluster
	// is kept until deletion protection is turned off.
	ReasonDeletionProtected = "DeletionProtected"
	// ReasonDeprovisioning means the cluster of a deleted Customer is being
	// deleted.
	ReasonDeprovisioning = "Deprovisioning"

	// TrialEnding's reasons: the trial runs, ends within three days, or has
	// ended and the Customer is being deleted.
	ReasonTrialRunning = "TrialRunning"
	ReasonTrialEnding  = "TrialEnding"
	ReasonTrialEnded   = "TrialEnded"
)

// TrialWarning is how long before a trial ends its customer is warned.
const TrialWarning = 3 * 24 * time.Hour

// CustomerTrial is a trial, which ends on its own unless an operator
// extends it or converts the customer.
type CustomerTrial struct {
	// EndsAt is when the trial ends: the provisioning controller then
	// deletes the Customer, which deprovisions its cluster.
	EndsAt metav1.Time `json:"endsAt"`

	// Contact is who the provisioning controller emails when the trial is
	// about to end and when it has ended: the trial request's verified
	// address. Without one, nobody is emailed.
	// +kubebuilder:validation:MaxLength=254
	// +optional
	Contact string `json:"contact,omitempty"`
}

// CustomerSpec is what Changeport runs for a customer. A customer on a trial
// cannot have deletion protection: its trial ends on its own.
// +kubebuilder:validation:XValidation:rule="!has(self.trial) || !has(self.deletionProtection) || !self.deletionProtection",message="a trial cannot have deletion protection; convert the customer first"
type CustomerSpec struct {
	// DisplayName is the customer's name as people know it, such as "Acme
	// Corp".
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=100
	DisplayName string `json:"displayName"`

	// Profile sizes the customer's dedicated Kafka cluster. It cannot change:
	// moving a cluster to another profile is a migration, not an edit.
	// +kubebuilder:validation:Enum=dev;dev-ha;production
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="the profile cannot change"
	Profile string `json:"profile"`

	// Environment names the customer's cluster among its organization's,
	// such as production or staging: each environment is a Customer of its
	// own, with its own cluster. It is unique within the organization, which
	// the console checks. Renaming it changes only the console's URLs:
	// everything the cluster runs is named by the customer ID.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=20
	// +kubebuilder:validation:Pattern=`^[a-z]([-a-z0-9]*[a-z0-9])?$`
	// +kubebuilder:default=production
	// +optional
	Environment string `json:"environment,omitempty"`

	// DefaultEnvironment, when true, makes this the environment its
	// organization's console opens by default. The console sets it on one
	// of the organization's Customers and clears it on the others. Should
	// two have it, as when two admins choose at once, the first in order
	// wins until the next choice.
	// +optional
	DefaultEnvironment bool `json:"defaultEnvironment,omitempty"`

	// ClerkOrganizationID is the Clerk organization whose members sign in to
	// the console as this customer, if any.
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^org_[A-Za-z0-9]+$`
	// +optional
	ClerkOrganizationID string `json:"clerkOrganizationID,omitempty"`

	// DeletionProtection, when true, keeps the customer's Kafka cluster when
	// the Customer is deleted: the controller deprovisions it only once
	// protection is turned off.
	// +optional
	DeletionProtection bool `json:"deletionProtection,omitempty"`

	// Trial, when set, is the customer's trial, which ends on its own.
	// +optional
	Trial *CustomerTrial `json:"trial,omitempty"`
}

// CustomerStatus is how far the customer's Kafka cluster is.
type CustomerStatus struct {
	// ObservedGeneration is the generation of the Customer the conditions
	// describe.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// BootstrapAddress is the public address the customer's clients
	// bootstrap from, as host:port.
	// +optional
	BootstrapAddress string `json:"bootstrapAddress,omitempty"`

	// ProvisionedAt is when the cluster first became Ready. Alerts about a
	// cluster that is down wait for it, so that a cluster still being created
	// raises none.
	// +optional
	ProvisionedAt *metav1.Time `json:"provisionedAt,omitempty"`

	// Conditions are CertificateIssued, KafkaReady, UsersReady,
	// AgentAvailable and Ready, CDCReady, and TrialEnding for a trial.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// TrialWarnedFor is the end of the trial its customer was warned about,
	// once per end: a trial extended past the warning is warned about again.
	// +optional
	TrialWarnedFor *metav1.Time `json:"trialWarnedFor,omitempty"`
}

// Customer is a customer of Changeport and their dedicated Kafka cluster.
// Its name is the customer ID, which names the namespace customer-<name> and
// the cluster's public names, so it follows the rules of
// provisioning.ValidateCustomerID.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Display name",type=string,JSONPath=`.spec.displayName`
// +kubebuilder:printcolumn:name="Environment",type=string,JSONPath=`.spec.environment`
// +kubebuilder:printcolumn:name="Profile",type=string,JSONPath=`.spec.profile`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`
// +kubebuilder:printcolumn:name="Trial ends",type=string,JSONPath=`.spec.trial.endsAt`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:validation:XValidation:rule="size(self.metadata.name) <= 30 && self.metadata.name.matches('^[a-z]([-a-z0-9]*[a-z0-9])?$')",message="the name is the customer ID: at most 30 lowercase letters, digits and dashes, starting with a letter and ending with a letter or digit"
type Customer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CustomerSpec   `json:"spec"`
	Status CustomerStatus `json:"status,omitempty"`
}

// CustomerList is a list of Customers.
//
// +kubebuilder:object:root=true
type CustomerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Customer `json:"items"`
}
