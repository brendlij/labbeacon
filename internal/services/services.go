// Package services reads only explicitly selected systemd unit states.
package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/brendlij/labbeacon/internal/config"
	"github.com/brendlij/labbeacon/internal/metric"
	"github.com/godbus/dbus/v5"
)

type Status struct{ Load, Active, Sub string }
type Reader interface {
	Read(context.Context, string) (Status, error)
}
type Collector struct {
	Checks []config.Check
	Reader Reader
}

func New(checks []config.Check) *Collector { return NewWithSocket(checks, "") }
func NewWithSocket(checks []config.Check, socket string) *Collector {
	return &Collector{Checks: checks, Reader: Bus{Socket: socket}}
}
func (c *Collector) Collect(ctx context.Context) ([]metric.Sample, error) {
	var out []metric.Sample
	var errs []error
	for _, check := range c.Checks {
		if check.Type != "systemd" {
			continue
		} // Legacy HTTP/TCP config is inert.
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		timeout := check.Timeout
		if timeout <= 0 {
			timeout = 5 * time.Second
		}
		request, cancel := context.WithTimeout(ctx, timeout)
		state, err := c.Reader.Read(request, check.SystemdUnit)
		cancel()
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", check.SystemdUnit, err))
			continue
		}
		value := state.Active
		if state.Load == "not-found" {
			value = "not-found"
		}
		s := metric.Sensor("service_"+metric.Key(check.Name), "State", "", value)
		s.Device = metric.DeviceRef{Kind: "service", Name: check.Name}
		s.Attributes = map[string]any{"unit": check.SystemdUnit, "load_state": state.Load, "sub_state": state.Sub}
		out = append(out, s)
	}
	return out, errors.Join(errs...)
}

type Bus struct{ Socket string }

func (b Bus) Read(ctx context.Context, unit string) (Status, error) {
	socket := b.Socket
	if socket == "" {
		socket = "/run/dbus/system_bus_socket"
		if hostRun := os.Getenv("HOST_RUN"); hostRun != "" {
			socket = filepath.Join(hostRun, "dbus", "system_bus_socket")
		}
	}
	conn, err := dbus.Connect("unix:path="+dbus.EscapeBusAddressValue(socket), dbus.WithContext(ctx))
	if err != nil {
		return Status{}, fmt.Errorf("host system bus unavailable at %s; check the host mount and D-Bus read permissions: %w", socket, err)
	}
	defer conn.Close()
	var path dbus.ObjectPath
	// Loading a unit's metadata does not start, enable, or restart the service.
	err = conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1").CallWithContext(ctx, "org.freedesktop.systemd1.Manager.LoadUnit", 0, unit).Store(&path)
	if err != nil {
		var busErr dbus.Error
		if errors.As(err, &busErr) && busErr.Name == "org.freedesktop.systemd1.NoSuchUnit" {
			return Status{Load: "not-found", Active: "inactive"}, nil
		}
		return Status{}, err
	}
	var props map[string]dbus.Variant
	err = conn.Object("org.freedesktop.systemd1", path).CallWithContext(ctx, "org.freedesktop.DBus.Properties.GetAll", 0, "org.freedesktop.systemd1.Unit").Store(&props)
	if err != nil {
		return Status{}, err
	}
	get := func(key string) string { value, _ := props[key].Value().(string); return value }
	state := Status{Load: get("LoadState"), Active: get("ActiveState"), Sub: get("SubState")}
	if state.Load == "" || state.Active == "" {
		return Status{}, fmt.Errorf("systemd returned incomplete unit properties")
	}
	return state, nil
}
