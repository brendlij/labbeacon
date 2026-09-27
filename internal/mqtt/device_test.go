package mqtt

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/brendlij/labbeacon/internal/config"
	"github.com/brendlij/labbeacon/internal/control"
	"github.com/brendlij/labbeacon/internal/metric"
)

func TestChildDiscoveryPreservesEntityIdentity(t *testing.T) {
	cfg := config.Defaults()
	cfg.Agent.ID, cfg.Agent.Name = "server-id", "ser5-01"
	for _, kind := range []string{"container", "service"} {
		s := metric.Sensor("existing_key", "CPU", "%", 1.234)
		old := Discovery(cfg, s)
		s.Device = metric.DeviceRef{Kind: kind, Name: "paperlessngx-broker-1"}
		d := Discovery(cfg, s)
		for _, key := range []string{"unique_id", "default_entity_id", "state_topic", "availability_topic"} {
			if d[key] != old[key] {
				t.Fatalf("migration changed %s", key)
			}
		}
		device := d["device"].(Device)
		if device.Name != "ser5-01_paperlessngx-broker-1" || device.ViaDevice != cfg.Agent.ID || device.Identifiers[0] == cfg.Agent.ID {
			t.Fatalf("bad child: %+v", device)
		}
		e := control.Entry{Name: "restart", Device: s.Device, Action: control.Function{Key: "existing_restart"}}
		if !reflect.DeepEqual(ButtonDiscovery(cfg, e, "nonce")["device"], device) {
			t.Fatal("button and sensor separated")
		}
		publisher := &fakePublisher{}
		if err := PublishSample(context.Background(), publisher, cfg, s); err != nil {
			t.Fatal(err)
		}
		if publisher.calls[0].topic != "homeassistant/sensor/server-id/existing_key/config" {
			t.Fatal("discovery topic changed")
		}
		var state struct{ Attributes map[string]any }
		if err := json.Unmarshal(publisher.calls[1].payload.([]byte), &state); err != nil {
			t.Fatal(err)
		}
		if state.Attributes["type"] != kind || state.Attributes["server"] != "ser5-01" {
			t.Fatal("missing type/server attributes")
		}
	}
}

func TestDeviceNamespacesAndRenaming(t *testing.T) {
	cfg := config.Defaults()
	cfg.Agent.ID, cfg.Agent.Name = "srv", "Server"
	parent := discoveryDevice(cfg, metric.DeviceRef{})
	a := discoveryDevice(cfg, metric.DeviceRef{Kind: "container", Name: "web"})
	b := discoveryDevice(cfg, metric.DeviceRef{Kind: "service", Name: "web"})
	if parent.Model != "Server" || parent.ViaDevice != "" || a.Model != "Container" || b.Model != "Service" {
		t.Fatal("incorrect types/topology")
	}
	if a.Identifiers[0] == b.Identifiers[0] {
		t.Fatal("container/service identity collision")
	}
	cfg.Agent.Name = "Renamed"
	renamed := discoveryDevice(cfg, metric.DeviceRef{Kind: "container", Name: "web"})
	if renamed.Identifiers[0] != a.Identifiers[0] || renamed.Name != "Renamed_web" {
		t.Fatal("display rename changed identity")
	}
	cfg.Agent.ID = "another-server"
	other := discoveryDevice(cfg, metric.DeviceRef{Kind: "container", Name: "web"})
	if other.Identifiers[0] == a.Identifiers[0] {
		t.Fatal("cross-server collision")
	}
}
