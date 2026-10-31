package console

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/ReneP92/changeport/api/v1alpha1"
	"github.com/ReneP92/changeport/internal/console/api"
)

var alertAdminsOnly = problem(http.StatusForbidden, "Admins only", "Ask an admin of your organization to change where alerts go and when they fire.")

var alertSettingsChanged = problem(http.StatusConflict, "The alert settings changed meanwhile", "Reload them, and try again.")

// alertCaller returns the caller's customer for the alerts' routes, or the
// problem why there is none; admin asks for an organization admin.
func (s *Server) alertCaller(ctx context.Context, admin bool) (*customerContext, *api.Problem, error) {
	customer, principal, refused, err := s.customerOf(ctx)
	if err != nil || refused != nil {
		return nil, refused, err
	}
	if admin && !principal.Admin() {
		return nil, &alertAdminsOnly, nil
	}
	return &customerContext{customer: customer, principal: principal}, nil, nil
}

// ListAlerts implements api.StrictServerInterface.
func (s *Server) ListAlerts(ctx context.Context, _ api.ListAlertsRequestObject) (api.ListAlertsResponseObject, error) {
	caller, refused, err := s.alertCaller(ctx, false)
	switch {
	case err != nil:
		return nil, err
	case refused != nil && forbidden(refused):
		return api.ListAlerts403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(*refused)}, nil
	case refused != nil:
		return api.ListAlerts404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(*refused)}, nil
	}
	alerts, err := s.alerts.Active(ctx, caller.id())
	if err != nil {
		return nil, err
	}
	return api.ListAlerts200JSONResponse{Alerts: alerts}, nil
}

// StreamAlerts implements api.StrictServerInterface.
func (s *Server) StreamAlerts(ctx context.Context, _ api.StreamAlertsRequestObject) (api.StreamAlertsResponseObject, error) {
	caller, refused, err := s.alertCaller(ctx, false)
	switch {
	case err != nil:
		return nil, err
	case refused != nil && forbidden(refused):
		return api.StreamAlerts403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(*refused)}, nil
	case refused != nil:
		return api.StreamAlerts404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(*refused)}, nil
	}
	// The first list comes before the stream, so that a failure is an
	// error rather than a stream without events.
	first, err := s.alerts.Active(ctx, caller.id())
	if err != nil {
		return nil, err
	}
	return alertStream{
		ctx: ctx, first: first, until: caller.principal.Expires, every: s.alertPoll,
		next: func(ctx context.Context) ([]api.Alert, error) { return s.alerts.Active(ctx, caller.id()) },
		logger: func(err error) {
			s.logger.WarnContext(ctx, "could not read a customer's alerts for its stream", "tenant", caller.id(), "error", err)
		},
	}, nil
}

// keepAlive is how often a quiet stream sends a comment, so that proxies
// and the browser see it is alive.
const keepAlive = 20 * time.Second

// alertStream streams a customer's alerts as server-sent events: an alerts
// event with the AlertList at once, and again whenever it changes, until
// the session token expires or the client goes away.
type alertStream struct {
	ctx    context.Context //nolint:containedctx // the request's, which the response outlives otherwise
	first  []api.Alert
	until  time.Time
	every  time.Duration
	next   func(context.Context) ([]api.Alert, error)
	logger func(error)
}

