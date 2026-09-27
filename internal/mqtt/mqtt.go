package mqtt

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"github.com/brendlij/labbeacon/internal/config"
	"github.com/brendlij/labbeacon/internal/control"
	"github.com/brendlij/labbeacon/internal/metric"
	"github.com/brendlij/labbeacon/internal/version"
)

type Device struct {
	Identifiers  []string `json:"identifiers"`
	Name         string   `json:"name"`
	Manufacturer string   `json:"manufacturer"`
	Model        string   `json:"model,omitempty"`
	Version      string   `json:"sw_version"`
}
type Publisher interface {
	Publish(string, byte, bool, interface{}) paho.Token
}
type Client struct {
	expiry    atomic.Int64
	controlMu sync.RWMutex
	session   string
	Commands  chan control.Request
	buttons   map[string]bool
	observed  map[string]bool
	log       *slog.Logger
	client    paho.Client
	cfg       config.Config
	Wake      chan struct{}
}

func New(cfg config.Config, log *slog.Logger) *Client {
	c := &Client{cfg: cfg, Wake: make(chan struct{}, 1), Commands: make(chan control.Request, 8), buttons: map[string]bool{}, observed: map[string]bool{}, log: log}
	c.expiry.Store(int64(cfg.Agent.ExpireAfter))
	opts := paho.NewClientOptions().AddBroker(cfg.MQTT.Broker).SetClientID("labbeacon-"+cfg.Agent.ID).
		SetUsername(cfg.MQTT.Username).SetPassword(cfg.MQTT.Password).
		SetCleanSession(true).SetAutoReconnect(true).SetMaxReconnectInterval(30*time.Second).
		SetConnectTimeout(5*time.Second).SetWriteTimeout(5*time.Second).
		SetKeepAlive(15*time.Second).SetPingTimeout(5*time.Second).
		SetTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12}).
		SetWill(cfg.Agent.ID+"/availability", "offline", 1, true)
	opts.OnConnect = func(_ paho.Client) {
		c.controlMu.Lock()
		c.session = rand.Text()
		c.controlMu.Unlock()
		log.Info("MQTT connected")
		select {
		case c.Wake <- struct{}{}:
		default:
		}
	}
	opts.OnConnectionLost = func(_ paho.Client, err error) { c.RotateSession(); log.Warn("MQTT disconnected", "error", err) }
	c.client = paho.NewClient(opts)
	return c
}
func wait(ctx context.Context, t paho.Token) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.Done():
		return t.Error()
	}
}
func (c *Client) Connect(ctx context.Context, log *slog.Logger) error {
	delay := time.Second
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := wait(ctx, c.client.Connect()); err == nil {
			return nil
		} else {
			log.Warn("MQTT connect failed; retrying", "retry_in", delay, "error", err)
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		delay = min(delay*2, 30*time.Second)
	}
}
func Discovery(cfg config.Config, s metric.Sample) map[string]any {
	base := cfg.Agent.ID + "/" + s.Component + "/" + s.Key
	d := map[string]any{
		"name":                     s.Name,
		"unique_id":                cfg.Agent.ID + "_" + s.Key,
		"default_entity_id":        s.Component + "." + strings.ToLower(strings.ReplaceAll(cfg.Agent.ID, "-", "_")) + "_" + s.Key,
		"state_topic":              base + "/state",
		"value_template":           "{{ value_json.value }}",
		"json_attributes_topic":    base + "/state",
		"json_attributes_template": "{{ value_json.attributes | tojson }}",
		"availability_topic":       cfg.Agent.ID + "/availability",
		"payload_available":        "online",
		"payload_not_available":    "offline",
		"expire_after":             int(cfg.Agent.ExpireAfter / time.Second),
		"device":                   Device{Identifiers: []string{cfg.Agent.ID}, Name: cfg.Agent.Name, Manufacturer: "labbeacon", Version: version.Version},
	}
	if s.Unit != "" {
		d["unit_of_measurement"] = s.Unit
	}
	if s.DeviceClass != "" {
		d["device_class"] = s.DeviceClass
	}
	if s.StateClass != "" {
		d["state_class"] = s.StateClass
	}
	if s.Component == "binary_sensor" {
		d["payload_on"] = "ON"
		d["payload_off"] = "OFF"
	}
	return d
}
func PublishSample(ctx context.Context, p Publisher, cfg config.Config, s metric.Sample) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Component != "sensor" && s.Component != "binary_sensor" {
		return fmt.Errorf("unsupported component %q", s.Component)
	}
	discovery, err := json.Marshal(Discovery(cfg, s))
	if err != nil {
		return err
	}
	if err = wait(ctx, p.Publish(cfg.MQTT.DiscoveryPrefix+"/"+s.Component+"/"+cfg.Agent.ID+"/"+s.Key+"/config", 1, true, discovery)); err != nil {
		return err
	}
	attrs := s.Attributes
	if attrs == nil {
		attrs = map[string]any{}
	}
	state, err := json.Marshal(map[string]any{"value": s.Value, "attributes": attrs})
	if err != nil {
		return err
	}
	// Do not retain readings: replayed old values would restart expire_after.
	return wait(ctx, p.Publish(cfg.Agent.ID+"/"+s.Component+"/"+s.Key+"/state", 1, false, state))
}
func (c *Client) Publish(ctx context.Context, samples []metric.Sample) error {
	if err := c.Online(ctx); err != nil {
		return err
	}
	for _, s := range samples {
		cfg := c.cfg
		cfg.Agent.ExpireAfter = time.Duration(c.expiry.Load())
		if err := PublishSample(ctx, c.client, cfg, s); err != nil {
			return fmt.Errorf("publish %s: %w", s.Key, err)
		}
	}
	return nil
}
func (c *Client) SetExpiry(d time.Duration) { c.expiry.Store(int64(d)) }
func (c *Client) Connected() bool           { return c.client.IsConnectionOpen() }

// Online announces availability before the potentially slower collection cycle.
func (c *Client) Online(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !c.client.IsConnectionOpen() {
		return fmt.Errorf("MQTT connection unavailable")
	}
	return wait(ctx, c.client.Publish(c.cfg.Agent.ID+"/availability", 1, true, "online"))
}
func (c *Client) Close(ctx context.Context) error {
	var err error
	if c.client.IsConnectionOpen() {
		err = wait(ctx, c.client.Publish(c.cfg.Agent.ID+"/availability", 1, true, "offline"))
	}
	c.client.Disconnect(250)
	return err
}
