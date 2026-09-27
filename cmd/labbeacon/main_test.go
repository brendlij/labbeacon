package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	broker "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"

	"github.com/brendlij/labbeacon/internal/config"
	"github.com/brendlij/labbeacon/internal/control"
)

func TestAgentRestartGraceful(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := broker.New(&broker.Options{Logger: log})
	if e := server.AddHook(new(auth.AllowHook), nil); e != nil {
		t.Fatal(e)
	}
	listener := listeners.NewTCP(listeners.Config{ID: "test", Address: "127.0.0.1:0"})
	if e := server.AddListener(listener); e != nil {
		t.Fatal(e)
	}
	if e := server.Serve(); e != nil {
		t.Fatal(e)
	}
	defer func() {
		if e := server.Close(); e != nil {
			t.Error(e)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg := config.Defaults()
	cfg.WebUI.Enabled = false
	cfg.Agent.ID = "restart-test"
	cfg.Agent.Name = "Restart Test"
	cfg.Agent.PollInterval = time.Second
	cfg.MQTT.Broker = "tcp://" + listener.Address()
	cfg.AgentControl.Enabled = true
	messages := make(chan paho.Message, 64)
	observer := paho.NewClient(paho.NewClientOptions().AddBroker(cfg.MQTT.Broker).SetClientID("restart-observer").SetDefaultPublishHandler(func(_ paho.Client, m paho.Message) {
		select {
		case messages <- m:
		default:
		}
	}))
	wait := func(token paho.Token) {
		t.Helper()
		select {
		case <-token.Done():
			if token.Error() != nil {
				t.Fatal(token.Error())
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	wait(observer.Connect())
	defer observer.Disconnect(100)
	wait(observer.SubscribeMultiple(map[string]byte{"homeassistant/button/restart-test/#": 1, "restart-test/availability": 1}, nil))
	wait(observer.Publish("homeassistant/button/restart-test/removed/config", 1, true, `{"name":"Removed button"}`))
	done := make(chan error, 1)
	go func() { done <- serve(ctx, cfg, log) }()
	var payload string
	removed := false
	for payload == "" || !removed {
		select {
		case m := <-messages:
			if m.Topic() == "homeassistant/button/restart-test/removed/config" && len(m.Payload()) == 0 {
				removed = true
			}
			if m.Topic() == "homeassistant/button/restart-test/agent_restart/config" {
				var d struct {
					Payload string `json:"payload_press"`
				}
				if e := json.Unmarshal(m.Payload(), &d); e != nil {
					t.Fatal(e)
				}
				payload = d.Payload
			}
		case <-ctx.Done():
			t.Fatal("no restart discovery")
		}
	}
	wait(observer.Publish("restart-test/button/agent_restart/command", 0, false, payload))
	select {
	case e := <-done:
		if !errors.Is(e, control.ErrRestart) {
			t.Fatalf("expected supervisor restart, got %v", e)
		}
	case <-ctx.Done():
		t.Fatal("agent did not exit")
	}
	for {
		select {
		case m := <-messages:
			if m.Topic() == "restart-test/availability" && string(m.Payload()) == "offline" {
				return
			}
		case <-ctx.Done():
			t.Fatal("missing graceful offline")
		}
	}
}
