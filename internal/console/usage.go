package console

import (
	"cmp"
	"context"
	"math"
	"net/http"
	"slices"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/ReneP92/changeport/api/v1alpha1"
	"github.com/ReneP92/changeport/internal/console/api"
	"github.com/ReneP92/changeport/internal/metrics"
	"github.com/ReneP92/changeport/internal/topics"
)

// usageDays is how far back usage goes: the last 30 days in UTC, today
// included, which the metrics store keeps (docs/console.md).
const usageDays = 30

// usagePeriod is the start of the oldest of the usage days, and of today,
// in UTC.
func usagePeriod(now time.Time) (start, today time.Time) {
	today = now.UTC().Truncate(24 * time.Hour)
	return today.AddDate(0, 0, -(usageDays - 1)), today
}

// since is the window from start to now, at least the second a query's
// window needs; increase counts from the sample before it.
func since(start, now time.Time) time.Duration {
	return max(now.Sub(start), time.Second)
}

// GetUsage implements api.StrictServerInterface.
func (s *Server) GetUsage(ctx context.Context, _ api.GetUsageRequestObject) (api.GetUsageResponseObject, error) {
	customer, _, refused, err := s.customerOf(ctx)
	switch {
	case err != nil:
		return nil, err
	case refused != nil && refused.Status == http.StatusNotFound:
		return api.GetUsage404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(*refused)}, nil
	case refused != nil:
		return api.GetUsage403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(*refused)}, nil
	}
	usage, err := s.usage(ctx, customer.Name, time.Now())
	if err != nil {
		return nil, err
	}
	return api.GetUsage200JSONResponse(usage), nil
}

// usage reads a cluster's usage from the metrics: each full day's bytes from
// a daily range, today's so far, the whole period's per topic, and what the
// topics store now. The platform's topics are left out.
func (s *Server) usage(ctx context.Context, tenant string, now time.Time) (api.Usage, error) {
	start, today := usagePeriod(now)
	day := metrics.Filter{Window: 24 * time.Hour}
	period := metrics.Filter{Window: since(start, now)}
	sofar := metrics.Filter{Window: since(today, now)}
	queries := []struct {
		q      metrics.Query
		filter metrics.Filter
		// daily ranges over the full days, ending at each one's end.
		daily bool
	}{
		{metrics.UsageIn, day, true},
		{metrics.UsageOut, day, true},
		{metrics.UsageIn, sofar, false},
		{metrics.UsageOut, sofar, false},
		{metrics.UsageIn, period, false},
		{metrics.UsageOut, period, false},
		{metrics.LocalSize, metrics.Filter{}, false},
		{metrics.RemoteSize, metrics.Filter{}, false},
	}
	results := make([][]metrics.Series, len(queries))
	group, ctx := errgroup.WithContext(ctx)
	for i, query := range queries {
		group.Go(func() error {
			var err error
			if query.daily {
				// A point at each midnight counts the day before it.
				results[i], err = s.metrics.Range(ctx, tenant, query.q, query.filter, start.Add(24*time.Hour), today, 24*time.Hour)
			} else {
				results[i], err = s.metrics.Instant(ctx, tenant, query.q, query.filter, now)
			}
			return err
		})
	}
	if err := group.Wait(); err != nil {
		return api.Usage{}, err
	}
	dailyIn, dailyOut, todayIn, todayOut, periodIn, periodOut, local, remote :=
		results[0], results[1], results[2], results[3], results[4], results[5], results[6], results[7]

	usage := api.Usage{Days: make([]api.UsageDay, usageDays), Topics: []api.TopicUsage{}}
	for i := range usage.Days {
		usage.Days[i].Date.Time = start.AddDate(0, 0, i)
	}
	// The day a point at midnight ends.
	index := func(t time.Time) int { return int(t.UTC().Sub(start)/(24*time.Hour)) - 1 }
	addDaily := func(series []metrics.Series, field func(*api.UsageDay) *int64) {
		for _, one := range series {
			if topics.Reserved(one.Labels["topic"]) {
				continue
			}
			for _, point := range one.Points {
				if i := index(point.Time); i >= 0 && i < usageDays-1 {
					*field(&usage.Days[i]) += wholeBytes(point.Value)
				}
			}
		}
	}
	addDaily(dailyIn, func(d *api.UsageDay) *int64 { return &d.BytesIn })
	addDaily(dailyOut, func(d *api.UsageDay) *int64 { return &d.BytesOut })
	last := &usage.Days[usageDays-1]
	last.BytesIn, last.BytesOut = sumBytes(todayIn), sumBytes(todayOut)

	byTopic := map[string]*api.TopicUsage{}
	topic := func(name string) *api.TopicUsage {
		if t, ok := byTopic[name]; ok {
			return t
		}
		t := &api.TopicUsage{Topic: name}
		byTopic[name] = t
		return t
	}
	for _, field := range []struct {
		series []metrics.Series
		value  func(*api.TopicUsage) *int64
	}{
		{periodIn, func(t *api.TopicUsage) *int64 { return &t.BytesIn }},
		{periodOut, func(t *api.TopicUsage) *int64 { return &t.BytesOut }},
		{local, func(t *api.TopicUsage) *int64 { return &t.LocalBytes }},
		{remote, func(t *api.TopicUsage) *int64 { return &t.RemoteBytes }},
	} {
		for _, one := range field.series {
			name := one.Labels["topic"]
			if name == "" || topics.Reserved(name) {
				continue
			}
			*field.value(topic(name)) += wholeBytes(value(one))
		}
	}
	for _, t := range byTopic {
		usage.Topics = append(usage.Topics, *t)
		usage.BytesIn += t.BytesIn
		usage.BytesOut += t.BytesOut
		usage.LocalBytes += t.LocalBytes
		usage.RemoteBytes += t.RemoteBytes
	}
	slices.SortFunc(usage.Topics, func(a, b api.TopicUsage) int {
		return cmp.Or(cmp.Compare(b.BytesIn+b.BytesOut, a.BytesIn+a.BytesOut), cmp.Compare(a.Topic, b.Topic))
	})
	return usage, nil
}

