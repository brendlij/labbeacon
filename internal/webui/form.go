package webui

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/brendlij/labbeacon/internal/config"
	"go.yaml.in/yaml/v3"
)

type field struct {
	Path, Label, Kind, Value   string
	Checked, Locked, SecretSet bool
}
type section struct {
	Title, Hint string
	Fields      []field
}
type choice struct {
	Name, Status string
	Allow, Deny  bool
}

func specs() []section {
	return []section{
		{Title: "Agent & MQTT", Hint: "Interval and expiry changes apply live. Identity, log level and MQTT connection changes require a restart.", Fields: []field{
			{Path: "agent.id", Label: "Agent ID", Kind: "text"}, {Path: "agent.name", Label: "Name", Kind: "text"}, {Path: "agent.poll_interval", Label: "Poll interval (e.g. 20s)", Kind: "text"}, {Path: "agent.expire_after", Label: "Expire after (e.g. 60s)", Kind: "text"}, {Path: "agent.log_level", Label: "Log level: debug / info / warn / error", Kind: "text"}, {Path: "agent.metrics_enabled", Label: "Agent version and uptime", Kind: "bool"},
			{Path: "mqtt.broker", Label: "MQTT broker", Kind: "text"}, {Path: "mqtt.username", Label: "MQTT username", Kind: "text"}, {Path: "mqtt.password", Label: "MQTT password", Kind: "password"}, {Path: "mqtt.discovery_prefix", Label: "Discovery prefix", Kind: "text"}}},
		{Title: "System", Hint: "Local metrics. Lists: one entry per line.", Fields: []field{
			{Path: "modules.system.enabled", Label: "Enable system module", Kind: "bool"}, {Path: "modules.system.disk_paths", Label: "Disk paths", Kind: "lines"}, {Path: "modules.system.temperature", Label: "CPU temperature", Kind: "bool"}, {Path: "modules.system.processes", Label: "Top processes", Kind: "bool"}, {Path: "modules.system.top_n", Label: "Top-N (1–100)", Kind: "number"}, {Path: "modules.system.file_descriptors", Label: "File descriptors", Kind: "bool"}, {Path: "modules.system.boot_time", Label: "Boot time", Kind: "bool"}}},
		{Title: "Docker", Hint: "Monitoring and control have separate switches. Select permissions below.", Fields: []field{
			{Path: "modules.docker.enabled", Label: "Enable Docker module", Kind: "bool"}, {Path: "modules.docker.socket_path", Label: "Socket path", Kind: "text"}, {Path: "modules.docker.timeout", Label: "Inventory timeout", Kind: "text"}, {Path: "modules.docker.stats", Label: "Container CPU / RAM", Kind: "bool"}, {Path: "modules.docker.control_containers.enabled", Label: "Allow container control", Kind: "bool"}, {Path: "modules.docker.image_updates.enabled", Label: "Anonymous image update checks", Kind: "bool"}, {Path: "modules.docker.image_updates.interval", Label: "Update check interval", Kind: "text"}, {Path: "modules.docker.image_updates.timeout", Label: "Registry timeout", Kind: "text"}}},
		{Title: "Services & VPN", Hint: "Edit HTTP/TCP checks in the service list. VPN monitoring requires the corresponding local CLI.", Fields: []field{
			{Path: "modules.services.enabled", Label: "Enable service checks", Kind: "bool"}, {Path: "modules.tailscale.enabled", Label: "Enable Tailscale", Kind: "bool"}, {Path: "modules.tailscale.command", Label: "Tailscale executable", Kind: "text"}, {Path: "modules.tailscale.timeout", Label: "Tailscale timeout", Kind: "text"}, {Path: "modules.netbird.enabled", Label: "Enable NetBird", Kind: "bool"}, {Path: "modules.netbird.command", Label: "NetBird executable", Kind: "text"}, {Path: "modules.netbird.timeout", Label: "NetBird timeout", Kind: "text"}}},
		{Title: "Network", Hint: "The public IP address is retrieved over HTTPS and cached.", Fields: []field{
			{Path: "modules.network.enabled", Label: "Enable network module", Kind: "bool"}, {Path: "modules.network.local_ips", Label: "Local IP addresses", Kind: "bool"}, {Path: "modules.network.public_ip.enabled", Label: "Public IP address", Kind: "bool"}, {Path: "modules.network.public_ip.endpoint", Label: "HTTPS endpoint", Kind: "text"}, {Path: "modules.network.public_ip.interval", Label: "IP cache interval", Kind: "text"}, {Path: "modules.network.public_ip.timeout", Label: "IP request timeout", Kind: "text"}}},
		{Title: "Control actions", Hint: "Enabling these options allows future MQTT commands. This page does not execute control actions. Commands remain configured in the local YAML file.", Fields: []field{
			{Path: "control_actions.enabled", Label: "Control actions – master switch", Kind: "bool"}, {Path: "agent_control.enabled", Label: "Allow agent restart (requires a supervisor)", Kind: "bool"}, {Path: "host_control.enabled", Label: "Allow host control", Kind: "bool"}, {Path: "host_control.timeout", Label: "Host command timeout", Kind: "text"}, {Path: "host_control.reboot.enabled", Label: "Enable reboot action", Kind: "bool"}, {Path: "host_control.reboot.confirm_required", Label: "Require MQTT confirmation for reboot", Kind: "bool"}, {Path: "host_control.shutdown.enabled", Label: "Enable shutdown action", Kind: "bool"}, {Path: "host_control.shutdown.confirm_required", Label: "Require MQTT confirmation for shutdown", Kind: "bool"}}},
		{Title: "Web UI", Hint: "Changes in this section require a restart. Without TLS, login credentials are sent without encryption.", Fields: []field{
			{Path: "webui.enabled", Label: "Enable web UI", Kind: "bool"}, {Path: "webui.bind_address", Label: "Bind address", Kind: "text"}, {Path: "webui.port", Label: "Port", Kind: "number"}, {Path: "webui.username", Label: "Login username", Kind: "text"}, {Path: "webui.password", Label: "Login password", Kind: "password"}, {Path: "webui.allowed_hosts", Label: "Allowed DNS names (without ports)", Kind: "lines"}}},
	}
}
func nodeAt(n *yaml.Node, path string) *yaml.Node {
	for _, key := range strings.Split(path, ".") {
		var next *yaml.Node
		for i := 0; i < len(n.Content); i += 2 {
			if n.Content[i].Value == key {
				next = n.Content[i+1]
				break
			}
		}
		if next == nil {
			return nil
		}
		n = next
	}
	return n
}
func fields(c config.Config) ([]section, []string) {
	var n yaml.Node
	if err := n.Encode(c); err != nil {
		return nil, nil
	}
	sections := specs()
	locked := map[string]bool{}
	var overrides []string
	for env, path := range config.Overrides() {
		if _, ok := os.LookupEnv(env); ok {
			locked[path] = true
			overrides = append(overrides, path+" ← "+env)
		}
	}
	sort.Strings(overrides)
	for i := range sections {
		for j := range sections[i].Fields {
			f := &sections[i].Fields[j]
			v := nodeAt(&n, f.Path)
			if v == nil {
				continue
			}
			f.Locked = locked[f.Path]
			switch f.Kind {
			case "bool":
				f.Checked = v.Value == "true"
			case "password":
				f.SecretSet = v.Value != ""
			case "lines":
				var values []string
				for _, item := range v.Content {
					values = append(values, item.Value)
				}
				f.Value = strings.Join(values, "\n")
			default:
				f.Value = v.Value
			}
		}
	}
	return sections, overrides
}

