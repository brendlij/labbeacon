package mqtt

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"homelab-agent/internal/config"
	"homelab-agent/internal/metric"
)

type token struct{ err error }

func (t token) Wait() bool                     { return true }
func (t token) WaitTimeout(time.Duration) bool { return true }
func (t token) Done() <-chan struct{}          { ch := make(chan struct{}); close(ch); return ch }
func (t token) Error() error                   { return t.err }

type publication struct {
	topic   string
	retain  bool
	payload interface{}
}
type fakePublisher struct {
	calls []publication
	err   error
}

func (f *fakePublisher) Publish(topic string, qos byte, retain bool, payload interface{}) paho.Token {
	f.calls = append(f.calls, publication{topic, retain, payload})
	return token{f.err}
}
func TestDiscoveryAndState(t *testing.T) {
	c := config.Defaults()
	c.Agent.ID = "srv-01"
	c.Agent.Name = "Server"
	s := metric.Binary("service_web", "Web", true, map[string]any{"latency": 2})
	p := &fakePublisher{}
	if e := PublishSample(context.Background(), p, c, s); e != nil {
		t.Fatal(e)
	}
	if len(p.calls) != 2 || p.calls[0].topic != "homeassistant/binary_sensor/srv-01/service_web/config" || !p.calls[0].retain || p.calls[1].retain || p.calls[1].topic != "srv-01/binary_sensor/service_web/state" {
		t.Fatalf("bad publications: %+v", p.calls)
	}
	var d map[string]any
	if e := json.Unmarshal(p.calls[0].payload.([]byte), &d); e != nil {
		t.Fatal(e)
	}
	if d["expire_after"] != float64(60) || d["payload_on"] != "ON" || d["availability_topic"] != "srv-01/availability" {
		t.Fatalf("bad discovery %v", d)
	}
	device := d["device"].(map[string]any)
	if device["identifiers"].([]any)[0] != "srv-01" {
		t.Fatal("wrong device")
	}
	var state map[string]any
	if e := json.Unmarshal(p.calls[1].payload.([]byte), &state); e != nil {
		t.Fatal(e)
	}
	if state["value"] != "ON" {
		t.Fatal("bad state")
	}
	p = &fakePublisher{err: errors.New("publish failed")}
	if e := PublishSample(context.Background(), p, c, s); e == nil || len(p.calls) != 1 {
		t.Fatal("must stop on failed discovery publish")
	}
}
