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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/ReneP92/changeport/api/v1alpha1"
	"github.com/ReneP92/changeport/internal/console/api"
)

// An organization has a customer, and a cluster, per environment, such as
// production and staging. Each environment is a Customer of its own; they
// share the organization. Its admins add, rename and remove environments,
// choose the default one and protect them, up to the configured limit;
// operators add environments beyond it.

// environmentPattern is the Customer CRD's rule for spec.environment: a DNS
// label of at most 20 characters, which a URL holds as it is.
var environmentPattern = regexp.MustCompile(`^[a-z]([-a-z0-9]{0,18}[a-z0-9])?$`)

const environmentRule = "Name the environment in 1 to 20 lowercase letters, digits and hyphens, starting with a letter, such as staging."

func validEnvironment(name string) bool { return environmentPattern.MatchString(name) }

var environmentAdminsOnly = problem(http.StatusForbidden, "Admins only", "Ask an admin of your organization to change its environments.")

// toEnvironment is a customer as one of its organization's environments.
func toEnvironment(c *v1alpha1.Customer) api.Environment {
	return api.Environment{
		Name:               environmentOf(c),
		CustomerID:         c.Name,
		Profile:            api.EnvironmentProfile(c.Spec.Profile),
		Ready:              toAPI(c).Ready,
		Deleting:           !c.DeletionTimestamp.IsZero(),
		Default:            c.Spec.DefaultEnvironment,
		DeletionProtection: c.Spec.DeletionProtection,
		Trial:              c.Spec.Trial != nil,
	}
}

// environments lists an organization's customers as its environments, the
// default first. The first in order is the default, whether or not it was
// chosen as one.
func (s *Server) environments(customers []v1alpha1.Customer) api.Environments {
	slices.SortFunc(customers, environmentOrder)
	list := api.Environments{Environments: make([]api.Environment, 0, len(customers)), Limit: s.config.maxEnvironments()}
	for i := range customers {
		environment := toEnvironment(&customers[i])
		environment.Default = i == 0
		list.Environments = append(list.Environments, environment)
	}
	return list
}

// newEnvironment checks a new environment's name and profile, returning the
// name, or the field and why it is refused.
func newEnvironment(body *api.NewEnvironment) (name, field, detail string) {
	if body == nil {
		return "", "environment", environmentRule
	}
	name = strings.TrimSpace(body.Environment)
	switch {
	case !validEnvironment(name):
		return "", "environment", environmentRule
	case !validProfile(string(body.Profile)):
		return "", "profile", "Choose dev, dev-ha or production."
	}
	return name, "", ""
}

// environmentTaken returns the problem with a name the organization has
// already, even for an environment being removed, or nil.
func environmentTaken(customers []v1alpha1.Customer, name string, except string) *api.Problem {
	i := slices.IndexFunc(customers, func(c v1alpha1.Customer) bool { return c.Name != except && environmentOf(&c) == name })
	if i < 0 {
		return nil
	}
	detail := "Your organization has an environment " + name + " already."
	if !customers[i].DeletionTimestamp.IsZero() {
		detail = "Your organization's environment " + name + " is being removed; its name is free once it is gone."
	}
	p := problem(http.StatusConflict, "Environment exists", detail)
	p.Field = ptr.To("environment")
	return &p
}

// addEnvironment provisions a new environment in an organization, sharing
// the display name of sibling, one of its customers.
func (s *Server) addEnvironment(ctx context.Context, principal Principal, organizationID string, sibling *v1alpha1.Customer, name string, body *api.NewEnvironment) (*v1alpha1.Customer, error) {
	customer := &v1alpha1.Customer{Spec: v1alpha1.CustomerSpec{
		DisplayName:        sibling.Spec.DisplayName,
		Environment:        name,
		Profile:            string(body.Profile),
		DeletionProtection: body.DeletionProtection,
	}}
	// The organization's members are the new environment's already.
	if _, err := s.provision(ctx, customer, organizationID, func(string) error { return nil }); err != nil {
		return nil, err
	}
	s.audit(ctx, principal, customer.Name, "environment added", "environment", name, "profile", customer.Spec.Profile,
		"customer_organization", customer.Spec.ClerkOrganizationID, "deletion_protection", customer.Spec.DeletionProtection)
	return customer, nil
}

