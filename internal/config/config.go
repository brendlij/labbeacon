package config

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

type Agent struct {
	ID           string        `yaml:"id"`
	Name         string        `yaml:"name"`
	PollInterval time.Duration `yaml:"poll_interval"`
	ExpireAfter  time.Duration `yaml:"expire_after"`
	LogLevel     string        `yaml:"log_level"`
}
type MQTT struct {
	Broker          string `yaml:"broker"`
	Username        string `yaml:"username"`
	Password        string `yaml:"password"`
	DiscoveryPrefix string `yaml:"discovery_prefix"`
}
type System struct {
	Enabled   bool     `yaml:"enabled"`
	DiskPaths []string `yaml:"disk_paths"`
}
type Docker struct {
	Enabled    bool          `yaml:"enabled"`
	SocketPath string        `yaml:"socket_path"`
	Timeout    time.Duration `yaml:"timeout"`
}
type CLI struct {
	Enabled bool          `yaml:"enabled"`
	Command string        `yaml:"command"`
	Timeout time.Duration `yaml:"timeout"`
}
type Check struct {
	Name           string        `yaml:"name"`
	Type           string        `yaml:"type"`
	URL            string        `yaml:"url"`
	ExpectedStatus int           `yaml:"expected_status"`
	Timeout        time.Duration `yaml:"timeout"`
	Host           string        `yaml:"host"`
	Port           int           `yaml:"port"`
}
type Services struct {
	Enabled bool    `yaml:"enabled"`
	Checks  []Check `yaml:"checks"`
}
type Modules struct {
	System    System   `yaml:"system"`
	Docker    Docker   `yaml:"docker"`
	Services  Services `yaml:"services"`
	Tailscale CLI      `yaml:"tailscale"`
	Netbird   CLI      `yaml:"netbird"`
}
type Config struct {
	Agent   Agent   `yaml:"agent"`
	MQTT    MQTT    `yaml:"mqtt"`
	Modules Modules `yaml:"modules"`
}

func Defaults() Config {
	return Config{Agent: Agent{PollInterval: 20 * time.Second, ExpireAfter: 60 * time.Second, LogLevel: "info"},
		MQTT: MQTT{DiscoveryPrefix: "homeassistant"}, Modules: Modules{
			System: System{DiskPaths: []string{"/"}}, Docker: Docker{SocketPath: "/var/run/docker.sock", Timeout: 5 * time.Second},
			Tailscale: CLI{Command: "tailscale", Timeout: 5 * time.Second}, Netbird: CLI{Command: "netbird", Timeout: 5 * time.Second}}}
}

