package config

import "testing"

func TestContainerPolicy(t *testing.T) {
	p := ContainerPolicy{Enabled: true, Deny: []string{"postgres"}}
	if !p.Permits("web") || p.Permits("postgres") {
		t.Fatal("denylist")
	}
	p.Allow = []string{"postgres", "web"}
	if p.Permits("other") || p.Permits("postgres") || !p.Permits("web") {
		t.Fatal("allow/deny precedence")
	}
	p.Enabled = false
	if p.Permits("web") {
		t.Fatal("disabled")
	}
}
func TestUnsafeConfig(t *testing.T) {
	for _, suffix := range []string{
		"modules:\n  services:\n    enabled: true\n    checks: [{name: x, type: tcp, host: localhost, port: 80, allow_control: true, systemd_unit: '--all'}]\n",
		"modules:\n  docker:\n    control_containers: {enabled: true}\n",
		"modules:\n  network:\n    enabled: true\n    public_ip: {enabled: true, endpoint: 'http://example.com'}\n",
		"host_control:\n  enabled: true\n  reboot: {enabled: true, command: []}\n",
	} {
		if _, err := loadText(t, valid+suffix); err == nil {
			t.Fatalf("unsafe config accepted: %s", suffix)
		}
	}
}
