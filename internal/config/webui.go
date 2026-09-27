package config

import (
	"fmt"
	"net"
	"slices"
	"strings"
)

type WebUI struct {
	Enabled      bool     `yaml:"enabled"`
	BindAddress  string   `yaml:"bind_address"`
	Port         int      `yaml:"port"`
	Username     string   `yaml:"username"`
	Password     string   `yaml:"password"`
	AllowedHosts []string `yaml:"allowed_hosts"`
}

func (c WebUI) Validate() error {
	if !c.Enabled {
		return nil
	}
	if net.ParseIP(c.BindAddress) == nil {
		return fmt.Errorf("webui.bind_address must be an explicit IP address")
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("webui.port must be 1..65535")
	}
	if (c.Username == "") != (c.Password == "") {
		return fmt.Errorf("webui.username and password must both be set or both empty")
	}
	if strings.Contains(c.Username, ":") {
		return fmt.Errorf("webui.username cannot contain a colon")
	}
	for _, h := range c.AllowedHosts {
		if h == "" || strings.ContainsAny(h, "/:*\r\n ") {
			return fmt.Errorf("webui.allowed_hosts must contain exact hostnames without ports")
		}
	}
	return nil
}

func (c Config) Clone() Config {
	c.WebUI.AllowedHosts = slices.Clone(c.WebUI.AllowedHosts)
	c.Modules.System.DiskPaths = slices.Clone(c.Modules.System.DiskPaths)
	c.Modules.Services.Checks = slices.Clone(c.Modules.Services.Checks)
	c.Modules.Docker.ControlContainers.Allow = slices.Clone(c.Modules.Docker.ControlContainers.Allow)
	c.Modules.Docker.ControlContainers.Deny = slices.Clone(c.Modules.Docker.ControlContainers.Deny)
	c.HostControl.Reboot.Command = slices.Clone(c.HostControl.Reboot.Command)
	c.HostControl.Reboot.Preflight = slices.Clone(c.HostControl.Reboot.Preflight)
	c.HostControl.Shutdown.Command = slices.Clone(c.HostControl.Shutdown.Command)
	c.HostControl.Shutdown.Preflight = slices.Clone(c.HostControl.Shutdown.Preflight)
	return c
}
