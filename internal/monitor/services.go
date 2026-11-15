package monitor

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

type Service struct {
	Available bool       `json:"available"`
	Stale     bool       `json:"stale"`
	SampledAt time.Time  `json:"sampled_at"`
	CheckedAt time.Time  `json:"checked_at"`
	ErrorCode string     `json:"error_code,omitempty"`
	Resources *Resources `json:"resources,omitempty"`
}

func ResourceService(r Resources) Service {
	return Service{Available: r.Available && !r.Stale, Stale: r.Stale,
		SampledAt: r.SampledAt, CheckedAt: time.Now().UTC(), ErrorCode: r.ErrorCode, Resources: &r}
}

type ServiceReader struct{ client *http.Client }

func NewServiceReader() *ServiceReader {
	return &ServiceReader{client: &http.Client{Timeout: time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func serviceFailure(old Service, now time.Time, code string) Service {
	old.Available, old.ErrorCode, old.CheckedAt = false, code, now
	old.Stale = old.SampledAt.IsZero() || now.Sub(old.SampledAt) > 15*time.Second
	return old
}

func (r *ServiceReader) Heatbot(ctx context.Context, old Service) Service {
	return r.heatbot(ctx, old, "http://heatbot:8090/internal/v1/metrics")
}

func (r *ServiceReader) heatbot(ctx context.Context, old Service, target string) Service {
	now := time.Now().UTC()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	response, err := r.client.Do(req)
	if err != nil {
		return serviceFailure(old, now, "heatbot_unreachable")
	}
	defer response.Body.Close()
	fail := func() Service { return serviceFailure(old, now, "heatbot_sample_invalid") }
	if response.StatusCode != http.StatusOK {
		return fail()
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 16*1024+1))
	if err != nil || len(raw) > 16*1024 {
		return fail()
	}
	var resource Resources
	if json.Unmarshal(raw, &resource) != nil || resource.Source != "heatbot_self" ||
		resource.SampledAt.After(now.Add(5*time.Second)) {
		return fail()
	}
	current := ResourceService(resource)
	current.CheckedAt = now
	current.Stale = resource.SampledAt.IsZero() || now.Sub(resource.SampledAt) > 15*time.Second
	current.Available = resource.Available && !current.Stale
	if !current.Available {
		current.ErrorCode = "heatbot_sample_unavailable"
	}
	return current
}

func (r *ServiceReader) Web(ctx context.Context, old Service) Service {
	return r.web(ctx, old, "http://web:8080/")
}

func (r *ServiceReader) web(ctx context.Context, old Service, target string) Service {
	now := time.Now().UTC()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, target, nil)
	if err != nil {
		return serviceFailure(old, now, "web_unreachable")
	}
	response, err := r.client.Do(req)
	if err != nil {
		return serviceFailure(old, now, "web_unreachable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return serviceFailure(old, now, "web_unreachable")
	}
	return Service{Available: true, SampledAt: now, CheckedAt: now}
}
