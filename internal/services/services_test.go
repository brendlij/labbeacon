package services

import (
	"context"
	"errors"
	"github.com/brendlij/labbeacon/internal/config"
	"testing"
	"time"
)

type fakeReader struct{ calls []string }

func (r *fakeReader) Read(ctx context.Context, unit string) (Status, error) {
	r.calls = append(r.calls, unit)
	if unit == "broken.service" {
		return Status{}, errors.New("bus failure")
	}
	if unit == "missing.service" {
		return Status{Load: "not-found", Active: "inactive"}, nil
	}
	return Status{Load: "loaded", Active: "inactive", Sub: "dead"}, nil
}
func TestOnlySelectedSystemdStates(t *testing.T) {
	r := &fakeReader{}
	c := &Collector{Reader: r, Checks: []config.Check{
		{Name: "old", Type: "http", URL: "http://must-not-be-called.invalid"},
		{Name: "SSH", Type: "systemd", SystemdUnit: "ssh.service", Timeout: time.Second},
		{Name: "Missing", Type: "systemd", SystemdUnit: "missing.service", Timeout: time.Second},
		{Name: "Broken", Type: "systemd", SystemdUnit: "broken.service", Timeout: time.Second},
	}}
	samples, err := c.Collect(context.Background())
	if err == nil || len(samples) != 2 || len(r.calls) != 3 {
		t.Fatalf("bad partial collection: %v %v", samples, err)
	}
	if samples[0].Value != "inactive" || samples[0].Component != "sensor" || samples[0].Device.Name != "SSH" || samples[0].Attributes["unit"] != "ssh.service" || samples[1].Value != "not-found" {
		t.Fatalf("bad states: %v", samples)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = c.Collect(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored")
	}
}
