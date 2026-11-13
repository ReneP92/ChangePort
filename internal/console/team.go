package console

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"k8s.io/utils/ptr"

	"github.com/ReneP92/changeport/internal/console/api"
)

// A customer's team is its Clerk organization's members and pending
// invitations, which the console manages so that a person belongs to one
// organization: an invitation to an address that belongs, or is invited, to
// an organization already is refused.

var teamAdminsOnly = problem(http.StatusForbidden, "Admins only", "Ask an admin of your organization to change its team.")

var lastAdmin = problem(http.StatusConflict, "The last admin",
	"Your organization keeps at least one admin, who manages its cluster and team. Make another member an admin first.")

// teamCaller returns the caller's customer for the team's routes, or the
// problem why there is none; admin asks for an organization admin.
func (s *Server) teamCaller(ctx context.Context, admin bool) (*customerContext, *api.Problem, error) {
	customer, principal, refused, err := s.customerOf(ctx)
	if err != nil || refused != nil {
		return nil, refused, err
	}
	if admin && !principal.Admin() {
		return nil, &teamAdminsOnly, nil
	}
	return &customerContext{customer: customer, principal: principal}, nil, nil
}

func toRole(role string) api.TeamRole {
	if role == adminRole {
		return api.Admin
	}
	return api.Member
}

func fromRole(role api.TeamRole) (string, bool) {
	switch role {
	case api.Admin:
		return adminRole, true
	case api.Member:
		return memberRole, true
	}
	return "", false
}

// team returns the organization's members and pending invitations.
func (s *Server) team(ctx context.Context, caller *customerContext) (api.Team, error) {
	organizationID := caller.customer.Spec.ClerkOrganizationID
	members, err := s.organizations.Members(ctx, organizationID)
	if err != nil {
		return api.Team{}, err
	}
	invitations, err := s.organizations.Invitations(ctx, organizationID)
	if err != nil {
		return api.Team{}, err
	}
	team := api.Team{Members: make([]api.Teammate, 0, len(members)), Invitations: make([]api.PendingInvitation, 0, len(invitations))}
	for _, m := range members {
		teammate := api.Teammate{UserID: m.UserID, Email: m.Email, Role: toRole(m.Role), JoinedAt: m.JoinedAt, You: m.UserID == caller.principal.UserID}
		if m.Name != "" {
			teammate.Name = ptr.To(m.Name)
		}
		team.Members = append(team.Members, teammate)
	}
	for _, i := range invitations {
		team.Invitations = append(team.Invitations, api.PendingInvitation{Id: i.ID, Email: i.Email, Role: toRole(i.Role), InvitedAt: i.CreatedAt})
	}
	return team, nil
}

// admins counts the admins among members.
func admins(members []Member) int {
	count := 0
	for _, m := range members {
		if m.Role == adminRole {
			count++
		}
	}
	return count
}

// GetTeam implements api.StrictServerInterface.
func (s *Server) GetTeam(ctx context.Context, _ api.GetTeamRequestObject) (api.GetTeamResponseObject, error) {
	caller, refused, err := s.teamCaller(ctx, false)
	switch {
	case err != nil:
		return nil, err
	case refused != nil && forbidden(refused):
		return api.GetTeam403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(*refused)}, nil
	case refused != nil:
		return api.GetTeam404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(*refused)}, nil
	}
	team, err := s.team(ctx, caller)
	if err != nil {
		return nil, err
	}
	return api.GetTeam200JSONResponse(team), nil
}

// InviteTeammate implements api.StrictServerInterface.
func (s *Server) InviteTeammate(ctx context.Context, request api.InviteTeammateRequestObject) (api.InviteTeammateResponseObject, error) {
	caller, refused, err := s.teamCaller(ctx, true)
	switch {
	case err != nil:
		return nil, err
	case refused != nil && forbidden(refused):
		return api.InviteTeammate403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(*refused)}, nil
	case refused != nil:
		return api.InviteTeammate404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(*refused)}, nil
	}
	invalid := func(field, detail string) api.InviteTeammateResponseObject {
		p := problem(http.StatusBadRequest, "Invalid invitation", detail)
		p.Field = &field
		return api.InviteTeammate400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse(p)}
	}
	if request.Body == nil {
		return invalid("email", "Enter an email address."), nil
	}
	email := strings.TrimSpace(request.Body.Email)
	role, ok := fromRole(request.Body.Role)
	switch {
	case !validEmail(email):
		return invalid("email", "Enter an email address, such as ada@example.com."), nil
	case !ok:
		return invalid("role", "Choose admin or member."), nil
	}
	organizationID := caller.customer.Spec.ClerkOrganizationID
	if p, err := s.unclaimed(ctx, email, organizationID); err != nil {
		return nil, err
	} else if p != nil {
		return api.InviteTeammate409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(*p)}, nil
	}
	if err := s.organizations.Invite(ctx, organizationID, email, role); errors.Is(err, ErrInvitationRefused) {
		return invalid("email", err.Error()), nil
	} else if err != nil {
		return nil, err
	}
	s.audit(ctx, caller.principal, caller.id(), "teammate invited", "role", role)
	team, err := s.team(ctx, caller)
	if err != nil {
		return nil, err
	}
	return api.InviteTeammate201JSONResponse(team), nil
}

