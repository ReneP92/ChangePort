package console

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	openapi_types "github.com/oapi-codegen/runtime/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/ReneP92/changeport/api/v1alpha1"
	"github.com/ReneP92/changeport/internal/console/api"
	email "github.com/ReneP92/changeport/internal/mail"
)

// Trial requests (#132): a signed-in user whose organization has no Kafka
// cluster asks for a trial, and an operator approves it, which provisions a
// customer for them, or declines it with a reason they see.

const (
	// declineWait is how long a declined user waits before asking again.
	declineWait = 7 * 24 * time.Hour
	// The TrialRequest resource's limits, in characters.
	maxTrialFieldLength    = 100
	maxUseCaseLength       = 2000
	maxDeclineReasonLength = 1000
	// trialProfile is the profile of an approved trial unless the operator
	// picks another.
	trialProfile = "dev"
	// trialLength is how long an approved trial lasts, unless an operator
	// extends it or converts the customer.
	trialLength = 14 * 24 * time.Hour
	// The furthest and nearest an operator may move a trial's end.
	maxTrialExtension = 90 * 24 * time.Hour
	minTrialRemaining = time.Hour
)

var trialRequestIDPattern = regexp.MustCompile(`^tr-[a-km-np-z2-9]{6}$`)

// Mail sends email (internal/mail).
type Mail interface {
	Send(ctx context.Context, template email.Template, to []string, data any) error
}

// The rough sizes of a request as emails say them, as the web app does.
var (
	throughputLabels = map[string]string{
		v1alpha1.ThroughputUnder1MBps: "Under 1 MB/s", v1alpha1.Throughput1To10MBps: "1 to 10 MB/s",
		v1alpha1.Throughput10To100MBps: "10 to 100 MB/s", v1alpha1.ThroughputOver100MBps: "Over 100 MB/s",
	}
	retentionLabels = map[string]string{
		v1alpha1.RetentionUpTo1Day: "Up to a day", v1alpha1.RetentionUpTo7Days: "Up to a week",
		v1alpha1.RetentionUpTo30Days: "Up to a month", v1alpha1.RetentionOver30Days: "Longer than a month",
	}
)

// sendMail sends an email about a trial, whose failure the Mailer has
// logged and audited; it never fails what the user did, which happened.
func (s *Server) sendMail(ctx context.Context, template email.Template, to []string, data any) {
	if len(to) == 0 {
		return
	}
	_ = s.mail.Send(context.WithoutCancel(ctx), template, to, data)
}

// consoleLink is the console's address at path, for email.
func (s *Server) consoleLink(path string) string {
	return strings.TrimSuffix(s.config.Origins[0], "/") + path
}

func validTrialRequestID(id string) bool {
	return trialRequestIDPattern.MatchString(id)
}

