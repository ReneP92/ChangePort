package console

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/ReneP92/changeport/api/v1alpha1"
	"github.com/ReneP92/changeport/internal/console/api"
	email "github.com/ReneP92/changeport/internal/mail"
)

// fakeTrialRequests holds tr-pend22, Initech's pending request by user_8.
type fakeTrialRequests struct {
	requests map[string]*v1alpha1.TrialRequest
	// created counts the requests created, which are a second apart.
	created int
	// deleted lists the requests deleted, by ID.
	deleted []string
	// changeOnDecide fails every decision as if another operator decided
	// first.
	changeOnDecide bool
	// afterCreate runs once, after the next request is created.
	afterCreate func()
}

func newFakeTrialRequests() *fakeTrialRequests {
	f := &fakeTrialRequests{requests: map[string]*v1alpha1.TrialRequest{}}
	f.add("tr-pend22", "user_8", "Initech", time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC))
	return f
}

// add puts a pending request of user's company, made at created, in the
// store.
func (f *fakeTrialRequests) add(id, user, company string, created time.Time) {
	request := &v1alpha1.TrialRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:              id,
			Labels:            map[string]string{v1alpha1.ClerkUserLabel: user},
			CreationTimestamp: metav1.NewTime(created),
			ResourceVersion:   "1",
		},
		Spec: v1alpha1.TrialRequestSpec{
			ClerkUserID: user, Email: user + "@example.com", Company: company, Role: "CTO",
			UseCase: "Orders between services", Throughput: v1alpha1.ThroughputUnder1MBps, Retention: v1alpha1.RetentionUpTo7Days,
		},
	}
	f.requests[id] = request
}

func (f *fakeTrialRequests) List(context.Context) ([]v1alpha1.TrialRequest, error) {
	var all []v1alpha1.TrialRequest
	for _, request := range f.requests {
		all = append(all, *request.DeepCopy())
	}
	return newestFirst(all), nil
}

func (f *fakeTrialRequests) ByUser(ctx context.Context, userID string) ([]v1alpha1.TrialRequest, error) {
	all, _ := f.List(ctx)
	var mine []v1alpha1.TrialRequest
	for _, request := range all {
		if request.Labels[v1alpha1.ClerkUserLabel] == userID {
			mine = append(mine, request)
		}
	}
	return mine, nil
}

func (f *fakeTrialRequests) Get(_ context.Context, id string) (*v1alpha1.TrialRequest, error) {
	if request, ok := f.requests[id]; ok {
		return request.DeepCopy(), nil
	}
	return nil, nil
}

func (f *fakeTrialRequests) Create(_ context.Context, request *v1alpha1.TrialRequest) error {
	if _, ok := f.requests[request.Name]; ok {
		return ErrTrialRequestExists
	}
	f.created++
	request.CreationTimestamp = metav1.NewTime(time.Now().Truncate(time.Second).Add(time.Duration(f.created) * time.Second))
	request.ResourceVersion = "1"
	f.requests[request.Name] = request.DeepCopy()
	if after := f.afterCreate; after != nil {
		f.afterCreate = nil
		after()
	}
	return nil
}

func (f *fakeTrialRequests) Delete(_ context.Context, id string) error {
	delete(f.requests, id)
	f.deleted = append(f.deleted, id)
	return nil
}

func (f *fakeTrialRequests) Decide(_ context.Context, request *v1alpha1.TrialRequest) error {
	stored, ok := f.requests[request.Name]
	if !ok || f.changeOnDecide || stored.ResourceVersion != request.ResourceVersion {
		return ErrTrialRequestChanged
	}
	stored.Status = request.Status
	stored.ResourceVersion += "1"
	request.ResourceVersion = stored.ResourceVersion
	return nil
}

// fakeMail records the email the console sends, and fails while fail is
// set.
type fakeMail struct {
	sent []sentMail
	fail bool
}

type sentMail struct {
	template email.Template
	to       []string
	data     any
}

func (f *fakeMail) Send(_ context.Context, template email.Template, to []string, data any) error {
	if f.fail {
		return errors.New("the mail server is down")
	}
	f.sent = append(f.sent, sentMail{template, to, data})
	return nil
}

const trialBody = `{"company":"Hooli","role":"Staff engineer","useCase":"Orders between services","throughput":"under-1-mbps","retention":"up-to-7-days"}`