// Expand scalar values after parsing so secrets cannot inject YAML structure.
func expand(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode && n.Tag == "!!str" {
		var missing string
		n.Value = os.Expand(n.Value, func(k string) string {
			v, ok := os.LookupEnv(k)
			if !ok {
				missing = k
			}
			return v
		})
		if missing != "" {
			return fmt.Errorf("environment variable %s is not set", missing)
		}
	}
	for _, child := range n.Content {
		if err := expand(child); err != nil {
			return err
		}
	}
	return nil
}
func Load(path string) (Config, error) {
	c := Defaults()
	data, err := os.ReadFile(path)
	if err != nil {
		return c, fmt.Errorf("read config: %w", err)
	}
	var root yaml.Node
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	if err = dec.Decode(&root); err != nil {
		return c, fmt.Errorf("parse config: %w", err)
	}
	var extra yaml.Node
	if err = dec.Decode(&extra); err != io.EOF {
		return c, errors.New("config must contain exactly one YAML document")
	}
	if err = expand(&root); err != nil {
		return c, err
	}
	data, err = yaml.Marshal(&root)
	if err != nil {
		return c, err
	}
	strict := yaml.NewDecoder(strings.NewReader(string(data)))
	strict.KnownFields(true)
	if err = strict.Decode(&c); err != nil {
		return c, fmt.Errorf("decode config: %w", err)
	}
	for key, dst := range map[string]*string{"AGENT_ID": &c.Agent.ID, "AGENT_NAME": &c.Agent.Name, "MQTT_BROKER": &c.MQTT.Broker, "MQTT_USER": &c.MQTT.Username, "MQTT_PASSWORD": &c.MQTT.Password, "LOG_LEVEL": &c.Agent.LogLevel} {
		if v, ok := os.LookupEnv(key); ok {
			*dst = v
		}
	}
	for key, dst := range map[string]*time.Duration{"POLL_INTERVAL": &c.Agent.PollInterval, "EXPIRE_AFTER": &c.Agent.ExpireAfter} {
		if v, ok := os.LookupEnv(key); ok {
			*dst, err = time.ParseDuration(v)
			if err != nil {
				return c, fmt.Errorf("%s: invalid duration", key)
			}
		}
	}
	for i := range c.Modules.Services.Checks {
		ch := &c.Modules.Services.Checks[i]
		if ch.Timeout == 0 {
			ch.Timeout = 5 * time.Second
		}
		if ch.ExpectedStatus == 0 {
			ch.ExpectedStatus = 200
		}
	}
	return c, c.Validate()
}

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func (c Config) Validate() error {
	if !idPattern.MatchString(c.Agent.ID) {
		return errors.New("agent.id must contain only letters, digits, underscores or hyphens")
	}
	if strings.TrimSpace(c.Agent.Name) == "" {
		return errors.New("agent.name is required")
	}
	if c.Agent.PollInterval < time.Second {
		return errors.New("agent.poll_interval must be at least 1s")
	}
	if c.Agent.ExpireAfter <= c.Agent.PollInterval || c.Agent.ExpireAfter%time.Second != 0 {
		return errors.New("agent.expire_after must be whole seconds and greater than poll_interval")
	}
	switch c.Agent.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return errors.New("agent.log_level must be debug, info, warn or error")
	}
	u, err := url.Parse(c.MQTT.Broker)
	if err != nil || u.Hostname() == "" {
		return errors.New("mqtt.broker is required and must be an absolute broker URL")
	}
	switch u.Scheme {
	case "tcp", "ssl", "tls", "ws", "wss":
	default:
		return errors.New("mqtt.broker scheme must be tcp, ssl, tls, ws or wss")
	}
	if u.User != nil {
		return errors.New("use mqtt.username/password instead of credentials in broker URL")
	}
	if port := u.Port(); port != "" {
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 {
			return errors.New("mqtt.broker port must be between 1 and 65535")
		}
	}
	if c.MQTT.DiscoveryPrefix == "" || strings.ContainsAny(c.MQTT.DiscoveryPrefix, "+#\x00") || strings.HasSuffix(c.MQTT.DiscoveryPrefix, "/") {
		return errors.New("mqtt.discovery_prefix is invalid")
	}
	if c.Modules.System.Enabled {
		for _, p := range c.Modules.System.DiskPaths {
			if strings.TrimSpace(p) == "" {
				return errors.New("system.disk_paths contains an empty path")
			}
		}
	}
	if c.Modules.Docker.Enabled && (c.Modules.Docker.SocketPath == "" || c.Modules.Docker.Timeout <= 0) {
		return errors.New("docker requires socket_path and positive timeout")
	}
	for name, v := range map[string]CLI{"tailscale": c.Modules.Tailscale, "netbird": c.Modules.Netbird} {
		if v.Enabled && (v.Command == "" || v.Timeout <= 0) {
			return fmt.Errorf("%s requires command and positive timeout", name)
		}
	}
	names := map[string]bool{}
	if c.Modules.Services.Enabled {
		for _, ch := range c.Modules.Services.Checks {
			if ch.Name == "" || names[ch.Name] {
				return errors.New("service names must be nonempty and unique")
			}
			names[ch.Name] = true
			if ch.Timeout <= 0 {
				return fmt.Errorf("service %s: timeout must be positive", ch.Name)
			}
			switch ch.Type {
			case "http":
				u, e := url.Parse(ch.URL)
				if e != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || ch.ExpectedStatus < 100 || ch.ExpectedStatus > 599 {
					return fmt.Errorf("service %s: invalid HTTP URL or status", ch.Name)
				}
			case "tcp":
				if ch.Host == "" || ch.Port < 1 || ch.Port > 65535 {
					return fmt.Errorf("service %s: invalid TCP host/port", ch.Name)
				}
				if _, _, e := net.SplitHostPort(net.JoinHostPort(ch.Host, strconv.Itoa(ch.Port))); e != nil {
					return e
				}
			default:
				return fmt.Errorf("service %s: supported check types are http and tcp", ch.Name)
			}
		}
	}
	return nil
}
