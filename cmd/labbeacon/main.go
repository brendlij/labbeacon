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

	"github.com/brendlij/labbeacon/internal/agent"
	"github.com/brendlij/labbeacon/internal/command"
	"github.com/brendlij/labbeacon/internal/config"
	"github.com/brendlij/labbeacon/internal/control"
	"github.com/brendlij/labbeacon/internal/docker"
	"github.com/brendlij/labbeacon/internal/module"
	"github.com/brendlij/labbeacon/internal/mqtt"
	"github.com/brendlij/labbeacon/internal/netbird"
	"github.com/brendlij/labbeacon/internal/network"
	"github.com/brendlij/labbeacon/internal/services"
	"github.com/brendlij/labbeacon/internal/system"
	"github.com/brendlij/labbeacon/internal/tailscale"
	"github.com/brendlij/labbeacon/internal/version"
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
	return serve(ctx, cfg, log, *path)
}

func registered(cfg config.Config, client *mqtt.Client, log *slog.Logger) (*module.Registry, error) {
	if !cfg.ControlActions.Enabled {
		cfg.Modules.Docker.ControlContainers.Enabled = false
	}
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
			modules = append(modules, module.Registration{ModuleName: name, Active: false})
			continue
		}
		resolved, e := exec.LookPath(cli.Command)
		if e != nil {
			log.Warn("module unavailable: CLI not found", "module", name, "command", cli.Command)
			modules = append(modules, module.Registration{ModuleName: name, Active: true, Collector: module.Unavailable{Reason: fmt.Errorf("CLI not found: %s", cli.Command)}})
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