var trialRequestID = regexp.MustCompile(`^tr-[a-km-np-z2-9]{6}$`)

func TestUsersWithoutAClusterRequestTrials(t *testing.T) {
	f := newFixture(t)
	for _, tc := range []struct {
		name    string
		session session
		status  int
		body    string
	}{
		{"no organization", session{user: "user_1"}, 201, `"email":"user_1@example.com"`},
		{"an organization without a cluster", session{user: "user_5", org: "org_new", role: "admin"}, 201, `"phase":"Pending"`},
		{"a customer's member", session{user: "user_2", org: "org_acme", role: "member"}, 409, "Your organization has a cluster"},
		{"an operator", session{user: "user_6", org: staffOrg, role: "member"}, 403, "Not for operators"},
		{"an unverified address", session{user: "user_unverified"}, 403, "Verify your email address"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := send(t, f.handler, http.MethodPost, "/api/trial-requests", f.signer.token(t, tc.session), trialBody)
			if status != tc.status || !strings.Contains(body, tc.body) {
				t.Fatalf("POST /api/trial-requests = %d %s, want %d containing %s", status, body, tc.status, tc.body)
			}
			if status != 201 {
				return
			}
			var created api.TrialRequest
			if err := json.Unmarshal([]byte(body), &created); err != nil {
				t.Fatal(err)
			}
			stored := f.trialRequests.requests[created.Id]
			if !trialRequestID.MatchString(created.Id) || stored == nil || stored.Spec.ClerkUserID != tc.session.user ||
				stored.Labels[v1alpha1.ClerkUserLabel] != tc.session.user || stored.Spec.Company != "Hooli" {
				t.Errorf("the request %s is not the user's: %+v", created.Id, stored)
			}
			if !strings.Contains(f.log.String(), `"msg":"trial requested","audit":true,"tenant":"`+created.Id+`","user":"`+tc.session.user+`"`) {
				t.Errorf("no audit line for the request in %s", f.log.String())
			}
		})
	}
	if status, _ := send(t, f.handler, http.MethodPost, "/api/trial-requests", "", trialBody); status != 401 {
		t.Errorf("an anonymous POST /api/trial-requests = %d, want 401", status)
	}

	// A requester sees their own requests and nobody else's.
	status, body, _ := get(t, f.handler, "/api/trial-requests/mine", f.signer.token(t, session{user: "user_1"}))
	if status != 200 || strings.Count(body, `"id":"tr-`) != 1 || !strings.Contains(body, `"userID":"user_1"`) {
		t.Errorf("GET /api/trial-requests/mine = %d %s, want user_1's one request", status, body)
	}
}

func TestTrialRequestsAreValidated(t *testing.T) {
	f := newFixture(t)
	user := f.signer.token(t, session{user: "user_1"})
	with := func(field, value string) string {
		var body map[string]any
		if err := json.Unmarshal([]byte(trialBody), &body); err != nil {
			t.Fatal(err)
		}
		body[field] = value
		encoded, _ := json.Marshal(body)
		return string(encoded)
	}
	for name, tc := range map[string]struct{ body, field string }{
		"a blank company":       {with("company", "   "), "company"},
		"a long role":           {with("role", strings.Repeat("r", 101)), "role"},
		"a long use case":       {with("useCase", strings.Repeat("u", 2001)), "useCase"},
		"an unknown throughput": {with("throughput", "lots"), "throughput"},
		"an unknown retention":  {with("retention", "forever"), "retention"},
		"a start in the past":   {with("preferredStart", "2020-01-01"), "preferredStart"},
		"a start years ahead":   {with("preferredStart", time.Now().AddDate(2, 0, 0).Format(time.DateOnly)), "preferredStart"},
	} {
		status, body := send(t, f.handler, http.MethodPost, "/api/trial-requests", user, tc.body)
		if status != 400 || !strings.Contains(body, `"field":"`+tc.field+`"`) {
			t.Errorf("%s: POST = %d %s, want 400 on %s", name, status, body, tc.field)
		}
	}
	if len(f.trialRequests.requests) != 1 {
		t.Errorf("refused requests were kept: %v", f.trialRequests.requests)
	}

	// Characters count, not bytes: a company of 100 accented letters fits.
	tomorrow := time.Now().AddDate(0, 0, 1).Format(time.DateOnly)
	body := with("company", strings.Repeat("é", 100))
	body = strings.Replace(body, `"company"`, `"preferredStart":"`+tomorrow+`","company"`, 1)
	status, response := send(t, f.handler, http.MethodPost, "/api/trial-requests", user, body)
	if status != 201 || !strings.Contains(response, `"preferredStart":"`+tomorrow+`"`) {
		t.Errorf("POST with a start tomorrow = %d %s, want 201", status, response)
	}
}