// VisitStreamAlertsResponse implements api.StreamAlertsResponseObject.
func (a alertStream) VisitStreamAlertsResponse(w http.ResponseWriter) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return errors.New("the response cannot stream")
	}
	ctx, cancel := context.WithDeadline(a.ctx, a.until)
	defer cancel()
	// The server's write timeout is for ordinary responses; a stream lasts
	// as long as its token.
	_ = http.NewResponseController(w).SetWriteDeadline(a.until.Add(keepAlive))
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	// Proxies such as nginx would hold the events back otherwise.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	send := func(alerts []api.Alert) error {
		data, err := json.Marshal(api.AlertList{Alerts: alerts})
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "event: alerts\ndata: %s\n\n", data); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}
	last := a.first
	if err := send(last); err != nil {
		return nil //nolint:nilerr // the client went away
	}
	poll, quiet := time.NewTicker(a.every), time.NewTicker(keepAlive)
	defer poll.Stop()
	defer quiet.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-quiet.C:
			if _, err := fmt.Fprint(w, ": still here\n\n"); err != nil {
				return nil //nolint:nilerr // the client went away
			}
			flusher.Flush()
		case <-poll.C:
			alerts, err := a.next(ctx)
			if err != nil {
				if ctx.Err() == nil {
					a.logger(err)
				}
				continue
			}
			if reflect.DeepEqual(alerts, last) {
				continue
			}
			last = alerts
			if err := send(alerts); err != nil {
				return nil //nolint:nilerr // the client went away
			}
			quiet.Reset(keepAlive)
		}
	}
}

// GetAlertSettings implements api.StrictServerInterface.
func (s *Server) GetAlertSettings(ctx context.Context, _ api.GetAlertSettingsRequestObject) (api.GetAlertSettingsResponseObject, error) {
	caller, refused, err := s.alertCaller(ctx, false)
	switch {
	case err != nil:
		return nil, err
	case refused != nil && forbidden(refused):
		return api.GetAlertSettings403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(*refused)}, nil
	case refused != nil:
		return api.GetAlertSettings404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(*refused)}, nil
	}
	settings, err := s.alertSettingsOf(ctx, caller)
	if err != nil {
		return nil, err
	}
	return api.GetAlertSettings200JSONResponse(toAPIAlertSettings(settings)), nil
}

// alertSettingsOf returns the customer's settings, or new ones with the
// defaults.
func (s *Server) alertSettingsOf(ctx context.Context, caller *customerContext) (*v1alpha1.AlertSettings, error) {
	settings, err := s.alertSettings.Get(ctx, caller.id())
	if err != nil || settings != nil {
		return settings, err
	}
	return &v1alpha1.AlertSettings{Spec: v1alpha1.DefaultAlertSettings}, nil
}

// thresholdLimits are the limits of the thresholds, as the CRD has them.
var thresholdLimits = struct{ lag, stuck, throttling, percent [2]int64 }{
	lag: [2]int64{60, 86400}, stuck: [2]int64{300, 86400}, throttling: [2]int64{10, 60000}, percent: [2]int64{1, 100},
}

// thresholdsProblem says what is wrong with thresholds, if anything.
func thresholdsProblem(t api.AlertThresholds) *api.Problem {
	check := func(field string, value int64, limits [2]int64, unit string) *api.Problem {
		if value < limits[0] || value > limits[1] {
			p := problem(http.StatusBadRequest, "Invalid alert settings",
				fmt.Sprintf("The %s threshold must be from %d to %d %s.", field, limits[0], limits[1], unit))
			return &p
		}
		return nil
	}
	for _, p := range []*api.Problem{
		check("consumer lag", t.ConsumerLag.Seconds, thresholdLimits.lag, "seconds"),
		check("stuck consumer", t.ConsumerStuck.Seconds, thresholdLimits.stuck, "seconds"),
		check("throttling", t.Throttling.Milliseconds, thresholdLimits.throttling, "milliseconds"),
		check("errors", t.Errors.Percent, thresholdLimits.percent, "percent"),
	} {
		if p != nil {
			return p
		}
	}
	return nil
}

