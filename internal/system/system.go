package system

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	gnet "github.com/shirou/gopsutil/v4/net"

	"homelab-agent/internal/config"
	"homelab-agent/internal/metric"
)

type Collector struct {
	Options         config.System
	processPrevious map[int32]processPoint
	Paths           []string
	previous        map[string]gnet.IOCountersStat
	last            time.Time
}

func New(paths []string) *Collector { return &Collector{Paths: paths} }
func (c *Collector) Collect(ctx context.Context) ([]metric.Sample, error) {
	var out []metric.Sample
	var errs []error
	add := func(k, n, u string, v any) {
		s := metric.Sensor(k, n, u, v)
		if u != "" {
			s.StateClass = "measurement"
		}
		out = append(out, s)
	}
	check := func(label string, e error) bool {
		if e != nil {
			errs = append(errs, fmt.Errorf("%s: %w", label, e))
			return false
		}
		return true
	}
	p, e := cpu.PercentWithContext(ctx, 200*time.Millisecond, false)
	if check("cpu", e) && len(p) > 0 {
		add("cpu_percent", "CPU usage", "%", p[0])
	}
	n, e := cpu.CountsWithContext(ctx, true)
	if check("cores", e) {
		add("cpu_cores", "CPU cores", "", n)
	}
	info, e := cpu.InfoWithContext(ctx)
	if check("cpu model", e) && len(info) > 0 {
		add("cpu_model", "CPU model", "", info[0].ModelName)
	}
	avg, e := load.AvgWithContext(ctx)
	if check("load", e) {
		add("load_1", "Load 1 minute", "", avg.Load1)
		add("load_5", "Load 5 minutes", "", avg.Load5)
		add("load_15", "Load 15 minutes", "", avg.Load15)
	}
	ram, e := mem.VirtualMemoryWithContext(ctx)
	if check("memory", e) {
		add("ram_used", "RAM used", "B", ram.Used)
		add("ram_total", "RAM total", "B", ram.Total)
		add("ram_percent", "RAM usage", "%", ram.UsedPercent)
	}
	swap, e := mem.SwapMemoryWithContext(ctx)
	if check("swap", e) {
		add("swap_used", "Swap used", "B", swap.Used)
		add("swap_total", "Swap total", "B", swap.Total)
		add("swap_percent", "Swap usage", "%", swap.UsedPercent)
	}
	for _, path := range c.Paths {
		d, e := disk.UsageWithContext(ctx, path)
		if check("disk "+path, e) {
			key := "disk_" + metric.Key(path)
			add(key+"_used", "Disk "+path+" used", "B", d.Used)
			add(key+"_total", "Disk "+path+" total", "B", d.Total)
			add(key+"_percent", "Disk "+path+" usage", "%", d.UsedPercent)
		}
	}
	h, e := host.InfoWithContext(ctx)
	if check("host", e) {
		if c.Options.BootTime {
			s := metric.Sensor("boot_time", "Boot time", "", time.Unix(int64(h.BootTime), 0).UTC().Format(time.RFC3339))
			s.DeviceClass = "timestamp"
			out = append(out, s)
		}
		add("uptime", "Uptime", "s", h.Uptime)
		add("hostname", "Hostname", "", h.Hostname)
		add("os", "OS", "", h.OS)
		add("platform", "Platform", "", h.Platform)
		add("kernel", "Kernel version", "", h.KernelVersion)
	}
	counters, e := gnet.IOCountersWithContext(ctx, true)
	if check("network", e) {
		now := time.Now()
		elapsed := now.Sub(c.last).Seconds()
		next := map[string]gnet.IOCountersStat{}
		for _, v := range counters {
			next[v.Name] = v
			if old, ok := c.previous[v.Name]; ok && elapsed > 0 {
				key := "net_" + metric.Key(v.Name)
				add(key+"_rx", "Network "+v.Name+" RX", "B/s", Rate(v.BytesRecv, old.BytesRecv, elapsed))
				add(key+"_tx", "Network "+v.Name+" TX", "B/s", Rate(v.BytesSent, old.BytesSent, elapsed))
			}
		}
		c.previous = next
		c.last = now
	}
	extra, err := c.extended(ctx)
	out = append(out, extra...)
	if err != nil {
		errs = append(errs, err)
	}
	return out, errors.Join(errs...)
}

// Rate treats counter resets as a new baseline rather than a huge spike.
func Rate(current, previous uint64, seconds float64) float64 {
	if current < previous || seconds <= 0 {
		return 0
	}
	return float64(current-previous) / seconds
}