// RequestTrial implements api.StrictServerInterface.
func (s *Server) RequestTrial(ctx context.Context, request api.RequestTrialRequestObject) (api.RequestTrialResponseObject, error) {
	principal, err := mustPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if s.operator(principal) {
		return api.RequestTrial403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(problem(http.StatusForbidden,
			"Not for operators", "Switch away from the Changeport staff organization to request a trial."))}, nil
	}
	if principal.OrganizationID != "" {
		customers, err := s.customers.ByOrganization(ctx, principal.OrganizationID)
		if err != nil {
			return nil, err
		}
		if len(customers) > 0 {
			return api.RequestTrial409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(problem(http.StatusConflict,
				"Your organization has a cluster", "Switch to another organization, or none, to request a trial."))}, nil
		}
	}

	invalid := func(field, detail string) api.RequestTrialResponseObject {
		p := problem(http.StatusBadRequest, "Invalid trial request", detail)
		p.Field = &field
		return api.RequestTrial400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse(p)}
	}
	body := request.Body
	company, role, useCase := strings.TrimSpace(body.Company), strings.TrimSpace(body.Role), strings.TrimSpace(body.UseCase)
	switch {
	case !lengthWithin(company, maxTrialFieldLength):
		return invalid("company", fmt.Sprintf("Name your company in 1 to %d characters.", maxTrialFieldLength)), nil
	case !lengthWithin(role, maxTrialFieldLength):
		return invalid("role", fmt.Sprintf("Give your role in 1 to %d characters.", maxTrialFieldLength)), nil
	case !lengthWithin(useCase, maxUseCaseLength):
		return invalid("useCase", fmt.Sprintf("Say what you want Kafka for in 1 to %d characters.", maxUseCaseLength)), nil
	case !body.Throughput.Valid():
		return invalid("throughput", "Choose under-1-mbps, 1-10-mbps, 10-100-mbps or over-100-mbps."), nil
	case !body.Retention.Valid():
		return invalid("retention", "Choose up-to-1-day, up-to-7-days, up-to-30-days or over-30-days."), nil
	}
	now := time.Now()
	preferredStart := ""
	if body.PreferredStart != nil {
		// A day of slack for time zones ahead of UTC's yesterday.
		day, today := body.PreferredStart.Time, now.UTC().Truncate(24*time.Hour)
		if day.Before(today.AddDate(0, 0, -1)) || day.After(today.AddDate(1, 0, 0)) {
			return invalid("preferredStart", "Choose a day from today to a year from now, or leave it empty."), nil
		}
		preferredStart = day.Format(time.DateOnly)
	}

	mine, err := s.trialRequests.ByUser(ctx, principal.UserID)
	if err != nil {
		return nil, err
	}
	if refused := refuseAnotherTrialRequest(mine, now); refused != nil {
		return refused, nil
	}
	verified, err := s.users.VerifiedEmail(ctx, principal.UserID)
	if errors.Is(err, ErrNoVerifiedEmail) {
		return api.RequestTrial403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(problem(http.StatusForbidden,
			"Verify your email address", "Verify your primary email address in your account settings, then request a trial."))}, nil
	}
	if err != nil {
		return nil, err
	}

	trial := &v1alpha1.TrialRequest{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{v1alpha1.ClerkUserLabel: principal.UserID}},
		Spec: v1alpha1.TrialRequestSpec{
			ClerkUserID:    principal.UserID,
			Email:          verified,
			Company:        company,
			Role:           role,
			UseCase:        useCase,
			Throughput:     string(body.Throughput),
			Retention:      string(body.Retention),
			PreferredStart: preferredStart,
		},
	}
	for attempt := 0; ; attempt++ {
		trial.Name, err = newID("tr-")
		if err == nil {
			err = s.trialRequests.Create(ctx, trial)
		}
		if !errors.Is(err, ErrTrialRequestExists) || attempt == 4 {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	// Two requests at once both pass the checks above. Every request looks
	// again once it exists, and all but the oldest pending one give way, so
	// a user never has two pending.
	mine, err = s.trialRequests.ByUser(ctx, principal.UserID)
	if err == nil {
		if oldest := oldestPending(mine); oldest != nil && oldest.Name != trial.Name {
			err = s.trialRequests.Delete(context.WithoutCancel(ctx), trial.Name)
			if err == nil {
				return api.RequestTrial409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(pendingTrialRequest(oldest))}, nil
			}
		}
	}
	if err != nil {
		return nil, err
	}
	s.audit(ctx, principal, trial.Name, "trial requested", "company", company)
	start := "As soon as possible"
	if preferredStart != "" {
		start = preferredStart
	}
	s.sendMail(ctx, email.TrialRequested, s.config.OperatorEmails, email.TrialRequest{
		ID: trial.Name, Company: company, Email: trial.Spec.Email, Role: role, UseCase: useCase,
		Throughput: throughputLabels[trial.Spec.Throughput], Retention: retentionLabels[trial.Spec.Retention],
		Start: start, Link: s.consoleLink("/operator/trials"),
	})
	return api.RequestTrial201JSONResponse(toAPITrialRequest(trial)), nil
}

