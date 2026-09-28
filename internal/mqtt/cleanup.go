package mqtt

import (
	"context"
	"strings"

	"github.com/brendlij/labbeacon/internal/config"
	"github.com/brendlij/labbeacon/internal/metric"
	paho "github.com/eclipse/paho.mqtt.golang"
)

func (c *Client) SetServices(services config.Services) {
	c.serviceKeys = map[string]bool{}
	if services.Enabled {
		for _, ch := range services.Checks {
			if ch.Type == "systemd" {
				c.serviceKeys["service_"+metric.Key(ch.Name)] = true
			}
		}
	}
}

func obsolete(component, key string, selected map[string]bool) bool {
	for _, prefix := range []string{"net_lo_", "net_veth", "net_docker", "net_br_", "net_tailscale", "net_wt0"} {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	if key == "top_processes" || key == "process_count" || key == "open_file_descriptors" {
		return true
	}
	if strings.HasPrefix(key, "tailscale_") || strings.HasPrefix(key, "netbird_") || key == "public_ip" || (strings.HasPrefix(key, "net_") && strings.HasSuffix(key, "_ips")) || strings.HasSuffix(key, "_image_update") {
		return true
	}
	if strings.HasPrefix(key, "service_") {
		return component == "binary_sensor" || !selected[key]
	}
	return false
}

// Retained discoveries are observed across restarts. Reconciliation is retried
// each poll and only touches this agent's obsolete topics, never other devices.
func (c *Client) cleanup(ctx context.Context) error {
	for _, component := range []string{"sensor", "binary_sensor"} {
		prefix := c.cfg.MQTT.DiscoveryPrefix + "/" + component + "/" + c.cfg.Agent.ID + "/"
		if err := wait(ctx, c.client.Subscribe(prefix+"+/config", 1, func(_ paho.Client, msg paho.Message) {
			if len(msg.Payload()) == 0 {
				return
			}
			c.controlMu.Lock()
			c.observedSensors[msg.Topic()] = true
			c.controlMu.Unlock()
		})); err != nil {
			return err
		}
		c.controlMu.Lock()
		var remove []string
		for topic := range c.observedSensors {
			if strings.HasPrefix(topic, prefix) {
				key := strings.TrimSuffix(strings.TrimPrefix(topic, prefix), "/config")
				if obsolete(component, key, c.serviceKeys) {
					remove = append(remove, topic)
				}
			}
		}
		c.controlMu.Unlock()
		for _, topic := range remove {
			if err := wait(ctx, c.client.Publish(topic, 1, true, []byte{})); err != nil {
				return err
			}
			c.controlMu.Lock()
			delete(c.observedSensors, topic)
			c.controlMu.Unlock()
		}
	}
	return nil
}
