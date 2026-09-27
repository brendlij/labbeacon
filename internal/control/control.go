// Package control handles explicitly enabled, auditable actions. MQTT input never
// becomes shell arguments: all targets and argv originate in local configuration.
package control

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/brendlij/labbeacon/internal/metric"
)

var ErrRestart = errors.New("agent restart requested")

type Action interface {
	ID() string
	Execute(context.Context) error
	RequiresConfirm() bool
}
type Provider interface {
	Actions(context.Context) ([]Entry, error)
}
type Entry struct {
	Device                   metric.DeviceRef
	Action                   Action
	Name, Module, Transition string
	Check                    func(context.Context) error
}
type Function struct {
	Key     string
	Confirm bool
	Run     func(context.Context) error
}

func (a Function) ID() string                        { return a.Key }
func (a Function) RequiresConfirm() bool             { return a.Confirm }
func (a Function) Execute(ctx context.Context) error { return a.Run(ctx) }

type Request struct {
	ID, Session, Topic string
	Confirmed          bool
	Received           time.Time
	QoS                byte
}

// Manager is owned by the main loop; collection, commands and shutdown serialize.
type Manager struct {
	Entries map[string]Entry
	Last    map[string]time.Time
	Log     *slog.Logger
}

func New(log *slog.Logger) *Manager {
	return &Manager{Entries: map[string]Entry{}, Last: map[string]time.Time{}, Log: log}
}
func (m *Manager) Replace(entries []Entry) error {
	next := map[string]Entry{}
	for _, e := range entries {
		if e.Action.ID() == "" {
			return fmt.Errorf("empty action ID")
		}
		if _, ok := next[e.Action.ID()]; ok {
			return fmt.Errorf("duplicate action %s", e.Action.ID())
		}
		next[e.Action.ID()] = e
	}
	m.Entries = next
	return nil
}
func (m *Manager) Execute(ctx context.Context, r Request, session string, announce func(context.Context, string) error) (err error) {
	fields := []any{"action", r.ID, "topic", r.Topic, "qos", r.QoS, "source", "mqtt", "actor", "unknown (MQTT does not forward publisher identity)"}
	defer func() {
		if err != nil && !errors.Is(err, ErrRestart) {
			m.Log.Warn("control rejected or failed", append(fields, "error", err)...)
		} else {
			m.Log.Info("control completed", fields...)
		}
	}()
	if err = ctx.Err(); err != nil {
		return err
	}
	if r.Session == "" || r.Session != session {
		return fmt.Errorf("stale control session")
	}
	if time.Since(r.Received) > 15*time.Second || r.Received.After(time.Now().Add(time.Second)) {
		return fmt.Errorf("command expired")
	}
	e, ok := m.Entries[r.ID]
	if !ok {
		return fmt.Errorf("action disabled or unavailable")
	}
	if e.Action.RequiresConfirm() && !r.Confirmed {
		return fmt.Errorf("explicit confirmation required; use the documented dashboard action")
	}
	if time.Since(m.Last[r.ID]) < 2*time.Second {
		return fmt.Errorf("action cooldown")
	}
	if e.Check != nil {
		if err = e.Check(ctx); err != nil {
			return err
		}
	}
	m.Last[r.ID] = time.Now()
	m.Log.Info("control executing", fields...)
	if e.Transition != "" {
		if err = announce(ctx, e.Transition); err != nil {
			return fmt.Errorf("transition announcement: %w", err)
		}
	}
	err = e.Action.Execute(ctx)
	if err != nil && !errors.Is(err, ErrRestart) && e.Transition != "" {
		if restoreErr := announce(ctx, "online"); restoreErr != nil {
			err = errors.Join(err, restoreErr)
		}
	}
	return err
}
