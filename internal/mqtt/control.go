package mqtt

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"github.com/brendlij/labbeacon/internal/config"
	"github.com/brendlij/labbeacon/internal/control"
	"github.com/brendlij/labbeacon/internal/version"
)

func (c *Client) Session() string { c.controlMu.RLock(); defer c.controlMu.RUnlock(); return c.session }
func (c *Client) RotateSession()  { c.controlMu.Lock(); c.session = rand.Text(); c.controlMu.Unlock() }
func ButtonDiscovery(cfg config.Config, e control.Entry, session string) map[string]any {
	payload, _ := json.Marshal(map[string]any{"session": session, "confirm": false})
	return map[string]any{
		"name": e.Name, "unique_id": cfg.Agent.ID + "_" + e.Action.ID(), "default_entity_id": "button." + strings.ToLower(strings.ReplaceAll(cfg.Agent.ID, "-", "_")) + "_" + e.Action.ID(),
		"command_topic": cfg.Agent.ID + "/button/" + e.Action.ID() + "/command", "payload_press": string(payload), "qos": 0, "retain": false,
		"availability_topic": cfg.Agent.ID + "/availability", "payload_available": "online", "payload_not_available": "offline", "entity_category": "config",
		"device": Device{Identifiers: []string{cfg.Agent.ID}, Name: cfg.Agent.Name, Manufacturer: "labbeacon", Version: version.Version},
	}
}
func (c *Client) receiveCommand(_ paho.Client, msg paho.Message) {
	reject := func(reason string) {
		c.log.Warn("control rejected", "topic", msg.Topic(), "reason", reason, "retained", msg.Retained(), "duplicate", msg.Duplicate())
	}
	if msg.Retained() || msg.Duplicate() {
		reject("retained or duplicate command")
		return
	}
	if len(msg.Payload()) > 512 {
		reject("oversized command")
		return
	}
	var body struct {
		Session string `json:"session"`
		Confirm bool   `json:"confirm"`
	}
	if err := json.Unmarshal(msg.Payload(), &body); err != nil {
		reject("invalid command JSON")
		return
	}
	if body.Session == "" || body.Session != c.Session() {
		reject("stale session")
		return
	}
	prefix := c.cfg.Agent.ID + "/button/"
	topic := msg.Topic()
	if !strings.HasPrefix(topic, prefix) || !strings.HasSuffix(topic, "/command") {
		reject("invalid topic")
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(topic, prefix), "/command")
	request := control.Request{ID: id, Topic: topic, Session: body.Session, Confirmed: body.Confirm, Received: time.Now(), QoS: msg.Qos()}
	select {
	case c.Commands <- request:
	default:
		reject("command queue full")
	}
}

// SyncActions removes vanished discovery and publishes only preflighted actions.
func (c *Client) SyncActions(ctx context.Context, entries []control.Entry) error {
	if !c.client.IsConnectionOpen() {
		return fmt.Errorf("MQTT unavailable")
	}
	// Discover retained buttons from older processes as well, including when all
	// control has now been disabled. Reconciliation finishes on the next poll.
	filter := c.cfg.MQTT.DiscoveryPrefix + "/button/" + c.cfg.Agent.ID + "/+/config"
	if err := wait(ctx, c.client.Subscribe(filter, 1, func(_ paho.Client, msg paho.Message) {
		if len(msg.Payload()) == 0 {
			return
		}
		prefix := c.cfg.MQTT.DiscoveryPrefix + "/button/" + c.cfg.Agent.ID + "/"
		id := strings.TrimSuffix(strings.TrimPrefix(msg.Topic(), prefix), "/config")
		c.controlMu.Lock()
		c.observed[id] = true
		c.controlMu.Unlock()
	})); err != nil {
		return err
	}
	c.controlMu.Lock()
	for id := range c.observed {
		c.buttons[id] = true
	}
	c.observed = map[string]bool{}
	c.controlMu.Unlock()
	next := map[string]bool{}
	for _, e := range entries {
		next[e.Action.ID()] = true
	}
	for id := range c.buttons {
		if !next[id] {
			if err := wait(ctx, c.client.Publish(c.cfg.MQTT.DiscoveryPrefix+"/button/"+c.cfg.Agent.ID+"/"+id+"/config", 1, true, []byte{})); err != nil {
				return err
			}
			delete(c.buttons, id)
		}
	}
	if len(entries) > 0 {
		if err := wait(ctx, c.client.Subscribe(c.cfg.Agent.ID+"/button/+/command", 0, c.receiveCommand)); err != nil {
			return err
		}
	} else {
		if err := wait(ctx, c.client.Unsubscribe(c.cfg.Agent.ID+"/button/+/command")); err != nil {
			return err
		}
	}
	for _, e := range entries {
		data, err := json.Marshal(ButtonDiscovery(c.cfg, e, c.Session()))
		if err != nil {
			return err
		}
		if err = wait(ctx, c.client.Publish(c.cfg.MQTT.DiscoveryPrefix+"/button/"+c.cfg.Agent.ID+"/"+e.Action.ID()+"/config", 1, true, data)); err != nil {
			return err
		}
		c.buttons[e.Action.ID()] = true
	}
	// A discoverable diagnostic attribute exposes the current nonce to HA scripts.
	if len(entries) > 0 {
		data, err := json.Marshal(map[string]any{"session": c.Session()})
		if err != nil {
			return err
		}
		if err = wait(ctx, c.client.Publish(c.cfg.Agent.ID+"/control/session", 1, true, data)); err != nil {
			return err
		}
	}
	return nil
}
func (c *Client) Announce(ctx context.Context, state string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return wait(ctx, c.client.Publish(c.cfg.Agent.ID+"/availability", 1, true, state))
}
