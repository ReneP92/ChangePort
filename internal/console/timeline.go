package console

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/user"
	"k8s.io/utils/ptr"

	"github.com/ReneP92/changeport/internal/console/api"
	"github.com/ReneP92/changeport/internal/events"
	"github.com/ReneP92/changeport/internal/metrics"
)

// Events reads customers' events for the timeline (docs/timeline.md);
// *events.Client implements it.
type Events interface {
	Read(ctx context.Context, q events.Query) ([]events.Event, bool, error)
}

// EventRecorder keeps the changes users make through the console for the
// timeline; *events.Recorder implements it.
type EventRecorder interface {
	Record(events ...events.Event)
}

// Users looks the console's users up: their names for the timeline, and
// their email addresses for trial requests.
type Users interface {
	// Name returns the user's name, or their email address without one.
	Name(ctx context.Context, userID string) (string, error)
	// VerifiedEmail returns the user's primary email address, or
	// ErrNoVerifiedEmail when they have none or have not verified it.
	VerifiedEmail(ctx context.Context, userID string) (string, error)
}

// ErrNoVerifiedEmail means a user has no verified primary email address.
var ErrNoVerifiedEmail = errors.New("no verified primary email address")

// ClerkUsers looks users up through Clerk's Backend API.
type ClerkUsers struct {
	users *user.Client
}

var _ Users = ClerkUsers{}

// NewClerkUsers returns Users that call Clerk's Backend API with secretKey.
func NewClerkUsers(secretKey string) ClerkUsers {
	return ClerkUsers{users: user.NewClient(&clerk.ClientConfig{BackendConfig: clerk.BackendConfig{Key: &secretKey}})}
}

// Name implements Users.
func (c ClerkUsers) Name(ctx context.Context, userID string) (string, error) {
	u, err := c.users.Get(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("get Clerk user %s: %w", userID, err)
	}
	name := strings.TrimSpace(ptr.Deref(u.FirstName, "") + " " + ptr.Deref(u.LastName, ""))
	if name != "" {
		return name, nil
	}
	for _, email := range u.EmailAddresses {
		if u.PrimaryEmailAddressID != nil && email.ID == *u.PrimaryEmailAddressID {
			return email.EmailAddress, nil
		}
	}
	return ptr.Deref(u.Username, userID), nil
}

// VerifiedEmail implements Users.
func (c ClerkUsers) VerifiedEmail(ctx context.Context, userID string) (string, error) {
	u, err := c.users.Get(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("get Clerk user %s: %w", userID, err)
	}
	for _, email := range u.EmailAddresses {
		if u.PrimaryEmailAddressID != nil && email.ID == *u.PrimaryEmailAddressID &&
			email.Verification != nil && email.Verification.Status == "verified" {
			return email.EmailAddress, nil
		}
	}
	return "", fmt.Errorf("user %s: %w", userID, ErrNoVerifiedEmail)
}

// userNames caches users' names for an hour, so that a burst of changes
// asks Clerk once.
type userNames struct {
	users Users
	mu    sync.Mutex
	names map[string]cachedName
}

type cachedName struct {
	name    string
	expires time.Time
}

// name returns a user's name, or their ID when Clerk cannot say.
func (n *userNames) name(ctx context.Context, userID string) string {
	n.mu.Lock()
	cached, ok := n.names[userID]
	n.mu.Unlock()
	if ok && time.Now().Before(cached.expires) {
		return cached.name
	}
	if n.users == nil {
		return userID
	}
	name, err := n.users.Name(ctx, userID)
	if err != nil || name == "" {
		return userID
	}
	n.mu.Lock()
	n.names[userID] = cachedName{name: name, expires: time.Now().Add(time.Hour)}
	n.mu.Unlock()
	return name
}

// recordTopicChange keeps a topic a user created or deleted for the
// timeline, with who did it; the timeline then leaves out the Kafka agent's
// record of the same change.
func (s *Server) recordTopicChange(ctx context.Context, principal Principal, tenant string, kind events.Kind, topic string, partitions int) {
	if s.recorder == nil {
		return
	}
	actor := s.userNames.name(ctx, principal.UserID)
	message := fmt.Sprintf("%s deleted topic %s in the console.", actor, topic)
	if kind == events.TopicCreated {
		message = fmt.Sprintf("%s created topic %s with %s in the console.", actor, topic, plural(partitions, "partition"))
	}
	s.recorder.Record(events.Event{
		Time: time.Now(), Tenant: tenant, Kind: kind, Topic: topic, Actor: actor, Message: message, Source: "console",
	})
}