// ChangeAlertSettings implements api.StrictServerInterface.
func (s *Server) ChangeAlertSettings(ctx context.Context, request api.ChangeAlertSettingsRequestObject) (api.ChangeAlertSettingsResponseObject, error) {
	caller, refused, err := s.alertCaller(ctx, true)
	switch {
	case err != nil:
		return nil, err
	case refused != nil && forbidden(refused):
		return api.ChangeAlertSettings403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(*refused)}, nil
	case refused != nil:
		return api.ChangeAlertSettings404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(*refused)}, nil
	case request.Body == nil:
		return api.ChangeAlertSettings400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse(
			problem(http.StatusBadRequest, "Invalid alert settings", "Send the thresholds."))}, nil
	}
	if p := thresholdsProblem(*request.Body); p != nil {
		return api.ChangeAlertSettings400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse(*p)}, nil
	}
	settings, err := s.alertSettingsOf(ctx, caller)
	if err != nil {
		return nil, err
	}
	t := request.Body
	settings.Spec.ConsumerLag = v1alpha1.ConsumerLagAlert{Enabled: t.ConsumerLag.Enabled, Seconds: t.ConsumerLag.Seconds}
	settings.Spec.ConsumerStuck = v1alpha1.ConsumerStuckAlert{Enabled: t.ConsumerStuck.Enabled, Seconds: t.ConsumerStuck.Seconds}
	settings.Spec.Throttling = v1alpha1.ThrottlingAlert{Enabled: t.Throttling.Enabled, Milliseconds: t.Throttling.Milliseconds}
	settings.Spec.Errors = v1alpha1.ErrorsAlert{Enabled: t.Errors.Enabled, Percent: t.Errors.Percent}
	if err := s.alertSettings.Save(ctx, caller.id(), settings); errors.Is(err, ErrAlertSettingsChanged) {
		return api.ChangeAlertSettings409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(alertSettingsChanged)}, nil
	} else if err != nil {
		return nil, err
	}
	s.audit(ctx, caller.principal, caller.id(), "alert thresholds changed", "thresholds", *t)
	return api.ChangeAlertSettings200JSONResponse(toAPIAlertSettings(settings)), nil
}

// receiverOf returns the field of a receiver in the settings.
func receiverOf(settings *v1alpha1.AlertSettings, kind api.ReceiverKind) **v1alpha1.AlertReceiver {
	switch kind {
	case api.Slack:
		return &settings.Spec.Receivers.Slack
	case api.PagerDuty:
		return &settings.Spec.Receivers.PagerDuty
	case api.Webhook:
		return &settings.Spec.Receivers.Webhook
	}
	return nil
}

func noSuchReceiver(kind api.ReceiverKind) api.Problem {
	return problem(http.StatusNotFound, "No such receiver", fmt.Sprintf("There is no receiver %q; there are slack, pagerDuty and webhook.", kind))
}

// SetAlertReceiver implements api.StrictServerInterface.
func (s *Server) SetAlertReceiver(ctx context.Context, request api.SetAlertReceiverRequestObject) (api.SetAlertReceiverResponseObject, error) {
	caller, refused, err := s.alertCaller(ctx, true)
	switch {
	case err != nil:
		return nil, err
	case refused != nil && forbidden(refused):
		return api.SetAlertReceiver403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(*refused)}, nil
	case refused != nil:
		return api.SetAlertReceiver404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(*refused)}, nil
	case !request.Receiver.Valid():
		return api.SetAlertReceiver404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(noSuchReceiver(request.Receiver))}, nil
	case request.Body == nil:
		return api.SetAlertReceiver400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse(
			problem(http.StatusBadRequest, "Invalid receiver", "Send the receiver's value."))}, nil
	}
	value := strings.TrimSpace(request.Body.Value)
	hint, err := s.receivers.Check(ctx, request.Receiver, value)
	if err != nil {
		return api.SetAlertReceiver400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse(
			problem(http.StatusBadRequest, "Invalid receiver", capitalizeFirst(err.Error())+"."))}, nil
	}
	settings, err := s.alertSettingsOf(ctx, caller)
	if err != nil {
		return nil, err
	}
	if settings.ResourceVersion == "" {
		// The Secret is the settings' to own, so they come first.
		if err := s.alertSettings.Save(ctx, caller.id(), settings); errors.Is(err, ErrAlertSettingsChanged) {
			return api.SetAlertReceiver409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(alertSettingsChanged)}, nil
		} else if err != nil {
			return nil, err
		}
	}
	secret, err := s.alertSettings.WriteReceiver(ctx, settings, request.Receiver, value)
	if err != nil {
		return nil, err
	}
	*receiverOf(settings, request.Receiver) = &v1alpha1.AlertReceiver{Secret: secret, Hint: hint}
	if err := s.alertSettings.Save(ctx, caller.id(), settings); errors.Is(err, ErrAlertSettingsChanged) {
		return api.SetAlertReceiver409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(alertSettingsChanged)}, nil
	} else if err != nil {
		return nil, err
	}
	s.audit(ctx, caller.principal, caller.id(), "alert receiver set", "receiver", request.Receiver, "hint", hint)
	return api.SetAlertReceiver200JSONResponse(toAPIAlertSettings(settings)), nil
}

