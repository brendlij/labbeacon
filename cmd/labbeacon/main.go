package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/brendlij/labbeacon/internal/agent"
	"github.com/brendlij/labbeacon/internal/config"
	"github.com/brendlij/labbeacon/internal/control"
	"github.com/brendlij/labbeacon/internal/docker"
	"github.com/brendlij/labbeacon/internal/module"
	"github.com/brendlij/labbeacon/internal/mqtt"
	"github.com/brendlij/labbeacon/internal/services"
	"github.com/brendlij/labbeacon/internal/system"
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
	systemCollector.Options.Processes = false
	systemCollector.Options.FileDescriptors = false
	cfg.Modules.Docker.ImageUpdates.Enabled = false
	dockerClient := docker.NewClient(cfg.Modules.Docker.SocketPath, cfg.Modules.Docker.Timeout)
	dockerCollector := &docker.Collector{API: dockerClient, Timeout: cfg.Modules.Docker.Timeout, Config: cfg.Modules.Docker}
	modules := []module.Module{
		module.Registration{ModuleName: "system", Active: cfg.Modules.System.Enabled, Collector: systemCollector},
		module.Registration{ModuleName: "docker", Active: cfg.Modules.Docker.Enabled, Collector: dockerCollector},
		module.Registration{ModuleName: "services", Active: cfg.Modules.Services.Enabled, Collector: services.NewWithSocket(cfg.Modules.Services.Checks, cfg.Modules.Services.BusSocket)},
		module.Registration{ModuleName: "agent", Active: cfg.Agent.MetricsEnabled, Collector: &agent.Collector{Started: started, Session: client.Session}},
	}
	registry := &module.Registry{}
	for _, m := range modules {
		if err := registry.Register(m); err != nil {
			return nil, err
		}
	}
	return registry, nil
}
