package console

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/ReneP92/changeport/api/v1alpha1"
	"github.com/ReneP92/changeport/internal/console/api"
)

// getIn requests path in an environment, as the web app does.
func getIn(t *testing.T, handler http.Handler, path, token, environment string) (int, string) {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, http.NoBody)
	request.Header.Set("Authorization", "Bearer "+token)
	if environment != "" {
		request.Header.Set(EnvironmentHeader, environment)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder.Code, recorder.Body.String()
}

// addStaging gives acme's organization a staging environment.
func addStaging(f *fixture) {
	staging := f.customers.customers[1]
	staging.Name = "acme-staging"
	staging.Spec.Environment = "staging"
	staging.Status.BootstrapAddress = "bootstrap.acme-staging.kafka.changeport.test:9094"
	f.customers.customers = append(f.customers.customers, staging)
}

func TestCustomerRoutesActOnTheRequestedEnvironment(t *testing.T) {
	f := newFixture(t)
	addStaging(f)
	member := f.signer.token(t, session{user: "user_2", org: "org_acme", role: "member"})
	for environment, want := range map[string]string{
		"":           `"id":"acme"`,
		"production": `"id":"acme"`,
		"staging":    `"id":"acme-staging"`,
	} {
		status, body := getIn(t, f.handler, "/api/customer", member, environment)
		if status != 200 || !strings.Contains(body, want) {
			t.Errorf("GET /api/customer in %q = %d %s, want %s", environment, status, body, want)
		}
	}
	if status, body := getIn(t, f.handler, "/api/customer/connection", member, "staging"); status != 200 || !strings.Contains(body, "acme-staging.kafka") {
		t.Errorf("staging's connection = %d %s", status, body)
	}
	if status, body := getIn(t, f.handler, "/api/customer/topics", member, "qa"); status != 404 || !strings.Contains(body, "No such environment") {
		t.Errorf("an unknown environment = %d %s, want 404", status, body)
	}
	// Globex has no staging, whatever it asks for.
	globex := f.signer.token(t, session{user: "user_4", org: "org_globex", role: "member"})
	if status, _ := getIn(t, f.handler, "/api/customer", globex, "staging"); status != 404 {
		t.Errorf("globex asking for staging = %d, want 404", status)
	}

	_, body, _ := get(t, f.handler, "/api/me", member)
	var me api.Me
	if err := json.Unmarshal([]byte(body), &me); err != nil {
		t.Fatal(err)
	}
	if me.Environments == nil || len(*me.Environments) != 2 || (*me.Environments)[0].Name != "production" ||
		(*me.Environments)[1].Name != "staging" || (*me.Environments)[1].CustomerID != "acme-staging" || me.CustomerID == nil || *me.CustomerID != "acme" {
		t.Errorf("GET /api/me = %s, want production then staging", body)
	}
}