func TestOneTrialRequestAtATime(t *testing.T) {
	f := newFixture(t)
	user := f.signer.token(t, session{user: "user_1"})
	request := func() (int, string) {
		return send(t, f.handler, http.MethodPost, "/api/trial-requests", user, trialBody)
	}
	status, body := request()
	if status != 201 {
		t.Fatalf("the first request = %d %s, want 201", status, body)
	}
	var first api.TrialRequest
	if err := json.Unmarshal([]byte(body), &first); err != nil {
		t.Fatal(err)
	}
	if status, body := request(); status != 409 || !strings.Contains(body, "You have a pending request") || !strings.Contains(body, first.Id) {
		t.Errorf("a second request while one is pending = %d %s, want 409 naming %s", status, body, first.Id)
	}

	decline := func(ago time.Duration) {
		f.trialRequests.requests[first.Id].Status = v1alpha1.TrialRequestStatus{
			Phase: v1alpha1.TrialRequestDeclined, DecidedBy: "user_6", Reason: "Not yet",
			DecidedAt: &metav1.Time{Time: time.Now().Add(-ago)},
		}
	}
	decline(24 * time.Hour)
	if status, body := request(); status != 429 || !strings.Contains(body, "you may ask again from") {
		t.Errorf("a request a day after a decline = %d %s, want 429", status, body)
	}
	decline(8 * 24 * time.Hour)
	status, body = request()
	if status != 201 {
		t.Fatalf("a request 8 days after a decline = %d %s, want 201", status, body)
	}
	var second api.TrialRequest
	if err := json.Unmarshal([]byte(body), &second); err != nil {
		t.Fatal(err)
	}

	f.trialRequests.requests[second.Id].Status = v1alpha1.TrialRequestStatus{
		Phase: v1alpha1.TrialRequestApproved, DecidedBy: "user_6", DecidedAt: &metav1.Time{Time: time.Now()}, Customer: "c-abcdef",
	}
	if status, body := request(); status != 409 || !strings.Contains(body, "You had a trial") {
		t.Errorf("a request after a trial = %d %s, want 409", status, body)
	}
}

func TestTwoTrialRequestsAtOnceLeaveOnePending(t *testing.T) {
	f := newFixture(t)
	// Another request of the same user, made a moment earlier, passed the
	// checks too.
	f.trialRequests.afterCreate = func() {
		f.trialRequests.add("tr-earz22", "user_1", "Hooli", time.Now().Add(-time.Minute))
	}
	status, body := send(t, f.handler, http.MethodPost, "/api/trial-requests", f.signer.token(t, session{user: "user_1"}), trialBody)
	if status != 409 || !strings.Contains(body, "tr-earz22") {
		t.Fatalf("the later of two requests at once = %d %s, want 409 naming tr-earz22", status, body)
	}
	mine, _ := f.trialRequests.ByUser(t.Context(), "user_1")
	if len(mine) != 1 || mine[0].Name != "tr-earz22" || len(f.trialRequests.deleted) != 1 {
		t.Errorf("user_1 has %v and deleted %v, want only tr-earz22 left", mine, f.trialRequests.deleted)
	}
}

