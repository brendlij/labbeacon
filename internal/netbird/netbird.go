package netbird

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"homelab-agent/internal/command"
	"homelab-agent/internal/metric"
)

type Collector struct {
	Runner  command.Runner
	Command string
	Timeout time.Duration
}

func (c *Collector) Collect(ctx context.Context) ([]metric.Sample, error) {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	data, err := c.Runner.Run(ctx, c.Command, "status", "--json")
	if err != nil {
		return nil, fmt.Errorf("netbird status: %w", err)
	}
	return Parse(data)
}
func Parse(data []byte) ([]metric.Sample, error) {
	var s struct {
		IP         string `json:"netbirdIp"`
		Management *struct {
			Connected bool `json:"connected"`
		} `json:"management"`
		Signal *struct {
			Connected bool `json:"connected"`
		} `json:"signal"`
		Peers struct {
			Total     int `json:"total"`
			Connected int `json:"connected"`
		} `json:"peers"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	if s.Management == nil || s.Signal == nil {
		return nil, fmt.Errorf("netbird: missing management/signal status")
	}
	return []metric.Sample{metric.Binary("netbird_online", "NetBird online", s.Management.Connected && s.Signal.Connected, map[string]any{"ip": s.IP, "peers_total": s.Peers.Total, "management_connected": s.Management.Connected, "signal_connected": s.Signal.Connected}), metric.Sensor("netbird_ip", "NetBird IP", "", s.IP), metric.Sensor("netbird_peers", "NetBird connected peers", "", s.Peers.Connected)}, nil
}