func TestOperatorsAddEnvironments(t *testing.T) {
	f := newFixture(t)
	staff := f.signer.token(t, session{user: "user_6", org: staffOrg, role: "member"})
	add := func(token, customer, body string) (int, string) {
		return send(t, f.handler, http.MethodPost, "/api/operator/customers/"+customer+"/environments", token, body)
	}
	admin := f.signer.token(t, session{user: "user_3", org: "org_acme", role: "admin"})
	if status, _ := add(admin, "acme", `{"environment":"staging","profile":"dev","deletionProtection":false}`); status != 403 {
		t.Errorf("a customer's admin adding an environment = %d, want 403", status)
	}
	for body, want := range map[string]string{
		`{"environment":"Staging EU","profile":"dev","deletionProtection":false}`: `"field":"environment"`,
		`{"environment":"staging","profile":"huge","deletionProtection":false}`:   `"field":"profile"`,
	} {
		if status, got := add(staff, "acme", body); status != 400 || !strings.Contains(got, want) {
			t.Errorf("adding %s = %d %s, want 400 %s", body, status, got, want)
		}
	}
	if status, body := add(staff, "acme", `{"environment":"production","profile":"dev","deletionProtection":false}`); status != 409 || !strings.Contains(body, "has an environment production already") {
		t.Errorf("adding production again = %d %s, want 409", status, body)
	}
	if status, _ := add(staff, "initech", `{"environment":"staging","profile":"dev","deletionProtection":false}`); status != 404 {
		t.Errorf("adding to an unknown customer = %d, want 404", status)
	}

	status, body := add(staff, "acme", `{"environment":"staging","profile":"dev-ha","deletionProtection":true}`)
	if status != 201 || !strings.Contains(body, `"environment":"staging"`) {
		t.Fatalf("adding staging = %d %s, want 201", status, body)
	}
	var created api.Customer
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatal(err)
	}
	customer, _ := f.customers.Get(t.Context(), created.Id)
	if customer == nil || customer.Spec.ClerkOrganizationID != "org_acme" || customer.Spec.Environment != "staging" ||
		customer.Spec.Profile != "dev-ha" || !customer.Spec.DeletionProtection || customer.Spec.DisplayName != "Acme" {
		t.Errorf("the new environment's customer = %+v, want staging of Acme in org_acme", customer)
	}
	if len(f.organizations.created) != 0 || len(f.organizations.invited) != 0 {
		t.Errorf("created organizations %v and invited %v, want neither", f.organizations.created, f.organizations.invited)
	}
	if !strings.Contains(f.log.String(), `"msg":"environment added","audit":true,"tenant":"`+created.Id+`","user":"user_6"`) {
		t.Errorf("no audit line for the new environment in %s", f.log.String())
	}

	// A trial is one environment.
	globex, _ := f.customers.Get(t.Context(), "globex")
	globex.Spec.Trial = &v1alpha1.CustomerTrial{EndsAt: metav1.NewTime(time.Now().Add(7 * 24 * time.Hour))}
	if status, body := add(staff, "globex", `{"environment":"staging","profile":"dev","deletionProtection":false}`); status != 409 || !strings.Contains(body, "A trial is one environment") {
		t.Errorf("adding to a trial = %d %s, want 409", status, body)
	}
}

func TestProvisioningNamesTheFirstEnvironment(t *testing.T) {
	f := newFixture(t)
	staff := f.signer.token(t, session{user: "user_6", org: staffOrg, role: "member"})
	provision := func(body string) (int, string) {
		return send(t, f.handler, http.MethodPost, "/api/operator/customers", staff, body)
	}
	status, body := provision(`{"displayName":"Initech","profile":"dev","deletionProtection":false}`)
	if status != 201 || !strings.Contains(body, `"environment":"production"`) {
		t.Errorf("provisioning without an environment = %d %s, want production", status, body)
	}
	status, body = provision(`{"displayName":"Hooli","profile":"dev","deletionProtection":false,"environment":"dev"}`)
	if status != 201 || !strings.Contains(body, `"environment":"dev"`) {
		t.Errorf("provisioning dev = %d %s", status, body)
	}
	if status, body := provision(`{"displayName":"Hooli","profile":"dev","deletionProtection":false,"environment":"-dev"}`); status != 400 || !strings.Contains(body, `"field":"environment"`) {
		t.Errorf("provisioning an invalid environment = %d %s, want 400", status, body)
	}
}

// sendIn sends a request in an environment, as the web app does.
func sendIn(t *testing.T, handler http.Handler, method, path, token, environment, body string) (int, string) {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(EnvironmentHeader, environment)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder.Code, recorder.Body.String()
}

func decodeEnvironments(t *testing.T, body string) api.Environments {
	t.Helper()
	var environments api.Environments
	if err := json.Unmarshal([]byte(body), &environments); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	return environments
}

// names lists the environments' names, in order.
func names(environments api.Environments) []string {
	var names []string
	for _, e := range environments.Environments {
		names = append(names, e.Name)
	}
	return names
}

