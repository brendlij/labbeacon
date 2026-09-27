package config

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"go.yaml.in/yaml/v3"
)

var ErrConflict = errors.New("config changed; reload the form before saving")

type Document struct {
	Config   Config
	Revision string
	YAML     []byte
}
type Store struct {
	Path string
	mu   sync.Mutex
}

func (s *Store) Read() (Document, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.read() }
func (s *Store) read() (Document, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return Document{}, err
	}
	cfg, err := Parse(data)
	if err != nil {
		return Document{}, err
	}
	return Document{Config: cfg, YAML: data, Revision: fmt.Sprintf("%x", sha256.Sum256(data))}, nil
}

// Save patches only changed effective values, preserving YAML comments and ENV
// references elsewhere. Environment overrides cannot be silently persisted as secrets.
func (s *Store) Save(revision string, next Config) (Document, error) {
	next = next.Clone()
	next.ApplyCheckDefaults()
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, err := s.read()
	if err != nil {
		return Document{}, err
	}
	if previous.Revision != revision {
		return Document{}, ErrConflict
	}
	if err = next.Validate(); err != nil {
		return Document{}, err
	}
	for env, path := range Overrides() {
		if _, set := os.LookupEnv(env); set {
			a := configValue(previous.Config, path)
			b := configValue(next, path)
			if !reflect.DeepEqual(a, b) {
				return Document{}, fmt.Errorf("%s is controlled by environment variable %s", path, env)
			}
		}
	}
	var raw, before, after yaml.Node
	if err = yaml.Unmarshal(previous.YAML, &raw); err != nil {
		return Document{}, err
	}
	if err = before.Encode(previous.Config); err != nil {
		return Document{}, err
	}
	if err = after.Encode(next); err != nil {
		return Document{}, err
	}
	patchNode(raw.Content[0], &before, &after)
	data, err := yaml.Marshal(&raw)
	if err != nil {
		return Document{}, err
	}
	resolved, err := Parse(data)
	if err != nil {
		return Document{}, err
	}
	// A literal '$' in an edited field must not accidentally become an ENV reference.
	var resolvedNode yaml.Node
	if err = resolvedNode.Encode(resolved); err != nil {
		return Document{}, err
	}
	if !equalNode(&resolvedNode, &after) {
		return Document{}, fmt.Errorf("saved values would differ after configuration resolution")
	}
	if err = AtomicWrite(s.Path, data); err != nil {
		return Document{}, err
	}
	return Document{Config: resolved, YAML: data, Revision: fmt.Sprintf("%x", sha256.Sum256(data))}, nil
}
func Overrides() map[string]string {
	return map[string]string{"AGENT_ID": "agent.id", "AGENT_NAME": "agent.name", "MQTT_BROKER": "mqtt.broker", "MQTT_USER": "mqtt.username", "MQTT_PASSWORD": "mqtt.password", "LOG_LEVEL": "agent.log_level", "POLL_INTERVAL": "agent.poll_interval", "EXPIRE_AFTER": "agent.expire_after"}
}
func configValue(c Config, path string) any {
	var n yaml.Node
	if err := n.Encode(c); err != nil {
		return nil
	}
	for _, part := range strings.Split(path, ".") {
		v := mappingValue(&n, part)
		if v == nil {
			return nil
		}
		n = *v
	}
	return n.Value
}
func mappingValue(n *yaml.Node, key string) *yaml.Node {
	if n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}
func equalNode(a, b *yaml.Node) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Kind != b.Kind || a.Tag != b.Tag || a.Value != b.Value || len(a.Content) != len(b.Content) {
		return false
	}
	for i := range a.Content {
		if !equalNode(a.Content[i], b.Content[i]) {
			return false
		}
	}
	return true
}
func patchNode(raw, before, after *yaml.Node) {
	if equalNode(before, after) {
		return
	}
	if raw.Kind == yaml.MappingNode && before.Kind == yaml.MappingNode && after.Kind == yaml.MappingNode {
		for i := 0; i < len(after.Content); i += 2 {
			key := after.Content[i].Value
			a := after.Content[i+1]
			b := mappingValue(before, key)
			if equalNode(b, a) {
				continue
			}
			target := mappingValue(raw, key)
			if target == nil {
				raw.Content = append(raw.Content, after.Content[i], literalNode(a))
			} else if b != nil {
				patchNode(target, b, a)
			} else {
				*target = *literalNode(a)
			}
		}
		return
	}
	if raw.Kind == yaml.SequenceNode && before.Kind == yaml.SequenceNode && after.Kind == yaml.SequenceNode {
		items := make([]*yaml.Node, 0, len(after.Content))
		used := map[int]bool{}
		for _, a := range after.Content {
			match := -1
			name := mappingValue(a, "name")
			for j, b := range before.Content {
				if used[j] || j >= len(raw.Content) {
					continue
				}
				oldName := mappingValue(b, "name")
				if equalNode(a, b) || (name != nil && oldName != nil && name.Value == oldName.Value) {
					match = j
					break
				}
			}
			if match >= 0 {
				used[match] = true
				item := raw.Content[match]
				patchNode(item, before.Content[match], a)
				items = append(items, item)
			} else {
				items = append(items, literalNode(a))
			}
		}
		raw.Content = items
		return
	}
	head, line, foot := raw.HeadComment, raw.LineComment, raw.FootComment
	*raw = *literalNode(after)
	raw.HeadComment, raw.LineComment, raw.FootComment = head, line, foot
}
func literalNode(n *yaml.Node) *yaml.Node {
	copy := *n
	copy.Content = nil
	for _, child := range n.Content {
		copy.Content = append(copy.Content, literalNode(child))
	}
	if copy.Kind == yaml.ScalarNode && copy.Tag == "!!str" {
		copy.Value = strings.ReplaceAll(copy.Value, "$", "$$")
	}
	return &copy
}

// AtomicWrite writes within the same directory and then atomically replaces the
// original. It never truncates the live file and refuses symlink destinations.
func AtomicWrite(path string, data []byte) (err error) {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("config must be a regular file, not a symlink")
	}
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".homelab-config-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer func() {
		if e := os.Remove(name); e != nil && !os.IsNotExist(e) && err == nil {
			err = e
		}
	}()
	// Keep restrictive permissions; never make a newly saved config world-readable.
	mode := info.Mode().Perm() & 0600
	if mode&0600 != 0600 {
		mode = 0600
	}
	if err = f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return replaceFile(name, path)
}