func TestOperatorsApproveTrialRequests(t *testing.T) {
	f := newFixture(t)
	staff := f.signer.token(t, session{user: "user_6", org: staffOrg, role: "member"})
	approve := func(id, body string) (int, string) {
		return send(t, f.handler, http.MethodPost, "/api/operator/trial-requests/"+id+"/approve", staff, body)
	}
	acmeAdmin := f.signer.token(t, session{user: "user_3", org: "org_acme", role: "admin"})
	if status, _ := send(t, f.handler, http.MethodPost, "/api/operator/trial-requests/tr-pend22/approve", acmeAdmin, `{}`); status != 403 {
		t.Errorf("a customer's admin approving = %d, want 403", status)
	}

	status, body := approve("tr-pend22", `{}`)
	if status != 200 {
		t.Fatalf("approving = %d %s, want 200", status, body)
	}
	var approved api.TrialRequest
	if err := json.Unmarshal([]byte(body), &approved); err != nil {
		t.Fatal(err)
	}
	if approved.Phase != api.TrialRequestPhaseApproved || approved.CustomerID == nil || approved.DecidedAt == nil {
		t.Fatalf("the approved request = %+v, want approved with its customer", approved)
	}
	customer, _ := f.customers.Get(t.Context(), *approved.CustomerID)
	if customer == nil || customer.Spec.DisplayName != "Initech" || customer.Spec.Profile != "dev" || customer.Spec.DeletionProtection ||
		customer.Spec.ClerkOrganizationID != "org_new1" || customer.Labels[v1alpha1.ClerkOrganizationLabel] != "org_new1" {
		t.Errorf("the trial's customer = %+v, want Initech on dev without deletion protection in org_new1", customer)
	}
	if customer.Spec.Trial == nil || time.Until(customer.Spec.Trial.EndsAt.Time) < trialLength-time.Minute || time.Until(customer.Spec.Trial.EndsAt.Time) > trialLength {
		t.Errorf("the trial's customer has trial %+v, want one that ends in 14 days", customer.Spec.Trial)
	}
	if f.organizations.names["org_new1"] != "Initech" || strings.Join(f.organizations.admins, ",") != "org_new1/user_8" || len(f.organizations.invited) != 0 {
		t.Errorf("organizations %v, admins %v, invited %v; want Initech with user_8 as its admin, uninvited", f.organizations.names, f.organizations.admins, f.organizations.invited)
	}
	if ptr.Deref(approved.OrganizationID, "") != "org_new1" {
		t.Errorf("the approved request names organization %v, want org_new1, so the requester can switch to it", approved.OrganizationID)
	}
	if stored := f.trialRequests.requests["tr-pend22"]; stored.Status.DecidedBy != "user_6" || stored.Status.Customer != customer.Name ||
		stored.Status.OrganizationID != "org_new1" {
		t.Errorf("the stored decision = %+v", stored.Status)
	}
	if !strings.Contains(f.log.String(), `"msg":"trial request approved","audit":true,"tenant":"`+customer.Name+`","user":"user_6"`) {
		t.Errorf("no audit line for the approval in %s", f.log.String())
	}
	_, mine, _ := get(t, f.handler, "/api/trial-requests/mine", f.signer.token(t, session{user: "user_8"}))
	if !strings.Contains(mine, `"phase":"Approved"`) || !strings.Contains(mine, `"customerID":"`+customer.Name+`"`) {
		t.Errorf("the requester sees %s, want the approval", mine)
	}

	if status, body := approve("tr-pend22", `{}`); status != 409 || !strings.Contains(body, "was approved already") {
		t.Errorf("approving again = %d %s, want 409", status, body)
	}
	if status, _ := approve("tr-nada22", `{}`); status != 404 {
		t.Errorf("approving an unknown request = %d, want 404", status)
	}
	f.trialRequests.add("tr-big222", "user_9", "Hooli", time.Now())
	if status, body := approve("tr-big222", `{"profile":"huge"}`); status != 400 || !strings.Contains(body, `"field":"profile"`) {
		t.Errorf("approving on an unknown profile = %d %s, want 400", status, body)
	}
	if status, body := approve("tr-big222", `{"profile":"production"}`); status != 200 || !strings.Contains(body, `"customerID":"c-`) {
		t.Fatalf("approving on production = %d %s, want 200", status, body)
	}
	if customer, _ := f.customers.Get(t.Context(), f.trialRequests.requests["tr-big222"].Status.Customer); customer.Spec.Profile != "production" {
		t.Errorf("the customer's profile = %s, want production", customer.Spec.Profile)
	}
}

