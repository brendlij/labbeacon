// Package webui serves the embedded settings UI without a frontend build step.
package webui

import (
	"reflect"
	"sync"
	"time"

	"homelab-agent/internal/config"
)

type ModuleStatus struct {
	Name        string
	Enabled     bool
	LastChecked time.Time
	LastError   string
}
type Container struct{ Name, Status string }
type Snapshot struct {
	Active     config.Config
	Pending    bool
	Restart    []string
	Modules    []ModuleStatus
	Containers []Container
	Updated    time.Time
}
type State struct {
	mu       sync.RWMutex
	snapshot Snapshot
	desired  config.Config
	pending  bool
	applying bool
	Wake     chan struct{}
}

func NewState(c config.Config) *State {
	return &State{snapshot: Snapshot{Active: c.Clone()}, desired: c.Clone(), Wake: make(chan struct{}, 1)}
}
func (s *State) View() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v := s.snapshot
	v.Active = v.Active.Clone()
	v.Modules = append([]ModuleStatus(nil), v.Modules...)
	v.Containers = append([]Container(nil), v.Containers...)
	v.Restart = append([]string(nil), v.Restart...)
	v.Pending = s.pending || s.applying
	return v
}
func (s *State) Submit(c config.Config) {
	s.mu.Lock()
	s.desired = c.Clone()
	s.pending = true
	s.snapshot.Restart = RestartFields(s.snapshot.Active, c)
	s.mu.Unlock()
	select {
	case s.Wake <- struct{}{}:
	default:
	}
}
func (s *State) Take() (config.Config, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.pending {
		return config.Config{}, false
	}
	s.pending = false
	s.applying = true
	return s.desired.Clone(), true
}
func (s *State) HasPending() bool { s.mu.RLock(); defer s.mu.RUnlock(); return s.pending }
func (s *State) Applied(c config.Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.Active = c.Clone()
	s.applying = false
	s.snapshot.Restart = RestartFields(c, s.desired)
	s.snapshot.Modules = nil
	s.snapshot.Containers = nil
}
func (s *State) Report(modules []ModuleStatus, containers []Container) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.Modules = append([]ModuleStatus(nil), modules...)
	s.snapshot.Containers = append([]Container(nil), containers...)
	s.snapshot.Updated = time.Now()
}
func RestartFields(active, next config.Config) []string {
	var fields []string
	if active.MQTT != next.MQTT {
		fields = append(fields, "MQTT-Verbindung / Discovery-Präfix")
	}
	if active.Agent.ID != next.Agent.ID || active.Agent.Name != next.Agent.Name {
		fields = append(fields, "Agent-ID / Name")
	}
	if active.Agent.LogLevel != next.Agent.LogLevel {
		fields = append(fields, "Log-Level")
	}
	if !reflect.DeepEqual(active.WebUI, next.WebUI) {
		fields = append(fields, "Web-UI: Adresse, Port oder Zugangsdaten")
	}
	return fields
}

// LiveConfig deliberately preserves connection, identity and UI authentication.
func LiveConfig(active, next config.Config) config.Config {
	c := next.Clone()
	c.MQTT = active.MQTT
	c.WebUI = active.WebUI
	c.Agent.ID = active.Agent.ID
	c.Agent.Name = active.Agent.Name
	c.Agent.LogLevel = active.Agent.LogLevel
	return c
}
func Dangerous(old, next config.Config) bool {
	if next.HostControl.Enabled && ((!old.HostControl.Enabled) || (!old.ControlActions.Enabled && next.ControlActions.Enabled)) {
		return true
	}
	if old.HostControl.Reboot.ConfirmRequired != next.HostControl.Reboot.ConfirmRequired || old.HostControl.Shutdown.ConfirmRequired != next.HostControl.Shutdown.ConfirmRequired {
		return true
	}
	return next.HostControl.Enabled && !reflect.DeepEqual(old.HostControl, next.HostControl)
}
