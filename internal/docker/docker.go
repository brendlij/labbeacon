// Package docker uses the local Docker Engine HTTP API, without shelling out.
package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/brendlij/labbeacon/internal/config"
	"github.com/brendlij/labbeacon/internal/metric"
)

type Container struct {
	ImageID         string   `json:"image_id"`
	RestartCount    int      `json:"restart_count"`
	CPUPercent      *float64 `json:"cpu_percent,omitempty"`
	MemoryBytes     *uint64  `json:"memory_bytes,omitempty"`
	MemoryLimit     *uint64  `json:"memory_limit_bytes,omitempty"`
	UpdateAvailable *bool    `json:"image_update_available,omitempty"`
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Status          string   `json:"status"`
	Image           string   `json:"image"`
	StartedAt       string   `json:"started_at"`
	Uptime          float64  `json:"uptime_seconds"`
	Health          string   `json:"health,omitempty"`
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
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("Docker socket missing: mount the host socket at the configured socket_path (default /var/run/docker.sock), then redeploy: %w", err)
		}
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("Docker socket permission denied: add its numeric host group with Compose group_add (stat -c %%g /var/run/docker.sock), then redeploy: %w", err)
		}
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
			RestartCount int
			Image        string
			ID           string `json:"Id"`
			Name         string
			Config       struct{ Image string }
			State        struct {
				Status    string
				StartedAt string
				Health    *struct{ Status string }
			}
		}
		if err := c.get(ctx, "/containers/"+url.PathEscape(item.ID)+"/json", &detail); err != nil {
			return nil, err
		}
		v := Container{ID: detail.ID, Name: strings.TrimPrefix(detail.Name, "/"), Image: detail.Config.Image, ImageID: detail.Image, RestartCount: detail.RestartCount, Status: detail.State.Status, StartedAt: detail.State.StartedAt}
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
	Config   config.Docker
	Snapshot []Container
	Updates  *UpdateChecker
	API      API
	Timeout  time.Duration
}

func (c *Collector) Collect(ctx context.Context) ([]metric.Sample, error) {
	c.Snapshot = nil
	inventoryCtx, cancel := context.WithTimeout(ctx, c.Timeout)
	containers, err := c.API.Containers(inventoryCtx)
	cancel()
	if err != nil {
		return nil, err
	}
	statsResults := c.collectStats(ctx, containers)
	running := 0
	var extra []metric.Sample
	var errs []error
	for i := range containers {
		v := &containers[i]
		if v.Status == "running" {
			running++
		}
		start := len(extra)
		key := "container_" + metric.Key(v.ID)
		status := metric.Sensor(key+"_status", "Status", "", v.Status)
		status.Attributes = map[string]any{"container_id": v.ID, "image": v.Image, "health": v.Health}
		extra = append(extra, status)
		extra = append(extra, metric.Sensor(key+"_restarts", "Restart count", "", v.RestartCount))
		if c.Config.Stats && v.Status == "running" {
			if statsResults != nil {
				stats, e := statsResults[i].stats, statsResults[i].err
				if e != nil {
					errs = append(errs, fmt.Errorf("stats %s: %w", v.Name, e))
				} else {
					v.CPUPercent = stats.CPU
					v.MemoryBytes = &stats.Memory
					v.MemoryLimit = &stats.Limit
					if stats.CPU != nil {
						sample := metric.Sensor(key+"_cpu", "CPU", "%", *stats.CPU)
						sample.StateClass = "measurement"
						extra = append(extra, sample)
					}
					sample := metric.Sensor(key+"_ram", "RAM", "B", stats.Memory)
					sample.StateClass = "measurement"
					extra = append(extra, sample)
				}
			}
		}
		if c.Config.ImageUpdates.Enabled && c.Updates != nil {
			available, e := c.Updates.Check(ctx, v.Image, v.ImageID)
			if e != nil {
				errs = append(errs, fmt.Errorf("image update %s: %w", v.Name, e))
			}
			v.UpdateAvailable = available
			if available != nil {
				sample := metric.Binary(key+"_image_update", "Image update", *available, nil)
				sample.DeviceClass = "update"
				extra = append(extra, sample)
			}
		}
		for j := start; j < len(extra); j++ {
			extra[j].Device = metric.DeviceRef{Kind: "container", Name: v.Name}
		}
	}
	c.Snapshot = containers
	s := metric.Sensor("containers_running", "Containers running", "", running)
	s.Attributes = map[string]any{"running": running, "total": len(containers), "summary": fmt.Sprintf("%d/%d", running, len(containers)), "containers": containers}
	return append([]metric.Sample{s, metric.Sensor("containers_total", "Containers total", "", len(containers))}, extra...), errors.Join(errs...)
}
