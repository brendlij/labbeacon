package webui

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
	"homelab-agent/internal/config"
)

func fixture(t *testing.T) (*Server, config.Config) {
	t.Helper()
	c := config.Defaults()
	c.Agent.ID = "test"
	c.Agent.Name = "Test server"
	c.MQTT.Broker = "tcp://localhost:1883"
	c.MQTT.Password = "must-not-appear"
	data, e := yaml.Marshal(c)
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if e = os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	s, e := New(&config.Store{Path: path}, NewState(c), c.WebUI, func() bool { return true }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if e != nil {
		t.Fatal(e)
	}
	return s, c
}
func fullForm(t *testing.T, s *Server) (url.Values, *http.Cookie) {
	t.Helper()
	d, e := s.Store.Read()
	if e != nil {
		t.Fatal(e)
	}
	values := url.Values{"form": {"settings"}, "revision": {d.Revision}}
	sections, _ := fields(d.Config)
	for _, section := range sections {
		for _, f := range section.Fields {
			if f.Locked {
				continue
			}
			if f.Kind == "bool" {
				if f.Checked {
					values.Set(f.Path, "on")
				}
			} else {
				values.Set(f.Path, f.Value)
			}
		}
	}
	_, services := serviceValues(d.Config.Modules.Services.Checks)
	values.Set("services_json", services)
	r := httptest.NewRequest("GET", "http://localhost/config", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("render failed %d: %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("no CSRF cookie")
	}
	values.Set("csrf", cookies[0].Value)
	return values, cookies[0]
}
func post(s *Server, path string, values url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "http://localhost"+path, strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "http://localhost")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func TestFormSaveAndRedaction(t *testing.T) {
	s, _ := fixture(t)
	values, cookie := fullForm(t, s)
	r := httptest.NewRequest("GET", "http://localhost/config", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if strings.Contains(w.Body.String(), "must-not-appear") {
		t.Fatal("password echoed into HTML")
	}
	values.Set("agent.poll_interval", "7s")
	w = post(s, "/config", values, cookie)
	if w.Code != 303 {
		t.Fatalf("save %d: %s", w.Code, w.Body.String())
	}
	next, ok := s.State.Take()
	if !ok || next.Agent.PollInterval != 7*time.Second || next.MQTT.Password != "must-not-appear" {
		t.Fatal("live update or password preservation failed")
	}
	d, e := s.Store.Read()
	if e != nil || d.Config.Agent.PollInterval != 7*time.Second {
		t.Fatal("not persisted")
	}
}
func TestCSRFAndOrigin(t *testing.T) {
	s, _ := fixture(t)
	values, cookie := fullForm(t, s)
	if w := post(s, "/config", values, nil); w.Code != 403 {
		t.Fatal("missing cookie accepted")
	}
	wrong := url.Values{}
	for k, v := range values {
		wrong[k] = append([]string(nil), v...)
	}
	wrong.Set("csrf", "wrong")
	if w := post(s, "/config/reload", wrong, cookie); w.Code != 403 {
		t.Fatal("invalid CSRF accepted")
	}
	r := httptest.NewRequest("POST", "http://localhost/config/reload", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "http://evil.example")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin accepted")
	}
}
func TestDangerAcknowledgmentAndConflict(t *testing.T) {
	s, _ := fixture(t)
	v, cookie := fullForm(t, s)
	v.Del("host_control.reboot.confirm_required")
	if w := post(s, "/config", v, cookie); w.Code != 400 {
		t.Fatal("dangerous change accepted without acknowledgement")
	}
	v.Set("ack_danger", "yes")
	if w := post(s, "/config", v, cookie); w.Code != 303 {
		t.Fatalf("acknowledged save failed: %s", w.Body.String())
	}
	if w := post(s, "/config", v, cookie); w.Code != 409 {
		t.Fatal("stale form accepted")
	}
}
func TestAuthenticationAndHost(t *testing.T) {
	s, _ := fixture(t)
	s.Settings.Username = "admin"
	s.Settings.Password = "password"
	for _, path := range []string{"/", "/config", "/config/export", "/assets/app.js"} {
		r := httptest.NewRequest("GET", "http://localhost"+path, nil)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("unprotected %s", path)
		}
		r.SetBasicAuth("admin", "password")
		w = httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("valid login denied %s: %s", path, w.Body.String())
		}
	}
	r := httptest.NewRequest("GET", "http://attacker.example/", nil)
	r.SetBasicAuth("admin", "password")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("DNS rebinding hostname accepted")
	}
}
func TestLiveConfigKeepsRestartOnlySettings(t *testing.T) {
	old := config.Defaults()
	next := old.Clone()
	next.MQTT.Broker = "tcp://new:1883"
	next.WebUI.Port = 8123
	next.Agent.PollInterval = 3 * time.Second
	next.Modules.Services.Enabled = true
	live := LiveConfig(old, next)
	if live.MQTT != old.MQTT || live.WebUI.Port != old.WebUI.Port || live.Agent.PollInterval != 3*time.Second || !live.Modules.Services.Enabled || len(RestartFields(old, next)) != 2 {
		t.Fatal("incorrect live/restart split")
	}
}

