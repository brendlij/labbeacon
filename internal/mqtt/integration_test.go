package mqtt

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	broker "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"

	"github.com/brendlij/labbeacon/internal/config"
	"github.com/brendlij/labbeacon/internal/metric"
)

// Runs with an embedded TCP broker by default; CI also runs against Mosquitto.
func TestBrokerIntegration(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	address := os.Getenv("MQTT_TEST_BROKER")
	var server *broker.Server
	if address == "" {
		server = broker.New(&broker.Options{Logger: log})
		if err := server.AddHook(new(auth.AllowHook), nil); err != nil {
			t.Fatal(err)
		}
		listener := listeners.NewTCP(listeners.Config{ID: "test", Address: "127.0.0.1:0"})
		if err := server.AddListener(listener); err != nil {
			t.Fatal(err)
		}
		if err := server.Serve(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := server.Close(); err != nil {
				t.Error(err)
			}
		})
		address = "tcp://" + listener.Address()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cfg := config.Defaults()
	cfg.Agent.ID = "integration"
	cfg.Agent.Name = "Integration"
	cfg.MQTT.Broker = address
	messages := make(chan paho.Message, 32)
	observer := paho.NewClient(paho.NewClientOptions().AddBroker(address).SetClientID("homelab-test-observer").SetDefaultPublishHandler(func(_ paho.Client, m paho.Message) {
		select {
		case messages <- m:
		default:
		}
	}))
	if err := wait(ctx, observer.Connect()); err != nil {
		t.Fatal(err)
	}
	defer observer.Disconnect(100)
	if err := wait(ctx, observer.SubscribeMultiple(map[string]byte{"integration/#": 1, "homeassistant/sensor/integration/#": 1}, nil)); err != nil {
		t.Fatal(err)
	}
	receive := func(topic, value string) {
		t.Helper()
		for {
			select {
			case m := <-messages:
				if m.Topic() == topic && (value == "" || string(m.Payload()) == value) {
					return
				}
			case <-ctx.Done():
				t.Fatalf("waiting for %s=%s: %v", topic, value, ctx.Err())
			}
		}
	}
	client := New(cfg, log)
	defer func() {
		closeCtx, done := context.WithTimeout(context.Background(), time.Second)
		defer done()
		if err := client.Close(closeCtx); err != nil {
			t.Error(err)
		}
	}()
	if err := client.Connect(ctx, log); err != nil {
		t.Fatal(err)
	}
	select {
	case <-client.Wake:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	sample := metric.Sensor("cpu_percent", "CPU", "%", 12.5)
	if err := client.Publish(ctx, []metric.Sample{sample}); err != nil {
		t.Fatal(err)
	}
	receive("integration/availability", "online")
	receive("homeassistant/sensor/integration/cpu_percent/config", "")
	receive("integration/sensor/cpu_percent/state", "")

	// Seed a discovery from the old VPN module and verify an actual retained tombstone.
	legacy := "homeassistant/sensor/integration/tailscale_ip/config"
	if err := wait(ctx, observer.Publish(legacy, 1, true, []byte(`{"name":"old"}`))); err != nil {
		t.Fatal(err)
	}
	for {
		client.controlMu.RLock()
		seen := client.observedSensors[legacy]
		client.controlMu.RUnlock()
		if seen {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("legacy discovery not observed")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err := client.Publish(ctx, []metric.Sample{sample}); err != nil {
		t.Fatal(err)
	}
	removed := false
	for !removed {
		select {
		case msg := <-messages:
			removed = msg.Topic() == legacy && len(msg.Payload()) == 0
		case <-ctx.Done():
			t.Fatal("legacy discovery was not removed")
		}
	}
	if server != nil {
		remote, ok := server.Clients.Get("labbeacon-integration")
		if !ok {
			t.Fatal("agent missing from broker")
		}
		// Abrupt TCP loss must emit the configured last will and trigger reconnect.
		if err := remote.Net.Conn.Close(); err != nil {
			t.Fatal(err)
		}
		receive("integration/availability", "offline")
		select {
		case <-client.Wake:
		case <-ctx.Done():
			t.Fatal("no reconnect", ctx.Err())
		}
		if err := client.Publish(ctx, []metric.Sample{sample}); err != nil {
			t.Fatal(err)
		}
		receive("integration/availability", "online")
		receive("homeassistant/sensor/integration/cpu_percent/config", "")
		receive("integration/sensor/cpu_percent/state", "")
	}
	if err := client.Close(ctx); err != nil {
		t.Fatal(err)
	}
	receive("integration/availability", "offline")
	// New subscriptions must recover retained discovery/availability, not readings.
	if err := wait(ctx, observer.Unsubscribe("integration/#", "homeassistant/sensor/integration/#")); err != nil {
		t.Fatal(err)
	}
	if err := wait(ctx, observer.SubscribeMultiple(map[string]byte{"integration/#": 1, "homeassistant/sensor/integration/#": 1}, nil)); err != nil {
		t.Fatal(err)
	}
	gotConfig, gotOffline := false, false
	for !gotConfig || !gotOffline {
		select {
		case m := <-messages:
			if !m.Retained() {
				t.Fatal("expected retained recovery")
			}
			switch m.Topic() {
			case "integration/availability":
				gotOffline = string(m.Payload()) == "offline"
			case "homeassistant/sensor/integration/cpu_percent/config":
				gotConfig = true
			default:
				t.Fatalf("unexpected retained state %s", m.Topic())
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}
