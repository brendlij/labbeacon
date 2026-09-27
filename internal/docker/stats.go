package docker

import "context"
import "net/url"

type cpuSnapshot struct {
	Usage struct {
		Total  uint64   `json:"total_usage"`
		PerCPU []uint64 `json:"percpu_usage"`
	} `json:"cpu_usage"`
	System uint64 `json:"system_cpu_usage"`
	Online uint64 `json:"online_cpus"`
}
type statsResponse struct {
	CPU      cpuSnapshot `json:"cpu_stats"`
	Previous cpuSnapshot `json:"precpu_stats"`
	Memory   struct {
		Usage uint64            `json:"usage"`
		Limit uint64            `json:"limit"`
		Stats map[string]uint64 `json:"stats"`
	} `json:"memory_stats"`
}
type Stats struct {
	CPU           *float64
	Memory, Limit uint64
}

func decodeStats(s statsResponse) Stats {
	out := Stats{Memory: s.Memory.Usage, Limit: s.Memory.Limit}
	cache := s.Memory.Stats["inactive_file"]
	if v, ok := s.Memory.Stats["total_inactive_file"]; ok {
		cache = v
	}
	if cache <= out.Memory {
		out.Memory -= cache
	}
	cpus := s.CPU.Online
	if cpus == 0 {
		cpus = uint64(len(s.CPU.Usage.PerCPU))
	}
	if s.CPU.Usage.Total >= s.Previous.Usage.Total && s.CPU.System > s.Previous.System && s.Previous.System > 0 && cpus > 0 {
		v := float64(s.CPU.Usage.Total-s.Previous.Usage.Total) / float64(s.CPU.System-s.Previous.System) * float64(cpus) * 100
		out.CPU = &v
	}
	return out
}
func (c *Client) Stats(ctx context.Context, id string) (Stats, error) {
	var s statsResponse
	err := c.get(ctx, "/containers/"+url.PathEscape(id)+"/stats?stream=false", &s)
	return decodeStats(s), err
}
