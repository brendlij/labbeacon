package mqtt

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/brendlij/labbeacon/internal/config"
	"github.com/brendlij/labbeacon/internal/control"
)

type commandMessage struct {
	data                []byte
	retained, duplicate bool
	topic               string
}

func (m commandMessage) Duplicate() bool   { return m.duplicate }
func (m commandMessage) Qos() byte         { return 0 }
func (m commandMessage) Retained() bool    { return m.retained }
func (m commandMessage) Topic() string     { return m.topic }
func (m commandMessage) MessageID() uint16 { return 1 }
func (m commandMessage) Payload() []byte   { return m.data }
func (m commandMessage) Ack()              {}
func TestControlMessageGates(t *testing.T) {
	cfg := config.Defaults()
	cfg.Agent.ID = "srv"
	c := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.RotateSession()
	session := c.Session()
	data, e := json.Marshal(map[string]any{"session": session, "confirm": true})
	if e != nil {
		t.Fatal(e)
	}
	for _, m := range []commandMessage{{data: data, retained: true, topic: "srv/button/host_reboot/command"}, {data: data, duplicate: true, topic: "srv/button/host_reboot/command"}, {data: []byte(`{"session":"old"}`), topic: "srv/button/host_reboot/command"}, {data: []byte(`{`), topic: "srv/button/host_reboot/command"}} {
		c.receiveCommand(nil, m)
	}
	if len(c.Commands) != 0 {
		t.Fatal("invalid command queued")
	}
	c.receiveCommand(nil, commandMessage{data: data, topic: "srv/button/host_reboot/command"})
	select {
	case r := <-c.Commands:
		if r.ID != "host_reboot" || !r.Confirmed || r.Session != session {
			t.Fatalf("bad request %+v", r)
		}
	default:
		t.Fatal("missing request")
	}
	c.RotateSession()
	c.receiveCommand(nil, commandMessage{data: data, topic: "srv/button/host_reboot/command"})
	if len(c.Commands) != 0 {
		t.Fatal("replayed old session queued")
	}
}
func TestButtonDiscoveryUsesSharedDevice(t *testing.T) {
	cfg := config.Defaults()
	cfg.Agent.ID = "srv-01"
	cfg.Agent.Name = "Server"
	e := control.Entry{Name: "Reboot", Action: control.Function{Key: "host_reboot", Confirm: true, Run: func(context.Context) error { return nil }}}
	d := ButtonDiscovery(cfg, e, "nonce")
	if d["command_topic"] != "srv-01/button/host_reboot/command" || d["retain"] != false || d["qos"] != 0 {
		t.Fatalf("bad discovery %v", d)
	}
	if _, ok := d["confirmation"]; ok {
		t.Fatal("MQTT discovery has no native confirmation field")
	}
	if _, ok := d["expire_after"]; ok {
		t.Fatal("buttons must not have sensor-only expire_after")
	}
	var payload struct {
		Session string
		Confirm bool
	}
	if err := json.Unmarshal([]byte(d["payload_press"].(string)), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Confirm || payload.Session != "nonce" {
		t.Fatal("ordinary press must not silently confirm")
	}
}
