package services

import (
	"bufio"
	"context"
	"github.com/godbus/dbus/v5"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type fakeManager struct{}

func (fakeManager) LoadUnit(name string) (dbus.ObjectPath, *dbus.Error) {
	if name == "missing.service" {
		return "", dbus.NewError("org.freedesktop.systemd1.NoSuchUnit", []interface{}{"missing"})
	}
	return "/org/freedesktop/systemd1/unit/example", nil
}

type fakeProperties struct{}

func (fakeProperties) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	return map[string]dbus.Variant{"LoadState": dbus.MakeVariant("loaded"), "ActiveState": dbus.MakeVariant("failed"), "SubState": dbus.MakeVariant("failed")}, nil
}
func TestBusReadsSystemdProperties(t *testing.T) {
	if _, err := exec.LookPath("dbus-daemon"); err != nil {
		t.Skip("dbus-daemon unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "dbus-daemon", "--session", "--nofork", "--print-address=1", "--address=unix:path="+t.TempDir()+"/bus")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = cmd.Wait() }()
	address, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	address = strings.TrimSpace(address)
	conn, err := dbus.Connect(address, dbus.WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err = conn.RequestName("org.freedesktop.systemd1", dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
	if err = conn.Export(fakeManager{}, "/org/freedesktop/systemd1", "org.freedesktop.systemd1.Manager"); err != nil {
		t.Fatal(err)
	}
	if err = conn.Export(fakeProperties{}, "/org/freedesktop/systemd1/unit/example", "org.freedesktop.DBus.Properties"); err != nil {
		t.Fatal(err)
	}
	path := strings.Split(strings.TrimPrefix(address, "unix:path="), ",")[0]
	reader := Bus{Socket: path}
	status, err := reader.Read(ctx, "example.service")
	if err != nil || status.Active != "failed" {
		t.Fatalf("%+v %v", status, err)
	}
	status, err = reader.Read(ctx, "missing.service")
	if err != nil || status.Load != "not-found" {
		t.Fatalf("%+v %v", status, err)
	}
}
