package events

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/ReneP92/changeport/internal/provisioning"
)

// The timeline's alerts (deploy/base/alerting/rules/timeline.yaml), which
// mark when something started and stopped rather than call for action:
// Alertmanager sends them only to AlertmanagerHandler.
const (
	// TimelineAudience is their audience label.
	TimelineAudience = "timeline"
	// ThrottlingAlert fires while the brokers throttle a client.
	ThrottlingAlert = "TimelineClientThrottled"
)

// alertmanagerAlert is an alert of Alertmanager's webhook payload.
type alertmanagerAlert struct {
	Status      string            `json:"status"`
	Labels      map[string]string `json:"labels"`
	StartsAt    time.Time         `json:"startsAt"`
	EndsAt      time.Time         `json:"endsAt"`
	Fingerprint string            `json:"fingerprint"`
}

// fromAlert returns the events of one of the timeline's alerts: when it
// started, and when it ended once resolved. Each event's ID comes from the
// alert's fingerprint and start, so that Alertmanager sending the alert
// again records nothing new.
func fromAlert(alert alertmanagerAlert) []Event {
	if alert.Labels["audience"] != TimelineAudience || alert.Labels["alertname"] != ThrottlingAlert {
		return nil
	}
	tenant := alert.Labels["tenant"]
	if provisioning.ValidateCustomerID(tenant) != nil || alert.StartsAt.IsZero() {
		return nil
	}
	client := alert.Labels["client_id"]
	id := alert.Fingerprint + "-" + strconv.FormatInt(alert.StartsAt.Unix(), 10)
	out := []Event{{
		ID: "throttled-" + id, Time: alert.StartsAt, Tenant: tenant, Kind: ClientThrottled, ClientID: client,
		Message: fmt.Sprintf("The brokers started throttling client %s: it exceeds a quota, so they delay its requests.", client),
		Source:  "provisioner",
	}}
	if alert.Status == "resolved" && !alert.EndsAt.IsZero() {
		out = append(out, Event{
			ID: "unthrottled-" + id, Time: alert.EndsAt, Tenant: tenant, Kind: ClientUnthrottled, ClientID: client,
			Message: fmt.Sprintf("The brokers stopped throttling client %s.", client),
			Source:  "provisioner",
		})
	}
	return out
}

// AlertmanagerHandler receives the timeline's alerts from Alertmanager's
// webhook and records their events.
func AlertmanagerHandler(recorder interface{ Record(...Event) }, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var notification struct {
			Alerts []alertmanagerAlert `json:"alerts"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&notification); err != nil {
			http.Error(w, "not an Alertmanager notification: "+err.Error(), http.StatusBadRequest)
			return
		}
		var found []Event
		for _, alert := range notification.Alerts {
			found = append(found, fromAlert(alert)...)
		}
		if len(found) > 0 {
			recorder.Record(found...)
			logger.Debug("recorded the timeline's alerts", "events", len(found))
		}
		w.WriteHeader(http.StatusOK)
	})
}
