package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"homelab-agent/internal/agent"
	"homelab-agent/internal/command"
	"homelab-agent/internal/config"
	"homelab-agent/internal/control"
	"homelab-agent/internal/docker"
	"homelab-agent/internal/module"
	"homelab-agent/internal/mqtt"
	"homelab-agent/internal/netbird"
	"homelab-agent/internal/network"
	"homelab-agent/internal/services"
	"homelab-agent/internal/system"
	"homelab-agent/internal/tailscale"
	"homelab-agent/internal/version"
)

var started = time.Now()

func main() {
	if err := run(); err != nil {
		if errors.Is(err, control.ErrRestart) {
			os.Exit(75)
		}
		slog.Error("agent stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	path := flag.String("config", "config.yaml", "configuration file")
	showVersion := flag.Bool("version", false, "print version")
	validate := flag.Bool("check-config", false, "validate configuration and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version.Version)
		return nil
	}
	cfg, err := config.Load(*path)
	if err != nil {
		return err
	}
	if *validate {
		fmt.Println("configuration valid")
		return nil
	}
	var level slog.Level
	if err = level.UnmarshalText([]byte(cfg.Agent.LogLevel)); err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(log)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return serve(ctx, cfg, log)
}

func registered(cfg config.Config, client *mqtt.Client, log *slog.Logger) (*module.Registry, error) {
	systemCollector := system.New(cfg.Modules.System.DiskPaths)
	systemCollector.Options = cfg.Modules.System
	dockerClient := docker.NewClient(cfg.Modules.Docker.SocketPath, cfg.Modules.Docker.Timeout)
	dockerCollector := &docker.Collector{API: dockerClient, Timeout: cfg.Modules.Docker.Timeout, Config: cfg.Modules.Docker, Updates: docker.NewUpdateChecker(dockerClient, cfg.Modules.Docker.ImageUpdates)}
	modules := []module.Module{
		module.Registration{ModuleName: "system", Active: cfg.Modules.System.Enabled, Collector: systemCollector},
		module.Registration{ModuleName: "docker", Active: cfg.Modules.Docker.Enabled, Collector: dockerCollector},
		module.Registration{ModuleName: "services", Active: cfg.Modules.Services.Enabled, Collector: services.New(cfg.Modules.Services.Checks)},
		module.Registration{ModuleName: "network", Active: cfg.Modules.Network.Enabled, Collector: network.New(cfg.Modules.Network)},
		module.Registration{ModuleName: "agent", Active: cfg.Agent.MetricsEnabled, Collector: &agent.Collector{Started: started, Session: client.Session}},
	}
	for name, cli := range map[string]config.CLI{"tailscale": cfg.Modules.Tailscale, "netbird": cfg.Modules.Netbird} {
		if !cli.Enabled {
			continue
		}
		resolved, e := exec.LookPath(cli.Command)
		if e != nil {
			log.Warn("module disabled: CLI not found", "module", name, "command", cli.Command)
			continue
		}
		r := module.Registration{ModuleName: name, Active: true}
		if name == "tailscale" {
			r.Collector = &tailscale.Collector{Runner: command.Exec{}, Command: resolved, Timeout: cli.Timeout}
		} else {
			r.Collector = &netbird.Collector{Runner: command.Exec{}, Command: resolved, Timeout: cli.Timeout}
		}
		modules = append(modules, r)
	}
	registry := &module.Registry{}
	for _, m := range modules {
		if err := registry.Register(m); err != nil {
			return nil, err
		}
	}
	return registry, nil
}
func serve(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	client := mqtt.New(cfg, log)
	registry, err := registered(cfg, client, log)
	if err != nil {
		return err
	}
	manager := control.New(log)
	static := append(control.HostEntries(cfg.HostControl, command.Exec{}), control.ServiceEntries(cfg.Modules.Services, command.Exec{})...)
	if cfg.AgentControl.Enabled {
		static = append(static, control.Entry{Name: "Agent restart", Module: "agent", Action: control.Function{Key: "agent_restart", Run: func(context.Context) error { return control.ErrRestart }}})
	}
	var ready []control.Entry
	for _, e := range static {
		preflight, cancel := context.WithTimeout(ctx, 5*time.Second)
		var err error
		if e.Check != nil {
			err = e.Check(preflight)
		}
		cancel()
		if err != nil {
			log.Warn("control unavailable", "action", e.Action.ID(), "module", e.Module, "error", err)
		} else {
			ready = append(ready, e)
		}
	}
	probe, cancel := context.WithTimeout(ctx, 5*time.Second)
	registry.Actions(probe, func(name string, e error) { log.Warn("control unavailable at startup", "module", name, "error", e) })
	cancel()
	defer func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if e := client.Close(shutdown); e != nil {
			log.Warn("offline announcement failed", "error", e)
		}
	}()
	if err = client.Connect(ctx, log); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	syncActions := func() error {
		controls, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		entries := append([]control.Entry{}, ready...)
		entries = append(entries, registry.Actions(controls, func(name string, e error) { log.Warn("control unavailable", "module", name, "error", e) })...)
		if e := manager.Replace(entries); e != nil {
			return e
		}
		return client.SyncActions(controls, entries)
	}
	poll := func() {
		cycle, cancel := context.WithTimeout(ctx, cfg.Agent.PollInterval)
		samples := registry.Collect(cycle, func(name string, e error) {
			if ctx.Err() == nil {
				log.Warn("collection incomplete", "module", name, "error", e)
			}
		})
		cancel()
		if e := syncActions(); e != nil && ctx.Err() == nil {
			log.Warn("control discovery failed", "error", e)
		}
		publishCtx, done := context.WithTimeout(ctx, 5*time.Second)
		defer done()
		if e := client.Publish(publishCtx, samples); e != nil && ctx.Err() == nil {
			log.Warn("publish failed", "error", e)
		}
	}
	log.Info("agent started", "id", cfg.Agent.ID, "version", version.Version)
	ticker := time.NewTicker(cfg.Agent.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Info("shutting down")
			return nil
		case <-client.Wake:
			online, done := context.WithTimeout(ctx, 5*time.Second)
			if e := client.Online(online); e != nil && ctx.Err() == nil {
				log.Warn("online announcement failed", "error", e)
			}
			done()
			poll()
		case request := <-client.Commands:
			previousAttempt := manager.Last[request.ID]
			timeout := 30 * time.Second
			if entry, ok := manager.Entries[request.ID]; ok && entry.Module == "host_control" {
				timeout = cfg.HostControl.Timeout
			}
			actionCtx, done := context.WithTimeout(ctx, timeout)
			e := manager.Execute(actionCtx, request, client.Session(), client.Announce)
			done()
			if errors.Is(e, control.ErrRestart) {
				return e
			}
			if e == nil && (request.ID == "host_reboot" || request.ID == "host_shutdown") {
				return nil
			}
			if manager.Last[request.ID] != previousAttempt {
				client.RotateSession()
				poll()
			}
		case <-ticker.C:
			poll()
		}
	}
}
