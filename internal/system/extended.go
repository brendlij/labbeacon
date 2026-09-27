package system

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/process"
	"github.com/shirou/gopsutil/v4/sensors"

	"github.com/brendlij/labbeacon/internal/metric"
)

type ProcessInfo struct {
	PID  int32    `json:"pid"`
	Name string   `json:"name"`
	CPU  *float64 `json:"cpu_percent,omitempty"`
	RSS  uint64   `json:"rss_bytes"`
}
type processPoint struct {
	born int64
	cpu  float64
	at   time.Time
}

func (c *Collector) processes(ctx context.Context) ([]metric.Sample, error) {
	processes, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil, err
	}
	out := []metric.Sample{metric.Sensor("process_count", "Process count", "", len(processes))}
	next := map[int32]processPoint{}
	rows := []ProcessInfo{}
	fds, fdVisible, visible := int64(0), 0, 0
	for _, p := range processes {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		if c.Options.FileDescriptors {
			n, e := p.NumFDsWithContext(ctx)
			if e == nil {
				fds += int64(n)
				fdVisible++
			}
		}
		if !c.Options.Processes {
			continue
		}
		name, e := p.NameWithContext(ctx)
		if e != nil {
			continue
		}
		memory, e := p.MemoryInfoWithContext(ctx)
		if e != nil {
			continue
		}
		visible++
		row := ProcessInfo{PID: p.Pid, Name: name, RSS: memory.RSS}
		times, e := p.TimesWithContext(ctx)
		born, bornErr := p.CreateTimeWithContext(ctx)
		if e == nil && bornErr == nil {
			now := time.Now()
			total := times.User + times.System
			next[p.Pid] = processPoint{born: born, cpu: total, at: now}
			if old, ok := c.processPrevious[p.Pid]; ok && old.born == born && total >= old.cpu {
				value := (total - old.cpu) / now.Sub(old.at).Seconds() * 100
				row.CPU = &value
			}
		}
		rows = append(rows, row)
	}
	c.processPrevious = next
	if c.Options.FileDescriptors && fdVisible > 0 {
		s := metric.Sensor("open_file_descriptors", "Open file descriptors (visible processes)", "", fds)
		s.Attributes = map[string]any{"observed_processes": fdVisible, "total_processes": len(processes), "partial": fdVisible < len(processes)}
		out = append(out, s)
	}
	if c.Options.Processes {
		byRAM := append([]ProcessInfo{}, rows...)
		sort.Slice(byRAM, func(i, j int) bool { return byRAM[i].RSS > byRAM[j].RSS })
		byRAM = byRAM[:min(c.Options.TopN, len(byRAM))]
		byCPU := []ProcessInfo{}
		for _, row := range rows {
			if row.CPU != nil {
				byCPU = append(byCPU, row)
			}
		}
		sort.Slice(byCPU, func(i, j int) bool { return *byCPU[i].CPU > *byCPU[j].CPU })
		byCPU = byCPU[:min(c.Options.TopN, len(byCPU))]
		s := metric.Sensor("top_processes", "Top processes", "", visible)
		s.Attributes = map[string]any{"top_cpu": byCPU, "top_ram": byRAM, "total_processes": len(processes), "observed_processes": visible, "cpu_basis": "interval; 100 percent per logical core", "partial": visible < len(processes)}
		out = append(out, s)
	}
	return out, nil
}
func cpuSensor(name string) bool {
	n := strings.ToLower(name)
	for _, hint := range []string{"coretemp", "k10temp", "cpu", "package", "tctl", "tdie", "x86_pkg_temp"} {
		if strings.Contains(n, hint) {
			return true
		}
	}
	return false
}
func temperatures(ctx context.Context) ([]metric.Sample, error) {
	readings, err := sensors.TemperaturesWithContext(ctx)
	var values []sensors.TemperatureStat
	for _, s := range readings {
		if cpuSensor(s.SensorKey) && s.Temperature > 0 && s.Temperature < 150 {
			values = append(values, s)
		}
	}
	if len(values) == 0 && runtime.GOOS == "linux" {
		root := os.Getenv("HOST_SYS")
		if root == "" {
			root = "/sys"
		}
		paths, e := filepath.Glob(filepath.Join(root, "class", "thermal", "thermal_zone*"))
		if e != nil {
			return nil, e
		}
		for _, p := range paths {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			kind, e := os.ReadFile(filepath.Join(p, "type"))
			if e != nil || !cpuSensor(string(kind)) {
				continue
			}
			data, e := os.ReadFile(filepath.Join(p, "temp"))
			if e != nil {
				continue
			}
			v, e := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
			if e == nil && v > 0 && v < 150000 {
				values = append(values, sensors.TemperatureStat{SensorKey: strings.TrimSpace(string(kind)), Temperature: v / 1000})
			}
		}
	}
	if len(values) == 0 {
		if err == nil {
			err = fmt.Errorf("no readable CPU temperature sensor")
		}
		return nil, err
	}
	hottest := values[0].Temperature
	for _, v := range values {
		hottest = max(hottest, v.Temperature)
	}
	sample := metric.Sensor("cpu_temperature", "CPU temperature", "°C", hottest)
	sample.DeviceClass = "temperature"
	sample.StateClass = "measurement"
	sample.Attributes = map[string]any{"sensors": values}
	return []metric.Sample{sample}, nil
}
func (c *Collector) extended(ctx context.Context) ([]metric.Sample, error) {
	var out []metric.Sample
	var errs []error
	if c.Options.Temperature {
		s, e := temperatures(ctx)
		out = append(out, s...)
		if e != nil {
			errs = append(errs, e)
		}
	}
	if c.Options.Processes || c.Options.FileDescriptors {
		s, e := c.processes(ctx)
		out = append(out, s...)
		if e != nil {
			errs = append(errs, e)
		}
	}
	return out, errors.Join(errs...)
}
