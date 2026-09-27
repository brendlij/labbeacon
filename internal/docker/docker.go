// Package docker uses the local Docker Engine HTTP API, without shelling out.
package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"homelab-agent/internal/metric"
)

type Container struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Status    string  `json:"status"`
	Image     string  `json:"image"`
	StartedAt string  `json:"started_at"`
	Uptime    float64 `json:"uptime_seconds"`
	Health    string  `json:"health,omitempty"`
}
type API interface {
	Containers(context.Context) ([]Container, error)
}
type Client struct{ http *http.Client }

func NewClient(socket string, timeout time.Duration) *Client {
	return &Client{http: &http.Client{Timeout: timeout, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}}}
}
func (c *Client) get(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker"+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Docker API %s: HTTP %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}
func (c *Client) Containers(ctx context.Context) ([]Container, error) {
	var listed []struct {
		ID string `json:"Id"`
	}
	if err := c.get(ctx, "/containers/json?all=1", &listed); err != nil {
		return nil, err
	}
	out := make([]Container, 0, len(listed))
	for _, item := range listed {
		var detail struct {
			ID     string `json:"Id"`
			Name   string
			Config struct{ Image string }
			State  struct {
				Status    string
				StartedAt string
				Health    *struct{ Status string }
			}
		}
		if err := c.get(ctx, "/containers/"+url.PathEscape(item.ID)+"/json", &detail); err != nil {
			return nil, err
		}
		v := Container{ID: detail.ID, Name: strings.TrimPrefix(detail.Name, "/"), Image: detail.Config.Image, Status: detail.State.Status, StartedAt: detail.State.StartedAt}
		if detail.State.Health != nil {
			v.Health = detail.State.Health.Status
		}
		if v.Status == "running" || v.Status == "paused" {
			started, err := time.Parse(time.RFC3339Nano, v.StartedAt)
			if err != nil {
				return nil, fmt.Errorf("container %s started_at: %w", v.Name, err)
			}
			if !started.IsZero() {
				v.Uptime = max(0, time.Since(started).Seconds())
			}
		}
		out = append(out, v)
	}
	return out, nil
}

type Collector struct {
	API     API
	Timeout time.Duration
}

func (c *Collector) Collect(ctx context.Context) ([]metric.Sample, error) {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	containers, err := c.API.Containers(ctx)
	if err != nil {
		return nil, err
	}
	running := 0
	for _, v := range containers {
		if v.Status == "running" {
			running++
		}
	}
	s := metric.Sensor("containers_running", "Containers running", "", running)
	s.Attributes = map[string]any{"running": running, "total": len(containers), "summary": fmt.Sprintf("%d/%d", running, len(containers)), "containers": containers}
	return []metric.Sample{s, metric.Sensor("containers_total", "Containers total", "", len(containers))}, nil
}
