package config

import (
	"fmt"
	"net/url"
	"regexp"
	"time"
)

type CommandAction struct {
	Enabled         bool     `yaml:"enabled"`
	Command         []string `yaml:"command"`
	Preflight       []string `yaml:"preflight"`
	ConfirmRequired bool     `yaml:"confirm_required"`
}
type HostControl struct {
	Enabled  bool          `yaml:"enabled"`
	Timeout  time.Duration `yaml:"timeout"`
	Reboot   CommandAction `yaml:"reboot"`
	Shutdown CommandAction `yaml:"shutdown"`
}
type AgentControl struct {
	Enabled bool `yaml:"enabled"`
}
type ContainerPolicy struct {
	Enabled bool     `yaml:"enabled"`
	Allow   []string `yaml:"allow"`
	Deny    []string `yaml:"deny"`
}

func (p ContainerPolicy) Permits(name string) bool {
	if !p.Enabled {
		return false
	}
	for _, s := range p.Deny {
		if s == name {
			return false
		}
	}
	if len(p.Allow) == 0 {
		return true
	}
	for _, s := range p.Allow {
		if s == name {
			return true
		}
	}
	return false
}

type ImageUpdates struct {
	Enabled  bool          `yaml:"enabled"`
	Interval time.Duration `yaml:"interval"`
	Timeout  time.Duration `yaml:"timeout"`
}
type PublicIP struct {
	Enabled  bool          `yaml:"enabled"`
	Endpoint string        `yaml:"endpoint"`
	Interval time.Duration `yaml:"interval"`
	Timeout  time.Duration `yaml:"timeout"`
}
type Network struct {
	Enabled  bool     `yaml:"enabled"`
	LocalIPs bool     `yaml:"local_ips"`
	PublicIP PublicIP `yaml:"public_ip"`
}

var unitPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.@:-]*\.service$`)

func (c Config) validateExtensions() error {
	if c.Modules.System.TopN < 1 || c.Modules.System.TopN > 100 {
		return fmt.Errorf("system.top_n must be 1..100")
	}
	if c.Modules.Docker.ControlContainers.Enabled && !c.Modules.Docker.Enabled {
		return fmt.Errorf("docker control requires docker.enabled")
	}
	if c.Modules.Docker.ImageUpdates.Enabled && (c.Modules.Docker.ImageUpdates.Interval < time.Minute || c.Modules.Docker.ImageUpdates.Timeout <= 0) {
		return fmt.Errorf("docker.image_updates requires interval >= 1m and positive timeout")
	}
	if c.HostControl.Enabled {
		if c.HostControl.Timeout <= 0 {
			return fmt.Errorf("host_control.timeout must be positive")
		}
		for name, a := range map[string]CommandAction{"reboot": c.HostControl.Reboot, "shutdown": c.HostControl.Shutdown} {
			if a.Enabled && (len(a.Command) == 0 || a.Command[0] == "" || len(a.Preflight) == 0 || a.Preflight[0] == "") {
				return fmt.Errorf("host_control.%s requires command and read-only preflight argv", name)
			}
		}
	}
	for _, ch := range c.Modules.Services.Checks {
		if ch.AllowControl && (!c.Modules.Services.Enabled || !unitPattern.MatchString(ch.SystemdUnit)) {
			return fmt.Errorf("service %s control requires enabled services and an exact .service unit name", ch.Name)
		}
	}
	p := c.Modules.Network.PublicIP
	if c.Modules.Network.Enabled && p.Enabled {
		u, e := url.Parse(p.Endpoint)
		if e != nil || u.Hostname() == "" || u.Scheme != "https" || u.User != nil || p.Interval < time.Minute || p.Timeout <= 0 {
			return fmt.Errorf("network.public_ip requires HTTPS endpoint without credentials, interval >= 1m and positive timeout")
		}
	}
	return nil
}