// AddEnvironment implements api.StrictServerInterface: operators add an
// environment to the organization of a customer that is no trial, beyond
// the limit its admins have.
func (s *Server) AddEnvironment(ctx context.Context, request api.AddEnvironmentRequestObject) (api.AddEnvironmentResponseObject, error) {
	principal, err := mustPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if !s.operator(principal) {
		return api.AddEnvironment403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(operatorsOnly)}, nil
	}
	name, field, detail := newEnvironment(request.Body)
	if field != "" {
		p := problem(http.StatusBadRequest, "Invalid environment", detail)
		p.Field = &field
		return api.AddEnvironment400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse(p)}, nil
	}
	sibling, err := s.customers.Get(ctx, request.CustomerID)
	if err != nil {
		return nil, err
	}
	conflict := func(p api.Problem) api.AddEnvironmentResponseObject {
		return api.AddEnvironment409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(p)}
	}
	switch {
	case sibling == nil:
		return api.AddEnvironment404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(problem(http.StatusNotFound, "No such customer", ""))}, nil
	case sibling.Spec.ClerkOrganizationID == "":
		return conflict(problem(http.StatusConflict, "No organization", "The customer has no organization to add an environment to.")), nil
	case sibling.Spec.Trial != nil:
		return conflict(problem(http.StatusConflict, "On a trial", "A trial is one environment. Convert the customer first, then add environments.")), nil
	}
	environments, err := s.customers.ByOrganization(ctx, sibling.Spec.ClerkOrganizationID)
	if err != nil {
		return nil, err
	}
	if p := environmentTaken(environments, name, ""); p != nil {
		p.Detail = ptr.To(strings.Replace(*p.Detail, "Your organization", "The organization", 1))
		return conflict(*p), nil
	}
	customer, err := s.addEnvironment(ctx, principal, sibling.Spec.ClerkOrganizationID, sibling, name, request.Body)
	if errors.Is(err, ErrCustomerExists) {
		return conflict(problem(http.StatusConflict, "Try again", "Could not find a free customer ID.")), nil
	}
	if err != nil {
		return nil, err
	}
	return api.AddEnvironment201JSONResponse(toAPI(customer)), nil
}

// environmentCaller returns the caller's organization for the environment
// routes, or the problem why there is none; admin asks for an organization
// admin.
func (s *Server) environmentCaller(ctx context.Context, admin bool) (*organizationContext, *api.Problem, error) {
	organization, refused, err := s.organizationOf(ctx)
	if err != nil || refused != nil {
		return nil, refused, err
	}
	if admin && !organization.principal.Admin() {
		return nil, &environmentAdminsOnly, nil
	}
	return organization, nil, nil
}

// environment returns the organization's environment of a customer, or the
// problem why it cannot change: there is no such, or it is being removed.
func (o *organizationContext) environment(customerID string) (*v1alpha1.Customer, *api.Problem) {
	i := slices.IndexFunc(o.customers, func(c v1alpha1.Customer) bool { return c.Name == customerID })
	if i < 0 {
		p := problem(http.StatusNotFound, "No such environment", "Your organization has no such environment.")
		return nil, &p
	}
	customer := &o.customers[i]
	if !customer.DeletionTimestamp.IsZero() {
		p := problem(http.StatusConflict, "Being removed", fmt.Sprintf("The environment %s is being removed.", environmentOf(customer)))
		return nil, &p
	}
	return customer, nil
}

// ListEnvironments implements api.StrictServerInterface.
func (s *Server) ListEnvironments(ctx context.Context, _ api.ListEnvironmentsRequestObject) (api.ListEnvironmentsResponseObject, error) {
	organization, refused, err := s.environmentCaller(ctx, false)
	if err != nil {
		return nil, err
	}
	if refused != nil {
		return api.ListEnvironments403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(*refused)}, nil
	}
	return api.ListEnvironments200JSONResponse(s.environments(organization.customers)), nil
}