// refuseAnotherTrialRequest refuses a user's new request while they have one
// pending, once they had a trial, and for a while after a decline.
func refuseAnotherTrialRequest(mine []v1alpha1.TrialRequest, now time.Time) api.RequestTrialResponseObject {
	for _, request := range mine {
		switch request.PhaseOrPending() {
		case v1alpha1.TrialRequestPending:
			return api.RequestTrial409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(pendingTrialRequest(&request))}
		case v1alpha1.TrialRequestApproved:
			return api.RequestTrial409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(problem(http.StatusConflict,
				"You had a trial", fmt.Sprintf("Your trial request %s was approved.", request.Name)))}
		case v1alpha1.TrialRequestDeclined:
			if request.Status.DecidedAt == nil {
				continue
			}
			if again := request.Status.DecidedAt.Add(declineWait); now.Before(again) {
				return api.RequestTrial429JSONResponse{TooManyRequestsJSONResponse: api.TooManyRequestsJSONResponse(problem(http.StatusTooManyRequests,
					"Declined recently", fmt.Sprintf("Your trial request %s was declined; you may ask again from %s.", request.Name, again.UTC().Format(time.DateOnly))))}
			}
		}
	}
	return nil
}

func pendingTrialRequest(request *v1alpha1.TrialRequest) api.Problem {
	return problem(http.StatusConflict, "You have a pending request",
		fmt.Sprintf("Your trial request %s waits for an operator's decision.", request.Name))
}

// oldestPending returns the oldest pending request of requests sorted newest
// first, or nil.
func oldestPending(requests []v1alpha1.TrialRequest) *v1alpha1.TrialRequest {
	for i, request := range slices.Backward(requests) {
		if request.PhaseOrPending() == v1alpha1.TrialRequestPending {
			return &requests[i]
		}
	}
	return nil
}

// ListMyTrialRequests implements api.StrictServerInterface.
func (s *Server) ListMyTrialRequests(ctx context.Context, _ api.ListMyTrialRequestsRequestObject) (api.ListMyTrialRequestsResponseObject, error) {
	principal, err := mustPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	mine, err := s.trialRequests.ByUser(ctx, principal.UserID)
	if err != nil {
		return nil, err
	}
	return api.ListMyTrialRequests200JSONResponse(toAPITrialRequests(mine)), nil
}

// ListTrialRequests implements api.StrictServerInterface.
func (s *Server) ListTrialRequests(ctx context.Context, _ api.ListTrialRequestsRequestObject) (api.ListTrialRequestsResponseObject, error) {
	principal, err := mustPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if !s.operator(principal) {
		return api.ListTrialRequests403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(operatorsOnly)}, nil
	}
	all, err := s.trialRequests.List(ctx)
	if err != nil {
		return nil, err
	}
	return api.ListTrialRequests200JSONResponse(toAPITrialRequests(all)), nil
}

