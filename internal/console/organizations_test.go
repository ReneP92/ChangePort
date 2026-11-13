package console

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ReneP92/changeport/api/v1alpha1"
	"github.com/ReneP92/changeport/internal/console/api"
)

// A person belongs to one organization: these test where the console makes
// memberships, which keep to it.

func TestProvisioningRefusesAnAddressThatHasAnOrganization(t *testing.T) {
	f := newFixture(t)
	f.organizations.claimed = map[string][]string{"ada@hooli.example": {"org_hooli"}}
	staff := f.signer.token(t, session{user: "user_6", org: staffOrg, role: "member"})
	status, body := send(t, f.handler, http.MethodPost, "/api/operator/customers", staff,
		`{"displayName":"Initech","profile":"dev","deletionProtection":false,"inviteEmail":"ada@hooli.example"}`)
	if status != 400 || !strings.Contains(body, `"field":"inviteEmail"`) || !strings.Contains(body, "belongs, or is invited, to an organization already") {
		t.Errorf("provisioning with an address of another organization = %d %s, want 400", status, body)
	}
	if len(f.organizations.created) != 0 || len(f.organizations.invited) != 0 {
		t.Errorf("created %v and invited %v, want nothing", f.organizations.created, f.organizations.invited)
	}
}

func TestTrialApprovalKeepsTheRequesterToOneOrganization(t *testing.T) {
	f := newFixture(t)
	staff := f.signer.token(t, session{user: "user_6", org: staffOrg, role: "member"})
	approve := func(id string) (int, string) {
		return send(t, f.handler, http.MethodPost, "/api/operator/trial-requests/"+id+"/approve", staff, `{}`)
	}
	f.organizations.memberships = map[string][]Membership{
		// The admin of an organization whose customer was deprovisioned.
		"user_8": {{OrganizationID: "org_initech", Role: adminRole}},
		// A member, not an admin, of such an organization.
		"user_10": {{OrganizationID: "org_vandelay", Role: memberRole}},
		// A member of a customer's organization.
		"user_11": {{OrganizationID: "org_acme", Role: adminRole}},
		"user_12": {{OrganizationID: "org_one", Role: adminRole}, {OrganizationID: "org_two", Role: adminRole}},
	}

	status, body := approve("tr-pend22")
	if status != 200 {
		t.Fatalf("approving the admin of an organization without a cluster = %d %s, want 200", status, body)
	}
	var approved api.TrialRequest
	if err := json.Unmarshal([]byte(body), &approved); err != nil {
		t.Fatal(err)
	}
	customer, _ := f.customers.Get(t.Context(), *approved.CustomerID)
	if customer == nil || customer.Spec.ClerkOrganizationID != "org_initech" || customer.Labels[v1alpha1.ClerkOrganizationLabel] != "org_initech" {
		t.Errorf("the trial's customer = %+v, want it in the requester's organization org_initech", customer)
	}
	if len(f.organizations.created) != 0 || len(f.organizations.admins) != 0 {
		t.Errorf("created %v and made admins %v, want neither: the requester is the admin of their organization", f.organizations.created, f.organizations.admins)
	}

	for user, want := range map[string]string{
		"user_10": "a member, not an admin",
		"user_11": "has its cluster already",
		"user_12": "belongs to several organizations",
	} {
		id := "tr-" + strings.TrimPrefix(user, "user_") + "abcd"
		f.trialRequests.add(id, user, "Company "+user, time.Now())
		if status, body := approve(id); status != 409 || !strings.Contains(body, want) {
			t.Errorf("approving %s = %d %s, want 409 saying %q", user, status, body, want)
		}
	}
	if len(f.organizations.created) != 0 {
		t.Errorf("refusals created organizations %v", f.organizations.created)
	}
}

func TestSomeoneInSeveralOrganizationsSeesNothing(t *testing.T) {
	f := newFixture(t)
	f.organizations.memberships = map[string][]Membership{
		"user_3": {{OrganizationID: "org_acme", Role: adminRole}, {OrganizationID: "org_globex", Role: memberRole}},
	}
	_, body, _ := get(t, f.handler, "/api/me", f.signer.token(t, session{user: "user_3", org: "org_acme", role: "admin"}))
	if !strings.Contains(body, `"access":"several"`) || strings.Contains(body, "customerID") {
		t.Errorf("GET /api/me in two organizations = %s, want access several", body)
	}
	_, body, _ = get(t, f.handler, "/api/me", f.signer.token(t, session{user: "user_2", org: "org_acme", role: "member"}))
	if !strings.Contains(body, `"access":"customer"`) {
		t.Errorf("GET /api/me in one organization = %s, want the customer", body)
	}
}

