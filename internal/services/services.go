package services

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/brendlij/labbeacon/internal/config"
	"github.com/brendlij/labbeacon/internal/metric"
)

type Collector struct {
	Checks []config.Check
	Client *http.Client
}

func New(checks []config.Check) *Collector {
	return &Collector{Checks: checks, Client: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (c *Collector) Collect(ctx context.Context) ([]metric.Sample, error) {
	results := make([]*metric.Sample, len(c.Checks))
	jobs := make(chan int, len(c.Checks))
	for i := range c.Checks {
		jobs <- i
	}
	close(jobs)
	var wg sync.WaitGroup
	for range min(8, len(c.Checks)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					return
				}
				s := c.check(ctx, c.Checks[i])
				if ctx.Err() == nil {
					results[i] = &s
				}
			}
		}()
	}
	wg.Wait()
	out := make([]metric.Sample, 0, len(results))
	for _, s := range results {
		if s != nil {
			out = append(out, *s)
		}
	}
	return out, ctx.Err()
}
func (c *Collector) check(parent context.Context, ch config.Check) metric.Sample {
	ctx, cancel := context.WithTimeout(parent, ch.Timeout)
	defer cancel()
	start := time.Now()
	online := false
	attrs := map[string]any{"checked_at": start.UTC().Format(time.RFC3339Nano), "check_type": ch.Type}
	switch ch.Type {
	case "http":
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ch.URL, nil)
		if err == nil {
			resp, e := c.Client.Do(req)
			err = e
			if resp != nil {
				attrs["status_code"] = resp.StatusCode
				online = resp.StatusCode == ch.ExpectedStatus
				if closeErr := resp.Body.Close(); closeErr != nil && err == nil {
					err = closeErr
				}
			}
		}
		if err != nil {
			online = false
			attrs["error"] = "HTTP request failed"
		}
	case "tcp":
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(ch.Host, strconv.Itoa(ch.Port)))
		if err == nil {
			online = true
			if err = conn.Close(); err != nil {
				online = false
			}
		}
		if err != nil {
			attrs["error"] = "TCP connection failed"
		}
	}
	attrs["response_time_ms"] = float64(time.Since(start).Microseconds()) / 1000
	return metric.Binary("service_"+metric.Key(ch.Name), ch.Name, online, attrs)
}
