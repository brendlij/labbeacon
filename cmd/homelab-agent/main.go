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
	"sync"
	"syscall"
	"time"

	"homelab-agent/internal/command"
	"homelab-agent/internal/config"
	"homelab-agent/internal/docker"
	"homelab-agent/internal/metric"
	"homelab-agent/internal/mqtt"
	"homelab-agent/internal/netbird"
	"homelab-agent/internal/services"
	"homelab-agent/internal/system"
	"homelab-agent/internal/tailscale"
	"homelab-agent/internal/version"
)

func main() {
	if err := run(); err != nil {
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
	collectors := map[string]metric.Collector{}
	if cfg.Modules.System.Enabled {
		collectors["system"] = system.New(cfg.Modules.System.DiskPaths)
	}
	if cfg.Modules.Docker.Enabled {
		d := cfg.Modules.Docker
		collectors["docker"] = &docker.Collector{API: docker.NewClient(d.SocketPath, d.Timeout), Timeout: d.Timeout}
	}
	if cfg.Modules.Services.Enabled {
		collectors["services"] = services.New(cfg.Modules.Services.Checks)
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
		if name == "tailscale" {
			collectors[name] = &tailscale.Collector{Runner: command.Exec{}, Command: resolved, Timeout: cli.Timeout}
		} else {
			collectors[name] = &netbird.Collector{Runner: command.Exec{}, Command: resolved, Timeout: cli.Timeout}
		}
	}
	client := mqtt.New(cfg, log)
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
	log.Info("agent started", "id", cfg.Agent.ID, "version", version.Version, "modules", len(collectors))
	poll := func() {
		cycle, cancel := context.WithTimeout(ctx, cfg.Agent.PollInterval)
		defer cancel()
		var wg sync.WaitGroup
		var mu sync.Mutex
		var samples []metric.Sample
		for name, collector := range collectors {
			wg.Add(1)
			go func() {
				defer wg.Done()
				values, e := collector.Collect(cycle)
				if e != nil && ctx.Err() == nil {
					log.Warn("collection incomplete", "module", name, "error", e)
				}
				mu.Lock()
				samples = append(samples, values...)
				mu.Unlock()
			}()
		}
		wg.Wait()
		publishCtx, done := context.WithTimeout(ctx, 5*time.Second)
		defer done()
		if e := client.Publish(publishCtx, samples); e != nil && ctx.Err() == nil {
			log.Warn("publish failed", "error", e)
		}
	}
	ticker := time.NewTicker(cfg.Agent.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Info("shutting down")
			return nil
		case <-client.Wake:
			onlineCtx, done := context.WithTimeout(ctx, 5*time.Second)
			if e := client.Online(onlineCtx); e != nil && ctx.Err() == nil {
				log.Warn("online announcement failed", "error", e)
			}
			done()
			poll()
		case <-ticker.C:
			poll()
		}
	}
}