// CreateEnvironment implements api.StrictServerInterface: an admin adds an
// environment to an organization that has one and is no trial, up to the
// limit.
func (s *Server) CreateEnvironment(ctx context.Context, request api.CreateEnvironmentRequestObject) (api.CreateEnvironmentResponseObject, error) {
	organization, refused, err := s.environmentCaller(ctx, true)
	if err != nil {
		return nil, err
	}
	if refused != nil {
		return api.CreateEnvironment403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(*refused)}, nil
	}
	name, field, detail := newEnvironment(request.Body)
	if field != "" {
		p := problem(http.StatusBadRequest, "Invalid environment", detail)
		p.Field = &field
		return api.CreateEnvironment400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse(p)}, nil
	}
	conflict := func(p api.Problem) api.CreateEnvironmentResponseObject {
		return api.CreateEnvironment409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(p)}
	}
	customers := organization.customers
	limit := s.config.maxEnvironments()
	switch {
	case len(customers) == 0:
		return conflict(problem(http.StatusConflict, "No cluster yet",
			"Your organization gets its first cluster with a trial, or from Changeport; then you can add environments.")), nil
	case slices.ContainsFunc(customers, func(c v1alpha1.Customer) bool { return c.Spec.Trial != nil }):
		return conflict(problem(http.StatusConflict, "On a trial",
			"A trial is one environment. Once your organization is a customer, you can add more.")), nil
	case len(customers) >= limit:
		return conflict(problem(http.StatusConflict, "At the limit",
			fmt.Sprintf("Your organization has %d environments, the most it may have. Remove one, or ask Changeport for more.", len(customers)))), nil
	}
	if p := environmentTaken(customers, name, ""); p != nil {
		return conflict(*p), nil
	}
	// The caller's organization, which the customers were found by.
	customer, err := s.addEnvironment(ctx, organization.principal, organization.principal.OrganizationID, &customers[0], name, request.Body)
	if errors.Is(err, ErrCustomerExists) {
		return conflict(problem(http.StatusConflict, "Try again", "Could not find a free customer ID.")), nil
	}
	if err != nil {
		return nil, err
	}
	return api.CreateEnvironment201JSONResponse(s.environments(append(customers, *customer))), nil
}

// ChangeEnvironment implements api.StrictServerInterface: an admin renames
// an environment, makes it the default, or turns its deletion protection on
// or off, which takes its name.
func (s *Server) ChangeEnvironment(ctx context.Context, request api.ChangeEnvironmentRequestObject) (api.ChangeEnvironmentResponseObject, error) {
	organization, refused, err := s.environmentCaller(ctx, true)
	if err != nil {
		return nil, err
	}
	if refused != nil {
		return api.ChangeEnvironment403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(*refused)}, nil
	}
	invalid := func(field, detail string) api.ChangeEnvironmentResponseObject {
		p := problem(http.StatusBadRequest, "Invalid change", detail)
		p.Field = &field
		return api.ChangeEnvironment400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse(p)}
	}
	conflict := func(p api.Problem) api.ChangeEnvironmentResponseObject {
		return api.ChangeEnvironment409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(p)}
	}
	customer, refused := organization.environment(request.CustomerID)
	if refused != nil {
		if refused.Status == http.StatusNotFound {
			return api.ChangeEnvironment404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(*refused)}, nil
		}
		return conflict(*refused), nil
	}
	body := request.Body
	if body == nil || body.Name == nil && body.Default == nil && body.DeletionProtection == nil {
		return invalid("name", "Say what to change: the name, the default or deletion protection."), nil
	}
	current := environmentOf(customer)
	name := current
	if body.Name != nil {
		name = strings.TrimSpace(*body.Name)
		if !validEnvironment(name) {
			return invalid("name", environmentRule), nil
		}
		if p := environmentTaken(organization.customers, name, customer.Name); p != nil {
			p.Field = ptr.To("name")
			return conflict(*p), nil
		}
	}
	protect := customer.Spec.DeletionProtection
	if body.DeletionProtection != nil {
		protect = *body.DeletionProtection
		switch {
		case protect && customer.Spec.Trial != nil:
			return conflict(problem(http.StatusConflict, "On a trial", "A trial ends on its own, so it cannot be protected.")), nil
		case !protect && customer.Spec.DeletionProtection && strings.TrimSpace(ptr.Deref(body.Confirm, "")) != current:
			return invalid("confirm", fmt.Sprintf("Type the environment's name, %s, to turn its deletion protection off.", current)), nil
		}
	}
	makeDefault := body.Default != nil && bool(*body.Default)
	err = s.customers.Change(ctx, customer, func(spec *v1alpha1.CustomerSpec) {
		spec.Environment = name
		spec.DeletionProtection = protect
		if makeDefault {
			spec.DefaultEnvironment = true
		}
	})
	if errors.Is(err, ErrCustomerChanged) {
		return conflict(problem(http.StatusConflict, "Changed meanwhile", "The environment changed meanwhile; nothing was changed. Try again.")), nil
	}
	if err != nil {
		return nil, err
	}
	if makeDefault {
		if err := s.clearDefaults(ctx, organization.customers, customer.Name); err != nil {
			return nil, err
		}
	}
	principal, tenant := organization.principal, customer.Name
	if name != current {
		s.audit(ctx, principal, tenant, "environment renamed", "environment", name, "previous_environment", current)
	}
	if makeDefault {
		s.audit(ctx, principal, tenant, "default environment set", "environment", name)
	}
	if body.DeletionProtection != nil {
		s.audit(ctx, principal, tenant, "deletion protection turned "+map[bool]string{true: "on", false: "off"}[protect], "environment", name)
	}
	return api.ChangeEnvironment200JSONResponse(s.environments(organization.customers)), nil
}

