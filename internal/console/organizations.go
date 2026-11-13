package console

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/organization"
	"github.com/clerk/clerk-sdk-go/v2/organizationinvitation"
	"github.com/clerk/clerk-sdk-go/v2/organizationmembership"
	"github.com/clerk/clerk-sdk-go/v2/user"
)

// Organizations manages customers' Clerk organizations, whose members sign
// in to the console as the customer.
type Organizations interface {
	// Create creates an organization named name, without members, and
	// returns its ID.
	Create(ctx context.Context, name string) (string, error)
	// Invite invites email to join the organization in role, or returns an
	// error satisfying errors.Is(err, ErrInvitationRefused) with Clerk's
	// reason.
	Invite(ctx context.Context, organizationID, email, role string) error
	// AddAdmin makes an existing user an admin of the organization at once,
	// without an invitation.
	AddAdmin(ctx context.Context, organizationID, userID string) error
	// Delete deletes an organization.
	Delete(ctx context.Context, organizationID string) error
	// Memberships returns the organizations a user belongs to.
	Memberships(ctx context.Context, userID string) ([]Membership, error)
	// Claimed returns the organizations an email address belongs to, or is
	// invited to: those of the users with that address, and those of its
	// pending invitations.
	Claimed(ctx context.Context, email string) ([]string, error)
	// Members returns an organization's members, oldest first.
	Members(ctx context.Context, organizationID string) ([]Member, error)
	// Invitations returns an organization's pending invitations, oldest
	// first.
	Invitations(ctx context.Context, organizationID string) ([]Invitation, error)
	// RevokeInvitation revokes a pending invitation of the organization.
	RevokeInvitation(ctx context.Context, organizationID, invitationID string) error
	// SetRole gives a member of the organization another role.
	SetRole(ctx context.Context, organizationID, userID, role string) error
	// RemoveMember removes a member from the organization.
	RemoveMember(ctx context.Context, organizationID, userID string) error
}

// Membership is a user's membership of an organization, in its role.
type Membership struct {
	OrganizationID string
	Role           string
}

// Member is a member of an organization, for its team page.
type Member struct {
	UserID   string
	Name     string
	Email    string
	Role     string
	JoinedAt time.Time
}

// Invitation is a pending invitation to an organization.
type Invitation struct {
	ID        string
	Email     string
	Role      string
	CreatedAt time.Time
}

// The roles of organization members: admins manage the organization's
// cluster and team, and members use it.
const (
	adminRole  = "org:admin"
	memberRole = "org:member"
)

// ErrInvitationRefused means Clerk refused to invite an address, such as one
// it considers invalid.
var ErrInvitationRefused = errors.New("clerk refused the invitation")

// ClerkOrganizations manages organizations through Clerk's Backend API.
type ClerkOrganizations struct {
	organizations *organization.Client
	invitations   *organizationinvitation.Client
	memberships   *organizationmembership.Client
	users         *user.Client
	// redirectURL is where an invitation's link leads once accepted: the
	// console.
	redirectURL string
}

var _ Organizations = ClerkOrganizations{}

// NewClerkOrganizations returns Organizations that call Clerk's Backend API
// with secretKey, whose invitations lead to the console at redirectURL.
func NewClerkOrganizations(secretKey, redirectURL string) ClerkOrganizations {
	config := &clerk.ClientConfig{BackendConfig: clerk.BackendConfig{Key: &secretKey}}
	return ClerkOrganizations{
		organizations: organization.NewClient(config),
		invitations:   organizationinvitation.NewClient(config),
		memberships:   organizationmembership.NewClient(config),
		users:         user.NewClient(config),
		redirectURL:   redirectURL,
	}
}

// Create implements Organizations.
func (c ClerkOrganizations) Create(ctx context.Context, name string) (string, error) {
	created, err := c.organizations.Create(ctx, &organization.CreateParams{Name: &name})
	if err != nil {
		return "", fmt.Errorf("create the Clerk organization %q: %w", name, err)
	}
	return created.ID, nil
}

// Invite implements Organizations.
func (c ClerkOrganizations) Invite(ctx context.Context, organizationID, email, role string) error {
	params := &organizationinvitation.CreateParams{
		OrganizationID: organizationID,
		EmailAddress:   &email,
		Role:           clerk.String(role),
	}
	if c.redirectURL != "" {
		params.RedirectURL = &c.redirectURL
	}
	_, err := c.invitations.Create(ctx, params)
	var refused *clerk.APIErrorResponse
	if errors.As(err, &refused) && refused.HTTPStatusCode >= 400 && refused.HTTPStatusCode < 500 {
		reason := "Clerk refused the address"
		if len(refused.Errors) > 0 {
			reason = refused.Errors[0].Message
			if refused.Errors[0].LongMessage != "" {
				reason = refused.Errors[0].LongMessage
			}
		}
		return fmt.Errorf("%w: %s", ErrInvitationRefused, reason)
	}
	if err != nil {
		return fmt.Errorf("invite %s to Clerk organization %s: %w", email, organizationID, err)
	}
	return nil
}

// AddAdmin implements Organizations.
func (c ClerkOrganizations) AddAdmin(ctx context.Context, organizationID, userID string) error {
	_, err := c.memberships.Create(ctx, &organizationmembership.CreateParams{
		OrganizationID: organizationID,
		UserID:         &userID,
		Role:           clerk.String(adminRole),
	})
	if err != nil {
		return fmt.Errorf("add %s to Clerk organization %s as an admin: %w", userID, organizationID, err)
	}
	return nil
}

