package control

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/brendlij/labbeacon/internal/command"
	"github.com/brendlij/labbeacon/internal/config"
	"github.com/brendlij/labbeacon/internal/metric"
)

// NativeSystemd checks read-only prerequisites, never reboots or starts a unit.
// Conservatively refuse container-local systemd/host power control.
func NativeSystemd() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("systemd control unavailable: requires native Linux")
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("systemd control unavailable: requires root; monitoring remains read-only")
	}
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return fmt.Errorf("host/systemd control unavailable inside Docker; run on the host")
	}
	if _, err := os.Stat("/run/.containerenv"); err == nil {
		return fmt.Errorf("host/systemd control unavailable inside a container")
	}
	if os.Getenv("container") != "" {
		return fmt.Errorf("container environment cannot control host systemd")
	}
	data, err := os.ReadFile("/proc/1/comm")
	if err != nil || strings.TrimSpace(string(data)) != "systemd" {
		return fmt.Errorf("host systemd manager is not PID 1")
	}
	_, err = exec.LookPath("systemctl")
	return err
}
func argv(ctx context.Context, runner command.Runner, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("empty command")
	}
	_, err := runner.Run(ctx, args[0], args[1:]...)
	return err
}
func HostEntries(cfg config.HostControl, runner command.Runner) []Entry {
	if !cfg.Enabled {
		return nil
	}
	var out []Entry
	for _, item := range []struct {
		name, transition string
		c                config.CommandAction
	}{{"reboot", "rebooting", cfg.Reboot}, {"shutdown", "shutting_down", cfg.Shutdown}} {
		if !item.c.Enabled {
			continue
		}
		check := func(ctx context.Context) error {
			if err := NativeSystemd(); err != nil {
				return err
			}
			if _, err := exec.LookPath(item.c.Command[0]); err != nil {
				return err
			}
			return argv(ctx, runner, item.c.Preflight)
		}
		out = append(out, Entry{Name: "Host " + item.name, Module: "host_control", Transition: item.transition, Check: check, Action: Function{Key: "host_" + item.name, Confirm: item.c.ConfirmRequired, Run: func(ctx context.Context) error { return argv(ctx, runner, item.c.Command) }}})
	}
	return out
}
func ServiceEntries(cfg config.Services, runner command.Runner) []Entry {
	if !cfg.Enabled {
		return nil
	}
	var out []Entry
	for _, ch := range cfg.Checks {
		if !ch.AllowControl {
			continue
		}
		for _, op := range []string{"start", "stop", "restart"} {
			check := func(ctx context.Context) error {
				if err := NativeSystemd(); err != nil {
					return err
				}
				data, err := runner.Run(ctx, "systemctl", "--no-ask-password", "show", ch.SystemdUnit, "--property=LoadState", "--value")
				if err != nil {
					return err
				}
				if strings.TrimSpace(string(data)) != "loaded" {
					return fmt.Errorf("systemd unit %s unavailable", ch.SystemdUnit)
				}
				return nil
			}
			out = append(out, Entry{Name: ch.Name + " " + op, Module: "services", Check: check, Action: Function{Key: "service_" + metric.Key(ch.Name) + "_" + op, Run: func(ctx context.Context) error {
				return argv(ctx, runner, []string{"systemctl", "--no-ask-password", op, ch.SystemdUnit})
			}}})
		}
	}
	return out
}
