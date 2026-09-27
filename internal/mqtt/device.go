package mqtt

import (
	"github.com/brendlij/labbeacon/internal/config"
	"github.com/brendlij/labbeacon/internal/metric"
	"github.com/brendlij/labbeacon/internal/version"
)

func discoveryDevice(cfg config.Config, ref metric.DeviceRef) Device {
	d := Device{Identifiers: []string{cfg.Agent.ID}, Name: cfg.Agent.Name, Model: "Server", Manufacturer: "labbeacon", Version: version.Version}
	if ref.Kind == "" {
		return d
	}
	// Namespace children separately from parent IDs and from each other.
	// Names keep a device stable across Docker container recreation.
	d.Identifiers = []string{"labbeacon:" + cfg.Agent.ID + ":" + ref.Kind + ":" + metric.Key(ref.Name)}
	d.Name = cfg.Agent.Name + "_" + ref.Name
	d.ViaDevice = cfg.Agent.ID
	switch ref.Kind {
	case "container":
		d.Model = "Container"
	case "service":
		d.Model = "Service"
	default:
		d.Model = ref.Kind
	}
	return d
}
