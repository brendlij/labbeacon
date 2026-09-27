package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"homelab-agent/internal/command"
	"homelab-agent/internal/config"
	"homelab-agent/internal/control"
	"homelab-agent/internal/docker"
	"homelab-agent/internal/mqtt"
	"homelab-agent/internal/version"
	"homelab-agent/internal/webui"
)

func controlEnabled(c config.Config) bool {
	if !c.ControlActions.Enabled {
		return false
	}
	if c.HostControl.Enabled || c.AgentControl.Enabled || c.Modules.Docker.ControlContainers.Enabled {
		return true
	}
	for _, ch := range c.Modules.Services.Checks {
		if ch.AllowControl {
			return true
		}
	}
	return false
}
func prepareControls(ctx context.Context, cfg config.Config, log *slog.Logger) ([]control.Entry, []string) {
	if !cfg.ControlActions.Enabled {
		return nil, nil
	}
	entries := append(control.HostEntries(cfg.HostControl, command.Exec{}), control.ServiceEntries(cfg.Modules.Services, command.Exec{})...)
	if cfg.AgentControl.Enabled {
		entries = append(entries, control.Entry{Name: "Agent restart", Module: "agent", Action: control.Function{Key: "agent_restart", Run: func(context.Context) error { return control.ErrRestart }}})
	}
	var ready []control.Entry
	var unavailable []string
	for _, e := range entries {
		preflight, cancel := context.WithTimeout(ctx, 5*time.Second)
		var err error
		if e.Check != nil {
			err = e.Check(preflight)
		}
		cancel()
		if err != nil {
			log.Warn("control unavailable", "action", e.Action.ID(), "module", e.Module, "error", err)
			unavailable = append(unavailable, e.Action.ID()+": "+err.Error())
		} else {
			ready = append(ready, e)
		}
	}
	return ready, unavailable
}

func serve(parent context.Context, cfg config.Config, log *slog.Logger, paths ...string) error {
	ctx, cancelRun := context.WithCancel(parent)
	client := mqtt.New(cfg, log)
	state := webui.NewState(cfg)
	path := "config.yaml"
	if len(paths) > 0 {
		path = paths[0]
	}
	var shutdownWeb func(context.Context) error
	var webDone <-chan error
	var connectDone chan struct{}
	defer func() {
		cancelRun()
		if shutdownWeb != nil {
			shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			if err := shutdownWeb(shutdown); err != nil {
				log.Warn("web shutdown incomplete", "error", err)
			}
			cancel()
		}
		if connectDone != nil {
			<-connectDone
		}
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := client.Close(shutdown); err != nil {
			log.Warn("offline announcement failed", "error", err)
		}
	}()
	if cfg.WebUI.Enabled {
		server, err := webui.New(&config.Store{Path: path}, state, cfg.WebUI, client.Connected, log)
		if err != nil {
			return err
		}
		shutdownWeb, webDone, err = server.Listen()
		if err != nil {
			return fmt.Errorf("web UI listen: %w", err)
		}
		log.Info("web UI listening", "bind", cfg.WebUI.BindAddress, "port", cfg.WebUI.Port, "authentication", cfg.WebUI.Username != "")
	}
	registry, err := registered(cfg, client, log)
	if err != nil {
		return err
	}
	manager := control.New(log)
	ready, staticErrors := prepareControls(ctx, cfg, log)
	probe, cancel := context.WithTimeout(ctx, 5*time.Second)
	registry.Actions(probe, func(name string, e error) { log.Warn("control unavailable at startup", "module", name, "error", e) })
	cancel()
	connectDone = make(chan struct{})
	connectErrors := make(chan error, 1)
	go func() { defer close(connectDone); connectErrors <- client.Connect(ctx, log) }()
	ticker := time.NewTicker(cfg.Agent.PollInterval)
	defer ticker.Stop()
	poll := func() {
		cycle, cancel := context.WithTimeout(ctx, cfg.Agent.PollInterval)
		samples := registry.Collect(cycle, func(name string, e error) {
			if ctx.Err() == nil {
				log.Warn("collection incomplete", "module", name, "error", e)
			}
		})
		cancel()
		controls, done := context.WithTimeout(ctx, 5*time.Second)
		actionErrors := append([]string(nil), staticErrors...)
		entries := append([]control.Entry{}, ready...)
		entries = append(entries, registry.Actions(controls, func(name string, e error) {
			actionErrors = append(actionErrors, name+": "+e.Error())
			log.Warn("control unavailable", "module", name, "error", e)
		})...)
		if err := manager.Replace(entries); err != nil {
			log.Error("control registration failed", "error", err)
		} else if client.Connected() {
			if err = client.SyncActions(controls, entries); err != nil && ctx.Err() == nil {
				log.Warn("control discovery failed", "error", err)
			}
		}
		done()
		var statuses []webui.ModuleStatus
		for _, s := range registry.Status() {
			statuses = append(statuses, webui.ModuleStatus{Name: s.Name, Enabled: s.Enabled, LastChecked: s.LastChecked, LastError: s.LastError})
		}
		statuses = append(statuses, webui.ModuleStatus{Name: "control_actions", Enabled: controlEnabled(cfg), LastChecked: time.Now(), LastError: strings.Join(actionErrors, "; ")})
		var containers []webui.Container
		for _, s := range samples {
			if s.Key == "containers_running" {
				if inventory, ok := s.Attributes["containers"].([]docker.Container); ok {
					for _, c := range inventory {
						containers = append(containers, webui.Container{Name: c.Name, Status: c.Status})
					}
				}
			}
		}
		state.Report(statuses, containers)
		if client.Connected() {
			publishCtx, done := context.WithTimeout(ctx, 5*time.Second)
			if e := client.Publish(publishCtx, samples); e != nil && ctx.Err() == nil {
				log.Warn("publish failed", "error", e)
			}
			done()
		}
	}
	applyPending := func() bool {
		next, ok := state.Take()
		if !ok {
			return false
		}
		live := webui.LiveConfig(cfg, next)
		updated, e := registered(live, client, log)
		if e != nil {
			log.Error("live configuration rejected", "error", e)
			state.Applied(cfg)
			return false
		}
		// The old control session is invalid before rebuilding any enabled actions.
		client.RotateSession()
		manager.Entries = map[string]control.Entry{}
		registry = updated
		cfg = live
		client.SetExpiry(cfg.Agent.ExpireAfter)
		ticker.Reset(cfg.Agent.PollInterval)
		ready, staticErrors = prepareControls(ctx, cfg, log)
		state.Applied(cfg)
		log.Info("live configuration applied", "restart_required", webui.RestartFields(cfg, next))
		return true
	}
	log.Info("agent started", "id", cfg.Agent.ID, "version", version.Version)
	poll()
	for {
		if ctx.Err() != nil {
			return nil
		}
		if applyPending() {
			poll()
		}
		select {
		case <-ctx.Done():
			log.Info("shutting down")
			return nil
		case e := <-webDone:
			if e != nil {
				return fmt.Errorf("web UI stopped: %w", e)
			}
			return nil
		case e := <-connectErrors:
			connectErrors = nil
			if e != nil && !errors.Is(e, context.Canceled) {
				return e
			}
		case <-state.Wake:
			continue
		case <-client.Wake:
			poll()
		case request := <-client.Commands:
			if state.HasPending() {
				log.Warn("control rejected during pending config reload", "action", request.ID)
				continue
			}
			previous := manager.Last[request.ID]
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
			if manager.Last[request.ID] != previous {
				client.RotateSession()
				poll()
			}
		case <-ticker.C:
			poll()
		}
	}
}