type serviceForm struct {
	Name           string `json:"name"`
	Type           string `json:"type"`
	URL            string `json:"url"`
	ExpectedStatus int    `json:"expected_status"`
	Timeout        string `json:"timeout"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	SystemdUnit    string `json:"systemd_unit"`
	AllowControl   bool   `json:"allow_control"`
}

func serviceValues(checks []config.Check) ([]serviceForm, string) {
	out := make([]serviceForm, 0, len(checks))
	for _, c := range checks {
		out = append(out, serviceForm{Name: c.Name, Type: c.Type, URL: c.URL, ExpectedStatus: c.ExpectedStatus, Timeout: c.Timeout.String(), Host: c.Host, Port: c.Port, SystemdUnit: c.SystemdUnit, AllowControl: c.AllowControl})
	}
	data, _ := json.Marshal(out)
	return out, string(data)
}
func containerChoices(c config.Config, current []Container) []choice {
	all := map[string]choice{}
	for _, v := range current {
		all[v.Name] = choice{Name: v.Name, Status: v.Status}
	}
	for _, name := range c.Modules.Docker.ControlContainers.Allow {
		v := all[name]
		v.Name = name
		v.Allow = true
		all[name] = v
	}
	for _, name := range c.Modules.Docker.ControlContainers.Deny {
		v := all[name]
		v.Name = name
		v.Deny = true
		all[name] = v
	}
	out := make([]choice, 0, len(all))
	for _, v := range all {
		if v.Status == "" {
			v.Status = "currently not found"
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
func lines(value string) []string {
	out := []string{}
	for _, s := range strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n") {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
func parseForm(current config.Config, values url.Values) (config.Config, error) {
	if values.Get("form") != "settings" {
		return config.Config{}, fmt.Errorf("Invalid form")
	}
	var n yaml.Node
	if err := n.Encode(current); err != nil {
		return config.Config{}, err
	}
	for _, section := range specs() {
		for _, f := range section.Fields {
			target := nodeAt(&n, f.Path)
			if target == nil {
				continue
			}
			value := values.Get(f.Path)
			if f.Kind != "bool" && !values.Has(f.Path) && !values.Has(f.Path+".clear") {
				continue
			}
			switch f.Kind {
			case "bool":
				target.Tag = "!!bool"
				target.Value = strconv.FormatBool(value == "on")
			case "number":
				if _, e := strconv.Atoi(value); e != nil {
					return config.Config{}, fmt.Errorf("%s: an integer is required", f.Label)
				}
				target.Tag = "!!int"
				target.Value = value
			case "password":
				if values.Get(f.Path+".clear") == "on" {
					value = ""
				} else if value == "" {
					continue
				}
				target.Tag = "!!str"
				target.Value = value
			case "lines":
				if err := target.Encode(lines(value)); err != nil {
					return config.Config{}, err
				}
			default:
				target.Tag = "!!str"
				target.Value = value
			}
		}
	}
	var next config.Config
	if err := n.Decode(&next); err != nil {
		return config.Config{}, fmt.Errorf("Invalid field value; enter durations with a unit (e.g. 20s)")
	}
	next.Modules.Docker.ControlContainers.Allow = append(values["docker_allow"], lines(values.Get("docker_extra_allow"))...)
	next.Modules.Docker.ControlContainers.Deny = append(values["docker_deny"], lines(values.Get("docker_extra_deny"))...)
	var services []serviceForm
	decoder := json.NewDecoder(strings.NewReader(values.Get("services_json")))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&services); err != nil {
		return config.Config{}, fmt.Errorf("Invalid service list")
	}
	if len(services) > 100 {
		return config.Config{}, fmt.Errorf("Maximum of 100 service checks")
	}
	next.Modules.Services.Checks = nil
	for _, s := range services {
		d, err := time.ParseDuration(s.Timeout)
		if err != nil {
			return config.Config{}, fmt.Errorf("Service %s: invalid timeout", s.Name)
		}
		next.Modules.Services.Checks = append(next.Modules.Services.Checks, config.Check{Name: s.Name, Type: s.Type, URL: s.URL, ExpectedStatus: s.ExpectedStatus, Timeout: d, Host: s.Host, Port: s.Port, SystemdUnit: s.SystemdUnit, AllowControl: s.AllowControl})
	}
	next.ApplyCheckDefaults()
	return next, next.Validate()
}
