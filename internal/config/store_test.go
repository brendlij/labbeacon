package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func makeStore(t *testing.T, text string) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return &Store{Path: path}
}
func TestStorePreservesReferencesAndComments(t *testing.T) {
	t.Setenv("STORE_PASSWORD", "secret:$with: punctuation")
	t.Setenv("CHECK_URL", "https://example.test/private-token")
	s := makeStore(t, valid+"  password: ${STORE_PASSWORD} # keep secret outside YAML\nmodules:\n  services:\n    enabled: true\n    checks:\n      - name: existing\n        type: http\n        url: ${CHECK_URL}\n")
	d, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	next := d.Config.Clone()
	next.Agent.PollInterval = 10 * time.Second
	next.Modules.Services.Checks = append(next.Modules.Services.Checks, Check{Name: "new", Type: "tcp", Host: "localhost", Port: 80, Timeout: time.Second, ExpectedStatus: 200})
	saved, err := s.Save(d.Revision, next)
	if err != nil {
		t.Fatal(err)
	}
	text := string(saved.YAML)
	if !strings.Contains(text, "${STORE_PASSWORD}") || !strings.Contains(text, "${CHECK_URL}") || !strings.Contains(text, "keep secret") || strings.Contains(text, "private-token") || strings.Contains(text, "secret:$") {
		t.Fatalf("reference or comment lost: %s", text)
	}
	if saved.Config.Agent.PollInterval != 10*time.Second || len(saved.Config.Modules.Services.Checks) != 2 {
		t.Fatal("changes missing")
	}
	files, err := filepath.Glob(filepath.Join(filepath.Dir(s.Path), ".homelab-config-*.tmp"))
	if err != nil || len(files) != 0 {
		t.Fatal("temporary file left behind")
	}
}
func TestStoreConflictValidationAndOverrides(t *testing.T) {
	s := makeStore(t, valid)
	d, e := s.Read()
	if e != nil {
		t.Fatal(e)
	}
	bad := d.Config
	bad.Agent.PollInterval = 0
	if _, e = s.Save(d.Revision, bad); e == nil {
		t.Fatal("invalid config saved")
	}
	data, e := os.ReadFile(s.Path)
	if e != nil || string(data) != valid {
		t.Fatal("original was changed")
	}
	if e = os.WriteFile(s.Path, []byte(valid+"# edited externally\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Save(d.Revision, d.Config); e != ErrConflict {
		t.Fatalf("want conflict: %v", e)
	}
	t.Setenv("MQTT_USER", "managed")
	d, e = s.Read()
	if e != nil {
		t.Fatal(e)
	}
	next := d.Config
	next.MQTT.Username = "override"
	if _, e = s.Save(d.Revision, next); e == nil {
		t.Fatal("ENV override should be locked")
	}
}
func TestPasswordDollarRoundtrip(t *testing.T) {
	s := makeStore(t, valid)
	d, e := s.Read()
	if e != nil {
		t.Fatal(e)
	}
	next := d.Config
	next.WebUI.Username = "admin"
	next.WebUI.Password = "long$literal${not-an-env}:#pass"
	saved, e := s.Save(d.Revision, next)
	if e != nil {
		t.Fatal(e)
	}
	if saved.Config.WebUI.Password != next.WebUI.Password {
		t.Fatal("password changed")
	}
}
func TestAtomicWriteRejectsSymlink(t *testing.T) {
	s := makeStore(t, valid)
	link := filepath.Join(filepath.Dir(s.Path), "link.yaml")
	if err := os.Symlink(s.Path, link); err != nil {
		t.Skip("symlinks unavailable")
	}
	if err := AtomicWrite(link, []byte("bad")); err == nil {
		t.Fatal("symlink accepted")
	}
	data, err := os.ReadFile(s.Path)
	if err != nil || string(data) != valid {
		t.Fatal("target changed")
	}
}