func TestAdminsManageEnvironments(t *testing.T) {
	f := newFixture(t)
	admin := f.signer.token(t, session{user: "user_3", org: "org_acme", role: "admin"})
	member := f.signer.token(t, session{user: "user_2", org: "org_acme", role: "member"})
	const path = "/api/customer/environments"

	// Members see the environments, whatever environment they are in.
	status, body := sendIn(t, f.handler, http.MethodGet, path, member, "qa", "")
	list := decodeEnvironments(t, body)
	if status != 200 || len(list.Environments) != 1 || !list.Environments[0].Default || list.Environments[0].Name != "production" ||
		!list.Environments[0].Ready || list.Limit != DefaultMaxEnvironments {
		t.Errorf("a member's GET %s = %d %s", path, status, body)
	}
	for _, request := range []struct{ method, path, body string }{
		{http.MethodPost, path, `{"environment":"staging","profile":"dev","deletionProtection":false}`},
		{http.MethodPatch, path + "/acme", `{"name":"live"}`},
		{http.MethodDelete, path + "/acme", ""},
	} {
		if status, _ := send(t, f.handler, request.method, request.path, member, request.body); status != 403 {
			t.Errorf("a member's %s %s = %d, want 403", request.method, request.path, status)
		}
	}

	// Adding.
	for body, want := range map[string]string{
		`{"environment":"Staging EU","profile":"dev","deletionProtection":false}`: `"field":"environment"`,
		`{"environment":"staging","profile":"huge","deletionProtection":false}`:   `"field":"profile"`,
	} {
		if status, got := send(t, f.handler, http.MethodPost, path, admin, body); status != 400 || !strings.Contains(got, want) {
			t.Errorf("adding %s = %d %s, want 400 %s", body, status, got, want)
		}
	}
	if status, body := send(t, f.handler, http.MethodPost, path, admin, `{"environment":"production","profile":"dev","deletionProtection":false}`); status != 409 || !strings.Contains(body, "has an environment production already") {
		t.Errorf("adding production again = %d %s, want 409", status, body)
	}
	status, body = send(t, f.handler, http.MethodPost, path, admin, `{"environment":"staging","profile":"dev-ha","deletionProtection":false}`)
	list = decodeEnvironments(t, body)
	if status != 201 || len(list.Environments) != 2 || list.Environments[1].Name != "staging" || list.Environments[1].Ready || list.Environments[1].Profile != api.EnvironmentProfileDevHa {
		t.Fatalf("adding staging = %d %s", status, body)
	}
	staging := list.Environments[1].CustomerID
	if customer, _ := f.customers.Get(t.Context(), staging); customer == nil || customer.Spec.ClerkOrganizationID != "org_acme" || customer.Spec.DisplayName != "Acme" {
		t.Errorf("staging's customer = %+v, want Acme's", customer)
	}
	if !strings.Contains(f.log.String(), `"msg":"environment added","audit":true,"tenant":"`+staging+`","user":"user_3"`) {
		t.Errorf("no audit line for the new environment in %s", f.log.String())
	}

	// Renaming, in an environment of any name.
	change := func(customerID, body string) (int, string) {
		return sendIn(t, f.handler, http.MethodPatch, path+"/"+customerID, admin, "staging", body)
	}
	for body, want := range map[string]string{
		`{"name":"QA"}`:         `"field":"name"`,
		`{}`:                    `"field":"name"`,
		`{"name":"production"}`: "has an environment production already",
	} {
		if status, got := change(staging, body); status/100 != 4 || !strings.Contains(got, want) {
			t.Errorf("changing staging with %s = %d %s, want %s", body, status, got, want)
		}
	}
	if status, _ := change("globex", `{"name":"qa"}`); status != 404 {
		t.Errorf("renaming another organization's environment = %d, want 404", status)
	}
	status, body = change(staging, `{"name":"qa"}`)
	if list := decodeEnvironments(t, body); status != 200 || !slices.Equal(names(list), []string{"production", "qa"}) {
		t.Errorf("renaming staging to qa = %d %s", status, body)
	}
	if customer, _ := f.customers.Get(t.Context(), staging); customer.Spec.Environment != "qa" {
		t.Errorf("the renamed customer's environment = %q, want qa", customer.Spec.Environment)
	}

	// The default, one at a time.
	status, body = change(staging, `{"default":true}`)
	if list := decodeEnvironments(t, body); status != 200 || !slices.Equal(names(list), []string{"qa", "production"}) || !list.Environments[0].Default || list.Environments[1].Default {
		t.Errorf("making qa the default = %d %s", status, body)
	}
	_, body, _ = get(t, f.handler, "/api/me", member)
	if !strings.Contains(body, `"customerID":"`+staging+`","default":true`) {
		t.Errorf("GET /api/me = %s, want qa first, the default", body)
	}
	status, body = change("acme", `{"default":true}`)
	if list := decodeEnvironments(t, body); status != 200 || !slices.Equal(names(list), []string{"production", "qa"}) {
		t.Errorf("making production the default again = %d %s", status, body)
	}
	if qa, _ := f.customers.Get(t.Context(), staging); qa.Spec.DefaultEnvironment {
		t.Error("qa is still the default, want only production")
	}

	// Protection: on at once, off only with the environment's name.
	if status, body := change(staging, `{"deletionProtection":true}`); status != 200 || !decodeEnvironments(t, body).Environments[1].DeletionProtection {
		t.Errorf("protecting qa = %d %s", status, body)
	}
	remove := func(customerID string) (int, string) {
		return send(t, f.handler, http.MethodDelete, path+"/"+customerID, admin, "")
	}
	if status, body := remove(staging); status != 409 || !strings.Contains(body, "Turn off the deletion protection of qa first") {
		t.Errorf("removing protected qa = %d %s, want 409", status, body)
	}
	for _, body := range []string{`{"deletionProtection":false}`, `{"deletionProtection":false,"confirm":"production"}`} {
		if status, got := change(staging, body); status != 400 || !strings.Contains(got, `"field":"confirm"`) {
			t.Errorf("unprotecting qa with %s = %d %s, want 400", body, status, got)
		}
	}
	if status, body := change(staging, `{"deletionProtection":false,"confirm":"qa"}`); status != 200 || decodeEnvironments(t, body).Environments[1].DeletionProtection {
		t.Errorf("unprotecting qa = %d %s", status, body)
	}
	for _, audit := range []string{"environment renamed", "default environment set", "deletion protection turned on", "deletion protection turned off"} {
		if !strings.Contains(f.log.String(), `"msg":"`+audit+`","audit":true`) {
			t.Errorf("no audit line %q in %s", audit, f.log.String())
		}
	}

	// Removing, but never the last.
	status, body = remove(staging)
	if list := decodeEnvironments(t, body); status != 200 || len(list.Environments) != 2 || !list.Environments[1].Deleting || list.Environments[0].Name != "production" {
		t.Errorf("removing qa = %d %s", status, body)
	}
	if !slices.Contains(f.customers.deleted, staging) || !strings.Contains(f.log.String(), `"msg":"environment removed","audit":true,"tenant":"`+staging+`"`) {
		t.Errorf("deleted %v; want qa deleted, and audited", f.customers.deleted)
	}
	if status, body := remove("acme"); status != 409 || !strings.Contains(body, "keeps at least one environment") {
		t.Errorf("removing the last environment = %d %s, want 409", status, body)
	}
}