// unclaimed returns the problem with inviting email, which may belong, or be
// invited, to no organization: a person belongs to one, and the one they
// join is the first.
func (s *Server) unclaimed(ctx context.Context, email, organizationID string) (*api.Problem, error) {
	claimed, err := s.organizations.Claimed(ctx, email)
	if err != nil {
		return nil, err
	}
	switch {
	case slices.Contains(claimed, organizationID):
		p := problem(http.StatusConflict, "Already on the team", email+" is a member of your organization, or invited to it.")
		p.Field = ptr.To("email")
		return &p, nil
	case len(claimed) > 0:
		p := problem(http.StatusConflict, "In another organization",
			email+" belongs, or is invited, to another organization. A person belongs to one organization; ask them to leave the other first, or invite another address.")
		p.Field = ptr.To("email")
		return &p, nil
	}
	return nil, nil
}

// RevokeInvitation implements api.StrictServerInterface.
func (s *Server) RevokeInvitation(ctx context.Context, request api.RevokeInvitationRequestObject) (api.RevokeInvitationResponseObject, error) {
	caller, refused, err := s.teamCaller(ctx, true)
	switch {
	case err != nil:
		return nil, err
	case refused != nil && forbidden(refused):
		return api.RevokeInvitation403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(*refused)}, nil
	case refused != nil:
		return api.RevokeInvitation404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(*refused)}, nil
	}
	organizationID := caller.customer.Spec.ClerkOrganizationID
	invitations, err := s.organizations.Invitations(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	if !slices.ContainsFunc(invitations, func(i Invitation) bool { return i.ID == request.InvitationID }) {
		return api.RevokeInvitation404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(
			problem(http.StatusNotFound, "No such invitation", "It may have been accepted or revoked meanwhile."))}, nil
	}
	if err := s.organizations.RevokeInvitation(ctx, organizationID, request.InvitationID); err != nil {
		return nil, err
	}
	s.audit(ctx, caller.principal, caller.id(), "invitation revoked", "invitation", request.InvitationID)
	team, err := s.team(ctx, caller)
	if err != nil {
		return nil, err
	}
	return api.RevokeInvitation200JSONResponse(team), nil
}

var noSuchTeammate = problem(http.StatusNotFound, "No such member", "They may have been removed meanwhile.")

// ChangeTeammateRole implements api.StrictServerInterface.
func (s *Server) ChangeTeammateRole(ctx context.Context, request api.ChangeTeammateRoleRequestObject) (api.ChangeTeammateRoleResponseObject, error) {
	caller, refused, err := s.teamCaller(ctx, true)
	switch {
	case err != nil:
		return nil, err
	case refused != nil && forbidden(refused):
		return api.ChangeTeammateRole403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(*refused)}, nil
	case refused != nil:
		return api.ChangeTeammateRole404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(*refused)}, nil
	}
	var role string
	ok := false
	if request.Body != nil {
		role, ok = fromRole(request.Body.Role)
	}
	if !ok {
		p := problem(http.StatusBadRequest, "Invalid role", "Choose admin or member.")
		p.Field = ptr.To("role")
		return api.ChangeTeammateRole400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse(p)}, nil
	}
	organizationID := caller.customer.Spec.ClerkOrganizationID
	members, err := s.organizations.Members(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(members, func(m Member) bool { return m.UserID == request.UserID })
	switch {
	case i < 0:
		return api.ChangeTeammateRole404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(noSuchTeammate)}, nil
	case members[i].Role == adminRole && role != adminRole && admins(members) == 1:
		return api.ChangeTeammateRole409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(lastAdmin)}, nil
	}
	if members[i].Role != role {
		if err := s.organizations.SetRole(ctx, organizationID, request.UserID, role); err != nil {
			return nil, err
		}
		s.audit(ctx, caller.principal, caller.id(), "teammate role changed", "member", request.UserID, "role", role)
	}
	team, err := s.team(ctx, caller)
	if err != nil {
		return nil, err
	}
	return api.ChangeTeammateRole200JSONResponse(team), nil
}

// RemoveTeammate implements api.StrictServerInterface.
func (s *Server) RemoveTeammate(ctx context.Context, request api.RemoveTeammateRequestObject) (api.RemoveTeammateResponseObject, error) {
	caller, refused, err := s.teamCaller(ctx, true)
	switch {
	case err != nil:
		return nil, err
	case refused != nil && forbidden(refused):
		return api.RemoveTeammate403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(*refused)}, nil
	case refused != nil:
		return api.RemoveTeammate404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(*refused)}, nil
	}
	organizationID := caller.customer.Spec.ClerkOrganizationID
	members, err := s.organizations.Members(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(members, func(m Member) bool { return m.UserID == request.UserID })
	switch {
	case i < 0:
		return api.RemoveTeammate404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(noSuchTeammate)}, nil
	case members[i].Role == adminRole && admins(members) == 1:
		return api.RemoveTeammate409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(lastAdmin)}, nil
	}
	if err := s.organizations.RemoveMember(ctx, organizationID, request.UserID); err != nil {
		return nil, err
	}
	s.audit(ctx, caller.principal, caller.id(), "teammate removed", "member", request.UserID)
	team, err := s.team(ctx, caller)
	if err != nil {
		return nil, err
	}
	return api.RemoveTeammate200JSONResponse(team), nil
}