// RemoveAlertReceiver implements api.StrictServerInterface. The provisioner
// deletes the receiver's Secret.
func (s *Server) RemoveAlertReceiver(ctx context.Context, request api.RemoveAlertReceiverRequestObject) (api.RemoveAlertReceiverResponseObject, error) {
	caller, refused, err := s.alertCaller(ctx, true)
	switch {
	case err != nil:
		return nil, err
	case refused != nil && forbidden(refused):
		return api.RemoveAlertReceiver403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(*refused)}, nil
	case refused != nil:
		return api.RemoveAlertReceiver404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(*refused)}, nil
	case !request.Receiver.Valid():
		return api.RemoveAlertReceiver404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(noSuchReceiver(request.Receiver))}, nil
	}
	settings, err := s.alertSettingsOf(ctx, caller)
	if err != nil {
		return nil, err
	}
	field := receiverOf(settings, request.Receiver)
	if *field == nil {
		return api.RemoveAlertReceiver200JSONResponse(toAPIAlertSettings(settings)), nil
	}
	*field = nil
	if err := s.alertSettings.Save(ctx, caller.id(), settings); errors.Is(err, ErrAlertSettingsChanged) {
		return api.RemoveAlertReceiver409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(alertSettingsChanged)}, nil
	} else if err != nil {
		return nil, err
	}
	s.audit(ctx, caller.principal, caller.id(), "alert receiver removed", "receiver", request.Receiver)
	return api.RemoveAlertReceiver200JSONResponse(toAPIAlertSettings(settings)), nil
}

func toAPIThresholds(spec v1alpha1.AlertSettingsSpec) api.AlertThresholds {
	return api.AlertThresholds{
		ConsumerLag:   api.SecondsThreshold{Enabled: spec.ConsumerLag.Enabled, Seconds: spec.ConsumerLag.Seconds},
		ConsumerStuck: api.SecondsThreshold{Enabled: spec.ConsumerStuck.Enabled, Seconds: spec.ConsumerStuck.Seconds},
		Throttling:    api.MillisecondsThreshold{Enabled: spec.Throttling.Enabled, Milliseconds: spec.Throttling.Milliseconds},
		Errors:        api.PercentThreshold{Enabled: spec.Errors.Enabled, Percent: spec.Errors.Percent},
	}
}

func toAPIAlertSettings(settings *v1alpha1.AlertSettings) api.AlertSettings {
	status := func(r *v1alpha1.AlertReceiver) *api.ReceiverStatus {
		if r == nil {
			return nil
		}
		return &api.ReceiverStatus{Hint: r.Hint}
	}
	receivers := settings.Spec.Receivers
	return api.AlertSettings{
		Thresholds: toAPIThresholds(settings.Spec),
		Defaults:   toAPIThresholds(v1alpha1.DefaultAlertSettings),
		Receivers: api.AlertReceivers{
			Slack: status(receivers.Slack), PagerDuty: status(receivers.PagerDuty), Webhook: status(receivers.Webhook),
		},
	}
}