func TestEnvironmentsWhileBeingRemoved(t *testing.T) {
	f := newFixture(t)
	addStaging(f)
	admin := f.signer.token(t, session{user: "user_3", org: "org_acme", role: "admin"})
	staging, _ := f.customers.Get(t.Context(), "acme-staging")
	staging.DeletionTimestamp = ptr.To(metav1.Now())
	staging.Spec.DefaultEnvironment = true

	// It is listed last, and not to switch to: the default is another.
	_, body, _ := get(t, f.handler, "/api/me", admin)
	if strings.Contains(body, "acme-staging") || !strings.Contains(body, `"customerID":"acme"`) {
		t.Errorf("GET /api/me = %s, want production alone", body)
	}
	status, body := send(t, f.handler, http.MethodGet, "/api/customer/environments", admin, "")
	if list := decodeEnvironments(t, body); status != 200 || !slices.Equal(names(list), []string{"production", "staging"}) || !list.Environments[1].Deleting || !list.Environments[0].Default {
		t.Errorf("the environments = %d %s, want staging last, being removed", status, body)
	}
	for _, request := range []struct{ method, body string }{{http.MethodPatch, `{"name":"qa"}`}, {http.MethodDelete, ""}} {
		if status, body := send(t, f.handler, request.method, "/api/customer/environments/acme-staging", admin, request.body); status != 409 || !strings.Contains(body, "is being removed") {
			t.Errorf("%s staging = %d %s, want 409", request.method, status, body)
		}
	}
	// Its name is not free until it is gone, and production is the last.
	if status, body := send(t, f.handler, http.MethodPost, "/api/customer/environments", admin, `{"environment":"staging","profile":"dev","deletionProtection":false}`); status != 409 || !strings.Contains(body, "its name is free once it is gone") {
		t.Errorf("adding staging again = %d %s, want 409", status, body)
	}
	if status, _ := send(t, f.handler, http.MethodDelete, "/api/customer/environments/acme", admin, ""); status != 409 {
		t.Errorf("removing production, the last left = %d, want 409", status)
	}
}

