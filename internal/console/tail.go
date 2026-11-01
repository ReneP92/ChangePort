package console

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"k8s.io/utils/ptr"

	"github.com/ReneP92/changeport/internal/clusters"
	"github.com/ReneP92/changeport/internal/console/api"
	"github.com/ReneP92/changeport/internal/provisioning"
	"github.com/ReneP92/changeport/internal/tail"
	"github.com/ReneP92/changeport/internal/topics"
)

// The tail's limits (docs/console.md).
const (
	// tailRate is how many messages a second a tail shows at most; the
	// others are counted as skipped.
	tailRate = 20
	// tailRecent is how many of a topic's last messages a tail starts with.
	tailRecent = 50
	// tailLimit is how many bytes of a key, value or header a tail shows.
	tailLimit = 8 << 10
	// tailHeaders is how many headers of a message a tail shows.
	tailHeaders = 20
	// tailsPerCustomer is how many tails a customer may run at once.
	tailsPerCustomer = 3
)

// tails counts the tails each customer runs.
type tails struct {
	mu      sync.Mutex
	running map[string]int
}

func (t *tails) acquire(customerID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.running[customerID] >= tailsPerCustomer {
		return false
	}
	t.running[customerID]++
	return true
}

func (t *tails) release(customerID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.running[customerID]--
}

var tailAdminsOnly = problem(http.StatusForbidden, "Admins only",
	"A topic's messages may hold what only your organization's admins should see; ask one of them.")

// TailTopic implements api.StrictServerInterface.
func (s *Server) TailTopic(ctx context.Context, request api.TailTopicRequestObject) (api.TailTopicResponseObject, error) {
	customer, principal, refused, err := s.customerOf(ctx)
	switch {
	case err != nil:
		return nil, err
	case refused != nil && forbidden(refused):
		return api.TailTopic403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(*refused)}, nil
	case refused != nil:
		return api.TailTopic404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(*refused)}, nil
	case !principal.Admin():
		return api.TailTopic403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(tailAdminsOnly)}, nil
	case topics.Reserved(request.Topic):
		// The platform's own topics are not the customer's to read.
		return api.TailTopic404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(noSuchTopic(request.Topic))}, nil
	}
	start := TailStart{Recent: tailRecent}
	if ptr.Deref(request.Params.From, api.Recent) == api.Latest {
		start.Recent = 0
	}
	if resume := ptr.Deref(request.Params.Resume, ""); resume != "" {
		offsets, err := parseResume(resume)
		if err != nil {
			return api.TailTopic400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse(
				problem(http.StatusBadRequest, "Bad resume", err.Error()))}, nil
		}
		start.Resume = offsets
	}
	if !s.tails.acquire(customer.Name) {
		return api.TailTopic429JSONResponse{TooManyRequestsJSONResponse: api.TooManyRequestsJSONResponse(problem(http.StatusTooManyRequests,
			"Too many tails", fmt.Sprintf("Your organization reads at most %d topics' messages at once; close one, and try again.", tailsPerCustomer)))}, nil
	}
	reader, err := s.messages.Open(ctx, customer.Name, request.Topic, start)
	if err != nil {
		s.tails.release(customer.Name)
		switch {
		case errors.Is(err, ErrNoSuchTopic):
			return api.TailTopic404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(noSuchTopic(request.Topic))}, nil
		case errors.Is(err, clusters.ErrNotReady):
			return api.TailTopic409JSONResponse{NotReadyJSONResponse: api.NotReadyJSONResponse(notReadyYet)}, nil
		}
		return nil, err
	}
	resumed := start.Resume != nil
	s.audit(ctx, principal, customer.Name, "topic tail started", "topic", request.Topic, "recent", start.Recent, "resumed", resumed)
	return &tailStream{
		ctx: ctx, until: principal.Expires, reader: reader,
		decoder: tail.Decoder{Schemas: s.registry(customer.Name), Limit: tailLimit},
		done: func(shown, skipped int64, err error) {
			s.tails.release(customer.Name)
			attributes := []any{"topic", request.Topic, "shown", shown, "skipped", skipped}
			if err != nil {
				attributes = append(attributes, "error", err.Error())
			}
			s.audit(ctx, principal, customer.Name, "topic tail ended", attributes...)
		},
	}, nil
}

// parseResume reads partition:offset pairs.
func parseResume(resume string) (map[int32]int64, error) {
	offsets := map[int32]int64{}
	for pair := range strings.SplitSeq(resume, ",") {
		partition, offset, ok := strings.Cut(pair, ":")
		p, err1 := strconv.ParseInt(partition, 10, 32)
		o, err2 := strconv.ParseInt(offset, 10, 64)
		if !ok || err1 != nil || err2 != nil || p < 0 || o < 0 {
			return nil, fmt.Errorf("%q is no partition:offset pair", pair)
		}
		offsets[int32(p)] = o
	}
	return offsets, nil
}