// Delete implements Organizations.
func (c ClerkOrganizations) Delete(ctx context.Context, organizationID string) error {
	if _, err := c.organizations.Delete(ctx, organizationID); err != nil {
		return fmt.Errorf("delete Clerk organization %s: %w", organizationID, err)
	}
	return nil
}

// pageSize is how many of a list Clerk returns at once, its most: an
// organization's members and invitations, and a user's memberships, fit in
// one page.
const pageSize = 500

// Memberships implements Organizations.
func (c ClerkOrganizations) Memberships(ctx context.Context, userID string) ([]Membership, error) {
	params := &user.ListOrganizationMembershipsParams{}
	params.Limit = clerk.Int64(pageSize)
	list, err := c.users.ListOrganizationMemberships(ctx, userID, params)
	if err != nil {
		return nil, fmt.Errorf("list the organizations of user %s: %w", userID, err)
	}
	memberships := make([]Membership, 0, len(list.OrganizationMemberships))
	for _, m := range list.OrganizationMemberships {
		if m.Organization != nil {
			memberships = append(memberships, Membership{OrganizationID: m.Organization.ID, Role: m.Role})
		}
	}
	return memberships, nil
}

// Claimed implements Organizations.
func (c ClerkOrganizations) Claimed(ctx context.Context, email string) ([]string, error) {
	var claimed []string
	users, err := c.users.List(ctx, &user.ListParams{EmailAddresses: []string{email}})
	if err != nil {
		return nil, fmt.Errorf("look up the users with address %s: %w", email, err)
	}
	for _, u := range users.Users {
		memberships, err := c.Memberships(ctx, u.ID)
		if err != nil {
			return nil, err
		}
		for _, m := range memberships {
			claimed = append(claimed, m.OrganizationID)
		}
	}
	params := &organizationinvitation.ListFromInstanceParams{
		Statuses: &[]string{"pending"},
		Query:    &email,
	}
	params.Limit = clerk.Int64(pageSize)
	invitations, err := c.invitations.ListFromInstance(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("look up the invitations of address %s: %w", email, err)
	}
	for _, invitation := range invitations.OrganizationInvitations {
		// The query matches parts of addresses too.
		if strings.EqualFold(invitation.EmailAddress, email) {
			claimed = append(claimed, invitation.OrganizationID)
		}
	}
	slices.Sort(claimed)
	return slices.Compact(claimed), nil
}

// Members implements Organizations.
func (c ClerkOrganizations) Members(ctx context.Context, organizationID string) ([]Member, error) {
	params := &organizationmembership.ListParams{OrganizationID: organizationID, OrderBy: clerk.String("created_at")}
	params.Limit = clerk.Int64(pageSize)
	list, err := c.memberships.List(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list the members of Clerk organization %s: %w", organizationID, err)
	}
	members := make([]Member, 0, len(list.OrganizationMemberships))
	for _, m := range list.OrganizationMemberships {
		member := Member{Role: m.Role, JoinedAt: time.UnixMilli(m.CreatedAt)}
		if d := m.PublicUserData; d != nil {
			member.UserID = d.UserID
			member.Email = d.Identifier
			member.Name = strings.TrimSpace(strings.Join([]string{ptrValue(d.FirstName), ptrValue(d.LastName)}, " "))
		}
		members = append(members, member)
	}
	return members, nil
}

// Invitations implements Organizations.
func (c ClerkOrganizations) Invitations(ctx context.Context, organizationID string) ([]Invitation, error) {
	params := &organizationinvitation.ListParams{OrganizationID: organizationID, Statuses: &[]string{"pending"}}
	params.Limit = clerk.Int64(pageSize)
	list, err := c.invitations.List(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list the invitations of Clerk organization %s: %w", organizationID, err)
	}
	invitations := make([]Invitation, 0, len(list.OrganizationInvitations))
	for _, i := range list.OrganizationInvitations {
		invitations = append(invitations, Invitation{ID: i.ID, Email: i.EmailAddress, Role: i.Role, CreatedAt: time.UnixMilli(i.CreatedAt)})
	}
	slices.SortFunc(invitations, func(a, b Invitation) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return invitations, nil
}

// RevokeInvitation implements Organizations.
func (c ClerkOrganizations) RevokeInvitation(ctx context.Context, organizationID, invitationID string) error {
	if _, err := c.invitations.Revoke(ctx, &organizationinvitation.RevokeParams{OrganizationID: organizationID, ID: invitationID}); err != nil {
		return fmt.Errorf("revoke invitation %s of Clerk organization %s: %w", invitationID, organizationID, err)
	}
	return nil
}

// SetRole implements Organizations.
func (c ClerkOrganizations) SetRole(ctx context.Context, organizationID, userID, role string) error {
	_, err := c.memberships.Update(ctx, &organizationmembership.UpdateParams{OrganizationID: organizationID, UserID: userID, Role: &role})
	if err != nil {
		return fmt.Errorf("make %s %s of Clerk organization %s: %w", userID, role, organizationID, err)
	}
	return nil
}

// RemoveMember implements Organizations.
func (c ClerkOrganizations) RemoveMember(ctx context.Context, organizationID, userID string) error {
	if _, err := c.memberships.Delete(ctx, &organizationmembership.DeleteParams{OrganizationID: organizationID, UserID: userID}); err != nil {
		return fmt.Errorf("remove %s from Clerk organization %s: %w", userID, organizationID, err)
	}
	return nil
}

func ptrValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
