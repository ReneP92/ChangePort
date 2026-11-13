package console

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