func TestApprovalUndoesWhatItDid(t *testing.T) {
	f := newFixture(t)
	staff := f.signer.token(t, session{user: "user_6", org: staffOrg, role: "member"})

	// The requester is gone from Clerk.
	f.trialRequests.add("tr-away22", "user_gone", "Vandelay", time.Now())
	if status, _ := send(t, f.handler, http.MethodPost, "/api/operator/trial-requests/tr-away22/approve", staff, `{}`); status != 500 {
		t.Errorf("approving a vanished user = %d, want 500", status)
	}
	if len(f.organizations.created) != 1 || len(f.organizations.deleted) != 1 || len(f.customers.customers) != 2 ||
		f.trialRequests.requests["tr-away22"].PhaseOrPending() != v1alpha1.TrialRequestPending {
		t.Errorf("a failed approval left organizations %v deleted %v, customers %d, phase %s",
			f.organizations.created, f.organizations.deleted, len(f.customers.customers), f.trialRequests.requests["tr-away22"].Status.Phase)
	}

	// Another operator decided first.
	f.trialRequests.changeOnDecide = true
	status, body := send(t, f.handler, http.MethodPost, "/api/operator/trial-requests/tr-pend22/approve", staff, `{}`)
	if status != 409 || !strings.Contains(body, "Decided meanwhile") {
		t.Errorf("approving what another operator decided = %d %s, want 409", status, body)
	}
	if len(f.organizations.created) != 2 || len(f.organizations.deleted) != 2 || len(f.customers.deleted) != 1 || len(f.customers.customers) != 2 {
		t.Errorf("the overtaken approval left organizations %v deleted %v, customers deleted %v",
			f.organizations.created, f.organizations.deleted, f.customers.deleted)
	}
}

func TestOperatorsDeclineTrialRequests(t *testing.T) {
	f := newFixture(t)
	staff := f.signer.token(t, session{user: "user_6", org: staffOrg, role: "member"})
	decline := func(id, body string) (int, string) {
		return send(t, f.handler, http.MethodPost, "/api/operator/trial-requests/"+id+"/decline", staff, body)
	}
	for _, reason := range []string{`""`, `"   "`, `"` + strings.Repeat("r", 1001) + `"`} {
		if status, body := decline("tr-pend22", `{"reason":`+reason+`}`); status != 400 || !strings.Contains(body, `"field":"reason"`) {
			t.Errorf("declining with the reason %.20s = %d %s, want 400", reason, status, body)
		}
	}
	member := f.signer.token(t, session{user: "user_2", org: "org_acme", role: "member"})
	if status, _ := send(t, f.handler, http.MethodPost, "/api/operator/trial-requests/tr-pend22/decline", member, `{"reason":"No"}`); status != 403 {
		t.Errorf("a customer's member declining = %d, want 403", status)
	}

	status, body := decline("tr-pend22", `{"reason":"We start with teams in the EU."}`)
	if status != 200 || !strings.Contains(body, `"phase":"Declined"`) || !strings.Contains(body, `"reason":"We start with teams in the EU."`) {
		t.Fatalf("declining = %d %s, want 200 with the reason", status, body)
	}
	if !strings.Contains(f.log.String(), `"msg":"trial request declined","audit":true,"tenant":"tr-pend22","user":"user_6"`) {
		t.Errorf("no audit line for the decline in %s", f.log.String())
	}
	_, mine, _ := get(t, f.handler, "/api/trial-requests/mine", f.signer.token(t, session{user: "user_8"}))
	if !strings.Contains(mine, `"reason":"We start with teams in the EU."`) {
		t.Errorf("the requester sees %s, want the reason", mine)
	}
	if status, body := decline("tr-pend22", `{"reason":"Again"}`); status != 409 || !strings.Contains(body, "was declined already") {
		t.Errorf("declining again = %d %s, want 409", status, body)
	}
	if status, _ := decline("tr-nada22", `{"reason":"No"}`); status != 404 {
		t.Errorf("declining an unknown request = %d, want 404", status)
	}
	f.trialRequests.add("tr-past22", "user_9", "Hooli", time.Now())
	f.trialRequests.changeOnDecide = true
	if status, body := decline("tr-past22", `{"reason":"No"}`); status != 409 || !strings.Contains(body, "Decided meanwhile") {
		t.Errorf("declining what another operator decided = %d %s, want 409", status, body)
	}
	if len(f.organizations.created) != 0 || len(f.customers.customers) != 2 {
		t.Errorf("declines provisioned something: organizations %v", f.organizations.created)
	}
}