func TestAddingEnvironmentsHasLimits(t *testing.T) {
	f := newFixture(t)
	add := func(token string) (int, string) {
		return send(t, f.handler, http.MethodPost, "/api/customer/environments", token, `{"environment":"qa","profile":"dev","deletionProtection":false}`)
	}
	// An organization without a cluster gets its first through a trial.
	initech := f.signer.token(t, session{user: "user_9", org: "org_initech", role: "admin"})
	if status, body := send(t, f.handler, http.MethodGet, "/api/customer/environments", initech, ""); status != 200 || len(decodeEnvironments(t, body).Environments) != 0 {
		t.Errorf("an organization without a cluster lists %d %s, want none", status, body)
	}
	if status, body := add(initech); status != 409 || !strings.Contains(body, "first cluster with a trial") {
		t.Errorf("adding without a cluster = %d %s, want 409", status, body)
	}

	// A trial is one environment, and unprotected.
	globex, _ := f.customers.Get(t.Context(), "globex")
	globex.Spec.Trial = &v1alpha1.CustomerTrial{EndsAt: metav1.NewTime(time.Now().Add(7 * 24 * time.Hour))}
	globexAdmin := f.signer.token(t, session{user: "user_5", org: "org_globex", role: "admin"})
	if status, body := add(globexAdmin); status != 409 || !strings.Contains(body, "A trial is one environment") {
		t.Errorf("adding to a trial = %d %s, want 409", status, body)
	}
	if status, body := send(t, f.handler, http.MethodPatch, "/api/customer/environments/globex", globexAdmin, `{"deletionProtection":true}`); status != 409 || !strings.Contains(body, "cannot be protected") {
		t.Errorf("protecting a trial = %d %s, want 409", status, body)
	}

	// At the limit, admins add no more; operators still may.
	acme := f.signer.token(t, session{user: "user_3", org: "org_acme", role: "admin"})
	for _, name := range []string{"dev", "staging", "test", "demo"} {
		if status, body := send(t, f.handler, http.MethodPost, "/api/customer/environments", acme, `{"environment":"`+name+`","profile":"dev","deletionProtection":false}`); status != 201 {
			t.Fatalf("adding %s = %d %s", name, status, body)
		}
	}
	if status, body := add(acme); status != 409 || !strings.Contains(body, "has 5 environments, the most it may have") {
		t.Errorf("adding a sixth = %d %s, want 409", status, body)
	}
	staff := f.signer.token(t, session{user: "user_6", org: staffOrg, role: "member"})
	if status, body := send(t, f.handler, http.MethodPost, "/api/operator/customers/acme/environments", staff, `{"environment":"qa","profile":"dev","deletionProtection":false}`); status != 201 {
		t.Errorf("an operator adding a sixth = %d %s, want 201", status, body)
	}
}

func TestTeamRoutesIgnoreTheEnvironment(t *testing.T) {
	f := newFixture(t)
	member := f.signer.token(t, session{user: "user_2", org: "org_acme", role: "member"})
	if status, body := getIn(t, f.handler, "/api/customer/team", member, "qa"); status != 200 {
		t.Errorf("the team in an unknown environment = %d %s, want 200", status, body)
	}
}

func TestMeListsEnvironmentsBeingRemovedWhenAllAre(t *testing.T) {
	f := newFixture(t)
	acme, _ := f.customers.Get(t.Context(), "acme")
	acme.DeletionTimestamp = ptr.To(metav1.Now())
	member := f.signer.token(t, session{user: "user_2", org: "org_acme", role: "member"})
	_, body, _ := get(t, f.handler, "/api/me", member)
	if !strings.Contains(body, `"customerID":"acme","default":true,"deleting":true`) {
		t.Errorf("GET /api/me = %s, want acme, being removed, to show", body)
	}
}