// timeRange returns the range a request asks for, the step of its charts,
// and its start and end, which ends now at the step.
func timeRange(requested *api.TimeRange) (api.TimeRange, time.Duration, time.Time, time.Time, bool) {
	window := ptr.Deref(requested, api.N1h)
	step, ok := throughputRanges[window]
	if !ok {
		return window, 0, time.Time{}, time.Time{}, false
	}
	end := time.Now().Truncate(step)
	return window, step, end.Add(-rangeDurations[window]), end, true
}

var unknownRange = problem(http.StatusBadRequest, "Unknown range", "Choose 1h, 6h, 24h or 7d.")

// ListEvents implements api.StrictServerInterface.
func (s *Server) ListEvents(ctx context.Context, request api.ListEventsRequestObject) (api.ListEventsResponseObject, error) {
	_, _, start, _, ok := timeRange(request.Params.Range)
	if !ok {
		return api.ListEvents400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse(unknownRange)}, nil
	}
	customer, _, refused, err := s.customerOf(ctx)
	switch {
	case err != nil:
		return nil, err
	case refused != nil && refused.Status == http.StatusNotFound:
		return api.ListEvents404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(*refused)}, nil
	case refused != nil:
		return api.ListEvents403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(*refused)}, nil
	}
	// Up to now, rather than the charts' last step, so that what just
	// happened shows.
	q := events.Query{Tenant: customer.Name, Start: start, End: time.Now().Add(time.Minute)}
	if request.Params.Topic != nil && *request.Params.Topic != "" {
		q.Topics = []string{*request.Params.Topic}
	}
	if group := ptr.Deref(request.Params.ConsumerGroup, ""); group != "" {
		q.ConsumerGroup = group
		// The changes of the topics the group reads matter to it too.
		series, err := s.metrics.Instant(ctx, customer.Name, metrics.LagMessages, metrics.Filter{ConsumerGroup: group}, time.Now())
		if err != nil {
			return nil, err
		}
		for _, one := range series {
			if topic := one.Labels["topic"]; topic != "" && !slices.Contains(q.Topics, topic) {
				q.Topics = append(q.Topics, topic)
			}
		}
	}
	found, truncated, err := s.events.Read(ctx, q)
	if err != nil {
		return nil, err
	}
	list := api.ListEvents200JSONResponse{Events: make([]api.TimelineEvent, 0, len(found)), Truncated: truncated}
	for _, e := range found {
		list.Events = append(list.Events, toAPIEvent(e))
	}
	return list, nil
}

func toAPIEvent(e events.Event) api.TimelineEvent {
	optional := func(s string) *string {
		if s == "" {
			return nil
		}
		return ptr.To(s)
	}
	return api.TimelineEvent{
		Id: e.ID, Time: e.Time.UTC(), Kind: api.TimelineEventKind(e.Kind), Message: e.Message,
		Topic: optional(e.Topic), ConsumerGroup: optional(e.ConsumerGroup), Broker: optional(e.Broker),
		ClientId: optional(e.ClientID), Actor: optional(e.Actor),
	}
}

// GetConsumerGroupLag implements api.StrictServerInterface.
func (s *Server) GetConsumerGroupLag(ctx context.Context, request api.GetConsumerGroupLagRequestObject) (api.GetConsumerGroupLagResponseObject, error) {
	window, step, start, end, ok := timeRange(request.Params.Range)
	if !ok {
		return api.GetConsumerGroupLag400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse(unknownRange)}, nil
	}
	customer, _, refused, err := s.customerOf(ctx)
	switch {
	case err != nil:
		return nil, err
	case refused != nil && refused.Status == http.StatusNotFound:
		return api.GetConsumerGroupLag404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(*refused)}, nil
	case refused != nil:
		return api.GetConsumerGroupLag403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(*refused)}, nil
	case platformGroup(request.Group):
		return api.GetConsumerGroupLag404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(noSuchGroup(request.Group))}, nil
	}
	series, err := s.metrics.Range(ctx, customer.Name, metrics.LagSeconds, metrics.Filter{ConsumerGroup: request.Group}, start, end, step)
	if err != nil {
		return nil, err
	}
	history := api.GetConsumerGroupLag200JSONResponse{Range: string(window), StepSeconds: int(step.Seconds()), Topics: []api.TopicLagHistory{}}
	for _, one := range series {
		points := make([]api.Point, 0, len(one.Points))
		for _, p := range one.Points {
			points = append(points, api.Point{Time: p.Time.UTC(), Value: float32(p.Value)})
		}
		history.Topics = append(history.Topics, api.TopicLagHistory{Topic: one.Labels["topic"], LagSeconds: points})
	}
	slices.SortFunc(history.Topics, func(a, b api.TopicLagHistory) int { return cmp.Compare(a.Topic, b.Topic) })
	return history, nil
}

func noSuchGroup(group string) api.Problem {
	return problem(http.StatusNotFound, "No such consumer group", fmt.Sprintf("There is no consumer group %s.", group))
}

// plural is "1 partition" or "3 partitions".
func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
