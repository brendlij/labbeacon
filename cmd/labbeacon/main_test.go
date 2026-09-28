package main

import (
	"context"
	"github.com/brendlij/labbeacon/internal/config"
	"github.com/brendlij/labbeacon/internal/mqtt"
	"io"
	"log/slog"
	"testing"
)

func TestRemovedFeaturesCannotActivate(t *testing.T) {
	c := config.Defaults()
	c.Agent.ID = "test"
	c.Modules.Tailscale.Enabled = true
	c.Modules.Netbird.Enabled = true
	c.Modules.Network.Enabled = true
	c.HostControl.Enabled = true
	c.AgentControl.Enabled = true
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	r, err := registered(c, mqtt.New(c, log), log)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range r.Status() {
		if s.Name == "tailscale" || s.Name == "netbird" || s.Name == "network" {
			t.Fatal("removed module registered")
		}
	}
	if entries, _ := prepareControls(context.Background(), c, log); len(entries) != 0 {
		t.Fatal("removed controls active")
	}
}