func TestOperatorsExtendAndConvertTrials(t *testing.T) {
	f := newFixture(t)
	staff := f.signer.token(t, session{user: "user_6", org: staffOrg, role: "member"})
	end := time.Now().Add(5 * 24 * time.Hour).Truncate(time.Second)
	acme, _ := f.customers.Get(t.Context(), "acme")
	acme.Spec.Trial = &v1alpha1.CustomerTrial{EndsAt: metav1.NewTime(end)}
	change := func(id, body string) (int, string) {
		return send(t, f.handler, http.MethodPatch, "/api/operator/customers/"+id+"/trial", staff, body)
	}
	at := func(when time.Time) string {
		return `{"action":"extend","endsAt":"` + when.UTC().Format(time.RFC3339) + `"}`
	}

	admin := f.signer.token(t, session{user: "user_3", org: "org_acme", role: "admin"})
	if status, _ := send(t, f.handler, http.MethodPatch, "/api/operator/customers/acme/trial", admin, `{"action":"convert"}`); status != 403 {
		t.Errorf("a customer's admin changing its trial = %d, want 403", status)
	}
	for name, body := range map[string]string{
		"no end":          `{"action":"extend"}`,
		"an end too soon": at(time.Now().Add(10 * time.Minute)),
		"an end too late": at(time.Now().Add(100 * 24 * time.Hour)),
	} {
		if status, response := change("acme", body); status != 400 || !strings.Contains(response, `"field":"endsAt"`) {
			t.Errorf("extending with %s = %d %s, want 400 on endsAt", name, status, response)
		}
	}
	if status, response := change("acme", `{"action":"shorten"}`); status != 400 || !strings.Contains(response, `"field":"action"`) {
		t.Errorf("an unknown action = %d %s, want 400 on action", status, response)
	}
	if status, _ := change("nobody", `{"action":"convert"}`); status != 404 {
		t.Errorf("changing the trial of an unknown customer = %d, want 404", status)
	}
	if status, response := change("globex", `{"action":"convert"}`); status != 409 || !strings.Contains(response, "Not on a trial") {
		t.Errorf("converting a customer not on a trial = %d %s, want 409", status, response)
	}

	extended := time.Now().Add(20 * 24 * time.Hour).Truncate(time.Second)
	status, body := change("acme", at(extended))
	if status != 200 || !strings.Contains(body, `"trial":{"endsAt":"`+extended.UTC().Format(time.RFC3339)+`"}`) {
		t.Fatalf("extending = %d %s, want 200 with the new end", status, body)
	}
	if acme, _ := f.customers.Get(t.Context(), "acme"); !acme.Spec.Trial.EndsAt.Time.Equal(extended) {
		t.Errorf("the stored trial ends at %s, want %s", acme.Spec.Trial.EndsAt, extended)
	}
	if !strings.Contains(f.log.String(), `"msg":"trial extended","audit":true,"tenant":"acme","user":"user_6"`) {
		t.Errorf("no audit line for the extension in %s", f.log.String())
	}

	f.customers.changed = true
	if status, response := change("acme", `{"action":"convert"}`); status != 409 || !strings.Contains(response, "Changed meanwhile") {
		t.Errorf("converting a customer that changed meanwhile = %d %s, want 409", status, response)
	}
	f.customers.changed = false
	status, body = change("acme", `{"action":"convert"}`)
	if status != 200 || strings.Contains(body, `"trial"`) {
		t.Fatalf("converting = %d %s, want 200 without a trial", status, body)
	}
	if acme, _ := f.customers.Get(t.Context(), "acme"); acme.Spec.Trial != nil {
		t.Errorf("the converted customer still has trial %+v", acme.Spec.Trial)
	}
	if !strings.Contains(f.log.String(), `"msg":"trial converted","audit":true,"tenant":"acme","user":"user_6"`) {
		t.Errorf("no audit line for the conversion in %s", f.log.String())
	}

	// A customer being deprovisioned keeps its trial as it is.
	acme, _ = f.customers.Get(t.Context(), "acme")
	acme.Spec.Trial = &v1alpha1.CustomerTrial{EndsAt: metav1.NewTime(end)}
	acme.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	if status, response := change("acme", `{"action":"convert"}`); status != 409 || !strings.Contains(response, "Being deleted") {
		t.Errorf("converting a customer being deleted = %d %s, want 409", status, response)
	}
}