// ApproveTrialRequest implements api.StrictServerInterface: it provisions the
// requester's customer as ProvisionCustomer does, with the requester as the
// organization's admin, and undoes it all when the decision cannot be
// written, such as when another operator decided first.
func (s *Server) ApproveTrialRequest(ctx context.Context, request api.ApproveTrialRequestRequestObject) (api.ApproveTrialRequestResponseObject, error) {
	principal, err := mustPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if !s.operator(principal) {
		return api.ApproveTrialRequest403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(operatorsOnly)}, nil
	}
	profile := trialProfile
	if request.Body.Profile != nil {
		profile = string(*request.Body.Profile)
	}
	if !validProfile(profile) {
		p := problem(http.StatusBadRequest, "Invalid approval", "Choose dev, dev-ha or production, or leave the profile out for dev.")
		p.Field = ptr.To("profile")
		return api.ApproveTrialRequest400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse(p)}, nil
	}
	trial, refused, err := s.pendingTrialRequest(ctx, request.TrialRequestID)
	switch {
	case err != nil:
		return nil, err
	case refused != nil && refused.Status == http.StatusNotFound:
		return api.ApproveTrialRequest404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(*refused)}, nil
	case refused != nil:
		return api.ApproveTrialRequest409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(*refused)}, nil
	}

	// Trials never have deletion protection: they end on their own.
	customer := &v1alpha1.Customer{Spec: v1alpha1.CustomerSpec{
		DisplayName: trial.Spec.Company,
		Profile:     profile,
		Trial: &v1alpha1.CustomerTrial{
			EndsAt:  metav1.NewTime(time.Now().Add(trialLength).Truncate(time.Second)),
			Contact: trial.Spec.Email,
		},
	}}
	existing, p, err := s.trialOrganization(ctx, trial.Spec.ClerkUserID)
	switch {
	case err != nil:
		return nil, err
	case p != nil:
		return api.ApproveTrialRequest409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(*p)}, nil
	}
	undo, err := s.provision(ctx, customer, existing, func(organizationID string) error {
		// The requester is the admin of their own organization already.
		if existing != "" {
			return nil
		}
		return s.organizations.AddAdmin(ctx, organizationID, trial.Spec.ClerkUserID)
	})
	if err != nil {
		return nil, err
	}
	trial.Status = v1alpha1.TrialRequestStatus{
		Phase:          v1alpha1.TrialRequestApproved,
		DecidedBy:      principal.UserID,
		DecidedAt:      ptr.To(metav1.Now()),
		Customer:       customer.Name,
		OrganizationID: customer.Spec.ClerkOrganizationID,
	}
	if err := s.trialRequests.Decide(ctx, trial); err != nil {
		undo()
		if errors.Is(err, ErrTrialRequestChanged) {
			return api.ApproveTrialRequest409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(decidedMeanwhile(trial.Name))}, nil
		}
		return nil, err
	}
	s.audit(ctx, principal, customer.Name, "trial request approved", "trial_request", trial.Name, "requester", trial.Spec.ClerkUserID,
		"profile", profile, "customer_organization", customer.Spec.ClerkOrganizationID, "trial_ends_at", customer.Spec.Trial.EndsAt.UTC())
	s.sendMail(ctx, email.TrialApproved, []string{trial.Spec.Email}, email.TrialDecision{
		Company: trial.Spec.Company, Link: s.consoleLink("/"), EndsAt: email.FormatTime(customer.Spec.Trial.EndsAt.Time),
	})
	return api.ApproveTrialRequest200JSONResponse(toAPITrialRequest(trial)), nil
}

// DeclineTrialRequest implements api.StrictServerInterface.
func (s *Server) DeclineTrialRequest(ctx context.Context, request api.DeclineTrialRequestRequestObject) (api.DeclineTrialRequestResponseObject, error) {
	principal, err := mustPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if !s.operator(principal) {
		return api.DeclineTrialRequest403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(operatorsOnly)}, nil
	}
	reason := strings.TrimSpace(request.Body.Reason)
	if !lengthWithin(reason, maxDeclineReasonLength) {
		p := problem(http.StatusBadRequest, "Invalid decline", fmt.Sprintf("Give the requester a reason in 1 to %d characters.", maxDeclineReasonLength))
		p.Field = ptr.To("reason")
		return api.DeclineTrialRequest400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse(p)}, nil
	}
	trial, refused, err := s.pendingTrialRequest(ctx, request.TrialRequestID)
	switch {
	case err != nil:
		return nil, err
	case refused != nil && refused.Status == http.StatusNotFound:
		return api.DeclineTrialRequest404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(*refused)}, nil
	case refused != nil:
		return api.DeclineTrialRequest409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(*refused)}, nil
	}
	trial.Status = v1alpha1.TrialRequestStatus{
		Phase:     v1alpha1.TrialRequestDeclined,
		DecidedBy: principal.UserID,
		DecidedAt: ptr.To(metav1.Now()),
		Reason:    reason,
	}
	if err := s.trialRequests.Decide(ctx, trial); errors.Is(err, ErrTrialRequestChanged) {
		return api.DeclineTrialRequest409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(decidedMeanwhile(trial.Name))}, nil
	} else if err != nil {
		return nil, err
	}
	s.audit(ctx, principal, trial.Name, "trial request declined", "requester", trial.Spec.ClerkUserID, "reason", reason)
	s.sendMail(ctx, email.TrialDeclined, []string{trial.Spec.Email}, email.TrialDecision{
		Company: trial.Spec.Company, Reason: reason,
		AskAgain: trial.Status.DecidedAt.Add(declineWait).UTC().Format("2 January 2006"),
	})
	return api.DeclineTrialRequest200JSONResponse(toAPITrialRequest(trial)), nil
}