// clearDefaults clears the default of the organization's other customers,
// once one was made the default, reading each again when it changed
// meanwhile.
func (s *Server) clearDefaults(ctx context.Context, customers []v1alpha1.Customer, except string) error {
	for i := range customers {
		c := &customers[i]
		if c.Name == except || !c.Spec.DefaultEnvironment {
			continue
		}
		clear := func(spec *v1alpha1.CustomerSpec) { spec.DefaultEnvironment = false }
		err := s.customers.Change(ctx, c, clear)
		if errors.Is(err, ErrCustomerChanged) {
			fresh, getErr := s.customers.Get(ctx, c.Name)
			if getErr != nil {
				return getErr
			}
			if fresh == nil {
				// Gone meanwhile, with its default.
				continue
			}
			*c = *fresh
			err = s.customers.Change(ctx, c, clear)
		}
		if err != nil {
			return fmt.Errorf("clear the default of environment %s: %w", environmentOf(c), err)
		}
	}
	return nil
}

// RemoveEnvironment implements api.StrictServerInterface: an admin removes
// an environment that is not protected, which deletes its cluster. The
// organization keeps one: closing an account is for Changeport.
func (s *Server) RemoveEnvironment(ctx context.Context, request api.RemoveEnvironmentRequestObject) (api.RemoveEnvironmentResponseObject, error) {
	organization, refused, err := s.environmentCaller(ctx, true)
	if err != nil {
		return nil, err
	}
	if refused != nil {
		return api.RemoveEnvironment403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(*refused)}, nil
	}
	conflict := func(p api.Problem) api.RemoveEnvironmentResponseObject {
		return api.RemoveEnvironment409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(p)}
	}
	customer, refused := organization.environment(request.CustomerID)
	if refused != nil {
		if refused.Status == http.StatusNotFound {
			return api.RemoveEnvironment404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(*refused)}, nil
		}
		return conflict(*refused), nil
	}
	name := environmentOf(customer)
	others := slices.ContainsFunc(organization.customers, func(c v1alpha1.Customer) bool {
		return c.Name != customer.Name && c.DeletionTimestamp.IsZero()
	})
	switch {
	case !others:
		return conflict(problem(http.StatusConflict, "The last environment",
			"Your organization keeps at least one environment. To close your account, contact Changeport.")), nil
	case customer.Spec.DeletionProtection:
		return conflict(problem(http.StatusConflict, "Deletion protection is on",
			fmt.Sprintf("Turn off the deletion protection of %s first.", name))), nil
	}
	if err := s.customers.Delete(ctx, customer.Name); errors.Is(err, ErrNoSuchCustomer) {
		p := problem(http.StatusNotFound, "No such environment", "Your organization has no such environment.")
		return api.RemoveEnvironment404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(p)}, nil
	} else if err != nil {
		return nil, err
	}
	s.audit(ctx, organization.principal, customer.Name, "environment removed", "environment", name)
	// Being deleted, as the next read says.
	customer.DeletionTimestamp = ptr.To(metav1.NewTime(time.Now()))
	return api.RemoveEnvironment200JSONResponse(s.environments(organization.customers)), nil
}