func TestServicesDockerAndExport(t *testing.T) {
	s, _ := fixture(t)
	s.State.Report(nil, []Container{{Name: "web", Status: "running"}, {Name: "db", Status: "running"}})
	v, cookie := fullForm(t, s)
	v.Set("modules.services.enabled", "on")
	v.Set("services_json", `[{"name":"Health","type":"http","url":"http://localhost/health","expected_status":204,"timeout":"2s"},{"name":"Database","type":"tcp","host":"localhost","port":5432,"timeout":"3s"}]`)
	v.Add("docker_allow", "web")
	v.Add("docker_deny", "db")
	if w := post(s, "/config", v, cookie); w.Code != 303 {
		t.Fatalf("save: %s", w.Body.String())
	}
	d, e := s.Store.Read()
	if e != nil {
		t.Fatal(e)
	}
	if len(d.Config.Modules.Services.Checks) != 2 || d.Config.Modules.Services.Checks[1].Port != 5432 || len(d.Config.Modules.Docker.ControlContainers.Deny) != 1 {
		t.Fatal("service or Docker values lost")
	}
	r := httptest.NewRequest("GET", "http://localhost/config/export", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") || w.Body.String() != string(d.YAML) {
		t.Fatal("export differs from saved YAML")
	}
	v, cookie = fullForm(t, s)
	v.Set("services_json", `[{"name":"Health edited","type":"http","url":"http://localhost/new","expected_status":200,"timeout":"1s"}]`)
	if w := post(s, "/config", v, cookie); w.Code != 303 {
		t.Fatal(w.Body.String())
	}
	d, e = s.Store.Read()
	if e != nil || len(d.Config.Modules.Services.Checks) != 1 || d.Config.Modules.Services.Checks[0].Name != "Health edited" {
		t.Fatal("edit/delete failed")
	}
	v, cookie = fullForm(t, s)
	v.Set("services_json", `[{"name":"Invalid","type":"tcp","port":99999,"timeout":"1s"}]`)
	if w := post(s, "/config", v, cookie); w.Code != 400 {
		t.Fatal("invalid service accepted")
	}
}

func TestReloadRequiresDangerAcknowledgment(t *testing.T) {
	s, c := fixture(t)
	v, cookie := fullForm(t, s)
	c.HostControl.Enabled = true
	data, e := yaml.Marshal(c)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(s.Store.Path, data, 0600); e != nil {
		t.Fatal(e)
	}
	if w := post(s, "/config/reload", v, cookie); w.Code != 400 {
		t.Fatal("unconfirmed reload accepted")
	}
	v.Set("ack_danger", "yes")
	if w := post(s, "/config/reload", v, cookie); w.Code != 303 {
		t.Fatal(w.Body.String())
	}
	if next, ok := s.State.Take(); !ok || !next.HostControl.Enabled {
		t.Fatal("reload not queued")
	}
	c.ControlActions.Enabled = false
	next := c.Clone()
	next.ControlActions.Enabled = true
	if !Dangerous(c, next) {
		t.Fatal("master switch bypasses host acknowledgment")
	}
}
