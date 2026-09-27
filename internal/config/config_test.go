package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const valid = "agent:\n  id: srv-01\n  name: Server\nmqtt:\n  broker: tcp://localhost:1883\n"

func loadText(t *testing.T, s string) (Config, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(s), 0600); err != nil {
		t.Fatal(err)
	}
	return Load(p)
}
func TestLoadDefaultsAndSecrets(t *testing.T) {
	t.Setenv("TEST_SECRET", "pass: # \" ' \nnext: value")
	t.Setenv("AGENT_ID", "override")
	t.Setenv("POLL_INTERVAL", "10s")
	c, err := loadText(t, valid+"  password: ${TEST_SECRET}\n")
	if err != nil {
		t.Fatal(err)
	}
	if c.MQTT.Password != os.Getenv("TEST_SECRET") || c.Agent.ID != "override" || c.Agent.PollInterval != 10*time.Second || c.Agent.ExpireAfter != 60*time.Second {
		t.Fatalf("defaults/expansion/overrides failed")
	}
}
func TestInvalidConfig(t *testing.T) {
	for _, tc := range []struct{ name, text string }{
		{"missing broker", "agent: {id: srv, name: Server}"},
		{"unknown key", valid + "  typo: true\n"},
		{"multiple documents", valid + "---\n" + valid},
		{"topic wildcard", strings.Replace(valid, "srv-01", "srv/+", 1)},
		{"unknown check", valid + "modules:\n  services:\n    enabled: true\n    checks: [{name: test, type: ping}]\n"},
		{"missing env", valid + "  password: ${HOMELAB_TEST_MISSING_123}\n"},
		{"duplicate service", valid + "modules:\n  services:\n    enabled: true\n    checks: [{name: web, type: tcp, host: localhost, port: 80}, {name: web, type: tcp, host: localhost, port: 81}]\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := loadText(t, tc.text); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
func TestExample(t *testing.T) {
	t.Setenv("MQTT_USER", "")
	t.Setenv("MQTT_PASSWORD", "")
	if _, err := Load("../../configs/config.example.yaml"); err != nil {
		t.Fatal(err)
	}
}