// periodUsage is the bytes a cluster processed over the usage period, on its
// customer's topics.
func (s *Server) periodUsage(ctx context.Context, tenant string, now time.Time) (in, out int64, err error) {
	start, _ := usagePeriod(now)
	filter := metrics.Filter{Window: since(start, now)}
	for q, total := range map[metrics.Query]*int64{metrics.UsageIn: &in, metrics.UsageOut: &out} {
		series, err := s.metrics.Instant(ctx, tenant, q, filter, now)
		if err != nil {
			return 0, 0, err
		}
		*total = sumBytes(series)
	}
	return in, out, nil
}

// customersUsage is each customer's usage over the period, in their order;
// one the metrics store does not answer for is not known, and the others
// still are.
func (s *Server) customersUsage(ctx context.Context, customers []v1alpha1.Customer) []api.EnvironmentUsage {
	now := time.Now()
	usage := make([]api.EnvironmentUsage, len(customers))
	var group errgroup.Group
	group.SetLimit(8)
	for i := range customers {
		group.Go(func() error {
			usage[i].CustomerID = customers[i].Name
			in, out, err := s.periodUsage(ctx, customers[i].Name, now)
			if err != nil {
				s.logger.WarnContext(ctx, "the metrics store did not answer", "tenant", customers[i].Name, "error", err)
				return nil
			}
			usage[i].BytesIn, usage[i].BytesOut = &in, &out
			return nil
		})
	}
	_ = group.Wait()
	return usage
}

// ListEnvironmentUsage implements api.StrictServerInterface: the usage of
// each of the organization's environments, for all its members.
func (s *Server) ListEnvironmentUsage(ctx context.Context, _ api.ListEnvironmentUsageRequestObject) (api.ListEnvironmentUsageResponseObject, error) {
	organization, refused, err := s.environmentCaller(ctx, false)
	if err != nil {
		return nil, err
	}
	if refused != nil {
		return api.ListEnvironmentUsage403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(*refused)}, nil
	}
	return api.ListEnvironmentUsage200JSONResponse{Environments: s.customersUsage(ctx, organization.customers)}, nil
}

// ListCustomerUsage implements api.StrictServerInterface: every customer's
// usage, for operators.
func (s *Server) ListCustomerUsage(ctx context.Context, _ api.ListCustomerUsageRequestObject) (api.ListCustomerUsageResponseObject, error) {
	principal, err := mustPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if !s.operator(principal) {
		return api.ListCustomerUsage403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(operatorsOnly)}, nil
	}
	customers, err := s.customers.List(ctx)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(customers, func(a, b v1alpha1.Customer) int { return cmp.Compare(a.Name, b.Name) })
	return api.ListCustomerUsage200JSONResponse{Customers: s.customersUsage(ctx, customers)}, nil
}

// sumBytes adds up per-topic series' latest values, without the platform's
// topics.
func sumBytes(series []metrics.Series) int64 {
	var total int64
	for _, one := range series {
		if name := one.Labels["topic"]; name != "" && !topics.Reserved(name) {
			total += wholeBytes(value(one))
		}
	}
	return total
}

// wholeBytes rounds a metric's value to whole bytes.
func wholeBytes(v float64) int64 {
	if math.IsNaN(v) || v < 0 {
		return 0
	}
	return int64(math.Round(v))
}
