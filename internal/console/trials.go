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
)

var trialRequestIDPattern = regexp.MustCompile(`^tr-[a-km-np-z2-9]{6}$`)

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
		customer, err := s.customers.ByOrganization(ctx, principal.OrganizationID)
		if err != nil {
			return nil, err
		}
		if customer != nil {
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
	email, err := s.users.VerifiedEmail(ctx, principal.UserID)
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
			Email:          email,
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
	customer := &v1alpha1.Customer{Spec: v1alpha1.CustomerSpec{DisplayName: trial.Spec.Company, Profile: profile}}
	undo, err := s.provision(ctx, customer, func(organizationID string) error {
		return s.organizations.AddAdmin(ctx, organizationID, trial.Spec.ClerkUserID)
	})
	if err != nil {
		return nil, err
	}
	trial.Status = v1alpha1.TrialRequestStatus{
		Phase:     v1alpha1.TrialRequestApproved,
		DecidedBy: principal.UserID,
		DecidedAt: ptr.To(metav1.Now()),
		Customer:  customer.Name,
	}
	if err := s.trialRequests.Decide(ctx, trial); err != nil {
		undo()
		if errors.Is(err, ErrTrialRequestChanged) {
			return api.ApproveTrialRequest409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(decidedMeanwhile(trial.Name))}, nil
		}
		return nil, err
	}
	s.audit(ctx, principal, customer.Name, "trial request approved", "trial_request", trial.Name, "requester", trial.Spec.ClerkUserID,
		"profile", profile, "customer_organization", customer.Spec.ClerkOrganizationID)
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
	return out
}
