package tailscale

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/brendlij/labbeacon/internal/command"
	"github.com/brendlij/labbeacon/internal/metric"
)

type Collector struct {
	Runner     command.Runner
	SocketPath string
	Command    string
	Timeout    time.Duration
}

func (c *Collector) Collect(ctx context.Context) ([]metric.Sample, error) {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	args := []string{"status", "--json"}
	socket := c.SocketPath
	if socket == "" {
		socket = detectSocket(os.Getenv("HOST_RUN"), os.Stat)
	}
	if socket != "" {
		args = append([]string{"--socket=" + socket}, args...)
	}
	data, err := c.Runner.Run(ctx, c.Command, args...)
	if err != nil {
		return nil, fmt.Errorf("tailscale status: %w. Ensure tailscaled is running and its socket is accessible. For a separate Tailscale container, share its socket directory and set modules.tailscale.socket_path", err)
	}
	return Parse(data)
}
func Parse(data []byte) ([]metric.Sample, error) {
	var s struct {
		BackendState string
		TailscaleIPs []string
		Self         *struct {
			Online         bool
			ExitNodeOption bool
		}
		Peer           map[string]struct{ Online bool }
		ExitNodeStatus *struct {
			ID           string
			Online       bool
			TailscaleIPs []string
		}
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	if s.BackendState == "" {
		return nil, fmt.Errorf("tailscale: missing BackendState")
	}
	peers := 0
	for _, p := range s.Peer {
		if p.Online {
			peers++
		}
	}
	attrs := map[string]any{"ips": s.TailscaleIPs, "backend_state": s.BackendState}
	if s.Self != nil {
		attrs["exit_node_advertised"] = s.Self.ExitNodeOption
	}
	if s.ExitNodeStatus != nil {
		attrs["exit_node"] = s.ExitNodeStatus
	}
	out := []metric.Sample{metric.Binary("tailscale_online", "Tailscale online", s.BackendState == "Running" && s.Self != nil && s.Self.Online, attrs), metric.Sensor("tailscale_peers", "Tailscale online peers", "", peers)}
	if len(s.TailscaleIPs) > 0 {
		out = append(out, metric.Sensor("tailscale_ip", "Tailscale IP", "", s.TailscaleIPs[0]))
	}
	if s.ExitNodeStatus != nil {
		out = append(out, metric.Binary("tailscale_exit_node", "Tailscale exit node online", s.ExitNodeStatus.Online, nil))
	}
	return out, nil
}

// Explicit configuration takes precedence; automatic probing only selects sockets.
func detectSocket(hostRun string, stat func(string) (os.FileInfo, error)) string {
	candidates := []string{"/var/run/tailscale/tailscaled.sock", "/run/tailscale/tailscaled.sock"}
	if hostRun != "" {
		candidates = append(candidates, filepath.Join(hostRun, "tailscale", "tailscaled.sock"))
	}
	for _, path := range candidates {
		if info, err := stat(path); err == nil && info.Mode()&os.ModeSocket != 0 {
			return path
		}
	}
	return ""
}
