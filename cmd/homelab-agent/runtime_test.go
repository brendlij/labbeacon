package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
	"homelab-agent/internal/config"
	"homelab-agent/internal/mqtt"
)

func TestWebReloadWhileBrokerOffline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	cfg := config.Defaults()
	cfg.Agent.ID, cfg.Agent.Name = "web-test", "Web Test"
	cfg.Agent.PollInterval = 30 * time.Second
	cfg.Agent.ExpireAfter = time.Minute
	cfg.WebUI.Port = port
	cfg.MQTT.Broker = "tcp://127.0.0.1:1"
	path := filepath.Join(t.TempDir(), "config.yaml")
	write := func(c config.Config) {
		t.Helper()
		b, e := yaml.Marshal(c)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(path, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	write(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), path) }()
	defer func() {
		cancel()
		select {
		case e := <-done:
			if e != nil {
				t.Error(e)
			}
		case <-time.After(8 * time.Second):
			t.Error("server did not stop")
		}
	}()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	var page string
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		r, e := client.Get(base + "/")
		if e == nil {
			b, _ := io.ReadAll(r.Body)
			r.Body.Close()
			page = string(b)
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(page, "MQTT getrennt") {
		t.Fatal("UI unavailable while MQTT is offline")
	}
	token := regexp.MustCompile(`name="csrf" value="([^"]+)"`).FindStringSubmatch(page)
	if len(token) != 2 {
		t.Fatal("missing CSRF token")
	}
	var probes atomic.Int32
	probe := http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { probes.Add(1); w.WriteHeader(204) })}
	pl, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	go probe.Serve(pl)
	defer probe.Close()
	cfg.Agent.PollInterval = time.Second
	cfg.Modules.Services.Enabled = true
	cfg.Modules.Services.Checks = []config.Check{{Name: "probe", Type: "http", URL: "http://" + pl.Addr().String(), ExpectedStatus: 204, Timeout: time.Second}}
	cfg.MQTT.Broker = "tcp://127.0.0.1:2" // persisted only: must remain a restart notice
	write(cfg)
	reload := func() *http.Response {
		t.Helper()
		r, e := client.PostForm(base+"/config/reload", url.Values{"csrf": {token[1]}})
		if e != nil {
			t.Fatal(e)
		}
		r.Body.Close()
		return r
	}
	if r := reload(); r.StatusCode != 303 {
		t.Fatalf("reload: %d", r.StatusCode)
	}
	deadline = time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) && probes.Load() < 2 {
		time.Sleep(20 * time.Millisecond)
	}
	if probes.Load() < 2 {
		t.Fatal("service list / one-second interval not applied live")
	}
	r, e := client.Get(base + "/")
	if e != nil {
		t.Fatal(e)
	}
	b, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if !strings.Contains(string(b), "Änderung erfordert Neustart des Agents") {
		t.Fatal("missing MQTT restart notice")
	}
	if e = os.WriteFile(path, []byte("bad: ["), 0600); e != nil {
		t.Fatal(e)
	}
	if r := reload(); r.StatusCode != 400 {
		t.Fatal("invalid reload accepted")
	}
	before := probes.Load()
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && probes.Load() == before {
		time.Sleep(20 * time.Millisecond)
	}
	if probes.Load() == before {
		t.Fatal("invalid reload stopped active checks")
	}
}

func TestControlMasterGate(t *testing.T) {
	c := config.Defaults()
	c.ControlActions.Enabled = false
	c.AgentControl.Enabled = true
	c.HostControl.Enabled = true
	c.Modules.Docker.Enabled = true
	c.Modules.Docker.ControlContainers.Enabled = true
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if controlEnabled(c) {
		t.Fatal("master gate ignored in status")
	}
	entries, errors := prepareControls(context.Background(), c, log)
	if len(entries) != 0 || len(errors) != 0 {
		t.Fatal("disabled controls ran preflight")
	}
	client := mqtt.New(c, log)
	registry, e := registered(c, client, log)
	if e != nil {
		t.Fatal(e)
	}
	entries = registry.Actions(context.Background(), func(_ string, e error) { t.Errorf("disabled Docker control probed socket: %v", e) })
	if len(entries) != 0 {
		t.Fatal("Docker actions bypassed master gate")
	}
}