func TestTrialsEmailOperatorsAndRequesters(t *testing.T) {
	f := newFixture(t)
	user := f.signer.token(t, session{user: "user_1"})
	staff := f.signer.token(t, session{user: "user_6", org: staffOrg, role: "member"})

	// A new request goes to the operators.
	status, body := send(t, f.handler, http.MethodPost, "/api/trial-requests", user, trialBody)
	if status != 201 {
		t.Fatalf("POST = %d %s", status, body)
	}
	var requested api.TrialRequest
	if err := json.Unmarshal([]byte(body), &requested); err != nil {
		t.Fatal(err)
	}
	if len(f.mail.sent) != 1 || f.mail.sent[0].template != email.TrialRequested || strings.Join(f.mail.sent[0].to, ",") != "ops@changeport.test" {
		t.Fatalf("sent %+v, want the request to the operators", f.mail.sent)
	}
	if got := f.mail.sent[0].data.(email.TrialRequest); got.ID != requested.Id || got.Company != "Hooli" || got.Email != "user_1@example.com" ||
		got.Throughput != "Under 1 MB/s" || got.Retention != "Up to a week" || got.Start != "As soon as possible" || got.Link != origin+"/operator/trials" {
		t.Errorf("the operators' email says %+v", got)
	}

	// An approval goes to the requester, with the console's link.
	if status, body := send(t, f.handler, http.MethodPost, "/api/operator/trial-requests/"+requested.Id+"/approve", staff, `{}`); status != 200 {
		t.Fatalf("approve = %d %s", status, body)
	}
	approved := f.mail.sent[1]
	if approved.template != email.TrialApproved || approved.to[0] != "user_1@example.com" {
		t.Fatalf("sent %+v, want the approval to the requester", approved)
	}
	if got := approved.data.(email.TrialDecision); got.Company != "Hooli" || got.Link != origin+"/" || got.EndsAt == "" {
		t.Errorf("the approval says %+v", got)
	}
	// The trial's emails go to the requester.
	var customer *v1alpha1.Customer
	for i := range f.customers.customers {
		if f.customers.customers[i].Spec.Trial != nil {
			customer = &f.customers.customers[i]
		}
	}
	if customer == nil || customer.Spec.Trial.Contact != "user_1@example.com" {
		t.Fatalf("the trial's customer = %+v, want its contact the requester", customer)
	}
	// Extending keeps the contact.
	extended := time.Now().Add(20 * 24 * time.Hour).UTC().Format(time.RFC3339)
	if status, body := send(t, f.handler, http.MethodPatch, "/api/operator/customers/"+customer.Name+"/trial", staff,
		`{"action":"extend","endsAt":"`+extended+`"}`); status != 200 {
		t.Fatalf("extend = %d %s", status, body)
	}
	if contact := customer.Spec.Trial.Contact; contact != "user_1@example.com" {
		t.Errorf("after the extension, the contact is %q", contact)
	}

	// A decline goes to the requester, with the reason and when to ask again;
	// the mail server being down fails nothing.
	f.trialRequests.add("tr-mail22", "user_9", "Initech", time.Now())
	f.mail.fail = true
	if status, body := send(t, f.handler, http.MethodPost, "/api/operator/trial-requests/tr-mail22/decline", staff, `{"reason":"Not yet"}`); status != 200 {
		t.Fatalf("declining while mail is down = %d %s, want 200", status, body)
	}
	f.mail.fail = false
	f.trialRequests.add("tr-mayy22", "user_9", "Initech", time.Now())
	if status, _ := send(t, f.handler, http.MethodPost, "/api/operator/trial-requests/tr-mayy22/decline", staff, `{"reason":"Not yet"}`); status != 200 {
		t.Fatal("decline failed")
	}
	declined := f.mail.sent[len(f.mail.sent)-1]
	if got, ok := declined.data.(email.TrialDecision); declined.template != email.TrialDeclined || declined.to[0] != "user_9@example.com" ||
		!ok || got.Reason != "Not yet" || got.AskAgain != time.Now().Add(declineWait).UTC().Format("2 January 2006") {
		t.Errorf("sent %+v, want the decline with its reason", declined)
	}
}