// pendingTrialRequest returns the trial request id while it waits for a
// decision, or a 404 or 409 problem.
func (s *Server) pendingTrialRequest(ctx context.Context, id string) (*v1alpha1.TrialRequest, *api.Problem, error) {
	trial, err := s.trialRequests.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if trial == nil {
		return nil, ptr.To(problem(http.StatusNotFound, "No such trial request", fmt.Sprintf("There is no trial request %s.", id))), nil
	}
	if phase := trial.PhaseOrPending(); phase != v1alpha1.TrialRequestPending {
		return nil, ptr.To(problem(http.StatusConflict, "Decided already",
			fmt.Sprintf("Trial request %s was %s already.", id, strings.ToLower(phase)))), nil
	}
	return trial, nil, nil
}

func decidedMeanwhile(id string) api.Problem {
	return problem(http.StatusConflict, "Decided meanwhile",
		fmt.Sprintf("Trial request %s changed while you decided, perhaps by another operator; nothing was changed. Reload it.", id))
}

// lengthWithin reports whether s has 1 to limit characters.
func lengthWithin(s string, limit int) bool {
	n := utf8.RuneCountInString(s)
	return n >= 1 && n <= limit
}

func toAPITrialRequests(requests []v1alpha1.TrialRequest) api.TrialRequestList {
	list := api.TrialRequestList{TrialRequests: make([]api.TrialRequest, 0, len(requests))}
	for i := range requests {
		list.TrialRequests = append(list.TrialRequests, toAPITrialRequest(&requests[i]))
	}
	return list
}

func toAPITrialRequest(request *v1alpha1.TrialRequest) api.TrialRequest {
	out := api.TrialRequest{
		Id:         request.Name,
		UserID:     request.Spec.ClerkUserID,
		Email:      request.Spec.Email,
		Company:    request.Spec.Company,
		Role:       request.Spec.Role,
		UseCase:    request.Spec.UseCase,
		Throughput: api.TrialThroughput(request.Spec.Throughput),
		Retention:  api.TrialRetention(request.Spec.Retention),
		Phase:      api.TrialRequestPhase(request.PhaseOrPending()),
		CreatedAt:  request.CreationTimestamp.Time,
	}
	if day, err := time.Parse(time.DateOnly, request.Spec.PreferredStart); err == nil {
		out.PreferredStart = &openapi_types.Date{Time: day}
	}
	if request.Status.DecidedAt != nil {
		out.DecidedAt = ptr.To(request.Status.DecidedAt.Time)
	}
	if request.Status.Reason != "" {
		out.Reason = ptr.To(request.Status.Reason)
	}
	if request.Status.Customer != "" {
		out.CustomerID = ptr.To(request.Status.Customer)
	}
	if request.Status.OrganizationID != "" {
		out.OrganizationID = ptr.To(request.Status.OrganizationID)
	}
	return out
}

