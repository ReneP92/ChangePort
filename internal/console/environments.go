package console

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"k8s.io/utils/ptr"

	"github.com/ReneP92/changeport/api/v1alpha1"
	"github.com/ReneP92/changeport/internal/console/api"
)

// An organization has a customer, and a cluster, per environment, such as
// production and staging, which operators add. Each environment is a
// Customer of its own; they share the organization.

// environmentPattern is the Customer CRD's rule for spec.environment: a DNS
// label of at most 20 characters, which a URL holds as it is.
var environmentPattern = regexp.MustCompile(`^[a-z]([-a-z0-9]{0,18}[a-z0-9])?$`)

const environmentRule = "Name the environment in 1 to 20 lowercase letters, digits and hyphens, starting with a letter, such as staging."

func validEnvironment(name string) bool { return environmentPattern.MatchString(name) }

// AddEnvironment implements api.StrictServerInterface: operators add an
// environment to the organization of a customer that is no trial.
func (s *Server) AddEnvironment(ctx context.Context, request api.AddEnvironmentRequestObject) (api.AddEnvironmentResponseObject, error) {
	principal, err := mustPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if !s.operator(principal) {
		return api.AddEnvironment403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(operatorsOnly)}, nil
	}
	invalid := func(field, detail string) api.AddEnvironmentResponseObject {
		p := problem(http.StatusBadRequest, "Invalid environment", detail)
		p.Field = &field
		return api.AddEnvironment400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse(p)}
	}
	body := request.Body
	if body == nil {
		return invalid("environment", environmentRule), nil
	}
	name := strings.TrimSpace(body.Environment)
	switch {
	case !validEnvironment(name):
		return invalid("environment", environmentRule), nil
	case !validProfile(string(body.Profile)):
		return invalid("profile", "Choose dev, dev-ha or production."), nil
	}
	sibling, err := s.customers.Get(ctx, request.CustomerID)
	if err != nil {
		return nil, err
	}
	conflict := func(title, detail string) api.AddEnvironmentResponseObject {
		return api.AddEnvironment409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(problem(http.StatusConflict, title, detail))}
	}
	switch {
	case sibling == nil:
		return api.AddEnvironment404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(problem(http.StatusNotFound, "No such customer", ""))}, nil
	case sibling.Spec.ClerkOrganizationID == "":
		return conflict("No organization", "The customer has no organization to add an environment to."), nil
	case sibling.Spec.Trial != nil:
		return conflict("On a trial", "A trial is one environment. Convert the customer first, then add environments."), nil
	}
	environments, err := s.customers.ByOrganization(ctx, sibling.Spec.ClerkOrganizationID)
	if err != nil {
		return nil, err
	}
	if slices.ContainsFunc(environments, func(c v1alpha1.Customer) bool { return environmentOf(&c) == name }) {
		p := problem(http.StatusConflict, "Environment exists", "The organization has an environment "+name+" already.")
		p.Field = ptr.To("environment")
		return api.AddEnvironment409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(p)}, nil
	}
	customer := &v1alpha1.Customer{Spec: v1alpha1.CustomerSpec{
		DisplayName:        sibling.Spec.DisplayName,
		Environment:        name,
		Profile:            string(body.Profile),
		DeletionProtection: body.DeletionProtection,
	}}
	// The organization's members are the new environment's already.
	if _, err := s.provision(ctx, customer, sibling.Spec.ClerkOrganizationID, func(string) error { return nil }); err != nil {
		if errors.Is(err, ErrCustomerExists) {
			return conflict("Try again", "Could not find a free customer ID."), nil
		}
		return nil, err
	}
	s.audit(ctx, principal, customer.Name, "environment added", "environment", name, "profile", customer.Spec.Profile,
		"customer_organization", customer.Spec.ClerkOrganizationID, "deletion_protection", customer.Spec.DeletionProtection)
	return api.AddEnvironment201JSONResponse(toAPI(customer)), nil
}
