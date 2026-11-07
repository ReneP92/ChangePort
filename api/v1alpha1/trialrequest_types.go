package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClerkUserLabel holds the Clerk user ID of whoever made a TrialRequest, so
// the console finds a user's requests with a label selector. The console
// sets it equal to spec.clerkUserID.
const ClerkUserLabel = "changeport.io/clerk-user"

// The phases of a TrialRequest. A request without a phase is pending.
const (
	TrialRequestPending  = "Pending"
	TrialRequestApproved = "Approved"
	TrialRequestDeclined = "Declined"
)

// The rough sizes a trial request gives, for the operator deciding on it.
const (
	ThroughputUnder1MBps  = "under-1-mbps"
	Throughput1To10MBps   = "1-10-mbps"
	Throughput10To100MBps = "10-100-mbps"
	ThroughputOver100MBps = "over-100-mbps"
	RetentionUpTo1Day     = "up-to-1-day"
	RetentionUpTo7Days    = "up-to-7-days"
	RetentionUpTo30Days   = "up-to-30-days"
	RetentionOver30Days   = "over-30-days"
)

// TrialRequestSpec is what someone asked for when they requested a trial.
// It cannot change.
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="a trial request cannot change"
type TrialRequestSpec struct {
	// ClerkUserID is who asked, as Clerk knows them, taken from their
	// session and never from the form.
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^user_[A-Za-z0-9]+$`
	ClerkUserID string `json:"clerkUserID"`

	// Email is the requester's verified primary email address, taken from
	// Clerk and never from the form.
	// +kubebuilder:validation:MinLength=3
	// +kubebuilder:validation:MaxLength=254
	Email string `json:"email"`

	// Company is the requester's company, which names the customer once the
	// request is approved.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=100
	Company string `json:"company"`

	// Role is the requester's role at the company, such as "Staff engineer".
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=100
	Role string `json:"role"`

	// UseCase is what they want Kafka for, in their words.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2000
	UseCase string `json:"useCase"`

	// Throughput is roughly how much they write at peak.
	// +kubebuilder:validation:Enum=under-1-mbps;1-10-mbps;10-100-mbps;over-100-mbps
	Throughput string `json:"throughput"`

	// Retention is roughly how long they keep messages.
	// +kubebuilder:validation:Enum=up-to-1-day;up-to-7-days;up-to-30-days;over-30-days
	Retention string `json:"retention"`

	// PreferredStart is the day they would like to start, if they said.
	// +kubebuilder:validation:Format=date
	// +optional
	PreferredStart string `json:"preferredStart,omitempty"`
}

// TrialRequestStatus is the operators' decision. A decision is final.
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.phase) || oldSelf.phase == 'Pending' || (has(self.phase) && self.phase == oldSelf.phase)",message="a decision is final"
// +kubebuilder:validation:XValidation:rule="!has(self.phase) || self.phase != 'Approved' || (has(self.customer) && has(self.decidedBy) && has(self.decidedAt))",message="an approval names the customer, who decided and when"
// +kubebuilder:validation:XValidation:rule="!has(self.phase) || self.phase != 'Declined' || (has(self.reason) && has(self.decidedBy) && has(self.decidedAt))",message="a decline gives the reason, who decided and when"
type TrialRequestStatus struct {
	// Phase is Pending until an operator approves or declines the request.
	// +kubebuilder:validation:Enum=Pending;Approved;Declined
	// +optional
	Phase string `json:"phase,omitempty"`

	// DecidedBy is the Clerk user ID of the operator who decided.
	// +kubebuilder:validation:MaxLength=63
	// +optional
	DecidedBy string `json:"decidedBy,omitempty"`

	// DecidedAt is when the operator decided.
	// +optional
	DecidedAt *metav1.Time `json:"decidedAt,omitempty"`

	// Reason is why the request was declined, which the requester sees.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1000
	// +optional
	Reason string `json:"reason,omitempty"`

	// Customer is the ID of the customer an approval created.
	// +kubebuilder:validation:MaxLength=30
	// +optional
	Customer string `json:"customer,omitempty"`
}

// TrialRequest is someone's request for a trial, which an operator approves,
// creating a customer for them, or declines. Its name is the request's ID,
// such as tr-7f3k2q.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=trial
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Company",type=string,JSONPath=`.spec.company`
// +kubebuilder:printcolumn:name="Email",type=string,JSONPath=`.spec.email`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Customer",type=string,JSONPath=`.status.customer`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:validation:XValidation:rule="self.metadata.name.matches('^tr-[a-km-np-z2-9]{6}$')",message="the name is the request ID: tr- and six lowercase letters or digits, such as tr-7f3k2q"
type TrialRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TrialRequestSpec   `json:"spec"`
	Status TrialRequestStatus `json:"status,omitempty"`
}

// PhaseOrPending returns the request's phase, Pending until one is set.
func (t *TrialRequest) PhaseOrPending() string {
	if t.Status.Phase == "" {
		return TrialRequestPending
	}
	return t.Status.Phase
}

// TrialRequestList is a list of TrialRequests.
//
// +kubebuilder:object:root=true
type TrialRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TrialRequest `json:"items"`
}
