package docker

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"runtime"
	"strings"

	"github.com/brendlij/labbeacon/internal/control"
	"github.com/brendlij/labbeacon/internal/metric"
)

type Controller interface {
	ControlReady(context.Context) error
	CheckTarget(context.Context, string, string) error
	Control(context.Context, string, string) error
}

func (c *Client) ControlReady(ctx context.Context) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("Docker control requires a Linux Unix socket")
	}
	// GET verifies socket permissions and daemon reachability without mutations.
	var version map[string]any
	if err := c.get(ctx, "/version", &version); err != nil {
		return fmt.Errorf("Docker control unavailable: socket/daemon access: %w", err)
	}
	return nil
}
func (c *Client) CheckTarget(ctx context.Context, id, name string) error {
	var detail struct {
		ID   string `json:"Id"`
		Name string
	}
	if err := c.get(ctx, "/containers/"+url.PathEscape(id)+"/json", &detail); err != nil {
		return err
	}
	if detail.ID != id || strings.TrimPrefix(detail.Name, "/") != name {
		return fmt.Errorf("container identity/name changed; wait for inventory refresh")
	}
	return nil
}
func (c *Client) Control(ctx context.Context, id, op string) error {
	switch op {
	case "start", "stop", "restart":
	default:
		return fmt.Errorf("unsupported Docker action")
	}
	path := "/containers/" + url.PathEscape(id) + "/" + op
	if op != "start" {
		path += "?t=10"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://docker"+path, nil)
	if err != nil {
		return err
	}
	client := *c.http
	client.Timeout = 0 // Execution is bounded by the action context, not the shorter polling timeout.
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotModified {
		return fmt.Errorf("Docker %s denied or failed: HTTP %d", op, resp.StatusCode)
	}
	return nil
}
func (c *Collector) Actions(ctx context.Context) ([]control.Entry, error) {
	if !c.Config.ControlContainers.Enabled {
		return nil, nil
	}
	api, ok := c.API.(Controller)
	if !ok {
		return nil, fmt.Errorf("Docker client has no control support")
	}
	if err := api.ControlReady(ctx); err != nil {
		return nil, err
	}
	var out []control.Entry
	for _, v := range c.Snapshot {
		if !c.Config.ControlContainers.Permits(v.Name) {
			continue
		}
		for _, op := range []string{"start", "stop", "restart"} {
			out = append(out, control.Entry{Name: v.Name + " " + op, Module: "docker", Check: func(ctx context.Context) error {
				if !c.Config.ControlContainers.Permits(v.Name) {
					return fmt.Errorf("container no longer allowed")
				}
				return api.CheckTarget(ctx, v.ID, v.Name)
			}, Action: control.Function{Key: "container_" + metric.Key(v.ID) + "_" + op, Run: func(ctx context.Context) error { return api.Control(ctx, v.ID, op) }}})
		}
	}
	return out, nil
}
