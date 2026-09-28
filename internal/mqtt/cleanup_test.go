package mqtt

import "testing"

func TestObsoleteDiscoveryScope(t *testing.T) {
	selected := map[string]bool{"service_selected": true}
	for _, key := range []string{"tailscale_online", "netbird_ip", "public_ip", "net_eth0_ips", "container_abc_image_update", "service_removed"} {
		if !obsolete("sensor", key, selected) {
			t.Fatalf("legacy discovery retained: %s", key)
		}
	}
	if !obsolete("binary_sensor", "service_selected", selected) {
		t.Fatal("legacy HTTP entity retained")
	}
	for _, key := range []string{"service_selected", "cpu_percent", "container_abc_status", "container_abc_ram", "net_eth0_rx"} {
		if obsolete("sensor", key, selected) {
			t.Fatalf("active discovery removed: %s", key)
		}
	}
}
