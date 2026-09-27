package control

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/brendlij/labbeacon/internal/config"
)

func TestManagerGatesAndAudit(t *testing.T) {
	for _, tc := range []struct {
		name     string
		request  Request
		confirm  bool
		checkErr error
		want     bool
	}{
		{name: "accepted", request: Request{ID: "test", Session: "session", Confirmed: true}, confirm: true, want: true},
		{name: "confirmation missing", request: Request{ID: "test", Session: "session"}, confirm: true},
		{name: "stale session", request: Request{ID: "test", Session: "old", Confirmed: true}, confirm: true},
		{name: "disabled", request: Request{ID: "disabled", Session: "session"}},
		{name: "preflight failure", request: Request{ID: "test", Session: "session"}, checkErr: errors.New("no permission")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			manager := New(slog.New(slog.NewJSONHandler(&logs, nil)))
			calls := 0
			entry := Entry{Name: "Test", Action: Function{Key: "test", Confirm: tc.confirm, Run: func(context.Context) error { calls++; return nil }}, Check: func(context.Context) error { return tc.checkErr }}
			if err := manager.Replace([]Entry{entry}); err != nil {
				t.Fatal(err)
			}
			tc.request.Received = time.Now()
			err := manager.Execute(context.Background(), tc.request, "session", nil)
			if (err == nil) != tc.want || (calls == 1) != tc.want {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
			if !strings.Contains(logs.String(), `"action"`) {
				t.Fatal("missing audit")
			}
			if tc.want {
				if err = manager.Execute(context.Background(), tc.request, "session", nil); err == nil || calls != 1 {
					t.Fatal("cooldown did not prevent duplicate execution")
				}
			}
		})
	}
}
func TestTransitionBeforeCommandAndRestore(t *testing.T) {
	var events []string
	m := New(slog.Default())
	if err := m.Replace([]Entry{{Action: Function{Key: "reboot", Run: func(context.Context) error { events = append(events, "execute"); return errors.New("denied") }}, Transition: "rebooting"}}); err != nil {
		t.Fatal(err)
	}
	err := m.Execute(context.Background(), Request{ID: "reboot", Session: "s", Received: time.Now()}, "s", func(_ context.Context, state string) error { events = append(events, state); return nil })
	if err == nil || strings.Join(events, ",") != "rebooting,execute,online" {
		t.Fatalf("%v %v", events, err)
	}
}
func TestNoControlByDefault(t *testing.T) {
	cfg := config.Defaults()
	if len(HostEntries(cfg.HostControl, nil)) != 0 || len(ServiceEntries(cfg.Modules.Services, nil)) != 0 || cfg.AgentControl.Enabled || cfg.Modules.Docker.ControlContainers.Permits("anything") {
		t.Fatal("controls must default off")
	}
}
func TestExpiredCanceledAndDuplicateRegistry(t *testing.T) {
	m := New(slog.Default())
	calls := 0
	e := Entry{Action: Function{Key: "a", Run: func(context.Context) error { calls++; return nil }}}
	if err := m.Replace([]Entry{e, e}); err == nil {
		t.Fatal("duplicate IDs accepted")
	}
	if err := m.Replace([]Entry{e}); err != nil {
		t.Fatal(err)
	}
	if err := m.Execute(context.Background(), Request{ID: "a", Session: "s", Received: time.Now().Add(-time.Minute)}, "s", nil); err == nil {
		t.Fatal("expired accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.Execute(ctx, Request{ID: "a", Session: "s", Received: time.Now()}, "s", nil); err == nil || calls != 0 {
		t.Fatal("canceled action executed")
	}
}
