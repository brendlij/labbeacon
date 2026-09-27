package agent

import (
	"context"
	"time"

	"github.com/brendlij/labbeacon/internal/metric"
	"github.com/brendlij/labbeacon/internal/version"
)

type Collector struct {
	Started time.Time
	Session func() string
}

func (c *Collector) Collect(context.Context) ([]metric.Sample, error) {
	v := metric.Sensor("agent_version", "Agent version", "", version.Version)
	if c.Session != nil {
		v.Attributes = map[string]any{"control_session": c.Session()}
	}
	up := metric.Sensor("agent_uptime", "Agent uptime", "s", time.Since(c.Started).Seconds())
	up.DeviceClass = "duration"
	up.StateClass = "measurement"
	return []metric.Sample{v, up}, nil
}