// ChangeTrial implements api.StrictServerInterface: it extends a customer's
// trial to a new end, or converts the customer, ending its trial.
func (s *Server) ChangeTrial(ctx context.Context, request api.ChangeTrialRequestObject) (api.ChangeTrialResponseObject, error) {
	principal, err := mustPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if !s.operator(principal) {
		return api.ChangeTrial403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(operatorsOnly)}, nil
	}
	body := request.Body
	var trial *v1alpha1.CustomerTrial
	switch body.Action {
	case api.Extend:
		now := time.Now()
		if body.EndsAt == nil || body.EndsAt.Before(now.Add(minTrialRemaining)) || body.EndsAt.After(now.Add(maxTrialExtension)) {
			p := problem(http.StatusBadRequest, "Invalid trial end", "Choose a new end at least an hour and at most 90 days from now.")
			p.Field = ptr.To("endsAt")
			return api.ChangeTrial400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse(p)}, nil
		}
		// The contact stays; the end moves.
		trial = &v1alpha1.CustomerTrial{EndsAt: metav1.NewTime(body.EndsAt.Truncate(time.Second))}
	case api.Convert:
	default:
		p := problem(http.StatusBadRequest, "Invalid trial change", "Choose extend or convert.")
		p.Field = ptr.To("action")
		return api.ChangeTrial400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse(p)}, nil
	}
	customer, err := s.customers.Get(ctx, request.CustomerID)
	if err != nil {
		return nil, err
	}
	switch {
	case customer == nil:
		return api.ChangeTrial404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(noSuchCustomer(request.CustomerID))}, nil
	case customer.DeletionTimestamp != nil:
		return api.ChangeTrial409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(problem(http.StatusConflict,
			"Being deleted", fmt.Sprintf("Customer %s is being deprovisioned; its trial can no longer change.", customer.Name)))}, nil
	case customer.Spec.Trial == nil:
		return api.ChangeTrial409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(problem(http.StatusConflict,
			"Not on a trial", fmt.Sprintf("Customer %s is not on a trial.", customer.Name)))}, nil
	}
	if trial != nil {
		trial.Contact = customer.Spec.Trial.Contact
	}
	if err := s.customers.SetTrial(ctx, customer, trial); errors.Is(err, ErrCustomerChanged) {
		return api.ChangeTrial409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(problem(http.StatusConflict,
			"Changed meanwhile", fmt.Sprintf("Customer %s changed while you decided; nothing was changed. Reload it.", customer.Name)))}, nil
	} else if err != nil {
		return nil, err
	}
	if trial == nil {
		s.audit(ctx, principal, customer.Name, "trial converted")
	} else {
		s.audit(ctx, principal, customer.Name, "trial extended", "trial_ends_at", trial.EndsAt.UTC())
	}
	return api.ChangeTrial200JSONResponse(toAPI(customer)), nil
}

// trialOrganization returns the organization a trial's cluster goes into,
// as a person belongs to one: a new one for a requester without, "", or
// the one they are the admin of when it has no cluster, such as one
// deprovisioned. Otherwise it returns why the trial cannot be approved.
func (s *Server) trialOrganization(ctx context.Context, userID string) (string, *api.Problem, error) {
	memberships, err := s.organizations.Memberships(ctx, userID)
	if err != nil {
		return "", nil, err
	}
	refuse := func(detail string) (string, *api.Problem, error) {
		p := problem(http.StatusConflict, "The requester has an organization", detail)
		return "", &p, nil
	}
	switch {
	case len(memberships) == 0:
		return "", nil, nil
	case len(memberships) > 1:
		return refuse("The requester belongs to several organizations, and a person belongs to one. Ask support to remove them from the others first.")
	}
	membership := memberships[0]
	customers, err := s.customers.ByOrganization(ctx, membership.OrganizationID)
	if err != nil {
		return "", nil, err
	}
	switch {
	case len(customers) > 0:
		return refuse(fmt.Sprintf("The requester belongs to the organization of customer %s, which has its cluster already.", customers[0].Name))
	case membership.Role != adminRole:
		return refuse("The requester is a member, not an admin, of an organization without a cluster. Its admin asks for the trial instead.")
	}
	return membership.OrganizationID, nil, nil
}