// registry returns the customer's schema registry, which only customers
// with Postgres sources have; the decoder then shows their Avro as JSON.
func (s *Server) registry(customerID string) tail.Schemas {
	s.registriesMu.Lock()
	defer s.registriesMu.Unlock()
	if r, ok := s.registries[customerID]; ok {
		return r
	}
	url := fmt.Sprintf("http://%s.%s.svc:%d", provisioning.Registry, namespaceOf(customerID), provisioning.RegistryPort)
	if s.registryURL != nil {
		url = s.registryURL(customerID)
	}
	r := &tail.Registry{URL: url}
	s.registries[customerID] = r
	return r
}

// tailStream streams a tail's messages as server-sent events until the
// session token expires, the client goes away, or reading fails, keeping
// to the tail's rate.
type tailStream struct {
	ctx     context.Context //nolint:containedctx // the request's, which the response outlives otherwise
	until   time.Time
	reader  MessageReader
	decoder tail.Decoder
	// done is told how many messages were shown and skipped, once.
	done func(shown, skipped int64, err error)
}

// VisitTailTopicResponse implements api.TailTopicResponseObject.
func (t *tailStream) VisitTailTopicResponse(w http.ResponseWriter) error {
	var shown, skipped int64
	var failed error
	defer func() {
		t.reader.Close()
		t.done(shown, skipped, failed)
	}()
	flusher, ok := w.(http.Flusher)
	if !ok {
		return errors.New("the response cannot stream")
	}
	ctx, cancel := context.WithDeadline(t.ctx, t.until)
	defer cancel()
	_ = http.NewResponseController(w).SetWriteDeadline(t.until.Add(keepAlive))
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	send := func(event string, data any) error {
		encoded, err := json.Marshal(data)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, encoded); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}
	// The recent messages the tail starts with all show; then a second's
	// worth at most, refilled as time passes.
	allowance, last := float64(max(tailRate, tailRecent)), time.Now()
	reportedSkipped := int64(0)
	quiet := time.Now()
	for ctx.Err() == nil {
		poll, cancelPoll := context.WithTimeout(ctx, time.Second)
		messages, err := t.reader.Poll(poll)
		cancelPoll()
		if err != nil {
			failed = err
			_ = send("failed", api.Problem{Status: http.StatusBadGateway, Title: "Reading the topic failed", Detail: ptr.To(err.Error())})
			return nil
		}
		now := time.Now()
		allowance = max(allowance, min(float64(tailRate), allowance+now.Sub(last).Seconds()*tailRate))
		last = now
		for _, m := range messages {
			if allowance < 1 {
				skipped++
				continue
			}
			allowance--
			if err := send("message", t.toAPI(ctx, m)); err != nil {
				return nil //nolint:nilerr // the client went away
			}
			shown++
			quiet = now
		}
		if skipped != reportedSkipped {
			if err := send("skipped", api.TailSkipped{Skipped: skipped}); err != nil {
				return nil //nolint:nilerr // the client went away
			}
			reportedSkipped, quiet = skipped, now
		}
		if now.Sub(quiet) >= keepAlive {
			if _, err := fmt.Fprint(w, ": still here\n\n"); err != nil {
				return nil //nolint:nilerr // the client went away
			}
			flusher.Flush()
			quiet = now
		}
	}
	return nil
}

func (t *tailStream) toAPI(ctx context.Context, m Message) api.TailMessage {
	out := api.TailMessage{
		Partition: m.Partition, Offset: m.Offset, Timestamp: m.Timestamp.UTC(),
		Key: toAPIPayload(t.decoder.Decode(ctx, m.Key)), Value: toAPIPayload(t.decoder.Decode(ctx, m.Value)),
		Headers: []api.TailHeader{},
	}
	for _, h := range m.Headers[:min(len(m.Headers), tailHeaders)] {
		out.Headers = append(out.Headers, api.TailHeader{Key: h.Key, Value: toAPIPayload(t.decoder.Decode(ctx, h.Value))})
	}
	return out
}

func toAPIPayload(p *tail.Payload) *api.TailPayload {
	if p == nil {
		return nil
	}
	out := &api.TailPayload{Format: api.TailPayloadFormat(p.Format), Text: p.Text, Size: p.Size, Truncated: p.Truncated}
	if p.SchemaID != 0 {
		out.SchemaId = ptr.To(int64(p.SchemaID))
	}
	return out
}