func decodeTeam(t *testing.T, body string) api.Team {
	t.Helper()
	var team api.Team
	if err := json.Unmarshal([]byte(body), &team); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	return team
}

func TestAdminsManageTheTeam(t *testing.T) {
	f := newFixture(t)
	joined := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	f.organizations.members = map[string][]Member{"org_acme": {
		{UserID: "user_3", Name: "Ada Lovelace", Email: "ada@acme.example", Role: adminRole, JoinedAt: joined},
		{UserID: "user_2", Email: "grace@acme.example", Role: memberRole, JoinedAt: joined.Add(time.Hour)},
	}}
	f.organizations.claimed = map[string][]string{"linus@hooli.example": {"org_hooli"}, "grace@acme.example": {"org_acme"}}
	admin := f.signer.token(t, session{user: "user_3", org: "org_acme", role: "admin"})
	member := f.signer.token(t, session{user: "user_2", org: "org_acme", role: "member"})

	status, body, _ := get(t, f.handler, "/api/customer/team", member)
	team := decodeTeam(t, body)
	if status != 200 || len(team.Members) != 2 || team.Members[0].Role != api.Admin || team.Members[1].You != true || team.Members[0].Name == nil {
		t.Errorf("a member's GET /api/customer/team = %d %+v", status, team)
	}
	for _, request := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/customer/team/invitations", `{"email":"new@acme.example","role":"member"}`},
		{http.MethodPut, "/api/customer/team/members/user_3", `{"role":"member"}`},
		{http.MethodDelete, "/api/customer/team/members/user_3", ""},
	} {
		if status, _ := send(t, f.handler, request.method, request.path, member, request.body); status != 403 {
			t.Errorf("a member's %s %s = %d, want 403", request.method, request.path, status)
		}
	}

	invite := func(body string) (int, string) {
		return send(t, f.handler, http.MethodPost, "/api/customer/team/invitations", admin, body)
	}
	for body, want := range map[string]string{
		`{"email":"linus@hooli.example","role":"member"}`: "In another organization",
		`{"email":"grace@acme.example","role":"member"}`:  "Already on the team",
	} {
		if status, got := invite(body); status != 409 || !strings.Contains(got, want) {
			t.Errorf("inviting %s = %d %s, want 409 %q", body, status, got, want)
		}
	}
	if status, body := invite(`{"email":"not an address","role":"member"}`); status != 400 || !strings.Contains(body, `"field":"email"`) {
		t.Errorf("inviting a non-address = %d %s, want 400", status, body)
	}
	status, body = invite(`{"email":"new@acme.example","role":"admin"}`)
	team = decodeTeam(t, body)
	if status != 201 || len(team.Invitations) != 1 || team.Invitations[0].Email != "new@acme.example" || team.Invitations[0].Role != api.Admin {
		t.Errorf("inviting = %d %+v", status, team)
	}
	if !strings.Contains(f.log.String(), `"msg":"teammate invited","audit":true,"tenant":"acme","user":"user_3"`) {
		t.Errorf("no audit line for the invitation in %s", f.log.String())
	}
	status, body = send(t, f.handler, http.MethodDelete, "/api/customer/team/invitations/"+team.Invitations[0].Id, admin, "")
	if status != 200 || len(decodeTeam(t, body).Invitations) != 0 {
		t.Errorf("revoking = %d %s", status, body)
	}
	if status, _ := send(t, f.handler, http.MethodDelete, "/api/customer/team/invitations/orginv_gone", admin, ""); status != 404 {
		t.Errorf("revoking an unknown invitation = %d, want 404", status)
	}

	// The organization keeps an admin.
	for _, request := range []struct{ method, body string }{{http.MethodPut, `{"role":"member"}`}, {http.MethodDelete, ""}} {
		if status, body := send(t, f.handler, request.method, "/api/customer/team/members/user_3", admin, request.body); status != 409 || !strings.Contains(body, "The last admin") {
			t.Errorf("%s the last admin = %d %s, want 409", request.method, status, body)
		}
	}
	status, body = send(t, f.handler, http.MethodPut, "/api/customer/team/members/user_2", admin, `{"role":"admin"}`)
	if status != 200 || decodeTeam(t, body).Members[1].Role != api.Admin {
		t.Errorf("making grace an admin = %d %s", status, body)
	}
	status, body = send(t, f.handler, http.MethodDelete, "/api/customer/team/members/user_3", admin, "")
	if status != 200 || len(decodeTeam(t, body).Members) != 1 {
		t.Errorf("removing ada, with grace an admin = %d %s", status, body)
	}
	if status, _ := send(t, f.handler, http.MethodDelete, "/api/customer/team/members/user_404", admin, ""); status != 404 {
		t.Errorf("removing an unknown member = %d, want 404", status)
	}
}
