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

	"go.yaml.in/yaml/v3"
	"homelab-agent/internal/config"
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
		{Title: "Agent & MQTT", Hint: "Intervall und Ablaufzeit gelten live. Identität, Log-Level und MQTT-Verbindung erfordern einen Neustart.", Fields: []field{
			{Path: "agent.id", Label: "Agent-ID", Kind: "text"}, {Path: "agent.name", Label: "Name", Kind: "text"}, {Path: "agent.poll_interval", Label: "Messintervall (z. B. 20s)", Kind: "text"}, {Path: "agent.expire_after", Label: "Werte gültig für (z. B. 60s)", Kind: "text"}, {Path: "agent.log_level", Label: "Log-Level: debug / info / warn / error", Kind: "text"}, {Path: "agent.metrics_enabled", Label: "Agent-Version und Uptime", Kind: "bool"},
			{Path: "mqtt.broker", Label: "MQTT-Broker", Kind: "text"}, {Path: "mqtt.username", Label: "MQTT-Benutzer", Kind: "text"}, {Path: "mqtt.password", Label: "MQTT-Passwort", Kind: "password"}, {Path: "mqtt.discovery_prefix", Label: "Discovery-Präfix", Kind: "text"}}},
		{Title: "System", Hint: "Lokale Messwerte. Listen: ein Eintrag je Zeile.", Fields: []field{
			{Path: "modules.system.enabled", Label: "System-Modul aktiv", Kind: "bool"}, {Path: "modules.system.disk_paths", Label: "Disk-Pfade", Kind: "lines"}, {Path: "modules.system.temperature", Label: "CPU-Temperatur", Kind: "bool"}, {Path: "modules.system.processes", Label: "Top-Prozesse", Kind: "bool"}, {Path: "modules.system.top_n", Label: "Top-N (1–100)", Kind: "number"}, {Path: "modules.system.file_descriptors", Label: "Dateideskriptoren", Kind: "bool"}, {Path: "modules.system.boot_time", Label: "Boot-Zeitpunkt", Kind: "bool"}}},
		{Title: "Docker", Hint: "Monitoring und Steuerung sind getrennt. Freigaben wählst du weiter unten.", Fields: []field{
			{Path: "modules.docker.enabled", Label: "Docker-Modul aktiv", Kind: "bool"}, {Path: "modules.docker.socket_path", Label: "Socket-Pfad", Kind: "text"}, {Path: "modules.docker.timeout", Label: "Inventar-Timeout", Kind: "text"}, {Path: "modules.docker.stats", Label: "Container CPU / RAM", Kind: "bool"}, {Path: "modules.docker.control_containers.enabled", Label: "Container-Steuerung erlauben", Kind: "bool"}, {Path: "modules.docker.image_updates.enabled", Label: "Anonyme Image-Update-Prüfung", Kind: "bool"}, {Path: "modules.docker.image_updates.interval", Label: "Update-Prüfintervall", Kind: "text"}, {Path: "modules.docker.image_updates.timeout", Label: "Registry-Timeout", Kind: "text"}}},
		{Title: "Services & VPN", Hint: "HTTP/TCP-Checks bearbeitest du in der Serviceliste. VPN benötigt die jeweilige lokale CLI.", Fields: []field{
			{Path: "modules.services.enabled", Label: "Service-Checks aktiv", Kind: "bool"}, {Path: "modules.tailscale.enabled", Label: "Tailscale aktiv", Kind: "bool"}, {Path: "modules.tailscale.command", Label: "Tailscale-Programm", Kind: "text"}, {Path: "modules.tailscale.timeout", Label: "Tailscale-Timeout", Kind: "text"}, {Path: "modules.netbird.enabled", Label: "NetBird aktiv", Kind: "bool"}, {Path: "modules.netbird.command", Label: "NetBird-Programm", Kind: "text"}, {Path: "modules.netbird.timeout", Label: "NetBird-Timeout", Kind: "text"}}},
		{Title: "Netzwerk", Hint: "Die öffentliche IP wird über einen gecachten HTTPS-Abruf ermittelt.", Fields: []field{
			{Path: "modules.network.enabled", Label: "Netzwerk-Modul aktiv", Kind: "bool"}, {Path: "modules.network.local_ips", Label: "Lokale IP-Adressen", Kind: "bool"}, {Path: "modules.network.public_ip.enabled", Label: "Öffentliche IP", Kind: "bool"}, {Path: "modules.network.public_ip.endpoint", Label: "HTTPS-Endpoint", Kind: "text"}, {Path: "modules.network.public_ip.interval", Label: "IP-Cacheintervall", Kind: "text"}, {Path: "modules.network.public_ip.timeout", Label: "IP-Abfrage-Timeout", Kind: "text"}}},
		{Title: "Steuerungsaktionen", Hint: "Aktivieren erlaubt zukünftige MQTT-Kommandos. Diese Seite führt keine Steuerungsaktion aus. Befehle bleiben in der lokalen YAML konfiguriert.", Fields: []field{
			{Path: "control_actions.enabled", Label: "Steuerungsaktionen – Hauptschalter", Kind: "bool"}, {Path: "agent_control.enabled", Label: "Agent-Restart erlauben (Supervisor nötig)", Kind: "bool"}, {Path: "host_control.enabled", Label: "Host-Steuerung erlauben", Kind: "bool"}, {Path: "host_control.timeout", Label: "Host-Befehls-Timeout", Kind: "text"}, {Path: "host_control.reboot.enabled", Label: "Reboot-Aktion aktiv", Kind: "bool"}, {Path: "host_control.reboot.confirm_required", Label: "Reboot braucht MQTT-Bestätigung", Kind: "bool"}, {Path: "host_control.shutdown.enabled", Label: "Shutdown-Aktion aktiv", Kind: "bool"}, {Path: "host_control.shutdown.confirm_required", Label: "Shutdown braucht MQTT-Bestätigung", Kind: "bool"}}},
		{Title: "Web-UI", Hint: "Änderungen in diesem Abschnitt erfordern einen Neustart. Ohne TLS wird Basic Auth unverschlüsselt transportiert.", Fields: []field{
			{Path: "webui.enabled", Label: "Web-UI aktiv", Kind: "bool"}, {Path: "webui.bind_address", Label: "Bind-Adresse", Kind: "text"}, {Path: "webui.port", Label: "Port", Kind: "number"}, {Path: "webui.username", Label: "Basic-Auth-Benutzer", Kind: "text"}, {Path: "webui.password", Label: "Basic-Auth-Passwort", Kind: "password"}, {Path: "webui.allowed_hosts", Label: "Erlaubte DNS-Namen (ohne Port)", Kind: "lines"}}},
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
			v.Status = "zurzeit nicht gefunden"
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
		return config.Config{}, fmt.Errorf("Ungültiges Formular")
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
					return config.Config{}, fmt.Errorf("%s: ganze Zahl erforderlich", f.Label)
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
		return config.Config{}, fmt.Errorf("Feldwert ungültig; Zeiten bitte mit Einheit eingeben (z. B. 20s)")
	}
	next.Modules.Docker.ControlContainers.Allow = append(values["docker_allow"], lines(values.Get("docker_extra_allow"))...)
	next.Modules.Docker.ControlContainers.Deny = append(values["docker_deny"], lines(values.Get("docker_extra_deny"))...)
	var services []serviceForm
	decoder := json.NewDecoder(strings.NewReader(values.Get("services_json")))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&services); err != nil {
		return config.Config{}, fmt.Errorf("Serviceliste ungültig")
	}
	if len(services) > 100 {
		return config.Config{}, fmt.Errorf("Maximal 100 Service-Checks")
	}
	next.Modules.Services.Checks = nil
	for _, s := range services {
		d, err := time.ParseDuration(s.Timeout)
		if err != nil {
			return config.Config{}, fmt.Errorf("Service %s: Timeout ungültig", s.Name)
		}
		next.Modules.Services.Checks = append(next.Modules.Services.Checks, config.Check{Name: s.Name, Type: s.Type, URL: s.URL, ExpectedStatus: s.ExpectedStatus, Timeout: d, Host: s.Host, Port: s.Port, SystemdUnit: s.SystemdUnit, AllowControl: s.AllowControl})
	}
	next.ApplyCheckDefaults()
	return next, next.Validate()
}
